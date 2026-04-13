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

Run against local core (default output is **text** on stdout; add **`--json`** or **`OAR_JSON=true`** when a script or program parses the CLI envelope):

```bash
cd cli
go run ./cmd/oar --base-url http://127.0.0.1:8000 --agent local version
go run ./cmd/oar --base-url http://127.0.0.1:8000 --agent local doctor
go run ./cmd/oar --base-url http://127.0.0.1:8000 --agent local auth bootstrap status
go run ./cmd/oar --base-url http://127.0.0.1:8000 --agent local auth register --username local.agent --bootstrap-token <token>
go run ./cmd/oar --agent local version
```

**Output modes:** concise text is the default for direct reading (including LLM tool output). JSON mode is for programmatic consumers (`jq`, CI, services). `auth register` does **not** write `"json": true` into the profile; older profiles may still set it—use `--json=false` / `OAR_JSON=false` for a single command if needed.

**Active profile (recommended for interactive use):** after you have at least one profile under `~/.config/oar/profiles/`, run `oar config use <name>` (or `oar auth default <name>`) once. The CLI stores the choice in `~/.config/oar/default-profile` and loads `base_url` and credentials from `~/.config/oar/profiles/<name>.json`, so later commands can omit `--base-url` and `--agent`. Inspect merged settings with `oar config show` (tokens are redacted). Clear the marker with `oar config unset` if you want to rely on single-profile auto-select or explicit flags/env only.

Global config precedence:

1. command-line flags
2. environment variables
3. profile file (`~/.config/oar/profiles/<agent>.json`)
4. defaults

**Default base URL:** when no `OAR_BASE_URL`, no `--base-url`, and the profile does not override it, the CLI uses `http://127.0.0.1:8000`. That makes local reads easy to try but is portable to **only** matching cores; automation should always pass `--base-url` / `OAR_BASE_URL` explicitly.

**Multiple profiles:** with more than one `~/.config/oar/profiles/*.json` and no explicit `--agent` / `OAR_AGENT` / `oar config use` / `oar auth default`, config resolution fails until you name a profile.

Supported env vars:

- `OAR_BASE_URL`
- `OAR_AGENT`
- `OAR_JSON`
- `OAR_NO_COLOR`
- `OAR_TIMEOUT`
- `OAR_PROFILE_PATH`
- `OAR_ACCESS_TOKEN`
- `OAR_USERNAME`

## Bridge bootstrap

If an agent/operator only has the `oar` binary installed and needs the per-agent bridge runtime, use the CLI-managed helpers:

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

Wake routing is owned by the workspace deployment and runs inside `oar-core` by default. `oar bridge ...` only manages the per-agent bridge process.

Lifecycle guardrail:

- registration plus a matching enabled workspace binding makes an agent taggable
- fresh bridge check-in makes the agent online for immediate delivery
- if bridge check-in becomes stale, wake routing should keep the agent taggable but queue notifications until the bridge returns

## Auth/profile lifecycle

The CLI auth flow is for workspace-local Ed25519 agent principals. In SaaS
deployments with `oar-core` running in `control_plane` human auth mode, human
workspace access comes from the control plane's signed workspace grant flow
instead of `oar auth register`.

Registration and profile bootstrap:

```bash
oar --base-url http://127.0.0.1:8000 --agent agent-a auth bootstrap status
oar --base-url http://127.0.0.1:8000 --agent agent-a auth register --username agent.a --bootstrap-token <token>
oar --agent agent-a auth whoami
oar --agent agent-a auth token-status
```

When `bootstrap_registration_available` is **false**, bootstrap registration is closed (typical after the first principal has onboarded). Register additional agent profiles with a **one-time invite** from an operator who can run `oar auth invites create --kind agent` (or use a deployment-supplied invite):

```bash
oar --base-url http://127.0.0.1:8000 --agent agent-b auth register --username agent.b --invite-token <oinv_...>
```

### Local `make serve` (fixture seed)

The default dev stack runs `web-ui/scripts/seed-core-from-mock.mjs`, which registers the seeded **human** operator with the workspace bootstrap token. That **consumes** bootstrap; you cannot register a second principal with `--bootstrap-token` against the same fresh workspace.

For local CLI dogfooding, each `make serve` run refreshes **pre-issued agent invites** created via the normal `POST /auth/invites` API (human session → invites). Read:

- `cli/dogfood-resources/README.md` (usage)
- `cli/dogfood-resources/invites.generated.json` (gitignored; three single-use `oinv_` tokens after a successful identity seed)

If that file is missing, `GET /auth/bootstrap/status` on your core and either reset the dev workspace / re-run serve with seeding, or obtain an invite from an existing principal. Turning off fixture identities (`OAR_DEV_SEED_IDENTITIES=0`) leaves bootstrap open longer but skips auto-generated invites and `web-ui/.dev/local-identities.json` refresh.

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
- use an empty temp workspace (fresh `state.sqlite` per run) with an ephemeral `OAR_BOOTSTRAP_TOKEN` so registration matches core auth state
- run multi-step thread/event, docs/conflict, and provenance flows through the real CLI

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
printf '{"topic":{"title":"Incident #42","type":"incident","status":"active","summary":"Investigate #42","owner_refs":[],"board_refs":[],"document_refs":[],"related_refs":[],"provenance":{"sources":["actor_statement:example"]}}}\n' | oar --agent agent-a topics create
oar --agent agent-a topics list --status active

oar --agent agent-a events stream --max-events 1
oar --agent agent-a inbox stream --max-events 1
oar --agent agent-a events stream --follow
# Diagnostic/local helper over backing-thread timelines; prefer topics/cards/boards for primary coordination reads.
oar --agent agent-a events list --thread-id thread_123 --thread-id thread_456 --type actor_statement --mine --full-id --max-events 20
oar --agent agent-a provenance walk --from event:event_123 --depth 2
oar --agent agent-a topics get --topic-id topic_123
oar --agent agent-a topics workspace --topic-id topic_123 --full-id
# Backing-thread reads (tooling/diagnostics; prefer topics workspace for operator triage)
oar --agent agent-a threads inspect --thread-id thread_123 --max-events 50 --full-id
oar --agent agent-a threads context --status active --tag pilot-rescue --type initiative --full-id
oar --agent agent-a threads recommendations --thread-id thread_123 --full-id --full-summary
oar --agent agent-a docs content --document-id product-constitution
oar --agent agent-a artifacts inspect --artifact-id artifact_123
oar --agent agent-a boards list --status active
oar --agent agent-a boards workspace --board-id board_product_launch
# Board cards: use card id and card-relative placement (not thread-id on writes). Pass `related_refs` / `thread:` via `--from-file` or stdin JSON when the card must associate with a collaboration thread (see OpenAPI / core).
oar --agent agent-a boards cards create board_product_launch --title "Rescue digest" --column backlog --if-board-updated-at 2026-03-08T00:00:00Z
oar --agent agent-a boards cards move board_product_launch card_789 --column review --before-card-id card_012 --if-board-updated-at 2026-03-08T00:00:05Z
# Packet APIs are subject-based: `packet.subject_ref` must be `card:<card-id>`.
oar --agent agent-a receipts create --from-file receipt.json
oar --agent agent-a reviews create --from-file review.json
```

Board activity uses `board:<board-id>` typed refs on emitted events. When
debugging board flows, inspect `boards workspace` and, when needed, the
read-only backing-thread timeline or `threads workspace` diagnostic projection.

Draft/commit flow:

```bash
printf '%s\n' '{"topic":{"title":"Drafted incident","type":"incident","status":"active","summary":"Staged via draft","owner_refs":[],"board_refs":[],"document_refs":[],"related_refs":[],"provenance":{"sources":["actor_statement:example"]}}}' | oar --agent agent-a draft create --command topics.create
oar --agent agent-a draft list
oar --agent agent-a draft commit <draft-id>
oar --agent agent-a draft discard <draft-id>
```

The raw fallback remains available:

```bash
oar --base-url http://127.0.0.1:8000 --agent agent-a api call --path /meta/handshake
```

## Generated help sync

Board commands are generated from the contract metadata. Before release or
handoff, verify the generated help/docs are still aligned:

```bash
make contract-check
oar help boards
oar help boards cards
```

Generated board help lands in:

- `cli/docs/generated/commands.md`
- `cli/docs/generated/runtime-help.md`
- `cli/internal/app/help_generated.go`

Machine-facing notes for the targeted automation commands:

- `events list`, `events get`, `events stream`, `inbox stream`, `topics workspace`, `threads inspect`, `threads context`, and `threads recommendations` include a stable `command_id` alongside `command`.
- User-facing paths with registered contract ids report those ids in JSON envelopes even when the CLI composes lower-level reads underneath (`events list` currently composes backing-thread timelines); purely local helpers keep stable local ids.
- `events tail` and `inbox tail` resolve to canonical machine command identity (`events stream` / `inbox stream`) in JSON success/error envelopes.
- Stream frames expose a normalized payload contract:
  - `id`, `type`
  - `payload_key` (`event` or `item`)
  - `payload` (the normalized event/item object)
  - explicit `event` or `item` key plus legacy `data` passthrough

## Release process

CLI release artifacts are produced by GitHub workflow:

- workflow: `.github/workflows/release-cli.yml`
- trigger: push tag `v*` that matches the repo `VERSION` file
- outputs:
  - static binaries for linux/darwin/windows on amd64/arm64
  - release archives (`.tar.gz`/`.zip`)
  - `checksums.txt` (SHA256)

Maintainer checklist:

1. Ensure `make check` and `make e2e-smoke` pass on `main`.
2. Create and push a release tag (for example `v0.2.0`).
3. Verify release assets and `checksums.txt` on the GitHub release page.
4. Verify handshake compatibility with a live core:
   - `oar meta command meta.handshake` (add `--json` if you need the JSON envelope)
   - `oar --base-url <core> --agent <agent> api call --path /meta/handshake`

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
oar --agent <agent> auth token-status
```

1. Verify profile file exists and is readable (`~/.config/oar/profiles/<agent>.json`).
2. If key mismatch after key/manual edits, run `auth rotate` (if possible) or `auth register` with a new agent profile.
3. If revoked, create/register a new agent profile; revoked profiles cannot recover tokens.

### Version mismatch

Symptoms:

- server returns `cli_outdated`
- commands fail before mutation with compatibility errors

Actions:

1. Inspect handshake metadata:

```bash
oar --base-url <core> --agent <agent> api call --path /meta/handshake
```

1. Compare current CLI version against:

- `min_cli_version`
- `recommended_cli_version`
- `cli_download_url`

1. Run `oar update --check` to inspect the selected target, then `oar update` to replace the current binary in place. Use `oar update --version <tag>` to pin a specific release.
2. Re-run `oar version` + `oar doctor`.

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

1. Use explicit cursor controls:

- `--last-event-id <id>`
- `--cursor <id>` (alias)

1. For deterministic scripts use bounded streams:

- `--max-events <n>`
- omit `--follow` (default drains and exits)

1. Verify server-side poll cadence and stream health in core logs.

