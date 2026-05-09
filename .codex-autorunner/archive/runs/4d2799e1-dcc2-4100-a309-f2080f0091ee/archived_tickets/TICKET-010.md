---
title: "Bring CLI usage preflight flag metadata back in sync"
agent: codex
done: true
ticket_id: "tkt_notifications_preflight_041"
---

## Triage

Priority: P2. This merges the bridge restart and notifications preflight drift findings because both should be fixed in the same CLI preflight metadata pass.

## Problem

The config-independent usage preflight has drifted from real CLI parsers in multiple places. Valid `anx bridge restart` flags can be rejected before command execution, while malformed notification flags can be hidden behind profile resolution failures on machines with multiple profiles.

Both cases violate the CLI invariant that usage and command-shape errors should beat config resolution whenever they can be detected without side effects.

## Evidence

- `cli/internal/app/bridge_lifecycle.go` defines the restart parser with `--config`, `--install-dir`, `--bin-dir`, `--timeout-seconds`, and `--force`.
- The same file's `localHelperTopic{Path: "bridge restart"}` lists only `--config` and `--force`.
- `cli/internal/app/command_usage_preflight.go` derives preflight flag specs from `localHelperTopics`, so stale help metadata becomes stale validation metadata.
- `cli/internal/app/subcommand_guidance.go` defines `notificationsSubcommandSpec` for `list`, `read`, and `dismiss`.
- `cli/internal/app/notifications_commands.go` accepts `notifications list --status --order` and mutations with `--wakeup-id`.
- `cli/internal/app/command_usage_preflight.go` has no manual entries for `"notifications list"`, `"notifications read"`, or `"notifications dismiss"`.
- Read-only commands used while scouting:
  - `rg -n "bridge restart|timeout-seconds|install-dir|bin-dir" cli/internal/app/bridge_lifecycle.go cli/internal/app/command_usage_preflight.go`
  - `rg -n '"notifications list"|"notifications read"|"notifications dismiss"|notificationsSubcommandSpec' cli/internal/app`

## Proposed Fix

Update the `bridge restart` local-helper flag metadata to include every flag accepted by `runBridgeRestart`:

- `--config`
- `--install-dir`
- `--bin-dir`
- `--timeout-seconds`
- `--force`

Add manual preflight flag specs for:

- `notifications list`: `--status <status>` and `--order <asc|desc>`
- `notifications read`: `--wakeup-id <id>`
- `notifications dismiss`: `--wakeup-id <id>`

Add tests covering valid bridge restart flags, unknown notification flags, and missing notification flag values in a home with multiple local profiles. The expected usage error should be `invalid_flags`, not `config_resolution_failed`.

## Validation

- PASS: `go test ./internal/app -run 'Bridge.*Restart|Notifications|Preflight|ConfigResolution'`
- PASS: `make cli-check`

## Completion Notes

- Updated `bridge restart` local-helper metadata and regenerated `cli/docs/generated/runtime-help.md`.
- Added manual preflight specs for `notifications list`, `notifications read`, and `notifications dismiss`.
- Added regression coverage for bridge restart parser flags and notification flag errors beating ambiguous profile resolution.
