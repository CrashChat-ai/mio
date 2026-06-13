# mio-web Deployment

`mio-web` is the internal operator console, decoupled into two processes behind
a single public origin:

- **API** (`mio-web-api`, Go BFF) serves JSON only at `/api/*`, `/auth/*`, and
  `/healthz`. No SPA is bundled; `go:embed` was removed.
- **Frontend** (`mio-web-frontend`) is a static React build (Vite) served by a
  tiny Caddy image; the SPA calls the API over `/api`.
- **Reverse proxy** fronts both on one origin: `/api`, `/auth`, `/healthz` →
  API; everything else → frontend. Same origin keeps the `SameSite=Lax` session
  cookie flowing with no CORS by default.

The browser never calls AdminService directly; the API BFF is the only caller.

## Why single origin

The OAuth state cookie is scoped `Path=/auth/callback`. If `/auth/*` did not
reach the API on the same origin as the SPA, the callback would not receive the
state cookie and login would fail. The proxy/ingress MUST route `/api`, `/auth`,
and `/healthz` to the API and the catch-all `/` to the frontend, with the
specific prefixes declared first so the SPA never shadows the auth routes.

## Topology

The frontend and API are separate containers/pods. The API calls AdminService:

- Separate pod: `mio-web-api` calls AdminService through an internal service DNS
  name such as `http://mio-admin:9090`.
- Same pod: `mio-web-api` may call `http://127.0.0.1:9090` only when
  AdminService is a sidecar in the same pod.

Do not configure a standalone `mio-web-api` pod to call another pod's
`127.0.0.1`. That points back at the API pod, not AdminService.

For the separate-pod topology, AdminService must bind a non-loopback address,
for example:

```bash
MIO_ADMIN_ADDR=0.0.0.0:9090
MIO_ADMIN_ALLOW_CIDRS=<mio-web-pod-cidr-or-node-cidr>
```

Use NetworkPolicy to restrict `mio-web` egress to AdminService, Postgres, DNS,
and Google HTTPS token/userinfo endpoints.

## Required Settings

### API process (`mio-web-api`)

| Setting | Purpose |
|---|---|
| `MIO_WEB_ADDR` | API listen address. Defaults to `:8080`. The proxy/ingress targets this port. |
| `MIO_WEB_PUBLIC_URL` | Browser-facing single origin, for example `https://mio-web.example.com`. Used to build the OAuth redirect URL. |
| `MIO_WEB_AUTH_MODE` | `google` in deployed environments; `dev` only for local compose. |
| `MIO_WEB_OPERATOR_EMAILS` / `MIO_WEB_OPERATOR_DOMAINS` | Allowlist checked before any admin route is served. |
| `MIO_WEB_OPERATOR_DEFAULT_ROLE` | Role for allowed operators without an explicit assignment. Defaults to `viewer`. |
| `MIO_WEB_OPERATOR_ROLES` | Comma-separated `email=role` or `domain=role` entries. Roles: `viewer`, `operator`, `credential-admin`. |
| `MIO_WEB_DATABASE_DSN` | Postgres DSN for operator sessions and mutation audit logs. Gateway migrations create `web_operator_sessions` and `web_operator_audit`. |
| `MIO_WEB_GOOGLE_CLIENT_ID` / `MIO_WEB_GOOGLE_CLIENT_SECRET` | Google OAuth client for operator login. |
| `MIO_WEB_OIDC_REDIRECT_URL` | Optional explicit OAuth redirect URL. When unset, derived from `MIO_WEB_PUBLIC_URL + /auth/callback`. |
| `MIO_WEB_STATE_SECRET` | High-entropy secret for signing OAuth state cookies. |
| `MIO_WEB_CORS_ORIGINS` | Comma-separated allowlist of cross-origin SPA origins. **Empty by default** — single-origin deploys need no CORS. Only set when the frontend is served from a different origin than the API. |
| `MIO_ADMIN_URL` | Internal AdminService URL. Use service DNS, not cross-pod loopback. |

### Frontend process (`mio-web-frontend`)

| Setting | Purpose |
|---|---|
| `VITE_API_BASE_URL` | Build-time API base. **Empty for single-origin** (the SPA calls relative `/api`). Set only for true cross-origin deploys, for example `https://api.mio.example.com`. |
| `VITE_DEV_API_TARGET` | Vite dev-server proxy target (bare-metal dev only). Defaults to `http://localhost:8081` (the local proxy origin). |

The Helm chart deploys two workloads (`*-api` and `*-frontend`) plus one
ingress that does the single-origin routing — there is no separate proxy pod in
the cluster, the ingress IS the proxy. It maps these from values and existing
Kubernetes Secrets:

```bash
helm upgrade --install mio-web deploy/charts/mio-web \
  --namespace mio \
  --set web.publicUrl=https://mio-web.example.com \
  --set ingress.enabled=true \
  --set ingress.host=mio-web.example.com \
  --set admin.url=http://mio-admin:9090 \
  --set operators.roles[0]=you@example.com=credential-admin \
  --set session.databaseSecretName=mio-gateway-secrets \
  --set google.existingSecret=mio-web-oauth \
  --set stateSecret.existingSecret=mio-web-session
```

The ingress declares the API prefixes before the catch-all so the SPA never
shadows auth routes:

```yaml
ingress:
  apiPaths:
    - { path: /api,     pathType: Prefix }
    - { path: /auth,    pathType: Prefix }
    - { path: /healthz, pathType: Exact }
  # path / (catch-all) -> frontend service
```

Images: `ghcr.io/crashchat-ai/mio/web-api` (Go API, `ui/web/Dockerfile`) and
`ghcr.io/crashchat-ai/mio/web-frontend` (static SPA, `ui/web/Dockerfile.frontend`).

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

Run Postgres, NATS, gateway migrations, AdminService, and the three web
processes (api + frontend + Caddy proxy) via the `operator` compose profile:

```bash
make operator-web-up
```

Defaults (single proxied origin):

- Operator console (Caddy proxy): http://localhost:8081
  - `/api`, `/auth`, `/healthz` → `mio-web-api:8080`
  - `/*` → `mio-web-frontend:80`
- AdminService: http://localhost:9090
- Auth mode: `dev`
- Allowed operator: `operator@localhost`
- Dev role: `credential-admin`

For bare-metal Vite dev (no Docker frontend), run the API on `:8080` and
`pnpm --dir ui/web/app dev`; the Vite proxy forwards `/api`, `/auth`, `/healthz`
to `VITE_DEV_API_TARGET` (default `http://localhost:8081`). Set it to
`http://localhost:8080` if you run the API directly without the Caddy proxy.

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
- Login hangs / state-cookie missing: the proxy/ingress routes `/auth/*` to the
  frontend instead of the API, so the `Path=/auth/callback` state cookie never
  reaches the API. Verify `/api`, `/auth`, `/healthz` resolve to the API origin.
- Blank page or `not_found` JSON from the frontend: the catch-all `/` route is
  pointing at the API instead of the frontend, or an `/api` prefix leaked to the
  SPA.
