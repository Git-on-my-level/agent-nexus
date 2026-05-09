---
title: "Stop silently dropping corrupt stored JSON list fields"
agent: codex
done: true
ticket_id: "tkt_core_json_list_decode_021"
---

## Triage

Priority: P1. Persistence corruption should surface as an integrity problem, but this change may touch several row-mapping paths and needs focused tests.

## Problem
The shared `decodeJSONListOrEmpty` helper turns any malformed stored JSON list into an empty slice. It is used for board/card/document refs, definition-of-done, and resolution evidence refs, so persistence corruption can be masked as legitimate empty data instead of surfacing an integrity error.

## Evidence
- `core/internal/primitives/docs_store.go:2205` returns `[]string{}` when `json.Unmarshal` fails.
- `core/internal/primitives/boards_store.go:4202` uses the helper for `definition_of_done`, `resolution_refs`, and `refs` when materializing cards.
- `rg -n "func decodeJSONListOrEmpty|decodeJSONListOrEmpty\\(" core/internal/primitives --glob '!**/*_test.go'` shows the helper is reused across boards, cards, documents, and revision mapping.

## Proposed Fix
Replace the silent helper with decode functions that return errors for persisted fields where corruption should fail the read. Thread those errors through row-to-map helpers and list/get methods. Keep an explicit lenient helper only for legacy migration/backfill paths that intentionally tolerate old data, and name it accordingly.

## Validation
- Add a primitives test that corrupts a card/document refs JSON column and asserts the relevant get/list path returns an error instead of empty refs.
- `go test ./internal/primitives -run 'JSON|Refs|Decode|Corrupt'`
- `make -C core check`

## Progress
- Replaced the silent stored JSON-list decoder with `decodeStoredJSONList`, which returns field-qualified errors for corrupt persisted lists.
- Threaded decode errors through document, board, card, and card-revision row materialization and related get/list/update paths.
- Added corruption regression coverage for document `refs_json` and card `refs_json` get/list behavior.
- Validation passed:
  - `go test ./internal/primitives -run 'JSON|Refs|Decode|Corrupt'`
  - `make -C core check`
