# Release Runbook

This runbook describes the repository-level release and verification process for `oar-core`, `oar`, and `oar-ui` contract compatibility.

## Pre-release checks

Run from repo root:

```bash
pnpm install
make check
make e2e-smoke
```

Required outcomes:

- contract drift check passes (`make contract-check`)
- core, cli, and web-ui checks pass
- end-to-end smoke script passes (core startup, CLI auth/token refresh/typed commands/streams, UI startup compatibility)

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
