# Recurring work and loops

A loop is an initiative card with cadence, a goal, or recent run receipts. It uses
ordinary work annotations and events, with no scheduler or host dependency.

```sh
anx work cadence card:optimizer --every 1d --budget 7d --stop-rule "Stop at 0.9"
anx work goal card:optimizer --metric quality.score --direction up --target 0.9 --baseline 0 --unit score
anx work receipt card:optimizer --run-id iteration-42 --outcome acted \
  --summary "Kept the faster candidate" --metric quality.score=0.82 \
  --candidate candidate-42 --decision kept --score-before 0.79 --score-after 0.82 \
  --ref https://example.org/changes/42
```

Durations accept seconds, minutes, hours or days (`60s`, `30m`, `12h`, `1d`).
Use `--clear` on cadence or goal to remove that annotation. A goal's direction is
`up` or `down`; target, baseline and unit are optional. Use `--series
'series:quality.score?project=optimizer'` to obtain goal values from an existing
series instead of receipts. Series definitions remain raw, create-only records.

Record every run, including `measured`, `no_op` and `infra_error`. Reuse the same
run id when retrying: its identity is scoped to the canonical card, independent
of which actor or spelling of the card ref submitted it. A replay returns the
original event id. Experiment decisions are `kept`, `reverted` or `rejected`.
Metrics and experiment scores must be finite numbers. Receipts may include at
most 16 metrics and 10 typed refs or HTTP/HTTPS effect links (including `url:` refs; URL schemes are case-insensitive). The required summary is
one human-readable line of at most 280 characters. `--started-at` accepts RFC3339;
finish time defaults to the recorded event time. Receipts are separate from
launcher runs (`anx runs`) and review receipts.

Core computes health in this order:

1. Two latest runs both have infrastructure errors: **failing**.
2. More than twice the cadence has elapsed since the last run: **overdue**.
   Before the first run, the clock starts when cadence is assigned.
3. Runs exist with no goal: **healthy**. A monitor that only reports is doing its
   whole job by running on cadence.
4. A goal is set, and no effect occurred within the progress budget: **stalled**.
5. An effect occurred within that budget: **healthy**.
6. Otherwise: **unknown**.

Cadence counts as tracking even before the first receipt. `plan_health`, card
summaries, ref previews and brief never label a cadence card `no_promise`.
They use a computed `promise.source=cadence`: `moving` through exactly twice
the cadence from the last receipt (cadence assignment before the first run,
creation time for legacy annotations), then `past_promise`. The health reason
says **Awaiting first run** before the first receipt within that window; `loop`
retains the full failing/overdue/stalled/healthy/unknown health. Notes, ordinary
evidence and milestone edits cannot postpone the receipt deadline. Done and
cancelled cards still read `done`.

The [2026-10-25 tracking decisions](decisions/sane-tracking-defaults.md) also
define default promises, fallback ownership, digest routing and receipt-based
cadence migration for the follow-up implementation.

The follow-up [tracking-defaults guide](tracking-defaults.md#cadence-migration)
documents the dry-run-first deterministic cadence migrator and its apply journal.

An effect is an `acted` outcome or a `kept` experiment. Each loop exposes
`effects_7d`, the number of such runs in the last seven days; a run satisfying
both conditions counts once. Overview `stats.effects_7d` sums these same counts. The default progress
budget is seven days. Done and cancelled cards retain a non-null `loop` when
they have cadence, a goal, or a run receipt in the last 30 days. Their health is
`unknown` with reason `done`; completion alone does not create a loop. Brief
includes only its bounded recent-done cohort. Archived cards are omitted entirely
from brief and `live-loops`; explicit `live-loop` detail still returns their
loop with `unknown` / `done`, under the same admission rule. In either lifecycle
state, a card with neither annotation nor a recent receipt has `loop: null`. Brief shows at most 12 recent
runs and 60 goal points, with best respecting direction. Receipt goal history
uses a 30-day window; series goals use daily last values over the same window.
Rejected and reverted candidates remain in `goal.points` with their decisions
so charts can mark them, but their scores were not adopted. Incumbent values
have no decision or decision `kept`: `latest` is the newest incumbent value and
`best` is the directional best incumbent value across the goal window.
`best_before`, `best_after` and `improved_7d` compare only incumbent values.
For example, a kept score of 0.8 followed by a rejected score of 0.95 still has
latest/best 0.8, with both points shown. The baseline is explicit when supplied,
otherwise the first displayed incumbent point.
Watch's existing series and flatness computations remain independent.

```sh
anx report init --template loops
anx report init --template hill-climb --card card:optimizer
```

`live-loops` accepts an optional `board` ref (default the initiatives board),
comma-separated `health`, and `limit` (default 24, maximum 48). Attention comes
first: failing, overdue, stalled, then healthy by latest effect. `live-loop`
requires `card` and includes the newest 50 run receipts. Statistics describe the
visible admitted cohort (at most 50 loops, selected from annotation or recent-receipt
indexes before enrichment, taking newest metadata updates and receipts first);
ordinary cards do not consume admission slots. The merged cohort retains the 50
most recent admitted loops before health ordering and the display limit.
`truncated` signals additional visible loops or display rows. Effects compare the
last seven days with the preceding seven. For improvement, `best_before` is the
incumbent best in `[now - 30d, now - 7d)` and `best_after` is the incumbent best
in `[now - 7d, now]`. An explicit baseline (otherwise the first displayed
incumbent) substitutes only when the earlier window has no incumbent. A week
above baseline but worse than the earlier incumbent best does not improve.

A generic optimizer ledger such as
`{action: accept|reject, objective, delta, commit}` maps to a receipt as follows:
`accept` becomes `outcome=acted, decision=kept`; `reject` becomes
`outcome=measured, decision=rejected`. Put the candidate identifier in
`experiment.candidate`, the objective value in the named metric and
`score_after`, and `objective - delta` in `score_before`. Publish a commit's HTTPS
URL as an effect ref only when it represents an actual effect. A failed runner
records `infra_error` even when no measurement or candidate exists.
