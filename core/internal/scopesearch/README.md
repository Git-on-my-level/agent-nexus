# Bounded scope search

This is stream C's disabled executor, rebased onto the SCA-666 foundation. It
does not register a route, initialize tables, replace `SearchDocuments`, or enable
the new authority. Legacy search/comment privacy tests and the legacy FTS path
remain authoritative until the complete phase-3 semantic cutover.

`Search` receives a trusted `Repository`, a persistent-secret token codec and a
finite request selection. The repository must authorize **all** selected scopes
before invoking the callback and keep binding checks, candidate keys and bounded
text hydration in one transaction. It exposes only typed operations; this package
has no database import, SQL handle or capability factory.

Search indexes current document heads and independent comment resources. `Apply`
replaces only that resource's postings in the caller's canonical transaction;
`ApplyCanonical` consumes the frozen `scopes.Change`, plus a trusted source-prefix
truncation bit. Its writer must delete at most 4,096 old postings, insert at most
4,096 new postings and advance the scope's search generation atomically. It must
never load a document's revisions or backing-thread history on a comment change.

Coverage is a normalized UTF-8 prefix of at most 65,536 bytes and 4,096 distinct
terms. Tokenization lowercases Unicode letters/numbers and replaces other input
with word boundaries. It stops when either cap is reached and reports the actual
indexed byte count, truncation and canonical version. Prefix/fuzzy and historical
version search are absent. Binary bodies supply no search text.

The sorted first query term is the fixed posting driver. Verification checks all
terms or the exact normalized phrase; it never uses corpus statistics. K<=64,
T<=8, P<=100, B<=4P, aggregate candidate keys C<=min(KB,4096), and hydrated text
V<=4 MiB including rejected and unreturned candidates. A request must allow at
least 64 KiB of verification and one candidate slot per selected scope. Bodies
are materialized in one bounded batch after candidate admission. The merge stops
at a saturated stream's frontier so the global cursor cannot skip unfetched keys.
A saturated final range may produce one extra terminal empty page. An unchanged
corpus always progresses; changed selected search/lifecycle/authority generations
return an explicit restart. Tokens bind query, principal, exact scope selection,
generations and the last fully verified key, and are encrypted/authenticated.

CPU is O(K log N + C log K + V*T + P*512), with fixed T<=8; SQL examines at most C
candidate keys and hydrates at most V bytes. Required indexes:

- Posting range `(scope_id,term,sort_key,rid,kind)`, where `sort_key=-recency`.
- Resource maintenance `(scope_id,kind,rid,term)`.
- Head metadata/text `(scope_id,kind,rid)` and generation `(scope_id)`.
- Current principal/scope membership and lifecycle authority primary keys.

Write work is O(L+(Uold+Unew) log N), Uold/Unew<=4,096, plus fixed generation
maintenance. There is no history-size term. RID accepts the foundation's 512-byte
bound. The scope and version fields remain finite; actual production identities
are A's opaque allocated IDs.

## Lead-owned integration proposal

1. Implement `Repository`/`Snapshot` and `Writer` inside `scopedrepo` using its
   transaction capabilities and reviewed generated templates. Use the durable
   reference adapter in `server/scope_search_repository_test.go` as executable
   evidence, not as a production factory. Its shadow DDL also specifies the
   posting/resource/generation shapes. Enforce immutable ownership, current head,
   complete old-posting bounds and scope/parent equality in the production schema.
2. Produce the bounded head/comment prefix from the incoming canonical write
   **before** forming the frozen bounded Change. Carry source truncation explicitly
   (the current shared Projection has no such field). Never pass a full oversized
   canonical record through `Change.Validate`, and never silently label an already
   truncated delta complete. Add a shared coverage bit when freezing that hook.
   Hooks must call this package for append/edit/delete/purge/lifecycle rebuilds in
   the same transaction as canonical state, including imports.
   Guarded-reference labels borrowed from another scope are render-only; never
   put their target title/preview in the destination's searchable source text.
3. Maintain monotonically increasing principal authority, scope lifecycle and
   search generations. Selected mutation/revocation/regrant/activation must expire
   old continuations; unrelated scope content must not invalidate them.
4. Integrate the proposed writer-inventory fingerprints, canonical contract,
   route/classifier/privacy/performance inventory, and authenticated selection.
   `Handler` is a disabled transport helper: nil `Ready` denies before work. Its
   positive readiness must come only from complete cutover approval.
5. Adopt #295's SCA-661 budgets and explain the **actual production** candidate,
   hydration and authority statements. The reference adapter's exact range and
   bounded VALUES hydration plans are checked, but this is not production route
   performance approval. Real canonical HTTP mutations and durable lifecycle
   worker tests remain integration gates. Do not retire old tests yet.

The HTTP tests exercise the production executor/helper over a real SQLite
workspace and foundation authority tables with explicit test selection. They
cover oversized repetitive heads, a late covered phrase, outside-prefix matches,
empty-page progress, cross-scope frontier ordering, generation binding, comment
history independence and transaction rollback. They do not claim the production
authentication dispatcher or registered legacy `/docs/search` has switched.
