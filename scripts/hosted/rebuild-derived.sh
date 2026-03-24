#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/common.sh"

usage() {
  cat <<'EOF'
Usage: scripts/hosted/rebuild-derived.sh [options]

Trigger an explicit derived-projection rebuild for a workspace core. This is
primarily useful when OAR_PROJECTION_MODE=manual and operators want a stable
helper instead of hand-written curl commands.

Options:
  --core-base-url URL   Workspace core base URL (default: $OAR_CORE_BASE_URL or http://127.0.0.1:8000)
  --actor-id ID         Actor id recorded on the rebuild-triggered events
  --auth-token TOKEN    Bearer token for the workspace core (default: $OAR_AUTH_TOKEN)
  --no-ops-health       Skip the follow-up GET /ops/health call
  -h, --help            Show help
EOF
}

CORE_BASE_URL="${OAR_CORE_BASE_URL:-http://127.0.0.1:8000}"
ACTOR_ID=""
AUTH_TOKEN="${OAR_AUTH_TOKEN:-}"
SHOW_OPS_HEALTH=1

while [[ $# -gt 0 ]]; do
  case "$1" in
    --core-base-url) CORE_BASE_URL="$2"; shift 2 ;;
    --actor-id) ACTOR_ID="$2"; shift 2 ;;
    --auth-token) AUTH_TOKEN="$2"; shift 2 ;;
    --no-ops-health) SHOW_OPS_HEALTH=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *)
      usage >&2
      die "unknown option: $1"
      ;;
  esac
done

require_command curl
[[ -n "$CORE_BASE_URL" ]] || die "--core-base-url is required"
[[ -n "$ACTOR_ID" ]] || die "--actor-id is required"
[[ "$ACTOR_ID" =~ ^[A-Za-z0-9][A-Za-z0-9._:-]*$ ]] || die "--actor-id must match ^[A-Za-z0-9][A-Za-z0-9._:-]*$"
[[ -n "$AUTH_TOKEN" ]] || die "--auth-token is required (or set OAR_AUTH_TOKEN)"

CORE_BASE_URL="${CORE_BASE_URL%/}"

REBUILD_RESPONSE="$(curl -fsS \
  -H "content-type: application/json" \
  -H "authorization: Bearer ${AUTH_TOKEN}" \
  -X POST \
  -d "{\"actor_id\":\"${ACTOR_ID}\"}" \
  "${CORE_BASE_URL}/derived/rebuild")"

if [[ "$(json_get "$REBUILD_RESPONSE" ok || true)" != "true" ]]; then
  die "unexpected rebuild response: ${REBUILD_RESPONSE}"
fi

log "derived rebuild response:"
printf '%s\n' "$REBUILD_RESPONSE"

if [[ "$SHOW_OPS_HEALTH" -eq 1 ]]; then
  OPS_HEALTH_RESPONSE="$(curl -fsS \
    -H "authorization: Bearer ${AUTH_TOKEN}" \
    "${CORE_BASE_URL}/ops/health")"
  log ""
  log "ops health:"
  printf '%s\n' "$OPS_HEALTH_RESPONSE"
fi
