# Contracts

`/contracts` is the canonical contract source of truth for the monorepo.

## Files

- `oar-openapi.yaml`: canonical workspace-core HTTP API contract (`OpenAPI 3.x`) with `x-oar-*` metadata used by CLI/help/doc generators, UI proxy catalog, and TS/Go clients.
- `non-openapi-endpoints.yaml`: explicit registry of **workspace-core** routes that are intentionally absent from `oar-openapi.yaml` (start empty; add entries only when an endpoint truly cannot be described in OpenAPI yet). `core` CI asserts every `registerRoute` using `exactRouteAccess` is covered by **OpenAPI-derived** `contracts/gen/meta/commands.json` **or** this file. Each entry must include `method`, `path_pattern` (OpenAPI-style, `{param}` segments), `owner`, `reason`, and `expected_clients` per the schema comments in the file.
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

Regenerate and validate compilation/tests (staging-safe; does not run `git diff`):

```bash
./scripts/contract-check
```

Assert generated outputs match the repository (same as CI):

```bash
./scripts/contract-check --committed
```

CI runs `contract-check --committed` and fails when artifacts drift.
