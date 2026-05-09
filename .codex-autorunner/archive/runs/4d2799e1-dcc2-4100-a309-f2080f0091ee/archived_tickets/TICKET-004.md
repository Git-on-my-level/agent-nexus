---
title: "Validate and clean up non-OpenAPI endpoint registry"
agent: codex
done: true
ticket_id: "tkt_non_openapi_registry_validation_003"
---

## Triage

Priority: P1. This merges the stale usage-summary exception with the missing registry validation because both are the same contract-safety gap.

## Problem

The non-OpenAPI endpoint registry documents required metadata fields, but the existing parity test only decodes and uses `method` and `path_pattern`. Missing `owner`, blank `reason`, invalid `expected_clients` values, duplicates, or entries for routes that are already covered by OpenAPI would not fail the contract safety check.

The current registry already contains a stale exception for `GET /v1/usage/summary`, even though that route is documented in OpenAPI and appears in generated command metadata. That weakens the registry's signal as the list of intentionally non-OpenAPI routes.

## Evidence

- `contracts/non-openapi-endpoints.yaml:4-14` says each entry must include `method`, `path_pattern`, `owner`, `reason`, and `expected_clients`, and documents allowed `expected_clients` values.
- `contracts/non-openapi-endpoints.yaml:18-24` lists `GET /v1/usage/summary` and says the route is documented in OpenAPI while a temporary route-parity exception is being realigned.
- `contracts/anx-openapi.yaml:3203-3218` defines `GET /v1/usage/summary` with `operationId: getUsageSummaryV1` and `x-anx-command-id: usage.summary.v1`.
- `contracts/gen/meta/commands.json` includes `usage.summary.v1` for `GET /v1/usage/summary`.
- `core/internal/server/route_openapi_parity_test.go:24-28` defines the decoded exception entries with only `Method` and `PathPattern`.
- `core/internal/server/route_openapi_parity_test.go:54-62` reads and decodes the YAML, but no schema validation checks the documented metadata fields.
- Commands used:
  - `nl -ba contracts/non-openapi-endpoints.yaml | sed -n '1,55p'`
  - `nl -ba contracts/anx-openapi.yaml | sed -n '3198,3220p'`
  - `rg -n "usage.summary.v1|/v1/usage/summary" contracts/gen/meta/commands.json`
  - `nl -ba core/internal/server/route_openapi_parity_test.go | sed -n '20,70p'`

## Proposed Fix

Remove the stale `GET /v1/usage/summary` entry from `contracts/non-openapi-endpoints.yaml`.

Add focused validation for `contracts/non-openapi-endpoints.yaml` either in the parity test or a small contract-lint test/script:

- Require non-empty `method`, `path_pattern`, `owner`, `reason`, and `expected_clients`.
- Restrict `expected_clients` to `none`, `server-only`, or `operator-cli`.
- Reject duplicate `method + path_pattern` entries.
- Reject exception entries that are already covered by OpenAPI route metadata.

## Validation

- Run the focused test or linter added for `contracts/non-openapi-endpoints.yaml`.
- Run `make -C core check` to ensure route parity still passes.
- Run `./scripts/contract-check --committed` if generated contract artifacts are touched.

## Completion Notes

- Removed stale `GET /v1/usage/summary` from `contracts/non-openapi-endpoints.yaml`; the route is already covered by OpenAPI-derived command metadata.
- Extended `core/internal/server/route_openapi_parity_test.go` to validate required registry metadata, allowed `expected_clients`, duplicate `method + path_pattern` entries, and entries already covered by OpenAPI command metadata.
- Validation run:
  - `go test ./internal/server -run 'TestExactRegisterRoutesCoveredByOpenAPOrExceptions'`
  - `make -C core check`
- Did not run `./scripts/contract-check --committed` because no generated contract artifacts were touched.
