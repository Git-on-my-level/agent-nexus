#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# Scan every tracked text file, including this policy; ignore binary artifacts.
# The path boundary distinguishes root mounts from system library paths.
GENERIC_PATTERNS='(^|[^[:alnum:]_/])/(Users|home)/[^/[:space:]]+/|(^|[^[:alnum:]_/])/[V]olumes/|\.tail[0-9a-f]{6}\.ts\.net'
extra_file_patterns=''
if [[ -n "${OSS_BOUNDARY_EXTRA_FILE:-}" ]]; then
  if ! extra_file_patterns="$(cat -- "$OSS_BOUNDARY_EXTRA_FILE" 2>/dev/null)"; then
    echo "OSS boundary check could not read the extra-pattern file." >&2
    exit 2
  fi
fi

# Read patterns on stdin so private expressions do not appear in process argv.
# Blank lines are ignored rather than becoming expressions matching every line.
# Suppress grep diagnostics: malformed expressions can otherwise echo secrets.
if printf '%s\n' "$GENERIC_PATTERNS" "${OSS_BOUNDARY_EXTRA_PATTERNS:-}" "$extra_file_patterns" |
  sed '/^[[:space:]]*$/d' |
  git grep -n -I -i -E -f - -- . 2>/dev/null; then
  echo "OSS boundary check failed: replace matched text with neutral examples." >&2
  exit 1
else
  result=$?
  if [[ "$result" -ne 1 ]]; then
    echo "OSS boundary check could not complete (exit $result). Check pattern syntax and Git availability." >&2
    exit "$result"
  fi
fi

echo "OSS boundary check passed."
