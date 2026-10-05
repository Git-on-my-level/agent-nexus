# Contracts

`/contracts` is the canonical contract source of truth for the monorepo.

## Files

- `anx-openapi.yaml`: canonical workspace-core HTTP API contract (`OpenAPI 3.x`) with `x-anx-*` metadata used by CLI/help/doc generators, UI proxy catalog, and TS/Go clients.
- `non-openapi-endpoints.yaml`: explicit registry of **workspace-core** routes that are intentionally absent from `anx-openapi.yaml` (start empty; add entries only when an endpoint truly cannot be described in OpenAPI yet). `core` CI asserts every `registerRoute` using `exactRouteAccess` is covered by **OpenAPI-derived** `contracts/gen/meta/commands.json` **or** this file. Each entry must include `method`, `path_pattern` (OpenAPI-style, `{param}` segments), `owner`, `reason`, and `expected_clients` per the schema comments in the file.
- `anx-schema.yaml`: canonical domain/schema contract currently consumed by core validation.
- `gen/`: generated artifacts committed to source control.

`gen/meta/routes.json` is a versioned machine-readable inventory of every
OpenAPI operation and intentional exception: HTTP method, path template, and
the core router's outer access class. `make route-inventory` generates it from
the contract and the actual mounted route classifiers; `make contract-gen`
also regenerates it. Core CI runs `make route-inventory-check` to catch router
access changes even when the contract is unchanged. These classes describe
core authentication buckets, not downstream authorization rules. Handlers may
apply stricter permissions. `handler_defined` means the outer classifier
defers to the handler; consumers must classify these routes explicitly.

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
- `cli/internal/registry/` (embedded generated metadata for CLI runtime)
- `cli/docs/generated/` (generated command/concept docs)

## x-anx Authoring

`x-anx-*` extension authoring rules are generated at:

- `contracts/gen/docs/x-anx-authoring.md`

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

## Visual reports

`visualreport/` is the shared Go module for complete report validation, live query
parsing, and the Markdown summary projection (`progress.done/total`, `needs[]`).
Core imports it directly; the CLI import surface in `cli/internal/visualreport`
forwards to it. Both modules use a local `replace`, as with `gen/go`.

The browser entry point is `web-ui/src/lib/visualReports.js`. The committed corpus
in `fixtures/visual-reports/` includes static layouts/charts, live queries, numeric
spellings, URLs, ECMAScript whitespace, timestamps, and Markdown fence examples.
Expected outcomes are fixed data, never recalculated by the check. Add regressions
there when extending either validator. `make visualreport-check` runs the Go tests
and compares the production Go and browser validators over every report. Existing
contracts CI runs this gate through `scripts/contract-check`; browser CI compares
both validators through `web-ui/Makefile` on every UI change. Go and Node are
required for the check. Invalid reports stay inspectable as text; the live endpoint
validates the entire envelope before executing queries.

URLs require explicit HTTP(S), no credentials or whitespace, valid percent escapes,
valid IP literals and ports, and canonical numeric IPv4 hosts. Escaped hostnames
are rejected. Integral JSON numbers may use decimal or exponent notation. Report
timestamps support RFC3339Nano (up to nine fractional digits).
