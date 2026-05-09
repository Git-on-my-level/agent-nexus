---
title: "Reload inbox detail safely when the item id changes"
agent: codex
done: true
ticket_id: "tkt_webui_inbox_detail_route_061"
---

## Triage

Priority: P1. This can cause an operator action to apply stale draft or attachment state to the wrong inbox item, so it leads the UI queue.

## Problem

The inbox detail page loads its item only from `onMount()`. Client-side navigation between two `/inbox/[id]` URLs can reuse the same Svelte component instance, leaving the old item, draft autosave key, attachments, and notification state active for the new route id. The notify-target debounce timer also is not cleared on destroy.

This can cause an operator to respond to or attach evidence against the wrong inbox item after in-app navigation.

## Evidence

- `web-ui/src/routes/o/[organization]/w/[workspace]/inbox/[id]/+page.svelte:206`-`:233` loads the item, but no effect watches `inboxItemID`.
- `web-ui/src/routes/o/[organization]/w/[workspace]/inbox/[id]/+page.svelte:345`-`:350` calls `loadItem()` once in `onMount()` and starts autosave.
- `web-ui/src/routes/o/[organization]/w/[workspace]/inbox/[id]/+page.svelte:185`-`:194` starts `notifyTargetSearchTimer`; `onDestroy()` at `:353`-`:355` clears only `autosaveInterval`.
- `web-ui/src/routes/o/[organization]/w/[workspace]/inbox/[id]/+page.svelte:253` posts to `respondInboxItem(inboxItemID, ...)`, so stale local state combined with a new route param can target the new id with old draft content.
- Command used: `nl -ba 'web-ui/src/routes/o/[organization]/w/[workspace]/inbox/[id]/+page.svelte' | sed -n '176,360p'`.

## Proposed Fix

Replace the one-shot `onMount()` load with a route-id-aware `$effect` or `afterNavigate` guard that:

- reloads when `workspaceSlug` or `inboxItemID` changes,
- resets `responseDraft`, attachment state, target notification state, and errors before applying the new item,
- ignores stale `getInboxItem()` responses from a prior id,
- clears `notifyTargetSearchTimer` in cleanup.

Keep the autosave interval, but make it use the current route key and avoid saving a draft for an item that is no longer loaded.

## Validation

- Add an e2e or component test that navigates from one inbox detail URL to another without full reload and asserts the second page fetches the new item and does not retain the first item's draft/attachments.
- Add coverage that unmount cleanup clears the pending notify search timer.
- Run the relevant inbox tests, then `make -C web-ui check` if practical.

## Progress

- Replaced the one-shot inbox detail load with route-keyed reactive loading for `workspaceSlug` and `inboxItemID`.
- Reset draft, attachment, notify-target, and error state before each route load.
- Added stale async response guards so late `getInboxItem()` responses cannot apply to the current route.
- Kept autosave route-aware by requiring the loaded item route key to match the current route before writing.
- Cleared the notify-target debounce timer when replacing searches, reloading routes, and destroying the component.
- Added `web-ui/tests/unit/inboxDetailRouteState.test.js` coverage for reused component route navigation, attachment/draft reset, stale load suppression, and timer cleanup.

## Validation Run

- `pnpm exec vitest run tests/unit/inboxDetailRouteState.test.js`
- `make -C web-ui check`
