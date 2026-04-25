# anx-ui Runbook

This runbook covers local integration and production-like serving for the
workspace-aware `anx-ui`.

## Configuration

### Core-backed Playwright (optional)

`tests/e2e/integration-core-golden-path.spec.js` exercises the UI against a **real** `anx-core`. It is **opt-in**: the spec skips unless you set `ANX_CORE_BASE_URL` or `PUBLIC_ANX_CORE_BASE_URL`. Default CI for the web UI does not assume a running core; enable that env in a dedicated job or locally when you want this coverage (see `docs/spec-compliance.md` — integration execution notes).

### Workspace catalog

Canonical runtime config is `ANX_WORKSPACES`.

- Accepts a JSON array or object.
- Each entry needs `organizationSlug`, a workspace `slug`, and a core base URL
  (or set `ANX_DEFAULT_ORGANIZATION` when using object-form `slug -> url` maps).
- Optional fields: `label`, `description`, `publicOrigin`/`public_origin`.
- Set `publicOrigin` to the externally reachable workspace URL when the UI is
  reverse-proxied, mounted under a base path, or otherwise sees loopback
  origins internally. Access-page copy and registration snippets use it as the
  fallback public URL.
- This is the authoritative routing source for self-host and local/dev.

Example:

```bash
export ANX_WORKSPACES='[
  {"organizationSlug":"local","slug":"dtrinity","label":"DTrinity","coreBaseUrl":"http://127.0.0.1:8000","publicOrigin":"https://anx.tailnet.ts.net/anx/o/local/w/dtrinity"},
  {"organizationSlug":"local","slug":"scalingforever","label":"Scaling Forever","coreBaseUrl":"http://127.0.0.1:8001","publicOrigin":"https://anx.tailnet.ts.net/anx/o/local/w/scalingforever"}
]'
export ANX_DEFAULT_WORKSPACE=dtrinity
export ANX_DEFAULT_ORGANIZATION=local
```

Route model:

- `/o/:organization/w/:workspace/...` is the canonical UI shape.
- `/` redirects to the last-used workspace when a cookie is set; otherwise to the
  workspace chooser. There is no silent default workspace.
- Optional mount prefix: set `ANX_UI_BASE_PATH=/anx`
  - External routes become `/anx/o/:organization/w/:workspace/...`
  - `ANX_UI_BASE_PATH` is applied by SvelteKit at dev/build startup, so use the
    intended value when running `./scripts/dev` or `./scripts/build`

Build-time config files:

- `web-ui/.env.build` is read by `svelte.config.js` for `./scripts/dev`,
  `./scripts/build`, and `pnpm run build`
- `web-ui/.env.build.local` layers on top for machine-local overrides
- Shell env wins over file values when both are set
- `.env.build` is gitignored by default; use `git add -f web-ui/.env.build` if
  you intentionally want to commit operator-specific build config

Workspace catalog:

- If `ANX_WORKSPACES` is unset, the static catalog is empty until you configure
  entries (no synthetic default from `ANX_CORE_BASE_URL` alone).
- If neither `ANX_WORKSPACES` nor per-workspace config resolves a `coreBaseUrl`
  for the workspace, **catalog-backed API paths** return **503** (`core_not_configured`)
  instead of being served locally. Run a real core (for example `make serve` /
  `./scripts/e2e-smoke`) or set `ANX_WORKSPACES` / `ANX_CORE_BASE_URL` per
  **Local integration** below.

### Required anx-core endpoints

The UI expects these HTTP endpoints (see `docs/http-api.md` for the full
contract):

- `GET /meta/handshake` (preferred startup compatibility check)
- `GET /version` (fallback)
- `POST /actors`, `GET /actors`
- `POST /auth/passkey/register/options`, `POST /auth/passkey/register/verify`
- `POST /auth/passkey/login/options`, `POST /auth/passkey/login/verify`
- `POST /auth/token`, `GET /agents/me`
- `GET /auth/bootstrap/status`
- `GET /auth/principals`, `POST /auth/principals/{agent_id}/revoke`
- `GET /auth/invites`, `POST /auth/invites`,
  `POST /auth/invites/{invite_id}/revoke`
- `GET /auth/audit`
- `POST /topics`, `GET /topics`, `GET /topics/{topic_id}`,
  `PATCH /topics/{topic_id}`, `GET /topics/{topic_id}/timeline`,
  `GET /topics/{topic_id}/workspace`
- `POST /boards`, `GET /boards`, `GET /boards/{board_id}`,
  `PATCH /boards/{board_id}`, `GET /boards/{board_id}/workspace`
- `POST /boards/{board_id}/cards`, `GET /boards/{board_id}/cards`,
  `PATCH /boards/{board_id}/cards/{thread_id}`,
  `POST /boards/{board_id}/cards/{thread_id}/move`,
  `POST /boards/{board_id}/cards/{thread_id}/remove`
- `POST /docs`, `GET /docs`, `GET /docs/{document_id}`,
  `PATCH /docs/{document_id}`, `GET /docs/{document_id}/history`
- `POST /artifacts`, `GET /artifacts`, `GET /artifacts/{artifact_id}`,
  `GET /artifacts/{artifact_id}/content`
- `POST /events`, `GET /events/{event_id}`
- `POST /packets/receipts`, `POST /packets/reviews`
- `POST /derived/rebuild` (optional)
- `GET /inbox`, `POST /inbox/{inbox_id}/acknowledge`

### Auth and actor storage

Identity is workspace-scoped.

- **Auth-first model**:
  - Users authenticate via passkey or agent token flows to access workspace routes.
  - Passkey registration creates a new agent with `principal_kind=human`, `auth_method=passkey`.
  - Authenticated writes lock to the principal's linked actor.
  - Unauthenticated users are redirected to `/login`.
- Passkey-authenticated mode:
  - Refresh/session state is carried in a same-origin `Secure`, `HttpOnly`, `SameSite=Lax` cookie per workspace.
  - Browser JavaScript does not read or write refresh tokens.
  - Access tokens stay on the server side and are refreshed through the cookie-backed session endpoint.
  - Browser API calls go through the same-origin BFF/proxy surface.
  - Authenticated writes lock to that workspace's principal actor.

Switching from `/dtrinity/...` to `/scalingforever/...` preserves each workspace's
own auth and actor state independently.

## Local integration

Single core:

```bash
cd ../core
./scripts/dev
```

```bash
cd ../web-ui
ANX_WORKSPACES='[{"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"}]' \
ANX_DEFAULT_WORKSPACE=local \
./scripts/dev
```

With an external mount prefix:

```bash
cd ../web-ui
ANX_WORKSPACES='[{"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"}]' \
ANX_DEFAULT_WORKSPACE=local \
ANX_UI_BASE_PATH=/anx \
./scripts/dev
```

Two cores:

```bash
export ANX_WORKSPACES='[
  {"slug":"dtrinity","label":"DTrinity","coreBaseUrl":"http://127.0.0.1:8000"},
  {"slug":"scalingforever","label":"Scaling Forever","coreBaseUrl":"http://127.0.0.1:8001"}
]'
export ANX_DEFAULT_WORKSPACE=dtrinity
./scripts/dev
```

Integration validation:

```bash
ANX_WORKSPACES='[{"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"}]' \
ANX_DEFAULT_WORKSPACE=local \
./scripts/e2e-with-core
```

Representative **dev fixture** data (boards/cards/docs/events) can be pushed
into a live core with:

```bash
ANX_CORE_BASE_URL=http://127.0.0.1:8000 \
node ./scripts/seed-core-from-mock.mjs
```

Select an alternate dev seed scenario with `ANX_DEV_SEED_SCENARIO`, for example:

```bash
ANX_CORE_BASE_URL=http://127.0.0.1:8000 \
ANX_DEV_SEED_SCENARIO=kids-lemonade-stand \
node ./scripts/seed-core-from-mock.mjs
```

`kids-lemonade-stand` is chapter-aware: web dev seeding applies all checked-in
chapters in sequence by default, so the workspace includes the board, cards,
message history, and document revision history from the full scenario.

With **`make serve`** (or matching env), the seed also writes **`web-ui/.dev/local-identities.json`**
(gitignored) when `ANX_DEV_SEED_IDENTITIES=1`, core has `ANX_BOOTSTRAP_TOKEN`, and
`ANX_DEV_REGISTER_LINKED_ACTORS=1` on anx-core. The sidebar **Fixture persona**
control then switches cookie-backed sessions among seeded principals.

Primary board UI entry points:

- `/:workspace/boards`
- `/:workspace/boards/:boardId`

The board detail page relies on `GET /boards/{board_id}/workspace` for the
canonical read model and reloads that workspace after mutations or `409
conflict` responses.

## Packaging and serving

Build distributable assets:

```bash
./scripts/build
```

Example build config file:

```bash
cat > .env.build <<'EOF'
ANX_UI_BASE_PATH=/anx ADAPTER=node
EOF
```

`./scripts/build` defaults to `ADAPTER=node`, producing a Node.js server at
`build/index.js`. Override with `ADAPTER=auto` if targeting a platform-specific
adapter (Vercel, Cloudflare, etc.), but note that bare-metal and reverse-proxied
deployments require the Node adapter for server-side proxy and hook support.

Serve the built UI:

```bash
ANX_WORKSPACES='[
  {"slug":"dtrinity","label":"DTrinity","coreBaseUrl":"http://127.0.0.1:8000"},
  {"slug":"scalingforever","label":"Scaling Forever","coreBaseUrl":"http://127.0.0.1:8001"}
]' \
ANX_DEFAULT_WORKSPACE=dtrinity \
./scripts/serve
```

`./scripts/serve` runs `node build/index.js` and fails fast if the Node adapter
build is missing. Run `./scripts/build` first.

`ORIGIN` defaults to `http://${HOST}:${PORT}`. Set it explicitly when serving
behind TLS or a reverse proxy on a different hostname, e.g.
`ORIGIN=https://m2-internal.scalingforever.com`.

**Do not use `vite preview` for production-like deployments.** `vite preview` is
a static preview server that does not execute SvelteKit server hooks or
server-side proxy logic. Requests to `/meta/handshake` and all proxied core API
traffic will fail (typically returning `200 OK` with an empty body) because the
server-side routing in `hooks.server.js` is not active.

## Reverse proxy shape

Recommended production shape: one UI process, many core processes, path-prefix
entrypoint at the edge.

Example Caddy config for external URLs like
`https://m2-internal.scalingforever.com/anx/dtrinity/...`:

```caddy
m2-internal.scalingforever.com {
  redir /anx /anx/ 301

  route /anx/* {
    reverse_proxy 127.0.0.1:4173
  }
}
```

Configure the UI with `ANX_UI_BASE_PATH=/anx` when building or running the dev
server. The reverse proxy must preserve `/anx` so SvelteKit can route and
generate links under the configured base path. The UI server then proxies API
traffic to the matching `anx-core` from `ANX_WORKSPACES`. Core instances do not
need to be internet-exposed.

Use `route`, not `handle`, for base-path proxy blocks. In Caddy, `handle`
blocks imported from separate snippet files can be auto-grouped as mutually
exclusive handlers, which can produce silent `200 OK` empty-body `NOP`
responses when another imported `handle` or `handle_path` block wins first.
Keep the `/anx/*` proxy block in the main Caddyfile, or verify with
`caddy adapt --config Caddyfile | jq` that the generated route does not include
a `group` field.

## Content Security Policy and Security Headers

The UI enforces strict security headers on all document navigation responses to
protect against XSS and injection attacks.

### Headers applied by default

On HTML document responses (not API/JSON responses), the UI sets:

- `Content-Security-Policy`: Restricts resource loading to approved sources
- `X-Frame-Options: DENY`: Prevents clickjacking via iframes
- `X-Content-Type-Options: nosniff`: Prevents MIME type sniffing
- `Referrer-Policy: strict-origin-when-cross-origin`: Limits referrer leakage

### Content Security Policy directives

The CSP is configured in `src/hooks.server.js` with these directives:

```
default-src 'self';
script-src 'self';
style-src 'self' 'unsafe-inline';
img-src 'self' data: https:;
font-src 'self' data:;
connect-src 'self';
frame-ancestors 'none';
base-uri 'self';
form-action 'self';
object-src 'none';
```

Key allowances:

- `'unsafe-inline'` in `style-src` is required for Tailwind CSS and dynamic
  styling. This is a common trade-off for utility-first CSS frameworks.
- `data:` and `https:` in `img-src` support user-provided images and icons.
- `connect-src 'self'` permits same-origin API calls to the UI server, which
  then proxies to `anx-core` instances.

### Reverse proxy considerations

When deploying behind a reverse proxy (Caddy, nginx, Cloudflare, etc.):

1. **Do not strip CSP headers**: The reverse proxy should preserve the
   `Content-Security-Policy` header set by the UI server.

2. **Avoid header injection**: Configure the proxy to merge rather than replace
   security headers. For example, in nginx:

   ```nginx
   # Good: proxy passes headers through
   proxy_pass http://127.0.0.1:4173;

   # Bad: overwrites UI security headers
   add_header Content-Security-Policy "...";
   ```

3. **Do not add additional `'unsafe-*'` directives**: If you must adjust the CSP
   for organizational requirements, avoid adding `'unsafe-eval'` or additional
   `'unsafe-inline'` directives, as these significantly weaken XSS protections.

4. **TLS considerations**: The CSP assumes TLS in production. If the proxy
   terminates TLS, ensure it forwards `https://` URLs to the UI so `connect-src
'self'` resolves correctly.

### Cloudflare Zero Trust and injected resources

Cloudflare products such as Access and Web Analytics can inject additional
browser resources into HTML responses. With the default strict UI CSP, those
resources will be blocked unless you explicitly allow them.

Supported runtime overrides:

- `ANX_UI_CSP_SCRIPT_SRC_EXTRA`
- `ANX_UI_CSP_STYLE_SRC_EXTRA`
- `ANX_UI_CSP_IMG_SRC_EXTRA`
- `ANX_UI_CSP_FONT_SRC_EXTRA`
- `ANX_UI_CSP_CONNECT_SRC_EXTRA`
- `ANX_UI_CSP_MANIFEST_SRC_EXTRA`

Each variable accepts a whitespace- or comma-separated list of additional CSP
sources appended to that directive.

Example for a Cloudflare Access + Web Analytics deployment:

```bash
ANX_UI_CSP_SCRIPT_SRC_EXTRA="https://static.cloudflareinsights.com 'sha256-<inline-script-hash-from-browser-console>'" \
ANX_UI_CSP_CONNECT_SRC_EXTRA="https://cloudflareinsights.com" \
ANX_UI_CSP_MANIFEST_SRC_EXTRA="https://scalingforever.cloudflareaccess.com" \
./scripts/serve
```

Notes:

- The inline script hash is tenant-dependent. Copy the exact `sha256-...` value
  reported by the browser CSP error for your Access-injected inline script.
- Prefer explicit hashes or host allowlists over adding `'unsafe-inline'` to
  `script-src`.
- If you do not need Cloudflare Web Analytics automatic injection, disabling it
  at Cloudflare is tighter than broadening the app CSP.

### Testing CSP in production

Use browser developer tools or online CSP evaluators to verify:

1. CSP header is present on HTML responses
2. No CSP violations appear in browser console
3. Legitimate resources (scripts, styles, images) load correctly

The e2e test suite includes CSP validation in `tests/e2e/csp.spec.js`.

## WebAuthn and hostname/origin limits

WebAuthn is host/origin sensitive, not path sensitive.

- Sharing one hostname across many workspaces is fine for browser passkey
  ceremonies.
- That does not create shared auth state across independent cores. `anx-ui`
  stores auth per workspace and each `anx-core` still validates its own tokens.
- If core is configured with explicit `ANX_WEBAUTHN_ORIGIN` or
  `ANX_WEBAUTHN_RPID`, the browser must open the UI on that exact hostname.
- Alternate hostnames such as `localhost`, `127.0.0.1`, Tailscale names, or raw
  IPs may fail if they do not match the configured RP ID/origin.

## Troubleshooting

### Core unavailable

Symptoms:

- Startup compatibility checks fail for one workspace.
- UI shows `core_unreachable` for workspace-scoped traffic.
- `./scripts/e2e-with-core` fails health checks.

Actions:

1. Confirm the target core is running.
2. Verify the exact upstream URL:
   `curl -fsS http://127.0.0.1:8000/meta/handshake`
3. Verify the matching workspace entry in `ANX_WORKSPACES`.

### Wrong workspace mapping

Symptoms:

- One workspace works and another consistently 404s/503s.
- Requests fail with `workspace_not_configured` or `workspace_header_required`.

Actions:

1. Confirm the UI URL includes a valid workspace slug.
2. Confirm `ANX_WORKSPACES` contains that slug.
3. Keep core base URLs as bare origins, not path-prefixed URLs.

### WebAuthn failures on one hostname but not another

Actions:

1. Open the UI on the hostname expected by core.
2. Check forwarded host/origin handling at the reverse proxy.
3. Do not assume path-prefix routing changes WebAuthn identity boundaries.
