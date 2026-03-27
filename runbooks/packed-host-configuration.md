# Packed-host production configuration

This runbook covers the required and optional configuration for the packed-host SaaS shape:

- one shared control plane
- one shared web UI
- many isolated workspace cores
- one Linux host first

For the architectural rationale, see [`../docs/architecture/saas-packed-host-v1.md`](../docs/architecture/saas-packed-host-v1.md).

## Self-host vs SaaS packed-host

| Aspect | Self-host | SaaS packed-host |
|---|---|---|
| Human auth | Workspace-local passkeys | Control-plane managed with workspace launch grants |
| Workspace count | One or few, statically configured | Many, dynamically provisioned |
| Workspace routing | Static `OAR_WORKSPACES` in web-ui | Dynamic control-plane resolution |
| Onboarding | Operator-driven bootstrap + invites | Self-serve account signup + org invites |
| Control plane | Not required | Required |
| Billing/quota | Not in product | Control-plane envelope per org |

The same `oar-core` binary serves both models. The difference is configuration:

- Self-host: `OAR_HUMAN_AUTH_MODE=workspace_local` or unset
- SaaS packed-host: `OAR_HUMAN_AUTH_MODE=control_plane` plus control-plane token settings

## Control plane configuration

Location: `/etc/oar/control-plane.env`

Required settings:

| Variable | Purpose |
|---|---|
| `OAR_CONTROL_PLANE_LISTEN_ADDR` | Loopback bind address, e.g. `127.0.0.1:8100` |
| `OAR_CONTROL_PLANE_WORKSPACE_ROOT` | Persistent state directory for control-plane DB |
| `OAR_CONTROL_PLANE_PUBLIC_BASE_URL` | Public browser-facing base URL used for workspace URLs, invite URLs, and launch-grant issuer defaults |
| `OAR_CONTROL_PLANE_WEBAUTHN_RPID` | Public hostname for passkey ceremonies |
| `OAR_CONTROL_PLANE_WEBAUTHN_ORIGIN` | Full origin including `https://`; defaults to the origin of `OAR_CONTROL_PLANE_PUBLIC_BASE_URL` when set |
| `OAR_CONTROL_PLANE_WORKSPACE_URL_TEMPLATE` | Optional override pattern for workspace URLs, `%s` = workspace path |
| `OAR_CONTROL_PLANE_INVITE_URL_TEMPLATE` | Optional override pattern for invite acceptance URLs |
| `OAR_CONTROL_PLANE_WORKSPACE_GRANT_SIGNING_KEY` | Base64 Ed25519 private key for signing launch grants |

Placement defaults for packed-host:

| Variable | Purpose |
|---|---|
| `OAR_CONTROL_PLANE_LOCAL_HOST_ID` | Identifier for this packed host |
| `OAR_CONTROL_PLANE_LOCAL_HOST_ROOT` | Parent directory for workspace roots |
| `OAR_CONTROL_PLANE_LOCAL_HOST_PORT_START` | First port in workspace core range |
| `OAR_CONTROL_PLANE_LOCAL_HOST_PORT_END` | Last port in workspace core range |
| `OAR_CONTROL_PLANE_HOSTED_SCRIPTS_DIR` | Path to `scripts/hosted/` |
| `OAR_CONTROL_PLANE_VERIFY_CORE_BINARY` | Path to `oar-core` binary for restore verification |
| `OAR_CONTROL_PLANE_VERIFY_SCHEMA_PATH` | Path to schema for restore verification |

See [`../deploy/env/packed-host/control-plane.env.example`](../deploy/env/packed-host/control-plane.env.example) for a complete template.

## Web UI configuration

Location: `/etc/oar/web-ui.env`

Required settings:

| Variable | Purpose |
|---|---|
| `HOST` | Loopback bind host, e.g. `127.0.0.1` |
| `PORT` | Loopback bind port, e.g. `4173` |
| `ORIGIN` | Public origin including `https://` |
| `OAR_CONTROL_BASE_URL` | Control plane loopback URL |

In SaaS mode, omit `OAR_WORKSPACES`. The web UI resolves workspace slugs dynamically from the control plane when a signed-in session exists. Static `OAR_WORKSPACES` is only for self-host fallback or development.

See [`../deploy/env/packed-host/web-ui.env.example`](../deploy/env/packed-host/web-ui.env.example) for a complete template.

## Workspace core configuration

Each workspace has its own env file: `/etc/oar/workspaces/<workspace-id>.env`
and one packed-host instance root at
`/var/lib/oar/workspaces/<workspace-id>/` with `workspace/`, `config/`,
`metadata/`, and `backups/`.

Required settings:

| Variable | Purpose |
|---|---|
| `OAR_LISTEN_ADDR` | Loopback bind address for this workspace |
| `OAR_WORKSPACE_ROOT` | Runtime workspace directory, typically `/var/lib/oar/workspaces/<workspace-id>/workspace` |
| `OAR_SCHEMA_PATH` | Path to shared schema |
| `OAR_CORE_INSTANCE_ID` | Unique identifier for this workspace |
| `OAR_HUMAN_AUTH_MODE` | Set to `control_plane` for SaaS |

Control-plane integration:

| Variable | Purpose |
|---|---|
| `OAR_CONTROL_PLANE_BASE_URL` | Control plane loopback URL |
| `OAR_CONTROL_PLANE_HEARTBEAT_INTERVAL` | Heartbeat frequency, default `30s` |
| `OAR_CONTROL_PLANE_TOKEN_ISSUER` | Must match control-plane issuer |
| `OAR_CONTROL_PLANE_TOKEN_AUDIENCE` | Must match control-plane audience |
| `OAR_CONTROL_PLANE_WORKSPACE_ID` | Workspace identifier known to control plane |
| `OAR_CONTROL_PLANE_TOKEN_PUBLIC_KEY` | Base64 Ed25519 public key from control plane |
| `OAR_WORKSPACE_SERVICE_ID` | Service identity for this workspace |
| `OAR_WORKSPACE_SERVICE_PRIVATE_KEY` | Private key for signing heartbeats |

See [`../deploy/env/packed-host/workspace-instance.env.example`](../deploy/env/packed-host/workspace-instance.env.example) for a complete template.

## Blob backend configuration

Default: filesystem blobs. PMF recommendation is to start here.

| Variable | Purpose |
|---|---|
| `OAR_BLOB_BACKEND` | `filesystem` (default) or `s3` |

Filesystem backend stores blobs under `<workspace-root>/artifacts/content/`. No additional configuration required.

S3-compatible backend requires:

| Variable | Purpose |
|---|---|
| `OAR_BLOB_S3_BUCKET` | Bucket name |
| `OAR_BLOB_S3_PREFIX` | Prefix within bucket for this workspace |
| `OAR_BLOB_S3_REGION` | Region or `auto` for R2 |
| `OAR_BLOB_S3_ENDPOINT` | Custom endpoint for R2, MinIO, etc. |
| `OAR_BLOB_S3_ACCESS_KEY_ID` | Access key |
| `OAR_BLOB_S3_SECRET_ACCESS_KEY` | Secret key |
| `OAR_BLOB_S3_SESSION_TOKEN` | Optional for temporary credentials |
| `OAR_BLOB_S3_FORCE_PATH_STYLE` | `true` for path-style requests |

See [`blob-backend-operations.md`](blob-backend-operations.md) for operational guidance.

## Projection maintenance configuration

Default: background maintenance.

| Variable | Purpose |
|---|---|
| `OAR_PROJECTION_MODE` | `background` (default) or `manual` |
| `OAR_PROJECTION_MAINTENANCE_INTERVAL` | Background loop interval |
| `OAR_PROJECTION_STALE_SCAN_INTERVAL` | Stale-thread scan interval |
| `OAR_PROJECTION_MAINTENANCE_BATCH_SIZE` | Batch size per loop iteration |

For maintenance windows or operator-controlled rebuilds, set `OAR_PROJECTION_MODE=manual`. Writes still queue dirty projections, but the background loop stays off. Use `POST /derived/rebuild` or hosted helper scripts to clear the backlog.

See [`projection-maintenance.md`](projection-maintenance.md) for operational guidance.

## Workspace heartbeat expectations

When control-plane integration is configured, each workspace core sends signed heartbeats:

- Interval: `OAR_CONTROL_PLANE_HEARTBEAT_INTERVAL` (default 30s)
- Payload includes: version, build, health summary, projection status, usage summary, last backup timestamp
- Failure policy: log and retry, do not take down the workspace

The control plane records:
- `last_heartbeat_at`
- health and projection summaries
- usage envelope
- last successful backup timestamp when standard backup manifests exist

Use control-plane diagnostics to detect stalled or unhealthy workspaces.

## Security hardening

Production requirements:

- All OAR services bind to loopback only
- Caddy is the only public listener
- `OAR_ALLOW_UNAUTHENTICATED_WRITES=false`
- `OAR_ENABLE_DEV_ACTOR_MODE=false`
- Bootstrap tokens cleared after first principal registration
- Private keys not world-readable
- Reserved workspace slugs enforced by control plane

Reserved slugs (cannot be used as workspace slugs in SaaS):
- `auth`
- `dashboard`
- `invites`
- `control`
- `api`
- `favicon.ico`
- `robots.txt`

## Next steps

- Deployment: [`../deploy/linux-packed-host.md`](../deploy/linux-packed-host.md)
- Launch checklist: [`packed-host-launch-checklist.md`](packed-host-launch-checklist.md)
- Backup/restore: [`packed-host-backup-restore.md`](packed-host-backup-restore.md)
