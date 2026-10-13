# Independent document and comment search

SCA-760 / PR #376 activates the production head/comment reader on
`GET /docs/search`. Migration **82** follows main's migration **81** (projection
retry backoff). Upgrade tests build real schema-80 (release 0.12.60) and schema-81
databases, reopen them through ordinary startup, and verify both migrations,
strict ordering, preserved retry columns and online capture of existing comments.
Release 0.12.61 also uses schema 81.

## Activation, fallback and recovery

`ANX_INDEPENDENT_DOCUMENT_SEARCH` defaults to enabled. Selection requires durable
completion of both event and head backfills, no pending comment normalization or
unready document head, and no pending parent attachment. A fresh empty workspace is certified immediately. The core
starts both resumable workers and cancels/joins them at shutdown. This has been
exercised in disposable workspaces; it is not a claim of deployment to a user's
workspace.

Set `ANX_INDEPENDENT_DOCUMENT_SEARCH=false` in the core process configuration to
select the aggregate implementation. Configuration changes take effect on
process restart; no migration rollback is needed. Reader selection itself does
not wait for a backfill. Re-enabling selects the independent reader once its
certification is complete.

Comment APIs no longer rebuild the aggregate, read a document blob, or enumerate
peer comments. Canonical triggers record dirty document IDs in the same
transaction. Fallback repairs one dirty document per request and per maintenance
tick, with a five-second transaction deadline. It reads the current blob outside
the transaction, rechecks revision/artifact/hash/type inside it, and rebuilds
from current canonical metadata and live comments. It clears the dirty marker
atomically. A successful fallback response is never admitted while dirty rows
remain. A backlog yields HTTP **503**, `search_repair_pending`, and
`Retry-After: 1` until repair catches up. This is a fail-closed operational
switch, **not a zero-downtime promise**: lazy aggregate reconstruction still costs
O(the repaired document's body and comment history). Workers also repair while
the independent backfill is incomplete. Once certified and enabled, they leave
aggregate repair dormant until fallback is selected.

Canonical source records remain authoritative. Core normalization errors abort
the source transaction. External mutations commit with bounded raw capture and
remain pending if later normalization fails; no successful mutation can commit
without its required capture.
Unavailable blobs or failed maintenance do not certify an incomplete head.
Worker errors are rate limited and never include source payloads.

## Search semantics and response

The compatibility rules and remaining intentional differences are executable contracts:

- Phrase-free AND matches across the current head and live comments of one
  parent. Each term probe reads already-authorized postings; private resources
  cannot supply a missing term. At most 4P driver keys and eight indexed term
  probes per key are admitted per page; no comment history is hydrated.
- Phrases must occur in one current head or one live comment. Cross-resource
  phrases remain an intentional difference from the concatenated aggregate.
- Terms use released unicode61 `remove_diacritics=1` Latin folding at both index
  and query time: `cafe`, `CAFÉ` and decomposed `café` agree. This preserves
  unicode61's historical exception for precomposed multi-diacritic characters.
  Migration 83 adds normalization-version certification for preview-82 stores
  and resets their two singleton job cursors, without scanning history. Online
  workers re-normalize heads/comments in bounded chunks; independent search
  requires both version-2 jobs complete and parent attachment complete. Until
  then, reads use the repaired aggregate. Checkpoints survive restart.
- Each source contributes at most 65,536 raw UTF-8 bytes, then at most 65,536
  normalized bytes and 4,096 distinct terms. Head metadata precedes body text.
  Truncation is retained even when the bounded body cache itself was clipped.
  `search_coverage` exposes the selected mode and these caps, including on empty
  responses. The aggregate fallback retains its released FTS semantics.
- Query input is limited to 1,024 bytes and eight normalized terms. A single
  punctuation-separated token is a normalized phrase, preserving the released
  quoted-token parser behavior; whitespace-separated tokens use AND.
- Independent results use stable document insertion order and rank 1, avoiding
  corpus-dependent ranking. Each parent appears once across an unchanged
  traversal even if hundreds of its comments match. Pages contain at most 100
  parents. Bounded verification or filters can produce an empty page with an
  opaque continuation; callers must follow it until absent. A private-only
  query returns neither results nor a continuation.
- Tags, knowledge, hosts and active/archived/trashed state filters use canonical
  metadata. Search returns canonical document metadata, the live comment-count
  rollup and a bounded last-comment preview. Optional list-only revision counts
  and head character counts are omitted; ordinary document listing retains them.

`TestCommentSearchIntentionalSemanticImprovements` compares the differing
semantics against actual aggregate FTS. The randomized test now runs **324**
comparisons through the **production SearchDocuments reader**, versus actual
aggregate FTS, across three seeds, mutation rounds and single/parent-AND/diacritic/phrase queries.
It checks membership in their shared bounded ASCII domain rather than pretending
different ranking or suffix coverage is parity.

## Authority and continuations

The canonical resourceaccess policy applies before candidate pagination and
payload hydration. A posting must still belong to a current document and, for
comments, an extant, nontrashed message in its current backing thread.
Document/thread ownership and referenced private resources remain authoritative.
A private contributor still constrains the whole document according to the
released ownership graph. The per-thread count is safe under that same graph.
Projection copies never grant access.

Binding validation, posting admission, normalized text hydration and response
metadata run in one immutable SQLite read transaction. Request readers reuse the
existing epoch-admitted denial snapshot; all consuming statements retain its
epoch guard. Trusted internal callers are also pinned to a read transaction.

Migration 82 creates a persistent random 32-byte cursor key and monotonic search
generation. Triggers advance the generation on head/comment changes, lifecycle
changes, deletion and reattachment. Both readers bind encrypted/authenticated
continuations to the current authority epoch, content generation, principal,
query, filters and reader mode. Any binding change returns HTTP 400 `invalid_request`
with an invalid-cursor message (restart); an unchanged cursor survives process restart. Deletion/recreation
cannot reset the workspace generation. Hidden content changes can invalidate a
cursor conservatively but never expose identities, text, counts or term-derived
private-only continuations.

Production and registered-route tests exercise owner/stranger/unauthorized-agent
controls, private references, revocation, lifecycle filters, head/comment edits,
deletion/restoration, duplicate-parent pagination, changed filters and principal,
fallback after a new comment edit, restart, and a body changing during blob I/O.

## Online construction and write bounds

Event capture visits one source primary key per transaction, with a durable
nullable cursor and five-second capture transaction deadline (a preview upgrade
may replace up to 4,096 old terms). Pending event raw text
and source version are read without a write transaction; preparation and JSON
encoding run outside the write lock. The guarded apply has a separate five-second
deadline for at most 4,096 terms, matching the document-head maintenance budget.
It applies only if source version, raw bytes, truncation and pending state still
match; concurrent edits remain pending. Busy acquisition yields immediately.
A durable pending cursor (migration 85, metadata only) advances before each
attempt and wraps through two indexed seeks. Failed rows remain uncertified,
rotate behind other pending rows, and surface through the worker's rate-limited
error callback. This bounds write-lock duration and prevents queue starvation.
Head capture visits one current
document, reads its blob outside the transaction and rechecks its complete source
binding before commit. Parent attachment visits one comment key per chunk.
Final empty seeks and completion checkpoints commit with their source checks.
Triggers capture inserts/edits behind committed cursors. Cancellation,
contention and failure leave the checkpoint resumable. Migration/startup creates
empty projection tables and indexes, never copies historical content.

`comment_search_heads/postings` are the canonical per-event normalized cache and
term lookup used when a parent attaches. `document_search_resources/postings`
materialize that parent binding for grouped traversal; they are actively
maintained lookup indexes, not a second normalizer or unused reader path.
Stored byte counts let the executor admit text without decoding every candidate.
Per-head artifact/revision indexes invalidate current head captures without
scanning historical revision tables.

One comment replacement normalizes at most 64 KiB and replaces at most 4,096 old
and 4,096 new terms. It updates only that event and its document bindings, using
event/resource primary keys and resource-local term indexes. Imports may share a
thread among at most 64 documents; canonical document mutations enforce that cap
before an unbounded comment fanout can arise. Ordinary documents own one thread.

Candidate traversal uses `(term,doc_n,res_n)`, resource lookup uses numeric primary
keys, and per-source cleanup uses `(res_n,term)`. The executor admits at most 4P
visible candidate keys (P <= 100), hydrates at most 4 MiB including rejected
candidates, and batch-loads response metadata. Authority filtering happens before
that candidate limit: its cold canonical graph and rejected private postings are
not covered by the executor's hydration bound. The broad performance suite must
therefore be reported separately from the bounded executor and the 1,000-comment
fixture; a passing microbenchmark is not certification of arbitrary denial size.

## Measured full operation cost

Same local 1,000-comment document with a 1 MB body, authorized
`UpdateDocumentComment`, warmed authority read, fresh instrumented pool:

| Operation                                                              | SQL calls | Rows read | SQLite VM steps | Local elapsed |
| ---------------------------------------------------------------------- | --------: | --------: | --------------: | ------------: |
| Aggregate-only baseline (e507ccc9 test disables only capture triggers) |        28 |     1,020 |          35,548 |     299.13 ms |
| Dual writer before cutover                                             |        28 |     1,020 |          37,893 |     316.20 ms |
| Independent cutover                                                    |        14 |         9 |           8,252 |      23.46 ms |

The write removes 99.1% of returned SQL rows and 76.8% of VM work in this fixture.
Timing is a local sample, not an SLO. The comparison includes the actual scoped
API, canonical ownership writes and projection triggers, not only normalization.

On the same corpus, an already-repaired aggregate search used 24,675 VM steps
(3.26 ms); independent search used 1,054 (3.32 ms), both returning the edited
parent. This small sample demonstrates reduced database work, not a latency
win over warm FTS. Lazy fallback repair is measured separately from serving.

The full-corpus registered route was also measured against main commit
`2157d4b1`, with the same scale fixture and privacy controls. Warm authorized /
unauthorized search VM work fell from **1,691,601 / 1,781,601** to
**1,886 / 2,039**; cold unauthorized work fell from **3,025,105** to **1,245,517**.
Both branches' advisory scale runs exit nonzero: source-pinned main baselines are
expired, exact plan reviews remain outstanding, and cold canonical authorization
still exceeds the standard 50,000-VM budget. No performance budget or plan
allowance was relaxed. This is remaining performance-gate review, not a passing
whole-workspace scale certification. Raw before/after reports accompany the PR.

Reproduction from `core/`:

```sh
go test ./internal/storage ./internal/primitives ./internal/scopesearch ./internal/server -run 'TestCommentSearch|TestDocumentSearch|TestResourceAccessBlobContentAndDocumentSearch' -count=1 -v -timeout=10m
ANX_PERFORMANCE_TEST=1 go test ./internal/storage -run 'TestPerformanceCommentSearch|TestPerformanceStartup' -count=1 -v -timeout=5m
```

The PR records full test logs, the broader scale-gate result, and any outstanding
performance review. The original uncalled `scopedrepo.SearchCanonicalHook` and
its parallel schema have been removed.

The scoped `document_search_postings` authority shadow is explicitly
`NOT MATERIALIZED`. Parent-AND probes keep their `(term,doc_n,res_n)` index seeks
and canonical authority predicates instead of relying on SQLite's default CTE
inlining decision. Plan regressions cover two/eight terms under canonical,
pinned-denial and point-snapshot policies. The production-reader VM regression
uses 100 and 10,000 unrelated documents with a private matching control;
only warm work after authority admission is budgeted, not cold closure creation.

## External writers and raw capture (migration 84)

Search's canonical event/document triggers use SQLite built-ins only. Event
triggers capture at most 64 KiB of raw text, its truncation bit, source identity
and a pending flag. They remove old terms immediately, dirty current aggregates
and invalidate continuations atomically with the source write. Document metadata
changes invalidate affected heads without calling a connection-local normalizer.

Core's scoped database wrapper installs a construction-time write hook. After
ordinary event-write authorization it normalizes at most 64 bound source
identities inside that same transaction, using the shared Go preparation kernel;
normalization failure rolls back the source and derived changes. The hook does
not enumerate the queue, load document blobs, or read peer comments. Non-core and
literal/bulk writes remain pending for the online worker. That worker stays alive
after certification, uses a cheap indexed idle check without taking a writer
lock, and processes one pending source per bounded transaction.

Independent admission checks current pending state inside its read snapshot.
Pending sources cannot make a stale independent result; the existing aggregate
fallback repairs canonical text lazily or returns retryable 503 until repair is
complete. Canonical source authority still applies before matching and pagination.
A worker failure leaves the source intact and pending, so it can retry. Migration
84 preserves already-normalized preview rows and replaces the old triggers using
metadata changes only; it does not normalize historical sources at startup.

The plain-driver regression installs the production search schema without
unrelated ownership triggers and proves zero user-defined functions are registered.
It exercises insert/edit/trash/restore/delete and worker catch-up. Full workspace
compatibility is exercised by the CLI's legacy fixture driver, which retains the
pre-existing reference/ownership function contract but has no search function.
Separate reader tests cover pending-state fallback, privacy changes before
processing, and synchronous source rollback on normalization failure.
