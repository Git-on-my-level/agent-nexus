# AGENTS

## Project purpose
organization-autorunner-core is the core backend/domain implementation for the Organization Autorunner system.

## Canonical references
- System spec: `docs/oar-core-spec.md`
- HTTP contract: `docs/http-api.md`
- Shared schema contract: `../contracts/oar-schema.yaml`
- Spec implementation matrix and known gaps: `docs/spec-compliance.md`
- Runtime and deployment runbook: `docs/runbook.md`

## Architecture map
- Process bootstrap and dependency wiring: `cmd/oar-core/main.go`
- HTTP route composition and shared response helpers: `internal/server/handler.go`
- Request validation and endpoint behavior by domain lives in:
- `internal/server/primitives_handlers.go` (events, artifacts, snapshots read)
- `internal/server/threads_handlers.go`
- `internal/server/commitments_handlers.go`
- `internal/server/packet_validation.go`
- `internal/server/packet_convenience_handlers.go`
- `internal/server/event_reference_validation.go`
- `internal/server/inbox_handlers.go`
- `internal/server/staleness.go`
- Canonical persistence and domain mutation logic: `internal/primitives/store.go`
- Actor registry storage: `internal/actors/store.go`
- Workspace layout and DB migrations: `internal/storage/workspace.go`, `internal/storage/migrations.go`
- Schema loading and validation helpers: `internal/schema/schema.go`, `internal/schema/validator.go`

## System model (implementation reality)
- Canonical state is persisted in SQLite (`events`, `snapshots`, `artifacts`, `actors`) plus artifact content files under `artifacts/content/`.
- Derived inbox/staleness behavior is computed from canonical data in server logic; no scheduler is required for correctness.
- `snapshots.kind` is currently `thread` or `commitment`; typed behavior is implemented as conventions over snapshot bodies.
- Unknown fields are intentionally preserved for snapshot/event/artifact round-tripping.
- Event and artifact refs are typed strings (`prefix:value`) and validated for shape and convention requirements.
- Most mutating endpoints require a registered `actor_id`; actor registration is the bootstrap path.

## Core invariants to preserve
- Append-only events: corrections are new events, not edits.
- Snapshot patch semantics: unspecified fields are preserved; list fields are replaced when present.
- `thread.open_commitments` is core-maintained and must not be user-writable.
- Commitment restricted transitions require evidence refs:
- `status -> done` requires artifact or event evidence refs.
- `status -> canceled` requires event evidence ref.
- Packet kinds (`work_order`, `receipt`, `review`) are schema-validated structured artifacts.
- Packet content ID fields must match `artifact.id`.
- Inbox item IDs are deterministic and acknowledgment suppression must remain stable across rebuilds.
- Staleness exceptions should be idempotent: no duplicate stale exceptions without newer thread activity.

## Change guide (where to edit)
- For schema fields/enums/ref conventions, update:
- `../contracts/oar-schema.yaml`
- `internal/schema/schema.go` (loader normalization)
- `internal/schema/validator.go` (validation behavior)
- corresponding handler/store validations in `internal/server/*.go` and `internal/primitives/store.go`
- For API route/behavior changes, update:
- `internal/server/handler.go` for routing
- domain handler file in `internal/server/`
- `docs/http-api.md` for contract
- `docs/spec-compliance.md` for requirement mapping
- For new persistence behavior, update:
- `internal/primitives/store.go` for domain operations
- `internal/storage/migrations.go` for schema migration
- integration tests in `internal/server/*_integration_test.go`
- For packet convention changes, update:
- `../contracts/oar-schema.yaml` packet + reference conventions
- `internal/server/packet_validation.go`
- `internal/server/packet_convenience_handlers.go` (if convenience endpoint is needed)
- packet integration tests
- For staleness/inbox logic changes, update:
- `internal/server/staleness.go`
- `internal/server/inbox_handlers.go`
- staleness/inbox integration tests

## Operational workflows
- Local dev server: `./scripts/dev`
- Production-like local binary run: `./scripts/run-prod`
- Lint + tests (same as CI): `./scripts/test`
- Fast CI smoke: `./scripts/ci-smoke`
- End-to-end API smoke flow: `./scripts/smoke`
- Container image build/run instructions: `docs/runbook.md` and `Dockerfile`

## Testing map
- Unit/domain store tests: `internal/primitives/store_test.go`, `internal/actors/store_test.go`, `internal/schema/*_test.go`, `internal/storage/workspace_test.go`
- HTTP integration tests by subsystem:
- threads: `internal/server/threads_integration_test.go`
- commitments: `internal/server/commitments_integration_test.go`
- primitives/events/artifacts: `internal/server/primitives_integration_test.go`
- packet flows and validation: `internal/server/packets_integration_test.go`
- inbox/staleness/derived rebuild: `internal/server/inbox_integration_test.go`, `internal/server/staleness_integration_test.go`, `internal/server/derived_rebuild_integration_test.go`
- end-to-end flow: `internal/server/api_comprehensive_integration_test.go`

## Known implementation gaps (track here until closed)
- No dedicated decision convenience endpoint; decisions are currently recorded through `POST /events`.
- `derived_views` table exists in migrations but current derived behavior is computed on demand in server handlers.

## Maintenance guidance
- Keep this file timeless and implementation-oriented.
- Prefer pointers to stable modules/runbooks over transient task notes.
- When behavior changes, update this file alongside:
- relevant docs in `docs/`
- schema contract in `../contracts/oar-schema.yaml` (if shape/rules changed)
- tests that enforce the changed behavior
