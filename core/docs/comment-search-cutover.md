# Independent comment search cutover

SCA-760 / PR #376 now has an active canonical writer and online backfill. Migration
81 installs empty derived tables and event triggers; `anx-core` starts the
maintenance worker and cancels/joins it on shutdown. The earlier uncalled
`scopedrepo.SearchCanonicalHook` and its separate schema have been removed.
The bounded `scopesearch` executor remains shared. `/docs/search` still uses the
aggregate, and comment APIs still rebuild it. This completes the writer/backfill
portion, not reader activation or the aggregate retirement criterion.

## Chosen semantic improvements

These are intentional changes for the independent reader, exercised by
`TestCommentSearchIntentionalSemanticImprovements` against actual aggregate FTS:

- AND and phrase matches must occur in one resource. Two unrelated comments, or
  the document body plus a comment, cannot manufacture one hit. A phrase cannot
  bridge the newline between comments. Every hit therefore has coherent evidence
  in its own source. The eventual document-head adapter must apply this same rule.
- Accents remain significant: `café` matches `CAFÉ`, but not `cafe`. This preserves
  spelling distinctions erased by default FTS5 diacritic folding. Normalization
  lowercases Unicode letters/numbers; no Unicode canonical-equivalence folding
  is promised.
- Coverage is bounded by the first 65,536 raw UTF-8 bytes, then by 65,536 normalized
  bytes and 4,096 distinct terms. This makes work independent of comment history
  and huge repetitive/punctuation input. Projection metadata and executor hits
  expose actual indexed bytes and truncation. Text after that prefix cannot
  match. This trades suffix recall for predictable cost with explicit coverage;
  a serving contract must expose that limitation, including on empty results.

`TestCommentSearchRandomizedAggregateParity` runs 324 comparisons (three seeds,
12 documents per seed, three mutation rounds, 12 queries, single/AND/phrase),
using actual canonical event triggers, actual aggregate `SearchDocuments`, and
the shared executor over scoped live-table snapshots at three hits per page.
It checks comment-derived document membership in the common bounded ASCII domain
through edits, deletion and restoration. It does not certify BM25 ordering,
head-only results, filters or the HTTP response contract. The test adapter has
fixed generations and is not production code.

## Atomic canonical capture and audience certification

Every event INSERT, UPDATE of content/identity/ownership/lifecycle columns, and
DELETE maintains `comment_search_heads` and `comment_search_postings` inside the
same SQLite transaction. This covers create/edit/delete/restore, imports,
replacement, hard deletion, thread movement and direct reference changes. A
projection failure aborts the source write. There is no API-path opt-in to miss.
All live `message_posted` thread events are captured, so later document/thread
attachment does not require rescanning history on the attaching mutation.

A head is bound to its canonical event and current thread with
`authority_binding=canonical-event-v1`. The binding is enforced in SQL. The
scoped relations in `primitives/access_scope.go` require an extant, nontrashed
message event in that same thread and apply the canonical event denial predicate
before count, selection or payload hydration. This inherits document/thread and
private-reference policy. It does not invent an all-members audience or copy
permissions into the projection. A visibility change on a referenced resource
changes current authority immediately; the projection's source binding remains
valid without fan-out rewriting comment text.

The durable job's `done=1` certifies complete source capture under that binding
policy, not a new audience grant or permission to serve the new HTTP reader.
Tests check owner, stranger and unauthorized-agent access, private references
introduced by editing, grant/revocation in a reused request context, and privacy
after backfill. Missing/deleted/reparented sources cannot expose orphan terms.
The new tables are explicitly classified in the storage privacy inventory.

## Online backfill and recovery

The worker visits canonical event primary keys in durable keyset order, one
source position per transaction, with a 250 ms context deadline and a 25 ms tick.
A worst-case source can produce 4,096 postings; source, derived rows and cursor
commit together. A nullable initial cursor handles the empty-string identity.
The final empty seek and certification commit under the same write transaction.
Triggers capture edits/inserts behind the cursor, including writes after `done`.
There is no request-time or startup history copy and no queue catch-up race.

Reopening resumes the committed cursor. Cancellation, SQLite contention or
storage failure leaves that row pending and retries later; errors are reported
at most once per minute without source data. Tests interrupt/reopen, insert
behind the cursor, edit/delete/trash/move records during the build, inject a
projection failure, cancel, contend with another writer, check timeout cleanup,
and compare all surviving projections with current canonical source data.
Backfill is exercised to certification in disposable local workspaces; this PR
has not been deployed to a user's workspace.

## Cost and indexes

Projection write work is O(L + (Uold + Unew) log N), with Uold/Unew <= 4,096.
L is the incoming source payload; SQL extracts its text once and the normalizer
reads at most 64 KiB. It never loads document blobs or peer comment history.
The posting primary key `(event_id,term)` and unique `(event_id,slot)` keep old
posting deletion resource-local. `(thread_id,term,event_id)` is the future
candidate range. All binding checks are primary-key probes. The backfill uses
one primary-key seek, one source point capture and one checkpoint per chunk;
its total work is linear in historical events but is never on the serving path.

`TestPerformanceCommentSearchWriteHistoryIndependence` measures the actual event
trigger with 100 and 1,000 peers: one top-level SQL update, 481 VM steps, no full
scan/sort/automatic index in both cases. This isolates the new projection cost.
`TestCommentSearchFullWriteCost1000Comments` compares the actual authorized
`UpdateDocumentComment` API on the same 1,000-comment, 1 MB document, disabling
only the new capture triggers for the baseline. Each side starts with the same
text and a warmed authority read; fresh instrumented connections avoid SQLite
automatic-reprepare measurement artifacts. Full API numbers are reported in the
PR and test logs. Aggregate rebuilding remains, so this phase does not claim a
live write speedup or reduced full-request history reads.

## Remaining merge criteria

1. Implement and review the production independent reader over current canonical
   authority, document heads plus comments, bounded candidate/hydration SQL,
   durable content/authority/lifecycle generations and response coverage. Test
   head/parent lifecycle, filters, ordering, continuations, and full HTTP privacy.
2. Select that reader default-on only after certification, with an instant,
   current aggregate fallback. Before removing aggregate writes, implement a
   bounded fallback strategy that accounts for post-certification mutations;
   switching to a stale FTS table is not acceptable. Source-version counters in
   these heads are not yet safe continuation generations (delete/restore resets
   a head); do not use the test adapter's constant binding in production.
3. Stop the three comment-driven aggregate rebuilds and aggregate comment
   population only with that safe reader/fallback transition. Run the full API
   before/after scale comparison again to demonstrate the actual reduction.

No HTTP routes or external contracts change in this writer/backfill phase.
The remaining criteria above are required before calling the full cutover done.

Focused reproduction from `core/`:

```sh
go test ./internal/storage -run 'TestCommentSearch|TestPerformanceCommentSearch' -count=1 -v -timeout=3m
go test ./internal/primitives -run 'TestCommentSearch|TestResourceAccessBlobContentAndDocumentSearch' -count=1 -v -timeout=5m
ANX_PERFORMANCE_TEST=1 go test ./internal/storage -run TestPerformanceStartup -count=1 -v -timeout=5m
```
