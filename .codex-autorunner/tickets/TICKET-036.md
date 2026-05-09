---
title: "Derive CLI usage preflight flags from runtime specs"
agent: codex
done: true
ticket_id: "tkt_cli_preflight_from_runtime_specs_036"
---

## Triage

Priority: P1. Usage errors must beat profile/config resolution. Manual preflight flag tables have already drifted from real parsers, which makes that invariant brittle.

## Problem

`command_usage_preflight.go` duplicates command flag metadata by hand. Several entries no longer match the real parsers, so valid or invalid usage can be classified differently before and after config resolution.

## Evidence

- `cli/AGENTS.md:69` requires command usage errors to beat profile/config resolution when detectable without side effects.
- `cli/internal/app/resource_commands.go:607` shows real `threads list` lifecycle flags, while `cli/internal/app/command_usage_preflight.go:472` lists unrelated `topic-ref`, `purpose`, and `with-counts`.
- `cli/internal/app/resource_commands.go:1069` shows real `artifacts list` flags such as `backing-scope`, `thread-id`, `ids`, and date filters, while `cli/internal/app/command_usage_preflight.go:482` has a smaller unrelated set.
- `cli/internal/app/resource_commands.go:2339` shows real `inbox get` flags including `inbox-id` and `risk-horizon-days`, while `cli/internal/app/command_usage_preflight.go:515` misses them.
- Commands used by scout: `nl -ba cli/internal/app/command_usage_preflight.go cli/internal/app/resource_commands.go`; `rg -n "from-file|body-file|if-updated-at|actor-id|dry-run|reason|json" cli/internal/app`.

## Proposed Fix

Introduce table-driven command flag metadata close to each parser or generate preflight specs from registry/runtime command descriptors. Keep lifecycle's existing derived pattern as the model. Add coverage that every documented command flag is accepted by preflight before config resolution.

## Validation

- Tests using ambiguous-profile setups that still return `invalid_flags` for bad flags and do not reject valid documented flags.
- `make cli-check`

## Progress

- Added shared runtime flag specs for `threads list`, `artifacts list`, and `inbox get` beside their parsers.
- Wired preflight specs from those runtime flag specs and removed stale manual preflight entries for those commands.
- Added regression coverage that runtime parser flags pass config-independent preflight and stale manual flags return `invalid_flags` before ambiguous-profile config resolution.
- Validation passed: `go test ./internal/app -run 'TestPreConfigUsagePreflight(AcceptsRuntimeParserFlags|RejectsStaleManualResourceFlags|BeatsAmbiguousProfileResolution|AcceptsBridgeRestartParserFlags)'` and `make cli-check`.
