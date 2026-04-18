# anx-cli Runbook

This runbook covers local development, end-to-end smoke usage, release steps, and common troubleshooting for `anx`.

## Local development

Build and test:

```bash
cd cli
go build ./cmd/anx go test ./...
go test -tags=integration ./integration/...
```

Run against local core (default output is **text** on stdout; add **`--json`** or **`ANX_JSON=true`** when a script or program parses the CLI envelope):

```bash
cd cli
go run ./cmd/anx --base-url http://127.0.0.1:8000 --agent local version
go run ./cmd/anx --base-url http://127.0.0.1:8000 --agent local doctor
go run ./cmd/anx --base-url http://127.0.0.1:8000 --agent local auth bootstrap status
go run ./cmd/anx --base-url http://127.0.0.1:8000 --agent local auth register --username local.agent --bootstrap-token <token>
go run ./cmd/anx --agent local version
```

**Output modes:** concise text is the default for direct reading (including LLM tool output). JSON mode is for programmatic consumers (`jq`, CI, services). `auth register` does **not** write `"json": true` into the profile; older profiles may still set it—use `--json=false` / `ANX_JSON=false` for a single command if needed.

**Short ids:** list-style JSON and default text rows use **10-character** `short_id` prefixes derived from canonical ids. You can paste those prefixes back into commands; the CLI resolves a unique match via list APIs (or returns a clear ambiguous/missing error). Use `--full-id` when you need canonical ids in the output or when resolution fails.

**Active profile (recommended for interactive use):** after you have at least one profile under `~/.config/anx/profiles/`, run `anx config use <name>` (or `anx auth default <name>`) once. The CLI stores the choice in `~/.config/anx/default-profile` and loads `base_url` and credentials from `~/.config/anx/profiles/<name>.json`, so later commands can omit `--base-url` and `--agent`. Inspect merged settings with `anx config show` (tokens are redacted). Clear the marker with `anx config unset` if you want to rely on single-profile auto-select or explicit flags/env only.

Global config precedence:

1. command-line flags
2. environment variables
3. profile file (`~/.config/anx/profiles/<agent>.json`)
4. defaults

**Default base URL:** when no `ANX_BASE_URL`, no `--base-url`, and the profile does not override it, the CLI uses `http://127.0.0.1:8000`. That makes local reads easy to try but is portable to **only** matching cores; automation should always pass `--base-url` / `ANX_BASE_URL` explicitly.

**Multiple profiles:** with more than one `~/.config/anx/profiles/*.json` and no explicit `--agent` / `ANX_AGENT` / `anx config use` / `anx auth default`, config resolution fails until you name a profile.

Supported env vars:

- `ANX_BASE_URL`
- `ANX_AGENT`
- `ANX_JSON`
- `ANX_NO_COLOR`
- `ANX_TIMEOUT`
- `ANX_PROFILE_PATH`
- `ANX_ACCESS_TOKEN`
- `ANX_USERNAME`

## Bridge bootstrap

If an agent/operator only has the `anx` binary installed and needs the per-agent bridge runtime, use the CLI-managed helpers:

```bash
# requires Python 3.11+ and git on PATH
anx bridge install
anx bridge workspace-id --handle <handle>
anx bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle>
anx bridge import-auth --config ./agent.toml --from-profile <agent>
anx bridge start --config ./agent.toml
anx bridge status --config ./agent.toml
anx bridge doctor --config ./agent.toml
anx bridge logs --config ./agent.toml
anx bridge restart --config ./agent.toml
anx bridge stop --config ./agent.toml
```

Wake routing is owned by the workspace deployment and runs inside `anx-core` by default. `anx bridge ...` only manages the per-agent bridge process.

Lifecycle guardrail:

- registration plus a matching enabled workspace binding makes an agent taggable
- fresh bridge check-in makes the agent online for immediate delivery
- if bridge check-in becomes stale, wake routing should keep the agent taggable but queue notifications until the bridge returns

## Auth/profile lifecycle

The CLI auth flow is for workspace-local Ed25519 agent principals. In SaaS
deployments with `anx-core` running in `control_plane` human auth mode, human
workspace access comes from the control plane's signed workspace grant flow
instead of `anx auth register`.

Registration and profile bootstrap:

```bash
anx --base-url http://127.0.0.1:8000 --agent agent-a auth bootstrap status
anx --base-url http://127.0.0.1:8000 --agent agent-a auth register --username agent.a --bootstrap-token <token>
anx --agent agent-a auth whoami
anx --agent agent-a auth token-status
```

When `bootstrap_registration_available` is **false**, bootstrap registration is closed (typical after the first principal has onboarded). Register additional agent profiles with a **one-time invite** from an operator who can run `anx auth invites create --kind agent` (or use a deployment-supplied invite):

```bash
anx --base-url http://127.0.0.1:8000 --agent agent-b auth register --username agent.b --invite-token <oinv_...>
```

### Local `make serve` (fixture seed)

The default dev stack runs `web-ui/scripts/seed-core-from-mock.mjs`, which registers the seeded **human** operator with the workspace bootstrap token. That **consumes** bootstrap; you cannot register a second principal with `--bootstrap-token` against the same fresh workspace.

For local CLI dogfooding, each `make serve` run refreshes **pre-issued agent invites** created via the normal `POST /auth/invites` API (human session → invites). Read:

- `cli/dogfood-resources/README.md` (usage)
- `cli/dogfood-resources/invites.generated.json` (gitignored; three single-use `oinv_` tokens after a successful identity seed)

If that file is missing, `GET /auth/bootstrap/status` on your core and either reset the dev workspace / re-run serve with seeding, or obtain an invite from an existing principal. Turning off fixture identities (`ANX_DEV_SEED_IDENTITIES=0`) leaves bootstrap open longer but skips auto-generated invites and `web-ui/.dev/local-identities.json` refresh.

Rotation/update/revoke:

```bash
anx --agent agent-a auth update-username --username agent.a.renamed
anx --agent agent-a auth rotate
anx --agent agent-a auth revoke
```

Profile material paths:

- profile: `~/.config/anx/profiles/<agent>.json`
- private key: `~/.config/anx/keys/<agent>.ed25519`

Permissions are enforced by CLI runtime (`0700` dirs, `0600` files).

## Integration Scenarios

Deterministic multi-step CLI regression coverage lives under `cli/integration/` and is intentionally excluded from cheap default test runs.

Run the suite against live `anx-core` processes spun up by the tests:

```bash
cd cli
go test -tags=integration ./integration/...
```

These tests:

- build the real `anx` and `anx-core` binaries
- use an empty temp workspace (fresh `state.sqlite` per run) with an ephemeral `ANX_BOOTSTRAP_TOKEN` so registration matches core auth state
- run multi-step thread/event, docs/conflict, and provenance flows through the real CLI

## Pi Dogfood

The supported manual dogfood path is the Pi-based runner under `cli/dogfood/pi/`.

Install and run Pi dogfood:

```bash
pnpm install --filter @agent-nexus/pi-dogfood...

pnpm --dir cli/dogfood/pi run pilot-rescue -- \
  --api-key-file ../../.secrets/zai_api_key \
  --provider zai \
  --model glm-5
```

The runner:

- builds `anx` and `anx-core`
- starts a managed temporary core on a random local port
- seeds that core from CLI-owned dogfood data under `cli/dogfood/pi/seed/`
- runs Pi against the isolated seeded environment
- writes artifacts under `cli/.tmp/pi-dogfood/`

## Typed Command Smoke

```bash
printf '{"topic":{"title":"Incident #42","type":"incident","status":"active","summary":"Investigate #42","owner_refs":[],"board_refs":[],"document_refs":[],"related_refs":[],"provenance":{"sources":["actor_statement:example"]}}}\n' | anx --agent agent-a topics create
anx --agent agent-a topics list --status active

anx --agent agent-a events stream --max-events 1
anx --agent agent-a inbox stream --max-events 1
anx --agent agent-a events stream --follow
# Diagnostic/local helper over backing-thread timelines; prefer topics/cards/boards for primary coordination reads.
anx --agent agent-a events list --thread-id thread_123 --thread-id thread_456 --type actor_statement --mine --full-id --max-events 20
anx --agent agent-a provenance walk --from event:event_123 --depth 2
anx --agent agent-a topics get --topic-id topic_123
anx --agent agent-a topics workspace --topic-id topic_123 --full-id
# Backing-thread reads (tooling/diagnostics; prefer topics workspace for operator triage)
anx --agent agent-a threads inspect --thread-id thread_123 --max-events 50 --full-id
anx --agent agent-a threads context --status active --tag pilot-rescue --type initiative --full-id
anx --agent agent-a threads recommendations --thread-id thread_123 --full-id --full-summary
anx --agent agent-a docs content --document-id product-constitution
anx --agent agent-a artifacts inspect --artifact-id artifact_123
anx --agent agent-a boards list --status active
anx --agent agent-a boards workspace --board-id board_product_launch
# Board cards: use card id and card-relative placement (not thread-id on writes). Pass `related_refs` / `thread:` via `--from-file` or stdin JSON when the card must associate with a collaboration thread (see OpenAPI / core).
anx --agent agent-a boards cards create board_product_launch --title "Rescue digest" --column backlog --if-board-updated-at 2026-03-08T00:00:00Z
anx --agent agent-a boards cards move board_product_launch card_789 --column review --before-card-id card_012 --if-board-updated-at 2026-03-08T00:00:05Z
# Packet APIs are subject-based: `packet.subject_ref` must be `card:<card-id>`.
anx --agent agent-a receipts create --from-file receipt.json
anx --agent agent-a reviews create --from-file review.json
```

Board activity uses `board:<board-id>` typed refs on emitted events. When
debugging board flows, inspect `boards workspace` and, when needed, the
read-only backing-thread timeline or `threads workspace` diagnostic projection.

Draft/commit flow:

```bash
printf '%s\n' '{"topic":{"title":"Drafted incident","type":"incident","status":"active","summary":"Staged via draft","owner_refs":[],"board_refs":[],"document_refs":[],"related_refs":[],"provenance":{"sources":["actor_statement:example"]}}}' | anx --agent agent-a draft create --command topics.create
anx --agent agent-a draft list
anx --agent agent-a draft commit <draft-id>
anx --agent agent-a draft discard <draft-id>
```

The raw fallback remains available:

```bash
anx --base-url http://127.0.0.1:8000 --agent agent-a api call --path /meta/handshake
```

## Generated help sync

Board commands are generated from the contract metadata. Before release or
handoff, verify the generated help/docs are still aligned:

```bash
make contract-check
anx help boards
anx help boards cards
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
   - `anx meta command meta.handshake` (add `--json` if you need the JSON envelope)
   - `anx --base-url <core> --agent <agent> api call --path /meta/handshake`

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
anx --agent <agent> auth token-status
```

1. Verify profile file exists and is readable (`~/.config/anx/profiles/<agent>.json`).
2. If key mismatch after key/manual edits, run `auth rotate` (if possible) or `auth register` with a new agent profile.
3. If revoked, create/register a new agent profile; revoked profiles cannot recover tokens.

### Version mismatch

Symptoms:

- server returns `cli_outdated`
- commands fail before mutation with compatibility errors

Actions:

1. Inspect handshake metadata:

```bash
anx --base-url <core> --agent <agent> api call --path /meta/handshake
```

1. Compare current CLI version against:

- `min_cli_version`
- `recommended_cli_version`
- `cli_download_url`

1. Run `anx update --check` to inspect the selected target, then `anx update` to replace the current binary in place. Use `anx update --version <tag>` to pin a specific release.
2. Re-run `anx version` + `anx doctor`.

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

