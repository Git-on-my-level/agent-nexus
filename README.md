# organization-autorunner

Monorepo for Organization Autorunner.

## Layout

- `contracts/`: canonical OpenAPI + schema contracts and generated artifacts
- `core/`: Go backend (`oar-core`)
- `cli/`: Go CLI (`oar`)
- `web-ui/`: SvelteKit frontend (`oar-ui`)
- `adapters/`: optional external runtime integrations vendored into the repo when needed

## Hosted v1

Hosted v1 is a managed offering, not a public self-service SaaS. The
authoritative architecture cut line in this branch is:

- one isolated workspace deployment per customer/workspace
- managed provisioning plus managed backup/restore scripts
- no required self-service control plane in this pack
- no shared row-level multitenancy
- auth required on all workspace data routes outside development mode
- `OAR_ALLOW_UNAUTHENTICATED_WRITES` and UI actor-selection flows are
  development-only
- passkey humans and Ed25519 key-pair agents are both workspace principals
- public registration is closed; onboarding is bootstrap/invite-gated
- no fine-grained RBAC in v1; authenticated principals share the same authority
- agents should prefer the CLI and generated clients over hand-authored HTTP
- workspace projection APIs are convenience reads, not durable automation
  contracts

## SaaS v-next

SaaS v-next is the explicit self-serve direction. It layers:

- one shared control plane for human accounts, organizations, workspace
  registry, provisioning/lifecycle jobs, usage/quota envelopes, and fleet
  metadata
- one isolated workspace core per workspace for durable OAR truth
- control-plane-managed human auth plus workspace-scoped launch/session grants
- workspace-local agent auth that stays inside each isolated workspace
- the current workspace noun and path-based human UI shape where possible

Architecture references:

- `docs/architecture/foundation.md`
- `docs/architecture/hosted-v1.md`
- `docs/architecture/saas-v-next.md`
- `docs/architecture/hosted-gate.md`

## Architecture / Design Docs

- **Foundation**: [docs/architecture/foundation.md](docs/architecture/foundation.md) — durable product and architecture decisions that define OAR.
- **Hosted v1**: [docs/architecture/hosted-v1.md](docs/architecture/hosted-v1.md) — architecture for the managed offering.
- **SaaS v-next**: [docs/architecture/saas-v-next.md](docs/architecture/saas-v-next.md) — architecture for the self-serve control plane plus isolated workspace cores direction.
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

`make serve` starts both services with the UI pointed at core:

- core: `http://127.0.0.1:8000`
- web-ui: `http://127.0.0.1:5173`
- before UI startup, `web-ui/scripts/seed-core-from-mock.mjs` populates core using the mock dataset

For SaaS-v-next control-plane work, start the shared control plane in a second
terminal:

```bash
make serve-control-plane
```

Defaults:

- control plane: `http://127.0.0.1:8100`

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

If this agent or machine also needs the wake-routing bridge runtime, bootstrap it from the CLI itself:

```bash
# requires Python 3.11+ and git on PATH
oar bridge install
oar bridge workspace-id --handle <handle>
oar bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle>
oar bridge import-auth --config ./agent.toml --from-profile <agent>
oar bridge start --config ./agent.toml
oar bridge status --config ./agent.toml
oar bridge doctor --config ./agent.toml
oar bridge logs --config ./agent.toml
oar bridge restart --config ./agent.toml
oar bridge stop --config ./agent.toml
```

See `runbooks/release.md` for version-pinning and custom install directory options.

## Useful Targets

- `make check`: run repo, core, cli, and web-ui checks
- `make workflow-check`: lint GitHub Actions workflows with the pinned repo-local `actionlint`
- `make contract-check`: verify generated contract artifacts are up to date
- `make cli-check`: run CLI tests
- `make hosted-smoke`: run hosted-v1 production smoke suite (auth gate, onboarding, workspace access, staleness)
- `make hosted-ops-test`: run hosted provisioning/backup/restore verification tests
- `make hosted-ops-smoke`: run one hosted provisioning/backup/restore smoke flow
- `make saas-smoke`: run SaaS control-plane multi-workspace smoke (account, org, workspaces, invite, launch, session-exchange)
- `make saas-e2e`: run extended SaaS e2e flow (workspace isolation, backup, session revocation)
- `make saas-load-smoke`: run SaaS load smoke test (multiple workspaces with concurrent operations)
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
- `OAR_ENABLE_DEV_ACTOR_MODE=1`: enable development actor mode for legacy actor picker/creator UI (default: `false` / auth-first)

## Adapter Integrations

The vendored bridge package at `adapters/agent-bridge/` provides durable `@handle`
wake routing plus local adapter daemons for Hermes ACP and ZeroClaw Gateway.

- CLI-only bootstrap: `oar bridge install`, `oar bridge init-config`, `oar bridge doctor`
- Repo-local contributor workflow: `make bridge-setup`, `make bridge-doctor`, `make bridge-test`
- Package-specific runtime notes: `adapters/agent-bridge/README.md`
