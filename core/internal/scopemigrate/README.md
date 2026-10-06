# Metadata migration foundation

This package stages metadata and cannot select a reader. Stream A has reviewed
and integrated the constructor change: startup never reads historical blobs.
Migration registration remains pending; the worker stays disabled/unwired until
the production authority adapter and capture are integrated.

`Store.Step(ctx, scopes.StepRequest)` implements A's frozen `MigrationStepper`
from #301 at `c03b020a`. Job, expected epoch, token and budgets are checked within
the worker transaction. Returned counts describe that committed chunk; a failed
request returns no progress. The opaque RID mapping and canonical transaction
hooks remain integration work.

`Runner.Acquire(ctx)` returns a monotonically fenced DB lease token.
`Runner.Step(ctx, token, limit)` commits at most 64 ordered metadata records,
4 MiB and 50 ms of work with their checkpoint. Failure rolls back the whole
chunk. An epoch change resets the checkpoint into a new unreadable generation;
old rows are retained for bounded maintenance rather than deleted in startup.
`Runner.Report(ctx)` reads one indexed job row, including exception counts.

The trusted `Source` adapter must provide strict indexed keysets and current
precomputed legacy audience-subset facts. Its epoch must include canonical rows,
aliases/identities, grants, PM selection, lifecycle and purges. Do not reuse
`resource_access_epoch` alone without proving capture completeness. No body,
historical prose, blob or per-record graph-closure reads are permitted. Unknown
manifests stay denied; unavailable content has no retry queue or worker.

Placement chooses a proven safe container, otherwise creator-private, then
admins-private. A denied fallback chooses the single restricting owner's private
scope when proven safe; multiple owners or no safe candidate choose the supplied
scope with **no grants, including implicit grants**. The authority adapter must
prove that property. Exceptions are counted and do not block cutover. Related
children placed elsewhere must remain guarded in A/B's projections.

`Runner.Run(ctx, interval, report)` is the foreground worker lifecycle. The server
must supervise it using a shutdown context and join it before closing storage;
readiness must not wait for it. B supplies `LifecycleRebuilder.Step` against the
same transaction and token, with at most 64 candidates. Reports carry sanitized
categories. `CheckDisk` must inspect the database/WAL filesystem before chunks;
SQLite FULL/ENOSPC still rolls back transactions. Disk thresholds and the actual
production supervisor/capture adapter belong to integration, not a test fixture.

`storage.InstallScopeMigration(ctx, tx)` creates only empty new tables/indexes.
A allocates its version against the actual merge head after #275's 64/65, updates
canonical internal schema classifications and both live inventories. The automatic
`BackfillArtifactAccess` call is removed from `NewStore`, with no replacement
legacy blob worker. The cold HTTP regression exercises the real constructor.
Unknown manifests and dependent resources may remain inaccessible indefinitely
until explicit maintenance or the future scope migration handles them. The
remaining maintenance method has no production caller or operator command.

Expanded databases acquire an OS serving lease. Driver connection references
retain it after `Workspace.Close` while an outstanding transaction/connection can
still write. Unexpanded storage retains its previous reopen behavior. Before
installing expansion/enabling the protocol, stop **all existing unleased processes
and connections**, including compatible processes opened before expansion.
No advisory lock can fence a connection which never participated in the protocol.

`Workspace.InstallScopeFormatFence(ctx)` atomically renames the ledger and installs
a view requiring the absent `anx_requires_scope_format_v1()` function. Compatible
opens read the renamed ledger, check its recorded-evidence digest and known
versions, and refuse malformed/unknown state. The digest is not a historical
migration-source hash: schema 63 never recorded those. This format supports only
the retained legacy reader. A must extend validation atomically with any semantic
cutover; this foundation cannot enable scope authority or retire the old graph.

Tests cover actual canonical metadata keysets at 806/8,060 records, restart,
subprocess death during an uncommitted chunk, lease takeover, real SQLite FULL,
epoch changes, lifecycle rollback, old-loader refusal, compatible subprocess
reopening, and live HTTP readiness with 100/1,000 unavailable blobs. Set
`ANX_SCOPE_OLD_BINARY` to an independently built schema-63 core executable to run
the full historical-bootstrap refusal test. Full canonical corpus migration,
production authority/projection parity, filesystem ENOSPC, SCA-661 budgets and
green exact-head CI remain release gates. No hosted data is used.

Lifecycle slice deadlines also own the transaction, including lock acquisition and commit; a callback that returns success after its deadline cannot commit. Cold readiness tests use a delayed, unavailable blob backend and require zero reads on repeated opens.
