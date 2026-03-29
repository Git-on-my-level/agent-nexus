# AGENTS

## Scope
Guide for work inside `core/internal/sidecar/`.

Read this after [core/AGENTS.md](../../AGENTS.md). This package hosts
privileged in-process sidecars inside `oar-core`.

## Package Purpose
`sidecar` owns the generic lifecycle shell for internal services that should run
inside the `oar-core` process while keeping their implementation package-local.

Use it when a service needs:
- process-local lifecycle management
- explicit readiness and ops-health reporting
- privileged direct access to core internals through narrow dependencies

Do not use it as a dumping ground for unrelated background work.

## High-Value Invariants
- Sidecars must receive explicit dependencies; avoid broad ambient reach into `core`.
- Readiness must fail closed for enabled sidecars so `/readyz` and heartbeat health stay honest.
- `/ops/health` snapshots should stay small, structured, and useful for operators.
- Disabled sidecars should remain visible in snapshots without making the host unhealthy.

## Edit Routing
- Generic host/lifecycle behavior belongs here.
- Sidecar-specific business logic belongs in the sidecar package itself, for example `../router/`.
- If you change readiness or ops-health semantics, update `core/docs/runbook.md` in the same change.

## Validation
- `go test ./internal/sidecar/...`
- `go test ./cmd/oar-core/...`
