# CLI Agent Guide

This directory hosts the `oar` CLI module.

## Canonical references

- Product/behavior spec: `../.codex-autorunner/contextspace/spec.md`
- Shared active context: `../.codex-autorunner/contextspace/active_context.md`
- Canonical API contract: `../contracts/oar-openapi.yaml`
- Generated command metadata: `../contracts/gen/meta/commands.json`
- Core API runbook: `../core/docs/runbook.md`

## Key internal modules

- `internal/app`: root command dispatch and command implementations.
- `internal/config`: flags/env/profile resolution with documented precedence.
- `internal/httpclient`: raw HTTP transport and generated-client wiring.
- `internal/output`: stable JSON output envelope.
- `internal/registry`: embedded command metadata and generated registry adapters.
- `internal/errnorm`: error normalization and exit-code mapping.

## Runtime invariants

- Non-interactive by default; no prompts.
- In `--json` mode, non-streaming commands emit exactly one JSON object to stdout.
- Exit code `2` is reserved for local usage/input failures.
