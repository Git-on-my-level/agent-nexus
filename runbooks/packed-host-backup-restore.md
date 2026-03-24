# Packed-host backup and restore

This runbook covers backup and restore procedures for the packed-host SaaS shape.

For hosted-v1 single-workspace operations, see [`../deploy/managed-hosting.md`](../deploy/managed-hosting.md).

## PMF expectations

Minimum production expectations:

- Nightly workspace backups
- Regular restore drills
- At least one verified restore per release train
- Recorded restore drill results

A deployment is not production-ready until a restore drill has been verified on the same host shape.

## Backup procedures

### Filesystem blob backend

Backups include:
- SQLite database
- Copied blob content from `<workspace-root>/artifacts/content/`
- Backup manifest and checksums

Using hosted scripts:

```bash
./scripts/hosted/backup-workspace.sh \
  --instance-root /var/lib/oar/workspaces/ws_example \
  --output-dir /var/backups/oar/ws_example-$(date -u +%Y%m%dT%H%M%SZ)
```

Bundle contents:
- `manifest.env` - metadata, backend info, counts
- `SHA256SUMS` - integrity checksums
- `workspace/state.sqlite` - database backup
- `workspace/blob-store/` - copied blob content
- `metadata/` - instance metadata if present

By default, `config/env.production` is NOT included (no secrets in bundle).

For secret-inclusive backups:

```bash
./scripts/hosted/backup-workspace.sh \
  --instance-root /var/lib/oar/workspaces/ws_example \
  --output-dir /var/backups/oar/ws_example-with-secrets \
  --include-config-secrets
```

WARNING: Secret-inclusive bundles contain live credentials. Handle with same care as source deployment.

### S3 blob backend

Backups include:
- SQLite database
- Metadata referencing bucket/prefix (not object copies)
- Backup manifest

The S3 backup is a reference, not an independent object snapshot. Restore configures the target workspace to read from the same bucket/prefix.

```bash
./scripts/hosted/backup-workspace.sh \
  --instance-root /var/lib/oar/workspaces/ws_example \
  --output-dir /var/backups/oar/ws_example-s3-ref
```

Manifest records:
- `OAR_BLOB_BACKEND=s3`
- Bucket, prefix, region, endpoint
- Whether credentials are included

If credentials are not included, restore requires ambient AWS-compatible credentials on target.

## Restore procedures

### Restore to new target (recommended)

Default behavior: restore refuses non-empty targets.

```bash
./scripts/hosted/restore-workspace.sh \
  --backup-dir /var/backups/oar/ws_example-20260324T020000Z \
  --target-instance-root /var/lib/oar/workspaces/ws_example_restored
```

### Restore with force

To overlay onto existing target:

```bash
./scripts/hosted/restore-workspace.sh \
  --backup-dir /var/backups/oar/ws_example-20260324T020000Z \
  --target-instance-root /var/lib/oar/workspaces/ws_example \
  --force
```

WARNING: Overlays backup-managed paths only. Does not delete backup source. Use with caution on production targets.

### Restore verification

Always verify before directing traffic to restored workspace:

```bash
./scripts/hosted/verify-restore.sh \
  --instance-root /var/lib/oar/workspaces/ws_example_restored \
  --core-bin /opt/oar/bin/oar-core \
  --schema-path /opt/oar/share/oar-schema.yaml
```

Verification checks:
- `GET /readyz` succeeds
- Artifact, agent, invite, document counts match manifest
- Live blob reads succeed through active backend
- Local-backend blob counts match

For S3 backends, verification uses ambient or configured credentials to read remote objects.

## Restore drill expectations

Each restore drill should record:

| Field | Example |
|---|---|---|
| Restore source | `/var/backups/oar/ws_example-20260324T020000Z` |
| Restore destination | `/var/lib/oar/workspaces/ws_example_drill` |
| Verification result | PASS / FAIL with details |
| Active blob backend | `filesystem` or `s3://bucket/prefix` |
| Operator date/time | `2026-03-24T10:30:00Z` |
| Performed by | Operator name or system |

Keep drill records for audit and release-train verification.

## Control-plane integration

The control plane records `last_successful_backup_at` when:
- Workspace heartbeats are enabled
- Standard hosted backup manifests are discoverable near workspace root

This enables fleet-wide backup monitoring from control-plane diagnostics.

To expose backup timestamps:
1. Use hosted backup scripts
2. Ensure backup output is under or near workspace root
3. Verify heartbeat payload includes backup timestamp

## Scheduling

Packed-host does not ship a backup scheduler. Options:

- systemd timers
- cron jobs
- External orchestration (Ansible, etc.)

Example systemd timer:

```ini
# /etc/systemd/system/oar-backup@.timer
[Unit]
Description=Daily backup for workspace %i

[Timer]
OnCalendar=daily
Persistent=true

[Install]
WantedBy=timers.target
```

```ini
# /etc/systemd/system/oar-backup@.service
[Unit]
Description=Backup workspace %i

[Service]
Type=oneshot
ExecStart=/opt/oar/scripts/hosted/backup-workspace.sh \
  --instance-root /var/lib/oar/workspaces/%i \
  --output-dir /var/backups/oar/%i
```

Enable:

```bash
sudo systemctl enable --now oar-backup@ws_example.timer
```

## Disaster recovery

Per-workspace DR flow:

1. Identify affected workspace
2. Stop workspace core if running
3. Restore from most recent verified backup
4. Verify restore
5. Update workspace env if credentials or endpoints changed
6. Start workspace core
7. Verify heartbeat reaches control plane
8. Direct traffic to restored workspace

Cross-workspace disaster (host failure):

1. Provision new host
2. Install binaries and assets
3. Restore each workspace from backups
4. Verify each restore
5. Update control-plane placement metadata
6. Direct traffic to new host

## Retention

Packed-host does not ship retention policy. Recommendations:

- Keep at least 7 daily backups
- Keep at least 4 weekly backups
- Keep at least 12 monthly backups
- Adjust based on RPO requirements

For S3 backends, consider bucket lifecycle policies for blob retention separate from SQLite backups.

## Related docs

- Configuration: [`packed-host-configuration.md`](packed-host-configuration.md)
- Blob backends: [`blob-backend-operations.md`](blob-backend-operations.md)
- Launch checklist: [`packed-host-launch-checklist.md`](packed-host-launch-checklist.md)
- Linux deployment: [`../deploy/linux-packed-host.md`](../deploy/linux-packed-host.md)
