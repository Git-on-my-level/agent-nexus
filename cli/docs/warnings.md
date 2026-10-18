# CLI write-time warnings

Loop-tier writes warn instead of the participant skill explaining rules.
A warning never rejects a write: the command still exits 0 and the write is
sent. Each warning carries its id as `code` in the JSON `warnings` array and
on the `warning code=...` text line.

The trigger phrase is an exact substring of the warning message. The
participant skill must not contain any trigger phrase (I6); this table is the
machine-readable source for that check.

| id                          | command     | trigger phrase                   | suggests                                                                                                                                                     |
| --------------------------- | ----------- | -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| ask_no_deadline             | ask         | `no --by or --blocks`            | Add `--by <when>` (when the human should decide) and/or `--blocks <ref>` for the work this decision blocks.                                                  |
| ask_overtaken_subject       | ask         | `since the subject last moved`   | The subject card is flagged possibly moot; if the decision is overtaken, `anx ask withdraw <ask-id> --reason overtaken`.                                     |
| ask_auto_created_card       | ask         | `No --subject-ref`               | Pass `--subject-ref card:<slug>` so the ask stays on an initiative. The warning lists up to three open initiatives when a cheap work-list page is available. |
| work_near_duplicate         | work create | `matches an open initiative`     | Add a step to the matching initiative: `anx plan step add <card> --title <title> --ref <url>`.                                                               |
| work_over_target            | work create | `open initiatives (target 15)`   | Prefer a plan step on an existing initiative over a new one.                                                                                                 |
| note_clears_promise         | note        | `clears your promise`            | Carry the named promise forward with the supplied `anx note <card> --next <at> "<what>"` command.                                                            |
| note_promise_unowned        | note        | `promise set on an unowned card` | Claim it with `anx work start <card>` so an assignee is prompted to renew it.                                                                                |
| note_no_next                | note        | `no --next`                      | Renew the card's promise: `anx note <card> --next <when> "<what>"`.                                                                                          |
| note_moved_without_evidence | note        | `promise moved without evidence` | Attach evidence so the move resets the clock: `anx note <card> --evidence <url-or-ref> --next <when>`.                                                       |
| note_beyond_horizon         | note        | `beyond the 2-day horizon`       | Promise a nearer checkpoint, for example `--next 2d "<checkpoint>"`.                                                                                         |

`<when>` everywhere is the `review_by` grammar: a UTC date (`YYYY-MM-DD`), an
RFC3339 timestamp, or a positive `s`/`m`/`h`/`d` duration. A malformed value is
a usage error (exit 2); a missing one is never an error.

How each warning is computed, and what it costs:

- `ask_no_deadline`: no `--by` and no `--blocks` on `anx ask`. After the write
  the CLI makes one best-effort `GET /brief` and appends the count of open
  decisions with a `decide_by` ("at least N" when the brief's decisions are
  truncated). A failed read leaves the generic message; `--dry-run` makes no
  request.
- `ask_overtaken_subject`: reuses the subject card read that grounds the ask
  thread and checks its `possibly_moot.flagged`. No extra request.
- `ask_auto_created_card`: the ask had no `--subject-ref` and no current card,
  so the CLI created a ready card. After that write, best-effort
  `GET /work?limit=50` pages (newest first) supply up to three other open
  initiatives. `GET /work` has no parameter that excludes done or cancelled
  (`phase` is one exact value), so closed rows are dropped client-side and
  the server order is kept. The CLI follows `next_cursor` until it has three
  suggestions or four pages. A failed first read leaves the warning without
  them.
- `work_near_duplicate`, `work_over_target`: one `GET /work?limit=200` before
  the write; token Jaccard or normalized Levenshtein >= 0.8 against open
  initiative titles; more than 15 open initiatives.
- `note_clears_promise`: without `--next`, one best-effort `GET /events`
  admitting the newest 50 card-thread notes checks the caller's latest note
  within the same candidate window core folds. Author filtering is local, after
  the indexed thread/time page, so an inactive author cannot cause a history scan.
  A newer note without `next` prevents reviving an older promise.
- `note_promise_unowned`: reuses the card read and checks assignees/owner. No extra request.
- `note_no_next`: `anx note` without `--next`. No request.
- `note_moved_without_evidence`: `--next` without `--evidence`, and the
  caller's newest earlier `next` on the card resolves to a different time. One
  `GET /events` of the newest 50 card-thread notes, then the same local author check.
- `note_beyond_horizon`: `--next` resolves more than 2 days out. No request.
