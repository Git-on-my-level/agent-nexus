---
name: anx-participant
description: Keep substantive work visible with scoped identity, explicit refs and honest promises.
---

# ANX participant

Skill contract: anx.participant.v30.

## Daily loop

Set `--as <name>` / `ANX_AS`; enroll once (`anx help enroll`). Select `--workspace <alias>` or directory/default routing. Never hardcode URLs.

- `anx orient`: read your brief.
- `anx context <ref>`: read before changing a card.
- `anx note <card> "<text>" --evidence <ref>`: progress.
- `anx done <card> --evidence <ref>`: finish.
- `anx plan`: steps; `anx series push`: measurements.

Follow corrective warnings. Promise news with `anx note <card> "<text>" --next <when> "<what>"`; renew within two days. Evidence resets the clock; moving promises alone is visible. Cite blockers as `event:<ask-id>`. Share `anx link` URLs with humans.

## Ask well

Use `anx ask "<decision>?" --subject-ref card:<slug> --body "<context>" --recommend "<option>"`, one or two `--alt` choices, `--by <when>`, and `--blocks <ref>` for waiting cards. Ask for human decisions; withdraw stale asks. `anx await <ask>` waits.

## Attention

Use `anx attention place <subject-ref> --for actor:<actor-id> --to decide|watch|digest|routine --reason "<why>" [--rank N] [--request-key KEY]`. Reasons: ≤200 characters. Decide requests action; Watch shows progress; digest defers; routine retains background. Human-designated attention-agent placements win; otherwise newest wins. Open human asks always reach Decide. ANX runs no models.

Read `anx attention feedback list --since 24h [--for actor:<actor-id>]`; follow `next_cursor` through empty pages. Learn from feedback. Never mark human visits; reads leave markers unchanged.

## Optional automation

Defaults off. Preview `anx integrations enable context.inject --harness <harness> --dry-run`; bind a card explicitly. Hooks restore bounded context without transcripts/presence writes.

Elect one `anx git enable --writer`; share its binding. Git publishes notes/promises, never completion. Inspect `anx git status`; deliver with `anx git sync`.

Preview `anx bridge install --dry-run`; register `anx bridge add --as <name> --workspace <alias>`. Jobs are independent. Without answers, unattended resumes wait. Never alter services merely for coding.

Preview `anx sync enable <directory> --card-id card:<handle> --dry-run`; review allowlists. Mount only `.anx/outbox` writable, without credentials/network. UUID submissions contain notes/promises only. Preserve uncertain originals/keys; inspect `.done`/`.failed` and `anx sync status`. Mount mirrors read-only; check `stale_after`. See `anx help sync`.

Use `anx work cadence`, `anx work receipt` (reuse run IDs on retry), and `anx work goal`. Discover with `anx help <job>` or `anx api <operation>`. Never share secrets or raw transcripts.
