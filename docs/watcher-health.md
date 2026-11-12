# Watcher health alarms

Core watches recurring work even when its runner has died. It runs no model and
starts no external execution. Alarms use existing attention placements and human
asks; nothing automatically retires, resumes or reassigns a loop. The initiative
[foundation](architecture/foundation.md) and existing receipt/promise health
rules remain unchanged.

The built-in PM presence and turn-deadline alarms were removed in 2026-11 with
the Ask PM product. Any ordinary agent can support project PM work with the `anx-pm` skill (`pm.propose` and `pm.approve` remain human-only); there
is no runner presence to monitor.

For a loop, Core retains `failing`, `overdue` and `stalled` precedence. After two
continuous unhealthy cadences it places the card in its responsible actor's
digest, and after four it promotes that placement to Decide. A human recipient
gets one coalesced recovery ask instead of a second card placement, so an old
attention-agent placement cannot demote the outstanding decision. For overdue loops
the unhealthy clock starts at the known two-cadence missed-receipt boundary;
failing/stalled clocks start when maintenance first observes them. Goal-only
loops use their progress budget, default seven days, as the interval. Reading a
card never raises an alarm or changes its promise.

An active human assignee remains responsible. An agent needs workspace activity
within the configured freshness window (a token issuance, authored event or
presence observation). Otherwise Core selects the first active workspace human
who already has access to the card, from an indexed cohort of at most 200 humans.
This is alarm ownership only: the card assignment and execution lease do not
change. Receipt health is evaluated as the recipient; a private receipt is not
forwarded to a less privileged fallback. Placements, asks and reminders retain
receipt evidence references, so later access revocation also hides derived copy. If nobody in the admitted human cohort
can read the card, no content is published. A workspace with more than 200 humans
should explicitly assign a responsible human for such loops.

Stop rules remain human-authored text. Core does not interpret that text. A
producer can include `stop_rule_triggered: true` in a canonical `run_recorded`
receipt after evaluating the rule. A later receipt without that assertion clears
it. Alternatively, set `cadence.stop_after_unhealthy_seconds` (60 through 3155760000) to open the ask after a continuous unhealthy interval. Either opens
one **Retire or resume this loop?** ask addressed to a readable human. These
fields are available through the existing generic event/work APIs; no new route
is needed. A historical prose rule requires its producer to report a trip or an
operator to configure the timer; installing this change does not invent a trip.

Each episode commits its state, request keys, placement and ask in one SQLite
transaction. Ask, reminder and recovery thread identities are random; public
event IDs cannot preclaim them. Durable episode state provides idempotency, so
retries do not duplicate them. Repeats refresh the same placement
and append an automatic `message_posted` event with `payload.source=watcher_health`
and the ask reference. The repeat interval doubles, capped at 24 hours. An open
ask stays coalesced; after a human resolves it, continued failure opens another
ask only when the next repeat is due. Recovery withdraws the current open system
ask and removes only Core's placement, restoring ordinary Watch defaults when no
other placer remains. It preserves human answers and other placers' records.

Channel adapters can consume the initial canonical ask and automatic reminder
events; this OSS change does not change any hosted notification consumer or
promise external delivery. A sleeping or unreachable Core cannot run maintenance;
its next tick catches up from persisted evidence. Out-of-process availability
monitoring remains a deployment concern.

## Configuration

All alarms ship enabled. Deployment environment settings tune alarm delivery,
without altering `plan_health` or authored promises:

| Variable                            | Default | Meaning                           |
| ----------------------------------- | ------- | --------------------------------- |
| `ANX_LOOP_HEALTH_DIGEST_CADENCES`   | `2`     | Unhealthy intervals before digest |
| `ANX_LOOP_HEALTH_DECIDE_CADENCES`   | `4`     | Unhealthy intervals before Decide |
| `ANX_WATCHER_AGENT_ACTIVITY_WITHIN` | `1h`    | Agent activity freshness          |
| `ANX_WATCHER_REPEAT_INITIAL`        | `1h`    | Initial repeat interval           |
| `ANX_WATCHER_REPEAT_MAX`            | `24h`   | Repeat interval cap               |

Durations accept Go duration syntax, one second through 365 days. Cadence
multipliers accept 1 through 1000. Invalid values log the variable name and use
the default. Decide is clamped to at least digest, and the repeat cap to at least
the initial interval. Migration 136 creates clock bookkeeping and compact loop/receipt position tables.
Existing partial indexes seed only annotated loop identities and live receipt
positions; triggers maintain them with canonical changes. It adds no expression
index on events or work metadata and rewrites no canonical history.
The durable loop cursor admits at most 25 identities each 30-second tick, so
large inventories have a bounded sweep delay rather than an unbounded tick.
