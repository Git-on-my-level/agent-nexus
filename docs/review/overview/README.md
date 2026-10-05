The Overview regression fixture covers 18 backlog cards on an archived board,
seven active initiatives with 3/7 checklists, one dashboard document and two
human asks. Browser fixtures are in `web-ui/tests/e2e/overview.spec.js`; the core
integration test independently verifies archive filtering and dashboard
pin/fallback against SQLite.

Historical before/after screenshots contained contributor-specific project and
human names and have been removed. They remain in Git history. To capture the
neutral fixture, run `cd web-ui && pnpm exec playwright test
tests/e2e/overview.spec.js --project=default --workers=1`.

Current captures covering initiative plan health, shape-specific tile graphics
and the "Since you last looked" digest are in `docs/review/initiative/`.
