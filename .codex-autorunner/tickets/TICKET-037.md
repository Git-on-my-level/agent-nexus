---
title: "Remove hidden prompts from secret create and update"
agent: codex
done: true
ticket_id: "tkt_cli_secret_no_hidden_prompts_037"
---

## Triage

Priority: P1. The CLI is agent-first and non-interactive by default. Hidden prompts are a high-risk behavior for scripts and tool calls.

## Problem

`anx secret create` and `anx secret update` prompt and read stdin by default when `--from-stdin` is absent, conflicting with deterministic CLI I/O expectations.

## Evidence

- `cli/AGENTS.md:21` and `cli/AGENTS.md:61` require no prompts and non-interactive defaults.
- `cli/internal/app/resource_secrets.go:109` prints `Enter secret value:` and reads stdin for `secret create`.
- `cli/internal/app/resource_secrets.go:262` does the same for `secret update`.
- `cli/README.md:41` already tells scripts to use `--from-stdin`.
- Command used by scout: `nl -ba cli/internal/app/resource_secrets.go cli/README.md cli/AGENTS.md`.

## Proposed Fix

Require `--from-stdin` or an explicit future `--value-file` for secret values. If human convenience must remain, gate it behind an explicit `--prompt` flag and never allow prompting in `--json` mode.

## Validation

- Secret command tests that missing input returns exit code `2` without prompt text.
- Secret command tests that `--from-stdin` behavior is unchanged.
- `go test ./internal/app -run Secret`
- `make cli-check`

## Progress

Completed:

- Removed implicit prompt-and-read fallback from `secret create` and `secret update`; both now return usage error `invalid_request` unless `--from-stdin` is explicitly set.
- Added CLI tests covering exit code `2`, absence of prompt text, and unchanged `--from-stdin` request bodies for create/update.
- Updated `cli/README.md` to document that secret create/update values require `--from-stdin`.

Validated:

- `go test ./internal/app -run Secret`
- `make cli-check`
