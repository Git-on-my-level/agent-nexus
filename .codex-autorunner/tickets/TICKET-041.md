---
title: "Centralize lifecycle state as a contract enum"
agent: codex
done: true
ticket_id: "tkt_contract_lifecycle_state_enum_041"
---

## Triage

Priority: P2. `active|archived|trashed` is a cross-resource concept. Keeping it repeated in prose and local helpers makes future lifecycle behavior easy to drift.

## Problem

Lifecycle state values are repeated across OpenAPI descriptions, schema prose, and core hard-coded helpers, but they are not a first-class shared schema enum.

## Evidence

- `contracts/anx-openapi.yaml:124`-`:129`, `:552`-`:557`, `:1190`-`:1195`, and `:1704`-`:1708` repeat lifecycle enum values.
- `contracts/anx-schema.yaml:288`-`:289`, `:350`-`:351`, and `:403`-`:406` describe lifecycle values in prose, but no `lifecycle_state` enum exists near `contracts/anx-schema.yaml:10`-`:99`.
- `core/internal/primitives/lifecycle_list_query.go:7`-`:14` hard-codes filter order.
- `core/internal/primitives/lifecycle_state.go:8`-`:27` derives lifecycle state separately.
- Command used by scout: `nl -ba contracts/anx-openapi.yaml contracts/anx-schema.yaml core/internal/primitives/lifecycle_list_query.go core/internal/primitives/lifecycle_state.go`.

## Proposed Fix

Add `lifecycle_state` to `contracts/anx-schema.yaml`, reference it from resource state fields, and have OpenAPI/generated metadata mirror it. Replace core's local state list with a schema-backed constant or generated taxonomy check.

## Validation

- `make contract-gen`
- `make contract-check`
- `make -C core check`

## Progress

- Added canonical `lifecycle_state` strict enum to `contracts/anx-schema.yaml`.
- Referenced `enums.lifecycle_state` from thread/topic/document/board `state` fields.
- Added OpenAPI `LifecycleState` schema and reused it for lifecycle list filters and state fields.
- Regenerated contract taxonomy metadata for contracts, CLI, and web UI.
- Centralized core lifecycle constants in `core/internal/primitives` and added a contract-backed test to catch drift.

## Validation Results

- `make contract-gen` passed.
- `make contract-check` passed.
- `go test ./internal/primitives -run 'TestLifecycle|TestBuildListDocumentsQueryStateFilter'` passed.
- `make -C core check` passed.
