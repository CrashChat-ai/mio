# mio-web Deployment

`mio-web` is the internal operator console. It is a Go BFF with an embedded
React SPA. Browser traffic terminates at `mio-web`; the browser never calls
AdminService directly.

## Topology

Use one of these topologies:

- Separate pod: `mio-web` calls AdminService through an internal service DNS
  name such as `http://mio-admin:9090`.
- Same pod: `mio-web` may call `http://127.0.0.1:9090` only when AdminService
  is a sidecar in the same pod.

Do not configure a standalone `mio-web` pod to call another pod's
`127.0.0.1`. That points back at the web pod, not AdminService.

For the separate-pod topology, AdminService must bind a non-loopback address,
for example:

```bash
MIO_ADMIN_ADDR=0.0.0.0:9090
MIO_ADMIN_ALLOW_CIDRS=<mio-web-pod-cidr-or-node-cidr>
```

Use NetworkPolicy to restrict `mio-web` egress to AdminService, Postgres, DNS,
and Google HTTPS token/userinfo endpoints.

## Required Settings

| Setting | Purpose |
|---|---|
| `MIO_WEB_PUBLIC_URL` | Browser-facing URL, for example `https://mio-web.example.com`. |
| `MIO_WEB_AUTH_MODE` | `google` in deployed environments; `dev` only for local compose. |
| `MIO_WEB_OPERATOR_EMAILS` / `MIO_WEB_OPERATOR_DOMAINS` | Allowlist checked before any admin route is served. |
| `MIO_WEB_OPERATOR_DEFAULT_ROLE` | Role for allowed operators without an explicit assignment. Defaults to `viewer`. |
| `MIO_WEB_OPERATOR_ROLES` | Comma-separated `email=role` or `domain=role` entries. Roles: `viewer`, `operator`, `credential-admin`. |
| `MIO_WEB_DATABASE_DSN` | Postgres DSN for operator sessions and mutation audit logs. Gateway migrations create `web_operator_sessions` and `web_operator_audit`. |
| `MIO_WEB_GOOGLE_CLIENT_ID` / `MIO_WEB_GOOGLE_CLIENT_SECRET` | Google OAuth client for operator login. |
| `MIO_WEB_STATE_SECRET` | High-entropy secret for signing OAuth state cookies. |
| `MIO_ADMIN_URL` | Internal AdminService URL. Use service DNS, not cross-pod loopback. |

The Helm chart maps these from values and existing Kubernetes Secrets:

```bash
helm upgrade --install mio-web deploy/charts/mio-web \
  --namespace mio \
  --set web.publicUrl=https://mio-web.example.com \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=mio-web.example.com \
  --set admin.url=http://mio-admin:9090 \
  --set operators.roles[0]=you@example.com=credential-admin \
  --set session.databaseSecretName=mio-gateway-secrets \
  --set google.existingSecret=mio-web-oauth \
  --set stateSecret.existingSecret=mio-web-session
```

## OAuth Registrations

MIO needs two different OAuth registrations:

1. Provider install OAuth, such as Zoho Cliq, uses AdminService callback:
   `MIO_ADMIN_PUBLIC_URL + /oauth/callback`.
2. Operator login OAuth uses `mio-web` callback:
   `MIO_WEB_PUBLIC_URL + /auth/callback`, or `MIO_WEB_OIDC_REDIRECT_URL` when
   explicitly set.

Keep these clients separate. Reusing the provider-install client for operator
login couples unrelated scopes and makes callback mismatch failures harder to
debug.

Google operator-login client setup:

- Application type: Web application.
- Authorized redirect URI: `https://mio-web.example.com/auth/callback`.
- Store client credentials in the Secret configured by `google.existingSecret`.

## Local Operator Stack

Run Postgres, NATS, gateway migrations, AdminService, and `mio-web`:

```bash
make operator-web-up
```

Defaults:

- `mio-web`: http://localhost:8081
- AdminService: http://localhost:9090
- Auth mode: `dev`
- Allowed operator: `operator@localhost`
- Dev role: `credential-admin`

For a local Google login test, override:

```bash
MIO_WEB_AUTH_MODE=google \
MIO_WEB_PUBLIC_URL=http://localhost:8081 \
MIO_WEB_COOKIE_SECURE=false \
MIO_WEB_OPERATOR_EMAILS=you@example.com \
MIO_WEB_OPERATOR_ROLES=you@example.com=operator \
MIO_WEB_GOOGLE_CLIENT_ID=... \
MIO_WEB_GOOGLE_CLIENT_SECRET=... \
MIO_WEB_STATE_SECRET="$(openssl rand -base64 32)" \
make operator-web-up
```

Register `http://localhost:8081/auth/callback` in the Google OAuth client.

## Failure Modes

- `google_login_not_configured`: missing Google client ID, client secret, or
  redirect URL while `MIO_WEB_AUTH_MODE=google`.
- `operator_not_allowed`: the Google email is valid but absent from
  `MIO_WEB_OPERATOR_EMAILS` and `MIO_WEB_OPERATOR_DOMAINS`.
- `admin_unavailable`: `MIO_ADMIN_URL` is wrong, AdminService is down, or a
  NetworkPolicy blocks web-to-admin egress.
- AdminService returns 403: `MIO_ADMIN_ALLOW_CIDRS` does not include the
  source CIDR used by `mio-web`.
- Callback mismatch: Google client redirect URI does not exactly match
  `MIO_WEB_PUBLIC_URL + /auth/callback` or `MIO_WEB_OIDC_REDIRECT_URL`.
