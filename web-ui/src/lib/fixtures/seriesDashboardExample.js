const panel = (id, type, title, source, fallback) => ({
  id,
  type,
  title,
  project_id: "workspace",
  author: "Collector",
  provenance: "reported",
  observed_at: null,
  freshness: "unavailable",
  source_ids: [],
  data: {},
  source,
  ...(fallback ? { fallback } : {}),
});
/** Host-local declarations make external evidence attributable and current. */
export const seriesDashboardExample = {
  kind: "anx.visual-report",
  schema_version: 1,
  title: "Live source dashboard",
  summary:
    "Declared host sources, with visible freshness and snapshot fallbacks.",
  generated_at: "2026-10-05T00:00:00Z",
  sources: [],
  projects: [
    {
      id: "workspace",
      title: "Workspace",
      summary: "Live evidence",
      outcome: "Make decisions from current observations",
    },
  ],
  panels: [
    panel("work", "chart", "Where the work went", {
      series: "github-prs",
      labels: { status: "merged" },
      range: "84d",
      agg: "last",
    }),
    panel(
      "builds",
      "metric",
      "Launch builds",
      { series: "builds", labels: { initiative: "launch" } },
      { as_of: "2026-10-01T00:00:00Z", data: { value: 12, unit: "builds" } },
    ),
    panel("throughput", "metric-strip", "Throughput by initiative", {
      series: "builds",
      range: "24h",
    }),
    panel("history", "table", "Recent observations", {
      series: "builds",
      range: "1h",
    }),
  ],
};
