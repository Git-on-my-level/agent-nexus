# oar-ui

This package contains the SvelteKit web UI for Organization Autorunner.

- `docs/`: operator runbooks and spec/compliance notes
- `/contracts/oar-schema.yaml`: shared schema contract (`0.2.2`)
- `/contracts/gen/ts/client.ts`: generated TS API client consumed by `web-ui`

## Runtime model

`oar-ui` now assumes project-aware proxying through the UI server.

- Canonical config: `OAR_PROJECTS`
  - JSON array or object mapping `project slug -> core base URL`
  - Example:

    ```bash
    export OAR_PROJECTS='[
      {"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"},
      {"slug":"ops","label":"Ops","coreBaseUrl":"http://127.0.0.1:8001"}
    ]'
    export OAR_DEFAULT_PROJECT=local
    ```

- UI routes are project-prefixed: `/:project/...`
  - Examples: `/local`, `/local/inbox`, `/ops/threads/thread-123`
  - `/` redirects to the default project.
  - Legacy root page routes (`/threads`, `/inbox`, etc.) redirect to the default
    project for convenience.
- Optional external mount prefix: `OAR_UI_BASE_PATH=/oar`
  - External routes become `/oar/:project/...`
  - Build/dev the UI with the same base path you plan to serve
  - Reverse proxies should preserve the prefix instead of stripping it

- The SvelteKit server resolves proxied API traffic from the active project
  context and forwards requests to the matching `oar-core`.

- Single-core fallback still works for local/dev:
  - `OAR_CORE_BASE_URL=http://127.0.0.1:8000`
  - This synthesizes one default `local` project when `OAR_PROJECTS` is unset.

See `docs/runbook.md` for deployment examples, auth/session behavior, and
WebAuthn constraints.

## Startup compatibility

On project route startup the UI calls `GET /meta/handshake` (falling back to
`GET /version`) through the project-aware proxy and requires
`schema_version === "0.2.2"`.

## Quick smoke check

```bash
env | rg '^(OAR_PROJECTS|OAR_DEFAULT_PROJECT|OAR_CORE_BASE_URL)='
curl -fsS http://127.0.0.1:8000/meta/handshake
curl -fsS -H 'x-oar-project-slug: local' http://127.0.0.1:5173/meta/handshake
```

The first `curl` checks `oar-core` directly. The second checks the UI proxy path
for one project.

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
OAR_PROJECTS='[{"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"}]' \
OAR_DEFAULT_PROJECT=local \
./scripts/e2e-with-core
```
