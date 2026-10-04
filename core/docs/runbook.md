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
| Public workspace web UI URL for host enrollment (for example `https://example.com/o/acme/w/main`) | `--public-web-ui-workspace-url` | `ANX_PUBLIC_WEB_UI_WORKSPACE_URL` | unset; enrollment returns the code without a link |
| Durable workspace id for wake routing | n/a | `ANX_WORKSPACE_ID` | `ws_main` |
| Workspace display name for wake packets | n/a | `ANX_WORKSPACE_NAME` | `Main` |
| Answer wake quiet window per requesting agent | n/a | `ANX_ANSWER_WAKE_QUIET_WINDOW` | `60s` |
| Flush answer wake when no asks remain | n/a | `ANX_ANSWER_WAKE_FLUSH_WHEN_NO_OPEN_ASKS` | `true` |
| Enable embedded wake-routing sidecar | n/a | `ANX_SIDECAR_ROUTER_ENABLED` | `true` |
| Embedded router state path | n/a | `ANX_SIDECAR_ROUTER_STATE_PATH` | `<workspace-root>/router/router-state.json` |
| Embedded router poll interval | n/a | `ANX_SIDECAR_ROUTER_POLL_INTERVAL` | `1s` |
| Embedded router principal cache TTL | n/a | `ANX_SIDECAR_ROUTER_PRINCIPAL_CACHE_TTL` | `60s` |
| Bootstrap token for first principal registration | n/a | `ANX_BOOTSTRAP_TOKEN` | unset |
| Selected PM agent actor (claim/complete/fail when set) | n/a | `ANX_PM_AGENT_ACTOR_ID` | unset |
| Selected PM agent handle (wake-bridge path only) | n/a | `ANX_PM_AGENT_HANDLE` | unset |
| PM runner lease TTL (renew at a cadence strictly less than TTL/2) | n/a | `ANX_PM_LEASE_TTL` | `60s` (range `1s`–`10m`) |
| PM turn wall time (queued runner and wake dispatch) | n/a | `ANX_PM_TURN_TIMEOUT` | `2m` (max `10m`) |
| PM turn response byte cap | n/a | `ANX_PM_MAX_OUTPUT_BYTES` | `16000` |
| Enable wake-routing PM bridge | n/a | `ANX_PM_BRIDGE_ENABLED` | `false` |
| Attest an independently enforced PM capability envelope | n/a | `ANX_PM_RUNTIME_ENVELOPE_ENFORCED` | `false` |
| Telegram webhook secret (≥32 chars) | n/a | `ANX_PM_TELEGRAM_WEBHOOK_SECRET` | unset |
| Telegram bot id (tenant) | n/a | `ANX_PM_TELEGRAM_BOT_ID` | unset |
| Telegram bot token (outbound only) | n/a | `ANX_PM_TELEGRAM_BOT_TOKEN` | unset |
| Telegram Bot API base (fakes/tests only) | n/a | `ANX_PM_TELEGRAM_API_BASE` | `https://api.telegram.org` |
| Discord application public key (32-byte hex) | n/a | `ANX_PM_DISCORD_PUBLIC_KEY` | unset |
| Discord application id | n/a | `ANX_PM_DISCORD_APPLICATION_ID` | unset |
| Discord bot token (outbound only) | n/a | `ANX_PM_DISCORD_BOT_TOKEN` | unset |
| Discord REST API base (fakes/tests only) | n/a | `ANX_PM_DISCORD_API_BASE` | `https://discord.com/api/v10` |
| WebAuthn RP ID | n/a | `ANX_WEBAUTHN_RPID` | derived from browser origin host |
| WebAuthn origin | n/a | `ANX_WEBAUTHN_ORIGIN` | derived from browser request origin |
| WebAuthn allowed origins | n/a | `ANX_WEBAUTHN_ALLOWED_ORIGINS` | unset |
| WebAuthn RP display name | n/a | `ANX_WEBAUTHN_RP_DISPLAY_NAME` | `Agent Nexus` |
| CORS allowed origins | n/a | `ANX_CORS_ALLOWED_ORIGINS` | unset (CORS disabled) |
| Enforce local workspace quotas on writes | `--enforce-local-quotas` | `ANX_ENFORCE_LOCAL_QUOTAS` | `false` |
| Workspace storage quota (blob bytes plus SQLite bytes when measurable) | n/a | `ANX_WORKSPACE_MAX_BLOB_BYTES` | `1073741824` |
| Workspace artifact quota | n/a | `ANX_WORKSPACE_MAX_ARTIFACTS` | `100000` |
| Workspace document quota | n/a | `ANX_WORKSPACE_MAX_DOCUMENTS` | `50000` |
| Workspace revision quota | n/a | `ANX_WORKSPACE_MAX_REVISIONS` | `250000` |
| Max upload size per workspace write | n/a | `ANX_WORKSPACE_MAX_UPLOAD_BYTES` | `8388608` |
| Default JSON request body cap | n/a | `ANX_REQUEST_BODY_LIMIT_BYTES` | `1048576` |
| Auth request body cap | n/a | `ANX_AUTH_REQUEST_BODY_LIMIT_BYTES` | `262144` |
| Large content request body cap | n/a | `ANX_CONTENT_REQUEST_BODY_LIMIT_BYTES` | `8388608` |
| Auth route rate limit per minute | n/a | `ANX_AUTH_ROUTE_RATE_LIMIT_PER_MINUTE` | `600` |
| Auth route burst | n/a | `ANX_AUTH_ROUTE_RATE_BURST` | `100` |
| Write route rate limit per minute | n/a | `ANX_WRITE_ROUTE_RATE_LIMIT_PER_MINUTE` | `1200` |
| Write route burst | n/a | `ANX_WRITE_ROUTE_RATE_BURST` | `200` |
| Graceful shutdown timeout | n/a | `ANX_SHUTDOWN_TIMEOUT` | `15s` |

### Optional: heartbeat publisher and account status (env contract)

For deployments that report usage/health to a remote listener or enforce account
status on refresh, `anx-core` supports two independent, generic env contracts.
See `AGENTS.md` in this module for the canonical list. Summary:

- **Heartbeat**: `ANX_HEARTBEAT_PUBLISHER_URL` plus `ANX_WORKSPACE_SERVICE_ID` and
  `ANX_WORKSPACE_SERVICE_PRIVATE_KEY`; optional `ANX_HEARTBEAT_INTERVAL`,
  `ANX_HEARTBEAT_AUDIENCE`.
- **Account status HTTP checker**: `ANX_ACCOUNT_STATUS_URL` (base URL) plus the
  same service identity env vars; optional `ANX_ACCOUNT_STATUS_PATH` (default
  `v1/internal/accounts/status`), `ANX_ACCOUNT_STATUS_AUDIENCE` (default
  `anx-control-plane`).

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
and starts the web UI. For the default game-dev-studio scenario it also seeds
the Studio PM agent (`actor-gds-pm` / `pm.dev-host`), sets `ANX_PM_AGENT_ACTOR_ID`
and `ANX_PM_AGENT_HANDLE`, enrolls the local dev host, and prints `anx pm serve`.
`make serve` also configures the local workspace web URL so interactive host
enrollment links to the web UI port.
Queued PM turns do not require `ANX_PM_BRIDGE_ENABLED` or an online wake handle.
`POST /pm/turns/claim` leases one `sending` turn; complete/fail with that
`lease_token`. Runners must renew through `POST /pm/turns/{turn_id}/heartbeat`
with `{"lease_token":"..."}` at a cadence strictly less than `ANX_PM_LEASE_TTL / 2`
(for example every 20s for the default 60s TTL). Use returned `lease_expires_at`
to track ownership, and stop execution on `409 lease_mismatch`. Renewal never
extends the turn deadline. A crashed runner frees capacity when its lease expires;
the next poll can reclaim the same turn with a fresh token and preserved history.
Ensure runner implementations support heartbeat before relying on turns longer
than the lease TTL. Past-deadline sending turns expire to `failed` on claim.
`ANX_PM_MAX_CONCURRENT` (default 2) bounds claimed turns with unexpired runner
leases; new claims with waiting work return `429 busy` at that cap (204 if no work waits). `ANX_PM_MAX_QUEUED` (default 20)
separately bounds open turns waiting without an unexpired lease, including
unknown wakeup outcomes. A full queue returns `429 busy` with `reason: queue`,
`queued`, and `limit`; each conversation still allows at most one pending turn.
Channel ingress turns carry `origin` and use this same claim pipeline. See
`cli/docs/runbook.md` for the omp / `zai/glm-5.3` recipe and `{prompt}` Hermes
or Codex argv.

## PM channels (Telegram and Discord)

Ingress is webhook/interaction only: `POST /pm/ingress/telegram` and
`POST /pm/ingress/discord`. Core never calls `setWebhook`, polls `getUpdates`,
or opens a Discord gateway. Point production bots here only after an explicit
canary; local proof uses the fakes under `tests/channels/` and the test-only
API bases `ANX_PM_TELEGRAM_API_BASE` / `ANX_PM_DISCORD_API_BASE`.

Operator setup, once dedicated bot credentials exist:

1. Set the env vars in the configuration table. Tokens stay in env, never in Git.
2. Bind each channel identity before anyone talks to the PM:
   `anx pm bindings create --from-file binding.json`
   (`transport`, `tenant_id`, `channel_id`, `external_user_id`, target actor,
   `can_approve`). Shared-channel membership is not authority.
3. Point Telegram's webhook at `/pm/ingress/telegram` with
   `secret_token` equal to `ANX_PM_TELEGRAM_WEBHOOK_SECRET`. Point Discord
   Interactions URL at `/pm/ingress/discord`. Do not reuse CAR or other
   production bot streams.
4. `anx pm channels doctor --telegram-webhook-url <ingress> --discord-webhook-url <ingress>`
   checks secret shape, GET reachability (no POST), and binding list. It
   never sends a chat message.

Unbound identities receive a bind-first reply and no turn. Duplicate
webhook/interaction ids do not create a second turn or a second outbound
send. 429/409 stay `pending_delivery` with backoff; unknown outcomes are not
auto-retried.

## Router responsibilities

`anx-router` is the embedded workspace-scoped sidecar inside `anx-core` that:

- tails `message_posted` from `anx-core`
- resolves `@handle` mentions against enabled derived-agent handles
- resolves enabled derived-agent handles before creating wake intent
- treats bridge check-in freshness as online/offline delivery state
- writes wake artifacts plus first-class `agent_wakeups` queue records

One bridge runs per enrolled host. It does not communicate with the router
directly; both services communicate through `anx-core` primitives.

Answer notifications are accumulated durably per requesting agent. The core
dispatcher normally wakes the agent after 60 seconds without another answer;
it can also wake immediately once that agent has no open asks. Set
`ANX_ANSWER_WAKE_QUIET_WINDOW` to change the quiet period (for example,
`90s`), and set `ANX_ANSWER_WAKE_FLUSH_WHEN_NO_OPEN_ASKS=false` to always wait
for the quiet period even after the last ask is answered.

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
- The first principal is a human registered through the bootstrap passkey ceremony.
- After bootstrap, human registration uses human invites; hosts enroll through human approval or a one-time headless token.
- Principal types are workspace-local:
  - humans via passkeys
  - hosts via Ed25519 key proofs, deriving agent access tokens

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

## Agent administration and fleet enrollment

Humans retain workspace administration rights. An agent receives them only through
an explicit human grant on the Access page or `anx auth admins grant <principal>`.
The grant is stored on the agent principal, never the host, and is checked against
durable state on each request. `auth admins revoke` immediately removes subsequent
administration access, without interrupting ordinary workspace reads.

Granting an agent on host X trusts every process that can read X's shared host
key and derive a token for that agent name. This is a shared host credential
boundary, even though the grant targets one exact principal. Grant/revoke audit
events include host ID, slug, and agent name. Administration writes revalidate
durable authority inside the write transaction after body decoding.

Only a human can create human invitations. Both invite resolution and credential
creation reject invitations issued by an agent, including historical invitations
and previously issued registration sessions. Agent bearer credentials cannot
authorize human registration, login, or external human-grant exchange. Anonymous
human ceremonies still require independently provisioned bootstrap/invite,
WebAuthn, or trusted external issuer proof; those credentials must not be shared
with agents. WebAuthn does not prove that a software client is a biological person.

Granted agents can list/approve/deny pending host enrollments, create/list/revoke
headless enrollment tokens, and revoke other hosts. They cannot revoke their own
host, change grants, edit hosts through an agent bearer, or bypass any other
human-only action. Host enrollment token creation, revocation, and consumption
are audited with principal IDs and the destination host on consumption. Tokens
are separate one-time grants; revoke outstanding tokens separately if necessary.

Use the stdin-over-SSH flow in [the CLI runbook](../../cli/docs/runbook.md#fleet-enrollment-by-an-auth-admin-agent)
for fleet enrollment. Logs and token lists never contain enrollment secrets.

### Capacity policy and technical safety

Self-hosted workspaces have no capacity quotas by default. Usage counters and
`GET /v1/usage/summary` remain available. `ANX_WORKSPACE_MAX_*` capacity settings
apply only when explicitly opting in with `ANX_ENFORCE_LOCAL_QUOTAS=true` (or
`--enforce-local-quotas`). External deployment policy may instead set the generic
`ANX_WORKSPACE_ACCESS_MODE`; this is independent of local quotas.

Technical limits stay active in both modes: `ANX_REQUEST_BODY_LIMIT_BYTES`,
`ANX_AUTH_REQUEST_BODY_LIMIT_BYTES`, `ANX_CONTENT_REQUEST_BODY_LIMIT_BYTES`,
`ANX_ATTACHMENT_MAX_UPLOAD_BYTES`, auth/write rate limits, and blob integrity
and reference checks. Disabling capacity quotas does not disable these limits.
