# Transaction and capture integration proposal for A

These are unapplied wiring obligations. The adapters in adjacent patches compile
against the frozen interfaces, but are not permission to relax either import
guard. Install no serving reader or constructor from this proposal.

1. Capture old canonical fields and source version through the request-scoped
   source transaction before writing. Resolve `(scope,kind,opaque resource)` to
   the immutable private RID in that transaction. The canonical source ID never
   enters JSON. Validate the adjacent next version and source authority; do not
   reconstruct old projections from history or accept descriptors from ingress.
2. Select the projector by canonical family in trusted constructor wiring.
   Capture each family's complete bounded payload, sort key and exact buckets
   from source fields. For inbox, include independent ask lifecycle and answer
   state; ancestor archival must not silently dismiss a live ask. Work and
   Overview must preserve the legacy roles, evidence/health and visibility rules.
   Projector closure data must also originate in this source transaction. The
   frozen generic Projection alone does not prove complete family semantics.
3. Write the canonical resource and registry version in that same transaction,
   then call `ApplyCanonicalHooks` with `ReadModelCanonicalHook` before committing.
   Return every semantic and SQL error. The adapter checks current domain state,
   generation, canonical ID, RID and registry version, then applies at most 24
   exact feed/payload/counter statements. It refuses writes while transitioning.
   Review/allowlist each named kernel template and the identity query first.
4. Cover every source writer, importer, mutation variant and external PM source
   that affects audience, projection or authorization. Advance the existing
   legacy epoch atomically wherever visibility can change. Passing registry
   checks is necessary but does not prove complete source provenance or coverage.
   Add source-capture and external-PM invalidation negatives before any receipt.
5. Register A's empty feed shadow schema through the shared migration after D's
   version allocation; add the separate job and board-qualified rank schemas
   from B's constants. Do not install DDL during requests. A owns live column
   classifications/ownership bindings; B's inventory changes cover only B files.
6. Parent lifecycle writes call `BeginLifecycle` before committing the parent.
   This reserves a monotonically increasing fence and empty staging partition
   without walking descendants. Fence other same-scope writes; a competing
   lifecycle change must refuse or reserve a fresh fence/job transactionally.
   Never reset/reuse an interrupted generation. Cross-scope inherited parents
   require a separately reviewed workspace fence, not this same-scope adapter.
7. D injects its claimed/leased transaction and complete indexed family loader
   into `NewDurableLifecycle`, then calls `Step` and commits only success.
   Selection is `(scope_id,rid)>cursor LIMIT 64`, including unavailable/dead
   descendants. Resolve <=8 ancestors by exact same-scope keys. No scan-until-live,
   full-body/history loading, caller cursor or synthetic success checkpoint.
   Lease renewal/expiry, crashes and stale worker recovery remain D-owned.
8. A's verified builder writes four versioned parity receipts for the staging
   generation, bound to the legacy epoch, only after independent full-generation
   audience/lifecycle/payload/counter checks. No kernel API writes receipts.
   Readiness also requires passing full-request costs and canonical/PM coverage.
   `Step` activates only after an empty final seek under the same job/fence and
   current receipt, atomically with job deletion. Neither a completed cursor nor
   a matching epoch alone proves parity. Old generation cleanup is bounded work.
9. Repositories may adopt the batch SQL builders only after reviewing the exact
   bounded shapes. Validate every ordinal, NULL/missing authority row, role,
   membership/binding generation and receipt in the same transaction. Missing
   authority denies the whole request before candidates. Preserve the ordered
   binding digest and capability expiry/sticky failures. Candidates return
   global P+1 with stream ordinals; hydrate only admitted final P identities.
   Separately certify disjoint identities across selected streams. The negative
   batch test demonstrates repeated identities without that prerequisite.
10. Measure directory selection, legacy denial preparation, authority, candidates,
    counters and hydration together through real HTTP and SCA-661 before selecting
    a reader. The current complete dependency request fails SQL/rows. The batch
    subtotal excludes legacy and directory preparation; it supplies no receipt.
    Preserve the existing 500 ms / 100 SQL / 1,024 rows gates and all harness pins.

The initial release uses SCA-665's legacy readers. The existing handler ownership
boundary remains in force until its PR merges. A integrates the shared changes;
B has not edited A's repository, constructors, migrations or authorization policy.
