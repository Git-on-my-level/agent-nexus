#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FIXTURE_DIR="$(mktemp -d)"
trap 'rm -rf "$FIXTURE_DIR"' EXIT

cat > "$FIXTURE_DIR/host_resolution.go" <<'EOF'
package app

import "syscall"

func lock(fd int) error { return syscall.Flock(fd, syscall.LOCK_EX) }
func unlock(fd int) error { return syscall.Flock(fd, syscall.LOCK_UN) }
EOF
cat > "$FIXTURE_DIR/runs_commands.go" <<'EOF'
package app

import "syscall"

const noFollow = syscall.O_NOFOLLOW
EOF

if OUTPUT="$(ANX_PLATFORM_SCAN_ROOT="$FIXTURE_DIR" "$ROOT_DIR/scripts/check-platform-constraints.sh" 2>&1)"; then
  echo "ERROR: platform checker accepted original untagged syscall usage" >&2
  exit 1
fi
for FILE in host_resolution.go runs_commands.go; do
  if [[ "$OUTPUT" != *"$FILE"* ]]; then
    echo "ERROR: platform checker did not flag $FILE" >&2
    exit 1
  fi
done

echo "OK: original untagged CLI syscall usage is rejected"
