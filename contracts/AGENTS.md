# AGENTS

## Scope
Guide for work inside `contracts/`.

Read this after the root [AGENTS.md](../AGENTS.md). This module defines the shared compatibility boundary for the monorepo.

## Module Purpose
`contracts` is the canonical contract source of truth for Agent Nexus.

It defines the durable language that `core`, `cli`, and `web-ui` use to interoperate: API shapes, schema rules, typed references, and command metadata inputs. Changes here are cross-module changes by default.

## Contract Responsibilities
- Define the canonical HTTP and schema contracts in source form.
- Express shared compatibility rules that runtime modules must implement consistently.
- Produce deterministic generated artifacts for code generation, help metadata, and published docs.
- Keep contract evolution explicit so downstream modules can update from one authoritative place.

## Invariants
- Canonical sources are `anx-openapi.yaml` and `anx-schema.yaml`.
- Generated artifacts are derived outputs and must remain reproducible.
- Shared behavior changes should be described here before they are implemented in runtime modules.
- Contract changes must preserve or intentionally document compatibility impact across `core`, `cli`, and `web-ui`.

## Canonical References
- Overview: `README.md`
- OpenAPI contract: `anx-openapi.yaml`
- Shared schema: `anx-schema.yaml`
- Generated artifacts: `gen/`

## Change Routing
- Update canonical YAML sources first.
- Regenerate artifacts next.
- Then update consuming modules and their tests.
- If a change is only implementation-deep and does not alter shared behavior, it probably does not belong here.

## Validation
- `make contract-gen`
- `make contract-check` (or `make contract-check-committed` / CI for Git drift)
- Run the relevant consumer checks in `core`, `cli`, and `web-ui` when the shared contract changes.

## Maintenance Guidance
- Keep this file focused on compatibility boundaries and contract ownership.
- Do not duplicate consumer-specific implementation detail here.
- Update this guide when the source-of-truth workflow or compatibility model changes.
