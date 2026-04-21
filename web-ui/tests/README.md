# Web UI tests

## Commands

- `pnpm run test:unit` — Vitest (`tests/unit/**/*.test.js`, including `*.integration.test.js`)
- `pnpm run test:e2e` — Playwright (separate dev server)

## Auth / callback matrix

| Callback route | Workspace resolution |
|----------------|----------------------|
| Nested `POST /o/{org}/w/{workspace}/auth/callback` | Slugs from URL params |
| Root `POST /auth/callback` | `workspace_id` + CP lookup in **packed-host-dev** only (`resolveAuthCapabilities`) |

## Agent vs actor IDs

Fixtures in `tests/fixtures/workspaceAuth.js` include human agent shapes with `agent_id` / `actor_id` combinations used across unit tests.

## Environment

- **Packed-host dev:** tests set `ANX_SAAS_PACKED_HOST_DEV`, `ANX_CONTROL_BASE_URL`, and often `ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN` (or cookie `oar_cp_dev_access_token`).

## Helpers

- `tests/helpers/svelteKitRequestEvent.js` — `createFormPostEvent`, `createGetEvent` for minimal `RequestEvent` stubs.

## Documentation

- Product auth overview: [docs/auth.md](../docs/auth.md)
