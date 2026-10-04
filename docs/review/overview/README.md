The before and after screenshots use the same Omi-shaped seed: 18 backlog
cards on an archived board, seven active initiatives with 3/7 checklists,
one dashboard document and two human asks. The before image runs the
Overview loader and page from `24fe910e` (main before this change). The after
image runs the shared core projection layout. Browser fixtures are in
`web-ui/tests/e2e/overview.spec.js`; the core integration test independently
verifies archive filtering and dashboard pin/fallback against SQLite.

To recapture after: `cd web-ui && PLAYWRIGHT_CHANNEL=chrome pnpm exec playwright
test tests/e2e/overview.spec.js --project=default --workers=1`.
