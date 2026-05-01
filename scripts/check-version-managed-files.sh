#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Validate that version-managed files match the canonical release-prep set.

Usage:
  ./scripts/check-version-managed-files.sh
  ./scripts/check-version-managed-files.sh --staged
EOF
}

die() {
  echo "$*" >&2
  exit 1
}

MODE="worktree"
case "${1:-}" in
  --staged)
    MODE="staged"
    ;;
  --worktree)
    MODE="worktree"
    ;;
  -h|--help)
    usage
    exit 0
    ;;
  "")
    ;;
  *)
    usage >&2
    die "unknown argument: $1"
    ;;
esac

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

EXPECTED_FILE="${TMP_DIR}/expected"
ACTUAL_FILE="${TMP_DIR}/actual"

mapfile -t expected_files < <("${SCRIPT_DIR}/version-managed-files.sh")
printf '%s\n' "${expected_files[@]}" | sort -u > "${EXPECTED_FILE}"

case "${MODE}" in
  worktree)
    {
      git diff --name-only --diff-filter=ACMRT
      git ls-files --others --exclude-standard
    } | sort -u > "${ACTUAL_FILE}"
    ;;
  staged)
    git diff --cached --name-only --diff-filter=ACMRT | sort -u > "${ACTUAL_FILE}"
    ;;
esac

if [[ ! -s "${ACTUAL_FILE}" ]]; then
  die "set-version.sh did not change any version-managed files"
fi

unexpected="$(comm -23 "${ACTUAL_FILE}" "${EXPECTED_FILE}")"
missing="$(comm -13 "${ACTUAL_FILE}" "${EXPECTED_FILE}")"

if [[ -n "${unexpected}" ]]; then
  die "$(printf 'unexpected version-managed file(s) changed outside the canonical release-prep set:\n%s' "${unexpected}")"
fi

if [[ -n "${missing}" ]]; then
  die "$(printf 'expected version-managed file(s) were not updated or staged:\n%s' "${missing}")"
fi

if [[ "${MODE}" == "staged" ]]; then
  unstaged="$(git diff --name-only -- "${expected_files[@]}")"
  [[ -z "${unstaged}" ]] || die "$(printf 'version-managed file(s) still unstaged after git add:\n%s' "${unstaged}")"
fi
