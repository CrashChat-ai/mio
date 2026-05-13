# Local deploy

Bring up Postgres + NATS + MinIO for local development:

```bash
make up
```

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
