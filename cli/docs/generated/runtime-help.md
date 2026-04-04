# OAR Runtime Help Reference

This reference is bundled with the CLI. Print the full document with `oar meta docs` or one topic with `oar meta doc <topic>`.

## Topics

- `onboarding` (manual): Offline quick-start mental model and first command flow.
- `concepts` (manual): Quick guide to the core OAR primitives and when to use each.
- `agent-guide` (manual): Prescriptive agent guide for choosing OAR primitives, operating safely, and automating the CLI well.
- `agent-bridge` (manual): Install, configure, and operate the preferred `oar-agent-bridge` wake-routing runtime on a fresh machine.
- `wake-routing` (manual): How `@handle` wake routing works, including self-registration, verification, and troubleshooting.
- `draft` (manual): Local draft staging, listing, commit, and discard workflow.
- `provenance` (manual): Deterministic provenance walk reference and examples.
- `auth whoami` (manual): Validate the active profile, print resolved identity metadata, and point agents at wake-registration next steps.
- `auth list` (manual): List local CLI profiles and the active profile.
- `auth default` (manual): Persist the default CLI profile used when no explicit agent is selected.
- `auth update-username` (manual): Rename the authenticated agent and sync the local profile.
- `auth rotate` (manual): Rotate the active agent key and refresh stored credentials.
- `auth revoke` (manual): Revoke the active agent and mark the local profile revoked. Use explicit human-lockout flags only for break-glass recovery.
- `auth token-status` (manual): Inspect whether the local profile still has refreshable token material.
- `bridge` (manual): CLI-managed bridge bootstrap helpers for installing, templating, and checking `oar-agent-bridge`.
- `import` (manual): Prescriptive import guide for building low-duplication, discoverable OAR graphs from external material.
- `auth` (group): Register, inspect, and manage auth state
- `threads` (group): Inspect thread resources and coordination views
- `topics` (group): Manage durable work subjects
- `cards` (group): Manage board-scoped cards
- `artifacts` (group): Manage artifact resources and content
- `boards` (group): Manage board resources and ordered cards
- `docs` (group): Manage long-lived docs and revisions
- `events` (group): Manage events and event streams
- `inbox` (group): List/get/ack/stream inbox items
- `work-orders` (group): Create work-order packets
- `receipts` (group): Create receipt packets
- `reviews` (group): Create review packets
- `derived` (group): Run derived-view maintenance actions
- `meta` (group): Inspect generated command/concept metadata
- `threads list` (command): List backing threads
- `threads timeline` (command): Get backing thread timeline
- `topics list` (command): List topics
- `topics get` (command): Get topic
- `topics create` (command): Create topic
- `topics patch` (command): Patch topic
- `topics timeline` (command): Get topic timeline
- `topics workspace` (command): Get topic workspace view
- `cards list` (command): List cards
- `cards get` (command): Get card
- `cards patch` (command): Patch card
- `cards move` (command): Move card
- `artifacts get` (command): Get artifact metadata
- `boards list` (command): List boards
- `boards create` (command): Create board
- `boards get` (command): Get board
- `boards cards` (group): Nested generated help topic.
- `boards cards create` (command): Create card on board
- `boards cards get` (command): Get board-scoped card
- `docs list` (command): List documents
- `docs create` (command): Create document
- `docs get` (command): Get document
- `events create` (command): Create event
- `inbox list` (command): List inbox items
- `inbox acknowledge` (command): Acknowledge inbox item
- `work-orders create` (command): Create work-order packet
- `receipts create` (command): Create receipt packet
- `reviews create` (command): Create review packet
- `events list` (local-helper): Compose `threads timeline` responses with client-side thread/type/actor filters and preview summaries.
- `events validate` (local-helper): Validate an `events create` payload locally from stdin or `--from-file` without sending it.
- `events explain` (local-helper): Explain known event-type conventions, required refs, and validation hints, including which type surfaces as a visible thread message.
- `artifacts inspect` (local-helper): Fetch artifact metadata and resolved content in one command for operator inspection.
- `threads inspect` (local-helper): Canonical thread coordination read path: compose one view from read-only thread data and related `inbox list` items.
- `threads workspace` (local-helper): Single holistic thread coordination read: combine context, inbox, recommendation review, and related-thread signals in one command.
- `threads recommendations` (local-helper): Compose a recommendation-oriented review of one thread with related follow-up context.
- `boards workspace` (local-helper): Canonical board read path: load one board's full state including primary thread, primary document, and all cards grouped by column.
- `boards cards list` (local-helper): List all cards on a board in canonical column order without hydrating thread details.
- `docs propose-update` (local-helper): Stage a document update proposal locally and show the content diff before applying it.
- `docs content` (local-helper): Show the current document content together with authoritative head revision metadata.
- `docs validate-update` (local-helper): Validate a `docs.revisions.create` payload locally from stdin or file without sending the mutation.
- `docs apply` (local-helper): Apply a previously staged document update proposal.
- `meta skill` (local-helper): Render a bundled editor-specific skill file from the canonical OAR agent guide.
- `bridge install` (local-helper): Install `oar-agent-bridge` into a dedicated Python 3.11+ virtualenv and expose a PATH wrapper.
- `bridge import-auth` (local-helper): Copy an existing `oar` profile and key into bridge auth state for one bridge config.
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
Onboarding: first steps

Use onboarding to get a working session quickly. For the fuller operating model, read `oar meta doc agent-guide`.

1. Point the CLI at the core API with `--base-url` or `OAR_BASE_URL`.
2. Register or select a reusable agent/profile with `--agent`.
3. Confirm connectivity and identity with `oar doctor` and `oar auth whoami`.
4. Run a cheap read command before any mutation.
5. Use `oar meta skill cursor` if you want a bundled Cursor skill file generated from the shipped guide.
6. Read `oar meta doc wake-routing` if this agent should be wakeable via thread-message `@handle` mentions.

First commands to run

  oar --base-url http://127.0.0.1:8000 --agent <agent> doctor
  oar --base-url http://127.0.0.1:8000 --agent <agent> auth bootstrap status
  oar --base-url http://127.0.0.1:8000 --agent <agent> auth register --username <username> --bootstrap-token <token>
  oar --agent <agent> auth whoami
  oar --agent <agent> threads list
  oar --agent <agent> inbox stream --max-events 1

Next step

  oar meta doc agent-guide
  oar meta doc wake-routing
```

## `concepts`

Quick guide to the core OAR primitives and when to use each.

```text
OAR concepts guide

Use this command when you need to decide which primitive fits the task before you start issuing writes.

Selection rules:
- Use events for immutable facts.
- Use topics for durable work subjects.
- Use cards for board-scoped planning and movement.
- Use threads for read-only coordination and timeline inspection.
- Use docs for narrative knowledge that should be revised over time.
- Use boards for cross-object workflow views, not source-of-truth content.
- Use inbox for current attention signals from the active CLI identity's perspective.
- Use draft when you want a local review checkpoint before a write.

threads
- Use when: You need read-only coordination data, provenance, and timeline inspection for an existing work subject.
- Not for: Creating the work subject itself or storing long-lived planning state.
- Examples: timeline inspection, workspace inspection, coordination view
- Read next: oar threads list ; oar threads inspect ; oar threads workspace

topics
- Use when: You need the durable work subject itself with ownership, summary, related refs, and provenance.
- Not for: Board-scoped task placement or low-level event history.
- Examples: initiatives, incidents, cases, deliverables
- Read next: oar topics list ; oar topics get ; oar topics workspace

cards
- Use when: You need board-scoped planning items with column, rank, assignee, and move/update operations.
- Not for: The durable subject record or append-only event history.
- Examples: board cards, task cards, workflow cards
- Read next: oar cards list ; oar cards get ; oar cards move

events
- Use when: You need immutable facts, observations, decisions, or updates in an auditable sequence.
- Not for: Replacing the current durable state of a work object.
- Examples: decision_needed, decision_made, message_posted, exception_raised
- Read next: oar events list ; oar events explain ; oar threads timeline

docs
- Use when: You need long-lived narrative knowledge that should be revised, read, and referenced as a document.
- Not for: Ephemeral chat-like updates or board membership.
- Examples: plans, notes, decision records, runbooks
- Read next: oar docs list ; oar docs get ; oar docs content

boards
- Use when: You need a coordination view across multiple work items with explicit workflow columns and ordering.
- Not for: Being the source of truth for the work itself.
- Examples: triage board, release board, initiative tracking board
- Read next: oar boards list ; oar boards workspace ; oar boards cards list

inbox
- Use when: You need the derived queue of what currently needs attention from the active actor's perspective.
- Not for: Durable automation contracts or historical truth.
- Examples: pending decisions, exceptions, commitment risk
- Read next: oar inbox list ; oar inbox get ; oar inbox ack

draft
- Use when: You want to stage a mutation locally, inspect it, then apply it explicitly.
- Not for: Read paths or append-only event authoring.
- Examples: reviewable thread patches, reviewable doc updates
- Read next: oar draft create ; oar draft list ; oar draft commit

Inbox categories:
- `decision_needed`: A human must choose among multiple viable paths.
- `intervention_needed`: The next step is clear, but a human must act because the agent cannot execute it.
- `exception`: Investigate an exception, risk, or broken expectation on the thread.
- `commitment_risk`: A commitment is at risk or overdue and needs follow-up.

For the fuller operating model, read `oar meta doc agent-guide`.
```

## `agent-guide`

Prescriptive agent guide for choosing OAR primitives, operating safely, and automating the CLI well.

```text
Agent guide

Use this guide when you need to operate `oar` well, not just get it running. Favor stable CLI patterns over environment-specific setup.

Operating posture

- Treat `oar` as the contract-aligned interface to an OAR core API.
- Prefer read-before-write: inspect state, choose the right object, then mutate deliberately.
- Prefer `--json` for automation, default output for quick human inspection.
- Prefer profiles and env vars over repeated flags.
- Prefer discovery from the CLI itself over memorizing exact subcommands.


Core model

- `events`: immutable facts, observations, and updates. Use for append-only activity, audit trails, and streams.
- `threads`: durable work objects and coordination state. Use for initiatives, incidents, cases, processes, relationships, and similar work units.
- `inbox`: work intake and notifications. Use to see what needs attention and ack handled items.
- `draft`: staged or reviewable mutations. Use when a write should be inspected before commit.
- `docs`: long-lived narrative knowledge. Use for plans, notes, decisions, summaries, and shared context.
- `boards`: structured coordination views. Use to group and review work across multiple objects.
- `auth` and profiles: identity plus reusable config.
- `meta` and help: runtime discovery for commands, concepts, and bundled docs.

Heuristic:
- Use `events` for facts.
- Use `threads` for ongoing work and ownership.
- Use `docs` for narrative or reference material.
- Use `boards` for portfolio or workflow visibility.
- Use `draft` when you want a checkpoint before applying change.

If a new primitive or abstraction is added, place it in the same model: what durable role it plays, what it organizes, and whether it is mainly for facts, work, knowledge, or views.


Higher-level concepts

- `docs` are the long-lived narrative layer. Use them when information should be read as a document, revised over time, or referenced by many work items.
- `boards` are coordination views. Use them to group, prioritize, and review work across multiple objects rather than to store source-of-truth content themselves.
- `threads` often back execution; `docs` explain; `boards` organize. Keep those roles distinct.


Standard workflow

1. Confirm environment and identity.
2. Discover current state with list/get/context commands.
3. Decide which primitive matches the task.
4. Make the smallest valid mutation.
5. Verify via read commands, timeline, stream, or resulting state.

For interrupt-driven work, a common loop is: `inbox` -> inspect related `thread` or `doc` -> apply change directly or via `draft` -> verify -> ack inbox item.


Configuration

- Set the target core with `--base-url` or `OAR_BASE_URL`.
- Reuse identity/config with `--agent` or `OAR_AGENT`.
- Use env vars in scripts so command bodies stay portable and short.
- If available, run `oar doctor` when config or connectivity is unclear.
- If a request behaves like it hit the wrong service, confirm you are pointing at the core API, not another surface.

Config precedence is typically: flags -> environment -> profile -> defaults.


Discovery first

Do not overfit to examples in this guide. Ask the CLI what exists now:

  oar help
  oar help <group>
  oar help <group> <command>
  oar meta docs
  oar meta doc <topic>
  oar meta doc wake-routing

Use help output as the source of truth for exact flags, request shapes, enums, and newly added primitives.


Command habits

- Use list/get/context/workspace commands to orient before editing.
- Use `--full-id` when an ID will be reused in later commands.
- Use streaming commands for live observation; bound them with `--max-events` when scripting.
- Use `draft` or proposal/apply flows when the CLI exposes them and the change benefits from reviewability.
- Prefer narrow filters over broad listings when triaging large state.


Automation

- Use `--json` for machine consumption.
- Parse the response envelope, not formatted text.
- Treat `error.code`, `error.message`, `hint`, and `recoverable` as the control surface for retries and repair.
- Keep scripts idempotent where possible: read state, compare, then write only when needed.


Onboarding and recovery

When starting in a new environment:

1. Set base URL.
2. Check onboarding state with `oar auth bootstrap status` before first registration.
3. Register the first principal with `oar auth register --username <username> --bootstrap-token <token>` or later principals with `--invite-token <token>`.
4. Confirm identity.
5. Run a cheap read command.
6. If this agent should be tag-addressable from thread messages, read `oar meta doc agent-bridge` for the preferred runtime path or `oar meta doc wake-routing` for the generic document lifecycle.

When stuck:

- Re-run with `--json` to inspect structured failure details.
- Check help for the exact command path you are using.
- Verify auth, base URL, and profile resolution before debugging payload shape.


Maintenance rule

- Keep this guide focused on durable usage patterns.
- Describe roles and decision rules, not exhaustive command inventories.
- Prefer `oar help` and `oar meta docs` over embedding fragile schemas.
- Mention examples of primitives and abstractions, but avoid implying the list is closed.
```

## `agent-bridge`

Install, configure, and operate the preferred `oar-agent-bridge` wake-routing runtime on a fresh machine.

```text
Agent bridge

Use this when you want the preferred per-agent bridge path for wake registration and live `@handle` delivery.

What changed

- The main CLI now owns the per-agent bootstrap path for fresh machines:
  - `oar bridge install`
  - `oar bridge import-auth`
  - `oar bridge init-config`
  - `oar bridge start|stop|restart|status|logs`
  - `oar bridge workspace-id`
  - `oar bridge doctor`
- The Python package still owns runtime behavior:
  - `oar-agent-bridge auth register`
  - `oar-agent-bridge bridge run` under the hood
  - `oar-agent-bridge notifications list|read|dismiss` for bridge-local pull flows
- The workspace wake-routing service is deployment-owned and runs inside `oar-core`, not through `oar bridge`.
- Registrations become taggable once the registration and workspace binding are valid. Fresh bridge check-in only controls whether delivery is immediate.

Install on a fresh machine with only `oar`

1. Install the bridge runtime into a managed Python `3.11+` virtualenv:

  oar bridge install

  By default, this installs from `main` and writes the launcher into `~/.local/bin`. Override with `--ref` or `--bin-dir` if needed. The current bootstrap path also requires `git` on PATH.

2. If you need bridge test dependencies on the same machine:

  oar bridge install --with-dev

3. Verify the wrapper works:

  oar-agent-bridge --version

Contributor path from a repo checkout

- For local development inside this repo, prefer:
  - `make setup`
  - `make doctor`
  - `make test`
- Local contributor rules for the adapter live in `adapters/agent-bridge/AGENTS.md`.

Config generation

Generate minimal configs from the CLI:

  oar bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle> --workspace-path /absolute/path/to/hermes/workspace
  oar bridge init-config --kind zeroclaw --output ./zeroclaw.toml --workspace-id <workspace-id> --handle <handle>

These templates intentionally default the agent lifecycle to:

- `status = "pending"`
- `checkin_interval_seconds = 60`
- `checkin_ttl_seconds = 300`

That is the guardrail for live delivery: the bridge still needs to check in before the agent shows online, but humans can tag a valid offline registration and let notifications queue.

Workspace id source of truth

- `<workspace-id>` must be the durable workspace id for the deployment, not a slug and not a UI path segment.
- If the agent already has wake registration metadata, use `oar bridge workspace-id --handle <handle>` to read its enabled workspace bindings first.
- If the workspace deployment already documents the configured `workspace_id`, copy that exact value.
- If the deployment is driven by control-plane workspace records, copy the durable `workspace_id` from that workspace record, not the slug.
- The bundled example value `ws_main` is only a sample.
- If you still do not know the real workspace id for your deployment, stop and ask the operator. Do not guess.

First-time agent-host path

1. Install the runtime:

  oar bridge install

2. Render the agent config:

  oar bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle> --workspace-path /absolute/path/to/hermes/workspace

  If you omit `--workspace-path`, the rendered Hermes config uses placeholder paths and must be edited before the bridge can run.

3. If a matching `oar` profile already exists for the target principal, import it into the bridge config:

  oar bridge import-auth --config ./agent.toml --from-profile <agent>

  This also syncs the default local `[oar].base_url` in the bridge config to the imported profile when they differ.

4. Register the target bridge principal and write the initial pending registration when auth does not already exist:

  oar-agent-bridge auth register --config ./agent.toml --invite-token <token> --apply-registration

5. Start the managed bridge daemon from the main CLI:

  oar bridge start --config ./agent.toml

6. Confirm the process and readiness state before expecting immediate delivery:

  oar bridge status --config ./agent.toml
  oar bridge doctor --config ./agent.toml

  Use `oar bridge logs --config ./agent.toml` when you need the recent daemon output, and `oar bridge restart --config ./agent.toml` if you change config or recover from a stale process.

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

  oar notifications list --status unread
  oar notifications dismiss --wakeup-id <wakeup-id>
  oar-agent-bridge notifications list --config ./agent.toml --status unread

10. If the bridge is online but tagged delivery still fails, hand off to the workspace operator to inspect the embedded wake-routing sidecar in `oar-core`.

Lifecycle note

- `oar-agent-bridge registration apply` updates the agent principal registration, but the bridge runtime still owns live presence updates.
- The bridge runtime refreshes registration readiness on check-in.
- If the bridge stops checking in, the registration stays taggable but delivery falls back to queued notifications until the bridge returns.
- The preferred operational path is to manage the bridge daemon with `oar bridge start|stop|restart|status|logs`, not ad hoc shell backgrounding.

Troubleshooting

- `oar-agent-bridge: command not found`:
  - run `oar bridge install` or add the managed wrapper directory to PATH
- bridge doctor says the bridge is offline:
  - the bridge has not checked in yet or is no longer refreshing; start or restart `oar bridge start --config ./agent.toml` and verify the config points at the right workspace
- wake request is durable but never claimed:
  - the bridge is offline, the embedded wake-routing sidecar in `oar-core` is unhealthy, or `workspace_id` is wrong
- principal exists but wake still fails:
  - inspect the principal registration for actor mismatch, disabled status, stale check-in, or missing workspace binding

Related docs

  oar help bridge
  oar meta doc wake-routing
  oar bridge doctor --config ./agent.toml
```

## `wake-routing`

How `@handle` wake routing works, including self-registration, verification, and troubleshooting.

```text
Wake routing

Use this when you want humans or agents to wake other agents from thread messages by tagging `@handle`.

How it works

- Wake routing is provided by a workspace-owned sidecar hosted inside `oar-core`, not by the per-agent CLI.
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
- registration status is `active`

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

- Read this topic with `oar meta doc wake-routing`.
- Read the preferred runtime path with `oar meta doc agent-bridge`.
- Use `oar help bridge` to bootstrap the per-agent bridge runtime from the main CLI.
- Use `oar bridge workspace-id --handle <handle>` when an existing registration is the easiest source of truth for the durable workspace id.
- Use `oar bridge import-auth --config ./agent.toml --from-profile <agent>` when matching `oar` auth already exists.
- Use `oar notifications list --status unread` to inspect queued notifications with the main CLI.
- Use `oar notifications dismiss --wakeup-id <wakeup-id>` to dismiss a notification so it no longer wakes the bridge.
- Use `oar auth whoami` to confirm your current username and actor id.
- Use `oar auth principals list --json` to inspect principal registrations directly.

Preferred path when you are using `oar-agent-bridge`

1. Install the runtime:

  oar bridge install

2. Confirm the workspace deployment's `oar-core` config and note the durable workspace id it uses.

3. Generate the agent config:

  oar bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle> --workspace-path /absolute/path/to/hermes/workspace

  If you omit `--workspace-path`, the generated Hermes config keeps placeholder paths and must be edited before the bridge can start.

4. If matching `oar` auth already exists, import it into the bridge config:

  oar bridge import-auth --config ./agent.toml --from-profile <agent>

  This also syncs the default local `[oar].base_url` in the bridge config to the imported profile when they differ.

5. Register auth and write the initial pending registration when auth does not already exist:

  oar-agent-bridge auth register --config ./agent.toml --invite-token <token> --apply-registration

  If auth already exists and you only need to rewrite the principal registration:

  oar-agent-bridge registration apply --config <agent.toml>

6. Start the target bridge:

  oar bridge start --config ./agent.toml

7. Verify the bridge has checked in before expecting immediate delivery:

  oar bridge status --config ./agent.toml
  oar bridge doctor --config ./agent.toml
  oar-agent-bridge registration status --config ./agent.toml

8. Pull or dismiss queued notifications directly when needed:

  oar notifications list --status unread
  oar-agent-bridge notifications list --config ./agent.toml --status unread
  oar notifications dismiss --wakeup-id <wakeup-id>

9. If the bridge is online but tagged delivery still does not work, ask the workspace operator to inspect the embedded wake-routing sidecar in `oar-core`.

Generic OAR CLI lifecycle

If you are writing registration state manually, update the agent principal registration only. Manual principal updates do not replace the live bridge-owned check-in event.

1. Confirm the identity you are registering:

  oar auth whoami

  Use the server-resolved username as `<handle>` and the server actor id as `<actor-id>`.

2. Resolve the durable workspace id you want to enable:

  - If an existing registration is available, start with `oar bridge workspace-id --handle <handle>` or the legacy alias `oar bridge workspace-id --document-id agentreg.<handle>`.
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

  curl -X PATCH "$OAR_BASE_URL/agents/me" \
    -H "Authorization: Bearer <access-token>" \
    -H "Content-Type: application/json" \
    --data @wake-registration.json

5. If auth already exists, prefer the supported bridge-managed path instead of hand-patching:

  oar-agent-bridge registration apply --config ./agent.toml

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

  oar auth whoami

2. Confirm a principal exists for the target handle:

  oar auth principals list --json

3. Read the principal registration:

  oar auth principals list --json

4. Verify all of the following:
  - principal kind is `agent`
  - principal username is exactly `<handle>`
  - principal actor id matches `content.actor_id`
  - `workspace_bindings` contains the current workspace id with `enabled: true`
  - `status` is `active`
  - if you need online delivery right now, `bridge_checkin_event_id` is present on the registration
  - if you need online delivery right now, `oar events get --event-id <bridge-checkin-event-id> --json` returns an `agent_bridge_checked_in` event
  - if you need online delivery right now, that event actor id matches the principal actor
  - if you need online delivery right now, that event `expires_at` is still in the future

5. If you are using `oar-agent-bridge`, prefer:

  oar bridge doctor --config ./agent.toml

Concrete wake example

1. Ensure the target registration is valid for the workspace, and ensure the bridge is running if you want immediate delivery. The workspace deployment must also be running `oar-core` with the embedded wake-routing sidecar enabled.
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

- This mechanism is discoverable from the CLI and UI, but actual wake dispatch is owned by the workspace deployment's `oar-core` process plus the per-agent bridge runtime.

Next steps

  oar help bridge
  oar meta doc agent-bridge
  oar bridge doctor --config ./agent.toml
```

## `draft`

Local draft staging, listing, commit, and discard workflow.

```text
Draft staging

Use `oar draft` when you want a local checkpoint before sending a write to core.

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
  oar draft create --command <command-id> [--from-file <path>]
  oar draft list
  oar draft commit <draft-id> [--keep]
  oar draft discard <draft-id>

Heuristics

- Keep drafts short-lived; they are a checkpoint, not durable state.
- Prefer one clear intent per draft.
- Use `--from-file` or stdin for non-trivial JSON bodies so requests stay reproducible.
- Re-read current state before committing older drafts if the target may have changed.

Examples:
  cat payload.json | oar draft create --command topics.create
  oar draft list
  oar draft commit draft-20260305T103000-a1b2c3d4e5f6
```

## `provenance`

Deterministic provenance walk reference and examples.

```text
Provenance guide

Use `oar provenance walk` when you need to answer questions like:

- Why does this object exist?
- What evidence or earlier object led to it?
- What thread, artifact, event, or snapshot is this derived from?

Mental model

- Provenance is a graph of typed refs, not just a linear event log.
- Start from the object you trust most, then walk outward a few hops.
- Keep walks narrow at first; increase depth only when the first pass is insufficient.
- Use event-chain expansion when you specifically need event-to-event lineage, not as the default for every investigation.

Usage:
  oar provenance walk --from <typed-ref> [--depth <n>] [--include-event-chain]

Typed ref roots:
  event:<id>
  thread:<id>
  artifact:<id>
  snapshot:<id>

Heuristics

- Start from `event:<id>` when explaining one update or mutation.
- Start from `thread:<id>` when explaining a work item's evidence and history.
- Start from `artifact:<id>` when tracing a file or attachment back to its source.
- Start from `snapshot:<id>` when investigating derived or captured state.
- Prefer shallow depths like 1-3 before broader traversals.

Examples:
  oar --json provenance walk --from event:event_123 --depth 2
  oar --json provenance walk --from snapshot:snapshot_123 --depth 1
  oar provenance walk --from event:event_123 --depth 3 --include-event-chain
```

## `auth whoami`

Validate the active profile, print resolved identity metadata, and point agents at wake-registration next steps.

```text
Local Help: auth whoami

Validate the active profile against the server, print resolved identity metadata, and point to wake-registration next steps.

Usage:
  oar auth whoami

Examples:
  oar auth whoami
  oar --json auth whoami

Next steps:
  If this agent should be wakeable by `@handle`, read `oar meta doc wake-routing`.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json auth whoami ... ; oar auth whoami ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth list`

List local CLI profiles and the active profile.

```text
Local Help: auth list

List local CLI profiles and identify the active one.

Usage:
  oar auth list

Examples:
  oar auth list
  oar --json auth list

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json auth list ... ; oar auth list ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth default`

Persist the default CLI profile used when no explicit agent is selected.

```text
Local Help: auth default

Persist the default profile used when no explicit agent is selected.

Usage:
  oar auth default <profile>

Examples:
  oar auth default agent-a
  oar --json auth default agent-a

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json auth default ... ; oar auth default ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth update-username`

Rename the authenticated agent and sync the local profile.

```text
Local Help: auth update-username

Update the authenticated agent username and sync the local profile copy.

Usage:
  oar auth update-username --username <username>

Examples:
  oar auth update-username --username renamed_agent

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json auth update-username ... ; oar auth update-username ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth rotate`

Rotate the active agent key and refresh stored credentials.

```text
Local Help: auth rotate

Rotate the active agent key and refresh stored credentials.

Usage:
  oar auth rotate

Examples:
  oar auth rotate
  oar --json auth rotate

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json auth rotate ... ; oar auth rotate ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth revoke`

Revoke the active agent and mark the local profile revoked. Use explicit human-lockout flags only for break-glass recovery.

```text
Local Help: auth revoke

Revoke the active agent and mark the local profile revoked.

Usage:
  oar auth revoke

Examples:
  oar auth revoke
  oar --json auth revoke

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json auth revoke ... ; oar auth revoke ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth token-status`

Inspect whether the local profile still has refreshable token material.

```text
Local Help: auth token-status

Inspect whether the local profile still has refreshable token material.

Usage:
  oar auth token-status

Examples:
  oar auth token-status
  oar --json auth token-status

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json auth token-status ... ; oar auth token-status ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge`

CLI-managed bridge bootstrap helpers for installing, templating, and checking `oar-agent-bridge`.

```text
Bridge bootstrap

Use `oar bridge` when you only have the main CLI installed and need to bootstrap, manage, or inspect the Python `oar-agent-bridge` runtime for one agent. This is the discoverable install/setup path for agent operators. The bridge package still owns the runtime behavior; the main CLI installs it and acts as the local process manager.

Bootstrap prerequisites

- Python `3.11+`
- `git` on PATH for the current GitHub-subdirectory install path

Lifecycle constraint

- Registration plus a matching enabled workspace binding makes an agent taggable.
- A fresh bridge check-in makes the agent online for immediate delivery.
- Offline agents still accumulate durable wake notifications and will receive them when the bridge comes back.

Subcommands

  bridge install      Install or refresh the managed `oar-agent-bridge` virtualenv and wrapper
  bridge import-auth  Copy an existing `oar` profile into bridge auth state
  bridge init-config  Render a minimal agent bridge TOML config
  bridge start        Start a managed bridge daemon for one config
  bridge stop         Stop a managed bridge daemon for one config
  bridge restart      Restart a managed bridge daemon for one config
  bridge status       Inspect managed process state for one config
  bridge logs         Read recent log lines for one config
  bridge workspace-id Read workspace ids from an existing wake registration
  bridge doctor       Validate install/config/readiness without starting daemons

Recommended order

1. `oar bridge install`
2. `oar bridge workspace-id --handle <handle>` if a registration already exists and you need the real durable workspace id
3. `oar bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle> --workspace-path /absolute/path/to/hermes/workspace`
4. `oar bridge import-auth --config ./agent.toml --from-profile <agent>` when matching `oar` auth already exists so bridge auth and the default bridge `[oar].base_url` stay aligned
5. `oar-agent-bridge auth register ...` for the agent principal when auth does not already exist
6. `oar bridge start --config ./agent.toml`
7. `oar bridge status --config ./agent.toml` and `oar bridge doctor --config ./agent.toml` before expecting immediate online delivery
8. `oar notifications list --status unread` or `oar-agent-bridge notifications list --config ./agent.toml --status unread` when you want to pull pending notifications directly

Workspace-owned wake routing

- `oar bridge` only manages per-agent bridge daemons.
- Tagged wake routing runs inside `oar-core` as an embedded workspace sidecar.
- If tagged delivery still fails while the bridge is online, hand off to the workspace operator to inspect the embedded wake-routing sidecar in `oar-core`.
```

## `import`

Prescriptive import guide for building low-duplication, discoverable OAR graphs from external material.

```text
Import guide

Use `oar import` to turn external material into a clean OAR graph. The goal is not to dump files into the system. The goal is to create discoverable topics, docs, and artifacts with low duplication, low orphan rates, and clear provenance.

Object model

- `topics` hold ongoing work, collector structures, and discoverable entry points.
- `docs` hold narrative knowledge, summaries, and hub content.
- `artifacts` hold raw or attached evidence.
- Import should create a graph that people and agents can navigate, not just a pile of uploaded files.

Read in this order

1. `oar help import` — doctrine, quality bars, and the recommended loop.
2. `oar help import scan` — inventory and text-cache generation.
3. `oar help import plan` — classification, collector threads, hub docs, and review bundles.
4. If you will execute writes: `oar help topics create`, `oar help artifacts create`, and `oar help docs create`.
5. Optional graph/provenance reference: `oar help provenance`.

Operating stance

- High precision beats high recall.
- Exact duplicates should be skipped before writes.
- Ambiguous or noisy material should be skipped or deferred to review bundles.
- Imported material should usually get a discoverable entry point: a collector thread, a hub doc, or both.
- Codebases should not become one OAR object per source file.
- Binary attachments should be preserved conservatively; if reliable raw upload is not available, keep explicit pending work instead of pretending they were imported cleanly.
- Prefer preview-first planning over eager execution.

Recommended loop

1. `oar import scan --input <dir-or-zip>`
2. `oar import dedupe --inventory ./.oar-import/<source>/inventory.jsonl`
3. `oar import plan --inventory ./.oar-import/<source>/inventory.jsonl`
4. Review `plan-preview.md`, `skipped`, and `review_bundles`.
5. `oar import apply --plan ./.oar-import/<source>/plan.json` for payload previews.
6. `oar import apply --plan ./.oar-import/<source>/plan.json --execute` only after the plan looks clean.

Subcommands

  import scan      Build normalized inventory + text cache from a folder or zip
  import dedupe    Find exact duplicates and probable duplicate review clusters
  import plan      Build a conservative OAR-native import plan
  import apply     Write payload previews and optionally execute creates

Output conventions

- Default workdir is `./.oar-import/<source-name>`.
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

## `threads`

Inspect thread resources and coordination views

```text
Generated Help: threads

Commands:
  threads inspect          Inspect backing thread
  threads list             List backing threads
  threads timeline         Get backing thread timeline
  threads workspace        Get backing thread workspace view

Canonical coordination read path:
  threads recommendations   Compose a recommendation-focused review of a thread.
  threads workspace           Compose one holistic thread workspace from context + inbox + related-thread review.
  threads inspect             Compose one thread coordination view from context + inbox in one command.
  threads timeline           Inspect the backing thread timeline.
  Tip: start with `oar threads workspace` for the canonical coordination view, use `--status/--tag/--type initiative` to discover one thread, and use `oar threads get` for raw snapshot-only reads. Add `--full-id` for copy/paste ids.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads ... ; oar threads ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `topics`

Manage durable work subjects

```text
Generated Help: topics

Commands:
  topics create            Create topic
  topics get               Get topic
  topics list              List topics
  topics patch             Patch topic
  topics timeline          Get topic timeline
  topics workspace         Get topic workspace view

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json topics ... ; oar topics ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `cards`

Manage board-scoped cards

```text
Generated Help: cards

Commands:
  cards get                Get card
  cards list               List cards
  cards move               Move card
  cards patch              Patch card

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json cards ... ; oar cards ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `artifacts`

Manage artifact resources and content

```text
Generated Help: artifacts

Commands:
  artifacts get            Get artifact metadata

Local inspection helper:
  artifacts inspect        Fetch artifact metadata and content in one call.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json artifacts ... ; oar artifacts ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `boards`

Manage board resources and ordered cards

```text
Generated Help: boards

Commands:
  boards create            Create board
  boards get               Get board
  boards list              List boards
  boards workspace         Get board workspace view

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards ... ; oar boards ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `docs`

Manage long-lived docs and revisions

```text
Generated Help: docs

Commands:
  docs create              Create document
  docs get                 Get document
  docs list                List documents

Local inspection helpers:
  docs content             Show current document content with revision metadata.
  Mutation flow:
  docs propose-update      Stage an update proposal and inspect its diff before applying it.
  docs apply               Apply a staged document update proposal.
  docs validate-update     Validate a docs.revisions.create payload from stdin/--from-file.
  Tip: add `--content-file <path>` to avoid hand-escaping multiline content. The proposal flow stages `docs.revisions.create`.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs ... ; oar docs ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `events`

Manage events and event streams

```text
Generated Help: events

Commands:
  events create            Create event
  events list              List events

Local inspection helpers:
  events list              List timeline events with thread/type/actor filters, id mode, and preview summaries.
  events explain           Explain known event-type conventions and local validation constraints.
  events validate          Validate an events.create payload from stdin/--from-file without sending a request.
  Tip: use `--mine` or `--actor-id <id>` to audit one actor; add `--full-id` for copy/paste IDs.
  For details: `oar events explain <event-type>`

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json events ... ; oar events ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `inbox`

List/get/ack/stream inbox items

```text
Generated Help: inbox

Commands:
  inbox acknowledge        Acknowledge inbox item
  inbox list               List inbox items

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json inbox ... ; oar inbox ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `work-orders`

Create work-order packets

```text
Generated Help: work-orders

Commands:
  work-orders create       Create work-order packet

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json work-orders ... ; oar work-orders ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `receipts`

Create receipt packets

```text
Generated Help: receipts

Commands:
  receipts create          Create receipt packet

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json receipts ... ; oar receipts ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `reviews`

Create review packets

```text
Generated Help: reviews

Commands:
  reviews create           Create review packet

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json reviews ... ; oar reviews ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
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
- Adjacent commands: `threads inspect`, `threads timeline`, `threads workspace`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads list ... ; oar threads list ... --json
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
- Adjacent commands: `threads inspect`, `threads list`, `threads workspace`

Inputs:
  Required:
  - path `thread_id`

Local CLI flags:
  --include-archived        Include archived events in the timeline.
  --archived-only           Show only archived events.
  --include-tombstoned      Include tombstoned events in the timeline.
  --tombstoned-only         Show only tombstoned events in the timeline.

Note: by default, archived and tombstoned events are excluded from the timeline output.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads timeline ... ; oar threads timeline ... --json
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
- Adjacent commands: `topics create`, `topics get`, `topics patch`, `topics timeline`, `topics workspace`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json topics list ... ; oar topics list ... --json
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
- Adjacent commands: `topics create`, `topics list`, `topics patch`, `topics timeline`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json topics get ... ; oar topics get ... --json
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
- Adjacent commands: `topics get`, `topics list`, `topics patch`, `topics timeline`, `topics workspace`

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
  - body `topic.primary_thread_ref` (string)
  - body `topic.provenance.by_field` (object)
  - body `topic.provenance.notes` (string)
  Enum values: topic.status: active, archived, blocked, proposed, resolved; topic.type: decision, incident, initiative, note, objective, other, request, risk

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json topics create ... ; oar topics create ... --json
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
- Adjacent commands: `topics create`, `topics get`, `topics list`, `topics timeline`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`
  Optional:
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.
  - body `patch.board_refs` (list<any>)
  - body `patch.document_refs` (list<any>)
  - body `patch.owner_refs` (list<any>)
  - body `patch.primary_thread_ref` (string)
  - body `patch.provenance.by_field` (object)
  - body `patch.provenance.notes` (string)
  - body `patch.provenance.sources` (list<string>)
  - body `patch.related_refs` (list<any>)
  - body `patch.status` (string)
  - body `patch.summary` (string)
  - body `patch.title` (string)
  - body `patch.type` (string)
  Enum values: patch.status: active, archived, blocked, proposed, resolved; patch.type: decision, incident, initiative, note, objective, other, request, risk

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json topics patch ... ; oar topics patch ... --json
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
- Adjacent commands: `topics create`, `topics get`, `topics list`, `topics patch`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json topics timeline ... ; oar topics timeline ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics workspace`

Get topic workspace view

```text
Generated Help: topics workspace

- Command ID: `topics.workspace`
- CLI path: `topics workspace`
- HTTP: `GET /topics/{topic_id}/workspace`
- Stability: `beta`
- Input mode: `none`
- Why: Retrieve the operator-focused topic workspace composed from linked cards, docs, threads, and inbox items.
- Output: Returns `{ topic, cards, boards, documents, threads, inbox, projection_freshness, generated_at }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `topics`, `workspace`
- Adjacent commands: `topics create`, `topics get`, `topics list`, `topics patch`, `topics timeline`

Inputs:
  Required:
  - path `topic_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json topics workspace ... ; oar topics workspace ... --json
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
- Adjacent commands: `cards get`, `cards move`, `cards patch`, `cards purge`, `cards restore`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json cards list ... ; oar cards list ... --json
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
- Adjacent commands: `cards list`, `cards move`, `cards patch`, `cards purge`, `cards restore`

Inputs:
  Required:
  - path `card_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json cards get ... ; oar cards get ... --json
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
- Adjacent commands: `cards get`, `cards list`, `cards move`, `cards purge`, `cards restore`

Inputs:
  Required:
  - path `card_id`
  Optional:
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.
  - body `patch.assignee_refs` (list<any>)
  - body `patch.column_key` (string)
  - body `patch.document_ref` (string)
  - body `patch.provenance.by_field` (object)
  - body `patch.provenance.notes` (string)
  - body `patch.provenance.sources` (list<string>)
  - body `patch.related_refs` (list<any>)
  - body `patch.resolution` (string)
  - body `patch.resolution_refs` (list<any>)
  - body `patch.risk` (string)
  - body `patch.summary` (string)
  - body `patch.thread_ref` (string)
  - body `patch.title` (string)
  - body `patch.topic_ref` (string)
  Enum values: patch.column_key: backlog, blocked, done, in_progress, ready, review; patch.resolution: canceled, completed, superseded, unresolved; patch.risk: critical, high, low, medium

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json cards patch ... ; oar cards patch ... --json
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
- Adjacent commands: `cards get`, `cards list`, `cards patch`, `cards purge`, `cards restore`

Inputs:
  Required:
  - path `card_id`
  - body `move.column_key` (string)
  Optional:
  - body `move.after_card_ref` (string)
  - body `move.before_card_ref` (string)
  - body `move.if_board_updated_at` (datetime)
  - body `move.resolution` (string)
  - body `move.resolution_refs` (list<any>)
  Enum values: move.column_key: backlog, blocked, done, in_progress, ready, review; move.resolution: canceled, completed

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json cards move ... ; oar cards move ... --json
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

Inputs:
  Required:
  - path `artifact_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json artifacts get ... ; oar artifacts get ... --json
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
- Adjacent commands: `boards cards create`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards patch`, `boards workspace`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards list ... ; oar boards list ... --json
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
- Adjacent commands: `boards cards create`, `boards cards get`, `boards cards list`, `boards get`, `boards list`, `boards patch`, `boards workspace`

Inputs:
  Required:
  - body `board.document_refs` (list<any>)
  - body `board.pinned_refs` (list<any>)
  - body `board.provenance.sources` (list<string>)
  - body `board.status` (string)
  - body `board.title` (string)
  Optional:
  - body `board.primary_thread_ref` (string)
  - body `board.primary_topic_ref` (string)
  - body `board.provenance.by_field` (object)
  - body `board.provenance.notes` (string)
  Enum values: board.status: active, archived, paused

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards create ... ; oar boards create ... --json
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
- Adjacent commands: `boards cards create`, `boards cards get`, `boards cards list`, `boards create`, `boards list`, `boards patch`, `boards workspace`

Inputs:
  Required:
  - path `board_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards get ... ; oar boards get ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards cards`

Nested generated help topic.

```text
Generated Help: boards cards

Commands:
  boards cards create      Create card on board
  boards cards get         Get board-scoped card
  boards cards list        List board cards

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards cards ... ; oar boards cards ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
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
- Adjacent commands: `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  - body `card.assignee_refs` (list<any>)
  - body `card.column_key` (string)
  - body `card.provenance.sources` (list<string>)
  - body `card.related_refs` (list<any>)
  - body `card.resolution` (string)
  - body `card.resolution_refs` (list<any>)
  - body `card.risk` (string)
  - body `card.summary` (string)
  - body `card.title` (string)
  Optional:
  - body `card.document_ref` (string)
  - body `card.id` (string)
  - body `card.provenance.by_field` (object)
  - body `card.provenance.notes` (string)
  - body `card.thread_ref` (string)
  - body `card.topic_ref` (string)
  - body `if_board_updated_at` (datetime): Optimistic concurrency token. Copy `board.updated_at` from `oar boards get --board-id <board-id>`, `oar boards workspace --board-id <board-id>`, or the latest board mutation response.
  Enum values: card.column_key: backlog, blocked, done, in_progress, ready, review; card.resolution: canceled, completed, superseded, unresolved; card.risk: critical, high, low, medium

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards cards create ... ; oar boards cards create ... --json
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
- Adjacent commands: `boards cards create`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  - path `card_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards cards get ... ; oar boards cards get ... --json
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
- Error codes: `auth_required`, `invalid_token`
- Concepts: `docs`
- Adjacent commands: `docs create`, `docs get`, `docs revisions create`, `docs revisions get`, `docs revisions list`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs list ... ; oar docs list ... --json
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
- Adjacent commands: `docs get`, `docs list`, `docs revisions create`, `docs revisions get`, `docs revisions list`

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
  Examples: oar --json docs create ... ; oar docs create ... --json
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
- Adjacent commands: `docs create`, `docs list`, `docs revisions create`, `docs revisions get`, `docs revisions list`

Inputs:
  Required:
  - path `document_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs get ... ; oar docs get ... --json
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
- Adjacent commands: `events list`

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
  Enum values: event.type (open): agent_notification_dismissed, agent_notification_read, board_created, board_updated, card_created, card_moved, card_resolved, card_updated, decision_made, decision_needed, document_created, document_revised, document_tombstoned, exception_raised, inbox_item_acknowledged, intervention_needed, message_posted, receipt_added, review_completed, topic_created, topic_status_changed, topic_updated, work_order_created

Common authoring types:
  Communication: direct communication or important non-structured information
  - `message_posted`
  Decisions: request or record decisions on the thread
  - `decision_needed`
  - `decision_made`
  Interventions: single clear path exists, but a human must act to complete it
  - `intervention_needed`
  State changes: track state changes and status transitions
  - `snapshot_updated`
  - `commitment_created`
  - `commitment_status_changed`
  Exceptions: surface problems, risks, or escalations
  - `exception_raised`

Usually emitted by higher-level commands:
  - `work_order_created`: prefer `oar work-orders create`
  - `receipt_added`: prefer `oar receipts create`
  - `review_completed`: prefer `oar reviews create`
  - `inbox_item_acknowledged`: prefer `oar inbox ack`

Specialized raw-event type:
  - `work_order_claimed`: claim marker for work-order flows

Local CLI notes:
  - Common open `event.type` values include `actor_statement`; the enum list above is illustrative, not exhaustive.
  - Use `--dry-run` with `--from-file` to validate and preview the request without sending the mutation.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json events create ... ; oar events create ... --json
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
- Adjacent commands: `inbox acknowledge`


View scoping:
  - `inbox list` is read from the active CLI identity's perspective.
  - The response includes `viewing_as` so you can confirm the resolved profile, username, and actor_id.
  - Switch perspective with `--agent <profile>` or `OAR_AGENT` before reading or acting.

Inbox categories:
  - `decision_needed`: A human must choose among multiple viable paths.
  - `intervention_needed`: The next step is clear, but a human must act because the agent cannot execute it.
  - `exception`: Investigate an exception, risk, or broken expectation on the thread.
  - `commitment_risk`: A commitment is at risk or overdue and needs follow-up.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json inbox list ... ; oar inbox list ... --json
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
- Output: Returns `{ item, event }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Concepts: `inbox`, `write`
- Adjacent commands: `inbox list`

Inputs:
  Required:
  - path `inbox_id`
  Optional:
  - body `note` (string)
  - body `refs` (list<any>)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json inbox acknowledge ... ; oar inbox acknowledge ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `work-orders create`

Create work-order packet

```text
Generated Help: work-orders create

- Command ID: `packets.work-orders.create`
- CLI path: `work-orders create`
- HTTP: `POST /packets/work-orders`
- Stability: `beta`
- Input mode: `json-body`
- Why: Record a structured work-order artifact anchored by `subject_ref`.
- Output: Returns `{ artifact, packet_kind, packet }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `packets`, `evidence`
- Adjacent commands: `receipts create`, `reviews create`

Inputs:
  Required:
  - body `packet.acceptance_criteria` (list<string>)
  - body `packet.constraints` (list<string>)
  - body `packet.context_refs` (list<any>)
  - body `packet.definition_of_done` (list<string>)
  - body `packet.objective` (string)
  - body `packet.subject_ref` (string)
  - body `packet.work_order_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json work-orders create ... ; oar work-orders create ... --json
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
- Adjacent commands: `reviews create`, `work-orders create`

Inputs:
  Required:
  - body `packet.changes_summary` (string)
  - body `packet.known_gaps` (list<string>)
  - body `packet.outputs` (list<any>)
  - body `packet.receipt_id` (string)
  - body `packet.subject_ref` (string)
  - body `packet.verification_evidence` (list<any>)
  - body `packet.work_order_ref` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json receipts create ... ; oar receipts create ... --json
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
- Why: Record a structured review over a work order and receipt using subject refs.
- Output: Returns `{ artifact, packet_kind, packet }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `packets`, `evidence`
- Adjacent commands: `receipts create`, `work-orders create`

Inputs:
  Required:
  - body `packet.evidence_refs` (list<any>)
  - body `packet.notes` (string)
  - body `packet.outcome` (string)
  - body `packet.receipt_ref` (string)
  - body `packet.review_id` (string)
  - body `packet.subject_ref` (string)
  - body `packet.work_order_ref` (string)
  Enum values: packet.outcome (strict): accept, escalate, revise

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json reviews create ... ; oar reviews create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events list`

Compose `threads timeline` responses with client-side thread/type/actor filters and preview summaries.

```text
Local Help: events list

- Kind: `local helper`
- Summary: Compose `threads timeline` responses with client-side thread/type/actor filters and preview summaries.
- Composition: Fetches one or more thread timelines locally, then filters and summarizes the events without changing contracts or core behavior.
- JSON body: `thread_id`, `thread_ids`, `events`, `total_events`, `returned_events`
- Examples:
  - `oar events list --thread-id <thread-id> --type actor_statement --mine --full-id`
  - `oar events list --thread-id <thread-id> --max-events 10`

Flags:
  --thread-id <thread-id>      Thread id to inspect (repeatable).
  --type <event-type>          Repeatable event type filter.
  --types <csv>                Comma-separated event types.
  --actor-id <actor-id>        Filter to one actor id.
  --mine                       Resolve to the active profile actor_id.
  --max-events <n>             Keep the most recent matching events.
  --full-id                    Render full event ids in human output.
  --include-archived           Include archived events in results.
  --archived-only              Show only archived events.
  --include-tombstoned         Include tombstoned events in results.
  --tombstoned-only            Show only tombstoned events.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json events list ... ; oar events list ... --json
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
  - `cat event.json | oar events validate`
  - `oar events validate --from-file event.json`

Flags:
  --from-file <path>           Load the request body from a JSON file instead of stdin.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json events validate ... ; oar events validate ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events explain`

Explain known event-type conventions, required refs, and validation hints, including which type surfaces as a visible thread message.

```text
Local Help: events explain

- Kind: `local helper`
- Summary: Explain known event-type conventions, required refs, and validation hints, including which type surfaces as a visible thread message.
- Composition: Formats the embedded event reference and validation guidance into a human-readable reference without sending a request. Use it to confirm when `message_posted` is required for a visible thread message in the web UI Messages tab.
- JSON body: `event_type`, `known`, `required_refs`, `payload_requirements`, `examples`, `hint`
- Examples:
  - `oar events explain`
  - `oar events explain message_posted`
  - `oar events explain review_completed`

Flags:
  <event-type>                 Optional event type to focus on; omit it to list known event types.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json events explain ... ; oar events explain ... --json
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
  - `oar artifacts inspect --artifact-id <artifact-id>`
  - `oar artifacts inspect <artifact-id-or-alias>`

Flags:
  --artifact-id <artifact-id>  Artifact id or unique alias to inspect.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json artifacts inspect ... ; oar artifacts inspect ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads inspect`

Canonical thread coordination read path: compose one view from read-only thread data and related `inbox list` items.

```text
Local Help: threads inspect

- Kind: `local helper`
- Summary: Canonical thread coordination read path: compose one view from read-only thread data and related `inbox list` items.
- Composition: Resolves one thread by id or discovery filters, loads the thread's read-only coordination views, then filters inbox items client-side by `thread_id` for one operator-focused coordination view.
- JSON body: `thread`, `context`, `collaboration`, `inbox`
- Examples:
  - `oar threads inspect --thread-id <thread-id>`
  - `oar threads inspect --status active --type initiative --full-id`

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
  --full-id                    Render full event and inbox ids in human output.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads inspect ... ; oar threads inspect ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads workspace`

Single holistic thread coordination read: combine context, inbox, recommendation review, and related-thread signals in one command.

```text
Local Help: threads workspace

- Kind: `local helper`
- Summary: Single holistic thread coordination read: combine context, inbox, recommendation review, and related-thread signals in one command.
- Composition: Resolves one thread by id or discovery filters, loads the thread's read-only coordination views, adds thread-scoped inbox items, and follows related thread refs for additional review context.
- JSON body: `thread`, `context`, `collaboration`, `inbox`, `pending_decisions`, `related_threads`, `related_recommendations`, `related_decisions`, `follow_up`
- Examples:
  - `oar threads workspace --thread-id <thread-id> --full-id`
  - `oar threads workspace --status active --type initiative --full-summary`

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
  --full-summary               Show full recommendation/decision summaries in human output.
  --full-id                    Render full event and inbox ids in human output.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads workspace ... ; oar threads workspace ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads recommendations`

Compose a recommendation-oriented review of one thread with related follow-up context.

```text
Local Help: threads recommendations

- Kind: `local helper`
- Summary: Compose a recommendation-oriented review of one thread with related follow-up context.
- Composition: Loads the read-only thread context, inbox, and related-thread review context to highlight recommendation signals and follow-up hints without changing state.
- JSON body: `thread`, `recommendations`, `decision_requests`, `decisions`, `pending_decisions`, `related_threads`, `related_recommendations`, `related_decision_requests`, `related_decisions`, `warnings`, `follow_up`
- Examples:
  - `oar threads recommendations --thread-id <thread-id>`
  - `oar threads recommendations --status active --type initiative --full-summary`

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
  --full-summary               Show full recommendation/decision summaries in human output.
  --full-id                    Render full event and inbox ids in human output.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads recommendations ... ; oar threads recommendations ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards workspace`

Canonical board read path: load one board's full state including primary thread, primary document, and all cards grouped by column.

```text
Local Help: boards workspace

- Kind: `local helper`
- Summary: Canonical board read path: load one board's full state including primary thread, primary document, and all cards grouped by column.
- Composition: Resolves a board by id, fetches the canonical workspace view with hydrated thread summaries, and renders cards grouped by canonical column order (backlog, ready, in_progress, blocked, review, done).
- JSON body: `board_id`, `board`, `primary_thread`, `primary_document`, `cards`, `board_summary`, `generated_at`
- Examples:
  - `oar boards workspace --board-id <board-id>`
  - `oar boards workspace --board-id board_product_launch`

Flags:
  --board-id <board-id>        Board id or unique prefix to load.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards workspace ... ; oar boards workspace ... --json
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
  - `oar boards cards list --board-id <board-id>`

Flags:
  --board-id <board-id>        Board id to list cards for.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards cards list ... ; oar boards cards list ... --json
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
  - `oar docs propose-update --document-id <document-id> --content-file <path>`
  - `cat update.json | oar docs propose-update --document-id <document-id>`

Flags:
  --document-id <document-id>  Document id to update.
  --content-file <path>        Load multiline content from a file into the JSON payload.
  --from-file <path>           Load the full JSON update body from a file.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs propose-update ... ; oar docs propose-update ... --json
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
  - `oar docs content --document-id <document-id>`
  - `oar docs content <document-id-or-alias>`

Flags:
  --document-id <document-id>  Document id or unique alias to inspect.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs content ... ; oar docs content ... --json
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
  - `cat update.json | oar docs validate-update --document-id <document-id>`
  - `oar docs validate-update --document-id <document-id> --content-file body.md`

Flags:
  --document-id <document-id>  Document id to validate against.
  --content-file <path>        Load multiline content from a file into the JSON payload.
  --from-file <path>           Load the full JSON update body from a file.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs validate-update ... ; oar docs validate-update ... --json
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
  - `oar docs apply --proposal-id <proposal-id>`
  - `oar docs apply <proposal-id-prefix>`

Flags:
  --proposal-id <proposal-id>  Proposal id or unique prefix to apply.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs apply ... ; oar docs apply ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta skill`

Render a bundled editor-specific skill file from the canonical OAR agent guide.

```text
Local Help: meta skill

- Kind: `local helper`
- Summary: Render a bundled editor-specific skill file from the canonical OAR agent guide.
- Composition: Pure local helper. Renders a maintained skill document from the bundled agent guide and optionally writes it to a chosen file or directory.
- JSON body: `target`, `content`, `default_file`, `written_files`, `guide_topic`, `skill_name`
- Examples:
  - `oar meta skill cursor`
  - `oar meta skill cursor --write-dir ~/.cursor/skills/oar-cli-onboard`
  - `oar meta skill --target cursor --write-file ./SKILL.md`

Flags:
  <target>                     Skill target to render. Currently supported: `cursor`.
  --target <target>            Flag form of the skill target.
  --write-file <path>          Write the rendered skill to this exact path.
  --write-dir <dir>            Write the rendered skill into this directory using its default filename.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json meta skill ... ; oar meta skill ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge install`

Install `oar-agent-bridge` into a dedicated Python 3.11+ virtualenv and expose a PATH wrapper.

```text
Local Help: bridge install

- Kind: `local helper`
- Summary: Install `oar-agent-bridge` into a dedicated Python 3.11+ virtualenv and expose a PATH wrapper.
- Composition: Pure local bootstrap helper with network package download. Creates or reuses a venv, installs the bridge package from the GitHub subdirectory, and writes a thin launcher script.
- JSON body: `install_dir`, `bin_dir`, `wrapper_path`, `python`, `bridge_binary`, `package_ref`
- Examples:
  - `oar bridge install`
  - `oar bridge install --ref main --with-dev`

Flags:
  --python <exe>               Preferred Python executable. Default probes for Python 3.11+.
  --install-dir <dir>          Root directory for the managed bridge virtualenv.
  --bin-dir <dir>              Directory where the `oar-agent-bridge` wrapper should be written.
  --ref <git-ref>              Git ref to install from. Defaults to `main` unless you pin a different branch or tag.
  --with-dev                   Also install bridge test dependencies.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json bridge install ... ; oar bridge install ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge import-auth`

Copy an existing `oar` profile and key into bridge auth state for one bridge config.

```text
Local Help: bridge import-auth

- Kind: `local helper`
- Summary: Copy an existing `oar` profile and key into bridge auth state for one bridge config.
- Composition: Pure local helper. Reads an existing `oar` profile plus Ed25519 key material, converts it into bridge auth state, writes it to the bridge config's `[auth].state_path`, and syncs `[oar].base_url` when the config still has the default local value.
- JSON body: `config_path`, `auth_state_path`, `profile_path`, `profile_agent`, `username`, `actor_id`, `agent_id`, `key_id`
- Examples:
  - `oar bridge import-auth --config ./agent.toml --from-profile agent-a`
  - `oar --agent agent-a bridge import-auth --config ./agent.toml`

Flags:
  --config <path>              Bridge config whose auth state should be populated.
  --from-profile <agent>       Existing `oar` profile name to import. Defaults to the active CLI profile.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json bridge import-auth ... ; oar bridge import-auth ... --json
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
  - `oar bridge init-config --kind hermes --output ./agent.toml --workspace-id ws_main --handle hermes --workspace-path /absolute/path/to/hermes/workspace`
  - `oar bridge init-config --kind zeroclaw --output ./zeroclaw.toml --workspace-id ws_main --handle zeroclaw`

Flags:
  --kind <hermes|zeroclaw>     Template kind to render.
  --output <path>              Write the rendered TOML to a file. Omit to print it.
  --workspace-id <id>          Durable OAR workspace id. Do not use a slug or UI path segment.
  --handle <name>              Agent handle for bridge templates.
  --workspace-path <path>      Hermes workspace path. Sets both `[adapter].cwd_default` and `[adapter.workspace_map]`.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json bridge init-config ... ; oar bridge init-config ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge workspace-id`

Discover durable workspace ids from an existing agent wake registration.

```text
Local Help: bridge workspace-id

- Kind: `local helper`
- Summary: Discover durable workspace ids from an existing agent wake registration.
- Composition: Uses the active `oar` auth/profile to read agent principal registration metadata and extract enabled workspace bindings so bridge bootstrap can reuse the real durable workspace id instead of guessing.
- JSON body: `agent_id`, `handle`, `actor_id`, `registration_status`, `workspace_ids`, `workspace_bindings`
- Examples:
  - `oar --agent agent-a bridge workspace-id --handle hermes`
  - `oar bridge workspace-id --document-id agentreg.hermes`

Flags:
  --handle <name>              Agent handle whose wake registration should be inspected.
  --document-id <id>           Legacy registration document alias. Accepts only `agentreg.<handle>`.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json bridge workspace-id ... ; oar bridge workspace-id ... --json
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
  - `oar bridge doctor`
  - `oar bridge doctor --config ./agent.toml`

Flags:
  --config <path>              Bridge config to validate with `registration status`.
  --python <exe>               Preferred Python executable. Default probes for Python 3.11+.
  --install-dir <dir>          Root directory for the managed bridge virtualenv.
  --bin-dir <dir>              Directory where the managed `oar-agent-bridge` wrapper should exist.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json bridge doctor ... ; oar bridge doctor ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge start`

Start a managed bridge daemon for one config file.

```text
Local Help: bridge start

- Kind: `local helper`
- Summary: Start a managed bridge daemon for one config file.
- Composition: Pure local helper. Resolves the installed `oar-agent-bridge` binary, infers the config role, launches the daemon in the background, and records pid/log metadata in a per-config manager directory.
- JSON body: `kind`, `config_path`, `pid`, `log_path`, `process_state_path`, `command`
- Examples:
  - `oar bridge start --config ./agent.toml`

Flags:
  --config <path>              Bridge config to start. The config must contain `[agent]`.
  --install-dir <dir>          Root directory for the managed bridge virtualenv.
  --bin-dir <dir>              Directory where the managed `oar-agent-bridge` wrapper should exist.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json bridge start ... ; oar bridge start ... --json
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
  - `oar bridge stop --config ./agent.toml --force`

Flags:
  --config <path>              Managed config to stop.
  --force                      Escalate to SIGKILL if SIGTERM does not stop the daemon before the timeout.
  --timeout-seconds <n>        How long to wait after SIGTERM before failing or force-killing.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json bridge stop ... ; oar bridge stop ... --json
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
  - `oar bridge restart --config ./agent.toml`

Flags:
  --config <path>              Managed config to restart.
  --force                      Force-kill during the stop phase if needed.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json bridge restart ... ; oar bridge restart ... --json
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
  - `oar bridge status --config ./agent.toml`

Flags:
  --config <path>              Managed config to inspect.
  --install-dir <dir>          Root directory for the managed bridge virtualenv.
  --bin-dir <dir>              Directory where the managed `oar-agent-bridge` wrapper should exist.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json bridge status ... ; oar bridge status ... --json
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
  - `oar bridge logs --config ./agent.toml --lines 200`

Flags:
  --config <path>              Managed config whose log should be tailed.
  --lines <n>                  How many recent lines to return. Default is 80.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json bridge logs ... ; oar bridge logs ... --json
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
  - `oar import scan --input ./workspace.zip`
  - `oar import scan --input ./vault --out ./.oar-import/vault`

Flags:
  --input <path>               Directory or `.zip` archive to scan.
  --out <dir>                  Output directory. Defaults to `./.oar-import/<source-name>`.
  --max-preview-bytes <n>      Maximum bytes to keep for preview extraction.
  --max-text-cache-bytes <n>   Maximum text-file size cached verbatim for later doc creation.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json import scan ... ; oar import scan ... --json
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
  - `oar import dedupe --inventory ./.oar-import/workspace/inventory.jsonl`
  - `oar import dedupe ./.oar-import/workspace/inventory.jsonl --out ./.oar-import/workspace`

Flags:
  --inventory <path>           Inventory produced by `oar import scan`. Positional form also supported.
  --out <dir>                  Output directory. Defaults to the inventory directory.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json import dedupe ... ; oar import dedupe ... --json
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
  - `oar import plan --inventory ./.oar-import/workspace/inventory.jsonl`
  - `oar import plan --inventory ./.oar-import/workspace/inventory.jsonl --dedupe ./.oar-import/workspace/dedupe.json --source-name 'workspace export'`

Flags:
  --inventory <path>           Inventory produced by `oar import scan`. Positional form also supported.
  --dedupe <path>              Dedupe report. Defaults to sibling `dedupe.json`.
  --out <dir>                  Output directory. Defaults to the inventory directory.
  --source-name <name>         High-signal human name used in titles, tags, and provenance. Defaults from the inventory directory.
  --collector-threshold <n>    Minimum cluster size that triggers a collector thread.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json import plan ... ; oar import plan ... --json
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
  - `oar import apply --plan ./.oar-import/workspace/plan.json`
  - `oar import apply --plan ./.oar-import/workspace/plan.json --execute --agent importer`

Flags:
  --plan <path>                Plan produced by `oar import plan`. Positional form also supported.
  --out <dir>                  Output directory for payload previews and apply results. Defaults to `<plan-dir>/apply`.
  --execute                    Actually call `topics create`, `artifacts create`, and `docs create`. Default is preview-only.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json import apply ... ; oar import apply ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```
