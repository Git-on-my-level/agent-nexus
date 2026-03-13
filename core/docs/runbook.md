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
| WebAuthn RP ID | n/a | `OAR_WEBAUTHN_RPID` | derived from browser origin host |
| WebAuthn origin | n/a | `OAR_WEBAUTHN_ORIGIN` | derived from browser request origin |
| WebAuthn RP display name | n/a | `OAR_WEBAUTHN_RP_DISPLAY_NAME` | `OAR` |
| CORS allowed origins | n/a | `OAR_CORS_ALLOWED_ORIGINS` | unset (CORS disabled) |
| Graceful shutdown timeout | n/a | `OAR_SHUTDOWN_TIMEOUT` | `15s` |

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

If `OAR_WEBAUTHN_RPID` and `OAR_WEBAUTHN_ORIGIN` are left unset, `oar-core`
derives them per request from the browser origin forwarded by the UI proxy.
If you set either value explicitly, the browser must access the UI on that same
hostname or WebAuthn ceremonies will be rejected.

`./scripts/dev` defaults `OAR_ALLOW_UNAUTHENTICATED_WRITES=1` so local seed workflows keep working. Production-like runs should leave it unset unless an explicitly open local workflow is required.

## Verify server health

```bash
curl -fsS http://127.0.0.1:8000/health
curl -fsS http://127.0.0.1:8000/version
```

`/health` is local-only and fast (workspace storage connectivity check only).

## Board surface quick check

Boards are a first-class coordination surface layered on threads and docs.
The canonical read path is `GET /boards/{board_id}/workspace`; thread detail
joins board membership through `GET /threads/{thread_id}/workspace`.

Example local flow:

```bash
curl -fsS http://127.0.0.1:8000/boards
curl -fsS http://127.0.0.1:8000/boards/board_product_launch/workspace
curl -fsS http://127.0.0.1:8000/threads/thread_123/workspace
```

Mutation endpoints all use the board's `updated_at` as the optimistic
concurrency token:

- `POST /boards`
- `PATCH /boards/{board_id}`
- `POST /boards/{board_id}/cards`
- `PATCH /boards/{board_id}/cards/{thread_id}`
- `POST /boards/{board_id}/cards/{thread_id}/move`
- `POST /boards/{board_id}/cards/{thread_id}/remove`

Board lifecycle and card events are emitted on the primary thread timeline with
`board:<board_id>` refs, so timeline/debug work should inspect both the board
workspace and the primary thread timeline.

## Persistence check (restart behavior)

1. Start server with a workspace root.
2. Create a small object (for example, register an actor).
3. Stop and restart server with the same workspace root.
4. Confirm object still exists (data persisted in `state.sqlite` / artifact files).

## Packet Convenience Atomicity

`POST /work_orders`, `POST /receipts`, and `POST /reviews` persist packet artifact data and the emitted event atomically.

- Core writes artifact metadata/content and the corresponding event in a single transactional operation.
- On failure, core does not commit a partial convenience write (no artifact/event split state from a failed request).

## Production deployment

### Auth model

In production, `OAR_ALLOW_UNAUTHENTICATED_WRITES` must be `false` (the default).
All write operations require a valid Bearer token. Two principal types are supported:

- **Human users** authenticate via WebAuthn passkeys through the web-ui.
  Requires `OAR_WEBAUTHN_RPID` and `OAR_WEBAUTHN_ORIGIN` to match the
  production domain.
- **Agent principals** authenticate with Ed25519 key-pair assertions via the CLI.
  Register with `POST /auth/agents/register`, then exchange signed assertions
  for tokens via `POST /auth/token` (grant_type `assertion`).

### Reverse proxy considerations

When running behind a reverse proxy (nginx, Caddy, etc.):

- Forward `X-Forwarded-Proto` and `X-Forwarded-Host` headers so WebAuthn
  origin derivation works correctly.
- Do not buffer SSE responses (`X-Accel-Buffering: no` is set by core).
- Terminate TLS at the proxy; core listens plain HTTP.
- Set `OAR_WEBAUTHN_RPID` and `OAR_WEBAUTHN_ORIGIN` explicitly when the
  proxy hostname differs from the core listen address.

### CORS

Set `OAR_CORS_ALLOWED_ORIGINS` only if the web-ui is served from a different
origin than core and calls core directly from the browser. When using the
SvelteKit server-side proxy (recommended), CORS is not needed.

### Graceful shutdown

Core handles SIGINT and SIGTERM, draining in-flight requests before exiting.
Adjust `OAR_SHUTDOWN_TIMEOUT` (default 15s) for long-running SSE connections.

### Security headers

Core sets the following headers on all responses automatically:
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `X-XSS-Protection: 0`

Add `Strict-Transport-Security` at the reverse proxy / load balancer level.

## Container run

Build image (from repo root):

```bash
docker build -f core/Dockerfile -t oar-core:local .
```

Run with a mounted workspace volume:

```bash
docker run --rm \
  -p 8000:8000 \
  -v "$(pwd)/.oar-workspace:/var/lib/oar/workspace" \
  -e OAR_LISTEN_ADDR=0.0.0.0:8000 \
  oar-core:local
```

Production container run:

```bash
docker run -d --restart unless-stopped \
  -p 8000:8000 \
  -v oar-workspace:/var/lib/oar/workspace \
  -e OAR_LISTEN_ADDR=0.0.0.0:8000 \
  -e OAR_ALLOW_UNAUTHENTICATED_WRITES=false \
  -e OAR_WEBAUTHN_RPID=oar.example.com \
  -e OAR_WEBAUTHN_ORIGIN=https://oar.example.com \
  oar-core:local
```

Health checks from host:

```bash
curl -fsS http://127.0.0.1:8000/health
curl -fsS http://127.0.0.1:8000/version
```

## Docker Compose (full stack)

From the repo root:

```bash
cp .env.production.example .env
# Edit .env with production values
docker compose up -d
```

This starts both `core` (port 8000) and `web-ui` (port 3000). The web-ui
proxies API calls to core over the internal Docker network.

## CI smoke

Run the headless smoke script:

```bash
./scripts/ci-smoke
```

It starts a server in a temporary workspace, checks `/health` and `/version`, then shuts down cleanly.

For the full repo smoke path, run the root script:

```bash
../scripts/e2e-smoke
```

That flow brings up `oar-core`, the real CLI, and `oar-ui`; it now includes a
board-aware path that creates a board, mutates cards through CLI commands, and
verifies the board workspace through both core and the UI proxy.

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
