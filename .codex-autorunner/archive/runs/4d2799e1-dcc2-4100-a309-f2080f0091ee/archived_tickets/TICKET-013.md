---
title: "Guard topics and threads lists against stale loads"
agent: codex
done: true
ticket_id: "tkt_webui_topics_stale_load_064"
---

## Triage

Priority: P2. This has the same stale-response class as Home, but the impact is list correctness rather than applying writes to the wrong item.

## Problem

The topics/threads list route fires async loads from URL-derived filters, but old responses are allowed to assign `topics`, `backingThreads`, `loading`, and `error` after a newer filter or route state is active. Rapid filter changes or workspace navigation can leave operators looking at stale topic/thread rows under the wrong filter state.

## Evidence

- `web-ui/src/routes/o/[organization]/w/[workspace]/topics/+page.svelte:106`-`:125` reacts to URL/search params and calls either `loadBackingThreads()` or `loadTopicsFromState(parsed)`.
- `web-ui/src/routes/o/[organization]/w/[workspace]/topics/+page.svelte:88`-`:103` assigns `backingThreads = response.threads ?? []` with no request token or route/filter check.
- `web-ui/src/routes/o/[organization]/w/[workspace]/topics/+page.svelte:127`-`:143` assigns `topics = response.topics ?? []` with no request token or route/filter check.
- Command used: `nl -ba 'web-ui/src/routes/o/[organization]/w/[workspace]/topics/+page.svelte' | sed -n '84,150p'`.

## Proposed Fix

Add separate request sequence tokens for topic and thread loads, or a shared current-load key derived from `workspaceSlug`, `listSurface`, and `$page.url.search`. Apply results and loading/error transitions only if the completing request still matches the latest key.

Avoid broad refactors; keep the existing `topicFilters` helpers and list rendering intact.

## Validation

- Add a Playwright route test or a unit-level extracted loader test that resolves an older list response after a newer one and asserts only the newer rows render.
- Run the relevant topics/threads tests and `make -C web-ui check` if practical.

## Progress

- Added a shared monotonic list-load token in `web-ui/src/routes/o/[organization]/w/[workspace]/topics/+page.svelte` so stale topic/thread completions cannot update rows, loading, retrying, or error state after a newer list request starts.
- Changed the threads URL effect to pass parsed URL filter state directly into the thread loader, avoiding a synchronous effect read-after-write of `filters`.
- Added `web-ui/tests/unit/topicListStaleLoad.test.js` covering late stale topic and thread responses after newer filter loads.
- Validation passed:
  - `pnpm vitest run tests/unit/topicListStaleLoad.test.js`
  - `make -C web-ui check`
