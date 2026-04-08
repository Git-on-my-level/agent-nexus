#!/usr/bin/env bash
# Optional second process: run the agent bridge with deterministic_ack (see
# adapters/agent-bridge/examples/deterministic.toml). Requires a registered auth
# state and workspace_id aligned with your core deployment.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG="${CONFIG:-${REPO_ROOT}/adapters/agent-bridge/examples/deterministic.toml}"

cd "${REPO_ROOT}/adapters/agent-bridge"
make bridge-run CONFIG="${CONFIG}"
