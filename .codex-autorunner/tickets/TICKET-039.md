---
title: "Consolidate workspace resource lifecycle list flows"
agent: codex
done: true
ticket_id: "tkt_webui_shared_lifecycle_list_controller_039"
---

## Triage

Priority: P1. Topics, docs, boards, artifacts, and trash share lifecycle semantics. Duplicated route-local implementations make operator behavior and contract handling drift.

## Problem

Workspace resource list pages each reimplement selection, lifecycle predicates, bulk archive/unarchive/trash, confirmation state, busy flags, reloads, and error handling.

## Evidence

- Large route files: `web-ui/src/routes/o/[organization]/w/[workspace]/topics/+page.svelte` 952 lines, `docs/+page.svelte` 845, `artifacts/+page.svelte` 814, `boards/+page.svelte` 724, and `trash/+page.svelte` 1462.
- `web-ui/src/routes/o/[organization]/w/[workspace]/topics/+page.svelte:378` starts a repeated lifecycle bulk loop.
- `web-ui/src/routes/o/[organization]/w/[workspace]/docs/+page.svelte:339` repeats the pattern.
- `web-ui/src/routes/o/[organization]/w/[workspace]/boards/+page.svelte:284` repeats the pattern.
- `web-ui/src/routes/o/[organization]/w/[workspace]/artifacts/+page.svelte:274` repeats the pattern.
- `web-ui/src/routes/o/[organization]/w/[workspace]/trash/+page.svelte:245` has its own type switch for purge behavior.
- Commands used by scout: `wc -l` on workspace route files; `nl -ba` on the listed route files.

## Proposed Fix

Introduce a shared resource lifecycle controller/helper that takes resource type, id extractor, lifecycle predicates, core actions, and reload callback. Keep route-specific rendering, but centralize lifecycle state transitions, bulk sequencing, confirmation modal state, and error messages.

## Validation

- Focused Playwright coverage for topics, docs, boards, artifacts, and trash lifecycle actions.
- `make -C web-ui check`

## Progress

- Added `web-ui/src/lib/workspaceResourceLifecycle.svelte.js` with shared controllers for archive/unarchive/trash list actions, bulk sequencing, confirmation state, reloads, and error reporting.
- Rewired topics, docs, boards, and artifacts list pages to use the shared lifecycle controller while keeping route-specific rendering and predicates.
- Rewired trash restore/purge dispatch through shared trash lifecycle helpers and kept the existing trash page confirmation flows.
- Added `web-ui/tests/e2e/workspace-resource-lifecycle.spec.js` covering archive, unarchive, and trash behavior across topics, docs, boards, and artifacts; existing trash e2e covers restore/purge behavior.

## Completed Validation

- `pnpm exec playwright test tests/e2e/workspace-resource-lifecycle.spec.js`
- `pnpm exec playwright test tests/e2e/trash.spec.js`
- `make -C web-ui check`
