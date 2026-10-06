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
ANX_PERFORMANCE_TEST=1 go test -p=1 -parallel=1 -timeout=20m -count=1 -v \
  ./internal/server ./internal/storage ./internal/testutil/perfguard -run TestPerformance
```

The fixture is synthetic; it never opens an existing workspace or reads real
credentials. `internal/testutil/perfguard.Seed(ctx, db, contentDir, owner,
seriesAnchor)` creates 4,096 cards, documents, artifacts, content-bearing events,
plans, inbox items, series points, principals and runs; nine boards include eight
private boards. Four PM record families contain 4,096 entries each. Deterministic
IDs and content are shared, while the explicit series anchor keeps observations
inside the requested window. Canonical ownership/reference triggers remain on.
Content-addressed artifacts share one synthetic text blob to reduce disk work.

The route test constructs one migrated workspace and reuses it across every
route, principal and sample. The startup test needs a separate schema-58 corpus
to measure a real upgrade. A standalone fixture integrity test checks table
cardinalities. Bulk construction takes tens of seconds rather than issuing
thousands of HTTP writes; there is no persisted cross-run cache that can conceal
schema or trigger changes. New targeted tests should call the same generator,
then add valid point records through store APIs and measure only the request.

## Route coverage and SQL plans

`internal/server/testdata/resource_access_routes.json` is the authority for route
coverage. Every entry must have either a budget in `performance_routes.json` or
an explicit mutation reason in `performance_write_routes.json`. Read-only POSTs
(ref resolution, report previews and PM turn context) have budgets too. A new or
stale entry fails in the short tier; GETs cannot be classified as mutations.

Each route uses an owner (or the appropriate agent) and an unrelated principal.
Point fixtures require successful authorized responses; denial expectations
follow each route's public/private semantics. Enrollment polling separately
requires its poll credential. Series responses must contain populated data, and
selected collection responses must contain known records. These are representative
selectors, not every possible route filter or a replacement for privacy tests.

One warm-up precedes four measured requests. Nearest-rank p95 is the maximum of
those four measurements; the default 500 ms budget also has a one-second context
deadline. SQL statement and returned-row counts provide machine-independent
bounds, including discarded authorization/projection rows. Series observations
have a separately documented fixed cap. SSE requests exercise the header flush
and two data/tick flushes, including an invalidation for agent streams; an error
event or a missing second poll fails. Network transport latency is not measured.

A wrapper around the canonical SQLite driver captures raw and authorization-
rewritten statements, including prepared and transactional operations. Every
unique SQL plus bound-argument combination is explained outside timing/capture.
Tables with at least 1,024 rows are discovered from the fixture. Plans fail on
large-table SCANs (including full covering-index scans), automatic indexes, or a
registered custom function in WHERE over a large relation. Alias handling includes
quoted and schema-qualified identifiers. Classification is conservative; SQLite
owns execution and its actual plan, and this lexer is not a general SQL parser.

`performance_plan_allowlist.json` starts empty. An exception must identify the
exact trimmed SQL SHA-256, exact finding, and a reviewed reason explaining the
index, bound and unavoidable cost. It cannot exempt a route or an entire table.
Changing SQL or its plan requires reviewing the exception again. Do not raise a
budget or add an exception just to hide O(workspace) behavior. Populate new large
record families and high-fanout selectors when adding endpoints; an empty table
or a shallow history is not useful performance evidence.

## Startup and migration readiness

The startup gate builds an actual schema-58 workspace, measures upgrading through
all current migrations with a 90-second budget, then measures a warm open with
a five-second budget. It includes blob access-manifest backfill and a database
ping, not merely opening a connection. A failed upgrade does not proceed to the
warm-open measurement. This is a database/store readiness proxy; deployment and
network readiness checks require their own integration coverage.

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

The initial scale run is red, rather than allowing known problems through. The
separate authorization repair must land before assessing downstream route timing:
its per-statement text scan currently overwhelms most request budgets. A one-row
work request exceeds one second; schema-58 reconciliation/blob migration exceeds
90 seconds. These are baseline defects, not justification for weaker thresholds.
The following independently visible hazards require remedies that preserve
privacy, legacy data and response contracts:

| Severity | Source                                                                                                                       | Affected reads                               | Cost before a request/page bound                                                                                                                                           |
| -------- | ---------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P1       | `internal/primitives/work_store.go:733`, `:518`                                                                              | `/work`, CLI orient                          | Loads all cards and metadata, projects and sorts O(C log C) before LIMIT.                                                                                                  |
| P1       | `internal/primitives/derived_store.go:164`, `internal/server/inbox_handlers.go:326`                                          | `/inbox`, `/inbox/summary`, overview         | JSON recipient filtering, full projection load/sort O(I log I), then per-item enrichment and subject checks.                                                               |
| P1       | `internal/server/stream_handlers.go:261`, `:491`, `:349`                                                                     | Inbox, events and receipt streams            | Repeats projection/history or subject work each tick; event history is loaded and sorted O(E log E).                                                                       |
| P1       | `internal/primitives/docs_store.go:120`, `:135`                                                                              | `/docs`, doc point/history reads             | Event joins use TRIM/COALESCE rather than indexed equality, scanning E even for one document.                                                                              |
| P1       | `internal/primitives/docs_store.go:1480`                                                                                     | `/docs/{id}/revisions`                       | Unpaginated revision history with queries per revision, O(R) SQL round trips plus authorization work.                                                                      |
| P1       | `internal/primitives/docs_knowledge.go:327`, `:335`, `:372`                                                                  | Document search                              | Full matching-corpus ranking and correlated event scans, O(matches × E); private corpus statistics need privacy review too.                                                |
| P1       | `internal/pm/store.go:83`, `internal/server/overview_handlers.go:163`                                                        | PM lists and overview                        | 200-row SQL windows are accumulated until exhaustion; O(PM records) memory/work before projection. Optional-filter ORs and JSON predicates need indexed selector analysis. |
| P1       | `internal/auth/admins.go:90`, `internal/server/overview_handlers.go:84`, `:99`                                               | Auth admin directory and overview enrichment | Per-entry host lookup and repeated full actor/principal directories; cost grows with identity cardinality.                                                                 |
| P1       | `internal/storage/workspace.go:65`, `internal/storage/migrations.go:1018`, `:1020`, `internal/primitives/access_blobs.go:19` | Upgrade and store startup                    | Reconciliation/content backfills grow with corpus; scan plus per-blob updates occurs before readiness. Progress is now signalled, but runtime cost still needs repair.     |

CLI `internal/app/daily_loop.go:86` requests `/work?limit=200`: the client makes a
bounded request, but inherits the server's unbounded implementation. Work commands
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
as mitigation. Review startup/backfill cost and advancing progress separately
from readiness. Check fixture coverage, successful point selectors, second SSE
ticks, and narrowly justified SQL exceptions before trusting a green result.
