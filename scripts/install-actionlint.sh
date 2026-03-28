#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BIN_DIR="${REPO_ROOT}/.bin"
BIN_PATH="${BIN_DIR}/actionlint"
VERSION="v1.7.7"
MODULE="github.com/rhysd/actionlint/cmd/actionlint"

installed_version() {
  if [[ ! -x "${BIN_PATH}" ]]; then
    return 1
  fi

  "${BIN_PATH}" -version | awk 'NR==1 { print $1 }'
}

if [[ "$(installed_version || true)" == "${VERSION}" ]]; then
  exit 0
fi

mkdir -p "${BIN_DIR}"
GOBIN="${BIN_DIR}" go install "${MODULE}@${VERSION}"
