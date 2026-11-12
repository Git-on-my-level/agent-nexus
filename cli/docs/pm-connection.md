# PM is a skill, not a connection

The built-in Ask PM product, its runner, `anx pm serve`, turn leases, presence
and channel ingress were removed in 2026-11. There is no PM service to install,
connect, diagnose or keep alive.

Any ordinary agent acts as project PM with the bundled `anx-pm` skill and the
normal `anx` CLI. Install the skill with `anx skills sync --pm`; it grants no
extra authority. Use `anx orient`, `anx context`, `anx ask`, cards and the Decide
commands to triage work. See [the skill source](../skills/anx-pm/SKILL.md) and
[the runbook](runbook.md).
