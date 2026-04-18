#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOSTED_JSON_TOOL_PATH="${REPO_ROOT}/scripts/hosted/json-tool.py"
HOSTED_BACKUP_FORMAT_VERSION="hosted-ops-backup/v1"
HOSTED_INSTANCE_FORMAT_VERSION="hosted-instance/v1"
HOSTED_BOOTSTRAP_PLACEHOLDER="REPLACE_WITH_SECURE_BOOTSTRAP_TOKEN"

log() {
  printf '%s\n' "$*"
}

warn() {
  printf 'warning: %s\n' "$*" >&2
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  local command_name
  for command_name in "$@"; do
    if ! command -v "$command_name" >/dev/null 2>&1; then
      die "required command not found: $command_name"
    fi
  done
}

require_hosted_json_tool() {
  require_command python3
  [[ -f "$HOSTED_JSON_TOOL_PATH" ]] || die "hosted JSON helper not found: $HOSTED_JSON_TOOL_PATH"
}

json_get() {
  local json="$1"
  local path="$2"
  require_hosted_json_tool
  printf '%s' "$json" | python3 "$HOSTED_JSON_TOOL_PATH" get "$path"
}

json_get_first() {
  local json="$1"
  shift
  local path value
  for path in "$@"; do
    if value="$(json_get "$json" "$path" 2>/dev/null)"; then
      printf '%s\n' "$value"
      return 0
    fi
  done
  return 1
}

json_count_key_value() {
  local json="$1"
  local key="$2"
  local value="$3"
  require_hosted_json_tool
  printf '%s' "$json" | python3 "$HOSTED_JSON_TOOL_PATH" count-key-value "$key" "$value"
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
    return 0
  fi
  die "need sha256sum or shasum for checksum generation"
}

validate_instance_name() {
  local instance_name="$1"
  [[ "$instance_name" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || die "instance name must match ^[A-Za-z0-9][A-Za-z0-9._-]*$"
}

validate_host() {
  local host="$1"
  [[ "$host" =~ ^[A-Za-z0-9._:-]+$ ]] || die "host contains unsupported characters: $host"
}

validate_port() {
  local port="$1"
  [[ "$port" =~ ^[0-9]+$ ]] || die "port must be numeric: $port"
  (( port >= 1 && port <= 65535 )) || die "port must be between 1 and 65535: $port"
}

validate_non_negative_integer() {
  local value="$1"
  local name="${2:-value}"
  [[ "$value" =~ ^[0-9]+$ ]] || die "${name} must be a non-negative integer: ${value}"
}

validate_bootstrap_token_mode() {
  local mode="$1"
  case "$mode" in
    placeholder|clear|keep-source|replace)
      ;;
    *)
      die "bootstrap token mode must be one of: placeholder, clear, keep-source, replace"
      ;;
  esac
}

validate_blob_backend() {
  local backend="$1"
  case "$backend" in
    filesystem|object|s3)
      ;;
    *)
      die "blob backend must be one of: filesystem, object, s3"
      ;;
  esac
}

normalize_bool_value() {
  local value="${1:-}"
  value="$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')"
  case "$value" in
    1|true|yes|on)
      printf 'true\n'
      ;;
    0|false|no|off|"")
      printf 'false\n'
      ;;
    *)
      die "boolean value must be one of: true, false, 1, 0, yes, no, on, off"
      ;;
  esac
}

validate_origin() {
  local origin="$1"
  [[ "$origin" =~ ^https?://[^/]+$ ]] || die "origin must be an absolute http(s) origin with no path: $origin"
}

validate_backup_format_version() {
  local format_version="$1"
  if [[ -z "$format_version" ]]; then
    die "backup manifest is missing FORMAT_VERSION"
  fi
  if [[ "$format_version" != "$HOSTED_BACKUP_FORMAT_VERSION" ]]; then
    die "unsupported backup format version: ${format_version} (supported: ${HOSTED_BACKUP_FORMAT_VERSION})"
  fi
}

origin_authority() {
  local origin="$1"
  local without_scheme="${origin#*://}"
  printf '%s\n' "$without_scheme"
}

origin_host() {
  local authority
  authority="$(origin_authority "$1")"
  if [[ "$authority" == \[*\]:* ]]; then
    authority="${authority%%]:*}]"
  elif [[ "$authority" == *:* ]]; then
    authority="${authority%%:*}"
  fi
  printf '%s\n' "$authority"
}

normalize_hostname() {
  local value="$1"
  value="$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')"
  value="${value%.}"
  if [[ "$value" == \[*\] ]]; then
    value="${value#[}"
    value="${value%]}"
  fi
  printf '%s\n' "$value"
}

hostname_looks_like_ip() {
  local host
  host="$(normalize_hostname "$1")"
  [[ "$host" == *:* || "$host" =~ ^[0-9.]+$ ]]
}

validate_webauthn_rpid_against_host() {
  local rpid host normalized_rpid normalized_host
  rpid="$1"
  host="$2"
  normalized_rpid="$(normalize_hostname "$rpid")"
  normalized_host="$(normalize_hostname "$host")"
  [[ -n "$normalized_rpid" && -n "$normalized_host" ]] || die "WebAuthn RP ID and host must be non-empty"
  if [[ "$normalized_rpid" == "$normalized_host" ]]; then
    return 0
  fi
  if hostname_looks_like_ip "$normalized_rpid" || hostname_looks_like_ip "$normalized_host"; then
    die "WebAuthn RP ID ${normalized_rpid} must exactly match origin host ${normalized_host}"
  fi
  if [[ "$normalized_rpid" == "localhost" || "$normalized_host" == "localhost" ]]; then
    die "WebAuthn RP ID ${normalized_rpid} must exactly match origin host ${normalized_host}"
  fi
  if [[ "$normalized_host" == *".${normalized_rpid}" ]]; then
    return 0
  fi
  die "WebAuthn RP ID ${normalized_rpid} must equal or be a suffix of origin host ${normalized_host}"
}

canonicalize_path_allow_missing() {
  local raw_path="$1"
  [[ -n "$raw_path" ]] || return 0
  local base_name parent_dir
  base_name="$(basename "$raw_path")"
  parent_dir="$(dirname "$raw_path")"
  mkdir -p "$parent_dir"
  parent_dir="$(cd "$parent_dir" && pwd -P)"
  printf '%s/%s\n' "$parent_dir" "$base_name"
}

path_is_within_base() {
  local path="$1"
  local base="$2"
  [[ -n "$path" && -n "$base" ]] || return 1
  [[ "$path" == "$base" || "$path" == "$base/"* ]]
}

remap_path_between_roots() {
  local source_path="$1"
  local source_base="$2"
  local target_base="$3"
  [[ -n "$source_path" && -n "$source_base" && -n "$target_base" ]] || return 1
  path_is_within_base "$source_path" "$source_base" || return 1
  if [[ "$source_path" == "$source_base" ]]; then
    printf '%s\n' "$target_base"
    return 0
  fi
  printf '%s/%s\n' "$target_base" "${source_path#"$source_base"/}"
}

blob_storage_mode() {
  local backend="${1:-filesystem}"
  case "$backend" in
    s3)
      printf 'remote\n'
      ;;
    *)
      printf 'local\n'
      ;;
  esac
}

blob_backup_mode() {
  local backend="${1:-filesystem}"
  case "$backend" in
    s3)
      printf 'reference\n'
      ;;
    *)
      printf 'copy\n'
      ;;
  esac
}

blob_bundle_path() {
  local backend="${1:-filesystem}"
  if [[ "$(blob_storage_mode "$backend")" == "local" ]]; then
    printf 'workspace/blob-store\n'
    return 0
  fi
  printf '\n'
}

blob_effective_local_root() {
  local workspace_root="$1"
  local backend="${2:-filesystem}"
  local configured_root="${3:-}"
  if [[ "$backend" == "s3" ]]; then
    printf '\n'
    return 0
  fi
  if [[ -n "$configured_root" ]]; then
    printf '%s\n' "$configured_root"
    return 0
  fi
  printf '%s/artifacts/content\n' "$workspace_root"
}

blob_effective_location() {
  local workspace_root="$1"
  local backend="${2:-filesystem}"
  local configured_root="${3:-}"
  local s3_bucket="${4:-}"
  local s3_prefix="${5:-}"
  case "$backend" in
    s3)
      if [[ -n "$s3_prefix" ]]; then
        printf 's3://%s/%s\n' "$s3_bucket" "$s3_prefix"
      else
        printf 's3://%s\n' "$s3_bucket"
      fi
      ;;
    *)
      blob_effective_local_root "$workspace_root" "$backend" "$configured_root"
      ;;
  esac
}

blob_key_format() {
  local backend="${1:-filesystem}"
  case "$backend" in
    filesystem)
      printf 'sha256-hex-filename\n'
      ;;
    object|s3)
      printf 'sha256-hex-sharded-object-key\n'
      ;;
    *)
      printf 'unknown\n'
      ;;
  esac
}

blob_s3_inline_credentials_present() {
  local access_key_id="${1:-}"
  local secret_access_key="${2:-}"
  local session_token="${3:-}"
  if [[ -n "$access_key_id" || -n "$secret_access_key" || -n "$session_token" ]]; then
    printf 'true\n'
    return 0
  fi
  printf 'false\n'
}

emit_blob_env_lines() {
  local backend="$1"
  local blob_root="$2"
  local s3_bucket="$3"
  local s3_prefix="$4"
  local s3_region="$5"
  local s3_endpoint="$6"
  local s3_access_key_id="$7"
  local s3_secret_access_key="$8"
  local s3_session_token="$9"
  local s3_force_path_style="${10:-false}"

  cat <<EOF
ANX_BLOB_BACKEND=${backend}
ANX_BLOB_ROOT=${blob_root}
ANX_BLOB_S3_BUCKET=${s3_bucket}
ANX_BLOB_S3_PREFIX=${s3_prefix}
ANX_BLOB_S3_REGION=${s3_region}
ANX_BLOB_S3_ENDPOINT=${s3_endpoint}
ANX_BLOB_S3_ACCESS_KEY_ID=${s3_access_key_id}
ANX_BLOB_S3_SECRET_ACCESS_KEY=${s3_secret_access_key}
ANX_BLOB_S3_SESSION_TOKEN=${s3_session_token}
ANX_BLOB_S3_FORCE_PATH_STYLE=${s3_force_path_style}
EOF
}

emit_blob_metadata_lines() {
  local workspace_root="$1"
  local backend="$2"
  local blob_root="$3"
  local s3_bucket="$4"
  local s3_prefix="$5"
  local s3_region="$6"
  local s3_endpoint="$7"
  local s3_access_key_id="$8"
  local s3_secret_access_key="$9"
  local s3_session_token="${10}"
  local s3_force_path_style="${11:-false}"
  local effective_location
  local inline_credentials_present

  effective_location="$(blob_effective_location "$workspace_root" "$backend" "$blob_root" "$s3_bucket" "$s3_prefix")"
  inline_credentials_present="$(blob_s3_inline_credentials_present "$s3_access_key_id" "$s3_secret_access_key" "$s3_session_token")"

  cat <<EOF
BLOB_BACKEND=${backend}
BLOB_STORAGE_MODE=$(blob_storage_mode "$backend")
BLOB_BACKUP_MODE=$(blob_backup_mode "$backend")
BLOB_ROOT=${blob_root}
BLOB_EFFECTIVE_LOCATION=${effective_location}
BLOB_KEY_FORMAT=$(blob_key_format "$backend")
BLOB_S3_BUCKET=${s3_bucket}
BLOB_S3_PREFIX=${s3_prefix}
BLOB_S3_REGION=${s3_region}
BLOB_S3_ENDPOINT=${s3_endpoint}
BLOB_S3_FORCE_PATH_STYLE=${s3_force_path_style}
BLOB_S3_INLINE_CREDENTIALS_PRESENT=${inline_credentials_present}
EOF
}

emit_workspace_quota_env_lines() {
  local max_blob_bytes="$1"
  local max_artifacts="$2"
  local max_documents="$3"
  local max_document_revisions="$4"
  local max_upload_bytes="$5"

  cat <<EOF
ANX_WORKSPACE_MAX_BLOB_BYTES=${max_blob_bytes}
ANX_WORKSPACE_MAX_ARTIFACTS=${max_artifacts}
ANX_WORKSPACE_MAX_DOCUMENTS=${max_documents}
ANX_WORKSPACE_MAX_DOCUMENT_REVISIONS=${max_document_revisions}
ANX_WORKSPACE_MAX_UPLOAD_BYTES=${max_upload_bytes}
EOF
}

emit_workspace_quota_metadata_lines() {
  local max_blob_bytes="$1"
  local max_artifacts="$2"
  local max_documents="$3"
  local max_document_revisions="$4"
  local max_upload_bytes="$5"

  cat <<EOF
WORKSPACE_MAX_BLOB_BYTES=${max_blob_bytes}
WORKSPACE_MAX_ARTIFACTS=${max_artifacts}
WORKSPACE_MAX_DOCUMENTS=${max_documents}
WORKSPACE_MAX_DOCUMENT_REVISIONS=${max_document_revisions}
WORKSPACE_MAX_UPLOAD_BYTES=${max_upload_bytes}
EOF
}

remap_local_blob_root_for_target() {
  local source_blob_root="${1:-}"
  local source_instance_root="${2:-}"
  local source_workspace_root="${3:-}"
  local target_instance_root="${4:-}"
  local target_workspace_root="${5:-}"
  [[ -n "$source_blob_root" ]] || return 0

  if remap_path_between_roots "$source_blob_root" "$source_workspace_root" "$target_workspace_root"; then
    return 0
  fi
  if remap_path_between_roots "$source_blob_root" "$source_instance_root" "$target_instance_root"; then
    return 0
  fi
  printf '%s\n' "$source_blob_root"
}

generate_token() {
  local bytes="${1:-24}"
  od -vAn -N "$bytes" -tx1 /dev/urandom | tr -d ' \n'
  printf '\n'
}

is_dir_empty() {
  local dir="$1"
  [[ -d "$dir" ]] || return 0
  local first_entry
  first_entry="$(find "$dir" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null || true)"
  [[ -z "$first_entry" ]]
}

ensure_empty_or_forced_target() {
  local dir="$1"
  local force="${2:-0}"
  if [[ -e "$dir" && ! -d "$dir" ]]; then
    die "target exists and is not a directory: $dir"
  fi
  if [[ -d "$dir" && "$force" -ne 1 ]] && ! is_dir_empty "$dir"; then
    die "target directory is not empty: $dir (rerun with --force to allow overlay)"
  fi
  mkdir -p "$dir"
}

copy_tree_contents() {
  local source_dir="$1"
  local target_dir="$2"
  mkdir -p "$target_dir"
  if [[ ! -d "$source_dir" ]]; then
    return 0
  fi
  local entry
  shopt -s dotglob nullglob
  for entry in "$source_dir"/*; do
    cp -R "$entry" "$target_dir"/
  done
  shopt -u dotglob nullglob
}

manifest_get() {
  local file="$1"
  local key="$2"
  local line
  line="$(grep -E "^${key}=" "$file" | head -n 1 || true)"
  if [[ -z "$line" ]]; then
    return 1
  fi
  printf '%s\n' "${line#*=}"
}

dotenv_get() {
  manifest_get "$1" "$2"
}

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

verify_backup_checksums() {
  local backup_dir="$1"
  local checksum_file="${backup_dir}/SHA256SUMS"
  local line expected_hash relative_path actual_hash

  [[ -f "$checksum_file" ]] || die "backup checksum file not found: ${checksum_file}"

  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ -n "$line" ]] || continue
    expected_hash="${line%%  *}"
    if [[ "$line" != *"  "* ]]; then
      die "invalid checksum entry in ${checksum_file}: ${line}"
    fi
    relative_path="${line#*  }"
    [[ "$expected_hash" =~ ^[0-9a-fA-F]{64}$ ]] || die "invalid checksum hash in ${checksum_file}: ${line}"
    [[ -n "$relative_path" ]] || die "invalid checksum entry in ${checksum_file}: ${line}"
    if [[ ! -f "${backup_dir}/${relative_path}" ]]; then
      die "checksum verification failed for ${relative_path}: file is missing from backup bundle"
    fi
    actual_hash="$(sha256_file "${backup_dir}/${relative_path}")"
    if [[ "$actual_hash" != "$expected_hash" ]]; then
      die "checksum verification failed for ${relative_path}: expected ${expected_hash}, got ${actual_hash}"
    fi
  done <"$checksum_file"
}

bootstrap_token_configured_state() {
  local token="${1:-}"
  if [[ -z "$token" ]]; then
    printf 'clear\n'
    return 0
  fi
  if [[ "$token" == "$HOSTED_BOOTSTRAP_PLACEHOLDER" ]]; then
    printf 'placeholder\n'
    return 0
  fi
  printf 'set\n'
}

load_dotenv_file() {
  local dotenv_path="$1"
  [[ -f "$dotenv_path" ]] || return 0
  set -a
  # shellcheck disable=SC1090
  source "$dotenv_path"
  set +a
}

sqlite_scalar() {
  local db_path="$1"
  local query="$2"
  sqlite3 -noheader -batch "$db_path" "$query"
}

count_files() {
  local dir="$1"
  if [[ ! -d "$dir" ]]; then
    printf '0\n'
    return 0
  fi
  find "$dir" -type f | wc -l | tr -d ' '
}

paths_containing_text() {
  local needle="$1"
  local root="$2"
  if [[ ! -e "$root" ]]; then
    return 0
  fi
  grep -R -I -F -l -- "$needle" "$root" 2>/dev/null || true
}

directory_size_bytes() {
  local dir="$1"
  if [[ ! -d "$dir" ]]; then
    printf '0\n'
    return 0
  fi
  du -sk "$dir" | awk '{print $1 * 1024}'
}

pick_loopback_port() {
  local attempts=0
  local port
  while (( attempts < 200 )); do
    port=$((18080 + RANDOM % 10000))
    if ! (echo >/dev/tcp/127.0.0.1/"$port") >/dev/null 2>&1; then
      printf '%s\n' "$port"
      return 0
    fi
    attempts=$((attempts + 1))
  done
  die "failed to find an available loopback port after 200 attempts"
}

wait_for_http_ok() {
  local url="$1"
  local timeout_seconds="${2:-20}"
  local waited=0
  while (( waited < timeout_seconds )); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    waited=$((waited + 1))
  done
  return 1
}

build_core_binary() {
  local bin_path="${1:-${REPO_ROOT}/core/.bin/anx-core}"
  local bin_dir
  bin_dir="$(dirname "$bin_path")"
  BIN_DIR="$bin_dir" ANX_CORE_BIN="$bin_path" "${REPO_ROOT}/core/scripts/build-prod" >&2
  printf '%s\n' "$bin_path"
}

start_core_server() {
  local pid_var_name="$1"
  local core_bin="$2"
  local workspace_root="$3"
  local schema_path="$4"
  local listen_host="$5"
  local listen_port="$6"
  local log_file="$7"
  local core_instance_id="${8:-}"
  local bootstrap_token_mode="${9:-unset}"
  local bootstrap_token="${10:-}"
  local dev_actor_mode="${11:-false}"
  local allow_unauthenticated_writes="${12:-false}"
  local allow_loopback_verification_reads="${13:-false}"
  local -a cmd

  validate_port "$listen_port"
  mkdir -p "$(dirname "$log_file")"

  cmd=(
    "$core_bin"
    --listen-addr "${listen_host}:${listen_port}"
    --schema-path "$schema_path"
    --workspace-root "$workspace_root"
  )
  if [[ -n "$core_instance_id" ]]; then
    cmd+=(--core-instance-id "$core_instance_id")
  fi

  (
    export ANX_ALLOW_LOOPBACK_VERIFICATION_READS="$allow_loopback_verification_reads"
    case "$bootstrap_token_mode" in
      unset)
        unset ANX_BOOTSTRAP_TOKEN
        ;;
      set)
        export ANX_BOOTSTRAP_TOKEN="$bootstrap_token"
        ;;
      *)
        die "unsupported bootstrap token mode for start_core_server: $bootstrap_token_mode"
        ;;
    esac
    exec "${cmd[@]}"
  ) >"$log_file" 2>&1 &

  printf -v "$pid_var_name" '%s' "$!"
}

stop_background_process() {
  local pid="${1:-}"
  if [[ -n "$pid" ]] && kill -0 "$pid" >/dev/null 2>&1; then
    kill "$pid" >/dev/null 2>&1 || true
    wait "$pid" >/dev/null 2>&1 || true
  fi
}

resolve_core_bin() {
  if [[ -n "${ANX_CORE_BIN:-}" && -x "${ANX_CORE_BIN}" ]]; then
    printf '%s\n' "${ANX_CORE_BIN}"
    return 0
  fi
  if [[ -x "${REPO_ROOT}/core/.bin/anx-core" ]]; then
    printf '%s\n' "${REPO_ROOT}/core/.bin/anx-core"
    return 0
  fi
  if [[ -x "${HOME}/.anx/bin/anx-core" ]]; then
    printf '%s\n' "${HOME}/.anx/bin/anx-core"
    return 0
  fi
  die "anx-core binary not found; build it with ./core/scripts/build-prod or set ANX_CORE_BIN"
}

resolve_schema_path() {
  if [[ -n "${ANX_SCHEMA_PATH:-}" && -f "${ANX_SCHEMA_PATH}" ]]; then
    printf '%s\n' "${ANX_SCHEMA_PATH}"
    return 0
  fi
  if [[ -f "${REPO_ROOT}/contracts/anx-schema.yaml" ]]; then
    printf '%s\n' "${REPO_ROOT}/contracts/anx-schema.yaml"
    return 0
  fi
  if [[ -f "${HOME}/.anx/share/anx-schema.yaml" ]]; then
    printf '%s\n' "${HOME}/.anx/share/anx-schema.yaml"
    return 0
  fi
  die "anx schema not found; set ANX_SCHEMA_PATH"
}
