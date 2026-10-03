<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { onMount } from "svelte";
  import { getPanelFreshness } from "$lib/visualReports.js";
  import VisualReportPanel from "./VisualReportPanel.svelte";
  import ReportLayout from "./ReportLayout.svelte";
  import { layoutPanelIds } from "./reportLayout.js";

  let { report } = $props();
  let now = $state(Date.now());
  const freshnessOptions = [
    "all",
    "current",
    "stale",
    "unknown",
    "unavailable",
  ];
  let project = $derived(
    report.projects.some(
      (item) => item.id === $page.url.searchParams.get("reportProject"),
    )
      ? $page.url.searchParams.get("reportProject")
      : "all",
  );
  let freshness = $derived(
    freshnessOptions.includes($page.url.searchParams.get("reportFreshness"))
      ? $page.url.searchParams.get("reportFreshness")
      : "all",
  );
  let evidence = $derived($page.url.searchParams.get("reportEvidence") ?? "");
  let panels = $derived(
    report.panels.filter(
      (panel) =>
        (project === "all" || panel.project_id === project) &&
        (freshness === "all" || getPanelFreshness(panel, now) === freshness),
    ),
  );
  let panelsById = $derived(new Map(panels.map((panel) => [panel.id, panel])));
  let referencedPanels = $derived(layoutPanelIds(report.layout));
  let remainingPanels = $derived(
    report.layout
      ? panels.filter((panel) => !referencedPanels.has(panel.id))
      : panels,
  );
  let tabSelections = $derived(
    new Map(
      [...$page.url.searchParams.entries()]
        .filter(([key]) => key.startsWith("reportTab."))
        .map(([key, value]) => [key.slice("reportTab.".length), value]),
    ),
  );
  let staleCount = $derived(
    report.panels.filter((panel) => getPanelFreshness(panel, now) === "stale")
      .length,
  );

  function setFilter(key, value) {
    const url = new URL($page.url);
    if (!value || (value === "all" && !key.startsWith("reportTab.")))
      url.searchParams.delete(key);
    else url.searchParams.set(key, value);
    if (key === "reportProject" || key === "reportFreshness")
      url.searchParams.delete("reportEvidence");
    void goto(url, { noScroll: true, keepFocus: true });
  }
  function inspectPanel(id) {
    setFilter("reportEvidence", evidence === id ? "" : id);
  }
  onMount(() => {
    const timer = window.setInterval(() => {
      now = Date.now();
    }, 60_000);
    return () => window.clearInterval(timer);
  });
</script>

<section class="visual-report" aria-label="Visual report">
  <header class="report-heading">
    <div>
      <p class="report-kicker">
        Visual report <span>· v{report.schema_version}</span>
      </p>
      <h2>{report.title}</h2>
      <p class="report-summary">{report.summary}</p>
    </div>
    <div class="report-snapshot">
      <span class="report-snapshot-dot" aria-hidden="true"></span><span
        >Snapshot, not live<br /><time datetime={report.generated_at}
          >{new Date(report.generated_at)
            .toISOString()
            .slice(0, 16)
            .replace("T", " ")} UTC</time
        ></span
      >
    </div>
  </header>

  {#if !report.layout || report.projects.length > 1}
    <div class="report-projects" aria-label="Project overview">
      {#each report.projects as item}
        <button
          type="button"
          class="report-project"
          class:report-project-selected={project === item.id}
          aria-pressed={project === item.id}
          onclick={() =>
            setFilter("reportProject", project === item.id ? "all" : item.id)}
        >
          <span class="report-project-name"
            >{item.title}<span aria-hidden="true">↗</span></span
          ><strong>{item.outcome}</strong><span class="report-project-summary"
            >{item.summary}</span
          >
        </button>
      {/each}
    </div>
  {/if}

  <div class="report-toolbar">
    <div class="flex flex-wrap items-center gap-3">
      <button
        type="button"
        class="report-all"
        aria-pressed={project === "all"}
        onclick={() => setFilter("reportProject", "all")}>All projects</button
      >
      <p class="text-micro text-fg-muted" aria-live="polite">
        {panels.length} of {report.panels.length} panels{staleCount
          ? ` · ${staleCount} stale`
          : ""}
      </p>
    </div>
    <label class="report-filter-label"
      >Freshness <select
        aria-label="Filter by freshness"
        value={freshness}
        onchange={(event) =>
          setFilter("reportFreshness", event.currentTarget.value)}
        >{#each freshnessOptions as option}<option value={option}
            >{option === "all"
              ? "All evidence"
              : option.charAt(0).toUpperCase() + option.slice(1)}</option
          >{/each}</select
      ></label
    >
  </div>

  {#if panels.length}
    {#if report.layout}
      <ReportLayout
        node={report.layout}
        {panelsById}
        sources={report.sources}
        {now}
        {evidence}
        {tabSelections}
        oninspect={inspectPanel}
        ontab={(id, value) => setFilter(`reportTab.${id}`, value)}
      />
    {/if}
    {#if remainingPanels.length}
      <div class="report-grid" class:report-layout-remainder={report.layout}>
        {#each remainingPanels as panel (panel.id)}
          <VisualReportPanel
            {panel}
            sources={report.sources}
            freshness={getPanelFreshness(panel, now)}
            evidenceOpen={evidence === panel.id}
            oninspect={inspectPanel}
          />
        {/each}
      </div>
    {/if}
  {:else}
    <div class="report-empty">
      <h3>No panels match these filters</h3>
      <p>Choose another project or freshness state to inspect the evidence.</p>
      <button type="button" onclick={() => setFilter("reportFreshness", "all")}
        >Show all freshness states</button
      >
    </div>
  {/if}
  <p class="report-footnote">
    Agent-assembled report · Source-linked claims · No automatic source refresh
  </p>
</section>

<style>
  .visual-report {
    container: visual-report / inline-size;
    min-width: 0;
    overflow-wrap: anywhere;
    color: var(--fg);
  }
  .report-heading {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    align-items: flex-start;
    gap: 20px;
    padding-bottom: 24px;
  }
  .report-heading > div:first-child {
    flex: 1;
    min-width: min(100%, 240px);
  }
  .report-kicker {
    color: var(--accent-text);
    text-transform: uppercase;
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.14em;
  }
  .report-kicker span {
    color: var(--fg-muted);
  }
  .report-heading h2 {
    font-size: 25px;
    font-weight: 600;
    line-height: 1.25;
    letter-spacing: -0.04em;
    margin-top: 10px;
    overflow-wrap: anywhere;
  }
  .report-summary {
    color: var(--fg-muted);
    font-size: 12px;
    line-height: 1.7;
    max-width: 650px;
    margin-top: 10px;
  }
  .report-snapshot {
    display: flex;
    gap: 8px;
    color: var(--fg-muted);
    font-size: 10px;
    line-height: 1.8;
    padding-top: 2px;
  }
  .report-snapshot-dot {
    width: 6px;
    height: 6px;
    border: 1px solid var(--fg-muted);
    border-radius: 50%;
    margin-top: 6px;
  }
  .report-projects {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px;
  }
  .report-project {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    text-align: left;
    gap: 10px;
    padding: 16px;
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--bg-soft);
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .report-project:hover {
    background: var(--panel-hover);
  }
  .report-project-selected {
    border-color: var(--accent);
  }
  .report-project-name {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    width: 100%;
    color: var(--fg-muted);
    font-size: 11px;
  }
  .report-project strong {
    font-size: 17px;
    letter-spacing: -0.025em;
    font-weight: 500;
  }
  .report-project-summary {
    font-size: 11px;
    line-height: 1.6;
    color: var(--fg-muted);
  }
  .report-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 12px;
    padding: 20px 0 12px;
  }
  .report-all {
    border: 1px solid var(--line);
    border-radius: 4px;
    padding: 6px 10px;
    font-size: 11px;
  }
  .report-all[aria-pressed="true"] {
    background: var(--panel);
    border-color: var(--line-strong);
  }
  .report-filter-label {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--fg-muted);
    font-size: 11px;
  }
  .report-filter-label select {
    background: var(--bg-soft);
    border: 1px solid var(--line);
    color: var(--fg);
    border-radius: 4px;
    padding: 6px 24px 6px 8px;
  }
  .report-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 16px;
    align-items: start;
  }
  .report-layout-remainder {
    margin-top: 24px;
  }
  .report-footnote {
    text-align: center;
    color: var(--fg-muted);
    font-size: 10px;
    padding: 20px 0 4px;
  }
  .report-empty {
    padding: 40px 16px;
    border: 1px dashed var(--line-strong);
    text-align: center;
    color: var(--fg-muted);
  }
  .report-empty h3 {
    color: var(--fg);
    font-weight: 500;
  }
  .report-empty p {
    margin: 8px 0 16px;
    font-size: 12px;
  }
  .report-empty button {
    color: var(--accent-text);
    font-size: 12px;
  }
  @media (max-width: 800px) {
    .report-grid {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  @media (max-width: 520px) {
    .report-projects {
      grid-template-columns: minmax(0, 1fr);
    }
    .report-heading h2 {
      font-size: 22px;
    }
    .report-heading {
      gap: 12px;
    }
  }
</style>
