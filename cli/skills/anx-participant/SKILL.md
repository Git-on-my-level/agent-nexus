---
name: anx-participant
description: Keep substantive work visible with scoped identity, explicit refs and honest promises.
---

# ANX participant

Skill contract: anx.participant.v37.

## Daily loop

Set `--as <name>` / `ANX_AS`; enroll (`anx help enroll`). Select `--workspace <alias>` or directory/default. Never hardcode URLs.

`anx orient`; `anx where <ref|path|text>` before edits.
`anx tree` shows initiatives, children and Unsorted.
Create with `anx work create --title "<outcome>" --under card:<parent>`;
`anx work start <card>` takes ownership of native work.
`anx note <card> "<text>" --evidence <ref>` posts progress;
`anx done <card> --evidence <ref>` finishes. `anx plan` is a power verb for checklists, not parentage.

Follow warnings. Promise `--next <when> "<what>"`; renew within two days.
Next step needs a human? File an ask; never wait in chat.
Record chat rulings: `anx note <card> "<text>" --decided-by <who>`.
Cite blockers as `event:<ask-id>`; share `anx link` URLs.

Initiatives are top-level cards outside Unsorted. Each card has one stored parent;
depth is at most three (initiative → workstream → task). Board roles and plan links
never determine parentage.
Creation without `--under` or explicit `--initiative` enters Unsorted (`needs_home=true`).
`where` suggests; confirm by naming a home: `anx mv <ref|path> --under <ref|path>`.
Move to top level with `anx mv <card> --root --reason "Independent outcome"`, or
back to the tray with `anx mv <card> --unsorted`. Moving an initiative under another
card requires a reason. Paths are aliases; keep stable card refs in stored evidence.
One owner per card. Awaiting an owner means empty assignee, never an expired lease.

## Write for a busy boss

Provide context:

- What's happening (`situation`): baseline, change, numbers.
- Why it matters (`impact`): affected people; why facts determine the choice.
- What I recommend (`recommendation: {action, why}`): reason, tradeoff, known cost.
- Alternatives: real actions and consequences.
- If no answer (`if_no_answer`): known default/blocker.

Preserve uncertainty; never invent facts, urgency,
costs, approvals or defaults. Omit unused optional fields. Collapse technical details/evidence.

`anx ask --template decision`: fictional narrative example.
Edit frontmatter; preview: `anx ask --from-file ask.md --preview`.
`--subject-ref card:<slug>`, `--recommend`, `--alt`, `--body`, `--by <when>`, `--blocks <ref>`.
Withdraw stale asks; `anx await <ask>`.

## Advanced setup

`anx help <job>` / `anx api <operation>` cover power verbs, attention,
measurements and automation. Preview setup with `--dry-run`; never alter host
services merely for coding. Mount only `.anx/outbox` writable for networkless
producers, without credentials; keep mirrors read-only. Preserve uncertain
originals and keys and inspect receipts. Never share secrets or raw transcripts.
