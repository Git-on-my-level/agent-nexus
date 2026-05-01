#!/usr/bin/env bash
set -euo pipefail

cat <<'EOF'
VERSION
adapters/agent-bridge/pyproject.toml
cli/internal/buildinfo/version_generated.go
core/internal/buildinfo/version_generated.go
web-ui/package.json
web-ui/src/lib/generated/version.js
EOF
