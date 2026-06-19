#!/usr/bin/env bash
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

failed=0

check_no_matches() {
  local label="$1"
  local pattern="$2"
  shift 2
  local tmp
  tmp="$(mktemp)"
  if rg -n -P --glob '!scripts/check-planning-drift.sh' "$pattern" "$@" >"$tmp" 2>/dev/null; then
    echo "ERROR: ${label}" >&2
    cat "$tmp" >&2
    failed=1
  fi
  rm -f "$tmp"
}

if [ -d .workbench ]; then
  check_no_matches \
    "legacy planning paths found; use .workbench/{plans,reports,journals,visuals,state}" \
    '(?<!\.workbench/)plans/(reports|goals|journals|visuals|[0-9])' \
    README.md docs .github Makefile scripts
fi

check_no_matches \
  "old TUI path found; use ui/tui" \
  'services/tui' \
  README.md docs .github Makefile scripts ui services

check_no_matches \
  "stale AdminService RPC names found; update docs from proto/mio/admin/v1/admin.proto" \
  '\b(CreateAccount|GetCredentials|CreateChannelInstall|ListChannelInstalls|RefreshCredentials)\b' \
  README.md docs .github scripts

if [ "$failed" -ne 0 ]; then
  exit 1
fi

echo "planning drift check: ok"
