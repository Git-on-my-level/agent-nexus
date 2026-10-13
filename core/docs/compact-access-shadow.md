# Compact access shadow migration

This is the off-by-default dual-write and verification foundation for SCA-766,
based on the [measured layout proposal](../../docs/design/proposals/compact-exact-access-edges.md).
It does **not** switch authorization readers or reclaim disk space. The original
exact ledger remains authoritative, including for Overview, card, inbox, receipt
streams, imports and rollback. No HTTP routes or public contracts change.

## Enable and resume

Set `ANX_COMPACT_ACCESS_SHADOW=true` or pass `anx-core --compact-access-shadow`.
The server-owned maintenance worker starts alongside serving. Initialization
atomically creates an empty compact table/index, reconstruction view, checkpoint
and three dual-write triggers. It does not copy history before readiness.

Each transaction copies at most 200 legacy edge keys and advances its durable
cursor. The worker uses the existing 100 ms maintenance deadline and adaptive
batch cap, yields immediately to an active writer, and waits 100 ms between
attempts. The nullable integer cursor includes the entire signed rowid domain.
INSERT/UPDATE/DELETE triggers mirror the old ledger atomically with canonical
writes, including changed/reused keys behind the cursor. Digests are decoded
from existing hexadecimal SHA-256, preserving the existing normalization.
Malformed digests abort the enclosing write rather than publish a partial mirror.

Process exit rolls back the current batch; restart with the switch enabled
resumes the last committed checkpoint. Disabling the switch pauses maintenance;
**already-installed dual-write triggers remain active**, so the mirror cannot
silently miss writes while paused. A fresh database with the switch disabled
creates no shadow artifacts and incurs no added query or write work.

After the final empty keyset seek, verification uses one read-only WAL snapshot:

- Both directions of EXCEPT compare every reconstructed legacy column.
- Both directions also compare the legacy ledger against independently hashed
  raw edges, rejecting equally corrupt legacy and shadow data.
- A separate check rejects orphan compact rows hidden by the reconstruction join.

Success logs `compact access shadow: verified rows=... epoch=...; legacy reads retained`.
The result describes that snapshot only; it is neither a durable readiness flag
nor permission for cutover. Verification has a five-minute deadline and stops
after reporting a failure; restarting the enabled worker repeats verification.
Batch failures retry with rate-limited generic diagnostics. No resource values
are logged. The worker is cancelled and joined during server shutdown.

## Cost and evidence

There is no new request read path. Opted-in writes add one integer-key mirror
write and a 32-byte target-key index update per exact edge. Backfill seeks use the
legacy INTEGER PRIMARY KEY, then copy a bounded key range in one statement:
O(log N + B) selection and O(B log N) index maintenance, B <= 200. The prospective
binary target lookup uses `idx_access_compact_target` and raw integer-key probes;
production adoption still requires dense-fanout latency gates. The reconstruction
view is for verification/testing, not an optimized production query interface.

Full equivalence verification is deliberately O(N) maintenance work, with
EXCEPT temporary storage proportional to the ledger. It holds no writer lock
but can retain WAL pages while writers run. Budget additional space for the
compact ledger, verification temporaries and WAL; do not infer headroom from the
compact component size alone. Cancellation releases the read snapshot.

Reproduce the actual expand-only measurement on disposable synthetic data:

```sh
cd core
ANX_PERFORMANCE_TEST=1 go test ./internal/storage \
  -run '^TestPerformanceCompactAccessShadow$' -count=1 -v -timeout=15m
```

The checked-in 4,096-resource-per-family scale fixture produced these measurements
on macOS arm64 (2026-10-13; shared host; no production data):

| Measurement                                         |            Result |
| --------------------------------------------------- | ----------------: |
| Verified exact edges                                |           243,770 |
| Legacy table + target index                         |  51,048,448 bytes |
| Compact table + target index                        |  21,204,992 bytes |
| Database file before shadow                         | 304,488,448 bytes |
| Database file after shadow                          | 325,701,632 bytes |
| Physically reclaimed bytes                          |             **0** |
| Metadata installation                               |           1.45 ms |
| Backfill, 1,220 immediately successive batches      |            4.18 s |
| Largest batch                                       |          47.86 ms |
| Verification plus final checkpoint/stat measurement |            2.21 s |

File sizes follow `wal_checkpoint(TRUNCATE)` in the isolated test. The production
worker's inter-batch waits are excluded from this backfill timing. This fixture
differs from the proposal's 943,778-edge private-history fixture; the figures are
not a before/after comparison to that earlier experiment. Full command output
and exit codes are attached to SCA-766.

## Rollback

Readers never left the old ledger, so disabling the switch requires no data
conversion. To remove all optional artifacts, first restart the core with the
switch disabled (so no worker can recreate them), then execute this transaction
using an operator SQLite connection with a busy timeout. Core can continue
serving from the old ledger while the short DDL transaction commits:

```sql
BEGIN IMMEDIATE;
DROP TRIGGER IF EXISTS access_compact_insert;
DROP TRIGGER IF EXISTS access_compact_update;
DROP TRIGGER IF EXISTS access_compact_delete;
DROP VIEW IF EXISTS resource_access_compact_reconstructed;
DROP TABLE IF EXISTS resource_access_compact_edges;
DROP TABLE IF EXISTS resource_access_compact_job;
COMMIT;
```

This frees reusable pages but does not shrink the file. Do not VACUUM this
workspace: raw `resource_access_edges.rowid` is implicit, and dependent mention
and exact ledgers require stable keys. No destructive cleanup or reclamation is
automated in this foundation.

## Remaining before production cutover

1. Design an explicit read capability/epoch handshake; gate it on fresh relation
   verification and concurrent authorization differential tests. Keep old tables
   and dual writes for reversible reader rollback.
2. Implement indexed compact readers and measure dense target/source fanout,
   including receipt checks and all canonical ownership graph consumers. Keep
   Overview's independent work on the unchanged storage interface.
3. Resolve legacy corruption before enabling compact reads. For example, raw
   `INSERT OR REPLACE` with SQLite recursive triggers disabled can retain a stale
   old exact row; the verifier rejects that state. This migration does not repair
   preexisting authorization indexes or certify corrupt imports.
4. Preserve raw edge identity explicitly, or atomically rebuild every dependent
   ledger, before designing a resumable reclamation operation. Verify cutover and
   rollback under serving writes and process death. Retain the old ledger until
   a later release authorizes its removal, then measure physical file savings
   after a documented maintenance-window copy/vacuum with disk-headroom checks.

Focused tests cover crashes before/after checkpoint commit, imports and key
reuse, rollback/re-enable, corruption, writer contention, concurrent reads and
writes, signed keys and plans. Randomized tests compare complete old/compact
denial closures across private human/agent ownership grants, revokes and transfers,
alongside the existing ordered-result matrix. Real HTTP tests exercise private
positive/negative controls and credential expiry/revocation with the shadow on.
