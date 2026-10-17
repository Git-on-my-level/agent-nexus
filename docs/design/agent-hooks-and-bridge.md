# Agent hooks, Git capture and a first-class bridge

Status: coordinator decisions accepted (2026-10-17). Owner: coordinator.
**Documentation only: implementation waits for [PR #429](https://github.com/Git-on-my-level/agent-nexus/pull/429)
to merge, then rebases on main.** Main proposal: about five minutes; appendix:
implementation details.

## Decisions

- Inject a bounded ANX brief at session start and after compaction where supported.
- Git trailers publish notes **and optional next promises**; never step/status/card completion.
- One explicitly elected capture writer per repo; same-writer deduplication accepted.
- File transport's explicit directory/card trust boundary is accepted.
- Bridge installation is the **recommended next step**, never a prerequisite.
  Per-machine liveness must be obvious, including hosts with no agents.
- Core remains authoritative; Git transports claims; mirrors are caches.
  Everything is optional and OSS-generic. No transcript ingestion or core-side remote fetching.

## What exists

| Area             | Current behavior / implication                                                                                                            |
| ---------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| CLI integrations | One Claude recipe/adapter. Extend parsing/rendering, preserve installs. Spool freezes requests but prunes entries: not a lossless outbox. |
| Go `bridge run`  | Per-agent answers and actor lock; default install writes its unit. Crash recovery can repeat executors.                                   |
| Python bridge    | Host wake router, full-roster validation; legacy install flags plus `start`, `stop`, `status`, `doctor --config`.                         |
| Core hosts       | Existing bridge instance/check-in/expiry columns drive `bridge_online`; no general host last-seen.                                        |
| Setup/UI         | `anx help <job>`; discovery means availability, not hook support. Host badges infer child-agent status and miss zero-agent hosts.         |

Preserve [ask delivery](../ask-delivery.md): bridge presence supplies no resume
command or host-wide wake capability. Await/batch/none remain valid.

## Hooks

Extend integrations with `context.inject`, `git.capture` and
`anx hook <event> [--harness <name>]`. Generic events: `session.start`,
`context.compacted`, `tool.after`, `git.commit` (from Git, never shell text).
Generic input: `{schema_version:1,cwd,session_id?,card_ref?,tool?:{name}}`;
absolute cwd required. Output: `{schema_version:1,context,card_ref,as_of,stale}`
(nullable ref/time). Harness output follows its protocol; other CLI output stays v2.

Claude/Codex inject on SessionStart (including compact); Cursor supports
asynchronous sessionStart, with no post-compaction guarantee. Git post-commit
preserves existing hooks. Tool-after only hints a scan. Adapter settings/payloads
are specified in the appendix; never read transcripts, prompts or tool output.
Unsupported versions retain manual/generic paths.

Card resolution: explicit `--card-id`, then payload ref → explicit local session
binding (`integrations bind --card-id`, workspace/principal/provider/session hash)
→ worktree default / longest directory card rule → none. Workspace rules select
only workspaces. No reverse session→cards API exists: validate known candidates
with scoped reads. No card enumeration, inaccessible-target fallback or implicit
participation/presence writes.

Use existing context reads and an end-to-end deadline; fail open with a sanitized
diagnostic. Stale cache fallback remains explicit opt-in. Extend
`integrations enable --harness … --dry-run`, preserving backups and owned-hook
removal. Do not add a second hook installer.

## Git notes and promises

Enable previews repo/workspace identity and initial HEAD; no initial history
upload. Explicit `git sync --since` backfills. Store bindings outside Git:

- Elected host runs `anx git enable --writer --workspace <alias>` and exports a
  binding with `anx git binding export`: `{repo_uuid,workspace_id,writer_host_id,writer_actor_id}`.
- Other hosts import with `anx git enable --binding <file>`. Intake/drain mismatch
  reports `not_capture_writer` with elected host/actor in hook stderr,
  `anx git status` and bridge doctor; no enqueue/send, hook still exits 0.
- Enable requires election/import; distribute the non-secret binding to clones.
  Unprovisioned clones cannot discover peers: this is operator election, not a
  global server lease. Handoff disables/reconciles the old writer before replacement;
  no automatic failover.

```text
Anx-Card: card:hook-work
Anx-Note: Added bounded context injection
Anx-Next: 2026-10-20 Validate bridge liveness
```

Parse only Git's trailer block; keys case-insensitive, one card, optional note
(default subject), optional Next. Next is a date/time token followed by required
promise text. Reuse `anx note --next` validation and warnings. Put
`payload.next:{at,what}` in the **same** replay-safe message event as the note;
normalize/freeze time once, never recalculate on retry. Missing Next leaves it
unset. Reserve Anx-Step/Anx-Status with an explicit unsupported-trailer failure.
A malformed trailer quarantines the whole capture rather than partially posting.

Serialize commits oldest-first per repo/card; persist order and confirmed promise
checkpoint. A retry cannot overtake an uncertain predecessor. Historical backfill
or rewrite rescans below that checkpoint retain notes but omit Next and structured
evidence **before** freezing (permalink stays text), so they cannot reset a newer
promise. Diagnose this; divergent history requires explicit reconciliation.
Latest accepted commit supersedes only this writer's promise. Keep the existing
per-author fold and evidence-reset rules; no cross-author last-write-wins or completion.

Use the existing card-message event with optional `payload.next`; note+promise
commit atomically, with no presence PATCH. Evidence is a configured credential-free
permalink or plain SHA text, never `commit:<sha>`, diffs or paths.
Key = `anx-git-v1-` + SHA256(repo UUID NUL object ID NUL canonical card ID).
Core replay is actor-scoped; cross-host deduplication is not promised. Rewrites
make new objects; reverts append information. Persist frozen bodies/identity before
sending; never prune unsent durable work. Keep visible backpressure and failed
requests; stop uncertain retries before server receipt expiry (appendix).

## Bridge: strongly recommended, never required

Setup help, `host enroll` output, `orient` and the participant skill prominently
recommend **`anx bridge install`**, with resolved agent/workspace, dry-run preview
and heartbeat-enabled option. Explain degradation: unattended resume callbacks
wait; Git sync/drain are manual and file delivery unavailable without enabled jobs.
Await/batch/manual reads still work. No silent install or blocking dependency.
Orient keeps its actor-lock check and recommends installation when absent.

Add an independent host-jobs mode/unit of `bridge run` (`--jobs git,spool`, later
files, explicit writer), locked per workspace/host. Existing per-agent answer
units stay valid. Jobs/heartbeat must not starve during long executors. Extend
no-config Go status/doctor; preserve Python --config behavior; add log rotation.

### Presence contract

Extend signed `POST /hosts/{host_id}/bridge/check-in` with optional
`service:{kind,actor_id,version,jobs}`, kind answer or host-jobs. Keep legacy
`{bridge_instance_id,checked_in_at,expires_at}`, proof headers and wake behavior.
New mode uses server receipt time plus five-minute expiry, never updates legacy
`hosts.bridge_*`/agent `bridge_online`, and never treats token grants as presence.
Verify active signing host/key and its non-excluded actor transactionally.

Next migration: `host_bridge_services(host_id,kind,actor_id,instance_id,version,
jobs_json,signed_at,last_seen_at,expires_at)`, PK `(host_id,kind,actor_id)`, host FK.
Upsert one slot; reject older signed observations. No history/backfill. Response
retains `{bridge:{host_id,bridge_instance_id,checked_in_at,expires_at}}`, adding
`bridge.service:{kind,actor_id,version,jobs,last_seen_at}` only for new mode.

Authenticated read-only `POST /hosts/bridge-status` accepts `{host_ids:[]}` and
returns `{as_of,hosts:[{host_id,revoked_at,wake,services,services_truncated}]}`;
wake is null or `{last_seen_at,expires_at}`, services contain
`{kind,actor_id,version,jobs,last_seen_at,expires_at}`. Read legacy wake times
directly; no fabricated general host last-seen. Unknown/inaccessible hosts disclose
nothing; revocation overrides presence.

Agents/Access show **per-host badges**, including zero-agent hosts: Live,
Stale (last seen…), Not reported, with separate wake/answer/jobs. Missing telemetry
cannot prove absence. Age expiry without reload; coalesce visible-host reads on
roster/focus/reconnect. Authenticated readers see only version/jobs/times;
counts/errors/paths/cards/sessions stay local.

## File transport

`anx sync enable <dir> --card-id <ref>` previews the accepted explicit allowlist,
workspace and writer binding. Owner-only `.anx/` is locally Git-excluded, never
credential-bearing. Directory writers have that agent's note/promise authority,
never arbitrary API or executor access.

| Files                                  | Contract                                                                                                                                           |
| -------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `STATUS.json`                          | `{schema_version,as_of,last_server_contact,stale,pending,failed,last_error_code}`                                                                  |
| `INDEX.md` / `cards/<canonical-id>.md` | Atomic completed generation; summary, promise, plan, recent notes; as_of/stale_after/resource version, no fictitious global seq.                   |
| `outbox/<uuid>.json`                   | `{schema_version:1,id,card_ref,text,evidence_refs:[],next?:{at,what}}`; producer UUID + temp/rename. Next uses the same note validation.           |
| `outbox/.done` / `.failed`             | Original bytes plus receipt/reason only after confirmation/definitive failure. Uncertain work remains pending, never rerouted or silently deleted. |

Key: `anx-file-v1-<directory_uuid>-<entry_uuid>`. Edited IDs conflict. Lock/fsync
intake, reject symlinks/path escape, preserve failed bytes. Refresh validates access;
403/404 removes cached card/index data. Identity changes remove generated caches
but preserve unconfirmed writes for explicit export/repair. Readers compare times:
a dead bridge cannot rewrite stale banners.

## Implementation plan

Three opt-in PRs, all after #429; contracts first for API changes.

| PR                                | Files/packages                                                                                                                                                                                                                                                | Acceptance                                                                                                                                                                                                                                                                                                                                         |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. Hooks + Git notes/promises     | CLI integrations; app integration/identity/note/message/dispatch/preflight/output/help/registry; local-command metadata; CLI integration fixtures, docs and participant bundle.                                                                               | Harness goldens and model-visible start/compact smoke; settings preservation, card isolation/timeouts. Real Git/CLI/core: writer mismatch/handoff, trailers/time normalization, ordered promises with two authors, uncertain retry, backfill/rewrite, crash/restart and backpressure.                                                              |
| 2. Bridge jobs + visible liveness | Canonical OpenAPI/schema and generated consumers; core auth/server/storage/resourceaccess + migration/inventories; app bridge/ask-delivery/host-enroll/orient/setup/unit lifecycle; UI status store, client, Agents/Access components/pages; skills/runbooks. | Proof/revocation/server-clock/back-compat/migration/scale tests; long executor cannot starve jobs. Playwright agents/access: not reported → live → expiry without reload → stale → recovery → revoked; zero-agent host, answer-only versus wake routing. Check prominent recommendations in all four CLI/skill surfaces and bridge-free operation. |
| 3. Files                          | Reuse integrations delivery; new CLI sync intake/mirror package; app commands/job wiring, integration fixtures, skill/catalog/bundle and security docs.                                                                                                       | Filesystem races/symlinks/disk-full/identity/replay-cutoff tests; atomic mirrors/privacy. Sandbox E2E: mounted outbox → bridge → one note/promise → mirror, with no producer network/token.                                                                                                                                                        |

PR2 extends check-in/adds read-only bridge-status; PR1/3 reuse events/scoped
reads. List changed routes; add privacy classifications, indexed page budgets and
[performance/migration gates](../../core/docs/performance.md). Do not expand
SCA-663 host baselines or add overview counts.

New features default off; setup recommendations default prominent. Probe status
support before new heartbeat fields; old core falls back. Rollback retains unsent
work/schema, removes owned hooks/units only. Update participant/catalog/bundle;
run focused tests, module/test-fast/contract checks and affected Playwright, then
terminal exact-head ci-ok (no polling). No implementation now.

Cuts: step/status/card completion, patch-ID deduplication, command-text inference,
recursive watchers, runtime unification, duplicate installer/setup wizard, remote
counters/errors, fleet counts, update auto-restart, event-log mirrors/editable
frontmatter, FSKit/FUSE/MCP. **Coordinator questions resolved; none remain.**

## Appendix: adapters, bounds and failures

| Adapter     | Input/settings                                                                                                                                                                 | Context output                                                                                                                  |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------- |
| Claude Code | Existing settings scopes; `session_id,cwd,hook_event_name,source`. `SessionStart` startup/resume/clear; source compact maps to compacted.                                      | `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"…"}}`                                               |
| Codex       | User/trusted-project `.codex/hooks.json`; same selected fields and SessionStart mapping. Preserve inline TOML; require harness trust review.                                   | Same wrapper as Claude; no injection through PreCompact/PostCompact.                                                            |
| Cursor      | User/project `.cursor/hooks.json`, `{version:1,hooks:{…}}`; select session_id (fallback conversation_id), workspace_roots, hook_event_name. Ambiguous roots require selection. | `sessionStart` returns `{"additional_context":"…"}` asynchronously. No promised post-compaction injection.                      |
| Git         | Effective post-commit hook path; repo from Git, no JSON stdin.                                                                                                                 | No stdout; schedule capture without rejecting commits. Preserve existing hooks; offer chaining, never replace `core.hooksPath`. |

Tool-after hooks only hint a bounded scan. Never consume tool output, transcripts,
prompts or compaction summaries. Pin supported versions/fixtures against the
[Claude](https://code.claude.com/docs/en/hooks),
[Codex](https://developers.openai.com/codex/hooks) and
[Cursor](https://cursor.com/docs/hooks) contracts checked 2026-10-17.
Unsupported versions retain manual `anx context` / generic hooks.

Host proof uses `X-ANX-Host-Key-Id`, `X-ANX-Host-Signed-At`,
`X-ANX-Host-Signature` over
`anx-host-bridge-check-in|host_id|signed_at|base64url(SHA256(raw_body))`.
Keep existing skew/replay/revoked-key checks; legacy responses remain unchanged.

Event body: `{actor_id,request_key,event:{type,actor_id,thread_ref,refs,summary,payload,provenance}}`.
Reuse the card-message builder's subject/card/thread/board fields, optional
`payload.next:{at,what}` and evidence; no convenience presence PATCH.

| Area        | Detail                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| ----------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Hook        | Input 64 KiB; context 6 KiB UTF-8 (~1.5k tokens); ten observations; 1.5 s total including stdin/identity/locks/HTTP. Existing 2 s agentctl probe must honor remaining budget. Fail-open exit 0; empty harness output, sanitized stderr; generic empty result.                                                                                                                                                                                                                                                                                                                       |
| Cache       | Private atomic workspace/principal/card/scope key. Stale fallback off; opt-in ≤5 min, labeled STALE. Never after 401/403/404, expired credential or identity change; disclose offline revocation limitation. Participation leases remain 120 s, not renewed by reads.                                                                                                                                                                                                                                                                                                               |
| Trailers    | Git interpret-trailers semantics plus raw-block rejection of folded/multiline ANX values; case-insensitive keys, trim; reject duplicates/control/NUL/invalid UTF-8; no inline comments. One card ≤256 bytes, message ≤64 KiB, note ≤4 KiB. Unknown keys ignored; reserved Step/Status quarantine.                                                                                                                                                                                                                                                                                   |
| Next        | First token UTC YYYY-MM-DD (end of day) or zoned RFC3339; remainder required text. Accept note duration grammar too, resolved once from commit committer time, not sync time. Reuse note's 160-rune text truncation, positive duration ≤3650d and two-day-horizon / moved-without-evidence warnings. Freeze normalized RFC3339 at; note+promise atomic, never a separate patch. Missing Next is not a clear. Preserve core's per-author fold and ordinary evidence-reset rules.                                                                                                     |
| Git walk    | Snapshot selected local refs; ≤100 commits / five seconds per tick; durable continuation/outcomes before cursor. Ordered per-card lane includes ordinary notes (evidence can affect promises). Missing/shallow objects pause; overflow explicit backfill; no fetch. Check writer on intake/drain; callbacks/scanners use the same binding.                                                                                                                                                                                                                                          |
| Spool       | Existing 3 s hook, 100 pending+dead, 1,000 receipts, 30-day pruning is not sufficient for durable intake. Version new payloads, read old queues unchanged; no unsent eviction, reject capacity before sending and retain source. Frozen identity/key/body; four sends/tick, 1 s HTTP, jittered 1–60 s retry. Network/timeout/429/5xx retain; 401/403/404 pause; 400/422/409 inspectable failure. FIFO promise lane does not rotate failed predecessors behind newer commits. Stop uncertain auto-retry at 25 days before 30-day receipt GC; reconcile explicitly after loss/expiry. |
| Heartbeat   | 60 s jittered independent cadence, 2 s HTTP, fresh proof per retry at most cadence-rate. Required wire duration positive ≤300 s; server fixes new expiry to 300 s. Version ≤64 ASCII, instance ≤128; unique compatible jobs ≤4: answers/git/spool/files. Existing host proof signs exact body; no bearer, arbitrary metadata, public/dev bypass or external telemetry.                                                                                                                                                                                                              |
| Status cost | ≤100 unique hosts; authorize first, PK-seek ≤33 service candidates/host, emit ≤32 with truncation; omit revoked/excluded actors without refill. Partial results never prove absence. Indexed heartbeat validation/upsert; no full GetHost/roster, history scan, N+1 calls or ownership-graph rebuild.                                                                                                                                                                                                                                                                               |
| Files       | ≤100 allowlisted cards, ten refreshes/tick, ten notes/card, bounded plans; entry ≤64 KiB, evidence ≤8 with note validation. Next optional, normalized/frozen once at durable intake; preserve file order per directory/card before newer promises. No tracked destination, credential export, symlink/path traversal, arbitrary actor/base URL or shell. Atomic generation+manifest; raw errors/counters stay local.                                                                                                                                                                |

Success checks: model-visible context, recovery tokens, commit→confirmed
note/promise lag, offline recovery and per-machine expiry rendering. No analytics
pipeline or mandatory bridge adoption target.
