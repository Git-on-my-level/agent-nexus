# Disabled metadata migration integration

No initializer or handler consumes this package. The legacy reader remains
authoritative. `MetadataSource` uses the frozen opaque resource/RID mapping;
`CaptureCanonical` runs reviewed single-statement SQL on the supplied canonical
transaction. It neither begins a source transaction nor reads historical content.

The production adapter currently has **no audience certificates**. It therefore
places every registered record in a verified permanent `inaccessible` scope and
counts it as an exception. This is the accepted compatibility fallback, not a
creator/owner ID assertion. The pure placement rules remain available for future
verified certificates. No subset proof or privacy/parity acceptance is claimed.
Registry/capture metadata also proves no historical content availability: these
records retain `content_unavailable=1`; no automatic retry or recovery is added.

The registry is not a complete historical census. Completion from this source
means completion of its registered RIDs only. It must never be used as proof that
all historical canonical families were registered, that projections are ready,
or that the reader can cut over. Historical registration remains an integration
gate and must include #275's evidence and roles after its actual merge head.

## Proposal for A: canonical and schema wiring

1. Register `storage.InstallScopeMigration` and the scope shadow schema only
   after allocating against the actual merge head; 64/65 remain reserved.
2. Install all optional canonical schemas, including PM/roles/evidence, then call
   `storage.InstallScopeMigrationEpoch(ctx, tx)` in that same schema transaction.
   This creates no canonical copies and scans only <=512 table definitions.
   Ordinary source/authority tables receive unconditional I/U/D invalidation.
   Reviewed exclusions are D's epoch/jobs/placements, reader cursors/Overview
   visits, series admission budgets, and B's feed/counter rebuild targets.
   Reader acknowledgements and worker staging cannot restart their own pass.
   Virtual/FTS
   shadow inputs are covered through their ordinary canonical tables. Any later
   DDL causes `ErrEpochCoverage`; rerun the installer explicitly in the schema
   transaction. Do not quietly repair coverage from a worker read.
3. Durably bind the complete externally selected authority configuration with
   `storage.BindScopeMigrationAuthority(ctx, tx, token)`, including the selected
   PM identity and any other non-SQL audience authority. Supply the identical
   token to `MetadataSource{AuthorityToken: token}`. Conflicting processes fail
   closed. Binding changes and their epoch increment roll back together. The
   current environment-only PM selector is not proof of this binding.
4. Integrate `proposals/canonical-hook.patch` into the trusted canonical adapter
   layer. Run it after the canonical write and registry version update, inside
   the **existing** `resourceaccess.Tx`, through `ApplyCanonicalHooks`. Imports
   must preserve the same transaction contract. Supply the previous canonical
   version, the actual RID/opaque/canonical tuple and detached bounded deltas.
   The tuple is rechecked in SQL; failed capture must roll back the source row,
   registry version, capture and epoch together. Never use `Store.Write` or
   `RegisterForMigration` to open a second transaction inside the source write.
5. Provide a transaction-bound legacy registration operation (the current
   `RegisterForMigration` owns its own transaction) and an inventoried bounded
   metadata census. It must register each historical family with opaque IDs,
   stable RIDs and source versions in <=64-row indexed keysets, checkpoint in
   that transaction, and prove coverage at the pinned epoch before placement
   completion can be considered workspace completion. No prose, blobs or
   transitive graph closure belongs in this census. Missing audience proof is
   permanently sealed, not sent to a recovery queue.

New schema classifications for A's registration/inventory review:

| Relation                   | Columns                                                 | Classification         |
| -------------------------- | ------------------------------------------------------- | ---------------------- |
| `scope_migration_epoch`    | singleton, version, schema_cookie, authority_token      | internal:authorization |
| `scope_migration_captures` | rid, scope_id, kind, resource_id, canonical_id, version | internal:authorization |

These tables are not installed on production workspaces yet, so their columns
are not appended to the live-schema inventory prematurely. Only D-owned writer
declarations/fingerprints are appended or updated by this stream.

## Proposal for A/B: supervised lifecycle integration

`StartWorker(ctx, false, nil, 0, nil)` is the default: no SQL and no goroutine.
An enabled worker must be started after HTTP readiness is available, supervised
by the server shutdown context, and `Close()` joined **before** `Workspace.Close`.
The worker never performs historical blob reads, and never repairs a missing
manifest. Migration and lifecycle slices use <=64 candidates and <=50ms; failed
transactions advance no success watermark. Readiness does not wait for a slice.
Lease acquisition, migration and lifecycle transactions pin a worker connection
and disable its SQLite busy wait. A competing writer causes immediate SQLITE_BUSY
and a later slice retries; the connection's serving timeout is restored after
rollback/commit, or the connection is discarded if restoration fails. Pool waits
and transaction work use the slice context. Physical driver initialization and
OS scheduling are outside the SQLite lock-wait bound; opening a cold connection
can exceed the requested duration and is refused once its context has expired.
Unchanged fenced opens do not write
the ledger digest, so they preserve the captured epoch and staged progress.

B's frozen `readmodel.Step(ctx, LifecycleTx)` at `d8e9d4b8` is absent from this
integration base. Once the package is integrated, A/B must supply the reviewed
durable SQL repository backing `LifecycleTx.Job`, `Rows`, `Apply`, `Checkpoint`
and `Activate`. The following adapter is the exact bridge to D's lifecycle:

```go
type LifecycleAdapter struct {
    Bind func(context.Context, *sql.Tx, int64) (readmodel.LifecycleTx, error)
}
func (a LifecycleAdapter) Step(ctx context.Context, tx *sql.Tx, limit int, token int64) (bool, error) {
    if limit != readmodel.MaxLifecycleChunk || token < 1 || a.Bind == nil {
        return false, scopemigrate.ErrBudget
    }
    repository, err := a.Bind(ctx, tx, token)
    if err != nil { return false, err }
    result, err := readmodel.Step(ctx, repository)
    return result.Complete, err
}
```

`Bind` must retain the supplied transaction, not create a connection/factory.
`Rows` must seek `(scope_id,rid)` with candidate limits before materialization,
resolve <=8 same-scope ancestors and select bounded lifecycle metadata.
`Checkpoint` and `Activate` must check both the scope job fence and D's current
owner/token/unexpired lease; a zero affected-row count aborts. Foreground fence,
parent mutation and durable enqueue belong to one A-owned transaction. Activation
requires an empty indexed seek after the staged generation is fully rebuilt.
No reviewed job/schema/projection repository exists on this base yet; substituting
a nil rebuilder is not evidence of lifecycle completion.

## Costs and remaining acceptance gates

Epoch, sink, capture and progress use singleton/unique-key probes. Metadata paging
materializes <=64 integer-RID candidates before exact registry/capture joins;
work is O(log N + K log N), K<=64. A missing registry row fails the chunk instead
of scanning through orphan RIDs. Bytes are bounded and a short byte page does
not claim completion. Capture is one indexed INSERT/UPSERT; each epoch trigger
does one singleton update. No new HTTP read path exists.

Current regressions prove database/runtime authority invalidation, rollback,
registry keyset plans, byte pagination, sink checks, 806/8,060 registry migrations
and compatible reopen. They do not prove all-family historical census, recorded
audience parity, production lifecycle/projection wiring or full-request SCA-661
acceptance. The harness owner's expired Overview/startup source allowances remain
real failures; no hashes or budgets are refreshed here. Keep the worker/readers
disabled until the applicable capture, census, privacy, performance and CI gates
pass. No merge, release or stage advancement is authorized.

## Retained downgrade and filesystem probes

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

The constructor remains metadata-only; the existing cold HTTP readiness and
old-binary refusal probes are retained. Set `ANX_SCOPE_OLD_BINARY` to an
independently built schema-63 executable for the historical-binary probe.

`TestFilesystemENOSPCPreservesCheckpoint` exercises actual filesystem exhaustion,
distinct from the SQLite page-limit test. Supply `ANX_SCOPE_ENOSPC_ROOT` pointing
to a disposable mounted filesystem of at most 128 MiB, containing a file named
`.anx-disposable-enospc` with exactly `scope migration disposable filesystem`
and a final newline. Run from `core`:

```sh
go test ./internal/scopemigrate -run TestFilesystemENOSPCPreservesCheckpoint -count=1 -v
```

The probe fills only its temporary directory on that filesystem until even a
one-byte write returns ENOSPC, requires SQLite FULL from the next metadata chunk,
then frees the filler, reopens storage and checks the unchanged durable checkpoint
and zero partial placements before resuming. Test cleanup removes its files. It
skips without the explicit environment variable and in the short test tier. A
64 MiB HFS+ disk-image run passed; Linux can use a dedicated small tmpfs mount.
