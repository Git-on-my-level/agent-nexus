---
name: anx-participant
description: Keep substantive work visible with scoped identity, explicit refs and honest promises.
---

# ANX participant

Skill contract: anx.participant.v42.

## Daily loop

Set `--as <name>` / `ANX_AS`; enroll (`anx help enroll`). Pick `--workspace <alias>`. Never hardcode URLs.

`anx orient`; `anx where <ref|path|text>` before edits. `anx tree` shows initiatives, children and Unsorted.
`anx context <card> --brief` gives a bounded handoff. At handoff, use `anx work checkpoint <card> --from-file <file|->` with the briefing's `checkpoint.version` as `if_version` (0 absent), never `work.version`. Local artifacts need host refs.

`anx work create --title "<outcome>" --under card:<parent>` creates; `anx work start <card>` takes native work. `anx note <card> "<text>" --evidence <ref>` posts progress and starts unowned work. `anx done <card> --evidence <ref>` finishes; `anx plan` makes checklists; `anx work measure` gives a goal its number.

Follow warnings. Promise `--next <when> "<what>"`; renew within two days. Only a human can choose: `anx ask`. Otherwise (digest, finding): `anx tell <card> "<headline>"`. Check `anx rulings list` first. Record rulings with `anx note <card> "<text>" --decided-by <who>`. Cite blockers `event:<ask-id>`; share `anx link`.

One parent per card; depth ≤3 (initiative → workstream → task). Without `--under` or `--initiative`, creation enters Unsorted. `where` suggests; confirm with `anx mv <ref|path> --under <ref|path>`. Humans set top-level goals; only when one asked: `anx mv <card> --root --reason "<why>" --requested-by <human>`. Return with `anx mv <card> --unsorted`. Store card refs, not paths. One owner per card; no owner is empty.

## Write for a busy boss

- Situation: baseline, change, numbers. Impact: who is affected and why it matters.
- Recommendation: `{action, why}`, tradeoff and known cost. List real alternatives and consequences.
- `if_no_answer`: known default or blocker.

Preserve uncertainty; never invent facts, urgency, costs, approvals or defaults. Omit unused fields.

`anx ask --template decision` is a fictional narrative. Edit frontmatter; preview `anx ask --from-file ask.md --preview`. Supply `--subject-ref card:<slug>`, `--recommend`, `--alt`, `--body`, `--by <when>`, `--blocks <ref>`. Withdraw stale asks; act on answers or re-ask. `anx orient` lists your upkeep.

After filing, arm `anx await <ask>` or `anx await --answers --deadline <time>`; re-arm on return. Mark only your asks read. Report issues with `anx feedback "<text>"`.

## Advanced setup

`anx help <job>` / `anx api <operation>` cover automation. Preview with `--dry-run`; never alter host services for coding. Networkless producers write only `.anx/outbox`, without credentials; mirrors are read-only. Keep uncertain originals and keys; inspect receipts. Never share secrets or raw transcripts.
