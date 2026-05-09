---
title: "Replace managed bridge lifecycle TOML sniffing with parsed config"
agent: codex
done: true
ticket_id: "tkt_cli_bridge_lifecycle_toml_parse_026"
---

## Triage

Priority: P2. This follows the completed bridge config parsing cleanup and removes a remaining parser drift point in managed lifecycle detection.

## Problem

Managed bridge lifecycle loading still infers config shape with a small hand-rolled TOML scanner even though nearby bridge config loading now uses `pelletier/go-toml/v2`. This leaves a second parser path that can drift on valid TOML syntax, comments, arrays of tables, and quoted values containing `#`.

## Evidence

- `cli/internal/app/bridge_lifecycle.go:503` reads bridge config content, then calls `inferBridgeRuntimeKind(string(content), absPath)` before separately unmarshalling TOML for `[bridge].managed_package_auto_update`.
- `cli/internal/app/bridge_lifecycle.go:536` checks top-level `agent_home` using `bridgeConfigStringValue` and `bridgeConfigHasTopLevelKey` instead of the parsed TOML root.
- `cli/internal/app/bridge_commands.go:1897` defines `parseBridgeConfigAssignment`, which strips content after `#` before unquoting and is reused by lifecycle sniffing helpers.
- `cli/internal/app/bridge_commands.go:1764` already has `loadBridgeConfigDetails`, which parses the same bridge config with `toml.Unmarshal` and rejects invalid TOML explicitly.
- Existing tests cover only simple `agent_home = ".anx"` and `agent_home = ".anx" # prod` in `TestLoadBridgeManagedConfigDetectsAgentConfig*`.
- Command used: `rg -n "bridgeConfigStringValue|parseBridgeConfigAssignment|bridgeSectionHeaderPattern|bridgeConfigHasSection|inferBridgeRuntimeKind" cli/internal/app cli/internal/bridgeconfig`.

## Proposed Fix

Make `loadBridgeManagedConfig` parse bridge config TOML once and infer managed runtime kind from the parsed root map. Remove or narrow the string-sniffing helpers used only for lifecycle inference, while preserving current behavior for valid agent bridge configs and explicit errors for non-agent or invalid configs.

Add regression tests for valid TOML that the ad hoc scanner is likely to mishandle, such as a quoted `agent_home` containing `#`, dotted or commented section syntax where supported by the TOML parser, and invalid TOML that should fail deterministically.

## Validation

- PASS: `go test ./internal/app -run 'BridgeManaged|LoadBridgeManaged|BridgeLifecycle|TOML'`
- PASS: `make cli-check`

## Progress

- Updated `loadBridgeManagedConfig` to parse bridge config TOML once with `pelletier/go-toml/v2`, reject invalid TOML with `bridge_config_toml_invalid`, and infer managed runtime kind from the parsed root map.
- Removed the lifecycle-only top-level TOML string scanner helpers while leaving config rewrite helpers intact.
- Added regressions for quoted `agent_home` keys/values containing `#`, dotted `bridge.managed_package_auto_update`, and invalid TOML rejection.
