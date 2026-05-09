---
title: "Prevent overlapping Home unread refreshes"
agent: codex
done: true
ticket_id: "tkt_webui_home_poll_stale_060"
---

## Triage

Priority: P1. Stale unread state affects operator attention and mark-read confidence; the fix should be narrow to the Home refresh coordinator.

## Problem

The workspace Home page starts a fixed 30s poll loop with no in-flight guard or stale-response token. If a slow `getHomeUnread()` call overlaps with a manual refresh, mark-read reload, or the next poll tick, an older response can overwrite newer unread state. That is an operator-safety risk because the Home feed may show stale unread counts or resurrect items immediately after an operator marks them read.

## Evidence

- `web-ui/src/routes/o/[organization]/w/[workspace]/+page.svelte:189` assigns `feed = await coreClient.getHomeUnread()` directly in `loadHome()`.
- `web-ui/src/routes/o/[organization]/w/[workspace]/+page.svelte:229`, `:263`, and `:300` call `await loadHome()` after mark-read writes.
- `web-ui/src/routes/o/[organization]/w/[workspace]/+page.svelte:311`-`:313` starts `setInterval(() => loadHome(), POLL_INTERVAL_MS)` without checking whether a prior refresh is still pending.
- Command used: `nl -ba 'web-ui/src/routes/o/[organization]/w/[workspace]/+page.svelte' | sed -n '184,320p'`.

## Proposed Fix

Add a Home refresh coordinator that serializes or versions refreshes. A simple fix is a monotonically increasing request id captured by each `loadHome()` call, with state writes applied only when the id is still current. Also prevent the interval tick from starting a second refresh while one is already in flight, unless explicitly requested by a mark-read flow.

Keep the mark-read paths authoritative: after a successful write, force a fresh reload and ensure any older poll result cannot overwrite it.

## Validation

- Add a focused unit/component test or Playwright route test where the first `/home/unread` response resolves after a later refresh, and assert the UI keeps the newer unread count.
- Run `make -C web-ui check` or the narrow web-ui test command covering the new test.

## Progress

- Added Home refresh request versioning in `web-ui/src/routes/o/[organization]/w/[workspace]/+page.svelte` so stale unread responses cannot overwrite newer state or errors.
- Added an interval-only in-flight guard so the 30s poll skips while an earlier refresh is pending; manual and mark-read refresh paths can still supersede pending poll results.
- Added `web-ui/tests/unit/homeUnreadRefresh.test.js` covering a late stale poll after a newer manual refresh and skipped overlapping interval ticks.

## Completed Validation

- `pnpm run test:unit -- homeUnreadRefresh.test.js` from `web-ui/` passed. Note: with this Vitest config, the filter still collected and ran the full unit suite: 113 files, 741 tests.
- `pnpm exec prettier --check 'src/routes/o/[organization]/w/[workspace]/+page.svelte' tests/unit/homeUnreadRefresh.test.js` from `web-ui/` passed.
- `make -C web-ui check` passed: lint, Svelte rune checks, topic-create call-site check, Prettier, all unit tests, and core proxy path audit.
