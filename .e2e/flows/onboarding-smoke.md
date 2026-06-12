# Flow: operator onboarding smoke

Verifies the pure-gateway refactor's operator-facing features end-to-end on the
local compose stack (ports per /tmp/mio-e2e.env).

## Preconditions

- Stack healthy (gateway :18080, admin :19090, mio-web :18081)
- Seeded via admin RPC: tenant `e2e-tenant`, one zoho_cliq account with
  external_id 637446511 (matches captured fixtures)
- Wire-level fixtures already POSTed (see e2e runner) so streams have data

## Steps

1. Open `/` → dev-mode login screen → complete dev login (operator@localhost).
2. Assert session chip / operator identity visible (credential-admin role).
3. Accounts panel: assert seeded account row (zoho_cliq, display name) renders.
4. Onboarding/Webhook panel for the account:
   - webhook URL shown = `http://localhost:18080/webhooks/zoho-cliq` (gateway base, NOT :19090 admin)
   - route alias `/cliq` listed
   - auth_kind `oauth2_refresh` + setup hint rendered
5. Stream health table: consumers visible with pending counts (after fixtures).
6. Channel types view: zoho_cliq capabilities render (threads, edit flags).
7. Credentials panel: metadata only — assert NO plaintext token anywhere.

## Verdict criteria

PASS = all asserts hold with trace evidence (screenshots + zero 5xx network
entries except deliberate negative tests). Any plaintext credential in DOM or
network = FAIL regardless of other results.
