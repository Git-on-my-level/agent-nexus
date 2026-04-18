#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/common.sh"

usage() {
  cat <<'EOF'
Usage: scripts/hosted/backup-workspace.sh --instance-root DIR [--output-dir DIR] [--include-config-secrets]

Create a portable hosted-v1 backup bundle containing:
  - manifest.env
  - SHA256SUMS
  - workspace/state.sqlite
  - workspace/blob-store/ for local blob backends
  - remote blob reference metadata for S3-backed workspaces
  - metadata/ (if present)

By default, config/env.production is NOT included in the backup bundle for
security. Use --include-config-secrets to include it when you need a
self-contained bundle with deployment secrets.

Options:
  --include-config-secrets  Include config/env.production in the backup bundle
                            (WARNING: bundle will contain secrets)
EOF
}

INSTANCE_ROOT=""
OUTPUT_DIR=""
INCLUDE_CONFIG_SECRETS=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --instance-root) INSTANCE_ROOT="$2"; shift 2 ;;
    --output-dir) OUTPUT_DIR="$2"; shift 2 ;;
    --include-config-secrets) INCLUDE_CONFIG_SECRETS=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *)
      usage >&2
      die "unknown option: $1"
      ;;
  esac
done

[[ -n "$INSTANCE_ROOT" ]] || die "--instance-root is required"
require_command sqlite3 find cp sort awk date

INSTANCE_ROOT="$(cd "$INSTANCE_ROOT" && pwd -P)"
WORKSPACE_ROOT="${INSTANCE_ROOT}/workspace"
CONFIG_DIR="${INSTANCE_ROOT}/config"
METADATA_DIR="${INSTANCE_ROOT}/metadata"
ENV_FILE="${CONFIG_DIR}/env.production"
INSTANCE_METADATA_FILE="${METADATA_DIR}/instance.env"
DB_PATH="${WORKSPACE_ROOT}/state.sqlite"

[[ -d "$WORKSPACE_ROOT" ]] || die "workspace directory not found: $WORKSPACE_ROOT"
[[ -f "$DB_PATH" ]] || die "workspace database not found: $DB_PATH (start anx-core once before backing up)"

ACTIVE_BLOB_BACKEND="$(dotenv_get "$ENV_FILE" ANX_BLOB_BACKEND || true)"
ACTIVE_BLOB_ROOT="$(dotenv_get "$ENV_FILE" ANX_BLOB_ROOT || true)"
ACTIVE_BLOB_S3_BUCKET="$(dotenv_get "$ENV_FILE" ANX_BLOB_S3_BUCKET || true)"
ACTIVE_BLOB_S3_PREFIX="$(dotenv_get "$ENV_FILE" ANX_BLOB_S3_PREFIX || true)"
ACTIVE_BLOB_S3_REGION="$(dotenv_get "$ENV_FILE" ANX_BLOB_S3_REGION || true)"
ACTIVE_BLOB_S3_ENDPOINT="$(dotenv_get "$ENV_FILE" ANX_BLOB_S3_ENDPOINT || true)"
ACTIVE_BLOB_S3_ACCESS_KEY_ID="$(dotenv_get "$ENV_FILE" ANX_BLOB_S3_ACCESS_KEY_ID || true)"
ACTIVE_BLOB_S3_SECRET_ACCESS_KEY="$(dotenv_get "$ENV_FILE" ANX_BLOB_S3_SECRET_ACCESS_KEY || true)"
ACTIVE_BLOB_S3_SESSION_TOKEN="$(dotenv_get "$ENV_FILE" ANX_BLOB_S3_SESSION_TOKEN || true)"
ACTIVE_BLOB_S3_FORCE_PATH_STYLE="$(normalize_bool_value "$(dotenv_get "$ENV_FILE" ANX_BLOB_S3_FORCE_PATH_STYLE || true)")"
[[ -n "$ACTIVE_BLOB_BACKEND" ]] || ACTIVE_BLOB_BACKEND="filesystem"
validate_blob_backend "$ACTIVE_BLOB_BACKEND"
ACTIVE_LOCAL_BLOB_ROOT="$(blob_effective_local_root "$WORKSPACE_ROOT" "$ACTIVE_BLOB_BACKEND" "$ACTIVE_BLOB_ROOT")"
ACTIVE_BLOB_STORAGE_MODE="$(blob_storage_mode "$ACTIVE_BLOB_BACKEND")"
ACTIVE_BLOB_BACKUP_MODE="$(blob_backup_mode "$ACTIVE_BLOB_BACKEND")"
BACKUP_BLOB_RELATIVE_PATH="$(blob_bundle_path "$ACTIVE_BLOB_BACKEND")"

if [[ -z "$OUTPUT_DIR" ]]; then
  local_name="$(basename "$INSTANCE_ROOT")"
  OUTPUT_DIR="${INSTANCE_ROOT}/backups/${local_name}-$(date -u +"%Y%m%dT%H%M%SZ")"
fi

mkdir -p "$OUTPUT_DIR"
OUTPUT_DIR="$(cd "$OUTPUT_DIR" && pwd -P)"
ensure_empty_or_forced_target "$OUTPUT_DIR" 0

BACKUP_DB_DIR="${OUTPUT_DIR}/workspace"
BACKUP_CONFIG_DIR="${OUTPUT_DIR}/config"
BACKUP_METADATA_DIR="${OUTPUT_DIR}/metadata"
MANIFEST_FILE="${OUTPUT_DIR}/manifest.env"
CHECKSUM_FILE="${OUTPUT_DIR}/SHA256SUMS"
BACKUP_BLOB_DIR=""
if [[ -n "$BACKUP_BLOB_RELATIVE_PATH" ]]; then
  BACKUP_BLOB_DIR="${OUTPUT_DIR}/${BACKUP_BLOB_RELATIVE_PATH}"
fi

mkdir -p "$BACKUP_DB_DIR" "$BACKUP_CONFIG_DIR" "$BACKUP_METADATA_DIR"
if [[ -n "$BACKUP_BLOB_DIR" ]]; then
  mkdir -p "$BACKUP_BLOB_DIR"
fi

sqlite3 "$DB_PATH" ".timeout 5000" ".backup '${BACKUP_DB_DIR}/state.sqlite'"
if [[ "$ACTIVE_BLOB_STORAGE_MODE" == "local" ]]; then
  copy_tree_contents "$ACTIVE_LOCAL_BLOB_ROOT" "$BACKUP_BLOB_DIR"
fi
if [[ "$INCLUDE_CONFIG_SECRETS" -eq 1 && ! -f "$ENV_FILE" ]]; then
  die "--include-config-secrets requires ${ENV_FILE} to exist"
fi
if [[ "$INCLUDE_CONFIG_SECRETS" -eq 1 ]]; then
  warn "--include-config-secrets specified: backup bundle will contain config/env.production with deployment secrets"
  cp "$ENV_FILE" "${BACKUP_CONFIG_DIR}/env.production"
  chmod 600 "${BACKUP_CONFIG_DIR}/env.production"
fi
copy_tree_contents "$METADATA_DIR" "$BACKUP_METADATA_DIR"

INSTANCE_NAME="$(dotenv_get "$INSTANCE_METADATA_FILE" INSTANCE_NAME || true)"
INSTANCE_FORMAT_VERSION="$(dotenv_get "$INSTANCE_METADATA_FILE" FORMAT_VERSION || true)"
PUBLIC_ORIGIN="$(dotenv_get "$INSTANCE_METADATA_FILE" PUBLIC_ORIGIN || true)"
CORE_INSTANCE_ID="$(dotenv_get "$ENV_FILE" ANX_CORE_INSTANCE_ID || true)"
BOOTSTRAP_TOKEN="$(dotenv_get "$ENV_FILE" ANX_BOOTSTRAP_TOKEN || true)"
WORKSPACE_MAX_BLOB_BYTES="$(dotenv_get "$ENV_FILE" ANX_WORKSPACE_MAX_BLOB_BYTES || true)"
WORKSPACE_MAX_ARTIFACTS="$(dotenv_get "$ENV_FILE" ANX_WORKSPACE_MAX_ARTIFACTS || true)"
WORKSPACE_MAX_DOCUMENTS="$(dotenv_get "$ENV_FILE" ANX_WORKSPACE_MAX_DOCUMENTS || true)"
WORKSPACE_MAX_DOCUMENT_REVISIONS="$(dotenv_get "$ENV_FILE" ANX_WORKSPACE_MAX_DOCUMENT_REVISIONS || true)"
WORKSPACE_MAX_UPLOAD_BYTES="$(dotenv_get "$ENV_FILE" ANX_WORKSPACE_MAX_UPLOAD_BYTES || true)"
BOOTSTRAP_STATE="disabled"
if [[ -n "$BOOTSTRAP_TOKEN" && "$BOOTSTRAP_TOKEN" != "$HOSTED_BOOTSTRAP_PLACEHOLDER" ]]; then
  BOOTSTRAP_STATE="available"
  consumed_state="$(sqlite_scalar "$DB_PATH" "SELECT COALESCE(consumed_at, '') FROM auth_bootstrap_state WHERE id = 1;")"
  if [[ -n "$consumed_state" ]]; then
    BOOTSTRAP_STATE="consumed"
  fi
fi

ARTIFACT_COUNT="$(sqlite_scalar "$DB_PATH" "SELECT COUNT(*) FROM artifacts;")"
AGENT_COUNT="$(sqlite_scalar "$DB_PATH" "SELECT COUNT(*) FROM agents;")"
INVITE_COUNT="$(sqlite_scalar "$DB_PATH" "SELECT COUNT(*) FROM auth_invites;")"
DOCUMENT_COUNT="$(sqlite_scalar "$DB_PATH" "SELECT COUNT(*) FROM documents;")"
DOCUMENT_REVISION_COUNT="$(sqlite_scalar "$DB_PATH" "SELECT COUNT(*) FROM document_revisions;")"
BLOB_HASH_COUNT="$(sqlite_scalar "$DB_PATH" "SELECT COUNT(DISTINCT content_hash) FROM artifacts WHERE TRIM(content_hash) <> '';")"
if [[ "$ACTIVE_BLOB_STORAGE_MODE" == "local" ]]; then
  BLOB_FILE_COUNT="$(count_files "$ACTIVE_LOCAL_BLOB_ROOT")"
  BLOB_TOTAL_BYTES="$(directory_size_bytes "$ACTIVE_LOCAL_BLOB_ROOT")"
else
  BLOB_FILE_COUNT="0"
  BLOB_TOTAL_BYTES="0"
fi
SQLITE_BACKUP_SHA256="$(sha256_file "${BACKUP_DB_DIR}/state.sqlite")"
SQLITE_SCHEMA_VERSION="$(sqlite_scalar "$DB_PATH" "PRAGMA schema_version;")"
SQLITE_USER_VERSION="$(sqlite_scalar "$DB_PATH" "PRAGMA user_version;")"
VERIFY_ARTIFACT_ID="$(sqlite_scalar "$DB_PATH" "SELECT COALESCE(id, '') FROM artifacts ORDER BY created_at ASC, id ASC LIMIT 1;")"
VERIFY_DOCUMENT_ID="$(sqlite_scalar "$DB_PATH" "SELECT COALESCE(id, '') FROM documents ORDER BY created_at ASC, id ASC LIMIT 1;")"
VERIFY_DOCUMENT_REVISION_ID="$(sqlite_scalar "$DB_PATH" "SELECT COALESCE(head_revision_id, '') FROM documents ORDER BY created_at ASC, id ASC LIMIT 1;")"

if [[ "$INCLUDE_CONFIG_SECRETS" -eq 1 && -f "$ENV_FILE" ]]; then
  CONFIG_INCLUDED="true"
  CONFIG_ENV_PATH="config/env.production"
else
  CONFIG_INCLUDED="false"
  CONFIG_ENV_PATH=""
fi

cat >"$MANIFEST_FILE" <<EOF
FORMAT_VERSION=${HOSTED_BACKUP_FORMAT_VERSION}
CREATED_AT=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
INSTANCE_NAME=${INSTANCE_NAME}
INSTANCE_FORMAT_VERSION=${INSTANCE_FORMAT_VERSION}
SOURCE_INSTANCE_ROOT=${INSTANCE_ROOT}
SOURCE_WORKSPACE_ROOT=${WORKSPACE_ROOT}
PUBLIC_ORIGIN=${PUBLIC_ORIGIN}
CORE_INSTANCE_ID=${CORE_INSTANCE_ID}
$(emit_workspace_quota_metadata_lines "$WORKSPACE_MAX_BLOB_BYTES" "$WORKSPACE_MAX_ARTIFACTS" "$WORKSPACE_MAX_DOCUMENTS" "$WORKSPACE_MAX_DOCUMENT_REVISIONS" "$WORKSPACE_MAX_UPLOAD_BYTES")
SQLITE_BACKUP_PATH=workspace/state.sqlite
SQLITE_BACKUP_STRATEGY=sqlite3_dot_backup
SQLITE_BACKUP_SHA256=${SQLITE_BACKUP_SHA256}
SQLITE_SCHEMA_VERSION=${SQLITE_SCHEMA_VERSION}
SQLITE_USER_VERSION=${SQLITE_USER_VERSION}
$(emit_blob_metadata_lines "$WORKSPACE_ROOT" "$ACTIVE_BLOB_BACKEND" "$ACTIVE_BLOB_ROOT" "$ACTIVE_BLOB_S3_BUCKET" "$ACTIVE_BLOB_S3_PREFIX" "$ACTIVE_BLOB_S3_REGION" "$ACTIVE_BLOB_S3_ENDPOINT" "$ACTIVE_BLOB_S3_ACCESS_KEY_ID" "$ACTIVE_BLOB_S3_SECRET_ACCESS_KEY" "$ACTIVE_BLOB_S3_SESSION_TOKEN" "$ACTIVE_BLOB_S3_FORCE_PATH_STYLE")
BLOB_DIR_PATH=${BACKUP_BLOB_RELATIVE_PATH}
BLOB_BUNDLE_PATH=${BACKUP_BLOB_RELATIVE_PATH}
CONFIG_INCLUDED=${CONFIG_INCLUDED}
CONFIG_ENV_PATH=${CONFIG_ENV_PATH}
METADATA_DIR_PATH=metadata
ARTIFACT_COUNT=${ARTIFACT_COUNT}
AGENT_COUNT=${AGENT_COUNT}
INVITE_COUNT=${INVITE_COUNT}
DOCUMENT_COUNT=${DOCUMENT_COUNT}
DOCUMENT_REVISION_COUNT=${DOCUMENT_REVISION_COUNT}
BLOB_HASH_COUNT=${BLOB_HASH_COUNT}
BLOB_FILE_COUNT=${BLOB_FILE_COUNT}
BLOB_TOTAL_BYTES=${BLOB_TOTAL_BYTES}
BOOTSTRAP_STATE=${BOOTSTRAP_STATE}
VERIFY_ARTIFACT_ID=${VERIFY_ARTIFACT_ID}
VERIFY_DOCUMENT_ID=${VERIFY_DOCUMENT_ID}
VERIFY_DOCUMENT_REVISION_ID=${VERIFY_DOCUMENT_REVISION_ID}
CHECKSUM_FILE=SHA256SUMS
EOF

(
  cd "$OUTPUT_DIR"
  find . -type f ! -name 'SHA256SUMS' -print | LC_ALL=C sort | while read -r path; do
    path="${path#./}"
    printf '%s  %s\n' "$(sha256_file "$path")" "$path"
  done >"$CHECKSUM_FILE"
)

log "Backup bundle created at ${OUTPUT_DIR}"
log "  manifest: ${MANIFEST_FILE}"
log "  sqlite:   ${BACKUP_DB_DIR}/state.sqlite"
if [[ "$ACTIVE_BLOB_STORAGE_MODE" == "local" ]]; then
  log "  blobs:    ${BACKUP_BLOB_DIR} (copied from ${ACTIVE_LOCAL_BLOB_ROOT})"
else
  log "  blobs:    remote reference to $(blob_effective_location "$WORKSPACE_ROOT" "$ACTIVE_BLOB_BACKEND" "$ACTIVE_BLOB_ROOT" "$ACTIVE_BLOB_S3_BUCKET" "$ACTIVE_BLOB_S3_PREFIX")"
fi
