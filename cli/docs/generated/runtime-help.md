# OAR Runtime Help Reference

This reference is bundled with the CLI. Print the full document with `oar meta docs` or one topic with `oar meta doc <topic>`.

## Topics

- `onboarding` (manual): Offline quick-start mental model and first command flow.
- `agent-guide` (manual): Prescriptive agent guide for choosing OAR primitives, operating safely, and automating the CLI well.
- `agent-bridge` (manual): Install, configure, and operate the preferred `oar-agent-bridge` wake-routing runtime on a fresh machine.
- `wake-routing` (manual): How `@handle` wake routing works, including self-registration, verification, and troubleshooting.
- `draft` (manual): Local draft staging, listing, commit, and discard workflow.
- `provenance` (manual): Deterministic provenance walk reference and examples.
- `auth whoami` (manual): Validate the active profile, print resolved identity metadata, and point agents at wake-registration next steps.
- `auth list` (manual): List local CLI profiles and the active profile.
- `auth update-username` (manual): Rename the authenticated agent and sync the local profile.
- `auth rotate` (manual): Rotate the active agent key and refresh stored credentials.
- `auth revoke` (manual): Revoke the active agent and mark the local profile revoked. Use explicit human-lockout flags only for break-glass recovery.
- `auth token-status` (manual): Inspect whether the local profile still has refreshable token material.
- `bridge` (manual): CLI-managed bridge bootstrap helpers for installing, templating, and checking `oar-agent-bridge`.
- `import` (manual): Prescriptive import guide for building low-duplication, discoverable OAR graphs from external material.
- `actors` (group): List and register actor identities
- `auth` (group): Register, inspect, and manage auth state
- `threads` (group): Manage thread resources
- `commitments` (group): Manage commitment resources
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
- `actors list` (command): List actors
- `actors register` (command): Register actor identity metadata
- `auth register` (command): Register agent principal and initial key
- `auth invites list` (command): List onboarding invites
- `auth invites create` (command): Create onboarding invite
- `auth invites revoke` (command): Revoke onboarding invite
- `auth bootstrap status` (command): Read bootstrap onboarding availability
- `threads list` (command): List thread snapshots
- `threads get` (command): Get thread snapshot by id
- `threads create` (command): Create thread snapshot
- `threads patch` (command): Patch thread snapshot
- `threads timeline` (command): Get thread timeline events and referenced entities
- `threads context` (command): Get bundled thread context for agent callers
- `commitments list` (command): List commitments
- `commitments get` (command): Get commitment by id
- `commitments create` (command): Create commitment snapshot
- `commitments patch` (command): Patch commitment snapshot
- `artifacts list` (command): List artifact metadata
- `artifacts get` (command): Get artifact metadata by id
- `artifacts create` (command): Create artifact
- `artifacts content` (command): Get artifact raw content
- `artifacts tombstone` (command): Tombstone an artifact (soft-delete)
- `boards list` (command): List boards with derived summary data
- `boards create` (command): Create board
- `boards get` (command): Get board metadata
- `boards update` (command): Update board metadata
- `boards cards` (group): Nested generated help topic.
- `boards cards add` (command): Add existing thread to board as a card
- `boards cards update` (command): Update board card metadata
- `boards cards move` (command): Move board card across columns or ranks
- `boards cards remove` (command): Remove board card membership
- `docs list` (command): List documents and their current head metadata
- `docs create` (command): Create document with initial immutable revision
- `docs get` (command): Get document and authoritative head revision
- `docs update` (command): Create a new immutable revision for an existing document
- `docs history` (command): List ordered immutable revisions for a document
- `docs revision` (group): Nested generated help topic.
- `docs tombstone` (command): Tombstone a document (soft-delete)
- `docs revision get` (command): Get one immutable document revision
- `events get` (command): Get event by id
- `events create` (command): Append event
- `events stream` (command): Stream events via Server-Sent Events (SSE)
- `events tail` (command): Stream events via Server-Sent Events (SSE)
- `inbox list` (command): List derived inbox items
- `inbox get` (command): Get derived inbox item detail
- `inbox ack` (command): Acknowledge an inbox item
- `inbox stream` (command): Stream derived inbox items via SSE
- `inbox tail` (command): Stream derived inbox items via SSE
- `derived rebuild` (command): Rebuild derived views
- `meta commands` (command): List generated command metadata
- `meta command` (command): Get generated metadata for a command id
- `meta concepts` (command): List generated concept metadata
- `meta concept` (command): Get generated metadata for one concept
- `work-orders create` (command): Create work-order packet artifact
- `receipts create` (command): Create receipt packet artifact
- `reviews create` (command): Create review packet artifact
- `events list` (local-helper): Compose `threads timeline` responses with client-side thread/type/actor filters and preview summaries.
- `events validate` (local-helper): Validate an `events create` payload locally from stdin or `--from-file` without sending it.
- `events explain` (local-helper): Explain known event-type conventions, required refs, and validation hints, including which type surfaces as a visible thread message.
- `artifacts inspect` (local-helper): Fetch artifact metadata and resolved content in one command for operator inspection.
- `threads inspect` (local-helper): Canonical thread coordination read path: compose one view from `threads context` and related `inbox list` items.
- `threads workspace` (local-helper): Single holistic thread coordination read: combine context, inbox, recommendation review, and related-thread signals in one command.
- `threads review` (local-helper): Opinionated deep-read helper: run the holistic workspace view with related-event hydration and full summaries enabled by default.
- `threads recommendations` (local-helper): Review one thread's recommendation/decision inputs plus related-thread signals with provenance and follow-up hints.
- `threads propose-patch` (local-helper): Stage a thread patch proposal locally and show the diff before applying it.
- `threads apply` (local-helper): Apply a previously staged thread patch proposal.
- `commitments propose-patch` (local-helper): Stage a commitment patch proposal locally and show the diff before applying it.
- `commitments apply` (local-helper): Apply a previously staged commitment update proposal.
- `boards workspace` (local-helper): Canonical board read path: load one board's full state including primary thread, primary document, and all cards grouped by column.
- `boards cards list` (local-helper): List all cards on a board in canonical column order without hydrating thread details.
- `docs propose-update` (local-helper): Stage a document update proposal locally and show the content diff before applying it.
- `docs content` (local-helper): Show the current document content together with authoritative head revision metadata.
- `docs validate-update` (local-helper): Validate a `docs update` payload locally from stdin or file without sending the mutation.
- `docs apply` (local-helper): Apply a previously staged document update proposal.
- `meta skill` (local-helper): Render a bundled editor-specific skill file from the canonical OAR agent guide.
- `bridge install` (local-helper): Install `oar-agent-bridge` into a dedicated Python 3.11+ virtualenv and expose a PATH wrapper.
- `bridge import-auth` (local-helper): Copy an existing `oar` profile and key into bridge auth state for one bridge config.
- `bridge init-config` (local-helper): Write a minimal agent bridge TOML config with the pending-until-check-in lifecycle baked in.
- `bridge workspace-id` (local-helper): Discover durable workspace ids from an existing agent registration document.
- `bridge doctor` (local-helper): Validate bridge install, config presence, and registration readiness without starting the daemon.
- `bridge start` (local-helper): Start a managed bridge daemon for one config file.
- `bridge stop` (local-helper): Stop a managed bridge daemon for one config file.
- `bridge restart` (local-helper): Restart a managed bridge daemon for one config file.
- `bridge status` (local-helper): Inspect managed process state for a bridge config.
- `bridge logs` (local-helper): Read recent log lines for a managed bridge config.
- `import scan` (local-helper): Scan a folder or zip archive into a normalized inventory with text cache, repo-root hints, and cluster hints.
- `import dedupe` (local-helper): Create exact and probable duplicate reports from a scan inventory with conservative skip recommendations.
- `import plan` (local-helper): Build a conservative import plan that prefers collector threads, hub docs, dedupe-first writes, and low orphan rates.
- `import apply` (local-helper): Write payload previews for a plan and optionally execute thread/artifact/doc creates in dependency order.


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

  oar bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle>
  oar bridge init-config --kind zeroclaw --output ./zeroclaw.toml --workspace-id <workspace-id> --handle <handle>

These templates intentionally default the agent lifecycle to:

- `status = "pending"`
- `checkin_interval_seconds = 60`
- `checkin_ttl_seconds = 300`

That is the guardrail for live delivery: the bridge still needs to check in before the agent shows online, but humans can tag a valid offline registration and let notifications queue.

Workspace id source of truth

- `<workspace-id>` must be the durable workspace id for the deployment, not a slug and not a UI path segment.
- If an `agentreg.<handle>` document already exists, use `oar bridge workspace-id --handle <handle>` to read its enabled workspace bindings first.
- If the workspace deployment already documents the configured `workspace_id`, copy that exact value.
- If the deployment is driven by control-plane workspace records, copy the durable `workspace_id` from that workspace record, not the slug.
- The bundled example value `ws_main` is only a sample.
- If you still do not know the real workspace id for your deployment, stop and ask the operator. Do not guess.

First-time agent-host path

1. Install the runtime:

  oar bridge install

2. Render the agent config:

  oar bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle>

3. If a matching `oar` profile already exists for the target principal, import it into the bridge config:

  oar bridge import-auth --config ./agent.toml --from-profile <agent>

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

- `oar-agent-bridge registration apply` writes the registration document, but the bridge runtime still owns live presence updates.
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
  - inspect `agentreg.<handle>` for actor mismatch, disabled status, stale check-in, or missing workspace binding

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
- The durable registration document id is `agentreg.<handle>`.
- The bridge-owned readiness proof is the latest `agent_bridge_checked_in` event referenced by `agentreg.<handle>`.
- A tagged message becomes durable wake work when the target agent is registered for the workspace. Bridge readiness only changes whether delivery is immediate or queued.

What counts as taggable

- principal kind is `agent`
- principal is not revoked
- principal has a username/handle
- registration document `agentreg.<handle>` exists
- registration document `actor_id` matches the principal actor
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
- Use `oar bridge workspace-id --handle <handle>` when an existing registration doc is the easiest source of truth for the durable workspace id.
- Use `oar bridge import-auth --config ./agent.toml --from-profile <agent>` when matching `oar` auth already exists.
- Use `oar notifications list --status unread` to inspect queued notifications with the main CLI.
- Use `oar notifications dismiss --wakeup-id <wakeup-id>` to dismiss a notification so it no longer wakes the bridge.
- Use `oar auth whoami` to confirm your current username and actor id.
- Use `oar docs get --document-id agentreg.<handle> --json` to inspect a registration document directly.

Preferred path when you are using `oar-agent-bridge`

1. Install the runtime:

  oar bridge install

2. Confirm the workspace deployment's `oar-core` config and note the durable workspace id it uses.

3. Generate the agent config:

  oar bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle>

4. If matching `oar` auth already exists, import it into the bridge config:

  oar bridge import-auth --config ./agent.toml --from-profile <agent>

5. Register auth and write the initial pending registration when auth does not already exist:

  oar-agent-bridge auth register --config ./agent.toml --invite-token <token> --apply-registration

  If auth already exists and you only need to rewrite the registration document:

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

If you are writing the document manually, only create the pending registration skeleton. Manual docs writes do not replace the live bridge-owned check-in event.

1. Confirm the identity you are registering:

  oar auth whoami

  Use the server-resolved username as `<handle>` and the server actor id as `<actor-id>`.

2. Resolve the durable workspace id you want to enable:

  - If an existing registration doc is available, start with `oar bridge workspace-id --handle <handle>` or `oar bridge workspace-id --document-id agentreg.<handle>`.
  - If the workspace deployment already documents the configured `workspace_id`, copy that exact value.
  - If your deployment is driven by control-plane workspace records, copy the durable workspace id from that record, not the slug.
  - The bundled example value `ws_main` is only a sample.
  - Do not use a workspace slug or URL path segment. If you cannot determine the real value, stop and ask the operator.

3. Create a first-time registration payload such as `wake-registration.json`:

  {
    "document": {
      "document_id": "agentreg.<handle>",
      "title": "Agent registration @<handle>",
      "status": "pending",
      "labels": [
        "agent-registration",
        "handle:<handle>",
        "actor:<actor-id>"
      ]
    },
    "content_type": "structured",
    "content": {
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

4. For first-time registration, create the document:

  oar docs create --from-file wake-registration.json --json

5. If `agentreg.<handle>` already exists, update it instead of retrying create:

  oar docs get --document-id agentreg.<handle> --json
  oar docs update --document-id agentreg.<handle> --from-file wake-registration-update.json --json

6. If `docs create` returns `conflict`, inspect the existing document and update it instead of retrying create blindly.

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
- Do not try to hand-author the bridge readiness proof. The supported path is to let the running bridge emit `agent_bridge_checked_in` and rewrite the registration.

Verification flow

1. Confirm your local and server identity:

  oar auth whoami

2. Confirm a principal exists for the target handle:

  oar auth principals list --json

3. Read the registration document:

  oar docs get --document-id agentreg.<handle> --json

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
- missing registration: `agentreg.<handle>` does not exist
- registration actor mismatch: the registration doc points at a different actor
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
- Prefer command-specific proposal flows when they exist, such as `threads propose-patch` or `docs propose-update`, because they add domain-aware diff/review helpers.
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
  cat payload.json | oar draft create --command threads.create
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
  bridge workspace-id Read workspace ids from an existing registration document
  bridge doctor       Validate install/config/readiness without starting daemons

Recommended order

1. `oar bridge install`
2. `oar bridge workspace-id --handle <handle>` if a registration doc already exists and you need the real durable workspace id
3. `oar bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle>`
4. `oar bridge import-auth --config ./agent.toml --from-profile <agent>` when matching `oar` auth already exists
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

Use `oar import` to turn external material into a clean OAR graph. The goal is not to dump files into the system. The goal is to create discoverable threads, docs, and artifacts with low duplication, low orphan rates, and clear provenance.

Object model

- `threads` hold ongoing work, collector structures, and discoverable entry points.
- `docs` hold narrative knowledge, summaries, and hub content.
- `artifacts` hold raw or attached evidence.
- Import should create a graph that people and agents can navigate, not just a pile of uploaded files.

Read in this order

1. `oar help import` — doctrine, quality bars, and the recommended loop.
2. `oar help import scan` — inventory and text-cache generation.
3. `oar help import plan` — classification, collector threads, hub docs, and review bundles.
4. If you will execute writes: `oar help threads create`, `oar help artifacts create`, and `oar help docs create`.
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

## `actors`

List and register actor identities

```text
Generated Help: actors

Commands:
  actors list              List actors
  actors register          Register actor identity metadata

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json actors ... ; oar actors ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `auth`

Register, inspect, and manage auth state

```text
Generated Help: auth

Commands:
  auth register            Register agent principal and initial key

Local auth lifecycle helpers:
  auth whoami             Validate the active profile against the server and show resolved identity.
  auth list               List local CLI profiles and which one is active.
  auth update-username    Update the current principal username and sync the local profile.
  auth rotate             Rotate the active agent key and refresh stored credentials.
  auth revoke             Revoke the active agent and mark the local profile revoked. Use explicit human-lockout flags only for break-glass recovery.
  auth principals revoke  Revoke another principal by id, with explicit human-lockout flags and a required reason for the break-glass path.
  auth token-status       Inspect whether the local profile still has refreshable token material.
  Tip: use `oar auth bootstrap status` before first registration, `oar auth register --username <username> --bootstrap-token <token>` for the first principal, and `oar auth invites create --kind human|agent` before later registrations.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json auth ... ; oar auth ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `threads`

Manage thread resources

```text
Generated Help: threads

Commands:
  threads context          Get bundled thread context for agent callers
  threads create           Create thread snapshot
  threads get              Get thread snapshot by id
  threads list             List thread snapshots
  threads patch            Patch thread snapshot
  threads timeline         Get thread timeline events and referenced entities
  threads workspace        Get thread workspace projection

Canonical coordination read path:
  threads review              Deep-read one thread workspace with review hydration enabled by default.
  threads workspace           Compose one holistic thread workspace from context + inbox + related-thread review.
  threads inspect             Compose one thread coordination view from context + inbox in one command.
  threads recommendations     Focus recommendation/decision review with actor+timestamp provenance.
  Mutation flow:
  threads patch               Send the thread patch to core immediately.
  threads propose-patch       Stage a thread patch proposal and inspect the diff before applying.
  threads apply               Apply a staged thread patch proposal.
  Tip: start with `oar threads review` when you want one deep review read, use `oar threads workspace` for the canonical coordination view, use `--status/--tag/--type initiative` to discover one thread, use `oar threads context` for cross-thread aggregates, and `oar threads get` for raw snapshot-only reads. Add `--full-id` for copy/paste ids.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads ... ; oar threads ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `commitments`

Manage commitment resources

```text
Generated Help: commitments

Commands:
  commitments create       Create commitment snapshot
  commitments get          Get commitment by id
  commitments list         List commitments
  commitments patch        Patch commitment snapshot

Mutation flow:
  commitments patch          Send the commitment patch to core immediately.
  commitments propose-patch  Stage a commitment patch proposal and inspect the diff before applying.
  commitments apply          Apply a staged commitment update proposal.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json commitments ... ; oar commitments ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `artifacts`

Manage artifact resources and content

```text
Generated Help: artifacts

Commands:
  artifacts create         Create artifact
  artifacts get            Get artifact metadata by id
  artifacts list           List artifact metadata
  artifacts tombstone      Tombstone an artifact (soft-delete)

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
  boards get               Get board metadata
  boards list              List boards with derived summary data
  boards update            Update board metadata
  boards workspace         Get board workspace projection

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
  docs create              Create document with initial immutable revision
  docs get                 Get document and authoritative head revision
  docs history             List ordered immutable revisions for a document
  docs list                List documents and their current head metadata
  docs tombstone           Tombstone a document (soft-delete)
  docs update              Create a new immutable revision for an existing document

Local inspection helpers:
  docs content             Show current document content with revision metadata.
  Mutation flow:
  docs update              Send the document update to core immediately.
  docs propose-update      Stage an update proposal and inspect its diff before applying it.
  docs apply               Apply a staged document update proposal.
  docs validate-update     Validate a docs.update payload from stdin/--from-file.
  Tip: add `--content-file <path>` to avoid hand-escaping multiline content.

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
  events create            Append event
  events get               Get event by id
  events stream            Stream events via Server-Sent Events (SSE)

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
  inbox ack                Acknowledge an inbox item
  inbox get                Get derived inbox item detail
  inbox list               List derived inbox items
  inbox stream             Stream derived inbox items via SSE

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
  work-orders create       Create work-order packet artifact

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
  receipts create          Create receipt packet artifact

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
  reviews create           Create review packet artifact

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json reviews ... ; oar reviews ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `derived`

Run derived-view maintenance actions

```text
Generated Help: derived

Commands:
  derived rebuild          Rebuild derived views

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json derived ... ; oar derived ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `meta`

Inspect generated command/concept metadata

```text
Generated Help: meta

Commands:
  meta command             Get generated metadata for a command id
  meta commands            List generated command metadata
  meta concept             Get generated metadata for one concept
  meta concepts            List generated concept metadata

Shipped reference docs:
  meta docs               Print the bundled Markdown runtime reference.
  meta doc                Print one bundled Markdown topic, for example `oar meta doc agent-guide`.
  meta skill              Render a bundled editor-specific skill file, for example `oar meta skill cursor`.
  Tip: use `oar help meta` for the short runtime surface, `oar meta docs` for the full shipped reference, and `oar meta skill cursor --write-dir ~/.cursor/skills/oar-cli-onboard` to export a Cursor skill.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json meta ... ; oar meta ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `actors list`

List actors

```text
Generated Help: actors list

- Command ID: `actors.list`
- CLI path: `actors list`
- HTTP: `GET /actors`
- Stability: `stable`
- Input mode: `none`
- Why: Resolve available actor identities for routing writes.
- Output: Returns `{ actors, next_cursor? }` ordered by created time ascending. Pagination is optional and backward-compatible.
- Error codes: `actor_registry_unavailable`
- Concepts: `identity`
- Agent notes: Safe and idempotent. Optional pagination with `q` for search, `limit` for page size, and `cursor` for continuation.
- Adjacent commands: `actors register`
- Examples:
  - List actors: `oar actors list --json`
  - Search actors by name: `oar actors list --q "bot" --json`
  - Paginated actor list: `oar actors list --limit 50 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json actors list ... ; oar actors list ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `actors register`

Register actor identity metadata

```text
Generated Help: actors register

- Command ID: `actors.register`
- CLI path: `actors register`
- HTTP: `POST /actors`
- Stability: `stable`
- Input mode: `json-body`
- Why: Bootstrap an authenticated caller identity before mutating thread state.
- Output: Returns `{ actor }` with canonicalized stored values.
- Error codes: `invalid_json`, `invalid_request`, `actor_exists`
- Concepts: `identity`
- Agent notes: Not idempotent by default; repeated creates with same id return conflict.
- Adjacent commands: `actors list`
- Examples:
  - Register actor: `oar actors register --id bot-1 --display-name "Bot 1" --created-at 2026-03-04T10:00:00Z --json`

Body schema:
  Required: actor (object)
  Optional: none

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json actors register ... ; oar actors register ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth register`

Register agent principal and initial key

```text
Generated Help: auth register

- Command ID: `auth.agents.register`
- CLI path: `auth register`
- HTTP: `POST /auth/agents/register`
- Stability: `beta`
- Input mode: `json-body`
- Why: Register an agent principal with a bootstrap token for the first principal or an invite token for later principals.
- Output: Returns `{ agent, key, tokens }`.
- Error codes: `invalid_json`, `invalid_request`, `invalid_token`, `username_taken`
- Concepts: `auth`, `identity`
- Agent notes: Bootstrap is accepted only for the first successful principal registration. Later registrations require an invite token.
- Adjacent commands: `auth audit list`, `auth bootstrap status`, `auth invites create`, `auth invites list`, `auth invites revoke`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth token`
- Examples:
  - Bootstrap first agent: `oar auth register --username agent.one --bootstrap-token <token> --json`
  - Register invited agent: `oar auth register --username agent.two --invite-token <token> --json`

Body schema:
  Required: public_key (string), username (string)
  Optional: bootstrap_token (string), invite_token (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json auth register ... ; oar auth register ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth invites list`

List onboarding invites

```text
Generated Help: auth invites list

- Command ID: `auth.invites.list`
- CLI path: `auth invites list`
- HTTP: `GET /auth/invites`
- Stability: `beta`
- Input mode: `none`
- Why: Inspect current invite state without exposing token secrets.
- Output: Returns `{ invites }` ordered by create time descending.
- Error codes: `auth_required`, `invalid_token`, `agent_revoked`
- Concepts: `auth`, `onboarding`
- Agent notes: Requires Bearer access token. Returned invites contain metadata only, never raw tokens.
- Adjacent commands: `auth audit list`, `auth bootstrap status`, `auth invites create`, `auth invites revoke`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth register`, `auth token`
- Examples:
  - List invites: `oar auth invites list --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json auth invites list ... ; oar auth invites list ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth invites create`

Create onboarding invite

```text
Generated Help: auth invites create

- Command ID: `auth.invites.create`
- CLI path: `auth invites create`
- HTTP: `POST /auth/invites`
- Stability: `beta`
- Input mode: `json-body`
- Why: Mint a single-use invite token for a future human or agent registration.
- Output: Returns `{ invite, token }`. The raw token is returned only once at creation time.
- Error codes: `auth_required`, `invalid_json`, `invalid_request`, `invalid_token`, `agent_revoked`
- Concepts: `auth`, `onboarding`
- Agent notes: Requires Bearer access token. `kind` may be `human`, `agent`, or `any`.
- Adjacent commands: `auth audit list`, `auth bootstrap status`, `auth invites list`, `auth invites revoke`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth register`, `auth token`
- Examples:
  - Create agent invite: `oar auth invites create --kind agent --json`

Body schema:
  Required: kind (string)
  Optional: expires_at (datetime)
  Enum values: kind: agent, any, human

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json auth invites create ... ; oar auth invites create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth invites revoke`

Revoke onboarding invite

```text
Generated Help: auth invites revoke

- Command ID: `auth.invites.revoke`
- CLI path: `auth invites revoke`
- HTTP: `POST /auth/invites/{invite_id}/revoke`
- Stability: `beta`
- Input mode: `none`
- Why: Invalidate an invite token before it is consumed.
- Output: Returns `{ invite }` with updated revoke metadata.
- Error codes: `auth_required`, `invalid_token`, `agent_revoked`, `not_found`
- Concepts: `auth`, `onboarding`
- Agent notes: Requires Bearer access token.
- Adjacent commands: `auth audit list`, `auth bootstrap status`, `auth invites create`, `auth invites list`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth register`, `auth token`
- Examples:
  - Revoke invite: `oar auth invites revoke --invite-id invite_123 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json auth invites revoke ... ; oar auth invites revoke ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth bootstrap status`

Read bootstrap onboarding availability

```text
Generated Help: auth bootstrap status

- Command ID: `auth.bootstrap.status`
- CLI path: `auth bootstrap status`
- HTTP: `GET /auth/bootstrap/status`
- Stability: `beta`
- Input mode: `none`
- Why: Check whether first-principal bootstrap registration is still available for this workspace.
- Output: Returns `{ bootstrap_registration_available }` without exposing token material.
- Concepts: `auth`, `onboarding`
- Agent notes: This endpoint is intentionally non-enumerating beyond the single bootstrap availability boolean.
- Adjacent commands: `auth audit list`, `auth invites create`, `auth invites list`, `auth invites revoke`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth register`, `auth token`
- Examples:
  - Read bootstrap status: `oar auth bootstrap status --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json auth bootstrap status ... ; oar auth bootstrap status ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads list`

List thread snapshots

```text
Generated Help: threads list

- Command ID: `threads.list`
- CLI path: `threads list`
- HTTP: `GET /threads`
- Stability: `stable`
- Input mode: `none`
- Why: Retrieve current thread state for triage and scheduling decisions.
- Output: Returns `{ threads, next_cursor? }`; query filters are additive. Pagination is optional and backward-compatible.
- Error codes: `invalid_request`
- Concepts: `threads`, `filtering`
- Agent notes: Safe and idempotent. Optional pagination with `q` for search, `limit` for page size, and `cursor` for continuation.
- Adjacent commands: `threads context`, `threads create`, `threads get`, `threads patch`, `threads timeline`, `threads workspace`
- Examples:
  - List active p1 threads: `oar threads list --status active --priority p1 --json`
  - Search threads by title: `oar threads list --q "launch" --json`
  - Paginated thread list: `oar threads list --limit 20 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads list ... ; oar threads list ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads get`

Get thread snapshot by id

```text
Generated Help: threads get

- Command ID: `threads.get`
- CLI path: `threads get`
- HTTP: `GET /threads/{thread_id}`
- Stability: `stable`
- Input mode: `none`
- Why: Resolve a raw authoritative thread snapshot for low-level reads before patching or composing packets.
- Output: Returns `{ thread }`.
- Error codes: `not_found`
- Concepts: `threads`
- Agent notes: Safe and idempotent. Prefer `oar threads inspect` for operator coordination reads.
- Adjacent commands: `threads context`, `threads create`, `threads list`, `threads patch`, `threads timeline`, `threads workspace`
- Examples:
  - Read thread: `oar threads get --thread-id thread_123 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads get ... ; oar threads get ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads create`

Create thread snapshot

```text
Generated Help: threads create

- Command ID: `threads.create`
- CLI path: `threads create`
- HTTP: `POST /threads`
- Stability: `stable`
- Input mode: `json-body`
- Why: Open a new thread for tracking ongoing organizational work.
- Output: Returns `{ thread }` including generated id and audit fields.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`
- Concepts: `threads`, `snapshots`
- Agent notes: Replay-safe when `request_key` is reused with the same body; otherwise core issues a new canonical thread id.
- Adjacent commands: `threads context`, `threads get`, `threads list`, `threads patch`, `threads timeline`, `threads workspace`
- Examples:
  - Create thread: `oar threads create --from-file thread.json --json`

Body schema:
  Required: thread.cadence (string), thread.current_summary (string), thread.key_artifacts (list<typed_ref>), thread.next_actions (list<string>), thread.priority (string), thread.provenance.sources (list<string>), thread.status (string), thread.tags (list<string>), thread.title (string), thread.type (string)
  Optional: actor_id (string), request_key (string), thread.next_check_in_at (datetime), thread.provenance.by_field (map<string, list<string>>), thread.provenance.notes (string)
  Enum values: thread.priority (strict): p0, p1, p2, p3; thread.status (strict): active, closed, paused; thread.type (strict): case, incident, initiative, other, process, relationship

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads create ... ; oar threads create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads patch`

Patch thread snapshot

```text
Generated Help: threads patch

- Command ID: `threads.patch`
- CLI path: `threads patch`
- HTTP: `PATCH /threads/{thread_id}`
- Stability: `stable`
- Input mode: `json-body`
- Why: Update mutable thread fields while preserving unknown data and auditability.
- Output: Returns `{ thread }` after patch merge and emitted event side effect.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`, `not_found`
- Concepts: `threads`, `patch`
- Agent notes: Use `if_updated_at` for optimistic concurrency.
- Adjacent commands: `threads context`, `threads create`, `threads get`, `threads list`, `threads timeline`, `threads workspace`
- Examples:
  - Patch thread: `oar threads patch --thread-id thread_123 --from-file patch.json --json`

Body schema:
  Required: none
  Optional: actor_id (string), if_updated_at (datetime), patch.cadence (string), patch.current_summary (string), patch.key_artifacts (list<typed_ref>), patch.next_actions (list<string>), patch.next_check_in_at (datetime), patch.priority (string), patch.provenance.by_field (map<string, list<string>>), patch.provenance.notes (string), patch.provenance.sources (list<string>), patch.status (string), patch.tags (list<string>), patch.title (string), patch.type (string)
  Enum values: patch.priority (strict): p0, p1, p2, p3; patch.status (strict): active, closed, paused; patch.type (strict): case, incident, initiative, other, process, relationship

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads patch ... ; oar threads patch ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads timeline`

Get thread timeline events and referenced entities

```text
Generated Help: threads timeline

- Command ID: `threads.timeline`
- CLI path: `threads timeline`
- HTTP: `GET /threads/{thread_id}/timeline`
- Stability: `stable`
- Input mode: `none`
- Why: Retrieve narrative event history plus referenced snapshots/artifacts in one call.
- Output: Returns `{ events, snapshots, artifacts }` where snapshot/artifact maps are sparse.
- Error codes: `not_found`
- Concepts: `threads`, `events`, `provenance`
- Agent notes: Events stay time ordered; missing refs are omitted from expansion maps.
- Adjacent commands: `threads context`, `threads create`, `threads get`, `threads list`, `threads patch`, `threads workspace`
- Examples:
  - Timeline: `oar threads timeline --thread-id thread_123 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads timeline ... ; oar threads timeline ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads context`

Get bundled thread context for agent callers

```text
Generated Help: threads context

- Command ID: `threads.context`
- CLI path: `threads context`
- HTTP: `GET /threads/{thread_id}/context`
- Stability: `beta`
- Input mode: `none`
- Why: Load one thread's state, recent events, key artifacts, open commitments, and linked documents in a single round-trip; CLI `oar threads context` can aggregate across threads by composing multiple calls.
- Output: Returns `{ thread, recent_events, key_artifacts, open_commitments, documents }`.
- Error codes: `invalid_request`, `not_found`
- Concepts: `threads`, `events`, `artifacts`, `commitments`, `docs`
- Agent notes: Derived thread context projection; do not build durable automation directly on projection payload shapes. Prefer canonical events and threads for durable substrate. Use include_artifact_content for prompt-ready previews; default mode keeps payloads lighter. Prefer `oar threads inspect` as the first single-thread coordination read.
- Adjacent commands: `threads create`, `threads get`, `threads list`, `threads patch`, `threads timeline`, `threads workspace`
- Examples:
  - Context with defaults: `oar threads context --thread-id thread_123 --json`
  - Context with artifact previews: `oar threads context --thread-id thread_123 --include-artifact-content --max-events 50 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads context ... ; oar threads context ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `commitments list`

List commitments

```text
Generated Help: commitments list

- Command ID: `commitments.list`
- CLI path: `commitments list`
- HTTP: `GET /commitments`
- Stability: `stable`
- Input mode: `none`
- Why: Monitor open/blocked work and due windows.
- Output: Returns `{ commitments }`.
- Error codes: `invalid_request`
- Concepts: `commitments`, `filtering`
- Agent notes: Safe and idempotent.
- Adjacent commands: `commitments create`, `commitments get`, `commitments patch`
- Examples:
  - List open commitments for a thread: `oar commitments list --thread-id thread_123 --status open --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json commitments list ... ; oar commitments list ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `commitments get`

Get commitment by id

```text
Generated Help: commitments get

- Command ID: `commitments.get`
- CLI path: `commitments get`
- HTTP: `GET /commitments/{commitment_id}`
- Stability: `stable`
- Input mode: `none`
- Why: Read commitment status/details before status transitions.
- Output: Returns `{ commitment }`.
- Error codes: `not_found`
- Concepts: `commitments`
- Agent notes: Safe and idempotent.
- Adjacent commands: `commitments create`, `commitments list`, `commitments patch`
- Examples:
  - Get commitment: `oar commitments get --commitment-id commitment_123 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json commitments get ... ; oar commitments get ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `commitments create`

Create commitment snapshot

```text
Generated Help: commitments create

- Command ID: `commitments.create`
- CLI path: `commitments create`
- HTTP: `POST /commitments`
- Stability: `stable`
- Input mode: `json-body`
- Why: Track accountable work items tied to a thread.
- Output: Returns `{ commitment }` with generated id.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`
- Concepts: `commitments`
- Agent notes: Replay-safe when `request_key` is reused with the same body; otherwise each create issues a new commitment id.
- Adjacent commands: `commitments get`, `commitments list`, `commitments patch`
- Examples:
  - Create commitment: `oar commitments create --from-file commitment.json --json`

Body schema:
  Required: commitment.definition_of_done (list<string>), commitment.due_at (datetime), commitment.links (list<typed_ref>), commitment.owner (string), commitment.provenance.sources (list<string>), commitment.status (string), commitment.thread_id (string), commitment.title (string)
  Optional: actor_id (string), commitment.provenance.by_field (map<string, list<string>>), commitment.provenance.notes (string), request_key (string)
  Enum values: commitment.status (strict): blocked, canceled, done, open

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json commitments create ... ; oar commitments create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `commitments patch`

Patch commitment snapshot

```text
Generated Help: commitments patch

- Command ID: `commitments.patch`
- CLI path: `commitments patch`
- HTTP: `PATCH /commitments/{commitment_id}`
- Stability: `stable`
- Input mode: `json-body`
- Why: Update ownership, due date, or status with evidence-aware transition rules.
- Output: Returns `{ commitment }` and emits a status-change event when applicable.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`, `not_found`
- Concepts: `commitments`, `patch`, `provenance`
- Agent notes: Provide `refs` for restricted transitions and use `if_updated_at` to avoid lost updates.
- Adjacent commands: `commitments create`, `commitments get`, `commitments list`
- Examples:
  - Mark commitment done: `oar commitments patch --commitment-id commitment_123 --from-file commitment-patch.json --json`

Body schema:
  Required: none
  Optional: actor_id (string), if_updated_at (datetime), patch.definition_of_done (list<string>), patch.due_at (datetime), patch.links (list<typed_ref>), patch.owner (string), patch.provenance.by_field (map<string, list<string>>), patch.provenance.notes (string), patch.provenance.sources (list<string>), patch.status (string), patch.title (string), refs (list<string>)
  Enum values: patch.status (strict): blocked, canceled, done, open

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json commitments patch ... ; oar commitments patch ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts list`

List artifact metadata

```text
Generated Help: artifacts list

- Command ID: `artifacts.list`
- CLI path: `artifacts list`
- HTTP: `GET /artifacts`
- Stability: `stable`
- Input mode: `none`
- Why: Discover evidence and packets attached to threads.
- Output: Returns `{ artifacts }` metadata only.
- Error codes: `invalid_request`
- Concepts: `artifacts`, `filtering`
- Agent notes: Safe and idempotent.
- Adjacent commands: `artifacts content get`, `artifacts create`, `artifacts get`, `artifacts tombstone`
- Examples:
  - List work orders for a thread: `oar artifacts list --kind work_order --thread-id thread_123 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json artifacts list ... ; oar artifacts list ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts get`

Get artifact metadata by id

```text
Generated Help: artifacts get

- Command ID: `artifacts.get`
- CLI path: `artifacts get`
- HTTP: `GET /artifacts/{artifact_id}`
- Stability: `stable`
- Input mode: `none`
- Why: Resolve artifact refs before downloading or rendering content.
- Output: Returns `{ artifact }` metadata.
- Error codes: `not_found`
- Concepts: `artifacts`
- Agent notes: Safe and idempotent.
- Adjacent commands: `artifacts content get`, `artifacts create`, `artifacts list`, `artifacts tombstone`
- Examples:
  - Get artifact: `oar artifacts get --artifact-id artifact_123 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json artifacts get ... ; oar artifacts get ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts create`

Create artifact

```text
Generated Help: artifacts create

- Command ID: `artifacts.create`
- CLI path: `artifacts create`
- HTTP: `POST /artifacts`
- Stability: `stable`
- Input mode: `file-and-body`
- Why: Persist immutable evidence blobs and metadata for references and review.
- Output: Returns `{ artifact }` metadata after content write.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Concepts: `artifacts`, `evidence`
- Agent notes: Treat as non-idempotent unless caller controls artifact id collisions.
- Adjacent commands: `artifacts content get`, `artifacts get`, `artifacts list`, `artifacts tombstone`
- Examples:
  - Create structured artifact: `oar artifacts create --from-file artifact-create.json --json`

Body schema:
  Required: artifact (object), content (object|string), content_type (string)
  Optional: actor_id (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json artifacts create ... ; oar artifacts create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts content`

Get artifact raw content

```text
Generated Help: artifacts content

- Command ID: `artifacts.content.get`
- CLI path: `artifacts content get`
- HTTP: `GET /artifacts/{artifact_id}/content`
- Stability: `stable`
- Input mode: `none`
- Why: Fetch opaque artifact bytes for downstream processors.
- Output: Raw bytes; content type mirrors stored artifact media.
- Error codes: `not_found`
- Concepts: `artifacts`, `content`
- Agent notes: Stream to file for large payloads.
- Adjacent commands: `artifacts create`, `artifacts get`, `artifacts list`, `artifacts tombstone`
- Examples:
  - Download content: `oar artifacts content get --artifact-id artifact_123 > artifact.bin`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json artifacts content ... ; oar artifacts content ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts tombstone`

Tombstone an artifact (soft-delete)

```text
Generated Help: artifacts tombstone

- Command ID: `artifacts.tombstone`
- CLI path: `artifacts tombstone`
- HTTP: `POST /artifacts/{artifact_id}/tombstone`
- Stability: `beta`
- Input mode: `json-body`
- Why: Mark an artifact as inactive while preserving provenance; tombstoned artifacts are excluded from list by default.
- Output: Returns `{ artifact }` with updated tombstone metadata.
- Error codes: `invalid_json`, `invalid_request`, `not_found`
- Concepts: `artifacts`, `lifecycle`
- Agent notes: Idempotent; repeated tombstone calls on the same artifact are safe.
- Adjacent commands: `artifacts content get`, `artifacts create`, `artifacts get`, `artifacts list`
- Examples:
  - Tombstone artifact: `oar artifacts tombstone --artifact-id artifact_123 --reason "superseded by newer version" --json`

Body schema:
  Required: actor_id (string)
  Optional: reason (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json artifacts tombstone ... ; oar artifacts tombstone ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards list`

List boards with derived summary data

```text
Generated Help: boards list

- Command ID: `boards.list`
- CLI path: `boards list`
- HTTP: `GET /boards`
- Stability: `beta`
- Input mode: `none`
- Why: Discover durable coordination boards with enough summary data for list pages and CLI triage without per-board fan-out.
- Output: Returns `{ boards, next_cursor? }`, where each item includes canonical board metadata plus a derived summary. Pagination is optional and backward-compatible.
- Error codes: `invalid_request`
- Concepts: `boards`, `planning`, `summaries`
- Agent notes: Safe and idempotent. Use repeatable `label` and `owner` filters to narrow the list server-side. Optional pagination with `q` for search, `limit` for page size, and `cursor` for continuation.
- Adjacent commands: `boards cards add`, `boards cards list`, `boards cards move`, `boards cards remove`, `boards cards update`, `boards create`, `boards get`, `boards update`, `boards workspace`
- Examples:
  - List boards: `oar boards list --json`
  - List active boards for an owner: `oar boards list --status active --owner actor_ceo --json`
  - Search boards by label: `oar boards list --q "launch" --json`
  - Paginated board list: `oar boards list --limit 30 --json`


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
- Why: Create a first-class coordination board with a canonical primary thread and optional primary document.
- Output: Returns `{ board }` with server-owned identity and concurrency metadata.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`
- Concepts: `boards`, `planning`, `concurrency`
- Agent notes: Replay-safe when `request_key` is reused with the same body. The primary thread is required and is never created as a card implicitly.
- Adjacent commands: `boards cards add`, `boards cards list`, `boards cards move`, `boards cards remove`, `boards cards update`, `boards get`, `boards list`, `boards update`, `boards workspace`
- Examples:
  - Create board: `oar boards create --from-file board-create.json --json`

Body schema:
  Required: board.primary_thread_id (string), board.title (string)
  Optional: actor_id (string), board.column_schema (list<any>), board.id (string), board.labels (list<string>), board.owners (list<string>), board.pinned_refs (list<string>), board.primary_document_id (string), board.status (string), request_key (string)
  Enum values: board.status: active, closed, paused

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards create ... ; oar boards create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards get`

Get board metadata

```text
Generated Help: boards get

- Command ID: `boards.get`
- CLI path: `boards get`
- HTTP: `GET /boards/{board_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve one board's canonical metadata and concurrency token without hydrating the full workspace projection.
- Output: Returns `{ board }`.
- Error codes: `invalid_request`, `not_found`
- Concepts: `boards`, `planning`
- Agent notes: Safe and idempotent.
- Adjacent commands: `boards cards add`, `boards cards list`, `boards cards move`, `boards cards remove`, `boards cards update`, `boards create`, `boards list`, `boards update`, `boards workspace`
- Examples:
  - Get board: `oar boards get --board-id board_product_launch --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards get ... ; oar boards get ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards update`

Update board metadata

```text
Generated Help: boards update

- Command ID: `boards.update`
- CLI path: `boards update`
- HTTP: `PATCH /boards/{board_id}`
- Stability: `beta`
- Input mode: `json-body`
- Why: Patch mutable board metadata with optimistic concurrency while preserving server-owned identity and timestamps.
- Output: Returns `{ board }` after the metadata patch is applied.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`, `not_found`
- Concepts: `boards`, `planning`, `concurrency`
- Agent notes: Set `if_updated_at` from `boards.get` or `boards.workspace` to avoid lost updates.
- Adjacent commands: `boards cards add`, `boards cards list`, `boards cards move`, `boards cards remove`, `boards cards update`, `boards create`, `boards get`, `boards list`, `boards workspace`
- Examples:
  - Update board metadata: `oar boards update --board-id board_product_launch --from-file board-update.json --json`

Body schema:
  Required: if_updated_at (datetime)
  Optional: actor_id (string), patch.column_schema (list<any>), patch.labels (list<string>), patch.owners (list<string>), patch.pinned_refs (list<string>), patch.primary_document_id (any|string), patch.status (string), patch.title (string)
  Enum values: patch.status: active, closed, paused

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards update ... ; oar boards update ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards cards`

Nested generated help topic.

```text
Generated Help: boards cards

Commands:
  boards cards add         Add existing thread to board as a card
  boards cards list        List ordered board cards
  boards cards move        Move board card across columns or ranks
  boards cards remove      Remove board card membership
  boards cards update      Update board card metadata

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards cards ... ; oar boards cards ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `boards cards add`

Add existing thread to board as a card

```text
Generated Help: boards cards add

- Command ID: `boards.cards.add`
- CLI path: `boards cards add`
- HTTP: `POST /boards/{board_id}/cards`
- Stability: `beta`
- Input mode: `json-body`
- Why: Create explicit board membership for an existing thread with canonical column placement and server-owned rank.
- Output: Returns `{ board, card }` after membership creation and board concurrency-token advancement.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`, `not_found`
- Concepts: `boards`, `planning`, `ordering`, `concurrency`
- Agent notes: Replay-safe when `request_key` is reused with the same body. The board primary thread cannot be added as a card.
- Adjacent commands: `boards cards list`, `boards cards move`, `boards cards remove`, `boards cards update`, `boards create`, `boards get`, `boards list`, `boards update`, `boards workspace`
- Examples:
  - Add card to backlog: `oar boards cards add --board-id board_product_launch --from-file board-card-add.json --json`

Body schema:
  Required: thread_id (string)
  Optional: actor_id (string), after_thread_id (string), before_thread_id (string), column_key (string), if_board_updated_at (datetime), pinned_document_id (string), request_key (string)
  Enum values: column_key: backlog, blocked, done, in_progress, ready, review

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards cards add ... ; oar boards cards add ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards cards update`

Update board card metadata

```text
Generated Help: boards cards update

- Command ID: `boards.cards.update`
- CLI path: `boards cards update`
- HTTP: `PATCH /boards/{board_id}/cards/{thread_id}`
- Stability: `beta`
- Input mode: `json-body`
- Why: Patch mutable board-card metadata, which in v1 is limited to the pinned document convenience link.
- Output: Returns `{ board, card }` after metadata update and board concurrency-token advancement.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`, `not_found`
- Concepts: `boards`, `planning`, `docs`, `concurrency`
- Agent notes: Set `if_board_updated_at` from the current board read before patching card metadata.
- Adjacent commands: `boards cards add`, `boards cards list`, `boards cards move`, `boards cards remove`, `boards create`, `boards get`, `boards list`, `boards update`, `boards workspace`
- Examples:
  - Update pinned document: `oar boards cards update --board-id board_product_launch --thread-id thread_123 --from-file board-card-update.json --json`

Body schema:
  Required: if_board_updated_at (datetime)
  Optional: actor_id (string), patch.pinned_document_id (any|string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards cards update ... ; oar boards cards update ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards cards move`

Move board card across columns or ranks

```text
Generated Help: boards cards move

- Command ID: `boards.cards.move`
- CLI path: `boards cards move`
- HTTP: `POST /boards/{board_id}/cards/{thread_id}/move`
- Stability: `beta`
- Input mode: `json-body`
- Why: Request relative placement for a card while keeping rank tokens opaque and server-owned.
- Output: Returns `{ board, card }` after the move is applied.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`, `not_found`
- Concepts: `boards`, `planning`, `ordering`, `concurrency`
- Agent notes: Provide at most one of `before_thread_id` or `after_thread_id`. If neither is set, the card moves to the end of the target column.
- Adjacent commands: `boards cards add`, `boards cards list`, `boards cards remove`, `boards cards update`, `boards create`, `boards get`, `boards list`, `boards update`, `boards workspace`
- Examples:
  - Move card into review: `oar boards cards move --board-id board_product_launch --thread-id thread_123 --from-file board-card-move.json --json`

Body schema:
  Required: column_key (string), if_board_updated_at (datetime)
  Optional: actor_id (string), after_thread_id (string), before_thread_id (string)
  Enum values: column_key: backlog, blocked, done, in_progress, ready, review

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards cards move ... ; oar boards cards move ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards cards remove`

Remove board card membership

```text
Generated Help: boards cards remove

- Command ID: `boards.cards.remove`
- CLI path: `boards cards remove`
- HTTP: `POST /boards/{board_id}/cards/{thread_id}/remove`
- Stability: `beta`
- Input mode: `json-body`
- Why: Delete canonical board membership for a card without introducing a separate archived-card lifecycle in v1.
- Output: Returns `{ board, removed_thread_id }` after membership removal and board concurrency-token advancement.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`, `not_found`
- Concepts: `boards`, `planning`, `concurrency`
- Agent notes: Removal deletes canonical membership. Cards are not archived separately in v1.
- Adjacent commands: `boards cards add`, `boards cards list`, `boards cards move`, `boards cards update`, `boards create`, `boards get`, `boards list`, `boards update`, `boards workspace`
- Examples:
  - Remove board card: `oar boards cards remove --board-id board_product_launch --thread-id thread_123 --from-file board-card-remove.json --json`

Body schema:
  Required: if_board_updated_at (datetime)
  Optional: actor_id (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json boards cards remove ... ; oar boards cards remove ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs list`

List documents and their current head metadata

```text
Generated Help: docs list

- Command ID: `docs.list`
- CLI path: `docs list`
- HTTP: `GET /docs`
- Stability: `beta`
- Input mode: `none`
- Why: Discover available documents without resolving each head individually, optionally scoped to a single thread.
- Output: Returns `{ documents, next_cursor? }` ordered by `updated_at` descending. Pagination is optional and backward-compatible.
- Error codes: `invalid_request`
- Concepts: `docs`, `revisions`
- Agent notes: Safe and idempotent. Use `thread_id` to focus on one thread's docs and `include_tombstoned=true` when auditing superseded documents. Optional pagination with `q` for search, `limit` for page size, and `cursor` for continuation.
- Adjacent commands: `docs create`, `docs get`, `docs history`, `docs revision get`, `docs tombstone`, `docs update`
- Examples:
  - List documents: `oar docs list --json`
  - Search documents by title: `oar docs list --q "constitution" --json`
  - Paginated document list: `oar docs list --limit 50 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs list ... ; oar docs list ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs create`

Create document with initial immutable revision

```text
Generated Help: docs create

- Command ID: `docs.create`
- CLI path: `docs create`
- HTTP: `POST /docs`
- Stability: `beta`
- Input mode: `json-body`
- Why: Bootstrap a first-class document identity and initial revision without manual head-pointer management.
- Output: Returns `{ document, revision }` where `revision` is the new head.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`
- Concepts: `docs`, `revisions`
- Agent notes: Replay-safe when `request_key` is reused with the same body; core can issue the canonical document id when one is omitted.
- Adjacent commands: `docs get`, `docs history`, `docs list`, `docs revision get`, `docs tombstone`, `docs update`
- Examples:
  - Create document: `oar docs create --from-file doc-create.json --json`

Body schema:
  Required: content (object|string), content_type (string), document (object)
  Optional: actor_id (string), refs (list<string>), request_key (string)
  Enum values: content_type: binary, structured, text

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs create ... ; oar docs create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs get`

Get document and authoritative head revision

```text
Generated Help: docs get

- Command ID: `docs.get`
- CLI path: `docs get`
- HTTP: `GET /docs/{document_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve the current authoritative document head without client-side lineage traversal.
- Output: Returns `{ document, revision }` where `revision` is the current head.
- Error codes: `invalid_request`, `not_found`
- Concepts: `docs`, `revisions`
- Agent notes: Safe and idempotent.
- Adjacent commands: `docs create`, `docs history`, `docs list`, `docs revision get`, `docs tombstone`, `docs update`
- Examples:
  - Get document head: `oar docs get --document-id product-constitution --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs get ... ; oar docs get ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs update`

Create a new immutable revision for an existing document

```text
Generated Help: docs update

- Command ID: `docs.update`
- CLI path: `docs update`
- HTTP: `PATCH /docs/{document_id}`
- Stability: `beta`
- Input mode: `json-body`
- Why: Append a revision and atomically advance document head with optimistic concurrency.
- Output: Returns `{ document, revision }` for the newly-created head revision.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`, `not_found`
- Concepts: `docs`, `revisions`, `concurrency`
- Agent notes: Set `if_base_revision` from `docs.get` to prevent lost updates.
- Adjacent commands: `docs create`, `docs get`, `docs history`, `docs list`, `docs revision get`, `docs tombstone`
- Examples:
  - Update document: `oar docs update --document-id product-constitution --from-file doc-update.json --json`

Body schema:
  Required: content (object|string), content_type (string), if_base_revision (string)
  Optional: actor_id (string), document (object), refs (list<string>)
  Enum values: content_type: binary, structured, text

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs update ... ; oar docs update ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs history`

List ordered immutable revisions for a document

```text
Generated Help: docs history

- Command ID: `docs.history`
- CLI path: `docs history`
- HTTP: `GET /docs/{document_id}/history`
- Stability: `beta`
- Input mode: `none`
- Why: Traverse full document lineage in canonical revision-number order.
- Output: Returns `{ document_id, revisions }` ordered by ascending `revision_number`.
- Error codes: `invalid_request`, `not_found`
- Concepts: `docs`, `revisions`, `lineage`
- Agent notes: Safe and idempotent.
- Adjacent commands: `docs create`, `docs get`, `docs list`, `docs revision get`, `docs tombstone`, `docs update`
- Examples:
  - List document history: `oar docs history --document-id product-constitution --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs history ... ; oar docs history ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs revision`

Nested generated help topic.

```text
Generated Help: docs revision

Commands:
  docs revision get        Get one immutable document revision

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs revision ... ; oar docs revision ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `docs tombstone`

Tombstone a document (soft-delete)

```text
Generated Help: docs tombstone

- Command ID: `docs.tombstone`
- CLI path: `docs tombstone`
- HTTP: `POST /docs/{document_id}/tombstone`
- Stability: `beta`
- Input mode: `json-body`
- Why: Mark a document as inactive while preserving revision history and provenance.
- Output: Returns `{ document, revision }` with updated tombstone metadata.
- Error codes: `invalid_json`, `invalid_request`, `not_found`
- Concepts: `docs`, `lifecycle`
- Agent notes: Idempotent; repeated tombstone calls on the same document are safe.
- Adjacent commands: `docs create`, `docs get`, `docs history`, `docs list`, `docs revision get`, `docs update`
- Examples:
  - Tombstone document: `oar docs tombstone --document-id product-constitution --reason "replaced by v2" --json`

Body schema:
  Required: actor_id (string)
  Optional: reason (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs tombstone ... ; oar docs tombstone ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs revision get`

Get one immutable document revision

```text
Generated Help: docs revision get

- Command ID: `docs.revision.get`
- CLI path: `docs revision get`
- HTTP: `GET /docs/{document_id}/revisions/{revision_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Read a specific historical revision payload without mutating document head.
- Output: Returns `{ revision }` including metadata and revision content.
- Error codes: `invalid_request`, `not_found`
- Concepts: `docs`, `revisions`
- Agent notes: Safe and idempotent.
- Adjacent commands: `docs create`, `docs get`, `docs history`, `docs list`, `docs tombstone`, `docs update`
- Examples:
  - Get revision: `oar docs revision get --document-id product-constitution --revision-id 019f... --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs revision get ... ; oar docs revision get ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events get`

Get event by id

```text
Generated Help: events get

- Command ID: `events.get`
- CLI path: `events get`
- HTTP: `GET /events/{event_id}`
- Stability: `stable`
- Input mode: `none`
- Why: Resolve event references and evidence links.
- Output: Returns `{ event }`.
- Error codes: `not_found`
- Concepts: `events`
- Agent notes: Safe and idempotent.
- Adjacent commands: `events create`, `events stream`
- Examples:
  - Get event: `oar events get --event-id event_123 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json events get ... ; oar events get ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events create`

Append event

```text
Generated Help: events create

- Command ID: `events.create`
- CLI path: `events create`
- HTTP: `POST /events`
- Stability: `stable`
- Input mode: `json-body`
- Why: Record append-only narrative or protocol state changes that complement snapshots.
- Output: Returns `{ event }` with generated id and timestamp.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Concepts: `events`, `append-only`
- Agent notes: Replay-safe when `request_key` is reused with the same body.
- Adjacent commands: `events get`, `events stream`
- Examples:
  - Append event: `oar events create --from-file event.json --json`

Body schema:
  Required: event.provenance.sources (list<string>), event.refs (list<typed_ref>), event.summary (string), event.type (string)
  Optional: actor_id (string), event.actor_id (string), event.payload (object), event.provenance.by_field (map<string, list<string>>), event.provenance.notes (string), event.thread_id (string), request_key (string)
  Enum values: event.type (open): agent_notification_dismissed, agent_notification_read, board_card_added, board_card_moved, board_card_removed, board_card_updated, board_created, board_updated, commitment_created, commitment_status_changed, decision_made, decision_needed, document_created, document_tombstoned, document_updated, exception_raised, inbox_item_acknowledged, message_posted, receipt_added, review_completed, snapshot_updated, work_order_claimed, work_order_created

Common authoring types:
  Communication: direct communication or important non-structured information
  - `message_posted`
  Decisions: request or record decisions on the thread
  - `decision_needed`
  - `decision_made`
  State and commitments: track state changes and commitments
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

## `events stream`

Stream events via Server-Sent Events (SSE)

```text
Generated Help: events stream

- Command ID: `events.stream`
- CLI path: `events stream`
- HTTP: `GET /events/stream`
- Stability: `beta`
- Input mode: `none`
- Why: Follow live event updates with resumable SSE semantics.
- Output: SSE stream where each event carries `{ event }` and uses event id for resume.
- Error codes: `internal_error`, `cli_outdated`
- Concepts: `events`, `streaming`
- Agent notes: Supports `Last-Event-ID` header or `last_event_id` query for resumable reads.
- Adjacent commands: `events create`, `events get`
- Examples:
  - Stream all events: `oar events tail --json`
  - Resume by id: `oar events tail --last-event-id <event_id> --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json events stream ... ; oar events stream ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events tail`

Stream events via Server-Sent Events (SSE)

```text
Generated Help: events tail

- Command ID: `events.stream`
- CLI path: `events stream`
- HTTP: `GET /events/stream`
- Stability: `beta`
- Input mode: `none`
- Why: Follow live event updates with resumable SSE semantics.
- Output: SSE stream where each event carries `{ event }` and uses event id for resume.
- Error codes: `internal_error`, `cli_outdated`
- Concepts: `events`, `streaming`
- Agent notes: Supports `Last-Event-ID` header or `last_event_id` query for resumable reads.
- Adjacent commands: `events create`, `events get`
- Examples:
  - Stream all events: `oar events tail --json`
  - Resume by id: `oar events tail --last-event-id <event_id> --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json events tail ... ; oar events tail ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox list`

List derived inbox items

```text
Generated Help: inbox list

- Command ID: `inbox.list`
- CLI path: `inbox list`
- HTTP: `GET /inbox`
- Stability: `stable`
- Input mode: `none`
- Why: Surface derived actionable risk and decision signals.
- Output: Returns `{ items, generated_at }`.
- Concepts: `inbox`, `derived-views`
- Agent notes: Derived inbox view; do not build durable automation directly on projection payload shapes. Prefer canonical events and threads for durable substrate. Safe and idempotent.
- Adjacent commands: `inbox ack`, `inbox get`, `inbox stream`
- Examples:
  - List inbox: `oar inbox list --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json inbox list ... ; oar inbox list ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox get`

Get derived inbox item detail

```text
Generated Help: inbox get

- Command ID: `inbox.get`
- CLI path: `inbox get`
- HTTP: `GET /inbox/{inbox_item_id}`
- Stability: `stable`
- Input mode: `none`
- Why: Inspect one inbox item in detail before acting on it.
- Output: Returns `{ item, generated_at }` for the requested inbox item.
- Error codes: `not_found`
- Concepts: `inbox`, `derived-views`
- Agent notes: Derived inbox view; do not build durable automation directly on projection payload shapes. Prefer canonical events and threads for durable substrate. CLI supports canonical ids, aliases, and unique prefixes.
- Adjacent commands: `inbox ack`, `inbox list`, `inbox stream`
- Examples:
  - Get inbox item by canonical id: `oar inbox get --id inbox:decision_needed:thread_123:none:event_123 --json`
  - Get inbox item by alias: `oar inbox get --id ibx_abcd1234ef56 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json inbox get ... ; oar inbox get ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox ack`

Acknowledge an inbox item

```text
Generated Help: inbox ack

- Command ID: `inbox.ack`
- CLI path: `inbox ack`
- HTTP: `POST /inbox/ack`
- Stability: `stable`
- Input mode: `json-body`
- Why: Suppress already-acted-on derived inbox signals.
- Output: Returns `{ event }` representing acknowledgment.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Concepts: `inbox`, `events`
- Agent notes: Derived inbox view; do not build durable automation directly on projection payload shapes. Prefer canonical events and threads for durable substrate. Idempotent at semantic level; repeated acks should not duplicate active inbox items.
- Adjacent commands: `inbox get`, `inbox list`, `inbox stream`
- Examples:
  - Ack inbox item: `oar inbox ack --thread-id thread_123 --inbox-item-id inbox:item-1 --json`
  - Ack inbox item by id: `oar inbox ack inbox:decision_needed:thread_123:none:event_1 --json`

Body schema:
  Required: inbox_item_id (string), thread_id (string)
  Optional: actor_id (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json inbox ack ... ; oar inbox ack ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox stream`

Stream derived inbox items via SSE

```text
Generated Help: inbox stream

- Command ID: `inbox.stream`
- CLI path: `inbox stream`
- HTTP: `GET /inbox/stream`
- Stability: `beta`
- Input mode: `none`
- Why: Follow live derived inbox updates without repeated polling.
- Output: SSE stream where each event carries `{ item }` derived inbox metadata.
- Error codes: `internal_error`, `cli_outdated`
- Concepts: `inbox`, `derived-views`, `streaming`
- Agent notes: Derived inbox view; do not build durable automation directly on projection payload shapes. Prefer canonical events and threads for durable substrate. Supports `Last-Event-ID` header or `last_event_id` query for resumable reads.
- Adjacent commands: `inbox ack`, `inbox get`, `inbox list`
- Examples:
  - Stream inbox updates: `oar inbox tail --json`
  - Resume inbox stream: `oar inbox tail --last-event-id <id> --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json inbox stream ... ; oar inbox stream ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox tail`

Stream derived inbox items via SSE

```text
Generated Help: inbox tail

- Command ID: `inbox.stream`
- CLI path: `inbox stream`
- HTTP: `GET /inbox/stream`
- Stability: `beta`
- Input mode: `none`
- Why: Follow live derived inbox updates without repeated polling.
- Output: SSE stream where each event carries `{ item }` derived inbox metadata.
- Error codes: `internal_error`, `cli_outdated`
- Concepts: `inbox`, `derived-views`, `streaming`
- Agent notes: Derived inbox view; do not build durable automation directly on projection payload shapes. Prefer canonical events and threads for durable substrate. Supports `Last-Event-ID` header or `last_event_id` query for resumable reads.
- Adjacent commands: `inbox ack`, `inbox get`, `inbox list`
- Examples:
  - Stream inbox updates: `oar inbox tail --json`
  - Resume inbox stream: `oar inbox tail --last-event-id <id> --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json inbox tail ... ; oar inbox tail ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `derived rebuild`

Rebuild derived views

```text
Generated Help: derived rebuild

- Command ID: `derived.rebuild`
- CLI path: `derived rebuild`
- HTTP: `POST /derived/rebuild`
- Stability: `beta`
- Input mode: `json-body`
- Why: Force deterministic recomputation of derived views after maintenance or migration.
- Output: Returns `{ ok: true }`.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Concepts: `derived-views`, `maintenance`
- Agent notes: Mutating admin command; serialize with other writes.
- Examples:
  - Rebuild derived: `oar derived rebuild --actor-id system --json`

Body schema:
  Required: none
  Optional: actor_id (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json derived rebuild ... ; oar derived rebuild ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta commands`

List generated command metadata

```text
Generated Help: meta commands

- Command ID: `meta.commands.list`
- CLI path: `meta commands`
- HTTP: `GET /meta/commands`
- Stability: `beta`
- Input mode: `none`
- Why: Load generated command metadata used for help, docs, and agent introspection.
- Output: Returns generated command registry metadata from the canonical contract.
- Error codes: `meta_unavailable`, `cli_outdated`
- Concepts: `meta`, `introspection`
- Agent notes: Safe and idempotent. Response shape matches committed generated artifacts.
- Adjacent commands: `meta command`, `meta concept`, `meta concepts`, `meta handshake`, `meta health`, `meta livez`, `meta ops health`, `meta readyz`, `meta version`
- Examples:
  - List command metadata: `oar meta commands --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json meta commands ... ; oar meta commands ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta command`

Get generated metadata for a command id

```text
Generated Help: meta command

- Command ID: `meta.commands.get`
- CLI path: `meta command`
- HTTP: `GET /meta/commands/{command_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve a stable command id to full generated metadata and guidance.
- Output: Returns `{ command }` metadata for the requested command id.
- Error codes: `not_found`, `meta_unavailable`, `cli_outdated`
- Concepts: `meta`, `introspection`
- Agent notes: Safe and idempotent.
- Adjacent commands: `meta commands`, `meta concept`, `meta concepts`, `meta handshake`, `meta health`, `meta livez`, `meta ops health`, `meta readyz`, `meta version`
- Examples:
  - Read command metadata: `oar meta command --command-id threads.list --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json meta command ... ; oar meta command ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta concepts`

List generated concept metadata

```text
Generated Help: meta concepts

- Command ID: `meta.concepts.list`
- CLI path: `meta concepts`
- HTTP: `GET /meta/concepts`
- Stability: `beta`
- Input mode: `none`
- Why: Discover conceptual groupings of commands generated from contract metadata.
- Output: Returns `{ concepts }` summary metadata for all known concepts.
- Error codes: `meta_unavailable`, `cli_outdated`
- Concepts: `meta`, `concepts`
- Agent notes: Safe and idempotent.
- Adjacent commands: `meta command`, `meta commands`, `meta concept`, `meta handshake`, `meta health`, `meta livez`, `meta ops health`, `meta readyz`, `meta version`
- Examples:
  - List concepts: `oar meta concepts --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json meta concepts ... ; oar meta concepts ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta concept`

Get generated metadata for one concept

```text
Generated Help: meta concept

- Command ID: `meta.concepts.get`
- CLI path: `meta concept`
- HTTP: `GET /meta/concepts/{concept_name}`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve one concept tag to the commands that implement that concept.
- Output: Returns `{ concept }` including matched command ids and command metadata.
- Error codes: `not_found`, `meta_unavailable`, `cli_outdated`
- Concepts: `meta`, `concepts`
- Agent notes: Safe and idempotent.
- Adjacent commands: `meta command`, `meta commands`, `meta concepts`, `meta handshake`, `meta health`, `meta livez`, `meta ops health`, `meta readyz`, `meta version`
- Examples:
  - Read one concept: `oar meta concept --concept-name compatibility --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json meta concept ... ; oar meta concept ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `work-orders create`

Create work-order packet artifact

```text
Generated Help: work-orders create

- Command ID: `packets.work-orders.create`
- CLI path: `work-orders create`
- HTTP: `POST /work_orders`
- Stability: `stable`
- Input mode: `json-body`
- Why: Create structured action packets with deterministic schema enforcement.
- Output: Returns `{ artifact, event }`.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Concepts: `packets`, `work-orders`
- Agent notes: Replay-safe when `request_key` is reused with the same body; packet id fields may be omitted and core will issue the canonical artifact id.
- Adjacent commands: `receipts create`, `reviews create`
- Examples:
  - Create work order: `oar work-orders create --from-file work-order.json --json`

Body schema:
  Required: artifact (object), packet.acceptance_criteria (list<string>), packet.constraints (list<string>), packet.context_refs (list<typed_ref>), packet.definition_of_done (list<string>), packet.objective (string), packet.thread_id (string), packet.work_order_id (string)
  Optional: actor_id (string), request_key (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json work-orders create ... ; oar work-orders create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `receipts create`

Create receipt packet artifact

```text
Generated Help: receipts create

- Command ID: `packets.receipts.create`
- CLI path: `receipts create`
- HTTP: `POST /receipts`
- Stability: `stable`
- Input mode: `json-body`
- Why: Record execution output and verification evidence for a work order.
- Output: Returns `{ artifact, event }`.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Concepts: `packets`, `receipts`
- Agent notes: Replay-safe when `request_key` is reused with the same body. Include evidence refs that satisfy packet conventions.
- Adjacent commands: `reviews create`, `work-orders create`
- Examples:
  - Create receipt: `oar receipts create --from-file receipt.json --json`

Body schema:
  Required: artifact (object), packet.changes_summary (string), packet.known_gaps (list<string>), packet.outputs (list<typed_ref>), packet.receipt_id (string), packet.thread_id (string), packet.verification_evidence (list<typed_ref>), packet.work_order_id (string)
  Optional: actor_id (string), request_key (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json receipts create ... ; oar receipts create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `reviews create`

Create review packet artifact

```text
Generated Help: reviews create

- Command ID: `packets.reviews.create`
- CLI path: `reviews create`
- HTTP: `POST /reviews`
- Stability: `stable`
- Input mode: `json-body`
- Why: Record acceptance/revision decisions over a receipt.
- Output: Returns `{ artifact, event }`.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Concepts: `packets`, `reviews`
- Agent notes: Include refs to both receipt and work order artifacts.
- Adjacent commands: `receipts create`, `work-orders create`
- Examples:
  - Create review: `oar reviews create --from-file review.json --json`

Body schema:
  Required: artifact (object), packet.evidence_refs (list<typed_ref>), packet.notes (string), packet.outcome (string), packet.receipt_id (string), packet.review_id (string), packet.work_order_id (string)
  Optional: actor_id (string), request_key (string)
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

Canonical thread coordination read path: compose one view from `threads context` and related `inbox list` items.

```text
Local Help: threads inspect

- Kind: `local helper`
- Summary: Canonical thread coordination read path: compose one view from `threads context` and related `inbox list` items.
- Composition: Resolves one thread by id or discovery filters, loads `threads context`, then filters inbox items client-side by `thread_id` for one operator-focused coordination view.
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
  --include-artifact-content   Include artifact content previews from `threads context`.
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
- Composition: Resolves one thread by id or discovery filters, loads `threads context`, adds thread-scoped inbox items from `inbox list`, and follows related thread refs for additional review context.
- JSON body: `thread`, `context`, `collaboration`, `inbox`, `pending_decisions`, `related_threads`, `related_recommendations`, `related_decisions`, `follow_up`
- Examples:
  - `oar threads workspace --thread-id <thread-id> --full-id`
  - `oar threads workspace --thread-id <thread-id> --include-related-event-content --verbose`
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
  --include-artifact-content   Include artifact content previews from `threads context`.
  --include-related-event-content Hydrate related review items with full `events get` content in one command.
  --full-summary               Show full recommendation/decision summaries in human output.
  --full-id                    Render full event and inbox ids in human output.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads workspace ... ; oar threads workspace ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads review`

Opinionated deep-read helper: run the holistic workspace view with related-event hydration and full summaries enabled by default.

```text
Local Help: threads review

- Kind: `local helper`
- Summary: Opinionated deep-read helper: run the holistic workspace view with related-event hydration and full summaries enabled by default.
- Composition: Uses the same aggregate view as `threads workspace`, but defaults to a review-oriented read by hydrating related review items with `events get` content and expanding recommendation summaries in one command.
- JSON body: `thread`, `context`, `collaboration`, `inbox`, `pending_decisions`, `related_threads`, `related_recommendations`, `related_decisions`, `follow_up`
- Examples:
  - `oar threads review --thread-id <thread-id>`
  - `oar threads review --thread-id <thread-id> --full-id`
  - `oar threads review --status active --type initiative`

Flags:
  --thread-id <thread-id>      Thread id to review.
  --status <status>            Discover one thread by status.
  --priority <priority>        Discover one thread by priority.
  --stale <bool>               Discover one thread by stale state.
  --tag <tag>                  Repeatable discovery tag filter.
  --cadence <cadence>          Repeatable discovery cadence filter.
  --type <thread-type>         Local discovery filter after `threads list`.
  --max-events <n>             Maximum recent context events to include.
  --include-artifact-content   Include artifact content previews from `threads context`.
  --full-id                    Render full event and inbox ids in human output.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads review ... ; oar threads review ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads recommendations`

Review one thread's recommendation/decision inputs plus related-thread signals with provenance and follow-up hints.

```text
Local Help: threads recommendations

- Kind: `local helper`
- Summary: Review one thread's recommendation/decision inputs plus related-thread signals with provenance and follow-up hints.
- Composition: Resolves one thread by id or discovery filters, loads `threads context`, adds thread-scoped pending decision inbox items from `inbox list`, and follows related thread refs for additional review context.
- JSON body: `thread`, `recommendations`, `decision_requests`, `decisions`, `pending_decisions`, `related_threads`, `related_recommendations`, `follow_up`
- Examples:
  - `oar threads recommendations --thread-id <thread-id> --full-id`
  - `oar threads recommendations --thread-id <thread-id> --include-related-event-content --verbose`
  - `oar threads recommendations --status active --type initiative --full-summary`

Flags:
  --thread-id <thread-id>      Thread id to review.
  --status <status>            Discover one thread by status.
  --priority <priority>        Discover one thread by priority.
  --stale <bool>               Discover one thread by stale state.
  --tag <tag>                  Repeatable discovery tag filter.
  --cadence <cadence>          Repeatable discovery cadence filter.
  --type <thread-type>         Local discovery filter after `threads list`.
  --max-events <n>             Maximum recent context events to include.
  --include-artifact-content   Include artifact content previews from `threads context`.
  --include-related-event-content Hydrate related review items with full `events get` content in one command.
  --full-summary               Show full recommendation/decision summaries in human output.
  --full-id                    Render full event and inbox ids in human output.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads recommendations ... ; oar threads recommendations ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads propose-patch`

Stage a thread patch proposal locally and show the diff before applying it.

```text
Local Help: threads propose-patch

- Kind: `local helper`
- Summary: Stage a thread patch proposal locally and show the diff before applying it.
- Composition: Resolves the thread id, fetches current state with `threads get`, computes a local diff, and persists a proposal file instead of sending the patch immediately.
- JSON body: `proposal_id`, `target_command_id`, `path`, `body`, `diff`, `apply_command`
- Examples:
  - `oar threads propose-patch --thread-id <thread-id> --from-file patch.json`
  - `cat patch.json | oar threads propose-patch --thread-id <thread-id>`

Flags:
  --thread-id <thread-id>      Thread id to patch.
  --from-file <path>           Load the patch body from a JSON file.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads propose-patch ... ; oar threads propose-patch ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads apply`

Apply a previously staged thread patch proposal.

```text
Local Help: threads apply

- Kind: `local helper`
- Summary: Apply a previously staged thread patch proposal.
- Composition: Loads the local proposal by exact id or unique prefix, validates it again, then sends the underlying `threads.patch` request.
- JSON body: `proposal_id`, `target_command_id`, `applied`, `kept`, `result`
- Examples:
  - `oar threads apply --proposal-id <proposal-id>`
  - `oar threads apply <proposal-id-prefix>`

Flags:
  --proposal-id <proposal-id>  Proposal id or unique prefix to apply.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads apply ... ; oar threads apply ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `commitments propose-patch`

Stage a commitment patch proposal locally and show the diff before applying it.

```text
Local Help: commitments propose-patch

- Kind: `local helper`
- Summary: Stage a commitment patch proposal locally and show the diff before applying it.
- Composition: Resolves the commitment id, fetches current state with `commitments get`, computes a local diff, and persists a proposal file instead of sending the patch immediately.
- JSON body: `proposal_id`, `target_command_id`, `path`, `body`, `diff`, `apply_command`
- Examples:
  - `oar commitments propose-patch --commitment-id <commitment-id> --from-file patch.json`

Flags:
  --commitment-id <commitment-id> Commitment id to patch.
  --from-file <path>           Load the patch body from a JSON file.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json commitments propose-patch ... ; oar commitments propose-patch ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `commitments apply`

Apply a previously staged commitment update proposal.

```text
Local Help: commitments apply

- Kind: `local helper`
- Summary: Apply a previously staged commitment update proposal.
- Composition: Loads the local proposal by exact id or unique prefix, validates it again, then sends the underlying `commitments.patch` request.
- JSON body: `proposal_id`, `target_command_id`, `applied`, `kept`, `result`
- Examples:
  - `oar commitments apply --proposal-id <proposal-id>`

Flags:
  --proposal-id <proposal-id>  Proposal id or unique prefix to apply.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json commitments apply ... ; oar commitments apply ... --json
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

Validate a `docs update` payload locally from stdin or file without sending the mutation.

```text
Local Help: docs validate-update

- Kind: `local helper`
- Summary: Validate a `docs update` payload locally from stdin or file without sending the mutation.
- Composition: Parses the same body accepted by `docs update`, expands `--content-file` when present, and returns a validation preview envelope without contacting core.
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
- Composition: Loads the local proposal by exact id or unique prefix, validates it again, then sends the underlying `docs.update` request.
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
- Composition: Pure local helper. Reads an existing `oar` profile plus Ed25519 key material, converts it into bridge auth state, and writes it to the bridge config's `[auth].state_path`.
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
  - `oar bridge init-config --kind hermes --output ./agent.toml --workspace-id ws_main --handle hermes`
  - `oar bridge init-config --kind zeroclaw --output ./zeroclaw.toml --workspace-id ws_main --handle zeroclaw`

Flags:
  --kind <hermes|zeroclaw>     Template kind to render.
  --output <path>              Write the rendered TOML to a file. Omit to print it.
  --workspace-id <id>          Durable OAR workspace id. Do not use a slug or UI path segment.
  --handle <name>              Agent handle for bridge templates.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json bridge init-config ... ; oar bridge init-config ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge workspace-id`

Discover durable workspace ids from an existing agent registration document.

```text
Local Help: bridge workspace-id

- Kind: `local helper`
- Summary: Discover durable workspace ids from an existing agent registration document.
- Composition: Uses the active `oar` auth/profile to read `agentreg.<handle>` and extract enabled workspace bindings so bridge bootstrap can reuse the real durable workspace id instead of guessing.
- JSON body: `document_id`, `handle`, `actor_id`, `registration_status`, `workspace_ids`, `workspace_bindings`
- Examples:
  - `oar --agent agent-a bridge workspace-id --handle hermes`
  - `oar bridge workspace-id --document-id agentreg.hermes`

Flags:
  --handle <name>              Agent handle whose `agentreg.<handle>` document should be inspected.
  --document-id <id>           Registration document id to inspect directly. Defaults to `agentreg.<handle>`.


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

Write payload previews for a plan and optionally execute thread/artifact/doc creates in dependency order.

```text
Local Help: import apply

- Kind: `local helper`
- Summary: Write payload previews for a plan and optionally execute thread/artifact/doc creates in dependency order.
- Composition: Local helper with optional network writes. Always writes payload previews first; when `--execute` is set it creates threads, then artifacts, then docs, substituting `$REF:<key>` placeholders after upstream IDs are known.
- JSON body: `plan`, `execute`, `results`, `refs`
- Examples:
  - `oar import apply --plan ./.oar-import/workspace/plan.json`
  - `oar import apply --plan ./.oar-import/workspace/plan.json --execute --agent importer`

Flags:
  --plan <path>                Plan produced by `oar import plan`. Positional form also supported.
  --out <dir>                  Output directory for payload previews and apply results. Defaults to `<plan-dir>/apply`.
  --execute                    Actually call `threads create`, `artifacts create`, and `docs create`. Default is preview-only.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json import apply ... ; oar import apply ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```
