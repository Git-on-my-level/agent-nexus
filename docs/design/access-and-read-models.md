# Explicit privacy domains and bounded read models

Status: proposed; product decisions below require acceptance before implementation.
This PR changes documentation only. The experiment is on a separate, non-merge
branch. It neither changes production authorization nor authorizes a deployment.

## Recommendation and decisions

Give each canonical record one **scope**, an explicit privacy domain. Scope
membership is durable authority; views and search are indexed by that same scope.
Replace general SQL rewriting with reviewed, scope-bound query templates. Compute
small summaries on writes, and merge bounded pages from accessible scopes.

Adopt explicit container privacy for newly authored content, with structured
guarded references. Do **not** describe link redaction as content sanitization:
removing `doc:secret` from “doc:secret says revenue is 42” leaves a disclosure.
Private material and server-derived facts stay in their source scope. A link
preview can cross scopes because it fetches its target under the reader's current
authority; a copied title, excerpt, count or stored computation cannot.

Product decisions for the owner, with recommendations:

1. **Replace automatic prose-based inheritance prospectively.** Recommend explicit
   containers, opaque guarded reference nodes, and explicit publication for copied
   material. Reject known cross-private raw references on new writes; do not
   silently redact arbitrary prose, JSON or binary bytes. Unknown future names
   no longer retroactively reclassify old prose. Existing restricted data is never
   made public by this change. This is a deliberate semantic change requiring
   approval, not an optimization of the current rule.
2. **Make privacy domains stable.** Recommend stable scope identity and mutable,
   revocable membership. New private containers start private. Cross-scope copies
   require source **publish** permission, destination write permission and an
   explicit publication action; read/write authority alone is insufficient.
   Ordinary edits cannot change scope. Bulk reclassification is deferred.
   Previously disclosed public bytes cannot be recalled from clients.
3. **Accept bounded surfaces and explicit freshness.** Recommend keyset pages,
   per-scope counts, capped search work with continuation, and eventually updated
   initiative health with an `as_of` marker. Never manufacture an exact total or
   “on track” state from a truncated scan. Inbox answers and their counters remain
   synchronous. This preserves the dense executive Overview without loading the
   entire workspace. A PM authorized for 80 scopes gets at most 64 selected scopes
   in one request: the other 16 are **not covered**, and no global total or complete
   executive view is asserted. The UI must show the selection and incomplete
   coverage, with scope-directory pagination. Inherited archive/trash may make an
   entire affected scope temporarily unavailable while a background job runs;
   archiving a large public board may temporarily block the workspace scope.
   Recommend accepting these limits for v1, not hiding them behind stale counts.
4. **Prefer unavailability, potentially permanent, over migration disclosure.** Recommend
   retaining old readers during background conversion, and sealing ambiguous
   legacy records until reviewed. Mixed-owner records, unreconstructable series,
   unavailable blobs and unknown ancestry do not become workspace-visible.
   Recovery is a bounded, separately authorized per-record operation. An auth-admin
   or selected PM cannot inspect a sealed record merely by holding that role.
   If a compacted series has unknown contributors and their owners cannot be
   established, it may remain sealed **permanently**. Recommend requiring proven
   source-owner authorization; no startup deadline justifies widening access.
5. **Replace global caller-selected names.** Recommend server-generated opaque
   global IDs, creator-scoped replay keys and scope-qualified human aliases. A
   private `report` in another scope must not change creation success or cause a
   public `report-2` suffix. Existing global refs remain readable under authority;
   new writes must stop promising a globally unique human-selected handle.

The v1 deliberately excludes bulk reclassification, fuzzy/prefix search and
historical-version search. Repository generation expands a small ownership
manifest into reviewed query templates and typed methods; it is not a policy
DSL, dynamic query planner or general authorization expression language.

### Action and role matrix

Roles are independent, scoped, explicit and revocable. Human and agent principals
may hold ordinary reader/writer roles. The selected PM's existing blanket read
capability does not imply publication, ownership, grant management or recovery.

| Action                       | Required authority                                                                                                                               | Administrative / PM shortcut                                                                                 |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ |
| Read/render                  | Current source-scope read grant; active scope; audience binding                                                                                  | PM may read ordinary active scopes, within request limits; auth-admin has no business read shortcut          |
| Author/edit                  | Destination writer; existing object scope immutable                                                                                              | Neither role alone grants write                                                                              |
| Derive projection            | Scope-bound computation capability; all private inputs and output in that same scope                                                             | A PM with both source read and destination write still cannot cross scopes                                   |
| Publish a copy               | Source publisher + source reader + destination writer; single-use intent bound to source version, output hash, destination and grant generations | Publisher is explicitly delegated by the human scope owner, never inferred from read/write, PM or auth-admin |
| Grant/revoke scope access    | Scope owner; delegation of owner/publisher rights requires human owner authorization                                                             | Auth-admin administers authentication only; it cannot grant itself private-content authority                 |
| Archive/trash/restore parent | Parent writer; start fenced lifecycle job                                                                                                        | No synchronous descendant fanout; no publication or scope change                                             |
| Reclassify scope             | Unsupported in v1                                                                                                                                | No role can bypass this refusal                                                                              |
| Recover/inspect sealed bytes | Separate record-bound recovery warrant, approved by every provable source owner; durable current read authority for all contributors             | Admin/PM alone cannot inspect. Unknown/missing owners mean no warrant and potentially permanent sealing      |

A recovery worker may examine sealed bytes internally to prove provenance, under
an audited maintenance identity with **no user serializer**. A reviewer sees only
sanitized job state until a valid warrant exists. Recovery never assigns a record
to workspace scope by default. Publication/recovery check source version and
current authority at commit, consume their intent once, and audit the audience
expansion. Possession of an ordinary HTTP bearer cannot manufacture these intents.

## Evidence and scope of the problem

Reviewed baseline: `e637014e8c6eadc660c7effe608a7e3ff6af6814`, schema 63.
[PR #288](https://github.com/Git-on-my-level/agent-nexus/pull/288) introduced the
comprehensive privacy boundary;
[#294](https://github.com/Git-on-my-level/agent-nexus/pull/294) materialized mention
resolution and request denial snapshots;
[#298](https://github.com/Git-on-my-level/agent-nexus/pull/298) bounded SQL preparation
and indexed ancillary privacy. They repaired real leaks and stalls. They did not
remove denial-graph construction or workspace-wide application projections.

The motivation is a small corpus with roughly 26 documents, 433 cards, 2,759
events, 806 artifacts, 499 threads and 12 inbox items. The issue reports a 280 MB
database, roughly 180 MB of authorization tables/indexes, 237,635 base edges and
the same number of exact-edge copies. These are supplied incident observations,
not measurements made by this design. No production data was opened or copied.
The synthetic experiment and its limitations are recorded in
[the evidence note](access-and-read-models-evidence.md).

The October 4/5 product decisions require explicit revocable agent grants, request
authority, generic OSS boundaries, live computed surfaces and executive-readable
output. The private decision log was read for context; no private configuration is
required by this design. The detailed existing privacy contract is in
`core/AGENTS.md`, the three PRs above, the executable ownership/route inventories,
and SCA-652. SCA-652 explicitly leaves global BM25 statistics, weak field
classifications, unbounded reads and legacy-series false positives unresolved.

## Security model

A scope names a stable audience, not a folder label. Lifecycle fencing is a separate
state of that scope and never widens its audience. Examples are workspace,
private board, private space and private conversation. Each record, including an
event, revision, artifact, profile, replay, projection and tombstone, belongs to
exactly one scope. Its canonical owner and children share that scope. A visible
board can contain a separate private subtree only through an opaque link, not
through a title/count-bearing child row in the board's public projection.

An authenticated principal receives a bounded set of explicit scope grants.
Anonymous development reads receive only configured workspace visibility, never
an implicit maintenance privilege. Private owners and the selected PM retain
their present access through explicit grants. Changing the selected PM must
revoke the old PM's access immediately: a single workspace PM authority row can
provide a special all-active-scopes capability to the current selected PM, with
indexed scope enumeration subject to the same scope/page limits. It is **not** an
auth-admin privilege. PM processing still excludes sealed migration data and
unindexed blobs. Human-only administrative actions remain human-only.

For each response, serialize only rows authorized in one read transaction.
Capabilities carry a principal and grant generation, not a cached eternal list.
Membership is loaded/revalidated from durable authority in that transaction.
Writes authorize the existing object, all source objects and the destination
inside their write transaction. No unscoped request context exists. Revocation
linearizes at the committing transaction; a read that already took its snapshot
may finish, as with today's statement snapshots. An SSE connection reopens an
authorized transaction on every tick. It must not retain pre-revocation rows in a
pending application queue; disconnect/invalidate on generation changes.

Authorization is about information returned, including presence, titles, IDs,
counts, timestamps, rank, cursors, error text and cached derived facts. Missing
and inaccessible point targets have the same external behavior. This does not
promise constant-time cryptographic noninterference on a shared CPU or physical
SQLite file size. It does prohibit content-dependent ranks, counts and private
metadata in responses. Quota enforcement remains canonical; reader-visible usage
is scoped. Shared disk-byte metrics retain their existing infrastructure meaning.

### References and generated content

Use three distinct operations, never one ambiguous `refs` interpretation:

- **Ownership:** bounded structural parent/child relationships in the same scope.
- **Guarded navigation:** a typed node stores an opaque immutable target key. Its
  label, URL, ID and preview are emitted only after a bounded target authorization
  lookup. Unauthorized and nonexistent targets render the same generic marker;
  no target-specific HTML, tooltip, download URL or serialized extension survives.
- **Evidence/copy/derivation:** canonical generated bytes must be in the same scope
  as every private contributor. A computation spanning scopes is either composed
  transiently from reader-authorized per-scope results or explicitly published
  into a destination by an authorized publisher. Never persist a mixed-scope
  aggregate and then filter its inputs at response time.

The safe initial rule for automatic computations is **same scope only** (public
constants may contribute). Here “public constants” means non-resource literals or
explicitly published immutable values, not a mutable public record that might
later be restricted. No membership-subset inference, intersection scopes,
or automatic declassification. The source scope still owns a computation if it
uses a public constant. A private source's later membership revocation therefore
also revokes its derived content, without finding dependent rows. Scope identity
does not change merely because two scopes currently have identical members.

Raw text/JSON has the destination's declared classification. Parse only the
bounded incoming write, preserving scalar-versus-JSON semantics and existing NUL
validation. Resolve at most the request's allowed reference count. For recognized
typed references and designated reference fields, reject
inaccessible and nonexistent targets with the same status, error and validation
behavior; an author must not learn that a guessed private target exists. A
reference authorized for the author but in another private scope requires a
guarded node or explicit publication. Arbitrary untyped prose words/JSON keys
are not looked up against private identities; their automatic taint semantics
change prospectively. Legacy untyped selectors still use a typed API context.
Do not accept a guessed nonexistent typed reference where an inaccessible one
would fail. Use opaque guarded nodes for unresolved links instead. Structured
nodes are the sole automatic redaction path.
Binary content remains byte-preserving and scope-owned; it is never rewritten.
Existing manifests still gate migration of old blobs. New binary uploads use a
bounded full-byte reference validation pass before publication; no reader scans
their bytes. Unresolvable arbitrary prose is not a durable taint graph.

An author authorized to read private data can manually retype a secret without a
reference. Neither the old matcher nor this proposal can infer that secret's
origin. The trusted publishing boundary must be stated honestly: server-produced
copies carry provenance and cannot cross scopes silently; user-authored content
is explicitly classified by its author. The publication UI/API must identify
audience expansion and require explicit confirmation. This is the product-level
trade-off in decision 1, not a claim that arbitrary prose has been sanitized.

### Collision-independent identities

New global identities are server-generated 128-bit opaque random values. Allocation
never probes user-supplied text in another scope. A random collision is retried
internally without exposing a suffix/collision count. Idempotency keys belong to
`(creator_principal, request_key)`, and replay always reauthorizes the result.
Human aliases and their reservations/tombstones are unique only within
`(scope_id, resource_kind, normalized_alias)`. A create without an alias returns
an opaque ID; a requested same-scope alias conflict is legitimate visible authority.
There is no auto-suffix allocator that inspects a global namespace.

Resolve scope-qualified aliases only after access to that scope is established.
Unqualified legacy refs resolve over at most the request's selected scopes and
return a generic unresolved/ambiguous result when appropriate, without choosing
among hidden candidates. Released global IDs/aliases stay in an immutable legacy
lookup for reads; they are **not** a namespace new creates must reserve against.
Existing tombstones remain scoped/reserved for those legacy reads. New caller-
chosen global IDs are uniformly invalid, regardless of occupancy. This requires
contracts/CLI changes; preserving old allocation behavior is not compatible with
the no-existence-oracle guarantee.

Guarded links bind immutable keys. Unknown links carry opaque pending tokens and
resolve under target authority. Missing and denied targets have identical outcomes,
including validation on writes. Differential create tests add/remove private
resources, aliases and tombstones and compare success, alias text, replay behavior
and ID shape/distribution, not random ID equality.

## Schema sketch

Names are illustrative, not assigned migration numbers or a contract change.
The resource registry is the authority for scope and lifecycle; type payloads do
not duplicate a independently writable scope. Selected indexes/read models
necessarily repeat scope keys, with transactionally enforced consistency.

```sql
CREATE TABLE visibility_scopes (
  id INTEGER PRIMARY KEY, kind TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('active','sealed','transitioning')),
  generation INTEGER NOT NULL
);
CREATE TABLE scope_grants (
  principal_id TEXT NOT NULL, scope_id INTEGER NOT NULL,
  role TEXT NOT NULL, PRIMARY KEY(principal_id,scope_id),
  FOREIGN KEY(scope_id) REFERENCES visibility_scopes(id)
) WITHOUT ROWID;
CREATE INDEX grants_by_scope ON scope_grants(scope_id,principal_id);
CREATE INDEX scopes_by_state ON visibility_scopes(state,id);
-- Audience bindings have an explicit <=4-per-scope, <=256-per-request contract.
-- Sequence heads use the exact (scope_id,family,audience_key) stream key.
CREATE TABLE resources (
  rid INTEGER PRIMARY KEY, kind TEXT NOT NULL, external_id TEXT NOT NULL,
  scope_id INTEGER NOT NULL, version INTEGER NOT NULL,
  lifecycle TEXT NOT NULL, UNIQUE(kind,external_id),
  FOREIGN KEY(scope_id) REFERENCES visibility_scopes(id)
);
CREATE INDEX resources_by_scope ON resources(scope_id,kind,rid);
-- Every type payload has a unique/FK rid; containment has a checked same-scope FK.
CREATE TABLE scope_feed (
  scope_id INTEGER NOT NULL, family TEXT NOT NULL, audience_key TEXT NOT NULL,
  sort_key INTEGER NOT NULL, rid INTEGER NOT NULL, version INTEGER NOT NULL,
  PRIMARY KEY(scope_id,family,audience_key,sort_key,rid)
) WITHOUT ROWID;
CREATE TABLE scope_counters (
  scope_id INTEGER NOT NULL, family TEXT NOT NULL, audience_key TEXT NOT NULL,
  bucket TEXT NOT NULL, value INTEGER NOT NULL,
  PRIMARY KEY(scope_id,family,audience_key,bucket)
) WITHOUT ROWID;
CREATE TABLE scope_changes (
  scope_id INTEGER NOT NULL, family TEXT NOT NULL, audience_key TEXT NOT NULL,
  seq INTEGER NOT NULL, rid INTEGER NOT NULL, version INTEGER NOT NULL,
  PRIMARY KEY(scope_id,family,audience_key,seq)
) WITHOUT ROWID;
CREATE TABLE search_postings (
  scope_id INTEGER NOT NULL, term TEXT NOT NULL, sort_key INTEGER NOT NULL,
  rid INTEGER NOT NULL, PRIMARY KEY(scope_id,term,sort_key,rid)
) WITHOUT ROWID;
CREATE INDEX search_by_resource ON search_postings(rid,term);
```

Use integer resource keys internally, durable typed external identities at the
boundary, explicit lifecycle columns, and foreign keys enabled on every
connection. Feed payloads hold keys and small sortable scalars, not copies of
full evidence JSON. Inbox audience keys represent the actual recipient or a
bounded role; do not replicate per member of a board. Role-wide and personal
rows have disjoint identities/deduplication before page admission. Personal read
state uses `(principal_id,rid)` and never requires scanning all members on writes.
Counters are maintained for supported dimensions only, not all possible filter
combinations. Unsupported exact totals become absent/unknown, not expensive COUNT.

Search postings are a retrieval index, not a second privacy graph. Canonical
navigation edges remain useful where needed, but there is only one edge source;
drop `resource_access_edges`, `resource_access_exact_edges`, mention buckets,
mention resolutions and derived identities after cutover and retention checks.
Keep canonical aliases, owner tombstones and unresolved legacy recovery evidence.

## Enforced by construction

SQLite has no native row-level security. A helper that developers may forget is
insufficient. Make database access a closed boundary:

1. Only `internal/scopedstore` and migration/maintenance packages may import
   `database/sql`, obtain raw connections or prepare statements. Existing generic
   `QueryContext(string)` APIs disappear from business packages after porting.
   A Go analyzer rejects raw imports, raw-handle escapes, arbitrary SQL, alternate
   driver connections and raw methods passed as function values. No runtime SQL
   rewriting or reliance on Go `internal` alone.
2. A small ownership manifest lists type/table, scope key, parent and audience
   columns. Generation expands reviewed SQL templates and typed methods only;
   it does not accept arbitrary predicates or define a second policy language. Every table/view/column must be owned, authority-only
   or derived from specified owners. An unclassified field fails generation.
   Ref-like names and JSON paths cannot be labeled identity without executable
   evidence and negative tests, addressing SCA-652's weak classification case.
3. Repositories require an unforgeable `Reader`/`Writer`, constructed by
   authentication for a transaction. Generated point and page queries combine
   authorization and data selection. DTO constructors accept authorized typed
   rows, not raw maps. No public constructor converts raw rows to visible DTOs.
   Existing map-based payloads must move behind this boundary before old policy
   removal. Authority-only results have separate types with no business serializer.
4. Storage constraints/triggers validate parent-scope equality, non-null ownership,
   immutable scope on ordinary UPDATE, projection scope equality and atomically
   maintained mandatory counters/change rows. Imports use the same writer
   boundary; an offline import must rebuild/validate before the database can
   become readable. A privileged maintenance capability cannot be passed to HTTP.
5. The exact generated contract routes, mounted handlers, authentication
   classifiers and privacy/performance manifests must agree in tests. All read,
   mutation, replay, read-only POST and stream routes exercise denied **and positive**
   controls. A new route with no scoped repository or no matrix test fails CI.

This boundary protects against contributor mistakes, not malicious changes that
edit the analyzer and its tests. Keep its exceptions small and reviewed, and
retain the current storage writer fingerprint inventory during conversion.

### Computation boundary, not just principal authority

Separate three APIs/packages: **author input**, **scope computation**, and
**render-only output**. A `Compute(scope)` capability pins the scope and transaction
for the whole computation. It can read source records only in that scope; every
persist operation, including a constant chosen after a private branch, uses the
same output scope. Owning write grants to another scope does not change this
capability. A destination mismatch or a value from another computation fails
before any database write. Only the trusted dispatcher can mint a capability;
business computations receive exactly one and cannot import the factory, model,
database, dispatcher, or a helper that starts another computation. Otherwise a
private lookup's success/error could choose a constant in a second public
computation. The analyzer must reject this implicit-flow path, including indirect
factory calls; scope tags on values alone do not stop it.

Derived values are opaque handles into computation-owned storage. They have no
public plaintext getter, `String`, marshal method or constructor from arbitrary
bytes. Capability objects also cannot contain reflectable plaintext: the prototype
keeps kernel state behind closure captures and tests formatting both handles and
capabilities. Joins/formatting/counting inside the trusted computation kernel preserve
the scope; values alone are not the only taint, since control flow can derive a
secret too. The render-only API can serialize authorized values into an HTTP
response but has no persistence capability. It cannot call the author-input
constructor. Author-input decoding exists only at authenticated ingress and
accepts request bytes, never a `DerivedValue` or response DTO.

The import/API analyzer must reject a package combining render/plaintext access
with author/persistence entrypoints, including transitive helpers and method
values. Reviewed kernel code is the small trusted boundary. A handler cannot get
a private title as a string and pass it to an ordinary public projection writer:
there is no such string-taking derived writer or permitted import path. Publication
is a separate audited operation from the action matrix, with explicit source
publisher authority. This prevents accidental laundering by a privileged PM; it
is not a claim of protection against malicious edits to the trusted kernel or an
authorized human retyping response bytes as newly classified author input.

The thin prototype tests direct copying, rebinding a private value to a public
computation, and persisting a private computation's constant. All fail mechanically;
same-scope persistence passes. This does not prove whole-program implicit-flow
rejection: the test harness can mint both capabilities as trusted setup. Complete
import/call-graph analyzer negative tests (including branching on private errors
before starting a public computation) and conversion of existing raw-map handlers
remain production acceptance gates.

## Bounded queries and read models

One `scope_id IN (...)` plus LIMIT is not a sufficient cost proof: SQLite may scan
all rows or sort all matches to produce a global order. For each explicitly selected, authorized stream
(scope/family/audience), seek its composite index with a keyset cursor, fetch at most `P+1` keys,
then perform a heap merge and batch hydrate the final P rows. One bounded SQL
batch may contain the per-scope subqueries; no query per returned row. For S
streams this intentionally admits at most `S(P+1)` candidates; a simple list has S=K. The cursor contains
the last total-order key, query identity, grant generation and projection version,
encrypted and authenticated by the server. A signature alone is insufficient:
the internal global `rid` can expose allocation gaps between visible records.
Never serialize integer allocation keys, query offsets, or global event IDs into
API data or plaintext cursors. External resource identities are opaque,
nonsequential keys; stream-local ordering is used for changes.

- **Work:** scope, supported filter, attention bucket, time key, rid form the
  index prefix/order. Avoid optional-filter OR clauses. Each supported sort/filter
  combination has a reviewed generated query and index. An unsupported combination
  uses explicitly bounded search semantics or is rejected, never a silent scan.
- **Inbox:** write-time source-to-item projection, scope plus recipient/status and
  trigger time index; answers update state, counters and source event atomically.
  Summary sums a fixed set of counter buckets across accessible scopes. Mutable
  recipient membership is checked at reads; no hidden source contributes an item
  or count. A recipient's personal read state cannot affect another recipient.
- **Overview:** bounded initiative pages, fixed counter buckets and fixed top-P
  urgent sections from the same scoped feeds. Join actor labels and plan summaries
  in batches only for returned keys. Store small health inputs on the work row.
  A bounded scheduler maintains time-dependent stale/due buckets. An expired
  scheduler watermark yields a visible “updating” state; reads do not repair by
  sweeping plans. Cross-scope health is composed transiently only for the visible
  page, with bounded plan refs and explicit incomplete status.
- **Search:** use scoped term postings with a fixed recency order, never global BM25
  statistics or dynamic cross-corpus ranking. At most T
  terms, K scopes and B posting candidates per stream are visited. AND/phrase
  verification happens only in that bounded candidate window, with an additional
  aggregate cap C <= min(KTB, 4,096) candidate keys and V <= 4 MiB of candidate
  verification bytes per request, including rejected hits. The executor fetches
  candidates in bounded batches and stops at these aggregate limits; it cannot
  hydrate KTB full bodies then apply the cap. Postings do not contain positions,
  so phrase verification is charged to V, separately from returned snippets.
  Continuation retains the last verified candidate; a body is never skipped as
  nonmatching merely because the budget ended. Each resource's **searchable**
  normalized UTF-8 text is capped at 64 KiB and 4,096 unique terms at write time.
  Phrase matching verifies only that declared prefix; matches beyond it are outside
  coverage, including for a large ranged document. With V>=64 KiB a candidate can
  always finish in a fresh request. Return `indexed_bytes`, truncation and head
  version with results. Comment text follows the same per-resource cap.
  Order by fixed recency key plus opaque stable tie-breaker, not a changing global
  rank. A cursor binds the query, last fully verified key, selected scopes and their
  search generations. V1 search covers scope-owned documents and individual
  comments only. Audience-restricted inbox/replay/profile payloads are not indexed
  by that scope-only relation; any future search of them needs the exact audience
  prefix and current binding checks before candidates/ranking. Any indexed mutation changes the relevant generation and
  forces an explicit restart; no live SQLite transaction spans HTTP requests.
  An unchanged corpus must progress through an oversized repetitive candidate;
  concurrent churn may require a restart, never an indefinite same-key loop.
  Return continuation even for an underfilled/empty page when candidates remain; no “keep scanning
  until P matches.” No exact total, unbounded ranking, or hidden-corpus IDF.
  Snippets hydrate visible rows only. Prefix/fuzzy expansion is deferred in v1.
  Search contract/UI must acknowledge coverage limits.
- **SSE:** scope/family/audience-local change sequences and index seeks after the last authorized
  cursor; bounded batch and output bytes each tick. Idle ticks read grant/state
  generations and seek empty ranges, not full inbox/overview. Resume tokens bind
  principal, query and grant generation. Revocation resets the visible snapshot;
  compacted cursors request a bounded resync. No global sequence gaps reveal
  private activity. A tick overflow continues on the next tick without draining
  all history in one request.
- **Reports, reference resolution, graph/context, exports:** each is a bounded
  composition of the above with explicit total node/ref/byte limits. Downloads
  authorize metadata first and stream a size-limited blob or bounded byte range.
  Exports are jobs composed of pages, not unbounded HTTP requests. Jobs reauthorize
  on every chunk and publish only under the same source scope.

### Lifecycle transitions and stream selection

Inherited archive/trash/restore is **not an ordinary row edit**. In v1, structural
parents/children share a privacy scope and depth is capped at eight. Cross-scope
project links are navigation, not new lifecycle inheritance. Existing cross-scope
legacy inheritance cannot be silently dropped: a transition fences the entire
workspace business surface while its legacy closure is rebuilt, or is rejected
until that compatibility job exists.

For same-scope parents, atomically set scope state to `transitioning`, update the
parent's requested lifecycle, and enqueue one job (three fixed writes in the
prototype). Ordinary reads/writes of that scope then return `scope_updating`;
counters are **unavailable**, not zero or stale exact totals. A mixed-scope request
containing that granted scope also returns this explicit incomplete condition.
An unaffected scope remains readable. No query joins archived parents to skip
an unbounded run of dead feed entries.

A checkpointed worker scans that scope by `(scope_id,rid)` in J<=64-row chunks,
checks at most eight ancestors per row, rebuilds the live feeds/postings/counters,
and advances its cursor in the same transaction. Scope writes remain fenced.
On completion it atomically activates the new generation. Crash/retry is idempotent;
restoration is the same job. Parent cascades, counters and pending inbox lifecycle
exceptions use their reviewed row projection function, so independent asks can
remain active after their context is archived. The total job may be O(scope size),
but the initiating request is not. A large public board can temporarily make the
workspace scope unavailable: this is an explicit product decision, not hidden cost.
No bulk security reclassification is implemented by this lifecycle machinery.

Define **S** as the total selected streams across scopes, audiences and families,
not merely K scopes. K<=64, at most four audience/family streams per selected scope,
and S<=256 per request/tick. One item has at most four audience destinations;
write fanout and duplicate admission are capped before mutation. Overflow is an
explicit narrowing/split-request error, never silent truncation. Recipient and
role streams have independent local sequence heads. Role bindings are current
indexed authority; their enumeration reads at most cap+1 _without_ filtering
inactive bindings in a joined relation. The API accepts a finite selected binding
set and validates each by point lookup. Revocation drops that selection/reset
under a principal-specific authority generation.

Concrete query templates (bound values, within one authority snapshot):

```sql
-- Directory page is of this principal's own grants, including unavailable slots.
SELECT scope_id FROM scope_grants
WHERE principal_id=? AND scope_id>? ORDER BY scope_id LIMIT 65;
-- Then <=65 scope PK probes. Never add WHERE scope.state='active' to that range.
-- Unavailable grants have a metadata-free unavailable slot; counts remain unknown.
-- PM's special directory has an equality prefix, not a post-range state filter:
SELECT id FROM visibility_scopes WHERE state='active' AND id>? ORDER BY id LIMIT 65;
-- One already-authorized audience/family stream, repeated/batched for S streams:
SELECT seq,rid,version FROM scope_changes
WHERE scope_id=? AND family=? AND audience_key=? AND seq>?
ORDER BY seq LIMIT ?; -- P+1
```

An ordinary granted-but-unavailable slot reveals only the principal's own binding;
a sealed record with no grant is never enumerated. Expired/revoked grant rows are
removed or selected by an indexed authority state, never filtered after a long
range scan. For SSE, no wrong-audience row enters a selected stream's key range.
Idle ticks return the same opaque continuation if there is no visible stream change;
there is no hidden sequence to scan past. Overflow advances only each emitted
visible stream position, preserving un-emitted heads. Cursors are encrypted and
bind the exact stream selection, principal generation and scope generations.
Retention resets are stream-local. The prototype measures examined candidates,
not just returned rows or EXPLAIN's `SEARCH` label.

### Exact mutation ledger

Every existing writer must be assigned a row in this ledger before old policy
removal. An unlisted writer/side effect is a failed inventory check, not permission
to fall back to old unbounded helpers.

| Mutation                                | Foreground work and indexes                                                                                                                | Overflow / deferred work                                                                                                                                                           |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Create identity / alias / replay        | O(1) opaque allocation and scoped alias/replay PK probes                                                                                   | No global name scan/suffix allocation; same-scope conflict or uniform invalid global-ID input                                                                                      |
| Card create/move/reorder                | Point-check anchor and scope; predecessor/successor from `(scope,board,column,rank,rid)` with LIMIT 1; fixed-width integer rank arithmetic | Exhausted gap returns `rank_gap_exhausted` with no writes; no `loadOrderedBoardCards` or automatic whole-column rebalance. Explicit fenced column maintenance is a job             |
| Document head edit                      | Bounded old/new head postings and 64 KiB searchable coverage; no backing-thread read                                                       | Full bytes can be ranged separately; excess search coverage is declared                                                                                                            |
| Comment append/edit/delete              | Separate comment resource/postings, same scope; bounded own text and posting set only                                                      | Comments stay searchable as individual hits with parent link; document rank/snippet no longer includes all historical comments. No `documentCommentSearchText` on a document write |
| Normal scalar/event/inbox mutation      | R<=200 reference probes, Q<=16 fixed projection/counter writes, <=4 audience streams                                                       | No per-member expansion; derive large dependency updates in jobs                                                                                                                   |
| Plan/observation/evidence change        | Bounded input/ref count and fixed row projection delta                                                                                     | Reverse dependency health is queued and explicitly stale; no synchronous fanout                                                                                                    |
| Archive/trash/restore or cascade delete | Scope fence + parent state + durable job; O(1) foreground writes                                                                           | All descendant feed/search/counter changes in J-row chunks; totals unavailable until activation                                                                                    |
| Grant / selected-PM change              | Authority PK updates and generation change; O(1)                                                                                           | No per-resource grant rewrite; selection bounded on next request                                                                                                                   |
| Blob append / series point / compaction | Explicit byte/token bounds; same-scope fixed aggregate deltas                                                                              | Legacy manifests, retention, history purge and compaction are checkpointed jobs                                                                                                    |
| Publish / recover                       | Explicit action matrix, version-bound intent and bounded record output                                                                     | No implicit cross-scope copy; unknown-owner recovery refused                                                                                                                       |

### Cost model and limits

Let N be indexed records, K the principal's selected/accessible scopes, P page
size, H bounded hydrated refs per page, L input bytes, R input refs, Q fixed
projection/counter updates, Unew/Uold distinct indexed terms in the new/old head,
S audience/family streams, T/B search terms/candidates, C aggregate candidates, V candidate-verification
bytes and Ls per-hit snippet bytes. Index operations cost
`O(log N)`; “bounded by the request” excludes an O(N) corpus term, not B-tree depth.
Proposed technical limits: P <= 100, H <= 200, K <= 64 per request, R <= 200,
T <= 8, B <= 4P, Q <= 16, Unew/Uold <= 4,096 and normal structured body <= 1 MiB.
Blob uploads retain
their explicitly configured byte limit and use streaming validation. These are
generic safety limits, not commercial tiers. They require contract review before
implementation; current larger batch callers must split requests.

K is explicit: more than 64 grants requires selecting scopes or paginating a
scope directory. Do not silently truncate authority or promise an exact
workspace-wide Overview across an unbounded set. Selected PM/all-scope readers
use the same selection limits. The concrete directory templates above bound
**examined** memberships; a post-join active-state filter would not.

| Route class                          | CPU / rows / SQL budget shape                                                                                                          | Required index/access                                                                                      |
| ------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| Point / revision / blob metadata     | O(log N + K + H log N); O(1+H) rows; fixed batch queries                                                                               | `(kind,external_id)`, `(rid,version)`, grant PK; permission before blob open                               |
| Lists / work / history / directories | O(S log N + SP + P log S + H log N); <= S(P+1)+H keys; fixed batches or <= K seeks                                                     | Scope + exact selector + total order + rid                                                                 |
| Inbox and summary                    | Page bound above; counters O(KQ); no projection rebuild                                                                                | Scope/recipient/status/time/rid; counter PK                                                                |
| Overview                             | Fixed section count times page bound + O(KQ); bounded plan/label hydration                                                             | Initiative role/attention scoped feed, counters, actor key                                                 |
| Search / read-only search POST       | O(KT log N + C log(KT) + V + P Ls); C <= min(KTB,4096), V <= 4 MiB including rejected hits                                             | Scoped postings; no global FTS rank/statistics                                                             |
| SSE tick                             | O(S log N + SP + P log S); idle O(S log N); cap response bytes                                                                         | Scope/family/audience/sequence PK and grant generation                                                     |
| Normal mutation                      | O(L + (R+Q+Unew+Uold) log N); <= R lookups, Q fixed projection writes and Unew+Uold posting operations plus bounded canonical children | Typed identity, same-scope parent, counters/feeds/change indexes and `(rid,term)` search maintenance index |
| Grant add/revoke                     | O(log N), fixed authority/generation writes; no resource fanout                                                                        | Grant PK, principal authority generation                                                                   |
| Maintenance / lifecycle jobs         | Each job slice O(J log N + bounded bytes); total may be O(N+E)                                                                         | Keyset checkpoints; never request-owned or startup work                                                    |

The implementation must budget serialized bytes as well as row counts. Large
single records/revisions are not a loophole. Supported endpoints with larger
output become ranged/paginated or asynchronous through reviewed contracts.

Ordinary row writes (excluding the explicitly fenced lifecycle operations)
update at most Q fixed views, independent of workspace size and
recipient count. Search maintenance is additional: index only the current head,
cap its distinct terms at 4,096 and delete/update at most Uold+Unew postings in the
same transaction. A `(rid,term)` maintenance index bounds old-posting deletion.
A small PATCH may still replace a large old head, so Uold is explicit, never
charged only to incoming L. Index a deterministic capped portion and expose
search-coverage metadata when the text exceeds the token budget; binary bytes
are not search text. Legacy over-limit indexes are rebuilt in the background
before cutover. Historical-version search is deferred; comments are independently indexed resources.
Purge removes the current searchable head synchronously and reclaims historical
bytes in a job; it cannot leave stale postings for readers to skip indefinitely.
Plan edits bound steps and references; reverse dependency
recomputation is a deduplicated background queue, not synchronous fanout. Health
may lag; authorization never does. Raw event history remains append-only, while
scope counters are algebraic insert/update/delete deltas. Numeric series sums and
counts allow bounded updates; arbitrary new aggregate functions need an explicit
bounded maintenance strategy. Retention/compaction runs in slices, preserving
scope and contributor classification. No transitive mention closure on writes.

### Storage budget

Target authorization overhead is <= 25% of canonical table/index bytes on the
synthetic reference-heavy fixtures, and <= 512 bytes per resource plus <= 256
bytes per grant on a large small-record fixture (page/index rounding excluded
only when reported explicitly). Feed/counter/change retention adds a separately
reported target <= 25% of canonical bytes for the fixed supported projections.
Search and blobs are reported separately. These are acceptance targets, not
proven universal ratios: tiny records and many grants can dominate content.

Keep scope metadata O(N + grants), fixed projections O(QN), and bounded-retention
change logs. There is no authorization term proportional to every textual atom
times every identity. Report logical live pages, physical DB/WAL/free pages and
canonical/authorization/projection/search components separately. Dropping tables
frees pages for reuse; it does not promise immediate file shrinkage. Large VACUUM
is an optional maintenance operation with disk headroom, never a startup task.

## Privacy guarantee disposition

“Kept” means a required implementation acceptance test, not a claim this document
has implemented it. “Changed” requires the product decisions above. No existing
restricted legacy content is released automatically. No privacy guarantee is
silently dropped; the removed mechanisms are explicitly distinguished below.

| Existing guarantee / source                                                                                                                           | Disposition                                                | Replacement and required coverage                                                                                                                                                                            |
| ----------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| #288 private board/card/backing thread inheritance, owner and selected PM; unrelated human/agent/anonymous exclusion                                  | Kept                                                       | Same-scope structural ownership; explicit grants and selected-PM authority; positive and negative controls                                                                                                   |
| #288 recursive evidence restrictions through canonical text, nested JSON keys/values, metadata, observations, participants, runs, plans and PM bodies | Changed prospectively                                      | Same-scope derivation; reject known raw cross-private refs; guarded nodes only for navigation; explicit publication. Legacy private closures preserved or sealed                                             |
| #288 revisions/comments/artifact body constrain parent; unknown or unavailable blob denied                                                            | Kept for existing data; changed prospective containment    | Parent, revisions, comments and artifact metadata share one scope; cross-scope attachment rejected. Unknown old manifests remain sealed; bytes are never guessed public                                      |
| #288 all binary byte boundaries, NUL/controls/invalid UTF-8, declared content type, scalar vs JSON, punctuation/Unicode/legacy IDs                    | Kept validation and byte preservation; changed inheritance | Keep parsers and NUL rejection for bounded incoming validation and legacy conversion; binary refs cannot evade cross-scope rejection; never scan on reads                                                    |
| #288/#294 future-created targets, late aliases/parents and virtual revisions reclassify previous prose                                                | Changed for new prose                                      | No retrospective prose taint. Structured target reads check current authority; legacy records see frozen conservative closure, and late legacy dependencies during migration restart affected classification |
| #288 filtering before limits/cursors/joins/aggregates/search; metadata/observations constrain whole work                                              | Kept                                                       | Scoped indexes and same-scope projections before page admission. Legacy mixed work stays restricted/sealed; no post-LIMIT privacy filter                                                                     |
| #288 stored linked-plan titles/refs, notification triggers, inbox parents/data and access-request reasons                                             | Kept                                                       | Scope-owned raw fields; no hidden label is serialized via stored snapshots; guarded cross-scope plans expose only authorized target previews                                                                 |
| #288 source event + navigation/ownership atomicity; denied response replacement; replay/claim/hash/conflict isolation                                 | Kept                                                       | One transaction for authority, canonical mutation, feed and replay scope. Replays are reauthorized, never keyed solely by idempotency token                                                                  |
| #288 cache suppression with inaccessible contributors, count-only caches, report outputs                                                              | Kept information boundary; changed representation          | Per-scope caches only; no mixed-scope persisted cache. Scope/generation/principal keys; authorization before cache delivery; no reader-filtered state written canonically                                    |
| #288 raw/daily/live series contributor restrictions survive compaction; unknown legacy rollups stay hidden; deletion/name reuse                       | Kept                                                       | Single-scope series streams/rollups; mixed or unknown legacy provenance sealed, immutable stream identity through reuse; compaction cannot clear restrictions                                                |
| #288 adapter shared last_push suppressed for partial readers                                                                                          | Kept privacy; changed projection                           | Expose only per-scope freshness computed from that scope. Never expose global last_push as the freshness of a visible subset                                                                                 |
| #288 host/actor/agent/enrollment/invite/token/audit/profile/presence/progress/secret metadata is scoped; encrypted credentials opaque                 | Kept                                                       | Explicit scope on all user-authored metadata; authority-only tables cannot serialize business profiles; authority invariants remain canonical                                                                |
| #288 signed wakeup host target checked first; foreign/nonexistent indistinguishable; card revision auth after authentication                          | Kept                                                       | Host capability + scope in same operation; standard point-denial behavior; exact route classifier regressions                                                                                                |
| #288 selector checks only for route-defined fields; shared health/maintenance errors suppressed                                                       | Kept                                                       | Typed generated selectors; constant sanitized maintenance errors; no incidental parameter existence oracle                                                                                                   |
| #288 purge preserves ownership/tombstones; no handle reuse; unindexed blobs denied                                                                    | Kept                                                       | Scope tombstones and reserved stable identities survive payload purge; attachment cache never bypasses blob metadata authorization                                                                           |
| #288 canonical quota/blob accounting separate from reader totals; maintenance never returns business content                                          | Kept                                                       | Separate authority-only maintenance capability; scoped usage/error DTOs; retain existing physical-byte semantics                                                                                             |
| #288 exact route/mount/classifier inventory, field/writer inventories and human/agent coverage                                                        | Kept and strengthened                                      | Generated store boundary, bidirectional field checks, raw-handle analyzer, positive route controls, read-only POST and second SSE tick                                                                       |
| #294 complete PM kinds with shared IDs, inbox raw/typed parent aliases, late revision parents, batch metadata hydration                               | Kept data privacy; replaced implementation                 | Typed immutable keys include kind; same-scope parent constraints, no ID-only contributor collision, indexed batched hydration                                                                                |
| #294/#298 fresh transaction/stream authority and epoch-valid ordinary GET snapshots; rebind clears cache                                              | Kept                                                       | Durable grant/scope generations checked in each read/write transaction and SSE tick; reject stale cursor/cache generations                                                                                   |
| #298 profile denials are leaves; PM cache protects routing only, never grants                                                                         | Kept isolation                                             | Profile scope cannot mutate unrelated resource authority; PM routing cache always rechecks durable selected-PM/grant authority                                                                               |
| #298 bounded SQL preparation, bound denial data, correct named/numbered bindings, indexed selectors                                                   | Kept cost/injection guarantee; old machinery removed       | Fixed generated query shapes with bound values; no recursive SQL shadows, literal denial sets or caller SQL; cap K/P/R and inspect actual prepare plans                                                      |
| SCA-652 global BM25 statistics can leak hidden corpus changes                                                                                         | Strengthened                                               | Content-local scoped ranking and visible-only snippets/cursors; differential add-private-doc test                                                                                                            |
| SCA-652 inventory accepts wrong identity classification                                                                                               | Strengthened                                               | Executable field ownership and ref-name/path checks with deliberately mislabeled negative fixtures                                                                                                           |
| SCA-652 preview57 false positives hide legitimate owner series                                                                                        | Availability limitation retained until proven repair       | Sealed recovery with reconstructed provenance/explicit publication; never erase ambiguous historical restrictions speculatively                                                                              |

Additional prospective contract changes are explicit: globally caller-selected
handle allocation is removed in favor of scoped aliases; comment search returns
individual hits instead of rebuilding a document's entire comment corpus; global
PM coverage and inherited lifecycle transitions use the availability limits above.
These are not relaxations permitting private content in public responses.

The guarantees above cover the PR descriptions' stored-source and surface tables,
not just their top-level summaries. During implementation, each row must map to
existing regression names plus new scoped equivalents before deleting a trigger.
Legacy restrictions lost in pre-migration purges cannot be reconstructed; this
existing limitation must remain documented, never converted into proof of access.

## Forward migration from schema 63

Do not allocate migration 64: #275 currently owns 64 (board roles) and 65 (evidence
projection/indexes). Allocate after the actual merge head. Preserve all released
migration hashes and historical schemas. This is a forward-only feature epoch;
older binaries must refuse the new epoch. Schema 63 currently ignores unknown
migration numbers, so a new `min_reader_version` row alone cannot enforce this.
The proposed concrete fence is an atomic ledger-format transition: rename the
real ledger to `scope_schema_migrations` and install a `schema_migrations` view
whose SELECT requires a deliberately unavailable function,
`anx_requires_scope_format_v1()`. The released loader's SELECT then fails before
serving; the new loader recognizes the format and reads the renamed ledger
explicitly. This compatibility fence is not business-query rewriting. Test the
actual schema-63 initializer/executable and supported older releases against it;
no graph retirement is allowed merely because new-code tests pass.

Ship a bridge release first that understands both ledger formats, enforces a
workspace writer/serving lease and contains the resumable migration machinery.
Expansion keeps the old ledger and old semantics. Final format transition requires
an exclusive maintenance interval with all pre-bridge server/store processes
stopped and their connections closed; it is not an in-place upgrade beside an
old serving process, which could skip startup checks. Backup/export/restore tools
must understand the new ledger; restoring a complete old backup is a separate
operator action, not downgrade support. Rollback means a later compatible binary
choosing the retained old reader **before** semantic cutover, never reopening a
cut-over database with old authority or a schema downgrade.

### Bridge prerequisites and durable state machine

The current `primitives.NewStore` synchronously calls `BackfillArtifactAccess`,
which gathers all unknown artifacts and reads every blob. Keeping that constructor
would defeat readiness even with empty shadow tables. **Before any expansion**,
the bridge must remove that call from the readiness path and install a supervised,
checkpointed legacy-blob worker. Unknown manifests remain denied by the old reader.

Discovery keyset-pages the artifact PK without filtering `content_refs_json IS
NULL` before LIMIT, visiting at most J candidates and inserting due jobs for the
unknown ones. Discovery cursor and inserts commit together. New/changed canonical
blob writes enqueue their version/hash in the same transaction. Retry work seeks
`(next_attempt_at,id)`, bounds simultaneous fetches, bytes and deadline, persists
backoff after failure and never restarts the whole unknown list on process open.
OpenReadStream must honor cancellation and enforce a byte cap; an over-limit or
unavailable blob stays denied. Worker exceptions, lack of disk headroom and queue
backpressure pause conversion, not readiness and not authorization. Successful
HTTP `/readyz` does not certify that legacy blob recovery is complete.

| Durable state        | Reader / readiness                                                     | Exit condition / invalidation                                                                          |
| -------------------- | ---------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `legacy_ready`       | Old scoped reader, unknown blobs denied; no blob retries before listen | Bridge lease and worker tables available                                                               |
| `discovering`        | Same reader; worker resumes PK cursor/due queue                        | Classified capture snapshot and bounded job slices                                                     |
| `classifying(epoch)` | Same reader; shadow generation inaccessible                            | Complete closure pass at pinned epoch; unknown roots sealed                                            |
| `validating(epoch)`  | Same reader; validation outside write fence                            | Full privacy/count/digest checks pass for that exact generation                                        |
| `eligible(epoch)`    | Same reader until atomic selection                                     | No intervening invalidating change, queue tail drained, sufficient disk, valid exclusive serving lease |
| `cutover`            | Brief explicit business-unavailable interval                           | Ledger fence and generation selection commit atomically, or transaction aborts                         |
| `new_ready`          | New reader, sealed data inaccessible                                   | Forward-compatible recovery may reopen; old binary refused                                             |
| `paused(reason)`     | Last committed reader remains authoritative                            | Worker restart/disk recovery resumes durable checkpoint; no success watermark is advanced              |

Conservatively invalidate a whole closure/validation pass on **any canonical
reference-bearing field, identity/alias, parent, lifecycle, artifact hash/manifest,
series provenance, grant, selected-PM or tombstone/purge change**. A single capture
epoch increments inside each such transaction. Restart the pass from an empty
shadow generation, never splice two closure epochs together. Blob progress that
changes a manifest also invalidates a pass. This can defer cutover under sustained
writes; the old reader stays ready. Pure non-authoritative job telemetry and reader
cursors do not invalidate. A later optimization may narrow dependencies only with
proof; this design does not assume one.

Use a supervised process lease plus a DB fencing token on every worker commit.
A stale process cannot commit after takeover. Pre-bridge binaries must be stopped
before enabling that protocol. Final cutover requires exclusive serving ownership;
a same-process table row alone is not an OS/process lease. Forward recovery verifies
ledger format, migration hashes, schema/feature epoch and selected generation before
serving; incomplete cutover uses the last committed reader. Low disk aborts a chunk
without changing checkpoint/generation. Full kill/restart, lease expiry/takeover and
real ENOSPC tests remain cutover gates; the thin prototype injects worker/low-disk
failures and demonstrates a fresh compatible process opening the fenced DB.

1. **Expand, bounded startup.** Add empty shadow tables, feature/job checkpoint
   rows and bounded capture triggers; no table rebuild, text/blob scan or legacy
   closure at startup. Adding an index to a populated old table is not “metadata
   only”; create indexes only on the empty shadow tables. Keep bridge-adjusted legacy readers
   authoritative (including deferred blob retries). Readiness means the selected old reader and current migrations
   are usable, not that new backfill has finished. Target <5 s warm open and <90 s
   expansion at the SCA-661 scale fixture, with its progress signals retained.
2. **Capture and classify in background.** Every canonical write transaction adds
   a monotonic dirty key/version. An in-process supervised worker persists its
   checkpoint and resumes after restart; no dependence on a task agent surviving.
   Use keyset chunks, e.g. <=256 records and <=4 MiB or 50 ms DB time, whichever
   comes first. Reduce chunks on contention. A complete legacy graph compilation can consume
   O(N+E) work per stable epoch in durable staging tables; it has no startup/read-path budget.
   Cycles/SCCs and unknown/missing ownership are resolved conservatively. Do not
   invoke the old full closure once per record.
3. **Preserve effective audiences.** Classify at a consistent source epoch. Public
   legacy records with no restricted/unknown contributor may stay workspace-scoped.
   A closure with one private audience can map to that stable owner scope only
   after equality/subset verification against old access. Any multiple-owner
   intersection, ambiguous series, unindexed blob or lost parent goes to a sealed
   scope with zero ordinary grants. Never approximate an intersection by a union,
   or assign the workspace scope because `scope_id` is missing. Preserve old
   manifests and relevant provenance in a recovery archive until resolved.
4. **Catch up and validate.** Compare source versions when writing each shadow
   row. Dirty changes, alias/identity creation, grant/PM changes and source purges
   invalidate classification; the capture mechanism must include every old
   ownership source, not just the record body. An invalidating capture epoch change
   restarts the whole staged graph pass as defined above. Under sustained churn defer cutover;
   do not publish partial graph results. Shadow projections are built only from
   classified data; unresolved generations remain unreadable.
5. **Atomic cutover.** Briefly fence business writers, record a high-water epoch,
   drain the bounded remaining change log, compare precomputed generation
   digests/high-water marks, and atomically select the fully validated generation.
   Full counts, graph checks and the privacy differential matrix run **before**
   the fence on a pinned source epoch. Any authority/identity change invalidates
   those results. Do not run a workspace validation scan inside the fence. The
   ledger-format transition and reader-generation selection commit together. If
   the tail does not fit the fence budget, release the fence and retry later.
   No union of old and new visible rows and no per-record fallback to unscoped
   legacy data. Cache/stream cursors invalidate at the feature epoch change.
   The old reader remains selected until this transaction commits.
6. **Contract and retire.** After accepted validation and a retention interval,
   stop legacy trigger maintenance, reclaim redundant graph tables in bounded
   maintenance and retain canonical evidence/aliases/tombstones. Do not claim
   immediate disk shrink; free pages are reusable. Incremental vacuum is only
   available if enabled appropriately; otherwise schedule explicit offline
   compaction with spare disk. A crash after every checkpoint/DDL/cutover step
   must reopen to exactly one authoritative generation.

Old readiness remains subject to old reader performance during conversion; this
design does not promise an instant latency repair before cutover. Budget disk
headroom for old graph + shadow data + WAL; preflight space, throttle queue growth,
pause conversion safely on low disk and report sanitized progress. Permission
and privacy checks never fail open when backfill is delayed or unavailable.

Bulk security reclassification is deferred. Its endpoint does not exist in v1,
and ordinary scope-changing writes fail uniformly. Lifecycle archive/restore jobs
do not grant authority to reclassify, publish or recover sealed content.

## Reviewable phases and concurrent work

This design is one PR; the following are future phases, not permission to start
implementation before acceptance. Prefer coherent changes over one PR per table.

1. **Contracts and closed storage boundary:** ratify semantics/limits, add scoped
   reviewed query templates, small ownership manifest, analyzer and negative tests; add expand-only migration
   and resumable job infrastructure. Keep old serving behavior. Contract changes
   land before consumers with `make contract-gen` and committed contract checks.
2. **Canonical ownership and writes:** add scopes, grants, structural constraints,
   guarded references, publication policy and dual maintenance. Port every
   inventoried source, including optional PM tables, auth/profile metadata,
   blobs, series and replay. New features cannot default to workspace scope.
3. **Bounded reads and projections:** inbox/work/overview, directories, search,
   SSE, reports and exports; differential tests against old privacy; new cost gates
   must pass before enabling any new reader. Do not ship only fast top-level routes
   while retaining an O(N) nested label/context helper.
4. **Migration and retirement:** full crash/fuzz/upgrade evidence, rehearsal on
   synthetic old schemas, scope recovery and cutover; remove old graph only after
   route and field matrices pass. Reassess storage and write budgets on full data.

SCA-665's bounded PM/history queries, normalized inbox columns and batched
enrichment remain valuable. Port their filter/order keys into scope-prefixed
indexes; keep their tests instead of rewriting their active branch. SCA-661/#295
owns the scale harness and budget/EXPLAIN infrastructure: extend it, do not create
a competing route inventory or relax its red baseline. SCA-642/#275's initiative
roles become an indexed work-feed dimension; its health inputs become same-scope
projection inputs and bounded transient cross-scope refs. Its generic external
evidence keys remain canonical aliases; published-key ownership becomes guarded
resolution or explicit same-scope evidence, never unreviewed public snapshots.
Its 64/65 migrations are unchanged. No work here blocks those interim releases.

## Test and acceptance strategy

Keep all current privacy regressions until their mapped replacement is accepted.
Run each exact route as owner, unrelated human, unauthorized agent, selected PM,
former PM and anonymous development reader. Include known nonexistent targets
and public positive sentinels so a universal 404 cannot pass. Enumerate all
resource fields, JSON paths, writer/import paths, revisions, binary manifests,
purges, optional PM kinds, ancillary profiles, replay and quota paths. Deliberately
misclassify a reference column and add a raw mux route/SQL helper: CI must fail.

Differential privacy tests hold visible inputs constant while adding/changing
hidden titles, documents, actors, inbox rows, PM history, series and events.
Compare response bodies, ranks, totals, snippets, cursors, cache headers and SSE
payloads. Exercise revocation between scope loading and statement execution,
between ticks, before replay/cache return, and concurrent source/destination
mutation. Compare write success/error outcomes for guessed missing and
inaccessible references, including nested typed atoms. Interleave hidden inserts
between visible rows and prove cursors reveal no global allocation gaps. Verify guarded-reference redaction in JSON, HTML,
reports, CLI and
download metadata, not only the UI label. Test all prior binary boundary cases.

Use SCA-661's 4,096-row-per-family fixture and actual SQL capture. Its current
defaults are 500 ms per read with a 1 s deadline, one warmup/four measured samples,
statement/row limits, two SSE flushes, no automatic indexes or large-table scans,
and no custom WHERE functions over large tables without a narrow reviewed
exception. Retain the 90 s upgrade / 5 s warm-open gates. The prototype's 25-sample
timings supplement those gates; they do not replace them. Add 1x/10x/100x corpus
growth with fixed K/P, and separately scale K, private fraction, fanout, grants,
history and query selectivity. Count work even when SQLite returns only one row.

EXPLAIN tests require scoped index SEARCH, no temp sort over unbounded matches,
and no repeated graph preparation. Test page 1 and deep cursors, all supported
filter combinations, empty phrase searches with long postings/large candidate
bodies (assert V even when P returned is zero), late alias resolution,
idle/second SSE ticks, expired health watermarks and a slow projection worker.
Clock-driven changes and retention must not trigger request-owned rebuilds.

Mutation gates measure reference-heavy inserts/updates, public/private writes,
grant revocation, alias changes, deletes and high-fanout targets. Assert Q/R bounds
and absence of synchronous dependency fanout. Count search posting insertions
and old-head deletions, including a tiny PATCH/delete against a maximum-token
head; Q does not include or hide those costs. Verify atomic counter consistency
under retries, idempotent replay, cancellation and process death. Measure live
authorization/projection bytes, WAL peaks and reclaimed pages independently.

Migration tests cover actual released schemas 54/60/63 and post-#275 64/65,
preview-series uncertainty, missing blobs, arbitrary binary bytes, cycles, late
parents and purged owners. Kill/restart after every chunk and immediately before
and after cutover; inject low disk, worker failure, concurrent changes and
write-fence expiry. Assert no stale shadow generation becomes readable and that
restart does not repeat a full scan on the readiness path. Make the older binary
refuse the new feature epoch, and test recovery with a forward-compatible build.

## Rejected alternatives

- **Keep full arbitrary transitive mention inheritance with only one scope.** A
  mention can require the intersection of unrelated audiences; target creation,
  alias changes and revocation can invalidate arbitrarily many descendants.
  A single precomputed scope only hides that graph in write fanout or exponentially
  many intersection scopes. It cannot meet both unrestricted semantics and bounded
  ordinary writes. Keeping it as an offline legacy compiler is reasonable.
- **Redact the mentioned name from arbitrary text or binary.** Surrounding facts,
  titles, JSON keys, images and counts can still reveal the source. It is neither
  a privacy proof nor a byte-preserving operation.
- **Cache the global denied set longer / optimize recursive CTEs again.** Helps a
  constant but keeps invalidation, preparation and cold-request cost coupled to
  the corpus. The current fixes are worthwhile interim work, not the final model.
- **Filter candidates after LIMIT or rank globally then redact.** Leaks hidden
  corpus properties and produces incorrect pages; does not bound candidate work.
- **One database per scope.** Strong isolation but complicates cross-scope
  transactions, blob ownership, backup and schema evolution. An optional future
  physical partition should preserve this logical model, not be required by OSS.
- **Per-principal materialized feeds.** Read-efficient but writes/retention grow
  with audience membership. Use per-scope/per-bounded-recipient rows instead.
- **A PostgreSQL/RLS or external search service prerequisite.** Violates the
  SQLite-per-workspace constraint. A future backend could implement the same
  typed repository contract without changing product privacy semantics.
- **Big-bang startup rewrite or automatic legacy declassification.** Neither a
  startup timeout nor a small dataset is permission to weaken privacy. Use
  resumable conversion, sealed ambiguous records and an atomic feature epoch.
