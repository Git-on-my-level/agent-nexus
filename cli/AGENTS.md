# CLI Agent Guide

This directory hosts the `oar` CLI module.

## Canonical references

- Root context and overview: `../README.md`
- Module-specific guidance: `../AGENTS.md`
- Canonical API contract: `../contracts/oar-openapi.yaml`
- Generated command metadata: `../contracts/gen/meta/commands.json`
- Core API runbook: `../core/docs/runbook.md`

## Key internal modules

- `internal/app`: root command dispatch and command implementations.
- `internal/config`: flags/env/profile resolution with documented precedence.
- `internal/httpclient`: raw HTTP transport and generated-client wiring.
- `internal/output`: stable JSON output envelope.
- `internal/registry`: embedded command metadata and generated registry adapters.
- `internal/authcli`: non-interactive register/whoami/update/rotate/revoke/token lifecycle service.
- `internal/profile`: profile + key persistence with strict filesystem permissions.
- `internal/streaming`: SSE event frame parser used by `events tail` and `inbox tail`.
- `internal/errnorm`: error normalization and exit-code mapping.

## Runtime invariants

- Non-interactive by default; no prompts.
- In `--json` mode, non-streaming commands emit exactly one JSON object to stdout.
- Exit code `2` is reserved for local usage/input failures.
