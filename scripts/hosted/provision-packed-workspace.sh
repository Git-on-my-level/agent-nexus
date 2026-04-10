#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/common.sh"

usage() {
  cat <<'EOF'
Usage: provision-packed-workspace.sh [options]

Create one packed-host workspace deployment root plus one systemd env file for
`oar-core@<workspace-id>.service`.

Required:
  --workspace-id ID
  --workspace-slug SLUG
  --workspace-root DIR       Packed-host instance root (workspace/, config/, metadata/, backups/)
  --env-file PATH
  --listen-port PORT
  --public-origin ORIGIN
  --control-plane-workspace-id ID
  --control-plane-base-url URL
  --control-plane-token-issuer ISSUER
  --control-plane-token-audience AUDIENCE
  --control-plane-token-public-key KEY
  --workspace-service-id ID
  --workspace-service-private-key KEY

Optional:
  --blob-backend filesystem|s3   default: filesystem
  --blob-root DIR
  --blob-s3-bucket NAME
  --blob-s3-prefix PREFIX
  --blob-s3-region REGION
  --blob-s3-endpoint URL
  --blob-s3-access-key-id ID
  --blob-s3-secret-access-key KEY
  --blob-s3-session-token TOKEN
  --blob-s3-force-path-style true|false
  --force                        rewrite config/env.production, metadata, and env file
  --enable                       enable and start systemd unit
  -h, --help                     show help
EOF
}

WORKSPACE_ID=""
WORKSPACE_SLUG=""
WORKSPACE_ROOT=""
ENV_FILE=""
LISTEN_PORT=""
PUBLIC_ORIGIN=""
CONTROL_PLANE_WORKSPACE_ID=""
CONTROL_PLANE_BASE_URL=""
CONTROL_PLANE_TOKEN_ISSUER=""
CONTROL_PLANE_TOKEN_AUDIENCE=""
CONTROL_PLANE_TOKEN_PUBLIC_KEY=""
WORKSPACE_SERVICE_ID=""
WORKSPACE_SERVICE_PRIVATE_KEY=""
BLOB_BACKEND="filesystem"
BLOB_ROOT=""
BLOB_S3_BUCKET=""
BLOB_S3_PREFIX=""
BLOB_S3_REGION=""
BLOB_S3_ENDPOINT=""
BLOB_S3_ACCESS_KEY_ID=""
BLOB_S3_SECRET_ACCESS_KEY=""
BLOB_S3_SESSION_TOKEN=""
BLOB_S3_FORCE_PATH_STYLE="false"
FORCE=0
ENABLE=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --workspace-id) WORKSPACE_ID="$2"; shift 2 ;;
    --workspace-slug) WORKSPACE_SLUG="$2"; shift 2 ;;
    --workspace-root) WORKSPACE_ROOT="$2"; shift 2 ;;
    --env-file) ENV_FILE="$2"; shift 2 ;;
    --listen-port) LISTEN_PORT="$2"; shift 2 ;;
    --public-origin) PUBLIC_ORIGIN="$2"; shift 2 ;;
    --control-plane-workspace-id) CONTROL_PLANE_WORKSPACE_ID="$2"; shift 2 ;;
    --control-plane-base-url) CONTROL_PLANE_BASE_URL="$2"; shift 2 ;;
    --control-plane-token-issuer) CONTROL_PLANE_TOKEN_ISSUER="$2"; shift 2 ;;
    --control-plane-token-audience) CONTROL_PLANE_TOKEN_AUDIENCE="$2"; shift 2 ;;
    --control-plane-token-public-key) CONTROL_PLANE_TOKEN_PUBLIC_KEY="$2"; shift 2 ;;
    --workspace-service-id) WORKSPACE_SERVICE_ID="$2"; shift 2 ;;
    --workspace-service-private-key) WORKSPACE_SERVICE_PRIVATE_KEY="$2"; shift 2 ;;
    --blob-backend) BLOB_BACKEND="$2"; shift 2 ;;
    --blob-root) BLOB_ROOT="$2"; shift 2 ;;
    --blob-s3-bucket) BLOB_S3_BUCKET="$2"; shift 2 ;;
    --blob-s3-prefix) BLOB_S3_PREFIX="$2"; shift 2 ;;
    --blob-s3-region) BLOB_S3_REGION="$2"; shift 2 ;;
    --blob-s3-endpoint) BLOB_S3_ENDPOINT="$2"; shift 2 ;;
    --blob-s3-access-key-id) BLOB_S3_ACCESS_KEY_ID="$2"; shift 2 ;;
    --blob-s3-secret-access-key) BLOB_S3_SECRET_ACCESS_KEY="$2"; shift 2 ;;
    --blob-s3-session-token) BLOB_S3_SESSION_TOKEN="$2"; shift 2 ;;
    --blob-s3-force-path-style) BLOB_S3_FORCE_PATH_STYLE="$(normalize_bool_value "$2")"; shift 2 ;;
    --force) FORCE=1; shift ;;
    --enable) ENABLE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

for value_name in \
  WORKSPACE_ID WORKSPACE_SLUG WORKSPACE_ROOT ENV_FILE LISTEN_PORT PUBLIC_ORIGIN \
  CONTROL_PLANE_WORKSPACE_ID CONTROL_PLANE_BASE_URL CONTROL_PLANE_TOKEN_ISSUER \
  CONTROL_PLANE_TOKEN_AUDIENCE CONTROL_PLANE_TOKEN_PUBLIC_KEY \
  WORKSPACE_SERVICE_ID WORKSPACE_SERVICE_PRIVATE_KEY
do
  if [[ -z "${!value_name}" ]]; then
    echo "Missing required value: ${value_name}" >&2
    exit 1
  fi
done

validate_instance_name "$WORKSPACE_ID"
validate_instance_name "$CONTROL_PLANE_WORKSPACE_ID"
validate_instance_name "$WORKSPACE_SERVICE_ID"
validate_port "$LISTEN_PORT"
validate_origin "$PUBLIC_ORIGIN"
validate_blob_backend "$BLOB_BACKEND"

upsert_env_var() {
  local file="$1"
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
  ' "$file" >"$tmp_file"
  mv "$tmp_file" "$file"
}

origin_port() {
  local authority
  authority="$(origin_authority "$1")"
  if [[ "$authority" == \[*\]:* ]]; then
    printf '%s\n' "${authority##*:}"
    return 0
  fi
  if [[ "$authority" == *:* ]]; then
    printf '%s\n' "${authority##*:}"
    return 0
  fi
  if [[ "$1" == https://* ]]; then
    printf '443\n'
  else
    printf '80\n'
  fi
}

mkdir -p "$(dirname "${ENV_FILE}")"
WORKSPACE_ROOT="$(canonicalize_path_allow_missing "$WORKSPACE_ROOT")"
WEB_UI_PORT="$(origin_port "$PUBLIC_ORIGIN")"

provision_args=(
  --instance "$WORKSPACE_ID"
  --instance-root "$WORKSPACE_ROOT"
  --public-origin "$PUBLIC_ORIGIN"
  --listen-host 127.0.0.1
  --listen-port "$LISTEN_PORT"
  --web-ui-port "$WEB_UI_PORT"
  --core-instance-id "$WORKSPACE_ID"
  --blob-backend "$BLOB_BACKEND"
)
if [[ -n "$BLOB_ROOT" ]]; then
  provision_args+=(--blob-root "$BLOB_ROOT")
fi
if [[ "$BLOB_BACKEND" == "s3" ]]; then
  [[ -n "$BLOB_S3_BUCKET" ]] || die "--blob-s3-bucket is required when --blob-backend s3"
  [[ -n "$BLOB_S3_REGION" ]] || die "--blob-s3-region is required when --blob-backend s3"
  provision_args+=(
    --blob-s3-bucket "$BLOB_S3_BUCKET"
    --blob-s3-prefix "$BLOB_S3_PREFIX"
    --blob-s3-region "$BLOB_S3_REGION"
    --blob-s3-endpoint "$BLOB_S3_ENDPOINT"
    --blob-s3-force-path-style "$BLOB_S3_FORCE_PATH_STYLE"
  )
  if [[ -n "$BLOB_S3_ACCESS_KEY_ID" ]]; then
    provision_args+=(--blob-s3-access-key-id "$BLOB_S3_ACCESS_KEY_ID")
  fi
  if [[ -n "$BLOB_S3_SECRET_ACCESS_KEY" ]]; then
    provision_args+=(--blob-s3-secret-access-key "$BLOB_S3_SECRET_ACCESS_KEY")
  fi
  if [[ -n "$BLOB_S3_SESSION_TOKEN" ]]; then
    provision_args+=(--blob-s3-session-token "$BLOB_S3_SESSION_TOKEN")
  fi
fi
if [[ "$FORCE" -eq 1 ]]; then
  provision_args+=(--force)
fi

"${SCRIPT_DIR}/provision-workspace.sh" "${provision_args[@]}"

RUNTIME_WORKSPACE_ROOT="${WORKSPACE_ROOT}/workspace"
ENV_PRODUCTION="${WORKSPACE_ROOT}/config/env.production"
METADATA_FILE="${WORKSPACE_ROOT}/metadata/instance.env"

upsert_env_var "$ENV_PRODUCTION" "OAR_LISTEN_ADDR" "127.0.0.1:${LISTEN_PORT}"
upsert_env_var "$ENV_PRODUCTION" "OAR_WORKSPACE_ROOT" "${RUNTIME_WORKSPACE_ROOT}"
upsert_env_var "$ENV_PRODUCTION" "OAR_CORE_BASE_URL" "$PUBLIC_ORIGIN"
upsert_env_var "$ENV_PRODUCTION" "OAR_WORKSPACE_ID" "$CONTROL_PLANE_WORKSPACE_ID"
upsert_env_var "$ENV_PRODUCTION" "OAR_WORKSPACE_NAME" "$WORKSPACE_SLUG"
upsert_env_var "$ENV_PRODUCTION" "OAR_HUMAN_AUTH_MODE" "control_plane"
upsert_env_var "$ENV_PRODUCTION" "OAR_CONTROL_PLANE_BASE_URL" "$CONTROL_PLANE_BASE_URL"
upsert_env_var "$ENV_PRODUCTION" "OAR_CONTROL_PLANE_HEARTBEAT_INTERVAL" "30s"
upsert_env_var "$ENV_PRODUCTION" "OAR_CONTROL_PLANE_TOKEN_ISSUER" "$CONTROL_PLANE_TOKEN_ISSUER"
upsert_env_var "$ENV_PRODUCTION" "OAR_CONTROL_PLANE_TOKEN_AUDIENCE" "$CONTROL_PLANE_TOKEN_AUDIENCE"
upsert_env_var "$ENV_PRODUCTION" "OAR_CONTROL_PLANE_WORKSPACE_ID" "$CONTROL_PLANE_WORKSPACE_ID"
upsert_env_var "$ENV_PRODUCTION" "OAR_CONTROL_PLANE_TOKEN_PUBLIC_KEY" "$CONTROL_PLANE_TOKEN_PUBLIC_KEY"
upsert_env_var "$ENV_PRODUCTION" "OAR_WORKSPACE_SERVICE_ID" "$WORKSPACE_SERVICE_ID"
upsert_env_var "$ENV_PRODUCTION" "OAR_WORKSPACE_SERVICE_PRIVATE_KEY" "$WORKSPACE_SERVICE_PRIVATE_KEY"
upsert_env_var "$ENV_PRODUCTION" "OAR_ENABLE_DEV_ACTOR_MODE" "false"
upsert_env_var "$ENV_PRODUCTION" "OAR_ALLOW_PASSKEY_DEV_BYPASS" "false"
upsert_env_var "$ENV_PRODUCTION" "OAR_PROJECTION_MODE" "background"
upsert_env_var "$ENV_PRODUCTION" "OAR_PROJECTION_MAINTENANCE_INTERVAL" "5s"
upsert_env_var "$ENV_PRODUCTION" "OAR_PROJECTION_STALE_SCAN_INTERVAL" "30s"
upsert_env_var "$ENV_PRODUCTION" "OAR_PROJECTION_MAINTENANCE_BATCH_SIZE" "50"
upsert_env_var "$METADATA_FILE" "WORKSPACE_SLUG" "$WORKSPACE_SLUG"
upsert_env_var "$METADATA_FILE" "SYSTEMD_ENV_FILE" "$ENV_FILE"

if [[ -f "${ENV_FILE}" && "${FORCE}" -ne 1 ]]; then
  echo "Preserving existing env file: ${ENV_FILE}"
else
  cat > "${ENV_FILE}" <<EOF
OAR_LISTEN_ADDR=127.0.0.1:${LISTEN_PORT}
OAR_WORKSPACE_ROOT=${RUNTIME_WORKSPACE_ROOT}
OAR_CORE_BASE_URL=${PUBLIC_ORIGIN}
OAR_WORKSPACE_ID=${CONTROL_PLANE_WORKSPACE_ID}
OAR_WORKSPACE_NAME=${WORKSPACE_SLUG}
OAR_SCHEMA_PATH=/opt/oar/share/oar-schema.yaml
OAR_META_COMMANDS_PATH=/opt/oar/share/meta/commands.json
OAR_CORE_INSTANCE_ID=${WORKSPACE_ID}

OAR_HUMAN_AUTH_MODE=control_plane
OAR_CONTROL_PLANE_BASE_URL=${CONTROL_PLANE_BASE_URL}
OAR_CONTROL_PLANE_HEARTBEAT_INTERVAL=30s
OAR_CONTROL_PLANE_TOKEN_ISSUER=${CONTROL_PLANE_TOKEN_ISSUER}
OAR_CONTROL_PLANE_TOKEN_AUDIENCE=${CONTROL_PLANE_TOKEN_AUDIENCE}
OAR_CONTROL_PLANE_WORKSPACE_ID=${CONTROL_PLANE_WORKSPACE_ID}
OAR_CONTROL_PLANE_TOKEN_PUBLIC_KEY=${CONTROL_PLANE_TOKEN_PUBLIC_KEY}
OAR_WORKSPACE_SERVICE_ID=${WORKSPACE_SERVICE_ID}
OAR_WORKSPACE_SERVICE_PRIVATE_KEY=${WORKSPACE_SERVICE_PRIVATE_KEY}

OAR_ALLOW_UNAUTHENTICATED_WRITES=false
OAR_ENABLE_DEV_ACTOR_MODE=false
OAR_ALLOW_PASSKEY_DEV_BYPASS=false
OAR_SHUTDOWN_TIMEOUT=15s

OAR_PROJECTION_MODE=background
OAR_PROJECTION_MAINTENANCE_INTERVAL=5s
OAR_PROJECTION_STALE_SCAN_INTERVAL=30s
OAR_PROJECTION_MAINTENANCE_BATCH_SIZE=50

$(emit_blob_env_lines "$BLOB_BACKEND" "$BLOB_ROOT" "$BLOB_S3_BUCKET" "$BLOB_S3_PREFIX" "$BLOB_S3_REGION" "$BLOB_S3_ENDPOINT" "$BLOB_S3_ACCESS_KEY_ID" "$BLOB_S3_SECRET_ACCESS_KEY" "$BLOB_S3_SESSION_TOKEN" "$BLOB_S3_FORCE_PATH_STYLE")
EOF

  chmod 600 "${ENV_FILE}"
fi

echo "Provisioned packed-host workspace:"
echo "  workspace_id:   ${WORKSPACE_ID}"
echo "  workspace_slug: ${WORKSPACE_SLUG}"
echo "  instance_root:  ${WORKSPACE_ROOT}"
echo "  workspace_root: ${RUNTIME_WORKSPACE_ROOT}"
echo "  env_file:       ${ENV_FILE}"
echo "  env_production: ${ENV_PRODUCTION}"
echo "  metadata_file:  ${METADATA_FILE}"
echo "  listen_port:    ${LISTEN_PORT}"
echo "  public_origin:  ${PUBLIC_ORIGIN}"

if [[ "${ENABLE}" -eq 1 ]]; then
  systemctl daemon-reload
  systemctl enable --now "oar-core@${WORKSPACE_ID}"
  echo "Started systemd unit: oar-core@${WORKSPACE_ID}"
fi
