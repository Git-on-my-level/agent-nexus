---
title: "Guard remaining web-ui list routes against stale loads"
agent: codex
done: true
ticket_id: "tkt_webui_remaining_list_stale_load_023"
---

## Triage

Priority: P2. This is adjacent to the completed topics-list guard and covers the remaining list surfaces with the same stale-response risk.

## Problem

Several web-ui list routes still let older async loads assign rows, loading flags, and errors after a newer filter, route, or manual reload is active. The topics list already received a stale-response guard, but the same low-risk pattern remains on inbox, boards, docs, and artifacts lists.

## Evidence

- `web-ui/src/routes/o/[organization]/w/[workspace]/inbox/+page.svelte:261`-`:278` assigns open inbox rows and loading/error state after `listInboxItems()` without checking the current URL/reload key.
- `web-ui/src/routes/o/[organization]/w/[workspace]/inbox/+page.svelte:281`-`:317` does the same for completed inbox rows, including pagination cursor state.
- `web-ui/src/routes/o/[organization]/w/[workspace]/boards/+page.svelte:102`-`:122` assigns `boards`, `loading`, and `retrying` after `listBoards()` with no request token.
- `web-ui/src/routes/o/[organization]/w/[workspace]/docs/+page.svelte:132`-`:150` assigns `documents`, `loading`, and `retrying` after `listDocuments()` with no request token.
- `web-ui/src/routes/o/[organization]/w/[workspace]/artifacts/+page.svelte:75`-`:99` reloads from URL search params and assigns `artifacts`, `loading`, and `retrying` after `listArtifacts()` with no stale-result guard.
- Existing completed ticket `TICKET-013.md` fixed this class only for `topics/+page.svelte`, so these are adjacent remaining surfaces rather than duplicates.

## Proposed Fix

Add route/filter-aware request tokens to the remaining list routes. Apply rows, cursors, loading flags, retry flags, and errors only when the completing request still matches the latest workspace, URL/search state, and list generation. Keep pagination append behavior for completed inbox by keying reset and load-more requests separately.

## Validation

- Add focused unit tests or Playwright route tests that resolve an older list response after a newer one for at least inbox completed/open and one resource list route.
- Run the new focused tests.
- Run `make -C web-ui check`.

## Progress

- Added stale-response guards to inbox open and completed list loads in `web-ui/src/routes/o/[organization]/w/[workspace]/inbox/+page.svelte`, including separate completed reset/page tokens so stale load-more responses cannot append into a refreshed completed list.
- Added request guards to boards, docs, and artifacts list routes so late responses cannot assign rows, errors, loading, or retrying state after a newer workspace/filter/search load is active.
- Added `web-ui/tests/unit/remainingListStaleLoad.test.js` covering stale open inbox, completed inbox, and artifact URL-filter responses.
- Validation passed:
  - `pnpm vitest run tests/unit/remainingListStaleLoad.test.js`
  - `make -C web-ui check`
