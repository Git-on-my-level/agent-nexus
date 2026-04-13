# oar-core Runbook

This runbook covers reproducible local and production-like operation for
`oar-core`, including the embedded workspace-owned `oar-router` sidecar.

The same Go module also ships `oar-control-plane`, the SaaS control-plane service
for human accounts, organizations, workspace registry, invites, provisioning
jobs, and audit history. Its local workflow is documented in a short section
below so it can run alongside the existing core + web-ui development loop.

For packed-host SaaS operations, see:
- Architecture: [`../docs/architecture/saas-packed-host-v1.md`](../docs/architecture/saas-packed-host-v1.md)
- Configuration: [`../runbooks/packed-host-configuration.md`](../runbooks/packed-host-configuration.md)
- Backup/restore: [`../runbooks/packed-host-backup-restore.md`](../runbooks/packed-host-backup-restore.md)
- Blob backends: [`../runbooks/blob-backend-operations.md`](../runbooks/blob-backend-operations.md)
- Projection maintenance: [`../runbooks/projection-maintenance.md`](../runbooks/projection-maintenance.md)

## Prerequisites

- Go toolchain (for source runs)
- `curl` (for health/smoke checks)
- Optional: Docker (for containerized runs)

## Configuration

`oar-core` reads configuration from flags (highest priority) and environment variables.

| Purpose | Flag | Env | Default |
|---|---|---|---|
| Workspace root (SQLite + artifacts) | `--workspace-root` | `OAR_WORKSPACE_ROOT` | `.oar-workspace` |
| Blob backend selector | `--blob-backend` | `OAR_BLOB_BACKEND` | `filesystem` |
| Filesystem/object blob root | `--blob-root` | `OAR_BLOB_ROOT` | workspace `artifacts/content/` |
| Listen host | `--host` | `OAR_HOST` | `127.0.0.1` |
| Listen port | `--port` | `OAR_PORT` | `8000` |
| Full listen address (overrides host+port) | `--listen-addr` | `OAR_LISTEN_ADDR` | unset |
| Schema path | `--schema-path` | `OAR_SCHEMA_PATH` | `../contracts/oar-schema.yaml` |
| Core instance identifier | `--core-instance-id` | `OAR_CORE_INSTANCE_ID` | `core-local` |
| Core base URL for wake-packet links | n/a | `OAR_CORE_BASE_URL` | derived from listen address |
| Durable workspace id for wake routing | n/a | `OAR_WORKSPACE_ID` | `OAR_CONTROL_PLANE_WORKSPACE_ID`, else `ws_main` |
| Workspace display name for wake packets | n/a | `OAR_WORKSPACE_NAME` | `Main` |
| Enable embedded wake-routing sidecar | n/a | `OAR_SIDECAR_ROUTER_ENABLED` | `true` |
| Embedded router state path | n/a | `OAR_SIDECAR_ROUTER_STATE_PATH` | `<workspace-root>/router/router-state.json` |
| Embedded router poll interval | n/a | `OAR_SIDECAR_ROUTER_POLL_INTERVAL` | `1s` |
| Embedded router principal cache TTL | n/a | `OAR_SIDECAR_ROUTER_PRINCIPAL_CACHE_TTL` | `60s` |
| Enable dev actor mode | n/a | `OAR_ENABLE_DEV_ACTOR_MODE` | `false` |
| Allow local passkey dev bypass | n/a | `OAR_ALLOW_PASSKEY_DEV_BYPASS` | `false` |
| Allow unauthenticated writes | n/a | `OAR_ALLOW_UNAUTHENTICATED_WRITES` | `false` |
| Bootstrap token for first principal registration | n/a | `OAR_BOOTSTRAP_TOKEN` | unset |
| WebAuthn RP ID | n/a | `OAR_WEBAUTHN_RPID` | derived from browser origin host |
| WebAuthn origin | n/a | `OAR_WEBAUTHN_ORIGIN` | derived from browser request origin |
| WebAuthn allowed origins | n/a | `OAR_WEBAUTHN_ALLOWED_ORIGINS` | unset |
| WebAuthn RP display name | n/a | `OAR_WEBAUTHN_RP_DISPLAY_NAME` | `OAR` |
| Human auth mode | n/a | `OAR_HUMAN_AUTH_MODE` | `workspace_local` |
| Control-plane heartbeat base URL | n/a | `OAR_CONTROL_PLANE_BASE_URL` | unset |
| Control-plane heartbeat interval | n/a | `OAR_CONTROL_PLANE_HEARTBEAT_INTERVAL` | `30s` |
| Control-plane token issuer | n/a | `OAR_CONTROL_PLANE_TOKEN_ISSUER` | unset |
| Control-plane token audience | n/a | `OAR_CONTROL_PLANE_TOKEN_AUDIENCE` | unset |
| Control-plane workspace identifier | n/a | `OAR_CONTROL_PLANE_WORKSPACE_ID` | unset |
| Control-plane token public key (base64 Ed25519) | n/a | `OAR_CONTROL_PLANE_TOKEN_PUBLIC_KEY` | unset |
| Workspace service identity id | n/a | `OAR_WORKSPACE_SERVICE_ID` | unset |
| Workspace service private key (base64 Ed25519) | n/a | `OAR_WORKSPACE_SERVICE_PRIVATE_KEY` | unset |
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

Filesystem blobs remain the default for self-hosted and first packed-host deployments.
Set `OAR_BLOB_BACKEND=s3` only when you explicitly want S3-compatible object storage.
When `OAR_BLOB_BACKEND=s3`, configure:

- `OAR_BLOB_S3_BUCKET`
- `OAR_BLOB_S3_PREFIX`
- `OAR_BLOB_S3_REGION`
- `OAR_BLOB_S3_ENDPOINT` for custom providers such as R2 or MinIO
- `OAR_BLOB_S3_ACCESS_KEY_ID`
- `OAR_BLOB_S3_SECRET_ACCESS_KEY`
- `OAR_BLOB_S3_SESSION_TOKEN` when temporary credentials are in use
- `OAR_BLOB_S3_FORCE_PATH_STYLE` when the provider requires path-style requests

## Workspace layout

The workspace root contains:

- `state.sqlite`: canonical structured data (events, topics, cards, artifacts metadata, documents, actors, derived views)
- `artifacts/content/`: artifact bytes when `OAR_BLOB_BACKEND=filesystem` or `object`
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

`make serve` from the repo root now starts `oar-core` with the embedded router
enabled, seeds the dev workspace, and starts the web UI.

## Control-plane local development run

Run the **full local SaaS stack** (control plane + web UI + workspace cores) from
the repo root:

```bash
# Default: clears core/.oar-control-plane each start; persist with RESET_CONTROL_PLANE_WORKSPACE=0
make serve-control-plane
```

Control-plane HTTP only (no UI / no core sync):

```bash
make -C core serve-control-plane
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
| Public base URL | `--public-base-url` | `OAR_CONTROL_PLANE_PUBLIC_BASE_URL` | unset |
| WebAuthn RP ID | `--webauthn-rpid` | `OAR_CONTROL_PLANE_WEBAUTHN_RPID` | derived from browser origin |
| WebAuthn origin | `--webauthn-origin` | `OAR_CONTROL_PLANE_WEBAUTHN_ORIGIN` | derived from browser origin or `public-base-url` origin |
| Workspace URL template | `--workspace-url-template` | `OAR_CONTROL_PLANE_WORKSPACE_URL_TEMPLATE` | `<public-base-url>/%s`, else `http://127.0.0.1:8000/%s` |
| Invite URL template | `--invite-url-template` | `OAR_CONTROL_PLANE_INVITE_URL_TEMPLATE` | `<public-base-url>/invites/%s`, else `http://127.0.0.1:8100/invites/%s` |
| Workspace grant issuer | `--workspace-grant-issuer` | `OAR_CONTROL_PLANE_WORKSPACE_GRANT_ISSUER` | `public-base-url` when signing is enabled, else listen URL |
| Workspace grant audience | `--workspace-grant-audience` | `OAR_CONTROL_PLANE_WORKSPACE_GRANT_AUDIENCE` | unset |
| Workspace grant signing key (base64 Ed25519 private key) | n/a | `OAR_CONTROL_PLANE_WORKSPACE_GRANT_SIGNING_KEY` | unset |
| Session TTL | `--session-ttl` | `OAR_CONTROL_PLANE_SESSION_TTL` | `12h` |
| Ceremony TTL | `--ceremony-ttl` | `OAR_CONTROL_PLANE_CEREMONY_TTL` | `5m` |
| Launch TTL | `--launch-ttl` | `OAR_CONTROL_PLANE_LAUNCH_TTL` | `10m` |
| Invite TTL | `--invite-ttl` | `OAR_CONTROL_PLANE_INVITE_TTL` | `168h` |
| Backup maintenance interval | `--backup-maintenance-interval` | `OAR_CONTROL_PLANE_BACKUP_MAINTENANCE_INTERVAL` | `5m` |
| Graceful shutdown timeout | `--shutdown-timeout` | `OAR_CONTROL_PLANE_SHUTDOWN_TIMEOUT` | `15s` |
| Stripe API base URL override | n/a | `OAR_CONTROL_PLANE_STRIPE_API_BASE_URL` | `https://api.stripe.com` |
| Stripe publishable key | n/a | `OAR_CONTROL_PLANE_STRIPE_PUBLISHABLE_KEY` | unset |
| Stripe secret key | n/a | `OAR_CONTROL_PLANE_STRIPE_SECRET_KEY` | unset |
| Stripe webhook signing secret | n/a | `OAR_CONTROL_PLANE_STRIPE_WEBHOOK_SECRET` | unset |
| Stripe checkout success URL | n/a | `OAR_CONTROL_PLANE_STRIPE_CHECKOUT_SUCCESS_URL` | unset |
| Stripe checkout cancel URL | n/a | `OAR_CONTROL_PLANE_STRIPE_CHECKOUT_CANCEL_URL` | unset |
| Stripe billing portal return URL | n/a | `OAR_CONTROL_PLANE_STRIPE_PORTAL_RETURN_URL` | unset |
| Stripe starter price id | n/a | `OAR_CONTROL_PLANE_STRIPE_PRICE_STARTER` | unset |
| Stripe team price id | n/a | `OAR_CONTROL_PLANE_STRIPE_PRICE_TEAM` | unset |
| Stripe scale price id | n/a | `OAR_CONTROL_PLANE_STRIPE_PRICE_SCALE` | unset |
| Stripe enterprise price id | n/a | `OAR_CONTROL_PLANE_STRIPE_PRICE_ENTERPRISE` | unset (optional when enterprise stays sales-led) |

The control plane keeps entitlements in the control plane and treats Stripe as the payment event source. Filling the Stripe env values above enables:

- live Stripe Checkout session creation for paid plan starts
- live Stripe Billing Portal session creation for linked customers
- webhook-driven billing-state sync back into org billing records and plan tiers

Hosted control-plane provisioning now writes plan-based workspace quota env vars into each deployment root. Current defaults are:

- `starter`: 1 workspace, 250 MiB, 1,000 artifacts
- `team`: 5 workspaces, 25 GiB, 25,000 artifacts
- `scale`: 25 workspaces, 250 GiB, 100,000 artifacts
- `enterprise`: higher custom/default caps

Useful control-plane endpoints:

- `GET /health`
- `GET /readyz`
- `GET /organizations`
- `GET /organizations/{organization_id}/billing`
- `POST /organizations/{organization_id}/billing/checkout-session`
- `POST /organizations/{organization_id}/billing/customer-portal-session`
- `POST /billing/webhooks/stripe`
- `GET /workspaces`
- `GET /provisioning/jobs`
- `GET /audit-events`

The control plane seeds per-workspace backup schedules, runs due backups on the
maintenance interval, and prunes expired backup bundles using the recorded
retention metadata.

Basic smoke check:

```bash
make smoke-control-plane PORT=18100 WORKSPACE_ROOT="$(mktemp -d)"
```

Signed control-plane-human workspace smoke check:

```bash
make smoke-control-plane-human PORT=18102
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
OAR_WORKSPACE_ID=ws_example \
OAR_WORKSPACE_NAME=Example \
OAR_WEBAUTHN_RPID=oar.example.com \
OAR_WEBAUTHN_ALLOWED_ORIGINS=https://oar.example.com,https://oar.tailnet.ts.net \
./scripts/run-prod
```

If `OAR_WEBAUTHN_RPID`, `OAR_WEBAUTHN_ORIGIN`, and
`OAR_WEBAUTHN_ALLOWED_ORIGINS` are left unset, `oar-core` derives them per
request from the browser origin forwarded by the UI proxy. Set
`OAR_WEBAUTHN_ORIGIN` for a single fixed browser origin, or
`OAR_WEBAUTHN_ALLOWED_ORIGINS` for a comma-separated allowlist. When
`OAR_WEBAUTHN_ALLOWED_ORIGINS` is set, it takes precedence over the single
origin and the browser origin must be one of the configured values.

`./scripts/dev` defaults `OAR_ENABLE_DEV_ACTOR_MODE=1`,
`OAR_ALLOW_PASSKEY_DEV_BYPASS=1`, and `OAR_ALLOW_UNAUTHENTICATED_WRITES=1` so
local actor-selection, passkey seed flows, and dev writes keep working. The
passkey bypass additionally requires the workspace marker
`.oar-dev-insecure-auth`, which `./scripts/dev` creates inside the workspace
root. Production-like runs should leave these unset unless an explicitly open
local workflow is required.

`OAR_HUMAN_AUTH_MODE=control_plane` enables the SaaS-v-next split. In that
mode, workspace-local passkey human auth is disabled, workspace-local Ed25519
agent auth remains enabled, and startup fails closed unless the
`OAR_CONTROL_PLANE_TOKEN_*` and `OAR_WORKSPACE_SERVICE_*` settings are valid.

Set `OAR_CONTROL_PLANE_BASE_URL` to enable the workspace heartbeat reporter.
When enabled, `oar-core` reuses `OAR_WORKSPACE_SERVICE_ID` and
`OAR_WORKSPACE_SERVICE_PRIVATE_KEY` to sign a `purpose=heartbeat` workspace
service assertion and POST a background heartbeat to the control plane every
`OAR_CONTROL_PLANE_HEARTBEAT_INTERVAL` (default `30s`). Heartbeat delivery
failures are logged and retried; they do not stop the workspace core. The
heartbeat payload includes:

- core version plus build identity
- readiness summary
- projection maintenance summary
- usage summary
- last successful backup timestamp when a standard hosted backup manifest is
  discoverable near the workspace root

## Router responsibilities

`oar-router` is the embedded workspace-scoped sidecar inside `oar-core` that:

- tails `message_posted` from `oar-core`
- resolves `@handle` mentions against registered agent principals
- verifies durable registration + workspace binding before creating wake intent
- treats bridge check-in freshness as online/offline delivery state, not taggability
- writes the wake artifact plus `agent_wakeup_requested`

Per-agent bridges remain separate runtimes. They do not communicate with the
router directly; both services communicate through `oar-core` primitives.

## Verify server health

```bash
curl -fsS http://127.0.0.1:8000/health
curl -fsS http://127.0.0.1:8000/livez
curl -fsS http://127.0.0.1:8000/readyz
curl -fsS http://127.0.0.1:8000/version
```

`/health` is the minimal public liveness probe and returns only `{ ok: true }`
when the process is alive.

`/livez` is an explicit liveness alias with the same minimal payload.

`/readyz` performs the workspace storage connectivity check and embedded sidecar
readiness checks before the instance is treated as ready.

`/ops/health` is for authenticated or loopback-only operator diagnostics. When
the readiness check passes, it also includes projection maintenance status and
the embedded sidecar status block:

- `mode`: `background` when the async maintainer loop is running, `manual` when
  writes only queue dirty projections and operators are expected to trigger
  rebuilds explicitly.
- `pending_dirty_count`: thread projections queued for refresh.
- `oldest_dirty_at` / `oldest_dirty_lag_seconds`: lag indicator for the oldest queued projection refresh.
- `last_successful_stale_scan_at`: last successful stale-topic scan, whether it
  came from the background loop or an explicit rebuild.
- `last_error`: last maintenance failure, if one has occurred since the most recent successful pass.

`/ops/usage-summary` is the workspace usage envelope for control-plane polling.
It reports blob bytes, blob object count, canonical artifact/document/revision counts,
and the configured quota limits. Those blob totals now come from the workspace DB's
blob usage ledger rather than a live filesystem walk or object-store listing. Use it
when you need an explicit storage/usage summary without scraping filesystem layout.

`POST /ops/blob-usage/rebuild` is the explicit blob-ledger repair tool. Use it after
operator blob cleanup, backend drift, or older-workspace migration issues when the
ledger must be reconciled against canonical `artifacts.content_hash` rows plus backend
blob existence/size checks. The response includes:

- `canonical_hash_count`: unique canonical content hashes considered from the DB
- `missing_blob_objects`: hashes still referenced canonically but missing in the backend
- `blob_bytes` / `blob_objects`: rebuilt totals now stored in the ledger
- `rebuilt_at`: reconciliation timestamp

Projection maintenance mode is controlled by:

- `OAR_PROJECTION_MODE=background|manual` (`background` by default)

Background mode is further driven by:

- `OAR_PROJECTION_MAINTENANCE_INTERVAL`
- `OAR_PROJECTION_STALE_SCAN_INTERVAL`
- `OAR_PROJECTION_MAINTENANCE_BATCH_SIZE`

Normal operations should allow the worker to catch up asynchronously in
`background` mode. In `manual` mode, writes still queue dirty projection work
but the background loop stays off; use `POST /derived/rebuild` or
`scripts/hosted/rebuild-derived.sh` to clear the backlog after operator
intervention, during packed-host maintenance windows, or after a code/data fix
that requires a full recompute from canonical state.

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
workspace and the primary thread timeline. Topic and packet writes should be
treated as the canonical coordination path; threads remain the read-only
backing timeline for evidence inspection.

## Persistence check (restart behavior)

1. Start server with a workspace root.
2. Create a small object (for example, register an actor).
3. Stop and restart server with the same workspace root.
4. Confirm object still exists (data persisted in `state.sqlite` plus the configured blob backend root).

If `OAR_BLOB_BACKEND=filesystem`, blob bytes stay under `workspace/artifacts/content/` by default and should be backed up with `state.sqlite`.

If `OAR_BLOB_BACKEND=object`, the content objects still live on the local filesystem but in an object-style sharded layout under the configured blob root. Backup and restore workflows must still capture the database and that blob root together.

If `OAR_BLOB_BACKEND=s3`, blob bytes live in the configured bucket/prefix instead of the local workspace tree. Self-host deployments do not require this. Packed-host SaaS can opt into S3-compatible storage when off-host blob durability or storage expansion is worth the added operator surface. Backup and restore workflows must capture `state.sqlite` plus the matching bucket/prefix state together.

The hosted-v1 scripts under `scripts/hosted/` are backend-aware:

- local backends (`filesystem`, `object`) are backed up as `workspace/blob-store/`
  inside the bundle and restored into the target's effective local blob root
- `s3` bundles record the active bucket/prefix in `manifest.env` and restore the
  target env/metadata to that same remote namespace
- restore verification validates live artifact/document reads through the active
  backend instead of only checking database row counts

## Packet Convenience Atomicity

`POST /receipts` and `POST /reviews` persist packet artifact data and the emitted event atomically.

- Core writes artifact metadata/content and the corresponding event in a single transactional operation.
- On failure, core does not commit a partial convenience write (no artifact/event split state from a failed request).

## Production deployment

Hosted v1 production operations are managed per isolated workspace deployment.
Use [`deploy/managed-hosting.md`](../../deploy/managed-hosting.md) for the
authoritative provision/bootstrap/backup/restore flow. The sections below focus
on core-specific runtime behavior inside that operator model.

### Auth model

In production, `OAR_ENABLE_DEV_ACTOR_MODE`, `OAR_ALLOW_PASSKEY_DEV_BYPASS`,
and `OAR_ALLOW_UNAUTHENTICATED_WRITES` must all be `false` (the defaults).
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
- Set `OAR_WEBAUTHN_RPID` plus either `OAR_WEBAUTHN_ORIGIN` or
  `OAR_WEBAUTHN_ALLOWED_ORIGINS` explicitly when the proxy hostname differs
  from the core listen address or when multiple trusted browser origins share
  one core instance.

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

    location ~ ^/(topics|boards|docs|artifacts|events|receipts|reviews|inbox/ack|derived/rebuild) {
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
  -e OAR_ALLOW_PASSKEY_DEV_BYPASS=false \
  -e OAR_ALLOW_UNAUTHENTICATED_WRITES=false \
  -e OAR_WEBAUTHN_RPID=oar.example.com \
  -e OAR_WEBAUTHN_ALLOWED_ORIGINS=https://oar.example.com,https://oar.tailnet.ts.net \
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
  --allowed-origins https://team-alpha.oar.example.com,https://team-alpha.tailnet.ts.net \
  --listen-port 8001 \
  --web-ui-port 3001 \
  --generate-bootstrap-token

docker compose --env-file /srv/oar/team-alpha/config/env.production up -d
```

This starts both `core` (port 8000) and `web-ui` (port 3000). The web-ui
proxies API calls to core over the internal Docker network. The generated env
file also carries `HOST_OAR_WORKSPACE_ROOT`, `OAR_CORE_INSTANCE_ID`, and
`OAR_BOOTSTRAP_TOKEN`, plus explicit blob backend settings, so the Compose
example matches the hosted-v1 managed-ops story instead of a generic shared
volume.

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
topic-and-board path that creates topics, docs, boards, cards, and packets
through CLI commands, then verifies the board workspace through both core and
the UI proxy.

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
