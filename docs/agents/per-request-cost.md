# Per-request cost

PR-description mechanics, moved from the root [AGENTS.md](../../AGENTS.md); the root guide keeps the merge-blocking rule. Read this before opening a PR that adds or changes any read path, including streaming poll ticks.

For every new or changed read path, including streaming poll ticks, describe its complexity in the PR: what grows with the request/page, what grows with workspace size, the enforced bounds, and the indexes used before filtering and pagination. Run the scale gate described in `core/docs/performance.md` when changing SQL, authorization, projections, streams, startup, or migrations. Add a budget entry for new reads and justify any exact query-plan exception in the checked-in allowlist. Do not weaken a budget to make a regression pass.

Reviewers must treat hot-path O(workspace) work as a merge-blocking P1, including unbounded materialization, per-row queries, full ownership/graph rebuilds, and read-time text/JSON scans. New or changed hot paths cannot be deferred as a performance follow-up. Existing main hazards may use exact, finite checked-in baselines linked to their P1 repair, as documented in `core/docs/performance.md`; remove those entries when repaired.
