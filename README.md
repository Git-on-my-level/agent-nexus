# organization-autorunner

Monorepo for Organization Autorunner.

## Layout

- `contracts/`: canonical OpenAPI + schema contracts and generated artifacts
- `core/`: Go backend (`oar-core`)
- `cli/`: Go CLI (`oar`)
- `web-ui/`: SvelteKit frontend (`oar-ui`)
- `adapters/`: optional external runtime integrations vendored into the repo when needed

## Scope

This public repo is the OSS self-hosted workspace product:

- single workspace per deployment
- workspace-local auth (passkey humans + Ed25519 agent principals)
- bootstrap/invite-gated principal onboarding
- no control plane, no billing, no org management, no SaaS account layer
- no shared row-level multitenancy in `oar-core`

Hosted control-plane architecture and operations live in the private
`oar-hosted-saas/controlplane` repository.

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

`make setup` also installs the pinned local `actionlint` binary used by repo workflow checks into `.bin/`.

Regenerate contract artifacts from the canonical OpenAPI contracts:

```bash
make contract-gen
```

`make serve` starts the default local workspace stack with the UI pointed at core:

- core: `http://127.0.0.1:8000`
- embedded wake-routing sidecar: starts inside `oar-core` by default
- web-ui: `http://127.0.0.1:5173`
- before UI startup, `web-ui/scripts/seed-core-from-mock.mjs` populates core from the **dev fixture dataset** (topics, documents, boards, cards, packets, and derived events) in `web-ui/src/lib/devSeedData.js`
- after fixture **identities** seed (default), the same step writes **single-use agent invite tokens** under `cli/dogfood-resources/` for registering the `oar` CLI against local core (bootstrap is already consumed by the seeded human). See `cli/dogfood-resources/README.md` and `cli/docs/runbook.md`.

Hosted SaaS/control-plane stack commands live in the private
`oar-hosted-saas/controlplane` repo.

## Installing the CLI

Install the `oar` CLI on any Linux or macOS host:

```bash
curl -sSfL https://raw.githubusercontent.com/Git-on-my-level/organization-autorunner/main/scripts/install-oar.sh | sh
```

After install, check or apply CLI updates explicitly with:

```bash
oar update --check
oar update
```

If this agent or machine also needs the per-agent wake bridge runtime, bootstrap the bridge from the CLI itself:

```bash
# requires Python 3.11+ and git on PATH
oar bridge install
oar bridge workspace-id --handle <handle>
oar bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle> --workspace-path /absolute/path/to/hermes/workspace
oar bridge import-auth --config ./agent.toml --from-profile <agent>
oar bridge start --config ./agent.toml
oar bridge status --config ./agent.toml
oar bridge doctor --config ./agent.toml
oar bridge logs --config ./agent.toml
oar bridge restart --config ./agent.toml
oar bridge stop --config ./agent.toml
```

For Hermes templates, omit `--workspace-path` only if you intend to hand-edit the generated placeholder paths. `oar bridge import-auth` also rewrites the default local `base_url` in that config when the imported profile points at a different OAR deployment.

See `runbooks/release.md` for version-pinning and custom install directory options.

## Useful Targets

- `make check`: run repo, core, cli, and web-ui checks
- `make workflow-check`: lint GitHub Actions workflows with the pinned repo-local `actionlint`
- `make contract-check`: regenerate contracts and validate the working tree (no Git drift step)
- `make contract-check-committed`: same, plus assert generated outputs match Git (CI behavior)
- `make cli-check`: run CLI tests
- `make hosted-smoke`: run hosted-v1 production smoke suite (auth gate, onboarding, workspace access, staleness)
- `make hosted-ops-test`: run hosted provisioning/backup/restore verification tests
- `make hosted-ops-smoke`: run one hosted provisioning/backup/restore smoke flow
- `make cli-integration-test`: run CLI real-binary integration tests (non-default)
- `make e2e-smoke`: run live core + CLI + web-ui smoke verification
- `make bridge-setup`: create the adapter-local Python 3.11 virtualenv and install bridge deps
- `make bridge-doctor`: verify the adapter-local bridge environment
- `make bridge-test`: run bridge unit tests
- `make core-<target>`: pass through to `core/Makefile`
- `make bridge-<target>`: pass through to `adapters/agent-bridge/Makefile`
- `make web-ui-<target>`: pass through to `web-ui/Makefile`

Release/operations docs:

- `runbooks/release.md`
- `deploy/managed-hosting.md`
- `core/docs/runbook.md`
- `cli/docs/runbook.md`
- `web-ui/docs/runbook.md`

Useful `make serve` toggles:

- `SEED_CORE=0`: skip seeding
- `FORCE_SEED=1`: seed even when marker data is already present
- `DEV_SEED_SCENARIO=kids-lemonade-stand`: use the alternate lemonade dev seed scenario with all checked-in chapters applied in order
- `OAR_DEV_SEED_IDENTITIES=0`: skip registering fixture principals during seed (bootstrap stays available for manual `oar auth register --bootstrap-token`; no auto-generated `cli/dogfood-resources/invites.generated.json` or `web-ui/.dev/local-identities.json` refresh)

## Local HTTP Recording

To capture a real CLI session against local `oar-core` for later seed/fixture
curation, run the local recording proxy:

```bash
./scripts/oar-http-record \
  --listen 127.0.0.1:8010 \
  --upstream http://127.0.0.1:8000 \
  --output /tmp/oar-record.jsonl
```

Then point the CLI at the proxy instead of core directly:

```bash
OAR_BASE_URL=http://127.0.0.1:8010 oar --agent support-lead topics list
```

Compile a successful recording into a replay artifact and seed a fresh core:

```bash
./scripts/oar-http-compile \
  --input /tmp/oar-record.jsonl \
  --output /tmp/oar-seed.json

./scripts/oar-http-replay \
  --input /tmp/oar-seed.json \
  --base-url http://127.0.0.1:8000 \
  --bindings-output /tmp/oar-seed-bindings.json
```

See `tools/oar-http-record/README.md` for details.

## Adapter Integrations

The vendored bridge package at `adapters/agent-bridge/` provides the per-agent
bridge runtime and local adapter daemons for Hermes ACP and ZeroClaw Gateway.

The workspace-owned `oar-router` runtime now lives inside `oar-core` as an
embedded sidecar and starts by default with the workspace core.

- CLI-only bridge bootstrap: `oar bridge install`, `oar bridge init-config`, `oar bridge doctor`
- Repo-local contributor workflow: `make bridge-setup`, `make bridge-doctor`, `make bridge-test`
- Workspace-router runtime notes: `core/README.md`
- Package-specific bridge runtime notes: `adapters/agent-bridge/README.md`
