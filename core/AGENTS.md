# AGENTS

## Scope
Guide for work inside `core/`.

Read this after the root [AGENTS.md](../AGENTS.md). Keep this file focused on durable core purpose, invariants, and edit routing. Put volatile implementation detail in specs, runbooks, and code-local docs instead.

## Module Purpose
`core` is the authoritative state and evidence service for Organization Autorunner.

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
- Real-world side effects outside the OAR workspace.
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
- Events are append-only. Corrections are new records, not edits in place.
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
- `ANX_CONTROL_PLANE_URL`: **deprecated** alias for `ANX_ACCOUNT_STATUS_URL`; if `ANX_ACCOUNT_STATUS_URL` is unset, this value is used and a deprecation log line is emitted.
- `ANX_WORKSPACE_SERVICE_ID` / `ANX_WORKSPACE_SERVICE_PRIVATE_KEY`: required when account status checks are enabled (same identity material as the heartbeat publisher).

## Edit Routing
- Contract or schema changes start in [../contracts/AGENTS.md](../contracts/AGENTS.md).
- API behavior changes should update the relevant HTTP handlers, backing domain/store logic, docs in `docs/`, and the tests that enforce the behavior.
- Persistence or projection changes should preserve canonical-versus-derived boundaries and include migration or rebuild coverage where needed.
- If a change affects client assumptions, update the contract docs first and then adjust CLI and UI consumers.

## Validation
- `make -C core check`
- `./scripts/test`
- Add or update focused unit and integration coverage for the touched subsystem.
- When contracts change, run `make contract-gen` and `make contract-check` from repo root.

## Maintenance Guidance
- Prefer describing stable responsibilities and boundaries here, not current file layout.
- Link to specs, runbooks, and tests for evolving implementation detail.
- Update this file when core purpose, module boundaries, or invariants change in a way downstream agents need to know early.
