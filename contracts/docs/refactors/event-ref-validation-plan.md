# Contract-Driven Event Reference Validation Plan

## Scope
This plan defines the smallest durable path to remove hand-maintained event-reference validation drift across:

- `contracts/oar-schema.yaml` (canonical contract),
- `core/internal/schema` + `core/internal/server` (authoritative runtime validation),
- `cli/internal/app/resource_commands.go` (create/update command entrypoints),
- `web-ui` mock/form validation paths.

Goal: one canonical rule source in contract data, consumed consistently by core, CLI, and web-ui.

## Current Duplication Map

| Surface | Current behavior | Drift/risk |
|---|---|---|
| `contracts/oar-schema.yaml` | Declares `reference_conventions.event_refs` and open `event_type` enum policy. | Canonical source exists, but part of conditional logic is prose (`refs_conditional`) not machine-structured. |
| `core/internal/schema/schema.go` | `rawEventRefConventions` is a hardcoded struct with one field per event type; `Load()` manually copies each known event key into `contract.EventRefRules`. | Every new event rule requires code edits in core schema loader, creating avoidable key drift. |
| `core/internal/server/event_reference_validation.go` | Applies shared required checks, but still carries hardcoded conditional branches for status-transition events (`done`, `canceled`). | Conditional rules are duplicated in code instead of being data-driven. |
| `web-ui/src/lib/commitmentUtils.js` | `validateCommitmentStatusTransition(...)` hardcodes the same `done/canceled` reference constraints and user-facing messages. | UI can drift from core when conditional rule semantics change. |
| `web-ui/src/routes/events/+server.js` (mock mode) | Hand-checks only one rule (`decision_made` requires `thread_id`). | Mock event validation only partially mirrors core conventions. |
| `cli/internal/app/resource_commands.go` | No local event-reference rule map; `events create` forwards payloads to core. | No duplication today, but no shared rule consumption either; adding CLI-side hints/validation later risks ad hoc maps unless a shared source is added now. |

Correction from ticket seed: `web-ui/src/lib/eventValidation.js` is not present in this repo; active UI rule duplication is in `web-ui/src/lib/commitmentUtils.js`.

## Chosen Architecture

### 1) Canonical source remains schema, with structured conditionals

Keep `contracts/oar-schema.yaml` as source of truth and add machine-readable conditional refs under `reference_conventions.event_refs` (while retaining prose `refs_conditional` for docs/readability).

Planned schema shape addition (illustrative):

- `refs_when_payload` array per event rule.
- Each entry includes:
  - `field` (payload key),
  - `equals` (trigger value),
  - `any_ref_prefixes` and/or `all_ref_prefixes`,
  - optional `error_code` for stable cross-surface messaging keys.

This removes hardcoded status-transition conditions from core/web-ui code.

### 2) Generate normalized event-ref metadata from schema

Extend `core/cmd/contract-gen` to emit:

- `contracts/gen/meta/event_ref_rules.json`

with normalized, machine-oriented data:

- `event_type_open_enum: true|false`,
- `event_ref_rules` keyed by event type,
- normalized booleans/arrays (`thread_id_required`, `refs_must_include_prefix_counts`, `payload_must_include_keys`),
- structured conditional rules (`refs_when_payload`),
- optional `error_code` fields.

### 3) Runtime consumption strategy by surface

- Core:
  - Stop hand-wiring event rule keys in `schema.Load()` by switching to map-based decoding for `event_refs`.
  - Update event validator to evaluate conditionals from rule data (remove hardcoded status-transition branch).
- CLI:
  - Add a lightweight preflight validator for `events create` that consumes generated rules via `cli/internal/registry` embedded JSON.
  - Keep core as authority; CLI preflight is best-effort and preserves open-enum pass-through.
- Web UI:
  - Replace hardcoded commitment transition checks with generated-rule-based helper.
  - Reuse same helper in mock `POST /events` route so mock and form checks use the same contract-derived rules.

## Compatibility Constraints

- Open enum behavior must remain intact:
  - Unknown `event.type` values are allowed in core, CLI preflight, and web-ui preflight.
  - No generated artifact may assume exhaustive event-type lists.
- Validation strictness must not weaken for known rules:
  - Existing required `thread_id`, `refs_must_include`, and conditional `done/canceled` checks remain enforced.
- Error messaging compatibility:
  - Core API error payload shape remains unchanged.
  - Existing integration tests that assert key message substrings should continue passing.
  - CLI/web-ui may render friendlier local messages, but core-response wording remains the contract for server errors.
- Backward compatibility for human-readable schema docs:
  - Keep prose `refs_conditional`; add structured fields alongside it.

## Migration Order

1. Add structured conditional fields in `contracts/oar-schema.yaml` and document rule semantics.
2. Extend `contract-gen` to emit `event_ref_rules.json` and copy/embed it for CLI registry.
3. Refactor core schema loading to map-based event rule ingestion (no per-event hardcoding).
4. Refactor core event validator to data-driven conditional rule evaluation.
5. Add CLI preflight validator backed by embedded generated event rules.
6. Replace web-ui hardcoded commitment/mock event checks with generated-rule helper.
7. Add/update cross-surface parity tests and run focused suites.

## Exact Files To Touch (Implementation Ticket)

Contracts/generation:

- `contracts/oar-schema.yaml`
- `core/cmd/contract-gen/main.go`
- `scripts/contract-gen`
- `contracts/gen/meta/event_ref_rules.json` (generated)
- `cli/internal/registry/event_ref_rules.json` (generated copy)

Core:

- `core/internal/schema/schema.go`
- `core/internal/schema/contract_test.go`
- `core/internal/server/event_reference_validation.go`
- `core/internal/server/event_reference_conventions_integration_test.go`

CLI:

- `cli/internal/registry/registry.go`
- `cli/internal/registry/registry_test.go`
- `cli/internal/app/resource_commands.go`
- `cli/internal/app/resource_commands_test.go` (or nearest command-behavior test file)

Web UI:

- `web-ui/src/lib/commitmentUtils.js`
- `web-ui/src/routes/events/+server.js`
- `web-ui/src/lib/eventRefRules.js` (new)
- `web-ui/tests/unit/commitmentUtils.test.js`
- `web-ui/tests/unit/eventRefRules.test.js` (new)
- `web-ui/tests/unit/mockCoreData.test.js` (extend for mock event-validation parity)

## Tests To Add/Update

Contracts/gen:

- `make contract-gen`
- `make contract-check`

Core:

- `cd core && go test ./internal/schema/...`
- `cd core && go test ./internal/server/... -run EventReference`

CLI:

- `cd cli && go test ./internal/registry/...`
- `cd cli && go test ./internal/app/... -run EventsCreate`

Web UI:

- `pnpm -C web-ui exec vitest run tests/unit/commitmentUtils.test.js tests/unit/eventRefRules.test.js tests/unit/mockCoreData.test.js`

## Explicit Non-Goals

- Changing the server-side authority model (core remains final validator).
- Reworking unrelated event rendering/UI labeling behavior.
- Introducing a broad new API endpoint for this refactor.

## Alternatives Considered and Rejected

1. Keep hardcoded conditional logic in core/web-ui and only document it better.
Rejected: does not remove multi-surface drift or repeated manual edits.

2. Parse `refs_conditional` prose strings at runtime.
Rejected: brittle and hard to test; prose should stay human-readable, not executable.

3. Add no CLI consumption because CLI currently has no local rule map.
Rejected: leaves future CLI validation/hinting path without guardrails and invites new ad hoc maps.

4. Move all validation exclusively to frontend/CLI and simplify core checks.
Rejected: weakens authoritative backend validation guarantees.
