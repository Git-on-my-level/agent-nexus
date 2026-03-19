#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/common.sh"

require_command curl grep

assert_file_exists() {
  [[ -f "$1" ]] || die "expected file to exist: $1"
}

assert_dir_exists() {
  [[ -d "$1" ]] || die "expected directory to exist: $1"
}

assert_equals() {
  local expected="$1"
  local actual="$2"
  local label="$3"
  [[ "$expected" == "$actual" ]] || die "${label}: expected ${expected}, got ${actual}"
}

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
    --core-instance-id hosted-ops-test \
    >"$log_file" 2>&1 &
  local server_pid="$!"
  trap 'kill "$server_pid" >/dev/null 2>&1 || true; wait "$server_pid" 2>/dev/null || true' RETURN
  wait_for_http_ok "http://127.0.0.1:${listen_port}/health" 20 || die "failed to start temporary core for test fixture"
  curl -fsS \
    -H 'content-type: application/json' \
    -X POST \
    -d '{"actor_id":"oar-core","artifact":{"kind":"evidence","refs":["url:https://example.test/ops-bundle"]},"content":"ops-bundle-blob","content_type":"text"}' \
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
RESTORE_ROOT="${TMP_ROOT}/team-alpha-restored"
NON_EMPTY_RESTORE_ROOT="${TMP_ROOT}/team-alpha-non-empty"
SEED_PORT="$(pick_loopback_port)"

"${SCRIPT_DIR}/provision-workspace.sh" \
  --instance team-alpha \
  --instance-root "$INSTANCE_ROOT" \
  --public-origin https://team-alpha.example.test \
  --listen-port 8001 \
  --generate-bootstrap-token

seed_workspace_with_artifact "${INSTANCE_ROOT}/workspace" "$CORE_BIN" "$SCHEMA_PATH" "$SEED_PORT" "${TMP_ROOT}/seed.log"

"${SCRIPT_DIR}/backup-workspace.sh" \
  --instance-root "$INSTANCE_ROOT" \
  --output-dir "$BACKUP_DIR"

assert_file_exists "${BACKUP_DIR}/manifest.env"
assert_file_exists "${BACKUP_DIR}/SHA256SUMS"
assert_file_exists "${BACKUP_DIR}/workspace/state.sqlite"
assert_dir_exists "${BACKUP_DIR}/workspace/artifacts/content"
assert_file_exists "${BACKUP_DIR}/config/env.production"
assert_file_exists "${BACKUP_DIR}/metadata/instance.env"

assert_equals "${HOSTED_BACKUP_FORMAT_VERSION}" "$(manifest_get "${BACKUP_DIR}/manifest.env" FORMAT_VERSION)" "manifest format version"
assert_equals "1" "$(manifest_get "${BACKUP_DIR}/manifest.env" ARTIFACT_COUNT)" "artifact count"
assert_equals "1" "$(manifest_get "${BACKUP_DIR}/manifest.env" BLOB_FILE_COUNT)" "blob file count"
grep -q 'manifest.env' "${BACKUP_DIR}/SHA256SUMS" || die "expected SHA256SUMS to include manifest.env"
grep -q 'workspace/state.sqlite' "${BACKUP_DIR}/SHA256SUMS" || die "expected SHA256SUMS to include sqlite backup"

mkdir -p "$NON_EMPTY_RESTORE_ROOT"
echo "occupied" >"${NON_EMPTY_RESTORE_ROOT}/keep.txt"
if "${SCRIPT_DIR}/restore-workspace.sh" --backup-dir "$BACKUP_DIR" --target-instance-root "$NON_EMPTY_RESTORE_ROOT" >/dev/null 2>&1; then
  die "restore should have refused non-empty target without --force"
fi

"${SCRIPT_DIR}/restore-workspace.sh" \
  --backup-dir "$BACKUP_DIR" \
  --target-instance-root "$RESTORE_ROOT"

"${SCRIPT_DIR}/verify-restore.sh" \
  --instance-root "$RESTORE_ROOT" \
  --core-bin "$CORE_BIN" \
  --schema-path "$SCHEMA_PATH"

blob_file="$(find "${RESTORE_ROOT}/workspace/artifacts/content" -type f | head -n 1 || true)"
[[ -n "$blob_file" ]] || die "expected restored blob file"
rm -f "$blob_file"
if "${SCRIPT_DIR}/verify-restore.sh" --instance-root "$RESTORE_ROOT" --core-bin "$CORE_BIN" --schema-path "$SCHEMA_PATH" >/dev/null 2>&1; then
  die "restore verification should fail after blob removal"
fi

log "Hosted ops tests passed."
