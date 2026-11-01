# AGENTS

## Scope

Root onboarding and routing guide for agents working in this monorepo. Use this file for high-level context only, then drill into the relevant module guide for the local rules, invariants, and checks that matter for your change.

- Run `make freshness` before relying on this checkout; stale checkouts mislead agents.
- [docs/map.md](docs/map.md) is the generated map of where things live; run `make map` to regenerate it, never edit it by hand.
- Files over 800 lines may not grow (`scripts/check-file-sizes.py`); split by concern first, then run `make file-size-baseline`.

## Keeping this file short

Guides hold only the rules an agent needs on every task. Past the line limit, move reference detail into docs and leave a one-line pointer saying when to read it, without being asked; `scripts/check-agent-guides.py` enforces this at commit.

## Monorepo Purpose

Agent Nexus is split into modules with different jobs: `contracts/` (canonical shared contract layer that every other module must honor), `core/` (canonical state and evidence service owning durable organizational truth and evidence-safe mutations), `cli/` (agent-first, non-interactive, script-safe, text/JSON-friendly command-line runtime), `web-ui/` (human-operator control surface for glanceable visibility, triage, and explicit human intervention), `adapters/` (independent optional generic provider adapters), and `runbooks/` (operational and release guidance).

## Progressive Discovery

1. Read [README.md](README.md) for repo layout and root targets.
2. Identify blast radius: `contracts/`, `core/`, `cli/`, `web-ui/`, `adapters/`.
3. Open the nearest relevant guide before editing behavior: [contracts/AGENTS.md](contracts/AGENTS.md), [core/AGENTS.md](core/AGENTS.md), [cli/AGENTS.md](cli/AGENTS.md), [web-ui/AGENTS.md](web-ui/AGENTS.md); for the host service in `cli/internal/bridge/`, follow `cli/AGENTS.md`.
4. If a subdirectory has its own `AGENTS.md`, treat it as a narrower local guide that supplements, rather than replaces, the parent module guide.
5. Plan validation from component scope outward to repo-level gates.

## Source Of Truth

- Contracts are authoritative: HTTP/API in `contracts/anx-openapi.yaml`, domain/schema in `contracts/anx-schema.yaml`.
- Generated artifacts are derived outputs. Regenerate with `make contract-gen`. Use `make contract-check` to validate the working tree after generation, and `make contract-check-committed` (or CI) to verify generated files match Git.
- Runtime behavior in `core`, `cli`, `web-ui`, and adapter integrations must remain contract-compatible.

## Cross-Module Boundaries

### Never lose user input

User-authored work must survive navigation, reload, tab close, offline periods, timeouts and uncertain server results. Mutation forms must autosave device-only drafts under workspace, principal and target keys, persist each submitted request before sending, and reuse its client-generated idempotency key until confirmed. Core must atomically record mutations and replay results; do not enable automatic retry on a route until its replay safety is verified. Never clear an unconfirmed request on error or show raw upstream response bodies as ordinary UI copy. Definitive failures keep the text with copy/edit actions; uncertain failures retry with bounded backoff. One tab drains an outbox, and sign-out/principal switches clear device work to prevent shared-machine disclosure. Storage failure must be visible and must prevent sending work that has not been persisted.

The rollout and current coverage are tracked in `web-ui/docs/durable-user-work.md`. New mutation surfaces must follow this rule; existing gaps are rollout work, not permission to add further bypasses.

- `core` is the system of record. Durable truth lives there, not in the CLI or UI.
- `core` may consume only a generic heartbeat publisher env contract (`ANX_HEARTBEAT_PUBLISHER_URL`, `ANX_HEARTBEAT_INTERVAL`, `ANX_HEARTBEAT_AUDIENCE`, `ANX_WORKSPACE_SERVICE_ID`, `ANX_WORKSPACE_SERVICE_PRIVATE_KEY`). This is deployment config only; do not add control-plane-specific imports, URLs, or type coupling.
- `cli` is the automation and agent surface. Preserve deterministic, non-interactive behavior and stable machine-facing output.
- `web-ui` is the human surface. Preserve readability, provenance visibility, and safe human intervention rather than agent orchestration.
- `adapters` own integration-side runtime behavior. Keep install/setup discoverable, but do not move durable truth out of Agent Nexus primitives.
- `contracts` defines the handshake between modules. Change it first when shared behavior or data shape changes.

## Change Routing

- Contract or schema change: start in [contracts/AGENTS.md](contracts/AGENTS.md), regenerate artifacts, then update consumers.
- Core behavior change: follow [core/AGENTS.md](core/AGENTS.md).
- CLI behavior or output change: follow [cli/AGENTS.md](cli/AGENTS.md).
- UI integration or operator workflow change: follow [web-ui/AGENTS.md](web-ui/AGENTS.md).
- Host bridge runtime/install/setup change: follow `cli/AGENTS.md` and `cli/docs/host-bridge.md`.

## Validation Ladder

Run the smallest relevant checks first: `make -C core check`, `make cli-check`, `make -C web-ui check`. When contracts change: `make contract-gen`, then `make contract-check`, and before push/handoff `make contract-check-committed` (or rely on CI). Before handoff: `make test-fast` plus targeted tests for the behavior you changed; CI runs all applicable integration, browser, visual and smoke checks, and `ci-ok` gates merging.

## Before you open a PR

- Keep the OSS repo generic: no hosted/control-plane, personal, or ecosystem-specific logic. Third-party integrations must be optional and documented; setup-specific adapters belong outside this repo.
- List every new or changed HTTP route in the PR description so hosted deployment can classify it.
- Run `make test-fast` plus targeted tests for what you changed. Static checks run at commit; changed-module unit tests run at push, queued under the `scripts/slot` host cap so overlapping pushes on one machine do not turn load into false failures (see [docs/agents/local-checks.md](docs/agents/local-checks.md)). CI runs the full suite and the required `ci-ok` check gates merging.
- For `web-ui`, run only the Playwright specs you changed or affected: `pnpm -C web-ui exec playwright test <spec>`. Leave the complete browser suite to CI.
- Write Playwright tests with `async ({}, testInfo)` destructuring, never `(fixtures, testInfo)`.
- Contracts first: update canonical contracts before implementation, then run `make contract-gen` and `make contract-check-committed`.
- Remove placeholder refs, handles, and paths from shipped code and docs.
- Before pushing, adversarially review correctness, security, contract and back-compat risks, and missing tests.
- After pushing, confirm the pushed head contains every fix claimed in the PR.
- Never skip or retry flaky tests to hide failures; fix the root cause or quarantine the test with a linked issue.
- Describe user-visible behavior before and after, validation, changed routes, and any uncertainty in the PR.

## Per-request cost

For every new or changed read path, including streaming poll ticks, describe its per-request complexity in the PR; exactly what to cover, when to run the scale gate, and budget-entry rules are in [docs/agents/per-request-cost.md](docs/agents/per-request-cost.md). Reviewers must treat hot-path O(workspace) work as a merge-blocking P1, including unbounded materialization, per-row queries, full ownership/graph rebuilds, and read-time text/JSON scans. New or changed hot paths cannot be deferred as a performance follow-up, and budgets are never weakened to make a regression pass. Existing main hazards may use exact, finite checked-in baselines linked to their P1 repair, as documented in `core/docs/performance.md`; remove those entries when repaired.

## Fast loop

Match the command to the change; anything wider belongs to CI.

- Any change, before pushing: `make test-fast`. It picks the affected Go packages and the related Vitest units itself.
- One Go test while iterating: `go test ./internal/<pkg> -run TestName` inside the module (add `-short` to match the fast tier).
- A whole Go module, rarely: `make -C core test` (it sets the package timeout; plain `go test ./...` in `core` has none and will not finish).
- Never run bare `go test ./...` in `core`, and never the full browser suite locally: run only the Playwright specs you changed, `pnpm -C web-ui exec playwright test <spec>`.
- E2E ports are derived per worktree, so parallel checkouts do not share servers. Server reuse is opt-in (`PLAYWRIGHT_REUSE_EXISTING_WEB_UI=1`, `PLAYWRIGHT_REUSE_EXISTING_CORE=1`).
- Do not watch CI. Push, then read `ci-ok` on the PR when it reports; no `gh pr checks --watch`, no polling.
- A commit prints the HTTP routes that changed against `origin/main` (`make check-routes`): copy them into the PR description.

## Local check tiers

Run `make setup` once to install dependencies and `make install-hooks` after moving a worktree; hooks never download dependencies, and missing tooling is a setup error.

- **Pre-commit:** `make check-static` checks staged files (formatting, Go vet/type checks, UI lint, syntax, version metadata, contracts regenerated into scratch) without rewriting your files or running tests; warm-cache budget 30 seconds, `STATIC_ARGS=--all` for all tracked files. `make check-routes` is advisory and never blocks the commit.
- **Pre-push:** `make test-fast` selects changed work from the `origin/main` merge base; idle-machine budget 3 minutes. Heavy phases queue on the `scripts/slot` host cap; `ANX_PREPUSH_FULL=1` checks every module.
- **CI:** the full integration, five-shard browser, visual, build and smoke jobs remain behind `ci-ok`; local budgets do not remove CI coverage.

Full tier mechanics — selection rules, the version-bump short-circuit, slot configuration and fast-tier test tagging: [docs/agents/local-checks.md](docs/agents/local-checks.md).

## References

[README.md](README.md) · [contracts/README.md](contracts/README.md) · [runbooks/release.md](runbooks/release.md) · [core/docs/runbook.md](core/docs/runbook.md) · [cli/docs/runbook.md](cli/docs/runbook.md) · [web-ui/docs/runbook.md](web-ui/docs/runbook.md) · [Host bridge](cli/docs/host-bridge.md)

## Common Pitfalls

- Do not edit generated artifacts by hand when the canonical source is under `contracts/`.
- Do not treat `make check` as the first debugging step; start with component checks.
- Do not move durable state or contract decisions into the CLI or UI layers.
- Do not duplicate module-specific detail here when it belongs in a child `AGENTS.md`.
- Git hooks use `core.hooksPath=scripts/git-hooks` (not `pre-commit install`). Run `make install-hooks` after clone, submodule init, or worktree moves; stale `.git/*/hooks/pre-commit` scripts from an old checkout are ignored once `hooksPath` is set.
