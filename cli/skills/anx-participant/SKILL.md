---
name: anx-participant
description: Keep substantive work visible with scoped identity, explicit refs and honest promises.
---

# ANX participant

Skill contract: anx.participant.v40.

## Daily loop

Set `--as <name>` / `ANX_AS`; enroll (`anx help enroll`). Pick `--workspace <alias>`. Never hardcode URLs.

`anx orient`; `anx where <ref|path|text>` before edits.
`anx tree` shows initiatives, children and Unsorted.
Create with `anx work create --title "<outcome>" --under card:<parent>`;
`anx work start <card>` takes ownership of native work.
`anx note <card> "<text>" --evidence <ref>` posts progress and starts
an unstarted card you or nobody owns.
`anx done <card> --evidence <ref>` finishes. `anx plan` is for checklists.

Follow warnings. Promise `--next <when> "<what>"`; renew within two days.
Next step needs a human? File an ask, not chat.
Record chat rulings: `anx note <card> "<text>" --decided-by <who>`.
Cite blockers as `event:<ask-id>`; share `anx link` URLs.

Initiatives are top-level cards outside Unsorted; one stored parent each,
depth at most three (initiative → workstream → task).
Without `--under` or `--initiative`, creation enters Unsorted.
`where` suggests; confirm by naming a home: `anx mv <ref|path> --under <ref|path>`.
Top level: `anx mv <card> --root --reason "Independent outcome"`, or
back to the tray: `anx mv <card> --unsorted`. Store card refs, not paths.
One owner per card; no owner means empty assignee, never an expired lease.

## Write for a busy boss

- What's happening (`situation`): baseline, change, numbers.
- Why it matters (`impact`): who is affected; why facts decide it.
- What I recommend (`recommendation: {action, why}`): reason, tradeoff, known cost.
- Alternatives: real actions and consequences.
- If no answer (`if_no_answer`): known default/blocker.

Preserve uncertainty; never invent facts, urgency,
costs, approvals or defaults. Omit unused optional fields. Collapse technical details/evidence.

`anx ask --template decision`: fictional narrative example.
Edit frontmatter; preview: `anx ask --from-file ask.md --preview`.
`--subject-ref card:<slug>`, `--recommend`, `--alt`, `--body`, `--by <when>`, `--blocks <ref>`.
Withdraw stale asks. Answers may be free text or questions: act or re-ask.
After filing, arm a wake: `anx await <ask>` or background `anx await --answers --deadline <time>`
(exit 14: no answer by then), re-armed on return. Mark read only your own asks.
ANX misbehaving? `anx feedback "<text>"`.

## Advanced setup

`anx help <job>` / `anx api <operation>` cover power verbs
and automation. Preview setup with `--dry-run`; never alter host
services for coding. Networkless producers get only `.anx/outbox` writable,
no credentials; mirrors stay read-only. Keep uncertain originals and keys;
inspect receipts. Never share secrets or raw transcripts.
