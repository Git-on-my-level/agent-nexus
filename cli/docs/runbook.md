# CLI runbook

## Host enrollment and identity

Set `ANX_BASE_URL` to the workspace core URL. A human auth-admin must bootstrap the workspace before a host can enroll.

```bash
anx host enroll --plan
anx host enroll --name my-mac
anx --as codex auth whoami
anx doctor
```

Interactive enrollment prints a user code and, when core has `ANX_PUBLIC_WEB_UI_WORKSPACE_URL` configured, the full workspace-scoped verification URL. Set that config to the public web UI workspace path, such as `http://127.0.0.1:5291/o/local/w/local`. In hosted deployments, a core API at `https://example.com/ws/acme/main` uses `https://example.com/o/acme/w/main` as its web UI workspace URL. Without it, open Access → Hosts in the workspace web UI and approve the printed code. The CLI polls at the server interval. For unattended fleet hosts, use the auth-admin agent flow below. `anx host enroll --token-stdin` keeps the secret out of process arguments. A host key and record are stored under `~/.config/anx/hosts/<workspace-key>/` with owner-only permissions.

Existing standalone agent profiles for the same workspace are adopted by default. `--plan` shows which profiles; repeat `--exclude <profile>` to leave one standalone. Successfully adopted local profile and key files are deleted.

`--as <name>` or `ANX_AS` chooses a derived agent. Otherwise `anx` first consults
the optional versioned `agentctl identity --json` report, then falls back to
legacy `agentctl run` context and known harness markers. Runtime evidence suggests
a name; it does not authenticate the principal or replace the enrolled host.
`anx auth whoami` shows the host, agent and resolution source. `anx host token
--as <name>` prints only the short-lived bearer in text mode; JSON mode returns
`{token, expires_at, agent: {id, handle}}` for the bridge. Protect its stdout as a
secret. Use `--config-dir <absolute-path>` or `ANX_CONFIG_DIR` to locate enrolled
hosts when `HOME` is absent.

The bridge uses host-signed `anx host bridge check-in --host-id <id> --instance-id <id> --ttl-seconds <n>` and `anx host bridge wake claim|complete|fail --host-id <id> --wakeup-id <id> --instance-id <id> [--error <text>]`. Its `[host].config_dir` must point to the same enrolled host directory used by `anx`.

Use `anx host status`, `anx host list`, `anx host exclude <name>` and `anx host include <name>` to inspect or edit this host. Humans and explicitly granted auth-admin agents can run `anx host revoke <host-id-or-slug>`; an agent cannot revoke its own host.

### Fleet enrollment by an auth-admin agent

A person first grants an existing agent administration access on the Access page,
or through `anx auth admins grant <principal-id-or-username>` using their own
human bearer (`ANX_ACCESS_TOKEN`). No agent receives this grant automatically;
`anx auth admins list` lists explicit agent grants. Only humans can grant or
revoke them. `anx auth admins revoke <principal>` takes effect on the next request,
even when an agent has a cached access token.

Once granted, the agent on host A enrolls host B through a one-time token. This is
the default fleet enrollment path. Use a workspace-scoped `ANX_BASE_URL` on both
machines, disable shell tracing, and pipe the secret directly to the remote CLI:

```bash
set +x
anx --as fleet --json host tokens create --label host-b --expires-in 1h \
  | jq -er '.result.token' \
  | ssh host-b 'ANX_BASE_URL=https://nexus.example/ws/team/main anx host enroll --name host-b --token-stdin'
```

The token appears only in its create response; never save that response in run
logs. It authorizes one host and expires after 10 minutes to 24 hours. The CLI
sends the requested lifetime to core, which measures it on its own clock.
`host tokens list` returns metadata without secrets; `host tokens revoke <token-id>`
invalidates an unused token. The remote host stores its own key with owner-only
permissions. A failed enrollment leaves the token usable; successful enrollment
consumes it atomically. Keep existing profile adoption/exclusion rules in mind.

For interactive requests, use `anx host enrollments list`, then
`anx host enrollments approve <user-code>` or `deny <user-code>`. The protected list
includes codes and machine details, never poll tokens or key proofs. Core audits
the acting principal for decisions and revocations. Headless consumption has no
authenticated principal actor; its audit records the token ID, destination host
and key ID, and the issuer separately. Enrollment tokens and already-approved
ceremonies remain consumable after the creator's auth-admin grant is removed,
until expiry or explicit cancellation. To withdraw outstanding fleet access, use
`anx host tokens list` then `anx host tokens revoke <token-id>`; use
`anx host enrollments list` then `anx host enrollments deny <user-code-or-id>`
for pending or approved ceremonies. Access shows approved ceremonies awaiting
completion with a Cancel approval button. Completed hosts require
`anx host revoke <host-id-or-slug>`.

Granting an agent on host X trusts every process that can read X's shared host
key and request that exact agent name. The grant names an existing principal;
audit records include its host ID, slug, and name. Protect the host key as an
administration credential. Agents cannot issue or revoke human invitations, revoke principals, or use the
human lockout override. Their grant covers fleet enrollment and administrative
reads only. Agents cannot use their credentials to mint human identities. Administration writes recheck the
grant inside their transaction, including requests paused during body upload.


## User-global workspace selection

Run `anx config workspaces` when unsure. It lists enrolled workspaces and aliases, the default, and which directory rule applies to cwd, even when selection is ambiguous. Enrollment discovers an alias from the workspace slug (falling back to local workspace metadata), preserves the default, and prints the command to select the new workspace.

```bash
anx config workspaces
anx config use personal
anx config map "~/workspace/omi/**" omi
anx config show
anx --workspace personal orient
anx config unmap "~/workspace/omi/**"
```

Preferences live in `~/.config/anx/workspaces.json`, alongside the JSON host records, or under `ANX_CONFIG_DIR` / `--config-dir`. No repository metadata is read or written. `config use` and `config map` accept aliases or absolute HTTP(S) base URLs; saved choices use URLs so alias changes cannot reroute them. Discovered aliases are persisted by enrollment and preference writes; reads remain read-only. Collisions receive stable numeric suffixes without rebinding existing aliases.

Selection order is `--base-url` or `--workspace` > `ANX_BASE_URL` > directory rule > configured default > single enrolled host (`bridge:auto-single`). Only zero enrolled hosts retain the localhost development default. Multiple enrolled workspaces with no selection fail before networking and name the aliases and exact `anx config use` repairs. `anx doctor` reports the selected workspace and source, and fails on ambiguity. Use `config workspaces` and user-global preferences for agent orientation; never hardcode `--base-url` in agent prompts.

Directory globs must be absolute or begin with `~/`; quote them against shell expansion. `**` matches zero or more complete components; `*`, `?`, and character classes match within a component. Matching follows the cwd volume's case sensitivity for every component, including literals after wildcards. Rules rank by longest literal prefix, then most literal characters, with lexical order breaking ties. Existing symlink prefixes are resolved when matching against cwd, so logical home/work paths also match physical directories. A rule wins over the configured default. `sources.base_url` names the winning rule as `config:directory-rule:<glob>`, the default as `config:default`, or an explicit alias as `flag:--workspace`. Use either URL flag or alias flag, not both.

An example file:

```json
{
  "default": "https://anx.example.com/ws/personal/main",
  "aliases": {"personal": "https://anx.example.com/ws/personal/main", "omi": "https://anx.example.com/ws/omi/main"},
  "directory_rules": {"/Users/me/workspace/omi/**": "https://anx.example.com/ws/omi/main"}
}
```

## Generic sessions and nonlocking task participation

An already authenticated agent can use these commands without `agentctl`, a
known harness, new credentials, or a dedicated execution runner. The stable
principal, native provider conversation, and individual run attempt are different
identities. This API registers metadata, not a new authenticated principal.

Inspect optional local runtime evidence with `anx host discover --json`. It reads
the `agentctl.identity.v1` contract when available, without registering a session,
reading transcripts, or uploading anything. An unavailable/old provider leaves
explicit registration usable. Installed harness availability does not establish
resume, history, or log support. Unmanaged evidence is a claim, not a verified
session identity. Keep any native correlation hash scoped to its enrolled host.

For a generic provider, prepare `session.json` with an opaque stable conversation
identifier. Reuse it for the same conversation and use a new one for fresh context:

```json
{
  "provider": "custom-agent",
  "native_session_id": "conversation-42",
  "native_session_id_kind": "opaque",
  "capabilities": {"resume": "unknown", "history": "unsupported", "logs": "unknown"},
  "activity": "active",
  "sequence": 1
}
```

```bash
anx --as reviewer sessions register --from-file session.json
anx --as reviewer sessions get <session-id>
```

Enrolled agents get a server-bound host namespace; do not put a machine pathname
or secret in `native_session_id`. Standalone principals supply an explicit opaque
`host_scope`, which is recorded as a namespace rather than proof of another
enrolled host. For agentctl correlation use its `native_session.id` and
`native_session.id_kind=provider_session_sha256`, not the execution ID. Missing
session evidence stays unknown; do not substitute a parent or guessed session.

Then prepare `participation.json` with the returned `session_id`:

```json
{"session_id":"<session-id>","activity":"active","sequence":1}
```

```bash
anx --as reviewer work participants register card:launch --from-file participation.json
anx --as reviewer work participants list card:launch --limit 50
```

Participation never changes assignees, phase, rank, source ownership, or task
completion. One session can join several tasks and a principal can have several
simultaneous sessions. Session records are owner-only; task participant reads
show task-scoped history without exposing native IDs, private session metadata,
or links to unrelated tasks. A session link is not transcript access.

Keep a monotonic sequence independently for each session and each task/session
pair. Replay the same sequence and same payload on retry; retries preserve the
original timestamps and do not prolong activity. Increment for new observations;
conflicting replay or stale sequence returns conflict. Activity expires after
120 seconds without a newer active observation, but history remains. Report idle
or closed for a session, idle or left for a task participation, as appropriate.
Closing a session is terminal and does not finish a task. An activity expiry is
absence of a recent observation, not proof that a process died.

Use `anx work context` to read evidence first. `anx work start` deliberately
assigns and moves native work; it is not a registration shortcut. Project
association should use clear configured evidence and ask when ambiguous. A
successful run/session does not satisfy the task's acceptance criteria.

The bundled participant skill advertises `anx.participant.v7`; explicitly
designated PMs can load the additional `anx.pm.v3` skill. Use `anx skills
configure|status|verify --path <skill-directory> --role participant|pm` for
versioned local ownership, clean refresh and read-only verification. Existing
unmanaged or edited content is preserved. File verification never proves an
existing session loaded the skill. Supported harness installation and auto-clean
refresh compose with agentctl packs, not a second ANX harness catalog. See
[managed skill sources and migration](../skills/README.md). Automatic enrollment
setup, real harness activation and existing-PM endpoint connection remain later
gates in the [adoption plan](../../docs/architecture/existing-agent-adoption.md).

## Runs from agentctl

Label launched work with `--label anx.card.<card-slug>`. To subscribe the execution to ANX, use agentctl's `command` destination; it appends an owner-only callback file path to the argv:

```bash
ANX_CONFIG_DIR="${ANX_CONFIG_DIR:-$HOME/.config/anx}"
agentctl subscribe create --execution "$EXECUTION_ID" --destination command --target "$(command -v anx)" \
  --arg --config-dir --arg "$ANX_CONFIG_DIR" --arg --base-url --arg "$ANX_BASE_URL" \
  --arg runs --arg ingest --kind all
```

agentctl's `command` destination supplies only `PATH=/usr/bin:/bin` and `LANG=C`; it gives no `HOME` or stdin, discards output, and appends the owner-only event file as the final argument. `anx runs ingest [--as <name>] [--config-dir <absolute-path>] [<event-file>]` reads that file, or one version 1 callback/execution JSON envelope on stdin if no path is given. It uses `--as` when supplied, otherwise the envelope adapter. Failures append an error code to the owner-only, bounded `<config-dir>/logs/runs-ingest.log`; successes and tokens are never logged. A failure exits nonzero for agentctl retry. Replayed observations converge on the same run. Read with `anx runs list` and `anx runs get <run-id>`.

## Integration Scenarios

Deterministic multi-step CLI regression coverage lives under `cli/integration/` and is intentionally excluded from cheap default test runs.

Run the suite against live `anx-core` processes spun up by the tests:

```bash
cd cli
go test -tags=integration ./integration/...
```

These tests:

- build the real `anx` and `anx-core` binaries
- use an empty temp workspace (fresh `state.sqlite` per run), bootstrap a human, and enroll a host through a one-time token
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
printf '{"topic":{"title":"Incident #42","summary":"Investigate #42","owner_refs":[],"board_refs":[],"document_refs":[],"related_refs":[],"provenance":{"sources":["event:example"]}}}\n' | anx --as agent-a topics create
anx --as agent-a topics list --state active

anx --as agent-a events stream --max-events 1
anx --as agent-a inbox stream --max-events 1
anx --as agent-a events stream --follow
# Diagnostic/local helper over backing-thread timelines; prefer topics/cards/boards for primary coordination reads.
anx --as agent-a events list --thread-id thread_123 --thread-id thread_456 --type message_posted --mine --max-events 20
anx --as agent-a provenance walk --from event:incident-42 --depth 2
anx --as agent-a topics get incident-42
anx --as agent-a topics create --title "Launch" --summary "Coordinate launch work"
anx --as agent-a topics message incident-42 --body-file message.md
anx --as agent-a topics messages incident-42 --max-events 10
anx --as agent-a topics workspace incident-42
# Backing-thread reads (tooling/diagnostics; prefer topics workspace for operator triage)
anx --as agent-a threads inspect thread_123 --max-events 50
anx --as agent-a threads context --state active
anx --as agent-a threads workspace thread_123
anx --as agent-a docs content product-constitution
anx --as agent-a docs message product-constitution --body-file note.md
anx --as agent-a docs messages product-constitution --max-events 10
anx --as agent-a artifacts inspect --artifact-id incident-42-log
anx --as agent-a workspace summary
anx --as agent-a boards list --state active
anx --as agent-a boards create --topic incident-42 --title "Launch board"
anx --as agent-a boards workspace product-launch
# Cards: draft prose locally, then use domain verbs for active work.
anx --as agent-a cards list --board product-launch
anx --as agent-a cards create --board product-launch --topic incident-42 --title "Rescue digest" --body-file card.md
anx --as agent-a cards revise rescue-digest --body-file card.md
anx --as agent-a cards assign rescue-digest --assignee-ref actor:agent-a
anx --as agent-a cards move rescue-digest --column review
anx --as agent-a cards resolve rescue-digest --body-file evidence.md
# Packet APIs are subject-based: `packet.subject_ref` must be `card:<card-handle>`.
anx --as agent-a receipts create --from-file receipt.json
anx --as agent-a reviews create --from-file review.json
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
refs, and resolved agent actor. Use raw `events create` only for contract-level writes
or unusual integrations.

Draft/commit flow:

```bash
printf '%s\n' '{"topic":{"title":"Drafted incident","summary":"Staged via draft","owner_refs":[],"board_refs":[],"document_refs":[],"related_refs":[],"provenance":{"sources":["event:example"]}}}' | anx --as agent-a draft create --command topics.create
anx --as agent-a draft list
anx --as agent-a draft commit <draft-id>
anx --as agent-a draft discard <draft-id>
```

Use `draft` for reviewable JSON writes, broad/risky mutations, or changes delegated by a human where an inspectable checkpoint is useful. Prefer direct domain verbs for narrow, already-verified changes.

The raw fallback remains available:

```bash
anx --base-url http://127.0.0.1:8000 --as agent-a api call --path /meta/handshake
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
   - `anx --base-url <core> --as <agent> api call --path /meta/handshake`

## Troubleshooting

### Host identity failures

Run `anx doctor` for enrollment, host key permissions, identity resolution, agentctl, and CLI/core version checks. Doctor fails when this CLI is older than handshake `min_cli_version` and warns when it is older than `recommended_cli_version`. The repair is `anx update --version <recommended>`. Use `--as <name>` when no harness or agentctl context can be detected. If the host was revoked, ask a human auth-admin to enroll a replacement. Workspace selection follows the user-global rules above; `anx config workspaces` diagnoses ambiguity.

### Version mismatch

Symptoms:

- server returns `cli_outdated`
- commands fail before mutation with compatibility errors
- `anx doctor` fails with `cli_outdated` when this CLI is below `min_cli_version`, and warns when it is below `recommended_cli_version` (`anx update --version <recommended>`)

Actions:

1. Inspect handshake metadata:

```bash
anx --base-url <core> --as <agent> api call --path /meta/handshake
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
their existing meaning. Select the workspace with user-global config or `--workspace` and the derived
agent with `--as`; reports use the enrolled host identity. No local tracker store or
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

`work create --from-file <path|->` registers work. `board_ref` is optional; when omitted, core uses the workspace's oldest active board, creating a default Tasks board if none exists.
`work patch <ref> --from-file <path|->` requires the API's `if_version` and `patch`
object. Read the version with `work get`; external source status/title/owner remain
source-owned and update through observations. These commands do not mutate the
external source.

All commands are noninteractive and support the existing single `--json` envelope.
Malformed flags and resource selectors fail with exit 2 before identity resolution.
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
`runner_id`; HTTP 204 means no claimable turn. `--runner-id` defaults to the
authenticated actor id. Text output prints `runner_id` and `lease_token` so
the same runner can release later:

```sh
anx --as pm pm turns claim --runner-id "$RUNNER_ID"
# turn-1  status=in progress  runner_id=runner-1  lease_token=...
anx --as pm pm turns release turn-1 --from-file - <<'EOF'
{"runner_id":"runner-1","lease_token":"<lease_token from claim>"}
EOF
```

Channel ingress (Telegram and Discord) creates conversations with `origin` and
posts through the same `/pm/conversations/{id}/messages` pipeline; those turns
are claimed, completed, and failed identically. The channels lane owns
transport authentication.

### PM runner (`anx pm serve`)

The PM is an external agent. Do not call a model in-process. `make serve` seeds
persona `pm` (`actor-gds-pm` / `dev.pm`) for the default game-dev-studio
scenario. Enroll a host and select `--as pm` before starting the PM runner.
Wake routing and
`ANX_PM_BRIDGE_ENABLED` are not required.

Export `ZAI_API_KEY` in the shell that launches `pm serve`. The runner does not
read `~/.hermes/auth.json` or inject the key. If the harness argv names `zai`
and that variable is unset, the turn fails with a sentence that names
`ZAI_API_KEY`.

The harness child receives the **full parent environment**, then `HOME` is
reset to the login account home from passwd (`user.Current().HomeDir`). That
is where harness config lives (omp `models.yml`, Hermes, Codex). Isolated
`ANX_AS=pm` applies to the `anx` process identity, not to the child harness. After a successful claim the runner also
sets `ANX_PM_LEASE_TOKEN` for that turn. `anx pm turns propose` and
`anx pm turns context` send it when `--lease-token` is omitted, so the harness
does not have to copy the token into `--from-file`. The lease token is in the
runner process environment: any same-uid process can read it. A stolen token
only lets its holder complete, fail, or release that one turn until the
deadline. Ctrl-C / SIGTERM kills the direct harness process group (SIGTERM,
then SIGKILL after a short grace). A harness's detached children (those that
have left that process group) survive a successful run; only Ctrl-C /
SIGTERM of `pm serve` cleans up an in-flight group — find leftovers with
`ps -o pid,pgid,command`. An `agentctl` background execution outlives the
runner; the log prints its execution id so you can `agentctl cancel <id>`.

Seatbelt on this OS cannot hide another same-uid process's environment, so
run the harness as a different uid or on a different host from core's JIT
generated-reader process. The runner's `ANX_PM_LEASE_TOKEN` is in that same
unprotected environment; a stolen token is limited to complete/fail/release
of that one turn until the deadline. Example, separate uid:

```sh
sudo -u pm-runner env ZAI_API_KEY="$ZAI_API_KEY" \
  ANX_AS=pm ./cli/anx --as pm pm serve \
  --work-dir .tmp/pm-runner \
  --runner 'omp -p --mode json --model zai/glm-5.3 --auto-approve'
```

Example, separate host: start core locally, then on the runner machine
`ANX_BASE_URL=http://core-host:8000 ZAI_API_KEY=... ./cli/anx --as pm pm serve ...`.

```sh
make cli-build
export ZAI_API_KEY
ANX_DEV_BLOB_BACKEND=filesystem make serve
ANX_AS=pm ./cli/anx --as pm pm serve \
  --work-dir .tmp/pm-runner \
  --runner 'omp -p --mode json --model zai/glm-5.3 --auto-approve'
ANX_AS=maya ./cli/anx --as maya pm ask --wait \
  "What needs my decision?"
```

`--runner` is the native harness argv. Two forms:

- Without `{prompt}`: argv after `agentctl run --`. Example:
  `omp -p --mode json --model zai/glm-5.3 --auto-approve`
- With `{prompt}`: argv is executed directly. `{prompt}` is replaced with the
  absolute prompt file path. `agentctl` is not required.

omp may silently substitute models; every omp run must show
`"provider":"zai","model":"glm-5.3"` in the harness JSON
(`grep -o '"provider":"[^"]*","model":"[^"]*"'`). GPT models never go through
omp. The prompt stays small: the PM loads tracker context through
`anx work list|get` and `anx pm context`, never from a stuffed dump. Proposed
decisions must bind `work_ref` to a task. To attach evidence, the harness must
end its answer with a `---evidence---` block (one typed ref per line: `card:`,
`work:`, `artifact:`, `event:`, `decision:`, `topic:`, `document:`) or a JSON
`evidence_refs` array on the same object as the assistant text. Nested tool
output is not harvested. Mentions in prose are not recorded. `pm serve`
verifies each attached ref against core (`topics get` / `docs get` included)
and drops unresolvable ones instead of failing the turn.

Turn text is capped at the claimed turn's `max_output_bytes`. When the turn
omits that field, the runner uses 64000 (core's maximum accepted turn text)
minus room for a truncation marker. Evidence is parsed from the full harness
output first; if the stored reply is cut, the runner appends
`[reply truncated by anx pm serve at <N> bytes; <M> bytes were dropped]` and
logs a stderr warning with those sizes. The marker is included in the byte
budget so core still accepts the text.

`complete` and `fail` are retried on network errors, HTTP 5xx, and 429, with
bounded exponential backoff (1s, 2s, 4s, 8s, 16s; at most about one minute or
until the turn deadline). Identical terminal replays are idempotent, so a
retry after a lost response is safe. `lease_required` is not retried. If the
lease was lost (`lease_mismatch`, `turn_closed`, `turn_not_claimed`), the
runner reads the turn: already terminal logs `turn already <status>; nothing
to do` and moves on. A lost `complete` counts as one delivery attempt; the
process does not release-and-reclaim that turn, and the next poll claims it
again. A saved reply is delivered on that claim (never followed by a harness
run). After 3 failed `complete` calls for the same turn (including the first
delivery that wrote the file), the runner fails the turn with the
reader-facing reason `The PM produced a reply but it could not be delivered.
Ask again, or check that a runner is attached.` Core error `code` and
`message` stay on stderr (`pm serve: turn … undeliverable: <code>: <message>`).
The next poll can then claim a later waiting turn. A turn is given up after 3
harness runs in this process. If no saved reply exists, the runner fails the
turn with `The PM could not produce a deliverable reply after several
attempts.` (technical detail stays on stderr).

While a harness run is in progress, the runner renews the lease with
`POST /pm/turns/{turn_id}/heartbeat`. The interval is half the remaining time from
claim `lease_expires_at` when present, otherwise every 20s (for a 60s core
lease TTL). A transient failure (network error, HTTP 5xx, timeout) retries
after 2s, then 4s, capped at 8s, until a successful renewal or the lease
expires. Only a 409 `lease_mismatch` / `turn_closed` heartbeat cancels that
harness, logs `lease lost`, and follows lease-loss recovery (read the turn;
still pending and claimable re-claims and retries). A 404 heartbeat (older
core) disables heartbeats for the process and is logged once.

If `complete` cannot be delivered after retries for a reason other than lease
loss, the runner writes the reply to `turn-<id>.reply.md` (0600) in
`--work-dir`, releases the lease (`released turn … : undeliverable terminal
call`), and logs that the turn stays claimable until its deadline. The next
claim of that turn in this process retries `complete` from the saved reply
with the same bounded budget and does **not** run the harness while that file
exists. After 3 failed `complete` calls the runner fails the turn as above so
the queue is not starved. The reply file is then deleted and the turn is
forgotten. If `fail` itself is refused with `lease_mismatch` or
`turn_closed`, the runner treats the turn as no longer ours, logs why,
deletes the file, and moves on. The file is also deleted after a successful
complete or when the turn is already terminal. If the saved reply is
unreadable or corrupt, the runner logs and falls back to the harness.
Runner-internal failures (harness crash, timeout, missing secret) still fail
the turn with a reader-facing reason; technical detail stays on stderr.
Shutdown releases log `released turn … : shutdown`.

`--max-concurrent N` (default 1) runs up to N turns in this process. Each
worker claims with a distinct runner id `<runner_id>-<slot>` (slots 1..N),
so core treats them as N runners. Capacity counts leases: N must be ≤
`ANX_PM_MAX_CONCURRENT`.

Output bytes and wall time come from core `pm.Config` (defaults 16000 bytes and
2 minutes; core accepts `ANX_PM_MAX_OUTPUT_BYTES` up to 64000). `make serve` sets `ANX_PM_TURN_TIMEOUT=10m` so omp/glm-5.3 can use
tools before the lease expires. `ANX_PM_MAX_CONCURRENT` (default 2) bounds
workspace sending turns. `make pm-serve` runs the seeded PM persona.

When stderr is not a TTY, runner logs are flushed immediately and harness
stdout/stderr are copied to stderr. On restart, `pm turns claim` with the same
worker runner ids (`<runner_id>-<slot>`) recovers in-flight leases;
past-deadline sending turns expire to `failed` with a visible reason.

Hermes and Codex are the same runner with a `{prompt}` argv (do not run Hermes
from this checkout unless asked):

```sh
# Hermes (direct)
ANX_AS=pm ./cli/anx --as pm pm serve \
  --work-dir .tmp/pm-runner \
  --runner 'hermes -p --provider zai --model glm-5.3 -- {prompt}'

# Codex (direct)
ANX_AS=pm ./cli/anx --as pm pm serve \
  --work-dir .tmp/pm-runner \
  --runner 'codex exec --skip-git-repo-check -- {prompt}'

# Same harnesses through agentctl (no {prompt} placeholder)
ANX_AS=pm ./cli/anx --as pm pm serve \
  --work-dir .tmp/pm-runner \
  --runner 'hermes -p --provider zai --model glm-5.3'
```

PM context is bounded to 1..50 items. PM conversation/decision/action lists accept
`--limit` (1..200) and `--cursor`, returning `next_cursor` and `has_more`. Cursors are
bound to the current workspace, principal and record kind; do not reuse one after
switching derived agents. Lists do not support server-side project filtering; use
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

## Docs as a cross-host knowledge base

Docs are the workspace knowledge base. An agent on a host with no other access
publishes what that host can see, then other hosts search and comment. Tag those
documents `knowledge` and always set:

- `--source` — canonical URL or `host://<hostname>/...` pointer
- `--hosts` — which hosts the fact applies to
- `--verified-at` — RFC3339 time of last verification

```bash
# Publish from this host (stdin body; handle is the idempotency key)
printf 'SSH to proxmox is keyed in ~/.ssh/id_ed25519_proxmox\n' | \
  anx docs put - \
    --handle kb-proxmox-ssh \
    --title "Proxmox SSH" \
    --tags knowledge \
    --source "host://$(hostname)/ssh" \
    --hosts "$(hostname)" \
    --verified-at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Find and read knowledge another host wrote
anx docs search "proxmox" --knowledge --host "$(hostname)" --limit 20
anx docs get kb-proxmox-ssh --format md

# Discussion survives later document revisions; comment refs are UI deep-links
anx docs comment kb-proxmox-ssh "Verified from $(hostname)"
anx docs comments kb-proxmox-ssh
anx docs comments edit kb-proxmox-ssh event:<handle> --body "Corrected"

# Publish a git markdown tree (read the files; do not write the repo)
anx docs ingest /path/to/knowledge-base \
  --source https://github.com/example/knowledge-base/blob/main
anx docs search "NOW.md" --knowledge --limit 20
```

`anx docs search` is SQLite FTS5 over title, body, summary, source, tags, and
comments. `--tag`, `--limit`, and `--cursor` paginate. `anx docs put -` reads
stdin. `anx docs get <handle> --format md` prints the body only.

Publish a markdown tree (idempotent by relative path; a second run creates no
new revisions):

```bash
anx docs ingest /path/to/knowledge-base \
  --source https://github.com/example/knowledge-base/blob/main
anx docs search "NOW.md" --knowledge --limit 20
```

## Initiative plans and ref previews

Keep one plan per initiative card. Add steps instead of progress prose, link each step to a real ref when possible, and never choose a view. `anx plan show card:<slug>` returns live step status, progress, critical path, next steps, shape and health. `anx plan set card:<slug> --from-file plan.json` accepts `{ "steps": [...] }`; it reads the current card token before writing. Pass `--if-updated-at` from a previous read when edits must bind to that snapshot. Step writes also read then compare the token; conflicts are returned without automatic retries.

```sh
anx plan step add card:launch --step-id build --title "Build" --ref card:implementation
anx plan step add card:launch --step-id test --title "Test" --after build
anx plan step add card:launch --step-id docs --title "Docs" --after build --ref doc:launch-guide
anx plan step update card:launch test --status done
anx refs resolve card:launch doc:launch-guide topic:release board:initiatives card:unknown
```

Use `step rm <card> <step-id>` after updating any dependent `--after` lists. Empty `--ref`, `--due`, or `--status` clears the optional field; `--after ""` clears dependencies. Known ref state overrides fallback status. Unknown URLs remain unresolved until an existing source-backed card supplies state; plan reads never fetch URLs.
