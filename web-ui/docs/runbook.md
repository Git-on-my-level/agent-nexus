# oar-ui Runbook

This runbook covers local integration and production-like serving for the
project-aware `oar-ui`.

## Configuration

### Project catalog

Canonical runtime config is `OAR_PROJECTS`.

- Accepts a JSON array or object.
- Each entry needs a project slug and a core base URL.
- Optional fields: `label`, `description`.

Example:

```bash
export OAR_PROJECTS='[
  {"slug":"dtrinity","label":"DTrinity","coreBaseUrl":"http://127.0.0.1:8000"},
  {"slug":"scalingforever","label":"Scaling Forever","coreBaseUrl":"http://127.0.0.1:8001"}
]'
export OAR_DEFAULT_PROJECT=dtrinity
```

Route model:

- `/:project/...` is the canonical UI shape.
- `/` redirects to `/${OAR_DEFAULT_PROJECT}`.
- Root page routes such as `/threads` and `/inbox` redirect to the default
  project to ease local use and old bookmarks.

Single-core fallback:

- If `OAR_PROJECTS` is unset, `OAR_CORE_BASE_URL` still creates one default
  `local` project for dev/integration use.
- If neither variable is set, the default `local` project uses same-origin mock
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
  `PATCH /threads/{thread_id}`, `GET /threads/{thread_id}/timeline`
- `POST /commitments`, `GET /commitments`, `GET /commitments/{commitment_id}`,
  `PATCH /commitments/{commitment_id}`
- `POST /artifacts`, `GET /artifacts`, `GET /artifacts/{artifact_id}`,
  `GET /artifacts/{artifact_id}/content`
- `POST /events`, `GET /events/{event_id}`
- `POST /work_orders`, `POST /receipts`, `POST /reviews`
- `GET /snapshots/{snapshot_id}`
- `POST /derived/rebuild` (optional)
- `GET /inbox`, `POST /inbox/ack`

### Auth and actor storage

Identity is project-scoped.

- Passkey-authenticated mode:
  - Access token stays in memory per project.
  - Refresh token is stored in `sessionStorage` per project.
  - Authenticated writes lock to that project’s principal actor.
- Actor-selection mode:
  - Selected actor is stored in `localStorage` per project.
  - Useful for local workflows when core allows unauthenticated writes.

Switching from `/dtrinity/...` to `/scalingforever/...` preserves each project’s
own auth and actor state independently.

## Local integration

Single core:

```bash
cd ../core
./scripts/dev
```

```bash
cd ../web-ui
OAR_PROJECTS='[{"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"}]' \
OAR_DEFAULT_PROJECT=local \
./scripts/dev
```

Two cores:

```bash
export OAR_PROJECTS='[
  {"slug":"dtrinity","label":"DTrinity","coreBaseUrl":"http://127.0.0.1:8000"},
  {"slug":"scalingforever","label":"Scaling Forever","coreBaseUrl":"http://127.0.0.1:8001"}
]'
export OAR_DEFAULT_PROJECT=dtrinity
./scripts/dev
```

Integration validation:

```bash
OAR_PROJECTS='[{"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"}]' \
OAR_DEFAULT_PROJECT=local \
./scripts/e2e-with-core
```

## Packaging and serving

Build distributable assets:

```bash
./scripts/build
```

Serve the built UI:

```bash
OAR_PROJECTS='[
  {"slug":"dtrinity","label":"DTrinity","coreBaseUrl":"http://127.0.0.1:8000"},
  {"slug":"scalingforever","label":"Scaling Forever","coreBaseUrl":"http://127.0.0.1:8001"}
]' \
OAR_DEFAULT_PROJECT=dtrinity \
./scripts/serve
```

`./scripts/serve` fails fast if build artifacts are missing. Run
`./scripts/build` first.

## Reverse proxy shape

Recommended production shape: one UI process, many core processes, path-prefix
entrypoint at the edge.

Example Caddy config for external URLs like
`https://m2-internal.scalingforever.com/oar/dtrinity/...`:

```caddy
m2-internal.scalingforever.com {
  handle_path /oar/* {
    reverse_proxy 127.0.0.1:4173
  }
}
```

`handle_path` strips `/oar`, so the UI receives `/:project/...` as expected.
The UI server then proxies API traffic to the matching `oar-core` from
`OAR_PROJECTS`. Core instances do not need to be internet-exposed.

## WebAuthn and hostname/origin limits

WebAuthn is host/origin sensitive, not path sensitive.

- Sharing one hostname across many projects is fine for browser passkey
  ceremonies.
- That does not create shared auth state across independent cores. `oar-ui`
  stores auth per project and each `oar-core` still validates its own tokens.
- If core is configured with explicit `OAR_WEBAUTHN_ORIGIN` or
  `OAR_WEBAUTHN_RPID`, the browser must open the UI on that exact hostname.
- Alternate hostnames such as `localhost`, `127.0.0.1`, Tailscale names, or raw
  IPs may fail if they do not match the configured RP ID/origin.

## Troubleshooting

### Core unavailable

Symptoms:

- Startup compatibility checks fail for one project.
- UI shows `core_unreachable` for project-scoped traffic.
- `./scripts/e2e-with-core` fails health checks.

Actions:

1. Confirm the target core is running.
2. Verify the exact upstream URL:
   `curl -fsS http://127.0.0.1:8000/meta/handshake`
3. Verify the matching project entry in `OAR_PROJECTS`.

### Wrong project mapping

Symptoms:

- One project works and another consistently 404s/503s.
- Requests fail with `project_not_configured` or `project_header_required`.

Actions:

1. Confirm the UI URL includes a valid project slug.
2. Confirm `OAR_PROJECTS` contains that slug.
3. Keep core base URLs as bare origins, not path-prefixed URLs.

### WebAuthn failures on one hostname but not another

Actions:

1. Open the UI on the hostname expected by core.
2. Check forwarded host/origin handling at the reverse proxy.
3. Do not assume path-prefix routing changes WebAuthn identity boundaries.
