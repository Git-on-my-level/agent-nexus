#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/common.sh"

usage() {
  cat <<'EOF'
Usage: scripts/hosted/restore-workspace.sh --backup-dir DIR --target-instance-root DIR [--force]

Restore a hosted-v1 backup into a target deployment root.
By default the target root must be empty.
EOF
}

BACKUP_DIR=""
TARGET_INSTANCE_ROOT=""
FORCE=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --backup-dir) BACKUP_DIR="$2"; shift 2 ;;
    --target-instance-root) TARGET_INSTANCE_ROOT="$2"; shift 2 ;;
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

BACKUP_DIR="$(cd "$BACKUP_DIR" && pwd -P)"
[[ -f "${BACKUP_DIR}/manifest.env" ]] || die "backup manifest not found: ${BACKUP_DIR}/manifest.env"
[[ -f "${BACKUP_DIR}/workspace/state.sqlite" ]] || die "backup sqlite file not found: ${BACKUP_DIR}/workspace/state.sqlite"

ensure_empty_or_forced_target "$TARGET_INSTANCE_ROOT" "$FORCE"
TARGET_INSTANCE_ROOT="$(cd "$TARGET_INSTANCE_ROOT" && pwd -P)"
TARGET_WORKSPACE_ROOT="${TARGET_INSTANCE_ROOT}/workspace"
TARGET_CONFIG_DIR="${TARGET_INSTANCE_ROOT}/config"
TARGET_METADATA_DIR="${TARGET_INSTANCE_ROOT}/metadata"
TARGET_BACKUPS_DIR="${TARGET_INSTANCE_ROOT}/backups"

mkdir -p \
  "${TARGET_WORKSPACE_ROOT}/artifacts/content" \
  "${TARGET_WORKSPACE_ROOT}/logs" \
  "${TARGET_WORKSPACE_ROOT}/tmp" \
  "$TARGET_CONFIG_DIR" \
  "$TARGET_METADATA_DIR" \
  "$TARGET_BACKUPS_DIR"

cp "${BACKUP_DIR}/workspace/state.sqlite" "${TARGET_WORKSPACE_ROOT}/state.sqlite"
copy_tree_contents "${BACKUP_DIR}/workspace/artifacts/content" "${TARGET_WORKSPACE_ROOT}/artifacts/content"
copy_tree_contents "${BACKUP_DIR}/config" "$TARGET_CONFIG_DIR"
copy_tree_contents "${BACKUP_DIR}/metadata" "$TARGET_METADATA_DIR"
cp "${BACKUP_DIR}/manifest.env" "${TARGET_METADATA_DIR}/restore-source-manifest.env"

cat >"${TARGET_METADATA_DIR}/restore-receipt.env" <<EOF
RESTORED_AT=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
SOURCE_BACKUP_DIR=${BACKUP_DIR}
TARGET_INSTANCE_ROOT=${TARGET_INSTANCE_ROOT}
FORCE_MODE=$([[ "$FORCE" -eq 1 ]] && printf 'true' || printf 'false')
EOF

log "Restore complete:"
log "  target root: ${TARGET_INSTANCE_ROOT}"
log "  workspace:   ${TARGET_WORKSPACE_ROOT}"
log "  manifest:    ${TARGET_METADATA_DIR}/restore-source-manifest.env"
