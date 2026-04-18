# anx-ui

This package contains the SvelteKit web UI for Organization Autorunner.

- `docs/`: operator runbooks and spec/compliance notes
- `/contracts/anx-schema.yaml`: shared schema contract (`0.2.3`)
- `/contracts/gen/ts/client.ts`: generated TS API client consumed by `web-ui`

## Runtime model

`anx-ui` now assumes workspace-aware proxying through the UI server.

- Canonical config: `ANX_WORKSPACES`
  - JSON array or object mapping `workspace slug -> core base URL`
  - Optional per-workspace `publicOrigin`/`public_origin` keeps copied links and
    registration snippets pinned to the externally reachable workspace URL when
    the UI itself is reverse-proxied or seen as loopback internally
  - Used directly for self-host and local/dev deployments
  - Example:

    ```bash
    export ANX_WORKSPACES='[
      {"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000","publicOrigin":"https://anx.tailnet.ts.net/anx/local"},
      {"slug":"ops","label":"Ops","coreBaseUrl":"http://127.0.0.1:8001","publicOrigin":"https://anx.tailnet.ts.net/anx/ops"}
    ]'
    export ANX_DEFAULT_WORKSPACE=local
    ```

  - Legacy aliases (deprecated): `ANX_PROJECTS` and `ANX_DEFAULT_PROJECT` still work if the new names are absent.

- UI routes are workspace-prefixed: `/:workspace/...`
  - Examples: `/local`, `/local/inbox`, `/ops/threads/thread-123`
  - `/` redirects to the default workspace.
  - Legacy root page routes (`/threads`, `/inbox`, etc.) redirect to the default
    workspace for convenience.
- Optional external mount prefix: `ANX_UI_BASE_PATH=/anx`
  - External routes become `/anx/:workspace/...`
  - Build/dev the UI with the same base path you plan to serve
  - Put build-time values in `web-ui/.env.build` or override them via shell env
  - Reverse proxies should preserve the prefix instead of stripping it

- The SvelteKit server resolves proxied API traffic from the active workspace
  context and forwards requests to the matching `anx-core`.

- Single-core fallback still works for local/dev:
  - `ANX_CORE_BASE_URL=http://127.0.0.1:8000`
  - This synthesizes one default `local` workspace when `ANX_WORKSPACES` is unset.

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
env | rg '^(ANX_WORKSPACES|ANX_DEFAULT_WORKSPACE|ANX_CORE_BASE_URL)='
curl -fsS http://127.0.0.1:8000/meta/handshake
curl -fsS -H 'x-anx-workspace-slug: local' http://127.0.0.1:5173/meta/handshake
```

The first `curl` checks `anx-core` directly. The second checks the UI proxy path
for one workspace.

## Integration E2E with real anx-core

The repo includes `./scripts/e2e-with-core` for a headless golden-path run
against a real `anx-core`.

Default local run:

Terminal A (backend):

```bash
cd ../core
./scripts/dev
```

Terminal B (ui):

```bash
cd ../web-ui
ANX_WORKSPACES='[{"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"}]' \
ANX_DEFAULT_WORKSPACE=local \
./scripts/e2e-with-core
```
