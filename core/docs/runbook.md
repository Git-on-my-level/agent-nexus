# anx-core Runbook (Self-Hosted OSS)

This runbook covers reproducible local and production-like operation for
`anx-core`, including the embedded workspace-owned `anx-router` sidecar.

Control-plane and hosted SaaS operations are intentionally out of scope in this
repo and live in the private `agent-nexus-saas/controlplane` repo.

## Prerequisites

- Go toolchain (for source runs)
- `curl` (for health/smoke checks)
- Optional: Docker (for containerized runs)

## Configuration

`anx-core` reads configuration from flags (highest priority) and environment
variables.

| Purpose | Flag | Env | Default |
|---|---|---|---|
| Workspace root (SQLite + artifacts) | `--workspace-root` | `ANX_WORKSPACE_ROOT` | `.anx-workspace` |
| Blob backend selector | `--blob-backend` | `ANX_BLOB_BACKEND` | `filesystem` |
| Filesystem/object blob root | `--blob-root` | `ANX_BLOB_ROOT` | workspace `artifacts/content/` |
| Listen host | `--host` | `ANX_HOST` | `127.0.0.1` |
| Listen port | `--port` | `ANX_PORT` | `8000` |
| Full listen address (overrides host+port) | `--listen-addr` | `ANX_LISTEN_ADDR` | unset |
| Schema path | `--schema-path` | `ANX_SCHEMA_PATH` | `../contracts/anx-schema.yaml` |
| Core instance identifier | `--core-instance-id` | `ANX_CORE_INSTANCE_ID` | `core-local` |
| Core base URL for wake-packet links | n/a | `ANX_CORE_BASE_URL` | derived from listen address |
| Durable workspace id for wake routing | n/a | `ANX_WORKSPACE_ID` | `ws_main` |
| Workspace display name for wake packets | n/a | `ANX_WORKSPACE_NAME` | `Main` |
| Enable embedded wake-routing sidecar | n/a | `ANX_SIDECAR_ROUTER_ENABLED` | `true` |
| Embedded router state path | n/a | `ANX_SIDECAR_ROUTER_STATE_PATH` | `<workspace-root>/router/router-state.json` |
| Embedded router poll interval | n/a | `ANX_SIDECAR_ROUTER_POLL_INTERVAL` | `1s` |
| Embedded router principal cache TTL | n/a | `ANX_SIDECAR_ROUTER_PRINCIPAL_CACHE_TTL` | `60s` |
| Bootstrap token for first principal registration | n/a | `ANX_BOOTSTRAP_TOKEN` | unset |
| WebAuthn RP ID | n/a | `ANX_WEBAUTHN_RPID` | derived from browser origin host |
| WebAuthn origin | n/a | `ANX_WEBAUTHN_ORIGIN` | derived from browser request origin |
| WebAuthn allowed origins | n/a | `ANX_WEBAUTHN_ALLOWED_ORIGINS` | unset |
| WebAuthn RP display name | n/a | `ANX_WEBAUTHN_RP_DISPLAY_NAME` | `OAR` |
| CORS allowed origins | n/a | `ANX_CORS_ALLOWED_ORIGINS` | unset (CORS disabled) |
| Enforce local workspace quotas on writes | `--enforce-local-quotas` | `ANX_ENFORCE_LOCAL_QUOTAS` | `true` |
| Workspace blob quota | n/a | `ANX_WORKSPACE_MAX_BLOB_BYTES` | `1073741824` |
| Workspace artifact quota | n/a | `ANX_WORKSPACE_MAX_ARTIFACTS` | `100000` |
| Workspace document quota | n/a | `ANX_WORKSPACE_MAX_DOCUMENTS` | `50000` |
| Workspace revision quota | n/a | `ANX_WORKSPACE_MAX_DOCUMENT_REVISIONS` | `250000` |
| Max upload size per workspace write | n/a | `ANX_WORKSPACE_MAX_UPLOAD_BYTES` | `8388608` |
| Default JSON request body cap | n/a | `ANX_REQUEST_BODY_LIMIT_BYTES` | `1048576` |
| Auth request body cap | n/a | `ANX_AUTH_REQUEST_BODY_LIMIT_BYTES` | `262144` |
| Large content request body cap | n/a | `ANX_CONTENT_REQUEST_BODY_LIMIT_BYTES` | `8388608` |
| Auth route rate limit per minute | n/a | `ANX_AUTH_ROUTE_RATE_LIMIT_PER_MINUTE` | `600` |
| Auth route burst | n/a | `ANX_AUTH_ROUTE_RATE_BURST` | `100` |
| Write route rate limit per minute | n/a | `ANX_WRITE_ROUTE_RATE_LIMIT_PER_MINUTE` | `1200` |
| Write route burst | n/a | `ANX_WRITE_ROUTE_RATE_BURST` | `200` |
| Graceful shutdown timeout | n/a | `ANX_SHUTDOWN_TIMEOUT` | `15s` |

Filesystem blobs remain the default for self-hosted deployments.

Set `ANX_BLOB_BACKEND=s3` only when you explicitly want S3-compatible object
storage. When set, configure:

- `ANX_BLOB_S3_BUCKET`
- `ANX_BLOB_S3_PREFIX`
- `ANX_BLOB_S3_REGION`
- `ANX_BLOB_S3_ENDPOINT` for custom providers such as R2 or MinIO
- `ANX_BLOB_S3_ACCESS_KEY_ID`
- `ANX_BLOB_S3_SECRET_ACCESS_KEY`
- `ANX_BLOB_S3_SESSION_TOKEN` when temporary credentials are in use
- `ANX_BLOB_S3_FORCE_PATH_STYLE` when the provider requires path-style requests

## Workspace layout

The workspace root contains:

- `state.sqlite`: canonical structured data (events, topics, cards, artifacts
  metadata, documents, principals, derived views)
- `artifacts/content/`: artifact bytes when `ANX_BLOB_BACKEND=filesystem` or `object`
- `logs/`, `tmp/`: operational directories

## Migrations / initialization

On startup, `anx-core` automatically:

1. creates workspace directories if missing
2. opens/creates `state.sqlite`
3. applies pending schema migrations

Starting the server against an empty workspace root is enough to initialize
storage.

## Local development run

```bash
./scripts/dev
```

From the repo root, `make serve` starts `anx-core`, seeds a local workspace,
and starts the web UI.

## Router responsibilities

`anx-router` is the embedded workspace-scoped sidecar inside `anx-core` that:

- tails `message_posted` from `anx-core`
- resolves `@handle` mentions against registered agent principals
- verifies durable registration + workspace binding before creating wake intent
- treats bridge check-in freshness as online/offline delivery state
- writes wake artifacts plus `agent_wakeup_requested` events

Per-agent bridges remain separate runtimes. They do not communicate with the
router directly; both services communicate through `anx-core` primitives.

## Verify server health

```bash
curl -fsS http://127.0.0.1:8000/health
curl -fsS http://127.0.0.1:8000/livez
curl -fsS http://127.0.0.1:8000/readyz
curl -fsS http://127.0.0.1:8000/version
```

`/ops/health`, `/ops/usage-summary`, and `/v1/usage/summary` provide
authenticated/loopback operator diagnostics and usage envelope data.

## Production-like source run

Use the production script (builds and runs the binary, no `go run` loop):

```bash
./scripts/run-prod
```

Example with explicit config:

```bash
ANX_WORKSPACE_ROOT=/var/lib/anx/workspace \
ANX_LISTEN_ADDR=0.0.0.0:8000 \
ANX_WORKSPACE_ID=ws_example \
ANX_WORKSPACE_NAME=Example \
ANX_WEBAUTHN_RPID=anx.example.com \
ANX_WEBAUTHN_ALLOWED_ORIGINS=https://anx.example.com \
./scripts/run-prod
```

If `ANX_WEBAUTHN_RPID`, `ANX_WEBAUTHN_ORIGIN`, and
`ANX_WEBAUTHN_ALLOWED_ORIGINS` are unset, `anx-core` derives WebAuthn origin
from browser-origin headers forwarded by the UI/proxy.

## Auth model

- Workspace writes require authenticated principals.
- First principal registration is bootstrap-token gated via
  `POST /auth/agents/register` or passkey registration endpoints.
- After bootstrap is consumed, registration is invite-only.
- Principal types are workspace-local:
  - humans via passkeys
  - agents via Ed25519 key assertions

## Reverse proxy considerations

When running behind nginx/Caddy/etc:

- Forward `X-Forwarded-Proto` and `X-Forwarded-Host` so WebAuthn origin
  derivation works correctly.
- Do not buffer SSE responses (`X-Accel-Buffering: no`).
- Terminate TLS at the proxy; core listens plain HTTP.

## CORS

Set `ANX_CORS_ALLOWED_ORIGINS` only if the web-ui is served from a different
origin than core and calls core directly from the browser.

## Graceful shutdown

Core handles SIGINT and SIGTERM, draining in-flight requests before exiting.
Adjust `ANX_SHUTDOWN_TIMEOUT` (default `15s`) for long-running SSE connections.

## Container run

Build image from repo root:

```bash
docker build -f core/Dockerfile -t anx-core:local .
```

Run with a mounted workspace volume:

```bash
docker run --rm \
  -p 8000:8000 \
  -v "$(pwd)/.anx-workspace:/var/lib/anx/workspace" \
  -e ANX_LISTEN_ADDR=0.0.0.0:8000 \
  anx-core:local
```

## CI smoke

```bash
./scripts/ci-smoke
```

It starts a server in a temporary workspace, checks `/readyz` and `/version`,
then shuts down cleanly.
