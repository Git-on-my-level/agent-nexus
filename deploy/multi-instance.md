# Multi-Instance Deployment on macOS (No Docker)

This document describes the managed hosted-v1 operating model on one Mac host:
one isolated workspace deployment per instance, provisioned by operators. It is
not a self-service control plane, and it does not introduce shared row-level
multitenancy.

Run N independent `oar-core` instances on a single Mac host, each with its own
workspace, port, and process supervision via `launchd`. A reverse proxy
(Caddy, nginx, etc.) fronts them with TLS and routing.

## Hosted-v1 cut line

- Each instance is one isolated workspace deployment.
- Provisioning is managed by operators with install/bootstrap scripts.
- Backup and restore happen per workspace instance.
- Hosted-v1 onboarding is token-gated, not public signup. Use the shipped
  bootstrap and invite-gated registration flow when provisioning principals for
  a workspace instance.
- A future control plane may automate this later, but it is not required to run
  hosted v1.

## Prerequisites

- macOS host with a Go toolchain (for building from source)
- The `organization-autorunner` repo cloned on the host (or a pre-built binary
  transferred to it)
- A reverse proxy for TLS termination and routing

## Architecture

```
                    ┌─────────────────┐
        HTTPS       │  Reverse Proxy  │
  ─────────────────►│  (Caddy/nginx)  │
                    └──┬──────────┬───┘
           :8001 ◄─────┘          └─────► :8002
      ┌────────────┐            ┌────────────┐
      │  oar-core   │            │  oar-core   │
      │  instance-a │            │  instance-b │
      │  workspace/ │            │  workspace/ │
      │   state.db  │            │   state.db  │
      └────────────┘            └────────────┘
```

Each instance has:
- Its own port (bound to `127.0.0.1`)
- Its own workspace directory (SQLite DB + artifact files)
- Its own `launchd` plist for process supervision
- Its own log files

Instances share a single binary and schema assets, but not state.

## Quick Start

### 1. Install binary + assets

From the repo root:

```bash
./scripts/install-oar-core.sh --prefix ~/.oar
```

This builds the binary and copies it along with schema assets to:

```
~/.oar/
├── bin/oar-core
├── share/oar-schema.yaml
├── share/meta/commands.json
├── logs/
└── workspaces/
```

### 2. Create instances

```bash
# Instance A on port 8001
./scripts/install-oar-core.sh --skip-build \
  --instance team-alpha --port 8001 --load

# Instance B on port 8002
./scripts/install-oar-core.sh --skip-build \
  --instance team-beta --port 8002 --load
```

Each `--instance` call:
1. Creates the workspace directory at `~/.oar/workspaces/<name>/`
2. Generates a launchd plist at `~/Library/LaunchAgents/com.oar.core.<name>.plist`
3. With `--load`, bootstraps the service immediately

### 3. Verify

```bash
curl -fsS http://127.0.0.1:8001/health
curl -fsS http://127.0.0.1:8002/health

# Check handshake metadata (includes core_instance_id)
curl -fsS http://127.0.0.1:8001/meta/handshake | jq .core_instance_id
```

## Instance Management

### Add an instance

```bash
./scripts/install-oar-core.sh --skip-build \
  --instance new-team --port 8003 --load
```

### Stop an instance

```bash
launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.oar.core.team-alpha.plist
```

### Start a stopped instance

```bash
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.oar.core.team-alpha.plist
```

### Remove an instance

```bash
./scripts/install-oar-core.sh --unload team-alpha
```

This stops the process and removes the plist. Workspace data is preserved at
`~/.oar/workspaces/team-alpha/` — delete it manually if no longer needed.

### List running instances

```bash
launchctl list | grep com.oar.core
```

### View logs

```bash
tail -f ~/.oar/logs/oar-core-team-alpha.err.log
tail -f ~/.oar/logs/oar-core-team-alpha.out.log
```

## Upgrading the Binary

Build once, restart all instances:

```bash
# Rebuild
./scripts/install-oar-core.sh --prefix ~/.oar

# Restart all instances (launchd restarts automatically on process exit)
for plist in ~/Library/LaunchAgents/com.oar.core.*.plist; do
  instance=$(basename "$plist" .plist | sed 's/com\.oar\.core\.//')
  echo "Restarting $instance…"
  launchctl bootout gui/$(id -u) "$plist" 2>/dev/null || true
  launchctl bootstrap gui/$(id -u) "$plist"
done
```

## Reverse Proxy Configuration

Each instance listens on `127.0.0.1:<port>` (plain HTTP). The proxy handles
TLS, routing, and public-facing concerns.

### Key requirements

| Concern | Detail |
|---|---|
| TLS termination | Proxy terminates TLS; core listens plain HTTP |
| Forwarded headers | Proxy must set `X-Forwarded-Proto` and `X-Forwarded-Host` |
| SSE streaming | Do not buffer SSE responses (core sets `X-Accel-Buffering: no`) |
| WebAuthn | Set `OAR_WEBAUTHN_RPID` / `OAR_WEBAUTHN_ORIGIN` if proxy hostname differs from core listen address |
| Health checks | Use `GET /health` for upstream health checking |

### Routing strategies

**Subdomain-based** (recommended for isolated tenants):

```
team-alpha.oar.example.com  →  127.0.0.1:8001
team-beta.oar.example.com   →  127.0.0.1:8002
```

Each instance gets its own `OAR_WEBAUTHN_RPID` matching the subdomain.

**Path-prefix based** (simpler DNS, but requires client path awareness):

```
oar.example.com/team-alpha/  →  127.0.0.1:8001
oar.example.com/team-beta/   →  127.0.0.1:8002
```

Requires a strip-prefix rewrite at the proxy since core routes don't have a
path prefix. All instances share one `OAR_WEBAUTHN_RPID`.

### Caddy example (subdomain routing)

```caddyfile
team-alpha.oar.example.com {
    reverse_proxy 127.0.0.1:8001 {
        flush_interval -1
        header_up X-Forwarded-Proto {scheme}
        header_up X-Forwarded-Host {host}
        health_uri /health
        health_interval 30s
    }
}

team-beta.oar.example.com {
    reverse_proxy 127.0.0.1:8002 {
        flush_interval -1
        header_up X-Forwarded-Proto {scheme}
        header_up X-Forwarded-Host {host}
        health_uri /health
        health_interval 30s
    }
}
```

`flush_interval -1` disables response buffering, which is required for SSE
streams (`/events/stream`, `/inbox/stream`).

### WebAuthn configuration

When instances are behind a proxy with different public hostnames, set WebAuthn
env vars per instance. The easiest way is to add `EnvironmentVariables` entries
to the launchd plist after generation:

```bash
# Edit the generated plist to add WebAuthn vars:
# In the <dict> under EnvironmentVariables, add:
#   OAR_WEBAUTHN_RPID     → team-alpha.oar.example.com
#   OAR_WEBAUTHN_ORIGIN   → https://team-alpha.oar.example.com
```

Alternatively, core can derive these from `X-Forwarded-Host` at request time
if the proxy sends the correct headers. Explicit configuration is more
predictable.

## Environment Variables Per Instance

The launchd plist template passes core configuration through CLI flags.
To set additional env vars (CORS, WebAuthn, shutdown timeout, etc.), edit the
generated plist's `EnvironmentVariables` dict. Example additions:

```xml
<key>EnvironmentVariables</key>
<dict>
    <key>OAR_ALLOW_UNAUTHENTICATED_WRITES</key>
    <string>false</string>
    <key>OAR_WEBAUTHN_RPID</key>
    <string>team-alpha.oar.example.com</string>
    <key>OAR_WEBAUTHN_ORIGIN</key>
    <string>https://team-alpha.oar.example.com</string>
    <key>OAR_CORS_ALLOWED_ORIGINS</key>
    <string>https://team-alpha.oar.example.com</string>
</dict>
```

After editing a plist, reload the instance:

```bash
launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.oar.core.team-alpha.plist
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.oar.core.team-alpha.plist
```

## Data Isolation

Each instance has a fully independent workspace:

```
~/.oar/workspaces/team-alpha/
├── state.sqlite       # events, snapshots, artifacts metadata, actors
├── artifacts/content/  # artifact bytes
├── logs/
└── tmp/
```

There is no shared state between instances. Each has its own SQLite database,
actor registry, and artifact storage.

The hosted-v1 assumption is strict workspace isolation per deployment, not
shared row-level tenancy inside one database.

## Backup and Restore

The hosted-v1 backup/restore story is now standardized in the repo-local ops
bundle under [`scripts/hosted/`](../scripts/hosted/). Use that flow instead of
ad hoc `sqlite3` or `rsync` commands so each instance gets the same manifest,
checksum set, restore guardrails, and verification path.

Provision one deployment root:

```bash
./scripts/hosted/provision-workspace.sh \
  --instance team-alpha \
  --instance-root ~/.oar/team-alpha \
  --public-origin https://team-alpha.oar.example.com \
  --listen-port 8001 \
  --generate-bootstrap-token
```

Back it up:

```bash
./scripts/hosted/backup-workspace.sh \
  --instance-root ~/.oar/team-alpha \
  --output-dir /backups/team-alpha-$(date -u +%Y%m%dT%H%M%SZ)
```

Restore it:

```bash
./scripts/hosted/restore-workspace.sh \
  --backup-dir /backups/team-alpha-20260319T020000Z \
  --target-instance-root ~/.oar/team-alpha-restore-drill
```

Verify the restored workspace before cutover:

```bash
./core/scripts/build-prod

./scripts/hosted/verify-restore.sh \
  --instance-root ~/.oar/team-alpha-restore-drill \
  --core-bin ./core/.bin/oar-core \
  --schema-path ./contracts/oar-schema.yaml
```

This recovery model remains intentionally script-driven for hosted v1. A
separate control plane may orchestrate it later, but it is not part of the
current pack. For the end-to-end operator flow, see
[`deploy/managed-hosting.md`](./managed-hosting.md).

## Troubleshooting

### Instance won't start

Check the error log:

```bash
cat ~/.oar/logs/oar-core-team-alpha.err.log
```

Common issues:
- Port conflict: another instance or process is using the same port
- Missing schema: `oar-schema.yaml` not found at the installed path
- Permission denied on workspace directory

### Port conflict detection

```bash
lsof -i :8001
```

### launchd reports process as "not running"

Check exit code and throttling:

```bash
launchctl print gui/$(id -u)/com.oar.core.team-alpha
```

The plist sets `ThrottleInterval` to 5 seconds to prevent rapid restart loops.
If the process crashes immediately, check the error log for the root cause.

### Reset an instance workspace

```bash
launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.oar.core.team-alpha.plist
rm -rf ~/.oar/workspaces/team-alpha
mkdir -p ~/.oar/workspaces/team-alpha
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.oar.core.team-alpha.plist
```

Core will re-initialize the workspace on next startup.
