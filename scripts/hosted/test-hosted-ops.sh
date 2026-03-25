#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/common.sh"

require_command curl grep awk cp find

assert_file_exists() {
  [[ -f "$1" ]] || die "expected file to exist: $1"
}

assert_dir_exists() {
  [[ -d "$1" ]] || die "expected directory to exist: $1"
}

assert_path_missing() {
  [[ ! -e "$1" ]] || die "expected path to be absent: $1"
}

assert_equals() {
  local expected="$1"
  local actual="$2"
  local label="$3"
  [[ "$expected" == "$actual" ]] || die "${label}: expected ${expected}, got ${actual}"
}

assert_not_equals() {
  local unexpected="$1"
  local actual="$2"
  local label="$3"
  [[ "$unexpected" != "$actual" ]] || die "${label}: did not expect ${unexpected}"
}

assert_file_contains() {
  local file="$1"
  local needle="$2"
  local label="$3"
  grep -F -q -- "$needle" "$file" || die "${label}: expected ${file} to contain ${needle}"
}

assert_path_only_in_restore_receipts() {
  local root="$1"
  local needle="$2"
  local match
  local found=0
  while IFS= read -r match; do
    found=1
    case "$match" in
      "${root}/metadata/restore-receipt.env"|\
      "${root}/metadata/restore-source-manifest.env")
        ;;
      *)
        die "unexpected source leakage for ${needle}: ${match}"
        ;;
    esac
  done < <(paths_containing_text "$needle" "$root")
  [[ "$found" -eq 1 ]] || die "expected to find ${needle} in restore receipt material"
}

replace_manifest_value() {
  local manifest_file="$1"
  local key="$2"
  local value="$3"
  local tmp_file
  tmp_file="$(mktemp)"
  awk -F= -v key="$key" -v value="$value" '
    BEGIN { replaced = 0 }
    $1 == key {
      print key "=" value
      replaced = 1
      next
    }
    { print $0 }
    END {
      if (replaced == 0) {
        print key "=" value
      }
    }
  ' "$manifest_file" >"$tmp_file"
  mv "$tmp_file" "$manifest_file"
}

rewrite_checksum_entry() {
  local bundle_dir="$1"
  local relative_path="$2"
  local tmp_file checksum
  checksum="$(sha256_file "${bundle_dir}/${relative_path}")"
  tmp_file="$(mktemp)"
  awk -v path="$relative_path" -v checksum="$checksum" '
    BEGIN { replaced = 0 }
    $2 == path {
      print checksum "  " path
      replaced = 1
      next
    }
    { print $0 }
    END {
      if (replaced == 0) {
        exit 1
      }
    }
  ' "${bundle_dir}/SHA256SUMS" >"$tmp_file" || die "failed to rewrite checksum entry for ${relative_path}"
  mv "$tmp_file" "${bundle_dir}/SHA256SUMS"
}

assert_command_fails() {
  local stderr_file="$1"
  shift
  if "$@" >"${stderr_file}.out" 2>"$stderr_file"; then
    die "expected command to fail: $*"
  fi
  rm -f "${stderr_file}.out"
}

seed_workspace_fixture() {
  local workspace_root="$1"
  local core_bin="$2"
  local schema_path="$3"
  local listen_port="$4"
  local log_file="$5"
  local server_pid
  local instance_root env_file
  local saved_blob_backend saved_blob_root saved_blob_bucket saved_blob_prefix saved_blob_region saved_blob_endpoint saved_blob_access_key_id saved_blob_secret_access_key saved_blob_session_token saved_blob_force_path_style

  instance_root="$(dirname "$workspace_root")"
  env_file="${instance_root}/config/env.production"
  saved_blob_backend="${OAR_BLOB_BACKEND-__unset__}"
  saved_blob_root="${OAR_BLOB_ROOT-__unset__}"
  saved_blob_bucket="${OAR_BLOB_S3_BUCKET-__unset__}"
  saved_blob_prefix="${OAR_BLOB_S3_PREFIX-__unset__}"
  saved_blob_region="${OAR_BLOB_S3_REGION-__unset__}"
  saved_blob_endpoint="${OAR_BLOB_S3_ENDPOINT-__unset__}"
  saved_blob_access_key_id="${OAR_BLOB_S3_ACCESS_KEY_ID-__unset__}"
  saved_blob_secret_access_key="${OAR_BLOB_S3_SECRET_ACCESS_KEY-__unset__}"
  saved_blob_session_token="${OAR_BLOB_S3_SESSION_TOKEN-__unset__}"
  saved_blob_force_path_style="${OAR_BLOB_S3_FORCE_PATH_STYLE-__unset__}"
  load_dotenv_file "$env_file"
  start_core_server \
    server_pid \
    "$core_bin" \
    "$workspace_root" \
    "$schema_path" \
    "127.0.0.1" \
    "$listen_port" \
    "$log_file" \
    "hosted-ops-test" \
    unset \
    "" \
    true \
    true \
    false
  trap 'stop_background_process "${server_pid:-}"' RETURN
  wait_for_http_ok "http://127.0.0.1:${listen_port}/readyz" 20 || die "failed to start temporary core for test fixture"

  curl -fsS \
    -H 'content-type: application/json' \
    -X POST \
    -d '{"actor_id":"oar-core","thread":{"id":"thread-hosted-ops","title":"Hosted ops restore drill","type":"incident","status":"active","priority":"p2","tags":["hosted","ops"],"cadence":"daily","current_summary":"Seed data for hosted restore verification","next_actions":["validate backup restore"],"key_artifacts":[],"provenance":{"sources":["actor_statement:hosted-ops-test"]}}}' \
    "http://127.0.0.1:${listen_port}/threads" >/dev/null

  curl -fsS \
    -H 'content-type: application/json' \
    -X POST \
    -d '{"actor_id":"oar-core","artifact":{"kind":"evidence","thread_id":"thread-hosted-ops","refs":["thread:thread-hosted-ops","url:https://example.test/ops-bundle"]},"content":"ops-bundle-blob","content_type":"text"}' \
    "http://127.0.0.1:${listen_port}/artifacts" >/dev/null

  curl -fsS \
    -H 'content-type: application/json' \
    -X POST \
    -d '{"actor_id":"oar-core","document":{"document_id":"ops-runbook","thread_id":"thread-hosted-ops","title":"Hosted Ops Runbook"},"refs":["thread:thread-hosted-ops"],"content":"restore drill document body","content_type":"text"}' \
    "http://127.0.0.1:${listen_port}/docs" >/dev/null

  stop_background_process "$server_pid"
  trap - RETURN
  for env_name in \
    OAR_BLOB_BACKEND OAR_BLOB_ROOT OAR_BLOB_S3_BUCKET OAR_BLOB_S3_PREFIX OAR_BLOB_S3_REGION \
    OAR_BLOB_S3_ENDPOINT OAR_BLOB_S3_ACCESS_KEY_ID OAR_BLOB_S3_SECRET_ACCESS_KEY \
    OAR_BLOB_S3_SESSION_TOKEN OAR_BLOB_S3_FORCE_PATH_STYLE
  do
    unset "$env_name"
  done
  [[ "$saved_blob_backend" == "__unset__" ]] || export OAR_BLOB_BACKEND="$saved_blob_backend"
  [[ "$saved_blob_root" == "__unset__" ]] || export OAR_BLOB_ROOT="$saved_blob_root"
  [[ "$saved_blob_bucket" == "__unset__" ]] || export OAR_BLOB_S3_BUCKET="$saved_blob_bucket"
  [[ "$saved_blob_prefix" == "__unset__" ]] || export OAR_BLOB_S3_PREFIX="$saved_blob_prefix"
  [[ "$saved_blob_region" == "__unset__" ]] || export OAR_BLOB_S3_REGION="$saved_blob_region"
  [[ "$saved_blob_endpoint" == "__unset__" ]] || export OAR_BLOB_S3_ENDPOINT="$saved_blob_endpoint"
  [[ "$saved_blob_access_key_id" == "__unset__" ]] || export OAR_BLOB_S3_ACCESS_KEY_ID="$saved_blob_access_key_id"
  [[ "$saved_blob_secret_access_key" == "__unset__" ]] || export OAR_BLOB_S3_SECRET_ACCESS_KEY="$saved_blob_secret_access_key"
  [[ "$saved_blob_session_token" == "__unset__" ]] || export OAR_BLOB_S3_SESSION_TOKEN="$saved_blob_session_token"
  [[ "$saved_blob_force_path_style" == "__unset__" ]] || export OAR_BLOB_S3_FORCE_PATH_STYLE="$saved_blob_force_path_style"
}

restore_bundle() {
  local backup_dir="$1"
  local restore_root="$2"
  local listen_port="$3"
  local web_ui_port="$4"
  local core_instance_id="$5"
  "${SCRIPT_DIR}/restore-workspace.sh" \
    --backup-dir "$backup_dir" \
    --target-instance-root "$restore_root" \
    --instance "$RESTORE_INSTANCE_NAME" \
    --public-origin "$RESTORE_PUBLIC_ORIGIN" \
    --listen-port "$listen_port" \
    --web-ui-port "$web_ui_port" \
    --core-instance-id "$core_instance_id"
}

instance_local_blob_root() {
  local instance_root="$1"
  local workspace_root="${instance_root}/workspace"
  local env_file="${instance_root}/config/env.production"
  local blob_backend blob_root
  blob_backend="$(dotenv_get "$env_file" OAR_BLOB_BACKEND || true)"
  blob_root="$(dotenv_get "$env_file" OAR_BLOB_ROOT || true)"
  [[ -n "$blob_backend" ]] || blob_backend="filesystem"
  blob_effective_local_root "$workspace_root" "$blob_backend" "$blob_root"
}

replace_blob_with_dummy() {
  local instance_root="$1"
  local missing_hash="$2"
  local dummy_name="$3"
  local expected_count="$4"
  local blob_root
  blob_root="$(instance_local_blob_root "$instance_root")"
  rm -f "${blob_root}/${missing_hash}"
  printf 'placeholder-%s\n' "$dummy_name" >"${blob_root}/${dummy_name}"
  assert_equals "$expected_count" "$(count_files "$blob_root")" "blob file count after ${dummy_name} mutation"
}

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

CORE_BIN="$(build_core_binary)"
SCHEMA_PATH="$(resolve_schema_path)"

INSTANCE_ROOT="${TMP_ROOT}/source/team-alpha"
BACKUP_DIR="${TMP_ROOT}/backup-bundle"
RESTORE_ROOT="${TMP_ROOT}/restored/team-beta"
NON_EMPTY_RESTORE_ROOT="${TMP_ROOT}/restored/non-empty"
OBJECT_INSTANCE_ROOT="${TMP_ROOT}/source/team-object"
OBJECT_BACKUP_DIR="${TMP_ROOT}/backup-object-bundle"
OBJECT_RESTORE_ROOT="${TMP_ROOT}/restored/team-object-restore"
S3_SOURCE_ROOT="${TMP_ROOT}/source/team-s3"
S3_BACKUP_DIR="${TMP_ROOT}/backup-s3-bundle"
S3_RESTORE_ROOT="${TMP_ROOT}/restored/team-s3-restore"
RESTORE_INSTANCE_NAME="team-beta"
RESTORE_PUBLIC_ORIGIN="https://team-beta.example.test"
RESTORE_CORE_INSTANCE_ID="team-beta-core"
SEED_PORT="$(pick_loopback_port)"

"${SCRIPT_DIR}/provision-workspace.sh" \
  --instance team-alpha \
  --instance-root "$INSTANCE_ROOT" \
  --public-origin https://team-alpha.example.test \
  --listen-port 8001 \
  --web-ui-port 3001 \
  --generate-bootstrap-token
INSTANCE_ROOT="$(cd "$INSTANCE_ROOT" && pwd -P)"

seed_workspace_fixture "${INSTANCE_ROOT}/workspace" "$CORE_BIN" "$SCHEMA_PATH" "$SEED_PORT" "${TMP_ROOT}/seed.log"

SOURCE_BOOTSTRAP_TOKEN="$(dotenv_get "${INSTANCE_ROOT}/config/env.production" OAR_BOOTSTRAP_TOKEN || true)"
[[ -n "$SOURCE_BOOTSTRAP_TOKEN" ]] || die "expected source bootstrap token to be configured"
assert_not_equals "$HOSTED_BOOTSTRAP_PLACEHOLDER" "$SOURCE_BOOTSTRAP_TOKEN" "source bootstrap token"

"${SCRIPT_DIR}/backup-workspace.sh" \
  --instance-root "$INSTANCE_ROOT" \
  --output-dir "$BACKUP_DIR"
BACKUP_DIR="$(cd "$BACKUP_DIR" && pwd -P)"

assert_file_exists "${BACKUP_DIR}/manifest.env"
assert_file_exists "${BACKUP_DIR}/SHA256SUMS"
assert_file_exists "${BACKUP_DIR}/workspace/state.sqlite"
assert_dir_exists "${BACKUP_DIR}/workspace/blob-store"
assert_path_missing "${BACKUP_DIR}/config/env.production"
assert_file_exists "${BACKUP_DIR}/metadata/instance.env"

assert_equals "${HOSTED_BACKUP_FORMAT_VERSION}" "$(manifest_get "${BACKUP_DIR}/manifest.env" FORMAT_VERSION)" "manifest format version"
assert_equals "${HOSTED_INSTANCE_FORMAT_VERSION}" "$(manifest_get "${BACKUP_DIR}/manifest.env" INSTANCE_FORMAT_VERSION)" "manifest instance format version"
assert_equals "false" "$(manifest_get "${BACKUP_DIR}/manifest.env" CONFIG_INCLUDED)" "manifest config included (default)"
assert_equals "" "$(manifest_get "${BACKUP_DIR}/manifest.env" CONFIG_ENV_PATH)" "manifest config env path (default)"
assert_equals "2" "$(manifest_get "${BACKUP_DIR}/manifest.env" ARTIFACT_COUNT)" "artifact count"
assert_equals "1" "$(manifest_get "${BACKUP_DIR}/manifest.env" DOCUMENT_COUNT)" "document count"
assert_equals "1" "$(manifest_get "${BACKUP_DIR}/manifest.env" DOCUMENT_REVISION_COUNT)" "document revision count"
assert_equals "2" "$(manifest_get "${BACKUP_DIR}/manifest.env" BLOB_HASH_COUNT)" "blob hash count"
assert_equals "2" "$(manifest_get "${BACKUP_DIR}/manifest.env" BLOB_FILE_COUNT)" "blob file count"
assert_file_contains "${BACKUP_DIR}/manifest.env" "SQLITE_SCHEMA_VERSION=" "manifest sqlite schema version"
assert_file_contains "${BACKUP_DIR}/manifest.env" "SQLITE_USER_VERSION=" "manifest sqlite user version"
assert_equals "filesystem" "$(manifest_get "${BACKUP_DIR}/manifest.env" BLOB_BACKEND)" "manifest blob backend"
assert_equals "local" "$(manifest_get "${BACKUP_DIR}/manifest.env" BLOB_STORAGE_MODE)" "manifest blob storage mode"
assert_equals "copy" "$(manifest_get "${BACKUP_DIR}/manifest.env" BLOB_BACKUP_MODE)" "manifest blob backup mode"
assert_equals "sha256-hex-filename" "$(manifest_get "${BACKUP_DIR}/manifest.env" BLOB_KEY_FORMAT)" "manifest blob key format"
assert_equals "workspace/blob-store" "$(manifest_get "${BACKUP_DIR}/manifest.env" BLOB_BUNDLE_PATH)" "manifest blob bundle path"
[[ -n "$(manifest_get "${BACKUP_DIR}/manifest.env" VERIFY_ARTIFACT_ID)" ]] || die "expected manifest verify artifact id"
assert_equals "ops-runbook" "$(manifest_get "${BACKUP_DIR}/manifest.env" VERIFY_DOCUMENT_ID)" "manifest verify document id"
[[ -n "$(manifest_get "${BACKUP_DIR}/manifest.env" VERIFY_DOCUMENT_REVISION_ID)" ]] || die "expected manifest verify document revision id"
grep -q 'manifest.env' "${BACKUP_DIR}/SHA256SUMS" || die "expected SHA256SUMS to include manifest.env"
grep -q 'workspace/state.sqlite' "${BACKUP_DIR}/SHA256SUMS" || die "expected SHA256SUMS to include sqlite backup"

BACKUP_WITH_SECRETS_DIR="${TMP_ROOT}/backup-bundle-with-secrets"
"${SCRIPT_DIR}/backup-workspace.sh" \
  --instance-root "$INSTANCE_ROOT" \
  --output-dir "$BACKUP_WITH_SECRETS_DIR" \
  --include-config-secrets
BACKUP_WITH_SECRETS_DIR="$(cd "$BACKUP_WITH_SECRETS_DIR" && pwd -P)"

assert_file_exists "${BACKUP_WITH_SECRETS_DIR}/manifest.env"
assert_file_exists "${BACKUP_WITH_SECRETS_DIR}/SHA256SUMS"
assert_file_exists "${BACKUP_WITH_SECRETS_DIR}/workspace/state.sqlite"
assert_dir_exists "${BACKUP_WITH_SECRETS_DIR}/workspace/blob-store"
assert_file_exists "${BACKUP_WITH_SECRETS_DIR}/config/env.production"
assert_file_exists "${BACKUP_WITH_SECRETS_DIR}/metadata/instance.env"

assert_equals "true" "$(manifest_get "${BACKUP_WITH_SECRETS_DIR}/manifest.env" CONFIG_INCLUDED)" "manifest config included (with secrets)"
assert_equals "config/env.production" "$(manifest_get "${BACKUP_WITH_SECRETS_DIR}/manifest.env" CONFIG_ENV_PATH)" "manifest config env path (with secrets)"

mkdir -p "$NON_EMPTY_RESTORE_ROOT"
echo "occupied" >"${NON_EMPTY_RESTORE_ROOT}/keep.txt"
if "${SCRIPT_DIR}/restore-workspace.sh" \
  --backup-dir "$BACKUP_DIR" \
  --target-instance-root "$NON_EMPTY_RESTORE_ROOT" \
  --instance "$RESTORE_INSTANCE_NAME" \
  --public-origin "$RESTORE_PUBLIC_ORIGIN" \
  --listen-port 8011 \
  --web-ui-port 3011 \
  >/dev/null 2>&1; then
  die "restore should have refused non-empty target without --force"
fi

UNSUPPORTED_BUNDLE="${TMP_ROOT}/backup-unsupported-format"
cp -R "$BACKUP_DIR" "$UNSUPPORTED_BUNDLE"
replace_manifest_value "${UNSUPPORTED_BUNDLE}/manifest.env" FORMAT_VERSION "hosted-ops-backup/v999"
rewrite_checksum_entry "$UNSUPPORTED_BUNDLE" "manifest.env"
UNSUPPORTED_ERR="${TMP_ROOT}/unsupported-format.err"
assert_command_fails "$UNSUPPORTED_ERR" \
  "${SCRIPT_DIR}/restore-workspace.sh" \
  --backup-dir "$UNSUPPORTED_BUNDLE" \
  --target-instance-root "${TMP_ROOT}/restored/unsupported-format" \
  --instance "$RESTORE_INSTANCE_NAME" \
  --public-origin "$RESTORE_PUBLIC_ORIGIN" \
  --listen-port 8012 \
  --web-ui-port 3012
assert_file_contains "$UNSUPPORTED_ERR" "unsupported backup format version" "unsupported format restore failure"

CORRUPT_BUNDLE="${TMP_ROOT}/backup-corrupt-checksum"
cp -R "$BACKUP_DIR" "$CORRUPT_BUNDLE"
printf 'tampered-backup\n' >>"${CORRUPT_BUNDLE}/workspace/state.sqlite"
CHECKSUM_ERR="${TMP_ROOT}/corrupt-checksum.err"
assert_command_fails "$CHECKSUM_ERR" \
  "${SCRIPT_DIR}/restore-workspace.sh" \
  --backup-dir "$CORRUPT_BUNDLE" \
  --target-instance-root "${TMP_ROOT}/restored/corrupt-checksum" \
  --instance "$RESTORE_INSTANCE_NAME" \
  --public-origin "$RESTORE_PUBLIC_ORIGIN" \
  --listen-port 8013 \
  --web-ui-port 3013
assert_file_contains "$CHECKSUM_ERR" "checksum verification failed for workspace/state.sqlite" "checksum restore failure"
assert_path_missing "${TMP_ROOT}/restored/corrupt-checksum/workspace/state.sqlite"

restore_bundle "$BACKUP_DIR" "$RESTORE_ROOT" 8011 3011 "$RESTORE_CORE_INSTANCE_ID"
RESTORE_ROOT="$(cd "$RESTORE_ROOT" && pwd -P)"

"${SCRIPT_DIR}/verify-restore.sh" \
  --instance-root "$RESTORE_ROOT" \
  --core-bin "$CORE_BIN" \
  --schema-path "$SCHEMA_PATH"

assert_equals "${RESTORE_ROOT}/workspace" "$(dotenv_get "${RESTORE_ROOT}/config/env.production" HOST_OAR_WORKSPACE_ROOT)" "restored workspace root"
assert_equals "$RESTORE_PUBLIC_ORIGIN" "$(dotenv_get "${RESTORE_ROOT}/config/env.production" OAR_WEB_UI_ORIGIN)" "restored env public origin"
assert_equals "$RESTORE_PUBLIC_ORIGIN" "$(dotenv_get "${RESTORE_ROOT}/config/env.production" OAR_WEBAUTHN_ORIGIN)" "restored webauthn origin"
assert_equals "$RESTORE_CORE_INSTANCE_ID" "$(dotenv_get "${RESTORE_ROOT}/config/env.production" OAR_CORE_INSTANCE_ID)" "restored core instance id"
assert_equals "$HOSTED_BOOTSTRAP_PLACEHOLDER" "$(dotenv_get "${RESTORE_ROOT}/config/env.production" OAR_BOOTSTRAP_TOKEN)" "restored bootstrap token default"
assert_equals "filesystem" "$(dotenv_get "${RESTORE_ROOT}/config/env.production" OAR_BLOB_BACKEND)" "restored blob backend"
assert_equals "$RESTORE_INSTANCE_NAME" "$(dotenv_get "${RESTORE_ROOT}/metadata/instance.env" INSTANCE_NAME)" "restored instance name"
assert_equals "$RESTORE_ROOT" "$(dotenv_get "${RESTORE_ROOT}/metadata/instance.env" INSTANCE_ROOT)" "restored metadata instance root"
assert_equals "${RESTORE_ROOT}/workspace" "$(dotenv_get "${RESTORE_ROOT}/metadata/instance.env" WORKSPACE_ROOT)" "restored metadata workspace root"
assert_equals "$RESTORE_PUBLIC_ORIGIN" "$(dotenv_get "${RESTORE_ROOT}/metadata/instance.env" PUBLIC_ORIGIN)" "restored metadata public origin"
assert_equals "$RESTORE_CORE_INSTANCE_ID" "$(dotenv_get "${RESTORE_ROOT}/metadata/instance.env" CORE_INSTANCE_ID)" "restored metadata core instance id"
assert_equals "placeholder" "$(dotenv_get "${RESTORE_ROOT}/metadata/instance.env" BOOTSTRAP_TOKEN_CONFIGURED)" "restored metadata bootstrap state"
assert_equals "filesystem" "$(dotenv_get "${RESTORE_ROOT}/metadata/instance.env" BLOB_BACKEND)" "restored metadata blob backend"
assert_equals "copied-local-blob-store" "$(dotenv_get "${RESTORE_ROOT}/metadata/restore-receipt.env" TARGET_BLOB_RESTORE_ACTION)" "restore receipt blob action"
assert_equals "placeholder" "$(dotenv_get "${RESTORE_ROOT}/metadata/restore-receipt.env" BOOTSTRAP_TOKEN_MODE)" "restore receipt bootstrap mode"
assert_equals "disabled" "$(dotenv_get "${RESTORE_ROOT}/metadata/restore-receipt.env" EXPECTED_ACTIVE_BOOTSTRAP_STATE)" "restore receipt expected bootstrap state"
assert_equals "$(manifest_get "${BACKUP_DIR}/manifest.env" PUBLIC_ORIGIN)" "$(dotenv_get "${RESTORE_ROOT}/metadata/restore-source-manifest.env" PUBLIC_ORIGIN)" "source manifest preserved"

assert_not_equals "$SOURCE_BOOTSTRAP_TOKEN" "$(dotenv_get "${RESTORE_ROOT}/config/env.production" OAR_BOOTSTRAP_TOKEN)" "restored bootstrap token"
if [[ -n "$(paths_containing_text "$SOURCE_BOOTSTRAP_TOKEN" "$RESTORE_ROOT")" ]]; then
  die "source bootstrap token should not be copied into restored target"
fi

assert_path_only_in_restore_receipts "$RESTORE_ROOT" "$INSTANCE_ROOT"
assert_path_only_in_restore_receipts "$RESTORE_ROOT" "${INSTANCE_ROOT}/workspace"
assert_path_only_in_restore_receipts "$RESTORE_ROOT" "https://team-alpha.example.test"

EXPECTED_BLOB_FILE_COUNT="$(manifest_get "${BACKUP_DIR}/manifest.env" BLOB_FILE_COUNT)"

MISSING_BLOB_ROOT="${TMP_ROOT}/restored/team-beta-missing-blob"
restore_bundle "$BACKUP_DIR" "$MISSING_BLOB_ROOT" 8014 3014 "team-beta-core-missing-blob"
MISSING_BLOB_ROOT="$(cd "$MISSING_BLOB_ROOT" && pwd -P)"
MISSING_ARTIFACT_HASH="$(sqlite_scalar "${MISSING_BLOB_ROOT}/workspace/state.sqlite" "SELECT COALESCE(content_hash, '') FROM artifacts WHERE kind != 'doc' ORDER BY created_at ASC, id ASC LIMIT 1;")"
[[ -n "$MISSING_ARTIFACT_HASH" ]] || die "expected non-document artifact hash in restored fixture"
replace_blob_with_dummy "$MISSING_BLOB_ROOT" "$MISSING_ARTIFACT_HASH" "placeholder-artifact-blob" "$EXPECTED_BLOB_FILE_COUNT"
MISSING_BLOB_ERR="${TMP_ROOT}/missing-blob.err"
assert_command_fails "$MISSING_BLOB_ERR" \
  "${SCRIPT_DIR}/verify-restore.sh" \
  --instance-root "$MISSING_BLOB_ROOT" \
  --core-bin "$CORE_BIN" \
  --schema-path "$SCHEMA_PATH"
assert_file_contains "$MISSING_BLOB_ERR" "artifact content request returned HTTP 404" "missing blob verification failure"

MISSING_DOC_ROOT="${TMP_ROOT}/restored/team-beta-missing-doc"
restore_bundle "$BACKUP_DIR" "$MISSING_DOC_ROOT" 8015 3015 "team-beta-core-missing-doc"
MISSING_DOC_ROOT="$(cd "$MISSING_DOC_ROOT" && pwd -P)"
MISSING_DOC_HASH="$(sqlite_scalar "${MISSING_DOC_ROOT}/workspace/state.sqlite" "SELECT COALESCE(a.content_hash, '') FROM document_revisions dr JOIN artifacts a ON a.id = dr.artifact_id ORDER BY dr.revision_number ASC, dr.revision_id ASC LIMIT 1;")"
[[ -n "$MISSING_DOC_HASH" ]] || die "expected document revision hash in restored fixture"
replace_blob_with_dummy "$MISSING_DOC_ROOT" "$MISSING_DOC_HASH" "placeholder-document-blob" "$EXPECTED_BLOB_FILE_COUNT"
MISSING_DOC_ERR="${TMP_ROOT}/missing-doc.err"
assert_command_fails "$MISSING_DOC_ERR" \
  "${SCRIPT_DIR}/verify-restore.sh" \
  --instance-root "$MISSING_DOC_ROOT" \
  --core-bin "$CORE_BIN" \
  --schema-path "$SCHEMA_PATH"
assert_file_contains "$MISSING_DOC_ERR" "document revision request returned HTTP 404" "missing document revision verification failure"

RESTORE_WITH_SECRETS_ROOT="${TMP_ROOT}/restored/team-gamma"
"${SCRIPT_DIR}/restore-workspace.sh" \
  --backup-dir "$BACKUP_WITH_SECRETS_DIR" \
  --target-instance-root "$RESTORE_WITH_SECRETS_ROOT" \
  --instance "team-gamma" \
  --public-origin "https://team-gamma.example.test" \
  --listen-port 8016 \
  --web-ui-port 3016 \
  --core-instance-id "team-gamma-core"
RESTORE_WITH_SECRETS_ROOT="$(cd "$RESTORE_WITH_SECRETS_ROOT" && pwd -P)"

"${SCRIPT_DIR}/verify-restore.sh" \
  --instance-root "$RESTORE_WITH_SECRETS_ROOT" \
  --core-bin "$CORE_BIN" \
  --schema-path "$SCHEMA_PATH"

assert_equals "${RESTORE_WITH_SECRETS_ROOT}/workspace" "$(dotenv_get "${RESTORE_WITH_SECRETS_ROOT}/config/env.production" HOST_OAR_WORKSPACE_ROOT)" "restore with secrets workspace root"
assert_equals "team-gamma-core" "$(dotenv_get "${RESTORE_WITH_SECRETS_ROOT}/config/env.production" OAR_CORE_INSTANCE_ID)" "restore with secrets instance id"
assert_equals "$HOSTED_BOOTSTRAP_PLACEHOLDER" "$(dotenv_get "${RESTORE_WITH_SECRETS_ROOT}/config/env.production" OAR_BOOTSTRAP_TOKEN)" "restore with secrets bootstrap token default"
assert_equals "filesystem" "$(dotenv_get "${RESTORE_WITH_SECRETS_ROOT}/config/env.production" OAR_BLOB_BACKEND)" "restore with secrets blob backend"

"${SCRIPT_DIR}/provision-workspace.sh" \
  --instance team-object \
  --instance-root "$OBJECT_INSTANCE_ROOT" \
  --public-origin https://team-object.example.test \
  --listen-port 8020 \
  --web-ui-port 3020 \
  --blob-backend object \
  --blob-root blob-store \
  --generate-bootstrap-token
OBJECT_INSTANCE_ROOT="$(cd "$OBJECT_INSTANCE_ROOT" && pwd -P)"

seed_workspace_fixture "${OBJECT_INSTANCE_ROOT}/workspace" "$CORE_BIN" "$SCHEMA_PATH" "$(pick_loopback_port)" "${TMP_ROOT}/seed-object.log"

"${SCRIPT_DIR}/backup-workspace.sh" \
  --instance-root "$OBJECT_INSTANCE_ROOT" \
  --output-dir "$OBJECT_BACKUP_DIR"
OBJECT_BACKUP_DIR="$(cd "$OBJECT_BACKUP_DIR" && pwd -P)"

assert_dir_exists "${OBJECT_BACKUP_DIR}/workspace/blob-store"
assert_equals "object" "$(manifest_get "${OBJECT_BACKUP_DIR}/manifest.env" BLOB_BACKEND)" "object manifest blob backend"
assert_equals "local" "$(manifest_get "${OBJECT_BACKUP_DIR}/manifest.env" BLOB_STORAGE_MODE)" "object manifest blob storage mode"
assert_equals "copy" "$(manifest_get "${OBJECT_BACKUP_DIR}/manifest.env" BLOB_BACKUP_MODE)" "object manifest blob backup mode"
assert_equals "${OBJECT_INSTANCE_ROOT}/blob-store" "$(manifest_get "${OBJECT_BACKUP_DIR}/manifest.env" BLOB_ROOT)" "object manifest blob root"
assert_equals "workspace/blob-store" "$(manifest_get "${OBJECT_BACKUP_DIR}/manifest.env" BLOB_BUNDLE_PATH)" "object manifest blob bundle path"

"${SCRIPT_DIR}/restore-workspace.sh" \
  --backup-dir "$OBJECT_BACKUP_DIR" \
  --target-instance-root "$OBJECT_RESTORE_ROOT" \
  --instance "team-object-restore" \
  --public-origin "https://team-object-restore.example.test" \
  --listen-port 8021 \
  --web-ui-port 3021 \
  --core-instance-id "team-object-restore-core"
OBJECT_RESTORE_ROOT="$(cd "$OBJECT_RESTORE_ROOT" && pwd -P)"

"${SCRIPT_DIR}/verify-restore.sh" \
  --instance-root "$OBJECT_RESTORE_ROOT" \
  --core-bin "$CORE_BIN" \
  --schema-path "$SCHEMA_PATH"

assert_equals "object" "$(dotenv_get "${OBJECT_RESTORE_ROOT}/config/env.production" OAR_BLOB_BACKEND)" "object restore env blob backend"
assert_equals "${OBJECT_RESTORE_ROOT}/blob-store" "$(dotenv_get "${OBJECT_RESTORE_ROOT}/config/env.production" OAR_BLOB_ROOT)" "object restore env blob root"
assert_equals "object" "$(dotenv_get "${OBJECT_RESTORE_ROOT}/metadata/instance.env" BLOB_BACKEND)" "object restore metadata blob backend"
assert_equals "${OBJECT_RESTORE_ROOT}/blob-store" "$(dotenv_get "${OBJECT_RESTORE_ROOT}/metadata/instance.env" BLOB_ROOT)" "object restore metadata blob root"
assert_equals "copied-local-blob-store" "$(dotenv_get "${OBJECT_RESTORE_ROOT}/metadata/restore-receipt.env" TARGET_BLOB_RESTORE_ACTION)" "object restore receipt blob action"

"${SCRIPT_DIR}/provision-workspace.sh" \
  --instance team-s3 \
  --instance-root "$S3_SOURCE_ROOT" \
  --public-origin https://team-s3.example.test \
  --listen-port 8022 \
  --web-ui-port 3022 \
  --generate-bootstrap-token
S3_SOURCE_ROOT="$(cd "$S3_SOURCE_ROOT" && pwd -P)"

seed_workspace_fixture "${S3_SOURCE_ROOT}/workspace" "$CORE_BIN" "$SCHEMA_PATH" "$(pick_loopback_port)" "${TMP_ROOT}/seed-s3-synthetic.log"

"${SCRIPT_DIR}/provision-workspace.sh" \
  --instance team-s3 \
  --instance-root "$S3_SOURCE_ROOT" \
  --public-origin https://team-s3.example.test \
  --listen-port 8022 \
  --web-ui-port 3022 \
  --blob-backend s3 \
  --blob-s3-bucket oar-test-blobs \
  --blob-s3-prefix workspaces/team-s3/ \
  --blob-s3-region auto \
  --blob-s3-endpoint https://r2.example.test \
  --blob-s3-force-path-style true \
  --force

"${SCRIPT_DIR}/backup-workspace.sh" \
  --instance-root "$S3_SOURCE_ROOT" \
  --output-dir "$S3_BACKUP_DIR"
S3_BACKUP_DIR="$(cd "$S3_BACKUP_DIR" && pwd -P)"

assert_path_missing "${S3_BACKUP_DIR}/workspace/blob-store"
assert_equals "s3" "$(manifest_get "${S3_BACKUP_DIR}/manifest.env" BLOB_BACKEND)" "s3 manifest blob backend"
assert_equals "remote" "$(manifest_get "${S3_BACKUP_DIR}/manifest.env" BLOB_STORAGE_MODE)" "s3 manifest blob storage mode"
assert_equals "reference" "$(manifest_get "${S3_BACKUP_DIR}/manifest.env" BLOB_BACKUP_MODE)" "s3 manifest blob backup mode"
assert_equals "0" "$(manifest_get "${S3_BACKUP_DIR}/manifest.env" BLOB_FILE_COUNT)" "s3 manifest blob file count"
assert_equals "" "$(manifest_get "${S3_BACKUP_DIR}/manifest.env" BLOB_BUNDLE_PATH)" "s3 manifest blob bundle path"
assert_equals "oar-test-blobs" "$(manifest_get "${S3_BACKUP_DIR}/manifest.env" BLOB_S3_BUCKET)" "s3 manifest bucket"
assert_equals "workspaces/team-s3/" "$(manifest_get "${S3_BACKUP_DIR}/manifest.env" BLOB_S3_PREFIX)" "s3 manifest prefix"
assert_equals "auto" "$(manifest_get "${S3_BACKUP_DIR}/manifest.env" BLOB_S3_REGION)" "s3 manifest region"
assert_equals "https://r2.example.test" "$(manifest_get "${S3_BACKUP_DIR}/manifest.env" BLOB_S3_ENDPOINT)" "s3 manifest endpoint"
assert_equals "true" "$(manifest_get "${S3_BACKUP_DIR}/manifest.env" BLOB_S3_FORCE_PATH_STYLE)" "s3 manifest force path style"

"${SCRIPT_DIR}/restore-workspace.sh" \
  --backup-dir "$S3_BACKUP_DIR" \
  --target-instance-root "$S3_RESTORE_ROOT" \
  --instance "team-s3-restore" \
  --public-origin "https://team-s3-restore.example.test" \
  --listen-port 8023 \
  --web-ui-port 3023 \
  --core-instance-id "team-s3-restore-core"
S3_RESTORE_ROOT="$(cd "$S3_RESTORE_ROOT" && pwd -P)"

assert_equals "s3" "$(dotenv_get "${S3_RESTORE_ROOT}/config/env.production" OAR_BLOB_BACKEND)" "s3 restore env blob backend"
assert_equals "oar-test-blobs" "$(dotenv_get "${S3_RESTORE_ROOT}/config/env.production" OAR_BLOB_S3_BUCKET)" "s3 restore env bucket"
assert_equals "workspaces/team-s3/" "$(dotenv_get "${S3_RESTORE_ROOT}/config/env.production" OAR_BLOB_S3_PREFIX)" "s3 restore env prefix"
assert_equals "auto" "$(dotenv_get "${S3_RESTORE_ROOT}/config/env.production" OAR_BLOB_S3_REGION)" "s3 restore env region"
assert_equals "https://r2.example.test" "$(dotenv_get "${S3_RESTORE_ROOT}/config/env.production" OAR_BLOB_S3_ENDPOINT)" "s3 restore env endpoint"
assert_equals "true" "$(dotenv_get "${S3_RESTORE_ROOT}/config/env.production" OAR_BLOB_S3_FORCE_PATH_STYLE)" "s3 restore env force path style"
assert_equals "s3" "$(dotenv_get "${S3_RESTORE_ROOT}/metadata/instance.env" BLOB_BACKEND)" "s3 restore metadata blob backend"
assert_equals "reference-remote-blob-store" "$(dotenv_get "${S3_RESTORE_ROOT}/metadata/restore-receipt.env" TARGET_BLOB_RESTORE_ACTION)" "s3 restore receipt blob action"
assert_equals "s3://oar-test-blobs/workspaces/team-s3/" "$(dotenv_get "${S3_RESTORE_ROOT}/metadata/restore-receipt.env" TARGET_BLOB_EFFECTIVE_LOCATION)" "s3 restore receipt blob location"

KEEP_SOURCE_ERR="${TMP_ROOT}/keep-source-err"
if "${SCRIPT_DIR}/restore-workspace.sh" \
  --backup-dir "$BACKUP_DIR" \
  --target-instance-root "${TMP_ROOT}/restored/keep-source-should-fail" \
  --instance "keep-source-test" \
  --public-origin "https://keep-source.example.test" \
  --listen-port 8017 \
  --web-ui-port 3017 \
  --bootstrap-token-mode keep-source \
  >"${KEEP_SOURCE_ERR}.out" 2>"$KEEP_SOURCE_ERR"; then
  die "keep-source should fail for secret-free bundle"
fi
assert_file_contains "$KEEP_SOURCE_ERR" "does not include config secrets" "keep-source failure message"

log "Hosted ops tests passed."
