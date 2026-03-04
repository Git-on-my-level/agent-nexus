# oar-ui Runbook

This runbook covers production-like build/serve usage and local integration with
`oar-core`.

## Configuration

### Core base URL

`oar-ui` supports two configuration modes for oar-core connectivity:

1. Server-side proxy mode (recommended for local/dev-like serving)
   - Set `OAR_CORE_BASE_URL` in the UI process environment.
   - The SvelteKit server proxies core API routes to that base URL.
   - Browser requests stay same-origin to the UI host.

2. Browser-direct mode
   - Set `PUBLIC_OAR_CORE_BASE_URL`.
   - This value is compiled into the frontend bundle at build time.
   - Browser sends requests directly to the core origin (CORS must be allowed).

If neither variable is set, UI requests are same-origin (`/version`, `/threads`,
etc.) and require an upstream reverse proxy that routes those paths to oar-core.

### Required oar-core endpoints

The UI expects these HTTP endpoints (see `docs/http-api.md` for full contract):

- `GET /version`
- `POST /actors`, `GET /actors`
- `POST /threads`, `GET /threads`, `GET /threads/{thread_id}`,
  `PATCH /threads/{thread_id}`, `GET /threads/{thread_id}/timeline`
- `POST /commitments`, `GET /commitments`, `GET /commitments/{commitment_id}`,
  `PATCH /commitments/{commitment_id}`
- `POST /artifacts`, `GET /artifacts`, `GET /artifacts/{artifact_id}`,
  `GET /artifacts/{artifact_id}/content`
- `POST /events`, `GET /events/{event_id}`
- `POST /work_orders`, `POST /receipts`, `POST /reviews`
- `GET /inbox`, `POST /inbox/ack`

### Auth assumptions (v0)

v0 uses actor selection only:

- No RBAC or external auth is enforced by the UI.
- Mutating operations include `actor_id` from the selected actor.

## Local Integration (Real Core)

Use sibling backend repo `../organization-autorunner-core`.

Terminal A (backend):

```bash
cd ../organization-autorunner-core
./scripts/dev
```

Backend defaults to `http://127.0.0.1:8000`.

Terminal B (ui):

```bash
cd ../organization-autorunner-ui
OAR_CORE_BASE_URL=http://127.0.0.1:8000 ./scripts/dev
```

For end-to-end integration validation:

```bash
OAR_CORE_BASE_URL=http://127.0.0.1:8000 ./scripts/e2e-with-core
```

## Packaging and Serving

Build distributable assets:

```bash
./scripts/build
```

This installs dependencies with `--frozen-lockfile` and runs `pnpm run build`.

Serve the built UI:

```bash
OAR_CORE_BASE_URL=http://127.0.0.1:8000 ./scripts/serve
```

`./scripts/serve` fails fast if build artifacts are missing. Run
`./scripts/build` first.

## Static Hosting Notes

This project currently uses SvelteKit with `@sveltejs/adapter-auto`.

- In static or CDN-only hosting (no Node server), server-side proxying via
  `OAR_CORE_BASE_URL` is unavailable.
- Use `PUBLIC_OAR_CORE_BASE_URL` during build so the browser can call core
  directly.
- Because `PUBLIC_*` values are build-time, changing core URL requires rebuild
  and redeploy.

## Troubleshooting

### Core unavailable

Symptoms:

- Startup/version checks fail.
- UI shows `core_unreachable` or network errors.
- Integration script fails fast on `${OAR_CORE_BASE_URL}/version`.

Actions:

1. Confirm backend is running:
   `cd ../organization-autorunner-core && ./scripts/dev`
2. Verify the exact URL:
   `curl -fsS http://127.0.0.1:8000/version`
3. Re-run UI with matching base URL:
   `OAR_CORE_BASE_URL=http://127.0.0.1:8000 ./scripts/dev`

### Misconfigured base URL

Symptoms:

- 404/5xx from proxied endpoints.
- Schema check fails at startup.

Actions:

1. Remove trailing typo/path segments (use bare origin, e.g.
   `http://127.0.0.1:8000`).
2. Ensure UI and backend schema versions match (`/version` should report
   `schema_version: "0.2.2"`).
3. If using `PUBLIC_OAR_CORE_BASE_URL`, rebuild after env changes:
   `./scripts/build`.
