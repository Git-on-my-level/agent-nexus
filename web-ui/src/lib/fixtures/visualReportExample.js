// Public GitHub metadata read on 2026-10-03. This is a fixed snapshot, not live telemetry.
// The second project is intentionally synthetic and must retain its illustrative label.
export const VISUAL_REPORT_EXAMPLE_OBSERVED_AT = "2026-10-03T06:38:37Z";

const observedAt = VISUAL_REPORT_EXAMPLE_OBSERVED_AT;
const github = "https://github.com/Git-on-my-level";

export const visualReportExample = {
  kind: "anx.visual-report",
  schema_version: 1,
  title: "ANX release and qualification report",
  summary:
    "A public release snapshot with explicit evidence boundaries. Release publication is verified; operational qualification is still unverified. A separate illustrative project demonstrates metric reporting.",
  generated_at: observedAt,
  projects: [
    {
      id: "anx-rollout",
      title: "ANX v0.12.0 release",
      summary:
        "Agent Nexus v0.12.0 is published. Its release notes identify agentctl v0.12.0 as the tested optional identity and skill-pack baseline. A newer agentctl v0.13.0 release is also public.",
      outcome: "Released; operational qualification pending",
    },
    {
      id: "reporting-example",
      title: "Reporting layout example",
      summary:
        "Synthetic values demonstrate a visual report layout. They are not measurements of Agent Nexus, agentctl, or any deployment.",
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
      id: "agentctl-baseline",
      label: "agentctl v0.12.0 compatibility baseline",
      url: `${github}/agentctl/releases/tag/v0.12.0`,
      observed_at: observedAt,
      kind: "release",
    },
    {
      id: "agentctl-latest",
      label: "agentctl v0.13.0 newer public release",
      url: `${github}/agentctl/releases/tag/v0.13.0`,
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
      author: "Public release snapshot",
      provenance: "reported",
      observed_at: observedAt,
      freshness: "current",
      source_ids: ["anx-release", "agentctl-baseline", "agentctl-latest"],
      data: {
        text: "Agent Nexus v0.12.0 and its tested optional agentctl v0.12.0 baseline have stable public releases. Agentctl v0.13.0 was published later; this snapshot does not qualify it for ANX. Publication confirms release availability, not deployed versions, native-session behavior, or operational health. Operational qualification therefore remains pending in this report.",
      },
    },
    {
      id: "release-evidence",
      project_id: "anx-rollout",
      type: "evidence-table",
      title: "Public release evidence",
      author: "GitHub public release metadata",
      provenance: "verified",
      observed_at: observedAt,
      freshness: "current",
      source_ids: [
        "anx-release",
        "agentctl-baseline",
        "agentctl-latest",
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
              "agentctl v0.12.0",
              "Stable release published 2026-10-02 16:17:10 UTC",
              "Optional compatibility baseline identified by ANX release notes",
            ],
            source_ids: ["agentctl-baseline", "anx-release"],
          },
          {
            cells: [
              "agentctl v0.13.0",
              "Stable release published 2026-10-02 17:42:18 UTC",
              "Newer release; not the ANX v0.12.0 qualification baseline",
            ],
            source_ids: ["agentctl-latest", "anx-release"],
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
      author: "Public release snapshot",
      provenance: "reported",
      observed_at: observedAt,
      freshness: "current",
      source_ids: ["agentctl-baseline", "release-preparation", "anx-release"],
      data: {
        items: [
          {
            label: "Optional agentctl baseline published",
            date: "2026-10-02T16:17:10Z",
            status: "complete",
            detail: "agentctl v0.12.0 is a stable public release.",
            source_ids: ["agentctl-baseline"],
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
              "No serving-version, runtime, or operational observation is included in this public snapshot.",
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
      author: "Public release snapshot",
      provenance: "reported",
      observed_at: observedAt,
      freshness: "current",
      source_ids: ["anx-release", "agentctl-baseline"],
      data: {
        nodes: [
          { id: "release", label: "ANX v0.12.0 published", status: "complete" },
          {
            id: "baseline",
            label: "Optional agentctl v0.12.0 baseline",
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
      author: "GitHub public release metadata",
      provenance: "verified",
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
      author: "Public release snapshot",
      provenance: "reported",
      observed_at: null,
      freshness: "unavailable",
      source_ids: [],
      data: {
        text: "This public snapshot has no operational telemetry, native-session test results, or deployment verification. Missing evidence is unavailable, not a healthy or completed result. An authorized operator must supply those observations before qualification can be marked verified.",
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
