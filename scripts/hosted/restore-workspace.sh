#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/common.sh"

usage() {
  cat <<'EOF'
Usage: scripts/hosted/restore-workspace.sh --backup-dir DIR --target-instance-root DIR --instance NAME --public-origin ORIGIN --listen-port PORT --web-ui-port PORT [options]

Restore portable hosted workspace data into a target deployment root, then
regenerate target-local config/env.production and metadata/instance.env.

Required:
  --backup-dir DIR             Backup bundle to restore from
  --target-instance-root DIR   Deployment root to restore into
  --instance NAME              Target instance name
  --public-origin ORIGIN       Target public https://origin
  --listen-port PORT           Target core listen port
  --web-ui-port PORT           Target web-ui port

Optional:
  --listen-host HOST           Target local bind host hint (default: 127.0.0.1)
  --core-instance-id ID        Target core instance id (default: instance name)
  --bootstrap-token-mode MODE  placeholder|clear|keep-source|replace (default: placeholder)
  --bootstrap-token TOKEN      Required when --bootstrap-token-mode replace
  --force                      Allow overlay onto a non-empty target root
  -h, --help                   Show help
EOF
}

BACKUP_DIR=""
TARGET_INSTANCE_ROOT=""
INSTANCE_NAME=""
PUBLIC_ORIGIN=""
LISTEN_HOST="127.0.0.1"
LISTEN_PORT=""
WEB_UI_PORT=""
CORE_INSTANCE_ID=""
BOOTSTRAP_TOKEN_MODE="placeholder"
BOOTSTRAP_TOKEN=""
FORCE=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --backup-dir) BACKUP_DIR="$2"; shift 2 ;;
    --target-instance-root) TARGET_INSTANCE_ROOT="$2"; shift 2 ;;
    --instance) INSTANCE_NAME="$2"; shift 2 ;;
    --public-origin) PUBLIC_ORIGIN="$2"; shift 2 ;;
    --listen-host) LISTEN_HOST="$2"; shift 2 ;;
    --listen-port) LISTEN_PORT="$2"; shift 2 ;;
    --web-ui-port) WEB_UI_PORT="$2"; shift 2 ;;
    --core-instance-id) CORE_INSTANCE_ID="$2"; shift 2 ;;
    --bootstrap-token-mode) BOOTSTRAP_TOKEN_MODE="$2"; shift 2 ;;
    --bootstrap-token) BOOTSTRAP_TOKEN="$2"; shift 2 ;;
    --force) FORCE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *)
      usage >&2
      die "unknown option: $1"
      ;;
  esac
done

[[ -n "$BACKUP_DIR" ]] || die "--backup-dir is required"
[[ -n "$TARGET_INSTANCE_ROOT" ]] || die "--target-instance-root is required"
[[ -n "$INSTANCE_NAME" ]] || die "--instance is required"
[[ -n "$PUBLIC_ORIGIN" ]] || die "--public-origin is required"
[[ -n "$LISTEN_PORT" ]] || die "--listen-port is required"
[[ -n "$WEB_UI_PORT" ]] || die "--web-ui-port is required"
[[ -n "$CORE_INSTANCE_ID" ]] || CORE_INSTANCE_ID="$INSTANCE_NAME"
validate_instance_name "$INSTANCE_NAME"
validate_instance_name "$CORE_INSTANCE_ID"
validate_host "$LISTEN_HOST"
validate_port "$LISTEN_PORT"
validate_port "$WEB_UI_PORT"
validate_origin "$PUBLIC_ORIGIN"
validate_bootstrap_token_mode "$BOOTSTRAP_TOKEN_MODE"
if [[ "$BOOTSTRAP_TOKEN_MODE" == "replace" && -z "$BOOTSTRAP_TOKEN" ]]; then
  die "--bootstrap-token is required when --bootstrap-token-mode replace"
fi
if [[ "$BOOTSTRAP_TOKEN_MODE" != "replace" && -n "$BOOTSTRAP_TOKEN" ]]; then
  die "--bootstrap-token is only supported with --bootstrap-token-mode replace"
fi

BACKUP_DIR="$(cd "$BACKUP_DIR" && pwd -P)"
[[ -f "${BACKUP_DIR}/manifest.env" ]] || die "backup manifest not found: ${BACKUP_DIR}/manifest.env"
[[ -f "${BACKUP_DIR}/SHA256SUMS" ]] || die "backup checksum file not found: ${BACKUP_DIR}/SHA256SUMS"
[[ -f "${BACKUP_DIR}/workspace/state.sqlite" ]] || die "backup sqlite file not found: ${BACKUP_DIR}/workspace/state.sqlite"
SOURCE_MANIFEST="${BACKUP_DIR}/manifest.env"
SOURCE_ENV_FILE="${BACKUP_DIR}/config/env.production"
SOURCE_FORMAT_VERSION="$(manifest_get "$SOURCE_MANIFEST" FORMAT_VERSION || true)"
validate_backup_format_version "$SOURCE_FORMAT_VERSION"
verify_backup_checksums "$BACKUP_DIR"
SOURCE_BOOTSTRAP_STATE="$(manifest_get "$SOURCE_MANIFEST" BOOTSTRAP_STATE || true)"
SOURCE_CONFIG_INCLUDED="$(manifest_get "$SOURCE_MANIFEST" CONFIG_INCLUDED || true)"
SOURCE_BOOTSTRAP_TOKEN=""
if [[ "$SOURCE_CONFIG_INCLUDED" == "true" && -f "$SOURCE_ENV_FILE" ]]; then
  SOURCE_BOOTSTRAP_TOKEN="$(dotenv_get "$SOURCE_ENV_FILE" ANX_BOOTSTRAP_TOKEN || true)"
fi
SOURCE_BLOB_BACKEND="$(manifest_get "$SOURCE_MANIFEST" BLOB_BACKEND || true)"
SOURCE_BLOB_ROOT="$(manifest_get "$SOURCE_MANIFEST" BLOB_ROOT || true)"
SOURCE_BLOB_STORAGE_MODE="$(manifest_get "$SOURCE_MANIFEST" BLOB_STORAGE_MODE || true)"
SOURCE_BLOB_BACKUP_MODE="$(manifest_get "$SOURCE_MANIFEST" BLOB_BACKUP_MODE || true)"
SOURCE_BLOB_BUNDLE_PATH="$(manifest_get "$SOURCE_MANIFEST" BLOB_BUNDLE_PATH || true)"
SOURCE_BLOB_S3_BUCKET="$(manifest_get "$SOURCE_MANIFEST" BLOB_S3_BUCKET || true)"
SOURCE_BLOB_S3_PREFIX="$(manifest_get "$SOURCE_MANIFEST" BLOB_S3_PREFIX || true)"
SOURCE_BLOB_S3_REGION="$(manifest_get "$SOURCE_MANIFEST" BLOB_S3_REGION || true)"
SOURCE_BLOB_S3_ENDPOINT="$(manifest_get "$SOURCE_MANIFEST" BLOB_S3_ENDPOINT || true)"
SOURCE_BLOB_S3_FORCE_PATH_STYLE="$(normalize_bool_value "$(manifest_get "$SOURCE_MANIFEST" BLOB_S3_FORCE_PATH_STYLE || true)")"
SOURCE_WORKSPACE_MAX_BLOB_BYTES="$(manifest_get "$SOURCE_MANIFEST" WORKSPACE_MAX_BLOB_BYTES || true)"
SOURCE_WORKSPACE_MAX_ARTIFACTS="$(manifest_get "$SOURCE_MANIFEST" WORKSPACE_MAX_ARTIFACTS || true)"
SOURCE_WORKSPACE_MAX_DOCUMENTS="$(manifest_get "$SOURCE_MANIFEST" WORKSPACE_MAX_DOCUMENTS || true)"
SOURCE_WORKSPACE_MAX_DOCUMENT_REVISIONS="$(manifest_get "$SOURCE_MANIFEST" WORKSPACE_MAX_DOCUMENT_REVISIONS || true)"
SOURCE_WORKSPACE_MAX_UPLOAD_BYTES="$(manifest_get "$SOURCE_MANIFEST" WORKSPACE_MAX_UPLOAD_BYTES || true)"
[[ -n "$SOURCE_BLOB_BACKEND" ]] || SOURCE_BLOB_BACKEND="filesystem"
validate_blob_backend "$SOURCE_BLOB_BACKEND"
[[ -n "$SOURCE_BLOB_STORAGE_MODE" ]] || SOURCE_BLOB_STORAGE_MODE="$(blob_storage_mode "$SOURCE_BLOB_BACKEND")"
[[ -n "$SOURCE_BLOB_BACKUP_MODE" ]] || SOURCE_BLOB_BACKUP_MODE="$(blob_backup_mode "$SOURCE_BLOB_BACKEND")"
if [[ -z "$SOURCE_BLOB_BUNDLE_PATH" && "$SOURCE_BLOB_STORAGE_MODE" == "local" ]]; then
  SOURCE_BLOB_BUNDLE_PATH="workspace/artifacts/content"
fi
SOURCE_BLOB_S3_ACCESS_KEY_ID=""
SOURCE_BLOB_S3_SECRET_ACCESS_KEY=""
SOURCE_BLOB_S3_SESSION_TOKEN=""
if [[ "$SOURCE_CONFIG_INCLUDED" == "true" && -f "$SOURCE_ENV_FILE" ]]; then
  SOURCE_BLOB_S3_ACCESS_KEY_ID="$(dotenv_get "$SOURCE_ENV_FILE" ANX_BLOB_S3_ACCESS_KEY_ID || true)"
  SOURCE_BLOB_S3_SECRET_ACCESS_KEY="$(dotenv_get "$SOURCE_ENV_FILE" ANX_BLOB_S3_SECRET_ACCESS_KEY || true)"
  SOURCE_BLOB_S3_SESSION_TOKEN="$(dotenv_get "$SOURCE_ENV_FILE" ANX_BLOB_S3_SESSION_TOKEN || true)"
fi

if [[ -d "$TARGET_INSTANCE_ROOT" ]]; then
  TARGET_INSTANCE_ROOT="$(cd "$TARGET_INSTANCE_ROOT" && pwd -P)"
else
  parent_dir="$(dirname "$TARGET_INSTANCE_ROOT")"
  mkdir -p "$parent_dir"
  parent_dir="$(cd "$parent_dir" && pwd -P)"
  TARGET_INSTANCE_ROOT="${parent_dir}/$(basename "$TARGET_INSTANCE_ROOT")"
fi
ensure_empty_or_forced_target "$TARGET_INSTANCE_ROOT" "$FORCE"
TARGET_WORKSPACE_ROOT="${TARGET_INSTANCE_ROOT}/workspace"
TARGET_CONFIG_DIR="${TARGET_INSTANCE_ROOT}/config"
TARGET_METADATA_DIR="${TARGET_INSTANCE_ROOT}/metadata"
TARGET_BACKUPS_DIR="${TARGET_INSTANCE_ROOT}/backups"
TARGET_ENV_FILE="${TARGET_CONFIG_DIR}/env.production"
TARGET_INSTANCE_METADATA_FILE="${TARGET_METADATA_DIR}/instance.env"
SOURCE_INSTANCE_NAME="$(manifest_get "$SOURCE_MANIFEST" INSTANCE_NAME || true)"
SOURCE_INSTANCE_ROOT="$(manifest_get "$SOURCE_MANIFEST" SOURCE_INSTANCE_ROOT || true)"
SOURCE_WORKSPACE_ROOT="$(manifest_get "$SOURCE_MANIFEST" SOURCE_WORKSPACE_ROOT || true)"
SOURCE_PUBLIC_ORIGIN="$(manifest_get "$SOURCE_MANIFEST" PUBLIC_ORIGIN || true)"
TARGET_BLOB_ROOT=""
if [[ "$SOURCE_BLOB_STORAGE_MODE" == "local" ]]; then
  TARGET_BLOB_ROOT="$(remap_local_blob_root_for_target "$SOURCE_BLOB_ROOT" "$SOURCE_INSTANCE_ROOT" "$SOURCE_WORKSPACE_ROOT" "$TARGET_INSTANCE_ROOT" "${TARGET_INSTANCE_ROOT}/workspace")"
fi

mkdir -p \
  "${TARGET_WORKSPACE_ROOT}/logs" \
  "${TARGET_WORKSPACE_ROOT}/tmp" \
  "$TARGET_CONFIG_DIR" \
  "$TARGET_METADATA_DIR" \
  "$TARGET_BACKUPS_DIR"

provision_args=(
  --instance "$INSTANCE_NAME"
  --instance-root "$TARGET_INSTANCE_ROOT"
  --public-origin "$PUBLIC_ORIGIN"
  --listen-host "$LISTEN_HOST"
  --listen-port "$LISTEN_PORT"
  --web-ui-port "$WEB_UI_PORT"
  --core-instance-id "$CORE_INSTANCE_ID"
  --blob-backend "$SOURCE_BLOB_BACKEND"
  --force
)
if [[ -n "$SOURCE_WORKSPACE_MAX_BLOB_BYTES" ]]; then
  provision_args+=(--max-blob-bytes "$SOURCE_WORKSPACE_MAX_BLOB_BYTES")
fi
if [[ -n "$SOURCE_WORKSPACE_MAX_ARTIFACTS" ]]; then
  provision_args+=(--max-artifacts "$SOURCE_WORKSPACE_MAX_ARTIFACTS")
fi
if [[ -n "$SOURCE_WORKSPACE_MAX_DOCUMENTS" ]]; then
  provision_args+=(--max-documents "$SOURCE_WORKSPACE_MAX_DOCUMENTS")
fi
if [[ -n "$SOURCE_WORKSPACE_MAX_DOCUMENT_REVISIONS" ]]; then
  provision_args+=(--max-document-revisions "$SOURCE_WORKSPACE_MAX_DOCUMENT_REVISIONS")
fi
if [[ -n "$SOURCE_WORKSPACE_MAX_UPLOAD_BYTES" ]]; then
  provision_args+=(--max-upload-bytes "$SOURCE_WORKSPACE_MAX_UPLOAD_BYTES")
fi
if [[ -n "$TARGET_BLOB_ROOT" ]]; then
  provision_args+=(--blob-root "$TARGET_BLOB_ROOT")
fi
if [[ "$SOURCE_BLOB_BACKEND" == "s3" ]]; then
  provision_args+=(
    --blob-s3-bucket "$SOURCE_BLOB_S3_BUCKET"
    --blob-s3-prefix "$SOURCE_BLOB_S3_PREFIX"
    --blob-s3-region "$SOURCE_BLOB_S3_REGION"
    --blob-s3-endpoint "$SOURCE_BLOB_S3_ENDPOINT"
    --blob-s3-force-path-style "$SOURCE_BLOB_S3_FORCE_PATH_STYLE"
  )
  if [[ -n "$SOURCE_BLOB_S3_ACCESS_KEY_ID" ]]; then
    provision_args+=(--blob-s3-access-key-id "$SOURCE_BLOB_S3_ACCESS_KEY_ID")
  fi
  if [[ -n "$SOURCE_BLOB_S3_SECRET_ACCESS_KEY" ]]; then
    provision_args+=(--blob-s3-secret-access-key "$SOURCE_BLOB_S3_SECRET_ACCESS_KEY")
  fi
  if [[ -n "$SOURCE_BLOB_S3_SESSION_TOKEN" ]]; then
    provision_args+=(--blob-s3-session-token "$SOURCE_BLOB_S3_SESSION_TOKEN")
  fi
fi

case "$BOOTSTRAP_TOKEN_MODE" in
  placeholder)
    EXPECTED_ACTIVE_BOOTSTRAP_STATE="disabled"
    ;;
  clear)
    provision_args+=(--clear-bootstrap-token)
    EXPECTED_ACTIVE_BOOTSTRAP_STATE="disabled"
    ;;
  keep-source)
    if [[ -n "$SOURCE_BOOTSTRAP_TOKEN" ]]; then
      provision_args+=(--bootstrap-token "$SOURCE_BOOTSTRAP_TOKEN")
    elif [[ "${SOURCE_BOOTSTRAP_STATE:-disabled}" == "disabled" ]]; then
      provision_args+=(--clear-bootstrap-token)
    elif [[ "$SOURCE_CONFIG_INCLUDED" != "true" ]]; then
      die "backup bundle does not include config secrets; cannot use keep-source mode (use placeholder or clear instead)"
    else
      die "backup bundle does not include a reusable source bootstrap token"
    fi
    EXPECTED_ACTIVE_BOOTSTRAP_STATE="${SOURCE_BOOTSTRAP_STATE:-disabled}"
    ;;
  replace)
    provision_args+=(--bootstrap-token "$BOOTSTRAP_TOKEN")
    if [[ "${SOURCE_BOOTSTRAP_STATE:-}" == "consumed" ]]; then
      EXPECTED_ACTIVE_BOOTSTRAP_STATE="consumed"
    else
      EXPECTED_ACTIVE_BOOTSTRAP_STATE="available"
    fi
    ;;
esac

"${SCRIPT_DIR}/provision-workspace.sh" "${provision_args[@]}"

[[ -f "$TARGET_ENV_FILE" ]] || die "provisioning did not produce ${TARGET_ENV_FILE}"
[[ -f "$TARGET_INSTANCE_METADATA_FILE" ]] || die "provisioning did not produce ${TARGET_INSTANCE_METADATA_FILE}"

cp "${BACKUP_DIR}/workspace/state.sqlite" "${TARGET_WORKSPACE_ROOT}/state.sqlite"

TARGET_EFFECTIVE_BLOB_LOCATION="$(blob_effective_location "$TARGET_WORKSPACE_ROOT" "$SOURCE_BLOB_BACKEND" "$TARGET_BLOB_ROOT" "$SOURCE_BLOB_S3_BUCKET" "$SOURCE_BLOB_S3_PREFIX")"
TARGET_EFFECTIVE_LOCAL_BLOB_ROOT="$(blob_effective_local_root "$TARGET_WORKSPACE_ROOT" "$SOURCE_BLOB_BACKEND" "$TARGET_BLOB_ROOT")"
TARGET_BLOB_RESTORE_ACTION="reference-remote-blob-store"
if [[ "$SOURCE_BLOB_STORAGE_MODE" == "local" ]]; then
  TARGET_BLOB_RESTORE_ACTION="copied-local-blob-store"
  rm -rf "$TARGET_EFFECTIVE_LOCAL_BLOB_ROOT"
  mkdir -p "$TARGET_EFFECTIVE_LOCAL_BLOB_ROOT"
  copy_tree_contents "${BACKUP_DIR}/${SOURCE_BLOB_BUNDLE_PATH}" "$TARGET_EFFECTIVE_LOCAL_BLOB_ROOT"
elif [[ "$SOURCE_BLOB_BACKEND" == "s3" && "$SOURCE_CONFIG_INCLUDED" != "true" ]]; then
  warn "restored S3 backend config without bundled inline credentials; verification/startup relies on ambient AWS credentials or instance identity"
fi

cp "$SOURCE_MANIFEST" "${TARGET_METADATA_DIR}/restore-source-manifest.env"

cat >"${TARGET_METADATA_DIR}/restore-receipt.env" <<EOF
RESTORED_AT=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
SOURCE_BACKUP_DIR=${BACKUP_DIR}
SOURCE_INSTANCE_NAME=${SOURCE_INSTANCE_NAME}
SOURCE_INSTANCE_ROOT=${SOURCE_INSTANCE_ROOT}
SOURCE_WORKSPACE_ROOT=${SOURCE_WORKSPACE_ROOT}
SOURCE_PUBLIC_ORIGIN=${SOURCE_PUBLIC_ORIGIN}
TARGET_INSTANCE_NAME=${INSTANCE_NAME}
TARGET_INSTANCE_ROOT=${TARGET_INSTANCE_ROOT}
TARGET_WORKSPACE_ROOT=${TARGET_WORKSPACE_ROOT}
TARGET_PUBLIC_ORIGIN=${PUBLIC_ORIGIN}
TARGET_LISTEN_HOST=${LISTEN_HOST}
TARGET_LISTEN_PORT=${LISTEN_PORT}
TARGET_WEB_UI_PORT=${WEB_UI_PORT}
TARGET_CORE_INSTANCE_ID=${CORE_INSTANCE_ID}
TARGET_BLOB_BACKEND=${SOURCE_BLOB_BACKEND}
TARGET_BLOB_STORAGE_MODE=${SOURCE_BLOB_STORAGE_MODE}
TARGET_BLOB_ROOT=${TARGET_BLOB_ROOT}
TARGET_BLOB_EFFECTIVE_LOCATION=${TARGET_EFFECTIVE_BLOB_LOCATION}
TARGET_BLOB_BUNDLE_PATH=${SOURCE_BLOB_BUNDLE_PATH}
TARGET_BLOB_RESTORE_ACTION=${TARGET_BLOB_RESTORE_ACTION}
TARGET_BLOB_S3_BUCKET=${SOURCE_BLOB_S3_BUCKET}
TARGET_BLOB_S3_PREFIX=${SOURCE_BLOB_S3_PREFIX}
TARGET_BLOB_S3_REGION=${SOURCE_BLOB_S3_REGION}
TARGET_BLOB_S3_ENDPOINT=${SOURCE_BLOB_S3_ENDPOINT}
TARGET_BLOB_S3_FORCE_PATH_STYLE=${SOURCE_BLOB_S3_FORCE_PATH_STYLE}
TARGET_BLOB_S3_INLINE_CREDENTIALS_PRESENT=$(blob_s3_inline_credentials_present "$SOURCE_BLOB_S3_ACCESS_KEY_ID" "$SOURCE_BLOB_S3_SECRET_ACCESS_KEY" "$SOURCE_BLOB_S3_SESSION_TOKEN")
BOOTSTRAP_TOKEN_MODE=${BOOTSTRAP_TOKEN_MODE}
EXPECTED_ACTIVE_BOOTSTRAP_STATE=${EXPECTED_ACTIVE_BOOTSTRAP_STATE}
FORCE_MODE=$([[ "$FORCE" -eq 1 ]] && printf 'true' || printf 'false')
EOF
chmod 644 "${TARGET_METADATA_DIR}/restore-receipt.env"

log "Restore complete:"
log "  target root: ${TARGET_INSTANCE_ROOT}"
log "  workspace:   ${TARGET_WORKSPACE_ROOT}"
log "  env file:    ${TARGET_ENV_FILE}"
log "  metadata:    ${TARGET_INSTANCE_METADATA_FILE}"
log "  blob:        ${TARGET_EFFECTIVE_BLOB_LOCATION} (${SOURCE_BLOB_BACKEND})"
log "  manifest:    ${TARGET_METADATA_DIR}/restore-source-manifest.env"
log "  receipt:     ${TARGET_METADATA_DIR}/restore-receipt.env"
