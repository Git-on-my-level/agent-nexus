# Contracts

`/contracts` is the canonical contract source of truth for the monorepo.

## Files

- `oar-openapi.yaml`: canonical workspace-core HTTP API contract (`OpenAPI 3.x`) with `x-oar-*` metadata used by CLI/help/doc generators.
- `oar-control-openapi.yaml`: canonical SaaS control-plane HTTP contract for organizations, workspace registry, provisioning, launch brokering, and usage envelopes.
- `oar-schema.yaml`: canonical domain/schema contract currently consumed by core validation.
- `gen/`: generated artifacts committed to source control.

## Generation

Generate all contract-derived artifacts from repo root:

```bash
./scripts/contract-gen
```

This writes deterministic outputs under:

- `contracts/gen/go/`
- `contracts/gen/ts/`
- `contracts/gen/meta/`
- `contracts/gen/docs/`
- `contracts/gen/control/go/`
- `contracts/gen/control/ts/`
- `contracts/gen/control/meta/`
- `contracts/gen/control/docs/`
- `cli/internal/registry/` (embedded generated metadata for CLI runtime)
- `cli/docs/generated/` (generated command/concept docs)

## x-oar Authoring

`x-oar-*` extension authoring rules are generated at:

- `contracts/gen/docs/x-oar-authoring.md`

## Drift Check

Validate generated outputs are committed and not stale:

```bash
./scripts/contract-check
```

CI runs the same check and fails when artifacts drift.
