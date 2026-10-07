# Access-history density and handler latency

This is the historical #320 fixture report. After rebasing onto #322, #324
and #325, the executable fixture retains main's expanded artifact history,
visible work, populated projections, private Inbox control, summary mode and
SQLite work bounds. See `dense-projection-latency.md` for that current shape.

The earlier card/principal fixture did not model personal's authorization
history. This follow-up uses the supplied production cardinalities, synthetic
content, the production SQLite import triggers, and real authenticated handlers.
It does not clone a production database or claim to reproduce the hosted 4.8 s
Inbox request.

| Table                                               | Supplied production |      Fixture |
| --------------------------------------------------- | ------------------: | -----------: |
| resource_access_edges / resource_access_exact_edges |        245,428 each | 240,877 each |
| resource_access_mention_buckets                     |             130,524 |      130,520 |
| resource_access_external_edges                      |              34,797 |       35,038 |
| resource_access_mentions                            |              16,919 |       16,315 |
| resource_access_identities                          |              11,674 |       11,674 |
| ref_edges                                           |              12,428 |       12,428 |
| events                                              |               2,759 |        2,759 |
| artifacts                                           |                 806 |          806 |
| threads                                             |                 499 |          499 |
| cards                                               |                 433 |          433 |
| actors / agents                                     |             26 / 25 |      26 / 25 |
| derived_inbox_items                                 |                  12 |           12 |

Twenty-four private principal owners are distributed across threads; cards,
prose mentions, historical aliases and navigation edges propagate their privacy.
The reader sees public work and twelve public asks. The fixture asserts that
private card titles do not reach either handler response.

## Amplification path

Canonical events insert into `events`; `access_events_insert` scans
`refs_json`, `payload_json` and `trash_reason` using `ReferenceSQLAtoms`. The atom
extractor recursively retains object keys and string values for legacy bare
IDs, plus complete prose candidates and extracted/normalized typed references.
`access_mention_edge_insert` adds one exact digest per atom and mention prefix
buckets; matching identities create resolved mentions. Work metadata uses the
same atom pipeline and also maintains the external-key projection.

Production's source breakdown identifies artifacts as 46% of access edges and
events as 32%. The fixture now models that distribution rather than putting most
density in events. Synthetic artifact bodies pass through the body-to-manifest
extractor before canonical imports; access indexes are never populated directly.
Their manifests contain six or seven prose references plus scalar values, yielding
140 indexed atoms per artifact. Events contain two prose references and scalar
values, yielding 28 atoms per event. Artifact manifests and event JSON produce
different indexing work, so equal input string counts are not interchangeable.

| Source kind          | Production edges | Fixture edges |
| -------------------- | ---------------: | ------------: |
| artifact             |          113,087 |       112,840 |
| event                |           77,856 |        77,252 |
| work_observation     |           22,144 |        22,382 |
| work_metadata        |           11,180 |        11,100 |
| work_evidence_record |            6,810 |         6,810 |
| thread               |            5,017 |         4,990 |
| card_revision        |            3,002 |         3,002 |
| card                 |            2,423 |         2,423 |

The fixture includes 589 observations and 370 metadata rows. Evidence/revision
row counts were not supplied; 227 synthetic evidence records and 1,501 revisions
reproduce their edge counts. Evidence aliases exercise the published identity
fields while retaining ordinary reference inheritance. Historical aliases are
balanced to retain exactly 11,674 identities. This is a synthetic distribution,
not a clone of production payloads or its exact ownership topology.

Arbitrary keys and bare
IDs cannot simply be discarded without changing inherited privacy for legacy
records or identities created later. This change does not delete edges, run a
maintenance rebuild, migrate schemas, or backfill at startup.

## Read changes

Structural authorization probes dispatch once per vertex kind and aggregate
only that vertex's indexed children. Raw and typed ID probes share an index
lookup, duplicate live ID spellings are skipped, and empty legacy-reference
probes use the covering index's collation.

Exact submitted values resolve through point identity indexes instead of
expanding every historical spelling of every denied resource. Prose retains the
full matcher. Regression tests compare the exact check with the former spelling
graph, including aliases, revisions, tombstones, URL identities and mixed-case
legacy kinds.

The Overview visit ledger can reuse the immutable request closure **only with
an authorization epoch check inside its write transaction**. Changed or missing
epochs evaluate the current graph; custom write policies remain authoritative.
Business mutations retain ordinary transaction checks. There are executable
tests for revocation after the read, missing epochs, custom policies and scope
isolation. Inbox read-model internals are unchanged.

## Measurements and remaining gaps

The following historical measurements used the earlier event-heavy fixture,
before this source-distribution correction. Before is PR #316 commit `467a7b67`; warm p95 uses ten
samples after two initial requests. Epoch-invalidated reads are logged separately
and are individual samples, not a p95 estimate. Loopback measurements include
authentication, SQL/row consumption, enrichment and response serialization;
SQL and assembly phase timings overlap.

| Full handler                          | Before |  After |
| ------------------------------------- | -----: | -----: |
| `/overview`, warm p95                 | 569 ms | 144 ms |
| `/inbox`, warm p95                    |  46 ms |  49 ms |
| `/overview`, after epoch invalidation | 864 ms | 316 ms |
| `/inbox`, after epoch invalidation    | 343 ms | 218 ms |

The dense fixture catches the former Overview regression. Its steady-state
target is 300 ms and wall-clock overruns are advisory (log only). SQL statement,
returned-row and source-cardinality bounds remain fatal. The heavy fixture skips
`testing.Short()`. The invalidated Overview sample is still above 300 ms, so the
all-request target is not fully achieved. The production Inbox delay is not
reproduced by cardinalities alone. Hosted Server-Timing and source-kind counts
are needed to distinguish a different ownership graph, projection/enrichment
shape, concurrent writes, and proxy overhead.

Run from `core/`:

```sh
go test ./internal/server -run TestOverviewDenseAccessWorkspaceLatency -count=1 -v
go test ./internal/primitives -run 'TestResourceAccess|TestOverviewVisitValidation' -count=1
```

The corrected artifact-heavy fixture measured warm p95 of 172 ms for Overview
and 68 ms for Inbox. Individual epoch-invalidated reads took 450 ms and 344 ms,
respectively, with roughly 276 ms in snapshot construction. These are current
synthetic measurements, not a matched before/after comparison with the older
fixture and not hosted measurements. They reinforce the remaining cold-read
budget gap; wall-clock values stay advisory. Overview used 22 SQL statements and
35 returned rows; Inbox used 8 and 6. Fatal request bounds are 40 statements and
200 returned rows, and the eight leading source counts must stay within 2% of
production. Total exact edges, mention buckets and external edges are bounded
as well; the identity count is exact.

The test logs table counts, first/warm/invalidated requests, SQL timing and
Server-Timing. It creates a disposable development workspace and uses no hosted
configuration or credentials.

## Rebase onto #322, #324 and #325

Measured on 2026-10-07. The pre-rebase #320 head `92d7c5f7` measured
Overview/Inbox warm p95 at 180.6/68.3 ms and individual epoch-invalidated
reads at 463.3/344.9 ms. That older fixture had a human reader, mostly hidden
work and no populated topic projections; it is not a matched comparison with
the expanded main fixture.

The following matched comparison uses main `17488440` before the tombstone
fix and the rebased branch after it, with the identical expanded fixture.
Both use 816 artifacts, 2,765 events, 499 projections, 100 visible work items,
twelve visible asks and an inaccessible thirteenth ask. The canonical shape
is 249,934 edges per ledger, 133,112 mention buckets and 11,691 identities.
Four warm repeats follow two initial reads; each route has a separate epoch
invalidation. These local wall-clock measurements are advisory.

| Route / mode                             | Before warm p95 | After warm p95 | Before / after invalidated read |
| ---------------------------------------- | --------------: | -------------: | ------------------------------: |
| `/overview`, legacy                      |         49.3 ms |        49.6 ms |                  55.5 / 54.7 ms |
| `/inbox`, legacy                         |        145.2 ms |       145.7 ms |                150.7 / 150.0 ms |
| `/overview?work_view=summary`, legacy    |         49.6 ms |        49.9 ms |                  53.6 / 54.6 ms |
| `/overview`, scoped                      |         50.6 ms |        50.7 ms |                  55.8 / 55.3 ms |
| `/inbox`, scoped                         |        146.6 ms |       146.1 ms |                151.0 / 151.1 ms |
| `/overview?work_view=summary`, scoped    |         50.9 ms |        49.8 ms |                  55.4 / 55.0 ms |
| `/overview`, amplified                   |         49.0 ms |        49.4 ms |                  55.8 / 57.0 ms |
| `/inbox`, amplified                      |        146.7 ms |       146.4 ms |                150.8 / 151.7 ms |
| `/overview?work_view=summary`, amplified |         50.0 ms |        50.2 ms |                  55.3 / 55.2 ms |

All three modes passed the unchanged fatal limits of 32 statements, 350 rows
and 2,000,000 SQLite VM steps per request, including invalidation. Indexed
projection probes, per-phase admitted transactions, `work_view=summary` and
the computed brief remain intact. Exact-value parity now covers plain and
colon-containing external-key alternate refs in canonical, cached-request and
admitted reads; projection parity covers those refs through candidate buckets
and admitted reads. The storage privacy inventory was regenerated and formatted;
its classifications and fingerprints are unchanged.
