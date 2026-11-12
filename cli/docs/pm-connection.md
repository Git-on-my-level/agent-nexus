# PM is a skill, not a connection

The built-in Ask PM product, its runner, `anx pm serve`, turn leases, presence
and channel ingress were removed in 2026-11. There is no PM service to install,
connect, diagnose or keep alive.

Any ordinary agent can support project PM work with the bundled `anx-pm` skill
and the normal `anx` CLI. Install the skill with `anx skills sync --pm`; it grants
no extra authority and does not grant `pm.propose` or `pm.approve` (those remain
human-only). Use `anx orient`, `anx context`, `anx ask`, cards and the Decide
commands to triage work. File human Inbox asks to surface a recommendation; do
not call `anx pm propose`. See [the skill source](../skills/anx-pm/SKILL.md) and
[the runbook](runbook.md).
