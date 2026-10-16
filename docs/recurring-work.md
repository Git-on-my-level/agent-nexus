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
most 16 metrics and 10 typed refs or HTTPS effect links. The required summary is
one human-readable line of at most 280 characters. `--started-at` accepts RFC3339;
finish time defaults to the recorded event time. Receipts are separate from
launcher runs (`anx runs`) and review receipts.

Core computes health in this order:

1. Two latest runs both have infrastructure errors: **failing**.
2. More than twice the cadence has elapsed since the last run: **overdue**.
   Before the first run, the clock starts when cadence is assigned.
3. Runs exist but no effect occurred within the progress budget: **stalled**.
4. An effect occurred within that budget: **healthy**.
5. Otherwise: **unknown**.

An effect is an `acted` outcome or a `kept` experiment. The default progress
budget is seven days. Done, cancelled and archived cards retain loop data with
health `unknown` and reason `done`. A card without annotations is a loop only
when it has a run receipt in the last 30 days. Brief shows at most 12 recent
runs and 60 goal points, with best respecting direction. Receipt goal history
uses a 30-day window; series goals use daily last values over the same window.
The baseline is explicit when supplied, otherwise the first displayed point.
Watch's existing series and flatness computations remain independent.

```sh
anx report init --template loops
anx report init --template hill-climb --card card:optimizer
```

`live-loops` accepts an optional `board` ref (default the initiatives board),
comma-separated `health`, and `limit` (default 24, maximum 48). Attention comes
first: failing, overdue, stalled, then healthy by latest effect. `live-loop`
requires `card` and includes the newest 50 run receipts. Statistics describe the
visible admitted cohort (at most 50 initiative candidates); `truncated` signals
additional candidates or display rows. Effects compare the last seven days with
the preceding seven; improved compares directional bests in the two periods.

A generic optimizer ledger such as
`{action: accept|reject, objective, delta, commit}` maps to a receipt as follows:
`accept` becomes `outcome=acted, decision=kept`; `reject` becomes
`outcome=measured, decision=rejected`. Put the candidate identifier in
`experiment.candidate`, the objective value in the named metric and
`score_after`, and `objective - delta` in `score_before`. Publish a commit's HTTPS
URL as an effect ref only when it represents an actual effect. A failed runner
records `infra_error` even when no measurement or candidate exists.
