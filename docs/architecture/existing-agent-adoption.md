# Existing-agent adoption and coordination

Status: accepted product direction, with phased implementation and qualification.
Date: 2026-10-02.

This is a target and acceptance plan, not a claim that every capability already
ships. The decisions below supersede conflicting onboarding/runtime assumptions
in [the earlier unified-work plan](unified-work-pm-plan.md), especially its
dedicated PM runner and mandatory accountable-owner assumptions. The existing
evidence, privacy, human-decision, and source-authority invariants still apply.

## Accepted product decisions

1. **Serve advanced agent users first.** Start with people who already run agents
   across multiple harnesses, hosts, repositories, and task systems. Adopt those
   agents rather than requiring a replacement runtime.
2. **Offer hosted coordination and self-hosting.** Nexus owns the shared project
   index, evidence, task relationships, decisions, and useful summaries. Users
   own agent execution and its costs in both deployment modes. Nexus does not
   host agent execution infrastructure, including a special hosted PM runtime.
3. **Designate an existing ordinary agent as PM.** Connect its existing reachable
   endpoint/session/channel when supported. Keep it usable outside Nexus. The PM
   receives a richer coordination skill, not new approval or mutation authority.
   The current `anx pm serve` launch-per-turn adapter is optional compatibility
   machinery; it is not proof of existing-agent attachment.
4. **Make authority explicit.** A project may use native Nexus commitments or
   externally authoritative work projected into Nexus. Both coexist. Preserve
   source IDs, assignees, original state, coverage, last successful read, and
   provenance. Do not duplicate a source task based on its title or silently
   overwrite source-owned fields with local annotations.
5. **Use project knowledge to reduce repeated management.** Give people concise,
   rich PM-like overviews: objective, accepted decisions, meaningful progress,
   blockers, uncertainty, and next steps, with evidence links and freshness.
   Summarize and reconcile; do not reproduce raw chats or unqualified agent claims.
6. **Keep participation separate from ownership.** Agents join or report work
   without locking, assigning, moving, or completing a task. A project need not
   have one owner. Preserve source assignees; resolve unclear responsibility from
   evidence, then ask the PM, then the user if uncertainty remains consequential.
7. **Support generic agents on day one.** Explicit registration accepts arbitrary
   providers. Ship well-tested defaults, but do not gate registration on a fixed
   harness list. Unsupported detection/resume/history capabilities stay visible.
8. **Keep three identities.** A stable authenticated Nexus agent principal is
   distinct from its native provider-scoped conversation and from each run/attempt.
   Multiple sessions of one principal and multiple tasks per session are valid.
9. **Prefer fresh context.** Prior sessions provide provenance and recovery clues
   for unfinished or unreflected work. Reusing a conversation is an explicit
   provider capability and choice, not the default dependency for continuity.
10. **Discover locally and share minimally.** Inspect consented host, repository,
    task, and session evidence locally first. Upload only scoped useful summaries
    and references. Do not scrape unrelated histories or treat a session link as
    permission to view or share its transcript.
11. **Teach every participant, with deeper PM guidance.** Maintain versioned,
    inspectable skills and verified harness configuration. Report meaningful
    updates. Automatically associate clear configured project evidence; ask for
    a new or ambiguous project; skip trivial activity. Do not turn read access
    into hidden registration, claiming, or task-state writes.
12. **Reuse the user's source tools.** Prefer existing MCP/CLI integrations and
    skill descriptors over a Nexus-maintained connector fleet. A source guide is
    a proposal until an actual read verifies identity, scope, coverage, freshness,
    permissions, and failure behavior.

## Ownership boundaries

| Layer | Owns | Must not imply |
| --- | --- | --- |
| Nexus core | Workspace principals, projects, source authority, task/session participation, evidence, summaries, decisions and receipts | Agent hosting, runtime permissions, source-write grants |
| Nexus hosted layer | Isolated workspace lifecycle, proxy/auth transport and account controls | A second tracker database or operator-managed PM execution |
| `agentctl` (optional) | Harness discovery, local identity/capability evidence, run IDs, supported native-session correlation and runtime/skill delivery | Nexus project authority or compulsory dependency |
| Native harness / other provider | Conversation identity, history access, execution, its permissions and costs | Universal resume/history access or cross-host reachability |
| Designated PM | Context reconciliation, useful overviews, proposals, authorized coordination | Human approval authority or a global private-history exception |

Providers describe evidence with a versioned contract. An execution identifier,
an agent name, and a native session identifier must never be substituted for one
another. Host-scoped references must not present local filesystem paths as public
links. Provider metadata is untrusted input, not an authentication assertion.

## Verified baseline and known gaps

The initial implementation inspection used OSS main `369dec2` and SaaS main
`191788b`; subsequent PRs must name their exact verified revisions.

- Existing work storage supports native/source authority, source-identity
  deduplication, annotations, and field guards. Reuse these primitives.
- Existing run storage accepts repeat observations, preserves terminal-state
  protections, and does not equate completed runs with completed tasks.
- Presence is currently keyed by principal and loses concurrent-session detail.
  The additive session/participation foundation reports its own bounded activity;
  the legacy agent roster does not yet project those new records.
- `anx work start` currently assigns and moves a task. It cannot be reused as a
  harmless participation operation, especially for projected source-owned work.
- Native harness identity detection and `agentctl doctor` discovery overlap;
  discovery currently filters through a short hardcoded harness list.
- A skill can be installed explicitly, but installation alone does not prove
  that an existing session loaded it, and update maintenance is not automatic.
- Existing PM queues and decision fences are useful foundations. The current
  launcher is not yet an existing-agent endpoint contract, and selected-PM
  private-thread access needs precise participant-scoped review.
- Hosted bearer proxy parity must be verified by exact method and route;
  `/work` or `/pm` prefix-wide authorization is not an acceptable fix.

## Implementation slices and acceptance gates

Slices are independently reviewable. Passing one is not approval to release all
of them or evidence that the later product experience is complete.

### 1. Transport compatibility and generic identity/participation

- [ ] Hosted bearer transport accepts only the intended existing workspace
      routes and methods, with negative tests for human approval, admin, source
      dispatch, ingress, unknown routes, and no-wake bearer behavior.
- [ ] An already authenticated arbitrary agent registers a provider/host-scoped
      session without new credentials or a supported-harness requirement.
- [ ] Retry-safe task participation preserves the card's phase, assignees,
      authority, rank, and completion. Duplicate/out-of-order observations do
      not overwrite newer state. A session can participate in several tasks.
- [ ] Two simultaneous sessions of one principal remain independently visible;
      stale activity expires while historical participation remains.
- [ ] Closing a session or finishing a run leaves the task unchanged. Reading
      context alone makes no hidden workflow writes.
- [ ] Public APIs and CLI expose the generic path, with deterministic JSON/text
      output, input validation, exact command help, and denial tests.
- [ ] Optional `agentctl` identity evidence is version-checked and capability
      gaps are explicit. Missing, old, malformed, or unavailable providers do
      not block explicit registration or override an explicit Nexus identity.

### 2. Existing-PM connection and maintained participant skills

- [ ] Select an existing ordinary participant as PM and bind a supported existing
      communication endpoint with explicit privacy and recipient scope.
- [ ] Exercise incoming request, answer, interruption, retry, disconnection and
      reconnect. Do not launch a replacement agent merely because it is offline.
- [ ] PM operations preserve requester-scoped context and human authorization;
      selecting the PM does not expose unrelated private conversations.
- [ ] Ship lean participant and richer PM skills. Installation/configuration
      reports version and evidence of activation separately; refresh owned skill
      content safely while preserving user edits and unsupported harness status.
- [ ] Verify Codex, Claude and Cursor defaults with real supported harnesses.
      Treat OMP, Multica and other providers according to observed capabilities.

### 3. Discovery, project association, and useful overviews

- [ ] Preview local discovery scope before sharing; handle consent, excluded
      paths, unknown projects, ambiguity and minimal-upload behavior.
- [ ] Associate clear configured repository/source/session evidence automatically;
      ask once when ambiguity matters and skip trivial activity.
- [ ] Produce project and task summaries with attributable facts, decisions,
      blockers, next actors/actions, provenance, coverage and freshness.
- [ ] Reuse existing source integrations and verify a read before marking one
      connected. Failed reads preserve last good evidence and show uncertainty.
- [ ] Demonstrate native and projected projects together without a mandatory
      owner, source-assignee replacement, or duplicate external commitments.

### 4. Dogfood, review, release, and deployment

- [ ] Run component checks, contract regeneration/drift checks, full repository
      gates, realistic local smoke tests and independent review on final commits.
- [ ] Use dedicated synthetic fixtures first, then authorized real source reads;
      do not contact external agents or mutate production work as a test.
- [ ] Record exact artifact versions, compatibility order, migration/rollback
      evidence, limitations and user-visible release notes.
- [ ] Publish draft PRs for review. Merge/tag/GitHub release happens only when
      separately authorized and gates pass; cloud deployment remains the user's
      action. No deployment can be inferred from a published artifact.
- [ ] After the user deploys, verify the running version and hosted read-only
      behavior before claiming rollout success. Keep a failed verification visible.

## Implementation recommendations, not extra product commitments

- Use small additive API/storage changes with server-received timestamps and
  caller-scoped idempotency/version checks before adding broad UI machinery.
- Keep the first generic registration explicit; layer automatic association only
  after clear-project evidence and privacy behavior have dedicated tests.
- Keep PM attachment independent of any specific native harness. Prefer a
  transport descriptor and observed capability matrix over promising arbitrary
  bidirectional contact with every installed application.
- Treat historical sessions as pointers and summarized evidence rather than
  building a central transcript database.
- If a gate needs new persistent access, an external contact, source mutation,
  or live production configuration changes, stop that gate for explicit approval
  while continuing independently testable work.
