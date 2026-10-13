# Compact access migration and reclamation

The migration serves indexed compact authorization reads after verifying both
ledgers and stabilizing raw edge identities. On the checked-in dense fixture,
reclamation reduced the database file from **304,500,736 to 246,775,808 bytes**
(**57,724,928 bytes saved, 18.96%**), while retaining the original exact ledger
for immediate reader rollback. No HTTP routes or canonical API contracts change.

## Release decision

`ANX_COMPACT_ACCESS_MIGRATION` / `--compact-access-migration` currently defaults
to **false pending the rollback retention decision**. Enabling it runs the full
migration, including certified reader cutover. It is no longer a shadow-only
worker. Empty capability metadata is installed before serving so every reader
can evaluate the selected format within its SQL snapshot.

Immediate, correct fallback requires a current old ledger. This implementation
keeps that ledger synchronized until its removal in a later release. Stopping
its writes while retaining it would require replay/rebuild before fallback, or
an additional indexed delta ledger. Do not enable this migration by default
until the retention policy is accepted and release certification is complete.

## Online migration

1. Atomically install dual-write triggers, a compact binary-digest ledger and a
   raw-edge staging table with `edge_id INTEGER PRIMARY KEY`. The original raw
   table and exact ledger continue serving throughout backfill.
2. Copy each ledger in resumable keyset batches of at most 200 rows. Each cursor
   commits with its rows. The existing adaptive 100 ms maintenance deadline
   reduces oversized batches; contention yields immediately to serving writers.
   The worker waits 100 ms between attempts. Signed and reused keys are supported.
3. Verify all exact columns against both the reconstructed compact relation and
   independently hashed raw edges, in both EXCEPT directions. Verify the staged
   raw IDs and bytes in that same read snapshot. Reject incomplete backfills,
   extra/missing/orphan rows and equally corrupt old/new ledgers.
4. Take a nonblocking writer lease and require the authorization epoch **and**
   ledger-write generation to match the verification snapshot. A stale proof
   retries verification. Atomically replace the raw table with the certified
   explicit-ID table, reinstall its triggers, remove its redundant target index,
   publish compact reader mode, and advance the authorization epoch. The DDL
   phase has a ten-second deadline; the measured dense-fixture commit took 81 ms.

The serving OS lease is retained after stabilization, including across open
connections, and reacquired before migrations on restart. Old binaries are not
supported after this format transition; reader rollback uses the current binary.

The compact target index stores 32-byte digest BLOBs. Each authorization probe
uses that index followed by the raw integer primary key. Legacy and compact SQL
arms are separately indexed and gated by the persisted mode inside the same
statement. Cached denial snapshots retain their existing epoch check/fallback.
Receipt ancestry reads use the same selected ledger. Overview's public storage
interface and policy remain unchanged.

The cutover repairs raw REPLACE handling with indexed conflict-key cleanup,
including SQLite's implicit deletes when recursive triggers are disabled.
Malformed digests abort writes. Corruption found before certification stops the
migration without changing the active reader; it is not silently repaired.

## Pause and instant reader rollback

Disabling the migration flag pauses the worker; it does not undo a committed
cutover. Installed mirrors remain transactional with canonical writes.

An operator can switch readers back immediately while the old ledger remains
current. Execute this short transaction with a local SQLite connection and a
busy timeout:

```sql
BEGIN IMMEDIATE;
UPDATE resource_access_compact_control SET reader=2 WHERE singleton=1;
UPDATE resource_access_epoch SET version=version+1 WHERE singleton=1;
COMMIT;
```

Mode 2 prevents the worker from undoing the rollback. Explicit raw IDs stay
stable. Re-certification/re-enablement requires deliberately resetting mode to
0 and restarting the enabled worker. No authorization data is copied during the
reader switch. Never manually switch to mode 1 without verification.

## Reclaim file space in a maintenance window

Cutover frees pages; it does not shrink an `auto_vacuum=NONE` file. Keep the core
stopped for **all** of the following steps. Disable supervisor restarts first.

1. Back up the workspace and record the stopped source's size. Budget disk space
   for both the old file and the new copy; migration itself temporarily stores
   the full staged raw table and its unique index.
2. Build/run the offline helper, supplying the actual source and a new output:

   ```sh
   cd core
   go run ./cmd/anx-access-reclaim --source "$ANX_WORKSPACE_ROOT/state.sqlite" \
     --destination "$ANX_WORKSPACE_ROOT/state.reclaimed.sqlite"
   ```

   The helper acquires the serving OS lease and an exclusive SQLite connection,
   requires certified explicit raw IDs, and uses `VACUUM INTO`. It never overwrites
   an existing destination or replaces the source. Before reporting success it
   checks output integrity and all table contents in both directions, plus counts
   and PM rowid-based cursor identities, then fsyncs the new file. A failed copy
   is removed. It refuses a workspace whose core still holds the serving lease.

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

## Evidence and cost

The disposable 4,096-resource-per-family fixture contains **243,770 exact edges**.
It differs from the original proposal's 943,778-edge fixture.

| Phase / component                                    |       Bytes |
| ---------------------------------------------------- | ----------: |
| Before migration (empty capability metadata present) | 304,500,736 |
| After cutover, before physical reclamation           | 421,027,840 |
| After verified reclamation                           | 246,775,808 |
| Original exact table + index, retained for rollback  |  51,048,448 |
| Compact table + target index                         |  21,204,992 |
| Redundant raw-edge target index removed              |  69,095,424 |

Backfill selects O(log N + B) keys and performs O(B log N) index work, B <= 200.
Full snapshot verification and offline reclamation are O(N) maintenance work;
EXCEPT uses temporary space and WAL snapshots can retain pages. Cutover never
runs on a request path. Ordinary opted-in writes update the current rollback
ledger and compact mirror plus a constant-size generation counter.

A 4,096-edge target fanout used one SQL statement, 4,096 returned rows and 28,731
VM steps, with no full scan, sort or automatic index. Adding 8,192 unrelated
edges changed this to 28,732 VM steps. Ordered old/compact authorization results
also match across concurrent grants/revokes, private human/agent ownership and
HTTP credential expiry/revocation.

Reproduce the primary gates (complete commands, outputs and exit codes are
attached to SCA-766):

```sh
cd core
GOMAXPROCS=2 ANX_PERFORMANCE_TEST=1 go test ./internal/storage \
  ./internal/primitives ./internal/server \
  -run '^TestCompact|^TestResourceReadSnapshotRandomizedDifferential$|^TestResourcePageSnapshotAuthenticationAndSingleConnection$|^TestResourceAccessCommonReadPerformance$|^TestPerformanceCompactAccessShadow$' \
  -count=1 -v -timeout=15m
```

A later release may stop old-ledger writes atomically with old-ledger removal,
ending instant reader rollback and yielding additional savings. That destructive
finalization is intentionally not available in this release.
