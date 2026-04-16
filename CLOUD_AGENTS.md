# Cursor Cloud specific instructions

## Environment requirements
- **Go 1.23+** is required (`go.mod` specifies `go 1.23.0`). The default VM ships with Go 1.22; the update script installs Go 1.23.6 to `/usr/local/go`. Ensure `/usr/local/go/bin` is on `PATH`.
- **Node.js 20+** and **pnpm 10.17.1** are pre-installed via nvm and corepack.
- No external services (databases, caches, queues) are needed — the backend uses embedded SQLite.

## Running services
- `make serve` starts the full stack: core on `:8000`, web-ui on `:5173`, and seeds mock data automatically. See `README.md` for toggles (`SEED_CORE`, `FORCE_SEED`).
- Core enforces authenticated writes. For local work, seed identities or register a principal with bootstrap/invite and use normal bearer-token auth.
- The web UI uses auth-first routing; unauthenticated users are sent through passkey/agent onboarding flows.

## Checks and testing
- Component checks: `make -C core check`, `make cli-check`, `make -C web-ui check` (lint + unit tests; web-ui check skips Playwright e2e by default).
- Full repo gate: `make check` (runs contract-check + all component checks).
- E2E smoke: `make e2e-smoke` (starts fresh core + cli + web-ui and runs a golden-path flow).
- `pnpm` may warn about ignored build scripts for `koffi` and `protobufjs`; these are transitive and do not affect functionality.

## Gotchas
- The Go binary at `/usr/bin/go` may be stale (1.22). Always ensure `/usr/local/go/bin` is first on `PATH`.
- `make serve` uses `trap` + `wait` to manage background processes. If the shell is interrupted, child processes may linger — clean up by PID, never `pkill -f`.
