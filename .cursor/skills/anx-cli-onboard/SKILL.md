---
name: anx-cli-onboard
description: >-
  Use the `anx` CLI effectively: configure the workspace and enrolled host, select a derived agent, discover the available command surface, choose the right primitive or higher-level abstraction, and choose text (default, LLM-friendly) or `--json` (programmatic) output as appropriate. Apply when running `anx`, interpreting its help/errors, or automating Agent Nexus workflows.
---

# Agent Nexus CLI guide for agents

Use this guide when you need to operate `anx` well, not just get it running. Favor stable CLI patterns over environment-specific setup.

## Operating posture

- Treat `anx` as the contract-aligned interface to an **anx-core** (Agent Nexus) API.
- Prefer read-before-write: inspect state, choose the right object, then mutate deliberately.
- Prefer **default (non-JSON) output** for normal agent work: concise text for direct consumption, usually fewer tokens than JSON envelopes.
- Use **`--json`** or **`ANX_JSON=true`** when the consumer is code, a shell script, CI, or anything that parses the stable JSON envelope (including rich `error.details`).
- Enroll each machine once with `anx host enroll`; select a derived agent with `--as <name>` or `ANX_AS`.
- Prefer discovery from the CLI itself over memorizing exact subcommands.

## Core model

- `events`: immutable facts, observations, and updates. Use for append-only activity, audit trails, and streams.
- `threads`: durable work objects and coordination state. Use for initiatives, incidents, cases, processes, relationships, and similar work units.
- `inbox`: work intake and notifications. Use to see what needs attention and ack handled items.
- `draft`: staged or reviewable mutations. Use when a write should be inspected before commit.
- `docs`: long-lived narrative knowledge. Use for plans, notes, decisions, summaries, and shared context.
- `boards`: structured coordination views. Use to group and review work across multiple objects.
- `auth` and `host`: identity, host enrollment, and reusable workspace config.
- `meta` and help: runtime discovery for commands, concepts, and bundled docs.

Heuristic:
- Use `events` for facts.
- Use `threads` for ongoing work and ownership.
- Use `docs` for narrative or reference material.
- Use `boards` for portfolio or workflow visibility.
- Use `draft` when you want a checkpoint before applying change.

If a new primitive or abstraction is added, place it in the same model: what durable role it plays, what it organizes, and whether it is mainly for facts, work, knowledge, or views.

## Higher-level concepts

- `docs` are the long-lived narrative layer. Use them when information should be read as a document, revised over time, or referenced by many work items.
- `boards` are coordination views. Use them to group, prioritize, and review work across multiple objects rather than to store source-of-truth content themselves.
- `threads` often back execution; `docs` explain; `boards` organize. Keep those roles distinct.

## Standard workflow

1. Confirm environment and identity.
2. Discover current state with list/get/context commands.
3. Decide which primitive matches the task.
4. Make the smallest valid mutation.
5. Verify via read commands, timeline, stream, or resulting state.

For interrupt-driven work, a common loop is: `inbox` -> inspect related `thread` or `doc` -> apply change directly or via `draft` -> verify -> ack inbox item.

## Configuration

- Local **`make serve`**: the seeded operator consumes bootstrap. Enroll a host using the local `anx host enroll --token <token>` setup flow documented by the dev server, then select a derived agent with `--as <name>` or `ANX_AS`.
- Host identity and short-lived derived-agent tokens live under `~/.config/anx/hosts/`. Use `--config-dir` or `ANX_CONFIG_DIR` for callbacks and isolated environments.
- Override the core URL per command with `--base-url` or `ANX_BASE_URL`.
- If available, run `anx doctor` when config or connectivity is unclear.
- If a request behaves like it hit the wrong service, confirm you are pointing at the core API, not another surface. Do not put a workspace browser path (for example `/o/.../w/...`) in `--base-url` or `ANX_BASE_URL` when the host serves the web UI separately: use the anx-core API origin (often the host that returns JSON from `GET /readyz`).

Config precedence is typically: flags -> environment -> defaults; host identity is resolved from the selected config directory.

## Discovery first

Do not overfit to examples in this guide. Ask the CLI what exists now:

  anx help
  anx help <group>
  anx help <group> <command>
  anx meta docs
  anx meta doc <topic>

Use help output as the source of truth for exact flags, request shapes, enums, and newly added primitives.

## Command habits

- Use list/get/context/workspace commands to orient before editing.
- Default output leads with **public typed refs** (`topic:<handle>`, `board:<handle>`, `card:<handle>`) and bare handles. The CLI resolves handles and typed refs to canonical resources on input. Use `--full-id` (debug/admin only) when you need the internal UUID for debugging.
- Use streaming commands for live observation; bound them with `--max-events` when scripting.
- Use `draft` or proposal/apply flows when the CLI exposes them and the change benefits from reviewability.
- Prefer narrow filters over broad listings when triaging large state.

## Programmatic output (`--json`)

- Use `--json` or `ANX_JSON=true` when you are parsing output in code or scripts (not for default agent readbacks).
- Parse the response envelope; do not assume the same shape for default text output.
- Treat `error.code`, `error.message`, `hint`, and `recoverable` as the control surface for retries and repair.
- Keep scripts idempotent where possible: read state, compare, then write only when needed.

## Onboarding and recovery

When starting in a new environment:

1. Set base URL.
2. Enroll the host once, then select a derived agent with `--as` / `ANX_AS` (or let agentctl/harness detection select it).
3. Confirm identity.
4. Run a cheap read command.

When stuck:

- Re-run with `--json` when structured failure fields (`error.details`, etc.) would help.
- Check help for the exact command path you are using.
- Verify host enrollment, selected agent, and base URL before debugging payload shape.

## Maintenance rule

- Keep this guide focused on durable usage patterns.
- Describe roles and decision rules, not exhaustive command inventories.
- Prefer `anx help` and `anx meta docs` over embedding fragile schemas.
- Mention examples of primitives and abstractions, but avoid implying the list is closed.
