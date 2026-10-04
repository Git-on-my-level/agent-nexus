# Workspace authentication (web-ui)

This document complements [AGENTS.md](../AGENTS.md) with one map of auth/session behavior and the control-plane provider seam.

## Out-of-workspace mode

`OutOfWorkspaceProvider` is the server-side seam for everything that depends on a control plane.

The standalone OSS build always selects the inert `local` provider. A composed
application supplies its provider through `src/lib/server/extensions/provider.js`.
Environment variables alone do not add account routes or commercial UI to OSS.
See [UI extensions](ui-extensions.md) for composition and ownership.

Implementation:
- `src/lib/server/outOfWorkspace/index.js`
- `src/lib/server/outOfWorkspace/local.js`

## Cookies (workspace vs control plane)

- Workspace session cookies:
  - `anx_ui_session_{slug}` (refresh token)
  - `anx_ui_access_{slug}` (access token)
- Control-plane session cookie:
  - `anx_cp_dev_access_token`

Workspace callback routes only write `anx_ui_*`; they never clear the CP cookie.

## Callback/error taxonomy

Canonical callback error codes are surfaced through `src/lib/workspaceCallbackErrorCopy.js`.

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
- `sessionEndedByAccountStatus` (terminal session: account disabled or account-status checks failed)
- single-flight `initializeAuthSession`

## Known limitation

Refresh replay detection (`REFRESH_REPLAY_WINDOW_MS` in `src/lib/server/authSession.js`) is in-memory per web-ui process.

## Development / Docker

If a workspace `anx-core` process or container is restarted, its SQLite auth tables are reset while the browser may still hold `anx_ui_*` cookies. Refresh-token exchange then fails until you sign in to the workspace again (or clear those cookies). This is separate from access-token expiry; it can look like “random” 401s after rebuilding or bouncing local cores.

Workspace home (`/o/.../w/...`) runs `initializeAuthSession` (GET `/auth/session`) before fan-out core reads so cookie refresh from the layout is less likely to race parallel API calls.

## Commands

- Unit tests: `pnpm run test:unit`
- Full checks: `pnpm test`

## Related docs

- Provider contract: [out-of-workspace-provider.md](./out-of-workspace-provider.md)
- Test guide: [tests/README.md](../tests/README.md)

## Seamless workspace navigation

Workspace auth state is keyed by organization and workspace. Late responses cannot overwrite another workspace's current identity or resurrect a cleared session. Hosted server loads can supply a checked `workspaceSession.agent`; the shell hydrates from that public row without a second browser auth/handshake waterfall. Actor directories are optional background work. The shell stays mounted; page content is keyed by workspace to prevent stale onMount-only views.

Visible hosted tabs revalidate visited sessions every 60 seconds and on visibility resume. Core access tokens are opaque: `anx_ui_access_{org}__{workspace}_expires` is an httpOnly scheduling hint, never authorization. Two minutes before expiry, the BFF rotates through the existing single-flight refresh path and validates the resulting bearer with core. Missing expiry metadata on old cookies is adopted on their next rotation. The existing process-local refresh replay limitation above still applies.

Overview snapshots live only in browser memory, keyed by organization/workspace/principal, with a 30-second admission TTL and a 12-entry bound. Revisits render and revalidate them; logout or observed revocation clears them. Hidden/offline tabs cannot promise a wall-clock revocation display bound; active connected tabs recheck at most one maintenance interval later, while API authorization remains server-enforced on each request. Hosted layout data is private/no-store, and no credentials are returned to browser JavaScript.
