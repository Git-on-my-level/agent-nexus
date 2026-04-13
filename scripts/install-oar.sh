#!/usr/bin/env bash
set -euo pipefail

REPO="Git-on-my-level/organization-autorunner"
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
  tag=$(curl -fsSL -o /dev/null -w '%{redirect_url}' \
    "https://github.com/${REPO}/releases/latest" 2>/dev/null \
    | sed 's|.*/||')
  if [ -z "$tag" ]; then
    fatal "Could not determine latest release. Set VERSION explicitly."
  fi
  echo "$tag"
}

main() {
  printf 'Installing oar CLI...\n'

  OS="$(detect_os)"
  ARCH="$(detect_arch)"
  VERSION="$(resolve_version)"

  info "Version:  ${VERSION}"
  info "OS/Arch:  ${OS}/${ARCH}"
  info "Install:  ${INSTALL_DIR}/oar"

  ARCHIVE="oar_${VERSION}_${OS}_${ARCH}.tar.gz"
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
  EXPECTED=$(grep "${ARCHIVE}" "${TMPDIR_DL}/checksums.txt" | awk '{print $1}')
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

  info "Extracting..."
  tar -xzf "${TMPDIR_DL}/${ARCHIVE}" -C "${TMPDIR_DL}" oar

  mkdir -p "$INSTALL_DIR"
  mv "${TMPDIR_DL}/oar" "${INSTALL_DIR}/oar"
  chmod +x "${INSTALL_DIR}/oar"

  printf '\noar %s installed to %s/oar\n' "$VERSION" "$INSTALL_DIR"

  if ! echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
    printf '\n  NOTE: %s is not in your PATH.\n' "$INSTALL_DIR"
    printf '  Add it with:  export PATH="%s:$PATH"\n' "$INSTALL_DIR"
  fi

  printf '\nQuick start:\n'
  info "oar --base-url http://<core-host>:8000 --agent <name> auth register --username <username>"
  info "oar bridge install"
  info "oar version"
}

main
