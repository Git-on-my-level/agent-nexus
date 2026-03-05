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
