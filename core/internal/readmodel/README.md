# Bounded read-model kernel

Stream B remains disabled and unwired. A's `c86cf596` feed dependency is integrated;
`Store.ReadFeed` is exercised only through test adapters. Both production import
guards remain unchanged. No HTTP routes, serving constructors or migrations are
registered, and existing authorization remains authoritative.

`Read` seeks at most P+1 keys per exact authorized stream, rejects duplicate RIDs
throughout admission (including lookahead), heap-merges, and hydrates at most P
items in one batch. Limits remain 64 scopes, four streams per scope, 256 streams
and 100 items. Resource identities must already be disjoint across the selected
streams before paging and counting; admission checks cannot prove whole-generation
disjointness. AES-GCM continuations bind ordered authority, generations, streams
and directory coverage. Integer allocation keys stay internal.

`Count` reads up to four exact sparse buckets. Any unavailable or uncertified
scope makes the whole selected page/count unavailable. Values are absent rather
than synthetic zero; shared coverage identifies unavailable slots and preserves
`more_scopes`, the directory continuation and `as_of`. The test adapter overlays
only a matching authorized directory page. A's explicit selection alone never
asserts that the PM directory is complete. Readiness receipts from the dependency
are an integration prerequisite, not evidence of legacy parity or a passing
request budget.

## Durable writer and lifecycle

`CaptureCanonical` consumes the frozen adjacent-version mutation and private RID;
A supplies the trusted bounded family projector. `ApplyProjection` updates exact
feed keys, audience/version-bound payloads and counters in the existing source
transaction. It validates the complete fanout and payload bytes before writing,
checks one affected row for every statement, and refuses stale deletes, missing
negative buckets, underflow and integer overflow. Callers must return every error
to the enclosing hook/worker; an affected-row error is semantic and is not itself
a sticky SQL error in A's proxy. No commit/rollback or database factory is exposed.
There are at most 16 feed/counter statements plus eight payload statements.

`BeginLifecycle` fences a scope and enqueues its durable job in two statements,
in the same transaction as the parent mutation. Active generation G reserves
fence G+1 and staging generation G+2; values are never reused. Existing source
mutations are refused during transition. `DurableLifecycle` reads its persisted
job, consumes D's exact indexed metadata loader in chunks of at most 64 records,
validates payloads before writing, and applies staging changes plus a fenced
checkpoint atomically. Every candidate consumes a slot, including archived-parent
descendants. Ancestors are same-scope and bounded to depth eight.

Only an empty final keyset seek may activate. Activation checks the job, current
scope fence, four parity certificate versions and the current legacy epoch, then
selects the staging generation and deletes the job in the same transaction. It
never creates certificates. D still owns durable worker claims/leases, complete
canonical-family enumeration and cleanup. A owns parent capture and epoch
invalidation. An unverified generation remains unavailable.

`RankNeighbor` uses the exact `(scope,generation,board,column)` prefix and one
indexed predecessor/successor seek. `RankBetween` refuses exhausted gaps without
request-time rebalance. Board identity prevents shared column keys from mixing
neighbors across boards.

## Repository integration and remaining proposals

A integrated the first two patches as `scopedrepo.AdaptReadModel` and
`scopedrepo.ReadModelCanonicalHook`, with immutable snapshot checks and exact
delta-SQL admission. The patch files remain the original proposal; use the Go
implementation for integration. Both adapters are still unreachable from live
consumers under `TestFoundationNotServing`. The inbox-first ownership and proof
plan is in `docs/design/scope-phase-two.md`.

- `proposals/trusted-feed-adapter.patch`: transaction-local Reader/CounterReader
  translation, with explicit directory coverage.
- `proposals/scopedrepo-mutation-hook.patch`: the frozen CanonicalHook adapter,
  exact private-registry/version/fence checks, and direct error propagation.
  This replaces the earlier raw-transaction hook sketch.
- `proposals/CAPTURE.md`: source/capture, schema, worker and certification wiring
  obligations. No shared constructors or migration files are edited by B.
- `batch_proposal.go`: bounded authority/binding VALUES batches, exact-prefix
  candidate seeks with a bounded global merge, and grouped sparse counters.
  These return SQL only; no repository executes them in production. The global
  P+1 candidate proposal is not equivalent to per-stream duplicate admission:
  separately certified disjoint identities are mandatory before adoption.

## Cost and evidence

| Path                 | Complexity / index                                                                                                |
| -------------------- | ----------------------------------------------------------------------------------------------------------------- |
| Current feed keys    | O(S log N + SP), at most S(P+1) tuples; `scope_feed` exact generation/family/audience prefix                      |
| Heap/hydration       | O(S + P log S); one final P-row exact registry/payload batch, <=16 KiB per payload                                |
| Exact counters       | O(SQ log N), Q<=4; `scope_counters` primary key                                                                   |
| Projection mutation  | <=24 exact statements; feed resource uniqueness and audience/version payload primary keys                         |
| Lifecycle initiation | Two exact statements plus canonical parent write; domain/job primary keys                                         |
| Lifecycle slice      | <=64 candidates and <=8 indexed same-scope ancestors; staging feed keys and fenced job checkpoint                 |
| Rank neighbors       | Two O(log N) seeks; `(scope,generation,board,column,rank,rid)` primary key                                        |
| Batch SQL proposal   | <=6 statements/526 returned rows before directory and legacy preparation; bounded S(P+1) internal candidate merge |

Instrumented SQLite tests measure the published maximum request at **643 SQL and
27,365 returned rows**. A sparse disabled dispatcher probe including actual legacy
denial preparation uses **645 SQL and 1,510 rows**. Both fail the unchanged serving
gate (500 ms / 100 SQL / 1,024 rows). The batch SQL shapes use five statements and
426 rows before hydration, directory and legacy preparation. These subtotals are
not production HTTP acceptance or a speed claim; no allowances or pins change.

Tests cover real repository paging/counts, epoch-invalidated continuations,
directory coverage, duplicate lookahead, 64,000 wrong-audience rows, actual SQL
plans, source/feed/payload/counter rollback through CanonicalHook, exact-zero
faults, private RID mismatches, transitioning-source refusal, 10,002 durable
metadata rows in <=64 chunks, restart, checkpoint/activation faults, missing/stale
receipts and board-local rank gaps. Batch tests check values, keysets, ordering,
indexed seeks and the adversarial distant-duplicate prerequisite.

Production HTTP/worker acceptance remains gated on A's complete canonical source
capture, external-PM epoch invalidation, independently verified audience/lifecycle/
projection parity, reviewed receipt writing and full SCA-661 request budgets.
SCA-665 must merge before B edits its existing handlers. Readers stay disabled.
