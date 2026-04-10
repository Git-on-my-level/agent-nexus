# organization-autorunner-core

Go-first bootstrap for the Organization Autorunner core backend.

This repo currently includes:
- `docs/`: spec + HTTP contract
- `../contracts/oar-schema.yaml`: shared schema
- `cmd/oar-core`: HTTP server (`/health`, `/livez`, `/readyz`, `/ops/health`, `/version`) with SQLite+filesystem workspace init
- `cmd/oar-control-plane`: SaaS control-plane HTTP server for accounts, organizations, workspace registry, jobs, invites, and audit state
- `scripts/dev`, `scripts/lint`, `scripts/test`: local workflows

## Quickstart

```bash
./scripts/test
./scripts/dev
./scripts/dev-control-plane
```

## Workspace Layout

By default, the server initializes storage under `.oar-workspace/`:
- `state.sqlite`: SQLite database (events, topics, cards, boards, documents, artifacts metadata, actors, backing threads, derived views)
- `artifacts/content/`: immutable artifact content files
- `logs/` and `tmp/`: reserved operational directories
- `router/`: local sidecar state for the embedded wake router

Config can be passed via flags or env vars:
- `--workspace-root` / `OAR_WORKSPACE_ROOT`
- `--host` + `--port` / `OAR_HOST` + `OAR_PORT`
- `--listen-addr` / `OAR_LISTEN_ADDR` (overrides host+port)

## Workspace Router

`oar-router` is the embedded workspace wake-routing sidecar hosted by
`oar-core`. It tails workspace `message_posted` events, resolves `@handle`
mentions, verifies durable registration + workspace binding, and emits
`agent_wakeup_requested` plus the wake artifact consumed by per-agent bridges.
Bridge check-ins now control whether agents are online for immediate push
delivery; offline but registered agents still accumulate durable notifications.

For local development:

```bash
./scripts/dev
```

For production-like local runs:

```bash
./scripts/build-prod
./scripts/run-prod
```
