---
title: "Fix secret command machine identities"
agent: opencode
done: true
ticket_id: "tkt_cli_secret_machine_identities_042"
---

## Triage

Priority: P2. This is a straightforward metadata alignment issue, but machine-facing command ids matter for scripts and JSON consumers.

## Problem

Secret JSON envelopes can report wrong `command_id` values because runtime identity mapping is incomplete or stale.

## Evidence

- `cli/internal/registry/commands.json:5342` defines `secrets.reveal` for `secret get --reveal`, `secrets.reveal-batch` for `secret exec`, and `secrets.update` for `secret update`.
- `cli/docs/generated/commands.md:1266` reflects the generated docs for secret commands.
- `cli/internal/app/machine_identity.go:14` and `:85` map `secret get` and `secret exec` to non-registry identities and omit `secret update`, which falls back to `secret.update`.
- Commands used by scout: `rg -n 'secrets\\.(list|create|get|update|delete|exec)|secret update' cli`; `nl -ba cli/internal/app/machine_identity.go cli/internal/registry/commands.json`.

## Proposed Fix

Map `secret update` to `secrets.update`, map `secret exec` to `secrets.reveal-batch`, and distinguish `secret get --reveal` as `secrets.reveal` from metadata lookup. Add envelope tests for the affected commands.

## Changes Made

### `cli/internal/app/machine_identity.go`
- Fixed `"secret exec"` mapping: `secrets.exec` → `secrets.reveal-batch` (matches registry)
- Added `"secret update"` → `secrets.update` entry (was missing, fell back to `secret.update`)
- Added `"secret get --reveal"` → `secrets.reveal` entry (distinguishes reveal from metadata-only get)

### `cli/internal/app/resource_secrets.go`
- Changed `runSecretGet` reveal-branch return value from `"secret get"` to `"secret get --reveal"` so the identity resolver picks up the correct registry command ID
- Fixed all reveal-branch error paths (`resolveSecretID`, network, HTTP failure) to also return `"secret get --reveal"`

### `cli/internal/app/resource_secrets_test.go`
- Added `TestSecretCommandMachineIdentities`: config-error-path identity checks for `secret update` (`secrets.update`), `secret exec` (`secrets.reveal-batch`), and `secret get` without reveal (`secrets.get`)
- Added `TestSecretGetRevealEnvelopeCommandID`: success-path test verifying `command: "secret get --reveal"` and `command_id: "secrets.reveal"` in the JSON envelope
- Added `TestSecretGetRevealErrorEnvelopeCommandID`: error-path test verifying the same identity when the reveal endpoint returns an HTTP error
- Added `TestSecretUpdateEnvelopeCommandID`: success-path test verifying `command: "secret update"` and `command_id: "secrets.update"` in the JSON envelope

## Validation

- `go test ./internal/app -run 'Secret|Machine'` — all pass
- `make cli-check` — all pass
