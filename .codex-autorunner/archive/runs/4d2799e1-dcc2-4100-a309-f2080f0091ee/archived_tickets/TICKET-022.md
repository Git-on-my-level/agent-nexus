---
title: "Ignore stale web-ui detail route load results"
agent: codex
done: true
ticket_id: "tkt_webui_detail_route_stale_load_022"
---

## Triage

Priority: P2. This extends the completed inbox-detail stale-load fix to other detail routes where stale state can still appear under a new URL.

## Problem

Several detail routes reload when the route id changes but do not guard late async responses from the previous id. Client-side navigation between detail pages can briefly or permanently apply the previous artifact, document, board workspace, card workspace, content, or error state under the new URL.

## Evidence

- `web-ui/src/routes/o/[organization]/w/[workspace]/artifacts/[artifactId]/+page.svelte:42`-`:45` starts `loadArtifact(id)` from a reactive effect when `artifactId` changes.
- `web-ui/src/routes/o/[organization]/w/[workspace]/artifacts/[artifactId]/+page.svelte:179`-`:218` assigns `loadedArtifactId`, `artifact`, `artifactContent`, `artifactContentType`, `contentLoadError`, and `loading` after `getArtifact()` / `getArtifactContent()` with no stale id check after either await.
- `web-ui/src/routes/o/[organization]/w/[workspace]/docs/[documentId]/+page.svelte:283`-`:286` starts `loadDocument(id)` when `documentId` changes.
- `web-ui/src/routes/o/[organization]/w/[workspace]/docs/[documentId]/+page.svelte:321`-`:345` assigns document, revision, modal/sidebar state, and loading/error state after `getDocument()` without confirming the route still matches.
- `web-ui/src/routes/o/[organization]/w/[workspace]/boards/[boardId]/+page.svelte:292`-`:295` and `:136`-`:146` load and assign board workspace state without a stale `boardId` guard.
- `web-ui/src/routes/o/[organization]/w/[workspace]/boards/[boardId]/cards/[cardId]/+page.svelte:216`-`:219` and `:85`-`:95` load and assign card workspace state without a stale route guard.
- Completed ticket `TICKET-011.md` fixed stale detail-state reuse for inbox detail only; these other detail routes still have the same route-id race.

## Proposed Fix

Use monotonically increasing request ids or captured route keys in each affected detail loader. Reset route-local state before starting a new id load, and apply success, content, error, and loading transitions only when the request still matches the current organization/workspace/id key. Keep mutation-triggered reloads compatible by reusing the same guarded loader.

## Validation

- Add route-state unit tests or Playwright route tests that navigate between two artifact/detail ids while the first response resolves last and assert only the second id's data renders.
- Cover at least one two-step detail load where the shell record resolves before stale content, such as artifact metadata followed by content.
- Run the focused tests and `make -C web-ui check`.

## Progress

- Added guarded route-key/request-id loaders for artifact, document, board, and card detail routes.
- Reset route-local detail state when a new route load starts, while keeping mutation-triggered reloads on the same route compatible.
- Added `web-ui/tests/unit/artifactDetailRouteState.test.js` covering a two-step artifact load where stale content from the previous artifact resolves after navigation.

## Validation Run

- `pnpm vitest run tests/unit/artifactDetailRouteState.test.js tests/unit/inboxDetailRouteState.test.js`
- `make -C web-ui check`
