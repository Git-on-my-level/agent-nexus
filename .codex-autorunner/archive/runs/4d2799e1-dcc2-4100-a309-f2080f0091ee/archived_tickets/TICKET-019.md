---
title: "Run a fresh tech-debt scout pass after cleanup lands"
agent: codex
done: true
ticket_id: "tkt_final_debt_scout_pass_019"
---

## Triage

Priority: P3. Run this after the cleanup queue and final review have landed, so scouts inspect the improved codebase instead of rediscovering already-scheduled work.

## Goal

Repeat the subagent scouting, triage, dedupe, and queue-appending process against the post-cleanup codebase to find remaining low-hanging tech debt.

## Problem

The current queue was created from a point-in-time scout pass. After these fixes land, some smells should disappear, some adjacent issues may become more obvious, and new low-risk cleanup opportunities may surface. A second pass should append only genuinely useful remaining work, not churn the completed queue.

## Proposed Work

- Spawn scoped subagents to scout for concrete, actionable tech debt in the post-cleanup codebase. Suggested scopes:
  - contracts and repo-level generated/check workflow,
  - core,
  - CLI and bridge adapter,
  - web-ui.
- Instruct scouts to create tickets only after reading the relevant `AGENTS.md` files and CAR ticket instructions.
- Assign disjoint numeric ranges after the current queue end so subagents do not conflict.
- Require each scout ticket to include `## Problem`, `## Evidence`, `## Proposed Fix`, and `## Validation`.
- After scouts finish, read the generated tickets, merge duplicates, remove weak findings, and reorder the appended block by priority.
- Keep already-completed tickets stable; append new tickets at the end unless the user explicitly asks to reprioritize the whole queue.

## Validation

- Run `python3 .codex-autorunner/bin/lint_tickets.py` after scout ticket creation and again after dedupe/reordering.
- Report the final appended ticket range, any merges or deletions, and the final lint result.

## Progress

- Spawned four scoped scout agents:
  - contracts and repo-level generated/check workflow (`TICKET-020..029` scout range),
  - core (`TICKET-030..039` scout range),
  - CLI and bridge adapter (`TICKET-040..049` scout range),
  - web-ui (`TICKET-050..059` scout range).
- Scouts created nine candidate tickets and each reported `python3 .codex-autorunner/bin/lint_tickets.py` passing after creation.
- Read and triaged every candidate against the completed cleanup queue.
- Kept all nine candidates; no duplicate or weak findings were deleted.
- No merges were needed. The web-ui stale-load tickets are adjacent to completed tickets `TICKET-011` and `TICKET-013`, but cover distinct remaining routes.
- Reordered and compacted the appended block by priority into final range `TICKET-020.md` through `TICKET-028.md`:
  - `TICKET-020.md`: Fail reads on corrupt agent wakeup refs JSON.
  - `TICKET-021.md`: Add secret command flags to config-independent preflight.
  - `TICKET-022.md`: Ignore stale web-ui detail route load results.
  - `TICKET-023.md`: Guard remaining web-ui list routes against stale loads.
  - `TICKET-024.md`: Retire legacy non-/stream SSE route registrations.
  - `TICKET-025.md`: Require x-anx streaming metadata to be an object with a valid mode.
  - `TICKET-026.md`: Replace managed bridge lifecycle TOML sniffing with parsed config.
  - `TICKET-027.md`: Clean generated contract output directories before regeneration.
  - `TICKET-028.md`: Remove no-op stale scan maintenance state.
