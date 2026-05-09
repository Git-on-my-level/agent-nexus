---
title: "Add secret command flags to config-independent preflight"
agent: codex
done: true
ticket_id: "tkt_cli_secret_preflight_flags_021"
---

## Triage

Priority: P1. This is a narrow CLI automation-safety fix: local usage errors should not be hidden behind profile/config failures.

## Problem

Secret command parsers accept local flags, but `command_usage_preflight.go` has no `secret ...` flag specs. On machines with ambiguous or missing profiles, malformed secret flags can surface as `config_resolution_failed` instead of the usage-level `invalid_flags` error that CLI guidance requires for side-effect-free command-shape mistakes.

## Evidence

- `cli/AGENTS.md` says usage and command-shape errors should beat profile/config resolution when they can be detected without side effects.
- `cli/internal/app/resource_secrets.go` defines flags for `secret create` (`--from-stdin`, `--description`), `secret get` (`--reveal`), `secret update` (`--from-stdin`, `--description`), and `secret exec` (`--secret`).
- `cli/internal/app/command_usage_preflight.go` includes `secretSubcommandSpec` for shape validation but `manualPreflightFlagSpecs()` has no entries for `secret create`, `secret get`, `secret update`, or `secret exec`.
- Command used: `rg -n "secret create|secret update|from-stdin|description|manualPreflightFlagSpecs|secretSubcommandSpec|config_resolution_failed|invalid_flags" cli/internal/app/*_test.go cli/internal/app/*.go cli/AGENTS.md cli/README.md`.

## Proposed Fix

Add manual preflight flag specs for the secret subcommands that have local flags:

- `secret create`: `--from-stdin`, `--description`
- `secret get`: `--reveal`
- `secret update`: `--from-stdin`, `--description`
- `secret exec`: `--secret`

Add focused tests using an ambiguous-profile home to prove unknown flags, missing flag values, and valid secret flags are classified before config resolution.

## Validation

- `go test ./internal/app -run 'Secret|Preflight|ConfigResolution|InvalidFlags'`
- `make cli-check`

## Progress

- Added config-independent preflight flag specs for `secret create`, `secret get`, `secret update`, and `secret exec`.
- Added focused ambiguous-profile regression coverage for unknown secret flags, missing secret flag values, and valid secret flags reaching config resolution.
- Validation passed:
  - `go test ./internal/app -run 'Secret|Preflight|ConfigResolution|InvalidFlags'`
  - `make cli-check`
