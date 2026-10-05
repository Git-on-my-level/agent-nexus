Current captures of the initiative-first Overview and initiative page, against
the backend contracts from #264.

What each image is evidence of:

- `overview-desktop.png`, `overview-phone.png` — the Overview with the
  "Since you last looked" digest, the single "N items need you" Inbox link, and
  the initiative tiles. Each tile shows the health core computed, a mini-viz
  laid out by the shape core computed (a column per dependency layer for a
  tree, a track per run for lanes, one track for a chain), the next step and
  when it last moved.
- `initiative-desktop.png`, `initiative-phone.png` — the initiative page
  leading with its plan: status line, health, the layered tech tree with the
  critical path highlighted and steps as resolved ref chips, then the
  checklist, participation, evidence and the card body below.

The seed is a three-initiative projection in the serialized shapes core sends,
one per health state, plus a four-step branching plan. Fixtures are in
`web-ui/tests/e2e/initiative-views.spec.js`; the shapes themselves are checked
against `contracts/fixtures/initiative-overview/` in
`web-ui/tests/unit/initiativeContractConformance.test.js`.

These are dark because the product is dark-only: `web-ui/src/app.css` defines a
single token set, with no `data-theme` and no `prefers-color-scheme` block, so
there is no light UI to capture. The coordinator waived the light-theme
requirement on that basis.

To recapture: `cd web-ui && PLAYWRIGHT_PORT=4291 pnpm exec playwright test
tests/e2e/initiative-views.spec.js --project=default`.
