# AGENTS

## Scope

Guide for work inside `core/`.

Read this after the root [AGENTS.md](../AGENTS.md). Keep this file focused on durable core purpose, invariants, and edit routing. Put volatile implementation detail in specs, runbooks, and code-local docs instead.

## Module Purpose

`core` is the authoritative state and evidence service for Agent Nexus.

It owns the canonical organizational record, validates and records state transitions for all actors, and exposes a stable programmatic interface to that record. Derived collaboration views exist to help clients operate, but they remain projections of canonical truth rather than independent sources of truth.

## Core Responsibilities

- Preserve durable organizational truth across canonical primitives such as events, topics, cards, boards, documents, artifacts, backing threads, and actor identity records.
- Enforce contract-safe and evidence-safe writes, including typed references, schema validation, and restricted transitions that require supporting evidence.
- Remain actor-agnostic: humans, agents, and future clients are all just actors operating through the same external contract.
- Separate canonical state from derived views and keep derived data reproducible from canonical records.
- Provide an auditable API and stream surface that other modules can rely on without embedding core internals.

## What Core Does Not Own

- Agent orchestration, dispatch, or lifecycle management.
- Human-facing operator UX beyond the API contract.
- Unscoped real-world side effects outside the Agent Nexus workspace. PM integration
  may hand off explicitly authorized actions to source tools; collectors remain
  read-only and durable decisions/receipts remain in this workspace.
- Control-plane-specific integration types, clients, or endpoint constants.
  Allowed carve-outs: generic env-driven contracts for the heartbeat publisher
  and for the optional HTTP account status checker (see below).

## Canonical References

- System spec: `docs/anx-core-spec.md`
- HTTP contract: `docs/http-api.md`
- Shared schema contract: `../contracts/anx-schema.yaml`
- Spec implementation matrix: `docs/spec-compliance.md`
- Runtime and deployment guidance: `docs/runbook.md`

## High-Value Invariants

- Event identity, ordering, refs, and payload content are append-only. Corrections are new records, not edits in place. Event lifecycle visibility fields (`archived_at`, `archived_by`, `trashed_at`, `trashed_by`, `trash_reason`) are the bounded mutable exception used for filtered views.
- Topic, card, board, and document updates use patch semantics: omitted fields are preserved, and list-valued fields are replaced only when explicitly present.
- Unknown fields and unknown open-enum values must round-trip safely unless the shared contract says otherwise.
- Restricted state transitions must remain evidence-backed.
- Derived views must stay rebuildable from canonical state.
- Inbox list/get/stream payloads use `related_refs` as the typed-ref collection on each item. Core may still read a legacy `refs` field only when backfilling stored derived rows inside `applyInboxContractShape`; clients and UI code should not treat `refs` as an inbox alias.
- Core-maintained collaboration state must remain correct without introducing misleading user-visible activity.

## Generic Heartbeat Publisher Contract

- `ANX_HEARTBEAT_PUBLISHER_URL`: when set, enables periodic signed heartbeat publishing from `anx-core`.
- `ANX_HEARTBEAT_INTERVAL`: optional interval, default `30s`.
- `ANX_HEARTBEAT_AUDIENCE`: optional JWT audience, default `anx-control-plane`.
- `ANX_WORKSPACE_SERVICE_ID`: required identity issuer/subject when publisher is enabled.
- `ANX_WORKSPACE_SERVICE_PRIVATE_KEY`: required base64 Ed25519 private key when publisher is enabled.

## Generic Account Status Checker Contract

- `ANX_ACCOUNT_STATUS_URL`: when set, enables optional HTTP account status checks during hosted human refresh flows (base URL only; path is configurable separately).
- `ANX_ACCOUNT_STATUS_PATH`: optional path suffix joined to the base URL, default `v1/internal/accounts/status`.
- `ANX_ACCOUNT_STATUS_AUDIENCE`: optional JWT audience for the workspace service assertion, default `anx-control-plane` (same default as the signer when audience is omitted in code).
- `ANX_WORKSPACE_SERVICE_ID` / `ANX_WORKSPACE_SERVICE_PRIVATE_KEY`: required when account status checks are enabled (same identity material as the heartbeat publisher).

## Edit Routing

- Contract or schema changes start in [../contracts/AGENTS.md](../contracts/AGENTS.md).
- API behavior changes should update the relevant HTTP handlers, backing domain/store logic, docs in `docs/`, and the tests that enforce the behavior.
- Persistence or projection changes should preserve canonical-versus-derived boundaries and include migration or rebuild coverage where needed.
- If a change affects client assumptions, update the contract docs first and then adjust CLI and UI consumers.
- Long-lived / streaming endpoints (WebSocket, SSE, chunked) MUST be registered via `internal/server/stream.Mount`.

## Validation

- `make -C core check`
- `./scripts/test`
- Add or update focused unit and integration coverage for the touched subsystem.
- When contracts change, run `make contract-gen` and `make contract-check` from repo root.

## Maintenance Guidance

- Prefer describing stable responsibilities and boundaries here, not current file layout.
- Link to specs, runbooks, and tests for evolving implementation detail.
- Update this file when core purpose, module boundaries, or invariants change in a way downstream agents need to know early.

## Resource authorization

HTTP authentication installs an immutable principal scope for both regular and
stream routes. `primitives.WithAccessScope` and `CanAccessResource` define one
record-visibility policy: backing threads inherit their canonical owner, cards
inherit their board, and evidence inherits every referenced private resource.
The selected PM agent and the private owner retain access; unrelated humans,
agents, and anonymous development readers do not.

Canonical text/JSON, event payloads, notification triggers, plans, work metadata,
observations/evidence and runs participate in inherited ownership. Metadata and
observations constrain the whole card before projection or search. Migration 55 backfills
`resource_access_edges`; database triggers maintain that index atomically with
canonical JSON/scalar writes, including imports. Migration 56 reconciles earlier
55 previews with metadata edges and the normalized project/wakeup indexes.
Migrations 57/58 reconcile every ref-bearing source and blob manifest, including
scalar-vs-structured parsing in already-applied previews. Migration 59 repairs
legacy prose reference candidates and marks preexisting series summaries whose
full contributor provenance cannot be reconstructed. Those streams remain hidden
when the reader has any denied contributor; ordinary appends do not clear the
uncertainty. Adapter last_push is suppressed for partial-visibility readers.
Migration 60 repairs NUL-truncated indexes and replaces legacy scalar-function
triggers before rescanning manifests. Reference SQL helpers cast arguments to
BLOB; unsafe TEXT calls fail closed. User text/JSON and business SQL writes
reject NUL with invalid_request at HTTP boundaries. Only internally generated
ReferenceManifest values preserve arbitrary binary/legacy bytes; this exemption
skips text validation, never authorization. Do not cast user input to that type.
Blob validation uses the declared content type: binary bytes remain binary even
when they parse as JSON. Binary manifests still scan every byte for ownership.
Reference matching treats controls and invalid UTF-8 as boundaries on both sides;
only identifier continuations suppress a boundary (with paired Markdown wrappers).
`resourceaccess.OwnershipSources` drives atomic triggers; `ReferenceAtoms` scans
nested JSON values/keys and typed refs in text with shared normalization. Prose candidates retain the original text so legacy IDs containing punctuation
or internal whitespace can match complete identities. Migration 61 resolves prose
inheritance at write time and on identity/alias/revision changes, including targets
created after the prose. Reads traverse materialized mention edges and fixed-size
exact-atom keys. Regular GET requests reuse an immutable denial snapshot only when
the authorization epoch still matches within the consuming statement; a mismatch
falls back to the canonical graph. Rebinding a scope clears its request state.
Inbox SSE starts a new epoch-validated read boundary on every tick and retains
no request snapshot across ticks. Other streams and transactional reads evaluate
the canonical graph directly.
Migration 62 backfills PM and inbox provenance and repairs virtual revision
identities when a parent arrives after its revisions. PM initialization installs
its body-reference triggers atomically with the table; retain separate `(kind,id)`
contributors even though inherited privacy follows PM IDs across kinds. Inbox
parent columns contribute both raw atoms and typed refs, including legacy aliases.
Canonical inbox collections use their indexed SQL visibility and batch metadata
hydration; avoid rebuilding authorization for every already-filtered ordinary row.
Migration 63 indexes ancillary profile fields without making profile denials
resource identities: those denials are graph leaves. Host/auth/actor profiles
remain scoped. Bind cached denial rows as data, never literal SQL, and avoid
repeated CTE references that duplicate the ownership graph during SQLite
preparation. Read selector checks reuse the epoch-validated request snapshot;
transactional mutation checks, including Overview visits, always evaluate their
current graph. Read closures may be shared only with bounded storage keyed by
database identity, ActorID, PMActorID and authorization epoch. Every consuming
statement must retain epoch validation and canonical fallback.
PM principal caches protect routing state only; authority I/O stays outside shared locks.
Use the registered SQLite driver so the reference scalar functions are available
to imports. Structured JSON contributes atoms, not whole container serialization;
scalar text preserves even JSON-shaped IDs. Keep this distinction in new fields.
Blob writers publish `content_refs_json` atomically with metadata. Startup never
reads historical blobs or schedules retries. Unindexed/unavailable old blobs
remain inaccessible; explicit maintenance is separate from readiness. Document search inherits private comments
and revision content before MATCH/rank/limit. Series rollups retain reference
provenance after compaction; full adapter data deletion removes that provenance. Ambiguous preview57 ledger
atoms are conservatively retained because compacted historical states cannot be
reconstructed; never remove them speculatively.
Event content and navigational `ref_edges` also commit in a single transaction.
Filtering a linked plan suppresses its stored refs and titles, not merely live
reference previews. Never serialize a shared maintenance error to a reader.

All canonical, PM and command-center database access must use
`internal/resourceaccess` handles with the request context. Scoped SELECTs use
canonical ownership CTEs before limits, cursors, aggregates and joins. Transaction
loaders use the same policy. Emit only relation shadows named by the SQL, while
keeping the complete canonical ownership graph. A mutation can skip the graph
only after proving its denied-root set empty in that same transaction snapshot. Mutations resolve handles to canonical IDs, load
existing targets/destinations, and bind resource identities as SQL arguments;
the database handle checks those arguments (including encoded JSON) within the
write transaction. Never use raw connections, contextless queries, `main.` table
qualification, SELECTs through Exec, or literal resource IDs to bypass the scope.
A new resource table must join the policy's ownership graph and scoped relations.
Ancillary actor/auth/host/series/secrets readers and transactional response
loaders must retain scoped handles too. Durable authentication/grant invariants
may read canonical authority without returning profile or business content.

Migrations 64/65 add board roles and the bounded evidence projection after main's
authorization history. Evidence records and alias keys are registered ownership
sources, inherit their card, and constrain that card when they reference private
resources. Their tables and views use scoped relations before lookup limits.
`source_refs` and metadata plans inherit through `work_metadata.metadata_json`;
card plans inherit through `card_plans.body_json`. Inbox summary counts and pages
the scoped `derived_inbox_items` relation in SQL. Do not add independent payload
authorization or bypass scoped handles for these projections.

`internal/storage/testdata/resource_access_storage.json` classifies every live
column (including PM/investigation schemas, generated columns and views) and
fingerprints persistence writers plus transitive helper callers in internal/cmd.
New fields and changed writers require an explicit storage privacy review.
`ANX_UPDATE_ACCESS_INVENTORY=1 go test ./internal/storage -run
TestResourceAccessStorageInventory` refreshes fingerprints and marks new fields
UNCLASSIFIED; classify them deliberately and retain executable ownership/filter
bindings. The field-driven regression must cover every indexed source/column.

Keep authorization separate from lifecycle filtering. Missing records may retain
legacy semantics, but a known inaccessible record must never be treated as
public. Never cache visibility decisions for an SSE connection. The event stream creates
a fresh epoch-validated read snapshot per bounded page. Its internal
`event_stream_positions` view exposes only immutable ID/timestamp traversal
metadata, allowing bounded progress over hidden rows. Never serialize those
positions; payloads must load through the scoped `events` relation before decoding.
Client resume IDs must authorize through the same event relation; hidden and
unknown IDs seed head identically. Empty pages advance silently within a fixed
2000-candidate chunk budget. Chunks with remaining positions continue immediately after
yielding; hidden backlog must never add poll-timer waits to visible delivery.
Keepalive cadence and visible-count continuation markers must never depend on hidden backlog or progress.
The event pager alone may narrow cached denial bindings to its explicit page keys
and batch-reference keys; parent denial propagation and the consuming statement's
epoch fallback must remain intact. Never reuse that context for a general read. Shared cached
projections with inaccessible contributors are unavailable to the reader;
a reader-filtered projection must never replace canonical derived state.

`CanonicalMaintenanceContext` is restricted to namespace allocation, quota
accounting, canonical projection/blob-ledger maintenance, shared-blob reference
counts and purge ownership retention. Purges must retain authorization before
deleting ownership edges; tombstoned handles remain reserved. Those paths must
not return unfiltered record data to HTTP callers. Ordinary reads and business
mutations must retain the request scope.

Usage, rebuild results and quota-error record/blob totals must use reader scope;
quota enforcement remains canonical. Database file bytes describe shared physical
infrastructure, not the number or contents of accessible workspace records.

The route matrix in `internal/server/testdata/resource_access_routes.json` must
classify each exact method/path in the generated contract route inventory.
`TestResourceAccessRouteInventory` fails on new, stale or unsupported entries.
Mounted-route checks also compare actual registration and authentication
classifiers; direct mux additions cannot bypass the inventory. Exemptions are
restricted to reviewed service boundaries and exercised for data disclosure.
Every record/collection/reference-read/reference-write/stream entry is exercised against a
private board with a public card thread, using both a stranger and an unauthorized
agent. Identity/transport-only exemptions require an explicit rationale. Extend
the fixture and the scoped store/SSE regressions when adding a resource relation.
Reference-read batches authorize each candidate through scoped loaders and may
echo requested identities without metadata. Their JSON decoding retains text,
size and syntax validation; reference writes retain whole-body ownership checks.
