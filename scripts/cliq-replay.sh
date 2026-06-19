#!/usr/bin/env bash
# cliq-replay.sh — drive a synthetic Zoho Cliq inbound message into the local
# gateway with NO real Zoho org. Reads a testdata fixture, unwraps the body_json
# envelope (the signature is over the INNER bytes, not the file as stored),
# HMAC-SHA256 signs it with the dev webhook secret, and POSTs it to
# /webhooks/zoho-cliq.
#
# Usage:
#   FIXTURE=dm-to-bot ./scripts/cliq-replay.sh
#   ./scripts/cliq-replay.sh channel-text
#   ./scripts/cliq-replay.sh path/to/fixture.json
#
# ponytail: openssl+jq+curl, no SDK needed for a dev injector.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TESTDATA="$ROOT/channels/zohocliq/testdata"
GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
SECRET_FILE="${CLIQ_WEBHOOK_SECRET_FILE:-$ROOT/deploy/local/secrets/cliq-webhook-secret}"

# channel-text closes the full round-trip (the Cliq adapter sends to channels by
# name; DM fixtures have no channel name → inbound-only). Override with any name.
FIXTURE="${FIXTURE:-${1:-channel-text}}"

# Resolve fixture: exact path, else newest testdata file whose name contains it.
if [[ -f "$FIXTURE" ]]; then
  file="$FIXTURE"
else
  shopt -s nullglob
  matches=("$TESTDATA"/*"$FIXTURE"*.json)
  shopt -u nullglob
  (( ${#matches[@]} )) && file="${matches[-1]}"   # glob is sorted; newest sorts last
fi
if [[ -z "${file:-}" || ! -f "$file" ]]; then
  echo "fixture '$FIXTURE' not found. Available:" >&2
  for f in "$TESTDATA"/*.json; do basename "$f" .json; done >&2
  exit 2
fi

# Unwrap the body_json envelope — signature is over these inner bytes. Posting
# the raw file would both send the wrong shape AND fail signature verification.
body="$(jq -c '.body_json' "$file")"
if [[ "$body" == "null" ]]; then
  body="$(jq -c '.' "$file")"   # fallback: fixture stores payload at top level
fi

# UNIQUE=1 rewrites the message id to a fresh nonce so the gateway's idempotency
# (account_id, source_message_id) does NOT dedupe a re-run — used by cliq-smoke so
# it's repeatable. Default off → faithful replay (re-running the same id dedupes).
if [[ "${UNIQUE:-0}" == "1" ]]; then
  nonce="local-$(date +%s)-${RANDOM}"
  body="$(jq -c --arg n "$nonce" '
    if .data.message.id then .data.message.id = $n
    elif .message.id then .message.id = $n
    else . end' <<<"$body")"
fi

secret="$(cat "$SECRET_FILE")"
sig="$(printf '%s' "$body" | openssl dgst -sha256 -hmac "$secret" -hex | awk '{print $NF}')"

echo "→ POST $GATEWAY_URL/webhooks/zoho-cliq  (fixture: $(basename "$file"))"
resp_file="$(mktemp)"
http_code="$(curl -sS -o "$resp_file" -w '%{http_code}' \
  -X POST "$GATEWAY_URL/webhooks/zoho-cliq" \
  -H 'Content-Type: application/json' \
  -H "X-Webhook-Signature: sha256=$sig" \
  --data-binary "$body")"
echo "← HTTP $http_code  $(cat "$resp_file")"
rm -f "$resp_file"
echo "  outbound leg → make cliq-logs   (look for cliq-mock 'outbound send -> 204')"

[[ "$http_code" == "200" ]]
