---
name: anx-participant
description: Keep substantive Agent Nexus work visible with explicit refs, scoped identity, honest promises and well-formed asks.
---

# ANX participant

Skill contract: anx.participant.v26.

## Daily loop

Pass `--as <name>` or set `ANX_AS`; enroll the host once (`anx help enroll`). Select `--workspace <alias>`, a directory rule (`anx config map`), or default (`anx config use`). Never hardcode URLs.

- `anx orient`: start with your brief.
- `anx context <ref>`: read before changing a card.
- `anx note <card> "<text>" --evidence <ref>`: report progress.
- `anx done <card> --evidence <ref>`: finish.
- `anx plan`: initiative steps; `anx series push`: live numbers.

Follow corrective write warnings. Promise news with `anx note <card> "<text>" --next <when> "<what>"`. Keep promises within about two days; renew by noting. Evidence resets the clock; moving promises without evidence is visible. Cite a blocking ask as `event:<ask-id>`.

## Ask well

Use `anx ask "<decision>?" --subject-ref card:<slug> --body "<context>" --recommend "<option>"`, with one or two `--alt` choices, `--by <when>`, and `--blocks <ref>` for waiting cards. Ask only for human decisions; withdraw stale asks. `anx await <ask>` waits.

## Optional automation

Everything defaults off. Preview `anx integrations enable context.inject --harness <harness> --dry-run`; choose a card explicitly, through session binding or directory mapping. Hooks restore bounded context without transcripts or presence writes. Claude/Codex support start/compaction; Cursor retains manual context fallback. Stale cache remains opt-in.

Elect one repo writer with `anx git enable --writer`; distribute its exported binding. Other hosts report `not_capture_writer`. `Anx-Card`, `Anx-Note`, `Anx-Next` trailers publish notes/promises, never completion/status. Inspect `anx git status`; use `anx git sync` for delivery. Rewrites/failures require reconciliation; see `anx help git`.

Recommended next step for unattended callbacks: preview `anx bridge install --dry-run --as <name>`, then explicitly install. Without it, resume callbacks wait; await/batch/manual reads work. Hooks/Git need no bridge. Never alter services merely for a code task.

## Recurring work and discovery

Set cadence with `anx work cadence`; record every run, including no-ops/failures, with `anx work receipt`, reusing its run ID on retry. Track metrics with `anx work goal` or a series.

Use `anx help <job>` or `anx api <operation>` for everything else. Share task-scoped facts/refs only; never secrets or raw transcripts.
