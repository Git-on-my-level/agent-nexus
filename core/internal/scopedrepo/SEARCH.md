# Independent search writer foundation

`SearchCanonicalHook` is a concrete, SQL-sealed adapter for the existing
`scopesearch.ApplyCanonical` executor. It is accepted by
`ApplyRegisteredCanonicalHooks` inside an existing canonical transaction.
`InitializeSearchSchema` installs only empty shadow tables. Neither is invoked by
workspace startup or production writers. `TestFoundationNotServing` remains in
force. This is the completed writer slice of SCA-760, not the serving cutover.

## Transaction and privacy boundary

The hook accepts exactly one `document` or `comment` delta with audience `all`.
An indexed identity must match the current registry's scope, canonical identity,
opaque identity, integer RID and version in the same transaction, and the scope
must be active. A comment requires a registered document parent in that scope.
The first binding reserves that parent permanently, including through delete,
restore and SQLite replacement. Recipient-specific deltas and multiple search
deltas are rejected before indexing. Text comes from the bounded canonical
capture; the hook never loads document blobs, revisions or event history.

These checks **do not prove source-text provenance or legacy visibility**.
Canonical capture remains integration-owned. Under today's policy a comment's
private reference can restrict its entire parent document. Merely placing that
document and comment in the same scope does not certify that every scope member
can read them. No reader may use these tables until that audience proof exists.
The existing `TestResourceAccessBlobContentAndDocumentSearch` owner/stranger/agent
regression remains authoritative. No permissions or HTTP contracts change here.

The hook replaces one head and its postings, and advances one search generation.
The shared hook boundary rolls back the entire source transaction on failure,
including failures after deleting old postings. Search generation overflow fails
closed through integer checks. Generations cannot be deleted, reset or replaced.
This adapter does not open or commit a source transaction, capture canonical
content, dispatch a worker, authorize a reader, or mint a readiness receipt.

## Bounds and schema review

All SQL is an exact constant admitted by the registered hook proxy. Posting
inserts use one fixed `json_each(?)` template over at most 128 prepared terms;
the JSON parameter is bounded by the already-prepared 64 KiB source and 4,096
terms. No persisted JSON is scanned. A resource has unique slots 0..4,095, so
imports cannot grow the old-posting deletion beyond 4,096 rows. The unique
`(scope_id,kind,rid,term)` index supports deletion without scanning peer comments.
The `(scope_id,term,sort_key,rid,kind)` primary key preserves the executor's
candidate ordering. All parent/registry checks use primary/unique-key probes.
Triggers retain these invariants with foreign keys disabled, as in workspace
pools. `search_schema.sql` is the schema authority for this shadow generation;
the older server reference fixture is still test-only.

A replacement uses one identity query, one binding insert, two resource deletes,
one head insert, up to 32 posting batches and one generation update: at most 38
SQL calls, plus one insert when creating the scope's first search generation.
Deletion omits the head/posting inserts. The scope's other resources, document
bytes and comment history are absent from those statements. Four maximum-size
search hooks fit the existing shared 256-call ceiling. Total write work is
O(L + (Uold + Unew) log N), with Uold/Unew <= 4,096. No serving cost, latency
improvement, or legacy authorization-cost reduction is claimed.

The dedicated performance regression uses the actual registered writer and
SQLite VM instrumentation with 100 then 1,000 peer comments, a 1.1 MB untouched
document source, and exact deletion plans. It enforces seven SQL calls, one
identity row, no full scans/sorts/automatic indexes, and a fixed VM-work ceiling
for a two-term comment edit. Maximum-posting tests separately exercise 4,096
terms and reject an overflow slot with foreign keys disabled.

The first writer implementation allowed SQLite to choose a scope-only primary
key range for deletion: 1,809 VM steps with 100 peer comments and 12,609 with
1,000. Pinning the complete maintenance index reduced both cases to 609 VM
steps, with seven SQL calls and one returned row. These are synthetic
before/after measurements of this new adapter, not the live aggregate path.

These tables are not live and therefore do not join the live storage-column
inventory yet. Their classification for migration registration is:

| Table                      | Classification                                                       |
| -------------------------- | -------------------------------------------------------------------- |
| `scope_search_bindings`    | Internal immutable resource and parent ownership binding; no payload |
| `scope_search_generations` | Internal continuation invalidation state; not reader metadata        |
| `scope_search_resources`   | Scope-owned current resource text, version, parent and coverage      |
| `scope_search_postings`    | Scope-owned derived terms and sort keys; never globally visible      |

## Differential parity and known incompatibilities

`TestScopeSearchRandomizedAggregateParity` compares real SQLite FTS5 aggregate
document membership with the independent executor over this writer's persisted
index. Three fixed seeds cover 40 documents each, public/private scopes,
randomized comments, edits, deletion and restoration, and pagination at three
hits per page. All 324 comparisons cover bounded ASCII single terms, paired AND
terms and phrases that occur within one resource. Comment hits are mapped to
their parent and deduplicated solely for comparison. The reader is the existing
trusted test adapter, not a production authority implementation.

Unrestricted old/new parity is currently false. The companion regression pins
these differences instead of hiding them:

- An AND query can match one word in a document and one in a comment, or words
  in two different comments, under aggregate FTS. Independent hits cannot.
- A phrase can cross the newline between two aggregated comments.
- Default FTS5 removes diacritics; the independent tokenizer preserves them.
- Legacy aggregate FTS can match comment suffixes outside the independent
  64 KiB prefix. The new hit must report incomplete coverage.

BM25 rank/order, result shape, tags/hosts/knowledge filters and lifecycle coverage
also need explicit contract treatment. This suite certifies only the stated
common domain; it is not evidence of full `/docs/search` compatibility.

## Remaining integration and retirement point

Do not build a second search engine to conceal those differences. The existing
scope implementation lead owns the semantic decision, production repository and
capture wiring; the migration owner owns online worker lifecycle. Continue the
SCA-666 implementation plan with these concrete remaining gates:

1. Resolve the differential cases and route contract. Prove legacy reader
   visibility for every returned resource before exposing independent results,
   including private-reference comments, revocation and parent lifecycle changes.
2. Wire all canonical head/comment create, edit, delete, restore, purge and import
   paths to bounded capture and this hook in their source transaction. Hook tests
   alone cannot certify coverage. Add the live storage and writer inventories.
3. Implement the **online, resumable backfill** in the existing migration worker:
   bounded keyset chunks, durable generation/cursor and source-version checks,
   delta catch-up with atomic checkpoints, cancellation/restart and concurrent
   edit/delete coverage. Publish no readiness before complete audience and source
   coverage. Reopening must resume committed progress; failure cannot advance it.
   This PR supplies neither that worker nor a migration number. Empty-table
   initialization is not backfill. Do not copy history at startup or on requests.
4. Pass full HTTP privacy/contract parity, production query-plan/scale budgets,
   migration interruption/low-disk/concurrent-writer tests and forward recovery.
5. **Retire aggregate indexing in the same release that atomically selects a
   completely certified independent generation.** Fence writers briefly, finish
   catch-up and verify the source high-water mark. Remove the three comment-path
   `rebuildDocumentFTS` calls and stop populating aggregate comments in
   `upsertDocumentFTSTx`; switch `/docs/search` under the approved contract.
   Retain the old table only for the documented forward-recovery interval, then
   reclaim it in maintenance. Never stop aggregate writes while a reader can
   still fall back to that index, and never serve a partially built generation.

Until those gates pass, the legacy reader and aggregate writer remain the only
production path. This writer slice can ship independently without widening an
audience or returning unverified comment text.

Reproduce focused evidence from `core/`:

```sh
go test ./internal/scopedrepo -run 'TestSearch|TestRegistered|TestPerformanceSearchWriter' -count=1 -v -timeout=3m
go test ./internal/server -run 'TestScopeSearch' -count=1 -v -timeout=3m
go test ./internal/primitives -run '^TestResourceAccessBlobContentAndDocumentSearch$' -count=1 -v -timeout=3m
```
