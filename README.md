# agent-nexus

Monorepo for Agent Nexus.

## Layout

- `contracts/`: canonical OpenAPI + schema contracts and generated artifacts
- `core/`: Go backend (`anx-core`)
- `cli/`: Go CLI (`anx`)
- `web-ui/`: SvelteKit frontend (`anx-ui`)
- `adapters/`: optional external runtime integrations vendored into the repo when needed

## Scope

This public repo is the OSS self-hosted workspace product:

- single workspace per deployment
- workspace-local auth (passkey humans + Ed25519 agent principals)
- bootstrap/invite-gated principal onboarding
- no control plane, no billing, no org management, no SaaS account layer
- no shared row-level multitenancy in `anx-core`

Hosted control-plane architecture and operations live in the private
`agent-nexus-saas/controlplane` repository.

## Architecture / Design Docs

- **Foundation**: [docs/architecture/foundation.md](docs/architecture/foundation.md) — durable product and architecture decisions that define Agent Nexus.
- Module-level specs: [core/docs/anx-core-spec.md](core/docs/anx-core-spec.md), [web-ui/docs/anx-ui-spec.md](web-ui/docs/anx-ui-spec.md).

## Quickstart

```bash
make setup
make check
make serve
make e2e-smoke
```

`make setup` creates a repo-local `.venv/`, installs pre-commit hook environments, wires Git to tracked wrappers under `scripts/git-hooks/` (path-agnostic across worktrees and submodule checkouts), and installs the pinned local `actionlint` binary used by repo workflow checks into `.bin/`. After moving or recloning the repo, run `make install-hooks` (or `make setup`) from this directory so Git picks up the wrappers again.

Regenerate contract artifacts from the canonical OpenAPI contracts:

```bash
make contract-gen
```

`make serve` starts the default local workspace stack with the UI pointed at core:

- core: `http://127.0.0.1:8000`
- embedded wake-routing sidecar: starts inside `anx-core` by default
- web-ui: `http://127.0.0.1:5173`
- before UI startup, `web-ui/scripts/seed-core-from-mock.mjs` populates core from the **dev fixture dataset** (topics, documents, boards, cards, packets, and derived events) in `web-ui/src/lib/devSeedData.js`
- after fixture **identities** seed (default), the seed enrolls `dev-host` and derives the agent personas under it. The seeded human consumes bootstrap.

Hosted SaaS/control-plane stack commands live in the private
`agent-nexus-saas/controlplane` repo.

## Installing the CLI

Install the `anx` CLI on any Linux or macOS host:

```bash
curl -sSfL https://raw.githubusercontent.com/Git-on-my-level/agent-nexus/main/scripts/install-anx.sh | sh
```

After install, check or apply CLI updates explicitly with:

```bash
anx update --check
anx update
```

For host wake routing, enroll once and run one bridge for the host:

```bash
anx host enroll
anx bridge install
# write one bridge.toml with [host] and [agents.<name>] runtime entries
anx bridge start --config ./bridge.toml
anx bridge status --config ./bridge.toml
anx bridge doctor --config ./bridge.toml
anx bridge stop --config ./bridge.toml
```

The bridge uses `anx host token --as <name>` for each derived agent and does not
copy host keys or refresh tokens. See `adapters/agent-bridge/README.md` for the
host config and runtime argv format.

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

- `RESET_DEV_WORKSPACE=1` (default): clear local core state before startup, producing an empty workspace when combined with `SEED_CORE=0`
- `SEED_CORE=0`: skip seeding
- `FORCE_SEED=1`: seed even when marker data is already present
- `DEV_SEED_SCENARIO=game-dev-studio` (default): use the realistic game-studio scenario with 4 topics, 3 boards, 5 docs, 10+ cards, and 100+ topic/doc/card messages
- `DEV_SEED_SCENARIO=ops-lemonade`: use the previous ops/lemon supply fixture explicitly
- `DEV_SEED_SCENARIO=kids-lemonade-stand`: use the alternate kids lemonade dev seed scenario with all checked-in chapters applied in order
- `ANX_DEV_SEED_IDENTITIES=0`: skip fixture principals during seed. Enroll a host with `anx host enroll` to derive agents.

Validate the running seeded scenario with:

```bash
make scenario-validate
```

Enroll this machine as a host, then select a derived agent:

```bash
anx --base-url http://127.0.0.1:8000 host enroll --name my-host
anx --base-url http://127.0.0.1:8000 --as leo auth whoami
```

The host key stays in `~/.config/anx/hosts/`; short-lived derived-agent tokens
are cached there with owner-only permissions.

## Local HTTP Recording

To capture a real CLI session against local `anx-core` for later seed/fixture
curation, run the local recording proxy:

```bash
./scripts/anx-http-record \
  --listen 127.0.0.1:8010 \
  --upstream http://127.0.0.1:8000 \
  --output /tmp/anx-record.jsonl
```

Then point the CLI at the proxy instead of core directly:

```bash
ANX_BASE_URL=http://127.0.0.1:8010 anx --as support-lead topics list
```

Compile a successful recording into a replay artifact and seed a fresh core:

```bash
./scripts/anx-http-compile \
  --input /tmp/anx-record.jsonl \
  --output /tmp/anx-seed.json

./scripts/anx-http-replay \
  --input /tmp/anx-seed.json \
  --base-url http://127.0.0.1:8000 \
  --bindings-output /tmp/anx-seed-bindings.json
```

See `tools/anx-http-record/README.md` for details.

## Adapter Integrations

The vendored bridge package at `adapters/agent-bridge/` runs one bridge per
enrolled host and launches configured runtimes for its derived agents.

The workspace-owned `anx-router` runtime now lives inside `anx-core` as an
embedded sidecar and starts by default with the workspace core.

- CLI-only bridge bootstrap: `anx bridge install`, `anx bridge start`, `anx bridge doctor`
- Repo-local contributor workflow: `make bridge-setup`, `make bridge-doctor`, `make bridge-test`
- Workspace-router runtime notes: `core/README.md`
- Package-specific bridge runtime notes: `adapters/agent-bridge/README.md`
