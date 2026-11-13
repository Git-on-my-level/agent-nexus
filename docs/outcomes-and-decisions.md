# Outcomes and decisions

ANX gives a person two screens: **Decide**, for the few choices only they can
make, and **Watch**, for the few numbers that say whether the work is winning.
This page describes the rules that keep both true. They hold for every agent
and every workspace; none of them is a setting.

The rules share one design choice: a write that would degrade the human's view
is **redirected, not rejected**. It still succeeds, lands where it belongs, and
the response says where. Rejections make agents retry or work around; advice in
a prompt gets skipped.

## Decide holds only decisions

**One question, one row.** Core, not the client, decides whether an ask is new.
An ask whose requester already has an open ask about the same card, for the
same recipient, with the same title (letters and digits only, case-folded) is
not stored: the response carries the open ask and `deduplicated: true`. The
card is part of the key because a generic title ("Approve this merge?") is a
different question on a different card. An ask that names no card would get a
fresh card each time, so the CLI first looks for the agent's own open ask with
that title anywhere in the workspace and returns it.
`--supersedes event:<old-ask>` withdraws the ask it replaces when the new one
is stored. `--again` files a new ask because the facts changed.

**Telling is not asking.** `anx tell <card> "<headline>" [--body <text>]` is
for anything a person should know that wants no answer: a digest, a finding, a
heads-up. It is stored on the card as a `message_posted` with `kind: fyi` and
listed on Watch under "For your information" (`brief.fyi`, newest ten of the
last week). It never appears in Decide and never becomes the card's headline
note. A repeat of one headline on one card by one author collapses into the
newest row with a count, where numbers in the headline are ignored, so a daily
digest is one row that says how often it arrived.

**Stakes.** `anx ask --irreversible --cost "<text>"` records the asker's claim
about what the decision risks, as `stakes` on the ask. Decide shows it as
badges so a reader can size a decision before opening it. It is never a sort
key: deadline and blocked work still order the list.

**Sending one back.** A person can mark a decision "Not a decision". That
retires the ask (`POST /asks/{id}/retire`), and the asker reads why in
`anx orient` under "returned to you" (`brief.returned_to_you`).

**Anyone may retire a stale ask.** Retirement is undoable, audited and
announced to the asker, so it needs no special authority. Any live member,
human or agent, may `anx ask retire <ask> --reason <why> --evidence <ref>`.
Rewriting someone's ask (`anx ask revise`) still needs the asker, a human or an
auth-admin agent.

## Standing rulings

A standing ruling is an answer a person made durable, so the same question
never returns to Decide. There are two kinds.

| Kind             | Made when a person…                        | Covers                                                                                       | What the next asker gets                                                            |
| ---------------- | ------------------------------------------ | -------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| `answer`         | answers and ticks "Always answer this way" | any later ask with the same title, whoever asks                                              | the answer at once (`answered_by_ruling: true`); nothing is stored                  |
| `not_a_decision` | sends an ask back and ticks "Always"       | later asks from the same requester whose title differs only in its numbers (a date, a count) | the content stored as an fyi on the subject (`redirected: tell`); nothing in Decide |

Matching is exact on purpose. Whether a differently worded question is covered
is a judgment for the asking agent, which reads `anx rulings list` before it
asks; `anx ask` also names up to three rulings that read alike
(`ask_similar_ruling`). Only a person makes or revokes a ruling
(`POST /rulings`, `POST /rulings/{id}/revoke`), from an ask they can read and
have already answered or sent back; every member may read the rulings whose
source ask they can read.
A ruling counts its uses, and Decide lists rulings with a Revoke button, so a
person can see what is being answered in their name. `--again` reaches the
person even when a ruling exists.

## Watch shows outcomes

**A measure is the number an initiative is judged by.**
`anx work measure <card> --label "<plain name>" --direction up|down --target <n> [--series series:<name>] [--unit <unit>]`
stores `metadata.measure`. A producer publishes readings with
`anx series push`; the brief computes `measure` on the initiative from the
series' daily values: the latest reading, the reading about a week earlier,
whether the target is met, and how old the reading is. Watch shows that first,
with the age ("read 4d ago") and no threshold that calls a number stale.
An initiative with a measure and no reading says "No reading", which is the
loudest state a goal can be in: nobody is measuring it.

A measure is separate from a loop's `goal`. A goal makes a card recurring work
with run receipts and loop health. A measure only says how to read an outcome.

**The promise clock stays, as a second axis.** Whether an owner is reporting
on time ("past promise") answers "is anyone carrying this?". The measure
answers "are we winning?". A row with a measure shows the reading first and
lateness as a small badge.

**The headline note is something an owner wrote.** Reminders core writes itself
(ownership fallback, default-promise reminders, the tracking digest) and fyi
notes are never a card's headline note.

## Top-level goals are a person's to set

A person creates or promotes a top-level initiative directly. An agent may
when a person asked, and names them:

```sh
anx work create --title "Cut onboarding time" --initiative --requested-by Dana
anx mv card:retries --root --reason "Independent outcome" --requested-by Dana
```

The name is recorded as the reason of the change, which the person reads under
recent changes. Without a named person:

- a new card is created in **Unsorted** and the response carries
  `redirected: unsorted`, with the command to group it under an existing goal;
- promoting existing work is refused with `human_request_required`, because
  there is nowhere else for a move to land; the agent proposes it with an ask.

Layout is not authority: an agent's card on an initiatives-role board also
waits in Unsorted. Recurring work (a cadence or a goal) is exempt; an agent may
start a loop. ANX cannot verify that a person asked in a chat it never saw, so
the named request is a claim the person can see and reverse, not a proof.

This is why there is no hidden kind of card for coordination plumbing (a lane
home, an audit trail, a standing subject for a digest). Such a card can no
longer become an initiative on an agent's own say; it is grouped under the goal
it serves.

## Upkeep without a privileged agent

No agent role has special authority. Every upkeep action is open to every
agent and undoable; a skill only makes one agent do it habitually.

- `anx orient` shows an agent the upkeep suggestions about its own asks and
  cards (`curation_suggestions[].yours`) and counts the rest;
  `anx orient --upkeep` lists all of them.
- A larger batch suggestion replaces the smaller ones it covers. Before this, a
  suggestion's key was its exact target set, so one more ask on a subject left
  "Batch 2", "Batch 3" … open side by side.
- The brief reports `upkeep`: how many suggestions are open, how long the
  oldest has waited, and `untended` once that passes the product's single
  fallback duration (seven days). Watch then tells the person that no agent is
  tending the workspace and how to give one the project-manager
  skill (`anx skills sync --pm`).
- The project-manager skill (`cli/skills/anx-pm`) describes one upkeep pass and
  its limits: act with undo instead of asking, at most one `anx tell` a day,
  and be judged on the state of the workspace rather than on activity.

## Cost

Every read added here is bounded and indexed.

| Read                         | Bound                                                                                   |
| ---------------------------- | --------------------------------------------------------------------------------------- |
| duplicate check on ask       | newest 64 open asks of one requester about one card (`idx_brief_asks_subject`)          |
| ruling check on ask          | two seeks on `idx_standing_rulings_key`                                                 |
| `GET /rulings`               | newest 200 through `idx_standing_rulings_recent`                                        |
| `brief.fyi`                  | newest 40 fyi notes of the last week through `idx_brief_notes_fyi`, shown as at most 10 |
| `brief.returned_to_you`      | 10 rows through `idx_brief_asks_answered`                                               |
| `measure` on initiatives     | one statement over the page's cards, plus at most 31 daily buckets per linked series    |
| `curation_suggestions.yours` | two batched lookups over the page's targets                                             |
| `brief.upkeep`               | 51 rows of the open-suggestion ledger; omitted for principal-scoped readers             |
| superseded batch suggestions | newest 200 open batch suggestions, once per formula run                                 |
