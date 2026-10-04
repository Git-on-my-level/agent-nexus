/**
 * Batch ref resolve, shaped like the contract the plan work defines:
 * `{ref, kind, title, status, owner, progress, url, resolvable}`.
 *
 * The chips and the preview card are built and tested against this so they are
 * ready to swap to the real endpoint. Keep the shape honest — if the server
 * ends up spelling a field differently, change it here and the components
 * follow.
 *
 * The rows deliberately cover each case a chip has to render: work in flight,
 * finished work, blocked work, a doc, a project, a board with no page to open,
 * an external pull request, and a ref that resolves to nothing.
 */
export const refResolveExample = {
  refs: [
    {
      ref: "card:initiative-plans",
      kind: "card",
      title: "Initiative plans on cards",
      status: "in_progress",
      owner: "Codex Sol",
      board: "Release B",
      priority: "high",
      progress: { done: 3, total: 7 },
      next_step: "Computed progress and health",
      last_moved_at: "2026-10-04T09:12:00Z",
      url: "",
      resolvable: true,
    },
    {
      ref: "card:shared-report-contracts",
      kind: "card",
      title: "Shared report contracts",
      status: "done",
      owner: "Codex Luna",
      board: "Release B",
      progress: { done: 5, total: 5 },
      last_moved_at: "2026-10-03T17:40:00Z",
      resolvable: true,
    },
    {
      ref: "card:pushed-series",
      kind: "card",
      title: "Pushed series and declared adapters",
      status: "blocked",
      owner: "Codex Sol",
      board: "Release B",
      progress: { done: 1, total: 6 },
      next_step: "Waiting on the panel binding decision",
      last_moved_at: "2026-09-27T11:02:00Z",
      resolvable: true,
    },
    {
      ref: "doc:release-b-plan",
      kind: "doc",
      title: "Release B plan",
      status: "",
      owner: "David Zhang",
      last_moved_at: "2026-10-04T08:00:00Z",
      resolvable: true,
    },
    {
      ref: "topic:release-b",
      kind: "topic",
      title: "Release B",
      status: "",
      resolvable: true,
    },
    {
      ref: "board:release-b",
      kind: "board",
      title: "Release B",
      status: "",
      resolvable: true,
    },
    {
      ref: "https://github.com/Git-on-my-level/agent-nexus/pull/246",
      kind: "pull_request",
      title: "SCA-598: live dashboards with shared report contracts",
      status: "merged",
      owner: "Codex Luna",
      url: "https://github.com/Git-on-my-level/agent-nexus/pull/246",
      resolvable: true,
    },
    {
      ref: "card:deleted-thing",
      resolvable: false,
    },
  ],
};

/** The refs a page would have asked for, including the one that resolves to nothing. */
export const refResolveExampleRequest = refResolveExample.refs.map(
  (row) => row.ref,
);
