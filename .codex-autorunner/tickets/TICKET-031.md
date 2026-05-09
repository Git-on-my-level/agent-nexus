---
title: "Make card board_ref use board handles consistently"
agent: codex
done: true
ticket_id: "tkt_core_card_board_ref_handles_031"
---

## Triage

Priority: P1. Public refs should be handle-first and stable. Emitting internal board ids in `board_ref` creates client drift and weakens the ref contract.

## Problem

Board resources expose refs as `board:<handle>`, but some card response shapes emit `board:<internal id>`. This leaks implementation identity into typed-ref fields and can break clients once board handles diverge from storage ids.

## Evidence

- `core/internal/primitives/boards_store.go:4242`-`:4246` builds board public refs with handle fallback.
- `core/internal/primitives/boards_store.go:4313`-`:4315` builds card `board_ref` from board id.
- `core/internal/primitives/boards_store.go:2902`-`:2905` builds board-membership card `board_ref` from board id.
- `core/internal/primitives/boards_store.go:4373`-`:4375` already uses board handle fallback in card revision shaping.
- `core/internal/server/boards_integration_test.go:817`-`:818` appears to lock in id-based behavior.
- Command used by scout: `nl -ba core/internal/primitives/boards_store.go core/internal/server/boards_integration_test.go`.

## Proposed Fix

Join/select board handle for card row and board-membership card shaping, then emit `board:<handle>` everywhere a public `board_ref` is returned. Preserve internal `board_id` only as debug/admin compatibility where documented. Update tests to use a board whose handle differs from id.

## Validation

- Focused board/card integration test with divergent board id and handle.
- `make -C core check`

## Progress

- Added board-handle selection to card row queries and board-membership shaping.
- Updated public card `board_ref` output to use `board:<handle>` with board id fallback.
- Kept `board_id` in card responses for existing internal/debug compatibility and added `board_handle` alongside it.
- Updated integration coverage for divergent board id/handle card mutation output and thread workspace board memberships.
- Validation passed:
  - `go test ./internal/server -run 'TestBoardsWorkspaceAndThreadWorkspaceMemberships|TestArchiveBoardCardGlobalRoute'`
  - `make -C core check`
