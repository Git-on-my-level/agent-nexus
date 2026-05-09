---
title: "Fail reads on corrupt agent wakeup refs JSON"
agent: codex
done: true
ticket_id: "tkt_core_agent_wakeup_refs_decode_020"
---

## Triage

Priority: P1. This is the same persistence-corruption class as the completed JSON-list cleanup, but it remains on the wakeup path used by routing and receipts.

## Problem
Agent wakeup rows still silently drop malformed persisted `refs_json`. This repeats the persistence-corruption masking fixed for boards/cards/documents, but on the wakeup notification path used by the router and agent notification receipts.

## Evidence
- `core/internal/primitives/agent_wakeups.go:241`-`:264` scans an agent wakeup row and runs `_ = json.Unmarshal([]byte(refsJSON), &wakeup.Refs)`, so invalid stored JSON becomes an empty `Refs` slice with no error.
- `core/internal/primitives/docs_store.go:2260` now has `decodeStoredJSONList`, which returns a field-qualified error for corrupt persisted JSON lists.
- Existing corruption tests cover document and card refs, but `rg -n "AgentWakeup|refs_json" core/internal/primitives/*_test.go core/internal/server/*_test.go` shows no wakeup `refs_json` corruption regression.

## Proposed Fix
Use the strict stored JSON-list decoder in `scanAgentWakeup`, returning an error such as `decode agent_wakeup.refs` when persisted refs are malformed. Add focused coverage that corrupts `agent_wakeups.refs_json` and asserts `GetAgentWakeup` and list/receipt paths fail instead of returning empty refs.

## Validation
- `go test ./internal/primitives -run 'AgentWakeup|Refs|Decode|Corrupt'`
- `go test ./internal/server -run 'Notification|Receipt|Wakeup|Corrupt'`
- `make -C core check`

## Progress
- Updated `scanAgentWakeup` to decode persisted `refs_json` with `decodeStoredJSONList("agent_wakeup.refs")` and return corruption errors instead of dropping malformed refs.
- Added primitive coverage for corrupt `agent_wakeups.refs_json` on `GetAgentWakeup` and `ListAgentWakeups`.
- Added server coverage that corrupt wakeup refs make notification list and thread timeline receipt reads fail with `internal_error`.
- Validation passed:
  - `go test ./internal/primitives -run 'AgentWakeup|Refs|Decode|Corrupt'`
  - `go test ./internal/server -run 'Notification|Receipt|Wakeup|Corrupt'`
  - `make -C core check`
