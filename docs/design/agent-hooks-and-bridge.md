# Agent hooks, Git capture and a first-class bridge

Status: reviewed proposal (2026-10-17). Owner: coordinator. Implementer: Codex.
**Implementation waits for [PR #429](https://github.com/Git-on-my-level/agent-nexus/pull/429)
to merge, then rebases on main. This turn is documentation only.**

## Purpose

Inject a small ANX brief at session start and after compaction, capture optional
Git progress notes, and let credential-less agents submit notes through files.
Keep the bridge first class, visible per machine and **never required**.
Core remains authoritative; Git transports authored claims; mirrors are caches.
No repository mirroring, core-side remote fetches, hosted-specific logic,
transcript ingestion, live mounts or new orchestration layer.

| Layer     | Without a bridge                                        | Bridge adds                                    |
| --------- | ------------------------------------------------------- | ---------------------------------------------- |
| Context   | `anx context`, generic hook, supported harness adapters | Nothing required                               |
| Git notes | Git post-commit hook or manual `anx git sync`           | Periodic sync and offline drain                |
| Files     | Original source files remain readable                   | Scoped mirror and credentialed outbox delivery |

## What exists (and corrections)

| Code                                                            | Current behavior; design consequence                                                                                                                                                                                                                                                                                                                                                                                                  |
| --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---- | ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `cli/internal/integrations/`                                    | One recipe, `claude-code-suggested-tasks`; one Claude settings adapter. Recipe embeds harness/event/parser; adapter owns settings only. Generalize parsing/rendering as well as settings. `enable` has no `--harness` today. Preserve existing recipe behavior.                                                                                                                                                                       |
| `cli/internal/app/integrations.go`, `integrations/spool.go`     | 3 s fail-open hook; frozen URL/agent/host/principal/body before send. Limits: 100 pending + dead letters, 1,000 receipts, 30 days, four sends/drain. Pruning can delete unsent entries: this is not a lossless user outbox.                                                                                                                                                                                                           |
| `bridge_commands.go`, `ask_bridge.go`, `bridge_unit_install.go` | Default `bridge install` installs Go `bridge run` per derived agent. Local workspace/actor lock proves answer-consumer presence. Locally registered shell commands receive answer JSON on stdin; five attempts / 30 min default. Success persists before acknowledgement, but a crash after execution and before persistence can repeat the executor.                                                                                 |
| `bridge_lifecycle.go`, `adapters/agent-bridge/`                 | Legacy `install --ref/--python/...` and `start                                                                                                                                                                                                                                                                                                                                                                                        | stop | status | doctor --config` manage the separate Python host wake router. It validates the complete runtime roster before signed host check-in. These lifecycle commands do not manage Go answer units today. |
| Core auth/host storage                                          | `hosts.bridge_instance_id`, `bridge_checked_in_at`, `bridge_expires_at` already exist. Signed `POST /hosts/{host_id}/bridge/check-in` drives agents' `bridge_online`. Host responses omit those times; there is no general host last-seen.                                                                                                                                                                                            |
| Setup, identity, UI                                             | Setup is `anx help <job>`, not an `anx setup` wizard. `host discover` reports optional agentctl identity/availability, not hook capability. Directory workspace rules do not select cards. `orient` already warns about undeliverable resume asks. Agents/Access already render bridge indicators; `HostCard` infers status from child agents, missing zero-agent hosts. Participant source is `cli/skills/anx-participant/SKILL.md`. |

Preserve [ask delivery](../ask-delivery.md): a live bridge alone supplies no
resume command; `await`, `batch` and explicit `none` remain supported. An answer
consumer must not set host-wide `bridge_online` or imply mentions can wake every
agent. Keep Python routing operational; runtime unification is out of scope.

## 1. Hooks and card resolution

Add `context.inject` and `git.capture` recipes through integrations, plus
`anx hook <event> [--harness <name>]`. Generic stdin is one object, ≤64 KiB:

```json
{
  "schema_version": 1,
  "session_id": "opaque-local-id",
  "cwd": "/absolute/project",
  "card_ref": "card:hook-work",
  "tool": { "name": "Bash" }
}
```

Only `schema_version` and absolute `cwd` are required. Generic events:
`session.start`, `context.compacted`, `tool.after`, `git.commit`. The last comes
from Git itself, never command-text matching. Generic stdout:
`{schema_version:1,context:string,card_ref:string|null,as_of:string|null,stale:boolean}`.
Harness mode uses its wire protocol instead of CLI envelopes; ordinary
install/status/sync commands retain envelope v2 and standard exit codes.

| Adapter     | Settings / selected input                                                                                                                                                        | Events / stdout                                                                                                                                                                                                             |
| ----------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Claude Code | Existing user/project/local settings paths; `session_id,cwd,hook_event_name,source`; tool hooks also `tool_name,tool_use_id`                                                     | `SessionStart` startup/resume/clear → start; source `compact` → compacted. Return `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"…"}}`. PostToolUse only hints a bounded repo scan, no output. |
| Codex       | User or trusted project `.codex/hooks.json`; preserve inline TOML hooks. Same selected fields as Claude                                                                          | `SessionStart`, including source `compact`, returns the same context wrapper. Do not inject through PreCompact/PostCompact. Harness hook trust review remains required.                                                     |
| Cursor      | User/project `.cursor/hooks.json`, `{version:1,hooks:{…}}`; `session_id` (fallback `conversation_id`), `workspace_roots,hook_event_name`; require a selected root when ambiguous | `sessionStart` returns `{"additional_context":"…"}` asynchronously. No promised post-compaction injection; retain manual `anx context`. Optional `afterShellExecution` only hints a scan.                                   |
| Plain Git   | Discover effective hook path through Git; no JSON stdin; repo comes from Git                                                                                                     | `post-commit` schedules capture, no stdout, never rejects a commit for an ANX failure. Never overwrite an existing hook or change `core.hooksPath`; offer an explicit chaining snippet if occupied.                         |

Sources checked 2026-10-17: [Claude Code](https://code.claude.com/docs/en/hooks),
[Codex](https://developers.openai.com/codex/hooks),
[Cursor](https://cursor.com/docs/hooks). Pin tested harness versions and fixtures
in PR1; availability is not capability. Unsupported versions retain the generic
and manual paths. Never read transcripts, prompts, tool output, email or
compaction summaries, even when their paths/content appear in hook payloads.

Resolve card in order:

1. Explicit new `--card-id`, then payload `card_ref`.
2. Explicit local session binding (`anx integrations bind --card-id <ref>`),
   keyed by workspace/principal/provider/session hash. A known participation can
   validate that candidate through existing session/task APIs; no reverse
   session→cards API exists today. Do not enumerate all cards looking for it.
3. Explicit per-worktree default, then longest directory card rule, stored in
   integration configuration outside Git. Workspace rules still select only a
   workspace. Do not infer cards from titles, branches or agent-wide presence.
4. None/ambiguous → no injection, with configuration guidance in status.

Use existing workspace/identity resolution; payloads never choose credentials
or base URLs. Resolve a canonical card with a scoped read. Invalid/inaccessible
explicit targets do not fall through. Existing participation leases are 120 s;
reading context must not implicitly renew sessions, participation or presence.

- Reuse `anx context`'s work/observation/refresh reads, with ten observations;
  render ref/title, phase, current promise, selected evidence and next action.
  Cap at 6 KiB UTF-8 (roughly 1.5k tokens, not a token guarantee).
- Enforce 1.5 s including stdin, identity, locks and HTTP. The current 2 s
  agentctl probe must accept the remaining budget. Fail open: exit 0, empty
  harness output, one sanitized stderr diagnostic; unconfigured is silent.
  No surviving background shell. Generic failures return an empty result.
- Owner-only atomic cache, keyed by workspace/principal/card/scope. Stale fallback
  is off by default; opt-in allows five minutes with `STALE as_of=…`. Never use
  it after 401/403/404, identity changes or expired local credentials. Offline
  revocation cannot be verified; disclose this when enabling fallback.
- Extend `integrations enable <recipe> --harness … --dry-run`; preserve backups,
  exact owned-command removal and concurrent-edit protections. Cut the duplicate
  `anx hooks install` installer and automatic edits to every detected harness.

## 2. Git capture: notes first

`anx git enable [<repo>] --workspace <alias>` previews and opts in one repo,
recording a random repo UUID and initial HEAD under Git's private metadata.
Separate per-worktree card defaults. Clones share UUID only by explicit choice.
No initial history upload: `anx git sync [<repo>] --since <sha>` is explicit
backfill. Freeze workspace/writer before scanning; never fetch remote refs.

```text
Anx-Card: card:hook-work
Anx-Note: Added bounded context injection
```

| Concern   | Rule                                                                                                                                                                                                                                                                                                                                                                                 |
| --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Parse     | Use exact Git argv and trailer-block semantics (`git interpret-trailers --parse`), never regex over prose/diffs or shell evaluation. ANX keys are case-insensitive; trim values. Inspect the original block to reject folded/multiline ANX values. Reject duplicate ANX keys, multiple cards, NUL/control characters or invalid UTF-8. Unknown trailers ignored; no inline comments. |
| Bounds    | One explicit `card:` per commit; missing card skips. Commit message ≤64 KiB, ref ≤256 bytes, note ≤4 KiB; absent note uses subject. Malformed/reserved trailers quarantine SHA/reason before advancing, rather than publishing a partial note.                                                                                                                                       |
| Authority | V1 publishes notes only. Reserve `Anx-Step`, `Anx-Status`, `Anx-Next` with `unsupported_trailer` diagnostics. Commits prove authored changes, not merge/deployment/acceptance. Never automatically complete cards.                                                                                                                                                                   |
| Evidence  | Explicit credential-free HTTP(S) commit URL template with full object ID. Otherwise record SHA as plain text and omit `evidence_refs`: `commit:<sha>` is not a supported evidence ref. Do not assume a Git remote is a web permalink or upload credential-bearing URLs. Core never fetches them.                                                                                     |
| Trigger   | Git post-commit, manual sync, optional tool-after scan hint and periodic bridge reconciliation of registered local refs. No command parsing or recursive filesystem watcher.                                                                                                                                                                                                         |
| Cursor    | Snapshot configured ref tips; at most 100 new commits / five seconds per tick. Persist bounded-walk continuation and durable outcomes before advancing. Enable seeds the seen boundary; rewrites rescan from saved boundaries within the same budget. Overflow requires visible explicit backfill. Shallow/missing objects defer; never skip a gap or walk unlimited history.        |

Build the existing ordinary `message_posted` request, not a subprocess running
`anx note`:
`{actor_id,request_key,event:{type,actor_id,thread_ref,refs,summary,payload,provenance}}`.
Use the card-message builder's resolved card/thread/board refs, `kind:card_message`,
text/subject fields, optional `evidence_refs` and SHA in text. No diffs, author
email or paths. Do not issue `anx note`'s convenience presence PATCH.

Key: `anx-git-v1-` + SHA256(repo UUID NUL full object ID NUL canonical card ID).
Event replay is **actor-scoped**: overlapping triggers deduplicate with the same
writer and frozen body, not across hosts/principals. Elect one capture writer
per repo. Rebase/cherry-pick/amend makes new objects and potentially new notes;
reverts append information. No patch-id guarantee. Keep confirmed-SHA receipts;
after state loss require explicit backfill, not speculative resending.

Generalize typed spool payload/dispatch while reading old queues unchanged.
For new durable capture/outbox entries, disable age/capacity deletion: refuse
intake visibly, leave commits/files intact, claim queued only after fsync.
Freeze body/key/URL/actor/host before sending; serialize drainers. Four sends/tick,
1 s per request, exponential jittered backoff 1–60 s. Network/timeout/429/5xx
retain the request; 401/403/404 pause for repair; 400/422/409 are inspectable
failures. Never recompute a conflicting body under the same key.
Follow `core/internal/replaypolicy/policy.json`: uncertain automatic retries stop
at 25 days, before 30-day receipt GC, pending explicit reconciliation. A local
ledger cannot promise server exactly-once forever.

## 3. Bridge services, heartbeat and setup

Keep answers per agent. Add opt-in host-jobs mode of `bridge run`
(`--jobs git,spool`, later `files`, explicit `--as` writer), with its own
workspace/host singleton lock and service unit. This mode runs jobs only;
existing answer units remain unchanged. Job failure must not stop another job
or answer delivery. Extend no-config `bridge status|doctor` for Go services;
preserve Python `--config` behavior. Log rotation is new required work;
automatic restart by `anx update` is deferred.

### API and data model

Extend signed `POST /hosts/{host_id}/bridge/check-in`. Do not introduce another
host-auth scheme or reuse core's unrelated heartbeat-publisher environment.

| Contract               | Proposal                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Legacy request         | `{bridge_instance_id,checked_in_at,expires_at}` and roster validation unchanged. Existing `X-ANX-Host-Key-Id`, `X-ANX-Host-Signed-At`, `X-ANX-Host-Signature` sign `anx-host-bridge-check-in\|host_id\|signed_at\|base64url(SHA256(raw_body))`. Keep skew/replay/revoked-key checks; no accompanying bearer.                                                                                                                                                             |
| New optional `service` | `{kind:"answer"\|"host-jobs",actor_id:string,version:string,jobs:string[]}`. Actor must be active, non-excluded, belonging to signing host. Version ≤64 ASCII characters, instance ≤128. Unique jobs ≤4 from `answers,git,spool,files`, compatible with kind. Reject arbitrary metadata. CLI helper signs the complete body.                                                                                                                                             |
| New-mode time          | Retain legacy required wire times; validate positive duration ≤300 s. Store server receipt time as `last_seen_at`, with fixed 300 s expiry. Client clock cannot extend presence. Legacy wake semantics unchanged.                                                                                                                                                                                                                                                        |
| Response               | Existing `{bridge:{host_id,bridge_instance_id,checked_in_at,expires_at}}`. New mode adds `service:{kind,actor_id,version,jobs,last_seen_at}` inside `bridge`; returned times reflect server values. Legacy response unchanged.                                                                                                                                                                                                                                           |
| Migration              | Next migration adds `host_bridge_services(host_id,kind,actor_id,instance_id,version,jobs_json,signed_at,last_seen_at,expires_at)`, PK `(host_id,kind,actor_id)`, host FK. Fixed-size metadata; upsert, no per-beat history. Verify active key/host/actor and write in one transaction; reject signed times older than the stored slot. Instances replace the same slot, not append rows. Classify all fields in the storage privacy inventory; no startup scan/backfill. |
| Separation             | New service beats never update `hosts.bridge_*` or agent `bridge_online`. Those retain wake-routing meaning. No historical timestamp backfill. Token grants, sessions and agent progress are separate. Revocation overrides unexpired presence immediately.                                                                                                                                                                                                              |

Beat immediately after readiness, then every 60 s with jitter on an independent
timer, including during long answer executors. HTTP timeout 2 s; fresh proof on
retry, at most cadence-rate retries. Reporting defaults off (`--heartbeat` /
persisted unit option). Unsupported core disables reporting with a diagnostic
without disrupting delivery. Presence means a consumer checked in, not that an
answer was delivered or jobs succeeded; stale after five minutes.

Add authenticated read-only `POST /hosts/bridge-status`:

- Input `{host_ids:[canonical_id,…]}`, unique ≤100.
- Output `{as_of,hosts:[{host_id,revoked_at:string|null,wake:{last_seen_at,expires_at}|null,services:[{kind,actor_id,version,jobs,last_seen_at,expires_at}],services_truncated:boolean}]}`.
- Read legacy wake times directly, no inferred general host last-seen. Return
  ≤32 services/host via 33-position PK seeks. Truncation cannot imply missing
  coverage. Unknown/inaccessible hosts expose no metadata. Authorize hosts first;
  admit at most 33 service slots, then omit revoked/excluded actors without
  refilling the window. Host `revoked_at` overrides all presence. Partial results
  never prove absence.

This batch avoids adding N+1 calls or more full-roster hydration to host routes.
UI shares a deduplicated store for visible hosts. Extend Agents/Access indicators
with wake-routing, this actor's answer delivery and host-job details. Render
`Live`, `Stale (last seen …)`, `Not reported`; absent opt-in telemetry cannot
prove “No bridge”. Zero-agent hosts still show wake history. Keep agent work
state separate. Age `expires_at` client-side so a dead bridge turns stale without
an event/reload; refetch on existing agents-change, visibility/reconnect and slow
fallback signals, coalesced, with no per-host poll.

Privacy: opt-in shares only version/job names/times with authenticated workspace
readers under host identity visibility. Counts/errors/card refs/repo and session
IDs/paths remain local: even counts reveal private task activity. No public
endpoint, anonymous development bypass, event history or external telemetry.

`host enroll`, `anx help bridge` and the participant skill recommend exact preview
and install commands once. Never install services from enrollment/skill execution
or add default-yes prompts. `orient` keeps the local actor-lock delivery check,
warns about configured degraded features, and stays quiet for healthy bridge-free
workflows. Callbacks must be repeat-safe, independent of heartbeat state.

## 4. Scoped file transport

`anx sync enable <dir> --card-id <ref>` previews an explicit allowlist and frozen
workspace/writer binding outside Git. Default `.anx/` is owner-only and locally
ignored through Git exclude; refuse an already tracked destination. ≤100 cards,
refresh ≤10/tick with continuation; no default “all open projects”.

| File                                  | V1                                                                                                                                                                                                      |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `STATUS.json`                         | `{schema_version:1,as_of,last_server_contact,stale,pending,failed,last_error_code}`; no raw upstream error. Age remains derivable when bridge is dead.                                                  |
| `INDEX.md`, `cards/<canonical-id>.md` | Read-only summary, phase, promise, bounded plan and ten recent notes. `as_of`, `stale_after`, resource version where available. No invented global `seq`: composed reads do not share a snapshot.       |
| `outbox/<uuid>.json`                  | `{schema_version:1,id:<uuid>,card_ref:<allowlisted-ref>,text:<note>,evidence_refs:[]}`, ≤64 KiB. Producer assigns UUID and publishes by temp + rename; bridge never stamps a changing file. Notes only. |
| `outbox/.done`, `.failed`             | Original bytes plus separate receipt/reason after confirmed send or definitive failure. Uncertain sends retain frozen request. Spool intake is not remote confirmation. No automatic source deletion.   |

Key: `anx-file-v1-<directory_uuid>-<entry_uuid>`, actor-scoped event replay.
Edited UUID contents conflict; repair uses a new UUID. Private directories,
no-follow regular-file reads, bounds, lock and fsynced intake journal; reject
symlinks/path traversal/out-of-allowlist targets. Same-UID writers can exercise
the configured agent's note authority: the mounted directory is the security
boundary. No arbitrary API methods, executors, actor selection or shell syntax.

Publish mirrors by atomic generation swap + completion manifest, preserving the
last complete generation. Validate access on refresh; 403/404 removes cached card
and index entry. Identity/workspace change removes generated caches, preserves
unconfirmed writes for explicit export/repair and never reroutes them. A dead
bridge cannot update a `STALE` banner: readers compare times. Cut `events.jsonl`
and mirror frontmatter edits/CAS from v1.

## Implementation plan

Three PRs, each independently useful and opt-in. **After #429 merges**, rebase
and recheck command/help metadata before implementation.

| PR                                          | Files/packages                                                                                                                                                                                                                                                                                                                                                                                                                                                        | Tests and acceptance                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| ------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. Hooks + Git notes                        | `cli/internal/integrations/` protocol/adapters/spool/trailers/cursors; `cli/internal/app/{integrations,host_resolution,loop_tier,resource_messages,command_usage_preflight,output_contract}.go`, dispatch/help/registry and CLI-local contract metadata; directory matcher only if needed; `cli/integration/`, `cli/skills/`, CLI docs                                                                                                                                | Harness input/output goldens, settings preservation, ambiguity/principal isolation, deadlines including blocked stdin/locks, cache denial; malformed/folded trailers, shallow/merge/rebase/revert/worktree histories, backpressure. Disposable Git + real CLI/core: overlapping triggers → one note for same actor; crash before/after server commit, restart, 409/revocation/retry cutoff. Harness smoke must show model-visible start/compact context where supported, not merely emitted JSON.                               |
| 2. Visible bridge services + Git/drain jobs | `contracts/anx-{openapi,schema}.yaml` first and generated clients/mirrors; `core/internal/{auth,server,storage,resourceaccess}/`, next migration and privacy/route/performance inventories; `cli/internal/app/{ask_bridge,host_bridge_commands,bridge_commands,bridge_unit_install,bridge_lifecycle,ask_delivery,loop_tier,setup_help}.go` plus scheduler; web-ui client/shared status store, agent/access components/pages; ask-delivery/runbooks/participant bundle | Proof tamper/replay/revocation, server-clock time, legacy compatibility, actors/instances, populated migration, bounded status reads/privacy. Fake-clock scheduler, unit lifecycle/doctor/log rotation; long executor cannot starve heartbeat/jobs. Real signed beat → status integration. **Affected Playwright `agents.spec.js` / `access.spec.js`: not reported → live → expiry without reload/event → stale → recovery → revoked; zero-agent host; answer-only never implies wake routing; old-core and opt-out fallback.** |
| 3. Outbox + scoped mirror                   | Reuse `cli/internal/integrations/` delivery; new `cli/internal/sync/` intake/mirror generation; app sync commands/host-jobs wiring; CLI integration fixtures, skills/bundle metadata and file-transport/security docs                                                                                                                                                                                                                                                 | Atomic producer/intake crashes, concurrent drain, symlink/replacement/path escape, partial JSON, edited IDs, disk full, identity switch, replay cutoff; crash after commit before receipt; mirror consistency/bounds/staleness/revocation. E2E sandbox writer with only mounted outbox → bridge → one core note → mirror; producer has no network/token. Preserve failed/uncertain bytes.                                                                                                                                       |

Routes: PR1 reuses `POST /events` and existing scoped context/session reads;
PR2 changes `POST /hosts/{host_id}/bridge/check-in` and adds read-only
`POST /hosts/bridge-status`; PR3 reuses card/plan/message reads and `POST /events`.
List every exact changed route in implementation PRs.

Cost: heartbeat uses indexed host/key/actor validation + PK upsert, never full
`GetHost`/roster hydration. Status work is O(requested hosts ×33), bounded indexed
seeks, no history/graph rebuild/per-agent queries. Context/mirror grow with
admitted cards, bounded plans and ten notes/observations. Reuse scoped handles,
classify new storage fields and transport-only exemptions, add populated budgets
and query-plan assertions; run affected scale and migration/startup gates in
[performance.md](../../core/docs/performance.md). Existing host-enrichment
baselines linked to SCA-663 cannot excuse new unbounded work or be widened.
Cut overview fleet counts rather than add a workspace scan.

Rollout: new recipes, capture, reporting, host jobs and files default off.
Explicit enable/install persists each choice and prints disable instructions.
Ship additive core before clients/UI; probe the new status route before sending
new check-in fields, treat 404 as unsupported. All old answer/routing behavior
works with new flags off. Rollback stops new jobs/reporting, removes owned hooks
and units only, retains unsent queues/files and additive schema. Update participant
skill version/catalog/bundle tests with each shipped behavior and its manual path.

Validation: focused unit/integration tests, module checks, `make test-fast`;
contract generation/checks where needed, only affected Playwright specs locally;
full CI/`ci-ok` before merge at exact head, no CI watching/polling. This doc-only
turn runs the selected static/fast gates.

## Recommendations and owner questions

- Adopt explicit card → session binding → worktree/directory default → none;
  60 s / 5 min service presence; notes-only capture; per-agent answers plus one
  optional host-jobs process. No mandatory bridge or default-yes install.
- Cut automatic step/card completion/promises, patch-id deduplication, shell
  commit inference, recursive watchers, runtime unification, duplicate installer/
  setup wizard, remote counters/errors, overview counts, update auto-restarts,
  event-log mirrors, mirror edits, FSKit/FUSE and MCP resources.
- **Owner before PR1:** is same-writer deduplication sufficient? Recommend yes.
  Workspace-wide cross-host deduplication needs a durable canonical receipt keyed
  by repo + commit + card, atomic with the note, plus privacy/retention contracts;
  hashing the existing actor-bound key cannot provide it.
- **Owner before PR3:** approve the explicit directory/card trust boundary and
  notes-only v1. Recommend deferring promises/status until source authority,
  merge evidence, multi-write atomicity and conflict UX are designed. Local
  default-branch reachability alone is insufficient authority.

Success checks are local/opt-in: model-visible start/compact brief, recovery
context tokens, commit→confirmed-note lag, offline drain recovery and rendered
service expiry. No new analytics pipeline or mandatory adoption target.
