---
title: "Replace bridge config discovery string parsing with TOML parsing"
agent: codex
done: true
ticket_id: "tkt_bridgeconfig_toml_042"
---

## Triage

Priority: P2. This is a correctness hardening task for config discovery; useful, but less urgent than side effects and auth error handling.

## Problem

CLI bridge base-url discovery uses a small hand-rolled TOML scanner instead of the TOML parser already used elsewhere in the CLI. It strips content after `#` before unquoting, only recognizes simple section headers, and can misread valid TOML string values such as URLs or paths containing `#`. Because `config.Resolve` can auto-select a single bridge config's base URL, parser drift here can route automation to the wrong core or fail config resolution.

## Evidence

- `cli/internal/bridgeconfig/bridgeconfig.go` implements `sectionHeaderPattern`, `configStringValue`, and `parseAssignment`.
- `parseAssignment` removes everything after `#` before `strconv.Unquote`, so a quoted value containing `#` is truncated.
- `cli/internal/app/bridge_lifecycle.go` and `cli/internal/app/bridge_commands.go` use `github.com/pelletier/go-toml/v2` for bridge TOML elsewhere.
- Read-only command used while scouting: `sed -n '1,220p' cli/internal/bridgeconfig/bridgeconfig.go`.

## Proposed Fix

Parse bridge config and agent manifest files with `pelletier/go-toml/v2` instead of string scanning. Preserve the current discovery behavior and error tolerance: unreadable files should still return contextual errors, and configs without a discoverable base URL should be skipped. Add tests for quoted `base_url` and `agent_home` values containing `#`, plus the existing direct `[anx].base_url` and manifest `[identity].base_url` fallback paths.

## Validation

- `go test ./internal/bridgeconfig ./internal/config`
- `make cli-check`

## Progress

Completed:

- Replaced bridge config discovery string scanning with `github.com/pelletier/go-toml/v2` decoding for bridge configs and agent manifests.
- Preserved discovery tolerance by skipping invalid or incomplete TOML and missing/unreadable manifest fallbacks, while keeping unreadable bridge config files as contextual discovery errors.
- Added regression coverage for quoted direct `[anx].base_url`, top-level `agent_home`, and manifest `[identity].base_url` values containing `#`.

Validated:

- `go test ./internal/bridgeconfig ./internal/config`
- `make cli-check`
