# Live dashboard sources

Prefer ANX live queries when the information already lives in the workspace.
For external facts, declare a host-local adapter and push a series. Use a pasted
number only as an explicitly timestamped snapshot or fallback.

A human or explicitly granted auth-admin declares an adapter with a description,
exact owning enrolled agent, expected interval, and series names/kinds/units:
`anx adapters declare --body-file adapter.json`. Core resolves the host from the
agent. The owning agent then uses `anx series push builds 12 --label initiative=launch`.
The CLI finds the declared adapter and exchanges its existing host-agent token
for a short-lived scoped series-write token before sending the point. It never
creates an undeclared series. `--adapter name` avoids the lookup.

Use `anx adapters list|revoke <name>|delete <name>` to manage sources. Revocation
immediately blocks admitted and future writes, but keeps history. Delete also
removes series data; a tombstone invalidates old tokens permanently. Declaration
and permission changes are in the existing auth audit log. Full host-agent
credentials are never handed to core as third-party source credentials.

`anx series list`, `anx series show builds`, and
`anx series query builds --range 7d --step 1h --agg sum --label initiative=launch`
return observations and provenance. Queries permit at most 200 time buckets,
100 label sets per series, eight label keys and 128 bytes per label value.
The workspace permits 1,000 declared series and 100,000 new or corrected points per UTC day.
Idempotent same-timestamp/same-value retries do not consume another point;
same-timestamp corrections update raw observations and consume ingestion budget. Backfill timestamps must be within 90 days and
at most five minutes ahead. Counters are nonnegative; state points contain a
nonempty string instead of a number (`anx series push health healthy`). Numeric values are finite within ±1e12.
Counters store samples (including resets), rather than computing a rate.

These are technical safety bounds on every deployment, including self-hosted
workspaces, independently of commercial quotas. Push requests have fixed UTC-minute
budgets: 1,200 per adapter and 2,400 across the workspace. Retries count against
the request budget, even when deduplication avoids consuming daily ingestion
budget. Durable counters preserve successful request budgets across restarts;
ingress also counts rejected and malformed attempts in memory. All credentials
for an adapter share its budget. At most two scoped requests per adapter and
four across the workspace can run at once, including a bounded authentication
lane for fresh credentials. Excess traffic receives `429 series_rate_limited`
with `Retry-After` seconds. Normal human operations do not share these limits.
Recognized retry storms are rejected before body reads or database access.

Raw observations older than 90 days compact into daily count/sum/min/max/last
rollups in the daily maintenance job. Queries use read-only snapshots and never
compact data or acquire the workspace write lock. Recent-day summaries are
updated in the point transaction, including exact retries, corrections and
out-of-order points. Day-sized steps scan matching summaries and archived rollups
once for fully covered interior UTC days. Indexed raw scans handle partial edge
days within the exact requested rolling interval; future observations are excluded
from both aggregates and freshness timestamps. Interior days require no raw scans
or per-bucket payload joins.
Admission and indexed freshness reads are batched across labels. Daily rollups are retained forever
with no expiry, per David's 2026-10-05 decision. Rollup updates and raw deletion
commit as one atomic checkpoint; interrupted or replayed compaction cannot count
a sample twice. Historical queries use whole UTC
days and a whole-day step, reporting their reduced resolution. State rollups
retain the last state. `avg` is weighted by the underlying sample count, and
`count` counts samples. A series label set is stale after twice its adapter's
expected interval; provenance includes the last received push as a separate fact.
Queries cover at most 3,650 days, with steps from one second to 3,650 days and
at most 200 buckets per label set. Retaining rollups forever does not permit an
unbounded query response. Default steps round up to fit that bucket limit;
historical ranges first round up to whole days, then choose a whole-day step.
Raw corrections and out-of-order samples must stay within the 90-day backfill
window; rolled-up observations cannot be rewritten.

Raw aggregation is limited to 4,096 observations across all matching label sets.
Dense ranges of at least one day automatically use a whole-day step and report
`resolution: daily`. Daily buckets keep their UTC grid without widening the
requested window. Partial edge days use exact raw observations while retained;
archived summaries contribute only fully covered days because expired raw edges
cannot be reconstructed. Shorter dense ranges return `series_capacity`; select fewer
labels, shorten the range or request a daily step. Daily aggregation is capped at
20,000 summary rows across both recent and archived summaries, plus 200,000 raw
observations across partial edges. The separate edge cap prevents concentrated
backfill from turning a partial-day scan into an unbounded query. Narrower ranges
or label filters are required beyond these bounds. These are query safety limits,
independent of commercial quotas.

The reproducible capacity benchmark seeds 1,000 declared series and measures
queries over 100,000 and 9,100,000 raw points, with either one or 100 label sets.
The larger case covers 91 UTC ingestion-day budgets in a rolling 90-day window.
The fixture upgrades a v50 workspace through the production migration, including
backfill and indexes. Fixture setup is excluded from the query timing:

```sh
cd core
GOMAXPROCS=2 go test ./internal/series -run '^$' -bench '^BenchmarkQueryCapacity$' -benchtime=1x
```

The benchmark runs at UTC noon, so it includes both exact partial-day edges.
Migration 52 adds a covering `(series, labels, ts, value)` index for those edge
aggregates; existing rollup workspaces install it without rebuilding summaries.

Fast benchmarks also cover the full query safety limits: 20,000 daily summary
rows across recent and archived history (both sum and last), and 4,096 raw points
across 100 label sets. These use the production workspace and driver:

```sh
GOMAXPROCS=2 go test ./internal/series -run '^$' -bench 'Benchmark(DailyQuery|DailyEdge|RawQuery)SafetyCapacity' -benchtime=5x
```

`TestQueryReadSnapshotDoesNotTakeWriteLock` pauses an actual query while an
immediate human mutation commits, then checks the reader's snapshot. Separate
tests verify indexed range scans and matching aggregates before and after
maintenance, including a UTC day split between raw points and a daily rollup.
`TestDailyAndAdaptiveQueriesAvoidRawAggregation` guards the daily path in CI:
day-sized queries never aggregate interior raw rows, and automatic downsampling
reads at most the bounded preflight before switching to summaries and exact edges.
`TestDailyQueriesEqualRawForRandomRollingWindows` compares every aggregation and
state payload with raw-only results, including timezone offsets and future points.
The combined edge benchmark covers both 20,000 summaries and 200,000 edge points.

## Panel binding

Chart, metric, metric-strip and table panels accept a single series binding.
The existing metric-chart/evidence-table forms also support it. Keep `data: {}`
for bound panels and put a static snapshot in `fallback`:

```json
{
  "id": "builds", "project_id": "launch", "type": "metric", "title": "Builds",
  "author": "collector", "provenance": "reported", "observed_at": null,
  "freshness": "unavailable", "source_ids": [], "data": {},
  "source": {"series": "builds", "labels": {"initiative": "launch"}, "range": "24h", "agg": "last"},
  "fallback": {"as_of": "2026-10-05T00:00:00Z", "data": {"value": 12, "unit": "builds"}}
}
```

Ranges in panel documents are positive integer `s`, `m`, `h`, or `d` durations,
up to 3650 days. The renderer reads through the saved report's existing API,
refreshes visible reports, exposes adapter/host/last push, and marks fallbacks
with their original "as of" time. Stale data never becomes a current snapshot.
Metrics require an exact single label set; use strips, charts or tables for
several groups. Wide panels declare when the displayed data is truncated.
Examples and schedule snippets are in `adapters/series/`.
