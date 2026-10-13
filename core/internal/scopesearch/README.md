# Bounded scope search

The bounded executor and HTTP helper remain disabled. Migration 82 now installs
an active canonical comment writer and an online maintenance worker using this
package's `PrepareComment` normalizer. See [the live cutover status](../../docs/comment-search-cutover.md)
for authority, coverage, evidence and the remaining reader switch. Legacy
`SearchDocuments` and aggregate rebuilding still serve requests.

`Search` receives a trusted `Repository`, a persistent-secret token codec and a
finite request selection. The repository must authorize **all** selected scopes
before invoking the callback and keep binding checks, candidate keys and bounded
text hydration in one transaction. It exposes only typed operations; this package
has no database import, SQL handle or capability factory.

Search indexes current document heads and independent comment resources. `Apply`
replaces only that resource's postings in the caller's canonical transaction;
`ApplyCanonical` consumes the frozen `scopes.Change`, conservatively OR-ing
`After.SourceTruncated`, the existing trusted source-prefix argument and its own
normalization truncation. Its writer must delete at most 4,096 old postings, insert at most
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
The symmetric token ceiling is derived from the complete cursor JSON shape:
62 scaffold bytes + two 64-byte hexadecimal fingerprints + 19 recency digits +
8 kind bytes + sixfold JSON escaping of a 512-byte RID and 256-byte scope =
4,825 bytes. AES-GCM's 12-byte nonce and 16-byte tag produce at most 6,471
unpadded base64url bytes. Both encoding and decoding enforce this same bound;
maximum HTML-sensitive and control-character keys resume an unchanged HTTP query.

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

## Reader integration

The former uncalled `scopedrepo.SearchCanonicalHook` adapter has been removed.
The live writer is `storage/comment_search.go`; it keeps canonical event
identity and current `resourceaccess` authority instead of granting new scope
membership. It captures the first 64 KiB of raw comment input before the shared
normalizer, with an explicit truncation bit, and replaces at most 4,096 postings.

The existing server reference adapters continue to test the bounded executor,
continuations, HTTP helper and snapshot interface. They do not certify the
registered `/docs/search` dispatcher. The live-table parity adapter is also
test-only: its fixed generations and point hydration are deliberately unsuitable
for serving. Production activation still needs a reviewed repository, current
document-head integration, lifecycle/filter/ranking contract, durable generation
invalidation, batched hydration and a fresh aggregate fallback. See the cutover
document for the exact remaining gates.
