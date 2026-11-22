# Compact access ledger and reclamation

Migration 150 completes the compact-access contraction (SCA-774). The compact
ledger (`resource_access_compact_edges`) is now the only exact-edge ledger.
The legacy `resource_access_exact_edges` table, its target index, and the
duplicate `resource_access_admission_compact_atoms` mirror are gone. Admission's
`exact` and `compact` origins both read the compact ledger, and the mention
triggers write it directly.

No HTTP routes or canonical API contracts change.

## Migration safety

Migration 150 runs once at startup, before serving. It:

1. Ensures the compact capability exists. The control row, compact ledger and
   job table were previously installed at open, not by a numbered migration;
   the migration now makes that dependency explicit for fresh and upgraded
   workspaces.
2. Completes an interrupted compact backfill from the legacy ledger in bounded
   batches. A certified ledger (`resource_access_compact_job.done=1`) is never
   overwritten.
3. Verifies the compact ledger against the legacy ledger and independently
   hashed raw edges in both `EXCEPT` directions, and rejects orphan compact
   rows. Any divergence aborts the upgrade; the migration never repairs or
   guesses.
4. Stabilizes raw edge identities in bounded batches, verifies both admission
   ledgers against the compact ledger, rebuilds only a mismatched ledger, drops
   the legacy tables, and reinstalls the writers and readers that referenced
   them.
5. Narrows the admission link indexes to their actual users:
   `idx_access_admission_child` and `idx_access_admission_parent` are partial on
   the `comment` and `document_comment` origins, and `idx_access_admission_via`
   is partial on the five origins that delete by key.

Each phase commits its own transaction and the runner checkpoints the WAL
between phases. Peak WAL is therefore bounded by one batch or one index build,
not by the database size. The first committed phase installs a format fence; a
pre-150 binary refuses an interrupted workspace, and the new binary resumes it.
The peak additional disk requirement is approximately one times the current
`state.sqlite` size for the raw-edge staging copy and verification temp
b-trees, plus WAL headroom for one bounded phase.

A workspace whose compact ledger diverges fails to open with a migration error.
Restore the verified backup, let the previous release finish certification, and
retry the upgrade.

## Rollback

The legacy exact ledger is retired, so the historical instant reader rollback
and `ANX_COMPACT_ACCESS_LEGACY_READS` switch no longer select a physical ledger.
The switch is a no-op once the legacy ledger is gone. The control columns remain
for compatibility, but the compact ledger is always the reader. Roll back a bad
release by restoring the verified pre-upgrade backup; do not attempt to
recreate the legacy table by hand.

## Reclaim file space in a maintenance window

For the precise hosted maintenance window and self-host procedure, see
[production reclamation](../../deploy/compact-access-reclamation.md). Reclamation
is **operator-run and offline**, never invoked by startup or the updater.

Dropping the legacy tables frees pages; it does not shrink an
`auto_vacuum=NONE` file. Keep the core stopped for **all** of the following
steps. Disable supervisor restarts first.

1. Back up the workspace and record the stopped source's size. Budget disk space
   for both the old file and the new copy.
2. Build/run the offline helper, supplying the actual source and a new output:

   ```sh
   cd core
   go run ./cmd/anx-access-reclaim --source "$ANX_WORKSPACE_ROOT/state.sqlite" \
     --destination "$ANX_WORKSPACE_ROOT/state.reclaimed.sqlite"
   ```

   The helper acquires the serving OS lease and an exclusive SQLite connection,
   requires certified stable raw IDs, and uses `VACUUM INTO`. It never overwrites
   an existing destination or replaces the source. Before reporting success it
   checks output integrity and all table contents in both directions, plus counts
   and PM rowid-based cursor identities, then fsyncs the new file. A failed copy
   is removed. It refuses a workspace whose core still holds the serving lease.
   The output is built with `auto_vacuum=INCREMENTAL` and the source page size,
   and the helper refuses to start unless the destination filesystem has the
   live page bytes plus the source WAL size plus a 2 GiB reserve.

3. While the core remains stopped, retain the old database as a rollback backup
   outside the active workspace. Replace `state.sqlite` with the verified copy
   using an atomic rename on the same filesystem. Preserve restrictive file
   permissions. The helper closes/checkpoints its source connection; do not
   attach stale `-wal` or `-shm` files to the replacement.
4. Restart the core with the current binary, check readiness and private positive
   and negative controls. Retain the backup according to the operator's backup
   policy. The quoted saving concerns the active database file, not retained
   backup storage.

Do not copy/swap a file while writers continue: later commits could be lost.
Do not apply unverified VACUUM output. In particular, the helper rejects a SQLite
build that changes implicit PM cursor IDs even though access-edge IDs are stable.

## Measuring the reduction

The checked-in dense fixture is the reproducible measurement. Run the
performance tier and record the logged `db_file_before`, `after_reclamation` and
`saved_bytes` values:

```sh
cd core
GOMAXPROCS=2 ANX_PERFORMANCE_TEST=1 go test ./internal/storage \
  -run '^TestPerformanceCompactAccessShadow$' -count=1 -v -timeout=30m
```

The test seeds the dense fixture, verifies the compact ledger, runs the offline
reclamation helper, and asserts the reclaimed file is smaller. The legacy
exact-ledger and admission-mirror bytes are no longer present after migration
150, so the remaining saving is the physical reclamation of freed pages. For a
read-only component comparison on a disposable copy, use
`core/dev/measure_compact_access.py`.

`TestMeasureLegacyAccessLedgerReduction` is the bounded, laptop-friendly
measurement. On a 20,000-edge synthetic corpus it records:

| Component / phase | Bytes |
| --- | ---: |
| Legacy exact table + target index | 3,989,504 |
| Admission compact mirror + index | 1,691,648 |
| Database before migration | 27,856,896 |
| Database after migration (pages freed, not reclaimed) | 27,856,896 |
| Database after `VACUUM INTO` reclamation | 21,086,208 |
| Saved by reclamation | 6,770,688 (24.3%) |

The exact-ledger and mirror bytes are already gone from the live schema after
migration 150; the reclamation step is what returns those freed pages to the
filesystem.

## Document revision edges

Before migration 111, every document revision and its revision artifact kept a
full copy of its reference edges, so a frequently revised document grew the
access graph by revisions times refs. Migration 111 installs triggers only; a
bounded maintenance cursor (`resource_access_document_history_job`) then moves
legacy per-revision edges into one deduplicated set per document, a few hundred
edges per short transaction. It is safe while serving, resumes after restart,
and every committed chunk preserves authorization. Each chunk's time budget
is checked between statements; SQL inside the write transaction is never
cancelled. Failed chunks back off exponentially up to five minutes and log the
revision and error class; after ten consecutive failures the log line reads
`stuck at revision <id> after <n> consecutive failures (<class>)`. A revision is
never skipped, because leaving its edges in place is always safe. Progress is
visible with:

```sql
SELECT cursor, done FROM resource_access_document_history_job;
SELECT count(*) FROM resource_access_edges
 WHERE source_kind IN ('artifact','document_revision');
```

Freed pages are reused by later writes; the file does not shrink on its own
(`auto_vacuum=NONE`). After `done=1`, shrink it with the offline reclamation
procedure above. Never run a full `VACUUM` against a serving core.

## Evidence and cost

The compact ledger stores 32-byte digest BLOBs. Each authorization probe uses
`idx_access_compact_target` followed by the raw integer primary key. The
admission link indexes are partial, so writes only maintain the entries the
comment/document cleanup triggers and key deletion actually read.

Backfill and verification are one-time maintenance work; they never run on a
request path. Ordinary post-migration writes update the compact ledger, the
mention buckets and the admission ledgers transactionally. The existing common
HTTP read performance gate runs in the final schema with the same budgets.
