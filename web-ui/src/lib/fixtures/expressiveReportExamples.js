// Entirely synthetic showcase data. Never presented as live operational facts.
const observed = "2026-10-03T07:00:00Z";
const base = (title, summary, project) => ({
  kind: "anx.visual-report",
  schema_version: 1,
  title,
  summary: `Illustrative scenario · ${summary} All names, counts and measurements below are synthetic.`,
  generated_at: observed,
  projects: [
    {
      id: project,
      title: project === "swarm" ? "Research swarm" : "Project portfolio",
      summary: "A composed report example using synthetic data.",
      outcome: "Illustrative scenario, not live telemetry",
    },
  ],
  sources: [],
  panels: [],
});
const panel = (project, id, type, title, data) => ({
  id,
  project_id: project,
  type,
  title,
  author: "Reporting showcase",
  provenance: "illustrative",
  observed_at: observed,
  freshness: "current",
  source_ids: [],
  data,
});
const ref = (panel_id, span) => ({
  type: "panel",
  panel_id,
  ...(span ? { span } : {}),
});
const chart = (project, id, title, option, caption, palette = "ocean") =>
  panel(project, id, "chart", title, { option, caption, palette });

export const swarmObservatoryReport = base(
  "Inside a research swarm",
  "Follow the work from intake to evidence-backed decisions, then inspect where capacity gets tight.",
  "swarm",
);
swarmObservatoryReport.panels = [
  panel("swarm", "swarm-pulse", "metric-strip", "The last 24 hours", {
    items: [
      {
        label: "Questions resolved",
        value: "92",
        detail: "+19 against previous illustrative window",
        trend: [52, 58, 55, 64, 69, 73, 92],
        trend_label: "Daily resolved questions · last 7 days",
        tone: "positive",
      },
      {
        label: "Evidence coverage",
        value: "94%",
        detail: "6% still awaiting independent corroboration",
        trend: [81, 82, 87, 84, 89, 91, 94],
        trend_label: "Coverage % · last 7 days",
        tone: "positive",
      },
      {
        label: "Median handoff",
        value: "4.2 min",
        detail: "Down from 6.8 min in prior window",
        trend: [6.8, 7.1, 6.4, 5.3, 5.8, 4.7, 4.2],
        trend_label: "Median minutes · last 7 days",
        tone: "positive",
      },
    ],
  }),
  chart(
    "swarm",
    "handoff-map",
    "How work moves through the team",
    {
      series: [
        {
          type: "sankey",
          name: "Illustrative work handoffs",
          data: [
            { name: "Intake" },
            { name: "Research" },
            { name: "Build" },
            { name: "Verification" },
            { name: "Human review" },
            { name: "Delivered" },
          ],
          links: [
            { source: "Intake", target: "Research", value: 82 },
            { source: "Intake", target: "Build", value: 46 },
            { source: "Research", target: "Verification", value: 66 },
            { source: "Research", target: "Human review", value: 16 },
            { source: "Build", target: "Verification", value: 40 },
            { source: "Build", target: "Human review", value: 6 },
            { source: "Verification", target: "Delivered", value: 92 },
            { source: "Verification", target: "Human review", value: 14 },
          ],
        },
      ],
    },
    "Width encodes handoffs. Human review remains a separate destination, never silently counted as delivered.",
    "forest",
  ),
  panel("swarm", "decision-note", "callout", "Needs a human decision", {
    tone: "warning",
    label: "Choose the next constraint to remove",
    text: "Verification is the shared bottleneck. In this scenario, add a second verifier before increasing intake. Twenty-two direct escalations remain separate from fourteen verification exceptions.",
  }),
  chart(
    "swarm",
    "capacity-map",
    "Capacity by specialty and time",
    {
      xAxis: {
        type: "category",
        data: ["08:00", "10:00", "12:00", "14:00", "16:00", "18:00"],
      },
      yAxis: {
        type: "category",
        data: ["Research", "Build", "Verify", "Synthesize"],
      },
      series: [
        {
          type: "heatmap",
          name: "Utilization %",
          data: [
            [0, 0, 42],
            [1, 0, 68],
            [2, 0, 82],
            [3, 0, 77],
            [4, 0, 61],
            [5, 0, 35],
            [0, 1, 28],
            [1, 1, 51],
            [2, 1, 72],
            [3, 1, 91],
            [4, 1, 84],
            [5, 1, 57],
            [0, 2, 39],
            [1, 2, 63],
            [2, 2, 88],
            [3, 2, 96],
            [4, 2, 94],
            [5, 2, 78],
            [0, 3, 18],
            [1, 3, 33],
            [2, 3, 47],
            [3, 3, 58],
            [4, 3, 71],
            [5, 3, 64],
          ],
        },
      ],
    },
    "Illustrative utilization percentages. Brighter cells indicate higher load, not worse quality.",
    "sunset",
  ),
  chart(
    "swarm",
    "throughput",
    "Throughput and remaining questions",
    {
      xAxis: {
        type: "category",
        data: ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"],
      },
      yAxis: { type: "value", name: "Questions" },
      legend: { show: true },
      series: [
        { type: "bar", name: "Resolved", data: [52, 58, 55, 64, 69, 73, 92] },
        {
          type: "line",
          name: "Remaining",
          data: [112, 105, 109, 98, 84, 68, 42],
          smooth: true,
          markLine: { data: [{ name: "Backlog target", yAxis: 50 }] },
        },
      ],
    },
    "Mixed series make the relationship visible without implying that lower backlog proves better outcomes.",
  ),
  panel("swarm", "rationale", "comparison", "Two ways to unblock the swarm", {
    items: [
      {
        title: "Add a verifier",
        summary: "Address the shared constraint while holding intake steady.",
        verdict: "recommended",
        attributes: [
          { label: "Expected benefit", value: "Shorter review queue" },
          { label: "Tradeoff", value: "More verification capacity" },
          { label: "Reversibility", value: "One-day trial" },
        ],
      },
      {
        title: "Raise intake",
        summary:
          "Increase available work before the review constraint is resolved.",
        verdict: "caution",
        attributes: [
          { label: "Expected benefit", value: "More parallel discovery" },
          { label: "Tradeoff", value: "Larger unverified backlog" },
          { label: "Reversibility", value: "Pause new requests" },
        ],
      },
    ],
  }),
];
swarmObservatoryReport.layout = {
  type: "stack",
  children: [
    ref("swarm-pulse"),
    {
      type: "section",
      title: "The coordination picture",
      description:
        "Start with the flow, then decide where intervention will matter.",
      children: [
        {
          type: "grid",
          columns: 3,
          children: [ref("handoff-map", 2), ref("decision-note")],
        },
      ],
    },
    {
      type: "tabs",
      id: "swarm-detail",
      items: [
        {
          id: "capacity",
          label: "Capacity & flow",
          children: [
            {
              type: "grid",
              columns: 2,
              children: [ref("capacity-map"), ref("throughput")],
            },
          ],
        },
        {
          id: "decision",
          label: "Decision brief",
          children: [ref("rationale")],
        },
      ],
    },
  ],
};

export const portfolioReviewReport = base(
  "Invest where the next hour matters",
  "A portfolio review connects resource allocation, quality and cycle time. Compare options without hiding tradeoffs.",
  "portfolio",
);
portfolioReviewReport.panels = [
  panel("portfolio", "portfolio-note", "callout", "Portfolio thesis", {
    tone: "info",
    label: "Illustrative planning brief",
    text: "Atlas carries the largest share of effort, while Beacon offers the cleanest near-term quality gain. Keep the allocation decision separate from the measured evidence.",
  }),
  chart(
    "portfolio",
    "effort-tree",
    "Where the effort went",
    {
      series: [
        {
          type: "treemap",
          name: "Illustrative agent-hours",
          data: [
            {
              name: "Atlas",
              children: [
                { name: "Research", value: 142 },
                { name: "Build", value: 208 },
                { name: "Verify", value: 96 },
              ],
            },
            {
              name: "Beacon",
              children: [
                { name: "Research", value: 88 },
                { name: "Build", value: 112 },
                { name: "Verify", value: 74 },
              ],
            },
            {
              name: "Cedar",
              children: [
                { name: "Research", value: 56 },
                { name: "Build", value: 91 },
                { name: "Verify", value: 63 },
              ],
            },
          ],
        },
      ],
    },
    "Area encodes synthetic agent-hours, grouped by project and activity. Resource share is not a measure of project value.",
    "forest",
  ),
  chart(
    "portfolio",
    "quality-frontier",
    "Quality versus cycle time",
    {
      xAxis: {
        type: "value",
        name: "Cycle time",
        scale: true,
        axisLabel: { formatter: "{value} h" },
      },
      yAxis: { type: "value", name: "Quality score", min: 60, max: 100 },
      legend: { show: true },
      series: [
        {
          type: "scatter",
          name: "Atlas",
          symbolSize: 12,
          data: [
            [6.2, 78],
            [5.9, 82],
            [5.2, 88],
            [4.8, 91],
          ],
        },
        {
          type: "scatter",
          name: "Beacon",
          symbolSize: 12,
          markLine: { data: [{ name: "Quality bar", yAxis: 85 }] },
          data: [
            [3.4, 80],
            [3.2, 86],
            [2.8, 93],
            [2.6, 96],
          ],
        },
        {
          type: "scatter",
          name: "Cedar",
          symbolSize: 12,
          data: [
            [4.2, 72],
            [4.7, 76],
            [4.1, 83],
            [3.8, 87],
          ],
        },
      ],
    },
    "Four synthetic observations per project. Position shows tradeoffs; it does not establish causation.",
    "categorical",
  ),
  chart(
    "portfolio",
    "mix-chart",
    "The weekly work mix",
    {
      xAxis: { type: "category", data: ["W1", "W2", "W3", "W4", "W5", "W6"] },
      yAxis: { type: "value", name: "Agent-hours" },
      legend: { show: true },
      series: [
        {
          type: "bar",
          name: "Research",
          stack: "effort",
          data: [78, 66, 59, 45, 38, 31],
        },
        {
          type: "bar",
          name: "Build",
          stack: "effort",
          data: [64, 72, 89, 108, 102, 91],
        },
        {
          type: "bar",
          name: "Verify",
          stack: "effort",
          data: [22, 28, 37, 49, 58, 64],
        },
      ],
    },
    "Stacked series show the growing verification investment as the synthetic portfolio matures.",
  ),
  chart(
    "portfolio",
    "dependency-orbit",
    "Shared work and dependencies",
    {
      series: [
        {
          type: "graph",
          name: "Illustrative dependency graph",
          layout: "circular",
          categories: [{ name: "Stage" }, { name: "Project" }],
          data: [
            { id: "research", name: "Research", symbolSize: 30, category: 0 },
            { id: "atlas", name: "Atlas", symbolSize: 37, category: 1 },
            { id: "beacon", name: "Beacon", symbolSize: 29, category: 1 },
            { id: "cedar", name: "Cedar", symbolSize: 26, category: 1 },
            {
              id: "verification",
              name: "Verification",
              symbolSize: 35,
              category: 0,
            },
            { id: "delivery", name: "Delivery", symbolSize: 25, category: 0 },
          ],
          links: [
            { source: "research", target: "atlas" },
            { source: "research", target: "beacon" },
            { source: "research", target: "cedar" },
            { source: "atlas", target: "verification" },
            { source: "beacon", target: "verification" },
            { source: "cedar", target: "verification" },
            { source: "verification", target: "delivery" },
          ],
        },
      ],
    },
    "Directed relationships are explicit. Node size is illustrative emphasis, not measured health.",
    "sunset",
  ),
  panel(
    "portfolio",
    "allocation-choice",
    "comparison",
    "Choose the next allocation",
    {
      items: [
        {
          title: "Focus Beacon",
          summary:
            "Concentrate a short iteration on the strongest quality/cycle-time tradeoff.",
          verdict: "recommended",
          attributes: [
            { label: "Next investment", value: "Verification + synthesis" },
            { label: "Decision horizon", value: "One iteration" },
            { label: "Review trigger", value: "Quality gain stalls" },
          ],
        },
        {
          title: "Rebalance Atlas",
          summary:
            "Reduce expensive discovery and validate the largest existing investment.",
          verdict: "neutral",
          attributes: [
            { label: "Next investment", value: "Evidence review" },
            { label: "Decision horizon", value: "Two iterations" },
            { label: "Review trigger", value: "Review queue grows" },
          ],
        },
      ],
    },
  ),
  panel("portfolio", "methodology", "explanation", "How to read this example", {
    text: "Every data point in this report is invented to demonstrate the reporting grammar. The treemap, scatterplot, stacked series and relationship graph answer different questions. Charts are locally rendered from the stored document snapshot. They do not fetch source data or execute agent-written code. Expand a chart’s data view for its accessible values, and inspect panel evidence for authorship and observation time.",
  }),
];
portfolioReviewReport.layout = {
  type: "stack",
  children: [
    ref("portfolio-note"),
    {
      type: "section",
      title: "Allocation × outcomes",
      description: "Two complementary views of the same synthetic portfolio.",
      children: [
        {
          type: "grid",
          columns: 2,
          children: [ref("effort-tree"), ref("quality-frontier")],
        },
      ],
    },
    {
      type: "grid",
      columns: 3,
      children: [ref("mix-chart", 2), ref("dependency-orbit")],
    },
    ref("allocation-choice"),
    {
      type: "disclosure",
      title: "Methodology & evidence boundaries",
      children: [ref("methodology")],
    },
  ],
};

// App-owned surface tokens let composition vary without raw CSS.
for (const report of [swarmObservatoryReport, portfolioReviewReport]) {
  for (const item of report.panels) {
    if (["metric-strip", "comparison"].includes(item.type))
      item.appearance = "plain";
    if (item.type === "callout") item.appearance = "soft";
  }
}
