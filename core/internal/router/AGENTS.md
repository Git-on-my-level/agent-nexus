# AGENTS

## Scope
Guide for work inside `core/internal/router/`.

Read this after [core/AGENTS.md](../../AGENTS.md). This package owns the workspace-scoped wake-routing runtime hosted inside `anx-core` as a sidecar.

## Package Purpose
`anx-router` converts durable `message_posted` events into durable wake requests for bridge-managed agents.

It should stay:
- lightweight to host inside `anx-core`
- bounded to explicit internal dependencies and documented OAR primitives
- explicit about workspace scope rather than host scope
- easy for future agents to extend without re-reading the whole repo

## High-Value Invariants
- Routing is workspace-scoped. Do not add host-local assumptions to wake decisions.
- Router state is local operational state only; canonical truth still lives in OAR primitives.
- Taggability must remain driven by registration plus workspace binding, never by a transient live bridge check-in.
- Bridge check-ins control online delivery semantics, not whether a valid registered agent can be tagged.
- Event handling must be idempotent across reconnects and duplicate deliveries.
- Keep the router package independently testable even though the runtime is hosted in-process.

## Edit Routing
- HTTP shape or auth semantics that affect other modules still start from contracts/core docs first.
- Keep mention parsing, readiness validation, and wake emission logic easy to find and test separately.
- If you add new operator-facing behavior, update `core/README.md` and `core/docs/runbook.md` in the same change.

## Validation
- `go test ./internal/router/... ./cmd/anx-core/...`
- `./scripts/build-prod`
- Relevant repo-level checks when the change crosses module boundaries.
