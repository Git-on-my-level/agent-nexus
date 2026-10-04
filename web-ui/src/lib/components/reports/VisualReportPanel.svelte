<script>
  import LiveReportPanel from "./LiveReportPanel.svelte";
  import { isLivePanel } from "$lib/liveReports.js";
  import ReportChart from "./ReportChart.svelte";
  import ReportDetails from "./ReportDetails.svelte";
  import ActorLabel from "$lib/components/ActorLabel.svelte";
  import { safeReportUrl } from "$lib/visualReports.js";

  let {
    compact = false,
    panel,
    sources = [],
    freshness,
    evidenceOpen = false,
    oninspect,
  } = $props();
  const typeLabels = {
    "live-initiatives": "Initiatives",
    "live-asks": "Needs an answer",
    "live-work-mix": "Open work",
    "live-activity": "Recent activity",
    chart: "Visualization",
    "metric-strip": "Measures",
    callout: "Decision note",
    comparison: "Comparison",
    explanation: "Context",
    "evidence-table": "Evidence",
    "milestone-timeline": "Milestones",
    "dependency-diagram": "Dependencies",
    "metric-chart": "Metric",
    "artifact-preview": "Artifact",
  };
  const stateLabels = {
    current: "Current snapshot",
    stale: "Stale snapshot",
    unknown: "Freshness unknown",
    unavailable: "Unavailable",
  };
  const provenanceLabels = {
    reported: "Reported",
    verified: "Source-backed",
    illustrative: "Illustrative example",
  };
  let linkedSources = $derived(
    sources.filter((source) => panel.source_ids.includes(source.id)),
  );
  let maximum = $derived(
    panel.type === "metric-chart"
      ? Math.max(1, ...panel.data.points.map((point) => point.value ?? 0))
      : 1,
  );
  function claimsForSource(sourceId) {
    if (panel.type === "evidence-table")
      return panel.data.rows
        .filter((row) => row.source_ids.includes(sourceId))
        .map((row) => row.cells[0]);
    if (panel.type === "milestone-timeline")
      return panel.data.items
        .filter((item) => item.source_ids.includes(sourceId))
        .map((item) => item.label);
    return [];
  }
  const date = (value) =>
    value
      ? new Date(value)
          .toISOString()
          .replace("T", " ")
          .replace(/\.\d{3}Z$/, " UTC")
      : "Unknown";
</script>

<section
  class="report-panel"
  aria-label={panel.title}
  data-report-panel={panel.id}
  data-appearance={panel.appearance ?? "outlined"}
  data-density={panel.density ?? "comfortable"}
>
  <header class="report-panel-header">
    <div class="min-w-0">
      <p class="report-eyebrow">{typeLabels[panel.type]}</p>
      <h3 class="mt-1 text-meta font-semibold text-fg">{panel.title}</h3>
    </div>
    {#if freshness !== "current" && !isLivePanel(panel)}
      <!-- Current is the expected state; only call out evidence that needs care. -->
      <span class="report-state" class:report-state-warn={freshness === "stale"}
        >{stateLabels[freshness]}</span
      >
    {/if}
  </header>

  <div class="report-panel-body">
    {#if isLivePanel(panel)}
      <LiveReportPanel {panel} />
    {:else if freshness === "unavailable"}
      <div class="report-unavailable">
        <span class="text-title text-fg-muted" aria-hidden="true">∅</span>
        <p class="font-medium text-fg">Evidence unavailable</p>
        <p class="mt-1 text-fg-muted">
          This panel cannot establish an outcome. Inspect its sources for
          context.
        </p>
      </div>
    {:else}
      {#if freshness === "stale"}
        <p class="mb-3 text-micro text-warn-text">
          Historical evidence. Refresh the source before acting.
        </p>
      {/if}
      {#if panel.type === "chart"}
        <ReportChart data={panel.data} title={panel.title} />
      {:else if ["metric-strip", "callout", "comparison"].includes(panel.type)}
        <ReportDetails {panel} />
      {:else if panel.type === "explanation"}
        <p class="report-explanation">{panel.data.text}</p>
      {:else if panel.type === "evidence-table"}
        <!-- Keyboard access is required for horizontally scrollable tables. -->
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <div
          class="report-table-scroll"
          tabindex="0"
          role="region"
          aria-label={`${panel.title} table`}
        >
          <table>
            <thead
              ><tr
                >{#each panel.data.columns as column}<th scope="col"
                    >{column}</th
                  >{/each}</tr
              ></thead
            >
            <tbody
              >{#each panel.data.rows as row}<tr
                  >{#each row.cells as cell}<td>{cell}</td>{/each}</tr
                >{/each}</tbody
            >
          </table>
        </div>
      {:else if panel.type === "milestone-timeline"}
        <ol class="report-timeline">
          {#each panel.data.items as item}
            <li>
              <span
                class="report-milestone-dot"
                class:report-milestone-complete={item.status === "complete"}
                aria-hidden="true"
                >{item.status === "complete" ? "✓" : "·"}</span
              >
              <div class="min-w-0">
                <div
                  class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1"
                >
                  <h4 class="font-medium text-fg">{item.label}</h4>
                  <span class="report-status-label">{item.status}</span>
                </div>
                <p class="mt-1 text-micro text-fg-muted">
                  {item.date ?? "Date not established"}
                </p>
                <p class="mt-1 text-meta text-fg-muted">{item.detail}</p>
              </div>
            </li>
          {/each}
        </ol>
      {:else if panel.type === "dependency-diagram"}
        <div class="report-dependency-nodes" aria-label="Dependency nodes">
          {#each panel.data.nodes as node}
            <div
              class="report-dependency-node"
              class:report-dependency-complete={node.status === "complete"}
            >
              <span class="text-micro text-fg-muted">{node.status}</span><strong
                >{node.label}</strong
              >
            </div>
          {/each}
        </div>
        <ul
          class="report-dependency-edges"
          aria-label="Dependency relationships"
        >
          {#each panel.data.edges as edge}
            <li>
              <span
                >{panel.data.nodes.find((node) => node.id === edge.from)
                  ?.label}</span
              ><span class="text-accent-text" aria-label="leads to">
                →
              </span><span
                >{panel.data.nodes.find((node) => node.id === edge.to)
                  ?.label}</span
              ><span class="block text-micro text-fg-muted">{edge.label}</span>
            </li>
          {/each}
        </ul>
      {:else if panel.type === "metric-chart"}
        {#if panel.data.illustrative}<p class="mb-3 text-micro text-warn-text">
            Illustrative values. Not live workspace metrics.
          </p>{/if}
        <p class="mb-3 text-meta font-medium text-fg">
          {panel.data.label}
          <span class="text-micro font-normal text-fg-muted"
            >({panel.data.unit})</span
          >
        </p>
        <div
          class="report-chart"
          role="img"
          aria-label={`${panel.data.label}: ${panel.data.points.map((point) => `${point.label} ${point.value ?? "unknown"}`).join(", ")}. Unit: ${panel.data.unit}`}
        >
          {#each panel.data.points as point}
            <div class="report-chart-row">
              <span>{point.label}</span>
              <div class="report-chart-track">
                {#if point.value !== null && point.value > 0}
                  <div
                    class="report-chart-bar"
                    style:width={`${(point.value / maximum) * 100}%`}
                  ></div>
                {/if}
              </div>
              <strong>{point.value ?? "?"}</strong>
            </div>
          {/each}
        </div>
        {#if panel.data.points.some((point) => point.value === null)}<p
            class="mt-3 text-micro text-fg-muted"
          >
            ? means unavailable, never zero.
          </p>{/if}
      {:else if panel.type === "artifact-preview"}
        <div class="flex flex-wrap items-start justify-between gap-2">
          <h4 class="text-meta font-medium text-fg">{panel.data.label}</h4>
          <span class="font-mono text-micro text-fg-muted"
            >{panel.data.media_type}</span
          >
        </div>
        <pre class="report-artifact-excerpt">{panel.data.excerpt}</pre>
        {#if safeReportUrl(panel.data.url)}<a
            class="text-micro text-accent-text hover:text-accent-hover"
            href={safeReportUrl(panel.data.url)}
            target="_blank"
            rel="noopener noreferrer">Open source artifact ↗</a
          >{/if}
      {/if}
    {/if}
  </div>

  {#if !compact && !isLivePanel(panel)}
    <footer class="report-panel-footer">
      <div class="report-provenance">
        <ActorLabel
          label={panel.author}
          seed={panel.author}
          size="xs"
          nameClass="text-micro text-fg-muted"
        />
        <span class="report-provenance-tag" data-provenance={panel.provenance}
          >{provenanceLabels[panel.provenance]}</span
        >
        <p class="text-micro text-fg-muted">
          {freshness === "current" ? "Current · " : ""}Observed
          <time datetime={panel.observed_at ?? undefined}
            >{date(panel.observed_at)}</time
          >
        </p>
      </div>
      <button
        type="button"
        class="report-evidence-button"
        aria-expanded={evidenceOpen}
        aria-controls={`report-evidence-${panel.id}`}
        onclick={() => oninspect(panel.id)}
        >Inspect evidence <span aria-hidden="true"
          >{evidenceOpen ? "−" : "+"}</span
        ></button
      >
    </footer>
  {/if}
  {#if !compact && evidenceOpen}
    <div id={`report-evidence-${panel.id}`} class="report-evidence-detail">
      <h4 class="text-meta font-medium text-fg">Source evidence</h4>
      <p class="mt-1 text-micro text-fg-muted">
        Author and verification labels are report claims. Review the linked
        records to verify them.
      </p>
      {#if linkedSources.length}
        <ul class="mt-3 space-y-3">
          {#each linkedSources as source}<li>
              <a
                class="text-meta text-accent-text hover:text-accent-hover"
                href={safeReportUrl(source.url)}
                target="_blank"
                rel="noopener noreferrer">{source.label} ↗</a
              >
              <p class="mt-1 text-micro text-fg-muted">
                {source.kind} · Observed {date(source.observed_at)}
              </p>
              {#if claimsForSource(source.id).length}<p
                  class="mt-1 text-micro text-fg-muted"
                >
                  Supports: {claimsForSource(source.id).join("; ")}
                </p>{/if}
            </li>{/each}
        </ul>
      {:else}
        <p class="mt-3 text-meta text-fg-muted">
          No source evidence supplied. This is not a verified outcome.
        </p>
      {/if}
      <p class="mt-3 font-mono text-micro text-fg-muted">Panel {panel.id}</p>
    </div>
  {/if}
</section>

<style>
  .report-panel {
    min-width: 0;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--panel);
    overflow-wrap: anywhere;
  }
  .report-panel[data-appearance="plain"] {
    border-color: transparent;
    background: transparent;
  }
  .report-panel[data-appearance="plain"] .report-panel-header {
    border-bottom: 0;
    padding-left: 0;
    padding-right: 0;
  }
  .report-panel[data-appearance="plain"] .report-panel-body {
    padding-left: 0;
    padding-right: 0;
  }
  .report-panel[data-appearance="plain"] .report-panel-footer {
    padding-left: 0;
    padding-right: 0;
  }
  .report-panel[data-appearance="soft"] {
    background: var(--bg-soft);
    border-color: var(--line-subtle);
  }
  .report-panel[data-density="compact"] .report-panel-header,
  .report-panel[data-density="compact"] .report-panel-body,
  .report-panel[data-density="compact"] .report-panel-footer {
    padding: 10px 12px;
  }
  .report-panel-header {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    padding: 16px;
    border-bottom: 1px solid var(--line-subtle);
  }
  .report-eyebrow {
    color: var(--fg-muted);
    font-size: 10px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    font-weight: 600;
  }
  .report-state {
    border: 1px solid var(--line-strong);
    border-radius: 4px;
    padding: 3px 6px;
    font-size: 10px;
    line-height: 1.4;
    color: var(--fg-muted);
    white-space: nowrap;
  }
  .report-state-warn {
    color: var(--warn-text);
    border-color: var(--warn);
  }
  .report-panel-body {
    padding: 16px;
    flex: 1;
    min-width: 0;
  }
  .report-explanation {
    color: var(--fg);
    font-size: 14px;
    line-height: 1.75;
    white-space: pre-line;
  }
  .report-panel-footer {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 6px 16px;
    padding: 10px 16px;
    border-top: 1px solid var(--line-subtle);
  }
  .report-provenance {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 10px;
    min-width: 0;
  }
  .report-provenance-tag {
    font-size: 10px;
    line-height: 1.4;
    padding: 1px 6px;
    border-radius: 999px;
    border: 1px solid var(--line);
    color: var(--fg-muted);
    white-space: nowrap;
  }
  .report-provenance-tag[data-provenance="illustrative"] {
    border-style: dashed;
  }
  .report-provenance-tag[data-provenance="verified"] {
    border-color: var(--accent-solid);
    color: var(--accent-text);
  }
  .report-evidence-button {
    font-size: 11px;
    color: var(--accent-text);
    display: inline-flex;
    gap: 12px;
    align-items: center;
    padding: 4px 0;
  }
  .report-evidence-detail {
    padding: 16px;
    border-top: 1px solid var(--line);
    background: var(--bg-soft);
    border-radius: 0 0 6px 6px;
  }
  .report-unavailable {
    padding: 24px 12px;
    text-align: center;
  }
  .report-table-scroll {
    overflow-x: auto;
    max-width: 100%;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    text-align: left;
    font-size: 12px;
  }
  th {
    color: var(--fg-muted);
    font-weight: 500;
    padding: 0 12px 10px 0;
    border-bottom: 1px solid var(--line);
  }
  td {
    padding: 12px 12px 12px 0;
    border-bottom: 1px solid var(--line-subtle);
    vertical-align: top;
    min-width: 90px;
  }
  .report-timeline {
    display: grid;
    gap: 20px;
  }
  .report-timeline li {
    display: grid;
    grid-template-columns: 22px minmax(0, 1fr);
    gap: 10px;
    position: relative;
  }
  .report-timeline li:not(:last-child)::after {
    content: "";
    width: 1px;
    position: absolute;
    top: 25px;
    bottom: -15px;
    left: 10px;
    background: var(--line-strong);
  }
  .report-milestone-dot {
    border: 1px solid var(--line-strong);
    border-radius: 50%;
    height: 22px;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--fg-muted);
  }
  .report-milestone-complete {
    border-color: var(--accent-solid);
    background: var(--accent-soft);
    color: var(--accent-text);
    font-size: 11px;
  }
  .report-status-label {
    color: var(--fg-muted);
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }
  .report-dependency-nodes {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(105px, 1fr));
    gap: 10px;
  }
  .report-dependency-node {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    border: 1px solid var(--line-strong);
    border-radius: 4px;
    font-size: 12px;
  }
  .report-dependency-complete {
    border-color: var(--accent-solid);
  }
  .report-dependency-edges {
    margin-top: 16px;
    display: grid;
    gap: 10px;
    font-size: 12px;
  }
  .report-chart {
    display: grid;
    gap: 14px;
  }
  .report-chart-row {
    display: grid;
    grid-template-columns: 70px minmax(0, 1fr) 34px;
    align-items: center;
    gap: 10px;
    font-size: 11px;
    color: var(--fg-muted);
  }
  .report-chart-row strong {
    text-align: right;
    color: var(--fg);
    font-weight: 500;
  }
  .report-chart-track {
    background: var(--bg-soft);
    height: 16px;
    border-radius: 2px;
    overflow: hidden;
  }
  .report-chart-bar {
    background: var(--accent-solid);
    height: 100%;
    border-right: 2px solid var(--accent-text);
  }
  .report-artifact-excerpt {
    margin: 16px 0;
    padding: 12px;
    border-left: 2px solid var(--line-strong);
    background: var(--bg-soft);
    color: var(--fg-muted);
    font-size: 11px;
    line-height: 1.75;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
