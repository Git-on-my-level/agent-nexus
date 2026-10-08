# Performance guardrails

A read or stream tick must be bounded by its selector or page, with indexed
filtering before materialization. Hot-path O(workspace) work is a merge-blocking
P1, including an authorization query that rebuilds a graph or scans stored text.
A LIMIT applied after loading, sorting, or enriching every record is not a bound.

## Idle streaming

Inbox, event and notification-receipt streams observe SQLite `data_version` on
one dedicated connection shared by the handler. The connection performs only
the version PRAGMA, holds no transaction and is released when the last stream
disconnects. An unchanged tick performs O(1) metadata work and a keepalive,
without authorization graph, projection or summary recomputation. Poll intervals
have a 100 ms floor; the default remains one second.

Every committed database write invalidates this conservative cursor, including
external writers, non-event permission changes and derived projection updates.
On changes, streams retain the existing bounded page readers and authorization
checks. Inbox pages (200 candidates plus lookahead) share computation for matching
authorization scope, recipient and pagination position. The cache holds at most
128 pages and expires delivered pages after a poll interval so a late subscriber
gets current ages and health. An incomplete inbox sweep always continues; commits
during a sweep cause another sweep from the head. Event/receipt history chunks
continue without waiting for the poll timer. Global commit versions are never
exposed as client cursors.

Custom stores without the observer, and SQLite pools explicitly limited to one
connection, retain uncached polling rather than reserving their only connection.
The new cursor needs no table scans, indexes or migration. Existing changed-page
authorization and enrichment costs remain covered by their separate scale gates.

Reproduce the five-stream synthetic CPU/latency measurement (fixture setup and
initial multi-page delivery are outside the CPU profile):

```sh
cd core
ANX_STREAM_PROFILE="$PWD/stream-idle.pprof" go test ./internal/server \
  -run '^TestStreamIdleScaleProfile$' -count=1 -v -timeout=15m
go tool pprof -top -cum stream-idle.pprof
go test ./internal/server ./internal/primitives -run \
  'TestFiveIdleInbox|TestSharedInbox|TestInboxCommitDuringSweep|TestStreamRevision'
```

The profile includes ten bounded page latency probes; their recomputations are
reported explicitly. The deterministic regression requires zero page reads on
idle ticks, one shared page for five matching readers after a non-event commit,
and complete delivery after a mutation to an earlier page during a sweep.

## Running the gates

Run the cheap route-inventory, SQL-hook/classifier and migration-progress checks:

```sh
cd core
go test -short ./internal/server ./internal/storage ./internal/testutil/perfguard
```

The independent scale tier is advisory. `.github/workflows/performance.yml`
runs four `core-performance-routes` shards, `core-performance-legacy`, and their
executed-artifact coverage check on pushes to main or manual dispatch. It has
no pull-request trigger and no dependency from `ci-ok`, so it does not delay
ordinary PRs or releases. Failed deterministic checks remain visible in that
separate workflow. Run the same tier locally with:

```sh
cd core
ANX_PERFORMANCE_TEST=1 go test -p=1 -parallel=1 -timeout=120m -count=1 -v \
  ./internal/server ./internal/storage ./internal/testutil/perfguard -run 'TestPerformance|TestResourceAccessCommonReadPerformance|TestResourceAccessLargeDenialPrepareAndPMRoutes'
```

The fixture is synthetic; it never opens an existing workspace or reads real
credentials. `internal/testutil/perfguard.Seed(ctx, db, contentDir, owner,
seriesAnchor)` creates 4,096 cards, documents, artifacts, content-bearing events,
plans, inbox items, series points, principals and runs; nine boards include eight
private boards. Four PM record families contain 1,024 entries each (4,096 PM records total).
The public board contains 3,840 cards; 256 cards belong to the eight private
boards. Each artifact retains a 16 KiB logical content blob; summaries/FTS use
1 KiB prose to avoid redundant copies. Deterministic
IDs and content are shared, while the explicit series anchor keeps observations
inside the requested window. Canonical ownership/reference triggers remain on.
Content-addressed artifacts share one synthetic text blob to reduce disk work.

All routes reuse one migrated workspace, with a fresh instrumented SQL pool and
complete native handler/PM runtime for each case and principal. The first read
cannot inherit another route's authorization cache. Five warm reads reuse only
that pool. A real trigger-backed thread update then advances the authorization
epoch before a separately measured post-invalidation read. Cursor setup uses a
separate handler, preserving the measured handler's cache state. No global cache
reset or production cache API is introduced. `/overview` and the default changes
case explicitly start without a saved visit; a separate `returning-visit` changes
case persists a scoped visit through the setup handler, then requires a recent
private decision in the owner response and a non-null visit cursor for both
principals. Shard order cannot turn that workload into an empty first visit. The
unfiltered default stream retains 8,192+ events, 4,096 distinct baseline threads
and card references, the large-history thread and canonical private-board graph.
After #311 it starts at HEAD: its first completed chunk must skip old history,
and its second must deliver fresh public/private controls. The large-history
case explicitly resumes after event 0000 and checks two ordered bounded pages,
with a private-reference control inside the second page. Resumed and idle cases
check fresh delivery separately; timer keepalive flushes never count as reads.
Both principals exercise every case, and fixture policy is recorded in each
sample. The startup test uses a separate schema-58 corpus
to measure a real upgrade. Every fixture consumer validates table/PM
cardinalities, the private-card distribution, FTS and populated series. The two
authorization hotfix regressions remain separate: their established common-read
fixture and 1,000-root denial graph retain their 25-sample latency, privacy and
SQL prepare-size/time budgets. Bulk construction takes a few minutes on a laptop
with all canonical triggers enabled, rather than issuing thousands of HTTP writes; there is no persisted cross-run cache that can conceal
schema or trigger changes. New targeted tests should call the same generator,
then add valid point records through store APIs and measure only the request.

The schema-65 external-evidence projection is sparse in this generic corpus,
and its inbox ask page is not a dense ask-page fixture. These results do not
certify dense external-evidence alias fanout or every composed inbox selector.
Keep the focused source-reference and inbox privacy/bounds regressions; add
populated high-fanout fixtures when changing those reads.

## Route coverage and SQL plans

`internal/server/testdata/resource_access_routes.json` is the authority for route
coverage. Every entry must have either a budget in `performance_routes.json` or
an explicit mutation reason in `performance_write_routes.json`. Read-only POSTs
(ref resolution, report previews, PM turn context and secret reveals) have budgets too. A new or
stale entry fails in the short tier; GETs cannot be classified as mutations.

Each route uses an owner (or the appropriate agent) and an unrelated principal.
Point fixtures require successful authorized responses; denial expectations
follow each route's public/private semantics. Enrollment polling separately
requires its poll credential. Series responses must contain populated data, and
selected collection responses must contain known records. Adapter inventory requires an explicit series grant (403 for the unrelated
principal); public series metadata/query and PM collections retain their
existing filtered-200 semantics. These are representative
selectors (including unfiltered, large-history and resumed event streams, a one-row
target-board page and a populated one-card ref source), not every possible route filter or a replacement for privacy
tests. The target-board page requires a positive owner result and an explicitly
empty unrelated-reader result. It does not certify default unpaginated board/ref
hydration, whose existing unrelated-reader overrun is tracked in SCA-665. The
one-card ref-source selector likewise does not certify a public source with
thousands of edges; that fanout overrun remains tracked in the same P1.
Secret listing/reveal operations use an encrypted positive fixture and audit
writes. The preview uses a populated fleet-health panel; saved reports cover live activity.
Secret cardinality and document/card revision histories are shallow;
those fanout dimensions need dedicated scale fixtures when changed.

Each case/principal executes one independently isolated first read, five warm
reads, and one read after invalidation of a previously warmed pool. The local
unpartitioned matrix uses the same pool for all seven. CI uses the separately
measured preparation described below and records eight requests per subject.
The warm median must meet the 500 ms ordinary latency reference; nearest-rank
p95 and the maximum are reported separately. A loose secondary maximum (four
times the reference, at least five seconds) bounds stalls and supplies deadlines.
The single first-read and invalidation samples primarily enforce deterministic
statement, row and VM ceilings, with that same loose secondary maximum rather
than a one-sample median. Their finite, source-pinned allowances have separate
cache-phase keys and never widen warm ceilings. All seven requests retain
status, privacy, positive-control and exact-plan assertions.

The four route shards keep both principals and all cache states of each case
together. `performance_shard_weights.json` records measured execution weights;
deterministic longest-processing-time balancing validates exact inventory
coverage before fixture setup. Select a local shard with
`ANX_PERFORMANCE_SHARD=1` (values 1–4); omitting it runs the full matrix. Every
shard uploads its actual executed report. `scripts/check-performance-shards.py`
requires four distinct, current-source reports, all 109+ cases, both principals,
exactly five warm and one first-read/invalidation samples, plus the measured
post-invalidation preparation in the correct order, valid metrics,
effective per-state limits, and successful case completions. Missing, duplicate,
stale, partial and zero-test reports fail. The separate legacy job retains both
25-sample authorization regressions, prepare-size/time checks, cache controls,
native-counter controls and startup measurements without repeating them in each
route shard. `TestResourceAccessCommonReadPerformance` logs its wall-clock
overruns as advisory measurements; SQL and row bounds still fail. The route
matrix retains fatal SQL, row, VM-work, response and privacy checks.

Each CI shard compiles once and runs three independent workers on
the standard four-CPU `ubuntu-24.04-arm` runner. Two workers retain the established `GOMAXPROCS=2`
setting and partition whole cases for the first read and five warm requests.
The third uses `GOMAXPROCS=1` and lower scheduling priority to reduce competition
with warm requests' garbage collection. It runs every case's
post-invalidation request on its own pool, first capturing and validating its
fresh cache-warming request against the first-read limits, then performing the
real epoch-changing write. That extra preparation is measured in the artifact;
no costly cold setup is hidden. Both principals remain in the same shard, and
workers never share an authorization cache. `ANX_PERFORMANCE_WORKER=1`, `2` or
`3` selects a selected shard's worker for local iteration.
CI waits for all processes, preserves their exit results and logs, and merges
their reports without manufacturing successful case completions. The coverage
job verifies each sample's worker assignment as well as the full shard union.
Reports record and validate the actual architecture, CPU count and Go CPU settings. Worker 1 completes its authorized stage before worker 2 constructs its
fixture and completes its authorized stage. Both finish before denied reads.
The post worker waits before constructing its fixture, preventing heavy denial
setup and GC from competing with owner timing checks. Atomic markers include
source, worker, completion time and stage outcome; failure releases peers with
its failed outcome intact. Waits are bounded by the test deadline, recorded
in artifacts and excluded from request timings. Coverage checks actual request
and fixture timestamps against every stage release. CI clears markers before launch.
Measured weights guide balancing; actual CI timings remain the runtime evidence,
since an indivisible slow case can dominate one worker.

Every read has an ordinary 50,000-VM-instruction ceiling alongside 100 statements
and 1,024 returned rows. `sqlite3_stmt_status` counters include indexed aggregate
work, prepared statements, transactions, early close and errors; fullscan, sort
and autoindex work are also recorded. A result with one row can execute thousands
of instructions. Native per-statement overflow, unsupported driver layouts and automatic
schema reprepare without a reliable trace lifetime fail closed. The route
fixture schema stays fixed during measurement. Cached FTS/internal statement
counters reset per execution; nested callbacks preserve parent guards and
capture epochs, so earlier executions cannot inflate a later sample. The test-only adapter validates the pinned modernc connection and
rows fields and uses its generated SQLite API; it does not alter production
connections. VM instructions are a work proxy, not bytes processed by Go UDFs,
so plan checks and the secondary timer remain necessary. See the
[SQLite counter definitions](https://www.sqlite.org/c3ref/c_stmtstatus_counter.html).

Stream cases have independent IDs, headers and allowances. Event coverage
includes the existing selected thread, the unfiltered workspace history, a
4,096-event single thread, resumed history and an idle Last-Event-ID poll.
Inbox snapshot/resumed/idle polls and agent-change invalidations are exercised.
The canonical inbox endpoint is `/stream/inbox`; there is no `/inbox/stream`
alias. Fresh public and inherited-private controls distinguish two actual polls
from header readiness or replay. Event controls are deleted after measurement
with canonical triggers active so sample count does not grow the denial graph.
Network transport latency is not measured.

A wrapper around the canonical SQLite driver captures raw and authorization-
rewritten statements, including prepared and transactional operations. Every
unique SQL shape plus typed argument set is explained outside timing/capture.
This SQLite build enables STAT4: equality/range distributions and LIKE/GLOB
patterns can change a plan. Repeated executions still count toward query/row
budgets; identical SQL text is interned to avoid retaining thousands of copies
of the authorization compiler output. Targeted filter-value tests remain necessary.
Tables with at least 1,024 rows are discovered from the fixture. Plans fail on
large-table SCANs (including full covering-index scans), automatic indexes, aggregate/window SEARCH inputs, or a
registered custom function in WHERE over a large relation. Aggregate/window names come from SQLite’s actual function registry, including MIN/MAX expressions and JSON aggregates. Referenced view definitions are expanded recursively for analysis, so a view cannot hide the aggregate or its input alias. Their SEARCH findings are conservative across nested SQL; bounded optimized cases need exact review rather than a wildcard. Alias handling includes
quoted and schema-qualified identifiers. Classification is conservative; SQLite
owns execution and its actual plan, and this lexer is not a general SQL parser.

`performance_plan_allowlist.json` identifies an exact SQL-shape SHA-256 (including
recursively referenced view definitions) and
specific EXPLAIN findings and the full ordered plan fingerprint (including
duplicate nodes), with a justification and link to the existing P1 issue.
The fingerprint preserves all SQL except the two numeric snapshot epochs emitted
by the authorization compiler: those are data, and change with fixture writes.
Quoted business literals and comments are preserved. Changing a view's bounds
expires its fingerprint even when submitted SQL and EXPLAIN stay identical;
unrelated view definitions do not affect it. Actual SQL is always used
for EXPLAIN; normalization never changes execution. New SQL or findings require
reviewing a new entry. There are no table-wide or route-wide plan exemptions.

The two compiler gates must use the same internal epoch table and value.
Receipt snapshots use `receipt_access_epoch`; ordinary snapshots use
`resource_access_epoch`. The table name stays in the fingerprint. Mixed tables
or additional gates retain their exact literals.

Receipt-stream cases retain the private-thread 200/404 case and add a public
thread for both principals. Initial and fresh payloads must have valid receipt
IDs and digests; comments and empty flushes cannot complete a receipt sample.
Two fixed control records change only delivery timestamps between polls, keeping
receipt ancestry and cardinality stable across five warm requests. The subject
cleanup runs after all seven requests. The independent cache control verifies
cold acquisition, warm reuse, stale-candidate validation after a trigger-backed
write, and acquisition through a second fresh pool. Both authorization epochs
must advance for post-invalidation receipt requests. Sparse receipt cardinality
does not certify the metadata query's full receipt-table scan or dense ancestry;
its exact plan exception states that limitation under SCA-665.

`performance_budget_allowlist.json` records finite baselines for existing hazards
on main, per method/path/case/principal/cache phase, with linked repair justification. Standard budgets
remain the default for every new route. Baselines retain query/row ceilings,
success/denial expectations, positive fixtures, private controls and two SSE data
flushes; exceeding a ceiling still fails. Their purpose is to allow the guardrail
harness to land while SCA-663/664/665/673 and the existing SCA-652 repairs proceed,
not to authorize new O(workspace) work. The merged #302 repairs retired the
previous broad PM/overview exceptions. Remaining entries are remeasured with
the expanded stream fixture and SQLite counters, and retain only dimensions
that exceed ordinary limits. The
ordinary limits stay at 500 ms, 100 SQL executions and 1,024 returned rows
(except the existing fixed series-observation cap).

Completed merged-code measurements replace the former threefold latency
allowances and identity-cache refresh extrapolation. Latency headroom is 20%,
rounded up to 50 ms; overrun counts get 2% headroom with a minimum of eight SQL
executions, 64 returned rows or 1,000 VM instructions. Dimensions below the
ordinary limit use that limit. First
and returning overview visits remain separately sampled. Legacy PM response
projection still issues 159 SQL for actions and up to 164 for decisions; identity
cache refresh is measured directly, rather than extrapolating a multiplier.
Completed denied PM/overview probes take seconds even after indexed principal
lookup. These are existing main hazards linked to SCA-663, not permission to
add repeated lookups. Diagnostic probes provide completed baseline evidence
but intentionally fail; normal mode and CI must independently pass status,
privacy, count, stream and exact-plan checks.

Round-two isolation preserves all previously reviewed warm ceilings numerically.
Cold and post-invalidation caps are measured separately and cannot widen warm
assertions. Shard weights combine prior CI measurements with the initial public
receipt probe; weights are scheduling estimates, never request allowances.

Every remaining route exception carries `core_source_sha256`: a fingerprint of core runtime
Go, module dependencies, local replacement modules, fixture/performance-harness
code, inventories and relevant text assets. Release-version metadata, unrelated
tests and the self-referential budget manifest are excluded. A changed or added
input expires the allowance in the advisory scale tier before corpus construction;
remeasure the linked P1 instead of copying the hash automatically. The cheap
inventory check always requires valid source hashes and finite exact entries,
but freshness does not gate ordinary CI. All other
routes retain their ordinary count ceilings. Remove entries as those repairs land.
A changed read must satisfy the ordinary budget; do not add a baseline for a new
regression. Populate new large record families and high-fanout selectors when
adding endpoints; empty tables and shallow histories are not scale evidence.

Set `ANX_PERFORMANCE_REPORT` to an absolute writable JSON path to collect
synthetic SQL shapes/findings and route samples for review. CI uploads each report as
`core-performance-routes-N`, so reviewers can inspect every shard's plans.
`ANX_PERFORMANCE_DIAGNOSTIC=1`
retains all seven state-specific samples and extends the request deadline to sixty minutes to
observe baseline counts; it always fails as a diagnostic run, and
latency/query/row/work and plan failures still fail. An exact
`ANX_PERFORMANCE_DIAGNOSTIC_ROUTE="METHOD /path"` selector can isolate one
diagnostic route; append ` [case-id]` to select one stream case. Setting it in acceptance mode fails before fixture setup. CI
never enables either diagnostic option.

## Startup and migration readiness

The startup gate builds an actual schema-58 workspace, measures upgrading through
all current migrations with a 90-second budget, then measures a warm open with
a five-second budget. It includes production store construction and a database
ping. Main #305 removed historical blob backfill from startup; explicit blob
maintenance is outside readiness, and unindexed blobs remain fail-closed.
A failed upgrade does not proceed to the
warm-open measurement. This is a database/store readiness proxy; deployment and
network readiness checks require their own integration coverage.

Main #275's batched source-edge reconciliation removes the measured legacy
upgrade overrun. The former fifteen-minute
SCA-664 exception is retired: `performance_startup_allowlist.json` is `null`, so
CI enforces the ordinary 90-second upgrade and five-second warm-open budgets.
If a legacy exception is ever needed, it must be finite, linked to a reviewed P1
and pinned to the current migration version and exact startup/index/backfill
source hashes. Changes expire it in the short tier. Exceptions do not relax
deployment probes automatically; adapters own any bounded probe grace.

Migrations emit `migration_started`, elapsed `migration_progress` every five
seconds, and `migration_finished`. The final signal means work stopped; successful
commit and the absence of an initialization error establish success. No record
contents are logged. Migrations still run transactionally before the listener
opens; progress does not make an unmigrated database ready. Probe supervisors
should permit an upgrade-specific startup grace period while progress advances,
then require normal readiness. They must still bound a stalled upgrade, preserve
logs, and distinguish process exit or errors from continuing migration work.
The generic core supplies signals; deployment-specific probe policy belongs in
the deployment adapter, outside core.

## Sweep findings and current baseline

The authorization repair (#294/#298) is now the base of the harness. Existing
projection, query-plan and startup hazards are tracked separately; the checked-in
exceptions link their P1 issues and cap the current behavior. This is a baseline,
not proof that every reader is bounded. The following independently visible hazards require remedies that preserve
privacy, legacy data and response contracts:

| Severity | Source                                                                                              | Affected reads                          | Cost before a request/page bound                                                                                                                                                                                                                                       |
| -------- | --------------------------------------------------------------------------------------------------- | --------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P1       | `internal/primitives/inbox_freshness.go:7`, `internal/primitives/inbox_reads.go:39`                 | Inbox freshness and summary             | Aggregate freshness still scans scoped thread candidates, with view/status joins. Summary counts and JSON-kind/lifecycle predicates inspect the corpus; output pagination does not bound candidates. #302 removes full page materialization.                           |
| Repaired | `internal/server/event_stream_scanner.go`                                                           | Default and large-history event streams | #311 bounds history traversal and starts an unfiltered new connection at HEAD. Canonical authorization and other stream work retain separate SCA-665 allowances where measured.                                                                                        |
| P1       | `internal/server/stream_handlers.go:261`, `:491`, `:349`                                            | Inbox, events and receipt streams       | Repeated freshness/history and subject work per tick remains. #302's internal 100-record inbox sampling also has a completeness regression, owned by the SCA-665 forward fix; representative two-tick tests do not certify complete feeds.                             |
| P1       | `internal/primitives/docs_store.go:120`, `:135`                                                     | Document point/history reads            | TRIM/COALESCE event joins inspect events rather than using direct indexed equality.                                                                                                                                                                                    |
| P1       | `internal/primitives/docs_store.go:1480`                                                            | Document revisions                      | Unpaginated revision history has queries per revision, O(R) round trips. The generic fixture has shallow histories.                                                                                                                                                    |
| P1       | `internal/primitives/docs_knowledge.go:327`, `:335`, `:372`                                         | Document search                         | Matching-corpus ranking and correlated event scans can grow with matches and event history.                                                                                                                                                                            |
| P1       | `internal/server/pm_runtime.go:310`, `internal/pm/resolution.go:48`, `internal/pm/decisions.go:622` | PM actions and decisions                | Legacy delivery-authority projection resolves work per returned record despite batched snapshots. Measured action/decision requests execute 159/164 SQL. #302's HTTP page selectors and indexed principal lookup retire the prior exhaustion/cache-refresh allowances. |
| P1       | `internal/pm/store.go:101`                                                                          | Internal PM list consumers              | Internal `listRecords` still accumulates 200-row windows to exhaustion; bounded HTTP pages bypass it. Do not apply this finding to the repaired HTTP page loaders.                                                                                                     |
| P1       | `internal/auth/hosts.go:738`, `internal/primitives/store.go:1905`                                   | Host enrichment, thread list            | Indexed host selection still hydrates an agent directory; thread lists retain per-thread subject/summary hydration. Selection indexes do not bound those downstream costs.                                                                                             |
| P1       | `internal/primitives/access_scope.go:56`, `internal/primitives/overview_changes.go:76`              | Fresh transaction/denied reads          | Fresh canonical denial closure and candidate filtering remain costly. Snapshot fallback executes on an epoch miss; cached denied/reference membership still executes on a hit.                                                                                         |
| P1       | `internal/storage/workspace.go:83`, `internal/storage/migrations.go:1018`                           | Upgrade and startup                     | Remaining reconciliation grows with corpus before readiness. #275 batches source edges and #305 removes historical blob reads; ordinary startup budgets now pass without an exception.                                                                                 |

#302 also repairs work selection before metadata/projection (`work_store.go:905`),
page-before-enrichment inbox loading (`derived_store.go:167`), bounded PM/overview
loaders, principal lookup and batched admin host lookup. Their former full-load,
N+1 and clock-refresh descriptions no longer apply to these paths.

CLI `internal/app/daily_loop.go:86` requests `/work?limit=200`: the client makes a
bounded request and now inherits #302's selection-before-projection bound. Work commands
carry limits/cursors through. No additional client-side unbounded pagination loop
was found in this sweep. Web UI overview server load returns an empty bootstrap;
business data loads use core. `src/lib/server/authSession.js:805` makes a fixed
`/agents/me` request, and layout/resolver loads do fixed configuration/auth work.
No additional workspace-corpus scan was found in those server loads. This is a
source sweep, not proof that every consumer or filter is bounded.

## Review prompt to adopt

For each changed read and stream tick, trace the complete request through
authorization, SQL, projection, enrichment and pagination. State its complexity,
maximum selector/page cardinality, and the indexes used before filtering or
LIMIT. Inspect the scale result and actual EXPLAIN plans, including denied reads.
Treat any hot-path O(workspace) scan, graph rebuild, read-time JSON/text extraction,
materialization-before-limit, or N+1 loop as a merge-blocking P1. Require a bounded
implementation and meaningful scale evidence; do not accept a future follow-up
as mitigation for new work. Existing main exceptions must remain exact, finite
and linked to the P1 repair; do not broaden them for a changed hot path. Review startup/backfill cost and advancing progress separately
from readiness. Check fixture coverage, successful point selectors, second SSE
ticks, and narrowly justified SQL exceptions before trusting a green result.
