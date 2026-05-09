---
title: "Retire legacy card resolution aliases"
agent: codex
done: true
ticket_id: "tkt_core_retire_card_resolution_aliases_030"
---

## Triage

Priority: P1. Card resolution is contract-defined and currently strict; keeping old aliases alive inside core preserves a pre-contract state model close to durable write paths.

## Problem

The contract exposes `card_resolution` as strict `done`, but core still accepts `completed`, `superseded`, and `unresolved` aliases on create, patch, move, and store normalization paths.

## Evidence

- `contracts/anx-schema.yaml:19`-`:24` defines strict `card_resolution` values with only `done`.
- `contracts/anx-openapi.yaml:4914`-`:4918` exposes only `done`.
- `core/internal/server/board_card_request_parse.go:245`-`:253` aliases old create values.
- `core/internal/server/boards_handlers.go:2227`-`:2234` aliases old patch values.
- `core/internal/primitives/boards_store.go:4623`-`:4632` still maps old values during store normalization.
- Command used by scout: `nl -ba contracts/anx-schema.yaml contracts/anx-openapi.yaml core/internal/server/board_card_request_parse.go core/internal/server/boards_handlers.go core/internal/primitives/boards_store.go`.

## Proposed Fix

Remove alias acceptance from request paths. Keep read-side cleanup only if existing persisted rows need migration, and make that boundary explicit in tests. Add regression coverage that `completed`, `superseded`, and `unresolved` are rejected on create, patch, and move.

## Validation

- Focused board/card create, patch, and move tests.
- `make -C core check`
- `make contract-check` if contract docs or deprecation notes change.

## Progress

- Removed legacy `completed`, `superseded`, and `unresolved` acceptance from card create, patch, move, replay matching, and primitive-store write validation paths.
- Kept legacy cleanup scoped to read-side shaping of already-persisted card rows and open-work calculations.
- Added regression coverage for create, patch, move, and primitive store create/update/move rejection of legacy aliases.
- Validation passed:
  - `go test ./internal/server ./internal/primitives -run 'TestValidateBoardCardCreateResolutionInput|TestBoardCardCreateRejectsInvalidResolutionCombinations|TestBoardCardPatch|TestCardMoveResolutionTransitionsAndEvents|TestBoardStoreMoveCardResolutionTransitions|TestBoardCardIsOpenWorkItem'`
  - `make -C core check`
