# Scope implementation: ownership and release order

The direction and product decisions are accepted. This is the dispatch contract
for implementation, not evidence that the new reader is ready to deploy. The
coordinator dispatches the delegable streams below and owns independent review,
merge, release and hosted verification. The technical lead integrates them.

Use a few stacked PRs targeting `main`, with a lead-owned integration branch
`integration/sca-666-scopes` for combined tests. Each stream branches from the
published foundation commit; no two agents edit the shared files listed below.
Never merge a feature merely because the integration branch passes. Each release
must be safe with its default feature flags and have its own green `ci-ok`.

## Phases that can ship independently

1. **Foundation and readiness:** add empty scope/read-model tables, typed access
   capabilities, reviewed query templates and capture hooks, with the old reader
   authoritative. Remove automatic legacy blob backfill before listen; unknown
   manifests remain denied, and missing historical content is unavailable with
   no retry worker. No production reader uses a partly backfilled table.
2. **Read models first, when proven equivalent:** build inbox/work/Overview feeds
   and synchronous counter deltas in the background. Enable a scope generation
   only after its audience, lifecycle and projection parity are verified. Keep
   current authorization during this bridge. The verified generation must prove
   uniform legacy visibility within each selected scope before paging and counts;
   no post-LIMIT privacy filter or scan-until-P loop is allowed. A generation
   without that proof stays on the old reader and makes no new cost claim.
   If legacy authorization preparation still exceeds SCA-661 budgets, do not
   claim a speed win or enable that bridge reader; SCA-665 remains the early fix.
3. **Complete semantic cutover:** all canonical writes, guarded references,
   explicit publication, search, SSE and derivation analyzer enforcement must be
   ready together before selecting the new authority. Retire the graph only after
   privacy, query cost, migration interruption and forward-binary recovery gates.

Thin-prototype tests become production regressions in their owning streams. They
must exercise real repositories/HTTP and durable workers, not merely import the
non-merge model. Preserve the old tests until equivalent new guarantees pass.

## Parallel ownership

| Stream                                     | Owner / exact file boundary                                                                                                                                                                                                                                                                                                                                                                                                                             | Deliverable and interface                                                                                                                                                                                                                                                      |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| A: scope authority and integration         | Technical lead: new `core/internal/scopes/`, `core/internal/scopedrepo/`, `core/cmd/scopedrepo-gen/`; shared `contracts/anx-schema.yaml`, `contracts/anx-openapi.yaml`, generated mirrors, `core/internal/storage/migrations.go`, `core/internal/primitives/store.go`, server route registration/auth classifier and both resource-access inventory JSON files                                                                                          | Authoritative scope schema, opaque IDs/scoped aliases, dispatcher/capabilities, owner/admin publication, reviewed template manifest/generator, factory/import analyzer and route/field tests. Lead alone integrates shared-file patches proposed by B–D.                       |
| B: inbox/work/Overview and mutation bounds | Delegable: new `core/internal/readmodel/`; new `core/internal/primitives/scope_feeds*.go`, `scope_ordering*.go`; new `core/internal/server/scope_readmodels*_test.go`. After SCA-665 lands, exclusive edits to `core/internal/primitives/overview.go`, `overview_changes.go`, `human_attention_inbox.go`, `derived_store.go`, `boards_store.go` and server `inbox_handlers.go`, `inbox_summary_handlers.go`, `work_handlers.go`, `overview_handlers.go` | Indexed feeds and exact counters; answers atomic with source mutations; fenced parent lifecycle; indexed rank neighbors/refusal; explicit coverage/continuation. Submit shared constructor/schema hooks to A as patches.                                                       |
| C: search and SSE                          | Delegable: new `core/internal/scopesearch/`, `core/internal/scopestream/`; `core/internal/primitives/docs_knowledge.go`; `core/internal/server/stream_handlers.go`, `stream/`; new `scope_search*_test.go` and `scope_stream*_test.go` in server                                                                                                                                                                                                        | Per-resource current-head/comment postings, bounded search coverage, total audience/family stream cap, tick reauthorization and opaque continuations. Route/contract registration changes go through A.                                                                        |
| D: migration and downgrade protection      | Delegable: new `core/internal/scopemigrate/`; new `core/internal/storage/scope_migration*.go` and tests, `core/internal/storage/workspace.go`                                                                                                                                                                                                                                                                                                           | Metadata-only startup, deterministic container/creator/admin placement, no historical blob/prose scan or blob retries, durable cursors/epochs, low-disk/crash safety, binary fence and compatible reopen. Migration registration and primitive constructor edits go through A. |

B and C propose canonical mutation hooks instead of independently modifying common
comment/event/transaction helpers. A owns final wiring, preserving one transaction
for canonical rows and all bounded deltas. D owns worker lifecycle; it consumes
B's lifecycle rebuilder through a package interface. The scopes package must not
import readmodel/search/stream/migration packages; no dependency cycle or raw SQL
handle escapes into HTTP handlers.

## Shared interface contract for the foundation commit

- `scopes.RequestSelection`: authenticated principal plus requested scope IDs;
  validates K<=64, grant generations and scope availability in the current
  transaction. `Selection` is opaque outside trusted repositories. The directory
  returns authorized scope IDs, continuation and `more_scopes`; Overview echoes
  its covered IDs. This is a contract sketch; freeze exact Go signatures in A's
  first commit before B–D integrate.
- `scopedrepo.Reader` / `Writer`: only reviewed typed templates, transaction-bound
  capabilities and bounded DTOs. Business computations receive one scope
  capability, no factory or raw handle. Publication is a separate version-bound
  operation requiring source owner/admin plus destination write authority.
- Internal `Change`: scope, kind, opaque resource identity, canonical version,
  before/after bounded projection fields, family and audience. B/C consume the
  same transaction-local old/new delta; no whole-record history reconstruction.
  Mutation hook wiring is lead-owned and inventoried.
- `readmodel.Page`: items, continuation, covered scopes, `more_scopes`,
  availability and `as_of`. `Count` reads exact generation-bound buckets; a
  transitioning scope returns unavailable, never a synthetic zero. Lifecycle
  initiation fences first; worker chunks are <=64 candidate records.
- `scopestream.Tick`: <=256 exact scope/family/audience streams, <=4 per scope;
  validate bindings before queries, preserve un-emitted heads in authenticated
  encrypted continuation. `scopesearch.Page`: fixed-order bounded candidate and
  verification budgets, declared coverage and generation-bound continuation.
- `scopemigrate.Step`: bounded keyset chunk plus durable checkpoint, source epoch
  and DB fencing token; failures advance no success watermark. No blob backend
  is needed to place historical metadata. An expanding creator/admin fallback uses the restricting owner when safe,
  otherwise a no-grants scope. Report exception counts; these do not block cutover.

## Concurrent PRs and hard gates

Land #275 before semantic cutover and port its roles/evidence/health fields; do
not allocate over its 64/65 migrations. It can land during foundation/backfill.
Allocate new migration numbers only against the actual merge head. Adopt #295's
SCA-661 harness and budgets; its owner retains harness files. Let SCA-665 land
first and rebase B onto it, retaining its tests rather than racing its files.

Every release PR needs the existing #288/#294/#298 privacy/route/storage inventories,
new direct and implicit-flow derivation negatives, local module checks and
`make test-fast`, independent subagent review and exact-head `ci-ok`. Enable new
readers only after SCA-661 scale/budget/query-plan checks pass. Cutover additionally
needs synthetic personal-size and 10x migration interruption runs, actual cold
readiness, real crash/lease/ENOSPC coverage and compatible forward recovery. The
morning target does not waive any gate; incomplete phases remain disabled.

## Frozen foundation interfaces

The first implementation lives in `core/internal/scopes` and
`core/internal/scopedrepo`; the repository README is the concrete API reference.
`scopes.RequestSelection`, `scopes.Change`, `scopedrepo.Reader` and
`scopedrepo.Writer` are now Go types, not pseudocode. Reader/Writer callbacks expire
at transaction exit; selection validation alone grants no reusable authority.
The foundation remains unwired pending reviewed integration.

B defines its `Item` and `type Page = scopes.Page[Item]`, using `scopes.Coverage`.
C implements `scopes.StreamTicker`: `Tick(context.Context, scopes.TickRequest)
(scopes.TickResult, error)`. D implements `scopes.MigrationStepper`:
`Step(context.Context, scopes.StepRequest) (scopes.StepResult, error)`.
These shared types contain the exact fields; worker checkpoint cursors remain
D-owned durable state and are not caller-supplied. Changes enter B/C only through
the lead-owned canonical transaction hook. The interface types do not themselves
confer authority or enable routes.

The frozen canonical adapter hook is
`scopedrepo.CanonicalHook.ApplyCanonical(context.Context, scopedrepo.MutationTx,
scopes.CanonicalMutation) error`. A invokes
`scopedrepo.ApplyCanonicalHooks(ctx, existingResourceaccessTx, mutation, hooks...)`
inside the source mutation transaction. It pins source authority, expires the
proxy/rows and caps all hooks together at 256 SQL calls. Reviewed adapters must
use transaction-preserving single statements; under that trusted contract,
errors/panics roll back. Exact template allowlisting remains a wiring gate. This trusted adapter boundary is not a computation capability or a
replacement for reviewed bounded SQL. No canonical constructors are wired yet.

`Reader.ResourceIdentity(scope, kind, opaqueID)` resolves the new immutable
`scope_resource_rids` mapping in the authorized read transaction. The positive
integer RID is private, database-global, and registered atomically with its opaque
resource identity. Canonical source IDs and RIDs never appear in JSON. The
`CanonicalMutation` descriptor binds that identity to the adjacent old/new
version and at most four distinct audience deltas. Structural validation is not
source-provenance validation: A must still capture these fields from canonical
rows, build B's feed adapter, and prove complete mutation coverage before serving.
