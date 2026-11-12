# CLI runbook

## Host enrollment and identity

Set `ANX_BASE_URL` to the workspace core URL. A human auth-admin must bootstrap the workspace before a host can enroll.

```bash
anx host enroll --plan
anx host enroll --name "$(hostname -s | tr A-Z a-z)"
anx auth whoami
anx doctor
```

If `identity_resolution` fails, pass `--as <agent-name>` or set
`ANX_AS=<agent-name>` to the lowercase name of the agent tool you are running
in. Reuse a name this host already has when one fits (doctor lists the names known on this machine); a
new name becomes a new persona on its first call, so never invent one per task.
Stop only if you cannot tell which agent tool you are running in; otherwise
rerun `anx doctor` with that identity.

Interactive enrollment prints a user code and, when core has `ANX_PUBLIC_WEB_UI_WORKSPACE_URL` configured, the full workspace-scoped verification URL. Set that config to this deployment's public web UI workspace path. The core API base URL and web UI path must target the same organization and workspace. Without it, open Access → Hosts in the workspace web UI and approve the printed code. The CLI polls at the server interval. For unattended fleet hosts, use the auth-admin agent flow below. `anx host enroll --token-stdin` keeps the secret out of process arguments. A host key and record are stored under `~/.config/anx/hosts/<workspace-key>/` with owner-only permissions.

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
: "${ANX_BASE_URL:?Set to this workspace's core URL on this machine}"
: "${ANX_AS:?Set to your explicitly granted auth-admin identity}"
: "${DESTINATION_HOST:?Set to the destination machine's SSH name}"
anx --json host tokens create --label "$DESTINATION_HOST" --expires-in 1h \
  | jq -er '.result.token' \
  | ssh "$DESTINATION_HOST" 'anx host enroll --name "$(hostname -s | tr A-Z a-z)" --token-stdin'
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

Run `anx config workspaces` when unsure. It lists enrolled workspaces and aliases, the default, and which directory rule applies to cwd, even when selection is ambiguous. Enrollment discovers an alias from the workspace slug (falling back to local workspace metadata), makes the first enrolled workspace the default only when no default exists, and prints the command to select the new workspace. Later enrollments preserve the configured default.

```bash
anx config workspaces
: "${ANX_WORKSPACE_ALIAS:?Choose an enrolled alias from the list above}"
: "${ANX_PROJECT_GLOB:?Set to an absolute glob for the project to map}"
anx config use "$ANX_WORKSPACE_ALIAS"
anx config map "$ANX_PROJECT_GLOB" "$ANX_WORKSPACE_ALIAS"
anx config show
anx --workspace "$ANX_WORKSPACE_ALIAS" orient
anx config unmap "$ANX_PROJECT_GLOB"
```

Preferences live in `~/.config/anx/workspaces.json`, alongside the JSON host records, or under `ANX_CONFIG_DIR` / `--config-dir`. No repository metadata is read or written. `config use` and `config map` accept aliases or absolute HTTP(S) base URLs; saved choices use URLs so alias changes cannot reroute them. Discovered aliases are persisted by enrollment and preference writes; reads remain read-only. Collisions receive stable numeric suffixes without rebinding existing aliases.

Selection order is `--base-url` or `--workspace` > `ANX_BASE_URL` > directory rule > configured default > single enrolled host (`bridge:auto-single`). Only zero enrolled hosts retain the localhost development default. Multiple enrolled workspaces with no selection fail before networking and name the aliases and exact `anx config use` repairs. `anx doctor` reports the selected workspace and source, and fails on ambiguity. Use `config workspaces` and user-global preferences for agent orientation; never hardcode `--base-url` in agent prompts.

Directory globs must be absolute or begin with `~/`; quote them against shell expansion. `**` matches zero or more complete components; `*`, `?`, and character classes match within a component. Matching follows the cwd volume's case sensitivity for every component, including literals after wildcards. Rules rank by longest literal prefix, then most literal characters, with lexical order breaking ties. Existing symlink prefixes are resolved when matching against cwd, so logical home/work paths also match physical directories. A rule wins over the configured default. `sources.base_url` names the winning rule as `config:directory-rule:<glob>`, the default as `config:default`, or an explicit alias as `flag:--workspace`. Use either URL flag or alias flag, not both.

This JSON shows the file shape only. Replace every placeholder with values for
the enrolled workspace and project before using it:

```json
{
  "default": "<workspace-core-url>",
  "aliases": {
    "<workspace-alias>": "<workspace-core-url>"
  },
  "directory_rules": {
    "<absolute-project-glob>": "<workspace-core-url>"
  }
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
  "capabilities": {
    "resume": "unknown",
    "history": "unsupported",
    "logs": "unknown"
  },
  "activity": "active",
  "sequence": 1
}
```

```bash
: "${ANX_AS:?Set this to the identity you intend to use with Agent Nexus}"
anx sessions register --from-file session.json
anx sessions get <session-id>
```

Enrolled agents get a server-bound host namespace; do not put a machine pathname
or secret in `native_session_id`. Standalone principals supply an explicit opaque
`host_scope`, which is recorded as a namespace rather than proof of another
enrolled host. For agentctl correlation use its `native_session.id` and
`native_session.id_kind=provider_session_sha256`, not the execution ID. Missing
session evidence stays unknown; do not substitute a parent or guessed session.

Then prepare `participation.json` with the returned `session_id`:

```json
{ "session_id": "<session-id>", "activity": "active", "sequence": 1 }
```

```bash
: "${ANX_AS:?Set this to the identity you intend to use with Agent Nexus}"
anx work participants register card:launch --from-file participation.json
anx work participants list card:launch --limit 50
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

Use `anx context` to read evidence first. `anx work start` deliberately
assigns and moves native work; it is not a registration shortcut. Project
association should use clear configured evidence and ask when ambiguous. A
successful run/session does not satisfy the task's acceptance criteria.

The bundled participant skill advertises `anx.participant.v8`; any ordinary
agent can load the additional `anx.pm.v8` skill to support project PM work. After host enrollment,
the CLI offers `anx skills sync`. Preview with `anx skills sync --dry-run`, then
run `anx skills sync` to install or refresh clean managed copies for detected
Claude Code, Codex/OMP, Cursor, Hermes, Gemini CLI, Pi, Continue and OpenCode
setups. Use `--pm` to opt into the PM skill. `anx skills status` and `anx doctor`
report state by harness. Edited or unmanaged content is preserved, and copies
marked as agentctl-owned are deferred to agentctl. Legacy ANX copies are reported
with their digest; `anx skills adopt <path>` shows the plan and only replaces one
after the digest is confirmed, migrating it to the canonical harness path and
keeping a timestamped backup in the legacy directory. Use `anx skills
configure|status|verify --path <skill-directory> --role participant|pm` for
explicit local management and read-only verification.

The CLI refreshes clean managed copies in a detached best-effort process after
an upgrade and once per day after a mutating command. Disable this with
`anx skills sync --no-auto-sync` or `ANX_SKILLS_AUTO_SYNC=0`. File verification
never proves an existing session loaded the skill. Supported harness delivery
composes with agentctl packs, not a second ANX harness catalog. See [managed
skill sources and migration](../skills/README.md). Automatic enrollment setup and
real harness activation remain later gates in the
[adoption plan](../../docs/architecture/existing-agent-adoption.md).

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

The ordinary integration lane includes the agent ergonomics scenario and a
20-iteration contention regression:

```bash
go test -tags=integration ./integration -run '^(TestAgentErgonomicsAgainstCore|TestConcurrentMovesAndAnsweredAwaitAgainstCore)$' -count=1 -v -timeout=10m
```

The contention test runs two core processes, with two concurrent agent writers
per process and separate boards to avoid intentional board-token conflicts.
Each agent issues 60 start/block commands and reads an answered ask 20 times. `anx await` reads
`GET /asks/{ask_id}` before subscribing; a terminal answer needs neither a
subscription write nor SSE. Open asks still subscribe and consume the stream's
initial durable snapshot, covering answers committed during subscription setup.

Card move errors with `outcome_unknown` or HTTP 503 trigger one authorized
work-state readback (without card revision history). When the target column and any supplied resolution match, the CLI
returns success with `recovered: true` without sending another move. Ranked
moves require manual placement verification. Unmatched or unavailable readbacks
retain the original error; `outcome_unknown` is non-retryable and never receives
fresh-token retry advice. `error.details.retry_after` preserves the server's
Retry-After header. This recovery does not automatically retry a failed write.

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

The work and resource refs below are demonstration values. Replace them with
refs from this workspace before running the commands. The commands use the
current ANX identity; set it explicitly only when automatic resolution is not
available.

```bash
: "${ANX_AS:?Set this to the identity you intend to use with Agent Nexus}"
printf '{"topic":{"title":"Incident #42","summary":"Investigate #42","owner_refs":[],"board_refs":[],"document_refs":[],"related_refs":[],"provenance":{"sources":["event:example"]}}}\n' | anx topics create
anx topics list --state active

anx events stream --max-events 1
anx inbox stream --max-events 1
anx events stream --follow
# Diagnostic/local helper over backing-thread timelines; prefer topics/cards/boards for primary coordination reads.
anx events list --thread-id thread_123 --thread-id thread_456 --type message_posted --mine --max-events 20
anx provenance walk --from event:incident-42 --depth 2
anx topics get incident-42
anx topics create --title "Launch" --summary "Coordinate launch work"
anx topics message incident-42 --body-file message.md
anx topics messages incident-42 --max-events 10
anx topics workspace incident-42
# Backing-thread reads (tooling/diagnostics; prefer topics workspace for operator triage)
anx threads inspect thread_123 --max-events 50
anx threads context --state active
anx threads workspace thread_123
anx docs content product-constitution
# Browser link for a human; requires ANX_WEB_BASE_URL on the core (see README).
anx link card:rescue-digest document:product-constitution
anx docs message product-constitution --body-file note.md
anx docs messages product-constitution --max-events 10
anx artifacts inspect --artifact-id incident-42-log
anx workspace summary
anx boards list --state active
anx boards create --topic incident-42 --title "Launch board"
anx boards workspace product-launch
# Cards: draft prose locally, then use domain verbs for active work.
anx cards list --board product-launch
anx cards create --board product-launch --topic incident-42 --title "Rescue digest" --body-file card.md
anx cards revise rescue-digest --body-file card.md
: "${ANX_ASSIGNEE_REF:?Set this to the intended typed actor ref}"
anx cards assign card:rescue-digest --assignee-ref "$ANX_ASSIGNEE_REF"
anx cards move rescue-digest --column review
anx cards resolve rescue-digest --body-file evidence.md
# Packet APIs are subject-based: `packet.subject_ref` must be `card:<card-handle>`.
anx receipts create --from-file receipt.json
anx reviews create --from-file review.json
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
printf '%s\n' '{"topic":{"title":"Drafted incident","summary":"Staged via draft","owner_refs":[],"board_refs":[],"document_refs":[],"related_refs":[],"provenance":{"sources":["event:example"]}}}' | anx draft create --command topics.create
anx draft list
anx draft commit <draft-id>
anx draft discard <draft-id>
```

Use `draft` for reviewable JSON writes, broad/risky mutations, or changes delegated by a human where an inspectable checkpoint is useful. Prefer direct domain verbs for narrow, already-verified changes.

The raw fallback remains available:

```bash
anx api call --path /meta/handshake
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

Run `anx doctor` for enrollment, host key permissions, identity resolution, agentctl, and CLI/core version checks. Active runtime evidence can resolve the current caller; an installed adapter by itself cannot. If identity remains unresolved, doctor shows the required `--as <agent-name>` / `ANX_AS=<agent-name>` form. Use the lowercase name of the agent tool you are running in. Reuse a name this host already has when one fits (doctor lists the names known on this machine); a new name becomes a new persona on its first call, so never invent one per task. A name the inactivity sweep archived is only hidden from Access: the next call with that name unarchives it. A name a human archived returns `agent_archived` (exit 5): use another name or ask a human to restore it on the Access page; never invent a new name to get around it. Stop only if you cannot tell which agent tool you are running in; otherwise rerun doctor with that identity. Missing `agentctl` is an optional warning for direct `anx` commands. Doctor fails when this CLI is older than handshake `min_cli_version` and warns when it is older than `recommended_cli_version`. The repair is `anx update --version <recommended>`. If the host was revoked, ask a human auth-admin to enroll a replacement. Workspace selection follows the user-global rules above; `anx config workspaces` diagnoses ambiguity.

### Version mismatch

Symptoms:

- server returns `cli_outdated` (HTTP 426) when this CLI is below handshake `min_cli_version`
- commands fail before mutation with compatibility errors
- a managed install with update policy `auto` installs the recommended release and retries the command once; `notify`, `off`, and unmanaged installs print `anx update --version <recommended>` and do not replace the binary
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
core_url="${ANX_BASE_URL:?set the reachable core URL}"
curl -N -H 'Accept: text/event-stream' "${core_url%/}/stream/events"
curl -N -H 'Accept: text/event-stream' "${core_url%/}/stream/inbox"
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
anx context card:launch --limit 10
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
    "facts": { "native_status": "in_progress" },
    "evidence": [
      { "ref": "artifact:synthetic-check", "summary": "Synthetic example only" }
    ],
    "uncertainty": ["Deployment not verified"],
    "coverage": { "complete": false }
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

The built-in Ask PM product, its runner, turn leases, presence and channel
ingress were removed in 2026-11. Any ordinary agent can support project PM work with the
bundled `anx-pm` skill and the normal CLI (`pm.propose` and `pm.approve` remain human-only); see [PM is a skill](pm-connection.md).
Decide, cards and goals remain.

```sh
anx pm context --work-ref card:launch --limit 20
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
`approve` and `text`. `pm.propose` and `pm.approve` are human-only; non-human
principals are rejected with `forbidden`, and core enforces the current principal
and scope. Agents support PM work by orienting, triaging, filing `anx ask`
recommendations, and updating cards. A successful answer
records intent, not delivery. `pm actions get` reports the actual action attempts
and receipt, including `source_reported`, `unknown` and
`receipt.independently_verified`. `reconcile` requests read-back, never a resend.
There is no client command to manufacture a verified receipt.

PM context is bounded to 1..50 items. Decision and action lists accept
`--limit` and `--cursor`.

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

## Work tree and Unsorted

Initiatives are top-level cards outside Unsorted. Each card has one stored parent;
board roles and plan links never define hierarchy. Start with `anx orient`,
`anx tree` and `anx where <ref|path|text>`. Create with `anx work create --title
"Outcome" --under card:<parent>`, or explicitly `--initiative`. Omitting both
lands in Unsorted with a warning. Confirm suggested homes with `anx mv <card>
--under <parent>`. Use `--root --reason "Independent outcome"` for top level or
`--unsorted` for the tray. Moving an initiative under another card needs a reason.
Depth is at most three; children are ordered by creation time. Stable card refs
are canonical; paths are aliases. See [the work tree guide](../../docs/grouping-work.md).

## Plans and ref previews (power verbs)

`anx plan` maintains independent checklists, including external steps and legacy
card refs with `after` dependencies. It never establishes parentage. Link steps
to real refs when possible. `anx plan show card:<slug>` returns live step status, progress, critical path, next steps, shape and health. `anx plan set card:<slug> --from-file plan.json` accepts `{ "steps": [...] }`; it reads the current card token before writing. Pass `--if-updated-at` from a previous read when edits must bind to that snapshot. Step writes also read then compare the token; conflicts are returned without automatic retries.

```sh
anx plan step add card:launch --step-id build --title "Build" --ref card:implementation
anx plan step add card:launch --step-id test --title "Test" --after build
anx plan step add card:launch --step-id docs --title "Docs" --after build --ref doc:launch-guide
anx plan step update card:launch test --status done
anx refs resolve card:launch doc:launch-guide topic:release board:initiatives card:unknown
```

Use `step rm <card> <step-id>` after updating any dependent `--after` lists. Empty `--ref`, `--due`, or `--status` clears the optional field; `--after ""` clears dependencies. Known ref state overrides fallback status. Unknown URLs remain unresolved until an existing source-backed card supplies state; plan reads never fetch URLs.

## Live dashboard series

Prefer native live queries. Declare external-data adapters before pushing, and keep third-party credentials on the enrolled owner host. See `../../docs/live-series.md` and `../../adapters/series/README.md` for commands, scripts, grants and schedules.

### Optional harness task mirroring

Use `anx integrations list`, then `anx integrations enable
claude-code-suggested-tasks --board <board-ref> --dry-run` to inspect setup before
opting in. Hooks are disabled by default. See [Harness integrations](../README.md#harness-integrations)
for scopes, cwd routing, agent identity, bounded spool retention, and disabling.
For pending `no_board` events, enable the integration with a board in that mapped
directory (or pin `--workspace`). For `delivery_failed`, check ordinary host
enrollment, authorization, connectivity and the configured board. A later hook
invocation retries up to four persisted events using their original replay keys.
Missing or corrupt installation config preserves incoming events with
`missing_config` or `config_invalid`; repair the config and re-enable to resume.
Card-create HTTP 400/422 is retained as a dead letter and excluded from automatic
drain. HTTP 401/403/404 stays pending as `auth`/`forbidden`/`board_not_found` and
replays after access or the board is repaired. Inspect `dead_letter_count` and
`dead_letter_reasons` in status. Pending and dead-letter text share the 100-event,
30-day retention budget. Status is offline and read-only; disable retains the
spool and receipts.
