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

Canonical event payloads (including nested refs), notification triggers/refs,
plans, work metadata, and runs participate in inherited ownership. Work metadata
constrains the whole card before projection or search. Migration 55 backfills
`resource_access_edges`; database triggers maintain that index atomically with
canonical JSON/scalar writes, including imports. Migration 56 reconciles earlier
55 previews with metadata edges and the normalized project/wakeup indexes.
Use `resourceaccess.ReferenceSQL`
for SQL reference matching so aliases and Unicode whitespace match the parser.
Event content and navigational `ref_edges` also commit in a single transaction.
Filtering a linked plan suppresses its stored refs and titles, not merely live
reference previews. Never serialize a shared maintenance error to a reader.

All canonical, PM and command-center database access must use
`internal/resourceaccess` handles with the request context. Scoped SELECTs use
canonical ownership CTEs before limits, cursors, aggregates and joins. Transaction
loaders use the same policy. Mutations resolve handles to canonical IDs, load
existing targets/destinations, and bind resource identities as SQL arguments;
the database handle checks those arguments (including encoded JSON) within the
write transaction. Never use raw connections, contextless queries, `main.` table
qualification, SELECTs through Exec, or literal resource IDs to bypass the scope.
A new resource table must join the policy's ownership graph and scoped relations.

Keep authorization separate from lifecycle filtering. Missing records may retain
legacy semantics, but a known inaccessible record must never be treated as
public. Never cache visibility decisions for an SSE connection. Shared cached
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
Every record/collection/reference-write/stream entry is exercised against a
private board with a public card thread, using both a stranger and an unauthorized
agent. Identity/transport-only exemptions require an explicit rationale. Extend
the fixture and the scoped store/SSE regressions when adding a resource relation.
