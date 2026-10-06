# Scope/read-model experiment

This is a feasibility experiment, not a production performance prediction or a
policy-equivalent replacement. No hosted data, credentials, blobs or profiles were
used. The prototype belongs on `experiment/sca-666-scopes`, not in the design PR.

## Method

Baseline is main `e637014e8c6eadc660c7effe608a7e3ff6af6814` (schema 63), with its
real modernc SQLite driver, ownership triggers and authenticated HTTP handlers.
The deterministic generator starts from the existing
`TestResourceAccessCommonReadPerformance` fixture. It creates 26 documents, 433
cards, 2,759 events, 806 artifacts, 499 threads and 12 inbox items at 1x; 10x
multiplies those families. Additional setup adds 10 private board/card controls
per scale, a public board, two principals, 43 records in each of three PM kinds
per scale and 12 plans per scale. Text-bearing rows contain 60 deterministic
document references; all generated content is synthetic.

The proposal uses a **separate database**, a scope-tagged canonical row table,
scope-prefixed feed index and small counters. It copies the fixture's payload
columns for cards, inbox, documents, events, artifacts, threads, PM and plans.
Queries fetch at most 20 rows in one read transaction; Overview also fetches its
fixed counter buckets. It marshals the result into JSON. It is a SQL kernel, not
an HTTP server, generated repository, complete authorization implementation or
wire-equivalent Overview. Its “private scope” sentinel is excluded by scope=1.

Both use WAL and synchronous=FULL. Run with `GOMAXPROCS=1`; this limits Go execution
parallelism, **not** CPU cgroup quota or machine isolation. The shared macOS ARM64
host was running other tests. There are two warmups and 25 measured samples for
each route/kernel and write; p95 is nearest rank. HTTP includes authentication,
legacy scope handling and the real projection/serialization. Kernel measurements
exclude those layers, grant resolution, multi-scope merge, reference hydration,
search, SSE and health computation. **Do not divide HTTP latency by kernel latency
and call that an end-to-end speedup.**

Baseline writes are committed canonical event INSERTs with current triggers;
proposal writes atomically insert canonical row + change/feed row + counter delta.
Both write the same small JSON payload with one public document reference.
Neither write timing includes HTTP validation, and the prototype does not yet
implement scoped writer authorization. This is append cost, not a bound for every
mutation, grant change, blob or pathological fanout.

Logical bytes are SQLite `page_count * page_size`, including allocated free pages,
before timed writes. Baseline authorization bytes use `dbstat` for
`resource_access_%`, `idx_access_%` and associated automatic indexes. Proposal
derived/index bytes include feed, counters and its scope page index. The proposal
does not reproduce all baseline schemas, indexes, FTS, resource registry, grants,
aliases, tombstones or full projections. Consequently total file sizes are
**not like-for-like storage estimates**; component measurements establish the
cost of these query shapes, not the final application's reduction percentage.

## Initial 1x results

| Measurement                                                   |     Current model |     Proposed kernel |
| ------------------------------------------------------------- | ----------------: | ------------------: |
| Inbox p95                                                     | 1,597.870 ms HTTP | 0.041 ms SQL + JSON |
| Overview p95                                                  |   714.870 ms HTTP | 0.074 ms SQL + JSON |
| Work list p95                                                 |   142.421 ms HTTP | 0.052 ms SQL + JSON |
| Event append p95                                              |          1.965 ms |            0.283 ms |
| Logical allocated bytes                                       |       166,772,736 |           8,814,592 |
| Authorization bytes / proposed feed+counter+scope index bytes |       111,222,784 |             319,488 |
| Current base access edges                                     |           118,509 |       No edge graph |

All three prototype feed plans use
`SEARCH feed USING PRIMARY KEY (scope=? AND kind=?)`, without table scans or a
temporary sort. Current route samples require status 200 and absence of the
private sentinel. The 10x run and final reproducibility details are pending in
this draft and must be recorded before handoff.

## What this establishes

The existing trigger model produces substantial derived storage on a small,
reference-heavy synthetic corpus. The scoped feed/counter query shape can retrieve
a bounded page cheaply without evaluating a denial graph. Neither observation
proves that the proposed complete policy, migration, grants, query composition or
storage budget works. Those are explicit implementation gates in the design.

Before enabling the new reader, port this experiment to SCA-661's shared fixture
and capture actual statements/row counts for a complete HTTP implementation.
Measure 1/16/64 scopes, allowed and denied principals, sparse pages, search
selectivity, second SSE ticks, write fanout, full schema storage and migration
peak disk. The current experiment is intentionally insufficient to accept a
production replacement or waive any existing privacy/performance gate.
