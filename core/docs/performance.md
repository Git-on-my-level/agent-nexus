# Performance guardrails

A read or stream tick must be bounded by its selector or page, with indexed
filtering before materialization. Hot-path O(workspace) work is a merge-blocking
P1, including an authorization query that rebuilds a graph or scans stored text.
A LIMIT applied after loading, sorting, or enriching every record is not a bound.

## Running the gates

Run the cheap route-inventory, SQL-hook/classifier and migration-progress checks:

```sh
cd core
go test -short ./internal/server ./internal/storage ./internal/testutil/perfguard
```

Run the independent scale tier (the `core-performance` CI job is required by
`ci-ok`):

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

The route test constructs one migrated workspace and reuses it across every
route, principal and sample. The startup test needs a separate schema-58 corpus
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
selectors (including a populated public-thread event stream and a one-row
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

One warm-up precedes three measured requests. Nearest-rank p95 is the maximum of
those three measurements. Explicit existing-main baselines with an allowance of
at least ten seconds use two full measured requests instead, including the
first/cold request; their maximum is a conservative smoke budget with reduced
sampling confidence, not a reliable tail-latency estimate. Every sample retains
all count, plan, status and privacy checks. The default 500 ms budget also has a one-second context
deadline. SQL statement and returned-row counts provide machine-independent
bounds, including discarded authorization/projection rows. Series observations
have a separately documented fixed cap. SSE requests exercise the header flush
and two data/tick flushes, including an invalidation for agent streams; an error
event or a missing second poll fails. Network transport latency is not measured.

A wrapper around the canonical SQLite driver captures raw and authorization-
rewritten statements, including prepared and transactional operations. Every
unique SQL shape plus typed argument set is explained outside timing/capture.
This SQLite build enables STAT4: equality/range distributions and LIKE/GLOB
patterns can change a plan. Repeated executions still count toward query/row
budgets; identical SQL text is interned to avoid retaining thousands of copies
of the authorization compiler output. Targeted filter-value tests remain necessary.
Tables with at least 1,024 rows are discovered from the fixture. Plans fail on
large-table SCANs (including full covering-index scans), automatic indexes, or a
registered custom function in WHERE over a large relation. Alias handling includes
quoted and schema-qualified identifiers. Classification is conservative; SQLite
owns execution and its actual plan, and this lexer is not a general SQL parser.

`performance_plan_allowlist.json` identifies an exact SQL-shape SHA-256 and
specific EXPLAIN findings and the full ordered plan fingerprint (including
duplicate nodes), with a justification and link to the existing P1 issue.
The fingerprint preserves all SQL except the two numeric snapshot epochs emitted
by the authorization compiler: those are data, and change with fixture writes.
Quoted business literals and comments are preserved. Actual SQL is always used
for EXPLAIN; normalization never changes execution. New SQL or findings require
reviewing a new entry. There are no table-wide or route-wide plan exemptions.

`performance_budget_allowlist.json` records finite baselines for existing hazards
on main, per method/path/principal, with linked P1 justification. Standard budgets
remain the default for every new route. Baselines retain query/row ceilings,
success/denial expectations, positive fixtures, private controls and two SSE data
flushes; exceeding a ceiling still fails. Their purpose is to allow the guardrail
harness to land while SCA-663/664/665 and the existing SCA-652 repairs proceed,
not to authorize new O(workspace) work. After merging #302 at `ac77fae8`
(schema66), all 24 previous PM/overview exceptions and 58 of the other 90
exceptions were retired. Seven PM/denied reads still need narrow allowances;
33 other reads retain only the dimensions that exceed ordinary limits. The
ordinary limits stay at 500 ms, 100 SQL executions and 1,024 returned rows
(except the existing fixed series-observation cap).

Completed merged-code measurements, including Linux CI run `37555029376`, replace the former threefold latency
allowances and identity-cache refresh extrapolation. Latency headroom is 20%,
rounded up to 50 ms; overrun counts get 2% headroom with a minimum of eight SQL
executions or 64 rows. Counts below the ordinary limit use that limit. First
and returning overview visits remain separately sampled. Legacy PM response
projection still issues 159 SQL for actions and up to 164 for decisions; indexed
identity-cache refresh adds two SQL executions and one row in the completed CI
capture, rather than the old directory-refresh multiplier.
Completed denied PM/overview probes take seconds even after indexed principal
lookup. These are existing main hazards linked to SCA-663, not permission to
add repeated lookups. Diagnostic probes provide completed baseline evidence
but intentionally fail; normal mode and CI must independently pass status,
privacy, count, stream and exact-plan checks.

Every remaining route exception carries `core_source_sha256`: a fingerprint of core runtime
Go, module dependencies, local replacement modules, fixture/performance-harness
code, inventories and relevant text assets. Release-version metadata, unrelated
tests and the self-referential budget manifest are excluded. A changed or added
input expires the allowance in the short tier and before corpus construction;
re-review the linked P1 instead of copying the hash automatically. All other
routes retain their ordinary count ceilings. Remove entries as those repairs land.
A changed read must satisfy the ordinary budget; do not add a baseline for a new
regression. Populate new large record families and high-fanout selectors when
adding endpoints; empty tables and shallow histories are not scale evidence.

Set `ANX_PERFORMANCE_REPORT` to an absolute writable JSON path to collect
synthetic SQL shapes/findings and route samples for review. CI uploads this report as
`core-performance-report`, so reviewers can inspect the plans behind the hashes.
`ANX_PERFORMANCE_DIAGNOSTIC=1`
uses one diagnostic sample and extends the request deadline to ten minutes to
observe baseline counts; it always fails as a diagnostic run, and
latency/query/row and plan failures still fail. An exact
`ANX_PERFORMANCE_DIAGNOSTIC_ROUTE="METHOD /path"` selector can isolate one
diagnostic route; setting it in acceptance mode fails before fixture setup. CI
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
upgrade overrun: the schema-58 through schema-66 upgrade takes 17.23 seconds,
with a 5.16 ms warm open, in the merged-code local measurement. The former fifteen-minute
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

| Severity | Source                                                                                              | Affected reads                    | Cost before a request/page bound                                                                                                                                                                                                                                       |
| -------- | --------------------------------------------------------------------------------------------------- | --------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P1       | `internal/primitives/inbox_freshness.go:7`, `internal/primitives/inbox_reads.go:39`                 | Inbox freshness and summary       | Aggregate freshness still scans scoped thread candidates, with view/status joins. Summary counts and JSON-kind/lifecycle predicates inspect the corpus; output pagination does not bound candidates. #302 removes full page materialization.                           |
| P1       | `internal/server/stream_handlers.go:261`, `:491`, `:349`                                            | Inbox, events and receipt streams | Repeated freshness/history and subject work per tick remains. #302's internal 100-record inbox sampling also has a completeness regression, owned by the SCA-665 forward fix; representative two-tick tests do not certify complete feeds.                             |
| P1       | `internal/primitives/docs_store.go:120`, `:135`                                                     | Document point/history reads      | TRIM/COALESCE event joins inspect events rather than using direct indexed equality.                                                                                                                                                                                    |
| P1       | `internal/primitives/docs_store.go:1480`                                                            | Document revisions                | Unpaginated revision history has queries per revision, O(R) round trips. The generic fixture has shallow histories.                                                                                                                                                    |
| P1       | `internal/primitives/docs_knowledge.go:327`, `:335`, `:372`                                         | Document search                   | Matching-corpus ranking and correlated event scans can grow with matches and event history.                                                                                                                                                                            |
| P1       | `internal/server/pm_runtime.go:310`, `internal/pm/resolution.go:48`, `internal/pm/decisions.go:622` | PM actions and decisions          | Legacy delivery-authority projection resolves work per returned record despite batched snapshots. Measured action/decision requests execute 159/164 SQL. #302's HTTP page selectors and indexed principal lookup retire the prior exhaustion/cache-refresh allowances. |
| P1       | `internal/pm/store.go:101`                                                                          | Internal PM list consumers        | Internal `listRecords` still accumulates 200-row windows to exhaustion; bounded HTTP pages bypass it. Do not apply this finding to the repaired HTTP page loaders.                                                                                                     |
| P1       | `internal/auth/hosts.go:738`, `internal/primitives/store.go:1905`                                   | Host enrichment, thread list      | Indexed host selection still hydrates an agent directory; thread lists retain per-thread subject/summary hydration. Selection indexes do not bound those downstream costs.                                                                                             |
| P1       | `internal/primitives/access_scope.go:56`, `internal/primitives/overview_changes.go:76`              | Fresh transaction/denied reads    | Fresh canonical denial closure and candidate filtering remain costly. Snapshot fallback executes on an epoch miss; cached denied/reference membership still executes on a hit.                                                                                         |
| P1       | `internal/storage/workspace.go:83`, `internal/storage/migrations.go:1018`                           | Upgrade and startup               | Remaining reconciliation grows with corpus before readiness. #275 batches source edges and #305 removes historical blob reads; ordinary startup budgets now pass without an exception.                                                                                 |

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
