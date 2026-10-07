# Inbox-first bridge implementation

Phase one shipped in v0.12.18. The next deliverable is a production inbox
reader whose complete request passes the existing privacy and performance
gates. Overview follows only after inbox establishes the capture and proof
mechanism. Search and SSE retain legacy authority until phase three.

## Review units and owners

1. **PR #303 — inbox integration, A and B.** B owns `readmodel/`, new
   `primitives/scope_inbox_projection.go`, and
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
2. **Scope bridge certification and rebuild PR — A and D, stacked after the
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
4. **Overview bridge PR — B with A integration.** Reuse the accepted inbox
   machinery, port the shipped SCA-665 tests and preserve exact counts,
   asynchronous health freshness and explicit scope coverage. No new global
   aggregates, approximate totals or independent authorization cache.

These are ordered review units, not permission to ship incomplete readers.
Do not promise a hosted enablement time before the full-request evidence.
The immediate checkpoint is runnable inbox capture/parity and an actual bounded
repository, not another interface-only handoff.

## Proof and request contract

The existing policy remains authoritative. A container label does not establish
uniform visibility. A bridge proof must cover audience, lifecycle, projected
payload, counters and disjointness of selected streams for the **entire** staged
generation. Bind it to policy/projector versions, source/legacy authority epoch,
scope generation, membership and audience bindings. Mint proofs only in trusted
code after comparison with legacy policy. Request code cannot write ready flags.

Background verification may enumerate a generation in bounded resumable chunks;
serving may not. Concurrent canonical or external-PM changes invalidate the
build or its proof atomically. A stale proof selects the old reader for the
whole affected response; never merge uncertified totals or filter a page after
LIMIT. Measure this fallback as well as the certified path. If the retained
denial construction fails the gate, inbox enablement stays blocked: a fast SQL
subtotal does not justify changing the budget.

The complete measurement includes directory selection, legacy authorization
work, authority/binding admission, candidate seeks, hydration, exact counters
and serialization. Use personal-size and 10x fixtures, cold/warm/revoked cases,
64 scopes, role plus personal audiences and mostly invisible/archived rows.
Keep 500 ms, 100 SQL and 1,024 returned rows; also assert examined-candidate
bounds and indexed query plans. The current per-stream API's 643 statements and
27,365 returned rows fails. A must replace this scheduling with reviewed batched
authority, bindings and counter probes and global page admission only after
cross-page identity disjointness is certified. No allowance or pin change is
part of this work.

## Remaining security gates

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
  not reasons to enable those transports with the inbox bridge.
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
B must supply capture/publication-authority proposals; A and D must bind complete
old-policy audience/lifecycle/payload/counter comparison and global disjointness
to that snapshot before minting. The unchanged no-serving guard remains the
production barrier. HTTP selection, exact full-request budgets, dispatcher and
hook sealing remain prerequisites; the batch subtotal does not satisfy them.
