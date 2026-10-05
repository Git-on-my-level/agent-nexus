#!/usr/bin/env bash
set -euo pipefail

REPO="Git-on-my-level/agent-nexus"
INSTALL_DIR="${INSTALL_DIR:-${HOME}/.local/bin}"
VERSION="${VERSION:-}"

info()  { printf '  %s\n' "$*"; }
fatal() { printf 'Error: %s\n' "$*" >&2; exit 1; }

detect_os() {
  case "$(uname -s)" in
    Linux*)  echo "linux"  ;;
    Darwin*) echo "darwin" ;;
    *)       fatal "Unsupported OS: $(uname -s). Download manually from https://github.com/${REPO}/releases" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)       echo "amd64" ;;
    aarch64|arm64)      echo "arm64" ;;
    *)                  fatal "Unsupported architecture: $(uname -m)" ;;
  esac
}

resolve_version() {
  if [ -n "$VERSION" ]; then
    echo "$VERSION"
    return
  fi
  if ! command -v curl >/dev/null 2>&1; then
    fatal "curl is required to resolve the latest version"
  fi
  if ! tag=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1); then
    tag=""
  fi
  if [ -z "$tag" ]; then
    redirect=$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest") || fatal "Could not determine latest release. Set VERSION explicitly."
    case "$redirect" in
      "https://github.com/${REPO}/releases/tag/"*) tag="${redirect##*/}" ;;
      *) fatal "Latest release redirect did not resolve a same-origin tag" ;;
    esac
  fi
  echo "$tag"
}

main() {
  printf 'Installing anx CLI...\n'

  OS="$(detect_os)"
  ARCH="$(detect_arch)"
  VERSION="$(resolve_version)"

  [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-+][A-Za-z0-9.-]+)?$ ]] || fatal "Invalid release tag: ${VERSION}"
  info "Version:  ${VERSION}"
  info "OS/Arch:  ${OS}/${ARCH}"
  info "Install:  ${INSTALL_DIR}/anx"

  ARCHIVE="anx_${VERSION}_${OS}_${ARCH}.tar.gz"
  BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
  ARCHIVE_URL="${BASE_URL}/${ARCHIVE}"
  CHECKSUMS_URL="${BASE_URL}/checksums.txt"

  TMPDIR_DL="$(mktemp -d)"
  trap 'rm -rf "$TMPDIR_DL"' EXIT

  info "Downloading ${ARCHIVE}..."
  curl -fsSL -o "${TMPDIR_DL}/${ARCHIVE}" "$ARCHIVE_URL" \
    || fatal "Download failed. Check that release ${VERSION} exists at https://github.com/${REPO}/releases"

  info "Downloading checksums..."
  curl -fsSL -o "${TMPDIR_DL}/checksums.txt" "$CHECKSUMS_URL" \
    || fatal "Checksum download failed"

  info "Verifying checksum..."
  EXPECTED=$(awk -v archive="$ARCHIVE" '$2 == archive {print $1}' "${TMPDIR_DL}/checksums.txt")
  if [ -z "$EXPECTED" ]; then
    fatal "Archive ${ARCHIVE} not found in checksums.txt"
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL=$(sha256sum "${TMPDIR_DL}/${ARCHIVE}" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    ACTUAL=$(shasum -a 256 "${TMPDIR_DL}/${ARCHIVE}" | awk '{print $1}')
  else
    fatal "Neither sha256sum nor shasum found; cannot verify checksum"
  fi

  if [ "$EXPECTED" != "$ACTUAL" ]; then
    fatal "Checksum mismatch:\n  expected: ${EXPECTED}\n  actual:   ${ACTUAL}"
  fi

  mkdir -p "$INSTALL_DIR"
  info "Extracting..."
  tar -xzf "${TMPDIR_DL}/${ARCHIVE}" -C "${TMPDIR_DL}"
  # Stage on the destination filesystem so replacement is an atomic rename.
  # Honor the updater's per-install lock rather than racing its receipt.
  INSTALL_LOCK="${INSTALL_DIR}/anx.anx-update.lock"
  (set -o noclobber; : > "$INSTALL_LOCK") 2>/dev/null || fatal "An update is active; retry later."
  trap 'rm -rf "$TMPDIR_DL"; rm -f "$INSTALL_LOCK"' EXIT
  INSTALL_STAGE=$(mktemp -d "${INSTALL_DIR}/.anx-install-XXXXXX")
  trap 'rm -rf "$TMPDIR_DL" "$INSTALL_STAGE"; rm -f "$INSTALL_LOCK"' EXIT
  cp "${TMPDIR_DL}/anx" "${INSTALL_STAGE}/anx"
  chmod +x "${INSTALL_STAGE}/anx"
  if command -v sha256sum >/dev/null 2>&1; then
    BINARY_SHA=$(sha256sum "${INSTALL_STAGE}/anx" | awk '{print $1}')
  else
    BINARY_SHA=$(shasum -a 256 "${INSTALL_STAGE}/anx" | awk '{print $1}')
  fi
  printf '{"managed_by":"anx","version":"%s","sha256":"%s","installed_at":"%s"}\n' "$VERSION" "$BINARY_SHA" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "${INSTALL_STAGE}/receipt.json"
  mv "${INSTALL_STAGE}/anx" "${INSTALL_DIR}/anx"
  mv "${INSTALL_STAGE}/receipt.json" "${INSTALL_DIR}/anx.anx-install.json"
  "${INSTALL_DIR}/anx" skills sync || info "Managed skill sync needs attention; run anx skills status."

  printf '\nanx %s installed to %s/anx\n' "$VERSION" "$INSTALL_DIR"

  if ! echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
    printf '\n  NOTE: %s is not in your PATH.\n' "$INSTALL_DIR"
    printf '  Add it with:  export PATH="%s:$PATH"\n' "$INSTALL_DIR"
  fi

  printf '\nQuick start:\n'
  info "anx --base-url http://<core-host>:8000 host enroll --name <host-slug>"
  info "anx --base-url http://<core-host>:8000 --as <name> auth whoami"
  info "anx bridge install"
  info "anx version"
}

main
