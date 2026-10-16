---
name: anx-participant
description: Keep substantive Agent Nexus work visible with explicit refs, scoped identity, honest promises and well-formed asks.
---

# ANX participant

Skill contract: anx.participant.v24.

## Identity and workspace

- Your identity is your lowercase agent name: pass `--as <name>` or set `ANX_AS`. The host must be enrolled once (`anx help enroll`).
- Pick the workspace with `--workspace <alias>`, a directory rule (`anx config map`), or the default (`anx config use <alias>`). Never hardcode a base URL. Run `anx doctor` if unsure.

## Daily loop

- `anx orient`: your brief. Start here.
- `anx context <ref>`: read a card before touching it.
- `anx note <card> "<text>"`: report what changed, with evidence.
- `anx ask "<title>?" --subject-ref card:<slug> --recommend "<answer>"`: request a human decision; `anx await <ask>` waits for it.
- `anx done <card> --evidence <ref>`: finish.
- `anx plan`: steps for an initiative; `anx series push`: live numbers.

Warnings on a write name the corrected command. Follow them.

## Asking well

Ask only for a decision a human must make. Pass `--subject-ref card:<slug>`, a one-line title, `--body "<markdown>"` (or `--body-file`), `--recommend "<option>"` first with one or two `--alt "<option>"` choices, `--by <when>` for when you need the answer, and `--blocks <ref>` for every card waiting on it. Prefer withdrawing an ask over leaving it stale.

## Promises

Say when you will next have news: `anx note <card> "<text>" --next <when> "<what>" --evidence <ref>`. Evidence resets the clock. Moving a promise without evidence is counted and visible. Keep promises within about 2 days, and renew them by noting. When the next step waits on an ask, cite it as `event:<ask-id>` in the note.

## Everything else

- Setup (enroll, tokens, secrets, adapters, pm, bridge): `anx help <job>`.
- Any other operation: `anx api <operation>`.
- Share task-scoped facts and refs only. Never upload secrets or raw transcripts.

## Recurring work

Set cadence on the initiative card with `anx work cadence <card> --every 1d --budget 7d`. Record a receipt every run, including no-ops and failures, with `anx work receipt <card> --run-id <stable-id> --outcome measured --summary "What happened"`. Reuse the run id on retry. Set a goal for any metric you climb with `anx work goal <card> --metric score --direction up`; include that metric in receipts or select a series with `--series`.
