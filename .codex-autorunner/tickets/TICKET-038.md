---
title: "Extract workspace shell auth bootstrap from root layout"
agent: codex
done: true
ticket_id: "tkt_webui_extract_workspace_bootstrap_038"
---

## Triage

Priority: P1. Auth/session bootstrap is central to every operator workflow. The current root layout mixes too many responsibilities, making fetch and redirect loops hard to reason about.

## Problem

The root layout owns shell rendering, workspace routing, hosted session loading, dev persona session creation, actor registry hydration, redirect-loop prevention, and identity gate behavior in one large Svelte file.

## Evidence

- `web-ui/src/routes/+layout.svelte` is over 1000 lines and owns both shell UI and auth bootstrap.
- `web-ui/src/routes/+layout.svelte:171` and `:260` split login redirect derived state from an imperative duplicate check.
- `web-ui/src/routes/+layout.svelte:298` installs redirect/fetch loop guards from the layout.
- `web-ui/src/routes/+layout.svelte:449` and `:532` embed dev persona session and workspace hydration.
- Commands used by scout: `wc -l web-ui/src/routes/+layout.svelte`; `nl -ba web-ui/src/routes/+layout.svelte`.

## Proposed Fix

Move workspace bootstrap and auth identity orchestration into a dedicated `$lib/workspaceBootstrap` controller or Svelte store with explicit states: unresolved, hydrating, anonymous-dev, authenticated-human, authenticated-agent, login-required, and failed. Keep the layout as a renderer of that state.

## Validation

- Existing unit coverage around hooks/session plus `tests/unit/sessionEndedSurface.test.js`, `workspaceLayoutServer*`, and `hooksLoopDetection`.
- Playwright `shell.spec.js`, `access.spec.js`, and `csp.spec.js`.
- `make -C web-ui check`

## Progress

- Extracted auth/session redirect guards, explicit bootstrap state classification, dev persona session bootstrap, actor registry reconciliation, and principal hydration into `web-ui/src/lib/workspaceBootstrap.js`.
- Kept `web-ui/src/routes/+layout.svelte` focused on shell state/rendering and UI actions, delegating bootstrap work to the new module.
- Added `web-ui/tests/unit/workspaceBootstrap.test.js` for bootstrap state classification, login redirect destination construction, and principal merging.

## Completed Validation

- `pnpm exec vitest run tests/unit/workspaceBootstrap.test.js`
- `make -C web-ui check`
- `pnpm exec playwright test tests/e2e/shell.spec.js tests/e2e/access.spec.js tests/e2e/csp.spec.js`
