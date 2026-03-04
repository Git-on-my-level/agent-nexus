# oar-ui — CAR ticket pack

This folder contains:

- `docs/`: the finalized spec and the concrete HTTP API contract for clients
- `contracts/oar-schema.yaml`: shared schema contract (v0.2.2)

Intended use: unzip into an empty `oar-ui` git repo, then run CAR.

## Runtime configuration

- `PUBLIC_OAR_CORE_BASE_URL`: base URL for the oar-core HTTP API.
  - Example: `PUBLIC_OAR_CORE_BASE_URL=http://127.0.0.1:8000`
  - If omitted, the UI uses same-origin requests.

On startup, the UI calls `GET /version` and requires
`schema_version === "0.2.2"`. If it does not match, boot fails with a clear
error so incompatible core/UI versions are surfaced immediately.

## Integration E2E with real oar-core

The repo includes `./scripts/e2e-with-core` for a headless golden-path
integration run against a real `oar-core`.

Behavior:

- Uses `OAR_CORE_BASE_URL` (default: `http://127.0.0.1:8000`)
- Proxies same-origin UI API routes to `OAR_CORE_BASE_URL` during the run
  (no browser CORS configuration required)
- Fails fast with a clear message if `${OAR_CORE_BASE_URL}/version` is
  unreachable
- Runs Playwright integration spec in headless mode
- Stores failure artifacts under `test-results/e2e-with-core/`
  (trace/screenshot/video)

Default local runbook:

Terminal A (backend):

```bash
cd ../organization-autorunner-core
./scripts/dev
```

Terminal B (ui):

```bash
cd ../organization-autorunner-ui
OAR_CORE_BASE_URL=http://127.0.0.1:8000 ./scripts/e2e-with-core
```
