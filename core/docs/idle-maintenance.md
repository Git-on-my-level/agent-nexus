# Diagnosing and bounding idle maintenance

Core can be busy without HTTP traffic. Workers used to discard most concurrent
SQLite connections between ticks: the default pool retained only two idle
connections, so the next tick reparsed the complete trigger schema. On the
settled synthetic fixture, 73.5% of the steady CPU sample was SQLite preparation.
The router also scanned historical messages at its cursor tail, and empty brief
and replay passes competed for the writer. Slow lock admission incorrectly
reduced adaptive batches, leaving one-row chunks even after contention ended.

During backfill, legacy document search repair was another offender. Its
`DELETE ... WHERE document_id=?` constrained an FTS5 **UNINDEXED** column, scanning
unrelated document content while holding the writer. One captured window spent
85.6% of CPU under that repair. The replacement uses an ordinary document-to-FTS
rowid table and FTS rowid deletes; it does not access FTS shadow tables. Existing
legacy duplicates are preserved in the mapping and removed together on repair.

The fixes retain eight idle connections, preserve serving concurrency for nested
reads, admit indexed positions before loading router payloads, yield after one
router page, check for work before acquiring maintenance writers, back off idle
brief passes and failed ask backfills, and recover batch sizes after sixteen fast
successful chunks. Existing 200-row adaptive ceilings and time targets remain.
Replay collection uses nonblocking writer admission and commits its existing
bounded batch without canceling SQL inside the transaction.

Heartbeat accounting now reuses a numeric canonical snapshot while the durable
accounting revision and SQLite schema version remain unchanged. Canonical writes
and blob totals advance that revision atomically. Optional PM and investigation
schemas install their own tracking; missing tracking disables the cache. Reader
scopes always take the original scoped accounting path. Physical database bytes
are refreshed on every snapshot. Public usage reads and quota checks retain their
existing paths.

## Operator diagnostics

`ANX_DEBUG_PPROF_ADDR=127.0.0.1:6060` enables the standard `/debug/pprof/` surface
on a separate HTTP listener. An empty address disables it. Only literal loopback
addresses are accepted unless `ANX_DEBUG_PPROF_ALLOW_NON_LOOPBACK=true` is set.
The override deliberately exposes full pprof on the supplied interface; use an
operator-only network. The workspace API mux is unchanged. Profiling starts
before workspace initialization so migrations can also be sampled.

Every five minutes, each registered worker emits a line such as:

```text
background_loop=brief runs=153 busy_ms=17 rows=0
```

Names are code constants. The other fields are numeric interval counters.
`busy_ms` is elapsed time inside completed active passes, including lock/network
waits, excluding timer sleeps. Concurrent workers overlap, so these times cannot
be summed into process CPU. `rows` counts logical candidates processed or moved,
not SQLite's internal VM steps or trigger fanout. Dormant and finished workers
remain visible as zeroes. CPU profiles also carry the constant `loop` label.
No SQL, record identifiers, titles, arguments or payloads enter these log lines.

## Synthetic measurements

The fixture contains 4,096 cards and plans, 4,096 documents, 32,768 document
revisions, 16,384 events including 4,096 asks, and 32,768 replay receipts. It uses
canonical triggers throughout. Retained receipt responses and historical bodies
are synthetic JSON. After settling all queues, `state.sqlite` is 2,271,711,232
bytes. Both compared runs start from that same settled schema-119 copy, including
the router cursor. The candidate applies migration 120 on its private copy.

Both use Go 1.27.0 and run Linux/arm64 with one CPU, 1 GiB memory, no swap and a 64 MiB `/tmp`.
There is no inbound workspace workload. The router runs normally and a local
receiver accepts signed heartbeat publications. Cgroup CPU includes the receiver
and measurement probes; pprof samples only core. Each run is bounded below ten
minutes and its container is stopped and removed.

| Measurement                      |    Before |     After |
| -------------------------------- | --------: | --------: |
| Cgroup CPU, early profile window |    41.25% |     1.72% |
| Cgroup CPU, steady window        |    21.42% |     1.14% |
| Peak cgroup memory               | 155.2 MiB | 160.8 MiB |

The full numeric samples, CPU counter boundaries, per-loop interval values and
candidate binary fingerprint are in [the evidence](idle-maintenance-evidence.json).
The settled comparison is separate from the initial cold fixture, which had real
backlogs and reproduced 97.5% CPU during its first profile. That cold run and the
intermediate repair profile identified the expensive backfill paths; they are
not presented as a like-for-like settled comparison.

The first five-minute interval contains the following active-pass elapsed times.
All logical work queues were empty. Runs and row counts for every worker are
in the JSON evidence. Zeroes also identify workers whose schedule did not fire.

| Worker            | Before busy ms | After busy ms | Before / after rows |
| ----------------- | -------------: | ------------: | ------------------: |
| answer_wake       |          49050 |            63 |               0 / 0 |
| ask_lifecycle     |          58402 |            76 |               0 / 0 |
| ask_subjects      |              0 |             0 |               0 / 0 |
| ask_webhooks      |              3 |             3 |               0 / 0 |
| brief             |          64967 |            34 |               0 / 0 |
| card_lease        |          11910 |          1495 |               0 / 0 |
| comment_search    |           4009 |          1525 |               0 / 0 |
| compact_migration |              0 |             0 |               0 / 0 |
| compact_retired   |              0 |             0 |               0 / 0 |
| curation          |              0 |             0 |               0 / 0 |
| document_history  |              0 |             0 |               0 / 0 |
| document_search   |           5234 |          1865 |               0 / 0 |
| escalation        |          10764 |            16 |               0 / 0 |
| heartbeat         |           4776 |          1953 |               0 / 0 |
| inbox_lifecycle   |              0 |          1341 |               0 / 0 |
| observation       |              0 |             0 |               0 / 0 |
| pm                |          25071 |            18 |               0 / 0 |
| projection        |          33508 |          1563 |               0 / 0 |
| replay_gc         |           2800 |             1 |               0 / 0 |
| router            |          41145 |          1652 |               0 / 0 |
| scope_inbox       |              0 |             0 |               0 / 0 |

On the 2.27 GB copy, migration 120 completed in 187 ms and the API listener opened
336 ms after the first process log. This is an already migrated and settled
workspace plus the new metadata migration, not a claim that historical admission
or search backfills finish in 336 ms. The unchanged schema-58 upgrade scale gate
also passes; its complete upgrade took 43.38 seconds locally and warm reopen
0.03 seconds. Real production attribution still requires enabling diagnostics
there; this work never reads production data.

## Reproduce locally

Requires Go, Docker, Python 3 and the repository dependencies. Put disposable
inputs and outputs on a sufficiently large scratch volume. On the development
host, choose a scratch directory with sufficient space; a neutral example is:

```sh
export IDLE_SCRATCH="${TMPDIR:-/tmp}/anx-idle-profile"
mkdir -p "$IDLE_SCRATCH"
go -C core run ./cmd/anx-idle-fixture --root "$IDLE_SCRATCH/fixture"
go -C core run ./cmd/anx-idle-fixture --root "$IDLE_SCRATCH/fixture" --settle
env CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go -C core build -o "$IDLE_SCRATCH/anx-core" ./cmd/anx-core
python3 scripts/profile-idle-core.py --binary "$IDLE_SCRATCH/anx-core" --workspace "$IDLE_SCRATCH/fixture" --output "$IDLE_SCRATCH/profile"
go tool pprof -top "$IDLE_SCRATCH/anx-core" "$IDLE_SCRATCH/profile/startup.pprof"
go tool pprof -top "$IDLE_SCRATCH/anx-core" "$IDLE_SCRATCH/profile/steady.pprof"
```

The fixture command refuses existing unmarked databases. `--settle` invokes real
maintenance and the strict legacy-search fallback; it never bypasses correctness
checks or marks pending work complete by fiat. Compare binaries using independent
copies of the same settled fixture, not the output DB of an earlier measurement.
For a cold profile, omit settling and keep its backlog separate from idle results.

The runner records `cpu.jsonl`, two 45-second profiles and the five-minute worker
log. It requires 410–580 seconds, limits core's lifetime to 590 seconds including
forced shutdown, and removes its container even on failure. It never uses a bind
mount, SSH, production credentials or a real workspace.

## Cost and correctness gates

Migration 120 scans existing message positions and FTS IDs once, then installs
small scalar accounting triggers. Those scans grow with message/FTS row count;
they do not parse historical payloads. Router ticks seek
`idx_router_message_positions_order(sort_time,event_id)` and admit at most 100
positions before primary-key payload joins. An empty tail is independent of
history, including large timestamp ties. Scoped callers retain the existing
visibility selection instead of using the internal canonical router selector.

FTS maintenance resolves document positions through the mapping's
`(document_id,fts_rowid)` primary key and FTS rowids. Ordinary canonical writes
retain one FTS row per document; legacy duplicate work grows only with that
document's duplicates. Metadata patching uses the same indexed mapping.

Idle job probes use singleton primary keys, queue existence seeks or the existing
expiry/receipt ordering indexes. The canonical heartbeat cache hits two scalar
reads; only a first or dirty snapshot measures canonical content and counts.
That refresh remains proportional to canonical bytes/rows, runs as a reader and
coalesces changes into the existing heartbeat interval. No unchanged workspace
scan runs on cache expiry alone. No HTTP route gains this cache or an extra read.

The new [read budgets](../internal/primitives/testdata/idle_maintenance_budget.json)
enforce 2,048 VM steps for an empty router tail and 20,000 for the small target
repair with 4,096 unrelated FTS documents; both permit zero full-scan steps.
Measured values were 41 / 28 VM steps at the router tails and 478 for repair,
versus 53,267 and 61,863 respectively before repair. Frozen heartbeat accounting
falls from 39 queries to two scalar probes. Completed/empty workers also have
regressions that hold another connection's writer: they must finish without
competing for it. Batch backoff, recovery, transactional rollback, legacy FTS
mapping and reader-scope regressions remain enforced. No existing budget or
query-plan allowance is increased.

The separate pprof listener adds `/debug/pprof/`, `/debug/pprof/profile`,
`/debug/pprof/trace`, `/debug/pprof/symbol`, `/debug/pprof/cmdline` and named runtime
profiles under `/debug/pprof/`. There are no changed workspace HTTP routes.
