# agent-nexus-core

Go-first bootstrap for the Agent Nexus core backend.

This repo currently includes:
- `docs/`: spec + HTTP contract
- `../contracts/anx-schema.yaml`: shared schema
- `cmd/anx-core`: HTTP server (`/health`, `/livez`, `/readyz`, `/ops/health`, `/version`) with SQLite+filesystem workspace init
- `scripts/dev`, `scripts/lint`, `scripts/test`: local workflows

## Quickstart

```bash
./scripts/test
./scripts/dev
```

## Workspace Layout

By default, the server initializes storage under `.anx-workspace/`:
- `state.sqlite`: SQLite database (events, topics, cards, boards, documents, artifacts metadata, actors, backing threads, derived views)
- `artifacts/content/`: immutable artifact content files
- `logs/` and `tmp/`: reserved operational directories
- `router/`: local sidecar state for the embedded wake router

Config can be passed via flags or env vars:
- `--workspace-root` / `ANX_WORKSPACE_ROOT`
- `--host` + `--port` / `ANX_HOST` + `ANX_PORT`
- `--listen-addr` / `ANX_LISTEN_ADDR` (overrides host+port)

## Workspace Router

`anx-router` is the embedded workspace wake-routing sidecar hosted by
`anx-core`. It tails workspace `message_posted` events, resolves `@handle`
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
