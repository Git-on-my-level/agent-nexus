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
  OAR_ENABLE_DEV_ACTOR_MODE=1 \
  OAR_ALLOW_UNAUTHENTICATED_WRITES=1 \
  "$core_bin" \
    --listen-addr "127.0.0.1:${listen_port}" \
    --schema-path "$schema_path" \
    --workspace-root "$workspace_root" \
    --core-instance-id hosted-smoke-seed \
    >"$log_file" 2>&1 &
  local server_pid="$!"
  trap 'kill "$server_pid" >/dev/null 2>&1 || true; wait "$server_pid" 2>/dev/null || true' RETURN
  wait_for_http_ok "http://127.0.0.1:${listen_port}/health" 20 || die "failed to start temporary seed server"
  curl -fsS \
    -H 'content-type: application/json' \
    -X POST \
    -d '{"actor_id":"oar-core","artifact":{"kind":"evidence","refs":["url:https://example.test/restore-drill"]},"content":"managed-hosting-smoke-artifact","content_type":"text"}' \
    "http://127.0.0.1:${listen_port}/artifacts" >/dev/null
  kill "$server_pid" >/dev/null 2>&1 || true
  wait "$server_pid" 2>/dev/null || true
  trap - RETURN
}

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

"${REPO_ROOT}/core/scripts/build-prod"
CORE_BIN="${REPO_ROOT}/core/.bin/oar-core"
SCHEMA_PATH="${REPO_ROOT}/contracts/oar-schema.yaml"

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
