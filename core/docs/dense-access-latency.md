# Access-history density and handler latency

The earlier card/principal fixture did not model personal's authorization
history. This follow-up uses the supplied production cardinalities, synthetic
content, the production SQLite import triggers, and real authenticated handlers.
It does not clone a production database or claim to reproduce the hosted 4.8 s
Inbox request.

| Table                                               | Supplied production |      Fixture |
| --------------------------------------------------- | ------------------: | -----------: |
| resource_access_edges / resource_access_exact_edges |        245,428 each | 247,686 each |
| resource_access_mention_buckets                     |             130,524 |      132,432 |
| resource_access_external_edges                      |              34,797 |       34,640 |
| resource_access_mentions                            |              16,919 |       16,554 |
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

Six prose references per synthetic event produce 48 prefix buckets; scalar
snapshot values add the other atoms. Each event contributes 77 atoms and work
metadata supplies most of the remaining ledger rows. This reproduces the total
edge/event ratio without directly populating any access index.

The ratio of **all** access edges to event rows is not evidence that events own
all those edges. Identifying the actual dominant production writer requires
`SELECT source_kind, count(*) FROM resource_access_edges GROUP BY source_kind`.
The synthetic write path demonstrates how the supplied density can arise; it
does not establish the production source distribution. Arbitrary keys and bare
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

Before is PR #316 commit `467a7b67`, using the same new fixture. Warm p95 uses ten
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
gate is 300 ms. The invalidated Overview sample is still above 300 ms, so the
all-request target is not fully achieved. The production Inbox delay is not
reproduced by cardinalities alone. Hosted Server-Timing and source-kind counts
are needed to distinguish a different ownership graph, projection/enrichment
shape, concurrent writes, and proxy overhead.

Run from `core/`:

```sh
go test ./internal/server -run TestOverviewDenseAccessWorkspaceLatency -count=1 -v
go test ./internal/primitives -run 'TestResourceAccess|TestOverviewVisitValidation' -count=1
```

The test logs table counts, first/warm/invalidated requests, SQL timing and
Server-Timing. It creates a disposable development workspace and uses no hosted
configuration or credentials.
