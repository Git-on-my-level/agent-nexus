# Blob backend operations

This runbook covers blob backend choices and operational procedures for Organization Autorunner.

## Backend choices

| Backend | Use case | Recommendation |
|---|---|---|
| `filesystem` | Local disk storage | Default for self-host and PMF packed-host |
| `s3` | S3-compatible object storage | Optional for off-host durability or storage expansion |

### Filesystem backend

Default behavior:
- Blobs stored under `<workspace-root>/artifacts/content/`
- Content-addressed layout: `<hash[:2]>/<hash[2:4]>/<hash>`
- No additional configuration required
- Blob bytes backed up with SQLite in standard backup flow

Advantages:
- Simple, no external dependencies
- Predictable performance
- Easy backup/restore with SQLite

Limitations:
- Storage bounded by local disk
- No built-in replication or durability beyond host

### S3-compatible backend

Enable with `OAR_BLOB_BACKEND=s3`.

Supports:
- AWS S3
- Cloudflare R2
- MinIO
- Any S3-compatible object store

Configuration:

```bash
OAR_BLOB_BACKEND=s3
OAR_BLOB_S3_BUCKET=oar-workspace-blobs
OAR_BLOB_S3_PREFIX=workspaces/ws_example/
OAR_BLOB_S3_REGION=auto
OAR_BLOB_S3_ENDPOINT=https://<account>.r2.cloudflarestorage.com
OAR_BLOB_S3_ACCESS_KEY_ID=<key-id>
OAR_BLOB_S3_SECRET_ACCESS_KEY=<secret>
OAR_BLOB_S3_FORCE_PATH_STYLE=true
```

For R2, set `OAR_BLOB_S3_REGION=auto` and use the R2 endpoint URL.

For MinIO or other path-style providers, set `OAR_BLOB_S3_FORCE_PATH_STYLE=true`.

Advantages:
- Off-host durability
- Storage expansion without local disk changes
- Potentially easier cross-host migration

Limitations:
- Additional operational surface (credentials, bucket lifecycle, etc.)
- Network latency on blob reads/writes
- Backup flow references remote objects instead of copying them

## Object layout

S3 backend uses the same content-addressed layout as filesystem:

```
bucket: <configured-bucket>
key: <prefix>/<hash[:2]>/<hash[2:4]>/<hash>
```

This layout:
- Matches filesystem backend structure
- Simplifies migration between backends
- Enables efficient deduplication

## Blob usage accounting

Blob usage is tracked in a workspace DB ledger, not by scanning the backend.

This means:
- Usage queries are fast, even with S3 backend
- No hot-path dependency on object-store list operations
- Ledger must be rebuilt after manual blob cleanup or backend drift

Rebuild the ledger:

```bash
curl -X POST http://127.0.0.1:8001/ops/blob-usage/rebuild \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

Response includes:
- `canonical_hash_count`: unique content hashes in DB
- `missing_blob_objects`: hashes referenced but missing in backend
- `blob_bytes` / `blob_objects`: rebuilt totals

Run rebuild after:
- Operator-initiated blob cleanup
- Direct backend manipulation
- Migration from one backend to another
- Suspected ledger drift

## Migration between backends

Migrating from filesystem to S3:

1. Stop the workspace core
2. Sync blob content to target bucket/prefix:

   ```bash
   aws s3 sync /var/lib/oar/workspaces/ws_example/artifacts/content/ \
     s3://oar-workspace-blobs/workspaces/ws_example/ \
     --endpoint-url https://<account>.r2.cloudflarestorage.com
   ```

3. Update workspace env: `OAR_BLOB_BACKEND=s3` plus S3 settings
4. Start the workspace core
5. Verify blob reads work
6. Rebuild blob usage ledger
7. Optionally remove local blob content after verification

Migrating from S3 to filesystem:

1. Stop the workspace core
2. Sync blob content to local:

   ```bash
   aws s3 sync s3://oar-workspace-blobs/workspaces/ws_example/ \
     /var/lib/oar/workspaces/ws_example/artifacts/content/ \
     --endpoint-url https://<account>.r2.cloudflarestorage.com
   ```

3. Update workspace env: `OAR_BLOB_BACKEND=filesystem`
4. Start the workspace core
5. Rebuild blob usage ledger

## Backup implications

Filesystem backend:
- Backup includes SQLite + copied blob content
- Restore replays local content into workspace blob root
- Self-contained backup bundle

S3 backend:
- Backup includes SQLite + metadata referencing bucket/prefix
- Restore configures target workspace to use same bucket/prefix
- Not a second independent object snapshot
- Operator owns bucket/prefix lifecycle separately

See [`packed-host-backup-restore.md`](packed-host-backup-restore.md) for backup/restore procedures.

## Troubleshooting

### Missing blobs after restore

If restore verification reports missing blobs:

1. Confirm backend configuration matches backup manifest
2. Check S3 credentials and endpoint
3. Verify bucket/prefix contains expected objects
4. Rebuild blob usage ledger to reconcile

### Blob quota issues

Blob quota is enforced via ledger, not backend scan.

If quota appears incorrect:

1. Check `/ops/usage-summary` for current totals
2. Run `POST /ops/blob-usage/rebuild` to reconcile
3. Verify no direct backend manipulation occurred

### S3 authentication failures

Common causes:
- Expired session tokens
- Incorrect endpoint URL
- Wrong region for standard S3 (use `auto` for R2)
- Path-style mismatch

Test connectivity directly:

```bash
aws s3 ls s3://oar-workspace-blobs/workspaces/ws_example/ \
  --endpoint-url https://<account>.r2.cloudflarestorage.com
```

## Hot-path warning

Do not use `blob.Backend.Usage(ctx)` on every quota read with S3 backend.

The workspace DB ledger exists specifically to avoid remote listing on hot paths. S3 backend implements `Usage` for operator tools and reconciliation only.
