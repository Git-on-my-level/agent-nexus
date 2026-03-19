# organization-autorunner

Monorepo for Organization Autorunner.

## Layout

- `contracts/`: canonical OpenAPI + schema contracts and generated artifacts
- `core/`: Go backend (`oar-core`)
- `cli/`: Go CLI (`oar`)
- `web-ui/`: SvelteKit frontend (`oar-ui`)

## Hosted v1 target state

Hosted v1 is a managed offering, not a public self-service SaaS. The
authoritative architecture cut line for this ticket pack is:

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

These docs describe the hosted-v1 target state. Some implementation details in
the current v0 codebase still reflect the older reference setup; later tickets
will close those gaps explicitly rather than pretending they already landed.

Architecture references:

- `docs/architecture/foundation.md`
- `docs/architecture/hosted-v1.md`
- `docs/architecture/hosted-gate.md`

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
- `make hosted-smoke`: run hosted-v1 production smoke suite (auth gate, onboarding, workspace access, staleness, backup/restore)
- `make hosted-ops-test`: run hosted provisioning/backup/restore verification tests
- `make hosted-ops-smoke`: run one hosted provisioning/backup/restore smoke flow
- `make cli-integration-test`: run CLI real-binary integration tests (non-default)
- `make e2e-smoke`: run live core + CLI + web-ui smoke verification
- `make core-<target>`: pass through to `core/Makefile`
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
