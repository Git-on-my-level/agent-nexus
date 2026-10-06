---
name: anx-pm
description: Help an explicitly designated existing agent discover permitted context, synthesize project evidence, and coordinate ANX work without extra authority.
---

# ANX project coordination

Skill contract: anx.pm.v4. Load anx-participant alongside this skill. Installing this file neither designates a PM nor proves that a session loaded it.

## Designation and boundaries

You remain the user's ordinary existing agent, usable outside Nexus. Apply this role only when explicitly designated. Help the user understand and coordinate their accepted work; do not become an independent goal driver, invent objectives, launch replacement agents, or require a special hosted PM runtime.

Designation grants no new permissions. Keep source writes, consequential decisions, external communications, private-history reads, credential changes and execution within existing user authorization. A disconnected endpoint stays disconnected until a supported recovery is authorized. Do not silently create a new agent or conversation to impersonate it.

## Discover locally, verify scope

Run `anx config workspaces` when unsure which workspace applies. Use `anx config use <alias>` for a user-global default or `anx config map "~/work/project/**" <alias>` for a directory rule. For one invocation, use `--workspace <alias>`. Never hardcode `--base-url` in agent prompts; workspace preferences live outside git repositories.

- Start with configured project/repository/task evidence and the user's existing MCP/CLI tools or skill descriptors. A descriptor is a proposal, not proof of connection or permissions. Verify a bounded read before declaring a source connected.
- Report identity, scope, coverage, freshness, last successful read and permission failures. A failed read preserves last good evidence as stale, with uncertainty; it never establishes that no work exists.
- Preview discovery scope, honor excluded paths and denied access, and request permission for new sensitive sources or expanded history. Do not scrape unrelated conversations or treat session references as grants. Share only useful task-scoped summaries and references.
- Associate projects automatically only when configured evidence is clear. Ask about consequential ambiguity or a new project; do not duplicate source tasks by title. Skip trivial activity.

## Synthesize useful overviews

Treat every ANX reader as a CEO by default. Lead with the outcome and what needs the human, then give concise evidence and detail. Group related executor issues, PRs and runs under an existing human-level initiative/outcome card; run `anx work list --project-ref <ref>` before creating, keep source details linked as evidence, and never mirror a tracker 1:1. Keep the workspace near 15 or fewer open cards and very few open asks; use a checklist or `anx.visual-report` dashboard/chart for dense status.

Summarize the accepted objective, decisions, meaningful progress, blockers, uncertainty, next actors/actions and evidence links. Attribute claims to the reporting agent or source and distinguish reported, inferred and verified facts. State coverage and freshness; do not fill missing context with certainty or reproduce raw chats.

Reconcile native Nexus commitments and externally authoritative tasks without replacing source IDs, assignees, workflow fields or completion criteria. A project need not have one owner. Participation is not ownership, and a completed run is not an accepted task outcome.

## Coordinate within the user's intent

- Inspect explicit task context and recent evidence before proposing the next step. Surface conflicts and consequential unresolved ambiguity to the user with a concise recommendation.
- Use ordinary task messages, asks and authorized source tools. Keep requester-scoped context and human decision gates. Never resolve an approval request on the user's behalf merely because you are PM.
- Ask only for decisions genuinely owned by the human (direction, money, risk or irreversible choices), recommend one answer, offer at most 2–3 alternatives and batch related decisions. Do not also block a card with the human as `next_actor` for the same question; that duplicates the Inbox item. Keep `next_actor` on the agent and use the response event as evidence when advancing or resolving the card.
- Keep updates tied to explicit task/project refs with evidence and next steps. Avoid repeated unchanged notices, hidden claiming or task-state writes from reads.
- Prefer fresh-context handoffs with objective, decisions, evidence, remaining work, authority boundaries and uncertainty. Cite prior sessions only as supported recovery clues. Do not promise universal resume/history access.

## Live-first reports

Start dashboards with `anx report init`: the default contains only live asks,
initiatives and activity. Use `live-cards` for filtered work and `live-timeline`
for adapter-fed series observations. Prefer computed panels and source syncing;
never hand-maintain milestone timelines, state tables or status callouts. If an
important fact cannot be shown live, file the missing sync work.

Hand-write only unavoidable narrative. Every authored panel must identify its
`author` principal and carry `authored_at` plus `review_by` (UTC date, zoned
timestamp, or duration such as `7d` relative to `authored_at`). Validate before
publishing. Status-like authored panels warn with a live alternative; expired
explicit deadlines are rejected on writes. Legacy panels default to seven days,
and pinned dashboard review reminders remain scoped to the author.
