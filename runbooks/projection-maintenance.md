# Projection maintenance

This runbook covers projection maintenance modes and operational procedures.

## Background

Derived projections (thread views, board workspaces, etc.) are convenience reads derived from canonical state. They are not durable truth but must stay consistent with the event log.

Two maintenance modes:

| Mode | Behavior | Use case |
|---|---|---|
| `background` | Async worker rebuilds dirty projections | Default, most workloads |
| `manual` | Writes queue dirty work, no auto-rebuild | Maintenance windows, operator control |

## Background mode

Default: `OAR_PROJECTION_MODE=background`

Configuration:

| Variable | Purpose | Default |
|---|---|---|
| `OAR_PROJECTION_MAINTENANCE_INTERVAL` | Loop tick | `5s` |
| `OAR_PROJECTION_STALE_SCAN_INTERVAL` | Full stale scan | `30s` |
| `OAR_PROJECTION_MAINTENANCE_BATCH_SIZE` | Projections per tick | `50` |

Behavior:
- Writes queue dirty projection records
- Background loop picks up dirty work every interval
- Stale scan catches projections that missed incremental updates
- Eventual consistency: projections may lag briefly behind canonical state

Health monitoring via `/ops/health`:

```json
{
  "projection_maintenance": {
    "mode": "background",
    "pending_dirty_count": 3,
    "oldest_dirty_at": "2026-03-24T10:00:00Z",
    "oldest_dirty_lag_seconds": 12,
    "last_successful_stale_scan_at": "2026-03-24T10:05:00Z",
    "last_error": null
  }
}
```

Key indicators:
- `pending_dirty_count`: backlog size
- `oldest_dirty_lag_seconds`: how far behind the oldest dirty projection is
- `last_error`: most recent failure, if any

Normal operation:
- `pending_dirty_count` fluctuates but trends to zero
- `oldest_dirty_lag_seconds` stays low (single-digit seconds)
- `last_error` is null

Warning signs:
- `pending_dirty_count` grows unbounded
- `oldest_dirty_lag_seconds` exceeds maintenance interval significantly
- `last_error` is set repeatedly

## Manual mode

Enable with `OAR_PROJECTION_MODE=manual`.

Behavior:
- Writes still queue dirty projection records
- Background loop is disabled
- Projections stay stale until explicit rebuild

Use cases:
- Planned maintenance windows
- Debugging projection logic
- Operator-controlled rebuild timing
- High-write periods where async catch-up is deferred

Explicit rebuild:

```bash
curl -X POST http://127.0.0.1:8001/derived/rebuild \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

Or using hosted helper:

```bash
OAR_CORE_BASE_URL=http://127.0.0.1:18001 \
OAR_AUTH_TOKEN=$ACCESS_TOKEN \
scripts/hosted/rebuild-derived.sh --actor-id operator_ws_example
```

The rebuild endpoint:
- Processes all queued dirty projections
- Returns when backlog is cleared
- Safe to call repeatedly

Switching modes at runtime requires restart:

```bash
# Update env file
sed -i 's/OAR_PROJECTION_MODE=background/OAR_PROJECTION_MODE=manual/' /etc/oar/workspaces/ws_example.env

# Restart workspace core
sudo systemctl restart oar-core@ws_example
```

## Diagnostic endpoints

### GET /ops/health

Returns projection maintenance status plus readiness.

Loopback-only or authenticated access recommended.

### POST /derived/rebuild

Triggers immediate rebuild of all dirty projections.

Returns rebuild summary:

```json
{
  "rebuilt_count": 42,
  "error_count": 0,
  "duration_ms": 150
}
```

### GET /ops/usage-summary

Workspace usage envelope including document counts.

Not projection-specific but useful for overall health.

## Common scenarios

### Catching up after high-write burst

If `pending_dirty_count` is high but not growing:

1. Confirm `mode=background`
2. Wait for async catch-up
3. Monitor `oldest_dirty_lag_seconds`

If lag is unacceptable:

1. Trigger explicit rebuild: `POST /derived/rebuild`
2. Or temporarily increase batch size

### Projection stuck after error

If `last_error` is set and projections are not advancing:

1. Check error message in logs
2. Identify root cause (schema mismatch, data corruption, etc.)
3. Fix underlying issue
4. Trigger explicit rebuild

### Maintenance window

Before maintenance that modifies canonical state:

1. Switch to manual mode
2. Perform maintenance
3. Trigger explicit rebuild
4. Switch back to background mode
5. Verify catch-up

### After restore

Restore verification includes projection rebuild.

If manual verification is needed after restore:

```bash
./scripts/hosted/verify-restore.sh \
  --instance-root /srv/oar/workspace-restore-drill \
  --core-bin ./core/.bin/oar-core \
  --schema-path ./contracts/oar-schema.yaml
```

## Performance tuning

High-write workloads may benefit from:

- Lower `OAR_PROJECTION_MAINTENANCE_INTERVAL` (more frequent ticks)
- Higher `OAR_PROJECTION_MAINTENANCE_BATCH_SIZE` (more work per tick)
- Lower `OAR_PROJECTION_STALE_SCAN_INTERVAL` (more aggressive stale detection)

Trade-offs:
- Lower intervals = more CPU, faster catch-up
- Higher batch size = longer tick duration, more memory per tick
- Lower stale scan interval = more background work, fewer missed projections

Monitor `/ops/health` after tuning. Target is steady-state with `pending_dirty_count` near zero.

## Relation to heartbeats

Workspace heartbeats include projection maintenance summary:

- mode
- pending_dirty_count
- oldest_dirty_lag_seconds

Control-plane diagnostics can surface unhealthy workspaces with stale projections.

See [`packed-host-configuration.md`](packed-host-configuration.md) for heartbeat configuration.
