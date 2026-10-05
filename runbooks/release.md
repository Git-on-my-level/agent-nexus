# Release Runbook

This runbook describes the repository-level release and verification process for `anx-core`, `anx`, and `anx-ui` contract compatibility.

Control-plane SaaS operations and release gates live in the private
`agent-nexus-saas/controlplane` repo.

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

SaaS control-plane validation moved to the private `agent-nexus-saas/controlplane`
repo and no longer runs from this OSS repository.

## CLI binary release automation

Workflow: `.github/workflows/release-cli.yml`

Preferred one-command path for a standard patch release from `origin/main`:

```bash
make release-patch
```

That flow:

- rejects dirty/untracked files and missing or broken active repo hook tooling before running checks (run `make setup` in each fresh worktree)
- fetches `origin/main` and tags
- computes the next patch version from the latest tag on `origin/main`
- verifies the checkout is clean and exactly matches `origin/main`
- resumes from the current `main` commit when all version-managed files already match the target and its tag is absent; otherwise it runs the local checks and creates the release prep commit
- waits for the `CI` and `System Smokes` workflows on the release commit, tags it, and waits for the `Release CLI` workflow to publish it

Keep release logs outside the checkout (or in an already ignored directory).
The script checks cleanliness again after checks and before changing versions,
so generated changes can be inspected separately. It does not automatically
discard version edits after later failures or undo published commits/tags.
`--dry-run` does not require commit hook tooling and reports whether it would
create a release prep commit or resume one already on `main`. Offline preflight
regression tests run with `python3 scripts/tests/test_release_preflight.py`.

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

Prepare the repo version before tagging. `./scripts/set-version.sh` is the canonical release-prep step: it updates [`VERSION`](../VERSION), syncs generated CLI/core/web version metadata, keeps `web-ui/package.json` aligned automatically, and updates the bridge package version. Use `./scripts/version-managed-files.sh` if you need the canonical release-prep file list. Either update it locally:

```bash
./scripts/set-version.sh v0.0.4
git add VERSION cli/internal/buildinfo/version_generated.go core/internal/buildinfo/version_generated.go adapters/agent-bridge/pyproject.toml web-ui/src/lib/generated/version.js web-ui/package.json
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

### CLI compatibility floor

`min_cli_version` is the wire-compatibility floor in [`core/internal/buildinfo/compatibility.go`](../core/internal/buildinfo/compatibility.go) (`MinCompatibleCLI`). Ordinary releases must not change it, and [`scripts/set-version.sh`](../scripts/set-version.sh) does not. `recommended_cli_version` tracks this core release (`VERSION`).

Raise `MinCompatibleCLI` only when a release breaks CLI/core wire compatibility, in the same release that requires the newer CLI, and record the break in this runbook. Operators can override a deployment with `ANX_MIN_CLI_VERSION` or `--min-cli-version` without changing the recommended version (`ANX_RECOMMENDED_CLI_VERSION`, `--recommended-cli-version`).

A client below the floor receives HTTP 426 `cli_outdated`. A managed CLI whose update policy is `auto` installs the recommended release with the verified updater and retries the original command once. Policy `notify` or `off`, and every unmanaged install, prints `anx update --version <recommended>` and does not replace the binary.

### Python `anx-agent-bridge` package version sync

[`scripts/sync-version.sh`](../scripts/sync-version.sh) bumps `project.version` in [`adapters/agent-bridge/pyproject.toml`](../adapters/agent-bridge/pyproject.toml) alongside the CLI/core/UI metadata so packaged bridge semver stays aligned with the repo tag. Run it (via `./scripts/set-version.sh` release prep or directly) before you tag. Publishing wheels to PyPI is optional and not wired in OSS CI yet; when you publish, ship the artifact for the tagged version after the Git tag exists so `pip install anx-agent-bridge` does not drift from `anx bridge install`'s pinned source install.

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
curl -sSfL https://raw.githubusercontent.com/Git-on-my-level/agent-nexus/main/scripts/install-anx.sh | sh
```

Pin a specific version:

```bash
curl -sSfL https://raw.githubusercontent.com/Git-on-my-level/agent-nexus/main/scripts/install-anx.sh | VERSION="$(./scripts/read-version.sh)" sh
```

Custom install directory:

```bash
curl -sSfL https://raw.githubusercontent.com/Git-on-my-level/agent-nexus/main/scripts/install-anx.sh | INSTALL_DIR=/usr/local/bin sh
```

The script detects OS/arch, downloads the correct archive from the GitHub release, verifies the SHA-256 checksum, and places the `anx` binary in `~/.local/bin` (or the specified `INSTALL_DIR`).

After install, enroll the host and select a derived agent:

```bash
anx --base-url http://<core-host>:8000 host enroll --name <host-slug>
anx --base-url http://<core-host>:8000 --as <agent-name> auth whoami
```

## Post-release validation

1. Download one target archive and verify checksum:

```bash
sha256sum -c checksums.txt --ignore-missing
```

2. Verify handshake compatibility with live core:

```bash
anx --base-url http://127.0.0.1:8000 --as release-check api call --path /meta/handshake
# Add --json if you need the CLI JSON envelope (e.g. scripted parsing).
```

3. Confirm generated docs and meta are current:

```bash
./scripts/contract-check --committed
```

## Failure recovery

- If release build matrix fails: inspect failed target archive build job logs.
- If checksum generation fails: verify artifact download and file naming patterns in the workflow.
- If clients fail with `cli_outdated`: check `/meta/handshake`. `min_cli_version` should stay at `MinCompatibleCLI` unless this release broke wire compatibility. `recommended_cli_version` should match the published CLI. Managed `auto` installs update on the 426 and retry once; other installs need `anx update --version <recommended>`.
