# oar-cli Runbook

This runbook covers local development, end-to-end smoke usage, release steps, and common troubleshooting for `oar`.

## Local development

Build and test:

```bash
cd cli
go build ./cmd/oar
go test ./...
go test -tags=integration ./integration/...
```

Run against local core:

```bash
cd cli
go run ./cmd/oar --json --base-url http://127.0.0.1:8000 --agent local version
go run ./cmd/oar --json --base-url http://127.0.0.1:8000 --agent local doctor
go run ./cmd/oar --json --base-url http://127.0.0.1:8000 --agent local auth register --username local.agent
go run ./cmd/oar --agent local version
```

Global config precedence:

1. command-line flags
2. environment variables
3. profile file (`~/.config/oar/profiles/<agent>.json`)
4. defaults

Supported env vars:

- `OAR_BASE_URL`
- `OAR_AGENT`
- `OAR_JSON`
- `OAR_NO_COLOR`
- `OAR_TIMEOUT`
- `OAR_PROFILE_PATH`
- `OAR_ACCESS_TOKEN`
- `OAR_USERNAME`

## Auth/profile lifecycle

Registration and profile bootstrap:

```bash
oar --json --base-url http://127.0.0.1:8000 --agent agent-a auth register --username agent.a
oar --agent agent-a auth whoami
oar --agent agent-a auth token-status
```

Rotation/update/revoke:

```bash
oar --agent agent-a auth update-username --username agent.a.renamed
oar --agent agent-a auth rotate
oar --agent agent-a auth revoke
```

Profile material paths:

- profile: `~/.config/oar/profiles/<agent>.json`
- private key: `~/.config/oar/keys/<agent>.ed25519`

Permissions are enforced by CLI runtime (`0700` dirs, `0600` files).

## Integration Scenarios

Deterministic multi-step CLI regression coverage lives under `cli/integration/` and is intentionally excluded from cheap default test runs.

Run the suite against live `oar-core` processes spun up by the tests:

```bash
cd cli
go test -tags=integration ./integration/...
```

These tests:
- build the real `oar` and `oar-core` binaries
- copy the repo's workspace snapshot into a temp directory
- run multi-step thread/event and docs/conflict flows through the real CLI

## Pi Dogfood

The supported manual dogfood path is the Pi-based runner under `cli/dogfood/pi/`.

Install and run Pi dogfood:

```bash
pnpm install --filter @organization-autorunner/pi-dogfood...

pnpm --dir cli/dogfood/pi run pilot-rescue -- \
  --api-key-file ../../.secrets/zai_api_key \
  --provider zai \
  --model glm-5
```

The runner:
- builds `oar` and `oar-core`
- starts a managed temporary core on a random local port
- seeds that core from CLI-owned dogfood data under `cli/dogfood/pi/seed/`
- runs Pi against the isolated seeded environment
- writes artifacts under `cli/.tmp/pi-dogfood/`

## Typed Command Smoke

```bash
printf '{"thread":{"title":"Incident #42"}}\n' | oar --agent agent-a threads create
oar --agent agent-a threads list --status active

oar --agent agent-a events stream --max-events 1
oar --agent agent-a inbox stream --max-events 1
oar --agent agent-a events stream --follow
oar --agent agent-a events list --thread-id thread_123 --thread-id thread_456 --type actor_statement --mine --full-id --max-events 20
oar --agent agent-a threads context --thread-id thread_123 --max-events 50
oar --agent agent-a docs content --document-id product-constitution
oar --agent agent-a commitments inspect --commitment-id commitment_123
oar --agent agent-a artifacts inspect --artifact-id artifact_123
```

Draft/commit flow:

```bash
cat payload.json | oar --agent agent-a draft create --command threads.create
oar --agent agent-a draft list
oar --agent agent-a draft commit <draft-id>
oar --agent agent-a draft discard <draft-id>
```

The raw fallback remains available:

```bash
oar --json --base-url http://127.0.0.1:8000 --agent agent-a api call --path /meta/handshake
```

## Release process

CLI release artifacts are produced by GitHub workflow:

- workflow: `.github/workflows/release-cli.yml`
- trigger: push tag `v*` or `oar-cli-v*`
- outputs:
  - static binaries for linux/darwin/windows on amd64/arm64
  - release archives (`.tar.gz`/`.zip`)
  - `checksums.txt` (SHA256)

Maintainer checklist:

1. Ensure `make check` and `make e2e-smoke` pass on `main`.
2. Create and push a release tag (for example `v0.2.0`).
3. Verify release assets and `checksums.txt` on the GitHub release page.
4. Verify handshake compatibility with a live core:
   - `oar --json meta command meta.handshake`
   - `oar --json --base-url <core> --agent <agent> api call --path /meta/handshake`

## Troubleshooting

### Auth/profile failures

Symptoms:

- `profile_not_found`
- `key_mismatch`
- `invalid_token`
- `agent_revoked`

Actions:

1. Check selected agent/profile:

```bash
oar --json --agent <agent> auth token-status
```

2. Verify profile file exists and is readable (`~/.config/oar/profiles/<agent>.json`).
3. If key mismatch after key/manual edits, run `auth rotate` (if possible) or `auth register` with a new agent profile.
4. If revoked, create/register a new agent profile; revoked profiles cannot recover tokens.

### Version mismatch

Symptoms:

- server returns `cli_outdated`
- commands fail before mutation with compatibility errors

Actions:

1. Inspect handshake metadata:

```bash
oar --json --base-url <core> --agent <agent> api call --path /meta/handshake
```

2. Compare current CLI version against:

- `min_cli_version`
- `recommended_cli_version`
- `cli_download_url`

3. Upgrade CLI binary and re-run `oar version` + `oar doctor`.

### SSE stream issues (`events stream` / `inbox stream`)

Symptoms:

- no events received
- reconnect loops
- dropped stream behavior

Actions:

1. Validate core stream endpoints directly:

```bash
curl -N -H 'Accept: text/event-stream' http://127.0.0.1:8000/events/stream
curl -N -H 'Accept: text/event-stream' http://127.0.0.1:8000/inbox/stream
```

2. Use explicit cursor controls:

- `--last-event-id <id>`
- `--cursor <id>` (alias)

3. For deterministic scripts use bounded streams:

- `--max-events <n>`
- omit `--follow` (default drains and exits)

4. Verify server-side poll cadence and stream health in core logs.
