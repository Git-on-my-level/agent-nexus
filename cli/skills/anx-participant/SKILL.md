---
name: anx-participant
description: Keep substantive Agent Nexus work visible with explicit task refs, source authority, scoped identity, and evidence-backed updates.
---

# ANX participant

Skill contract: anx.participant.v4. Installation does not prove that a session loaded this version.

Every ANX reader is a CEO by default: lead with outcomes, decisions and evidence.

## Setup and scope

- Run `anx config workspaces` when unsure which workspace applies. Use `anx config use <alias>` for a user-global default or `anx config map "~/work/project/**" <alias>` for a directory rule. For one invocation, use `--workspace <alias>`. Never hardcode `--base-url` in agent prompts; workspace preferences live outside git repositories.
- Enroll the host once with `anx host enroll`; a human approves it. Choose your stable principal with `--as <name>` or `ANX_AS`. Check identity with `anx orient`. Registration never grants credentials or additional authority.
- `anx host discover` reports optional local identity evidence. A detected harness is not a live session, transcript permission, or proof of resume support. Arbitrary providers can register explicitly without agentctl.
- Match an existing project only from clear configured repository/source/task evidence. Ask about new or ambiguous projects. Skip trivial activity. Reading context alone does not create tasks, register participation, or report progress.
- Preserve native versus source-owned task authority, source IDs, assignees, state and provenance. Use an authorized source workflow for source-owned changes. Participation is nonlocking and does not assign, move, or complete work.

## Daily loop

1. Run `anx orient`, then `anx work context card:<slug>` for the explicit task. Keep stable principal, provider/host-scoped conversation, and run attempt separate. Use `anx sessions register --from-file session.json` and `anx work participants register card:<slug> --from-file participation.json` when doing substantive work; inspect exact fields with `anx help`.
2. Preserve sequence and identical payload on retries. Increment only for a new observation. Sessions can participate in several tasks. Always report against the explicit task ref; participation does not update legacy current-card selection.
3. Post meaningful progress with `anx cards message card:<slug> --body "What changed, evidence, uncertainty, and next step"`. Report facts with provenance, distinguish claims from verification, and avoid unchanged updates or raw chat copies.
4. Report blockers on that same task. If an answer gates work, use `anx ask "Question" --subject-ref card:<slug> --recommend "Preferred answer"`, then `anx await <ask-id>`. Exit 8 is timeout; 9 is rejected. Preserve human approval gates.
5. Verify acceptance criteria before reporting completion. Only for an authorized Nexus-native task, `anx work start card:<slug>` assigns and moves work, and `anx work done card:<slug> --evidence <url|event:ref|artifact:ref>` completes it. Source-owned completion needs its authorized source workflow. A finished run or closed session never completes a task.

## Keep the workspace executive-readable

- A card is a human-level initiative or outcome that can span executor tasks and outlive them. Keep issues, PRs and run detail in their source; link them as evidence. Before `anx work create`, run `anx work list --project-ref <ref>` and update a matching card. Never mirror an issue tracker 1:1.
- Put status in a short plain-language summary, checklist or `anx.visual-report` dashboard/chart. Keep about 15 or fewer open cards per workspace and very few open asks; consolidate as you approach that budget.
- For a human-facing dashboard, read `anx report schema`, then publish with `anx report publish <file> --topic <topic-ref> [--title <title>] [--doc <doc-ref>]`. It validates the report, writes a text document, and verifies the saved revision.
- Ask only for a human decision (direction, money, risk or an irreversible choice). Recommend one answer, offer at most 2–3 alternatives, and batch related questions. Do not also block the card or set `next_actor` to the human for the same ask. Keep `next_actor` on the agent and advance after the answer using its response event as evidence, for example `anx work done <card> --evidence event:<response_event_id>`.
- Lead with the outcome and what needs the human, then details. Example: 12 PRs + 4 Multica issues for one project → 1 card with a checklist and links, not 16 cards.

## Privacy and handoff

- Share task-scoped facts and references only. Do not upload secrets, raw transcripts, unrelated histories, or local paths as public links. A session reference grants no history access. Preview and obtain appropriate permission before expanding discovery or sharing scope.
- Prefer fresh context from durable evidence. Prior sessions are provenance/recovery clues for unfinished or unreflected work; resume/history support must be observed, never assumed.
- When using agentctl, label work `anx.card.<card-slug>` to link run evidence. Native execution remains with the user's harness. Use compact text output for reading, `--json` for scripts, and returned next actions rather than guessed refs.
- PM designation requires the richer anx-pm skill and an explicit user designation. It grants no new source-write, private-history, approval, or execution authority.
