# oar-ui — CAR ticket pack

This folder contains:

- `docs/`: the finalized spec and the concrete HTTP API contract for clients
- `/contracts/oar-schema.yaml`: shared schema contract (v0.2.2)
- `/contracts/gen/ts/client.ts`: generated TS API client used by `web-ui`

Intended use: unzip into an empty `oar-ui` git repo, then run CAR.

## Runtime configuration

- `PUBLIC_OAR_CORE_BASE_URL`: base URL for the oar-core HTTP API.
  - Example: `PUBLIC_OAR_CORE_BASE_URL=http://127.0.0.1:8000`
  - If omitted, the UI uses same-origin requests.
- `OAR_CORE_BASE_URL`: server-side proxy target used by the SvelteKit runtime.
  - Recommended for local/dev and integration runs to avoid browser CORS setup.

See `docs/runbook.md` for full packaging, serving, endpoint, and troubleshooting
guidance.

On startup, the UI calls `GET /meta/handshake` (falling back to `GET /version`
for compatibility) and requires `schema_version === "0.2.2"`. If it does not
match, boot fails with a clear error so incompatible core/UI versions are
surfaced immediately.

## Proxy/Env smoke check

Use this quick check before integration runs:

```bash
env | rg '^(OAR_CORE_BASE_URL|PUBLIC_OAR_CORE_BASE_URL)='
```

- Proxy mode (recommended): `OAR_CORE_BASE_URL` must be set in the UI process.
- Browser-direct mode: `PUBLIC_OAR_CORE_BASE_URL` must be set before build.

Then confirm the UI can reach core through the configured path:

```bash
curl -fsS http://127.0.0.1:8000/meta/handshake
curl -fsS http://127.0.0.1:5173/meta/handshake
```

The first command checks core directly; the second checks the UI route (proxied
when `OAR_CORE_BASE_URL` is set). `GET /version` remains available as a
fallback compatibility endpoint.

## Integration E2E with real oar-core

The repo includes `./scripts/e2e-with-core` for a headless golden-path
integration run against a real `oar-core`.

Behavior:

- Uses `OAR_CORE_BASE_URL` (default: `http://127.0.0.1:8000`)
- Proxies same-origin UI API routes to `OAR_CORE_BASE_URL` during the run
  (no browser CORS configuration required)
- Fails fast with a clear message if `${OAR_CORE_BASE_URL}/meta/handshake` is
  unreachable
- Runs Playwright integration spec in headless mode
- Stores failure artifacts under `test-results/e2e-with-core/`
  (trace/screenshot/video)

Default local runbook:

Terminal A (backend):

```bash
cd ../core
./scripts/dev
```

Terminal B (ui):

```bash
cd ../web-ui
OAR_CORE_BASE_URL=http://127.0.0.1:8000 ./scripts/e2e-with-core
```
