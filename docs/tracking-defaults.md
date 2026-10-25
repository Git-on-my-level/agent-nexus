# Tracking without dropped work

The [2026-10-25 decisions](decisions/sane-tracking-defaults.md) keep creation
non-blocking and make missing follow-through visible without repeated alerts.

New cards get a computed promise twelve hours after creation when there is no
explicit promise or cadence. It has `promise.source=default`, not an authored
`next` note. The exact deadline, including fractional seconds from creation,
survives both the materialized brief and direct reads. A supplied assignee is preserved; otherwise creation assigns the
creating actor. Explicit promises and genuine evidence replace or meet the
default; ordinary conversation and plan authoring do not meet it.

Agent creators automatically acquire an ordinary #466 lease in the same create
transaction. Its initial TTL is 24 hours, chosen to allow the 12-hour reminder
before automatic expiry; callers can renew it or explicitly hand it off under
the existing fencing rules. The 12-hour ruling is the default promise deadline,
not the lease TTL; the automatic agent lease remains 24 hours. Human creators do
not acquire a lease. Existing opaque launcher owner labels remain supported;
the automatic lease owner is
`actor:<creating-actor-id>`, visible through `anx lease show`.

Explicit acquire and renew re-point `cards.assignee` to the lease owner when
the owner is an `actor:` reference or a resolvable `agent:` reference. This
assignment is atomic with the lease change, including a renewal by the same
owner after somebody changed the card's assignee. Opaque launcher labels do
not identify an actor and leave the assignee unchanged. A same-owner renewal
after the default reminder preserves its daily digest route; renewing execution
is not evidence of progress and does not send another reminder.

Release without hand-off and expiry assign responsibility to the chosen parent
initiative's assignee, then the configured workspace lead, and append an
automatic note in the same transaction. The fallback gets responsibility, not
a newly held execution lease. If neither candidate can read the card, existing
responsible ownership is retained and digested; no read permission is granted.
The lease clock records expiry on its next tick, as before; a sleeping core can
delay that audit/assignment, while reads already compute the expired lease.
Explicit hand-off transfers the lease without entering the digest.

An unmet default records one reminder and an idempotent notification to its
owner. Afterward its health is `digest`; it does not generate another
`past_promise`/`no_promise` alert every cycle. Unowned and fallback work also
reads `digest`. Cadence cards keep their independent receipt/overdue judgment.
`no_promise` and inactivity ordering are reserved for live-owned non-cadence
work. Terminal cards stop pending tracking work.

At 09:00 UTC, the tracking clock queues at most one daily digest per responsible
actor. The preview is at most 50 cards and points to the paged work inventory.
Pages and retries share the owner/day delivery identity. Agent recipients use
the existing bridge queue; humans use their enabled push/digest channels.
With no channel configured, the automatic note and the reader's
`brief.daily_digest` remain available. `anx orient` lists these cards under
`daily digest`; Watch's Initiatives tab shows “Needs an owner's look” with
the preview count and links to the same initiative pages in every layout. Reading the brief
sends no notification.
Ownership is checked again in the enqueue transaction; reassignment, a new
explicit promise, completion or lost visibility can cancel a stale snapshot.
A filtered snapshot anchors its note and wake to the first surviving readable
card's thread; no survivors means no notification. Queued human reminders and digests store
card identities only. Overdue-ask pushes and ask digests also omit cached titles.
Claim and delivery-list reads resolve current titles and reasons under the
recipient's access, remove denied contributors and hide empty aggregates,
including snapshots queued before this change. Every delivery kind then checks
its source and complete serialized payload with the shared resource policy;
this includes nested, prose and non-card references in legacy or future formats.
Revoked subjects are omitted from pending, claimed, delivered, failed and canceled
history alike. Wake and webhook mirrors use this same history guard, while their
canonical notification paths retain their scoped wakeup/event reads.
Aggregate source events and historical adapter error text are not exposed by
these filtered reads. Receipt acknowledgements, including idempotent retries,
return queue state with an empty payload and no source or provider error. They
never replay historical content, even if access changed after the claim.
Step/source evidence cancels the default routes when the canonical brief fold
records it; explicit note promises/evidence cancel them in the write transaction.
Notification delivery itself uses the existing durable delivery/retry queue.

Migration 121 installs bookkeeping and indexes without scanning canonical rows
or assigning historical cadences. A bounded maintenance cursor admits at most
100 old identities per tick; it does not backdate default promises. Unassigned
legacy cards enter the fallback digest. Existing sourced workflow ownership
remains source-owned; fallback responsibility is a local card/summary concern.

## Cadence migration

Run the generic operator script separately for each enrolled workspace. It uses
ordinary scoped CLI/API reads and conditional metadata patches. It never runs
a model and never reads a database or credentials directly.

```sh
python3 scripts/migrate-tracking-cadence.py --workspace "$ANX_WORKSPACE" --as "$ANX_AS" --report cadence-report.json
python3 scripts/migrate-tracking-cadence.py --workspace "$ANX_WORKSPACE" --as "$ANX_AS" --report cadence-report.json --apply
```

The first command writes an exclusive dry-run report and makes no writes to the
workspace. Review its `infer`, `already_tracked`, and `owner_review` entries.
The apply command automatically applies all eligible inferred cadences; no
per-card labeling or model inference is involved. It lists the rest for their
owners. A report is bound to workspace ID, URL and principal, so it cannot be
applied through a changed workspace alias or another reader's visibility.

Inference takes the newest at most 50 visible receipts for each card. At least
three receipts are required. Intervals must be positive, and their maximum
minus minimum must be at most 20% of their median. The inferred cadence is the
median rounded to whole seconds, within the existing cadence limits. This is
integer arithmetic, including the average of the middle intervals for an even
sample. Future, archived and withdrawn (trashed) receipts are excluded. Restored
or unarchived receipts count again. A truncated sample is explicitly marked.
Cadence does not make old receipts fresh: after apply, overdue still uses the
last receipt's original timestamp.

Apply re-reads identity, metadata version and receipt fingerprint. It preserves
concurrent edits and existing cadences, recording conflicts rather than
overwriting them. Before sending each conditional patch it fsyncs an adjacent
`.apply.json` journal containing the exact request. A lost response remains
`uncertain`; rerun the same apply command with the same report. A matching
observed cadence resolves the uncertainty without a duplicate patch. Disk
failure prevents sending an unpersisted write. No production migration was run
by the implementation lane.
