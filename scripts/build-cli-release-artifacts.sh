#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Build release archives and checksums for the anx CLI.

Usage:
  ./scripts/build-cli-release-artifacts.sh [version] [output-dir]

Examples:
  ./scripts/build-cli-release-artifacts.sh
  ./scripts/build-cli-release-artifacts.sh v1.2.3 dist
EOF
}

if [[ "${1:-}" == "-h" ]] || [[ "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CLI_DIR="${REPO_ROOT}/cli"
EXPECTED_VERSION="$("${SCRIPT_DIR}/read-version.sh")"
VERSION="${1:-${EXPECTED_VERSION}}"
OUTPUT_DIR="${2:-dist}"
DIST_DIR="${REPO_ROOT}/${OUTPUT_DIR}"

if [[ "${VERSION}" != "${EXPECTED_VERSION}" ]]; then
  echo "release version mismatch: requested ${VERSION}, repo VERSION is ${EXPECTED_VERSION}" >&2
  exit 1
fi

checksum_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
    return
  fi
  echo "missing checksum tool (need sha256sum or shasum)" >&2
  exit 1
}

rm -rf "${DIST_DIR}"
mkdir -p "${DIST_DIR}"

for GOOS in linux darwin windows; do
  for GOARCH in amd64 arm64; do
    TARGET="anx_${VERSION}_${GOOS}_${GOARCH}"
    STAGE_DIR="${DIST_DIR}/${TARGET}"
    BIN_NAME="anx"
    BIN_EXT=""

    if [[ "${GOOS}" == "windows" ]]; then
      BIN_EXT=".exe"
    fi

    mkdir -p "${STAGE_DIR}"

    (
      cd "${CLI_DIR}"
      CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" go build \
        -trimpath \
        -ldflags="-s -w -X agent-nexus-cli/internal/buildinfo.Current=${VERSION}" \
        -o "${STAGE_DIR}/${BIN_NAME}${BIN_EXT}" \
        ./cmd/anx )

    if [[ "${GOOS}" == "windows" ]]; then
      (
        cd "${STAGE_DIR}"
        zip -q "../${TARGET}.zip" "${BIN_NAME}${BIN_EXT}"
      )
    else
      tar -C "${STAGE_DIR}" -czf "${DIST_DIR}/${TARGET}.tar.gz" "${BIN_NAME}${BIN_EXT}"
    fi

    rm -rf "${STAGE_DIR}"
  done
done

(
  cd "${DIST_DIR}"
  : > checksums.txt
  for artifact in *.tar.gz *.zip; do
    printf '%s  %s\n' "$(checksum_file "${artifact}")" "${artifact}" >> checksums.txt
  done
)

printf 'Built release artifacts in %s\n' "${DIST_DIR}"
