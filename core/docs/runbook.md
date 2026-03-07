# oar-core Runbook

This runbook covers reproducible local and production-like operation for `oar-core`.

## Prerequisites

- Go toolchain (for source runs)
- `curl` (for health/smoke checks)
- Optional: Docker (for containerized runs)

## Configuration

`oar-core` reads configuration from flags (highest priority) and environment variables.

| Purpose | Flag | Env | Default |
|---|---|---|---|
| Workspace root (SQLite + artifacts) | `--workspace-root` | `OAR_WORKSPACE_ROOT` | `.oar-workspace` |
| Listen host | `--host` | `OAR_HOST` | `127.0.0.1` |
| Listen port | `--port` | `OAR_PORT` | `8000` |
| Full listen address (overrides host+port) | `--listen-addr` | `OAR_LISTEN_ADDR` | unset |
| Schema path | `--schema-path` | `OAR_SCHEMA_PATH` | `../contracts/oar-schema.yaml` |
| Allow unauthenticated writes | n/a | `OAR_ALLOW_UNAUTHENTICATED_WRITES` | `false` |
| WebAuthn RP ID | n/a | `OAR_WEBAUTHN_RPID` | `127.0.0.1` |
| WebAuthn origin | n/a | `OAR_WEBAUTHN_ORIGIN` | `http://127.0.0.1:5173` |
| WebAuthn RP display name | n/a | `OAR_WEBAUTHN_RP_DISPLAY_NAME` | `OAR` |

## Workspace layout

The workspace root contains:

- `state.sqlite`: canonical structured data (events, snapshots, artifacts metadata, actors, derived views)
- `artifacts/content/`: artifact bytes
- `logs/`, `tmp/`: operational directories

## Migrations / initialization

There is no separate migration command in v0.

On startup, `oar-core` automatically:

1. creates workspace directories if missing
2. opens/creates `state.sqlite`
3. applies pending schema migrations

Starting the server against an empty workspace root is enough to initialize storage.

## Local development run

```bash
./scripts/dev
```

## Production-like source run

Use the production script (builds and runs the binary, no development `go run` loop):

```bash
./scripts/run-prod
```

Example with explicit config:

```bash
OAR_WORKSPACE_ROOT=/var/lib/oar/workspace \
OAR_LISTEN_ADDR=0.0.0.0:8000 \
OAR_WEBAUTHN_RPID=oar.example.com \
OAR_WEBAUTHN_ORIGIN=https://oar.example.com \
./scripts/run-prod
```

`./scripts/dev` defaults `OAR_ALLOW_UNAUTHENTICATED_WRITES=1` so local seed workflows keep working. Production-like runs should leave it unset unless an explicitly open local workflow is required.

## Verify server health

```bash
curl -fsS http://127.0.0.1:8000/health
curl -fsS http://127.0.0.1:8000/version
```

`/health` is local-only and fast (workspace storage connectivity check only).

## Persistence check (restart behavior)

1. Start server with a workspace root.
2. Create a small object (for example, register an actor).
3. Stop and restart server with the same workspace root.
4. Confirm object still exists (data persisted in `state.sqlite` / artifact files).

## Packet Convenience Atomicity

`POST /work_orders`, `POST /receipts`, and `POST /reviews` persist packet artifact data and the emitted event atomically.

- Core writes artifact metadata/content and the corresponding event in a single transactional operation.
- On failure, core does not commit a partial convenience write (no artifact/event split state from a failed request).

## Container run

Build image:

```bash
docker build -f core/Dockerfile -t oar-core:local ..
```

Run with a mounted workspace volume:

```bash
docker run --rm \
  -p 8000:8000 \
  -v "$(pwd)/.oar-workspace:/var/lib/oar/workspace" \
  -e OAR_LISTEN_ADDR=0.0.0.0:8000 \
  oar-core:local
```

Health checks from host:

```bash
curl -fsS http://127.0.0.1:8000/health
curl -fsS http://127.0.0.1:8000/version
```

## CI smoke

Run the headless smoke script:

```bash
./scripts/ci-smoke
```

It starts a server in a temporary workspace, checks `/health` and `/version`, then shuts down cleanly.

## Compatibility troubleshooting

### Version mismatch / outdated clients

Use handshake metadata to debug CLI/UI compatibility:

```bash
curl -fsS http://127.0.0.1:8000/meta/handshake
```

Check:

- `schema_version`
- `min_cli_version`
- `recommended_cli_version`
- `cli_download_url`

If clients receive `cli_outdated`, upgrade client binaries and retry.

## SSE troubleshooting

Validate stream endpoints directly:

```bash
curl -N -H 'Accept: text/event-stream' http://127.0.0.1:8000/events/stream
curl -N -H 'Accept: text/event-stream' http://127.0.0.1:8000/inbox/stream
```

Resume semantics:

- use `Last-Event-ID` header or `last_event_id` query
- ensure reverse proxies do not buffer SSE responses
- verify `X-Accel-Buffering: no` is preserved
