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
document references; all generated content is synthetic. The 60 occurrences
repeat among 26 targets at 1x but reach up to 60 distinct targets at 10x. Thus
unique edge cardinality grows faster than 10x; this is not a fixed-fanout
asymptotic test.

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
before timed writes. Baseline authorization bytes join `dbstat` with `sqlite_master` by owning
`resource_access_%` table, including **all** associated indexes. The first timing
run used a name-prefix filter that missed `idx_resource_access_edges_target`; its
raw component counts are undercounts. Corrected 1x storage was measured in a
separate storage-only fixture repeat; corrected 10x storage was read from a
synthetic snapshot taken after fixture construction. Logs preserve this correction.
The repeated 1x allocated size differs by 610,304 bytes (about 0.4%) from the
original run because the auth/board control IDs are randomized; timed data bodies
and row counts are deterministic. Proposal
derived/index bytes include feed, counters and its scope page index. The proposal
does not reproduce all baseline schemas, indexes, FTS, resource registry, grants,
aliases, tombstones or full projections. Consequently total file sizes are
**not like-for-like storage estimates**; component measurements establish the
cost of these query shapes, not the final application's reduction percentage.

## 1x results

| Measurement                                                   |     Current model |     Proposed kernel |
| ------------------------------------------------------------- | ----------------: | ------------------: |
| Inbox p95                                                     | 1,597.870 ms HTTP | 0.041 ms SQL + JSON |
| Overview p95                                                  |   714.870 ms HTTP | 0.074 ms SQL + JSON |
| Work list p95                                                 |   142.421 ms HTTP | 0.052 ms SQL + JSON |
| Event append p95                                              |          1.965 ms |            0.283 ms |
| Logical allocated bytes                                       |       167,383,040 |           8,814,592 |
| Authorization bytes / proposed feed+counter+scope index bytes |       156,073,984 |             319,488 |
| Current base access edges                                     |           118,509 |       No edge graph |

## 10x results

| Measurement                                                   |      Current model |     Proposed kernel |
| ------------------------------------------------------------- | -----------------: | ------------------: |
| Inbox p95                                                     | 26,624.419 ms HTTP | 0.044 ms SQL + JSON |
| Overview p95                                                  |  2,432.128 ms HTTP | 0.061 ms SQL + JSON |
| Work list p95                                                 |    354.944 ms HTTP | 0.036 ms SQL + JSON |
| Event append p95                                              |           1.217 ms |            0.178 ms |
| Logical allocated bytes                                       |      2,450,599,936 |          88,055,808 |
| Authorization bytes / proposed feed+counter+scope index bytes |      2,347,954,176 |           3,366,912 |
| Current base access edges                                     |          2,552,998 |       No edge graph |

The 10x allocated baseline size is 2,450,599,936 bytes, with 2,347,954,176 bytes
in authorization tables/indexes and 2,552,998 base access edges. Its prototype
uses 88,055,808 allocated bytes, including 3,366,912 bytes of feed/counters/scope
page index. These remain different schema coverages, not final reduction ratios.

The unchanged schema-63 storage initializer was also tested directly: an unknown
migration number is accepted, but the proposed atomic ledger-view fence rejects
both repeated opens with `no such function: anx_requires_scope_format_v1` while
preserving every ledger row. This is not a full historical-release or crash test.

All three prototype feed plans use
`SEARCH feed USING PRIMARY KEY (scope=? AND kind=?)`, without table scans or a
temporary sort. Current route samples require status 200 and absence of the
private sentinel. The combined run passed in 1,421.8 seconds, including expensive fixture setup
(1x subtest 82.6 s; 10x 1,338.7 s). Timing a full setup phase separately was not
instrumented; these subtest durations include reads/writes and are not pure seed
costs. The single-reference append timings do not capture that bulk build cost.

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

## Reproduction and review

The non-merge [experiment branch](https://github.com/Git-on-my-level/agent-nexus/tree/experiment/sca-666-scopes)
contains the generator, fence test, README and raw logs under `experiments/scopes/`.
No synthetic database is committed. From that branch's repository root:

```sh
GOMAXPROCS=1 go -C core test ./internal/server \
  -run '^TestScopeDesignExperiment$' -count=1 -timeout=25m -v
GOMAXPROCS=1 go -C core test ./internal/server \
  -run '^TestScopeDesignDowngradeFence$' -count=1 -v
```

Allow several GB of temporary disk and substantial seeding time. Host: macOS
ARM64; Go 1.27.0; modernc SQLite v1.38.2. The fixture's canonical payloads are
synthetic; setup identity/control IDs vary. The name-prefix storage accounting
error is retained in the original log and corrected in separate evidence rather
than rewriting historical output. The final prototype uses ownership-based
index accounting. An optional `ANX_SCOPE_STORAGE_ONLY=1` repeat measures storage
without repeating routes. Shared-host contention and the live synthetic snapshot
copy are additional noise sources; these are feasibility numbers, not capacity
planning measurements.

Independent adversarial design review requested and received corrections for a
reference-write existence oracle, plaintext global cursor IDs, omitted old/new
search posting and rejected-candidate costs, and unenforceable downgrade refusal.
That first re-review approved its documentation revision; the subsequent
coordinator review identified the additional boundaries exercised below. Neither
review approves implementation or waives the full privacy, crash,
historical-binary and SCA-661 performance gates.

## Hard-boundary prototype after coordinator review

The additional `core/experiments/scopeboundary` package on the non-merge branch
exercises the mechanisms missing from the first kernel. It does not replace the
real storage implementation, query-template/analyzer inventory, or production
cutover gates. All fixtures are synthetic; no long benchmark was rerun.

| Review boundary         | Executed probe                                                                                              | Result / limitation                                                                                                                                                                     |
| ----------------------- | ----------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Global allocation       | Create with private record, same-name private alias and private tombstone present/absent                    | Same success and unchanged requested alias; 128-bit opaque ID shape; creator replay checked; no hidden suffix allocation                                                                |
| Parent lifecycle        | 1,000/10,000 descendants plus one unaffected sibling; restart worker mid-job                                | Three initiating writes; unavailable counters/pages during fence; 17/158 worker slices of <=64 rows; final count one and one examined page candidate                                    |
| Audiences and 64 scopes | 64,000 wrong-audience changes; selected personal streams in 64 scopes                                       | 64 examined selected candidates for a one-result request; zero wrong-audience/idle candidates; continuation advances only emitted heads                                                 |
| Grant enumeration       | 10,000 sealed-scope bindings after the active page                                                          | Exactly 65 examined own-binding rows, not a join-filtered search for 65 active scopes; unavailable slots are explicit                                                                   |
| Derivation              | Principal with private/public grants; direct copy, rebinding and private constant into public projection    | All three mechanically rejected; same-scope positive passes. Handles contain no private plaintext. Full import/call-graph analyzer is not implemented by this thin probe                |
| Ordering/history        | 10,000-card column and 10,000-comment thread                                                                | One successor candidate; exhausted gap refused; new three-term comment causes four row writes and no history reconstruction                                                             |
| Search progress         | Repetitive document larger than searchable prefix, phrase only beyond coverage, then a matching next record | First candidate consumed within 64 KiB declared coverage; next record reached on continuation; changed generation rejected                                                              |
| Cold startup            | Close/reopen schema-63 DB with 100/1,000 unknown artifacts and delayed/unavailable blob backend             | Actual storage initializer, experimental primitive constructor, core HTTP handler and network `/readyz` succeed with zero startup blob reads; persisted worker cursor survives reopen   |
| Recovery faults         | Inject low disk and worker failure, then successful retry; old/new ledger opens                             | Jobs/checkpoint preserved, unknown blobs denied, old initializer refuses fence; a **fresh compatible test executable** serves `/readyz` from the fenced DB without restoring old ledger |

The derivation probe rejects cross-scope handles and scope-pinned constants; it
does not prove whole-program implicit-flow safety. Only the trusted dispatcher may
mint capabilities in production, and the analyzer must prohibit business code from
opening a second computation after observing private results/errors. The test
harness has dispatcher authority to inject both handles for the negative test.

The `scope_probe` scalar function counts candidates visited in measured SQL ranges;
it is test instrumentation, not a proposed production WHERE function. It measures
more than returned rows, but does not count B-tree traversal instructions. The
lifecycle prototype handles one-level ancestry; the production depth<=8 checks,
independent inbox lifecycle rules and legacy cross-scope fence remain gates. The
stream prototype has broadcast and personal audiences; role binding validation,
wire SSE framing, encrypted cursors and concurrent revocation remain gates.

The constructor in `core/internal/primitives/scope_experiment_bridge.go` exists
**only on the experiment branch**. It bypasses the old synchronous blob backfill
by constructing without a backend, then attaches the backend. The experimental
worker persists discovery/due jobs and tests bounded retries. The real bridge must
wire a supervisor, enforce byte limits with `OpenReadStream`, capture every relevant
write and implement fencing-token leases; the experiment's lease test proves only
single-slot exclusion, not takeover after process death. Low disk is injected
before work, not a real full filesystem. The compatible recovery child verifies
the renamed ledger and serves actual core HTTP, but does not validate all historical
migration hashes or run the complete CLI/bootstrap path. OS/filesystem caches are
not flushed. These are startup-boundary probes, not deployment readiness or 1-CPU
capacity measurements.

Run the complete additional suite (including child-process recovery):

```sh
GOMAXPROCS=1 go -C core test ./experiments/scopeboundary \
  -v -count=1 -timeout=3m
```

The design now defers bulk reclassification, fuzzy/prefix and historical-version
search. It limits generation to reviewed query templates and a small ownership
manifest. Publication/recovery powers, incomplete PM coverage above 64 scopes,
whole-scope lifecycle unavailability and potentially permanent sealing are explicit
owner decisions, not inferred administrative privileges.

Independent subagent re-review of this revision: **APPROVE**, with no remaining
P1/P2 findings in the design/prototype scope. Review found and fixed a reflectable
plaintext map on the computation capability; kernel state now lives in closure
captures, with a regression formatting both capability and value. The reviewer
independently ran the complete suite and repeated derivation/startup tests after
that correction. Full analyzer/control-flow enforcement and production cutover
gates remain open. The recorded additional suite passed in 2.855 s; cold HTTP
readiness/reopen samples were 6.07–8.28 ms with zero startup blob reads on this
shared host, without flushing filesystem caches.
