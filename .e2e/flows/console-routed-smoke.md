# Flow: modernized routed console smoke

Verifies the TanStack-routed MIO operator console (post web-modernize) end-to-end
on the local compose stack (ports per /tmp/mio-e2e.env: web :18081, admin :19090,
gateway :18080). Styling reference: .work/visuals/mio-console/mio-console-dashboard.html.

## Preconditions

- Stack healthy; mio-web rebuilt with the new SPA.
- Seeded via /tmp/mio-e2e-wire.sh: tenant `e2e-tenant`, one zoho_cliq account
  external_id 637446511; signed Cliq fixtures POSTed so streams + tables have data.

## Steps (route-driven)

1. `/` → dev login → redirects to `/dashboard`. Assert sidebar nav renders all 9
   items (sentence case: Dashboard, Tenants, Accounts, Onboarding, Stream health,
   Live tail, Channel types, Audit) + operator identity chip.
2. Dashboard: assert stat cards (Tenants/Accounts/Channel types/Messages), stream
   health table with consumers incl. ai-consumer-enriched@MESSAGES_INBOUND_ENRICHED,
   and the degraded-banner deep-link if a consumer lags. Sentence-case headings.
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

PASS = all routes render their surface with real data, zero console/network 5xx in
trace (except deliberate negatives), DataTable + side-panel + wizard function, no
plaintext credential anywhere, sentence-case consistent. Any unhandled route error
boundary or blank route = FAIL.
