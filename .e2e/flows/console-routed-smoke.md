# Flow: decoupled routed console smoke

Verifies the TanStack-routed MIO operator console served by the **decoupled
stack** (pure React frontend + API-only Go server) behind a single Caddy proxy
origin. Everything is reached through one base URL (ports per /tmp/mio-e2e.env:
proxy/web :18081, admin :19090, gateway :18080). Styling reference:
.workbench/visuals/mio-console/mio-console-dashboard.html.

## Topology under test (single origin :18081)

- `/api`, `/auth`, `/healthz` → `mio-web-api:8080` (Go JSON BFF)
- `/*` → `mio-web-frontend:80` (static React build)
- Same origin ⇒ session cookie (SameSite=Lax) and OAuth state cookie
  (Path=/auth/callback) both flow; no CORS.

## Preconditions

- `docker compose --profile operator` healthy: mio-web-api, mio-web-frontend,
  mio-web-proxy all up; admin + gateway up.
- Seeded via /tmp/mio-e2e-wire.sh: tenant `e2e-tenant`, one zoho_cliq account
  external_id 637446511; signed Cliq fixtures POSTed so streams + tables have data.

## Steps (route-driven, all through the proxy origin)

0. **Proxy routing asserts (no browser needed):**
   - `GET /healthz` → 200 (API reached through proxy).
   - `GET /api/session` → 200 JSON `{authenticated:false, authMode:"dev"}`
     before login (API reached, not the SPA `not_found`).
   - `GET /auth/login` → 302 to `/` (dev login handled by API on same origin —
     proves `/auth/*` routes to API, not the frontend). See onboarding-smoke for
     the cookie/callback assertion.
   - `GET /` → 200 HTML SPA shell (frontend reached via catch-all).
1. `/` → SPA `/login` page → click "Continue as dev operator" (anchor to
   `/auth/login`) → 302 back to `/` → redirects to `/dashboard`. Assert sidebar
   nav renders all 9 items (sentence case: Dashboard, Tenants, Accounts,
   Onboarding, Stream health, Live tail, Channel types, Audit) + operator
   identity chip (from `GET /api/session`).
2. Dashboard: assert stat cards (Tenants/Accounts/Channel types/Messages), stream
   health table with consumers incl. ai-consumer-enriched@MESSAGES_INBOUND_ENRICHED,
   and the degraded-banner deep-link if a consumer lags. Sentence-case headings.
   All data comes from `/api/admin/*` through the proxy.
3. Navigate `/tenants`: DataTable renders the seeded tenant row (e2e-tenant). Click
   row → side-panel detail opens (history-aware; URL reflects selection).
4. `/accounts`: table shows the seeded account; row → side panel with tabs incl.
   Credentials; assert external_id 637446511, NO plaintext token in DOM.
5. `/onboarding`: steps wizard renders; webhook URL chip shows gateway base
   (http://localhost:18080/webhooks/zoho-cliq); Copy button present (aria-live).
6. `/health`: stream-health route — consumers + pending/ack columns, refetch toggle.
7. `/tail`: live-tail route — connect with the seeded account, assert message rows
   render (or honest empty state), pause/filter controls present.
8. `/channel-types`: zoho_cliq capabilities render (oauth2_refresh, 10/s, threads/edit).
9. `/audit`: audit route renders (table or empty state); Refresh button.

## Verdict criteria

PASS = step-0 proxy asserts hold (api/auth/healthz reach the API, `/` reaches the
SPA), all routes render their surface with real data over `/api/*`, zero
console/network 5xx in trace (except deliberate negatives), DataTable +
side-panel + wizard function, no plaintext credential anywhere, sentence-case
consistent. Any unhandled route error boundary, blank route, or `/auth`/`/api`
request served by the SPA shell = FAIL.
