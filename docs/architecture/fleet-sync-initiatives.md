# Fleet ingestion belongs to initiatives

Decision, 2026-10-05 (SCA-609): recurring fleet ingestion must not mint human-facing
cards from execution items. The 0.1 adapter's source-triple deduplication prevented
duplicate cards but still produced hundreds of unique low-level cards. Deduplication
alone cannot enforce an executive-readable workspace.

## Model and boundary

The adapter now has no card-create client method. Its only recurring card write
is a revision on an explicitly mapped, existing Nexus-owned initiative. N source
items create zero cards, regardless of N, cache loss, retries or terminal state.
External systems remain authoritative; source observations never move, assign,
resolve or complete the initiative. Core and the shared API contract are unchanged.
No hosted or control-plane concepts enter core.

One workspace-local ANX document contains versioned mapping JSON. This is preferable
to host-only configuration because its history is reviewable alongside the outcomes
it affects. The host stores only its connection/reader configuration and document
ref. The document's workspace URL must match the invocation's configured workspace.
Every target resolves within that workspace, using its enrolled host and derived
agent. No cross-workspace principal or source-assignee identity is copied.

Pins precede ordered, first-match rules; rule predicates are conjunctive. Every
previewed item carries its source triple and the winning rule ID. Typos and invalid
destinations fail closed. Unknown items become one Unsorted report panel. Project,
repo and every label are evaluated independently, even when another dimension
is present. Each cluster must meet the threshold (default five) before proposal
deduplication. Qualifying clusters sharing any source identity are combined,
including transitive overlaps: one suggestion carries the union of unique items
and all qualifying dimensions in `clusters`. Disjoint groups and source
connections stay separate. Sub-threshold clusters are never combined to reach
the threshold. Suggestions appear in the same panel without creating cards or
asks. Users can deliberately promote or map a cluster. Table limits are explicit; the full list remains available in the preview.

## Evidence and initiative plans (Release B / SCA-606)

Use the required **linked evidence** branch for automatic ingestion. A collapsed
marked block on the initiative records source links, last observed status, routing
provenance and preserved legacy refs. A source identity's current entry replaces
its previous entry; immutable card revisions preserve history. Absent items retain
their last observation. A mapping change adds the new relationship and leaves the
old relationship as historical evidence. This is intentionally conservative.

SCA-606's plan API shipped in #258 and is the authoritative milestone graph:
steps have stable IDs, titles, refs, after[], optional due and status. Ingestion
and migration use only card body revisions; they never call `plan set` or send
`plan` / `plan_state` fields. Core stores the graph separately and derives state
from its refs and movement timestamps. Updating evidence preserves the graph,
progress, effective step statuses, shape, critical path, next steps, health and
last movement when referenced facts have not changed. Normal source changes and
time can still legitimately change computed state.

We deliberately do **not** add a separate step association or edit step status
when a source URL exactly matches a step ref: that step already links the same
source. The evidence retains that URL, and core resolves it where supported.
This avoids redundant metadata and respects independently authored milestones.
An agent may explicitly use `anx plan step add|update` for a deliberate milestone;
the adapter neither infers dependency edges nor chooses a graph view.

The isolated core/CLI smoke seeds a branching plan with explicit and referenced
statuses, a due date, source URL and legacy-card refs. It verifies the full plan
and computed state before/after ingestion, cache-free replay, migration and
migration replay, including a concurrent plan edit between ingestion preview and
apply. Archived source cards continue resolving through the existing plan API.

SCA-607's live-series API also shipped in #254. Fleet-sync continues publishing
its existing report metric history; adopting declared series is separate from
this mapping change and does not require duplicating that API.

## Revision and failure semantics

Read-before-write yields the exact `if_base_revision` and replacement summary.
Only the marked block changes; title, human text, acceptance criteria, related refs
and provenance survive. A race produces a conflict and a failed run, not an
unconditional retry. Identical input produces no write without relying on a local
cache. Malformed blocks fail closed. Failed/partial readers never delete evidence,
infer completion, or change initiative state. Report failures return nonzero.

One report is published by the existing dashboard mechanism. Its samples are
snapshots from actual runs, not synthetic live-series data. Report creation keeps
the existing single-writer-per-workspace deployment assumption. Cache entries are
workspace-bound; switching workspace discards cached dashboard refs and history.

## Safe migration

The separate migrator recognizes only source cards with a fleet-sync observation
reader ID. It inventories every page, aborts on incomplete inventory, and emits a
workspace-bound, digest-addressed manifest. Native/manual cards are excluded.
Unmatched detail cards remain visible pending explicit mapping; inventing broad
initiatives solely to hide clutter would repeat the original error.

Review and publish mapping, stop old writers, then have the coordinator apply the
exact manifest. Apply preflights the whole batch against source/card fences, folds
all mapped evidence and verifies readback, adds destination/digest relation tombstones,
and archives with a board fence. Original bodies, revisions, conversations and
source IDs remain available. No purge or source writes exist in the tool. A late
failure is resumable; changed cards require a new preview. Do not run two migrations
or legacy writers concurrently. Archive is reversible via `anx cards restore`.

The SCA-609 live dry-run is delivered privately on the issue, not committed to this
public repository. Fixture tests cover 340-item batches, idempotency/cache loss,
routing precedence, workspace isolation, schema limits, conflicts, ownership,
changed-source refusal, evidence-before-archive, tombstones and interrupted replay.
