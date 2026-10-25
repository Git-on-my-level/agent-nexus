# Sane tracking defaults

Accepted by David, 2026-10-25. Scope: agent-and-inbox ergonomics.

1. Creation remains easy. A create without `--next` or cadence succeeds for
   every creator and gets a computed 12-hour promise, `promise.source=default`.
   Lapse sends the owner one reminder; afterward the card appears in a digest,
   without a new alert each cycle.
2. Agent-created cards acquire a work lease for the creating actor. Release
   without hand-off or expiry assigns the card to its parent initiative's owner,
   then the workspace lead, with an automatic note. Never leave it ownerless.
3. `no_promise` and stale flags apply only to live-owned, non-cadence work.
   Cadence work is tracked by receipts and the recurring-work overdue rule.
   Unowned work goes into one daily digest for its fallback owner.
4. Migration starts with a dry-run report for each workspace. At least three
   regular receipts with low interval spread permit deterministic cadence
   inference from the median interval, without a model, and automatic apply.
   Other cards are listed for their owners. No production migration is authorized
   by the implementation lane.

Delivery is split: the urgent change makes cadence count as tracked in
`plan_health`, summaries, ref previews and brief. It preserves existing state
enums: `moving` through the overdue boundary, `past_promise` afterward, with
`promise.source=cadence`. Receipt health remains in `loop` and the plan-health
reason. Completion still reads `done`. The other defaults, ownership and
migration behavior are implemented in the follow-up change, described in
[tracking defaults](../tracking-defaults.md). Source and tests do not imply an
installed runtime or a production migration. Both PRs remain unmerged.
