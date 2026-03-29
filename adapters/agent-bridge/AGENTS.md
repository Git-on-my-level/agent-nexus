# AGENTS

## Scope
Guide for work inside `adapters/agent-bridge/`.

Read this after the root `AGENTS.md`. This adapter owns the local runtime that turns `@handle` mentions into bridge wakeups.

## Module Purpose
`oar-agent-bridge` is the integration-side runtime for durable wake routing.

It owns:
- registration document writes for `agentreg.<handle>`
- router-side mention resolution and wake packet creation
- bridge-side check-in, wake claim, adapter dispatch, and reply writeback
- local install and test ergonomics for the Python package

It does not own canonical OAR state. The durable truth still lives in OAR primitives.

## High-Value Invariants
- A registration document alone must not make an agent taggable.
- Bridge-managed registrations stay `pending` until the bridge has checked in.
- Routing must treat stale or missing bridge check-ins as not wakeable.
- Workspace binding must use the durable `workspace_id`, never a slug or UI path segment.
- Keep the runtime working with only documented OAR primitives: docs, events, artifacts, auth principals.

## Local Workflow
- Python `3.11+` is required. The repo-local convention is `.python-version = 3.11`.
- Prefer the adapter-local make targets:
  - `make setup`
  - `make doctor`
  - `make test`
  - `make smoke`
- The default venv is `adapters/agent-bridge/.venv`.

## Validation
- `make bridge-doctor`
- `make bridge-test`
- If you touch CLI/docs/bootstrap behavior too, also run:
  - `make cli-check`
  - relevant `web-ui` tests when wakeability summaries change

## Editing Guidance
- Keep install/setup discoverable for two audiences:
  - repo contributors working from this checkout
  - agents/operators who only have the `oar` CLI and use `oar bridge ...`
- Update `README.md`, CLI help topics, and examples together when the lifecycle or setup path changes.
- If you add readiness metadata, mirror the same semantics in the router and the human-facing Access UI.
