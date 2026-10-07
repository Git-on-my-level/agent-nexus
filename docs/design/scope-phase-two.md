# Inbox scope-authority cutover

Phase one shipped in v0.12.18. The next deliverable is a production inbox
reader whose complete request passes the existing privacy and performance
gates. Overview follows only after inbox establishes the capture and proof
mechanism. Search and SSE retain legacy authority until phase three.

## Decision from the cold/revoked evidence

Stop full-request certification of the graph-on-request bridge. The SCA-671
probe measured 1x cold/revoked preparation at 1.128/0.962 seconds and 10x at
47.914/45.025 seconds; legacy denial construction dominates. These are diagnostic
single samples, not HTTP p95 measurements. They establish that this implementation
cannot satisfy the required cold/revoked gate. They do not establish that every
way of preserving legacy semantics requires a graph on each request.

Instead, move inbox serving authority directly to scopes for certified generations.
Legacy policy is the **off-request comparison oracle**, not a prerequisite call
on a certified request. Persist the proof across restarts. Uncertified or stale
scopes retain the legacy reader. Do not enable Overview, search or SSE as part of
this narrow cutover. The evidence and reproducible probe remain in
[scope-batch-evidence.md](scope-batch-evidence.md).

## Review units and owners

1. **PR #303 — inbox integration, A and B.** B owns `readmodel/`, new
   `primitives/scope_feeds*.go`, and
   `server/scope_readmodels_inbox_test.go`. B supplies the typed projection of
   canonical ask/answer state and executable HTTP parity fixtures. A owns
   `scopedrepo/`, migrations, constructors, route/auth integration and the exact
   trusted-adapter exception in the kernel boundary test. A integrates the two
   `proposal for A` patches here, preserves the existing source transaction,
   and admits exact mutation templates. The PR remains disabled until the
   source capture, proof and request gates below pass; a reviewed disabled
   checkpoint may land independently. Share the branch without force pushes.
   SCA-665's follow-up #308 owns the existing complete inbox-stream fix; B must
   preserve it and coordinate the minimal handler switch after it lands.
2. **Inbox proof and rebuild PR — A and D, stacked after the
   source schema in #303 is fixed.** D owns indexed source enumeration,
   lease/fence-bound rebuild chunks, checkpoints, interruption and shutdown.
   A owns placement/authority validation, the proof writer and serving selector.
   An injected worker is insufficient evidence: use actual canonical inbox
   rows, including missing, denied and archived-parent cases. Keep startup
   metadata-only. This PR supplies shadow comparison and proof invalidation;
   it cannot select a reader merely because a cursor completed.
3. **Inbox enablement PR — A, B and SCA-661 owner.** A owns dispatcher and
   authority selection, B owns the existing inbox route integration, and the
   SCA-661 owner validates the harness and full-request evidence. Gate selection
   on a versioned proof in the same database snapshot, with an immediate
   fail-closed legacy fallback on absent/stale proof. The coordinator reviews,
   merges, releases and verifies hosted behavior before wider enablement.
4. **Overview scope-reader PR — B with A integration.** Reuse the accepted inbox
   machinery, port the shipped SCA-665 tests and preserve exact counts,
   asynchronous health freshness and explicit scope coverage. No new global
   aggregates, approximate totals or independent authorization cache.

The next #303 changes are concrete and parallel:

- **A:** extend `scopedrepo.ReadBatchFeed` to B's BLOB comparator/private RID
  hydration; add the inbox-only dispatcher, persisted-proof selector and exact
  registered hook read/write allowlist. A owns shared storage/auth wiring.
- **B:** finish the canonical mutation ledger and old/new capture or invalidation
  for bulk replacement, answers/decisions/read state, report reviews, lifecycle,
  imports and PM sources in its owned primitive files; propose shared-file hooks
  to A. Extend mounted HTTP tests to real target/freshness/count enrichment,
  stale/revoked proofs and mixed-selection fallback. Do not spend another run
  trying to certify per-request legacy graph preparation or change smoke scripts.
- **A/D:** implement bounded canonical enumeration and the production proof
  writer, then verify complete generations against legacy policy off-request.
  D owns worker/lease/checkpoint code; A owns authority and invalidation coverage.
  Use 1x/10x interruption and concurrent-authority-change fixtures. The synthetic
  proof writer in tests is not an implementation of this verifier.

These are ordered review units, not permission to ship incomplete readers.
Do not promise a hosted enablement time before the full-request evidence.
The immediate checkpoint is runnable inbox capture/parity and an actual bounded
repository, not another interface-only handoff.

## Proof and request contract

The existing policy remains the semantic baseline; the certified inbox repository
becomes the serving authority. A container label does not establish uniform
visibility. A proof covers audience, lifecycle, every projected/enriched field,
exact counters, ordering and disjointness for the **entire** staged generation.
Bind it to policy/projector versions, canonical source and legacy authority epochs,
scope generation, membership and audience bindings. Only the trusted verifier may
mint it after complete comparison with legacy policy. Request code cannot write
ready flags or substitute a matching shadow revision for canonical provenance.

A proof over selected streams alone is insufficient: the trusted verifier also
records canonical coverage/completeness for inbox scope and audience discovery.
Bind this receipt to the source/authority epochs and directory revision so every
eligible canonical inbox source maps to a discovered stream or an explicit
uncertified partition. Entirely missing scopes/bindings must trigger legacy
fallback, not disappear before proof lookup. Creation, relocation, deletion and
membership changes invalidate this coverage atomically. Test newly eligible
sources/scopes, missing directory entries and selections exceeding 64 scopes.
Directory keyset continuation and explicit coverage must preserve completeness
across requests; totals describe only covered scopes. If the existing HTTP
contract cannot express that distinction, update canonical contracts and clients
before enabling it; never silently truncate a legacy complete-inbox response.

In one read snapshot, the dispatcher authenticates the principal, selects at most
64 scopes/256 streams, and validates current grants, audiences and proof bindings
with indexed queries before candidates, hydration and counts. The certified path
must never instantiate the legacy denial graph, including through notification,
access-request, label or freshness enrichment. Enrichment must be covered by the
proof or a bounded independently authorized guarded lookup; otherwise that scope
is uncertified. The existing BLOB comparator and private RID/version joins remain
mandatory. Continuations authenticate the exact selection/proof/version snapshot;
a changed binding invalidates continuation rather than silently mixing versions.

Background verification enumerates a generation in bounded resumable chunks.
Every canonical or external-PM change that can affect an inbox result either
updates its certified projection and proof atomically through reviewed code or
invalidates the affected proof **before the source transaction commits**. This
includes changes to ancestry, lifecycle, owners, membership/roles, mentions,
references and aliases outside inbox tables. Initially, a global legacy authority
epoch may conservatively invalidate all inbox proofs. Selective invalidation is
an optimization requiring demonstrated complete dependency capture. Unsupported
projection size or bulk mutation invalidates/enqueues a rebuild; it must not
silently skip capture or introduce a new refusal of a previously valid legacy
write. Failure to persist invalidation rolls back the canonical write.

Missing/stale proof is the only availability fallback to legacy serving; authority
revocation still denies the scope, and repository corruption/errors fail closed.
Use legacy results only for uncertified scopes. A mixed selection may combine
certified and fallback partitions only after proving identical ordering, identity
disjointness, exact counts and a continuation for both partitions. Until that
merge is implemented and tested, mixed selections stay on the legacy response
as a whole (triggered by the uncertified scope), without claiming fast-path
coverage. Never merge uncertified totals or filter a new-model page after LIMIT.

Record internal request/scope fallback rates, reasons, rebuild age/duration and
latency, including cold opens and authority churn; do not expose hidden activity
in public diagnostics. Report end-to-end latency including fallbacks separately
from certified-path latency, with coverage and sample counts. A slow fallback is
not a passing bounded request. Rollout acceptance must review its observed rate
and complete latency distribution; no fallback allowance is invented here.
Low fallback frequency cannot turn a failed required full-request case into a
pass. Retained legacy fallback cost is reported honestly; this does not restart
certification of the rejected graph-on-request bridge.

The certified complete request (authentication, directory, proof admission,
candidates, hydration, enrichment, counts and serialization) must pass 500 ms,
100 SQL and 1,024 returned rows, plus examined-candidate and indexed-plan checks.
Use 1x/10x fixtures, cold/warm/revoked cases, 64 scopes, role/personal audiences
and mostly invisible/archived rows. Revocation tests must prove no stale proof
can serve; separately measure the resulting fallback and recertification.
The batch subtotal is 7 SQL/527 rows, replacing the failing per-stream
643 SQL/27,365 rows; neither subtotal is HTTP acceptance. No budget or pin changes.

## Minimum semantic boundary for inbox only

This cutover changes how existing inbox answers are authorized, not what legacy
canonical writes mean. Broad publication UI, new guarded-reference editing and
the full-repository analyzer need not launch together with this reader, provided:

- All canonical mutation paths preserve today's semantics and atomically capture
  or invalidate proof, including imports, bulk regeneration and external PM rows.
  No new scope-changing or private-to-public write capability is exposed.
- The inbox projector/dispatcher has a closed, mechanically enforced computation
  boundary: no business-code raw handles, capability factories or cross-scope
  callbacks. Capture originates in real canonical rows in their transaction.
  Direct and implicit private-to-public derivations fail mechanically. A small
  inbox-slice analyzer or equivalent closed typed boundary is mandatory; scope
  tags and principal read/write authority are insufficient.
- Returned titles, references, previews, summaries and counts are proven under
  legacy visibility or resolved through bounded guarded authority. No new raw
  cross-private reference ingress, silent prose rewriting or wider audience is
  introduced. Explicit publication, if later added, requires source owner/admin
  plus destination write authority and its separately reviewed declassification.
- Real mounted route-matrix and differential tests cover all principals, every
  response field, stale/missing proofs, concurrent revocation and fallback.
  Existing privacy tests remain green. No protected route may reach the new
  capability transitively except the reviewed inbox dispatcher.

If any of these boundaries cannot be closed for inbox, that generation cannot
cut over; enabling only the new SQL without them is unsafe. General publication,
guarded-reference semantics and analyzer coverage remain prerequisites for the
later full semantic cutover. Search/SSE retain all #306 wiring gates.

## Remaining security gates

- Restored database clones share the namespace identity of the original; define a clone identity policy before enabling any reader.
- **Dispatcher isolation:** business computations cannot obtain factories,
  raw handles or capabilities for a second scope. Enforce transitive imports
  and call paths, including implicit-flow derivation negatives and route matrix.
- **Provenance:** capture old/new values from actual canonical rows inside the
  source transaction; adjacent versions and matching scope tags alone are not
  information provenance. Cover ask creation, answer, reopen/dismiss, deletion,
  assignment, priority, recipients, roles, imports and external-PM changes.
  Source-private values must fail mechanical cross-scope persistence without
  explicit owner/admin publication plus destination write access.
- **Hook SQL:** the integrated read-model executor admits only six exact delta
  templates. The generic `CanonicalHook`/`MutationTx` API is still a trusted
  adapter/testing boundary, not a sealed production capability. Before live
  registration, admit only reviewed hook implementations and their exact read
  and write templates; prohibit transaction control, compound SQL and arbitrary
  callback SQL, even when errors are ignored.
- **#306 wiring gates:** enforce persisted 4,096-posting and same-scope-parent
  bounds, immutable ownership and trusted canonical provenance; resolve
  overlapping audience destinations before admission and preserve per-item
  reconnect cursors. These remain prerequisites for phase-three search/SSE,
  not reasons to enable those transports with the inbox cutover.
- **Acceptance:** keep #288/#294/#298 privacy and inventories, direct and
  implicit derivation negatives, mutation rollback, real worker interruption,
  parity and complete SCA-661 evidence. Every PR needs adversarial review and
  exact-head ci-ok, then independent coordinator review. No hosted cutover is
  authorized by a unit test or by a disabled PR's merge.

## Adapter checkpoint in #303

`scopedrepo.AdaptReadModel` wraps a transaction-bound `FeedReader` without giving
the kernel SQL or a factory. It copies directory inputs, requires matching IDs
and availability, and freezes the observed binding/generations. Directory input
is trusted dispatcher data; this adapter does not certify its provenance.
`ReadModelCanonicalHook` checks canonical identity/version/RID in the source
transaction before applying feed, payload and exact counter deltas. Capture
descriptors are detached and SQL admission is exact. Neither adapter is wired
into a live constructor or handler.

The kernel boundary admits exactly these two repository files. The independent
repository import guard still forbids **every** live consumer of `scopedrepo`,
so this dependency edge cannot reach serving through a transitive import.

## Batch selection checkpoint

A's `scopedrepo.ReadBatchFeed` now consumes a persisted full-selection proof before
exposing a batch capability: exact batched authority/audiences, one global P+1
candidate query, existing exact RID/version hydration and grouped counters.
B can target `BatchFeedReader` after adding trusted cursor adaptation; the existing
per-stream `AdaptReadModel` API is preserved. See `scopedrepo/FEEDS.md` for the
seven-query repository bound and proof contract. Neither generation flags nor
caller input can substitute for the proof lookup.

The proof table has no production writer. Its revision clock covers shadow data
and identity changes, not yet B's actual canonical ask/answer source snapshot.
B must supply complete source capture/invalidation proposals; A and D must bind complete
old-policy audience/lifecycle/payload/counter comparison and global disjointness
to that snapshot before minting. The unchanged no-serving guard remains the
production barrier. HTTP selection, exact full-request budgets, dispatcher and
hook sealing remain prerequisites; the batch subtotal does not satisfy them.

## Disabled inbox integration checkpoint

The ordered repository now consumes the canonical inbox BLOB comparator directly.
`scope_inbox_order` is the inbox comparator; it is not a second permanent inbox
copy beside an integer feed. No inbox writer or serving schema is registered yet.
The repository validates private RID, canonical key and version in one hydration
batch. Its ordered adapter validates the full typed inbox envelope, then detaches
only the original item JSON so unknown fields and exact numeric bytes survive.
The cursor binds the current proof/source snapshot and encrypts the private key.

`InboxDispatcher.Read` is a fixed render computation: principal, page size and
continuation are its only request inputs. It accepts no caller-selected scope,
audience, SQL, projector or callback. It discovers the own directory and current
inbox bindings in the same SQLite transaction as admission, candidates,
hydration and counters. Directory discovery reads at most 65 membership rows;
each of at most 64 current-generation inbox binding branches stops at five rows.
Exceeding the new repository's 64-scope/256-stream bound sends the
whole response through legacy rather than silently truncating its counts.

Batch selection proof is necessary but insufficient. The dispatcher additionally
requires `scope_inbox_serving_receipts`, bound to the same snapshot, compiled
registry hash, canonical/authority/directory revisions, schema cookie and
independently recorded directory coverage. Discovery, enrichment and derivation
versions must all match the reviewed implementation. There is **no production
writer for this receipt**. A base-row comparison cannot create it. Missing/stale
proof reports an internal whole-response fallback; denial, corruption, invalid
continuation and SQL errors return no data. Internal request/scope fallback
counters have no principal labels and are not HTTP response fields. Their latency
covers the dispatcher only; future route wiring must measure legacy fallback and
the entire request, including authentication and enrichment.

`InstallScopeInboxInvalidation` derives 59 watched tables (177 triggers) from the
canonical producer ledger and executable ownership/filter/publication registries plus inbox, authority, enrichment and
scope-directory dependencies. All INSERT/UPDATE/DELETE operations conservatively
advance source, authority and directory revisions and the selection proof clock,
and clear directory coverage. Installation validates actual columns and binds a
compiled registry digest and SQLite schema cookie. Late PM schema installation is
explicitly incomplete until reconciliation occurs in its table-creation
transaction. Dropping/changing a trigger or table invalidates admission. This is
unregistered maintenance DDL: it must run after all shadow/verifier tables are
installed and cannot be selected by a startup constructor yet.

The ledger includes report pins, passkey principal-kind fallback and host-agent
notification bindings outside the ownership inventory. The compiled digest pins
both table coverage and dependency meaning. The earlier ledger-only epoch trigger
proposal remains a test alternative, not a second trigger set to install alongside
the combined source-clock installer. Mounted legacy HTTP fixtures cover current
access-request/notification enrichment and omitted-directory, counter, ordering,
duplicate-audience and over-limit refusal cases. They do not switch the route to
the dispatcher or establish complete enriched-generation proof.

The conservative invalidation cost is **two clock-row updates per watched row
trigger invocation**, multiplied by existing legacy index cascades. Actual
fixtures produced 70 clock advances for an owner mutation, 46 for a mention
mutation and 18 for a PM body mutation. These are diagnostic observations, not a
constant bound on an entire canonical write. No new corpus scan is introduced by
the invalidation hook, but existing graph writes can still expand with workspace
size. Review a less amplified invalidation/rebuild strategy before registering it
on production writes; no write-budget or storage-overhead gate is waived.

The off-request verifier uses current `derived_inbox_items` base rows and the fixed legacy
eligibility relation, not caller-authored payloads or flags. It discovers the own
directory/bindings, compares eligible canonical envelopes/order/private identity,
reverse-enumerates the staged generation to reject extras and RID duplication,
and checks exact total/category counters. Every durable slice enumerates at most
64 rows and fences canonical/authority/directory/shadow/legacy revisions. This is
a **source-row chunk bound**, not a bound on total database work or duration:
legacy policy and lifecycle comparison remain expensive off-request operations.
Cancellation rolls back the checkpoint; reopen resumes it. Source or authority
changes refuse the old job. Completed cursors without exact comparison receipts
cannot establish success. `canonical_base_v1` receipts always have
`serving_eligible=0`, do not mark directory coverage, and do not mint either batch
or serving certificates. A disabled supervisor and bounded scratch cleanup are
implemented below. Production scheduling and complete HTTP enrichment remain
integration work.

Here “base rows” means the existing `derived_inbox_items` relation. Complete
enumeration of that relation does not prove every event/ask/answer source was
projected into it, external-PM source completeness, or mounted route
enrichment/freshness. All-event source coverage and actual directory coverage
must be established separately before a serving receipt can be minted.

Registered hook execution rejects unknown concrete implementations before any
callback and admits only the exact reviewed identity query and six delta writes.
Transaction control, compound/altered SQL and arbitrary queries poison the proxy;
ignored failures roll back the source transaction. The older generic hook API is
still a disabled test adapter. SQL sealing does not establish provenance for its
trusted Capture callback. Live hook registration still needs actual canonical
capture and direct/implicit derivation negatives. The kernel import gate now uses
a closed pure-library/type allowlist so a new helper package cannot acquire raw
authority transitively. The independent foundation gate still rejects **every**
live repository consumer; no HTTP handler or migration exception was added.

Synthetic maximum-selection evidence covers 64 scopes, 256 audiences, P=100 and
10,000 irrelevant bindings: the ordered repository returns 527 rows in seven SQL
reads; the fixed dispatcher returns 848 rows in ten SQL reads. Directory branches
seek exact principal/scope/current-generation/family prefixes; ordered branches
seek the full BLOB/RID range before LIMIT and examine at most S(P+1) candidates.
These remain repository subtotals with fixture-injected certificates, **not**
full-request SCA-661 acceptance, production proof minting or hosted enablement.
The real route/field matrix, all-event canonical capture/rebuild, actual PM hook
registration, whole enriched-generation proof, production worker registration and
whole-work budget evidence, and cold/warm/revoked HTTP budgets remain required.

## Comparison supervision and scratch lifetime

The disabled `StartInboxVerificationWorker` supervises actual base-row comparison
with a unique attempt owner, integral monotonic database lease token and SQLite
lease time. Renewal also advances the token, so an old handle cannot regain
authority after takeover or renewal. The opaque job is its only work selection;
callers cannot supply rows, checkpoints, certificates, success flags or callbacks.
The foreground slice convenience API uses the same private runner and lease.
The default-disabled constructor performs no database work and starts no worker.
An enabled owner must cancel and join `Close` before closing the database.

The source oracle runs in a **read-only, deferred SQLite snapshot**, including
when the workspace driver uses `_txlock=immediate` for writes. Expensive legacy
graph preparation therefore does not acquire the SQLite writer lock. Its bounded
batch is private to the runner. After closing that snapshot, a short write
transaction checks every source/authority/directory/shadow/legacy epoch, the
compiled registry and exact job checkpoint before using the prepared result.
Any intervening affecting write refuses comparison. Lease checks occur before
work and after all checkpoint/receipt writes, with expiry or commit failure
rolling the entire slice back. A savepoint removes partial comparison rows on
semantic refusal; only the terminal refusal is recorded. Generic SQL errors and
deadlines leave the checkpoint resumable.

The worker's configurable deadline is at most 50 seconds, below its at-most-one-
minute lease. These are off-request maintenance limits, not the 50 ms metadata
migration chunk allowance or the 500 ms HTTP gate. The legacy SQL can still do
workspace-sized work, consume CPU and retain a read snapshot/WAL while preparing;
the enumeration cap does not certify whole-work cost. Production scheduling,
resource contention and rebuild churn still need full-fixture evidence. The
pinned worker connection disables busy waits only for its transaction and
restores the prior timeout before returning to serving; failed restoration
discards the connection. No retry alters a privacy or serving proof gate.

`CancelInboxVerification` explicitly retires unfinished jobs. Completed/refused
jobs record a terminal timestamp. `PruneInboxVerification` seeks one old terminal
job through its retention index and deletes at most 64 scratch/receipt/job rows
per transaction with job-prefix lookups. It does not cascade over a generation,
delete canonical or shadow rows, or scan for another candidate past a live lease.
Unfinished crash checkpoints remain available for resume until explicitly
cancelled. Partially pruned jobs reject old supervisors, and stale release cannot
clear a successor's lease. Retention scheduling remains unregistered maintenance.

Executable evidence includes 16/160-base-row supervisor completion, concurrent
and repeated shutdown, cancellation during active SQLite work, actual database
close/reopen with an abandoned persisted lease, monotonic takeover/renewal and
overflow refusal, late expiry after both checkpoints and receipt insertion,
partial-mismatch rollback, deadline interruption and busy-timeout restoration.
A driver-observed oracle fixture on the actual immediate-lock DSN lets a source
write commit while comparison is preparing, then requires stale-epoch refusal
and no receipt. Multi-chunk cleanup preserves active jobs and canonical/shadow
data. These are maintenance regressions; they do not establish canonical event
projection completeness, enriched serving proof or full-request acceptance.
