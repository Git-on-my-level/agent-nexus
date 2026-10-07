## Inbox integration for A

#302, #301, #305, #306 and #307 are on main. B has integrated those released
changes and A's `4e9f0c12` trusted adapters/phase plan plus `48b65e2f` batch
admission while preserving A/C/D
implementation, privacy gates and budgets. The Linux legacy-test-name fix is
retained. No handler or constructor selects B yet. Follow
`docs/design/scope-phase-two.md`; #308 owns the inbox stream follow-up before
the eventual minimal handler switch.

### Concrete next wiring

1. Use `primitives.WriteScopeInboxItem` for bounded point mutations inside the
   existing `resourceaccess.Tx`. Its required trusted capture receives detached
   column-authoritative before/after rows; an ignored error rolls back the source
   transaction. Validate the real source and private RID tuple, advance the
   canonical version, and call `ApplyCanonicalHooks` plus B's projection writer
   in that transaction. `EncodeScopeInbox` stores a <=16 KiB envelope;
   `DecodeScopeInbox` requires the independently joined exact tuple and preserves
   unknown JSON fields. Neither method grants scope/audience provenance.
2. A's disabled `InstallScopeInboxInvalidation` consumes the detached
   `ScopeInboxMutationLedger` together with the ownership and directory inventory
   for bulk `ReplaceDerivedInboxItems`, decision/answer/read state, report reviews,
   lifecycle and imports. It binds the complete ledger meaning and schema cookie,
   invalidates source/authority/directory and batch-proof clocks, and explicitly
   rolls back ignored clock failures. Register it atomically through A's reviewed
   schema/hook boundary before publishing any receipt. The older unapplied
   `ScopeInboxInvalidationProposal`/`ScopeInboxEpochGuardProposal` remain isolated
   ledger test alternatives; do not install both trigger sets. A bulk
   replacement invalidates and enqueues a durable keyset rebuild;
   do not turn the point helper into an unbounded loop or certify a partially
   captured generation. Bind all external PM authority to invalidation as well.
3. The integer `FeedKey` cannot preserve existing inbox order. Use the bounded
   `ScopeInboxSortKey`: category rank ASC, stored trigger text DESC, canonical
   ID ASC, all using the existing BINARY comparator. This preserves equal and
   fractional timestamps without parsing/rounding. The <=770-byte key contains
   private storage IDs and must remain a BLOB/internal value. The empty
   `ScopeInboxOrderSchemaProposal` and exact seek templates isolate the v2 shape
   for tests. Prefer replacing/versioning the inbox feed comparator when
   integrating; do not retain two permanent copies of the inbox feed.
4. Adapt the authenticated, transaction-bound repository to
   `readmodel.OrderedReader`. Admit tuples using the entire `(order_key,rid)`
   range before LIMIT; never fetch an integer-key page and then sort/filter it.
   A's `ReadBatchFeed` now supplies proof-bound batched authority/candidates;
   extend its integer comparator to this BLOB shape under A's ownership. Bind
   trusted ordered cursor decoding to `Snapshot.Binding` and retain directory
   coverage. Its persisted selection proof must cover complete old-policy
   audience/lifecycle/payload/counters and entire-generation RID disjointness;
   the shadow/identity revision alone misses canonical ask/answer and external
   PM changes. Supply actual same-transaction old/new sources and publication
   authority to A's comparator; no production proof writer exists yet.
   Hydrate once using `OrderedHydrationProposal`, including the private canonical
   key/version join. Validate every ordinal, tuple, payload, nullable join and
   byte bound, then call `DecodeScopeInbox`. Keep capability expiry and all
   errors sticky. The SQL builder is an unapplied template, not a capability.
5. `ReadOrdered` uses one encrypted global comparator key with separate AAD,
   authenticated selection/epoch/generation/stream/directory binding, and a
   <=2 KiB token independent of fanout. Disjoint resource identities and uniform
   old-policy visibility must be certified before paging AND counting. Duplicate
   admission rejects corruption; it does not replace the disjointness proof.
6. D's `Runner.LifecycleStep` owns transaction/lease/deadline. B's durable bridge
   must retain that transaction and the scope fence. The candidate bridge in
   `worker_integration_test.go` demonstrates <=64 examined candidates, restart,
   checkpoint error and lease-loss rollback before/after B's work, and refusal
   to activate without independently supplied current receipts. The production
   source/ancestry loader and reviewed schema registration remain A/D work.
7. Preserve the existing bounded notification-target and access-request
   enrichment. The HTTP hydration fixture uses real actor/agent target lookups; a separate
   fixture checks canonical access-request metadata and target revocation. Neither
   test admits enrichment into the closed production authority boundary. Lifecycle exclusions
   must be captured before candidate admission, including the independent-ask
   exception. Do not carry the legacy handler's post-page lifecycle filter into
   the new bridge.

### Evidence and remaining gates

The mounted authenticated `/inbox` probe covers owner, stranger and agent with
private/public boards and wrong-audience domination. It proves captured payload
equivalence, explicitly rejects the integer/RID comparator as an enablement
proof, and exercises BLOB seeks + one exact batched RID hydration per page in a
test-only pinned SQL adapter. The adapter uses synthetic fixture authority and
receipts; this is not production dispatcher isolation or uniform-scope proof.
The 65-canonical-source mounted HTTP oracle exercises page order, summary
counts and dirty/error freshness for sources with no inbox rows. It demonstrates
that the old selected-only snapshot can omit a canonical source while claiming
no more scopes. Missing proof/directory, >64 selection, mixed transition, revoked
membership and an out-of-selection canonical mutation all refuse the repository
batch callback. The mounted legacy route retains the whole result. This is not
a test of a production mixed-selection fallback dispatcher: none exists yet.

The canonical point mutation probe covers real `derived_inbox_items`, the
published canonical hook, source/registry/feed/payload/counter commit and full
rollback after projection writes. The durable worker probe examines 10,000
archived descendants without selecting them, including a database reopen.

The BLOB continuation plan is an indexed SEARCH over
`(scope,generation,family,audience,order_key,rid)` before LIMIT. Candidate work is
O(S log N + S(P+1)), heap merge O(S + P log S), hydration one VALUES batch of
<=P exact identity/payload joins. Payload bytes are <=P\*16 KiB; cursors <=2 KiB.
The older per-stream adapter costs (643 SQL/27,365 rows; legacy-prepared sparse
request 645 SQL/1,510 rows) fail 500 ms /100 SQL/1,024 rows. A's new batch
repository subtotal is 7 SQL/527 rows. Its SCA-661 preparation probes still fail:
1x cold/revoked 1.128s/0.962s, 10x 47.914s/45.025s, dominated by legacy denial
construction. See `docs/design/scope-batch-evidence.md`; those probes omit full
HTTP/count/fallback acceptance. The coordinator has stopped graph-on-request
bridge certification: certify legacy equivalence off-request, then serve certified
inbox generations under bounded scope authority. BLOB batching, disjointness,
notification/freshness enrichment and all HTTP overhead must pass together
without hidden legacy graph construction. Measure missing/stale-proof fallback
rate and complete latency separately; see the revised phase-two plan.

A's dispatcher isolation, trusted provenance and hook-SQL allowlisting, complete
capture/receipt/epoch coverage, C's derivation/INFO wiring, D's supervised loader,
uniform old-policy parity, route/storage inventories and full SCA-661 budgets
remain mandatory. A's exact two-file trusted adapter exception is retained, and
the independent repository guard still rejects every live consumer. No scope is enabled,
no schema is installed at startup, and no speed improvement is claimed.

### Canonical invalidation proposal

`primitives/scope_feeds_ledger.go` lists 47 canonical dependencies, including all
legacy ownership/profile sources, answer/read/resolution state, report pins and
revisions, freshness queues/status, auth routing, aliases/ancestry and PM sources.
The tests install the proposed DDL only in isolated real-workspace fixtures.
Actual legacy APIs cover ask/answer/read, bulk replacement (including a valid
legacy payload larger than the certified codec), report review creation, owner
changes, archive and canonical deletion; PM import/change/delete uses its actual
same-database canonical table. Missing/exhausted clocks explicitly roll back
ignored errors and earlier transaction writes. An injected SQLite ABORT confirms that a generic epoch
write error aborts the source statement but can leave earlier writes committable;
A’s sticky transaction error boundary remains required. The passkey regression
covers the legacy auth classification fallback that changes notification targets
without changing the agent row. Existing epoch triggers may advance the clock
further; receipts compare equality rather than delta size.

A must register the complete ledger against the schema in one trusted migration
and review exact trigger SQL/read/write effects. Optional PM tables must be
installed when their producer is enabled, before any proof is minted. Separate
external PM databases/policies require a trusted revision witness and atomic
invalidation protocol; these same-database triggers do not certify them. Epoch
delete/replacement, database restore and DDL changes remain trusted maintenance
boundaries, requiring receipt invalidation rather than epoch reuse.

The standalone proposal adds one primary-key epoch update per changed canonical row; bulk
operations retain their legacy write semantics and invalidate globally, without
materializing inbox payloads or enumerating scopes. This conservative proposal
also invalidates on profile and operational changes, so fallback churn and full
latency must be measured after A registers it. No production registration, proof
writer, canonical completeness verifier or reader switch is added by B.

A's combined disabled installer now consumes this ledger and watches 59 tables
with 177 triggers, including scope-directory and graph-index dependencies. It
adds two clock-row updates per watched trigger invocation, with additional
legacy cascade amplification; see the measured write-cost caveats in
`docs/design/scope-phase-two.md`. This integration has no live registration.
