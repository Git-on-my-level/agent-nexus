# Overview reference lookup and read admission

Overview now shares the existing `BeginResourceRead` transaction across work,
changes, inbox, decisions and roster. Identity-only SQL uses its admitted denial
map; reference/prose SQL retains the complete canonical policy. The transaction
ends before recording the visit and before successful response serialization.
The visit keeps its fresh write-transaction authorization. `/overview/changes`
also shares an admitted read phase. The dashboard-pin response uses the same
Overview handler after its existing mutation.

Placement authorization previously used one correlated `NOT EXISTS` with
`source_ref = ref OR target_ref = ref`. SQLite scanned the complete denied-ref
relation for every placement. Two equivalent negative equality probes now use
automatic covering indexes. Metadata/prose checks, case sensitivity, aliases,
NULL behavior and authorization-before-pagination remain unchanged. This is a
shared predicate optimization, not a second authorization implementation.

## Cost and remaining limits

The work windows remain 100 open and 100 closed cards plus lookahead. Candidate
selection retains `idx_cards_work_page`, the native closed-phase indexes and
external metadata index; hydration uses primary keys and
`idx_ref_edges_board_card_page`. The reference membership stage changes from
O(P × D) scans to indexed probes after constructing the existing O(D) denied
reference set, where P is selected placements and D is denied reference
spellings. SQLite may build O(D) temporary indexes. Eligible identity-only
sections reuse constant-time membership without rebuilding that relation.
There is no new workspace scan, per-item query, cache or schema migration.

This slice does **not** make the entire Overview bounded by its output size.
Cold authorization admission, repeated full reference-set construction,
candidate rejection through hidden/lifecycle rows, inbox candidate/freshness
scans and fresh visit validation retain their existing workspace-sensitive
costs. Full SQL capture still attributes most warm time to report-work reference
expansion and visit validation. The p50 <300 ms / p95 <1 s target remains open.
No performance allowance or existing budget was widened.

## Matched local measurements

Measured on an M1 Studio, macOS arm64, Go 1.26.5, `GOMAXPROCS=2`, one client,
alternating owner/stranger, CPU profiling enabled. The host was shared (observed
load averages roughly 19–36); these are measurements, not an isolated-host
causal estimate. The final baseline is PR #379 at `b0e963e1`; this PR is stacked directly on it.

The SCA-747 archive supplied the probe but excluded its database. SSH access to
that host was unavailable. This run reconstructs the documented shape using
`perfguard.Seed` plus 100,000 inbox rows inheriting a private archived thread:
4,096 cards, 104,096 inbox rows, 943,778 rows in each access-edge ledger, and a
684,343,296-byte SQLite file. It is synthetic and is **not** the identical
original database. Before/after use copies of this same prepared local fixture.
The probe matches the prior configuration without optional PM/roster stores;
full configured-handler coverage is separate in the integration/scale tests.

| Overview, seconds            |         Before |         After |
| ---------------------------- | -------------: | ------------: |
| All 12 requests: p50 / p95   | 4.557 / 12.774 | 1.672 / 6.002 |
| Ten warm requests: p50 / p95 |  4.511 / 4.669 | 1.664 / 1.705 |

As in the original probe, p50 uses sorted element `n/2` (upper middle for even
samples), and p95 uses sorted element `(n*95-1)/100`. The first table includes
two priming requests, one per principal. The second excludes them. Ten warm
samples are descriptive, not a robust production percentile estimate.

A separate instrumented warm stranger request used 23 SQL statements / 663
returned rows before and 22 / 662 after; the removed row is a redundant
snapshot admission. The report-work query fell from 3.291 s to
1.380 s; its two endpoint checks changed from a correlated scan to
`SEARCH _anx_denied_refs USING AUTOMATIC COVERING INDEX (ref=?)`. SQL durations
include row consumption and are not added to inclusive handler timings.

## Verification and reproduction

```sh
cd core
GOMAXPROCS=2 GOFLAGS=-p=1 ANX_PERFORMANCE_TEST=1 go test ./internal/server \
  -run '^TestPerformanceOverviewReferenceMembership$|^TestPerformanceResourceReadSnapshotBudget$|^TestResourceAccessCommonReadPerformance$' \
  -count=1 -v -parallel=1 -timeout=8m
GOMAXPROCS=2 GOFLAGS=-p=1 go test ./internal/primitives ./internal/server \
  -run 'TestResourceReadSnapshotRandomizedDifferential|TestOverviewReadSnapshot|TestOverviewVisitValidationUsesFreshWriteGraph|TestResourcePageSnapshotAuthenticationAndSingleConnection|TestOverviewPrivatePayloadExcludedAcrossEpochsAndInboxReaders|TestOverviewAuthenticatedResponsePhases' \
  -count=1 -v -parallel=1 -timeout=8m
```

The placement gate uses 100 records with 1,024 and 4,096 denied identities:
159,261 and 616,989 VM steps respectively, one query and 100 rows. It requires
two indexed equality probes and caps construction plus probes at
`160 * denied + 10000` VM steps. This exposes the inherited linear construction
cost instead of claiming it is constant. The shared identity point gate stays
at 50 VM steps as the denied corpus grows from zero to 4,096.

The randomized test compares 440 ordered resource results and 40 full Overview
payloads against old pinned JSON/OR policy and the fresh canonical graph over
five rounds of ownership grants, transfers and revocations. HTTP tests cover
both inbox readers, private positive/negative controls, credential expiry and
revocation, summary/full Overview, change reads, and a one-connection pool.
Existing visit validation tests revoke authority between read and write.

SCA-765's evidence attachment contains the complete measurement probe, seed
recipe, exact command/cwd/exit-code JSON, all test output (including failures),
SQL and EXPLAIN plans, CPU profiles and environment/fixture cardinalities. Copy
the attached `measurement_probe_test.go` into `core/internal/server/` to rerun;
use only a disposable local workspace. The attachment records exact seed,
build and measurement commands for each revision, including all environment
variables and working directories. The measurement command uses one worker,
ten waves, profiling enabled and the same prepared fixture on both revisions.
For SQL/EXPLAIN use the same probe with `ANX_MEMORY_EXPLAIN=1` and no CPU profile.
Do not use either probe against a serving workspace: initialization and Overview
visits write to the database. Raw fixture databases are not published.
