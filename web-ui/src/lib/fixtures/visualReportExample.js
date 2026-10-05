// Fictional releases and evidence demonstrate the report contract.
// URLs, dates and values are examples, not observed telemetry.
export const VISUAL_REPORT_EXAMPLE_OBSERVED_AT = "2026-10-03T06:38:37Z";

const observedAt = VISUAL_REPORT_EXAMPLE_OBSERVED_AT;
const github = "https://example.test/releases";

export const visualReportExample = {
  kind: "anx.visual-report",
  schema_version: 1,
  title: "Illustrative release and qualification report",
  summary:
    "A fictional release snapshot demonstrating evidence boundaries. All releases, sources, timestamps and measurements are illustrative; they are not observations of any deployment.",
  generated_at: observedAt,
  projects: [
    {
      id: "anx-rollout",
      title: "ANX v0.12.0 release",
      summary:
        "In this fictional example, the application release is published and an optional runtime baseline is recorded. A newer runtime release still needs qualification.",
      outcome: "Released; operational qualification pending",
    },
    {
      id: "reporting-example",
      title: "Reporting layout example",
      summary:
        "Synthetic values demonstrate a visual report layout. They are not measurements of Agent Nexus, example-runtime, or any deployment.",
      outcome: "Illustrative example only",
    },
  ],
  sources: [
    {
      id: "anx-release",
      label: "Agent Nexus v0.12.0 public release",
      url: `${github}/agent-nexus/releases/tag/v0.12.0`,
      observed_at: observedAt,
      kind: "release",
    },
    {
      id: "example-runtime-baseline",
      label: "example-runtime v0.12.0 compatibility baseline",
      url: `${github}/example-runtime/releases/tag/v0.12.0`,
      observed_at: observedAt,
      kind: "release",
    },
    {
      id: "example-runtime-latest",
      label: "example-runtime v0.13.0 newer public release",
      url: `${github}/example-runtime/releases/tag/v0.13.0`,
      observed_at: observedAt,
      kind: "release",
    },
    {
      id: "release-preparation",
      label: "ANX release preparation PR #233",
      url: `${github}/agent-nexus/pull/233`,
      observed_at: observedAt,
      kind: "pull-request",
    },
  ],
  panels: [
    {
      id: "rollout-summary",
      project_id: "anx-rollout",
      type: "explanation",
      title: "Release outcome",
      author: "Illustrative release snapshot",
      provenance: "illustrative",
      observed_at: observedAt,
      freshness: "current",
      source_ids: [
        "anx-release",
        "example-runtime-baseline",
        "example-runtime-latest",
      ],
      data: {
        text: "Agent Nexus v0.12.0 and its tested optional example-runtime v0.12.0 baseline have stable public releases. Example runtime v0.13.0 was published later; this snapshot does not qualify it for ANX. Publication confirms release availability, not deployed versions, native-session behavior, or operational health. Operational qualification therefore remains pending in this report.",
      },
    },
    {
      id: "release-evidence",
      project_id: "anx-rollout",
      type: "evidence-table",
      title: "Illustrative release evidence",
      author: "Illustrative release metadata",
      provenance: "illustrative",
      observed_at: observedAt,
      freshness: "current",
      source_ids: [
        "anx-release",
        "example-runtime-baseline",
        "example-runtime-latest",
        "release-preparation",
      ],
      data: {
        columns: ["Evidence", "Observed result", "Boundary"],
        rows: [
          {
            cells: [
              "ANX v0.12.0",
              "Stable release published 2026-10-02 17:11:49 UTC",
              "Publication only; no deployment observation",
            ],
            source_ids: ["anx-release"],
          },
          {
            cells: [
              "example-runtime v0.12.0",
              "Stable release published 2026-10-02 16:17:10 UTC",
              "Optional compatibility baseline identified by ANX release notes",
            ],
            source_ids: ["example-runtime-baseline", "anx-release"],
          },
          {
            cells: [
              "example-runtime v0.13.0",
              "Stable release published 2026-10-02 17:42:18 UTC",
              "Newer release; not the ANX v0.12.0 qualification baseline",
            ],
            source_ids: ["example-runtime-latest", "anx-release"],
          },
          {
            cells: [
              "ANX PR #233",
              "Merged 2026-10-02 17:04:33 UTC",
              "Release preparation and acceptance ledger",
            ],
            source_ids: ["release-preparation"],
          },
        ],
      },
    },
    {
      id: "release-milestones",
      project_id: "anx-rollout",
      type: "milestone-timeline",
      title: "Release milestones",
      author: "Illustrative release snapshot",
      provenance: "illustrative",
      observed_at: observedAt,
      freshness: "current",
      source_ids: [
        "example-runtime-baseline",
        "release-preparation",
        "anx-release",
      ],
      data: {
        items: [
          {
            label: "Optional example-runtime baseline published",
            date: "2026-10-02T16:17:10Z",
            status: "complete",
            detail: "example-runtime v0.12.0 is a stable public release.",
            source_ids: ["example-runtime-baseline"],
          },
          {
            label: "ANX release preparation merged",
            date: "2026-10-02T17:04:33Z",
            status: "complete",
            detail:
              "PR #233 prepares v0.12.0 and its adoption acceptance ledger.",
            source_ids: ["release-preparation"],
          },
          {
            label: "ANX v0.12.0 published",
            date: "2026-10-02T17:11:49Z",
            status: "complete",
            detail:
              "The public release includes six platform archives and checksums.txt.",
            source_ids: ["anx-release"],
          },
          {
            label: "Operational qualification",
            date: null,
            status: "unknown",
            detail:
              "No serving-version, runtime, or operational observation is included in this illustrative snapshot.",
            source_ids: [],
          },
        ],
      },
    },
    {
      id: "qualification-dependencies",
      project_id: "anx-rollout",
      type: "dependency-diagram",
      title: "Qualification dependencies",
      author: "Illustrative release snapshot",
      provenance: "illustrative",
      observed_at: observedAt,
      freshness: "current",
      source_ids: ["anx-release", "example-runtime-baseline"],
      data: {
        nodes: [
          { id: "release", label: "ANX v0.12.0 published", status: "complete" },
          {
            id: "baseline",
            label: "Optional example-runtime v0.12.0 baseline",
            status: "complete",
          },
          {
            id: "runtime",
            label: "Runtime and deployment observations",
            status: "unknown",
          },
          {
            id: "qualification",
            label: "Operational qualification",
            status: "pending",
          },
        ],
        edges: [
          {
            from: "release",
            to: "qualification",
            label: "Release prerequisite",
          },
          {
            from: "baseline",
            to: "qualification",
            label: "When optional integration is used",
          },
          {
            from: "runtime",
            to: "qualification",
            label: "Evidence still required",
          },
        ],
      },
    },
    {
      id: "release-artifact",
      project_id: "anx-rollout",
      type: "artifact-preview",
      title: "Release artifact preview",
      author: "Illustrative release metadata",
      provenance: "illustrative",
      observed_at: observedAt,
      freshness: "current",
      source_ids: ["anx-release"],
      data: {
        label: "ANX v0.12.0 release asset inventory",
        media_type: "text/plain",
        excerpt:
          "anx_v0.12.0_darwin_amd64.tar.gz\nanx_v0.12.0_darwin_arm64.tar.gz\nanx_v0.12.0_linux_amd64.tar.gz\nanx_v0.12.0_linux_arm64.tar.gz\nanx_v0.12.0_windows_amd64.zip\nanx_v0.12.0_windows_arm64.zip\nchecksums.txt\n\nAsset names observed in release metadata. Archive bytes and checksums were not independently downloaded or validated for this report.",
        url: `${github}/agent-nexus/releases/tag/v0.12.0`,
      },
    },
    {
      id: "unavailable-observations",
      project_id: "anx-rollout",
      type: "explanation",
      title: "Operational evidence unavailable",
      author: "Illustrative release snapshot",
      provenance: "illustrative",
      observed_at: null,
      freshness: "unavailable",
      source_ids: [],
      data: {
        text: "This illustrative snapshot has no operational telemetry, native-session test results, or deployment verification. Missing evidence is unavailable, not a healthy or completed result. An authorized operator must supply those observations before qualification can be marked verified.",
      },
    },
    {
      id: "example-metrics",
      project_id: "reporting-example",
      type: "metric-chart",
      title: "Illustrative review throughput",
      author: "Example data author",
      provenance: "illustrative",
      observed_at: null,
      freshness: "unknown",
      source_ids: [],
      data: {
        unit: "example items",
        label: "Synthetic completed reviews per example period",
        points: [
          { label: "Period 1", value: 3 },
          { label: "Period 2", value: 5 },
          { label: "Period 3", value: null },
          { label: "Period 4", value: 8 },
        ],
        illustrative: true,
      },
    },
  ],
};

export const visualReportExampleContent = JSON.stringify(
  visualReportExample,
  null,
  2,
);
