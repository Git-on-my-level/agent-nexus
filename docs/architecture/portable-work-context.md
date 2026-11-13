# Portable work context

Work context is a deterministic projection, not a scheduler or a second task
store. `anx context <card> --brief` (also `anx work briefing <card>`) reads one
visible commitment, its latest stored source observation, freshness and current
attributed checkpoint. It does not contact the upstream system or scan history.
Use `anx pm context --work-ref <card>` for decisions; the briefing explicitly
states that decision history is not included and grants no approval.

## Sources and portability

GitHub, another tracker, an execution adapter or a research agent can use the
same contract. Source identities retain authority/connection/native ID. Unknown
source states and evidence coverage remain in the latest observation. Collectors
submit observations through the existing work-observation API; a source refresh
is an explicit operation. No host names, repository layout, provider SDK or
fleet service is required by the briefing/checkpoint protocol.

Agent checkpoints record intent which cannot be inferred deterministically from
logs. Send a checkpoint at a meaningful boundary, such as a review, interruption
or handoff, rather than every tool call. A checkpoint is an attributed report,
not a verified outcome, ownership transfer, accepted result or durable decision.
Use the canonical decision and receipt surfaces for those records.

Each artifact has a reference and declared portability. `local_only` requires a
host identifier; another host must not assume it can open that path. `portable`
is an author's claim of reachability, not proof of a backup. This API never copies
artifact bytes. Preserve an endangered patch or result through the artifact's
own storage before claiming recoverability. Do not include credentials, raw
transcripts, private customer data or large logs.

## Bounded storage and concurrency

One current checkpoint is retained per card, at most 16 KiB including its encoded envelope.
It replaces its predecessor with an independent version, reporter, digest and
update timestamp. It does not append an event, create a document revision,
create a replay ledger entry, alter task activity or invalidate work decisions.
Identical content from the same reporter is a no-op. Different content requires
the current checkpoint version (zero when absent); simultaneous writers cannot
silently overwrite each other. After an uncertain response, read back before
retrying. If a later checkpoint exists, an old write conflicts rather than
restoring old content. Checkpoint version is not a lease or proof of ownership.

Briefings are built on demand and never stored. Checkpoint bodies appear only
in single-work reads and briefings, not bulk work projections. Optional oversized fields are
listed in `omitted`; incomplete output must not be treated as complete context.
The response is capped at 64 KiB and fails explicitly if it cannot fit. Existing
source observation history and canonical audit retention are unchanged: this
feature does not erase old evidence or promise to bound unrelated histories.

## Offline discussion

A caller may explicitly export JSON to a private local file (for example with
`umask 077` and shell redirection). That file is a snapshot, not an automatically
refreshed authority. Label the export with its originating workspace, reader
and export time; card handles alone are not globally unique. Its
observation/checkpoint timestamps expose age; when
using it offline, state that access revocation and current ownership could not
be checked. Never use it to take over a writer, infer permission, or repeat a
non-idempotent action. Keep exports outside repositories, scoped to the reader,
and delete them when no longer needed. No shared implicit cache is introduced.

## Adapter responsibilities

Resolve work by canonical identity, not title similarity. Publish bounded
observations with revision/sequence, check time, coverage and evidence. Publish
agent intent through checkpoints separately from source facts. A host-local
collector can report local artifacts without giving the server SSH or filesystem
access. Cross-device relays may carry references or change notifications; they
are optional and never become task authority.

## Validation

`cli/integration/work_context_integration_test.go` runs compiled binaries against
an isolated core and SQLite workspace. It exercises native research, GitHub and
a generic external tracker: failed reads preserve last good source state,
checkpoint retries are no-ops, and stale checkpoint versions conflict.

`core/internal/primitives/work_checkpoint_test.go` checks restart persistence,
simultaneous writers, patch races, unchanged task/decision versions, the envelope
size limit, private references, and unchanged event/observation/replay counts.
`core/internal/server/work_checkpoint_access_test.go` verifies the authenticated
HTTP privacy boundary. The standard scale corpus includes the briefing in
`TestPerformanceWorkTreeRoutes` and the full route performance gate.

On 2026-11-13, a local dogfood run used this implementation's own work location,
branch, pending checks and next action as the checkpoint. A server restart
retained it, an identical retry changed nothing, and a separate reviewing agent
could recover the work location, pending steps and authority limits. Repository
inspection was still necessary for the exact diff and current check results.
