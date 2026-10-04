/**
 * A batch ref resolve response, in the wire shape core returns:
 * `{items: [{ref, kind, title, phase, status, owner, progress, url, resolvable}]}`.
 *
 * Only contract fields appear here. In particular there is no board, priority,
 * next step or last-moved on a resolved ref — the preview card renders those
 * when present and omits them otherwise, which today means it omits them.
 *
 * `url` is workspace-relative for native entities and absent for topics and
 * boards, which have no detail surface; the components derive their own link
 * for those rather than inventing a server URL.
 *
 * The rows cover each case a chip has to render: work in flight, finished
 * work, blocked work, a doc, a project, a board, an external pull request, and
 * a ref that resolves to nothing.
 */
export const refResolveExample = {
  items: [
    {
      ref: "card:initiative-plans",
      kind: "card",
      title: "Initiative plans on cards",
      status: "in_progress",
      owner: "Codex Sol",
      progress: { done: 3, total: 7 },
      url: "",
      resolvable: true,
    },
    {
      ref: "card:shared-report-contracts",
      kind: "card",
      title: "Shared report contracts",
      status: "done",
      owner: "Codex Luna",
      progress: { done: 5, total: 5 },
      resolvable: true,
    },
    {
      ref: "card:pushed-series",
      kind: "card",
      title: "Pushed series and declared adapters",
      status: "blocked",
      owner: "Codex Sol",
      progress: { done: 1, total: 6 },
      resolvable: true,
    },
    {
      ref: "doc:release-b-plan",
      kind: "doc",
      title: "Release B plan",
      status: "",
      owner: "David Zhang",
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
export const refResolveExampleRequest = refResolveExample.items.map(
  (row) => row.ref,
);
