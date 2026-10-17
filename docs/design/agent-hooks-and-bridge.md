# Agent hooks, Git capture and a first-class bridge

Status: coordinator decisions accepted; single-host bridge supersedes earlier runtime split (2026-10-17).
**PR 1 is merged in #431. PR 2 consolidates the host service in this branch;
PR 3 (files) remains follow-up work. No live service is used for acceptance.** Main proposal: about five minutes; appendix:
implementation details.

## Decisions

- Inject a bounded ANX brief at session start and after compaction where supported.
- Git trailers publish notes **and optional next promises**; never step/status/card completion.
- One explicitly elected capture writer per repo; same-writer deduplication accepted.
- File transport's explicit directory/card trust boundary is accepted.
- **One bridge service/unit per host**, covering all configured workspaces and
  registered agents; answers, wake routing, Git, files and drain are independent jobs.
- Installation is the **recommended next step**, never a prerequisite. Per-machine
  liveness must be obvious, including hosts with no agents. No runtime/API
  compatibility is required; migration must preserve registered work.
- Core remains authoritative; Git transports claims; mirrors are caches.
  Everything is optional and OSS-generic. No transcript ingestion or core-side remote fetching.

## What exists before consolidation

| Area             | Current behavior / implication                                                                                                            |
| ---------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| CLI integrations | One Claude recipe/adapter. Extend parsing/rendering, preserve installs. Spool freezes requests but prunes entries: not a lossless outbox. |
| Go `bridge run`  | Per-agent answers and actor lock; default install writes its unit. Replace these units; crash recovery can repeat executors.              |
| Python bridge    | Host wake router; fold dispatch into Go, then delete package and legacy lifecycle/install paths.                                          |
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

Compose existing scoped work/observations/plan GETs concurrently and use an end-to-end deadline; fail open with a sanitized
diagnostic. Stale cache fallback remains explicit opt-in. Extend
`integrations enable --harness … --dry-run`, preserving backups and owned-hook
removal. Do not add a second hook installer.

## Git notes and promises

Enable previews repo/workspace identity and initial HEAD; no initial history
upload. Explicit `git sync --since` backfills. Store bindings outside Git:

- Elected host runs `anx git enable --writer --workspace <alias>` and exports a
  binding with `anx git binding export`: `{repo_uuid,base_url,writer_host_id,writer_actor_id,writer_agent}`.
- Other hosts import with `anx git enable --binding <file>`. Intake/drain mismatch
  reports `not_capture_writer` with elected host/actor in hook stderr,
  `anx git status` and bridge doctor; no enqueue/send, hook still exits 0.
- Enable requires election/import; distribute the non-secret binding to clones.
  Unprovisioned clones cannot discover peers: this is operator election, not a
  global server lease. Handoff disables/reconciles the old writer before replacement;
  no automatic failover. Linked worktrees share a common-repo writer, FIFO and
  replay namespace with separate worktree scan cursors; register each explicitly.
  Separate clones import that same binding, even on the writer host.

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
checkpoint `{canonical_card_id:{sha,sequence}}`. Backfill compares Git ancestry:
only strict ancestors of that card checkpoint lose promises/evidence; newer
commits and cards without a checkpoint keep them. A retry cannot overtake an uncertain predecessor. Historical backfill
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

## One host bridge: strongly recommended, never required

`anx bridge install` installs **one** host unit, independent of workspace/agent.
`anx bridge add --as <agent> [--workspace <alias>]` registers an enrolled agent,
enabling its answer job; optional wake/runtime, Git, files and spool jobs are
configured independently. Add every intended agent/workspace; adding one never
creates another unit. `run` is the same host supervisor in the foreground and
shares its singleton lock. Start/stop/status/doctor operate this service only.
No per-agent flags on run and no Python runtime install mode remain.

The local registry pins workspace ID/base URL, enrollment/config directory,
agent actor ID, enabled jobs, resume-registry roots and execution policy. It
references existing credentials; never copies keys/tokens. All configured
workspaces share one scheduler with isolated per-workspace/agent queues, bounded
workers and an independent heartbeat timer. Long executors or one offline
workspace cannot block others. One wake dispatcher partitions ask-delivery wakes
from ordinary runtime wakes, so the two jobs cannot claim the same work.

There is one host presence lifecycle. Because workspace APIs/host enrollments are
local to each deployment, its heartbeat timer sends **one signed, workspace-only
projection per configured workspace**, not one request exposing the whole fleet.
Each projection contains host identity/version plus registered-agent/job readiness.
Core stores one host presence row and normalized agent/job rows; see appendix.
An agent is wake-online only when its own wake job is enabled/ready under fresh
host presence. Answers alone cannot imply wake routing, including for unregistered
agents. This rule also replaces the host-wide auth/roster online inference.

Agents/Access show per-host Live / Stale (last seen…) / No bridge badges,
including zero-agent hosts, with agent/job detail. Age expiry without reload;
coalesce visible-host reads. Only workspace-local identity/version/jobs/times
leave the host; queue counts, paths, errors and resume commands stay local.

Setup help, host-enroll output, orient and the participant skill prominently
recommend `anx bridge install`, then the relevant `bridge add` command. Explain
that unattended resumes wait without the answer job; Git/drain become manual
and file delivery unavailable without their jobs. Await/batch/manual reads work.
Replace orient's per-agent lock probe with host-service readiness **for that
workspace/actor/job**, not merely a live daemon. No mandatory or silent install.

Install is also the migration entry point: preview/import old registrations,
quiesce proven old units/processes, activate the sole new unit, verify every
imported namespace and print replacements. Accept arbitrary verified labels and
wrapper-managed children, including the owner's three-workspace example; never
hardcode harness names. No successful install while old bridge supervisors remain.
The migration journal, ownership checks and loss-prevention gates are below.

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

Three opt-in PRs after #429; contracts first for API changes. PR 1 usage and
operational limits are in [the CLI guide](../../cli/docs/agent-hooks.md).

| PR                             | Files/packages                                                                                                                                                                                                                                                                                                                                                                                   | Acceptance                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. Hooks + Git notes/promises  | CLI integrations; app integration/identity/note/message/dispatch/preflight/output/help/registry; local-command metadata/MCP exclusions; CLI integration fixtures, docs and participant bundle.                                                                                                                                                                                                   | Unit/httptest with real Git: adapters, isolation/timeouts, prepare failures/skip/backoff/cutoff, writer fencing, per-card backfill, worktrees, capacity and sentinel config preservation. Real CLI/Git/core: automatic delivery, repeated sync, normalized promises and two-author ordering, frozen replay, generic context and hook removal. Installed-harness model-visible start/compact smoke remains separate.                                                                                                                                  |
| 2. One host service + liveness | Canonical contracts/generated consumers; core auth/server/storage/resourceaccess/commandcenter + migration/inventories; new `cli/internal/bridge/` supervisor/registry/migrator, rewrite bridge commands/units/ask delivery/enrollment/orient; port wake dispatch/run ingestion, remove Python/lifecycle code and update build/version/docs/tests; UI status store/client/Agents/Access; skills. | Multi-agent × multi-workspace isolation/fairness and callback recovery; signed heartbeat, per-agent wake truth, scale/privacy. Migration fixtures for launchd/systemd, custom wrappers/labels, duplicate config roots, in-flight/acknowledged resumes, crash rollback and exactly one supervisor. Real CLI/core E2E after migration: answer each of three workspaces exactly once where outcome is known. Playwright: host live/stale/recovery/revoked, zero-agent and answer-only/unserved agents; prominent setup guidance, bridge-free operation. |
| 3. Files                       | Reuse integrations delivery; new CLI sync intake/mirror package; host-supervisor job wiring, integration fixtures, skill/catalog/bundle and security docs.                                                                                                                                                                                                                                       | Filesystem races/symlinks/disk-full/identity/replay-cutoff tests; atomic mirrors/privacy. Sandbox E2E: mounted outbox → bridge → one note/promise → mirror, with no producer network/token.                                                                                                                                                                                                                                                                                                                                                          |

PR2 replaces host check-in/status contracts, changes agent/host/principal wake
projections and signals, and reuses existing wake/ask/run routes; list exact changed
routes in the PR. PR1/3 reuse events/scoped reads. Add privacy classifications,
indexed page budgets and [performance/migration gates](../../core/docs/performance.md);
never expand SCA-663 baselines to justify new scans.

Install opts into the single presence reporter; add/import explicitly enables jobs.
New Git/hooks/files remain off until enabled. Release matching core/CLI/UI together;
no legacy runtime/API compatibility. Advertise `host_bridge_v1` in the handshake;
preflight every workspace before stopping old services. Rollback preserves unit/config snapshots, registry files and unsent work;
never start both supervisors or automatically repeat uncertain callbacks. Update
participant/catalog/bundle, setup/release docs and module guides. Run focused tests,
module/test-fast/contract checks and affected Playwright, then terminal exact-head
ci-ok (no polling). PRs 1/2 are implemented; PR 3 follows separately. Host operation and migration
are documented in [the host bridge guide](../../cli/docs/host-bridge.md).

Cuts retained: completion/status trailers, patch-ID deduplication, command-text
inference, recursive watchers, duplicate installer, remote counters/errors, fleet
counts, update auto-restart, event-log mirrors/editable frontmatter, FSKit/FUSE/MCP.
Runtime consolidation is approved and belongs in PR2; no owner questions remain.

PR 1 validation boundary: deterministic native payload/merge tests and disposable
Git/CLI/core E2E are automated. Installed-harness model-visible start/compact smoke
remains a separate release acceptance check; see the CLI guide. No live profile,
bridge or production workspace is used by these tests.

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

### Host registry and heartbeat contract (PR2)

- Canonical owner-only local `bridge/registry.json` under the installation's ANX
  root: `{schema_version:1,revision,workspaces:[{workspace_id,base_url,host_id,
config_dir,agents:[{name,actor_id,jobs,registry_dirs,execution_policy}]}]}`.
  Exact runtime argv/cwd and resume policy stay local. Pin resolved identities,
  not mutable default aliases. `bridge remove --as <name> --workspace <alias>` retires a local binding without
  deleting retained commands or receipts, including after revocation.
  Different config roots register into this same
  installation; alternate invocation flags cannot create another service/lock.
- One launchd label `io.agent-nexus.bridge` or systemd user unit
  `anx-bridge.service`, owned by the enrolled OS user. Canonical per-host install
  record/socket/lock applies across workspace/config roots; a second run attaches
  or reports already running. Cross-user ownership conflicts stop install rather
  than quietly creating another host bridge. No root/system-wide authority added.
- Replace `POST /hosts/{host_id}/bridge/check-in` request with
  `{instance_id,sequence,config_revision,version,agents:[{actor_id,jobs:[{kind,enabled,ready}]}]}`;
  kinds `answers,wake,git,files,spool`. Full workspace-local snapshot; omission
  clears old coverage, zero agents valid. `ready` is self-reported job readiness,
  not proof of completed delivery. Disabled/degraded jobs cannot imply wake-online.
- Existing three host-proof headers sign
  `anx-host-bridge-check-in|host_id|signed_at|base64url(SHA256(raw_body))`;
  retain skew/replay/revocation checks, no bearer. Check every actor belongs to
  this active enrollment and is not excluded; only registered jobs are advertised.
  Derive a workspace-scoped instance ID from the local daemon incarnation; never
  export a machine-global correlation identifier or another workspace's config.
- Server times govern presence. Atomically accept a newer sequence for the same
  instance, or a newly signed replacement instance; reject older signed snapshots
  and stale-instance updates. Store `host_bridge_presence(host_id PK,instance_id,
sequence,config_revision,version,signed_at,last_seen_at,expires_at)` and
  `host_bridge_jobs(host_id,actor_id,kind,enabled,ready)`, PK `(host_id,actor_id,kind)`
  with presence FK. Replace that host's bounded job snapshot transactionally.
  Keep retired instance IDs in `host_bridge_retired_instances(host_id,instance_id)`
  (PK) so stale daemons cannot reclaim presence. Advance client sequence on every
  attempt, including a lost response; replacement snapshots need not start at 1.
  Return `{bridge:{host_id,instance_id,sequence,last_seen_at,expires_at}}`.
- Migration 107 creates these tables, removes obsolete `hosts.bridge_*`
  columns/readers/writers and classifies privacy fields; no historical live-state
  backfill. Auth principal enrichment, commandcenter, host and agent responses use
  indexed per-actor wake-job joins instead of shared host expiry. Fresh parent
  presence + enabled/ready wake job + non-revoked host/actor is required everywhere,
  including PM readiness and mention routing. Config/revocation publishes the
  existing agents-change signal; reads evaluate expiry at request time and UI
  ages locally, without a new expiry sweep or progress event.
- Authenticated read-only `POST /hosts/bridge-status` input `{host_ids:[]}`;
  output `{as_of,hosts:[{host_id,revoked_at,bridge:null|{version,last_seen_at,
expires_at},agents:[{actor_id,jobs:[{kind,enabled,ready}]}],agents_truncated}]}`.
  Unknown/inaccessible hosts disclose nothing. One host badge is independent of
  agent count; job detail is workspace-local. Never use these observations as
  authorization. All changed response shapes/consumers update together.

### Install migration and dependency inventory (PR2)

1. `install --dry-run` lists candidate OS units, loaded jobs, owner-verified process
   trees and workspace/config/registry bindings; normal install prints the same
   replacement plan. Inspect effective argv, executable identity, UID, parent/unit
   ownership and process start time, not label prefixes or process names alone.
   A custom shell wrapper may own several actual `anx … bridge run` children;
   inspect those without executing arbitrary scripts or reading secrets/env files.
   Unresolved ownership/argv is a reported blocker; do not kill it or claim success.
2. Build and fsync an owner-only migration manifest and rollback snapshots before
   stopping anything. Import every discovered agent/workspace, timeout/attempt
   policy, cwd/config root and Python runtime argv/callback wiring. Resolve
   workspace aliases once; enumerate configured enrollments too. Unsupported
   settings must be reported for repair, never silently dropped.
3. Preserve existing resume namespaces **in place**:
   `<config-dir>/answer-resumes/SHA256(trimmed_base_url + newline + actor_id)[:16]`
   (hex), source registry files plus cursor. Keep command bytes, cwd, subscription,
   attempts, delivered/acknowledged state and outstanding wake/claim instance IDs.
   Deduplicate identical physical roots; conflicting duplicate asks pause that
   namespace for reconciliation. New ask writes use the same registered namespace;
   preserve concurrent arrivals instead of moving/deleting the directory.
4. Quiesce old launchers and wait for callbacks to settle before handoff. Recheck
   PID identity before signaling and disable restart sources. Already delivered
   commands retry acknowledgement only. Interrupted attempts with unknown effects
   remain visible/paused for reconciliation, never blindly replayed. Persist the
   final cutover snapshot after old writers stop; resume pending wakes with their
   original claims or an explicit host-authorized claim transfer.
5. Stage one disabled unit, atomically publish merged registry, activate it, and
   verify readiness for every imported namespace with the host singleton held.
   Confirm exactly one bridge unit/supervisor and no surviving legacy consumers;
   runtime/resume child commands are allowed, extra bridge daemons are not. Print
   replaced labels/PIDs, imported workspace/agent/registry counts and unresolved
   items, never raw callback commands. Re-running install is idempotent.
6. Journal each phase. Before activation failure restores the saved plan where
   safe; after callbacks may have run, rollback first stops the new consumer and
   reconciles durable outcomes, not simultaneous old/new execution. Retain source
   state and backups. Do not return success on partial coverage or unsafe leftovers.

Owner acceptance fixture (generic discovery, no hardcoded label):
`io.agent-nexus.hermes-answer.anx` owns three
`anx --as hermes --workspace {personal,anx,omi} bridge run --command-timeout 30m`
processes. Import all three as answers-enabled registrations under one unit,
preserve all three resume namespaces and 30m policy, then retire the wrapper and
its children. Test actual argv arrays for each alias, not shell brace expansion;
repeat equivalent launchd/systemd and custom-config-root cases.

Repository audit found no active dependency that requires retaining the Python
bridge. Do not delete capabilities along with the package:

| Dependency                                                                                             | Consolidation action                                                                                                                                                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Python bridge / CLI lifecycle / wake E2E                                                               | Port wake packet reads, claims/outcomes, exact runtime launch, optional agentctl run + runs-ingest callbacks and retry receipts into Go jobs; then remove `adapters/agent-bridge/`, `bridge_lifecycle*`, Python installer/probes and per-agent unit rendering. Replace tests with unified-service integration coverage. |
| Makefile, version sync/managed-files scripts, hook fixtures, release docs, deterministic-bridge script | Remove Python build/version targets; replace the deterministic fixture (its referenced examples are already absent), update inventories/generated help and test selection.                                                                                                                                              |
| `adapters/pm/pm_adapter.py`                                                                            | Independent subprocess protocol wrapper, no import from the removed package: retain its PM limits/isolation contract. README's example module `anx_agent_bridge.adapters.hermes_acp` is absent already; replace stale guidance with generic registered runtime wiring.                                                  |
| Core router/PM/auth + guides                                                                           | Preserve workspace-side wake/PM contracts; fix the stale models.py comment, consume per-agent readiness, update root/module guides, README and both ask-delivery docs. No separate PM bridge daemon required.                                                                                                           |

If implementation reveals an additional dependency that cannot be folded, report
it and leave that component intact pending resolution; do not substitute a second
bridge service silently. PR 1 only records this removal plan; PR 2 implements it.

Event body: `{actor_id,request_key,event:{type,actor_id,thread_ref,refs,summary,payload,provenance}}`.
Reuse the card-message builder's subject/card/thread/board fields, optional
`payload.next:{at,what}` and evidence; no convenience presence PATCH.

| Area        | Detail                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Hook        | Input 64 KiB; context 6 KiB UTF-8 (~1.5k tokens); ten observations; 1.5 s total including stdin/identity/locks/HTTP. Existing 2 s agentctl probe must honor remaining budget. Fail-open exit 0; empty harness output, sanitized stderr; generic empty result.                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| Cache       | Private atomic workspace/host/principal/card key; identical scoped reads share denial invalidation. Stale fallback off; opt-in ≤5 min, labeled STALE. Never after 401/403/404, expired credential or identity change; disclose offline revocation limitation. Participation leases remain 120 s, not renewed by reads.                                                                                                                                                                                                                                                                                                                                                                                                             |
| Trailers    | Git interpret-trailers semantics plus raw-block rejection of folded/multiline ANX values; case-insensitive keys, trim; reject duplicates/control/NUL/invalid UTF-8; no inline comments. One card ≤256 bytes, message ≤64 KiB, note ≤4 KiB. Unknown keys ignored; reserved Step/Status quarantine.                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| Next        | First token UTC YYYY-MM-DD (end of day) or zoned RFC3339; remainder required text. Accept note duration grammar too, resolved once from commit committer time, not sync time. Reuse note's 160-rune text truncation, positive duration ≤3650d and two-day-horizon / moved-without-evidence warnings. Freeze normalized RFC3339 at; note+promise atomic, never a separate patch. Missing Next is not a clear. Preserve core's per-author fold and ordinary evidence-reset rules.                                                                                                                                                                                                                                                    |
| Git walk    | Freeze local HEAD (or explicit --to); ≤100 positions / five seconds per manual tick; durable continuation/outcomes before cursor. A shared repo FIFO (stricter than per-card) includes ordinary notes (evidence can affect promises). Missing/shallow objects pause; overflow explicit backfill; no fetch. Check writer on intake/drain; callbacks/scanners use the same binding.                                                                                                                                                                                                                                                                                                                                                  |
| Spool       | Existing 3 s hook, 100 pending+dead, 1,000 receipts, 30-day pruning is not sufficient for durable intake. Version new payloads, read old queues unchanged; no unsent eviction, reject capacity before sending and retain source. Frozen identity/key/body; four sends/tick, 1 s HTTP, jittered 2–33 s retry. Network/timeout/429/5xx retain; 401/403/404 pause; 400/422/409 inspectable failure. Persist first-attempt/retry metadata before preparation too; unsent prepare failures become inspectable failed/deferred heads and allow explicit --skip. FIFO never automatically bypasses a failed/uncertain predecessor. Stop uncertain auto-retry at 25 days before 30-day receipt GC; reconcile explicitly after loss/expiry. |
| Heartbeat   | One timer; each workspace receives its own signed projection every 60 s, 2 s HTTP, fixed server expiry 300 s. Workspace-local monotonic sequence; fresh proof per retry. Version ≤64 ASCII, instance ≤128; ≤256 registered actors × five unique compatible jobs, body ≤256 KiB; reject oversized configuration before enabling, never truncate a heartbeat snapshot. No arbitrary metadata, public/dev bypass or external telemetry.                                                                                                                                                                                                                                                                                               |
| Status cost | ≤100 hosts; authorize first, admit ≤33 actor groups by `(host_id,actor_id,kind)` index (five rows/group), emit ≤32 with truncation; omit revoked/excluded without refill. Partial results never prove absence. Presence PK seek + bounded job snapshot replace/joins; O(admitted agents/jobs), no workspace/roster/history scan or graph rebuild. Batch scope/revocation checks; add native read/write budgets and query-plan tests.                                                                                                                                                                                                                                                                                               |
| Files       | ≤100 allowlisted cards, ten refreshes/tick, ten notes/card, bounded plans; entry ≤64 KiB, evidence ≤8 with note validation. Next optional, normalized/frozen once at durable intake; preserve file order per directory/card before newer promises. No tracked destination, credential export, symlink/path traversal, arbitrary actor/base URL or shell. Atomic generation+manifest; raw errors/counters stay local.                                                                                                                                                                                                                                                                                                               |

Success checks: model-visible context, recovery tokens, commit→confirmed
note/promise lag, offline recovery and per-machine expiry rendering. No analytics
pipeline or mandatory bridge adoption target.
