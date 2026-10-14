# Write-time warnings

Warnings never reject a write. Each has an id, the command that emits it, a
trigger phrase that appears in the warning text, and the corrected command it
suggests. The participant skill must not contain any trigger phrase; the tool
says it at the point of action instead (see `skills/skill_warnings_test.go`).

| id | command | trigger phrase | suggests |
| --- | --- | --- | --- |
| ask_no_deadline | ask | `no --by or --blocks` | add --by <when> and --blocks <ref> |
| ask_overtaken_subject | ask | `since the subject last moved` | anx ask withdraw <ref> --reason overtaken |
| ask_auto_subject | ask | `created a subject card` | pass --subject-ref <card> |
| work_near_duplicate | work create | `matches an open initiative` | anx plan step add <card> --ref <url> |
| work_over_target | work create | `open initiatives (target 15)` | fold into an existing initiative |
| note_no_next | note | `no --next` | add --next <when> "<what>" |
| note_moved_without_evidence | note | `promise moved without evidence` | add --evidence <ref> |
| note_beyond_horizon | note | `beyond the 2-day horizon` | promise a nearer checkpoint |
