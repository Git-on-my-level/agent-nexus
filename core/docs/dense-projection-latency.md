# Dense workspace projection authorization latency

The earlier dense fixture hid nearly all canonical work and had no populated
topic projections. Matching edge totals alone therefore missed the expensive
Inbox path. The updated fixture has visible work, twelve visible inbox items,
499 populated topic projections and an agent reader, alongside private threads
and their events. All names, payloads and credentials are synthetic.

## Shape

| Table                                     | Reported hosted rows |    Fixture rows |
| ----------------------------------------- | -------------------: | --------------: |
| resource_access_edges / exact_edges, each |              248,867 |         249,930 |
| resource_access_mention_buckets           |              130,709 |         133,112 |
| resource_access_mentions                  |               17,037 |          16,639 |
| resource_access_external_edges            |               34,797 |          35,038 |
| resource_access_identities                |               11,726 |          11,690 |
| auth_access_tokens                        |               16,262 |          16,262 |
| auth_refresh_sessions                     |               14,997 |          14,997 |
| ref_edges                                 |               12,491 |          12,491 |
| events                                    |                2,765 |           2,765 |
| inbox_hidden_subject_refs                 |                1,280 |           1,280 |
| work_evidence_index                       |                  959 |             959 |
| idempotency_replays                       |                3,126 |           3,126 |
| artifacts / cards / threads               |      816 / 433 / 499 | 816 / 433 / 499 |

Artifacts generate about 148 rows per exact ledger, about 296 combined; events
generate about 28 per ledger. A separate upper envelope generates exactly 300
rows per artifact in each ledger (244,800 artifact edges alone). The fixture
uses production JSON-to-manifest extraction and SQLite write triggers; no access
edge table is filled directly. Topic projection cardinality and payload shape
were not supplied from production: 499 projections with 20 scalar values and two
prose refs each deliberately exercise the previously missing path. The hidden
subject ledger and credential history are synthetic shape imports.

## Before and after

Full authenticated HTTP requests, four warm repeats after two initial reads,
plus a separate epoch invalidation. After also captures SQLite VM work. Before uses main `21df0cae` with the same
canonical fixture and timing instrumentation; after changes the shared JSON
authorization predicate. These local measurements reproduce a slow-path class,
not the exact hosted 4.7-second elapsed time. Shared-host wall clock is advisory.

| Route / reader                              |    Before median / p95 | After median / p95 |
| ------------------------------------------- | ---------------------: | -----------------: |
| /inbox, legacy                              | 17,911.6 / 21,229.2 ms |   155.0 / 155.4 ms |
| /inbox, scoped flag                         | 17,899.8 / 18,100.1 ms |   155.7 / 157.3 ms |
| /overview, legacy                           |       101.3 / 103.9 ms |   111.1 / 112.3 ms |
| /overview, scoped flag                      |         99.2 / 99.5 ms |   113.1 / 113.4 ms |
| /inbox, amplified envelope + scoped flag    |           not measured |   152.8 / 153.4 ms |
| /overview, amplified envelope + scoped flag |           not measured |   104.3 / 105.8 ms |

Inbox executes eight statements / 216 returned rows in legacy mode and ten /
229 with the flag. Its slow reads were about 3 seconds loading the 100-topic
sample and 14 seconds aggregating freshness. Paging only twelve inbox records
was cheap. The scoped reader alone leaves those shared reads expensive.

Overview's hosted 875–1200 ms was not reproduced here; its local measurement
slightly increases and remains below 300 ms. There is no claim of hosted
before/after improvement until deployment is measured. The attached count-only
probe and existing Overview Server-Timing can identify the remaining difference.

## Change and safeguards

The old projection predicate joined every JSON atom to every denied spelling
with `equality OR prose_match`. SQLite could not use an equality index for that
join. Exact atoms now probe an automatic covering equality index. Prose atoms
first probe a conservative Unicode-folded kind/identity bucket index, then run
the unchanged complete reference boundary matcher. Buckets never grant access.
All canonical contributor checks, scoped filtering before limits/counts and
consuming-statement epoch fallback remain in place, including transaction reads.

Parity tests compare the new predicate with the original full-spelling matcher
for legacy IDs, aliases, JSON keys, Unicode kinds/IDs, punctuation, internal
whitespace, Markdown wrappers and control-byte boundaries. Query-plan tests
require equality searches for exact and prose candidates. The dense fixture
requires at most 32 statements, 350 returned rows and 2,000,000 SQLite VM steps
per request, including invalidation. Visible-result controls prevent an empty
response from appearing fast. The heavy fixture skips `testing.Short()`.

No schema migration, edge compaction, startup backfill or background work is
needed: existing workspaces immediately use the cheaper read predicate. Existing
exact freshness aggregation still visits visible thread/projection metadata;
this removes its JSON-atoms × denied-spellings cross-product rather than adding
a new workspace scan. It does not solve every existing O(workspace) aggregate.

GET /inbox adds fixed Server-Timing phases `threads`, `topic_projections`,
`inbox` and `freshness`, alongside existing auth/serialization/core timing. It
emits no identities or payload values. Existing Overview phases are unchanged.

From `core/`:

```sh
go test ./internal/server -run '^TestOverviewDenseAccessWorkspaceLatency$' -count=1 -v
go test ./internal/primitives -run '^TestProjectionReferenceProbeParity$' -count=1
```

For hosted follow-up, run `projection-latency-probe.sql` using sqlite3's `-readonly`
flag, bind `:reader_username` to the authenticated agent's workspace username,
and capture Server-Timing from the same authenticated /overview and /inbox
repeats. The SQL returns counts and byte sizes without content or credentials.
