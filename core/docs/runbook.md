# oar-core Runbook

This runbook covers reproducible local and production-like operation for `oar-core`.

The same Go module also ships `oar-control-plane`, the SaaS control-plane service
for human accounts, organizations, workspace registry, invites, provisioning
jobs, and audit history. Its local workflow is documented in a short section
below so it can run alongside the existing core + web-ui development loop.

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
| Core instance identifier | `--core-instance-id` | `OAR_CORE_INSTANCE_ID` | `core-local` |
| Enable dev actor mode | n/a | `OAR_ENABLE_DEV_ACTOR_MODE` | `false` |
| Allow unauthenticated writes | n/a | `OAR_ALLOW_UNAUTHENTICATED_WRITES` | `false` |
| Bootstrap token for first principal registration | n/a | `OAR_BOOTSTRAP_TOKEN` | unset |
| WebAuthn RP ID | n/a | `OAR_WEBAUTHN_RPID` | derived from browser origin host |
| WebAuthn origin | n/a | `OAR_WEBAUTHN_ORIGIN` | derived from browser request origin |
| WebAuthn RP display name | n/a | `OAR_WEBAUTHN_RP_DISPLAY_NAME` | `OAR` |
| CORS allowed origins | n/a | `OAR_CORS_ALLOWED_ORIGINS` | unset (CORS disabled) |
| Workspace blob quota | n/a | `OAR_WORKSPACE_MAX_BLOB_BYTES` | `1073741824` |
| Workspace artifact quota | n/a | `OAR_WORKSPACE_MAX_ARTIFACTS` | `100000` |
| Workspace document quota | n/a | `OAR_WORKSPACE_MAX_DOCUMENTS` | `50000` |
| Workspace revision quota | n/a | `OAR_WORKSPACE_MAX_DOCUMENT_REVISIONS` | `250000` |
| Max upload size per workspace write | n/a | `OAR_WORKSPACE_MAX_UPLOAD_BYTES` | `8388608` |
| Default JSON request body cap | n/a | `OAR_REQUEST_BODY_LIMIT_BYTES` | `1048576` |
| Auth request body cap | n/a | `OAR_AUTH_REQUEST_BODY_LIMIT_BYTES` | `262144` |
| Large content request body cap | n/a | `OAR_CONTENT_REQUEST_BODY_LIMIT_BYTES` | `8388608` |
| Auth route rate limit per minute | n/a | `OAR_AUTH_ROUTE_RATE_LIMIT_PER_MINUTE` | `600` |
| Auth route burst | n/a | `OAR_AUTH_ROUTE_RATE_BURST` | `100` |
| Write route rate limit per minute | n/a | `OAR_WRITE_ROUTE_RATE_LIMIT_PER_MINUTE` | `1200` |
| Write route burst | n/a | `OAR_WRITE_ROUTE_RATE_BURST` | `200` |
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

## Control-plane local development run

Run the control plane in a second terminal when working on SaaS-v-next flows:

```bash
make serve-control-plane PORT=8100 WORKSPACE_ROOT="$(pwd)/.oar-control-plane"
```

Or invoke the helper script directly:

```bash
./scripts/dev-control-plane
```

Relevant control-plane configuration:

| Purpose | Flag | Env | Default |
|---|---|---|---|
| Workspace root (SQLite state) | `--workspace-root` | `OAR_CONTROL_PLANE_WORKSPACE_ROOT` | `.oar-control-plane` |
| Listen host | `--host` | `OAR_CONTROL_PLANE_HOST` | `127.0.0.1` |
| Listen port | `--port` | `OAR_CONTROL_PLANE_PORT` | `8100` |
| Full listen address (overrides host+port) | `--listen-addr` | `OAR_CONTROL_PLANE_LISTEN_ADDR` | unset |
| WebAuthn RP ID | `--webauthn-rpid` | `OAR_CONTROL_PLANE_WEBAUTHN_RPID` | derived from browser origin |
| WebAuthn origin | `--webauthn-origin` | `OAR_CONTROL_PLANE_WEBAUTHN_ORIGIN` | derived from browser origin |
| Workspace URL template | `--workspace-url-template` | `OAR_CONTROL_PLANE_WORKSPACE_URL_TEMPLATE` | `http://127.0.0.1:8000/%s` |
| Invite URL template | `--invite-url-template` | `OAR_CONTROL_PLANE_INVITE_URL_TEMPLATE` | `http://127.0.0.1:8100/invites/%s` |
| Session TTL | `--session-ttl` | `OAR_CONTROL_PLANE_SESSION_TTL` | `12h` |
| Ceremony TTL | `--ceremony-ttl` | `OAR_CONTROL_PLANE_CEREMONY_TTL` | `5m` |
| Launch TTL | `--launch-ttl` | `OAR_CONTROL_PLANE_LAUNCH_TTL` | `10m` |
| Invite TTL | `--invite-ttl` | `OAR_CONTROL_PLANE_INVITE_TTL` | `168h` |
| Graceful shutdown timeout | `--shutdown-timeout` | `OAR_CONTROL_PLANE_SHUTDOWN_TIMEOUT` | `15s` |

Useful control-plane endpoints:

- `GET /health`
- `GET /readyz`
- `GET /organizations`
- `GET /workspaces`
- `GET /provisioning/jobs`
- `GET /audit-events`

Basic smoke check:

```bash
make smoke-control-plane PORT=18100 WORKSPACE_ROOT="$(mktemp -d)"
```

Typical local split:

- Terminal 1: `make serve`
- Terminal 2: `make serve-control-plane`

This preserves the current workspace-core + web-ui loop while bringing up the
new shared control-plane service beside it.

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

`./scripts/dev` defaults `OAR_ENABLE_DEV_ACTOR_MODE=1` and
`OAR_ALLOW_UNAUTHENTICATED_WRITES=1` so local actor-selection, reads, and seed
workflows keep working. Production-like runs should leave both unset unless an
explicitly open local workflow is required.

## Verify server health

```bash
curl -fsS http://127.0.0.1:8000/health
curl -fsS http://127.0.0.1:8000/readyz
curl -fsS http://127.0.0.1:8000/version
```

`/health` is the minimal public liveness probe and returns only `{ ok: true }`
when the process is alive.

`/readyz` performs the workspace storage connectivity check before the instance
is treated as ready.

`/ops/health` is for authenticated or loopback-only operator diagnostics. When
the readiness check passes, it also includes projection maintenance status:

- `pending_dirty_count`: thread projections queued for refresh.
- `oldest_dirty_at` / `oldest_dirty_lag_seconds`: lag indicator for the oldest queued projection refresh.
- `last_successful_stale_scan_at`: last successful background stale-thread scan.
- `last_error`: last maintenance failure, if one has occurred since the most recent successful pass.

Background projection maintenance is driven by:

- `OAR_PROJECTION_MAINTENANCE_INTERVAL`
- `OAR_PROJECTION_STALE_SCAN_INTERVAL`
- `OAR_PROJECTION_MAINTENANCE_BATCH_SIZE`

Normal operations should allow the worker to catch up asynchronously. `POST /derived/rebuild`
is still the explicit repair tool when the queue is badly behind, after operator intervention,
or after a code/data fix that requires a full recompute from canonical state.

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

Hosted v1 production operations are managed per isolated workspace deployment.
Use [`deploy/managed-hosting.md`](../../deploy/managed-hosting.md) for the
authoritative provision/bootstrap/backup/restore flow. The sections below focus
on core-specific runtime behavior inside that operator model.

### Auth model

In production, `OAR_ENABLE_DEV_ACTOR_MODE` and
`OAR_ALLOW_UNAUTHENTICATED_WRITES` must both be `false` (the defaults).
Workspace reads and writes require a valid Bearer token, and `POST /actors`
must remain disabled. Two principal types are supported:

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

Core already enforces request-body limits, workspace quotas, and route-class
throttles. Edge limits should be complementary, not a replacement. A minimal
nginx example:

```nginx
http {
  limit_req_zone $binary_remote_addr zone=oar_auth:10m rate=30r/m;
  limit_req_zone $binary_remote_addr zone=oar_write:10m rate=300r/m;

  server {
    location /auth/ {
      limit_req zone=oar_auth burst=10 nodelay;
      proxy_pass http://127.0.0.1:8000;
    }

    location ~ ^/(threads|commitments|boards|docs|artifacts|events|work_orders|receipts|reviews|inbox/ack|derived/rebuild) {
      limit_req zone=oar_write burst=100 nodelay;
      proxy_pass http://127.0.0.1:8000;
    }
  }
}
```

When these limits trip, clients should expect:

- `413 request_too_large` for oversized request bodies
- `507 workspace_quota_exceeded` for storage or count quota exhaustion
- `429 rate_limited` with `Retry-After` for route-class throttling

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
  -e OAR_ENABLE_DEV_ACTOR_MODE=false \
  -e OAR_ALLOW_UNAUTHENTICATED_WRITES=false \
  -e OAR_WEBAUTHN_RPID=oar.example.com \
  -e OAR_WEBAUTHN_ORIGIN=https://oar.example.com \
  oar-core:local
```

Health checks from host:

```bash
curl -fsS http://127.0.0.1:8000/readyz
curl -fsS http://127.0.0.1:8000/version
```

## Docker Compose (full stack)

From the repo root:

```bash
./scripts/hosted/provision-workspace.sh \
  --instance team-alpha \
  --instance-root /srv/oar/team-alpha \
  --public-origin https://team-alpha.oar.example.com \
  --listen-port 8001 \
  --web-ui-port 3001 \
  --generate-bootstrap-token

docker compose --env-file /srv/oar/team-alpha/config/env.production up -d
```

This starts both `core` (port 8000) and `web-ui` (port 3000). The web-ui
proxies API calls to core over the internal Docker network. The generated env
file also carries `HOST_OAR_WORKSPACE_ROOT`, `OAR_CORE_INSTANCE_ID`, and
`OAR_BOOTSTRAP_TOKEN` so the Compose example matches the hosted-v1 managed-ops
story instead of a generic shared volume.

## CI smoke

Run the headless smoke script:

```bash
./scripts/ci-smoke
```

It starts a server in a temporary workspace, checks `/readyz` and `/version`, then shuts down cleanly.

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
