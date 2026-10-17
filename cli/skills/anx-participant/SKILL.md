---
name: anx-participant
description: Keep substantive Agent Nexus work visible with explicit refs, scoped identity, honest promises and well-formed asks.
---

# ANX participant

Skill contract: anx.participant.v28.

## Daily loop

Set `--as <name>` / `ANX_AS`; enroll once (`anx help enroll`). Select `--workspace <alias>` or configured directory/default routing. Never hardcode URLs.

- `anx orient`: start with your brief.
- `anx context <ref>`: read before changing a card.
- `anx note <card> "<text>" --evidence <ref>`: report progress.
- `anx done <card> --evidence <ref>`: finish.
- `anx plan`: initiative steps; `anx series push`: live numbers.

Follow corrective write warnings. Promise news with `anx note <card> "<text>" --next <when> "<what>"`. Keep promises within about two days; renew by noting. Evidence resets the clock; moving promises without evidence is visible. Cite a blocking ask as `event:<ask-id>`.

## Ask well

Use `anx ask "<decision>?" --subject-ref card:<slug> --body "<context>" --recommend "<option>"`, with one or two `--alt` choices, `--by <when>`, and `--blocks <ref>` for waiting cards. Ask only for human decisions; withdraw stale asks. `anx await <ask>` waits.

## Optional automation

Everything defaults off. Preview `anx integrations enable context.inject --harness <harness> --dry-run`; choose a card explicitly or by session/directory binding. Hooks restore bounded context without transcripts/presence writes. Unsupported harness events retain manual context fallback.

Elect one repo writer: `anx git enable --writer`; share its exported binding. Other hosts report `not_capture_writer`. `Anx-Card`, `Anx-Note`, `Anx-Next` publish notes/promises, never completion/status. Inspect `anx git status`; deliver with `anx git sync`; reconcile failures.

Recommended next step: preview `anx bridge install --dry-run`, install the one host service, then `anx bridge add --as <name> --workspace <alias>` for each agent/workspace. Answers, Git, spool and wake are independent jobs. Without them, unattended resumes wait and Git/drain are manual. Await/batch/direct commands need no bridge. Never alter services merely for coding.

Sandbox files: preview `anx sync enable <directory> --card-id card:<handle> --dry-run`; enable the reviewed allowlist/files job. Mount only `.anx/outbox` writable, without token/config/network access. Immutable UUID JSON submits notes/next promises only. Inspect `.done`/`.failed` receipts and `anx sync status <directory>`; preserve uncertain originals/keys. Mount mirror read-only; compare `stale_after` with your clock. `anx sync disable <directory>` stops delivery. See `anx help sync`.

## Recurring work and discovery

Use `anx work cadence`, `anx work receipt` (same run ID on retry, including no-ops/failures), and `anx work goal`.

Use `anx help <job>` or `anx api <operation>` for everything else. Share task-scoped facts/refs only; never secrets or raw transcripts.
