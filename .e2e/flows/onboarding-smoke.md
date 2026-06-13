# Flow: operator onboarding + auth-path-routing smoke

Verifies the operator-facing onboarding features AND the decoupled stack's
auth-path routing end-to-end on the local compose stack (single Caddy proxy
origin, ports per /tmp/mio-e2e.env: proxy/web :18081, admin :19090,
gateway :18080).

## Preconditions

- `docker compose --profile operator` healthy: mio-web-api, mio-web-frontend,
  mio-web-proxy, admin, gateway all up.
- Seeded via admin RPC: tenant `e2e-tenant`, one zoho_cliq account with
  external_id 637446511 (matches captured fixtures).
- Wire-level fixtures already POSTed (see e2e runner) so streams have data.

## Steps

1. **Auth-path routing through the proxy (the P3 invariant):**
   - `GET /auth/login` (no redirect follow) → status 302 with `Location: /`,
     AND a `mio_web_session` cookie set in the response. This proves the proxy
     routes `/auth/*` to the API (not the SPA) on the same origin, so the
     `Path=/auth/callback` OAuth state cookie / session cookie can flow. If the
     SPA had answered, there would be no 302 and no Set-Cookie → FAIL.
   - In `google` auth mode, `GET /auth/login` instead 302s to accounts.google.com
     and sets the `Path=/auth/callback` state cookie; the callback at
     `/auth/callback` MUST reach the API on the same origin or login breaks.
     (Dev mode is the automated path; this line documents the prod invariant.)
2. Open `/` → SPA `/login` page → click "Continue as dev operator" (anchor to
   `/auth/login`) → session created → land on `/dashboard`.
3. Assert session chip / operator identity visible (credential-admin role) —
   sourced from `GET /api/session` through the proxy.
4. Accounts panel: assert seeded account row (zoho_cliq, display name) renders.
5. Onboarding/Webhook panel for the account:
   - webhook URL shown = `http://localhost:18080/webhooks/zoho-cliq` (gateway base, NOT :19090 admin)
   - route alias `/cliq` listed
   - auth_kind `oauth2_refresh` + setup hint rendered
6. Stream health table: consumers visible with pending counts (after fixtures).
7. Channel types view: zoho_cliq capabilities render (threads, edit flags).
8. Credentials panel: metadata only — assert NO plaintext token anywhere.

## Verdict criteria

PASS = step-1 auth routing holds (`/auth/login` 302 + Set-Cookie from the API
origin), all onboarding asserts hold with trace evidence (screenshots + zero 5xx
network entries except deliberate negative tests). Any plaintext credential in
DOM or network = FAIL regardless of other results. `/auth/*` served by the SPA
(no 302, no cookie) = FAIL (broken single-origin routing).
