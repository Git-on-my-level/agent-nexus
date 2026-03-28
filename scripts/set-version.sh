#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Validate or update the repo VERSION file and generated cli/core/web version metadata.

Usage:
  ./scripts/set-version.sh <version>
  ./scripts/set-version.sh --check <version>
EOF
}

die() {
  echo "$*" >&2
  exit 1
}

validate_version() {
  local version="$1"
  if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
    die "invalid version ${version}; expected v<major>.<minor>.<patch> with optional prerelease suffix"
  fi
}

MODE="write"
TARGET_VERSION=""

case "${1:-}" in
  --check)
    [[ $# -eq 2 ]] || {
      usage >&2
      exit 1
    }
    MODE="check"
    TARGET_VERSION="$2"
    ;;
  -h|--help)
    usage
    exit 0
    ;;
  "")
    usage >&2
    exit 1
    ;;
  *)
    [[ $# -eq 1 ]] || {
      usage >&2
      exit 1
    }
    TARGET_VERSION="$1"
    ;;
esac

validate_version "$TARGET_VERSION"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
VERSION_FILE="${REPO_ROOT}/VERSION"
CURRENT_VERSION="$("${SCRIPT_DIR}/read-version.sh")"

validate_version "$CURRENT_VERSION"

if [[ "${MODE}" == "check" ]]; then
  if [[ "${CURRENT_VERSION}" != "${TARGET_VERSION}" ]]; then
    die "repo VERSION ${CURRENT_VERSION} does not match expected release version ${TARGET_VERSION}"
  fi
  "${SCRIPT_DIR}/sync-version.sh" --check
  exit 0
fi

if [[ "${CURRENT_VERSION}" != "${TARGET_VERSION}" ]]; then
  printf '%s\n' "${TARGET_VERSION}" > "${VERSION_FILE}"
fi

"${SCRIPT_DIR}/sync-version.sh"
printf 'repo VERSION set to %s\n' "${TARGET_VERSION}"
