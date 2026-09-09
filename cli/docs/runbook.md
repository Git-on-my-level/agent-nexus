# anx-cli Runbook

This runbook covers local development, end-to-end smoke usage, release steps, and common troubleshooting for `anx`.

## Local development

Build and test:

```bash
cd cli
go build ./cmd/anx go test ./...
go test -tags=integration ./integration/...
```

Run against local core (default output is **text** on stdout and should be the first choice for agent readbacks; add **`--json`** or **`ANX_JSON=true`** only when a script or program parses the CLI envelope):

```bash
cd cli
go run ./cmd/anx --base-url http://127.0.0.1:8000 --agent local version
go run ./cmd/anx --base-url http://127.0.0.1:8000 --agent local doctor
go run ./cmd/anx --base-url http://127.0.0.1:8000 --agent local auth bootstrap status
go run ./cmd/anx --base-url http://127.0.0.1:8000 --agent local auth register --username local.agent --bootstrap-token <token>
go run ./cmd/anx --agent local version
```

**Output modes:** concise text is the default for direct reading and normal LLM tool output. JSON mode is for programmatic consumers (`jq`, CI, services, scripts). `auth register` does **not** write `"json": true` into the profile; older profiles may still set it—use `--json=false` / `ANX_JSON=false` for a single command if needed.

**Refs and handles:** list-style JSON and default text rows lead with public typed refs such as `topic:<handle>`, `board:<handle>`, and `card:<handle>`. You can paste typed refs or bare handles back into commands; the CLI passes them through to core for resolution. Use `--json` when scripts need `ref` and `handle` fields directly.

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
# default: installs bridge at the same git tag as this anx binary (e.g. v0.3.2); use --ref main for default-branch HEAD
anx bridge install
anx bridge init-config --kind subprocess --output ./agent.toml --handle <handle> --adapter-entrypoint ./adapter.py
anx bridge import-auth --config ./agent.toml --from-profile <agent>
anx bridge start --config ./agent.toml
anx bridge status --config ./agent.toml
anx bridge doctor --config ./agent.toml
anx bridge logs --config ./agent.toml
anx bridge restart --config ./agent.toml
anx bridge stop --config ./agent.toml
```

`anx bridge init-config` discovers the durable workspace id from the active profile or core handshake. Add `--workspace-id <workspace-id>` only when discovery fails or you need an explicit binding.

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
printf '{"topic":{"title":"Incident #42","summary":"Investigate #42","owner_refs":[],"board_refs":[],"document_refs":[],"related_refs":[],"provenance":{"sources":["event:example"]}}}\n' | anx --agent agent-a topics create
anx --agent agent-a topics list --state active

anx --agent agent-a events stream --max-events 1
anx --agent agent-a inbox stream --max-events 1
anx --agent agent-a events stream --follow
# Diagnostic/local helper over backing-thread timelines; prefer topics/cards/boards for primary coordination reads.
anx --agent agent-a events list --thread-id thread_123 --thread-id thread_456 --type message_posted --mine --max-events 20
anx --agent agent-a provenance walk --from event:incident-42 --depth 2
anx --agent agent-a topics get incident-42
anx --agent agent-a topics create --title "Launch" --summary "Coordinate launch work"
anx --agent agent-a topics message incident-42 --body-file message.md
anx --agent agent-a topics messages incident-42 --max-events 10
anx --agent agent-a topics workspace incident-42
# Backing-thread reads (tooling/diagnostics; prefer topics workspace for operator triage)
anx --agent agent-a threads inspect thread_123 --max-events 50
anx --agent agent-a threads context --state active
anx --agent agent-a threads workspace thread_123
anx --agent agent-a docs content product-constitution
anx --agent agent-a docs message product-constitution --body-file note.md
anx --agent agent-a docs messages product-constitution --max-events 10
anx --agent agent-a artifacts inspect --artifact-id incident-42-log
anx --agent agent-a workspace summary
anx --agent agent-a boards list --state active
anx --agent agent-a boards create --topic incident-42 --title "Launch board"
anx --agent agent-a boards workspace product-launch
# Cards: draft prose locally, then use domain verbs for active work.
anx --agent agent-a cards list --board product-launch
anx --agent agent-a cards create --board product-launch --topic incident-42 --title "Rescue digest" --body-file card.md
anx --agent agent-a cards revise rescue-digest --body-file card.md
anx --agent agent-a cards assign rescue-digest --assignee-ref actor:agent-a
anx --agent agent-a cards move rescue-digest --column review
anx --agent agent-a cards resolve rescue-digest --body-file evidence.md
# Packet APIs are subject-based: `packet.subject_ref` must be `card:<card-handle>`.
anx --agent agent-a receipts create --from-file receipt.json
anx --agent agent-a reviews create --from-file review.json
```

Board activity uses `board:<board-handle>` typed refs on emitted events. When
debugging board flows, inspect `boards workspace` and, when needed, the
read-only backing-thread timeline or `threads workspace` diagnostic projection.
Use `boards cards list|get|create-batch` only for board-scoped reads or batch
JSON creation; use `anx cards ...` for individual card workflow. The
agent-facing conversation verbs are `topics message/messages/reply`,
`docs message/messages/reply`, and
`cards create/message/messages/reply/revise/move/assign/resolve/reopen`. For
ordinary domain conversation updates, prefer `anx <domain> message <id>
--body-file update.md`; the CLI fills the backing `thread_id`, domain/thread
refs, and profile actor. Use raw `events create` only for contract-level writes
or unusual integrations.

Draft/commit flow:

```bash
printf '%s\n' '{"topic":{"title":"Drafted incident","summary":"Staged via draft","owner_refs":[],"board_refs":[],"document_refs":[],"related_refs":[],"provenance":{"sources":["event:example"]}}}' | anx --agent agent-a draft create --command topics.create
anx --agent agent-a draft list
anx --agent agent-a draft commit <draft-id>
anx --agent agent-a draft discard <draft-id>
```

Use `draft` for reviewable JSON writes, broad/risky mutations, or changes delegated by a human where an inspectable checkpoint is useful. Prefer direct domain verbs for narrow, already-verified changes.

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
anx help cards
```

Generated board help lands in:

- `cli/docs/generated/commands.md`
- `cli/docs/generated/runtime-help.md`
- `cli/internal/app/help_generated.go`

Machine-facing notes for the targeted automation commands:

- `events list`, `events get`, `events stream`, `inbox stream`, `topics workspace`, `threads inspect`, `threads context`, and `threads workspace` include a stable `command_id` alongside `command`.
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
curl -N -H 'Accept: text/event-stream' http://127.0.0.1:8000/stream/events
curl -N -H 'Accept: text/event-stream' http://127.0.0.1:8000/stream/inbox
```

1. Use explicit cursor controls:

- `--last-event-id <id>`
- `--cursor <id>` (alias)

1. For deterministic scripts use bounded streams:

- `--max-events <n>`
- omit `--follow` (default drains and exits)

1. Verify server-side poll cadence and stream health in core logs.

## Unified work and remote observations

`anx work` reads the central work projection of existing cards. Projects are
existing topics (`anx topics list`); boards and native card workflow commands keep
their existing meaning. Select the workspace with the existing `--agent` profile
and `--base-url`; reports reuse its key/token identity. No local tracker store or
remote daemon is required.

```sh
anx work capabilities
anx work list --project-ref topic:launch --source github --freshness stale --limit 50
anx --json work list --limit 50 --cursor '<opaque next_cursor>'
anx work get card:launch
anx work context card:launch --limit 10
anx work freshness card:launch
anx work observations list card:launch --limit 10
anx work observations submit card:launch --from-file report.json
anx work refresh request card:launch
anx work refresh get card:launch
```

Lists return a single bounded page and preserve `next_cursor`. Pass the cursor
unchanged with the same filters; the CLI does not silently crawl all projects.
`context` performs three read-only calls (work, observations, refresh), preserving
the observation page boundary. This is a composed view, not an atomic snapshot.
Freshness distinguishes last observation, source activity and meaningful progress.
A queued refresh is not a successful read; failed reads retain their error status.

Observation input is the API request object:

```json
{
  "observation": {
    "idempotency_key": "synthetic-report-42",
    "reader_id": "approved-reader",
    "reader_revision": "v1",
    "observed_at": "2026-09-08T00:00:00Z",
    "source_sequence": 42,
    "status": "reported",
    "facts": {"native_status": "in_progress"},
    "evidence": [{"ref": "artifact:synthetic-check", "summary": "Synthetic example only"}],
    "uncertainty": ["Deployment not verified"],
    "coverage": {"complete": false}
  }
}
```

Preserve the idempotency key and original observation on retry. Core owns duplicate
and out-of-order handling; the CLI prints the actual server result without hiding
`duplicate`, uncertainty, coverage or freshness. The server supplies received time
and authenticated actor; remote claims cannot grant themselves verified authority.
The CLI never retries a failed observation write automatically.

`work create --from-file <path|->` registers work using an existing `board_ref`.
`work patch <ref> --from-file <path|->` requires the API's `if_version` and `patch`
object. Read the version with `work get`; external source status/title/owner remain
source-owned and update through observations. These commands do not mutate the
external source.

All commands are noninteractive and support the existing single `--json` envelope.
Malformed flags and resource selectors fail with exit 2 before profile resolution.
API denial/conflict/rate-limit errors retain the shared machine-readable error
contract. `anx help work` and `anx help work observations submit` work offline.

### PM decisions and receipt reporting

```sh
anx pm context --work-ref card:launch --limit 20
anx pm conversations list
anx pm conversations create --from-file conversation.json
anx pm conversations message <conversation-id> --from-file message.json
anx pm decisions list
anx pm decisions get <decision-id>
anx pm decisions create --from-file instruction.json
anx pm decisions answer <decision-id> --from-file answer.json
anx pm decisions dispatch <decision-id>
anx pm actions list
anx pm actions get <action-id>
anx pm actions reconcile <action-id>
```

An instruction body contains `request_key`, `work_ref`, `instruction`, `scope` and
`target_revision`. An answer body contains the current decision `revision`,
`approve` and `text`. Agent keys may propose, but cannot inherit human approval
permissions; core enforces the current principal and scope. A successful answer
records intent, not delivery. `pm actions get` reports the actual action attempts
and receipt, including `source_reported`, `unknown` and
`receipt.independently_verified`. `reconcile` requests read-back, never a resend.
There is no client command to manufacture a verified receipt.

Conversation creation uses `request_key`, `title` and optional `work_ref`; messages
use `request_key` and `text`. Preserve request keys on retry. A queued turn is not
an assistant response or completed work. Status vocabulary is `sending`,
`unknown`, `failed`, or completed with `response` (`delivered`).

The selected PM agent can use `pm turns claim`, `pm turns context <turn-id>`,
`pm turns propose <turn-id> --from-file ...`, `pm turns complete <turn-id>
--from-file ...`, and `pm turns fail <turn-id> --from-file ...`. Other agents
cannot impersonate it. Claim is lease-based and idempotent for the same
`runner_id`; HTTP 204 means no claimable turn.

### PM runner (`anx pm serve`)

The PM is an external agent. Do not call a model in-process. `make serve` seeds
persona `pm` (`actor-gds-pm` / `dev.pm`) for the default game-dev-studio
scenario, writes CLI profile homes from registration tokens (no refresh
exchange), and prints the exact command. Wake routing and
`ANX_PM_BRIDGE_ENABLED` are not required.

```sh
make cli-build
ANX_DEV_BLOB_BACKEND=filesystem make serve
HOME=.tmp/anx-dev-profile-homes/pm ./cli/anx --agent pm pm serve \
  --work-dir .tmp/pm-runner \
  --runner 'omp -p --mode json --model zai/glm-5.3 --auto-approve'
HOME=.tmp/anx-dev-profile-homes/maya ./cli/anx --agent maya pm ask --wait \
  "What needs my decision?"
```

`--runner` is the native harness argv after `agentctl run --`. omp may silently
substitute models; every run must show `"provider":"zai","model":"glm-5.3"` in
the harness JSON (`grep -o '"provider":"[^"]*","model":"[^"]*"'`). GPT models
never go through omp. The prompt stays small: the PM loads tracker context
through `anx work list|get` and `anx pm context`, never from a stuffed dump.
Output bytes and wall time come from core `pm.Config` (defaults 16000 bytes and
2 minutes). `make serve` sets `ANX_PM_TURN_TIMEOUT=10m` so omp/glm-5.3 can use
tools before the lease expires.

PM context is bounded to 1..50 items. PM conversation/decision/action lists accept
`--limit` (1..200) and `--cursor`, returning `next_cursor` and `has_more`. Cursors are
bound to the current workspace, principal and record kind; do not reuse one after
switching profiles. Lists do not support server-side project filtering; use
`work list --project-ref` for project-scoped work queries.

For cross-lane validation only, the real-binary harness accepts
`ANX_INTEGRATION_CORE_BINARY` pointing to a compiled core artifact. Without it the
harness builds this checkout's core. This is not a mock backend; record the core
source revision when using the override.

### PM channels (`anx pm channels doctor`)

Telegram and Discord ingress are webhook/interaction only. Bind a channel
identity before the PM will accept messages:

```sh
anx pm bindings create --from-file binding.json
anx pm channels doctor \
  --telegram-webhook-url http://127.0.0.1:8000/pm/ingress/telegram \
  --discord-webhook-url http://127.0.0.1:8000/pm/ingress/discord
```

Doctor reads env (never prints token values), probes those URLs with GET, and
lists `/pm/bindings`. It does not POST an update, send a Bot API message, or
open a Discord gateway.

Core env (set on `anx-core`, not in Git):

| Env | Role |
|---|---|
| `ANX_PM_TELEGRAM_WEBHOOK_SECRET` | Telegram secret header; at least 32 characters |
| `ANX_PM_TELEGRAM_BOT_ID` | Expected bot / tenant id |
| `ANX_PM_TELEGRAM_BOT_TOKEN` | Outbound `sendMessage` |
| `ANX_PM_DISCORD_PUBLIC_KEY` | 32-byte hex Ed25519 public key |
| `ANX_PM_DISCORD_APPLICATION_ID` | Expected application id |
| `ANX_PM_DISCORD_BOT_TOKEN` | Outbound REST `Bot` token |
| `ANX_PM_TELEGRAM_API_BASE` | Test-only Bot API base (local fake) |
| `ANX_PM_DISCORD_API_BASE` | Test-only Discord REST base (local fake, include `/api/v10`) |
| `ANX_PM_TELEGRAM_WEBHOOK_URL` | Optional doctor GET target |
| `ANX_PM_DISCORD_WEBHOOK_URL` | Optional doctor GET target |

When dedicated bots exist: create the bots, set the env vars, register the
webhook/interactions URL at core ingress, bind each human identity, then run
doctor. Do not point existing production bot streams at this workspace. Local
proof uses `tests/channels/` fakes, not live Telegram or Discord.
