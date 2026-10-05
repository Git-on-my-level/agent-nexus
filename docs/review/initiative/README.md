The initiative-first Overview and initiative page, against the backend
contracts from #264.

Screenshots have been removed: review binaries do not ship in this repo. They
remain in Git history. What they showed, and what the fixture still exercises:

- The Overview tiles. Each tile shows the health core computed, a mini-viz laid
  out by the shape core computed (a column per dependency layer for a tree, a
  track per run for lanes, one track for a chain), the next step and when it
  last moved.
- The initiative page leading with its plan: status line, health, the layered
  tech tree with the critical path highlighted and steps as resolved ref chips.

The seed is a three-initiative projection in the serialized shapes core sends,
one per health state, plus a four-step branching plan. Fixtures are in
`web-ui/tests/e2e/initiative-views.spec.js`; the shapes themselves are checked
against `contracts/fixtures/initiative-overview/` in
`web-ui/tests/unit/initiativeContractConformance.test.js`.

To exercise it: `cd web-ui && PLAYWRIGHT_PORT=4291 pnpm exec playwright test
tests/e2e/initiative-views.spec.js --project=default`.

SCA-629 moved on from these for everything below the plan: the Overview's
single "N items need you" link is now an urgent band, tiles are sorted by
attention and carry compact badges, and plan nodes are named by their step
title. To capture the current pages for a review, see `docs/review/sca-629/`.
