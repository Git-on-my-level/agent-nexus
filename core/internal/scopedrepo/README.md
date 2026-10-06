# Scope repository foundation

This package is additive and **not used by serving or workspace startup**. Existing
privacy policy, routes, migration numbers and data stay authoritative. It supplies
shared code and executable boundaries for the parallel scope workstreams. No
container-to-scope shortcut is authorized by adding these tables.

The reviewed shadow DDL is `schema.sql`; `Initialize(context.Context)` creates only
empty tables. It is deliberately not registered in `storage/migrations.go` until
the migration owner rebases after #275 and coordinates the next free version.
The new table/column manifest below must join the live storage inventory at that
point. Existing writer fingerprints already cover this implementation. Permanent
no-grants legacy exceptions use `inaccessible` domains; SQL triggers reject
membership grants, and the PM active-scope directory must exclude them. This
adds no inspection/recovery workflow.

| Relation                                                                     | Authority / data classification                              |
| ---------------------------------------------------------------------------- | ------------------------------------------------------------ |
| `scope_domains(id,state,generation)`                                         | Internal authorization, indexed scope directory              |
| `scope_memberships(principal,scope_id,role,generation)`                      | Internal authorization; exact principal/scope binding        |
| `scope_resources(scope_id,kind,id,canonical_id,version)`                     | Scope-owned reference directory; no canonical payload copies |
| `scope_resource_rids(rid,scope_id,kind,resource_id)`                         | Internal positive integer key; never an external identity    |
| `scope_aliases(scope_id,kind,alias,resource_id,retired)`                     | Scoped identity including reserved retired aliases           |
| `scope_replays(principal,replay_key,scope_id,kind,request_hash,resource_id)` | Creator replay; reauthorizes scope and checks request hash   |
| `scope_projection_values(scope_id,projection_key,value)`                     | Derived value, readable/writable only under its source scope |

## Interfaces available to parallel workstreams

- `scopes.ID`, `Role`, `Stream`, `Binding`, `DirectoryPage`, fixed scope/stream/page
  budgets and typed errors are in `internal/scopes` (no database dependency).
- `scopes.RequestSelection` and `scopes.Change` freeze dispatcher input and bounded
  before/after deltas. `scopes.Page[T]`/`Coverage`, `StreamTicker` and
  `MigrationStepper` freeze B/C/D response and worker signatures. They confer no
  authority on their own.
- `Read(ctx, scopes.RequestSelection, func(Reader) error)` authorizes every selected
  scope in one transaction and expires the render-only Reader on return.
  `Reader.ResourceIdentity(scope, kind, resourceID)` returns
  `(scopes.ResourceIdentity, error)` by two exact unique-key probes, sharing the
  256-operation budget. The private RID is globally unique within this database,
  allocated by an insert trigger in the resource registration transaction and
  immutable/reserved even across SQLite replacement. Replay retains the same RID.
  RID and canonical ID are excluded from JSON; transports must use opaque IDs.
  Initialization never scans existing registries: this unpublished shadow schema
  expects a fresh generation. A worker must explicitly fill any older experimental
  generation before it can become ready.
  `Write(ctx, scopes.RequestSelection, func(Writer) error)` accepts exactly one
  scope and exposes opaque derived values. Complete dispatcher separation remains
  an integration gate; business code cannot be given both factories.
- Trusted setup only: `scopedrepo.New(*sql.DB) *Store`, then
  `Initialize(context.Context) error` on the shadow generation.
- `Directory(ctx, principal string, after scopes.ID, limit int)` returns
  `(scopes.DirectoryPage, error)`. It examines <=limit+1 binding candidates and
  <=limit exact domain probes. Unavailable own bindings consume slots. `After`
  is internal; transports must encrypt cursors and reauthorize each page.
- `ValidateSelection(ctx, principal string, ids []scopes.ID) error` checks <=64
  exact memberships. It is validation, **not a reusable authorization token**.
- `RegisterForMigration(ctx, principal string, scope scopes.ID, kind, canonical,
alias, replay string) (string,error)` creates a random 128-bit reference identity
  for a canonical row. This is trusted backfill registration, **not a user-facing
  creation endpoint**. Live creation must combine canonical insertion and scope
  registration in one transaction; that API is still a cutover prerequisite.
- `Compute(ctx, principal string, scope scopes.ID, func(*Computation) error)` pins
  one transaction. `DocumentTitle(id)` and `Constant(string)` return opaque
  `Derived`; `Persist(destination scopes.ID,key string,value Derived)` rejects
  rebinding and cross-scope output even for an owner of both scopes. Values and
  capabilities expire on callback exit and contain no reflectable plaintext.
  Work is capped at 256 operations, 64 values, 64 KiB each.
- `CanPublish(ctx, principal string, source,destination scopes.ID) error` checks
  source owner/admin plus destination writer. It is **not a publication intent or
  mutation**; publishing must recheck inside its own commit transaction.

`go generate ./internal/scopedrepo` copies reviewed SQL templates into Go constants.
`manifest.json` has only template name and ownership category. It cannot express
policy or construct SQL. Generator tests reject unowned templates and stale output.
Business capabilities expose exact/range key lookups, not arbitrary SQL.

### Trusted canonical hook contract

`scopedrepo.CanonicalHook.ApplyCanonical(context.Context, MutationTx,
scopes.CanonicalMutation) error` is the injection interface for A-owned adapters.
`ApplyCanonicalHooks(ctx, *resourceaccess.Tx, mutation, hooks...) error` consumes
an **existing canonical transaction**; it neither begins nor commits one. Up to
four hooks share a 256-call budget. The proxy exposes only `ExecContext` and
`QueryContext`, pins the source context/policy, and closes returned rows when its
callback expires. Hooks must issue only reviewed transaction-preserving single
statements (no transaction control, compound SQL or schema changes). Under that trusted-template
contract, any SQL/budget/callback failure, including an ignored proxy error or
panic, rolls back the source transaction. The canonical writer commits
only after success. Both source and supplied contexts control cancellation and
deadlines, including row iteration; authority and all context values remain pinned to the source context.

`scopes.CanonicalMutation` carries the internal identity, previous version and
one to four distinct family/audience `Change` deltas. The new version must be the
previous version plus one; every delta must name the same scope/resource/version.
Audience moves use old-only/new-only deltas. Adapters receive detached copies.

Search source capture uses `scopes.BoundSearchProjection(Projection)
(Projection, error)` before constructing a bounded Change. It reserves space for
metadata, copies at most 64 KiB of valid UTF-8 title/body/metadata, trims body only
at rune boundaries, and preserves `Projection.SourceTruncated`. It neither scans
the rest of an oversized incoming body nor retains its backing string. Metadata
that cannot fit is refused. Callers must pass the complete incoming body or carry
any prior truncation explicitly; this helper does not authorize classification.
C's search adapter must OR `After.SourceTruncated` with its existing trusted
truncation argument and its own normalized-prefix coverage. An omitted source
suffix can never become complete merely because normalization fits the budget.

This raw SQL proxy belongs **only to trusted repository adapters using reviewed,
bounded templates**; it is not a business computation capability, and a call
count does not bound arbitrary SQL work. Exact B/C template allowlisting is a
wiring gate; the current proxy does not parse or mechanically restrict SQL.
Descriptor validation is structural, not proof of provenance. A still owns canonical capture coverage, constructor
wiring and the concrete B/C template adapters. Those require same-transaction
source reads and mutation capture, plus legacy-policy parity before enablement.
The import gate below remains in force; this contract alone enables no handler.

## Explicit integration gates

`TestFoundationNotServing` rejects every production import of `scopedrepo`, including
alias/dot imports. Thus neither a helper nor a handler can start a second
computation with private control-flow information today. This is a temporary
integration gate, **not the completed computation analyzer**. Replace it only with
reviewed dispatcher isolation, transitive call-graph/implicit-flow negative tests,
route/field privacy coverage and migration parity. Serving must never receive a
Store or DB handle. The selected PM's special authority is not wired yet.

The existing HTTP policy remains fully in force. Phase-B read models must measure
full request cost including the old denial graph and prove old-policy parity for
counts, search and streams. Passing these small repository tests establishes
neither endpoint speed nor permission to switch the reader. Migration interruption,
1x/10x fixtures, SCA-661 budgets, real crash/lease/ENOSPC and compatible reopen stay
release gates for the phases that introduce those behaviors.
