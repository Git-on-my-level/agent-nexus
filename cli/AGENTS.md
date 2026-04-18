# AGENTS

## Scope

Guide for work inside `cli/`.

Read this after the root [AGENTS.md](../AGENTS.md). Use child `AGENTS.md` files for narrower local workflows such as dogfood lanes.

## Module Purpose

`cli` is the agent-first command-line runtime for Organization Autorunner.

Its job is to give LLM agents and other automation a stable, non-interactive, contract-aligned way to read state, submit work, and inspect results. The durable value of this module is predictable command behavior, deterministic I/O, and automation-safe ergonomics rather than any specific implementation language or command layout.

## Primary audience

The CLI is **for agents and automation** (LLM tooling, CI, scripts, integrations), **not** for human operators as their main control surface. Humans triage and intervene through **web-ui** (and related human-auth flows such as passkey in the browser). Design and prioritize commands, auth ergonomics, and defaults for **agent principals** (e.g. workspace-local Ed25519 registration, bearer tokens on profiles, invite/bootstrap for new agents). Human-centric HTTP flows may exist on `anx-core` for completeness; they are not the CLI’s primary product story.

## CLI Responsibilities

- Map stable command identities to contract-defined API behavior.
- Optimize for agent and script use: no prompts, no hidden interactivity, and explicit side effects.
- Preserve deterministic I/O across flags, env vars, profiles, stdin, stdout, stderr, and exit codes.
- Provide dual output modes: concise **text by default** (direct consumption, including LLM tool output) and strict **`--json` envelopes** for programmatic use (scripts, services, `jq`).
- Normalize transport and API errors into stable local behavior that orchestrators can reason about.

## Output And Runtime Invariants

- Non-interactive by default.
- In `--json` mode, non-streaming commands emit exactly one JSON envelope to stdout.
- Streaming commands preserve their documented stream framing and resume behavior.
- Exit code `2` remains reserved for local usage and validation failures.
- Default text output (non-JSON) should stay line-oriented and concise rather than depending on rich terminal interaction.
- Remote API failures: stderr prints `Error (<code>): <message>` plus a `Hint:` line when the CLI has recovery guidance. In `--json` mode, the same hint is in `error.hint`, and `error.details.hint` is kept in sync with that value when enrichment runs. `error.details` may include `oar_cli_recovery` (e.g. `kind` values such as `stale_concurrency_token`, `invalid_enum`, `auth_refresh`, `key_mismatch`, `agent_revoked`, `resource_exists`, plus `field`, `schema_enum`, `refresh_cli`, `valid_enum_values`, `reason`, `list_cli`, `register_cli`) as a machine-readable supplement—do not rely on it without checking `kind`. Deeper fields under `error.details.parsed` still mirror the raw API payload.

## What CLI Does Not Own

- Canonical state or schema authority.
- Human-operator dashboards or glanceable monitoring UX.
- Primary human onboarding or identity UX (passkey/WebAuthn and operator workflows belong on human-facing clients).
- Implicit orchestration that hides which API mutations are occurring.

## Canonical References

- Root context: `../README.md`
- Shared contracts: `../contracts/anx-openapi.yaml`, `../contracts/gen/meta/commands.json`
- Runtime and smoke workflows: `docs/runbook.md` (local dev, integration tests, Pi dogfood, release-adjacent notes)
- `anx secret` scripting quirks: `README.md` (Workspace secrets); local invite tokens: `dogfood-resources/README.md`
- Core operations reference: `../core/docs/runbook.md`

## Edit Routing

- Shared API or schema changes start in [../contracts/AGENTS.md](../contracts/AGENTS.md).
- Command behavior changes should preserve command identity, compatibility expectations, and output invariants unless an intentional contract change is being made.
- Auth, profile, transport, output, and streaming changes should be reviewed for automation safety first, then for default text clarity.
- Dogfood-only workflow rules belong in narrower local guides such as [dogfood/pi/AGENTS.md](dogfood/pi/AGENTS.md).

## Validation

- `make cli-check`
- `go test ./...`
- `go test -tags=integration ./integration/...`
- For agent-ergonomics changes, run the relevant dogfood or smoke path described in `docs/runbook.md`.
- When contracts change, run `make contract-gen` and `make contract-check` from repo root.

## Maintenance Guidance

- Keep this file centered on agent ergonomics, runtime boundaries, and stable output rules.
- Put exhaustive command examples and refactor notes in runbooks or generated docs, not here.
- Update this guide when CLI behavior changes in ways that affect automation assumptions.