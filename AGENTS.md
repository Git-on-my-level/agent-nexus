# AGENTS

## Scope
Root onboarding and routing guide for agents working in this monorepo.

Use this file for high-level context only. Then drill into the relevant module guide for the local rules, invariants, and checks that matter for your change.

## Monorepo Purpose
Organization Autorunner is split into a small set of modules with different jobs:

- `contracts/`: canonical shared contract layer. Defines the durable API and schema boundary that every other module must honor.
- `core/`: canonical state and evidence service. Owns durable organizational truth and evidence-safe mutations.
- `cli/`: agent-first command-line runtime. Optimized for non-interactive, script-safe, text/JSON-friendly workflows.
- `web-ui/`: human-operator control surface. Optimized for glanceable visibility, triage, and explicit human intervention.
- `adapters/`: optional runtime integrations that connect external agents or services to OAR.
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
- `cli` is the automation and agent surface. Preserve deterministic, non-interactive behavior and stable machine-facing output.
- `web-ui` is the human surface. Preserve readability, provenance visibility, and safe human intervention rather than agent orchestration.
- `adapters` own integration-side runtime behavior. Keep install/setup discoverable, but do not move durable truth out of OAR primitives.
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

Before handoff on cross-module work:

- `make check`
- `make e2e-smoke`

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
