---
name: anx-participant
description: Keep substantive work visible with scoped identity, explicit refs and honest promises.
---

# ANX participant

Skill contract: anx.participant.v34.

## Daily loop

Set `--as <name>` / `ANX_AS`; enroll (`anx help enroll`). Select `--workspace <alias>` or directory/default. Never hardcode URLs.

`anx orient`; `anx context <ref>` before edits.
`anx note <card> "<text>" --evidence <ref>` posts progress;
`anx done <card> --evidence <ref>` finishes. `anx plan`;
`anx series push` measurements.

Follow warnings. Promise `--next <when> "<what>"`; renew within two days.
Evidence resets the clock; moving promises alone remains visible.
Cite blockers as `event:<ask-id>`; share `anx link` URLs.

Label agentctl runs `anx.card.<initiative>` and cards land under it. `--under` overrides; `--top-level` opts out. Group: `anx work create --under card:P` or `anx work group card:C --under card:P`.
`likely_home` never blocks. `anx work fold` preserves refs/asks; `work unfold` reverses it.

## Write for a busy boss

Propose to a busy boss without context:

- What's happening (`situation`): baseline, change, numbers.
- Why it matters (`impact`): affected people; why facts determine the choice.
- What I recommend (`recommendation: {action, why}`): reason, tradeoff, known cost.
- Alternatives: real actions and consequences.
- If no answer (`if_no_answer`): known default/blocker.

Explain fully; no quota. Preserve uncertainty; never invent facts, urgency,
costs, approvals or defaults. Omit unused optional fields. Collapse technical details/evidence.

`anx ask --template decision`: fictional narrative example.
Edit frontmatter; preview: `anx ask --from-file ask.md --preview`.
Require situation/impact/recommendation/why; plain answer buttons.
`--subject-ref card:<slug>`, `--recommend`, `--alt`, `--body`, `--by <when>`, `--blocks <ref>`.
Withdraw stale asks; `anx await <ask>`.

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

Preview `anx bridge install --dry-run`; `anx bridge add --as <name> --workspace <alias>`.
Answers need wiring; never alter services merely for coding.

Preview `anx sync enable <directory> --card-id card:<handle> --dry-run`.
Review allowlists. Mount only `.anx/outbox` writable, without credentials/network;
mirrors read-only. Preserve UUIDs, uncertain originals/keys; inspect receipts and `stale_after`.

Recurring: `anx work cadence|receipt|goal`; reuse run IDs.
`anx help <job>` / `anx api <operation>`. Never share secrets or raw transcripts.
