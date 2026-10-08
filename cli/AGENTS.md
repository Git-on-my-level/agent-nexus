# AGENTS

## Scope

Guide for work inside `cli/`.

Read this after the root [AGENTS.md](../AGENTS.md). Use child `AGENTS.md` files for narrower local workflows such as dogfood lanes.

## Module Purpose

`cli` is the agent-first command-line runtime for Agent Nexus.

Its job is to give LLM agents and other automation a stable, non-interactive, contract-aligned way to read state, submit work, and inspect results. The durable value of this module is predictable command behavior, deterministic I/O, and automation-safe ergonomics rather than any specific implementation language or command layout.

## Primary audience

The CLI is **for agents and automation** (LLM tooling, CI, scripts, integrations), **not** for human operators as their main control surface. Humans triage and intervene through **web-ui**. A machine enrolls one workspace-local host key; agent principals are derived by name through short-lived host assertion grants. Use `--as` / `ANX_AS`, agentctl run context, or verified harness detection to select the name. Humans grant/revoke agent auth-admin through Access or `auth admins`; explicitly granted agents can approve enrollments, issue headless tokens, and revoke other hosts through the CLI.

## CLI Responsibilities

- Map stable command identities to contract-defined API behavior.
- Optimize for agent and script use: no prompts, no hidden interactivity, and explicit side effects.
- Preserve deterministic I/O across flags, env vars, host credentials, stdin, stdout, stderr, and exit codes.
- Provide dual output modes: concise **text by default** (direct consumption, including LLM tool output) and strict **`--json` envelopes** for programmatic use (scripts, services, `jq`).
- Normalize transport and API errors into stable local behavior that orchestrators can reason about.

## Domain Model And Verbs

The CLI should teach the same workspace model as the web UI:

- **Inbox** is human attention (decisions, blocked/stale work, material events). Use `anx inbox ...`.
- **Work / Tasks** is the projection over commitments. Prefer `anx work list|get|create` and public `card:<handle>` refs. `anx cards move` is the write for Nexus-owned phase/rank changes; source-owned changes go through PM decisions.
- **Docs** are durable context and institutional knowledge. `docs create` and `docs revise` should prefer local files via `--body-file` (or inline `--body` on create).
- **Topics** remain the backing discussion/context primitive. Use `topics message/messages/reply` when you are already on a topic thread.
- **Boards and cards** are the store behind Tasks, not a second product. Use `anx cards ...` for card create, list, get, message, assign, move, revise, resolve, reopen, and lifecycle. `cards create` and `cards revise` should prefer `--body-file` (or inline `--body`). Board-scoped `boards cards list|get|create-batch` is only for board-scoped reads or batch JSON creation; do not add card workflow verbs under `boards cards`.
- **Domain messages** are ordinary conversation/status updates on a Topic, Document, or Card backing thread. Use `topics message/messages/reply`, `docs message/messages/reply`, and `cards message/messages/reply` for agent authoring; use `--body-file` for file-backed message bodies; do not make agents hand-author `message_posted` event JSON for routine domain discussion.

Use the smallest domain verb that says what is happening:

- `create` creates durable resources.
- `revise` changes text-heavy content bodies from local files, currently Docs and Cards.
- `patch` changes resource metadata fields.
- `move` changes Card workflow position or column.
- `message`, `messages`, and `reply` cover first-class domain conversation authoring/reading and fill the backing thread/event details for agents.
- `assign`, `resolve`, and `reopen` are Card workflow verbs with domain meaning.
- `workspace` is the composed read for an agent that needs the useful surrounding context.

Run `anx config workspaces` when unsure which workspace applies. Use `anx config use <alias>` for a user-global default or `anx config map "~/work/project/**" <alias>` for a directory rule. For one invocation, use `--workspace <alias>`. Never hardcode `--base-url` in agent prompts; workspace preferences live outside git repositories.

### Flag conventions

- **Lifecycle verbs:** `archive`, `unarchive`, `trash`, `restore`, and `purge` (where supported) share one parser-driven surface across artifacts, boards, docs, events, cards, and topics: optional `--from-file` JSON, `--reason`, `--actor-id` (except `purge`), and `--dry-run`, plus the resource id as a leading positional or `--<resource>-id`. The canonical verb matrix per resource is `internal/app/lifecycle_spec.go`; `--reason` / `--actor-id` overlay JSON from `--from-file` when both are supplied.
- **Body (human prose / file bytes):** `--body` for inline text; `--body-file <path|->` for a path or stdin (`-`). Use `--from-file` only for advanced JSON mutation bodies.
- **Resource id:** the leading positional accepts a ref, handle, or internal id. The explicit flag form is `--<resource>-id` (for example `--card-id`, `--board-id`). Do not introduce bare `--card` / `--board` flags as alternate spellings of id flags; that short flag space stays reserved for relational anchors (for example `--board` on `cards create` names the parent board).
- **`--reason` vs `--body`:** on lifecycle verbs, `--reason` is a short audit string stamped on the lifecycle/trash event. `--body` / `--body-file` are for evidence or narrative posted on a backing thread (for example card messages), not aliases of `--reason`.
- **Concurrency tokens:** when a command documents `--if-updated-at`, `--if-board-updated-at`, or similar, its help text names the `anx ... get` command that yields the correct token.

Do not add compatibility aliases for new CLI surfaces. Transitional aliases removed in the CLI ergonomics consolidation include: `--content-file`, `--json-file`, and bare `--card` / `--board` as duplicates of `--<resource>-id` on the same command.

If an old command path conflicts with this model, prefer a clean replacement and update help, generated metadata, tests, and this guide together.

## Output And Runtime Invariants

- Non-interactive by default.
- In `--json` mode, non-streaming commands emit exactly one JSON envelope to stdout.
- Streaming commands emit one envelope v2 per event and preserve resume behavior.
- JSON output uses envelope v2: success `{ok, schema_version:2, command, result, warnings:[], next_actions:[]}` and errors `{ok:false, schema_version:2, error:{code,message,retryable,exit_code,details,next_actions}, warnings:[], next_actions:[]}`.
- Exit codes are 0 success, 2 usage, 3 not found, 4 conflict, 5 auth, 6 network/unavailable, 7 outdated, 8 timeout, 9 rejected, 10 needs_context, 11 withdrawn, 12 expired, and 1 other.
- Default text is a projection of the same document: one fact per line with a lead token and `key=value` fields, plus `warning code=…` and runnable `next <argv>` lines.
- Default text output is the preferred agent readback mode. Use JSON for code/script parsing, CI, or `jq`, not as the default way to inspect state.
- Public refs/handles are the primary identity contract. Board-card text rows lead with the card ref/title and show assignees; backing `thread_ref` or `thread_id` is available in JSON.
- Remote API failures use the same renderer as success. `error.details.hint` carries supplementary human guidance; runnable repairs belong in `error.next_actions`. `error.details` may include `anx_cli_recovery` with a typed `kind` and fields for the specific repair. Deeper fields under `error.details.parsed` mirror the raw API payload.
- Usage and command-shape errors must beat host/config resolution whenever they can be detected without side effects. When adding a command or flag, update `internal/app/command_usage_preflight.go` alongside the real parser/help so unenrolled hosts see `invalid_flags` or `unknown_subcommand` instead of a misleading identity error.

## What CLI Does Not Own

- Canonical state or schema authority.
- Human-operator dashboards or glanceable monitoring UX.
- Primary human onboarding or identity UX (passkey/WebAuthn and operator workflows belong on human-facing clients).
- Implicit orchestration that hides which API mutations are occurring.

## Canonical References

- Root context: `../README.md`
- Shared contracts: `../contracts/anx-openapi.yaml`, `../contracts/gen/meta/commands.json`
- Runtime and smoke workflows: `docs/runbook.md` (host enrollment, agentctl runs ingestion, local dev, integration tests)
- `anx secret` scripting quirks: `README.md` (Workspace secrets); local invite tokens: `dogfood-resources/README.md`
- Core operations reference: `../core/docs/runbook.md`

## Edit Routing

- Shared API or schema changes start in [../contracts/AGENTS.md](../contracts/AGENTS.md).
- Command behavior changes should preserve command identity, compatibility expectations, and output invariants unless an intentional contract change is being made.
- Auth, host identity, transport, output, and streaming changes should be reviewed for automation safety first, then for default text clarity.
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

## Adding result actions

Add command side effect classification in `internal/app/output_contract.go` and generated command classification in `internal/registry/registry.go`. Add bounded, concrete result-state rules to `deriveNextActions`: inspect the flattened result, require a real ref or cursor, and construct exact argv accepted by the parser. Include `mutates` and `side_effect_class` through the shared `action` constructor. Add error repairs to `deriveErrorActions`, warnings to `resultWarnings`, and verify both JSON and text projection. Never infer a target from a title or a partial ref.
