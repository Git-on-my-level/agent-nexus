# Work leases, deadlines and approval apply-by

Three small coordination primitives for multi-agent work. Each one turns a
convention that agents kept in prose into a durable record with an owner, a time
and a wake source.

| Primitive         | Commands                                               | Status                                     |
| ----------------- | ------------------------------------------------------ | ------------------------------------------ |
| Work lease        | `anx lease acquire / renew / release / show`           | **Implemented** (routes, core, CLI, tests) |
| Deadline          | `anx deadline set / clear / list`                      | Specified here, not implemented            |
| Approval apply-by | `anx ask ... --apply-by <dur> [--apply-owner <actor>]` | Specified here, not implemented            |

## 1. Work leases

A lease is one card's answer to "who is working this right now, until when?".
Use it before starting work, so that a second session or lane can't start the
same work.

```sh
anx lease acquire card:ramp-batch --owner exec-7f3a --ttl 2h --reason "ramp step 3"
anx lease renew   card:ramp-batch --owner exec-7f3a            # keeps the TTL
anx lease release card:ramp-batch --owner exec-7f3a --reason "step 3 merged"
anx lease show    card:ramp-batch
```

### Record

Each card has at most one row in `card_leases`, holding:

- `owner`: an opaque id, 1 to 128 characters of `[A-Za-z0-9._:@/+-]`. Use an
  execution, session or lane id. It is not a principal.
- `epoch`
- `holder_actor_id`: the authenticated principal that last acquired or renewed
  the lease.
- `ttl_seconds`, `acquired_at`, `renewed_at`, `expires_at`, `released_at`

`state` is computed on every read from the server clock:

| state      | meaning                                                          |
| ---------- | ---------------------------------------------------------------- |
| `none`     | never leased (`epoch` 0)                                         |
| `held`     | `owner` set, `expires_at` in the future, not released            |
| `expired`  | `expires_at` passed without renew or release; anyone may acquire |
| `released` | the holder released it; anyone may acquire                       |

### Semantics

- **Acquire** `--owner O --ttl D` (60s to 7d):
  - If the lease is `held` by another owner, the request is refused with
    `409 lease_held`. `error.details` names `owner`, `epoch`, `expires_at` and
    `holder_actor_id`. The CLI exits **13**, a code used for nothing else, so a
    launcher can tell "someone else is on it" from every other failure.
  - If the lease is `held` by O, the lease is extended in the **same epoch**
    (recorded as a renewal). Retrying an acquire is therefore safe.
  - Otherwise (`none`, `expired` or `released`) O takes the lease and **epoch
    increments**. The audit event names the previous owner and state.
  - `--if-epoch N` makes the acquire conditional on the current epoch still being
    N (0 when the card was never leased). On a mismatch the request fails with
    `409 conflict` (exit 4).
  - Archived or trashed cards cannot be leased.
- **Renew** `--owner O [--ttl D]` extends the lease from now. The TTL defaults to
  the lease's current TTL. Renew never revives an expired lease: it fails with
  `409 lease_expired` (exit 12) and O must acquire again, which takes a new epoch.
  This rule keeps the epoch a correct fencing token, because a holder that
  stalled past expiry always learns that its tenure ended. Renewing someone
  else's lease fails with `lease_held` (exit 13). Renewing a free lease fails
  with `lease_not_held` (exit 4).
- **Release** `--owner O`:
  - Releasing a free lease is a no-op (`changed:false`).
  - Releasing another owner's unexpired lease fails with `lease_held`.
  - O may release its own expired lease to close it explicitly.
- **Fencing.** Pass `epoch` downstream, for example in a PR trailer or a deploy
  receipt. A writer that checks "my epoch is still current" rejects work from a
  holder that has been superseded.

### Concurrency

Every lease change runs in one SQLite write transaction:

1. Load the card and its lease row.
2. Decide the change.
3. Apply it with a compare-and-swap on the previous `(owner, epoch, expires_at,
released_at)` tuple.
4. Append the timeline event.

Two concurrent acquirers can never both win. The loser sees `lease_held`, or
`conflict` if it used `--if-epoch`.

**Deliberate refinement of "card-revision CAS".** The lease has its own CAS
token, the lease tuple plus `epoch`, rather than the card body's
`if_base_revision`. If acquires and renewals bumped the card revision, every
renewal would invalidate in-flight `cards revise` edits and clutter content
history. The concurrency guarantee is the same: optimistic, server-side and
atomic.

### Audit and expiry

- These events are appended to the card timeline in the same transaction as the
  change: `card_lease_acquired` (with `previous_owner`, `previous_state` and
  `previous_epoch` on a takeover), `card_lease_renewed` and
  `card_lease_released`. All of them carry `owner`, `epoch` and `expires_at`, plus
  `reason` when one was given.
- Lease event types are core-written only: `POST /events` rejects them with
  `403 protected_event_type`.
- A core clock (`CardLeaseClock`, every 30 s) seeks
  `idx_card_leases_due` for unreleased leases past `expires_at`. For each one it:
  - records `card_lease_expired` **once per epoch**;
  - wakes the card's agent assignee through the durable wake path, or the
    workspace PM when the card has no agent assignee. The wake is an
    `agent_wakeups` row, mirrored into the `deliveries` queue on channel
    `bridge`. The target is woken only if it can read the card.
- State never depends on the clock: reads compute expiry from `expires_at`. A
  hosted core that is asleep records the expiry event late, but reports the lease
  as `expired` on time. The heartbeat does not yet publish the next lease expiry
  (see open question 4).
- A failed wake is logged and not retried. The expiry event is already durable.

### Limits, stated plainly

- **This is optimistic concurrency on the server, not a partition-safe authority
  lock.** A holder cut off from the server can't learn that its lease expired or
  moved until it next renews, and can keep acting meanwhile. That is acceptable
  for work claims: a brief double-claim is visible and recoverable. It is **not**
  acceptable for the program authority that gates merges, deploys and prod config.
  That authority stays in driver-failover's git-ref CAS lease with fencing.
- `owner` is a label, not an identity. Any principal that can write the card and
  presents the matching `owner` can renew or release. `holder_actor_id` records
  who did.
- There is no forced takeover. To take a live lease, ask the holder to release
  it, or wait for `expires_at`. A launcher `--force` belongs in the launcher and
  should be logged to the card as a note.
- A renewal writes one timeline event. Renew at about TTL/3, not every minute.

### Out of scope for anx

anx does not run processes, poll hosts, hold secrets for leases, or act as a
prod authority lock. Liveness belongs to the agent runtime, which renews the
lease while its execution is alive. anx only records what agents tell it, and
the clock.

## 2. Deadlines (spec only)

Prose deadlines ("cut when ready", "the 13Z freeze") are missed because nothing
fires before them. A deadline makes the time an event.

```sh
anx deadline set card:listen-roll --at 2026-10-25T13:00:00Z --warn 90m,30m \
  --what "freeze: last train cut" [--channel lead]
anx deadline clear card:listen-roll
anx deadline list [--due-within 6h]
```

### Behaviour

- `set` writes, in one transaction:
  - The card's `due_at := at`. This makes the deadline the card's milestone:
    brief, orient and `plan_health` already count `due_at` once no live note
    promise remains, and report `past_promise` after it.
  - A `card_deadlines` row: `card_id`, `at`, `warn_offsets` (at most 4, each
    1m to 7d before `at`), `what` (at most 160 characters), `channel`, `set_by`,
    `revision`.
  - One scheduled delivery per warn offset, plus one at `at`, inserted with
    `delivery.Request.NotBefore = at - offset`. Each row has:
    - idempotency key `deadline:<card_id>:<revision>:<offset>`;
    - kind `push`;
    - payload `{card_ref, at, what, offset, kind: deadline_warning|deadline_due}`.

    `deliveries claim --channel <channel>` already returns only rows whose
    `next_attempt_at` has passed. The adapter that delivers the channel claims
    each one when it falls due, for example a lead-wait adapter that declares
    `delivers: [lead]`.

  - An event: `card_updated` with `changed_fields:[deadline]`.

- **Recipients.** Rows go to the card's assignee principal. Without one they go
  to the `set_by` principal. When `--channel` is omitted, the clock also wakes
  the agent assignee at each offset through the bridge wake path, as it does for
  lease expiry.
- A second `set` replaces the deadline: the revision increments and the
  previous revision's pending rows are cancelled (`status=canceled`).
- `clear` cancels the pending rows and clears `due_at` only when it still equals
  the deadline's `at`. If someone edited `due_at` since, it is left alone.
- `list` is an indexed seek on `card_deadlines(at)` limited to cards the reader
  can see, ordered by `at`. `--due-within` bounds it; the page is capped at 50.
- **Not a note promise.** `set` does not write `--next`. A note promise belongs
  to its author, and a deadline set by the lead must not replace the owner's
  promise. Brief shows both: the note promise while it is live, then `due_at`.

## 3. Approval apply-by (spec only)

An approval that nobody applies costs as much as no decision. With `--apply-by`,
an approved answer creates a dated promise to apply it.

```sh
anx ask "Retain audio 90d?" --subject card:retention --recommend "Yes, 90d" \
  --alt "Keep 30d" --apply-by 12h [--apply-owner actor:lead]
```

### Behaviour

- The ask stores `apply_by` (s/m/h/d duration, 1m to 7d) and `apply_owner`
  (defaults to the asker) in `human_attention_requested.payload`.
  `--apply-by` requires `--subject <card>`.
- **What counts as approval:**
  - outcome `approved` (explicit Approve);
  - outcome `answered` with `followed_recommendation=true`, meaning the response
    equals the first response proposal, the recommended one.
- **What does not:**
  - `rejected`, `needs_context`, `acknowledged`, `resolved`, withdrawn or expired
    asks;
  - an `answered` response that picked an alternative or free text. The ask
    result warns the owner to record its own `--next` (open question 2).
- **On approval**, in the respond transaction, core:
  - appends a note to the subject card authored by `apply_owner`, with
    provenance `{"sources":["event:<answer_id>"],"by":"core:ask_apply"}` and
    `next = {at: answered_at + apply_by, what: "apply: <ask title>"}`;
  - inserts an `ask_apply` row: `ask_id`, `card_id`, `owner`, `due_at`, state
    `open`.
- **Applied:** any later note with an evidence ref that resolves, or the card
  reaching done or cancelled. These are the existing promise-reset rules.
- **Breach:** the promise passes `due_at` without evidence.
  - orient and brief already show it as `past_promise`, which anti-drop sweeps
    read.
  - The clock seeks `ask_apply(state, due_at)`. It records `exception_raised`
    (subtype `apply_overdue`) on the card, wakes `apply_owner`, and enqueues one
    `push` escalation to the approving human's notification channels.
  - At `due_at + apply_by/2` it escalates once more, to the workspace PM, and
    marks the row `escalated`.
- **SLA policy stays with the caller.** anx has no default `--apply-by`. The
  Omi rule (12h, or 4h for changes worth more than $1k/month) belongs in the
  caller's ask conventions. Encoding dollar thresholds in anx would make it
  workspace-specific.

## 4. Open questions

1. Takeover: should `lease acquire --force` exist in anx, always logged and
   recording the overridden epoch, or stay a launcher concern? Recommended:
   launcher only, for now.
2. Should an answer that picks an alternative also create an apply promise,
   whose `what` names the chosen option? Recommended: no for v1, plus a warning.
3. Should renewals be coalesced on the timeline (for example, record only when
   TTL changes or at most hourly) if lanes renew often?
4. Should the hosted heartbeat publish the earliest lease expiry and deadline
   offset, so that an idle hosted core wakes on time to send those wakes?
