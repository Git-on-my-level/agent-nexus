# Explicit privacy domains and bounded read models

Status: proposed; product decisions below require acceptance before implementation.
This PR changes documentation only. The experiment is on a separate, non-merge
branch. It neither changes production authorization nor authorizes a deployment.

## Recommendation and decisions

Give each canonical record one **scope**, an explicit privacy domain. Scope
membership is durable authority; views and search are indexed by that same scope.
Replace general SQL rewriting with generated, scope-bound repositories. Compute
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
   require an explicit publication action and authority for both source and
   destination; ordinary edits cannot change scope. A bulk reclassification is an
   asynchronous, fenced operation with temporarily unavailable affected scopes.
   Previously disclosed public bytes cannot be recalled from clients.
3. **Accept bounded surfaces and explicit freshness.** Recommend keyset pages,
   per-scope counts, capped search work with continuation, and eventually updated
   initiative health with an `as_of` marker. Never manufacture an exact total or
   “on track” state from a truncated scan. Inbox answers and their counters remain
   synchronous. This preserves the dense executive Overview without loading the
   entire workspace.
4. **Prefer temporary unavailability over migration disclosure.** Recommend
   retaining old readers during background conversion, and sealing ambiguous
   legacy records until reviewed. Mixed-owner records, unreconstructable series,
   unavailable blobs and unknown ancestry do not become workspace-visible.
   Recovery is a bounded, explicitly authorized per-record operation; no startup
   deadline justifies discarding their restrictions.

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

A scope names a stable audience, not a folder label. Examples are workspace,
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
constants may contribute). No membership-subset inference, intersection scopes,
or automatic declassification. The source scope still owns a computation if it
uses a public constant. A private source's later membership revocation therefore
also revokes its derived content, without finding dependent rows. Scope identity
does not change merely because two scopes currently have identical members.

Raw text/JSON has the destination's declared classification. Parse only the
bounded incoming write, preserving scalar-versus-JSON semantics and existing NUL
validation. Resolve at most the request's allowed reference count. Reject known
private cross-scope atoms rather than accepting them and searching for a safe
substring replacement. Structured nodes are the sole automatic redaction path.
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

Aliases resolve to immutable resource keys. Reserved tombstones prevent handle
reuse from changing a guarded link's meaning. Unresolved links retain only an
opaque pending token; late resolution is a target-authorized read, not a workspace
rescan or a retroactive rewrite. Legacy untyped IDs and alias normalization stay
supported at API boundaries, with bounded resolution. Server responses never
echo a denied input's inferred canonical ID or title.

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
  scope_id INTEGER NOT NULL, seq INTEGER NOT NULL, rid INTEGER NOT NULL,
  version INTEGER NOT NULL, PRIMARY KEY(scope_id,seq)
) WITHOUT ROWID;
CREATE TABLE search_postings (
  scope_id INTEGER NOT NULL, term TEXT NOT NULL, sort_key INTEGER NOT NULL,
  rid INTEGER NOT NULL, PRIMARY KEY(scope_id,term,sort_key,rid)
) WITHOUT ROWID;
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
2. A schema manifest generates sealed resource types, repositories, constructors
   and bound query variants. Every table/view/column must be owned, authority-only
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

## Bounded queries and read models

One `scope_id IN (...)` plus LIMIT is not a sufficient cost proof: SQLite may scan
all rows or sort all matches to produce a global order. For each authorized active
scope, seek its composite index with a keyset cursor, fetch at most `P+1` keys,
then perform a heap merge and batch hydrate the final P rows. One bounded SQL
batch may contain the per-scope subqueries; no query per returned row. For K
scopes this intentionally admits at most `K(P+1)` candidates. The cursor contains
the last total-order key, query identity, grant generation and projection version,
authenticated by the server. It contains no hidden offsets or global event IDs.

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
- **Search:** use scoped term postings and content-local ranking (e.g. term
  presence/local frequency then recency), never global BM25 statistics. At most T
  terms, K scopes and B posting candidates per stream are visited. AND/phrase
  verification happens only in that bounded candidate window. Return continuation
  even for an underfilled/empty page when candidates remain; no “keep scanning
  until P matches.” No exact total, unbounded ranking, or hidden-corpus IDF.
  Snippets hydrate visible rows only. Prefix/fuzzy expansion has its own bound;
  overflow reports incomplete results. Search contract/UI must acknowledge this.
- **SSE:** scope-local change sequences and index seeks after the last authorized
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

### Cost model and limits

Let N be indexed records, K the principal's selected/accessible scopes, P page
size, H bounded hydrated refs per page, L input bytes, R input refs, Q fixed
projection/counter updates and T/B search terms/candidates. Index operations cost
`O(log N)`; “bounded by the request” excludes an O(N) corpus term, not B-tree depth.
Proposed technical limits: P <= 100, H <= 200, K <= 64 per request, R <= 200,
T <= 8, B <= 4P, Q <= 16 and normal structured body <= 1 MiB. Blob uploads retain
their explicitly configured byte limit and use streaming validation. These are
generic safety limits, not commercial tiers. They require contract review before
implementation; current larger batch callers must split requests.

K is explicit: more than 64 grants requires selecting scopes or paginating a
scope directory. Do not silently truncate authority or promise an exact
workspace-wide Overview across an unbounded set. Selected PM/all-scope readers
use the same scope selection limits. Grant enumeration queries `LIMIT 65` to
detect overflow without loading all memberships.

| Route class                          | CPU / rows / SQL budget shape                                                         | Required index/access                                                        |
| ------------------------------------ | ------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| Point / revision / blob metadata     | O(log N + K + H log N); O(1+H) rows; fixed batch queries                              | `(kind,external_id)`, `(rid,version)`, grant PK; permission before blob open |
| Lists / work / history / directories | O(K log N + KP + P log K + H log N); <= K(P+1)+H keys; fixed batches or <= K seeks    | Scope + exact selector + total order + rid                                   |
| Inbox and summary                    | Page bound above; counters O(KQ); no projection rebuild                               | Scope/recipient/status/time/rid; counter PK                                  |
| Overview                             | Fixed section count times page bound + O(KQ); bounded plan/label hydration            | Initiative role/attention scoped feed, counters, actor key                   |
| Search / read-only search POST       | O(KT(log N+B) + KTB log(KT) + PL); L here is capped per-hit snippet bytes             | Scoped postings; no global FTS rank/statistics                               |
| SSE tick                             | O(K log N + KP + P log K); idle O(K log N); cap response bytes                        | Scope-local change PK and grant generation                                   |
| Normal mutation                      | O(L + (R+Q) log N); <= R lookups and Q derived writes plus bounded canonical children | Typed identity, same-scope parent, counters/feeds/change indexes             |
| Grant add/revoke                     | O(log N), fixed authority/generation writes; no resource fanout                       | Grant PK, principal authority generation                                     |
| Maintenance / reclassification       | Each job slice O(J log N + bounded bytes); total may be O(N+E)                        | Keyset checkpoints; never request-owned or startup work                      |

The implementation must budget serialized bytes as well as row counts. Large
single records/revisions are not a loophole. Supported endpoints with larger
output become ranged/paginated or asynchronous through reviewed contracts.

Ordinary writes update at most Q fixed views, independent of workspace size and
recipient count. Plan edits bound steps and references; reverse dependency
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

The guarantees above cover the PR descriptions' stored-source and surface tables,
not just their top-level summaries. During implementation, each row must map to
existing regression names plus new scoped equivalents before deleting a trigger.
Legacy restrictions lost in pre-migration purges cannot be reconstructed; this
existing limitation must remain documented, never converted into proof of access.

## Forward migration from schema 63

Do not allocate migration 64: #275 currently owns 64 (board roles) and 65 (evidence
projection/indexes). Allocate after the actual merge head. Preserve all released
migration hashes and historical schemas. This is a forward-only feature epoch;
older binaries must refuse the new epoch. Rollback means a later compatible
binary choosing the retained old reader before cutover, never a schema downgrade.

1. **Expand, bounded startup.** Add empty shadow tables, feature/job checkpoint
   rows and bounded capture triggers; no table rebuild, text/blob scan or legacy
   closure at startup. Adding an index to a populated old table is not “metadata
   only”; create indexes only on the empty shadow tables. Keep schema-63 readers
   authoritative. Readiness means the selected old reader and current migrations
   are usable, not that new backfill has finished. Target <5 s warm open and <90 s
   expansion at the SCA-661 scale fixture, with its progress signals retained.
2. **Capture and classify in background.** Every canonical write transaction adds
   a monotonic dirty key/version. An in-process supervised worker persists its
   checkpoint and resumes after restart; no dependence on a task agent surviving.
   Use keyset chunks, e.g. <=256 records and <=4 MiB or 50 ms DB time, whichever
   comes first. Reduce chunks on contention. Legacy graph compilation can consume
   O(N+E) total work in durable staging tables; it has no startup/read-path budget.
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
   ownership source, not just the record body. A conservative workspace epoch
   change may restart a staged graph pass. Under sustained churn defer cutover;
   do not publish partial graph results. Shadow projections are built only from
   classified data; unresolved generations remain unreadable.
5. **Atomic cutover.** Briefly fence business writers, record a high-water epoch,
   drain the bounded remaining change log, validate counts/digests and the privacy
   differential matrix, and atomically select the fully validated generation. If
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

Scope reclassification uses the same fenced job machinery after migration:
revoke readability of the entire affected source/destination scopes in O(1)
state writes, rebuild all affected canonical and derived rows in chunks, validate,
then activate a new generation atomically. Cross-scope generated descendants are
forbidden, so the fence does not require a hidden transitive graph. Guarded links
already authorize their targets. Until a safe reclassification implementation is
accepted, ordinary scope changes remain rejected.

## Reviewable phases and concurrent work

This design is one PR; the following are future phases, not permission to start
implementation before acceptance. Prefer coherent changes over one PR per table.

1. **Contracts and closed storage boundary:** ratify semantics/limits, add scoped
   repository generator, analyzer and negative tests; add expand-only migration
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
mutation. Verify guarded-reference redaction in JSON, HTML, reports, CLI and
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
filter combinations, empty searches with long postings, late alias resolution,
idle/second SSE ticks, expired health watermarks and a slow projection worker.
Clock-driven changes and retention must not trigger request-owned rebuilds.

Mutation gates measure reference-heavy inserts/updates, public/private writes,
grant revocation, alias changes, deletes and high-fanout targets. Assert Q/R bounds
and absence of synchronous dependency fanout. Verify atomic counter consistency
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
