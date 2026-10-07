# Staged feed repository

`Store.ReadFeed(ctx, request, streams, func(FeedReader) error)` supplies a typed,
read-only capability to one callback. `FeedReader` has these methods:

```go
Snapshot() (FeedSnapshot, error)
Candidates(stream int, after *FeedKey, limit int) ([]FeedCandidate, error)
Hydrate([]FeedReference) ([]FeedItem, error)
Buckets([]string) (map[string]int64, error)
```

The enclosing context controls all queries. Repository types mirror the frozen
read-model shapes without importing that package. A future trusted adapter can
translate these types and the read-model's context-bearing method signatures.
No such adapter, HTTP reader, factory exposure, or production initializer is
enabled here. The foundation import test remains unchanged.

## Authority and readiness

One read-only SQLite transaction first authorizes every selected membership,
including authorized transitioning scopes. It then resolves generation state and
exact `(principal, scope, generation, family, audience)` bindings. A binding must
match the current membership generation. Missing or stale bindings deny the
whole request; no candidate query or callback runs with partial authority.
Scope membership never implies permission for an arbitrary audience string.

Readiness requires an active domain and four persisted certificate versions
matching the repository's reviewed version, all for the current domain generation.
The captured legacy authorization epoch must also match the existing
`resource_access_epoch(singleton=1).version` in that same transaction. Missing
certification, mismatched versions, and epoch changes produce `Ready=false`.
Transitioning slots remain in the snapshot as `scope_updating`. Every data or
count method refuses the entire selection if any selected scope is unready;
even empty hydration and zero-stream counts cannot manufacture available zero.

These version fields represent trusted persisted review receipts, **not proof
that parity or the legacy authorization budget has been achieved**. This change
provides no certification setter or writer. Actual certification, atomic
generation publication, canonical source capture, feed/payload/counter parity,
audience disjointness, lifecycle invalidation, and complete mutation coverage
remain enablement gates. The legacy epoch must cover every relevant canonical
and external PM source; matching its current value does not establish that
coverage. Regrant writers must monotonically advance membership generations;
audience binding changes must advance their binding generation. Domain and
projection generations must never be reused. Deletion of a membership cascades its stream bindings through persistent
triggers even on connections with foreign keys disabled. This indexed operation
can touch historical generations; bounded retirement and revocation remain
write-enablement gates.

The binding digest includes principal identity, ordered scope/stream selection,
roles, domain state/generation, membership generations, certificate versions and
captured epochs, current legacy epoch, and audience binding generations. As-of
time is fixed for the callback but excluded from the digest so a continuation
can survive a subsequent transaction with unchanged authority. Directory
continuation is empty and `MoreScopes=false`: this API only covers its explicit
selection and does not assert directory completeness.

## SQL and data budgets

There are at most 64 scopes, 256 streams, and four streams per scope. Strings
are bounded (principal/scope/audience 512 bytes, family/bucket 128 bytes), valid
UTF-8, nonempty, and NUL-free. The reader has its own budget; the generic reader's
256-operation allowance cannot accommodate 256 candidate queries plus hydration.

| Operation                 | Per callback bound                                               |
| ------------------------- | ---------------------------------------------------------------- |
| Membership authority      | 64 exact queries, one row each                                   |
| Current legacy epoch      | 1 singleton query                                                |
| Generation certificates   | 64 exact queries, one row each                                   |
| Principal stream bindings | 256 exact queries, one row each                                  |
| Candidate reads           | Once per stream, at most 256 queries, 101 rows each              |
| Hydration                 | Once, at most 100 admitted references, one VALUES batch          |
| Counters                  | Once, 1–4 unique buckets × at most 256 streams, one VALUES batch |
| Snapshot copies           | 8, without SQL                                                   |

This is at most **643 SQL statements and 27,365 result rows**, including initial
authorization, for a combined maximum page plus counts callback. This exceeds
the unchanged **100-SQL full-request serving gate**. It does not authorize
production use at maximum fanout: batched authorization and candidate scheduling,
plus measurement of the complete request including legacy authorization, are
required before activation. The bounded repository foundation is not a claim
that SCA-661 or the serving budget is complete.

Each seek uses the exact scope, generation, family, and audience prefix of the
feed primary key before a `(sort_key,rid)` range and `LIMIT`. No parent or
audience filtering follows that limit. At most 25,856 fixed-size candidate tuples
are retained as the admission set. Hydration requires those exact candidate
tuples and rejects duplicate RIDs. One bounded VALUES relation drives exact feed,
payload, RID registry, and resource identity/version probes. Missing rows,
versions, invalid JSON, or invalid identities fail the whole hydration. Returned
payloads are at most 16 KiB apiece (1,638,400 payload bytes per callback), and
refs at most 512 bytes. SQL CASE guards prevent oversized persisted payload or
identity strings from being copied into Go. Output order follows input ordinals;
no SQL query is issued per hydrated row.

Counters are sparse: **only for a certified ready generation**, an absent exact
stream/bucket row means zero. A missing generation certificate means unavailable,
never zero. The single VALUES batch has at most 1,024 exact counter probes.
Nonnegative signed 64-bit addition is checked before summing; corruption or
overflow returns an error rather than wrapping. The generation parity writer
must make this sparse-zero contract true before certifying a generation.

Capability methods serialize through one mutex, copy all returned slices and
payloads, and expire before callback success, error, or panic leaves `ReadFeed`.
Every capability error is sticky: ignoring it in a callback cannot make the
outer read succeed. A new transaction reauthorizes; a concurrently committed
revocation does not mutate the already-pinned snapshot.

## Schema and remaining gates

`Initialize` mints a 128-bit random workspace namespace once, in its schema
transaction. The singleton is immutable under UPDATE, DELETE and replacement,
and survives database reopen and repeated initialization. Both feed authority
bindings hash that namespace; selection and serving receipts and encrypted
continuations therefore cannot cross separately initialized databases even
when all local identities/clocks and the codec key match. Existing receipts
without this namespace binding become unavailable and require fresh proof.
The namespace is private authorization metadata and never appears on the wire.

`InitializeFeedSchema(ctx)` installs only empty shadow DDL, separately from the
existing foundation initializer. It intentionally fails when these tables already
exist; it performs no upgrades, automatic registration, backfill, or certification.
The existing canonical authorization epoch table is a prerequisite, not created
or populated by this initializer. The feed and counter keys match the read-model
proposal; helper tables hold generation receipts, exact audience bindings, and
audience-specific payloads. Payloads never join through RID alone.

All new tables require reviewed storage inventory and migration registration.
Reviewed constructors/configuration, atomic canonical source writers, certification
and re-certification, cursor binding/transport adaptation, the computation
call-graph analyzer, route privacy tests, and the full SCA-661 acceptance work
remain explicit gates. No caller-supplied scope provenance claim is introduced.

SQLite regressions cover wrong principal/family/audience/selection, 64 scopes and
256 streams with one batched 100-item hydration and 1,024 counter probes,
10,000 irrelevant feed rows with indexed query plans, stale payload/identity
versions, invalid/oversized JSON, stale epochs, lifecycle transitions, sparse
counters, overflow, immutable transaction snapshots, next-transaction revocation,
capability expiry (including panic), concurrency, and ignored SQL/budget errors.

## Proof-bound batch selection (disabled)

`Store.ReadBatchFeed(ctx, selection, streams, callback)` is the next bridge
boundary. It leaves the old `ReadFeed` experiment intact. The callback receives
`BatchFeedReader`, with `Candidates(after []*FeedKey, size int)` returning one
ordered global P+1 slice of `FeedReference`, followed by the existing exact
hydration and grouped counters. Every stream's continuation must come from a
trusted cursor whose binding equals `Snapshot().Binding`. The future dispatcher
must perform that validation and preserve directory coverage. This is an
internal capability, not a transport accepting caller-authored keys or proofs.

Admission executes a bounded scope/membership/generation batch, the legacy epoch
lookup, and an exact audience-binding batch before consulting proof state.
Any invalid scope or audience rejects the entire selection before the callback.
Only then does an indexed `scope_feed_selection_proofs` lookup admit the exact
ordered authority binding. Its versioned record requires complete audience,
lifecycle, payload and counter comparison plus cross-stream RID disjointness.
No exported API writes this record. Missing, stale or malformed proof returns
`scopes.ErrUpdating`, without candidates, payloads or synthetic counts. That
means the future HTTP dispatcher must use the old reader for the whole response.
The older generation flags alone never admit this batch capability.

The proof binds principal, ordered scopes/streams, roles, scope/membership/audience
generations, generation certificates and legacy authority epoch. A monotonic
shadow/identity revision also invalidates proofs on writes to resources, RIDs,
domains, memberships, feed/payload/counter rows, generations and audience bindings,
including ABA changes. The cursor binding includes this revision and the compiled
policy/projector versions. All checks and reads use one transaction snapshot.

**There is deliberately no production proof minting.** The revision clock covers
shadow tables and canonical identity, not all old canonical ask/answer state or
external publication authority. Before production use, B's source capture and D's
complete enumeration must let a trusted comparator compare actual old/new source
snapshots under the complete old policy, not matching scope tags or epochs.
It must reject duplicate RIDs over the _entire_ selected generation and publish
only under the same source snapshot and publication authority. Canonical and
external-PM mutations must atomically invalidate that proof. The current raw
trusted hook API is not a permitted production proof writer; production hook
registration and exact SQL admission remain separate gates. Test fixtures inject
records using raw SQLite solely to exercise consumption and corruption defenses.

For nonempty maximum selection, the repository uses **7 SQL / 527 returned rows**:
64 scope authorities, one epoch, 256 bindings, one proof, 101 candidates,
100 hydrated items and four aggregate counters. Each stream seek has its exact
primary-key prefix and P+1 bound before UNION; at most `S*(P+1) = 25,856`
candidate tuples can be examined by the bounded merge. It does not scan unrelated
feeds. Plan tests and an instrumented projection count verify the bound with
30,000 rows, both initial and deep continuation seeks. Grouped counters perform
at most 1,024 primary-key probes, return at most four rows, and fail on signed
integer overflow or corrupt values. Local duplicate detection is defense in
depth; it does not establish the cross-page disjointness proof.

These are repository bounds, **not full-request acceptance**. Directory discovery,
legacy denial preparation, HTTP parity, serialization and stale-proof fallback
must still satisfy SCA-661 with personal-size/10x fixtures and 64 scopes. No
allowance or budget changes, route changes, startup migrations, production
registration or serving import exceptions accompany this checkpoint.

New unregistered authority columns are `scope_feed_proof_clock.singleton/revision`
and all `scope_feed_selection_proofs` fields: authority-binding digest, source
revision, format/policy/projector versions, four comparison flags and disjointness.
They contain no projected user payload. They must join the live storage inventory
and reviewed migration only when their trusted production writer is defined.
