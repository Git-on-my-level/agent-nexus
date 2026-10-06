# Bounded read-model kernel

This package is stream B's integration candidate for SCA-666. It has no
production caller, feature enablement, database factory, HTTP route, or migration
registration. Existing authorization remains authoritative. It is based on A's
foundation `c03b020a` and reuses its `scopes.ID`, `scopes.Stream`, `scopes.Page[Item]`,
`scopes.Coverage` and budget constants. A has published RequestSelection,
Reader/Writer and Change. Feed/hydration repository methods, constructor-hook
wiring and the numeric RID mapping are still pending; B's adapter interfaces are
provisional.

`Read` seeks at most `P+1` keys per already-authorized exact stream, heap-merges,
and batch-hydrates at most `P` rows. Request limits are 64 scopes, four streams
per scope, 256 streams total and 100 returned rows. Continuations use AES-GCM
with a server-owned persistent key; their binding includes principal/query/grant
identity, scope generations, ordered streams and directory coverage. Integer
allocation keys remain internal. Rejected candidates never trigger a refill
scan. Projection identities must be disjoint across audience streams.

`Count` reads up to four supported buckets with explicit shared scope coverage. A
transitioning scope makes the entire selected page/count unavailable; values are
absent, never synthetic zero. `Coverage.UnavailableScopeIDs` identifies both
transitioning and not-yet-proven selected generations. `Scope.Ready` defaults
false and represents proven
uniform legacy audience, lifecycle and projection parity plus full-request
authorization preparation within budget for that generation. A boolean supplied
by a handler is not evidence: only A's trusted repository may supply it from
durable verified generation state. Expired scheduler health watermarks must
also produce updating status in the repository snapshot.

## Proposed shared-file integration for A

1. Adapt `Reader` and `CounterReader` to transaction-bound `scopedrepo.Reader`
   templates and `scopes.RequestSelection`. Resolve current scope and audience
   authority before querying; obtain Snapshot and all feed/counter/hydration
   reads in the same transaction. Include unavailable grants as directory slots.
   Use directory keyset pages of 64 slots plus one lookahead, without filtering
   transitioning slots after the grant seek.
2. Integrate `SchemaProposal` and the named templates into the lead-owned
   migration and reviewed manifest. Generation participates in feed/counter
   keys. Add registry foreign keys using the frozen authority schema. Never
   install schema from a request or constructor. No migration number is reserved
   by B; allocate against the actual merge head after SCA-642.
   A's foundation currently has opaque TEXT resource IDs without a numeric
   registry key. Supply an internal RID-to-opaque-resource mapping (or freeze an
   equivalent opaque ordering key) before wiring these candidate templates.
3. In the single canonical mutation hook, derive bounded old/new `Projection`
   fields from A's `Change`, validate `PlanDelta` before source writes, and apply
   feed/counter operations inside the source transaction. Check exact affected
   rows for old feed deletion and decrements. An absent/stale old row, counter
   underflow, integer overflow or hook fault aborts the whole source mutation.
   The sum of feed and counter statements is capped at 16. Publication and
   cross-scope transfers require separately authorized operations; PlanDelta
   rejects changing its scope or generation.
4. Lifecycle initiation performs scope fence, parent lifecycle write and durable
   job enqueue atomically with constant foreground work. Refuse legacy
   cross-scope lifecycle inheritance until its workspace fence/rebuilder exists.
   D supplies `LifecycleTx` under its claimed fencing token: indexed keyset
   selection of <=64 records, complete same-scope ancestor checks of depth <=8,
   reviewed projection rules (including independent inbox asks), and staging
   deltas plus checkpoint in one transaction. Target-generation partitions start
   empty. Activation selects exactly the rebuilt generation under the same
   fence; failures and expired claims activate nothing. Old partitions are
   cleaned asynchronously. Ordinary source mutations stay fenced throughout.
5. Supply `RankBetween` with indexed predecessor/successor queries on
   `(scope,board,column,rank,rid)` using `LIMIT 1`, in the mutation transaction.
   Exhausted gaps return `rank_gap_exhausted` before any source/projection writes.
   This package performs no automatic column rebalance.
6. After SCA-665 lands, B can integrate legacy handlers within its assigned file
   boundary. A owns constructor/mutation-hook wiring, contracts, route/auth
   registration and inventories. No endpoint may enable the bridge before real
   HTTP parity and SCA-661 full-request cost gates pass.

## Cost and evidence

| Operation                  | Bound / proposed index                                                                                                             |
| -------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| Feed keys                  | `O(S log N + SP)`; `scope_feed` composite primary key, generation/family/audience equality before keyset/limit                     |
| Merge/hydration            | `O(S + P log S)` merge, `O(SP)` admitted-key storage; one batch of `P` resource/version keys and <=16 KiB payload per item         |
| Exact counters             | `O(KQ log N)` for fixed selected dimensions; generation-bound `scope_counters` primary key                                         |
| Normal projection mutation | At most 16 exact feed/counter writes, four audience destinations; `scope_feed_resource` uniqueness prevents duplicate sort entries |
| Lifecycle slice            | <=64 records and <=8 ancestors each; authority registry `(scope_id,rid)` keyset plus ancestor primary keys                         |
| Rank arithmetic            | Constant arithmetic after at most two indexed neighbor seeks; explicit gap refusal                                                 |

Focused tests execute the proposed feed/counter SQL on modernc SQLite and check
SEARCH/no-scan/no-temporary-sort plans. A 64-scope fixture dominated by 64,000
wrong-audience rows consumes 64 selected candidates for a one-item page. Durable
SQLite lifecycle tests examine 10,002 metadata records in 157 nonempty chunks,
restart mid-job, inject checkpoint failure, preserve the selected generation
and rebuild restore into a new generation. Counter tests check transactional
source rollback, underflow and integer overflow. Unit tests cover continuation,
authority invalidation, coverage, fanout refusal and full-range signed ranks.

These are kernel/adapter-contract tests, **not production HTTP acceptance**.
Remaining gates are real repositories/HTTP/durable production worker tests,
legacy visibility parity before paging/counts, lifecycle depth/independent-ask
and compatibility behavior, actual indexed rank lookups, the SCA-661 harness
including denial graph preparation, and route/storage/derivation inventories.
No latency improvement or reader readiness is claimed by this package.
