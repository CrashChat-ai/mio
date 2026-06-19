#!/usr/bin/env bash
# cliq-smoke.sh — replay a fixture and assert the full round-trip closed:
# gateway accepted (200) AND the outbound leg reached cliq-mock (204 No Content).
# Assumes `make cliq-up` already ran. This is the runnable check the Cliq local
# loop leaves behind — it fails if any hop in inbound→process→outbound breaks.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE=(docker compose -f "$ROOT/deploy/local/docker-compose.yml")
FIXTURE="${FIXTURE:-channel-text}"   # channel fixture closes the full loop (DM is inbound-only)

count_outbound() { "${COMPOSE[@]}" logs --no-color cliq-mock 2>/dev/null | grep -c 'outbound send' || true; }

before="$(count_outbound)"

# UNIQUE=1 → fresh message id each run, so idempotency never dedupes the smoke.
UNIQUE=1 FIXTURE="$FIXTURE" "$ROOT/scripts/cliq-replay.sh"   # non-zero unless gateway returned 200

# The echo-consumer round-trip is async; poll cliq-mock for a NEW outbound hit.
deadline=$((SECONDS + 20))
while (( SECONDS < deadline )); do
  if (( "$(count_outbound)" > before )); then
    echo "✓ smoke: outbound leg reached cliq-mock (204) — round-trip closed, no Zoho"
    exit 0
  fi
  sleep 1
done

echo "✗ smoke: no new cliq-mock outbound hit within 20s — round-trip did NOT close" >&2
echo "  debug: ${COMPOSE[*]} logs --tail=30 gateway echo-consumer cliq-mock" >&2
exit 1
