# ANX Runtime Help Reference

This reference is bundled with the CLI. Print the full document with `anx meta docs` or one topic with `anx meta doc <topic>`.

## Topics

- `onboarding` (manual): Offline quick-start mental model and first command flow.
- `concepts` (manual): Quick guide to the core ANX primitives and when to use each.
- `agent-guide` (manual): Prescriptive agent guide for choosing ANX primitives, operating safely, and automating the CLI well.
- `agent-bridge` (manual): Install, configure, and operate the preferred per-agent `anx-agent-bridge` runtime (local adapter + check-in); workspace wake routing still lives in `anx-core`.
- `wake-routing` (manual): How `@handle` wake routing works, including self-registration, verification, and troubleshooting.
- `draft` (manual): Local draft staging, listing, commit, and discard workflow.
- `provenance` (manual): Deterministic provenance walk reference and examples.
- `auth whoami` (manual): Validate the active profile, print resolved identity metadata, and point agents at wake-registration next steps.
- `auth list` (manual): List local CLI profiles and the active profile.
- `auth default` (manual): Persist the default CLI profile used when no explicit agent is selected.
- `config use` (manual): Set the active CLI profile used when --agent and ANX_AGENT are omitted.
- `config show` (manual): Print effective CLI settings and per-field sources (tokens redacted).
- `config unset` (manual): Clear the persisted default profile marker (~/.config/anx/default-profile).
- `auth update-username` (manual): Rename the authenticated agent and sync the local profile.
- `auth rotate` (manual): Rotate the active agent key and refresh stored credentials.
- `auth revoke` (manual): Revoke the active agent and mark the local profile revoked. Use explicit human-lockout flags only for break-glass recovery.
- `auth token-status` (manual): Inspect whether the local profile still has refreshable token material.
- `bridge` (manual): CLI-managed bridge bootstrap helpers for installing, templating, and checking `anx-agent-bridge`.
- `import` (manual): Prescriptive import guide for building low-duplication, discoverable ANX graphs from external material.
- `auth` (group): Register, inspect, and manage auth state
- `topics` (group): Manage durable work subjects
- `cards` (group): Manage board-scoped cards
- `threads` (group): Read-only backing-thread inspection (tooling and diagnostics)
- `artifacts` (group): Manage artifact resources and content
- `boards` (group): Manage board resources and ordered cards
- `docs` (group): Manage long-lived docs and revisions
- `events` (group): Manage events and event streams
- `inbox` (group): List/get/ack/stream inbox items
- `receipts` (group): Create receipt packets (subject_ref must be card:<card_id>)
- `reviews` (group): Create review packets (subject_ref + receipt_ref; subject_ref must be card:<card_id>)
- `derived` (group): Run derived-view maintenance actions
- `meta` (group): Inspect generated command/concept metadata
- `auth invites list` (command): List invite tokens
- `auth invites create` (command): Create invite token
- `auth invites revoke` (command): Revoke invite
- `auth bootstrap status` (command): Bootstrap registration availability
- `threads list` (command): List backing threads
- `threads get` (command): Inspect backing thread
- `threads timeline` (command): Get backing thread timeline
- `threads context` (command): Get backing thread coordination context
- `topics list` (command): List topics
- `topics get` (command): Get topic
- `topics create` (command): Create topic
- `topics patch` (command): Patch topic
- `topics timeline` (command): Get topic timeline
- `topics workspace` (command): Get topic workspace (primary operator coordination read)
- `topics archive` (command): Archive topic
- `topics unarchive` (command): Unarchive topic
- `topics trash` (command): Move topic to trash
- `topics restore` (command): Restore topic from trash
- `cards list` (command): List cards
- `cards get` (command): Get card
- `cards create` (command): Create card (global path)
- `cards patch` (command): Patch card
- `cards move` (command): Move card
- `cards archive` (command): Archive card
- `cards trash` (command): Move card to trash
- `cards purge` (command): Permanently delete archived or trashed card
- `cards restore` (command): Restore archived or trashed card
- `cards timeline` (command): Get card timeline
- `artifacts list` (command): List artifacts
- `artifacts get` (command): Get artifact metadata
- `artifacts create` (command): Create artifact
- `artifacts archive` (command): Archive artifact
- `artifacts unarchive` (command): Unarchive artifact
- `artifacts trash` (command): Move artifact to trash
- `artifacts restore` (command): Restore artifact from trash
- `artifacts purge` (command): Permanently delete trashed artifact
- `boards list` (command): List boards
- `boards create` (command): Create board
- `boards get` (command): Get board
- `boards archive` (command): Archive board
- `boards unarchive` (command): Unarchive board
- `boards trash` (command): Move board to trash
- `boards restore` (command): Restore board from trash
- `boards purge` (command): Permanently delete trashed board
- `boards cards` (group): Nested generated help topic.
- `boards cards create` (command): Create card on board
- `boards cards create-batch` (command): Batch create cards on board
- `boards cards get` (command): Get board-scoped card
- `docs list` (command): List documents
- `docs create` (command): Create document
- `docs get` (command): Get document
- `docs trash` (command): Move document to trash
- `docs archive` (command): Archive document
- `docs unarchive` (command): Unarchive document
- `docs restore` (command): Restore document from trash
- `docs purge` (command): Permanently delete trashed document
- `events get` (command): Get event by id
- `events create` (command): Create event
- `events stream` (command): Stream events (SSE)
- `events tail` (command): Stream events (SSE)
- `events archive` (command): Archive event
- `events unarchive` (command): Unarchive event
- `events trash` (command): Move event to trash
- `events restore` (command): Restore event from trash
- `inbox list` (command): List inbox items
- `inbox get` (command): Get one inbox item
- `inbox acknowledge` (command): Acknowledge inbox item
- `inbox ack` (command): Acknowledge inbox item
- `inbox stream` (command): Stream inbox items (SSE)
- `inbox tail` (command): Stream inbox items (SSE)
- `derived rebuild` (command): Rebuild derived projections
- `meta commands` (command): List command registry metadata
- `meta command` (command): Get one command metadata entry
- `meta concepts` (command): List concept index
- `meta concept` (command): Get commands grouped by concept
- `receipts create` (command): Create receipt packet
- `reviews create` (command): Create review packet
- `secret list` (command): List secrets
- `secret create` (command): Create secret
- `secret delete` (command): Delete secret
- `secret get --reveal` (command): Reveal secret value
- `secret exec` (command): Reveal multiple secrets by name
- `secret update` (command): Update secret value
- `events list` (local-helper): Compose backing-thread timeline reads with client-side thread/type/actor filters and preview summaries.
- `events validate` (local-helper): Validate an `events create` payload locally from stdin or `--from-file` without sending it.
- `events explain` (local-helper): Explain known event-type conventions, required refs, and validation hints, including when `message_posted` targets a backing-thread message stream.
- `artifacts inspect` (local-helper): Fetch artifact metadata and resolved content in one command for operator inspection.
- `threads inspect` (local-helper): Diagnostic backing-thread bundle: compose one view from read-only thread data and related `inbox list` items.
- `threads workspace` (local-helper): Read-only backing-thread workspace projection: context, inbox, recommendation review, and related-thread signals in one command.
- `threads recommendations` (local-helper): Compose a diagnostic recommendation-oriented review of one backing thread with related follow-up context.
- `boards workspace` (local-helper): Canonical board read path: load one board's workspace: optional primary topic, cards by column, linked documents, inbox items, and summary.
- `boards cards list` (local-helper): List all cards on a board in canonical column order without hydrating thread details.
- `docs propose-update` (local-helper): Stage a document update proposal locally and show the content diff before applying it.
- `docs content` (local-helper): Show the current document content together with authoritative head revision metadata.
- `docs validate-update` (local-helper): Validate a `docs.revisions.create` payload locally from stdin or file without sending the mutation.
- `docs apply` (local-helper): Apply a previously staged document update proposal.
- `meta skill` (local-helper): Render a bundled editor-specific skill file from the canonical ANX agent guide.
- `bridge install` (local-helper): Install `anx-agent-bridge` into a dedicated Python 3.11+ virtualenv and expose a PATH wrapper.
- `bridge import-auth` (local-helper): Copy an existing `anx` profile and key into bridge auth state for one bridge config.
- `bridge init-config` (local-helper): Write a minimal agent bridge TOML config with the pending-until-check-in lifecycle baked in.
- `bridge workspace-id` (local-helper): Discover durable workspace ids from an existing agent wake registration.
- `bridge doctor` (local-helper): Validate bridge install, config presence, and registration readiness without starting the daemon.
- `bridge start` (local-helper): Start a managed bridge daemon for one config file.
- `bridge stop` (local-helper): Stop a managed bridge daemon for one config file.
- `bridge restart` (local-helper): Restart a managed bridge daemon for one config file.
- `bridge status` (local-helper): Inspect managed process state for a bridge config.
- `bridge logs` (local-helper): Read recent log lines for a managed bridge config.
- `import scan` (local-helper): Scan a folder or zip archive into a normalized inventory with text cache, repo-root hints, and cluster hints.
- `import dedupe` (local-helper): Create exact and probable duplicate reports from a scan inventory with conservative skip recommendations.
- `import plan` (local-helper): Build a conservative import plan that prefers collector threads, hub docs, dedupe-first writes, and low orphan rates.
- `import apply` (local-helper): Write payload previews for a plan and optionally execute topic/artifact/doc creates in dependency order.


## `onboarding`

Offline quick-start mental model and first command flow.

```text
Onboarding: first steps (agents / automation)

This CLI is for agent principals. For the full operating model, read `anx meta doc agent-guide`.

1. Point the CLI at the core API with `--base-url` or `ANX_BASE_URL`.
2. Choose a profile name and pass it with `--agent` (or `ANX_AGENT`) for registration and first checks below.
3. Run `anx doctor`, then `anx auth bootstrap status` to see whether first-principal bootstrap is still open on this workspace.
4. Register the agent profile:
   - If bootstrap is available: `anx auth register --username <username> --bootstrap-token <token>` (token comes from workspace operators / deployment).
   - If bootstrap is closed: obtain a one-time invite (`auth invites create --kind agent` from an already-authorized principal on that workspace), then `anx auth register --username <username> --invite-token <token>`.
5. On a machine where `~/.config` persists, set the active profile once: `anx config use <agent>` (same as `anx auth default <agent>`). Later commands can omit `--base-url` / `--agent`; use `anx config show` to verify. For CI or ephemeral environments, keep using env vars or flags instead.
6. Confirm with `anx auth whoami`, run a cheap read (`topics list`), then mutate deliberately.
7. Use `anx meta skill cursor` to export a bundled Cursor skill from the shipped guide if desired.
8. Read `anx meta doc wake-routing` if this agent should be wakeable via thread-message `@handle` mentions.

First commands to run

  anx --base-url http://127.0.0.1:8000 --agent <agent> doctor
  anx --base-url http://127.0.0.1:8000 --agent <agent> auth bootstrap status
  anx --base-url http://127.0.0.1:8000 --agent <agent> auth register --username <username> --bootstrap-token <token>   # only when bootstrap is open
  anx --base-url http://127.0.0.1:8000 --agent <new-agent> auth register --username <username> --invite-token <token>   # when bootstrap is closed
  anx config use <agent>   # optional after register: shorter commands on this machine (same as: anx auth default <agent>)
  anx --agent <agent> auth whoami
  anx --agent <agent> topics list
  anx --agent <agent> inbox stream --max-events 1

Next step

  anx meta doc agent-guide
  anx meta doc wake-routing
```

## `concepts`

Quick guide to the core ANX primitives and when to use each.

```text
ANX concepts guide

Use this command when you need to decide which primitive fits the use case before you start issuing writes.

Selection rules:
- Use events for immutable facts.
- For decision lifecycle events, always include `thread:<thread_id>` in refs; do not rely on `topic:` alone or `topic:<thread_id>` as a thread substitute.
- Use topics for durable work subjects and primary operator coordination (`topics workspace`).
- Use cards for board-scoped planning and movement.
- Use threads for read-only backing-thread diagnostics and timeline inspection — not as the default coordination surface.
- Use docs for narrative knowledge that should be revised over time.
- Use boards for cross-object workflow views, not source-of-truth content.
- Use inbox for current attention signals from the active CLI identity's perspective.
- Use draft when you want a local review checkpoint before a write.

topics
- Use when: You need the durable work subject itself with ownership, summary, related refs, and provenance — including the primary operator coordination read.
- Not for: Board-scoped card placement or low-level backing-thread-only diagnostics.
- Examples: initiatives, incidents, cases, deliverables
- Read next: anx topics list ; anx topics get ; anx topics workspace

threads
- Use when: You need read-only backing-thread diagnostics: timelines, raw thread records, or thread-scoped projection bundles for troubleshooting.
- Not for: Primary operator triage when a topic exists — use topics workspace instead.
- Examples: backing thread timeline, diagnostic workspace projection, compatibility inspection
- Read next: anx threads list ; anx threads inspect ; anx threads workspace

cards
- Use when: You need board-scoped planning items with column, rank, assignee, and move/update operations.
- Not for: The durable subject record or append-only event history.
- Examples: board cards, tracked cards, workflow cards
- Read next: anx cards list ; anx cards get ; anx cards move

events
- Use when: You need immutable facts, observations, decisions, or updates in an auditable sequence. Decision lifecycle events (`decision_needed`, `intervention_needed`, `decision_made`) must include `thread:<thread_id>` in refs; optional `topic:` refs are cross-links only, not a substitute for the thread anchor.
- Not for: Replacing the current durable state of a work object.
- Examples: decision_needed, decision_made, message_posted, exception_raised
- Read next: anx events list ; anx events explain ; anx threads timeline

docs
- Use when: You need long-lived narrative knowledge that should be revised, read, and referenced as a document.
- Not for: Ephemeral chat-like updates or board membership.
- Examples: plans, notes, decision records, runbooks
- Read next: anx docs list ; anx docs get ; anx docs content

boards
- Use when: You need a coordination view across multiple work items with explicit workflow columns and ordering.
- Not for: Being the source of truth for the work itself.
- Examples: triage board, release board, initiative tracking board
- Read next: anx boards list ; anx boards workspace ; anx boards cards list

inbox
- Use when: You need the derived queue of what currently needs attention from the active actor's perspective.
- Not for: Durable automation contracts or historical truth.
- Examples: pending decisions, exceptions, stalled work
- Read next: anx inbox list ; anx inbox get ; anx inbox ack

draft
- Use when: You want to stage a mutation locally, inspect it, then apply it explicitly.
- Not for: Read paths or append-only event authoring.
- Examples: reviewable thread patches, reviewable doc updates
- Read next: anx draft create ; anx draft list ; anx draft commit

Inbox categories:
- `action_needed`: A human must decide, take direct action, or own the next step (includes prior decision and intervention queue signals).
- `risk_exception`: Exceptions, stale cadence, or at-risk work items that need follow-up.
- `attention`: Review or lighter operator focus (for example document attention).

For the fuller operating model, read `anx meta doc agent-guide`.
```

## `agent-guide`

Prescriptive agent guide for choosing ANX primitives, operating safely, and automating the CLI well.

```text
Agent guide

Use this guide when you need to operate `anx` well, not just get it running. Favor stable CLI patterns over environment-specific setup.

Operating posture

- Treat `anx` as the contract-aligned interface to an ANX core API.
- Prefer read-before-write: inspect state, choose the right object, then mutate deliberately.
- Prefer **default (non-JSON) output** for normal agent work: concise text for direct consumption, usually fewer tokens than JSON envelopes.
- Use **`--json`** or **`ANX_JSON=true`** when the consumer is code, a shell script, CI, or anything that parses the stable JSON envelope (including rich `error.details`).
- Prefer profiles and env vars over repeated flags.
- Prefer discovery from the CLI itself over memorizing exact subcommands.


Core model

- `events`: immutable facts, observations, and updates. Use for append-only activity, audit trails, and streams. Decision lifecycle events (`decision_needed`, `intervention_needed`, `decision_made`) require `thread:<thread_id>` in refs; `topic:` refs are optional context when a topic exists.
- `topics`: the primary durable work subjects. Use them as the main organizational root for initiatives, incidents, cases, processes, relationships, and similar work.
- `cards`: the primary work items. Use them for tracked execution on boards.
- `threads`: backing timelines and packet-routing infrastructure. Use them for read-only diagnostics, low-level inspection, and wake/tooling flows rather than normal coordination.
- `inbox`: work intake and notifications. Use to see what needs attention and ack handled items.
- `draft`: staged or reviewable mutations. Use when a write should be inspected before commit.
- `docs`: long-lived narrative knowledge. Use for plans, notes, decisions, summaries, and shared context.
- `boards`: structured coordination views. Use to group and review work across multiple objects.
- `auth` and profiles: identity plus reusable config.
- `meta` and help: runtime discovery for commands, concepts, and bundled docs.

Heuristic:
- Use `events` for facts.
- Use `topics` for ongoing work, ownership, and operator coordination.
- Use `cards` for concrete tracked execution and delivery state.
- Use `docs` for narrative or reference material.
- Use `boards` for portfolio or workflow visibility.
- Use `threads` only when you need backing-timeline diagnostics or tooling-specific inspection.
- Use `draft` when you want a checkpoint before applying change.

If a new primitive or abstraction is added, place it in the same model: what durable role it plays, what it organizes, and whether it is mainly for facts, work, knowledge, or views.


Higher-level concepts

- `docs` are the long-lived narrative layer. Use them when information should be read as a document, revised over time, or referenced by many work items.
- `boards` are coordination views. Use them to group, prioritize, and review work across multiple objects rather than to store source-of-truth content themselves.
- `threads` back topics, cards, boards, and documents; `docs` explain; `boards` organize. Keep those roles distinct.


Standard workflow

1. Confirm environment and identity.
2. Discover current state with list/get/context commands.
3. Decide which primitive matches the task.
4. Make the smallest valid mutation.
5. Verify via read commands, timeline, stream, or resulting state.

For interrupt-driven work, a common loop is: `inbox` -> inspect the related `topic`, `card`, or `doc` -> apply change directly or via `draft` -> verify -> ack inbox item. Reach for `threads ...` only when you need backing-thread diagnostics.


Configuration

- On a durable workstation, set the active profile once with `anx config use <profile>` (equivalent to `anx auth default <profile>`). Later commands can omit repeated `--base-url` / `--agent`; inspect merged settings with `anx config show` (tokens redacted).
- Override per command with `--base-url` or `ANX_BASE_URL` and `--agent` or `ANX_AGENT` when needed.
- Prefer `ANX_BASE_URL` and `ANX_AGENT` in scripts, CI, or environments without a persistent `~/.config/anx`.
- If available, run `anx doctor` when config or connectivity is unclear.
- If a request behaves like it hit the wrong service, confirm you are pointing at the core API, not another surface.

Config precedence is typically: flags -> environment -> profile -> defaults.


Discovery first

Do not overfit to examples in this guide. Ask the CLI what exists now:

  anx help
  anx help <group>
  anx help <group> <command>
  anx meta docs
  anx meta doc <topic>
  anx meta doc wake-routing

Use help output as the source of truth for exact flags, request shapes, enums, and newly added primitives.


Command habits

- Use list/get/context/workspace commands to orient before editing.
- Default text and JSON list payloads use a 10-character `short_id` prefix; the CLI resolves that prefix to a canonical id when you pass it back into commands. Use `--full-id` when a value is ambiguous or you need the full id for copy/paste.
- In default text resource lists (threads, boards, topics, etc.), the first column may show a short scan label derived after the type prefix (not the same as `short_id`); use `--json` `id`/`short_id` or `--full-id` when passing ids back into commands.
- Use streaming commands for live observation; bound them with `--max-events` when scripting.
- Use `draft` or proposal/apply flows when the CLI exposes them and the change benefits from reviewability.
- Prefer narrow filters over broad listings when triaging large state.


Programmatic output (`--json`)

- Use `--json` or `ANX_JSON=true` when you are parsing output in code or scripts (not for default agent readbacks).
- Parse the response envelope; do not assume the same shape for default text output.
- Treat `error.code`, `error.message`, `hint`, and `recoverable` as the control surface for retries and repair.
- Keep scripts idempotent where possible: read state, compare, then write only when needed.


Onboarding and recovery

When starting in a new environment:

1. Set base URL.
2. Check onboarding state with `anx auth bootstrap status` before first registration.
3. Register the first principal with `anx auth register --username <username> --bootstrap-token <token>` or later principals with `--invite-token <token>`.
4. Confirm identity.
5. Run a cheap read command.
6. If this agent should be tag-addressable from thread messages, read `anx meta doc agent-bridge` for the preferred runtime path or `anx meta doc wake-routing` for the generic document lifecycle.

When stuck:

- Re-run with `--json` when structured failure fields (`error.details`, etc.) would help.
- Check help for the exact command path you are using.
- Verify auth, base URL, and profile resolution before debugging payload shape.


Maintenance rule

- Keep this guide focused on durable usage patterns.
- Describe roles and decision rules, not exhaustive command inventories.
- Prefer `anx help` and `anx meta docs` over embedding fragile schemas.
- Mention examples of primitives and abstractions, but avoid implying the list is closed.
```

## `agent-bridge`

Install, configure, and operate the preferred per-agent `anx-agent-bridge` runtime (local adapter + check-in); workspace wake routing still lives in `anx-core`.

```text
Agent bridge

Use this when you want the preferred per-agent bridge path for wake registration and live `@handle` delivery.

What changed

- The main CLI now owns the per-agent bootstrap path for fresh machines:
  - `anx bridge install`
  - `anx bridge import-auth`
  - `anx bridge init-config`
  - `anx bridge start|stop|restart|status|logs`
  - `anx bridge workspace-id`
  - `anx bridge doctor`
- The Python package still owns runtime behavior:
  - `anx-agent-bridge auth register`
  - `anx-agent-bridge bridge run` under the hood
  - `anx-agent-bridge notifications list|read|dismiss` for bridge-local pull flows
- The workspace wake-routing service is deployment-owned and runs inside `anx-core`, not through `anx bridge`.
- Registrations become taggable once the registration and workspace binding are valid. Fresh bridge check-in only controls whether delivery is immediate.

Install on a fresh machine with only `anx`

1. Install the bridge runtime into a managed Python `3.11+` virtualenv:

  anx bridge install

  By default, this installs the bridge package at the same git ref as your `anx` release tag and writes the launcher into `~/.local/bin`. Use `--ref main` when you need the latest default-branch commit ahead of that tag. Override `--bin-dir` if needed. The current bootstrap path also requires `git` on PATH.

2. If you need bridge test dependencies on the same machine:

  anx bridge install --with-dev

3. Verify the wrapper works:

  anx-agent-bridge --version

Contributor path from a repo checkout

- For local development inside this repo, prefer:
  - `make setup`
  - `make doctor`
  - `make test`
- Local contributor rules for the adapter live in `adapters/agent-bridge/AGENTS.md`.

Config generation

Generate minimal configs from the CLI:

  anx bridge init-config --kind subprocess --output ./agent.toml --workspace-id <workspace-id> --handle <handle> --adapter-entrypoint ./adapter.py
  anx bridge init-config --kind python-plugin --output ./agent.toml --workspace-id <workspace-id> --handle <handle> --plugin-module my_bridge --plugin-factory build_adapter

You own the adapter implementation. ANX does not ship or maintain integrations for specific third-party agents.

These templates intentionally default the agent lifecycle to:

- `status = "pending"`
- `checkin_interval_seconds = 60`
- `checkin_ttl_seconds = 300`

That is the guardrail for live delivery: the bridge still needs to check in before the agent shows online, but humans can tag a valid offline registration and let notifications queue.

Workspace id source of truth

- `<workspace-id>` must be the durable workspace id for the deployment, not a slug and not a UI path segment.
- If the agent already has wake registration metadata, use `anx bridge workspace-id --handle <handle>` to read its enabled workspace bindings first.
- If the workspace deployment already documents the configured `workspace_id`, copy that exact value.
- If the deployment is driven by control-plane workspace records, copy the durable `workspace_id` from that workspace record, not the slug.
- The bundled example value `ws_main` is only a sample.
- If you still do not know the real workspace id for your deployment, stop and ask the operator. Do not guess.

First-time agent-host path

1. Install the runtime:

  anx bridge install

2. Render the agent config and implement the adapter (see `anx-agent-bridge adapter contract --config ./agent.toml`):

  anx bridge init-config --kind subprocess --output ./agent.toml --workspace-id <workspace-id> --handle <handle> --adapter-entrypoint ./adapter.py

3. If a matching `anx` profile already exists for the target principal, import it into the bridge config:

  anx bridge import-auth --config ./agent.toml --from-profile <agent>

  This also syncs the default local `[anx].base_url` in the bridge config to the imported profile when they differ.

4. Register the target bridge principal and write the initial pending registration when auth does not already exist:

  anx-agent-bridge auth register --config ./agent.toml --invite-token <token> --apply-registration

5. Start the managed bridge daemon from the main CLI:

  anx bridge start --config ./agent.toml

6. Confirm the process and readiness state before expecting immediate delivery:

  anx bridge status --config ./agent.toml
  anx bridge doctor --config ./agent.toml

  Use `anx bridge logs --config ./agent.toml` when you need the recent daemon output, and `anx bridge restart --config ./agent.toml` if you change config or recover from a stale process.

  The doctor should report both adapter readiness and the bridge as online for immediate delivery. If it still says offline, stale, or adapter probe failed, tags will queue notifications until you fix that.

7. Post a test wake message containing `@<handle>`.

8. Confirm the durable trace:
  - `message_posted`
  - `agent_wakeup_requested`
  - if online, `agent_wakeup_claimed`
  - if online, bridge reply `message_posted`
  - if online, `agent_wakeup_completed`
  - if offline, the notification remains queued until the bridge reconnects

9. Pull or dismiss queued notifications directly when needed:

  anx notifications list --status unread
  anx notifications dismiss --wakeup-id <wakeup-id>
  anx-agent-bridge notifications list --config ./agent.toml --status unread

10. If the bridge is online but tagged delivery still fails, hand off to the workspace operator to inspect the embedded wake-routing sidecar in `anx-core`.

Lifecycle note

- `anx-agent-bridge registration apply` updates the agent principal registration, but the bridge runtime still owns live presence updates.
- The bridge runtime refreshes registration readiness on check-in.
- If the bridge stops checking in, the registration stays taggable but delivery falls back to queued notifications until the bridge returns.
- The preferred operational path is to manage the bridge daemon with `anx bridge start|stop|restart|status|logs`, not ad hoc shell backgrounding.

Troubleshooting

- `anx-agent-bridge: command not found`:
  - run `anx bridge install` or add the managed wrapper directory to PATH
- bridge doctor says the bridge is offline:
  - the bridge has not checked in yet or is no longer refreshing; start or restart `anx bridge start --config ./agent.toml` and verify the config points at the right workspace
- wake request is durable but never claimed:
  - the bridge is offline, the embedded wake-routing sidecar in `anx-core` is unhealthy, or `workspace_id` is wrong
- principal exists but wake still fails:
  - inspect the principal registration for actor mismatch, disabled status, stale check-in, or missing workspace binding

Related docs

  anx help bridge
  anx meta doc wake-routing
  anx bridge doctor --config ./agent.toml
```

## `wake-routing`

How `@handle` wake routing works, including self-registration, verification, and troubleshooting.

```text
Wake routing

Use this when you want humans or agents to wake other agents from thread messages by tagging `@handle`.

How it works

- Wake routing is provided by a workspace-owned sidecar hosted inside `anx-core`, not by the per-agent CLI.
- The durable wake registration now lives on the agent principal metadata, not in `docs`.
- The bridge-owned readiness proof is the latest `agent_bridge_checked_in` event referenced by that principal registration.
- A tagged message becomes durable wake work when the target agent is registered for the workspace. Bridge readiness only changes whether delivery is immediate or queued.

What counts as taggable

- principal kind is `agent`
- principal is not revoked
- principal has a username/handle
- principal has wake registration metadata
- registration `actor_id` matches the principal actor
- registration has an enabled binding for the current workspace
- registration status is not `disabled` (often `pending` until the first bridge check-in, then `active`)

What counts as online

- the agent is already taggable
- registration records a bridge check-in event id
- that `agent_bridge_checked_in` event exists, matches the same actor, and has a fresh bridge check-in window

Important lifecycle rule

- Bridge-managed registrations still start as `pending` until the bridge checks in and finalizes the live registration payload.
- Once registration and workspace binding are valid, humans can tag the agent even if the bridge is offline.
- If the bridge stops checking in, the agent becomes offline but remains taggable; pending notifications queue until the bridge returns.

How humans discover it

- In the web UI Access page, look for registered agent principals and their `@handle`.
- `Online` means immediate delivery is available now. `Offline` means tags still queue durable notifications for later delivery.

How agents discover it

- Read this topic with `anx meta doc wake-routing`.
- Read the preferred runtime path with `anx meta doc agent-bridge`.
- Use `anx help bridge` to bootstrap the per-agent bridge runtime from the main CLI.
- Use `anx bridge workspace-id --handle <handle>` when an existing registration is the easiest source of truth for the durable workspace id.
- Use `anx bridge import-auth --config ./agent.toml --from-profile <agent>` when matching `anx` auth already exists.
- Use `anx notifications list --status unread` to inspect queued notifications with the main CLI.
- Use `anx notifications dismiss --wakeup-id <wakeup-id>` to dismiss a notification so it no longer wakes the bridge.
- Use `anx auth whoami` to confirm your current username and actor id.
- Use `anx auth principals list --handles-only` to inspect the exact handles that can be mentioned.
- Use `anx auth principals list --taggable` if you want the filtered principal rows as well.
- Use `anx auth principals list` for readable rows; add `--json` when you need the full wake-routing metadata in a parseable envelope (scripts, debugging).

Preferred path when you are using `anx-agent-bridge`

1. Install the runtime:

  anx bridge install

2. Confirm the workspace deployment's `anx-core` config and note the durable workspace id it uses.

3. Generate the agent config and implement your adapter (subprocess JSON or python_plugin):

  anx bridge init-config --kind subprocess --output ./agent.toml --workspace-id <workspace-id> --handle <handle> --adapter-entrypoint ./adapter.py

  Inspect the exact stdin/stdout JSON contract with `anx-agent-bridge adapter contract --config ./agent.toml`.

4. If matching `anx` auth already exists, import it into the bridge config:

  anx bridge import-auth --config ./agent.toml --from-profile <agent>

  This also syncs the default local `[anx].base_url` in the bridge config to the imported profile when they differ.

5. Register auth and write the initial pending registration when auth does not already exist:

  anx-agent-bridge auth register --config ./agent.toml --invite-token <token> --apply-registration

  If auth already exists and you only need to rewrite the principal registration:

  anx-agent-bridge registration apply --config <agent.toml>

6. Start the target bridge:

  anx bridge start --config ./agent.toml

7. Verify the bridge has checked in before expecting immediate delivery:

  anx bridge status --config ./agent.toml
  anx bridge doctor --config ./agent.toml
  anx-agent-bridge registration status --config ./agent.toml

8. Pull or dismiss queued notifications directly when needed:

  anx notifications list --status unread
  anx-agent-bridge notifications list --config ./agent.toml --status unread
  anx notifications dismiss --wakeup-id <wakeup-id>

9. If the bridge is online but tagged delivery still does not work, ask the workspace operator to inspect the embedded wake-routing sidecar in `anx-core`.

Generic ANX CLI lifecycle

If you are writing registration state manually, update the agent principal registration only. Manual principal updates do not replace the live bridge-owned check-in event.

1. Confirm the identity you are registering:

  anx auth whoami

  Use the server-resolved username as `<handle>` and the server actor id as `<actor-id>`.

2. Resolve the durable workspace id you want to enable:

  - If an existing registration is available, start with `anx bridge workspace-id --handle <handle>`.
  - If the workspace deployment already documents the configured `workspace_id`, copy that exact value.
  - If your deployment is driven by control-plane workspace records, copy the durable workspace id from that record, not the slug.
  - The bundled example value `ws_main` is only a sample.
  - Do not use a workspace slug or URL path segment. If you cannot determine the real value, stop and ask the operator.

3. Create a first-time registration payload such as `wake-registration.json`:

  {
    "registration": {
      "version": "agent-registration/v1",
      "handle": "<handle>",
      "actor_id": "<actor-id>",
      "delivery_mode": "pull",
      "driver_kind": "custom",
      "resume_policy": "resume_or_create",
      "status": "pending",
      "adapter_kind": "custom",
      "updated_at": "<current-utc-timestamp>",
      "workspace_bindings": [
        {
          "workspace_id": "<workspace-id>",
          "enabled": true
        }
      ]
    }
  }

4. For first-time registration, patch the current authenticated agent:

  curl -X PATCH "$ANX_BASE_URL/agents/me" \
    -H "Authorization: Bearer <access-token>" \
    -H "Content-Type: application/json" \
    --data @wake-registration.json

5. If auth already exists, prefer the supported bridge-managed path instead of hand-patching:

  anx-agent-bridge registration apply --config ./agent.toml

Registration schema notes

- Fields required for routing correctness are:
  - `content.handle` matching the principal username
  - `content.actor_id` matching the principal actor id
  - at least one enabled `content.workspace_bindings[].workspace_id` matching the current workspace id
- Bridge readiness fields are:
  - `content.bridge_checkin_event_id` points at the latest `agent_bridge_checked_in` event
  - `content.bridge_signing_public_key_spki_b64` stores the bridge-managed public proof key
  - that event payload includes `bridge_instance_id`, `checked_in_at`, and `expires_at`
  - that event payload also includes `proof_signature_b64`, which must verify against the registration's public proof key
- `updated_at` is advisory metadata. Set it to the current UTC time when creating or updating the registration, or let bridge-managed flows populate it.
- Do not hand-edit `status = "active"` before the bridge has actually checked in.
- Do not try to hand-author the bridge readiness proof. The supported path is to let the running bridge emit `agent_bridge_checked_in` and refresh the registration.

Verification flow

1. Confirm your local and server identity:

  anx auth whoami

2. Confirm a principal exists for the target handle:

  anx auth principals list --handles-only

3. Read the principal registration (`--json` when a script parses the full payload):

  anx auth principals list --json

4. Verify all of the following:
  - principal kind is `agent`
  - principal username is exactly `<handle>`
  - principal actor id matches `content.actor_id`
  - `workspace_bindings` contains the current workspace id with `enabled: true`
  - `status` is `active`
  - if you need online delivery right now, `bridge_checkin_event_id` is present on the registration
  - if you need online delivery right now, `anx events get --event-id <bridge-checkin-event-id>` (add `--json` for the CLI JSON envelope) returns an `agent_bridge_checked_in` event
  - if you need online delivery right now, that event actor id matches the principal actor
  - if you need online delivery right now, that event `expires_at` is still in the future

5. If you are using `anx-agent-bridge`, prefer:

  anx bridge doctor --config ./agent.toml

Concrete wake example

1. Ensure the target registration is valid for the workspace, and ensure the bridge is running if you want immediate delivery. The workspace deployment must also be running `anx-core` with the embedded wake-routing sidecar enabled.
2. Post a thread message containing `@<handle>`, for example:

  @<handle> summarize the latest onboarding blockers.

3. Expected durable trace:
- existing `message_posted`
- new `agent_wakeup_requested`
- if online, new `agent_wakeup_claimed`
- if online, new bridge reply `message_posted`
- if online, new `agent_wakeup_completed`
- if offline, the `agent_wakeup_requested` stays pending until the bridge later claims it

Common failure modes

- unknown handle: no matching agent principal username exists
- missing registration: the agent principal does not have wake registration metadata
- registration actor mismatch: the registration points at a different actor
- workspace not bound: registration exists but is not enabled for this workspace
- bridge not checked in: the registration may still be pending, or the bridge may simply be offline for immediate delivery
- stale bridge check-in: the bridge stopped refreshing readiness, so delivery is queued until it returns
- wake-routing sidecar unavailable: the workspace deployment is not currently routing tagged messages
- wrong workspace id: the registration uses a slug or another id that does not match the workspace deployment

Operational note

- This mechanism is discoverable from the CLI and UI, but actual wake dispatch is owned by the workspace deployment's `anx-core` process plus the per-agent bridge runtime.

Next steps

  anx help bridge
  anx meta doc agent-bridge
  anx bridge doctor --config ./agent.toml
```

## `draft`

Local draft staging, listing, commit, and discard workflow.

```text
Draft staging

Use `anx draft` when you want a local checkpoint before sending a write to core.

Choose the right path:

- Use direct commands when the mutation is small and you are ready to apply it now.
- Prefer command-specific proposal flows when they exist, such as `docs propose-update`, because they add domain-aware diff/review helpers.
- Use `draft` for lower-level commands, generic JSON bodies, or cases where you want to stage the exact request before commit.

Standard workflow

1. Build the exact payload for the target command.
2. Stage it with `draft create`.
3. Inspect staged drafts with `draft list`.
4. Commit when ready, or discard if the request should not be sent.

Usage:
  anx draft create --command <command-id> [--from-file <path>]
  anx draft list
  anx draft commit <draft-id> [--keep]
  anx draft discard <draft-id>

Heuristics

- Keep drafts short-lived; they are a checkpoint, not durable state.
- Prefer one clear intent per draft.
- Use `--from-file` or stdin for non-trivial JSON bodies so requests stay reproducible.
- Re-read current state before committing older drafts if the target may have changed.

Examples:
  cat payload.json | anx draft create --command topics.create
  anx draft list
  anx draft commit draft-20260305T103000-a1b2c3d4e5f6
```

## `provenance`

Deterministic provenance walk reference and examples.

```text
Provenance guide

Use `anx provenance walk` when you need to answer questions like:

- Why does this object exist?
- What evidence or earlier object led to it?
- What thread, artifact, event, or topic is this derived from?

Mental model

- Provenance is a graph of typed refs, not just a linear event log.
- Start from the object you trust most, then walk outward a few hops.
- Keep walks narrow at first; increase depth only when the first pass is insufficient.
- Use event-chain expansion when you specifically need event-to-event lineage, not as the default for every investigation.

Usage:
  anx provenance walk --from <typed-ref> [--depth <n>] [--include-event-chain]

Typed ref roots:
  event:<id>
  thread:<id>
  artifact:<id>
  topic:<id>

Heuristics

- Start from `event:<id>` when explaining one update or mutation.
- Start from `thread:<id>` when explaining backing-thread evidence and history.
- Start from `artifact:<id>` when tracing a file or attachment back to its source.
- Start from `topic:<id>` when explaining operator-facing topic state and linked refs.
- Prefer shallow depths like 1-3 before broader traversals.

Examples:
  anx provenance walk --from event:event_123 --depth 2
  anx provenance walk --from topic:topic_123 --depth 1
  anx --json provenance walk --from event:event_123 --depth 2
  anx provenance walk --from event:event_123 --depth 3 --include-event-chain
```

## `auth whoami`

Validate the active profile, print resolved identity metadata, and point agents at wake-registration next steps.

```text
Local Help: auth whoami

Validate the active profile against the server, print resolved identity metadata, and point to wake-registration next steps.

Usage:
  anx auth whoami

Examples:
  anx auth whoami
  anx --json auth whoami

Next steps:
  If this agent should be wakeable by `@handle`, read `anx meta doc wake-routing`.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth whoami ... ; anx --json auth whoami ... ; anx auth whoami ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth list`

List local CLI profiles and the active profile.

```text
Local Help: auth list

List local CLI profiles and identify the active one.

Usage:
  anx auth list

Examples:
  anx auth list
  anx --json auth list

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth list ... ; anx --json auth list ... ; anx auth list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth default`

Persist the default CLI profile used when no explicit agent is selected.

```text
Local Help: auth default

Persist the default profile used when no explicit agent is selected.

Usage:
  anx auth default <profile>

Examples:
  anx auth default agent-a
  anx --json auth default agent-a

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth default ... ; anx --json auth default ... ; anx auth default ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `config use`

Set the active CLI profile used when --agent and ANX_AGENT are omitted.

```text
Local Help: config use

Persist the named profile as the active default used when --agent and ANX_AGENT are omitted.

Usage:
  anx config use <profile>

Examples:
  anx config use agent-a
  anx --json config use agent-a

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx config use ... ; anx --json config use ... ; anx config use ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `config show`

Print effective CLI settings and per-field sources (tokens redacted).

```text
Local Help: config show

Print effective CLI settings and the source of each field (access tokens are redacted).

Usage:
  anx config show

Examples:
  anx config show
  anx --json config show

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx config show ... ; anx --json config show ... ; anx config show ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `config unset`

Clear the persisted default profile marker (~/.config/anx/default-profile).

```text
Local Help: config unset

Remove the default profile marker file so the CLI falls back to single-profile auto-select or explicit flags/env.

Usage:
  anx config unset

Examples:
  anx config unset
  anx --json config unset

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx config unset ... ; anx --json config unset ... ; anx config unset ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth update-username`

Rename the authenticated agent and sync the local profile.

```text
Local Help: auth update-username

Update the authenticated agent username and sync the local profile copy.

Usage:
  anx auth update-username --username <username>

Examples:
  anx auth update-username --username renamed_agent

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth update-username ... ; anx --json auth update-username ... ; anx auth update-username ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth rotate`

Rotate the active agent key and refresh stored credentials.

```text
Local Help: auth rotate

Rotate the active agent key and refresh stored credentials.

Usage:
  anx auth rotate

Examples:
  anx auth rotate
  anx --json auth rotate

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth rotate ... ; anx --json auth rotate ... ; anx auth rotate ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth revoke`

Revoke the active agent and mark the local profile revoked. Use explicit human-lockout flags only for break-glass recovery.

```text
Local Help: auth revoke

Revoke the active agent and mark the local profile revoked.

Usage:
  anx auth revoke

Examples:
  anx auth revoke
  anx --json auth revoke

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth revoke ... ; anx --json auth revoke ... ; anx auth revoke ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth token-status`

Inspect whether the local profile still has refreshable token material.

```text
Local Help: auth token-status

Inspect whether the local profile still has refreshable token material.

Usage:
  anx auth token-status

Examples:
  anx auth token-status
  anx --json auth token-status

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth token-status ... ; anx --json auth token-status ... ; anx auth token-status ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge`

CLI-managed bridge bootstrap helpers for installing, templating, and checking `anx-agent-bridge`.

```text
Bridge bootstrap

Use `anx bridge` when you only have the main CLI installed and need to bootstrap, manage, or inspect the Python `anx-agent-bridge` runtime for one agent. This is the discoverable install/setup path for agent operators. The bridge package still owns the runtime behavior; the main CLI installs it and acts as the local process manager.

Bootstrap prerequisites

- Python `3.11+`
- `git` on PATH for the current GitHub-subdirectory install path

Lifecycle constraint

- Registration plus a matching enabled workspace binding makes an agent taggable.
- A fresh bridge check-in makes the agent online for immediate delivery.
- Offline agents still accumulate durable wake notifications and will receive them when the bridge comes back.

Subcommands

  bridge install      Install or refresh the managed `anx-agent-bridge` virtualenv and wrapper
  bridge import-auth  Copy an existing `anx` profile into bridge auth state
  bridge init-config  Render a minimal agent bridge TOML config
  bridge start        Start a managed bridge daemon for one config
  bridge stop         Stop a managed bridge daemon for one config
  bridge restart      Restart a managed bridge daemon for one config
  bridge status       Inspect managed process state for one config
  bridge logs         Read recent log lines for one config
  bridge workspace-id Read workspace ids from an existing wake registration
  bridge doctor       Validate install/config/readiness without starting daemons

Recommended order

1. `anx bridge install`
2. `anx bridge workspace-id --handle <handle>` if a registration already exists and you need the real durable workspace id
3. `anx bridge init-config --kind subprocess --output ./agent.toml --workspace-id <workspace-id> --handle <handle> --adapter-entrypoint ./adapter.py`
4. `anx bridge import-auth --config ./agent.toml --from-profile <agent>` when matching `anx` auth already exists so bridge auth and the default bridge `[anx].base_url` stay aligned
5. `anx-agent-bridge auth register ...` for the agent principal when auth does not already exist
6. `anx bridge start --config ./agent.toml`
7. `anx bridge status --config ./agent.toml` and `anx bridge doctor --config ./agent.toml` before expecting immediate online delivery
8. `anx notifications list --status unread` or `anx-agent-bridge notifications list --config ./agent.toml --status unread` when you want to pull pending notifications directly

Workspace-owned wake routing

- `anx bridge` only manages per-agent bridge daemons.
- Tagged wake routing runs inside `anx-core` as an embedded workspace sidecar.
- If tagged delivery still fails while the bridge is online, hand off to the workspace operator to inspect the embedded wake-routing sidecar in `anx-core`.
```

## `import`

Prescriptive import guide for building low-duplication, discoverable ANX graphs from external material.

```text
Import guide

Use `anx import` to turn external material into a clean ANX graph. The goal is not to dump files into the system. The goal is to create discoverable topics, docs, and artifacts with low duplication, low orphan rates, and clear provenance.

Object model

- `topics` hold ongoing work, collector structures, and discoverable entry points.
- `docs` hold narrative knowledge, summaries, and hub content.
- `artifacts` hold raw or attached evidence.
- Import should create a graph that people and agents can navigate, not just a pile of uploaded files.

Read in this order

1. `anx help import` — doctrine, quality bars, and the recommended loop.
2. `anx help import scan` — inventory and text-cache generation.
3. `anx help import plan` — classification, collector threads, hub docs, and review bundles.
4. If you will execute writes: `anx help topics create`, `anx help artifacts create`, and `anx help docs create`.
5. Optional graph/provenance reference: `anx help provenance`.

Operating stance

- High precision beats high recall.
- Exact duplicates should be skipped before writes.
- Ambiguous or noisy material should be skipped or deferred to review bundles.
- Imported material should usually get a discoverable entry point: a collector thread, a hub doc, or both.
- Codebases should not become one ANX object per source file.
- Binary attachments should be preserved conservatively; if reliable raw upload is not available, keep explicit pending work instead of pretending they were imported cleanly.
- Prefer preview-first planning over eager execution.

Recommended loop

1. `anx import scan --input <dir-or-zip>`
2. `anx import dedupe --inventory ./.anx-import/<source>/inventory.jsonl`
3. `anx import plan --inventory ./.anx-import/<source>/inventory.jsonl`
4. Review `plan-preview.md`, `skipped`, and `review_bundles`.
5. `anx import apply --plan ./.anx-import/<source>/plan.json` for payload previews.
6. `anx import apply --plan ./.anx-import/<source>/plan.json --execute` only after the plan looks clean.

Subcommands

  import scan      Build normalized inventory + text cache from a folder or zip
  import dedupe    Find exact duplicates and probable duplicate review clusters
  import plan      Build a conservative ANX-native import plan
  import apply     Write payload previews and optionally execute creates

Output conventions

- Default workdir is `./.anx-import/<source-name>`.
- `scan` writes `inventory.jsonl` and `scan-summary.json`.
- `dedupe` writes `dedupe.json`.
- `plan` writes `plan.json` and `plan-preview.md`.
- `apply` writes payload previews plus `apply-results.json` and `apply-commands.sh`.
```

## `auth`

Register, inspect, and manage auth state

```text
Auth lifecycle and registration surface

Use this group to register a profile, inspect the active identity, and manage local auth state.

Core commands:
  auth register       Create or register a profile.
  auth whoami         Inspect the active profile.
  auth list           List local profiles.
  auth default        Select the default profile.
  auth update-username  Rename the current principal locally.
  auth rotate         Rotate the active agent key.
  auth revoke         Revoke the current profile.
  auth token-status   Inspect whether the profile still has refreshable token material.

	Related commands:
  auth invites        Manage invite tokens and invite-backed registration.
  auth bootstrap      Inspect bootstrap status before first registration.
  auth principals     Inspect or revoke principals.
  auth audit          Inspect audit records for auth activity.
```

## `topics`

Manage durable work subjects

```text
Generated Help: topics

Commands:
  topics archive           Archive topic
  topics create            Create topic
  topics get               Get topic
  topics list              List topics
  topics patch             Patch topic
  topics restore           Restore topic from trash
  topics timeline          Get topic timeline
  topics trash             Move topic to trash
  topics unarchive         Unarchive topic
  topics workspace         Get topic workspace (primary operator coordination read)

Primary operator coordination:
  topics workspace        Load the topic workspace (cards, docs, backing threads, inbox).
  topics list / topics get   Discover and resolve topic ids.
  Tip: start with `anx topics workspace --topic-id <topic-id>` for triage; use `anx topics list` to find ids. Add `--full-id` for copy/paste ids.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics ... ; anx --json topics ... ; anx topics ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `cards`

Manage board-scoped cards

```text
Generated Help: cards

Commands:
  cards archive            Archive card
  cards create             Create card (global path)
  cards get                Get card
  cards list               List cards
  cards move               Move card
  cards patch              Patch card
  cards purge              Permanently delete archived or trashed card
  cards restore            Restore archived or trashed card
  cards timeline           Get card timeline
  cards trash              Move card to trash

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards ... ; anx --json cards ... ; anx cards ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `threads`

Read-only backing-thread inspection (tooling and diagnostics)

```text
Generated Help: threads

Commands:
  threads context          Get backing thread coordination context
  threads inspect          Inspect backing thread
  threads list             List backing threads
  threads timeline         Get backing thread timeline
  threads workspace        Get backing thread workspace projection (diagnostic)

Read-only backing-thread diagnostics (tooling):
  threads recommendations   Recommendation-focused review for one backing thread.
  threads workspace       Diagnostic workspace projection (context + inbox + related-thread review).
  threads inspect          Smaller diagnostic bundle (context + inbox).
  threads timeline         Backing thread timeline and expansions.
  Tip: prefer `anx topics workspace` for normal operator coordination. Use `anx threads workspace` when you need the backing-thread projection or related-thread review; use `--status/--tag/--type initiative` to discover one thread. For a minimal `{thread}` read, use `anx threads get` (contract: `threads.inspect`). Add `--full-id` for copy/paste ids.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx threads ... ; anx --json threads ... ; anx threads ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `artifacts`

Manage artifact resources and content

```text
Generated Help: artifacts

Commands:
  artifacts archive        Archive artifact
  artifacts create         Create artifact
  artifacts get            Get artifact metadata
  artifacts list           List artifacts
  artifacts purge          Permanently delete trashed artifact
  artifacts restore        Restore artifact from trash
  artifacts trash          Move artifact to trash
  artifacts unarchive      Unarchive artifact

Local inspection helper:
  artifacts inspect        Fetch artifact metadata and content in one call.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts ... ; anx --json artifacts ... ; anx artifacts ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `boards`

Manage board resources and ordered cards

```text
Generated Help: boards

Commands:
  boards archive           Archive board
  boards create            Create board
  boards get               Get board
  boards list              List boards
  boards purge             Permanently delete trashed board
  boards restore           Restore board from trash
  boards trash             Move board to trash
  boards unarchive         Unarchive board
  boards workspace         Get board workspace view

Batch card creation:
  boards cards create-batch   POST body via stdin or `--from-file`; profile supplies `actor_id` when omitted. See `anx help boards cards create-batch`.

Read paths:
  boards get / boards workspace   Board metadata including `updated_at` for optimistic concurrency.
  boards cards list               Existing cards and refs before adding more.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards ... ; anx --json boards ... ; anx boards ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `docs`

Manage long-lived docs and revisions

```text
Generated Help: docs

Commands:
  docs archive             Archive document
  docs create              Create document
  docs get                 Get document
  docs list                List documents
  docs purge               Permanently delete trashed document
  docs restore             Restore document from trash
  docs trash               Move document to trash
  docs unarchive           Unarchive document

Local inspection helpers:
  docs content             Show current document content with revision metadata.
  Mutation flow:
  docs propose-update      Stage an update proposal and inspect its diff before applying it.
  docs apply               Apply a staged document update proposal.
  docs validate-update     Validate a docs.revisions.create payload from stdin/--from-file.
  Tip: add `--content-file <path>` to avoid hand-escaping multiline content. The proposal flow stages `docs.revisions.create`.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs ... ; anx --json docs ... ; anx docs ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `events`

Manage events and event streams

```text
Generated Help: events

Commands:
  events archive           Archive event
  events create            Create event
  events get               Get event by id
  events list              List events
  events restore           Restore event from trash
  events stream            Stream events (SSE)
  events trash             Move event to trash
  events unarchive         Unarchive event

Local inspection helpers:
  events list              List timeline events with thread/type/actor filters, id mode, and preview summaries.
  events explain           Explain known event-type conventions and local validation constraints.
  events validate          Validate an events.create payload from stdin/--from-file without sending a request.
  Tip: use `--mine` or `--actor-id <id>` to audit one actor; add `--full-id` for copy/paste IDs.
  For details: `anx events explain <event-type>`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx events ... ; anx --json events ... ; anx events ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `inbox`

List/get/ack/stream inbox items

```text
Generated Help: inbox

Commands:
  inbox acknowledge        Acknowledge inbox item
  inbox get                Get one inbox item
  inbox list               List inbox items
  inbox stream             Stream inbox items (SSE)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx inbox ... ; anx --json inbox ... ; anx inbox ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `receipts`

Create receipt packets (subject_ref must be card:<card_id>)

```text
Generated Help: receipts

Commands:
  receipts create          Create receipt packet

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx receipts ... ; anx --json receipts ... ; anx receipts ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `reviews`

Create review packets (subject_ref + receipt_ref; subject_ref must be card:<card_id>)

```text
Generated Help: reviews

Commands:
  reviews create           Create review packet

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx reviews ... ; anx --json reviews ... ; anx reviews ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `derived`

Run derived-view maintenance actions

```text
Derived maintenance surface

Use this group to refresh or inspect derived views that are computed from canonical state.

Core commands:
  derived rebuild     Rebuild derived state from the canonical records.
  derived status      Inspect the current derived maintenance state.

Tip: derived commands are operational helpers, not the source of truth.
```

## `meta`

Inspect generated command/concept metadata

```text
Metadata and shipped reference surface

Use this group to inspect CLI/runtime metadata and to print the bundled runtime reference docs.

Core commands:
  meta health     Inspect overall CLI/runtime health.
  meta readyz     Check readiness.
  meta version    Print version information.

Reference commands:
  meta docs       Print the bundled runtime help reference.
  meta doc        Print one bundled runtime help topic.
  meta skill      Export a bundled editor skill file.
  meta commands   Inspect generated command metadata.
  meta concepts   Inspect generated concepts metadata.
```

## `auth invites list`

List invite tokens

```text
Generated Help: auth invites list

- Command ID: `auth.invites.list`
- CLI path: `auth invites list`
- HTTP: `GET /auth/invites`
- Stability: `beta`
- Input mode: `none`
- Why: Operator listing of outstanding invites.
- Output: Returns `{ invites }`.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `auth`
- Adjacent commands: `auth register`, `auth audit list`, `auth bootstrap status`, `auth invites create`, `auth invites revoke`, `auth passkey dev login`, `auth passkey dev register`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth token`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth invites list ... ; anx --json auth invites list ... ; anx auth invites list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth invites create`

Create invite token

```text
Generated Help: auth invites create

- Command ID: `auth.invites.create`
- CLI path: `auth invites create`
- HTTP: `POST /auth/invites`
- Stability: `beta`
- Input mode: `json-body`
- Why: Issue a one-time invite for human or agent principals.
- Output: Returns `{ invite, token }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `auth`
- Adjacent commands: `auth register`, `auth audit list`, `auth bootstrap status`, `auth invites list`, `auth invites revoke`, `auth passkey dev login`, `auth passkey dev register`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth token`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth invites create ... ; anx --json auth invites create ... ; anx auth invites create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth invites revoke`

Revoke invite

```text
Generated Help: auth invites revoke

- Command ID: `auth.invites.revoke`
- CLI path: `auth invites revoke`
- HTTP: `POST /auth/invites/{invite_id}/revoke`
- Stability: `beta`
- Input mode: `json-body`
- Why: Invalidate an outstanding invite by id.
- Output: Returns `{ invite }`.
- Error codes: `auth_required`, `invalid_request`, `not_found`, `invalid_token`
- Concepts: `auth`
- Adjacent commands: `auth register`, `auth audit list`, `auth bootstrap status`, `auth invites create`, `auth invites list`, `auth passkey dev login`, `auth passkey dev register`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth token`

Inputs:
  Required:
  - path `invite_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth invites revoke ... ; anx --json auth invites revoke ... ; anx auth invites revoke ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth bootstrap status`

Bootstrap registration availability

```text
Generated Help: auth bootstrap status

- Command ID: `auth.bootstrap.status`
- CLI path: `auth bootstrap status`
- HTTP: `GET /auth/bootstrap/status`
- Stability: `beta`
- Input mode: `none`
- Why: Report whether first-principal bootstrap registration is still available.
- Output: Returns `{ bootstrap_registration_available, dev_passkey_bypass_available? }`, where the dev bypass field reflects the effective local-only passkey bypass capability.
- Concepts: `auth`
- Adjacent commands: `auth register`, `auth audit list`, `auth invites create`, `auth invites list`, `auth invites revoke`, `auth passkey dev login`, `auth passkey dev register`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth token`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth bootstrap status ... ; anx --json auth bootstrap status ... ; anx auth bootstrap status ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads list`

List backing threads

```text
Generated Help: threads list

- Command ID: `threads.list`
- CLI path: `threads list`
- HTTP: `GET /threads`
- Stability: `beta`
- Input mode: `none`
- Why: Inspect backing infrastructure threads without making them the primary planning noun.
- Output: Returns `{ threads }`.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `threads`, `inspection`
- Adjacent commands: `threads context`, `threads inspect`, `threads timeline`, `threads workspace`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx threads list ... ; anx --json threads list ... ; anx threads list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads get`

Inspect backing thread

```text
Generated Help: threads get

- Command ID: `threads.inspect`
- CLI path: `threads inspect`
- HTTP: `GET /threads/{thread_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve one backing thread for low-level inspection and diagnostics.
- Output: Returns `{ thread }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `threads`, `inspection`
- Adjacent commands: `threads context`, `threads list`, `threads timeline`, `threads workspace`

Inputs:
  Required:
  - path `thread_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx threads get ... ; anx --json threads get ... ; anx threads get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads timeline`

Get backing thread timeline

```text
Generated Help: threads timeline

- Command ID: `threads.timeline`
- CLI path: `threads timeline`
- HTTP: `GET /threads/{thread_id}/timeline`
- Stability: `beta`
- Input mode: `none`
- Why: Retrieve event history plus typed-ref expansions for one backing thread.
- Output: Returns `{ thread, events, artifacts, topics, cards, documents }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `threads`, `timeline`
- Adjacent commands: `threads context`, `threads inspect`, `threads list`, `threads workspace`

Inputs:
  Required:
  - path `thread_id`

Local CLI flags:
  --include-archived        Include archived events in the timeline.
  --archived-only           Show only archived events.
  --include-trashed      Include trashed events in the timeline.
  --trashed-only         Show only trashed events in the timeline.

Note: by default, archived and trashed events are excluded from the timeline output.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx threads timeline ... ; anx --json threads timeline ... ; anx threads timeline ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads context`

Get backing thread coordination context

```text
Generated Help: threads context

- Command ID: `threads.context`
- CLI path: `threads context`
- HTTP: `GET /threads/{thread_id}/context`
- Stability: `beta`
- Input mode: `none`
- Why: Load a compact coordination bundle (thread, recent events, key artifacts, cards, documents) for inspection and triage.
- Output: Returns `{ thread, recent_events, key_artifacts, open_cards, documents }` plus forward-compatible fields.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Concepts: `threads`, `inspection`
- Adjacent commands: `threads inspect`, `threads list`, `threads timeline`, `threads workspace`

Inputs:
  Required:
  - path `thread_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx threads context ... ; anx --json threads context ... ; anx threads context ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics list`

List topics

```text
Generated Help: topics list

- Command ID: `topics.list`
- CLI path: `topics list`
- HTTP: `GET /topics`
- Stability: `beta`
- Input mode: `none`
- Why: Scan the durable topic inventory.
- Output: Returns `{ topics }`.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `topics`
- Adjacent commands: `topics archive`, `topics create`, `topics get`, `topics patch`, `topics restore`, `topics timeline`, `topics trash`, `topics unarchive`, `topics workspace`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics list ... ; anx --json topics list ... ; anx topics list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics get`

Get topic

```text
Generated Help: topics get

- Command ID: `topics.get`
- CLI path: `topics get`
- HTTP: `GET /topics/{topic_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve one topic and its canonical durable fields.
- Output: Returns `{ topic }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `topics`
- Adjacent commands: `topics archive`, `topics create`, `topics list`, `topics patch`, `topics restore`, `topics timeline`, `topics trash`, `topics unarchive`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics get ... ; anx --json topics get ... ; anx topics get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics create`

Create topic

```text
Generated Help: topics create

- Command ID: `topics.create`
- CLI path: `topics create`
- HTTP: `POST /topics`
- Stability: `beta`
- Input mode: `json-body`
- Why: Create a first-class durable topic before attaching cards, docs, or packets.
- Output: Returns `{ topic }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `topics`, `write`
- Agent notes: Replay-safe when the same request key and body are reused.
- Adjacent commands: `topics archive`, `topics get`, `topics list`, `topics patch`, `topics restore`, `topics timeline`, `topics trash`, `topics unarchive`, `topics workspace`

Inputs:
  Required:
  - body `topic.board_refs` (list<any>)
  - body `topic.document_refs` (list<any>)
  - body `topic.owner_refs` (list<any>)
  - body `topic.provenance.sources` (list<string>)
  - body `topic.related_refs` (list<any>)
  - body `topic.status` (string)
  - body `topic.summary` (string)
  - body `topic.title` (string)
  - body `topic.type` (string)
  Optional:
  - body `topic.provenance.by_field` (object)
  - body `topic.provenance.notes` (string)
  Enum values: topic.status: active, archived, blocked, closed, paused, proposed, resolved; topic.type: case, decision, incident, initiative, note, objective, other, process, relationship, request, risk

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics create ... ; anx --json topics create ... ; anx topics create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics patch`

Patch topic

```text
Generated Help: topics patch

- Command ID: `topics.patch`
- CLI path: `topics patch`
- HTTP: `PATCH /topics/{topic_id}`
- Stability: `beta`
- Input mode: `json-body`
- Why: Update topic state with provenance and optimistic concurrency.
- Output: Returns `{ topic }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `topics`, `write`, `concurrency`
- Adjacent commands: `topics archive`, `topics create`, `topics get`, `topics list`, `topics restore`, `topics timeline`, `topics trash`, `topics unarchive`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.
  Optional:
  - body `patch.board_refs` (list<any>)
  - body `patch.document_refs` (list<any>)
  - body `patch.owner_refs` (list<any>)
  - body `patch.provenance.by_field` (object)
  - body `patch.provenance.notes` (string)
  - body `patch.provenance.sources` (list<string>)
  - body `patch.related_refs` (list<any>)
  - body `patch.status` (string)
  - body `patch.summary` (string)
  - body `patch.title` (string)
  - body `patch.type` (string)
  Enum values: patch.status: active, archived, blocked, closed, paused, proposed, resolved; patch.type: case, decision, incident, initiative, note, objective, other, process, relationship, request, risk

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics patch ... ; anx --json topics patch ... ; anx topics patch ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics timeline`

Get topic timeline

```text
Generated Help: topics timeline

- Command ID: `topics.timeline`
- CLI path: `topics timeline`
- HTTP: `GET /topics/{topic_id}/timeline`
- Stability: `beta`
- Input mode: `none`
- Why: Load chronological evidence and related resources for one topic.
- Output: Returns `{ topic, events, artifacts, cards, documents, threads }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `topics`, `timeline`
- Adjacent commands: `topics archive`, `topics create`, `topics get`, `topics list`, `topics patch`, `topics restore`, `topics trash`, `topics unarchive`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics timeline ... ; anx --json topics timeline ... ; anx topics timeline ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics workspace`

Get topic workspace (primary operator coordination read)

```text
Generated Help: topics workspace

- Command ID: `topics.workspace`
- CLI path: `topics workspace`
- HTTP: `GET /topics/{topic_id}/workspace`
- Stability: `beta`
- Input mode: `none`
- Why: Primary operator coordination read — load the topic workspace composed from linked cards, docs, backing threads, and inbox items. Prefer this over thread workspace for triage and planning.
- Output: Returns `{ topic, cards, boards, documents, threads, inbox, projection_freshness, generated_at }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `topics`, `workspace`
- Adjacent commands: `topics archive`, `topics create`, `topics get`, `topics list`, `topics patch`, `topics restore`, `topics timeline`, `topics trash`, `topics unarchive`

Inputs:
  Required:
  - path `topic_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics workspace ... ; anx --json topics workspace ... ; anx topics workspace ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics archive`

Archive topic

```text
Generated Help: topics archive

- Command ID: `topics.archive`
- CLI path: `topics archive`
- HTTP: `POST /topics/{topic_id}/archive`
- Stability: `beta`
- Input mode: `json-body`
- Why: Soft-archive a topic (orthogonal to business status; clears default list visibility).
- Output: Returns `{ topic }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `topics`, `write`
- Adjacent commands: `topics create`, `topics get`, `topics list`, `topics patch`, `topics restore`, `topics timeline`, `topics trash`, `topics unarchive`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics archive ... ; anx --json topics archive ... ; anx topics archive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics unarchive`

Unarchive topic

```text
Generated Help: topics unarchive

- Command ID: `topics.unarchive`
- CLI path: `topics unarchive`
- HTTP: `POST /topics/{topic_id}/unarchive`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear archived_at on a topic (restore default list visibility).
- Output: Returns `{ topic }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `topics`, `write`
- Adjacent commands: `topics archive`, `topics create`, `topics get`, `topics list`, `topics patch`, `topics restore`, `topics timeline`, `topics trash`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics unarchive ... ; anx --json topics unarchive ... ; anx topics unarchive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics trash`

Move topic to trash

```text
Generated Help: topics trash

- Command ID: `topics.trash`
- CLI path: `topics trash`
- HTTP: `POST /topics/{topic_id}/trash`
- Stability: `beta`
- Input mode: `json-body`
- Why: Move topic to trash with an explicit operator reason.
- Output: Returns `{ topic }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `topics`, `write`
- Adjacent commands: `topics archive`, `topics create`, `topics get`, `topics list`, `topics patch`, `topics restore`, `topics timeline`, `topics unarchive`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`
  - body `reason` (string)
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics trash ... ; anx --json topics trash ... ; anx topics trash ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics restore`

Restore topic from trash

```text
Generated Help: topics restore

- Command ID: `topics.restore`
- CLI path: `topics restore`
- HTTP: `POST /topics/{topic_id}/restore`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear trash lifecycle fields on a topic after an explicit restore action.
- Output: Returns `{ topic }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `topics`, `write`
- Adjacent commands: `topics archive`, `topics create`, `topics get`, `topics list`, `topics patch`, `topics timeline`, `topics trash`, `topics unarchive`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics restore ... ; anx --json topics restore ... ; anx topics restore ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards list`

List cards

```text
Generated Help: cards list

- Command ID: `cards.list`
- CLI path: `cards list`
- HTTP: `GET /cards`
- Stability: `beta`
- Input mode: `none`
- Why: Scan first-class card resources across boards.
- Output: Returns `{ cards }`.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `cards`
- Adjacent commands: `cards archive`, `cards create`, `cards get`, `cards move`, `cards patch`, `cards purge`, `cards restore`, `cards timeline`, `cards trash`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards list ... ; anx --json cards list ... ; anx cards list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards get`

Get card

```text
Generated Help: cards get

- Command ID: `cards.get`
- CLI path: `cards get`
- HTTP: `GET /cards/{card_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve one first-class card by id.
- Output: Returns `{ card }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `cards`
- Adjacent commands: `cards archive`, `cards create`, `cards list`, `cards move`, `cards patch`, `cards purge`, `cards restore`, `cards timeline`, `cards trash`

Inputs:
  Required:
  - path `card_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards get ... ; anx --json cards get ... ; anx cards get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards create`

Create card (global path)

```text
Generated Help: cards create

- Command ID: `cards.create`
- CLI path: `cards create`
- HTTP: `POST /cards`
- Stability: `beta`
- Input mode: `json-body`
- Why: Create a card with the same body as POST /boards/{board_id}/cards, but supply board_id or board_ref here instead of a path segment. Interoperable with board-scoped create.
- Output: Returns `{ board, card }` (same as board-scoped create).
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `cards`, `boards`, `write`
- Adjacent commands: `cards archive`, `cards get`, `cards list`, `cards move`, `cards patch`, `cards purge`, `cards restore`, `cards timeline`, `cards trash`

Inputs:
  Required:
  - body `card.assignee_refs` (list<any>)
  - body `card.column_key` (string)
  - body `card.provenance.sources` (list<string>)
  - body `card.related_refs` (list<any>)
  - body `card.resolution_refs` (list<any>)
  - body `card.risk` (string)
  - body `card.summary` (string)
  - body `card.title` (string)
  Optional:
  - body `board_id` (string)
  - body `board_ref` (any)
  - body `card.after_card_id` (string)
  - body `card.before_card_id` (string)
  - body `card.definition_of_done` (list<string>)
  - body `card.document_ref` (string)
  - body `card.due_at` (datetime)
  - body `card.id` (string)
  - body `card.provenance.by_field` (object)
  - body `card.provenance.notes` (string)
  - body `card.resolution` (string)
  - body `card.topic_ref` (string)
  - body `if_board_updated_at` (datetime): Optimistic concurrency token. Copy `board.updated_at` from `anx boards get --board-id <board-id>`, `anx boards workspace --board-id <board-id>`, or the latest board mutation response.
  Enum values: card.column_key: backlog, blocked, done, in_progress, ready, review; card.resolution: canceled, done; card.risk: critical, high, low, medium

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards create ... ; anx --json cards create ... ; anx cards create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards patch`

Patch card

```text
Generated Help: cards patch

- Command ID: `cards.patch`
- CLI path: `cards patch`
- HTTP: `PATCH /cards/{card_id}`
- Stability: `beta`
- Input mode: `json-body`
- Why: Update card fields, including resolution and resolution refs.
- Output: Returns `{ card }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `cards`, `write`, `concurrency`
- Adjacent commands: `cards archive`, `cards create`, `cards get`, `cards list`, `cards move`, `cards purge`, `cards restore`, `cards timeline`, `cards trash`

Inputs:
  Required:
  - path `card_id`
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.
  Optional:
  - body `patch.assignee_refs` (list<any>)
  - body `patch.definition_of_done` (list<string>)
  - body `patch.document_ref` (string)
  - body `patch.due_at` (datetime)
  - body `patch.provenance.by_field` (object)
  - body `patch.provenance.notes` (string)
  - body `patch.provenance.sources` (list<string>)
  - body `patch.related_refs` (list<any>)
  - body `patch.resolution` (string)
  - body `patch.resolution_refs` (list<any>)
  - body `patch.risk` (string)
  - body `patch.summary` (string)
  - body `patch.title` (string)
  - body `patch.topic_ref` (string)
  Enum values: patch.resolution: canceled, done; patch.risk: critical, high, low, medium

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards patch ... ; anx --json cards patch ... ; anx cards patch ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards move`

Move card

```text
Generated Help: cards move

- Command ID: `cards.move`
- CLI path: `cards move`
- HTTP: `POST /cards/{card_id}/move`
- Stability: `beta`
- Input mode: `json-body`
- Why: Reposition a card within a board column using the card's first-class identity.
- Output: Returns `{ card }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `cards`, `boards`, `write`
- Adjacent commands: `cards archive`, `cards create`, `cards get`, `cards list`, `cards patch`, `cards purge`, `cards restore`, `cards timeline`, `cards trash`

Inputs:
  Required:
  - path `card_id`
  - body `column_key` (string)
  - body `if_board_updated_at` (datetime): Optimistic concurrency token. Copy `board.updated_at` from `anx boards get --board-id <board-id>`, `anx boards workspace --board-id <board-id>`, or the latest board mutation response.
  Optional:
  - body `actor_id` (string)
  - body `after_card_id` (string)
  - body `before_card_id` (string)
  - body `move.after_card_id` (string)
  - body `move.before_card_id` (string)
  - body `move.column_key` (string)
  - body `move.if_board_updated_at` (datetime)
  - body `move.resolution` (string)
  - body `move.resolution_refs` (list<any>)
  - body `resolution` (string)
  - body `resolution_refs` (list<any>)
  Enum values: column_key: backlog, blocked, done, in_progress, ready, review; move.column_key: backlog, blocked, done, in_progress, ready, review; move.resolution: canceled, done; resolution: canceled, done

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards move ... ; anx --json cards move ... ; anx cards move ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards archive`

Archive card

```text
Generated Help: cards archive

- Command ID: `cards.archive`
- CLI path: `cards archive`
- HTTP: `POST /cards/{card_id}/archive`
- Stability: `beta`
- Input mode: `json-body`
- Why: Soft-delete a first-class card by setting archived_at (board concurrency via if_board_updated_at).
- Output: Returns `{ board, card }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`, `already_trashed`
- Concepts: `cards`, `write`
- Adjacent commands: `cards create`, `cards get`, `cards list`, `cards move`, `cards patch`, `cards purge`, `cards restore`, `cards timeline`, `cards trash`

Inputs:
  Required:
  - path `card_id`
  Optional:
  - body `actor_id` (string)
  - body `if_board_updated_at` (datetime): Optimistic concurrency token. Copy `board.updated_at` from `anx boards get --board-id <board-id>`, `anx boards workspace --board-id <board-id>`, or the latest board mutation response.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards archive ... ; anx --json cards archive ... ; anx cards archive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards trash`

Move card to trash

```text
Generated Help: cards trash

- Command ID: `cards.trash`
- CLI path: `cards trash`
- HTTP: `POST /cards/{card_id}/trash`
- Stability: `beta`
- Input mode: `json-body`
- Why: Move a card to trash with an explicit operator reason while keeping archive lifecycle distinct.
- Output: Returns `{ board, card }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `cards`, `write`
- Adjacent commands: `cards archive`, `cards create`, `cards get`, `cards list`, `cards move`, `cards patch`, `cards purge`, `cards restore`, `cards timeline`

Inputs:
  Required:
  - path `card_id`
  - body `reason` (string)
  Optional:
  - body `actor_id` (string)
  - body `if_board_updated_at` (datetime): Optimistic concurrency token. Copy `board.updated_at` from `anx boards get --board-id <board-id>`, `anx boards workspace --board-id <board-id>`, or the latest board mutation response.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards trash ... ; anx --json cards trash ... ; anx cards trash ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards purge`

Permanently delete archived or trashed card

```text
Generated Help: cards purge

- Command ID: `cards.purge`
- CLI path: `cards purge`
- HTTP: `POST /cards/{card_id}/purge`
- Stability: `beta`
- Input mode: `json-body`
- Why: Permanently delete an archived or trashed card (human-gated).
- Output: Returns `{ purged, card_id }`.
- Error codes: `auth_required`, `human_only`, `invalid_token`, `not_found`, `conflict`
- Concepts: `cards`, `write`
- Adjacent commands: `cards archive`, `cards create`, `cards get`, `cards list`, `cards move`, `cards patch`, `cards restore`, `cards timeline`, `cards trash`

Inputs:
  Required:
  - path `card_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards purge ... ; anx --json cards purge ... ; anx cards purge ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards restore`

Restore archived or trashed card

```text
Generated Help: cards restore

- Command ID: `cards.restore`
- CLI path: `cards restore`
- HTTP: `POST /cards/{card_id}/restore`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear archive or trash lifecycle fields on a card so it reappears on boards.
- Output: Returns `{ board, card }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `cards`, `write`
- Adjacent commands: `cards archive`, `cards create`, `cards get`, `cards list`, `cards move`, `cards patch`, `cards purge`, `cards timeline`, `cards trash`

Inputs:
  Required:
  - path `card_id`
  Optional:
  - body `actor_id` (string)
  - body `if_board_updated_at` (datetime): Optimistic concurrency token. Copy `board.updated_at` from `anx boards get --board-id <board-id>`, `anx boards workspace --board-id <board-id>`, or the latest board mutation response.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards restore ... ; anx --json cards restore ... ; anx cards restore ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards timeline`

Get card timeline

```text
Generated Help: cards timeline

- Command ID: `cards.timeline`
- CLI path: `cards timeline`
- HTTP: `GET /cards/{card_id}/timeline`
- Stability: `beta`
- Input mode: `none`
- Why: Load chronological evidence and related resources for one card.
- Output: Returns `{ card, events, artifacts, cards, documents, threads }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `cards`, `timeline`
- Adjacent commands: `cards archive`, `cards create`, `cards get`, `cards list`, `cards move`, `cards patch`, `cards purge`, `cards restore`, `cards trash`

Inputs:
  Required:
  - path `card_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards timeline ... ; anx --json cards timeline ... ; anx cards timeline ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts list`

List artifacts

```text
Generated Help: artifacts list

- Command ID: `artifacts.list`
- CLI path: `artifacts list`
- HTTP: `GET /artifacts`
- Stability: `beta`
- Input mode: `none`
- Why: Search and filter immutable artifacts across the workspace.
- Output: Returns `{ artifacts }`.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `artifacts`
- Adjacent commands: `artifacts archive`, `artifacts content`, `artifacts create`, `artifacts get`, `artifacts purge`, `artifacts restore`, `artifacts trash`, `artifacts unarchive`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts list ... ; anx --json artifacts list ... ; anx artifacts list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts get`

Get artifact metadata

```text
Generated Help: artifacts get

- Command ID: `artifacts.get`
- CLI path: `artifacts get`
- HTTP: `GET /artifacts/{artifact_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve immutable artifact metadata referenced from timelines and packets.
- Output: Returns `{ artifact }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `artifacts`
- Adjacent commands: `artifacts archive`, `artifacts content`, `artifacts create`, `artifacts list`, `artifacts purge`, `artifacts restore`, `artifacts trash`, `artifacts unarchive`

Inputs:
  Required:
  - path `artifact_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts get ... ; anx --json artifacts get ... ; anx artifacts get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts create`

Create artifact

```text
Generated Help: artifacts create

- Command ID: `artifacts.create`
- CLI path: `artifacts create`
- HTTP: `POST /artifacts`
- Stability: `beta`
- Input mode: `json-body`
- Why: Store content-addressed artifact metadata and payload (bytes, text, or structured packet JSON).
- Output: Returns `{ artifact }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `conflict`
- Concepts: `artifacts`, `write`
- Adjacent commands: `artifacts archive`, `artifacts content`, `artifacts get`, `artifacts list`, `artifacts purge`, `artifacts restore`, `artifacts trash`, `artifacts unarchive`

Inputs:
  Required:
  - body `artifact` (object)
  - body `content_type` (string)
  Optional:
  - body `actor_id` (string)
  - body `content` (any)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts create ... ; anx --json artifacts create ... ; anx artifacts create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts archive`

Archive artifact

```text
Generated Help: artifacts archive

- Command ID: `artifacts.archive`
- CLI path: `artifacts archive`
- HTTP: `POST /artifacts/{artifact_id}/archive`
- Stability: `beta`
- Input mode: `json-body`
- Why: Set archived_at on artifact metadata (orthogonal to trash lifecycle).
- Output: Returns `{ artifact }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `artifacts`, `write`
- Adjacent commands: `artifacts content`, `artifacts create`, `artifacts get`, `artifacts list`, `artifacts purge`, `artifacts restore`, `artifacts trash`, `artifacts unarchive`

Inputs:
  Required:
  - path `artifact_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts archive ... ; anx --json artifacts archive ... ; anx artifacts archive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts unarchive`

Unarchive artifact

```text
Generated Help: artifacts unarchive

- Command ID: `artifacts.unarchive`
- CLI path: `artifacts unarchive`
- HTTP: `POST /artifacts/{artifact_id}/unarchive`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear archived_at on artifact metadata.
- Output: Returns `{ artifact }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `artifacts`, `write`
- Adjacent commands: `artifacts archive`, `artifacts content`, `artifacts create`, `artifacts get`, `artifacts list`, `artifacts purge`, `artifacts restore`, `artifacts trash`

Inputs:
  Required:
  - path `artifact_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts unarchive ... ; anx --json artifacts unarchive ... ; anx artifacts unarchive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts trash`

Move artifact to trash

```text
Generated Help: artifacts trash

- Command ID: `artifacts.trash`
- CLI path: `artifacts trash`
- HTTP: `POST /artifacts/{artifact_id}/trash`
- Stability: `beta`
- Input mode: `json-body`
- Why: Move artifact metadata to trash with an explicit operator reason.
- Output: Returns `{ artifact }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Concepts: `artifacts`, `write`
- Adjacent commands: `artifacts archive`, `artifacts content`, `artifacts create`, `artifacts get`, `artifacts list`, `artifacts purge`, `artifacts restore`, `artifacts unarchive`

Inputs:
  Required:
  - path `artifact_id`
  - body `reason` (string)
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts trash ... ; anx --json artifacts trash ... ; anx artifacts trash ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts restore`

Restore artifact from trash

```text
Generated Help: artifacts restore

- Command ID: `artifacts.restore`
- CLI path: `artifacts restore`
- HTTP: `POST /artifacts/{artifact_id}/restore`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear trash lifecycle fields on an artifact after an explicit restore action.
- Output: Returns `{ artifact }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `artifacts`, `write`
- Adjacent commands: `artifacts archive`, `artifacts content`, `artifacts create`, `artifacts get`, `artifacts list`, `artifacts purge`, `artifacts trash`, `artifacts unarchive`

Inputs:
  Required:
  - path `artifact_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts restore ... ; anx --json artifacts restore ... ; anx artifacts restore ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts purge`

Permanently delete trashed artifact

```text
Generated Help: artifacts purge

- Command ID: `artifacts.purge`
- CLI path: `artifacts purge`
- HTTP: `POST /artifacts/{artifact_id}/purge`
- Stability: `beta`
- Input mode: `json-body`
- Why: Permanently delete a trashed artifact (human-gated).
- Output: Returns `{ purged, artifact_id }`.
- Error codes: `auth_required`, `human_only`, `invalid_token`, `not_found`, `conflict`
- Concepts: `artifacts`, `write`
- Adjacent commands: `artifacts archive`, `artifacts content`, `artifacts create`, `artifacts get`, `artifacts list`, `artifacts restore`, `artifacts trash`, `artifacts unarchive`

Inputs:
  Required:
  - path `artifact_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts purge ... ; anx --json artifacts purge ... ; anx artifacts purge ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards list`

List boards

```text
Generated Help: boards list

- Command ID: `boards.list`
- CLI path: `boards list`
- HTTP: `GET /boards`
- Stability: `beta`
- Input mode: `none`
- Why: Scan durable coordination boards and lightweight summaries.
- Output: Returns `{ boards, summaries }`.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `boards`
- Adjacent commands: `boards archive`, `boards cards create`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards list ... ; anx --json boards list ... ; anx boards list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards create`

Create board

```text
Generated Help: boards create

- Command ID: `boards.create`
- CLI path: `boards create`
- HTTP: `POST /boards`
- Stability: `beta`
- Input mode: `json-body`
- Why: Create a durable board over topics and cards.
- Output: Returns `{ board }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `boards`, `write`
- Adjacent commands: `boards archive`, `boards cards create`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - body `board.document_refs` (list<any>)
  - body `board.pinned_refs` (list<any>)
  - body `board.provenance.sources` (list<string>)
  - body `board.status` (string)
  - body `board.title` (string)
  Optional:
  - body `board.primary_topic_ref` (string)
  - body `board.provenance.by_field` (object)
  - body `board.provenance.notes` (string)
  Enum values: board.status: active, closed, paused

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards create ... ; anx --json boards create ... ; anx boards create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards get`

Get board

```text
Generated Help: boards get

- Command ID: `boards.get`
- CLI path: `boards get`
- HTTP: `GET /boards/{board_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve canonical board state and summary.
- Output: Returns `{ board, summary }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `boards`
- Adjacent commands: `boards archive`, `boards cards create`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards get ... ; anx --json boards get ... ; anx boards get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards archive`

Archive board

```text
Generated Help: boards archive

- Command ID: `boards.archive`
- CLI path: `boards archive`
- HTTP: `POST /boards/{board_id}/archive`
- Stability: `beta`
- Input mode: `json-body`
- Why: Soft-archive a board (orthogonal to business status; clears default list visibility).
- Output: Returns `{ board }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `boards`, `write`
- Adjacent commands: `boards cards create`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards archive ... ; anx --json boards archive ... ; anx boards archive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards unarchive`

Unarchive board

```text
Generated Help: boards unarchive

- Command ID: `boards.unarchive`
- CLI path: `boards unarchive`
- HTTP: `POST /boards/{board_id}/unarchive`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear archived_at on a board (restore default list visibility).
- Output: Returns `{ board }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `boards`, `write`
- Adjacent commands: `boards archive`, `boards cards create`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards unarchive ... ; anx --json boards unarchive ... ; anx boards unarchive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards trash`

Move board to trash

```text
Generated Help: boards trash

- Command ID: `boards.trash`
- CLI path: `boards trash`
- HTTP: `POST /boards/{board_id}/trash`
- Stability: `beta`
- Input mode: `json-body`
- Why: Move board to trash with an explicit operator reason.
- Output: Returns `{ board }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `boards`, `write`
- Adjacent commands: `boards archive`, `boards cards create`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  - body `reason` (string)
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards trash ... ; anx --json boards trash ... ; anx boards trash ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards restore`

Restore board from trash

```text
Generated Help: boards restore

- Command ID: `boards.restore`
- CLI path: `boards restore`
- HTTP: `POST /boards/{board_id}/restore`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear trash lifecycle fields on a board after an explicit restore action.
- Output: Returns `{ board }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `boards`, `write`
- Adjacent commands: `boards archive`, `boards cards create`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards restore ... ; anx --json boards restore ... ; anx boards restore ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards purge`

Permanently delete trashed board

```text
Generated Help: boards purge

- Command ID: `boards.purge`
- CLI path: `boards purge`
- HTTP: `POST /boards/{board_id}/purge`
- Stability: `beta`
- Input mode: `json-body`
- Why: Permanently delete a trashed board (human-gated).
- Output: Returns `{ purged, board_id }`.
- Error codes: `auth_required`, `human_only`, `invalid_token`, `not_found`, `conflict`
- Concepts: `boards`, `write`
- Adjacent commands: `boards archive`, `boards cards create`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards purge ... ; anx --json boards purge ... ; anx boards purge ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards cards`

Nested generated help topic.

```text
Generated Help: boards cards

Commands:
  boards cards create      Create card on board
  boards cards create-batch Batch create cards on board
  boards cards get         Get board-scoped card
  boards cards list        List board cards

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards cards ... ; anx --json boards cards ... ; anx boards cards ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `boards cards create`

Create card on board

```text
Generated Help: boards cards create

- Command ID: `boards.cards.create`
- CLI path: `boards cards create`
- HTTP: `POST /boards/{board_id}/cards`
- Stability: `beta`
- Input mode: `json-body`
- Why: Create a first-class card and attach it to a board.
- Output: Returns `{ card }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `boards`, `cards`, `write`
- Adjacent commands: `boards archive`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  - body `card.assignee_refs` (list<any>)
  - body `card.column_key` (string)
  - body `card.provenance.sources` (list<string>)
  - body `card.related_refs` (list<any>)
  - body `card.resolution_refs` (list<any>)
  - body `card.risk` (string)
  - body `card.summary` (string)
  - body `card.title` (string)
  Optional:
  - body `board_id` (string)
  - body `board_ref` (any)
  - body `card.after_card_id` (string)
  - body `card.before_card_id` (string)
  - body `card.definition_of_done` (list<string>)
  - body `card.document_ref` (string)
  - body `card.due_at` (datetime)
  - body `card.id` (string)
  - body `card.provenance.by_field` (object)
  - body `card.provenance.notes` (string)
  - body `card.resolution` (string)
  - body `card.topic_ref` (string)
  - body `if_board_updated_at` (datetime): Optimistic concurrency token. Copy `board.updated_at` from `anx boards get --board-id <board-id>`, `anx boards workspace --board-id <board-id>`, or the latest board mutation response.
  Enum values: card.column_key: backlog, blocked, done, in_progress, ready, review; card.resolution: canceled, done; card.risk: critical, high, low, medium

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards cards create ... ; anx --json boards cards create ... ; anx boards cards create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards cards create-batch`

Batch create cards on board

```text
Generated Help: boards cards create-batch

- Command ID: `boards.cards.batch_add`
- CLI path: `boards cards create-batch`
- HTTP: `POST /boards/{board_id}/cards/batch`
- Stability: `beta`
- Input mode: `json-body`
- Why: Create multiple cards in one transaction using a single board concurrency token.
- Output: Returns `{ board, cards }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `boards`, `cards`, `write`
- Adjacent commands: `boards archive`, `boards cards create`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  - body `items` (list<any>)
  Optional:
  - body `actor_id` (string): Defaults from the active CLI profile when omitted. Non-empty `--actor-id` overrides `actor_id` in the JSON body.
  - body `if_board_updated_at` (datetime): Optimistic concurrency token. Copy `board.updated_at` from `anx boards get --board-id <board-id>`, `anx boards workspace --board-id <board-id>`, or the latest board mutation response. You may pass `--if-board-updated-at` instead of embedding it in JSON.
  - body `request_key` (string): Idempotency key for the whole batch. Non-empty `--request-key` overrides `request_key` in the JSON body.

CLI input:
  - Provide a JSON object on stdin or via `--from-file`; it must include `items` (array of card create payloads).
  - Board id: `--board-id <id>` or a single positional `<board-id>` before flags (no other positionals).
  - `actor_id` defaults from the active profile when omitted from JSON; `--actor-id` sets or overrides it.
  - `--request-key` and `--if-board-updated-at`, when non-empty, override the same keys in the JSON body.

Agent tip: run `anx boards get --board-id <board-id> --json` (or `boards workspace`) first, copy `board.updated_at` into `if_board_updated_at`, or pass `--if-board-updated-at` from that value. Each item's `related_refs` must reference source threads not already backing another card on this board, or the server returns `conflict`.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards cards create-batch ... ; anx --json boards cards create-batch ... ; anx boards cards create-batch ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards cards get`

Get board-scoped card

```text
Generated Help: boards cards get

- Command ID: `boards.cards.get`
- CLI path: `boards cards get`
- HTTP: `GET /boards/{board_id}/cards/{card_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve a card through its board membership context.
- Output: Returns `{ card }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `boards`, `cards`
- Adjacent commands: `boards archive`, `boards cards create`, `boards cards create-batch`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  - path `card_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards cards get ... ; anx --json boards cards get ... ; anx boards cards get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs list`

List documents

```text
Generated Help: docs list

- Command ID: `docs.list`
- CLI path: `docs list`
- HTTP: `GET /docs`
- Stability: `beta`
- Input mode: `none`
- Why: Scan canonical document lineages.
- Output: Returns `{ documents }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `docs`
- Adjacent commands: `docs archive`, `docs create`, `docs get`, `docs purge`, `docs restore`, `docs revisions create`, `docs revisions get`, `docs revisions list`, `docs trash`, `docs unarchive`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs list ... ; anx --json docs list ... ; anx docs list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs create`

Create document

```text
Generated Help: docs create

- Command ID: `docs.create`
- CLI path: `docs create`
- HTTP: `POST /docs`
- Stability: `beta`
- Input mode: `json-body`
- Why: Create a canonical document lineage anchored to a typed subject ref.
- Output: Returns `{ document, revision }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `docs`, `write`
- Adjacent commands: `docs archive`, `docs get`, `docs list`, `docs purge`, `docs restore`, `docs revisions create`, `docs revisions get`, `docs revisions list`, `docs trash`, `docs unarchive`

Inputs:
  Required:
  - body `document.body_markdown` (string)
  - body `document.provenance.sources` (list<string>)
  - body `document.refs` (list<any>)
  - body `document.subject_ref` (string)
  - body `document.title` (string)
  Optional:
  - body `document.provenance.by_field` (object)
  - body `document.provenance.notes` (string)
  - body `document.summary` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs create ... ; anx --json docs create ... ; anx docs create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs get`

Get document

```text
Generated Help: docs get

- Command ID: `docs.get`
- CLI path: `docs get`
- HTTP: `GET /docs/{document_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve a document lineage and its current head revision.
- Output: Returns `{ document, revision }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `docs`
- Adjacent commands: `docs archive`, `docs create`, `docs list`, `docs purge`, `docs restore`, `docs revisions create`, `docs revisions get`, `docs revisions list`, `docs trash`, `docs unarchive`

Inputs:
  Required:
  - path `document_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs get ... ; anx --json docs get ... ; anx docs get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs trash`

Move document to trash

```text
Generated Help: docs trash

- Command ID: `docs.trash`
- CLI path: `docs trash`
- HTTP: `POST /docs/{document_id}/trash`
- Stability: `beta`
- Input mode: `json-body`
- Why: Move a document lineage to trash with an explicit operator reason.
- Output: Returns `{ document, revision }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `docs`, `write`
- Adjacent commands: `docs archive`, `docs create`, `docs get`, `docs list`, `docs purge`, `docs restore`, `docs revisions create`, `docs revisions get`, `docs revisions list`, `docs unarchive`

Inputs:
  Required:
  - path `document_id`
  - body `reason` (string)
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs trash ... ; anx --json docs trash ... ; anx docs trash ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs archive`

Archive document

```text
Generated Help: docs archive

- Command ID: `docs.archive`
- CLI path: `docs archive`
- HTTP: `POST /docs/{document_id}/archive`
- Stability: `beta`
- Input mode: `json-body`
- Why: Soft-archive a document lineage (orthogonal to head revision content).
- Output: Returns `{ document, revision }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `docs`, `write`
- Adjacent commands: `docs create`, `docs get`, `docs list`, `docs purge`, `docs restore`, `docs revisions create`, `docs revisions get`, `docs revisions list`, `docs trash`, `docs unarchive`

Inputs:
  Required:
  - path `document_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs archive ... ; anx --json docs archive ... ; anx docs archive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs unarchive`

Unarchive document

```text
Generated Help: docs unarchive

- Command ID: `docs.unarchive`
- CLI path: `docs unarchive`
- HTTP: `POST /docs/{document_id}/unarchive`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear archived_at on a document so it returns to default visibility.
- Output: Returns `{ document, revision }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `docs`, `write`
- Adjacent commands: `docs archive`, `docs create`, `docs get`, `docs list`, `docs purge`, `docs restore`, `docs revisions create`, `docs revisions get`, `docs revisions list`, `docs trash`

Inputs:
  Required:
  - path `document_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs unarchive ... ; anx --json docs unarchive ... ; anx docs unarchive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs restore`

Restore document from trash

```text
Generated Help: docs restore

- Command ID: `docs.restore`
- CLI path: `docs restore`
- HTTP: `POST /docs/{document_id}/restore`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear trash state on a document after an explicit restore action.
- Output: Returns `{ document, revision }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `docs`, `write`
- Adjacent commands: `docs archive`, `docs create`, `docs get`, `docs list`, `docs purge`, `docs revisions create`, `docs revisions get`, `docs revisions list`, `docs trash`, `docs unarchive`

Inputs:
  Required:
  - path `document_id`
  Optional:
  - body `actor_id` (string)
  - body `reason` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs restore ... ; anx --json docs restore ... ; anx docs restore ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs purge`

Permanently delete trashed document

```text
Generated Help: docs purge

- Command ID: `docs.purge`
- CLI path: `docs purge`
- HTTP: `POST /docs/{document_id}/purge`
- Stability: `beta`
- Input mode: `json-body`
- Why: Permanently delete a trashed document (human-gated).
- Output: Returns `{ purged, document_id }`.
- Error codes: `auth_required`, `human_only`, `invalid_token`, `not_found`, `conflict`
- Concepts: `docs`, `write`
- Adjacent commands: `docs archive`, `docs create`, `docs get`, `docs list`, `docs restore`, `docs revisions create`, `docs revisions get`, `docs revisions list`, `docs trash`, `docs unarchive`

Inputs:
  Required:
  - path `document_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs purge ... ; anx --json docs purge ... ; anx docs purge ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events get`

Get event by id

```text
Generated Help: events get

- Command ID: `events.get`
- CLI path: `events get`
- HTTP: `GET /events/{event_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Fetch one append-only event record by stable id.
- Output: Returns `{ event }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `events`
- Adjacent commands: `events archive`, `events create`, `events list`, `events restore`, `events stream`, `events trash`, `events unarchive`

Inputs:
  Required:
  - path `event_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx events get ... ; anx --json events get ... ; anx events get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events create`

Create event

```text
Generated Help: events create

- Command ID: `events.create`
- CLI path: `events create`
- HTTP: `POST /events`
- Stability: `beta`
- Input mode: `json-body`
- Why: Append an event that links first-class resources and evidence through typed refs.
- Output: Returns `{ event }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `events`, `write`
- Adjacent commands: `events archive`, `events get`, `events list`, `events restore`, `events stream`, `events trash`, `events unarchive`

Inputs:
  Required:
  - body `event.actor_id` (string)
  - body `event.provenance.sources` (list<string>)
  - body `event.refs` (list<any>)
  - body `event.summary` (string)
  - body `event.type` (string)
  Optional:
  - body `event.payload` (object)
  - body `event.provenance.by_field` (object)
  - body `event.provenance.notes` (string)
  - body `event.thread_ref` (string)
  Enum values: event.type (open): agent_notification_dismissed, agent_notification_read, board_card_added, board_card_archived, board_card_moved, board_card_trashed, board_created, board_updated, card_archived, card_created, card_moved, card_resolved, card_trashed, card_updated, decision_made, decision_needed, document_created, document_revised, document_revision_created, document_trashed, exception_raised, inbox_item_acknowledged, intervention_needed, message_posted, receipt_added, review_completed, topic_archived, topic_created, topic_restored, topic_status_changed, topic_trashed, topic_updated

Common authoring types:
  Communication: direct communication or important non-structured information
  - `message_posted`
  Decisions: request or record decisions tied to a topic
  - `decision_needed`
  - `decision_made`
  Interventions: single clear path exists, but a human must act to complete it
  - `intervention_needed`
  Topics and documents: durable subject and document lifecycle signals
  - `topic_created`, `topic_updated`, `topic_status_changed`
  - `document_created`, `document_revised`, `document_trashed`
  Boards and cards: workflow placement and movement
  - `board_created`, `board_updated`
  - `card_created`, `card_updated`, `card_moved`, `card_resolved`
  Exceptions: surface problems, risks, or escalations
  - `exception_raised`

Usually emitted by higher-level commands:
  - `receipt_added`: prefer `anx receipts create`
  - `review_completed`: prefer `anx reviews create`
  - `inbox_item_acknowledged`: prefer `anx inbox ack`

Local CLI notes:
  - Common open `event.type` values include `actor_statement`; the enum list above is illustrative, not exhaustive.
  - Use `--dry-run` with `--from-file` to validate and preview the request without sending the mutation.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx events create ... ; anx --json events create ... ; anx events create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events stream`

Stream events (SSE)

```text
Generated Help: events stream

- Command ID: `events.stream`
- CLI path: `events stream`
- HTTP: `GET /events/stream`
- Stability: `beta`
- Input mode: `none`
- Why: Long-lived SSE feed of workspace events with optional thread/type filters and Last-Event-ID resume.
- Output: Each SSE message is `event: …` with JSON data `{ "event": <event> }` (see core/docs/http-api.md).
- Error codes: `auth_required`, `invalid_token`
- Concepts: `events`
- Adjacent commands: `events archive`, `events create`, `events get`, `events list`, `events restore`, `events trash`, `events unarchive`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx events stream ... ; anx --json events stream ... ; anx events stream ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events tail`

Stream events (SSE)

```text
Generated Help: events tail

- Command ID: `events.stream`
- CLI path: `events stream`
- HTTP: `GET /events/stream`
- Stability: `beta`
- Input mode: `none`
- Why: Long-lived SSE feed of workspace events with optional thread/type filters and Last-Event-ID resume.
- Output: Each SSE message is `event: …` with JSON data `{ "event": <event> }` (see core/docs/http-api.md).
- Error codes: `auth_required`, `invalid_token`
- Concepts: `events`
- Adjacent commands: `events archive`, `events create`, `events get`, `events list`, `events restore`, `events trash`, `events unarchive`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx events tail ... ; anx --json events tail ... ; anx events tail ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events archive`

Archive event

```text
Generated Help: events archive

- Command ID: `events.archive`
- CLI path: `events archive`
- HTTP: `POST /events/{event_id}/archive`
- Stability: `beta`
- Input mode: `json-body`
- Why: Set archived_at on an append-only event record for filtered views.
- Output: Returns `{ event }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `events`, `write`
- Adjacent commands: `events create`, `events get`, `events list`, `events restore`, `events stream`, `events trash`, `events unarchive`

Inputs:
  Required:
  - path `event_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx events archive ... ; anx --json events archive ... ; anx events archive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events unarchive`

Unarchive event

```text
Generated Help: events unarchive

- Command ID: `events.unarchive`
- CLI path: `events unarchive`
- HTTP: `POST /events/{event_id}/unarchive`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear archived_at on an event.
- Output: Returns `{ event }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `events`, `write`
- Adjacent commands: `events archive`, `events create`, `events get`, `events list`, `events restore`, `events stream`, `events trash`

Inputs:
  Required:
  - path `event_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx events unarchive ... ; anx --json events unarchive ... ; anx events unarchive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events trash`

Move event to trash

```text
Generated Help: events trash

- Command ID: `events.trash`
- CLI path: `events trash`
- HTTP: `POST /events/{event_id}/trash`
- Stability: `beta`
- Input mode: `json-body`
- Why: Move event to trash with an explicit operator reason.
- Output: Returns `{ event }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Concepts: `events`, `write`
- Adjacent commands: `events archive`, `events create`, `events get`, `events list`, `events restore`, `events stream`, `events unarchive`

Inputs:
  Required:
  - path `event_id`
  - body `reason` (string)
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx events trash ... ; anx --json events trash ... ; anx events trash ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events restore`

Restore event from trash

```text
Generated Help: events restore

- Command ID: `events.restore`
- CLI path: `events restore`
- HTTP: `POST /events/{event_id}/restore`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear trash state on an event after an explicit restore action.
- Output: Returns `{ event }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `events`, `write`
- Adjacent commands: `events archive`, `events create`, `events get`, `events list`, `events stream`, `events trash`, `events unarchive`

Inputs:
  Required:
  - path `event_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx events restore ... ; anx --json events restore ... ; anx events restore ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox list`

List inbox items

```text
Generated Help: inbox list

- Command ID: `inbox.list`
- CLI path: `inbox list`
- HTTP: `GET /inbox`
- Stability: `beta`
- Input mode: `none`
- Why: Load the derived operator inbox generated from refs and canonical events.
- Output: Returns `{ items }`.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `inbox`
- Adjacent commands: `inbox acknowledge`, `inbox get`, `inbox stream`


View scoping:
  - `inbox list` is read from the active CLI identity's perspective.
  - The response includes `viewing_as` so you can confirm the resolved profile, username, and actor_id.
  - Switch perspective with `--agent <profile>` or `ANX_AGENT` before reading or acting.

Inbox categories:
  - `action_needed`: A human must decide, take direct action, or own the next step (includes prior decision and intervention queue signals).
  - `risk_exception`: Exceptions, stale cadence, or at-risk work items that need follow-up.
  - `attention`: Review or lighter operator focus (for example document attention).

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx inbox list ... ; anx --json inbox list ... ; anx inbox list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox get`

Get one inbox item

```text
Generated Help: inbox get

- Command ID: `inbox.get`
- CLI path: `inbox get`
- HTTP: `GET /inbox/{inbox_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Side-effect free read of one materialized inbox row.
- Output: Returns `{ item, generated_at, projection_freshness }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `inbox`
- Adjacent commands: `inbox acknowledge`, `inbox list`, `inbox stream`

Inputs:
  Required:
  - path `inbox_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx inbox get ... ; anx --json inbox get ... ; anx inbox get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox acknowledge`

Acknowledge inbox item

```text
Generated Help: inbox acknowledge

- Command ID: `inbox.acknowledge`
- CLI path: `inbox acknowledge`
- HTTP: `POST /inbox/{inbox_id}/acknowledge`
- Stability: `beta`
- Input mode: `json-body`
- Why: Suppress or clear a derived inbox item via a durable acknowledgment event.
- Output: Returns `{ event }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Concepts: `inbox`, `write`
- Adjacent commands: `inbox get`, `inbox list`, `inbox stream`

Inputs:
  Required:
  - path `inbox_id`
  - body `subject_ref` (string)
  Optional:
  - body `actor_id` (string)
  - body `inbox_item_id` (string)
  - body `note` (string)
  - body `refs` (list<any>)

CLI flags (`inbox acknowledge` / `inbox ack`):
  --inbox-item-id <id>   Inbox item id or list alias (see `inbox list`).
  --subject-ref <ref>    Typed subject ref; omitted ids may be resolved from `inbox list`.
  --actor-id <id>        Actor id (`me` uses the active profile's actor when configured).
  --from-file <path>     JSON body file (API request shape).
  Positional: inbox item id when not given via `--inbox-item-id`.
  Otherwise: JSON object on stdin (`inbox_item_id`, `subject_ref`, optional fields).

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx inbox acknowledge ... ; anx --json inbox acknowledge ... ; anx inbox acknowledge ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox ack`

Acknowledge inbox item

```text
Generated Help: inbox ack

- Command ID: `inbox.acknowledge`
- CLI path: `inbox acknowledge`
- HTTP: `POST /inbox/{inbox_id}/acknowledge`
- Stability: `beta`
- Input mode: `json-body`
- Why: Suppress or clear a derived inbox item via a durable acknowledgment event.
- Output: Returns `{ event }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Concepts: `inbox`, `write`
- Adjacent commands: `inbox get`, `inbox list`, `inbox stream`

Inputs:
  Required:
  - path `inbox_id`
  - body `subject_ref` (string)
  Optional:
  - body `actor_id` (string)
  - body `inbox_item_id` (string)
  - body `note` (string)
  - body `refs` (list<any>)

CLI flags (`inbox acknowledge` / `inbox ack`):
  --inbox-item-id <id>   Inbox item id or list alias (see `inbox list`).
  --subject-ref <ref>    Typed subject ref; omitted ids may be resolved from `inbox list`.
  --actor-id <id>        Actor id (`me` uses the active profile's actor when configured).
  --from-file <path>     JSON body file (API request shape).
  Positional: inbox item id when not given via `--inbox-item-id`.
  Otherwise: JSON object on stdin (`inbox_item_id`, `subject_ref`, optional fields).

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx inbox ack ... ; anx --json inbox ack ... ; anx inbox ack ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox stream`

Stream inbox items (SSE)

```text
Generated Help: inbox stream

- Command ID: `inbox.stream`
- CLI path: `inbox stream`
- HTTP: `GET /inbox/stream`
- Stability: `beta`
- Input mode: `none`
- Why: Server-sent events feed of inbox projection updates.
- Output: SSE `inbox_item` events with JSON payloads.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `inbox`
- Adjacent commands: `inbox acknowledge`, `inbox get`, `inbox list`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx inbox stream ... ; anx --json inbox stream ... ; anx inbox stream ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox tail`

Stream inbox items (SSE)

```text
Generated Help: inbox tail

- Command ID: `inbox.stream`
- CLI path: `inbox stream`
- HTTP: `GET /inbox/stream`
- Stability: `beta`
- Input mode: `none`
- Why: Server-sent events feed of inbox projection updates.
- Output: SSE `inbox_item` events with JSON payloads.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `inbox`
- Adjacent commands: `inbox acknowledge`, `inbox get`, `inbox list`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx inbox tail ... ; anx --json inbox tail ... ; anx inbox tail ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `derived rebuild`

Rebuild derived projections

```text
Generated Help: derived rebuild

- Command ID: `derived.rebuild`
- CLI path: `derived rebuild`
- HTTP: `POST /derived/rebuild`
- Stability: `beta`
- Input mode: `json-body`
- Why: Deterministic operator repair for inbox/thread projections.
- Output: Returns `{ ok: true }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `projections`, `maintenance`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx derived rebuild ... ; anx --json derived rebuild ... ; anx derived rebuild ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta commands`

List command registry metadata

```text
Generated Help: meta commands

- Command ID: `meta.commands.list`
- CLI path: `meta commands`
- HTTP: `GET /meta/commands`
- Stability: `stable`
- Input mode: `none`
- Why: Expose embedded Agent Nexus command metadata for discovery and codegen parity.
- Output: Returns generated command registry JSON.
- Error codes: `meta_unavailable`
- Concepts: `compatibility`
- Adjacent commands: `meta command`, `meta concept`, `meta concepts`, `meta handshake`, `meta health`, `meta livez`, `meta readyz`, `meta version`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx meta commands ... ; anx --json meta commands ... ; anx meta commands ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta command`

Get one command metadata entry

```text
Generated Help: meta command

- Command ID: `meta.commands.get`
- CLI path: `meta command`
- HTTP: `GET /meta/commands/{command_id}`
- Stability: `stable`
- Input mode: `none`
- Why: Resolve command metadata by stable command id.
- Output: Returns `{ command }`.
- Error codes: `meta_unavailable`, `not_found`
- Concepts: `compatibility`
- Adjacent commands: `meta commands`, `meta concept`, `meta concepts`, `meta handshake`, `meta health`, `meta livez`, `meta readyz`, `meta version`

Inputs:
  Required:
  - path `command_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx meta command ... ; anx --json meta command ... ; anx meta command ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta concepts`

List concept index

```text
Generated Help: meta concepts

- Command ID: `meta.concepts.list`
- CLI path: `meta concepts`
- HTTP: `GET /meta/concepts`
- Stability: `stable`
- Input mode: `none`
- Why: Group command metadata by concept tags.
- Output: Returns `{ concepts: [...] }`.
- Error codes: `meta_unavailable`
- Concepts: `compatibility`
- Adjacent commands: `meta command`, `meta commands`, `meta concept`, `meta handshake`, `meta health`, `meta livez`, `meta readyz`, `meta version`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx meta concepts ... ; anx --json meta concepts ... ; anx meta concepts ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta concept`

Get commands grouped by concept

```text
Generated Help: meta concept

- Command ID: `meta.concepts.get`
- CLI path: `meta concept`
- HTTP: `GET /meta/concepts/{concept_name}`
- Stability: `stable`
- Input mode: `none`
- Why: Expand one concept into related commands.
- Output: Returns `{ concept: {...} }`.
- Error codes: `meta_unavailable`, `not_found`
- Concepts: `compatibility`
- Adjacent commands: `meta command`, `meta commands`, `meta concepts`, `meta handshake`, `meta health`, `meta livez`, `meta readyz`, `meta version`

Inputs:
  Required:
  - path `concept_name`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx meta concept ... ; anx --json meta concept ... ; anx meta concept ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `receipts create`

Create receipt packet

```text
Generated Help: receipts create

- Command ID: `packets.receipts.create`
- CLI path: `receipts create`
- HTTP: `POST /packets/receipts`
- Stability: `beta`
- Input mode: `json-body`
- Why: Record structured delivery evidence anchored by `subject_ref`.
- Output: Returns `{ artifact, packet_kind, packet }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `packets`, `evidence`
- Adjacent commands: `reviews create`

Inputs:
  Required:
  - body `artifact` (object)
  - body `packet.changes_summary` (string)
  - body `packet.known_gaps` (list<string>)
  - body `packet.outputs` (list<any>)
  - body `packet.receipt_id` (string)
  - body `packet.subject_ref` (typed_ref)
  - body `packet.verification_evidence` (list<any>)
  Optional:
  - body `actor_id` (string)
  - body `request_key` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx receipts create ... ; anx --json receipts create ... ; anx receipts create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `reviews create`

Create review packet

```text
Generated Help: reviews create

- Command ID: `packets.reviews.create`
- CLI path: `reviews create`
- HTTP: `POST /packets/reviews`
- Stability: `beta`
- Input mode: `json-body`
- Why: Record a structured review over a receipt anchored to the same card as subject_ref.
- Output: Returns `{ artifact, packet_kind, packet }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `packets`, `evidence`
- Adjacent commands: `receipts create`

Inputs:
  Required:
  - body `artifact` (object)
  - body `packet.evidence_refs` (list<any>)
  - body `packet.notes` (string)
  - body `packet.outcome` (string)
  - body `packet.receipt_ref` (string)
  - body `packet.review_id` (string)
  - body `packet.subject_ref` (typed_ref)
  Optional:
  - body `actor_id` (string)
  - body `request_key` (string)
  Enum values: packet.outcome (strict): accept, escalate, revise

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx reviews create ... ; anx --json reviews create ... ; anx reviews create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `secret list`

List secrets

```text
Generated Help: secret list

- Command ID: `secrets.list`
- CLI path: `secret list`
- HTTP: `GET /secrets`
- Stability: `beta`
- Input mode: `none`
- Why: List workspace secret metadata without exposing values.
- Output: Returns `{ secrets }`.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `secrets`
- Adjacent commands: `secret create`, `secret delete`, `secret exec`, `secret get --reveal`, `secret update`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx secret list ... ; anx --json secret list ... ; anx secret list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `secret create`

Create secret

```text
Generated Help: secret create

- Command ID: `secrets.create`
- CLI path: `secret create`
- HTTP: `POST /secrets`
- Stability: `beta`
- Input mode: `json-body`
- Why: Store an encrypted workspace credential with metadata.
- Output: Returns `{ secret }` (metadata only, value is not echoed).
- Error codes: `auth_required`, `invalid_token`, `human_only`, `invalid_request`, `resource_exists`, `secrets_not_configured`
- Concepts: `secrets`, `write`
- Agent notes: Only human principals may create secrets.
- Adjacent commands: `secret delete`, `secret exec`, `secret get --reveal`, `secret list`, `secret update`

Inputs:
  Required:
  - body `name` (string)
  - body `value` (string)
  Optional:
  - body `description` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx secret create ... ; anx --json secret create ... ; anx secret create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `secret delete`

Delete secret

```text
Generated Help: secret delete

- Command ID: `secrets.delete`
- CLI path: `secret delete`
- HTTP: `DELETE /secrets/{secret_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Permanently remove a secret and its encrypted value.
- Output: Returns `{ deleted: true, secret_id }`.
- Error codes: `auth_required`, `invalid_token`, `human_only`, `not_found`, `secrets_not_configured`
- Concepts: `secrets`, `write`
- Agent notes: Only human principals may delete secrets.
- Adjacent commands: `secret create`, `secret exec`, `secret get --reveal`, `secret list`, `secret update`

Inputs:
  Required:
  - path `secret_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx secret delete ... ; anx --json secret delete ... ; anx secret delete ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `secret get --reveal`

Reveal secret value

```text
Generated Help: secret get --reveal

- Command ID: `secrets.reveal`
- CLI path: `secret get --reveal`
- HTTP: `POST /secrets/{secret_id}/reveal`
- Stability: `beta`
- Input mode: `none`
- Why: Decrypt and return a secret value. Logged in audit.
- Output: Returns `{ name, value }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`, `secrets_not_configured`
- Concepts: `secrets`
- Agent notes: Every reveal is logged in auth audit. POST (not GET) to prevent caching.
- Adjacent commands: `secret create`, `secret delete`, `secret exec`, `secret list`, `secret update`

Inputs:
  Required:
  - path `secret_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx secret get --reveal ... ; anx --json secret get --reveal ... ; anx secret get --reveal ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `secret exec`

Reveal multiple secrets by name

```text
Generated Help: secret exec

- Command ID: `secrets.reveal-batch`
- CLI path: `secret exec`
- HTTP: `POST /secrets/reveal-batch`
- Stability: `beta`
- Input mode: `json-body`
- Why: Batch-fetch secrets for env injection. Each reveal is audited.
- Output: Returns `{ secrets: [{ name, value }] }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`, `invalid_request`, `secrets_not_configured`
- Concepts: `secrets`
- Agent notes: Each resolved secret generates an audit event. Missing names return not_found.
- Adjacent commands: `secret create`, `secret delete`, `secret get --reveal`, `secret list`, `secret update`

Inputs:
  Required:
  - body `names` (list<string>)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx secret exec ... ; anx --json secret exec ... ; anx secret exec ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `secret update`

Update secret value

```text
Generated Help: secret update

- Command ID: `secrets.update`
- CLI path: `secret update`
- HTTP: `PUT /secrets/{secret_id}`
- Stability: `beta`
- Input mode: `json-body`
- Why: Replace an encrypted secret value.
- Output: Returns `{ secret }` (metadata only).
- Error codes: `auth_required`, `invalid_token`, `human_only`, `not_found`, `invalid_request`, `secrets_not_configured`
- Concepts: `secrets`, `write`
- Agent notes: Only human principals may update secrets.
- Adjacent commands: `secret create`, `secret delete`, `secret exec`, `secret get --reveal`, `secret list`

Inputs:
  Required:
  - path `secret_id`
  - body `value` (string)
  Optional:
  - body `description` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx secret update ... ; anx --json secret update ... ; anx secret update ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events list`

Compose backing-thread timeline reads with client-side thread/type/actor filters and preview summaries.

```text
Local Help: events list

- Kind: `local helper`
- Summary: Compose backing-thread timeline reads with client-side thread/type/actor filters and preview summaries.
- Composition: Fetches one or more backing-thread timelines locally, then filters and summarizes the events without changing contracts or core behavior. Use it as a diagnostic read; prefer `topics workspace` and card/board reads for normal coordination.
- JSON body: `thread_id`, `thread_ids`, `events`, `total_events`, `returned_events`
- Examples:
  - `anx events list --thread-id <thread-id> --type actor_statement --mine --full-id`
  - `anx events list --thread-id <thread-id> --max-events 10`

Flags:
  --thread-id <thread-id>      Thread id to inspect (repeatable).
  --type <event-type>          Repeatable event type filter.
  --types <csv>                Comma-separated event types.
  --actor-id <actor-id>        Filter to one actor id.
  --mine                       Resolve to the active profile actor_id.
  --max-events <n>             Keep the most recent matching events.
  --max <n>                    Alias for --max-events.
  --full-id                    Render full event ids in default text output (non-JSON).
  --include-archived           Include archived events in results.
  --archived-only              Show only archived events.
  --include-trashed            Include trashed events in results.
  --trashed-only               Show only trashed events.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx events list ... ; anx --json events list ... ; anx events list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events validate`

Validate an `events create` payload locally from stdin or `--from-file` without sending it.

```text
Local Help: events validate

- Kind: `local helper`
- Summary: Validate an `events create` payload locally from stdin or `--from-file` without sending it.
- Composition: Parses the same JSON body accepted by `events create`, runs local validation rules, and returns a validation preview envelope without contacting core.
- JSON body: `command`, `command_id`, `path_params`, `query`, `body`, `valid`
- Examples:
  - `cat event.json | anx events validate`
  - `anx events validate --from-file event.json`

Flags:
  --from-file <path>           Load the request body from a JSON file instead of stdin.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx events validate ... ; anx --json events validate ... ; anx events validate ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events explain`

Explain known event-type conventions, required refs, and validation hints, including when `message_posted` targets a backing-thread message stream.

```text
Local Help: events explain

- Kind: `local helper`
- Summary: Explain known event-type conventions, required refs, and validation hints, including when `message_posted` targets a backing-thread message stream.
- Composition: Formats the embedded event reference and validation guidance into a plain-text reference without sending a request. Use it to confirm when `message_posted` is required for a visible backing-thread message in the web UI Messages tab.
- JSON body: `event_type`, `known`, `required_refs`, `payload_requirements`, `examples`, `hint`
- Examples:
  - `anx events explain`
  - `anx events explain message_posted`
  - `anx events explain review_completed`

Flags:
  <event-type>                 Optional event type to focus on; omit it to list known event types.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx events explain ... ; anx --json events explain ... ; anx events explain ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts inspect`

Fetch artifact metadata and resolved content in one command for operator inspection.

```text
Local Help: artifacts inspect

- Kind: `local helper`
- Summary: Fetch artifact metadata and resolved content in one command for operator inspection.
- Composition: Loads artifact metadata with `artifacts get`, then fetches content with `artifacts content` using the resolved artifact id.
- JSON body: `artifact`, `content`, `content_headers`, `content_text`, `content_base64`
- Examples:
  - `anx artifacts inspect --artifact-id <artifact-id>`
  - `anx artifacts inspect <artifact-id-or-alias>`

Flags:
  --artifact-id <artifact-id>  Artifact id or unique alias to inspect.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts inspect ... ; anx --json artifacts inspect ... ; anx artifacts inspect ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads inspect`

Diagnostic backing-thread bundle: compose one view from read-only thread data and related `inbox list` items.

```text
Local Help: threads inspect

- Kind: `local helper`
- Summary: Diagnostic backing-thread bundle: compose one view from read-only thread data and related `inbox list` items.
- Composition: Resolves one thread by id or discovery filters, loads read-only thread projections, then filters inbox items client-side by `thread_id`. Prefer `topics workspace` for primary operator coordination when you have a topic id.
- JSON body: `thread`, `context`, `collaboration`, `inbox`
- Examples:
  - `anx threads inspect --thread-id <thread-id>`
  - `anx threads inspect --status active --type initiative --full-id`

Flags:
  --thread-id <thread-id>      Thread id to inspect.
  --status <status>            Discover one thread by status.
  --priority <priority>        Discover one thread by priority.
  --stale <bool>               Discover one thread by stale state.
  --tag <tag>                  Repeatable discovery tag filter.
  --cadence <cadence>          Repeatable discovery cadence filter.
  --type <thread-type>         Local discovery filter after `threads list`.
  --max-events <n>             Maximum recent context events to include.
  --include-artifact-content   Include artifact content previews from the underlying read-only thread views.
  --full-id                    Render full event and inbox ids in default text output (non-JSON).


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx threads inspect ... ; anx --json threads inspect ... ; anx threads inspect ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads workspace`

Read-only backing-thread workspace projection: context, inbox, recommendation review, and related-thread signals in one command.

```text
Local Help: threads workspace

- Kind: `local helper`
- Summary: Read-only backing-thread workspace projection: context, inbox, recommendation review, and related-thread signals in one command.
- Composition: Resolves one thread by id or discovery filters, loads read-only thread projections, adds thread-scoped inbox items, and follows related thread refs for diagnostic review. Prefer `topics workspace` for normal operator coordination.
- JSON body: `thread`, `context`, `collaboration`, `inbox`, `pending_decisions`, `related_threads`, `related_recommendations`, `related_decisions`, `follow_up`
- Examples:
  - `anx threads workspace --thread-id <thread-id> --full-id`
  - `anx threads workspace --status active --type initiative --full-summary`

Flags:
  --thread-id <thread-id>      Thread id to inspect.
  --status <status>            Discover one thread by status.
  --priority <priority>        Discover one thread by priority.
  --stale <bool>               Discover one thread by stale state.
  --tag <tag>                  Repeatable discovery tag filter.
  --cadence <cadence>          Repeatable discovery cadence filter.
  --type <thread-type>         Local discovery filter after `threads list`.
  --max-events <n>             Maximum recent context events to include.
  --include-artifact-content   Include artifact content previews from the underlying read-only thread views.
  --full-summary               Show full recommendation/decision summaries in default text output (non-JSON).
  --full-id                    Render full event and inbox ids in default text output (non-JSON).


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx threads workspace ... ; anx --json threads workspace ... ; anx threads workspace ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads recommendations`

Compose a diagnostic recommendation-oriented review of one backing thread with related follow-up context.

```text
Local Help: threads recommendations

- Kind: `local helper`
- Summary: Compose a diagnostic recommendation-oriented review of one backing thread with related follow-up context.
- Composition: Loads the read-only thread context, inbox, and related-thread review context to highlight recommendation signals and follow-up hints without changing state. Prefer `topics workspace` for the main coordination read when a topic exists.
- JSON body: `thread`, `recommendations`, `decision_requests`, `decisions`, `pending_decisions`, `related_threads`, `related_recommendations`, `related_decision_requests`, `related_decisions`, `warnings`, `follow_up`
- Examples:
  - `anx threads recommendations --thread-id <thread-id>`
  - `anx threads recommendations --status active --type initiative --full-summary`

Flags:
  --thread-id <thread-id>      Thread id to inspect.
  --status <status>            Discover one thread by status.
  --priority <priority>        Discover one thread by priority.
  --stale <bool>               Discover one thread by stale state.
  --tag <tag>                  Repeatable discovery tag filter.
  --cadence <cadence>          Repeatable discovery cadence filter.
  --type <thread-type>         Local discovery filter after `threads list`.
  --max-events <n>             Maximum recent context events to include.
  --include-artifact-content   Include artifact content previews from the underlying read-only thread views.
  --include-related-event-content Hydrate related review items with full `events.get` payloads.
  --full-summary               Show full recommendation/decision summaries in default text output (non-JSON).
  --full-id                    Render full event and inbox ids in default text output (non-JSON).


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx threads recommendations ... ; anx --json threads recommendations ... ; anx threads recommendations ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards workspace`

Canonical board read path: load one board's workspace: optional primary topic, cards by column, linked documents, inbox items, and summary.

```text
Local Help: boards workspace

- Kind: `local helper`
- Summary: Canonical board read path: load one board's workspace: optional primary topic, cards by column, linked documents, inbox items, and summary.
- Composition: Resolves a board by id, fetches the projection workspace with per-card thread backing and renders cards grouped by canonical column order (backlog, ready, in_progress, blocked, review, done).
- JSON body: `board_id`, `board`, `primary_topic`, `cards`, `documents`, `inbox`, `board_summary`, `projection_freshness`, `board_summary_freshness`, `warnings`, `section_kinds`, `generated_at`
- Examples:
  - `anx boards workspace --board-id <board-id>`
  - `anx boards workspace --board-id board_product_launch`

Flags:
  --board-id <board-id>        Board id or unique prefix to load.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards workspace ... ; anx --json boards workspace ... ; anx boards workspace ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards cards list`

List all cards on a board in canonical column order without hydrating thread details.

```text
Local Help: boards cards list

- Kind: `local helper`
- Summary: List all cards on a board in canonical column order without hydrating thread details.
- Composition: Fetches the raw card list for a board ordered by canonical column sequence and per-column rank.
- JSON body: `board_id`, `cards`
- Examples:
  - `anx boards cards list --board-id <board-id>`

Flags:
  --board-id <board-id>        Board id to list cards for.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards cards list ... ; anx --json boards cards list ... ; anx boards cards list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs propose-update`

Stage a document update proposal locally and show the content diff before applying it.

```text
Local Help: docs propose-update

- Kind: `local helper`
- Summary: Stage a document update proposal locally and show the content diff before applying it.
- Composition: Fetches the current document revision with `docs get`, computes a local diff against the proposed update, and persists a proposal file instead of sending the update immediately.
- JSON body: `proposal_id`, `target_command_id`, `path`, `body`, `diff`, `apply_command`
- Examples:
  - `anx docs propose-update --document-id <document-id> --content-file <path>`
  - `cat update.json | anx docs propose-update --document-id <document-id>`

Flags:
  --document-id <document-id>  Document id to update.
  --content-file <path>        Load multiline content from a file into the JSON payload.
  --from-file <path>           Load the full JSON update body from a file.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs propose-update ... ; anx --json docs propose-update ... ; anx docs propose-update ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs content`

Show the current document content together with authoritative head revision metadata.

```text
Local Help: docs content

- Kind: `local helper`
- Summary: Show the current document content together with authoritative head revision metadata.
- Composition: Loads `docs get`, then renders the current revision content and metadata in one operator-friendly response.
- JSON body: `document`, `revision`, `content`, `status_code`, `headers`
- Examples:
  - `anx docs content --document-id <document-id>`
  - `anx docs content <document-id-or-alias>`

Flags:
  --document-id <document-id>  Document id or unique alias to inspect.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs content ... ; anx --json docs content ... ; anx docs content ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs validate-update`

Validate a `docs.revisions.create` payload locally from stdin or file without sending the mutation.

```text
Local Help: docs validate-update

- Kind: `local helper`
- Summary: Validate a `docs.revisions.create` payload locally from stdin or file without sending the mutation.
- Composition: Parses the same body accepted by `docs.revisions.create`, expands `--content-file` when present, and returns a validation preview envelope without contacting core.
- JSON body: `command`, `command_id`, `path_params`, `query`, `body`, `valid`
- Examples:
  - `cat update.json | anx docs validate-update --document-id <document-id>`
  - `anx docs validate-update --document-id <document-id> --content-file body.md`

Flags:
  --document-id <document-id>  Document id to validate against.
  --content-file <path>        Load multiline content from a file into the JSON payload.
  --from-file <path>           Load the full JSON update body from a file.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs validate-update ... ; anx --json docs validate-update ... ; anx docs validate-update ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs apply`

Apply a previously staged document update proposal.

```text
Local Help: docs apply

- Kind: `local helper`
- Summary: Apply a previously staged document update proposal.
- Composition: Loads the local proposal by exact id or unique prefix, validates it again, then sends the underlying `docs.revisions.create` request.
- JSON body: `proposal_id`, `target_command_id`, `applied`, `kept`, `result`
- Examples:
  - `anx docs apply --proposal-id <proposal-id>`
  - `anx docs apply <proposal-id-prefix>`

Flags:
  --proposal-id <proposal-id>  Proposal id or unique prefix to apply.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs apply ... ; anx --json docs apply ... ; anx docs apply ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta skill`

Render a bundled editor-specific skill file from the canonical ANX agent guide.

```text
Local Help: meta skill

- Kind: `local helper`
- Summary: Render a bundled editor-specific skill file from the canonical ANX agent guide.
- Composition: Pure local helper. Renders a maintained skill document from the bundled agent guide and optionally writes it to a chosen file or directory.
- JSON body: `target`, `content`, `default_file`, `written_files`, `guide_topic`, `skill_name`
- Examples:
  - `anx meta skill cursor`
  - `anx meta skill cursor --write-dir ~/.cursor/skills/anx-cli-onboard`
  - `anx meta skill --target cursor --write-file ./SKILL.md`

Flags:
  <target>                     Skill target to render. Currently supported: `cursor`.
  --target <target>            Flag form of the skill target.
  --write-file <path>          Write the rendered skill to this exact path.
  --write-dir <dir>            Write the rendered skill into this directory using its default filename.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx meta skill ... ; anx --json meta skill ... ; anx meta skill ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge install`

Install `anx-agent-bridge` into a dedicated Python 3.11+ virtualenv and expose a PATH wrapper.

```text
Local Help: bridge install

- Kind: `local helper`
- Summary: Install `anx-agent-bridge` into a dedicated Python 3.11+ virtualenv and expose a PATH wrapper.
- Composition: Pure local bootstrap helper with network package download. Creates or reuses a venv, installs the bridge package from the GitHub subdirectory at a pinned git ref (defaults to the running CLI release tag), and writes a thin launcher script.
- JSON body: `install_dir`, `bin_dir`, `wrapper_path`, `python`, `bridge_binary`, `package_ref`
- Examples:
  - `anx bridge install`
  - `anx bridge install --ref main --with-dev`

Flags:
  --python <exe>               Preferred Python executable. Default probes for Python 3.11+.
  --install-dir <dir>          Root directory for the managed bridge virtualenv.
  --bin-dir <dir>              Directory where the `anx-agent-bridge` wrapper should be written.
  --ref <git-ref>              Git ref to install from. Defaults to the running CLI's version tag (e.g. `v0.3.2`) so the bridge matches this binary; use `main` for the latest commit on the default branch.
  --with-dev                   Also install bridge test dependencies.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx bridge install ... ; anx --json bridge install ... ; anx bridge install ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge import-auth`

Copy an existing `anx` profile and key into bridge auth state for one bridge config.

```text
Local Help: bridge import-auth

- Kind: `local helper`
- Summary: Copy an existing `anx` profile and key into bridge auth state for one bridge config.
- Composition: Pure local helper. Reads an existing `anx` profile plus Ed25519 key material, converts it into bridge auth state, writes it to the bridge config's `[auth].state_path`, and syncs `[anx].base_url` when the config still has the default local value.
- JSON body: `config_path`, `auth_state_path`, `profile_path`, `profile_agent`, `username`, `actor_id`, `agent_id`, `key_id`
- Examples:
  - `anx bridge import-auth --config ./agent.toml --from-profile agent-a`
  - `anx --agent agent-a bridge import-auth --config ./agent.toml`

Flags:
  --config <path>              Bridge config whose auth state should be populated.
  --from-profile <agent>       Existing `anx` profile name to import. Defaults to the active CLI profile.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx bridge import-auth ... ; anx --json bridge import-auth ... ; anx bridge import-auth ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge init-config`

Write a minimal agent bridge TOML config with the pending-until-check-in lifecycle baked in.

```text
Local Help: bridge init-config

- Kind: `local helper`
- Summary: Write a minimal agent bridge TOML config with the pending-until-check-in lifecycle baked in.
- Composition: Pure local helper. Renders one minimal bridge config template with explicit workspace-id and readiness settings; optionally writes it to disk.
- JSON body: `kind`, `output`, `workspace_id`, `handle`, `content`
- Examples:
  - `anx bridge init-config --kind subprocess --output ./agent.toml --workspace-id ws_main --handle myagent --adapter-entrypoint ./adapter.py`
  - `anx bridge init-config --kind python-plugin --output ./agent.toml --workspace-id ws_main --handle myagent --plugin-module my_bridge --plugin-factory build_adapter`

Flags:
  --kind <subprocess|python-plugin> Template kind to render.
  --output <path>              Write the rendered TOML to a file. Omit to print it.
  --base-url <url>             ANX base URL for `[anx].base_url` (defaults to active CLI profile base URL).
  --workspace-id <id>          Durable ANX workspace id. Do not use a slug or UI path segment.
  --workspace-name <name>      Display name for `[anx].workspace_name`.
  --workspace-url <url>        Optional `[anx].workspace_url`.
  --handle <name>              Agent handle (required); must match the principal username for bridge-managed registration.
  --auth-state-path <path>     Optional `[auth].state_path` override.
  --state-dir <path>           Optional `[agent].state_dir` override prefix.
  --adapter-entrypoint <path>  Subprocess template: script path used as the second element of `[adapter].command` after python3.
  --plugin-module <module>     python-plugin template: Python module for `[adapter].plugin_module`.
  --plugin-factory <callable>  python-plugin template: factory name for `[adapter].plugin_factory`.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx bridge init-config ... ; anx --json bridge init-config ... ; anx bridge init-config ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge workspace-id`

Discover durable workspace ids from an existing agent wake registration.

```text
Local Help: bridge workspace-id

- Kind: `local helper`
- Summary: Discover durable workspace ids from an existing agent wake registration.
- Composition: Uses the active `anx` auth/profile to read agent principal registration metadata and extract enabled workspace bindings so bridge bootstrap can reuse the real durable workspace id instead of guessing.
- JSON body: `agent_id`, `handle`, `actor_id`, `registration_status`, `workspace_ids`, `workspace_bindings`
- Examples:
  - `anx --agent agent-a bridge workspace-id --handle myagent`

Flags:
  --handle <name>              Agent handle whose wake registration should be inspected.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx bridge workspace-id ... ; anx --json bridge workspace-id ... ; anx bridge workspace-id ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge doctor`

Validate bridge install, config presence, and registration readiness without starting the daemon.

```text
Local Help: bridge doctor

- Kind: `local helper`
- Summary: Validate bridge install, config presence, and registration readiness without starting the daemon.
- Composition: Pure local helper plus optional bridge CLI calls. Probes Python, the managed install, and `registration status` for a supplied config.
- JSON body: `checks`, `registration`, `bridge_binary`, `python`
- Examples:
  - `anx bridge doctor`
  - `anx bridge doctor --config ./agent.toml`

Flags:
  --config <path>              Bridge config to validate with `registration status`.
  --python <exe>               Preferred Python executable. Default probes for Python 3.11+.
  --install-dir <dir>          Root directory for the managed bridge virtualenv.
  --bin-dir <dir>              Directory where the managed `anx-agent-bridge` wrapper should exist.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx bridge doctor ... ; anx --json bridge doctor ... ; anx bridge doctor ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge start`

Start a managed bridge daemon for one config file.

```text
Local Help: bridge start

- Kind: `local helper`
- Summary: Start a managed bridge daemon for one config file.
- Composition: Pure local helper. Resolves the installed `anx-agent-bridge` binary, infers the config role, launches the daemon in the background, and records pid/log metadata in a per-config manager directory.
- JSON body: `kind`, `config_path`, `pid`, `log_path`, `process_state_path`, `command`
- Examples:
  - `anx bridge start --config ./agent.toml`

Flags:
  --config <path>              Bridge config to start. The config must contain `[agent]`.
  --install-dir <dir>          Root directory for the managed bridge virtualenv.
  --bin-dir <dir>              Directory where the managed `anx-agent-bridge` wrapper should exist.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx bridge start ... ; anx --json bridge start ... ; anx bridge start ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge stop`

Stop a managed bridge daemon for one config file.

```text
Local Help: bridge stop

- Kind: `local helper`
- Summary: Stop a managed bridge daemon for one config file.
- Composition: Pure local helper. Reads the per-config manager state, sends SIGTERM, and records the stopped timestamp once the daemon exits.
- JSON body: `kind`, `config_path`, `pid`, `stopped_at`, `last_signal`
- Examples:
  - `anx bridge stop --config ./agent.toml --force`

Flags:
  --config <path>              Managed config to stop.
  --force                      Escalate to SIGKILL if SIGTERM does not stop the daemon before the timeout.
  --timeout-seconds <n>        How long to wait after SIGTERM before failing or force-killing.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx bridge stop ... ; anx --json bridge stop ... ; anx bridge stop ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge restart`

Restart a managed bridge daemon for one config file.

```text
Local Help: bridge restart

- Kind: `local helper`
- Summary: Restart a managed bridge daemon for one config file.
- Composition: Pure local helper. Stops the existing managed process if one is present, then launches a fresh daemon and updates the manager state.
- JSON body: `kind`, `config_path`, `pid`, `log_path`, `process_state_path`
- Examples:
  - `anx bridge restart --config ./agent.toml`

Flags:
  --config <path>              Managed config to restart.
  --force                      Force-kill during the stop phase if needed.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx bridge restart ... ; anx --json bridge restart ... ; anx bridge restart ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge status`

Inspect managed process state for a bridge config.

```text
Local Help: bridge status

- Kind: `local helper`
- Summary: Inspect managed process state for a bridge config.
- Composition: Pure local helper plus optional bridge CLI calls. Reports the background process state, log path, and agent registration readiness when available.
- JSON body: `kind`, `managed`, `running`, `pid`, `log_path`, `process_state_path`, `registration`
- Examples:
  - `anx bridge status --config ./agent.toml`

Flags:
  --config <path>              Managed config to inspect.
  --install-dir <dir>          Root directory for the managed bridge virtualenv.
  --bin-dir <dir>              Directory where the managed `anx-agent-bridge` wrapper should exist.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx bridge status ... ; anx --json bridge status ... ; anx bridge status ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge logs`

Read recent log lines for a managed bridge config.

```text
Local Help: bridge logs

- Kind: `local helper`
- Summary: Read recent log lines for a managed bridge config.
- Composition: Pure local helper. Reads the per-config managed log file and returns the last N lines without requiring direct shell access.
- JSON body: `kind`, `config_path`, `log_path`, `lines`, `content`
- Examples:
  - `anx bridge logs --config ./agent.toml --lines 200`

Flags:
  --config <path>              Managed config whose log should be tailed.
  --lines <n>                  How many recent lines to return. Default is 80.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx bridge logs ... ; anx --json bridge logs ... ; anx bridge logs ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `import scan`

Scan a folder or zip archive into a normalized inventory with text cache, repo-root hints, and cluster hints.

```text
Local Help: import scan

- Kind: `local helper`
- Summary: Scan a folder or zip archive into a normalized inventory with text cache, repo-root hints, and cluster hints.
- Composition: Pure local filesystem helper. Expands `.zip` inputs, ignores obvious generated junk, fingerprints files, caches readable text, and emits `inventory.jsonl` plus `scan-summary.json`.
- JSON body: `input`, `scan_root`, `extracted_root`, `inventory`, `file_count`, `counts_by_category`, `counts_by_cluster_hint`, `repo_roots`
- Examples:
  - `anx import scan --input ./workspace.zip`
  - `anx import scan --input ./vault --out ./.anx-import/vault`

Flags:
  --input <path>               Directory or `.zip` archive to scan.
  --out <dir>                  Output directory. Defaults to `./.anx-import/<source-name>`.
  --max-preview-bytes <n>      Maximum bytes to keep for preview extraction.
  --max-text-cache-bytes <n>   Maximum text-file size cached verbatim for later doc creation.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx import scan ... ; anx --json import scan ... ; anx import scan ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `import dedupe`

Create exact and probable duplicate reports from a scan inventory with conservative skip recommendations.

```text
Local Help: import dedupe

- Kind: `local helper`
- Summary: Create exact and probable duplicate reports from a scan inventory with conservative skip recommendations.
- Composition: Pure local helper. Uses normalized text hashes for readable content and raw SHA-256 for everything else; exact drops are recommended, probable duplicates are review-only.
- JSON body: `inventory`, `exact_duplicates`, `probable_duplicates`, `recommended_skip_ids`
- Examples:
  - `anx import dedupe --inventory ./.anx-import/workspace/inventory.jsonl`
  - `anx import dedupe ./.anx-import/workspace/inventory.jsonl --out ./.anx-import/workspace`

Flags:
  --inventory <path>           Inventory produced by `anx import scan`. Positional form also supported.
  --out <dir>                  Output directory. Defaults to the inventory directory.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx import dedupe ... ; anx --json import dedupe ... ; anx import dedupe ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `import plan`

Build a conservative import plan that prefers collector threads, hub docs, dedupe-first writes, and low orphan rates.

```text
Local Help: import plan

- Kind: `local helper`
- Summary: Build a conservative import plan that prefers collector threads, hub docs, dedupe-first writes, and low orphan rates.
- Composition: Pure local helper. Classifies inventory items into docs, artifacts, repo bundles, review bundles, and collector/hub structures. It writes `plan.json` plus `plan-preview.md` without sending requests.
- JSON body: `source_name`, `inventory`, `dedupe`, `principles`, `objects`, `skipped`, `review_bundles`, `notes`
- Examples:
  - `anx import plan --inventory ./.anx-import/workspace/inventory.jsonl`
  - `anx import plan --inventory ./.anx-import/workspace/inventory.jsonl --dedupe ./.anx-import/workspace/dedupe.json --source-name 'workspace export'`

Flags:
  --inventory <path>           Inventory produced by `anx import scan`. Positional form also supported.
  --dedupe <path>              Dedupe report. Defaults to sibling `dedupe.json`.
  --out <dir>                  Output directory. Defaults to the inventory directory.
  --source-name <name>         High-signal display name used in titles, tags, and provenance. Defaults from the inventory directory.
  --collector-threshold <n>    Minimum cluster size that triggers a collector thread.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx import plan ... ; anx --json import plan ... ; anx import plan ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `import apply`

Write payload previews for a plan and optionally execute topic/artifact/doc creates in dependency order.

```text
Local Help: import apply

- Kind: `local helper`
- Summary: Write payload previews for a plan and optionally execute topic/artifact/doc creates in dependency order.
- Composition: Local helper with optional network writes. Always writes payload previews first; when `--execute` is set it creates topics, then artifacts, then docs, substituting `$REF:<key>` placeholders after upstream IDs are known.
- JSON body: `plan`, `execute`, `results`, `refs`
- Examples:
  - `anx import apply --plan ./.anx-import/workspace/plan.json`
  - `anx import apply --plan ./.anx-import/workspace/plan.json --execute --agent importer`

Flags:
  --plan <path>                Plan produced by `anx import plan`. Positional form also supported.
  --out <dir>                  Output directory for payload previews and apply results. Defaults to `<plan-dir>/apply`.
  --execute                    Actually call `topics create`, `artifacts create`, and `docs create`. Default is preview-only.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx import apply ... ; anx --json import apply ... ; anx import apply ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```
