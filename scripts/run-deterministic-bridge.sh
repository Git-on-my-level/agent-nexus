#!/usr/bin/env bash
# Isolated deterministic bridge fixtures; never installs or controls a service.
set -euo pipefail
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}/cli"
go test ./internal/bridge ./internal/app -run 'Test(MigrationCustomWrapper|UnitParsers|HostBridgeInstall|HostBridgeMigration)' -count=1 -timeout=5m
