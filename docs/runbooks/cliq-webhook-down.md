# Runbook: Cliq webhook down

**Symptom:** Zoho Cliq returns 5xx (or no echo arrives) when posting to
`https://mio.abspectrumservices.org/cliq`.

**Audience:** anyone on call. First-five-minute diagnosis tree only —
deeper failure modes (jetstream-degraded, outbound-rate-limit) live in
P10 runbooks.

**Cluster:** `dp-prod-7e26` · **Namespace:** `mio`

## 1. Are gateway pods healthy?

```bash
kubectl -n mio get pods -l app.kubernetes.io/name=mio-gateway
```

- All `Ready 1/1` → continue to step 2.
- `CrashLoopBackOff` → `kubectl -n mio describe pod <name>`; common: DB
  not reachable (CNPG bootstrap race) or missing Secret.
- `ImagePullBackOff` → `ghcr-pull` Secret missing or PAT expired.
  Re-encrypt `apps/prod/mio/ghcr-pull.enc.yaml` with a fresh token.

## 2. Recent gateway log lines

```bash
kubectl -n mio logs -l app.kubernetes.io/name=mio-gateway --tail=100 \
  | grep -E "bad_signature|publish|error"
```

- `bad_signature` → HMAC mismatch. Cliq bot UI secret ≠ cluster Secret.
  Compare against playground `secrets.env`; if rotated, see step 5.
- `publish error` → JetStream not reachable. Continue to step 3.
- Nothing logged → ingress never delivered the request. Continue to step 4.

## 3. JetStream stream present?

```bash
kubectl -n mio exec deploy/mio-nats -- nats stream info MESSAGES_INBOUND
```

- `Messages: <n>` accumulating → stream healthy; consumer is the problem
  (see `kubectl -n mio logs -l app.kubernetes.io/name=mio-echo-consumer`).
- Stream missing → gateway never bootstrapped it. Restart the gateway
  Deployment to retrigger `AddOrUpdateStream`.
- NATS pod down → `kubectl -n mio get pods -l app.kubernetes.io/name=nats`.
  POC is single-replica + emptyDir; pod loss = data loss (Risk #1).

## 4. Ingress reachable from outside?

```bash
curl -I https://mio.abspectrumservices.org/cliq
```

- `HTTP/2 405` → routing + TLS work; problem is Cliq-side (bot disabled,
  webhook URL wrong, network egress from Zoho blocked).
- TLS error → `kubectl -n mio describe cert mio-gateway-tls`; if not
  Ready, check cert-manager logs and DNS.
- Connection refused / timeout → DNS A record or ingress LB IP mismatch.
  `kubectl -n ingress-nginx get svc nginx-ingress-controller`.

## 5. Rotate `CLIQ_WEBHOOK_SECRET`

Production secret lives in `infra/fluxcd/apps/prod/mio/secrets.enc.yaml`,
SOPS-encrypted with the age key at `infra/.secrets/age-key.txt`.

```bash
cd ~/git/work/ab-spectrum/infra
SOPS_AGE_KEY_FILE=.secrets/age-key.txt sops fluxcd/apps/prod/mio/secrets.enc.yaml
# edit CLIQ_WEBHOOK_SECRET → save → encrypted on write
git add -A && git commit -m "chore(mio): rotate cliq webhook secret"
git push
flux reconcile kustomization mio --with-source   # force reconcile
```

Then update the secret in the Zoho Cliq bot UI to match. **Order matters:**
push the cluster change first; if you rotate Cliq UI first, every webhook
fails with `bad_signature` until Flux catches up.

## Escalate

If none of the above resolves within ~5 min, page the on-call engineer
and capture: `kubectl -n mio get all`, recent gateway + nats + echo-consumer
logs, and `flux get kustomizations -A | grep mio`.
