# organization-autorunner

Monorepo for Organization Autorunner.

## Layout

- `contracts/`: canonical OpenAPI + schema contracts and generated artifacts
- `core/`: Go backend (`oar-core`)
- `cli/`: Go CLI (`oar`)
- `web-ui/`: SvelteKit frontend (`oar-ui`)

## Architecture / Design Docs

- **Foundation**: [docs/architecture/foundation.md](docs/architecture/foundation.md) — durable product and architecture decisions that define OAR.
- Module-level specs: [core/docs/oar-core-spec.md](core/docs/oar-core-spec.md), [web-ui/docs/oar-ui-spec.md](web-ui/docs/oar-ui-spec.md).

## Quickstart

```bash
make setup
make check
make serve
make e2e-smoke
```

Regenerate contract artifacts from the canonical OpenAPI contract:

```bash
make contract-gen
```

`make serve` starts both services with the UI pointed at core:

- core: `http://127.0.0.1:8000`
- web-ui: `http://127.0.0.1:5173`
- before UI startup, `web-ui/scripts/seed-core-from-mock.mjs` populates core using the mock dataset

## Installing the CLI

Install the `oar` CLI on any Linux or macOS host:

```bash
curl -sSfL https://raw.githubusercontent.com/Git-on-my-level/organization-autorunner/main/scripts/install-oar.sh | sh
```

See `runbooks/release.md` for version-pinning and custom install directory options.

## Useful Targets

- `make check`: run checks for both projects
- `make contract-check`: verify generated contract artifacts are up to date
- `make cli-check`: run CLI tests
- `make cli-integration-test`: run CLI real-binary integration tests (non-default)
- `make e2e-smoke`: run live core + CLI + web-ui smoke verification
- `make core-<target>`: pass through to `core/Makefile`
- `make web-ui-<target>`: pass through to `web-ui/Makefile`

Release/operations docs:

- `runbooks/release.md`
- `core/docs/runbook.md`
- `cli/docs/runbook.md`
- `web-ui/docs/runbook.md`

Useful `make serve` toggles:

- `SEED_CORE=0`: skip seeding
- `FORCE_SEED=1`: seed even when marker data is already present
