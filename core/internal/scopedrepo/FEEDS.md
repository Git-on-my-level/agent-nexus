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
projection generations must never be reused. Foreign-key-enabled deletion of a
membership cascades its stream bindings.

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
