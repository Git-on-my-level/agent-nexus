# Release Runbook

This runbook describes the repository-level release and verification process for `oar-core`, `oar`, and `oar-ui` contract compatibility.

## Pre-release checks

Run from repo root:

```bash
make setup
make check
make e2e-smoke
```

Required outcomes:

- contract drift check passes (`make contract-check`)
- core, cli, and web-ui checks pass
- end-to-end smoke script passes (core startup, CLI auth/token refresh/typed commands/streams, UI startup compatibility)

### Hosted gates

Hosted validation runs automatically in CI when relevant code changes:

- `make hosted-ops-test` - runs on hosted-sensitive changes (core, contracts, web-ui, scripts/hosted, deploy, workflow files)
- `make hosted-smoke` - runs on hosted-sensitive changes

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

Trigger by pushing a release tag:

```bash
git tag v0.2.0
git push origin v0.2.0
```

Release workflow outputs:

- linux/darwin/windows archives for `amd64` + `arm64`
- SHA256 checksum manifest (`checksums.txt`)

## Installing the CLI on agent hosts

One-command install (latest release):

```bash
curl -sSfL https://raw.githubusercontent.com/Git-on-my-level/organization-autorunner/main/scripts/install-oar.sh | sh
```

Pin a specific version:

```bash
curl -sSfL https://raw.githubusercontent.com/Git-on-my-level/organization-autorunner/main/scripts/install-oar.sh | VERSION=v0.2.0 sh
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
oar --json --base-url http://127.0.0.1:8000 --agent release-check api call --path /meta/handshake
```

3. Confirm generated docs and meta are current:

```bash
./scripts/contract-check
```

## Failure recovery

- If release build matrix fails: inspect failed target archive build job logs.
- If checksum generation fails: verify artifact download and file naming patterns in the workflow.
- If clients fail with `cli_outdated`: check `/meta/handshake` and publish updated CLI binaries.
