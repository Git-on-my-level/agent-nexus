#!/usr/bin/env bash
# Lists registerRoute prefixes from anx-core handler (inventory aid for contract work).
# Does not replace CI parity checks.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
grep -n 'registerRoute("' "$ROOT/core/internal/server/handler.go" | head -200
