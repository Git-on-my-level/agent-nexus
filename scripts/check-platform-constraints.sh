#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

VIOLATIONS=""

while IFS= read -r FILE; do
  if [[ "$FILE" == *"vendor"* ]] || [[ "$FILE" == *"generated"* ]] || [[ "$FILE" == *"_test.go" ]]; then
    continue
  fi

  if grep -qE 'syscall\.Kill|Setpgid|syscall\.Signal' "$FILE" 2>/dev/null; then
    if ! head -5 "$FILE" | grep -qE '//go:build !windows|// \+build !windows'; then
      REL_PATH="${FILE#$ROOT_DIR/}"
      VIOLATIONS="${VIOLATIONS}  ${REL_PATH}"$'\n'
    fi
  fi
done < <(find "$ROOT_DIR/cli" "$ROOT_DIR/core" -name "*.go" -type f)

if [ -n "$VIOLATIONS" ]; then
  echo "ERROR: Unix-only syscalls without !windows build constraint:" >&2
  echo "$VIOLATIONS" >&2
  echo "Add '//go:build !windows' and create a matching *_windows.go stub." >&2
  exit 1
fi

echo "OK: no platform constraint violations"
