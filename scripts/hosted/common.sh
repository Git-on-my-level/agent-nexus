#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
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

resolve_core_bin() {
  if [[ -n "${OAR_CORE_BIN:-}" && -x "${OAR_CORE_BIN}" ]]; then
    printf '%s\n' "${OAR_CORE_BIN}"
    return 0
  fi
  if [[ -x "${REPO_ROOT}/core/.bin/oar-core" ]]; then
    printf '%s\n' "${REPO_ROOT}/core/.bin/oar-core"
    return 0
  fi
  if [[ -x "${HOME}/.oar/bin/oar-core" ]]; then
    printf '%s\n' "${HOME}/.oar/bin/oar-core"
    return 0
  fi
  die "oar-core binary not found; build it with ./core/scripts/build-prod or set OAR_CORE_BIN"
}

resolve_schema_path() {
  if [[ -n "${OAR_SCHEMA_PATH:-}" && -f "${OAR_SCHEMA_PATH}" ]]; then
    printf '%s\n' "${OAR_SCHEMA_PATH}"
    return 0
  fi
  if [[ -f "${REPO_ROOT}/contracts/oar-schema.yaml" ]]; then
    printf '%s\n' "${REPO_ROOT}/contracts/oar-schema.yaml"
    return 0
  fi
  if [[ -f "${HOME}/.oar/share/oar-schema.yaml" ]]; then
    printf '%s\n' "${HOME}/.oar/share/oar-schema.yaml"
    return 0
  fi
  die "oar schema not found; set OAR_SCHEMA_PATH"
}
