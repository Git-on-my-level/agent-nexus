# Compact exact access edges (SCA-747 proposal)

Status: measured layout experiment, **not an implemented migration**. The first
SCA-747 PR optimizes card and inbox read snapshots. It changes no stored data.
Online migration, rollback, and physical file reclamation remain follow-ups.

## Evidence

The SCA-746 synthetic private-history fixture, copied with SQLite's backup API,
contains 943,778 rows in each of `resource_access_edges` and
`resource_access_exact_edges`. No production data was used. SQLite `dbstat`
reports these live bytes after the read workload:

| Structure                      |       Bytes |
| ------------------------------ | ----------: |
| Raw edges                      |  60,022,784 |
| Raw-edge primary-key autoindex | 107,433,984 |
| Raw-edge target index          | 107,409,408 |
| Exact edges                    |  92,684,288 |
| Exact-edge target index        | 102,543,360 |

A blanket `WITHOUT ROWID` conversion would break the stable integer edge keys
used by mention buckets, materialized mentions, receipt checks, and triggers.
Retain that identity unless all consumers are migrated together. Also, SQLite
may renumber implicit rowids during VACUUM; the raw edge table has no explicit
INTEGER PRIMARY KEY. A physical-reclamation plan must preserve or deliberately
rebuild every dependent ledger rather than assume those keys survive. See the
[SQLite VACUUM documentation](https://www.sqlite.org/lang_vacuum.html).

## Candidate layout

The exact ledger duplicates `source_kind` and `source_id` already held by the raw
edge at `edge_id = resource_access_edges.rowid`. It also stores the same 32-byte
SHA-256 digest as 64 hexadecimal characters in both its table and index.

```sql
CREATE TABLE compact_exact (
  edge_id INTEGER PRIMARY KEY,
  target_key BLOB NOT NULL
);
CREATE INDEX compact_exact_target ON compact_exact(target_key);

SELECT e.source_kind, e.source_id
FROM compact_exact c
JOIN resource_access_edges e ON e.rowid = c.edge_id
WHERE c.target_key = ?;
```

The experiment copies existing digests as binary; it changes neither the hash
nor ASCII-only NOCASE normalization. Reconstructing the legacy relation by
joining raw edges and taking `lower(hex(target_key))` must produce zero rows in
**both directions of EXCEPT**. That full-relation comparison passed on all
943,778 fixture rows. This establishes layout equivalence on this fixture, not
proof of migration safety or authorization equivalence during concurrent writes.

The original exact table plus index occupies **195,227,648 bytes**. The compact
experiment, including its new database's schema page, occupies **78,196,736
bytes**: **117,030,912 bytes (59.95%) less** for this component. This is a layout
measurement only; the runtime workspace file has not been compacted.

The lookup plan uses the covering `compact_exact_target(target_key)` index and
an integer primary-key lookup of the raw edge. It adds one raw-edge lookup per
matching edge in exchange for removing duplicated strings. Dense target fanout
and source-kind filtering need explicit before/after latency gates before
adoption. The raw-edge target index appears unused by current runtime consumers;
removing it is a separate candidate, requiring upgrade/import and trigger tests.

## Required migration protocol

1. Install the compact table, a durable job checkpoint, and INSERT/UPDATE/DELETE
   dual-write triggers atomically. Readers continue using the original ledger.
2. Backfill fixed-size batches selected by the integer edge key. Copy and advance
   the checkpoint in one transaction. Dual writes cover deletes, updates, and
   reused keys behind the cursor. Test process death before/after every boundary.
3. Under one snapshot, compare both complete relations and randomized old/new
   authorization decisions, including private inheritance, aliases, prose,
   tombstones, grant changes, and credential expiry/revocation. Any difference
   blocks cutover. Corrupted or inconsistent legacy ledgers must fail closed.
4. Enable compact reads with an explicit schema/capability handshake. Retain the
   old ledger and dual writes during rollback validation. Exercise rollback under
   concurrent canonical and imported writes, including restart after each phase.
5. Only after verified rollback, remove redundant data in a separately recorded
   finalization step. Rebuilding the old ledger from raw edges must itself be
   resumable if later downgrade support is promised.

The current fixture uses SQLite `auto_vacuum=NONE`. Dropping a table/index frees
reusable pages but does **not** shrink its billed database file. An online file
reclamation strategy and its disk-headroom budget are therefore part of this
follow-up, not an implied benefit of DROP INDEX. Do not run an unbounded VACUUM
on a request or startup path. This PR changes no database file layout.

Reproduce the layout measurement on a disposable synthetic fixture:

```sh
python3 core/dev/measure_compact_access.py fixture/state.sqlite compact.sqlite
```

The destination must not exist. The experiment creates a separate database and
opens its fixture read-only. Full `dbstat` measurements, query plans, relation
comparisons, CPU profiles, commands and exit codes are attached to SCA-747 in
Multica.
