const panel = (id, type, title, data, extra = {}) => ({
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
  ...extra,
});

/**
 * A Watch-style dashboard built from collections: initiatives grouped by
 * project, loops that need a look, the agent roster, and loop-scoped
 * activity as a secondary panel. Roles, not layout, say what folds on a phone.
 */
export const collectionDashboardExample = {
  kind: "anx.visual-report",
  schema_version: 1,
  title: "Loops and initiatives",
  summary: "Where every initiative and loop stands, and who is working.",
  generated_at: "2026-10-01T00:00:00Z",
  projects: [
    {
      id: "workspace",
      title: "Workspace",
      summary: "All projects",
      outcome: "Keep loops reporting and initiatives moving",
    },
  ],
  sources: [],
  panels: [
    panel("initiatives", "live-collection", "Initiatives", {
      collection: "initiatives",
      group_by: "project",
      limit: 24,
    }),
    panel("loops", "live-collection", "Loops that need a look", {
      collection: "loops",
      status: ["failing", "stopped_reporting", "flat"],
      group_by: "none",
    }),
    panel("agents", "live-collection", "Agents", {
      collection: "agents",
      group_by: "none",
      limit: 12,
    }),
    panel(
      "changes",
      "live-activity",
      "What loops changed",
      { collection: "loops", limit: 10 },
      { role: "secondary" },
    ),
  ],
};
