# Linux packed-host deployment

This runbook is for the recommended PMF-friendly SaaS shape:

- one shared control plane
- one shared web UI
- many isolated workspace cores
- one Linux host first

It assumes the implementation tickets in this pack are complete.

Related docs:
- Architecture: [`../docs/architecture/saas-packed-host-v1.md`](../docs/architecture/saas-packed-host-v1.md)
- Configuration: [`../runbooks/packed-host-configuration.md`](../runbooks/packed-host-configuration.md)
- Launch checklist: [`../runbooks/packed-host-launch-checklist.md`](../runbooks/packed-host-launch-checklist.md)
- Backup/restore: [`../runbooks/packed-host-backup-restore.md`](../runbooks/packed-host-backup-restore.md)
- Blob backends: [`../runbooks/blob-backend-operations.md`](../runbooks/blob-backend-operations.md)
- Projection maintenance: [`../runbooks/projection-maintenance.md`](../runbooks/projection-maintenance.md)

## Target host layout

```text
/opt/oar/
  bin/
    oar-core
    oar-control-plane
  share/
    oar-schema.yaml
    meta/commands.json
  web-ui/
    build/
  scripts/
    hosted/

/etc/oar/
  control-plane.env
  web-ui.env
  workspaces/
    ws_123.env
    ws_456.env

/var/lib/oar/
  control-plane/
  workspaces/
    ws_123/
    ws_456/
```

## Services

- `oar-control-plane.service`
- `oar-web-ui.service`
- `oar-core@<workspace-id>.service`
- `caddy.service`

All OAR services bind to loopback. Caddy is the only public listener.

## Recommended first-production defaults

- OS: Ubuntu LTS
- Reverse proxy: Caddy
- Workspace DBs: local SQLite with WAL
- Blob backend: `filesystem` first
- Public hostname: one shared hostname, e.g. `app.example.com`
- Workspace paths: `/<workspace-slug>/...`
- Control-plane UI paths: `/dashboard`, `/auth`, `/invites`, `/control/...`

Because workspace slugs live at the top level, the control plane must reserve shipped UI route names.

## Install binaries and assets

Build from the repo and install to `/opt/oar`:

```bash
sudo mkdir -p /opt/oar/bin /opt/oar/share/meta /opt/oar/web-ui /opt/oar/scripts
sudo cp core/.bin/oar-core /opt/oar/bin/oar-core
sudo cp core/.bin/oar-control-plane /opt/oar/bin/oar-control-plane
sudo cp contracts/oar-schema.yaml /opt/oar/share/oar-schema.yaml
sudo cp contracts/gen/meta/commands.json /opt/oar/share/meta/commands.json
sudo rsync -a web-ui/build/ /opt/oar/web-ui/build/
sudo rsync -a scripts/hosted/ /opt/oar/scripts/hosted/
```

Adjust to your own packaging process if you build OS packages or use release artifacts.

## Base directories

```bash
sudo mkdir -p /etc/oar/workspaces
sudo mkdir -p /var/lib/oar/control-plane
sudo mkdir -p /var/lib/oar/workspaces
sudo chown -R oar:oar /etc/oar /var/lib/oar /opt/oar
```

## Control plane env

Copy `deploy/env/packed-host/control-plane.env.example` to `/etc/oar/control-plane.env` and fill in:

- WebAuthn origin and RP ID
- workspace URL template
- workspace grant signing key
- local packed-host placement defaults

## Web UI env

Copy `deploy/env/packed-host/web-ui.env.example` to `/etc/oar/web-ui.env` and fill in:

- `ORIGIN=https://app.example.com`
- `OAR_CONTROL_BASE_URL=http://127.0.0.1:8100`

Do not rely on static `OAR_WORKSPACES` in SaaS mode after the dynamic-routing ticket lands. Keep that variable for self-host or fallback-only use.

## Systemd units

Install the shipped units:

```bash
sudo cp deploy/systemd/oar-control-plane.service /etc/systemd/system/
sudo cp deploy/systemd/oar-web-ui.service /etc/systemd/system/
sudo cp deploy/systemd/oar-core@.service /etc/systemd/system/
sudo systemctl daemon-reload
```

Start shared services:

```bash
sudo systemctl enable --now oar-control-plane
sudo systemctl enable --now oar-web-ui
```

## Caddy

Install the example config:

```bash
sudo cp deploy/caddy/Caddyfile.packed-host.example /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

The shared web UI is the only public origin. Workspace cores and the control plane stay on loopback.

## Provision one workspace

Use the helper script to create the workspace root and env file:

```bash
sudo scripts/hosted/provision-packed-workspace.sh   --workspace-id ws_example   --workspace-slug example   --workspace-root /var/lib/oar/workspaces/ws_example   --env-file /etc/oar/workspaces/ws_example.env   --listen-port 18001   --public-origin https://app.example.com   --control-plane-workspace-id ws_example   --control-plane-base-url http://127.0.0.1:8100   --control-plane-token-issuer https://app.example.com   --control-plane-token-audience oar-core   --control-plane-token-public-key REPLACE_ME   --workspace-service-id svc_ws_example   --workspace-service-private-key REPLACE_ME   --enable
```

Then verify:

```bash
sudo systemctl status oar-core@ws_example
curl -fsS http://127.0.0.1:18001/readyz
```

Projection maintenance defaults to `OAR_PROJECTION_MODE=background`. If a
packed host should rely on operator-driven rebuilds instead, change the
workspace env file to `OAR_PROJECTION_MODE=manual`, restart the instance, and
use the hosted helper with an authenticated bearer token when you need to flush
queued projection work:

```bash
sudo systemctl restart oar-core@ws_example
OAR_CORE_BASE_URL=http://127.0.0.1:18001 \
OAR_AUTH_TOKEN=REPLACE_WITH_WORKSPACE_TOKEN \
scripts/hosted/rebuild-derived.sh --actor-id operator_ws_example
```

## Heartbeats

Packed-host workspace env files should include:

- `OAR_CONTROL_PLANE_BASE_URL`
- `OAR_CONTROL_PLANE_HEARTBEAT_INTERVAL`
- `OAR_CONTROL_PLANE_WORKSPACE_ID`
- `OAR_WORKSPACE_SERVICE_ID`
- `OAR_WORKSPACE_SERVICE_PRIVATE_KEY`

When those settings are present, `oar-core` sends background signed heartbeats
directly to the control plane and retries on failures without taking the
workspace down. Verify in the control plane:

- `last_heartbeat_at`
- health summary
- projection maintenance summary
- usage summary
- `last_successful_backup_at` when standard hosted backup manifests are present

## Backups

Minimum production expectation:

- nightly workspace backups
- regular restore drills
- at least one recent verified restore per release train

Filesystem blobs:
- back up SQLite + the hosted bundle's copied local blob store + metadata

S3-compatible blobs:
- back up SQLite + the hosted manifest/receipt material and keep operator
  ownership of the referenced bucket/prefix state
- if the bundle does not include inline S3 credentials, restore drills must run
  with ambient AWS-compatible credentials or instance identity on the target

## Restore drills

A deployment is not production-ready until a restore drill has been run on the same host shape. Use the hosted restore scripts and record:

- restore source
- restore destination
- restore verification result
- active blob backend and effective blob location
- operator date/time

## When to add a second host

Add a second packed host only when one of these is true:

- memory pressure becomes persistent
- backups or restore drills become too slow
- noisy-neighbor issues become real
- one-host blast radius is no longer acceptable

Do not add orchestration layers earlier than necessary.
