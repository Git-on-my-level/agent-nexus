#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/common.sh"

usage() {
  cat <<'EOF'
Usage: scripts/hosted/verify-restore.sh --instance-root DIR [options]

Starts oar-core against the restored workspace on a loopback port, verifies
/health, and checks restored metadata counts against the source manifest.

Options:
  --manifest PATH        Manifest to compare against
  --core-bin PATH        oar-core binary to use
  --schema-path PATH     Schema path to use
  --listen-port PORT     Explicit loopback verification port
  --timeout SECONDS      Health wait timeout (default: 20)
EOF
}

INSTANCE_ROOT=""
MANIFEST_PATH=""
CORE_BIN=""
SCHEMA_PATH=""
LISTEN_PORT=""
TIMEOUT_SECONDS="20"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --instance-root) INSTANCE_ROOT="$2"; shift 2 ;;
    --manifest) MANIFEST_PATH="$2"; shift 2 ;;
    --core-bin) CORE_BIN="$2"; shift 2 ;;
    --schema-path) SCHEMA_PATH="$2"; shift 2 ;;
    --listen-port) LISTEN_PORT="$2"; shift 2 ;;
    --timeout) TIMEOUT_SECONDS="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *)
      usage >&2
      die "unknown option: $1"
      ;;
  esac
done

[[ -n "$INSTANCE_ROOT" ]] || die "--instance-root is required"
require_command sqlite3 curl

INSTANCE_ROOT="$(cd "$INSTANCE_ROOT" && pwd -P)"
WORKSPACE_ROOT="${INSTANCE_ROOT}/workspace"
ENV_FILE="${INSTANCE_ROOT}/config/env.production"
if [[ -z "$MANIFEST_PATH" ]]; then
  MANIFEST_PATH="${INSTANCE_ROOT}/metadata/restore-source-manifest.env"
fi
[[ -f "$MANIFEST_PATH" ]] || die "manifest not found: $MANIFEST_PATH"
[[ -f "${WORKSPACE_ROOT}/state.sqlite" ]] || die "restored sqlite database not found: ${WORKSPACE_ROOT}/state.sqlite"

if [[ -z "$CORE_BIN" ]]; then
  CORE_BIN="$(resolve_core_bin)"
fi
if [[ -z "$SCHEMA_PATH" ]]; then
  SCHEMA_PATH="$(resolve_schema_path)"
fi
if [[ -z "$LISTEN_PORT" ]]; then
  LISTEN_PORT="$(pick_loopback_port)"
fi
validate_port "$LISTEN_PORT"

load_dotenv_file "$ENV_FILE"
EXPECTED_ARTIFACT_COUNT="$(manifest_get "$MANIFEST_PATH" ARTIFACT_COUNT)"
EXPECTED_AGENT_COUNT="$(manifest_get "$MANIFEST_PATH" AGENT_COUNT)"
EXPECTED_INVITE_COUNT="$(manifest_get "$MANIFEST_PATH" INVITE_COUNT)"
EXPECTED_BLOB_FILE_COUNT="$(manifest_get "$MANIFEST_PATH" BLOB_FILE_COUNT)"
EXPECTED_CORE_INSTANCE_ID="$(manifest_get "$MANIFEST_PATH" CORE_INSTANCE_ID || true)"

SERVER_LOG_DIR="${WORKSPACE_ROOT}/logs"
SERVER_LOG_FILE="${SERVER_LOG_DIR}/restore-verify.log"
mkdir -p "$SERVER_LOG_DIR"

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]]; then
    kill "${SERVER_PID}" >/dev/null 2>&1 || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT

OAR_ENABLE_DEV_ACTOR_MODE=false \
OAR_ALLOW_UNAUTHENTICATED_WRITES=false \
OAR_BOOTSTRAP_TOKEN="${OAR_BOOTSTRAP_TOKEN:-}" \
"$CORE_BIN" \
  --listen-addr "127.0.0.1:${LISTEN_PORT}" \
  --schema-path "$SCHEMA_PATH" \
  --workspace-root "$WORKSPACE_ROOT" \
  --core-instance-id "${OAR_CORE_INSTANCE_ID:-${EXPECTED_CORE_INSTANCE_ID:-restore-verify}}" \
  >"$SERVER_LOG_FILE" 2>&1 &
SERVER_PID="$!"

if ! wait_for_http_ok "http://127.0.0.1:${LISTEN_PORT}/health" "$TIMEOUT_SECONDS"; then
  die "restore verification could not reach /health on 127.0.0.1:${LISTEN_PORT}; see ${SERVER_LOG_FILE}"
fi

ACTUAL_ARTIFACT_COUNT="$(sqlite_scalar "${WORKSPACE_ROOT}/state.sqlite" "SELECT COUNT(*) FROM artifacts;")"
ACTUAL_AGENT_COUNT="$(sqlite_scalar "${WORKSPACE_ROOT}/state.sqlite" "SELECT COUNT(*) FROM agents;")"
ACTUAL_INVITE_COUNT="$(sqlite_scalar "${WORKSPACE_ROOT}/state.sqlite" "SELECT COUNT(*) FROM auth_invites;")"
ACTUAL_BLOB_FILE_COUNT="$(count_files "${WORKSPACE_ROOT}/artifacts/content")"

[[ "$ACTUAL_ARTIFACT_COUNT" == "$EXPECTED_ARTIFACT_COUNT" ]] || die "artifact count mismatch: expected ${EXPECTED_ARTIFACT_COUNT}, got ${ACTUAL_ARTIFACT_COUNT}"
[[ "$ACTUAL_AGENT_COUNT" == "$EXPECTED_AGENT_COUNT" ]] || die "agent count mismatch: expected ${EXPECTED_AGENT_COUNT}, got ${ACTUAL_AGENT_COUNT}"
[[ "$ACTUAL_INVITE_COUNT" == "$EXPECTED_INVITE_COUNT" ]] || die "invite count mismatch: expected ${EXPECTED_INVITE_COUNT}, got ${ACTUAL_INVITE_COUNT}"
[[ "$ACTUAL_BLOB_FILE_COUNT" == "$EXPECTED_BLOB_FILE_COUNT" ]] || die "blob file count mismatch: expected ${EXPECTED_BLOB_FILE_COUNT}, got ${ACTUAL_BLOB_FILE_COUNT}"
if [[ -n "$EXPECTED_CORE_INSTANCE_ID" && -n "${OAR_CORE_INSTANCE_ID:-}" && "$EXPECTED_CORE_INSTANCE_ID" != "${OAR_CORE_INSTANCE_ID}" ]]; then
  die "core instance id mismatch: manifest=${EXPECTED_CORE_INSTANCE_ID} env=${OAR_CORE_INSTANCE_ID}"
fi

log "Restore verification succeeded for ${INSTANCE_ROOT}"
log "  /health:                 ok"
log "  artifact count:          ${ACTUAL_ARTIFACT_COUNT}"
log "  agent count:             ${ACTUAL_AGENT_COUNT}"
log "  invite count:            ${ACTUAL_INVITE_COUNT}"
log "  blob file count:         ${ACTUAL_BLOB_FILE_COUNT}"
