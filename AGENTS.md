# AGENTS

## Scope
Root onboarding and index for agents working in this monorepo.

Goals:
- find the right module guide quickly
- preserve cross-component contracts and invariants
- run the right checks before handoff

## Start Here
1. Read [README.md](README.md) for current repo layout and root targets.
2. Identify blast radius: `contracts/`, `core/`, `cli/`, `web-ui/`.
3. Open module guidance before editing behavior:
- [core/AGENTS.md](core/AGENTS.md)
- [cli/AGENTS.md](cli/AGENTS.md)
4. If API/schema may change, read [contracts/README.md](contracts/README.md) first.
5. Plan validation from component scope to repo-level gates.

## Repository Index
- `contracts/`: canonical OpenAPI + schema contracts and generated artifacts.
- `core/`: Go backend/domain implementation (`oar-core`).
- `cli/`: Go CLI (`oar`).
- `web-ui/`: SvelteKit frontend (`oar-ui`).
- `runbooks/`: release and operations workflows.

Primary references:
- [README.md](README.md)
- [contracts/README.md](contracts/README.md)
- [runbooks/release.md](runbooks/release.md)

## Source Of Truth Rules
- Contracts are authoritative: HTTP/API in `contracts/oar-openapi.yaml`, domain/schema in `contracts/oar-schema.yaml`.
- Generated artifacts are derived and must stay reproducible: regenerate with `make contract-gen`, verify drift with `make contract-check`.
- Runtime behavior in `core`, `cli`, and `web-ui` must remain contract-compatible.

## Invariants To Preserve
- Keep generated files aligned with canonical contract sources.
- Preserve CLI machine-facing behavior (non-interactive defaults, stable `--json` flows).
- Preserve core event/snapshot/commitment invariants in [core/AGENTS.md](core/AGENTS.md).
- Preserve cross-component handshake/schema compatibility.

## Change Routing
- Contract/API/schema change: update `contracts/` first, regenerate artifacts, then update consumers in `core`/`cli`/`web-ui`.
- Core behavior change: follow edit/test map in [core/AGENTS.md](core/AGENTS.md).
- CLI behavior/output change: follow module and runtime guidance in [cli/AGENTS.md](cli/AGENTS.md).
- UI integration change: use [web-ui/README.md](web-ui/README.md) and `web-ui/docs/` runbook/spec docs.

## Validation Ladder
Component-scoped checks first:
- `make -C core check`
- `make cli-check`
- `make -C web-ui check`

Contract checks when API/schema is touched:
- `make contract-gen`
- `make contract-check`

Repo-level gates before handoff:
- `make check`
- `make e2e-smoke`

## Operational References
- [runbooks/release.md](runbooks/release.md)
- [core/docs/runbook.md](core/docs/runbook.md)
- [cli/docs/runbook.md](cli/docs/runbook.md)
- [web-ui/docs/runbook.md](web-ui/docs/runbook.md)

## Common Pitfalls
- `make check` is broad; run component checks first for faster diagnosis.
- `make -C core fmt` rewrites files; run intentionally near the end of edits.
- `make serve` runs multiple processes; ensure they are cleaned up after interrupts.
- `pnpm` workspace scope is UI-focused; `core` and `cli` are Go module workflows.

## Handoff Checklist
- Changes follow the right source of truth.
- Relevant component checks pass.
- Contract drift is resolved (if applicable).
- Repo-level gates run for cross-component changes.
- Related docs are updated with behavior/contract changes.

## Cursor Cloud specific instructions

### Environment requirements
- **Go 1.23+** is required (`go.mod` specifies `go 1.23.0`). The default VM ships with Go 1.22; the update script installs Go 1.23.6 to `/usr/local/go`. Ensure `/usr/local/go/bin` is on `PATH`.
- **Node.js 20+** and **pnpm 10.17.1** are pre-installed via nvm and corepack.
- No external services (databases, caches, queues) are needed — the backend uses embedded SQLite.

### Running services
- `make serve` starts the full stack: core on `:8000`, web-ui on `:5173`, and seeds mock data automatically. See `README.md` for toggles (`SEED_CORE`, `FORCE_SEED`).
- Core runs with `OAR_ALLOW_UNAUTHENTICATED_WRITES=1` in dev mode, so API writes work without crypto key auth.
- The web-ui actor gate requires selecting an actor identity on first load; seeded actors include `Zara (OpsAI)`, `SqueezeBot 3000`, `FlavorMind`, `SupplyRover`, and `Till-E`.

### Checks and testing
- Component checks: `make -C core check`, `make cli-check`, `make -C web-ui check` (lint + unit tests; web-ui check skips Playwright e2e by default).
- Full repo gate: `make check` (runs contract-check + all component checks).
- E2E smoke: `make e2e-smoke` (starts fresh core + cli + web-ui and runs a golden-path flow).
- `pnpm` may warn about ignored build scripts for `koffi` and `protobufjs`; these are transitive and do not affect functionality.

### Gotchas
- The Go binary at `/usr/bin/go` may be stale (1.22). Always ensure `/usr/local/go/bin` is first on `PATH`.
- `make serve` uses `trap` + `wait` to manage background processes. If the shell is interrupted, child processes may linger — clean up by PID, never `pkill -f`.
