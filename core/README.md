# organization-autorunner-core

Go-first bootstrap for the Organization Autorunner core backend.

This repo currently includes:
- `docs/`: spec + HTTP contract
- `../contracts/oar-schema.yaml`: shared schema
- `cmd/oar-core`: HTTP server (`/health`, `/version`) with SQLite+filesystem workspace init
- `scripts/dev`, `scripts/lint`, `scripts/test`: local workflows

## Quickstart

```bash
./scripts/test
./scripts/dev
```

## Workspace Layout

By default, the server initializes storage under `.oar-workspace/`:
- `state.sqlite`: SQLite database (events, snapshots, artifacts metadata, actors, derived views)
- `artifacts/content/`: immutable artifact content files
- `logs/` and `tmp/`: reserved operational directories

Config can be passed via flags or env vars:
- `--workspace-root` / `OAR_WORKSPACE_ROOT`
- `--host` + `--port` / `OAR_HOST` + `OAR_PORT`
- `--listen-addr` / `OAR_LISTEN_ADDR` (overrides host+port)
