# AGENTS

## Scope

Root onboarding and routing guide for agents working in this monorepo.

Use this file for high-level context only. Then drill into the relevant module guide for the local rules, invariants, and checks that matter for your change.

## Monorepo Purpose

Agent Nexus is split into a small set of modules with different jobs:

- `contracts/`: canonical shared contract layer. Defines the durable API and schema boundary that every other module must honor.
- `core/`: canonical state and evidence service. Owns durable organizational truth and evidence-safe mutations.
- `cli/`: agent-first command-line runtime. Optimized for non-interactive, script-safe, text/JSON-friendly workflows.
- `web-ui/`: human-operator control surface. Optimized for glanceable visibility, triage, and explicit human intervention.
- `adapters/`: first-party generic adapters (agent-bridge).
- `runbooks/`: operational and release guidance.

## Progressive Discovery

1. Read [README.md](README.md) for repo layout and root targets.
2. Identify blast radius: `contracts/`, `core/`, `cli/`, `web-ui/`, `adapters/`.
3. Open the nearest relevant guide before editing behavior:

- [contracts/AGENTS.md](contracts/AGENTS.md)
- [core/AGENTS.md](core/AGENTS.md)
- [cli/AGENTS.md](cli/AGENTS.md)
- [web-ui/AGENTS.md](web-ui/AGENTS.md)
- `adapters/agent-bridge/AGENTS.md`

4. If a subdirectory has its own `AGENTS.md`, treat it as a narrower local guide that supplements, rather than replaces, the parent module guide.
5. Plan validation from component scope outward to repo-level gates.

## Source Of Truth

- Contracts are authoritative: HTTP/API in `contracts/anx-openapi.yaml`, domain/schema in `contracts/anx-schema.yaml`.
- Generated artifacts are derived outputs. Regenerate with `make contract-gen`. Use `make contract-check` to validate the working tree after generation, and `make contract-check-committed` (or CI) to verify generated files match Git.
- Runtime behavior in `core`, `cli`, `web-ui`, and adapter integrations must remain contract-compatible.

## Cross-Module Boundaries

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
- Adapter runtime/install/setup change: follow `adapters/agent-bridge/AGENTS.md`.

## Validation Ladder

Run the smallest relevant checks first:

- `make -C core check`
- `make cli-check`
- `make -C web-ui check`

When contracts change:

- `make contract-gen`
- `make contract-check`
- Before push/handoff: `make contract-check-committed` (or rely on CI)

Before handoff:

- `make test-fast` plus targeted tests for the behavior you changed
- CI runs all applicable integration, browser, visual and smoke checks; `ci-ok` gates merging

## Before you open a PR

- Keep the OSS repo generic: no hosted/control-plane, personal, or ecosystem-specific logic. Third-party integrations must be optional and documented; setup-specific adapters belong outside this repo.
- List every new or changed HTTP route in the PR description so hosted deployment can classify it.
- Run `make test-fast` plus targeted tests for what you changed. Static checks run at commit; changed-module unit tests run at push. CI runs the full suite and the required `ci-ok` check gates merging.
- For `web-ui`, run only the Playwright specs you changed or affected: `pnpm -C web-ui exec playwright test <spec>`. Leave the complete browser suite to CI.
- Write Playwright tests with `async ({}, testInfo)` destructuring, never `(fixtures, testInfo)`.
- Contracts first: update canonical contracts before implementation, then run `make contract-gen` and `make contract-check-committed`.
- Remove placeholder refs, handles, and paths from shipped code and docs.
- Before pushing, adversarially review correctness, security, contract and back-compat risks, and missing tests.
- After pushing, confirm the pushed head contains every fix claimed in the PR.
- Never skip or retry flaky tests to hide failures; fix the root cause or quarantine the test with a linked issue.
- Describe user-visible behavior before and after, validation, changed routes, and any uncertainty in the PR.

## Per-request cost

For every new or changed read path, including streaming poll ticks, describe its
complexity in the PR: what grows with the request/page, what grows with workspace
size, the enforced bounds, and the indexes used before filtering and pagination.
Run the scale gate described in `core/docs/performance.md` when changing SQL,
authorization, projections, streams, startup, or migrations. Add a budget entry
for new reads and justify any exact query-plan exception in the checked-in
allowlist. Do not weaken a budget to make a regression pass.

Reviewers must treat hot-path O(workspace) work as a merge-blocking P1, including
unbounded materialization, per-row queries, full ownership/graph rebuilds, and
read-time text/JSON scans. New or changed hot paths cannot be deferred as a performance follow-up. Existing
main hazards may use exact, finite checked-in baselines linked to their P1 repair,
as documented in `core/docs/performance.md`; remove those entries when repaired.

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

Run `make setup` once to install dependencies and `make install-hooks` after moving a worktree. `make install-hooks` also registers the per-entry merge driver for generated JSON inventories. Hooks never download dependencies; missing tooling is a setup error. Without `.venv`, the hooks run the same checks directly with `python3` instead of failing.

- **Pre-commit:** `make check-static` checks staged files: formatting, Go vet/type checks, UI lint, shell/Python syntax, version metadata, OSS boundary guards when present, and actionlint for workflow edits. It regenerates contracts into scratch space, type-checks their TypeScript client, and compares generated files and mirrors to the index without rewriting your files or running tests. Untracked build inputs must be staged or moved so they cannot mask errors in the committed tree. The standalone staged check rejects unstaged tracked edits; `.venv/bin/pre-commit run --hook-stage pre-commit` safely stashes them as a commit would. `make check-routes` then reports route additions, removals and reclassifications; it is advisory and never blocks the commit. Warm-cache budget: 30 seconds. Use `make check-static STATIC_ARGS=--all` for all tracked files.
- **Pre-push:** `make test-fast` selects work relative to the merge base with `origin/main`, including committed, staged, unstaged and untracked changes. Inside a Go module it runs the changed packages first and then every package that imports them (test imports included), so failures land early; web-ui runs the Vitest units related to the changed files. Module metadata, scripts, shared build/hook/contract roots and anything it cannot attribute to a package widen to the whole module. No merge base means every module, without fetching. When every commit since that base is an exact generated `vX.Y.Z` version bump -- the `scripts/version-managed-files.sh` list, regenerated from each parent and compared by the same detector as the CI short-circuit, over a clean working tree -- it selects nothing, because the release flow gates those commits on the source commit's CI instead. A prerelease version, a hand edit to a listed file, or any uncommitted work runs the normal selection. Use `TEST_FAST_BASE=<ref> make test-fast` to choose a base or `make test-fast TEST_FAST_ARGS=--all` to check every module. Idle-machine budget: 3 minutes.
- **CI:** the existing full integration, five-shard browser, visual, build and smoke jobs remain behind `ci-ok`. Local budgets do not remove CI coverage.

The fast runner uses the CI single-fork Vitest configuration and sets `ANX_TEST_FAST=1` to exclude the Vitest real-binary CLI conformance integration; CI runs it normally.

Go tests that start the full HTTP/storage stack or real sandbox/process integrations must honor `testing.Short()`; real-binary CLI integration already uses the `integration` build tag. Run targeted full tests with `go test ./path/to/package -run TestName` (without `-short`) or `go test -tags=integration ./integration/... -run TestName` inside the module. Small in-process HTTP mocks and store unit tests remain in the fast tier. Keep Playwright in CI except the specific specs you changed or affected.

## References

- [README.md](README.md)
- [contracts/README.md](contracts/README.md)
- [runbooks/release.md](runbooks/release.md)
- [core/docs/runbook.md](core/docs/runbook.md)
- [cli/docs/runbook.md](cli/docs/runbook.md)
- [web-ui/docs/runbook.md](web-ui/docs/runbook.md)
- `adapters/agent-bridge/README.md`

## Common Pitfalls

- Do not edit generated artifacts by hand when the canonical source is under `contracts/`.
- Do not treat `make check` as the first debugging step; start with component checks.
- Do not move durable state or contract decisions into the CLI or UI layers.
- Do not duplicate module-specific detail here when it belongs in a child `AGENTS.md`.
- Git hooks use `core.hooksPath=scripts/git-hooks` (not `pre-commit install`). Run `make install-hooks` after clone, submodule init, or worktree moves; stale `.git/*/hooks/pre-commit` scripts from an old checkout are ignored once `hooksPath` is set.
