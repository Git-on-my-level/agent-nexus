# Tech Debt Repay Run

## Objective

Repay targeted architecture debt discovered in the May 2026 codebase sweep. The run should move Agent Nexus closer to a simpler, stricter, contract-led architecture without broad rewrites or unrelated feature work.

## Outcomes

- Tighten contract boundaries so core, CLI, web UI, and adapter behavior match the canonical contracts rather than legacy aliases or duplicated local interpretations.
- Preserve core as the durable source of truth; adapters and clients must not let local state, UI state, or convenience wrappers become authoritative.
- Reduce duplicated parsing, lifecycle handling, command metadata, and client adapter code where repetition is already causing drift or making correctness hard to prove.
- Keep operator and agent surfaces deterministic: CLI commands remain non-interactive and machine-stable; web UI workflows remain explicit, evidence-aware, and core-backed.
- Favor small, verifiable tickets over sweeping refactors. Each ticket should include focused tests and run the narrowest relevant component check before broader validation.

## Scope

This run covers the open CAR tickets beginning at `TICKET-029`. Work is intentionally cross-module, but each ticket should keep its blast radius narrow and follow the nearest module `AGENTS.md`.

Priority order:

1. Correctness and contract enforcement issues that can persist invalid state or lose work.
2. Architecture simplifications that remove duplicated sources of truth.
3. Mechanical cleanup that makes future changes easier to keep aligned.

## Non-Goals

- No product redesigns, new orchestration features, or broad UI restyling.
- No compatibility aliases unless a ticket explicitly documents a remaining migration boundary.
- No generated artifact edits by hand; contract changes start from canonical sources and regenerate outputs.

## Validation

Use component checks first:

- Core/contracts: `make contract-check`, `make -C core check`
- CLI: `make cli-check`
- Web UI: `make -C web-ui check`
- Bridge: `make -C adapters/agent-bridge test`

Use `make check` or `make e2e-smoke` only when the completed ticket crosses module boundaries enough to justify repo-level validation.
