#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/common.sh"

require_command curl

seed_workspace_with_artifact() {
  local workspace_root="$1"
  local core_bin="$2"
  local schema_path="$3"
  local listen_port="$4"
  local log_file="$5"
  local server_pid
  start_core_server \
    server_pid \
    "$core_bin" \
    "$workspace_root" \
    "$schema_path" \
    "127.0.0.1" \
    "$listen_port" \
    "$log_file" \
    "hosted-smoke-seed" \
    unset \
    "" \
    true \
    true \
    false
  trap 'stop_background_process "${server_pid:-}"' RETURN
  wait_for_http_ok "http://127.0.0.1:${listen_port}/health" 20 || die "failed to start temporary seed server"
  curl -fsS \
    -H 'content-type: application/json' \
    -X POST \
    -d '{"actor_id":"oar-core","artifact":{"kind":"evidence","refs":["url:https://example.test/restore-drill"]},"content":"managed-hosting-smoke-artifact","content_type":"text"}' \
    "http://127.0.0.1:${listen_port}/artifacts" >/dev/null
  stop_background_process "$server_pid"
  trap - RETURN
}

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

CORE_BIN="$(build_core_binary)"
SCHEMA_PATH="$(resolve_schema_path)"

INSTANCE_ROOT="${TMP_ROOT}/team-alpha"
BACKUP_DIR="${TMP_ROOT}/backup-bundle"
RESTORED_ROOT="${TMP_ROOT}/restored/team-beta"
SEED_PORT="$(pick_loopback_port)"

"${SCRIPT_DIR}/provision-workspace.sh" \
  --instance team-alpha \
  --instance-root "$INSTANCE_ROOT" \
  --public-origin https://team-alpha.example.test \
  --listen-port 8001 \
  --web-ui-port 3001 \
  --generate-bootstrap-token

seed_workspace_with_artifact "${INSTANCE_ROOT}/workspace" "$CORE_BIN" "$SCHEMA_PATH" "$SEED_PORT" "${TMP_ROOT}/seed.log"

"${SCRIPT_DIR}/backup-workspace.sh" \
  --instance-root "$INSTANCE_ROOT" \
  --output-dir "$BACKUP_DIR"

"${SCRIPT_DIR}/restore-workspace.sh" \
  --backup-dir "$BACKUP_DIR" \
  --target-instance-root "$RESTORED_ROOT" \
  --instance team-beta \
  --public-origin https://team-beta.example.test \
  --listen-port 8011 \
  --web-ui-port 3011

"${SCRIPT_DIR}/verify-restore.sh" \
  --instance-root "$RESTORED_ROOT" \
  --core-bin "$CORE_BIN" \
  --schema-path "$SCHEMA_PATH"

log "Hosted ops smoke flow completed."
log "  instance root: ${INSTANCE_ROOT}"
log "  backup dir:    ${BACKUP_DIR}"
log "  restored root: ${RESTORED_ROOT}"
