/**
 * Batch ref resolve responses for the UI's own tests and dev surfaces.
 *
 * The authoritative shape is `contracts/fixtures/initiative-overview/refs.json`,
 * which core serializes its production response against. Tests that only need
 * "a real response" should import that file directly — see
 * `tests/unit/initiativeContractConformance.test.js`.
 *
 * What lives here is a wider cast of rows in that same shape: the cases a chip
 * has to render that one contract fixture does not happen to include — a doc, a
 * project, a board with no detail surface, an external pull request, work in
 * flight, finished work and blocked work. Every field below exists in the
 * contract; nothing is invented.
 */
export const refResolveExample = {
  items: [
    {
      ref: "card:initiative-plans",
      kind: "card",
      title: "Initiative plans on cards",
      status: "in_progress",
      phase: "in_progress",
      owner: "actor:codex-sol",
      owner_display: "Codex Sol",
      board: { ref: "board:release-b", title: "Release B" },
      priority: "p1",
      last_moved_at: "2026-10-04T09:12:00Z",
      next_step: { title: "Computed progress and health" },
      progress: { done: 3, total: 7 },
      url: "/tasks/initiative-plans",
      resolvable: true,
    },
    {
      ref: "card:shared-report-contracts",
      kind: "card",
      title: "Shared report contracts",
      status: "done",
      phase: "done",
      owner: "actor:codex-luna",
      owner_display: "Codex Luna",
      board: { ref: "board:release-b", title: "Release B" },
      priority: "p2",
      last_moved_at: "2026-10-03T17:40:00Z",
      progress: { done: 5, total: 5 },
      url: "/tasks/shared-report-contracts",
      resolvable: true,
    },
    {
      ref: "card:pushed-series",
      kind: "card",
      title: "Pushed series and declared adapters",
      status: "blocked",
      phase: "blocked",
      owner: "actor:codex-sol",
      owner_display: "Codex Sol",
      board: { ref: "board:release-b", title: "Release B" },
      priority: "p1",
      last_moved_at: "2026-09-27T11:02:00Z",
      next_step: { title: "Waiting on the panel binding decision" },
      progress: { done: 1, total: 6 },
      url: "/tasks/pushed-series",
      resolvable: true,
    },
    {
      ref: "doc:release-b-plan",
      kind: "document",
      title: "Release B plan",
      owner: "actor:operator",
      owner_display: "Alex Morgan",
      last_moved_at: "2026-10-04T08:00:00Z",
      url: "/docs/release-b-plan",
      resolvable: true,
    },
    {
      // Topics and boards have no detail surface, so core sends no `url`.
      ref: "topic:release-b",
      kind: "topic",
      title: "Release B",
      resolvable: true,
    },
    {
      ref: "board:release-b",
      kind: "board",
      title: "Release B",
      resolvable: true,
    },
    {
      // An external source URL is returned as-is, never prefixed.
      ref: "https://github.com/Git-on-my-level/agent-nexus/pull/246",
      kind: "card",
      title: "SCA-598: live dashboards with shared report contracts",
      status: "merged",
      owner: "actor:codex-luna",
      owner_display: "Codex Luna",
      url: "https://github.com/Git-on-my-level/agent-nexus/pull/246",
      resolvable: true,
    },
    {
      // Unknown or inaccessible refs come back without metadata.
      ref: "card:deleted-thing",
      resolvable: false,
    },
  ],
};

/** The refs a page would have asked for, including the one that resolves to nothing. */
export const refResolveExampleRequest = refResolveExample.items.map(
  (row) => row.ref,
);
