# Out-of-workspace provider

`OutOfWorkspaceProvider` is the single server-side boundary for any web-ui behavior that depends on a control plane outside the current workspace process.

## Why this exists

The web-ui previously spread hosted-mode checks across multiple helpers and env flags. That produced inconsistent behavior between resolver paths, login flows, callback routes, and hosted API proxying. This provider collapses all of that behind one mode switch.

## Implementation selection

The standalone build selects `local`. The optional factory in
`src/lib/server/extensions/provider.js` is the single extension point for an
external workspace provider. Composition replaces the factory explicitly;
setting an external service URL cannot import an implementation into OSS.
See [UI extensions](ui-extensions.md).

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

External implementations, including their account routes and components, live
outside this repository. They implement the same provider contract without
introducing service-specific dependencies into core.

## Request plumbing

- `hooks.server.js` sets `event.locals.outOfWorkspace`.
- Call sites should prefer `event.locals.outOfWorkspace`.
- Fallback (`getOutOfWorkspaceProvider(...)`) exists for code paths executed without locals in unit tests.

## OSS safety invariants

- No imports from `controlplane/` into web-ui.
- `local` mode remains inert and safe for self-host.
- `anx-core` API contract is unchanged.
