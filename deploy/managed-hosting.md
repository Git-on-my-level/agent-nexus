# Managed Hosted-v1 Operations

Hosted v1 is a managed offering built from one isolated deployment root per
workspace. This document is the operator runbook for that model.

It is intentionally not a self-service control plane. Provisioning, bootstrap,
backup, restore, and restore verification are all operator-driven steps.

## Hosted v1 model

Hosted v1 is:

- One isolated deployment per workspace/customer
- Operator-driven provisioning and lifecycle
- No shared control plane
- Workspace-local auth for both humans and agents
- Manual backup scheduling and DR drills
- No fine-grained RBAC (authenticated principals share authority)

## What the bundle does

The hosted ops bundle lives under `scripts/hosted/`:

- `provision-workspace.sh`: scaffold one deployment root with `workspace/`,
  `config/env.production`, `metadata/instance.env`, and `backups/`
- `backup-workspace.sh`: create a portable backup bundle with a manifest,
  SQLite backup, backend-aware blob copy/reference metadata, and checksums
- `restore-workspace.sh`: restore a bundle into an empty target by default
- `verify-restore.sh`: start `oar-core` against the restored workspace on a
  loopback port, verify `/readyz`, and validate live blob reads through the
  restored backend config
- `smoke-test.sh`: local end-to-end provision → backup → restore → verify path

Minimum operator dependencies:

- `bash`
- `curl`
- `sqlite3`
- `sha256sum` or `shasum`
- a built `oar-core` binary for `verify-restore.sh`

## Deployment root layout

Provisioning creates one isolated deployment root:

```text
/srv/oar/team-alpha/
├── workspace/
│   ├── artifacts/content/
│   ├── logs/
│   └── tmp/
├── config/
│   └── env.production
├── metadata/
│   └── instance.env
└── backups/
```

`workspace/` is the durable state passed to `oar-core`. `config/` and
`metadata/` are the operator-facing configuration and recovery hints that the
backup/restore flow carries forward.

## First deployment

### 1. Provision the deployment root

```bash
./scripts/hosted/provision-workspace.sh \
  --instance team-alpha \
  --instance-root /srv/oar/team-alpha \
  --public-origin https://team-alpha.oar.example.com \
  --listen-port 8001 \
  --web-ui-port 3001 \
  --generate-bootstrap-token
```

This validates the instance name, host port, and public origin, then writes:

- `/srv/oar/team-alpha/config/env.production`
- `/srv/oar/team-alpha/metadata/instance.env`
- the empty workspace directory structure required by core

If you do not pass `--generate-bootstrap-token` or `--bootstrap-token`, the
env file is written with a secure placeholder and bootstrap onboarding remains
disabled until you replace it.

### 2. Start the stack

Docker Compose example from the repo root:

```bash
docker compose --env-file /srv/oar/team-alpha/config/env.production up -d
```

The generated env file sets:

- `HOST_OAR_WORKSPACE_ROOT` for a bind-mounted workspace
- `OAR_CORE_INSTANCE_ID`
- `OAR_BOOTSTRAP_TOKEN`
- `OAR_BLOB_BACKEND` plus either the effective local blob root or the active
  S3 bucket/prefix settings
- WebAuthn origin/RP values for the workspace domain

For source-run or launchd deployments, use the same values from
`config/env.production` when configuring the process.

## Reverse proxy edge limits

Core already enforces request-size limits, workspace quotas, and in-process
route-class throttles. The reverse proxy should add complementary edge limits
so abusive traffic is rejected before it reaches the workspace instance.

Example nginx configuration:

```nginx
http {
  limit_req_zone $binary_remote_addr zone=oar_auth:10m rate=30r/m;
  limit_req_zone $binary_remote_addr zone=oar_write:10m rate=300r/m;

  server {
    location /auth/ {
      limit_req zone=oar_auth burst=10 nodelay;
      proxy_pass http://127.0.0.1:8001;
    }

    location ~ ^/(threads|topics|cards|boards|docs|artifacts|events|receipts|reviews|inbox/ack|derived/rebuild) {
      limit_req zone=oar_write burst=100 nodelay;
      proxy_pass http://127.0.0.1:8001;
    }
  }
}
```

If the edge limit trips, clients should see `429` responses before the core
workload is consumed. Core still returns explicit `request_too_large`,
`workspace_quota_exceeded`, and `rate_limited` payloads when requests reach it.

### 3. Confirm the empty deployment is healthy

```bash
curl -fsS http://127.0.0.1:8001/readyz
curl -fsS http://127.0.0.1:8001/auth/bootstrap/status
```

Before the first principal is created, `bootstrap_registration_available`
should be `true`.

## Bootstrap onboarding

Hosted v1 is not open registration. The first principal must use the bootstrap
token configured for that deployment.

For a deterministic operator flow, bootstrap an agent principal first:

```bash
export OAR_BOOTSTRAP_TOKEN="$(grep '^OAR_BOOTSTRAP_TOKEN=' /srv/oar/team-alpha/config/env.production | cut -d= -f2-)"

curl -fsS \
  -H 'content-type: application/json' \
  -X POST \
  -d '{
    "username": "team-alpha.bootstrap",
    "public_key": "<base64-ed25519-public-key>",
    "bootstrap_token": "'"${OAR_BOOTSTRAP_TOKEN}"'"
  }' \
  http://127.0.0.1:8001/auth/agents/register
```

That response includes a bearer token and refresh token for the first
authenticated principal. After bootstrap succeeds, the bootstrap token is no
longer accepted for future registrations.

If your first principal is a human passkey user instead, use the same bootstrap
token through the web-ui passkey registration flow once the workspace origin is
live.

## Invite issuance after bootstrap

After the first principal exists, all subsequent principals enter through
invite-gated onboarding.

Issue an invite with the bearer token from the bootstrap principal:

```bash
export ACCESS_TOKEN="<bootstrap-access-token>"

curl -fsS \
  -H "authorization: Bearer ${ACCESS_TOKEN}" \
  -H 'content-type: application/json' \
  -X POST \
  -d '{"kind":"human","note":"initial operator invite"}' \
  http://127.0.0.1:8001/auth/invites
```

Use `kind:"agent"` for CLI/agent onboarding. Hosted v1 has no fine-grained
RBAC layer, so any authenticated principal can issue and revoke invites.

## Routine backup

Create one backup bundle per deployment:

```bash
./scripts/hosted/backup-workspace.sh \
  --instance-root /srv/oar/team-alpha \
  --output-dir /var/backups/oar/team-alpha-$(date -u +%Y%m%dT%H%M%SZ)
```

The backup bundle contains:

- `manifest.env`
- `SHA256SUMS`
- `workspace/state.sqlite`
- `workspace/blob-store/` when the active blob backend is local (`filesystem`
  or `object`)
- explicit remote blob reference metadata in `manifest.env` when the active
  backend is `s3`
- `metadata/` if present

By default, `config/env.production` is **not** included in the backup bundle. This
makes the default backup safer to store, transfer, and share because it contains
no deployment secrets (bootstrap tokens, etc.).

The manifest records whether config was included via `CONFIG_INCLUDED` and
`CONFIG_ENV_PATH` fields, making it unambiguous whether a bundle contains
secrets. It also records the active blob backend, effective blob location,
bundle mode (`copy` vs `reference`), and S3 storage parameters when relevant.

For `OAR_BLOB_BACKEND=s3`, the default backup path is intentionally a remote
reference, not a second independent object snapshot. The manifest tells the
operator exactly which bucket/prefix the restored workspace will read from.
If inline S3 credentials are not included in the bundle, restore verification
relies on ambient AWS-compatible credentials or instance identity on the target.

### Secret-inclusive backups

If you need a self-contained bundle that includes deployment secrets:

```bash
./scripts/hosted/backup-workspace.sh \
  --instance-root /srv/oar/team-alpha \
  --output-dir /var/backups/oar/team-alpha-with-secrets-$(date -u +%Y%m%dT%H%M%SZ) \
  --include-config-secrets
```

WARNING: This creates a bundle containing `config/env.production` with live
secrets. Handle secret-inclusive bundles with the same care as the source
deployment.

The SQLite copy is produced with `sqlite3 .backup`, so online backups remain
boring and predictable.

## Restore drill

Restore into a new target root by default:

```bash
./scripts/hosted/restore-workspace.sh \
  --backup-dir /var/backups/oar/team-alpha-20260319T020000Z \
  --target-instance-root /srv/oar/team-alpha-restore-drill
```

The restore script refuses non-empty targets unless you pass `--force`. Even
with `--force`, it overlays backup-managed paths only and never deletes the
backup source.

Verify the restored workspace before directing real traffic at it:

```bash
./core/scripts/build-prod

./scripts/hosted/verify-restore.sh \
  --instance-root /srv/oar/team-alpha-restore-drill \
  --core-bin ./core/.bin/oar-core \
  --schema-path ./contracts/oar-schema.yaml
```

Verification checks:

- `GET /readyz` succeeds against a loopback-only temporary server
- artifact, agent, invite, and document counts still match the backup manifest
- live artifact/document blob reads succeed through the active backend config
- local-backend restores still match the copied blob object count from the
  manifest

Use `GET /ops/health` only for authenticated or loopback-only operator diagnostics such as projection-maintenance lag.

## Disaster recovery expectations

Hosted v1 disaster recovery is per-workspace and operator-driven:

- recover one isolated deployment root at a time
- restore into a fresh target root whenever practical
- verify the restore before switching proxy or DNS traffic
- keep backup bundles portable enough to move between hosts

This ticket pack does not automate traffic cutover, secret distribution, DNS,
TLS issuance, scheduled backup orchestration, or cross-workspace fleet control.

## What is and is not automated in hosted v1

Automated by the bundle:

- deployment-root scaffolding
- env/metadata scaffolding
- SQLite backup creation
- local-backend blob copying and checksum emission
- backend-aware manifest generation for local and S3 blob stores
- restore guardrails on unsafe targets
- restore verification on a temporary loopback server

Not automated in hosted v1:

- reverse proxy provisioning
- DNS and certificate management
- secret escrow or HSM integration
- backup scheduling and retention policy
- independent S3 object snapshotting or bucket/prefix migration
- invite delivery to end users
- any self-service tenant control plane

