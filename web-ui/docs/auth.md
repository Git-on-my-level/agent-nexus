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

## When a session is cleared

Every caller (BFF proxy, hosted `/ws` proxy, dev stream proxy, `/auth/session`, `/auth/workspace-session` activation, layout reads, the browser's `/auth/session` reader and the shell bootstrap) uses one rule, `classifyWorkspaceFailure` in `src/lib/workspaceFailure.js` (the server re-exports it as `classifyWorkspaceSessionFailure`):

- **Rejected, session cleared**: **401** (`invalid_token`, `session_ended_by_account_status`, `key_mismatch`, ...) or **403** (`agent_revoked`, ...). On a proxied request the rule applies to the final upstream answer too: any 401 (including one with no refresh token to try) clears the session, and so does a 403 whose code ends the session (`agent_revoked`, `session_ended_by_account_status`, `cp_account_inactive`). A 403 that only refuses one action (`human_required`, `auth_admin_required`, ...) never does. A 401 `invalid_token` from a refresh while a token was held can be a concurrent rotation, so it is kept once and cleared on the second consecutive occurrence (short-lived `anx_ui_auth_retry_*` cookie).
- **Not found, session kept**: **404**, the workspace itself is missing. Proxies and `/auth/session` answer **404 `workspace_not_found`**, the layout renders the "Workspace not found" error page, and the shell shows "This workspace doesn't exist, or it was moved or removed." with a link to the workspace list (no Retry). No session, in this or any other workspace, is cleared.
- **Unavailable, session kept**: everything else, including 5xx (and `auth_session_retryable`), 429, other 4xx, network errors, timeouts, aborts, and malformed or shape-invalid successes (an unparseable body, or `/agents/me` / `/auth/session` without a valid `agent`). A proxy whose pre-refresh failed this way still sends the request without a token, so public routes such as the handshake keep working; if core then answers 401, the refresh-and-retry path answers **503 `auth_session_retryable`** instead of a 401 the browser would read as sign-out. The layout logs `workspace.layout.session_read_deferred` and marks `workspaceSession.sessionReadDeferred`. The shell, including a signed-in viewer whose handshake fails, shows "This workspace is temporarily unreachable" with Retry; the state clears on Retry or the next `online` / visibility resume that succeeds.

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
