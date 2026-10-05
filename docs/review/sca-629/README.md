How to capture the Overview and the initiative page for a review.

No images here: review binaries do not ship in this repo. Run the capture, look
at the PNGs, and attach them wherever the review lives.

`web-ui/tests/e2e/review-captures.spec.js` drives both pages at 1440 and 390
from one seeded workspace — five initiatives (one blocked, one stale, one on
track, one done, one with no plan), a five-step branching plan, and a card body
written in ordinary markdown with a task list, a table, an ANX ref, a GitHub
pull request and a `<details>` aside. The fixture carries both `plan_health`
and the older `health {status}`, so a capture of an older revision is missing
nothing for want of a field.

From `web-ui`:

```
REVIEW_CAPTURES=after PLAYWRIGHT_PORT=4291 pnpm exec playwright test \
  tests/e2e/review-captures.spec.js --project=default --workers=1
```

Output lands in `web-ui/.screenshots/review/`, which is gitignored. The spec
skips itself when `REVIEW_CAPTURES` is unset, so it costs a normal run nothing.

For a before column, add a worktree at the base commit, copy the spec into it,
and run the same command with `REVIEW_CAPTURES=before`. `REVIEW_CAPTURES` only
changes the filename; the fixture is identical, which is the point.

What the pairs are evidence of, for SCA-629:

- **Overview.** Before: a "Needs you" header with a count and no items, the
  dashboard above the work, tiles in projection order with `**Goal:**` showing
  through, and "moved 5h ago" on every one. After: one urgent band naming what
  is waiting and which initiatives have stopped moving, then initiatives worst
  first with done and planless ones folded away, then the dashboard. Tile
  descriptions are prose; health is a tone-coloured glyph with its reason on
  hover; the age is `8h`.
- **Initiative page.** Before: plan nodes labelled by a truncated ref chip, the
  card body as raw markdown source — asterisks, pipe tables, a visible
  `<!-- fleet-sync:evidence:v1 -->` and a literal `<details>` tag — and four
  paragraphs explaining two empty sections. After: nodes named by their step
  title with the ref beneath (the GitHub step reads `merged`), the body
  rendered, the empty sections one line each with their caveats folded, and
  Authority/Owner gone where they had nothing to say.
- **390px.** Neither page scrolls sideways in either column; the plan diagram
  is the one thing that does, deliberately. After, the left nav is the bottom
  tab bar and the right rail stacks with its content still readable.

Captures are dark because the product is dark-only: `web-ui/src/app.css`
defines a single token set, with no `data-theme` and no `prefers-color-scheme`
block.
