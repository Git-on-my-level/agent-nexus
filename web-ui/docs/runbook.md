# oar-ui Runbook

This runbook covers local integration and production-like serving for the
workspace-aware `oar-ui`.

## Configuration

### Workspace catalog

Canonical runtime config is `OAR_WORKSPACES`.

- Accepts a JSON array or object.
- Each entry needs a workspace slug and a core base URL.
- Optional fields: `label`, `description`.

Example:

```bash
export OAR_WORKSPACES='[
  {"slug":"dtrinity","label":"DTrinity","coreBaseUrl":"http://127.0.0.1:8000"},
  {"slug":"scalingforever","label":"Scaling Forever","coreBaseUrl":"http://127.0.0.1:8001"}
]'
export OAR_DEFAULT_WORKSPACE=dtrinity
```

Legacy aliases (deprecated):

- `OAR_PROJECTS` is accepted if `OAR_WORKSPACES` is not set.
- `OAR_DEFAULT_PROJECT` is accepted if `OAR_DEFAULT_WORKSPACE` is not set.

Route model:

- `/:workspace/...` is the canonical UI shape.
- `/` redirects to `/${OAR_DEFAULT_WORKSPACE}`.
- Root page routes such as `/threads` and `/inbox` redirect to the default
  workspace to ease local use and old bookmarks.
- Optional mount prefix: set `OAR_UI_BASE_PATH=/oar`
  - External routes become `/oar/:workspace/...`
  - `OAR_UI_BASE_PATH` is applied by SvelteKit at dev/build startup, so use the
    intended value when running `./scripts/dev` or `./scripts/build`

Build-time config files:

- `web-ui/.env.build` is read by `svelte.config.js` for `./scripts/dev`,
  `./scripts/build`, and `pnpm run build`
- `web-ui/.env.build.local` layers on top for machine-local overrides
- Shell env wins over file values when both are set
- `.env.build` is gitignored by default; use `git add -f web-ui/.env.build` if
  you intentionally want to commit operator-specific build config

Single-core fallback:

- If `OAR_WORKSPACES` is unset, `OAR_CORE_BASE_URL` still creates one default
  `local` workspace for dev/integration use.
- If neither variable is set, the default `local` workspace uses same-origin mock
  routes.

### Required oar-core endpoints

The UI expects these HTTP endpoints (see `docs/http-api.md` for the full
contract):

- `GET /meta/handshake` (preferred startup compatibility check)
- `GET /version` (fallback)
- `POST /actors`, `GET /actors`
- `POST /auth/passkey/register/options`, `POST /auth/passkey/register/verify`
- `POST /auth/passkey/login/options`, `POST /auth/passkey/login/verify`
- `POST /auth/token`, `GET /agents/me`
- `POST /threads`, `GET /threads`, `GET /threads/{thread_id}`,
  `PATCH /threads/{thread_id}`, `GET /threads/{thread_id}/timeline`,
  `GET /threads/{thread_id}/workspace`
- `POST /commitments`, `GET /commitments`, `GET /commitments/{commitment_id}`,
  `PATCH /commitments/{commitment_id}`
- `POST /boards`, `GET /boards`, `GET /boards/{board_id}`,
  `PATCH /boards/{board_id}`, `GET /boards/{board_id}/workspace`
- `POST /boards/{board_id}/cards`, `GET /boards/{board_id}/cards`,
  `PATCH /boards/{board_id}/cards/{thread_id}`,
  `POST /boards/{board_id}/cards/{thread_id}/move`,
  `POST /boards/{board_id}/cards/{thread_id}/remove`
- `POST /artifacts`, `GET /artifacts`, `GET /artifacts/{artifact_id}`,
  `GET /artifacts/{artifact_id}/content`
- `POST /events`, `GET /events/{event_id}`
- `POST /work_orders`, `POST /receipts`, `POST /reviews`
- `GET /snapshots/{snapshot_id}`
- `POST /derived/rebuild` (optional)
- `GET /inbox`, `POST /inbox/ack`

### Auth and actor storage

Identity is workspace-scoped.

- **Auth-first model (default)**:
  - When `dev_actor_mode=false` (default), users MUST authenticate via passkey to access the workspace.
  - Passkey registration creates a new agent with `principal_kind=human`, `auth_method=passkey`.
  - Authenticated writes lock to the principal's linked actor.
  - The legacy actor picker/creator flow is hidden.
  - Unauthenticated users are redirected to `/login`.
- **Development actor mode (dev convenience)**:
  - When `dev_actor_mode=true` (set via `OAR_ENABLE_DEV_ACTOR_MODE=1` on core), the legacy actor selection flow is available.
  - Selected actor is stored in `localStorage` per workspace.
  - Useful for local workflows when core allows unauthenticated writes.
  - Clearly labeled as "development-only" in the UI.
- Passkey-authenticated mode:
  - Refresh/session state is carried in a same-origin `Secure`, `HttpOnly`, `SameSite=Lax` cookie per workspace.
  - Browser JavaScript does not read or write refresh tokens.
  - Access tokens stay on the server side and are refreshed through the cookie-backed session endpoint.
  - Browser API calls go through the same-origin BFF/proxy surface.
  - Authenticated writes lock to that workspace's principal actor.
- Actor-selection mode (dev only):
  - Selected actor is stored in `localStorage` per workspace.
  - Only available when `dev_actor_mode=true`.

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
OAR_WORKSPACES='[{"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"}]' \
OAR_DEFAULT_WORKSPACE=local \
./scripts/dev
```

With an external mount prefix:

```bash
cd ../web-ui
OAR_WORKSPACES='[{"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"}]' \
OAR_DEFAULT_WORKSPACE=local \
OAR_UI_BASE_PATH=/oar \
./scripts/dev
```

Two cores:

```bash
export OAR_WORKSPACES='[
  {"slug":"dtrinity","label":"DTrinity","coreBaseUrl":"http://127.0.0.1:8000"},
  {"slug":"scalingforever","label":"Scaling Forever","coreBaseUrl":"http://127.0.0.1:8001"}
]'
export OAR_DEFAULT_WORKSPACE=dtrinity
./scripts/dev
```

Integration validation:

```bash
OAR_WORKSPACES='[{"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"}]' \
OAR_DEFAULT_WORKSPACE=local \
./scripts/e2e-with-core
```

Representative seeded local data, including boards/cards/docs from mock mode,
can be pushed into a live core with:

```bash
OAR_CORE_BASE_URL=http://127.0.0.1:8000 \
node ./scripts/seed-core-from-mock.mjs
```

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
OAR_UI_BASE_PATH=/oar
ADAPTER=node
EOF
```

`./scripts/build` defaults to `ADAPTER=node`, producing a Node.js server at
`build/index.js`. Override with `ADAPTER=auto` if targeting a platform-specific
adapter (Vercel, Cloudflare, etc.), but note that bare-metal and reverse-proxied
deployments require the Node adapter for server-side proxy and hook support.

Serve the built UI:

```bash
OAR_WORKSPACES='[
  {"slug":"dtrinity","label":"DTrinity","coreBaseUrl":"http://127.0.0.1:8000"},
  {"slug":"scalingforever","label":"Scaling Forever","coreBaseUrl":"http://127.0.0.1:8001"}
]' \
OAR_DEFAULT_WORKSPACE=dtrinity \
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
`https://m2-internal.scalingforever.com/oar/dtrinity/...`:

```caddy
m2-internal.scalingforever.com {
  handle /oar* {
    reverse_proxy 127.0.0.1:4173
  }
}
```

Configure the UI with `OAR_UI_BASE_PATH=/oar` when building or running the dev
server. The reverse proxy must preserve `/oar` so SvelteKit can route and
generate links under the configured base path. The UI server then proxies API
traffic to the matching `oar-core` from `OAR_WORKSPACES`. Core instances do not
need to be internet-exposed.

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
  then proxies to `oar-core` instances.

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
- That does not create shared auth state across independent cores. `oar-ui`
  stores auth per workspace and each `oar-core` still validates its own tokens.
- If core is configured with explicit `OAR_WEBAUTHN_ORIGIN` or
  `OAR_WEBAUTHN_RPID`, the browser must open the UI on that exact hostname.
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
3. Verify the matching workspace entry in `OAR_WORKSPACES`.

### Wrong workspace mapping

Symptoms:

- One workspace works and another consistently 404s/503s.
- Requests fail with `workspace_not_configured` or `workspace_header_required`.

Actions:

1. Confirm the UI URL includes a valid workspace slug.
2. Confirm `OAR_WORKSPACES` contains that slug.
3. Keep core base URLs as bare origins, not path-prefixed URLs.

### WebAuthn failures on one hostname but not another

Actions:

1. Open the UI on the hostname expected by core.
2. Check forwarded host/origin handling at the reverse proxy.
3. Do not assume path-prefix routing changes WebAuthn identity boundaries.
