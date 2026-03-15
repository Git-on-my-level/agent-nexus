# AGENTS

## Scope
Guide for work inside `cli/`.

Read this after the root [AGENTS.md](../AGENTS.md). Use child `AGENTS.md` files for narrower local workflows such as dogfood lanes.

## Module Purpose
`cli` is the agent-first command-line runtime for Organization Autorunner.

Its job is to give LLM agents and other automation a stable, non-interactive, contract-aligned way to read state, submit work, and inspect results. The durable value of this module is predictable command behavior, deterministic I/O, and automation-safe ergonomics rather than any specific implementation language or command layout.

## CLI Responsibilities
- Map stable command identities to contract-defined API behavior.
- Optimize for agent and script use: no prompts, no hidden interactivity, and explicit side effects.
- Preserve deterministic I/O across flags, env vars, profiles, stdin, stdout, stderr, and exit codes.
- Provide dual output modes: concise human-readable summaries by default and strict machine-readable `--json` envelopes for automation.
- Normalize transport and API errors into stable local behavior that orchestrators can reason about.

## Output And Runtime Invariants
- Non-interactive by default.
- In `--json` mode, non-streaming commands emit exactly one JSON envelope to stdout.
- Streaming commands preserve their documented stream framing and resume behavior.
- Exit code `2` remains reserved for local usage and validation failures.
- Human-readable output should stay text-friendly and concise rather than depending on rich terminal interaction.

## What CLI Does Not Own
- Canonical state or schema authority.
- Human-operator dashboards or glanceable monitoring UX.
- Implicit orchestration that hides which API mutations are occurring.

## Canonical References
- Root context: `../README.md`
- Shared contracts: `../contracts/oar-openapi.yaml`, `../contracts/gen/meta/commands.json`
- Runtime and smoke workflows: `docs/runbook.md`
- Core operations reference: `../core/docs/runbook.md`

## Edit Routing
- Shared API or schema changes start in [../contracts/AGENTS.md](../contracts/AGENTS.md).
- Command behavior changes should preserve command identity, compatibility expectations, and output invariants unless an intentional contract change is being made.
- Auth, profile, transport, output, and streaming changes should be reviewed for automation safety first, then for human readability.
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
