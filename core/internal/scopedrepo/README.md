# Scope repository foundation

This package is additive and **not used by serving or workspace startup**. Existing
privacy policy, routes, migration numbers and data stay authoritative. It supplies
shared code and executable boundaries for the parallel scope workstreams. No
container-to-scope shortcut is authorized by adding these tables.

The reviewed shadow DDL is `schema.sql`; `Initialize(context.Context)` creates only
empty tables. It is deliberately not registered in `storage/migrations.go` until
the migration owner rebases after #275 and coordinates the next free version.
The new table/column manifest below must join the live storage inventory at that
point. Existing writer fingerprints already cover this implementation.

| Relation                                                                     | Authority / data classification                              |
| ---------------------------------------------------------------------------- | ------------------------------------------------------------ |
| `scope_domains(id,state,generation)`                                         | Internal authorization, indexed scope directory              |
| `scope_memberships(principal,scope_id,role,generation)`                      | Internal authorization; exact principal/scope binding        |
| `scope_resources(scope_id,kind,id,canonical_id,version)`                     | Scope-owned reference directory; no canonical payload copies |
| `scope_aliases(scope_id,kind,alias,resource_id,retired)`                     | Scoped identity including reserved retired aliases           |
| `scope_replays(principal,replay_key,scope_id,kind,request_hash,resource_id)` | Creator replay; reauthorizes scope and checks request hash   |
| `scope_projection_values(scope_id,projection_key,value)`                     | Derived value, readable/writable only under its source scope |

## Interfaces available to parallel workstreams

- `scopes.ID`, `Role`, `Stream`, `Binding`, `DirectoryPage`, fixed scope/stream/page
  budgets and typed errors are in `internal/scopes` (no database dependency).
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
Queries are exact/range key lookups; no arbitrary query method is exposed.

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
