#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Cut and publish a patch release from origin/main.

Usage:
  ./scripts/release-patch.sh
  ./scripts/release-patch.sh --version v0.0.13
  ./scripts/release-patch.sh --dry-run

Options:
  --version <version>  Override the computed next patch version.
  --skip-checks        Skip `make check`, `make e2e-smoke`, and `make cli-check`.
  --no-wait            Do not wait for the GitHub release workflow to finish.
  --dry-run            Print the planned version and release base, then exit.
  -h, --help           Show this help text.
EOF
}

die() {
  echo "$*" >&2
  exit 1
}

require_cmd() {
  local cmd="$1"
  command -v "$cmd" >/dev/null 2>&1 || die "required command not found: ${cmd}"
}

validate_version() {
  local version="$1"
  [[ "${version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] \
    || die "invalid patch release version ${version}; expected v<major>.<minor>.<patch>"
}

ensure_clean_worktree() {
  local untracked_files

  git diff --quiet || die "working tree has unstaged changes; commit or stash them first"
  git diff --cached --quiet || die "working tree has staged changes; commit or stash them first"
  untracked_files="$(git ls-files --others --exclude-standard)"
  [[ -z "${untracked_files}" ]] || die "$(printf 'working tree has untracked files; commit, stash, or remove them first:\n%s' "${untracked_files}")"
}

next_patch_version() {
  local latest_tag
  latest_tag="$(git describe --tags --abbrev=0 origin/main 2>/dev/null || true)"
  if [[ -z "${latest_tag}" ]]; then
    printf 'v0.0.1\n'
    return 0
  fi

  validate_version "${latest_tag}"
  if [[ ! "${latest_tag}" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    die "could not parse latest tag ${latest_tag}"
  fi

  printf 'v%s.%s.%s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "$(( ${BASH_REMATCH[3]} + 1 ))"
}

find_release_run_id() {
  local target_version="$1"
  local target_sha="$2"

  gh run list \
    --workflow "Release CLI" \
    --limit 20 \
    --json databaseId,headBranch,headSha \
    --jq ".[] | select(.headBranch == \"${target_version}\" and .headSha == \"${target_sha}\") | .databaseId" \
    | head -n1
}

wait_for_release() {
  local target_version="$1"
  local target_sha="$2"
  local run_id=""

  for _ in $(seq 1 20); do
    run_id="$(find_release_run_id "${target_version}" "${target_sha}" || true)"
    if [[ -n "${run_id}" ]]; then
      break
    fi
    sleep 3
  done

  [[ -n "${run_id}" ]] || die "could not find Release CLI workflow run for ${target_version}"

  gh run watch "${run_id}" --exit-status
  gh release view "${target_version}"
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
TARGET_VERSION=""
SKIP_CHECKS=0
WAIT_FOR_RELEASE=1
DRY_RUN=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      [[ $# -ge 2 ]] || die "--version requires a value"
      TARGET_VERSION="$2"
      shift 2
      ;;
    --skip-checks)
      SKIP_CHECKS=1
      shift
      ;;
    --no-wait)
      WAIT_FOR_RELEASE=0
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      die "unknown argument: $1"
      ;;
  esac
done

cd "${REPO_ROOT}"

require_cmd git
require_cmd make

if [[ "${DRY_RUN}" != "1" ]]; then
  require_cmd gh
  gh auth status >/dev/null
fi

git fetch origin main --tags
ensure_clean_worktree

HEAD_SHA="$(git rev-parse HEAD)"
ORIGIN_MAIN_SHA="$(git rev-parse origin/main)"
[[ "${HEAD_SHA}" == "${ORIGIN_MAIN_SHA}" ]] \
  || die "HEAD ${HEAD_SHA} does not match origin/main ${ORIGIN_MAIN_SHA}; release from an up-to-date checkout of origin/main"

if [[ -z "${TARGET_VERSION}" ]]; then
  TARGET_VERSION="$(next_patch_version)"
fi
validate_version "${TARGET_VERSION}"

if git rev-parse -q --verify "refs/tags/${TARGET_VERSION}" >/dev/null 2>&1; then
  die "tag ${TARGET_VERSION} already exists"
fi

if [[ "${DRY_RUN}" == "1" ]]; then
  cat <<EOF
release base: ${ORIGIN_MAIN_SHA}
next version: ${TARGET_VERSION}
skip checks: ${SKIP_CHECKS}
wait for release: ${WAIT_FOR_RELEASE}
EOF
  exit 0
fi

TMP_RELEASE_DIR="${REPO_ROOT}/.tmp/release-artifacts-test"
cleanup() {
  rm -rf "${TMP_RELEASE_DIR}"
}
trap cleanup EXIT

if [[ "${SKIP_CHECKS}" != "1" ]]; then
  make check
  make e2e-smoke
fi

"${SCRIPT_DIR}/set-version.sh" "${TARGET_VERSION}"

if [[ "${SKIP_CHECKS}" != "1" ]]; then
  make cli-check
fi

"${SCRIPT_DIR}/build-cli-release-artifacts.sh" "${TARGET_VERSION}" ".tmp/release-artifacts-test"

git add \
  VERSION \
  cli/internal/buildinfo/version_generated.go \
  core/internal/buildinfo/version_generated.go \
  web-ui/src/lib/generated/version.js \
  web-ui/package.json
git commit -m "Prepare release ${TARGET_VERSION}"
git push origin HEAD:main
git tag -a "${TARGET_VERSION}" -m "Release ${TARGET_VERSION}"
git push origin "${TARGET_VERSION}"

if [[ "${WAIT_FOR_RELEASE}" == "1" ]]; then
  wait_for_release "${TARGET_VERSION}" "$(git rev-parse HEAD)"
else
  echo "tag ${TARGET_VERSION} pushed; release workflow should now be running"
fi
