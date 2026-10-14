# Ask answer delivery

Every ask records an **answer-delivery mode**: how a human's answer is supposed
to reach the asking agent. Answers are never silently dropped — the mode is
chosen explicitly, recorded on the ask, shown by `anx orient` and
`anx inbox list`, and printed in the ask result together with the reason and
the exact follow-up command.

## Modes

| mode     | for                                  | behaviour                                                                                                                                                                                                        |
| -------- | ------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `await`  | the agent is blocked and still alive | the caller blocks on `anx await <ask-id>`; the ask result prints the exact command                                                                                                                               |
| `resume` | long-running or sleeping hosts       | the ask registers an executor command (`--on-answer`, `ANX_RESUME_CMD`, or an agentctl continuation) that a running `anx bridge run` fires the moment the answer lands — even if the asking session is long gone |
| `batch`  | many low-stakes asks                 | answers queue as notifications; nothing wakes; the owning agent sweeps them on its next run with `anx await --answers`                                                                                           |
| `none`   | FYI / human-owned follow-through     | explicit opt-out. Nothing is registered; the answer stays visible through `anx orient` and `anx inbox list --status answered`                                                                                    |

`none` is never chosen automatically — it exists so "no delivery" is always a
deliberate decision.

## Choosing the mode

`anx ask` accepts `--delivery await|resume|batch|none` (as do `anx review` and
`anx escalate`). When the flag is missing the CLI picks a default in this
order:

1. **Executor wiring wins** — if the ask has a resume command (`--on-answer`,
   `ANX_RESUME_CMD`, or an active agentctl continuation via
   `AGENTCTL_EXECUTION_ID`), the mode is `resume`. Registration is durable: a
   bridge that starts later still delivers, and the result says whether one is
   reachable right now.
2. **Workspace default** — `anx config ask-delivery <mode>` stores a fleet-wide
   preference next to the other CLI workspace preferences (in
   `workspaces.json`, outside any repository). A workspace default of `resume`
   on a host that cannot deliver falls back to `batch` with a warning rather
   than failing the ask.
3. **Auto default** — `batch`. Filing an ask never fails just because the mode
   is missing; `resume` is chosen automatically only when executor wiring
   exists (rule 1), because a running bridge alone has nothing to fire.

An **explicit `--delivery resume`** is stricter: on a host with no running
bridge and no agentctl continuation it fails at ask time with the available
choices, instead of silently never firing.

Every ask result prints:

- the chosen `mode` and the `reason` it was chosen,
- the exact follow-up command: `anx await <ask-id>` for `await`,
  `anx await --answers` for `batch`, `anx orient` (delivery state) for
  `resume`, and `anx inbox list --status answered` for `none`.

## Filing related asks as one batch

```bash
anx ask batch --from-file asks.json   # 2-10 related asks, one human decision pass
```

A batch files N related asks (2–10, hard cap 10) as **one atomic request**:
every ask passes the same authoring lint as `anx ask`, all asks share one
backing thread, and the server assigns the shared batch id. The file holds
`{"asks":[...]}` where each entry accepts `title`, `body`, `subject_ref`,
`thread_id`, `refs`, `recommend`, `alts`, `request_id`, `coverage_hint`,
`evidence`, `supersedes`, and an optional per-ask `delivery` override (a
top-level `delivery` or `--delivery` is the default mode). `--dry-run`
validates and prints every member without sending; `-` reads the JSON from
stdin.

The human side sees **one** inbox item with one decision section per ask and
a single submit (the web UI keeps each section's text in its own durable
draft, so a reload returns the reader to what they had written). Answering
records all N answers atomically and releases them together to each ask's own
delivery mode — an ask filed with `--delivery none` is recorded without
waking its agent, the rest queue into the requester's debounced answer wake.
The result prints the batch id plus each ask's id and delivery follow-up.

## The bridge

`anx bridge run` is a long-lived process on the asking agent's host. It
consumes answer wakes for its agent, claims them, and executes the registered
resume command with the full answer JSON on stdin (plus `ANX_ASK_ID`,
`ANX_CARD_REF`, `ANX_OUTCOME`, `ANX_RESPONSE_EVENT_ID` in the environment).
Registrations are durable in the local answer registry, so a bridge that
starts later still delivers answers to asks filed while it was down.

Set it up once per host:

```bash
anx bridge install --as <agent>          # writes a launchd agent (macOS) or
                                         # systemd user unit (Linux), idempotent
anx bridge install --dry-run --as <agent>  # print the unit without writing
```

Run it by hand instead with `anx --as <agent> bridge run`.

`anx orient` flags the failure mode early: when the host has open `resume`
asks but no running `anx bridge run` and no agentctl continuation, orient
returns a `delivery_health` warning pointing at `anx bridge install`.

## Sweeping batched answers

```bash
anx await --answers               # block until a batch of answers arrives
anx inbox list --status answered  # everything already answered, including
                                  # answers filed while the session was closed
anx inbox read <ask-id>           # one answer in full, marks it read
```

`anx inbox list --status answered` always includes the caller's own answered
asks and agrees with `anx orient` → `my_asks_and_answers`.
