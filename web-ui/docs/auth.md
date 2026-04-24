# Workspace authentication (web-ui)

This document complements [AGENTS.md](../AGENTS.md) with one map of auth/session behavior and the control-plane provider seam.

## Out-of-workspace mode

`OutOfWorkspaceProvider` is the server-side seam for everything that depends on a control plane.

Signal:
- `ANX_CONTROL_BASE_URL` unset -> `mode: "local"`
- `ANX_CONTROL_BASE_URL` set -> `mode: "hosted"`

Implementation:
- `src/lib/server/outOfWorkspace/index.js`
- `src/lib/server/outOfWorkspace/local.js`
- `src/lib/server/outOfWorkspace/hosted.js`

## Cookies (workspace vs control plane)

- Workspace session cookies:
  - `oar_ui_session_{slug}` (refresh token)
  - `oar_ui_access_{slug}` (access token)
- Control-plane session cookie:
  - `oar_cp_dev_access_token`

Workspace callback routes only write `oar_ui_*`; they never clear the CP cookie.

## Callback/error taxonomy

Canonical callback error codes are surfaced through `src/lib/hosted/callbackErrorCopy.js`.

Notable route behavior:
- Nested callback (`/o/{org}/w/{workspace}/auth/callback`) always resolves by URL params.
- Root callback (`/auth/callback`) resolves by `workspace_id` through `event.locals.outOfWorkspace.resolveWorkspaceById`.
- Root callback unresolved reasons:
  - `control_plane_unavailable`
  - `control_plane_unauthenticated`
  - `workspace_unknown`

## Client session state

`src/lib/authSession.js` owns:
- `authSessionReady`
- `authenticatedAgent`
- `sessionEndedByCp`
- single-flight `initializeAuthSession`

## Known limitation

Refresh replay detection (`REFRESH_REPLAY_WINDOW_MS` in `src/lib/server/authSession.js`) is in-memory per web-ui process.

## Commands

- Unit tests: `pnpm run test:unit`
- Full checks: `pnpm test`

## Related docs

- Provider contract: [out-of-workspace-provider.md](./out-of-workspace-provider.md)
- Test guide: [tests/README.md](../tests/README.md)
