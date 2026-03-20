#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/common.sh"

usage() {
  cat <<'EOF'
Usage: scripts/hosted/provision-workspace.sh [options]

Provision one managed hosted-v1 deployment root with:
  - workspace/          durable SQLite + blob state
  - config/env.production
  - metadata/instance.env
  - backups/            default operator backup destination

Required:
  --instance NAME           Stable instance identifier
  --instance-root DIR       Deployment root to create/update
  --public-origin ORIGIN    Public https://origin for the workspace

Optional:
  --listen-host HOST        Local bind host hint (default: 127.0.0.1)
  --listen-port PORT        Local/core host port hint (default: 8000)
  --web-ui-port PORT        Host port hint for web-ui example flows (default: 3000)
  --core-instance-id ID     Runtime core instance id (default: instance name)
  --bootstrap-token TOKEN   Write the provided bootstrap token into env.production
  --clear-bootstrap-token   Write an empty bootstrap token into env.production
  --generate-bootstrap-token
                            Generate and write a fresh bootstrap token
  --force                   Rewrite config/env.production and metadata/instance.env
  -h, --help                Show help
EOF
}

INSTANCE_NAME=""
INSTANCE_ROOT=""
PUBLIC_ORIGIN=""
LISTEN_HOST="127.0.0.1"
LISTEN_PORT="8000"
WEB_UI_PORT="3000"
CORE_INSTANCE_ID=""
BOOTSTRAP_TOKEN=""
CLEAR_BOOTSTRAP_TOKEN=0
GENERATE_BOOTSTRAP_TOKEN=0
FORCE=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --instance) INSTANCE_NAME="$2"; shift 2 ;;
    --instance-root) INSTANCE_ROOT="$2"; shift 2 ;;
    --public-origin) PUBLIC_ORIGIN="$2"; shift 2 ;;
    --listen-host) LISTEN_HOST="$2"; shift 2 ;;
    --listen-port) LISTEN_PORT="$2"; shift 2 ;;
    --web-ui-port) WEB_UI_PORT="$2"; shift 2 ;;
    --core-instance-id) CORE_INSTANCE_ID="$2"; shift 2 ;;
    --bootstrap-token) BOOTSTRAP_TOKEN="$2"; shift 2 ;;
    --clear-bootstrap-token) CLEAR_BOOTSTRAP_TOKEN=1; shift ;;
    --generate-bootstrap-token) GENERATE_BOOTSTRAP_TOKEN=1; shift ;;
    --force) FORCE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *)
      usage >&2
      die "unknown option: $1"
      ;;
  esac
done

[[ -n "$INSTANCE_NAME" ]] || die "--instance is required"
[[ -n "$INSTANCE_ROOT" ]] || die "--instance-root is required"
[[ -n "$PUBLIC_ORIGIN" ]] || die "--public-origin is required"
[[ -n "$CORE_INSTANCE_ID" ]] || CORE_INSTANCE_ID="$INSTANCE_NAME"
(( GENERATE_BOOTSTRAP_TOKEN == 0 )) || [[ -z "$BOOTSTRAP_TOKEN" ]] || die "use either --bootstrap-token or --generate-bootstrap-token"
(( CLEAR_BOOTSTRAP_TOKEN == 0 )) || [[ -z "$BOOTSTRAP_TOKEN" ]] || die "use either --bootstrap-token or --clear-bootstrap-token"
(( CLEAR_BOOTSTRAP_TOKEN == 0 )) || (( GENERATE_BOOTSTRAP_TOKEN == 0 )) || die "use either --clear-bootstrap-token or --generate-bootstrap-token"

validate_instance_name "$INSTANCE_NAME"
validate_instance_name "$CORE_INSTANCE_ID"
validate_host "$LISTEN_HOST"
validate_port "$LISTEN_PORT"
validate_port "$WEB_UI_PORT"
validate_origin "$PUBLIC_ORIGIN"

mkdir -p "$INSTANCE_ROOT"
INSTANCE_ROOT="$(cd "$INSTANCE_ROOT" && pwd -P)"
WORKSPACE_ROOT="${INSTANCE_ROOT}/workspace"
CONFIG_DIR="${INSTANCE_ROOT}/config"
METADATA_DIR="${INSTANCE_ROOT}/metadata"
BACKUPS_DIR="${INSTANCE_ROOT}/backups"
ENV_FILE="${CONFIG_DIR}/env.production"
INSTANCE_METADATA_FILE="${METADATA_DIR}/instance.env"
WEBAUTHN_RPID="$(origin_host "$PUBLIC_ORIGIN")"

if [[ "$GENERATE_BOOTSTRAP_TOKEN" -eq 1 ]]; then
  BOOTSTRAP_TOKEN="$(generate_token 24)"
fi
if [[ "$CLEAR_BOOTSTRAP_TOKEN" -eq 1 ]]; then
  BOOTSTRAP_TOKEN=""
elif [[ -z "$BOOTSTRAP_TOKEN" ]]; then
  BOOTSTRAP_TOKEN="$HOSTED_BOOTSTRAP_PLACEHOLDER"
fi

mkdir -p \
  "$WORKSPACE_ROOT/artifacts/content" \
  "$WORKSPACE_ROOT/logs" \
  "$WORKSPACE_ROOT/tmp" \
  "$CONFIG_DIR" \
  "$METADATA_DIR" \
  "$BACKUPS_DIR"

if [[ -f "$ENV_FILE" && "$FORCE" -ne 1 ]]; then
  log "preserving existing ${ENV_FILE}"
else
  cat >"$ENV_FILE" <<EOF
# Managed hosted-v1 env file for ${INSTANCE_NAME}.
# This file may contain secrets. Keep permissions restricted.
HOST_OAR_WORKSPACE_ROOT=${WORKSPACE_ROOT}
OAR_CORE_PORT=${LISTEN_PORT}
OAR_WEB_UI_PORT=${WEB_UI_PORT}
OAR_WEB_UI_ORIGIN=${PUBLIC_ORIGIN}
OAR_ALLOW_UNAUTHENTICATED_WRITES=false
OAR_WEBAUTHN_RPID=${WEBAUTHN_RPID}
OAR_WEBAUTHN_ORIGIN=${PUBLIC_ORIGIN}
OAR_WEBAUTHN_RP_DISPLAY_NAME=OAR
OAR_CORS_ALLOWED_ORIGINS=
OAR_CORE_INSTANCE_ID=${CORE_INSTANCE_ID}
OAR_BOOTSTRAP_TOKEN=${BOOTSTRAP_TOKEN}
OAR_SHUTDOWN_TIMEOUT=15s
EOF
  chmod 600 "$ENV_FILE"
  log "wrote ${ENV_FILE}"
fi

if [[ -f "$INSTANCE_METADATA_FILE" && "$FORCE" -ne 1 ]]; then
  log "preserving existing ${INSTANCE_METADATA_FILE}"
else
  cat >"$INSTANCE_METADATA_FILE" <<EOF
FORMAT_VERSION=${HOSTED_INSTANCE_FORMAT_VERSION}
PROVISIONED_AT=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
INSTANCE_NAME=${INSTANCE_NAME}
INSTANCE_ROOT=${INSTANCE_ROOT}
WORKSPACE_ROOT=${WORKSPACE_ROOT}
BACKUPS_DIR=${BACKUPS_DIR}
LISTEN_HOST=${LISTEN_HOST}
LISTEN_PORT=${LISTEN_PORT}
WEB_UI_PORT=${WEB_UI_PORT}
PUBLIC_ORIGIN=${PUBLIC_ORIGIN}
WEBAUTHN_RPID=${WEBAUTHN_RPID}
CORE_INSTANCE_ID=${CORE_INSTANCE_ID}
BOOTSTRAP_TOKEN_CONFIGURED=$(bootstrap_token_configured_state "$BOOTSTRAP_TOKEN")
EOF
  chmod 644 "$INSTANCE_METADATA_FILE"
  log "wrote ${INSTANCE_METADATA_FILE}"
fi

log ""
log "Provisioned managed hosted-v1 deployment root:"
log "  instance:   ${INSTANCE_NAME}"
log "  root:       ${INSTANCE_ROOT}"
log "  workspace:  ${WORKSPACE_ROOT}"
log "  env file:   ${ENV_FILE}"
log "  metadata:   ${INSTANCE_METADATA_FILE}"
if [[ -z "$BOOTSTRAP_TOKEN" ]]; then
  log "  bootstrap:  cleared in env.production"
elif [[ "$BOOTSTRAP_TOKEN" == "$HOSTED_BOOTSTRAP_PLACEHOLDER" ]]; then
  log "  bootstrap:  placeholder written; replace before first bootstrap onboarding"
else
  log "  bootstrap:  token written to env.production"
fi
