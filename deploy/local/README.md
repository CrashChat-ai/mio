# Local deploy

**Turnkey Zoho Cliq loop (no real Zoho org, no cluster):**

```bash
make cliq-up        # gateway + cliq-mock + media-vault enrich + echo + db-seed
make cliq-replay    # synthetic Cliq inbound (channel-text) → full round-trip
make cliq-smoke     # assert outbound reached cliq-mock (204)
```

See **[docs/local-dev-mio-cliq.md](../../docs/local-dev-mio-cliq.md)** for the full
walkthrough (the round-trip, the `body_json`/signature contract, channel-pulse hookup,
and the optional `make ingest-prod` real-data path).

Bring up the default stack (Postgres, NATS, MinIO, gateway, cliq-mock, db-seed,
sink-gcs) — the `media` and `operator` profiles stay down:

```bash
make up
```

Bring up the local operator stack (`gateway`, AdminService, and the decoupled
`mio-web` API + static frontend behind a Caddy reverse proxy):

```bash
make operator-web-up
```

The operator console is served on one origin (http://localhost:8081) by the
`mio-web-proxy` Caddy service, which routes `/api`, `/auth`, `/healthz` to
`mio-web-api` and everything else to the static `mio-web-frontend`. Same origin
keeps the session cookie flowing without CORS. Dev auth signs in
`operator@localhost`. See `docs/mio-web-deployment.md` for Google OAuth setup.

## Environment variables

The gateway and admin binaries read configuration from environment variables
plus file-mounted secrets (`/etc/mio/secrets/`). The control-plane work added
the variables below; everything else lives in `services/gateway/internal/config`.

| Variable | Default | Description |
|---|---|---|
| `MIO_ENV` | `dev` | Deploy environment. One of `dev`, `staging`, `prod`. Gates `NoopCipher` (panics outside `dev`) and the all-in-one binary's memory-storage guard rail (forbidden in `prod`). |
| `MIO_AGE_KEY_FILE` | _empty_ | Path to an age identity file. Read by `services/gateway/internal/crypto.NewAgeFileCipher` to encrypt credentials at rest. In `dev` an empty value falls back to `NoopCipher`; outside `dev` the admin binary refuses to start without it. |
| `MIO_ADMIN_ADDR` | `127.0.0.1:9090` | Admin connect-go listener bind. Loopback by default; widen via `MIO_ADMIN_ALLOW_CIDRS` or reverse proxy for non-local deploys. |
| `MIO_ADMIN_PUBLIC_URL` | `http://127.0.0.1:9090` | External URL pointing at the admin listener (or its reverse proxy). Used to build `redirect_uri` for OAuth callbacks. |
| `MIO_ADMIN_ALLOW_CIDRS` | _empty_ | Comma-separated CIDR list permitted to call admin RPCs. Loopback (`127.0.0.1`, `::1`) is always allowed; anything else returns 403. |
| `MIO_WEB_PUBLIC_URL` | `http://localhost:8081` | Reverse-proxy origin for local `mio-web`; Google login redirects to `/auth/callback` under this URL. |
| `MIO_WEB_AUTH_MODE` | `dev` in compose | `dev` signs in `MIO_WEB_DEV_OPERATOR_EMAIL`; deployed environments should use `google`. |
| `MIO_WEB_OPERATOR_EMAILS` / `MIO_WEB_OPERATOR_DOMAINS` | `operator@localhost` / _empty_ | Operator allowlist enforced before admin routes are served. |
| `MIO_WEB_OPERATOR_DEFAULT_ROLE` / `MIO_WEB_OPERATOR_ROLES` | `viewer` / `operator@localhost=credential-admin` | Role assignments for allowed operators. |
| `MIO_WEB_DEV_OPERATOR_ROLE` | `credential-admin` | Role used by local dev-auth sessions. |
| `MIO_WEB_DATABASE_DSN` | local Postgres DSN | Postgres-backed operator sessions and mutation audit logs. |
| `CLIQ_CLIENT_ID` / `CLIQ_CLIENT_SECRET` / `CLIQ_REFRESH_TOKEN` | _empty_ | Zoho Cliq OAuth credentials. The refresh token is provisioned once via the admin's OAuth dance (`StartInstall` → `/oauth/callback` → `CompleteInstall`). |
| `CLIQ_REDIRECT_URI` | _empty_ | The redirect URI registered with Zoho; consumed by `tokenCredentials.AuthorizeURL`. Must match `MIO_ADMIN_PUBLIC_URL + /oauth/callback` for the admin-driven install flow. |
| `CLIQ_OAUTH_SCOPE` | `ZohoCliq.Webhooks.CREATE,ZohoCliq.messages.CREATE` | Override the default OAuth scope; rarely needed. |

## Generating an age key

```bash
age-keygen -o deploy/local/secrets/age-key.txt
export MIO_AGE_KEY_FILE=$PWD/deploy/local/secrets/age-key.txt
```

The first line of the generated file is `# created: ...`, second is the
public key, third is `AGE-SECRET-KEY-...`. Keep the file mode at `0o600`.

## All-in-one binary

See `services/gateway/cmd/all-in-one` (introduced in Phase 5). Boots an embedded
NATS JetStream + the gateway in one process. Use `make run-laptop` for
memory storage (volatile) or `make run-laptop-persist` for file storage
(survives restart). Production deploys should continue to use the
external NATS cluster — the embedded server is single-node only.
