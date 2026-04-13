---
name: oar-cli-onboard
description: >-
  Use the `oar` CLI effectively: configure base URL/auth/profile, discover the available command surface, choose the right primitive or higher-level abstraction, and choose text (default, LLM-friendly) or `--json` (programmatic) output as appropriate. Apply when running `oar`, interpreting its help/errors, or automating OAR workflows.
---

# OAR CLI guide for agents

Use this guide when you need to operate `oar` well, not just get it running. Favor stable CLI patterns over environment-specific setup.

## Operating posture

- Treat `oar` as the contract-aligned interface to an OAR core API.
- Prefer read-before-write: inspect state, choose the right object, then mutate deliberately.
- Prefer **default (non-JSON) output** for normal agent work: concise text for direct consumption, usually fewer tokens than JSON envelopes.
- Use **`--json`** or **`OAR_JSON=true`** when the consumer is code, a shell script, CI, or anything that parses the stable JSON envelope (including rich `error.details`).
- Prefer profiles and env vars over repeated flags.
- Prefer discovery from the CLI itself over memorizing exact subcommands.

## Core model

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

- Local **`make serve`**: bootstrap is usually **already consumed** by the seeded human; register the CLI with a **single-use invite** from `cli/dogfood-resources/invites.generated.json` (regenerated each serve) or from `oar auth invites create --kind agent` on an existing principal. See `cli/docs/runbook.md` (fixture seed) and `cli/README.md` (`oar secret` quirks).
- On a durable workstation, set the active profile once with `oar config use <profile>` (equivalent to `oar auth default <profile>`). Later commands can omit repeated `--base-url` / `--agent`; inspect merged settings with `oar config show` (tokens redacted).
- Override per command with `--base-url` or `OAR_BASE_URL` and `--agent` or `OAR_AGENT` when needed.
- Prefer `OAR_BASE_URL` and `OAR_AGENT` in scripts, CI, or environments without a persistent `~/.config/oar`.
- If available, run `oar doctor` when config or connectivity is unclear.
- If a request behaves like it hit the wrong service, confirm you are pointing at the core API, not another surface.

Config precedence is typically: flags -> environment -> profile -> defaults.

## Discovery first

Do not overfit to examples in this guide. Ask the CLI what exists now:

  oar help
  oar help <group>
  oar help <group> <command>
  oar meta docs
  oar meta doc <topic>

Use help output as the source of truth for exact flags, request shapes, enums, and newly added primitives.

## Command habits

- Use list/get/context/workspace commands to orient before editing.
- Use `--full-id` when an ID will be reused in later commands.
- Use streaming commands for live observation; bound them with `--max-events` when scripting.
- Use `draft` or proposal/apply flows when the CLI exposes them and the change benefits from reviewability.
- Prefer narrow filters over broad listings when triaging large state.

## Programmatic output (`--json`)

- Use `--json` or `OAR_JSON=true` when you are parsing output in code or scripts (not for default agent readbacks).
- Parse the response envelope; do not assume the same shape for default text output.
- Treat `error.code`, `error.message`, `hint`, and `recoverable` as the control surface for retries and repair.
- Keep scripts idempotent where possible: read state, compare, then write only when needed.

## Onboarding and recovery

When starting in a new environment:

1. Set base URL.
2. Register or select an agent/profile if required.
3. Confirm identity.
4. Run a cheap read command.

When stuck:

- Re-run with `--json` when structured failure fields (`error.details`, etc.) would help.
- Check help for the exact command path you are using.
- Verify auth, base URL, and profile resolution before debugging payload shape.

## Maintenance rule

- Keep this guide focused on durable usage patterns.
- Describe roles and decision rules, not exhaustive command inventories.
- Prefer `oar help` and `oar meta docs` over embedding fragile schemas.
- Mention examples of primitives and abstractions, but avoid implying the list is closed.
