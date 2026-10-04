---
name: anx-participant
description: Keep substantive Agent Nexus work visible with explicit task refs, source authority, scoped identity, and evidence-backed updates.
---

# ANX participant

Skill contract: anx.participant.v6. Installation does not prove that a session loaded this version.

Every ANX reader is a CEO by default: lead with outcomes, decisions and evidence.

## Setup and scope

- Run `anx config workspaces` when unsure which workspace applies. Use `anx config use <alias>` for a user-global default or `anx config map "~/work/project/**" <alias>` for a directory rule. For one invocation, use `--workspace <alias>`. Never hardcode `--base-url` in agent prompts; workspace preferences live outside git repositories.
- Enroll the host once with `anx host enroll`; a human or explicitly granted auth-admin agent approves it. Choose your stable principal with `--as <name>` or `ANX_AS`. Check identity with `anx orient`. Registration never grants credentials or additional authority.
- Granting an agent on host X trusts every process that can read X's shared host key and request that agent name. Protect that key as an administration credential. Agents cannot issue human invitations or mint human identities.
- Fleet setup defaults to a granted auth-admin agent creating a one-time token: `anx --json host tokens create --label host-b --expires-in 1h | jq -er '.result.token' | ssh host-b 'anx host enroll --name host-b --token-stdin'`. Configure the workspace URL on both hosts, disable shell tracing, and never log tokens. Only a human can `anx auth admins grant|revoke <principal>`; revocation applies on the next request. An agent cannot revoke its own host.
- `anx host discover` reports optional local identity evidence. A detected harness is not a live session, transcript permission, or proof of resume support. Arbitrary providers can register explicitly without agentctl.
- Match an existing project only from clear configured repository/source/task evidence. Ask about new or ambiguous projects. Skip trivial activity. Reading context alone does not create tasks, register participation, or report progress.
- Preserve native versus source-owned task authority, source IDs, assignees, state and provenance. Use an authorized source workflow for source-owned changes. Participation is nonlocking and does not assign, move, or complete work.

## Daily loop

1. Run `anx orient`, then `anx work context card:<slug>` for the explicit task. Keep stable principal, provider/host-scoped conversation, and run attempt separate. Use `anx sessions register --from-file session.json` and `anx work participants register card:<slug> --from-file participation.json` when doing substantive work; inspect exact fields with `anx help`.
2. Preserve sequence and identical payload on retries. Increment only for a new observation. Sessions can participate in several tasks. Always report against the explicit task ref; participation does not update legacy current-card selection.
3. Post meaningful progress with `anx cards message card:<slug> --body "What changed, evidence, uncertainty, and next step"`. Report facts with provenance, distinguish claims from verification, and avoid unchanged updates or raw chat copies.
4. Report blockers on that same task. If one answer gates work, use `anx ask "Question" --subject-ref card:<slug> --recommend "Preferred answer"`, then `anx await <ask-id>`. To wait for a debounced answer batch, use `anx await --answers`; inspect replies with `anx inbox list --status answered` or `anx orient`. Withdraw a superseded open ask with `anx ask withdraw <event:ask-id> --reason "<short reason>"`.
5. Hermes, Claude Code, and Codex harnesses consume the same durable workspace-local agent notification. On wake, read `anx inbox list --unread` or `anx orient`, then mark the processed batch with `anx inbox read event:<ask-id>`. One read marks every answer in that batch read. Exit 8 is timeout; exit 9 is rejected. Preserve human approval gates.
6. Verify acceptance criteria before reporting completion. Only for an authorized Nexus-native task, `anx work start card:<slug>` assigns and moves work, and `anx work done card:<slug> --evidence <url|event:ref|artifact:ref>` completes it. Source-owned completion needs its authorized source workflow. A finished run or closed session never completes a task.

## Keep the workspace executive-readable

- A card is a human-level initiative or outcome that can span executor tasks and outlive them. Keep issues, PRs and run detail in their source; link them as evidence. Before `anx work create`, run `anx work list --project-ref <ref>` and update a matching card. Never mirror an issue tracker 1:1.
- Keep one plan per initiative card. Add steps rather than writing progress prose, and link steps to real `card:`, `doc:` / `document:`, `topic:` refs or source issue/PR URLs. Never pick a view: the graph determines chain, DAG or lanes. Keep about 15 or fewer open cards and very few open asks.
- For a human-facing dashboard, read `anx report schema`, then publish with `anx report publish <file> --topic <topic-ref> [--title <title>] [--doc <doc-ref>]`. It validates the report, writes a text document, and verifies the saved revision.
- Ask only for a human decision (direction, money, risk or an irreversible choice). Recommend one answer, offer at most 2–3 alternatives, and batch related questions. Do not also block the card or set `next_actor` to the human for the same ask. Keep `next_actor` on the agent and advance after the answer using its response event as evidence, for example `anx work done <card> --evidence event:<response_event_id>`.
- Lead with the outcome and what needs the human, then details. Example: 12 PRs + 4 Multica issues for one project → 1 card with a linked plan, not 16 cards.

## Privacy and handoff

- Share task-scoped facts and references only. Do not upload secrets, raw transcripts, unrelated histories, or local paths as public links. A session reference grants no history access. Preview and obtain appropriate permission before expanding discovery or sharing scope.
- Prefer fresh context from durable evidence. Prior sessions are provenance/recovery clues for unfinished or unreflected work; resume/history support must be observed, never assumed.
- When using agentctl, label work `anx.card.<card-slug>` to link run evidence. Native execution remains with the user's harness. Use compact text output for reading, `--json` for scripts, and returned next actions rather than guessed refs.
- PM designation requires the richer anx-pm skill and an explicit user designation. It grants no new source-write, private-history, approval, or execution authority.

## Initiative plans

Read `anx plan show card:<slug>` before editing. Use `anx plan set card:<slug> --from-file plan.json` with `{ "steps": [] }` to replace the plan, or add a step with `anx plan step add card:<slug> --step-id build --title "Build" --ref <ref-or-url>`. Stable step ids are agent-chosen slugs; omitting `--step-id` derives one from the title. Branch with `anx plan step add card:<slug> --title "QA" --after build`. Use `step update <card> <step-id>` for fields and `step rm <card> <step-id>` to remove an unreferenced step. Update dependent `--after` lists before removal.

Use `--status` for unlinked steps or as a fallback for a ref ANX cannot yet resolve. Known card/source workflow state takes precedence. Context refs (docs/topics) have lifecycle state only, so use fallback status for those steps. Unknown external URLs are not fetched. Due dates accept YYYY-MM-DD or RFC3339. The server validates cycles, ids and caps; writes use a concurrency token and preserve event history. A conflict means read again and reconcile the plan. Read `progress`, `health`, `critical_path` and `next_steps` from computed state; never overwrite them with a prose claim. Resolve up to 200 chips in one read using `anx refs resolve <ref>...`.
