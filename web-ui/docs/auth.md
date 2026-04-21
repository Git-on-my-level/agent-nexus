# Workspace authentication (web-ui)

This document complements [AGENTS.md](../AGENTS.md) with a single map of **modes**, **cookies**, **error codes**, and **tests**.

## Capability modes (`resolveAuthCapabilities`)

| Mode | Typical env | CP workspace-by-id lookup |
|------|-------------|---------------------------|
| `local` | No `ANX_CONTROL_BASE_URL` | No |
| `packed-host-dev` | `ANX_SAAS_PACKED_HOST_DEV` + CP URL | Yes (root `/auth/callback`) |
| `hosted` | CP URL without packed-host flag | No (use full shell path in workspace base URL) |

Implementation: `src/lib/server/authCapabilities.js`.

## Cookies (workspace vs control plane)

- **Workspace session:** `oar_ui_session_{slug}` (refresh), `oar_ui_access_{slug}` (access).
- **Control plane (dev):** `oar_cp_dev_access_token` — **not** cleared when switching workspace; workspace callbacks only touch `oar_ui_*`.

## Error taxonomy

Canonical codes live in `src/lib/authErrorCodes.js` (`AuthErrorCode`). Server routes should use these constants instead of new string literals.

Notable codes:

- `session_ended_by_cp` — terminal; client shows `SessionEndedOverlay`.
- `workspace_resolve_failed` — root callback could not resolve org/workspace slugs.
- `request_loop_detected` — SSR loop short-circuit (`hooks.server.js`).

## Client session state (`src/lib/authSession.js`)

Documented in file header: `authSessionReady`, `authenticatedAgent`, `sessionEndedByCp`, single-flight `initializeAuthSession`, optional `authDriver: "layout"`.

## Known limitations

- **Refresh replay window** (`REFRESH_REPLAY_WINDOW_MS` in `src/lib/server/authSession.js`): in-memory only for the current dev server process; do not “fix” with blind retries. See [AGENTS.md](../AGENTS.md) pointer below.

## Commands

- Unit tests: `pnpm run test:unit`
- Full check: `pnpm test` (lint + unit + e2e)

## Test pointers

See [tests/README.md](../tests/README.md) for callback matrices and fixtures.
