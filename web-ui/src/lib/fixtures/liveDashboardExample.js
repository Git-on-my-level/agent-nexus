const panel = (id, type, title, data) => ({
  id,
  project_id: "workspace",
  type,
  title,
  author: "Workspace",
  provenance: "reported",
  observed_at: null,
  freshness: "unknown",
  source_ids: [],
  data,
});

/** Query-only dashboard: no agent-maintained counters or stored live results. */
export const liveDashboardExample = {
  kind: "anx.visual-report",
  schema_version: 1,
  title: "Workspace dashboard",
  summary: "What is in flight, what needs a decision, and what changed.",
  generated_at: "2026-10-01T00:00:00Z",
  projects: [
    {
      id: "workspace",
      title: "Workspace",
      summary: "Current priorities",
      outcome: "Keep the launch moving",
    },
  ],
  sources: [],
  panels: [
    panel("initiatives", "live-initiatives", "In flight", {
      limit: 7,
      sort: "priority",
    }),
    panel("asks", "live-asks", "Needs a decision", {
      limit: 5,
      include_answered: true,
    }),
    panel("mix", "live-work-mix", "Work by phase", { group_by: "phase" }),
    panel("activity", "live-activity", "Recent changes", { limit: 5 }),
    panel("context", "callout", "This week’s focus", {
      tone: "info",
      text: "Ship the launch, then measure adoption. This note is an authored snapshot.",
    }),
  ],
  layout: {
    type: "stack",
    children: [
      {
        type: "grid",
        columns: 2,
        children: [
          { type: "panel", panel_id: "initiatives" },
          { type: "panel", panel_id: "asks" },
        ],
      },
      {
        type: "grid",
        columns: 2,
        children: [
          { type: "panel", panel_id: "mix" },
          { type: "panel", panel_id: "activity" },
        ],
      },
      { type: "panel", panel_id: "context" },
    ],
  },
};
