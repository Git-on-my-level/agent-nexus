# anx-ui

This package contains the SvelteKit web UI for Organization Autorunner.

- `docs/`: operator runbooks and spec/compliance notes
- `/contracts/anx-schema.yaml`: shared schema contract (`0.2.3`)
- `/contracts/gen/ts/client.ts`: generated TS API client consumed by `web-ui`

## Runtime model

`anx-ui` now assumes workspace-aware proxying through the UI server.

- Canonical config: `ANX_WORKSPACES`
  - JSON array or object mapping `workspace slug -> core base URL`
  - Each entry must include `organizationSlug` and `slug` (workspace), or set
    `ANX_DEFAULT_ORGANIZATION` for object-form `slug -> url` maps
  - Optional per-workspace `publicOrigin`/`public_origin` keeps copied links and
    registration snippets pinned to the externally reachable workspace URL when
    the UI itself is reverse-proxied or seen as loopback internally
  - Used directly for self-host and local/dev deployments
  - Example:

    ```bash
    export ANX_WORKSPACES='[
      {"organizationSlug":"local","slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000","publicOrigin":"https://anx.tailnet.ts.net/anx/o/local/w/local"},
      {"organizationSlug":"local","slug":"ops","label":"Ops","coreBaseUrl":"http://127.0.0.1:8001","publicOrigin":"https://anx.tailnet.ts.net/anx/o/local/w/ops"}
    ]'
    export ANX_DEFAULT_WORKSPACE=local
    export ANX_DEFAULT_ORGANIZATION=local
    ```

  - Legacy aliases (deprecated): `ANX_PROJECTS` and `ANX_DEFAULT_PROJECT` still work if the new names are absent.

- UI workspace routes use a reserved prefix: `/o/:organization/w/:workspace/...`
  - Examples: `/o/local/w/local`, `/o/local/w/local/inbox`, `/o/acme/w/ops/threads/thread-123`
  - `/` redirects to the last-used workspace when `anx_last_workspace` is set and still
    resolves. With no cookie: if `ANX_CONTROL_BASE_URL` is **unset** (self-host / local
    provider) and the catalog has a **default workspace** (from `ANX_DEFAULT_WORKSPACE` /
    `ANX_DEFAULT_ORGANIZATION` and `ANX_WORKSPACES`), `/` redirects there (typical
    passkey login flow). Otherwise it redirects to the hosted chooser (`/hosted/start`).
    If `ANX_WORKSPACES` is empty, there is no default workspace and `/` still uses
    `/hosted/start`.
- Optional external mount prefix: `ANX_UI_BASE_PATH=/anx`
  - External routes become `/anx/o/:organization/w/:workspace/...`
  - Build/dev the UI with the same base path you plan to serve
  - Put build-time values in `web-ui/.env.build` or override them via shell env
  - Reverse proxies should preserve the prefix instead of stripping it

- The SvelteKit server resolves proxied API traffic from the active workspace
  context and forwards requests to the matching `anx-core`.

- When `ANX_WORKSPACES` is unset, the catalog is empty until you configure at
  least one workspace (no synthetic default).

See `docs/runbook.md` for deployment examples, auth/session behavior, and
WebAuthn constraints.

## Development

From `agent-nexus/`: `make -C web-ui check` runs lint and unit tests. Use
`pnpm run dev` (in `web-ui/`) for the Vite dev server, and `pnpm run test:e2e`
for Playwright.
