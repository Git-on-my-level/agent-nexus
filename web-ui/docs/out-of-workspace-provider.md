# Out-of-workspace provider

`OutOfWorkspaceProvider` is the single server-side boundary for any web-ui behavior that depends on a control plane outside the current workspace process.

## Why this exists

The web-ui previously spread hosted-mode checks across multiple helpers and env flags. That produced inconsistent behavior between resolver paths, login flows, callback routes, and hosted API proxying. This provider collapses all of that behind one mode switch.

## Mode selection

One signal decides the implementation:

- `ANX_CONTROL_BASE_URL` unset -> `local` provider
- `ANX_CONTROL_BASE_URL` set -> `hosted` provider

Implementation entrypoint: `src/lib/server/outOfWorkspace/index.js`.

## Contract

Contract typedefs live in `src/lib/server/outOfWorkspace/contract.js`.

Key methods:
- Workspace resolution by slug/id
- Organization workspace listing
- Launch-session begin/exchange
- Hosted sign-in URL construction
- Hosted API proxy (`/hosted/api/*`)
- Shell capability hints (`mode`, account path, CP public origin, empty-static-catalog allowance)

## Implementations

- `local.js`
  - Fully inert, frozen object
  - No control-plane calls
  - Launch-session begin always returns `workspace_native_login`
  - Exchange always returns structured `control_plane_unavailable`

- `hosted.js`
  - Uses `cpClient.js` for control-plane HTTP calls
  - Maps CP rows to resolver/catalog workspace entries
  - Handles launch-session redirects / signin fallback
  - Owns CP session-exchange error canonicalization
  - Enforces hosted API allowlist (`hostedControlPlaneAllowlist.js`)

## Request plumbing

- `hooks.server.js` sets `event.locals.outOfWorkspace`.
- Call sites should prefer `event.locals.outOfWorkspace`.
- Fallback (`getOutOfWorkspaceProvider(...)`) exists for code paths executed without locals in unit tests.

## OSS safety invariants

- No imports from `controlplane/` into web-ui.
- `local` mode remains inert and safe for self-host.
- `anx-core` API contract is unchanged.
