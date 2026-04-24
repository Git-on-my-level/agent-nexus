# Web UI tests

## Commands

- `pnpm run test:unit` — Vitest (`tests/unit/**/*.test.js`, including `*.integration.test.js`)
- `pnpm run test:e2e` — Playwright (separate dev server)

## Auth / callback matrix

| Callback route | Workspace resolution |
|----------------|----------------------|
| Nested `POST /o/{org}/w/{workspace}/auth/callback` | Slugs from URL params |
| Root `POST /auth/callback` | `workspace_id` via `event.locals.outOfWorkspace.resolveWorkspaceById` |

## Agent vs actor IDs

Fixtures in `tests/fixtures/workspaceAuth.js` include human agent shapes with `agent_id` / `actor_id` combinations used across unit tests.

## Environment

- Hosted behavior is enabled by `ANX_CONTROL_BASE_URL`.
- CP auth in tests uses env token (`ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN`) or cookie token (`oar_cp_dev_access_token`).
- Most unit tests now inject provider mocks (`mockLocalProvider`, `mockHostedProvider`) through `event.locals.outOfWorkspace` instead of mocking legacy helper modules.

## Helpers

- `tests/helpers/svelteKitRequestEvent.js` — `createFormPostEvent`, `createGetEvent` for minimal `RequestEvent` stubs.

## Documentation

- Product auth overview: [docs/auth.md](../docs/auth.md)
- Provider contract: [docs/out-of-workspace-provider.md](../docs/out-of-workspace-provider.md)
