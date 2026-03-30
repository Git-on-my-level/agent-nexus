# oar-ui

This package contains the SvelteKit web UI for Organization Autorunner.

- `docs/`: operator runbooks and spec/compliance notes
- `/contracts/oar-schema.yaml`: shared schema contract (`0.2.3`)
- `/contracts/gen/ts/client.ts`: generated TS API client consumed by `web-ui`

## Runtime model

`oar-ui` now assumes workspace-aware proxying through the UI server.

- Canonical config: `OAR_WORKSPACES`
  - JSON array or object mapping `workspace slug -> core base URL`
  - Optional per-workspace `publicOrigin`/`public_origin` keeps copied links and
    registration snippets pinned to the externally reachable workspace URL when
    the UI itself is reverse-proxied or seen as loopback internally
  - Used directly for self-host and local/dev deployments
  - Example:

    ```bash
    export OAR_WORKSPACES='[
      {"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000","publicOrigin":"https://oar.tailnet.ts.net/oar/local"},
      {"slug":"ops","label":"Ops","coreBaseUrl":"http://127.0.0.1:8001","publicOrigin":"https://oar.tailnet.ts.net/oar/ops"}
    ]'
    export OAR_DEFAULT_WORKSPACE=local
    ```

  - Legacy aliases (deprecated): `OAR_PROJECTS` and `OAR_DEFAULT_PROJECT` still work if the new names are absent.

- SaaS packed-host routing:
  - Signed-in control-plane sessions can resolve workspace slugs dynamically from the control plane.
  - This lets newly created SaaS workspaces become reachable without editing `OAR_WORKSPACES` or restarting `oar-ui`.
  - `OAR_WORKSPACES` remains the fallback when no control-plane session is present.
  - Control-plane workspace routing is cached briefly in memory inside the UI server to avoid a control-plane lookup on every proxied request.

- UI routes are workspace-prefixed: `/:workspace/...`
  - Examples: `/local`, `/local/inbox`, `/ops/threads/thread-123`
  - `/` redirects to the default workspace.
  - Legacy root page routes (`/threads`, `/inbox`, etc.) redirect to the default
    workspace for convenience.
- Optional external mount prefix: `OAR_UI_BASE_PATH=/oar`
  - External routes become `/oar/:workspace/...`
  - Build/dev the UI with the same base path you plan to serve
  - Put build-time values in `web-ui/.env.build` or override them via shell env
  - Reverse proxies should preserve the prefix instead of stripping it

- The SvelteKit server resolves proxied API traffic from the active workspace
  context and forwards requests to the matching `oar-core`.

- Single-core fallback still works for local/dev:
  - `OAR_CORE_BASE_URL=http://127.0.0.1:8000`
  - This synthesizes one default `local` workspace when `OAR_WORKSPACES` is unset.

See `docs/runbook.md` for deployment examples, auth/session behavior, and
WebAuthn constraints.

## Production serving

`./scripts/build` produces a Node.js server (`ADAPTER=node` by default).
`svelte.config.js` reads `web-ui/.env.build` and `web-ui/.env.build.local` at
startup, with shell env taking precedence over file values.
`./scripts/serve` starts it with `node build/index.js`. Do not use
`vite preview` for production or reverse-proxied deployments -- it does not
execute SvelteKit server hooks, so API proxying and bootstrap endpoints will
return empty responses.

See `docs/runbook.md` for reverse proxy configuration and deployment examples.

## Startup compatibility

On workspace route startup the UI calls `GET /meta/handshake` (falling back to
`GET /version`) through the workspace-aware proxy and requires
`schema_version === "0.2.3"`.

## Quick smoke check

```bash
env | rg '^(OAR_WORKSPACES|OAR_DEFAULT_WORKSPACE|OAR_CORE_BASE_URL)='
curl -fsS http://127.0.0.1:8000/meta/handshake
curl -fsS -H 'x-oar-workspace-slug: local' http://127.0.0.1:5173/meta/handshake
```

The first `curl` checks `oar-core` directly. The second checks the UI proxy path
for one workspace.

## Integration E2E with real oar-core

The repo includes `./scripts/e2e-with-core` for a headless golden-path run
against a real `oar-core`.

Default local run:

Terminal A (backend):

```bash
cd ../core
./scripts/dev
```

Terminal B (ui):

```bash
cd ../web-ui
OAR_WORKSPACES='[{"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"}]' \
OAR_DEFAULT_WORKSPACE=local \
./scripts/e2e-with-core
```
