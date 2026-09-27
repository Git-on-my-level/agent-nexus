#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# CI cross-compiles cli. Retain the older core check for process signals;
# core's JIT file lock is part of a server target that CI does not cross-build.
if [[ -n "${ANX_PLATFORM_SCAN_ROOT:-}" ]]; then
  SCAN_ROOTS=("$ANX_PLATFORM_SCAN_ROOT")
else
  SCAN_ROOTS=("$ROOT_DIR/cli" "$ROOT_DIR/core")
fi
VIOLATIONS=()

while IFS= read -r -d '' FILE; do
  case "$FILE" in
    */vendor/*|*/generated/*|*_test.go|*_windows.go) continue ;;
  esac

  case "$FILE" in
    "$ROOT_DIR"/core/*) PATTERN='syscall\.(Kill|Signal)|(^|[^[:alnum:]_])Setpgid([^[:alnum:]_]|$)' ;;
    "$ROOT_DIR"/cli/*) PATTERN='syscall\.(Flock|LOCK_[A-Z0-9_]+|O_NOFOLLOW|Kill|Signal|Setpgid|Getpgid|Setsid|Fcntl|Kqueue|Mmap|Munmap|Mount|Unmount)|(^|[^[:alnum:]_])Setpgid([^[:alnum:]_]|$)' ;;
    *) PATTERN='syscall\.(Flock|LOCK_[A-Z0-9_]+|O_NOFOLLOW|Kill|Signal|Setpgid|Getpgid|Setsid|Fcntl|Kqueue|Mmap|Munmap|Mount|Unmount)|(^|[^[:alnum:]_])Setpgid([^[:alnum:]_]|$)' ;;
  esac
  if ! grep -qE "$PATTERN" "$FILE"; then
    continue
  fi

  # OS suffixes are implicit Go build constraints. For generic names, require
  # a constraint that excludes Windows rather than merely any constraint.
  case "$FILE" in
    *_linux.go|*_darwin.go|*_freebsd.go|*_netbsd.go|*_openbsd.go|*_dragonfly.go|*_solaris.go|*_aix.go) continue ;;
  esac
  BUILD_TAG="$(head -n 10 "$FILE" | sed -n 's@^//go:build[[:space:]]*@@p' | head -n 1)"
  if [[ "$BUILD_TAG" == *"!windows"* ]]; then
    continue
  fi
  if [[ "$BUILD_TAG" != *"windows"* ]] && [[ "$BUILD_TAG" =~ (^|[^a-z])(linux|darwin|freebsd|netbsd|openbsd|dragonfly|solaris|aix)([^a-z]|$) ]]; then
    continue
  fi

  VIOLATIONS+=("${FILE#$ROOT_DIR/}")
done < <(find "${SCAN_ROOTS[@]}" -type f -name '*.go' -print0)

if (( ${#VIOLATIONS[@]} > 0 )); then
  echo "ERROR: Unix-only syscall identifiers in Windows-buildable CLI files:" >&2
  printf '  %s\n' "${VIOLATIONS[@]}" >&2
  echo "Move platform code behind a build constraint or use a portable helper." >&2
  exit 1
fi

echo "OK: no platform constraint violations"
