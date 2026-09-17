# Web UI tests

## Commands

- `pnpm run test:unit` — Vitest (`tests/unit/**/*.test.js`, including `*.integration.test.js`)
- `pnpm run test:e2e` — Playwright (separate dev server)

- `pnpm run test:layout` — only the rendering audits (every `*-states.spec.js` plus `layout-sweep.spec.js`)

No bundled Chromium? Run against an installed browser: `PLAYWRIGHT_CHANNEL=chrome pnpm run test:e2e`.

## Rendering audits

`tests/helpers/layoutAudit.js` asserts layout invariants from page geometry instead of comparing screenshots, so it can run after every UI transition without baselines:

| Check              | Catches                                                                                             |
| ------------------ | --------------------------------------------------------------------------------------------------- |
| `text-overlap`     | Text painted over other text with no opaque layer between (floating banner with a translucent fill) |
| `occluded-control` | A non-modal floating layer covering a button, link or input                                         |
| `page-overflow-x`  | Horizontal page scroll                                                                              |
| `text-spill`       | Text escaping its bordered/filled box (long ids, tokens, emails)                                    |
| `fixed-offscreen`  | A fixed layer partly outside the viewport                                                           |

- `*-states.spec.js` — one per app area. Each drives its pages through every reachable state (loading, empty, errors, in-flight submits, modals, popovers, banners, long content) at each `AUDIT_VIEWPORTS` size and calls `expectCleanLayout(page, "state name")` after each transition. `access-states.spec.js` is the reference pattern: a mutable mock API with `hold.<endpoint>` (in-flight) and `fail.<endpoint>` (error) switches.
- `layout-sweep.spec.js` — audits every `QA_SCENES` entry from `scripts/qa-visual.mjs`; new QA scenes are covered automatically.
- `LAYOUT_AUDIT_SCREENSHOTS=<dir>` saves a PNG of every audited state for eyeballing what geometry cannot judge.
- When adding UI: add its states to the area spec. Prefer in-flow content or a real modal over floating layers; `pnpm run lint` rejects `fixed` layers whose only background is translucent (`scripts/check-floating-layers.mjs`).
- A false positive can be scoped out with `expectCleanLayout(page, label, { ignore: ["<selector>"] })` or `data-layout-audit-ignore`; leave a comment saying why.

## Auth / callback matrix

| Callback route                                     | Workspace resolution                                                  |
| -------------------------------------------------- | --------------------------------------------------------------------- |
| Nested `POST /o/{org}/w/{workspace}/auth/callback` | Slugs from URL params                                                 |
| Root `POST /auth/callback`                         | `workspace_id` via `event.locals.outOfWorkspace.resolveWorkspaceById` |

## Agent vs actor IDs

Fixtures in `tests/fixtures/workspaceAuth.js` include human agent shapes with `agent_id` / `actor_id` combinations used across unit tests.

## Environment

- Hosted behavior is enabled by `ANX_CONTROL_BASE_URL`.
- CP auth in tests uses env token (`ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN`) or cookie token (`anx_cp_dev_access_token`).
- Most unit tests now inject provider mocks (`mockLocalProvider`, `mockHostedProvider`) through `event.locals.outOfWorkspace` instead of mocking legacy helper modules.

- `ANX_UI_DISABLE_REQUEST_LOOP_GUARD=1` (set by `playwright.config.js`) turns off the per-URL navigation-loop guard in `hooks.server.js`; parallel workers loading the same page would otherwise get `request_loop_detected`.

## Helpers

- `tests/helpers/pageReady.js` — `waitForAppReady(page)`; use it instead of `waitForLoadState("networkidle")`, which never settles on pages holding the live event stream.
- `tests/helpers/svelteKitRequestEvent.js` — `createFormPostEvent`, `createGetEvent` for minimal `RequestEvent` stubs.

## Documentation

- Product auth overview: [docs/auth.md](../docs/auth.md)
- Provider contract: [docs/out-of-workspace-provider.md](../docs/out-of-workspace-provider.md)
