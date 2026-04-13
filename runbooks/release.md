# Release Runbook

This runbook describes the repository-level release and verification process for `oar-core`, `oar`, and `oar-ui` contract compatibility.

For packed-host SaaS operations, see [`packed-host-configuration.md`](packed-host-configuration.md) and related runbooks.

## Pre-release checks

Run from repo root:

```bash
make setup
make check
make e2e-smoke
```

`make e2e-smoke` now auto-selects unused local ports if the default smoke ports are
already occupied. Set `AUTO_SELECT_PORTS=0` to keep the old fail-fast behavior.

Required outcomes:

- contract drift check passes (`make contract-check-committed`)
- core, cli, and web-ui checks pass
- end-to-end smoke script passes (core startup, CLI auth/token refresh/typed commands/streams, UI startup compatibility)

### Hosted gates

Hosted validation runs automatically in CI when relevant code changes:

- `make hosted-ops-test` - runs on hosted-sensitive changes or root/build files (core, contracts, web-ui, `scripts/hosted/**`, `scripts/hosted-smoke`, deploy, workflow files, `Makefile`, `package.json`, `pnpm-workspace.yaml`, `pnpm-lock.yaml`)
- `make hosted-smoke` - runs on the same hosted-sensitive changes or root/build files

These gates are first-class CI jobs and do not require manual workflow dispatch.

For local validation or pre-release checks outside CI:

```bash
make hosted-ops-test
make hosted-smoke
```

### SaaS gates

SaaS (self-serve control-plane) validation runs automatically in CI when relevant code changes:

- `make saas-smoke` - multi-workspace control-plane smoke (account, organization, workspace provisioning, invites, launch/session exchange, workspace read/write)
- `make saas-e2e` - extended flow including workspace isolation, backup jobs, session revocation, and re-authentication
- `make saas-load-smoke` - basic load test with multiple workspaces and concurrent operations

These gates trigger on changes to core, contracts, web-ui, scripts, deploy, workflow files, and Makefile.

For local validation or pre-release checks outside CI:

```bash
make saas-smoke
make saas-e2e
make saas-load-smoke
make packed-host-smoke
```

### Environment variables for load testing

The load smoke test can be tuned:

```bash
SAAS_LOAD_NUM_WORKSPACES=3    # number of workspaces to provision (default: 3)
SAAS_LOAD_NUM_THREADS=5       # threads per workspace (default: 5)
SAAS_LOAD_CONCURRENT=3        # concurrent read requests per workspace (default: 3)
make saas-load-smoke
```

## CLI binary release automation

Workflow: `.github/workflows/release-cli.yml`

Preferred one-command path for a standard patch release from `origin/main`:

```bash
make release-patch
```

That flow:

- fetches `origin/main` and tags
- computes the next patch version from the latest tag on `origin/main`
- verifies the checkout is clean and exactly matches `origin/main`
- runs `make check`, `make e2e-smoke`, and local release artifact validation
- updates version metadata, commits the release prep, pushes `main`, tags the release, and waits for the `Release CLI` workflow to publish it

Useful variants:

```bash
./scripts/release-patch.sh --dry-run
VERSION=v0.0.13 make release-patch
RELEASE_ARGS="--no-wait" make release-patch
```

Validate the packaging output locally before you push a tag:

```bash
make cli-check
VERSION="$(./scripts/read-version.sh)"
./scripts/build-cli-release-artifacts.sh "$VERSION"
```

Prepare the repo version before tagging. `./scripts/set-version.sh` is the canonical release-prep step: it updates [`VERSION`](../VERSION), syncs generated CLI/core/web version metadata, and keeps `web-ui/package.json` aligned automatically. Either update it locally:

```bash
./scripts/set-version.sh v0.0.4
git add VERSION cli/internal/buildinfo/version_generated.go core/internal/buildinfo/version_generated.go web-ui/src/lib/generated/version.js web-ui/package.json
git commit -m "Prepare CLI release v0.0.4"
git push
```

Or use the manual GitHub workflow `.github/workflows/prepare-cli-release.yml`
to open a draft PR that stages the requested version bump for review.

This produces:

- linux/darwin/windows archives for `amd64` + `arm64`
- SHA-256 checksum manifest (`checksums.txt`)

Cut a release by pushing an annotated tag:

```bash
VERSION="$(./scripts/read-version.sh)"
git tag -a "$VERSION" -m "Release $VERSION"
git push origin "$VERSION"
```

The workflow fails if the pushed tag does not match the committed [`VERSION`](../VERSION) file or if generated CLI version metadata is stale.

### Painpoint status

Resolved:

- PR-time auth/smoke coverage runs automatically for auth-sensitive and contract-sensitive changes
- release-prep reruns reopen or reuse the staged draft PR instead of silently succeeding without a review path
- rerunning CLI release publication no longer risks deleting existing GitHub release assets on upload failure
- repo release prep now aligns CLI, core, and web-ui version metadata from the single `VERSION` source of truth

Still not solved:

- the tag-driven GitHub workflow publishes CLI binaries only; core deployment publication and web deployment rollout remain outside this automation path

Then watch the GitHub workflow and confirm the published release:

```bash
gh run watch --workflow "Release CLI"
gh release view "$(./scripts/read-version.sh)"
```

`make release-patch` already performs both of those confirmation steps unless you
pass `RELEASE_ARGS="--no-wait"`.

## Installing the CLI on agent hosts

One-command install (latest release):

```bash
curl -sSfL https://raw.githubusercontent.com/Git-on-my-level/organization-autorunner/main/scripts/install-oar.sh | sh
```

Pin a specific version:

```bash
curl -sSfL https://raw.githubusercontent.com/Git-on-my-level/organization-autorunner/main/scripts/install-oar.sh | VERSION="$(./scripts/read-version.sh)" sh
```

Custom install directory:

```bash
curl -sSfL https://raw.githubusercontent.com/Git-on-my-level/organization-autorunner/main/scripts/install-oar.sh | INSTALL_DIR=/usr/local/bin sh
```

The script detects OS/arch, downloads the correct archive from the GitHub release, verifies the SHA-256 checksum, and places the `oar` binary in `~/.local/bin` (or the specified `INSTALL_DIR`).

After install, register the agent with core:

```bash
oar --base-url http://<core-host>:8000 register --agent <agent-name>
```

## Post-release validation

1. Download one target archive and verify checksum:

```bash
sha256sum -c checksums.txt --ignore-missing
```

2. Verify handshake compatibility with live core:

```bash
oar --base-url http://127.0.0.1:8000 --agent release-check api call --path /meta/handshake
# Add --json if you need the CLI JSON envelope (e.g. scripted parsing).
```

3. Confirm generated docs and meta are current:

```bash
./scripts/contract-check --committed
```

## Failure recovery

- If release build matrix fails: inspect failed target archive build job logs.
- If checksum generation fails: verify artifact download and file naming patterns in the workflow.
- If clients fail with `cli_outdated`: check `/meta/handshake` and publish updated CLI binaries.
