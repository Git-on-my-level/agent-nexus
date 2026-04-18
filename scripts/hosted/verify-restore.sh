#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/common.sh"

usage() {
  cat <<'EOF'
Usage: scripts/hosted/verify-restore.sh --instance-root DIR [options]

Starts anx-core against the restored workspace on a loopback port, verifies
/readyz, and checks restored metadata counts against the source manifest.

Options:
  --manifest PATH        Manifest to compare against
  --receipt PATH         Restore receipt to compare against
  --core-bin PATH        anx-core binary to use
  --schema-path PATH     Schema path to use
  --listen-port PORT     Explicit loopback verification port
  --timeout SECONDS      Readiness wait timeout (default: 20)
EOF
}

INSTANCE_ROOT=""
MANIFEST_PATH=""
RECEIPT_PATH=""
CORE_BIN=""
SCHEMA_PATH=""
LISTEN_PORT=""
TIMEOUT_SECONDS="20"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --instance-root) INSTANCE_ROOT="$2"; shift 2 ;;
    --manifest) MANIFEST_PATH="$2"; shift 2 ;;
    --receipt) RECEIPT_PATH="$2"; shift 2 ;;
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
INSTANCE_METADATA_FILE="${INSTANCE_ROOT}/metadata/instance.env"
if [[ -z "$MANIFEST_PATH" ]]; then
  MANIFEST_PATH="${INSTANCE_ROOT}/metadata/restore-source-manifest.env"
fi
if [[ -z "$RECEIPT_PATH" ]]; then
  RECEIPT_PATH="${INSTANCE_ROOT}/metadata/restore-receipt.env"
fi
[[ -f "$MANIFEST_PATH" ]] || die "manifest not found: $MANIFEST_PATH"
[[ -f "$INSTANCE_METADATA_FILE" ]] || die "instance metadata not found: $INSTANCE_METADATA_FILE"
[[ -f "${WORKSPACE_ROOT}/state.sqlite" ]] || die "restored sqlite database not found: ${WORKSPACE_ROOT}/state.sqlite"
validate_backup_format_version "$(manifest_get "$MANIFEST_PATH" FORMAT_VERSION || true)"

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

unset ANX_BOOTSTRAP_TOKEN
load_dotenv_file "$ENV_FILE"
EXPECTED_ARTIFACT_COUNT="$(manifest_get "$MANIFEST_PATH" ARTIFACT_COUNT)"
EXPECTED_AGENT_COUNT="$(manifest_get "$MANIFEST_PATH" AGENT_COUNT)"
EXPECTED_INVITE_COUNT="$(manifest_get "$MANIFEST_PATH" INVITE_COUNT)"
EXPECTED_DOCUMENT_COUNT="$(manifest_get "$MANIFEST_PATH" DOCUMENT_COUNT || true)"
EXPECTED_DOCUMENT_REVISION_COUNT="$(manifest_get "$MANIFEST_PATH" DOCUMENT_REVISION_COUNT || true)"
EXPECTED_BLOB_FILE_COUNT="$(manifest_get "$MANIFEST_PATH" BLOB_FILE_COUNT || true)"
EXPECTED_BLOB_BACKEND="$(manifest_get "$MANIFEST_PATH" BLOB_BACKEND || true)"
EXPECTED_BLOB_STORAGE_MODE="$(manifest_get "$MANIFEST_PATH" BLOB_STORAGE_MODE || true)"
EXPECTED_BLOB_BACKUP_MODE="$(manifest_get "$MANIFEST_PATH" BLOB_BACKUP_MODE || true)"
EXPECTED_BLOB_ROOT="$(manifest_get "$MANIFEST_PATH" BLOB_ROOT || true)"
EXPECTED_BLOB_EFFECTIVE_LOCATION="$(manifest_get "$MANIFEST_PATH" BLOB_EFFECTIVE_LOCATION || true)"
EXPECTED_BLOB_BUNDLE_PATH="$(manifest_get "$MANIFEST_PATH" BLOB_BUNDLE_PATH || true)"
EXPECTED_BLOB_S3_BUCKET="$(manifest_get "$MANIFEST_PATH" BLOB_S3_BUCKET || true)"
EXPECTED_BLOB_S3_PREFIX="$(manifest_get "$MANIFEST_PATH" BLOB_S3_PREFIX || true)"
EXPECTED_BLOB_S3_REGION="$(manifest_get "$MANIFEST_PATH" BLOB_S3_REGION || true)"
EXPECTED_BLOB_S3_ENDPOINT="$(manifest_get "$MANIFEST_PATH" BLOB_S3_ENDPOINT || true)"
EXPECTED_BLOB_S3_FORCE_PATH_STYLE="$(normalize_bool_value "$(manifest_get "$MANIFEST_PATH" BLOB_S3_FORCE_PATH_STYLE || true)")"
[[ -n "$EXPECTED_BLOB_BACKEND" ]] || EXPECTED_BLOB_BACKEND="filesystem"
validate_blob_backend "$EXPECTED_BLOB_BACKEND"
[[ -n "$EXPECTED_BLOB_STORAGE_MODE" ]] || EXPECTED_BLOB_STORAGE_MODE="$(blob_storage_mode "$EXPECTED_BLOB_BACKEND")"
[[ -n "$EXPECTED_BLOB_BACKUP_MODE" ]] || EXPECTED_BLOB_BACKUP_MODE="$(blob_backup_mode "$EXPECTED_BLOB_BACKEND")"
EXPECTED_CORE_INSTANCE_ID="$(manifest_get "$MANIFEST_PATH" CORE_INSTANCE_ID || true)"
EXPECTED_BOOTSTRAP_STATE="$(manifest_get "$MANIFEST_PATH" BOOTSTRAP_STATE || true)"
VERIFY_ARTIFACT_ID="$(manifest_get "$MANIFEST_PATH" VERIFY_ARTIFACT_ID || true)"
VERIFY_DOCUMENT_ID="$(manifest_get "$MANIFEST_PATH" VERIFY_DOCUMENT_ID || true)"
VERIFY_DOCUMENT_REVISION_ID="$(manifest_get "$MANIFEST_PATH" VERIFY_DOCUMENT_REVISION_ID || true)"
SOURCE_INSTANCE_ROOT="$(manifest_get "$MANIFEST_PATH" SOURCE_INSTANCE_ROOT || true)"
SOURCE_WORKSPACE_ROOT="$(manifest_get "$MANIFEST_PATH" SOURCE_WORKSPACE_ROOT || true)"
SOURCE_PUBLIC_ORIGIN="$(manifest_get "$MANIFEST_PATH" PUBLIC_ORIGIN || true)"
TARGET_CORE_INSTANCE_ID="$(dotenv_get "$ENV_FILE" ANX_CORE_INSTANCE_ID || true)"
TARGET_WORKSPACE_ROOT="$(dotenv_get "$ENV_FILE" HOST_ANX_WORKSPACE_ROOT || true)"
TARGET_WEB_UI_ORIGIN="$(dotenv_get "$ENV_FILE" ANX_WEB_UI_ORIGIN || true)"
TARGET_WEBAUTHN_ORIGIN="$(dotenv_get "$ENV_FILE" ANX_WEBAUTHN_ORIGIN || true)"
TARGET_BLOB_BACKEND="$(dotenv_get "$ENV_FILE" ANX_BLOB_BACKEND || true)"
TARGET_BLOB_ROOT="$(dotenv_get "$ENV_FILE" ANX_BLOB_ROOT || true)"
TARGET_BLOB_S3_BUCKET="$(dotenv_get "$ENV_FILE" ANX_BLOB_S3_BUCKET || true)"
TARGET_BLOB_S3_PREFIX="$(dotenv_get "$ENV_FILE" ANX_BLOB_S3_PREFIX || true)"
TARGET_BLOB_S3_REGION="$(dotenv_get "$ENV_FILE" ANX_BLOB_S3_REGION || true)"
TARGET_BLOB_S3_ENDPOINT="$(dotenv_get "$ENV_FILE" ANX_BLOB_S3_ENDPOINT || true)"
TARGET_BLOB_S3_FORCE_PATH_STYLE="$(normalize_bool_value "$(dotenv_get "$ENV_FILE" ANX_BLOB_S3_FORCE_PATH_STYLE || true)")"
[[ -n "$TARGET_BLOB_BACKEND" ]] || TARGET_BLOB_BACKEND="filesystem"
validate_blob_backend "$TARGET_BLOB_BACKEND"
TARGET_BLOB_STORAGE_MODE="$(blob_storage_mode "$TARGET_BLOB_BACKEND")"
TARGET_EFFECTIVE_BLOB_LOCATION="$(blob_effective_location "$WORKSPACE_ROOT" "$TARGET_BLOB_BACKEND" "$TARGET_BLOB_ROOT" "$TARGET_BLOB_S3_BUCKET" "$TARGET_BLOB_S3_PREFIX")"
TARGET_EFFECTIVE_LOCAL_BLOB_ROOT="$(blob_effective_local_root "$WORKSPACE_ROOT" "$TARGET_BLOB_BACKEND" "$TARGET_BLOB_ROOT")"
METADATA_INSTANCE_ROOT="$(dotenv_get "$INSTANCE_METADATA_FILE" INSTANCE_ROOT || true)"
METADATA_WORKSPACE_ROOT="$(dotenv_get "$INSTANCE_METADATA_FILE" WORKSPACE_ROOT || true)"
METADATA_BACKUPS_DIR="$(dotenv_get "$INSTANCE_METADATA_FILE" BACKUPS_DIR || true)"
METADATA_PUBLIC_ORIGIN="$(dotenv_get "$INSTANCE_METADATA_FILE" PUBLIC_ORIGIN || true)"
METADATA_CORE_INSTANCE_ID="$(dotenv_get "$INSTANCE_METADATA_FILE" CORE_INSTANCE_ID || true)"
METADATA_BLOB_BACKEND="$(dotenv_get "$INSTANCE_METADATA_FILE" BLOB_BACKEND || true)"
METADATA_BLOB_STORAGE_MODE="$(dotenv_get "$INSTANCE_METADATA_FILE" BLOB_STORAGE_MODE || true)"
METADATA_BLOB_ROOT="$(dotenv_get "$INSTANCE_METADATA_FILE" BLOB_ROOT || true)"
METADATA_BLOB_EFFECTIVE_LOCATION="$(dotenv_get "$INSTANCE_METADATA_FILE" BLOB_EFFECTIVE_LOCATION || true)"
METADATA_BLOB_S3_BUCKET="$(dotenv_get "$INSTANCE_METADATA_FILE" BLOB_S3_BUCKET || true)"
METADATA_BLOB_S3_PREFIX="$(dotenv_get "$INSTANCE_METADATA_FILE" BLOB_S3_PREFIX || true)"
METADATA_BLOB_S3_REGION="$(dotenv_get "$INSTANCE_METADATA_FILE" BLOB_S3_REGION || true)"
METADATA_BLOB_S3_ENDPOINT="$(dotenv_get "$INSTANCE_METADATA_FILE" BLOB_S3_ENDPOINT || true)"
METADATA_BLOB_S3_FORCE_PATH_STYLE="$(normalize_bool_value "$(dotenv_get "$INSTANCE_METADATA_FILE" BLOB_S3_FORCE_PATH_STYLE || true)")"
EXPECTED_TARGET_WORKSPACE_ROOT="${INSTANCE_ROOT}/workspace"
EXPECTED_TARGET_BACKUPS_DIR="${INSTANCE_ROOT}/backups"
EXPECTED_ACTIVE_BOOTSTRAP_STATE=""
RESTORE_BOOTSTRAP_TOKEN_MODE=""
RECEIPT_TARGET_BLOB_BACKEND=""
RECEIPT_TARGET_BLOB_ROOT=""
RECEIPT_TARGET_BLOB_EFFECTIVE_LOCATION=""
RECEIPT_TARGET_BLOB_RESTORE_ACTION=""
if [[ -f "$RECEIPT_PATH" ]]; then
  EXPECTED_ACTIVE_BOOTSTRAP_STATE="$(dotenv_get "$RECEIPT_PATH" EXPECTED_ACTIVE_BOOTSTRAP_STATE || true)"
  RESTORE_BOOTSTRAP_TOKEN_MODE="$(dotenv_get "$RECEIPT_PATH" BOOTSTRAP_TOKEN_MODE || true)"
  RECEIPT_TARGET_BLOB_BACKEND="$(dotenv_get "$RECEIPT_PATH" TARGET_BLOB_BACKEND || true)"
  RECEIPT_TARGET_BLOB_ROOT="$(dotenv_get "$RECEIPT_PATH" TARGET_BLOB_ROOT || true)"
  RECEIPT_TARGET_BLOB_EFFECTIVE_LOCATION="$(dotenv_get "$RECEIPT_PATH" TARGET_BLOB_EFFECTIVE_LOCATION || true)"
  RECEIPT_TARGET_BLOB_RESTORE_ACTION="$(dotenv_get "$RECEIPT_PATH" TARGET_BLOB_RESTORE_ACTION || true)"
fi

SERVER_LOG_DIR="${WORKSPACE_ROOT}/logs"
SERVER_LOG_FILE="${SERVER_LOG_DIR}/restore-verify.log"
mkdir -p "$SERVER_LOG_DIR"
VERIFY_TMP_DIR="$(mktemp -d)"

sqlite_quote() {
  printf '%s' "$1" | sed "s/'/''/g"
}

curl_expect_200() {
  local url="$1"
  local output_file="$2"
  local label="$3"
  local status
  status="$(curl -sS -o "$output_file" -w '%{http_code}' "$url")" || die "${label} request failed: ${url}"
  [[ "$status" == "200" ]] || die "${label} request returned HTTP ${status}: ${url}"
}

verify_live_artifact_read() {
  local artifact_id="$1"
  local metadata_file="${VERIFY_TMP_DIR}/artifact-metadata.json"
  local content_file="${VERIFY_TMP_DIR}/artifact-content.bin"
  local expected_content_hash
  local actual_content_hash

  expected_content_hash="$(sqlite_scalar "${WORKSPACE_ROOT}/state.sqlite" "SELECT COALESCE(content_hash, '') FROM artifacts WHERE id = '$(sqlite_quote "$artifact_id")';")"
  [[ -n "$expected_content_hash" ]] || die "live artifact verification sample not found in restored database: ${artifact_id}"

  curl_expect_200 "http://127.0.0.1:${LISTEN_PORT}/artifacts/${artifact_id}" "$metadata_file" "artifact metadata"
  grep -F "\"id\":\"${artifact_id}\"" "$metadata_file" >/dev/null || die "artifact metadata response did not include artifact id ${artifact_id}"
  grep -F "\"content_hash\":\"${expected_content_hash}\"" "$metadata_file" >/dev/null || die "artifact metadata response did not include expected content hash ${expected_content_hash} for ${artifact_id}"

  curl_expect_200 "http://127.0.0.1:${LISTEN_PORT}/artifacts/${artifact_id}/content" "$content_file" "artifact content"
  [[ -s "$content_file" ]] || die "artifact content response was empty for ${artifact_id}"
  actual_content_hash="$(sha256_file "$content_file")"
  [[ "$actual_content_hash" == "$expected_content_hash" ]] || die "artifact content hash mismatch for ${artifact_id}: expected ${expected_content_hash}, got ${actual_content_hash}"
}

verify_live_document_revision_read() {
  local document_id="$1"
  local revision_id="$2"
  local revision_file="${VERIFY_TMP_DIR}/document-revision.json"
  local expected_content_hash

  expected_content_hash="$(sqlite_scalar "${WORKSPACE_ROOT}/state.sqlite" "SELECT COALESCE(a.content_hash, '') FROM document_revisions dr JOIN artifacts a ON a.id = dr.artifact_id WHERE dr.document_id = '$(sqlite_quote "$document_id")' AND dr.revision_id = '$(sqlite_quote "$revision_id")';")"
  [[ -n "$expected_content_hash" ]] || die "live document verification sample not found in restored database: document=${document_id} revision=${revision_id}"

  curl_expect_200 "http://127.0.0.1:${LISTEN_PORT}/docs/${document_id}/revisions/${revision_id}" "$revision_file" "document revision"
  grep -F "\"document_id\":\"${document_id}\"" "$revision_file" >/dev/null || die "document revision response did not include document id ${document_id}"
  grep -F "\"revision_id\":\"${revision_id}\"" "$revision_file" >/dev/null || die "document revision response did not include revision id ${revision_id}"
  grep -F "\"content_hash\":\"${expected_content_hash}\"" "$revision_file" >/dev/null || die "document revision response did not include expected content hash ${expected_content_hash} for ${revision_id}"
  grep -F '"content":' "$revision_file" >/dev/null || die "document revision response did not include revision content for ${revision_id}"
}

verify_referenced_blob_reachability() {
  local artifact_id content_hash
  while IFS='|' read -r artifact_id content_hash; do
    [[ -n "$artifact_id" && -n "$content_hash" ]] || continue
    verify_live_artifact_read "$artifact_id"
  done < <(sqlite3 -noheader -batch -separator '|' "${WORKSPACE_ROOT}/state.sqlite" "SELECT MIN(id), content_hash FROM artifacts WHERE TRIM(content_hash) <> '' GROUP BY content_hash ORDER BY content_hash;")
}

cleanup() {
  stop_background_process "${SERVER_PID:-}"
  rm -rf "$VERIFY_TMP_DIR"
}
trap cleanup EXIT

start_core_server \
  SERVER_PID \
  "$CORE_BIN" \
  "$WORKSPACE_ROOT" \
  "$SCHEMA_PATH" \
  "127.0.0.1" \
  "$LISTEN_PORT" \
  "$SERVER_LOG_FILE" \
  "${TARGET_CORE_INSTANCE_ID:-${EXPECTED_CORE_INSTANCE_ID:-restore-verify}}" \
  set \
  "${ANX_BOOTSTRAP_TOKEN-}" \
  false \
  false \
  true

if ! wait_for_http_ok "http://127.0.0.1:${LISTEN_PORT}/readyz" "$TIMEOUT_SECONDS"; then
  die "restore verification could not reach /readyz on 127.0.0.1:${LISTEN_PORT}; see ${SERVER_LOG_FILE}"
fi

ACTUAL_ARTIFACT_COUNT="$(sqlite_scalar "${WORKSPACE_ROOT}/state.sqlite" "SELECT COUNT(*) FROM artifacts;")"
ACTUAL_AGENT_COUNT="$(sqlite_scalar "${WORKSPACE_ROOT}/state.sqlite" "SELECT COUNT(*) FROM agents;")"
ACTUAL_INVITE_COUNT="$(sqlite_scalar "${WORKSPACE_ROOT}/state.sqlite" "SELECT COUNT(*) FROM auth_invites;")"
ACTUAL_DOCUMENT_COUNT="$(sqlite_scalar "${WORKSPACE_ROOT}/state.sqlite" "SELECT COUNT(*) FROM documents;")"
ACTUAL_DOCUMENT_REVISION_COUNT="$(sqlite_scalar "${WORKSPACE_ROOT}/state.sqlite" "SELECT COUNT(*) FROM document_revisions;")"
ACTUAL_BLOB_FILE_COUNT=""
if [[ "$TARGET_BLOB_STORAGE_MODE" == "local" ]]; then
  ACTUAL_BLOB_FILE_COUNT="$(count_files "$TARGET_EFFECTIVE_LOCAL_BLOB_ROOT")"
fi
ACTUAL_BOOTSTRAP_STATE="disabled"
if [[ -n "${ANX_BOOTSTRAP_TOKEN:-}" && "${ANX_BOOTSTRAP_TOKEN}" != "$HOSTED_BOOTSTRAP_PLACEHOLDER" ]]; then
  ACTUAL_BOOTSTRAP_STATE="available"
  BOOTSTRAP_CONSUMED_AT="$(sqlite_scalar "${WORKSPACE_ROOT}/state.sqlite" "SELECT COALESCE(consumed_at, '') FROM auth_bootstrap_state WHERE id = 1;")"
  if [[ -n "$BOOTSTRAP_CONSUMED_AT" ]]; then
    ACTUAL_BOOTSTRAP_STATE="consumed"
  fi
fi

[[ "$ACTUAL_ARTIFACT_COUNT" == "$EXPECTED_ARTIFACT_COUNT" ]] || die "artifact count mismatch: expected ${EXPECTED_ARTIFACT_COUNT}, got ${ACTUAL_ARTIFACT_COUNT}"
[[ "$ACTUAL_AGENT_COUNT" == "$EXPECTED_AGENT_COUNT" ]] || die "agent count mismatch: expected ${EXPECTED_AGENT_COUNT}, got ${ACTUAL_AGENT_COUNT}"
[[ "$ACTUAL_INVITE_COUNT" == "$EXPECTED_INVITE_COUNT" ]] || die "invite count mismatch: expected ${EXPECTED_INVITE_COUNT}, got ${ACTUAL_INVITE_COUNT}"
if [[ -n "$EXPECTED_DOCUMENT_COUNT" ]]; then
  [[ "$ACTUAL_DOCUMENT_COUNT" == "$EXPECTED_DOCUMENT_COUNT" ]] || die "document count mismatch: expected ${EXPECTED_DOCUMENT_COUNT}, got ${ACTUAL_DOCUMENT_COUNT}"
fi
if [[ -n "$EXPECTED_DOCUMENT_REVISION_COUNT" ]]; then
  [[ "$ACTUAL_DOCUMENT_REVISION_COUNT" == "$EXPECTED_DOCUMENT_REVISION_COUNT" ]] || die "document revision count mismatch: expected ${EXPECTED_DOCUMENT_REVISION_COUNT}, got ${ACTUAL_DOCUMENT_REVISION_COUNT}"
fi
[[ "$TARGET_BLOB_BACKEND" == "$EXPECTED_BLOB_BACKEND" ]] || die "blob backend mismatch: manifest=${EXPECTED_BLOB_BACKEND} env=${TARGET_BLOB_BACKEND}"
[[ "$METADATA_BLOB_BACKEND" == "$TARGET_BLOB_BACKEND" ]] || die "active metadata/blob backend mismatch: env=${TARGET_BLOB_BACKEND} metadata=${METADATA_BLOB_BACKEND:-<unset>}"
[[ "$METADATA_BLOB_STORAGE_MODE" == "$TARGET_BLOB_STORAGE_MODE" ]] || die "active metadata/blob storage mode mismatch: env=${TARGET_BLOB_STORAGE_MODE} metadata=${METADATA_BLOB_STORAGE_MODE:-<unset>}"
[[ "$METADATA_BLOB_ROOT" == "$TARGET_BLOB_ROOT" ]] || die "active metadata/blob root mismatch: env=${TARGET_BLOB_ROOT:-<unset>} metadata=${METADATA_BLOB_ROOT:-<unset>}"
[[ "$METADATA_BLOB_EFFECTIVE_LOCATION" == "$TARGET_EFFECTIVE_BLOB_LOCATION" ]] || die "active metadata/blob location mismatch: env=${TARGET_EFFECTIVE_BLOB_LOCATION} metadata=${METADATA_BLOB_EFFECTIVE_LOCATION:-<unset>}"
if [[ -n "$RECEIPT_TARGET_BLOB_BACKEND" ]]; then
  [[ "$RECEIPT_TARGET_BLOB_BACKEND" == "$TARGET_BLOB_BACKEND" ]] || die "restore receipt/blob backend mismatch: receipt=${RECEIPT_TARGET_BLOB_BACKEND} env=${TARGET_BLOB_BACKEND}"
fi
if [[ -n "$RECEIPT_TARGET_BLOB_ROOT" || -n "$TARGET_BLOB_ROOT" ]]; then
  [[ "$RECEIPT_TARGET_BLOB_ROOT" == "$TARGET_BLOB_ROOT" ]] || die "restore receipt/blob root mismatch: receipt=${RECEIPT_TARGET_BLOB_ROOT:-<unset>} env=${TARGET_BLOB_ROOT:-<unset>}"
fi
if [[ -n "$RECEIPT_TARGET_BLOB_EFFECTIVE_LOCATION" ]]; then
  [[ "$RECEIPT_TARGET_BLOB_EFFECTIVE_LOCATION" == "$TARGET_EFFECTIVE_BLOB_LOCATION" ]] || die "restore receipt/blob location mismatch: receipt=${RECEIPT_TARGET_BLOB_EFFECTIVE_LOCATION} env=${TARGET_EFFECTIVE_BLOB_LOCATION}"
fi
if [[ "$TARGET_BLOB_STORAGE_MODE" == "local" && -n "$EXPECTED_BLOB_FILE_COUNT" ]]; then
  [[ "$ACTUAL_BLOB_FILE_COUNT" == "$EXPECTED_BLOB_FILE_COUNT" ]] || die "blob file count mismatch: expected ${EXPECTED_BLOB_FILE_COUNT}, got ${ACTUAL_BLOB_FILE_COUNT}"
fi
[[ "$TARGET_WORKSPACE_ROOT" == "$EXPECTED_TARGET_WORKSPACE_ROOT" ]] || die "active env workspace root mismatch: expected ${EXPECTED_TARGET_WORKSPACE_ROOT}, got ${TARGET_WORKSPACE_ROOT:-<unset>}"
[[ "$METADATA_INSTANCE_ROOT" == "$INSTANCE_ROOT" ]] || die "active metadata instance root mismatch: expected ${INSTANCE_ROOT}, got ${METADATA_INSTANCE_ROOT:-<unset>}"
[[ "$METADATA_WORKSPACE_ROOT" == "$EXPECTED_TARGET_WORKSPACE_ROOT" ]] || die "active metadata workspace root mismatch: expected ${EXPECTED_TARGET_WORKSPACE_ROOT}, got ${METADATA_WORKSPACE_ROOT:-<unset>}"
[[ "$METADATA_BACKUPS_DIR" == "$EXPECTED_TARGET_BACKUPS_DIR" ]] || die "active metadata backups dir mismatch: expected ${EXPECTED_TARGET_BACKUPS_DIR}, got ${METADATA_BACKUPS_DIR:-<unset>}"
[[ "$TARGET_WEB_UI_ORIGIN" == "$TARGET_WEBAUTHN_ORIGIN" ]] || die "active env origin mismatch: web ui origin=${TARGET_WEB_UI_ORIGIN:-<unset>} webauthn origin=${TARGET_WEBAUTHN_ORIGIN:-<unset>}"
[[ "$TARGET_WEB_UI_ORIGIN" == "$METADATA_PUBLIC_ORIGIN" ]] || die "active metadata/public origin mismatch: env=${TARGET_WEB_UI_ORIGIN:-<unset>} metadata=${METADATA_PUBLIC_ORIGIN:-<unset>}"
[[ "$TARGET_CORE_INSTANCE_ID" == "$METADATA_CORE_INSTANCE_ID" ]] || die "active metadata/core instance id mismatch: env=${TARGET_CORE_INSTANCE_ID:-<unset>} metadata=${METADATA_CORE_INSTANCE_ID:-<unset>}"
if [[ -n "$SOURCE_INSTANCE_ROOT" && "$SOURCE_INSTANCE_ROOT" != "$INSTANCE_ROOT" ]]; then
  [[ "$METADATA_INSTANCE_ROOT" != "$SOURCE_INSTANCE_ROOT" ]] || die "active metadata leaked source instance root"
  [[ "$METADATA_BACKUPS_DIR" != "${SOURCE_INSTANCE_ROOT}/backups" ]] || die "active metadata leaked source backups dir"
fi
if [[ -n "$SOURCE_WORKSPACE_ROOT" && "$SOURCE_WORKSPACE_ROOT" != "$EXPECTED_TARGET_WORKSPACE_ROOT" ]]; then
  [[ "$TARGET_WORKSPACE_ROOT" != "$SOURCE_WORKSPACE_ROOT" ]] || die "active env leaked source workspace root"
  [[ "$METADATA_WORKSPACE_ROOT" != "$SOURCE_WORKSPACE_ROOT" ]] || die "active metadata leaked source workspace root"
fi
if [[ "$TARGET_BLOB_STORAGE_MODE" == "local" && -n "$SOURCE_INSTANCE_ROOT" && "$SOURCE_INSTANCE_ROOT" != "$INSTANCE_ROOT" ]]; then
  [[ "$TARGET_EFFECTIVE_BLOB_LOCATION" != "$SOURCE_INSTANCE_ROOT" ]] || die "active env leaked source blob root"
fi
if [[ -n "$SOURCE_PUBLIC_ORIGIN" && "$SOURCE_PUBLIC_ORIGIN" != "$TARGET_WEB_UI_ORIGIN" ]]; then
  [[ "$TARGET_WEB_UI_ORIGIN" != "$SOURCE_PUBLIC_ORIGIN" ]] || die "active env leaked source public origin"
  [[ "$TARGET_WEBAUTHN_ORIGIN" != "$SOURCE_PUBLIC_ORIGIN" ]] || die "active env leaked source webauthn origin"
  [[ "$METADATA_PUBLIC_ORIGIN" != "$SOURCE_PUBLIC_ORIGIN" ]] || die "active metadata leaked source public origin"
fi
case "$TARGET_BLOB_BACKEND" in
  filesystem|object)
    [[ "$EXPECTED_BLOB_STORAGE_MODE" == "local" ]] || die "manifest blob storage mode mismatch for local backend: ${EXPECTED_BLOB_STORAGE_MODE}"
    ;;
  s3)
    [[ "$TARGET_BLOB_S3_BUCKET" == "$EXPECTED_BLOB_S3_BUCKET" ]] || die "blob bucket mismatch: manifest=${EXPECTED_BLOB_S3_BUCKET} env=${TARGET_BLOB_S3_BUCKET:-<unset>}"
    [[ "$TARGET_BLOB_S3_PREFIX" == "$EXPECTED_BLOB_S3_PREFIX" ]] || die "blob prefix mismatch: manifest=${EXPECTED_BLOB_S3_PREFIX:-<unset>} env=${TARGET_BLOB_S3_PREFIX:-<unset>}"
    [[ "$TARGET_BLOB_S3_REGION" == "$EXPECTED_BLOB_S3_REGION" ]] || die "blob region mismatch: manifest=${EXPECTED_BLOB_S3_REGION} env=${TARGET_BLOB_S3_REGION:-<unset>}"
    [[ "$TARGET_BLOB_S3_ENDPOINT" == "$EXPECTED_BLOB_S3_ENDPOINT" ]] || die "blob endpoint mismatch: manifest=${EXPECTED_BLOB_S3_ENDPOINT:-<unset>} env=${TARGET_BLOB_S3_ENDPOINT:-<unset>}"
    [[ "$TARGET_BLOB_S3_FORCE_PATH_STYLE" == "$EXPECTED_BLOB_S3_FORCE_PATH_STYLE" ]] || die "blob force-path-style mismatch: manifest=${EXPECTED_BLOB_S3_FORCE_PATH_STYLE} env=${TARGET_BLOB_S3_FORCE_PATH_STYLE}"
    [[ "$METADATA_BLOB_S3_BUCKET" == "$TARGET_BLOB_S3_BUCKET" ]] || die "active metadata/blob bucket mismatch: env=${TARGET_BLOB_S3_BUCKET:-<unset>} metadata=${METADATA_BLOB_S3_BUCKET:-<unset>}"
    [[ "$METADATA_BLOB_S3_PREFIX" == "$TARGET_BLOB_S3_PREFIX" ]] || die "active metadata/blob prefix mismatch: env=${TARGET_BLOB_S3_PREFIX:-<unset>} metadata=${METADATA_BLOB_S3_PREFIX:-<unset>}"
    [[ "$METADATA_BLOB_S3_REGION" == "$TARGET_BLOB_S3_REGION" ]] || die "active metadata/blob region mismatch: env=${TARGET_BLOB_S3_REGION:-<unset>} metadata=${METADATA_BLOB_S3_REGION:-<unset>}"
    [[ "$METADATA_BLOB_S3_ENDPOINT" == "$TARGET_BLOB_S3_ENDPOINT" ]] || die "active metadata/blob endpoint mismatch: env=${TARGET_BLOB_S3_ENDPOINT:-<unset>} metadata=${METADATA_BLOB_S3_ENDPOINT:-<unset>}"
    [[ "$METADATA_BLOB_S3_FORCE_PATH_STYLE" == "$TARGET_BLOB_S3_FORCE_PATH_STYLE" ]] || die "active metadata/blob force-path-style mismatch: env=${TARGET_BLOB_S3_FORCE_PATH_STYLE} metadata=${METADATA_BLOB_S3_FORCE_PATH_STYLE}"
    ;;
esac
if [[ -n "$EXPECTED_ACTIVE_BOOTSTRAP_STATE" ]]; then
  [[ "$ACTUAL_BOOTSTRAP_STATE" == "$EXPECTED_ACTIVE_BOOTSTRAP_STATE" ]] || die "bootstrap state mismatch: expected ${EXPECTED_ACTIVE_BOOTSTRAP_STATE}, got ${ACTUAL_BOOTSTRAP_STATE}"
elif [[ -n "$EXPECTED_BOOTSTRAP_STATE" ]]; then
  [[ "$ACTUAL_BOOTSTRAP_STATE" == "$EXPECTED_BOOTSTRAP_STATE" ]] || die "bootstrap state mismatch: expected ${EXPECTED_BOOTSTRAP_STATE}, got ${ACTUAL_BOOTSTRAP_STATE}"
fi
if [[ -f "$RECEIPT_PATH" ]]; then
  RECEIPT_CORE_INSTANCE_ID="$(dotenv_get "$RECEIPT_PATH" TARGET_CORE_INSTANCE_ID || true)"
  if [[ -n "$RECEIPT_CORE_INSTANCE_ID" && "$RECEIPT_CORE_INSTANCE_ID" != "$TARGET_CORE_INSTANCE_ID" ]]; then
    die "core instance id mismatch: receipt=${RECEIPT_CORE_INSTANCE_ID} env=${TARGET_CORE_INSTANCE_ID}"
  fi
elif [[ -n "$EXPECTED_CORE_INSTANCE_ID" && -n "$TARGET_CORE_INSTANCE_ID" && "$EXPECTED_CORE_INSTANCE_ID" != "$TARGET_CORE_INSTANCE_ID" ]]; then
  die "core instance id mismatch: manifest=${EXPECTED_CORE_INSTANCE_ID} env=${TARGET_CORE_INSTANCE_ID}"
fi

if [[ -z "$VERIFY_ARTIFACT_ID" ]]; then
  VERIFY_ARTIFACT_ID="$(sqlite_scalar "${WORKSPACE_ROOT}/state.sqlite" "SELECT COALESCE(id, '') FROM artifacts ORDER BY created_at ASC, id ASC LIMIT 1;")"
fi
if [[ -n "$VERIFY_ARTIFACT_ID" ]]; then
  verify_live_artifact_read "$VERIFY_ARTIFACT_ID"
else
  log "artifact live read:      skipped (no artifacts in restored workspace)"
fi

if [[ "$ACTUAL_DOCUMENT_COUNT" -gt 0 ]]; then
  if [[ -z "$VERIFY_DOCUMENT_ID" || -z "$VERIFY_DOCUMENT_REVISION_ID" ]]; then
    VERIFY_DOCUMENT_ID="$(sqlite_scalar "${WORKSPACE_ROOT}/state.sqlite" "SELECT COALESCE(id, '') FROM documents ORDER BY created_at ASC, id ASC LIMIT 1;")"
    VERIFY_DOCUMENT_REVISION_ID="$(sqlite_scalar "${WORKSPACE_ROOT}/state.sqlite" "SELECT COALESCE(head_revision_id, '') FROM documents ORDER BY created_at ASC, id ASC LIMIT 1;")"
  fi
  if [[ -n "$VERIFY_DOCUMENT_ID" && -n "$VERIFY_DOCUMENT_REVISION_ID" ]]; then
    verify_live_document_revision_read "$VERIFY_DOCUMENT_ID" "$VERIFY_DOCUMENT_REVISION_ID"
  else
    log "document live read:      skipped (no documents in restored workspace)"
  fi
fi

verify_referenced_blob_reachability

log "Restore verification succeeded for ${INSTANCE_ROOT}"
log "  /readyz:                 ok"
log "  artifact count:          ${ACTUAL_ARTIFACT_COUNT}"
log "  agent count:             ${ACTUAL_AGENT_COUNT}"
log "  invite count:            ${ACTUAL_INVITE_COUNT}"
log "  document count:          ${ACTUAL_DOCUMENT_COUNT}"
log "  document revision count: ${ACTUAL_DOCUMENT_REVISION_COUNT}"
log "  blob backend:            ${TARGET_BLOB_BACKEND}"
log "  blob location:           ${TARGET_EFFECTIVE_BLOB_LOCATION}"
if [[ -n "$RECEIPT_TARGET_BLOB_RESTORE_ACTION" ]]; then
  log "  blob restore action:     ${RECEIPT_TARGET_BLOB_RESTORE_ACTION}"
fi
if [[ "$TARGET_BLOB_STORAGE_MODE" == "local" ]]; then
  log "  blob file count:         ${ACTUAL_BLOB_FILE_COUNT}"
else
  log "  blob file count:         n/a (${TARGET_BLOB_STORAGE_MODE})"
fi
if [[ -n "$VERIFY_ARTIFACT_ID" ]]; then
  log "  artifact live read:      ${VERIFY_ARTIFACT_ID}"
else
  log "  artifact live read:      skipped"
fi
if [[ "$ACTUAL_DOCUMENT_COUNT" -gt 0 ]]; then
  log "  document live read:      ${VERIFY_DOCUMENT_ID}@${VERIFY_DOCUMENT_REVISION_ID}"
else
  log "  document live read:      skipped"
fi
if [[ -n "$RESTORE_BOOTSTRAP_TOKEN_MODE" ]]; then
  log "  bootstrap mode:          ${RESTORE_BOOTSTRAP_TOKEN_MODE}"
fi
log "  bootstrap state:         ${ACTUAL_BOOTSTRAP_STATE}"
