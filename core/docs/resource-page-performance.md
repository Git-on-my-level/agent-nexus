# Card and inbox read snapshots (SCA-747)

Card detail (`GET /cards/{card_id}` and
`GET /boards/{board_id}/cards/{card_id}`) and open inbox (`GET /inbox`) now
pin their read phase to one admitted SQLite transaction. Eligible identity-only
queries reuse the existing denial-map scalar lookup, instead of decoding and
indexing the entire cached denial JSON for each SQL statement. Reference/prose
queries keep the existing full SQL policy. Response contracts are unchanged.

Admission checks the current authorization epoch in the transaction. A stale
shared cache recomputes the canonical graph. Proof tokens remain local to that
transaction and are deleted on close; invalid proofs fail closed. Rebinding a
principal cannot reuse another scope's proof. The read transaction ends before
the handler returns. Business mutations continue checking their own current
transaction. Nested inbox snapshots borrow the outer read transaction; their
commit/rollback does not close it. Selector checks use that same read handle,
which also prevents deadlock with a one-connection pool.

## Cost and remaining work

The changed membership operation performs O(1) lookups per admitted candidate;
it does not materialize the denial set per eligible statement. It does not add
per-item queries. The existing bounded cache and point-index lifecycle are
unchanged. Admission still has the inherited cold-closure/epoch-mismatch cost,
and the baseline still copies encoded denial data at warm admission (PR #371
addresses that separately).

This is a partial sprint delivery, not certification of the three-view
p50 <300 ms / p95 <1 s target. Existing costs remain:

- Overview's report-work/ref-edge projection expands the complete denial/ref
  set and was the largest CPU consumer in the dense fixture. Its query path is
  unchanged here.
- Card selector validation and reference-edge hydration still use the full
  reference policy. Card history is still proportional to revision history.
- Inbox freshness still aggregates the scoped thread/projection corpus. The
  existing page selector can scan candidates before its limit; 50 returned rows
  do not bound all underlying work.

Card handle/ID lookups retain their unique handle and primary-key indexes;
revision hydration uses the existing card-revision ordering index. Inbox retains
its category/trigger ordering index and lifecycle/audience filters before page
selection. A separate deterministic gate grows the denied corpus from zero to
4,096 entries and requires one indexed point query, one returned row, at most
64 VM steps, and no fullscan/sort/automatic-index work. It isolates the changed
operation; it does not excuse the pre-existing full-route costs above.

## Profiling the dense fixture

The local synthetic SCA-746 fixture has 100,000 archived private inbox rows in
addition to the 4,096-card/document/artifact performance corpus. The baseline
CPU profile attributes 79% of sampled CPU to SQLite stepping and 31% to B-tree
insertion (overlapping cumulative stacks). EXPLAIN shows materialized denial
and reference relations with temporary B-trees/automatic indexes. This is
repeated SQL authorization work, not an HTTP JSON-encoding bottleneck.

The warm stranger trace executed 23 Overview statements (663 returned rows),
12 card statements (12 rows), and 15 inbox statements (306 rows). The optimized
trace has the same counts. It changes work within those statements, rather
than removing an N+1 loop. On the baseline trace, Overview report-work took
5.42 s; card reference-edge hydration took 2.18 s and selector validation 1.39 s.
Inbox page selection took 715 ms, thread selection 419 ms, and freshness 348 ms.
Warm epoch capture/admission took roughly 5–11 ms. These individual diagnostic
samples explain costs; they are not route latency percentiles.

The issue attachment preserves the probe source, all SQL and EXPLAIN output,
CPU profiles, successful and failed runs, and exact commands/exit codes. The
probe opens disposable local copies only; it never fetches a serving workspace.

## Before/after latency and database size

Baseline `84e1a5cc` and this change used independent SQLite backup copies of the
same fixture on macOS arm64 with Go 1.27.0 and `GOMAXPROCS=2`. One client ran ten
waves, alternating owner and stranger, after two priming requests per route.
The probe includes those priming requests: 12 samples per route, upper-middle
median and nearest-rank p95. CPU profiling was enabled on both runs.

| View     | Before median / p95 | After median / p95 |
| -------- | ------------------: | -----------------: |
| Overview |   31.526 / 57.176 s |  13.598 / 38.359 s |
| Card     |   12.665 / 19.703 s |    4.745 / 8.052 s |
| Inbox    |     7.133 / 9.773 s |    0.929 / 1.852 s |

Both runs passed their response/privacy controls. The worker was shared and
wall-clock variability was substantial: Overview's unchanged query shapes also
became much faster. These timings are indicative, not an isolated estimate of
causal speedup or evidence that the latency target is met. The separate VM/plan
gate measures the changed lookup independently of host scheduling.

An earlier four-client unprofiled baseline returned an Overview HTTP 500 under
contention; its complete failed output is retained as `latency-before.log`, not
counted as a passing baseline. Initial profiled four-client experiments and the
final passing four-client run are also retained with their different settings.

After the matched serial runs, **both database files were 670,736,384 bytes**
(163,754 pages at 4,096 bytes), with no WAL bytes or free pages. No schema or
storage-layout change is shipped. The independent compact exact-edge prototype
saved 117,030,912 live bytes for that component, but has not been integrated or
validated as an online migration.

## Verification

```sh
cd core
go test -short ./internal/primitives \
  -run 'TestResourceRead|TestOverviewReadSnapshot|TestOverviewVisitValidation|TestPointSnapshot' -count=1
go test ./internal/server \
  -run '^TestResourcePageSnapshotAuthenticationAndSingleConnection$' -count=1
ANX_PERFORMANCE_TEST=1 go test -p=1 -parallel=1 ./internal/server \
  -run '^TestPerformanceResourceReadSnapshotBudget$' -count=1 -v
ANX_PERFORMANCE_TEST=1 go test -p=1 -parallel=1 ./internal/server \
  -run '^TestResourceAccessCommonReadPerformance$' -count=1 -v -timeout=15m
```

The fixed-seed differential test compares ordered result bytes under the old
pinned JSON policy, new pinned membership policy, and fresh canonical graph.
It includes randomized principals, document/card inheritance and references,
inbox rows, revisions, and repeated ownership grants, revocations and transfers.
Full HTTP tests warm caches before credential expiry and revocation, check both
card routes and inbox, and exercise the default and opt-in inbox readers with a
single database connection. Existing snapshot tests cover epoch changes,
missing epochs, principal/database isolation and cancellation.

The access-index layout experiment and required online migration protocol are
in `docs/design/proposals/compact-exact-access-edges.md`. No migration or physical
DB-size reduction is delivered by this read optimization.
