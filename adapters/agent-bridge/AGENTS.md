# AGENTS

## Scope
Guide for work inside `adapters/agent-bridge/`.

Read this after the root `AGENTS.md`. This adapter owns the local runtime that turns `@handle` mentions into bridge wakeups.

## Module Purpose
`anx-agent-bridge` is the integration-side runtime for bridge-managed agent wake handling.

It owns:
- principal registration metadata writes for bridge-managed `@handle` routing
- bridge-side check-in, wake claim, adapter dispatch, and reply writeback
- local install and test ergonomics for the Python package

It does not own workspace routing. `@handle` mention routing lives in the workspace router deployed with `anx-core`.

It does not own canonical Agent Nexus state. The durable truth still lives in Agent Nexus primitives.

## High-Value Invariants
- Durable taggability is driven by a valid registration plus workspace binding, not bridge uptime.
- Bridge-managed registrations stay `pending` until the bridge has checked in.
- Routing must treat stale or missing bridge check-ins as offline for immediate delivery, while leaving durable notifications queueable.
- Workspace binding must use the durable `workspace_id`, never a slug or UI path segment.
- Keep the runtime working with only documented Agent Nexus primitives: auth principals, events, and artifacts.

## Local Workflow
- Python `3.11+` is required. The repo-local convention is `.python-version = 3.11`.
- Prefer the adapter-local make targets:
  - `make setup`
  - `make doctor`
  - `make test`
  - `make smoke`
- The default venv is `adapters/agent-bridge/.venv`.

## Validation
- `make doctor`
- `make test`
- If you touch CLI/docs/bootstrap behavior too, also run:
  - `make cli-check`
  - relevant `web-ui` tests when wakeability summaries change

## Adapters
- **`subprocess`** is the supported production path: any executable receiving JSON on stdin and printing JSON on stdout (`ANX_BRIDGE_MODE=dispatch|doctor`).
- **`python_plugin`** loads an explicit `plugin_module` + `plugin_factory` for in-process adapters.
- **`deterministic_ack`** is **dev-forward** only: fixed rotation of short replies. Use for local wake QA; do not imply production use.

## Editing Guidance
- Keep install/setup discoverable for two audiences:
  - repo contributors working from this checkout
  - agents/operators who only have the `anx` CLI and use `anx bridge ...`
- Update `README.md`, CLI help topics, and examples together when the lifecycle or setup path changes.
- If you add readiness metadata, keep the bridge-facing semantics aligned with the workspace router and the human-facing Access UI.
