# Unified work tracking and conversational PM

Status: accepted implementation direction; implementation and qualification in progress.
This document states the target, not a claim that the features below already work.
The **Rulings (2026-09-08)** section at the end supersedes any earlier line it conflicts with.

## Product decision

Use Agent Nexus as the central workspace, web product, API and agent CLI. Broad
architecture changes are permitted for this pre-adoption product. Reuse CAR v3's
human-decision invariants and UI lessons where useful; do not run two equal task
stores with bidirectional synchronization. CAR is reference material, not a new
runtime dependency by default.

Deliver this architecture and its polished implementation in one pull request.
Merge and deployment require separate approval. No billing, public signup,
control-plane expansion, or fleet rollout is included.

## Ownership and domain

- One authoritative record per commitment. GitHub and Multica retain their
  existing source work; Nexus owns Nexus-native commitments and local annotations.
- Work survives agent runs, sessions, hosts, retries and model changes. Link runs
  and artifacts beneath work rather than treating process completion as delivery.
- Extend existing cards, topics, boards, events, relationships and identity where
  possible. Keep canonical API semantics shared by web, CLI and PM tools.
- Separate source-native status, normalized phase, board presentation and evidence.
  Preserve unfamiliar source states. Title similarity does not establish identity.
- Capture objective, accountable owner, acceptance criteria, priority, next actor,
  next action, blockers, dates and typed dependencies. Timeline rendering is deferred.
- External board moves cannot silently mutate source workflow or create a competing
  status. Source changes use explicit authorized action semantics.

## Observation contract

Observations identify source/target, authenticated submitter, reader revision,
source revision, checked/received times, evidence, uncertainty and coverage.
Last attempted refresh, last successful read, source activity and meaningful
progress are separate facts. Failed reads preserve the last good observation and
show its age. Unreachable does not mean failed work, deletion or completion.

Reported, verified and uncertain describe knowledge, not workflow state.
An agent's report stays attributable; source-confirmed unblocking is not proof of
independent product verification. Use source revisions/cursors where possible;
reject regression from duplicate/out-of-order reports without trusting host clocks.

Readers are read-only upstream. Writing observations into Nexus is not upstream
write authority. Support deterministic GitHub/Multica reads, approved SSH repository
evidence, authenticated remote CLI reports and bounded agent investigations. A Git
commit is not evidence that its deployment is serving traffic successfully.

Refresh policy includes interval, stale-after, manual trigger, timeout, output and
model/tool budgets, backoff/rate-limit handling, and no overlapping target refresh.
Cheap deterministic reads do not need model calls. Reuse existing agent/provider
configuration and explicit selection; no silent provider fallback.

## Product surfaces

- Email-like inbox: material events, grouped routine updates, decisions and receipts.
  Dismiss/read/snooze affects the inbox only, never underlying work completion.
- Board and table: shared queries and filters for source/project/owner/phase/freshness;
  evidence, next action, uncertainty and integration health are visible.
- Conversational PM: primary management experience in web, Telegram and Discord;
  not a switchboard for chatting directly with implementation workers.
- CLI: existing `anx` is a central API client on every participating machine, with
  discoverable help, pagination, structured results and authorized context/reporting.
- Usability: mobile and desktop, keyboard and accessible controls, useful empty,
  loading and error states, no fabricated counts or hidden stale obligations.

The PM queries authorized workspace context instead of putting the entire tracker
in every prompt. Discussion is not authorization. Consequential ambiguity requires
clarification. Shared channel membership never grants another user's authority.

## Decisions, actions and follow-through

Persist intent before effects. Bind authorization to actor, target, scope and
relevant revision. Revalidate source state before acting on old approvals.
Distinguish requested, answered, pending delivery, communicated, received,
acknowledged, applied and outcome-verified states. Silence or timeout is not consent.
A successful comment is not proof that the intended work happened.

Read back external actions and record authoritative receipts. Reconcile uncertain
outcomes before retrying non-idempotent writes. Duplicate channel deliveries must
not duplicate semantic actions. Pending follow-up and accountable ownership survive
restarts. Keep human decisions distinct from general standing capability grants.

## Generated readers and investigators

Generated code is first-class, with no mandatory human code PR for activation
inside an already-approved capability envelope. Required lifecycle:

1. Generate code, manifest and runnable checks in a managed revision workspace.
2. Validate contract and requested capabilities.
3. Run isolated fixture/failure checks.
4. Perform a bounded real-source read-only canary.
5. Atomically activate a versioned revision with rollback evidence.
6. Suspend or roll back after policy violations/repeated failures.

A directory, subprocess, prompt or linter is not a sandbox. Enforce read paths,
scratch writes, network destinations, credential handles and runtime limits.
Generated code cannot edit policy, the runner or other adapters; cannot obtain
workspace-wide agent secrets; cannot install unrestricted dependencies. Deny new
capabilities pending approval. Fail closed on hosts without suitable enforcement,
or use an explicitly approved isolated runner. Test denials on harmless controlled
fixtures, not third-party exploitation targets.

## Implementation ownership

Parallel lanes cover canonical core/contracts; observations/integrations/isolation;
PM/channels; UI; CLI/MCP; independent qualification. One integration branch and PR.
Only canonical contract owners change shared schemas; consumers coordinate through
explicit interfaces. Module guides and generated-contract gates remain binding.
Prefer existing abstractions and native dependencies over new parallel frameworks.

## Acceptance gates

- [ ] Native and externally authoritative work is visible without forced migration.
- [ ] Real GitHub and Multica reads preserve canonical identities and linked evidence.
- [ ] Approved remote SSH evidence and second-machine CLI reporting are exercised.
- [ ] Duplicate/out-of-order reports preserve newer evidence without duplicate actions.
- [ ] Refresh failure retains prior evidence and marks stale/unknown honestly.
- [ ] A bounded deployment investigation represents insufficient evidence as uncertain.
- [ ] Board/table agree; reading/dismissing an inbox event leaves work intact.
- [ ] A contextual PM conversation works in web and dedicated Telegram/Discord tests.
- [ ] Channel identity and cross-workspace authorization deny unauthorized access.
- [ ] Decision delivery failure stays visible; receipt and outcome remain distinct.
- [ ] Restart/replay preserves follow-up without blind repeated mutations.
- [ ] A finished run without accepted artifact does not complete the commitment.
- [ ] JIT generation, isolated tests, canary, activation and rollback are exercised.
- [ ] Runtime denies unapproved paths/destinations/credentials and source mutation.
- [ ] Pagination, rate limits, revoked credentials and partial coverage are explicit.
- [ ] Real browser/mobile/keyboard verification complements unit/integration checks.
- [ ] Independent review, contract generation, component gates, full checks and smoke
      have evidence attached to the exact final revision.

Real-account reads are required; source write canaries are limited to explicitly
authorized dedicated test items. Do not use production/customer work as fixtures.
No tokens, private transcripts or customer data in repository artifacts. Label
synthetic data. Record actual commands, revisions, receipts and limitations;
unavailable access is a qualification blocker, not a passing test. A draft PR or
partial feature set does not satisfy these gates.

## Rulings (2026-09-08)

David's decisions after the first integrated review. These override earlier
sections where they conflict. Nobody uses Agent Nexus yet, so deletions are cheap:
prefer removing a surface over keeping it "for compatibility".

### Three product primitives, nothing else in primary navigation

1. **Inbox**: the only attention surface. Decisions awaiting an answer, blocked
   or stale tasks, and material events, in three mailboxes: Needs you, Watching,
   Handled. The workspace root routes here. The Home unread feed and the separate
   Decisions page are removed; their content becomes Inbox rows.
2. **Tasks**: the projection over heterogeneous trackers and sources of truth.
   This is the former Work surface renamed. Table and board views over the same
   records. The old Boards surface is deleted, but Tasks adopts its simple
   ergonomics: drag a card between phases, keyboard moves, quick open. A drag on a
   Nexus-owned task mutates the task; a drag on a source-owned task opens a PM
   decision to request the change at the source. Never a silent source mutation.
3. **Docs**: the global knowledge base for every agent with access to Agent
   Nexus. Docs are the source of truth for meta knowledge and cross-machine
   knowledge, and normally point at their canonical source when they aggregate.
   Agents on one host use Docs to share what other hosts cannot see. Comments are
   first-class and preserved, so a doc can organically become a discussion room.

Topics, Boards, Events, Artifacts and Trash leave primary and secondary
navigation. Their data and APIs stay until a lane deletes them deliberately;
Events survive as an audit log under settings. Settings holds Access, Secrets,
Integrations and the audit log.

### PM stays an external agent, bridged through the existing harnesses

The PM is not an in-process model call. It must run through the agent harnesses
David already operates (omp, Hermes, Codex, opencode, Claude, Cursor) so it gains
their tools (bash, python, ssh, git) and improves as they do. Simplify the bridge
to one runner: `anx pm serve` claims queued turns, fetches the turn context, runs
the configured harness through `agentctl` with the `anx` CLI as the PM's tools,
and completes the turn. No wake-routing or online-handle prerequisite for this
path. Authorization is enforced by anx-core per requesting principal; the PM
process runs on a trusted host with that host's credentials.

Dogfood order: `omp` with `glm-5.3` on the M4 Air first, then Hermes once stable.

### JIT generated adapters stay, and must run on macOS and Linux

Generated readers are kept. They must execute for real on this macOS host now
and on Linux (Proxmox VMs) later. Linux keeps bubblewrap. macOS gets an equally
enforced Apple Seatbelt profile (`sandbox-exec`): deny by default, read-only
reader artifact, scratch only, no network. Generated code transforms data that
trusted readers fetched; it never holds credentials. A directory or a prompt is
still not a sandbox. Fail closed on any host without an enforced runner.

### Channels

Telegram and Discord ship together; CAR overfit to Telegram last time. Build both
transports to test-readiness now. David supplies dedicated bot credentials last;
until then, end-to-end proof and dogfood run on web and CLI.

### Receipts

The record keeps its eleven machine states (three decision, eight action); the
earlier "fourteen" was stale. The UI shows four: needs you,
delivered, done, failed. Everything else sits behind a disclosure.

### Order of work

1. Web and CLI end to end on this host: a real PM answer to "what needs my
   decision?" that names a seeded task, through `omp`/`glm-5.3`.
2. JIT runner on macOS with a real read-only GitHub canary and negative tests.
3. Inbox, Tasks and Docs as described; legacy surfaces removed.
4. Telegram and Discord to test-readiness.
5. Independent qualification against the rulings, not the original gate list.
