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
| `batch`  | many low-stakes asks                 | answers queue as notifications; nothing wakes; the owning agent collects them with `anx await --answers`                                                                                                         |
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

`anx bridge run` is one host supervisor across all registered agents/workspaces.
Its answer jobs claim only answer-subscription wakes, execute local commands with
answer JSON on stdin and the original workspace/agent/config identity, then
acknowledge the durable result. The host service shares no wake coverage between
agents: ordinary wake routing requires that agent's explicit runtime job.

Recommended setup:

```sh
anx bridge install --dry-run
anx bridge install
anx bridge add --as codex
```

Register every intended agent/workspace; adding one never creates another unit.
The foreground supervisor uses the same singleton. Resume commands and state stay
in the original owner-only namespaces, including after legacy-unit migration.
Delivered callbacks only retry acknowledgement; unknown interrupted outcomes
pause for explicit reconciliation. Use `ANX_ASK_ID` idempotently in callbacks.

Orient prominently recommends install/registration and reports undelivered resume
asks. Without the answer job, unattended resumes wait; Git/drain remain manual
without their jobs. Await/batch/direct commands continue to work. See the
[host bridge guide](../cli/docs/host-bridge.md) for policies, migration and doctor.

## Waiting on answers

```bash
anx await <ask-id>                # one answer gates the next step
anx await --answers --timeout 4h  # any answer this session has not been handed
anx inbox list --status answered  # everything already answered, including
                                  # answers filed while the session was closed
anx inbox list --unread           # answers nobody has marked read
anx inbox read <ask-ref>          # mark one answer read; accepts event:<id> and
                                  # the ask:... ref that orient prints
```

`anx inbox list --status answered` always includes the caller's own answered
asks and agrees with `anx orient` → `my_asks_and_answers`.

### What `anx await --answers` returns

Every session of a harness on one host shares a derived principal, and both
answer read state and wake notifications belong to that principal. So the wait
keeps its own local state under `<config>/answer-waits/` and writes nothing to
the server:

- **Answers to this session's asks** always come back, once, however old the ask
  and even if they landed before the first wait. They are found by reading each
  ask directly, because the asks page is ordered by ask time and an old ask
  answered late can sit arbitrarily deep.
- **Asks of unknown origin** (filed elsewhere, without a session, or by an older
  CLI) come back only if they were answered after this session's first wait, and
  only from the newest 200 asks.
- **Asks another session filed** are skipped. `--all` includes them. A timeout
  reports `other_session_unread` so a lost answer stays discoverable.
- **A withdrawn or expired ask of yours** also ends the wait, with `answer: null`.

The session is `ANX_SESSION`, else the harness session id, else, when a child
agent inherits two harness markers, the agentctl execution id. With none there is
one cursor per agent. A resumed session that gets a new id can no longer claim
its old asks: set `ANX_SESSION` to a stable name, or use `--all`. Waiters sharing
a session share a cursor, so whichever polls first is handed the answer.

Delivery is at-least-once and the local cursor is a wake mechanism, not a record:
`anx inbox list --unread` and `anx orient` stay authoritative after a crash.

### Debounce

Two independent windows apply.

The server batches **wake notifications** per principal: a wake fires 60 seconds
(`ANX_ANSWER_WAKE_QUIET_WINDOW`) after the _latest_ answer, and within about a
second when that principal has no open asks left. Measured on a local core:

| answers                                    | wake             |
| ------------------------------------------ | ---------------- |
| one, another ask still open                | +60.8s           |
| two 20.5s apart, another ask still open    | +81.8s, one wake |
| one, and it was the last open ask          | +1.0s            |
| two 5.4s apart, the second closing the set | +6.3s, one wake  |

Because the window is per principal, one session's open ask delays the wake for
every other session of that agent. `anx await --answers` does not read wakes.

The **wait** applies its own quiet period, `--settle` (default 20s): after the
latest answer it keeps collecting while in-scope asks stay open, and returns at
once when none do or when `--timeout` arrives. A held answer never becomes a
timeout. Poll cost is one read per ask the session still waits on, plus one page
of at most 200 asks, every 5 seconds, with at most 25 asks read per poll; a
session with more takes turns through them.
