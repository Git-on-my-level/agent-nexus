Before and after for SCA-629: the CEO Overview, the collapsible layout, the
shared markdown renderer, compact badges and readable plans.

Both columns run the **same fixture** — one workspace, five initiatives (one
blocked, one stale, one on track, one done, one with no plan), a five-step
branching plan, and a card body written in ordinary markdown with a task list,
a table, an ANX ref, a GitHub pull request and a `<details>` aside. `before` is
that fixture against `d998dfd6` (main before this change); `after` is the same
fixture against this branch. The fixture carries both `plan_health` and the
older `health {status}`, so the before images are missing nothing for want of a
field.

| | before | after |
| --- | --- | --- |
| Overview, 1440px | `before-overview-desktop.png` | `after-overview-desktop.png` |
| Overview, 390px | `before-overview-390.png` | `after-overview-390.png` |
| Initiative page, 1440px | `before-initiative-desktop.png` | `after-initiative-desktop.png` |
| Initiative page, 390px | `before-initiative-390.png` | `after-initiative-390.png` |

What the pairs are evidence of:

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

These are dark because the product is dark-only: `web-ui/src/app.css` defines a
single token set, with no `data-theme` and no `prefers-color-scheme` block.

To recapture, from `web-ui`:

```
REVIEW_CAPTURES=after PLAYWRIGHT_PORT=4291 pnpm exec playwright test \
  tests/e2e/review-captures.spec.js --project=default --workers=1
```

For the before column, add a worktree at the base commit, copy
`tests/e2e/review-captures.spec.js` into it, and run the same command with
`REVIEW_CAPTURES=before`. The spec skips itself when `REVIEW_CAPTURES` is
unset, so it costs a normal run nothing.
