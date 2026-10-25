---
name: anx-participant
description: Keep substantive work visible with scoped identity, explicit refs and honest promises.
---

# ANX participant

Skill contract: anx.participant.v33.

## Daily loop

Set `--as <name>` / `ANX_AS`; enroll (`anx help enroll`). Select `--workspace <alias>` or directory/default. Never hardcode URLs.

`anx orient` reads the brief; `anx context <ref>` precedes edits.
`anx note <card> "<text>" --evidence <ref>` posts progress;
`anx done <card> --evidence <ref>` finishes. `anx plan` tracks steps;
`anx series push` records measurements.

Follow warnings. Promise `--next <when> "<what>"`; renew within two days.
Evidence resets the clock; moving promises alone remains visible.
Cite blockers as `event:<ask-id>`; share `anx link` URLs.

Label agentctl runs `anx.card.<initiative>` and cards land under it. `--under` overrides; `--top-level` opts out. Group: `anx work create --under card:P` or `anx work group card:C --under card:P`.
`likely_home` never blocks. `anx work fold` preserves refs/asks; `work unfold` reverses it.

## Write for a busy boss

`anx ask --template decision` gives a before/after example. Assume no context:
`situation` (1–2 sentences), `impact` (users/money/risk/time),
`recommendation: {action, why}` (include cost), optional
`alternatives: [{action, consequence}]`, `if_no_answer` (default/blocker and by when).
Collapse technical `details` and evidence; legacy asks retain body.

`anx ask --from-file ask.md --preview` checks locally. Missing
situation/impact/recommendation/why blocks publishing; `--force --reason` records
exceptions. Use plain answer sentences, short titles and no unexplained identifiers.
Inline: `--subject-ref card:<slug>`, `--situation`, `--impact`, `--recommend`,
`--why`, `--alt`, `--body`, `--by <when>`, `--blocks <ref>`.
Ask human decisions; withdraw stale asks; `anx await <ask>` waits.

## Attention

`anx attention place <subject-ref> --for actor:<actor-id> --to decide|watch|digest|routine --reason "<why>"` (≤200 characters).
Human-designated placements win, otherwise newest; open asks always reach Decide.
ANX runs no models. `anx attention feedback list --since 24h`: follow
`next_cursor` through empty pages and learn. Never mark human visits.

## Optional automation

Defaults off. Preview `anx integrations enable context.inject --harness <harness> --dry-run`;
bind a card. Hooks restore context without transcripts/presence writes.

Elect one `anx git enable --writer`; share binding. `anx git sync` publishes
notes/promises, never completion.

Preview `anx bridge install --dry-run`; register `anx bridge add --as <name> --workspace <alias>`.
Answers need wiring; never alter services merely for coding.

Preview `anx sync enable <directory> --card-id card:<handle> --dry-run`.
Review allowlists. Mount only `.anx/outbox` writable, without credentials/network;
mirrors read-only. Preserve UUIDs, uncertain originals/keys; inspect receipts and `stale_after`.

Recurring: `anx work cadence|receipt|goal`; reuse run IDs.
`anx help <job>` / `anx api <operation>`. Never share secrets or raw transcripts.
