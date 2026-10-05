<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { onMount } from "svelte";
  import { coreClient } from "$lib/coreClient";
  import { isLivePanel, withLiveObservation } from "$lib/liveReports.js";
  import { getPanelFreshness } from "$lib/visualReports.js";
  import VisualReportPanel from "./VisualReportPanel.svelte";
  import AnxRefPreview from "$lib/components/AnxRefPreview.svelte";
  import {
    collectPageRefs,
    indexResolvedRefs,
    resolveRefsInBatches,
  } from "$lib/refResolve.js";
  import { reportRefStrings } from "./reportRefs.js";
  import ReportLayout from "./ReportLayout.svelte";
  import { layoutPanelIds } from "./reportLayout.js";

  let {
    report,
    documentId = "",
    revisionRef = "",
    compact = false,
    previewObservations = null,
  } = $props();
  let liveObservations = $state(new Map());
  let hasLive = $derived(report.panels.some(isLivePanel));
  let observedPanels = $derived(
    report.panels.map((panel) =>
      withLiveObservation(panel, liveObservations.get(panel.id)),
    ),
  );
  let now = $state(Date.now());

  /**
   * Every ref written anywhere in the report, resolved in one request so chips
   * in table cells, callouts, timelines and diagram nodes render without a
   * fetch each.
   */
  let refPreview = $state();
  let resolvedRefs = $state(new Map());
  $effect(() => {
    const refs = collectPageRefs(reportRefStrings(report?.panels ?? []));
    if (!refs.length) {
      resolvedRefs = new Map();
      return;
    }
    let cancelled = false;
    // Batched: a report may name more refs than one request accepts, and an
    // oversized request is rejected whole.
    void resolveRefsInBatches(refs, (batch) => coreClient.resolveRefs(batch))
      .then((result) => {
        if (!cancelled) resolvedRefs = result;
      })
      .catch(() => {
        // Unresolved refs still render, as "not found" chips.
        if (!cancelled) resolvedRefs = indexResolvedRefs({}, refs);
      });
    return () => {
      cancelled = true;
    };
  });
  const refProps = () => ({
    resolved: resolvedRefs,
    // A report can render outside a workspace route; without a workspace a
    // chip simply does not link rather than throwing.
    organizationSlug: $page?.params?.organization ?? "",
    workspaceSlug: $page?.params?.workspace ?? "",
    onpreview: (model, anchor) => refPreview?.open(model, anchor),
    onpreviewclose: () => refPreview?.requestClose(),
  });
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
    observedPanels.filter(
      (panel) =>
        (compact || project === "all" || panel.project_id === project) &&
        (compact ||
          freshness === "all" ||
          getPanelFreshness(panel, now) === freshness),
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
    observedPanels.filter((panel) => getPanelFreshness(panel, now) === "stale")
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
  // Each definition/reader change starts a fresh read. Ignore late responses
  // after navigation and drop old successful data immediately on a failed refresh.
  $effect(() => {
    const id = documentId;
    const expectedRevision = revisionRef;
    const livePanels = report.panels.filter(isLivePanel);
    liveObservations = new Map();
    if (!livePanels.length) return;
    if (Array.isArray(previewObservations)) {
      liveObservations = new Map(
        previewObservations.map((panel) => [panel.id, panel]),
      );
      return;
    }
    let disposed = false;
    let inFlight = false;
    async function refresh() {
      if (inFlight || disposed) return;
      inFlight = true;
      let results;
      try {
        if (!id) throw new Error("A saved document is required.");
        const response = await coreClient.renderReport(id);
        if (
          !Array.isArray(response?.panels) ||
          (expectedRevision && response.revision_ref !== expectedRevision)
        )
          throw new Error("The report changed. Reload this document.");
        results = new Map(response.panels.map((panel) => [panel.id, panel]));
        for (const panel of livePanels) {
          if (results.get(panel.id)?.type !== panel.type)
            results.set(panel.id, {
              status: "unavailable",
              message: "The report changed. Reload this document.",
              data: {},
            });
        }
      } catch {
        results = new Map(
          livePanels.map((panel) => [
            panel.id,
            {
              status: "unavailable",
              message:
                "Live data unavailable. Reload the document or check your access.",
              data: {},
            },
          ]),
        );
      }
      if (!disposed) liveObservations = results;
      inFlight = false;
    }
    void refresh();
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible") void refresh();
    }, 30_000);
    const resume = () => {
      if (document.visibilityState === "visible") void refresh();
    };
    document.addEventListener("visibilitychange", resume);
    return () => {
      disposed = true;
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", resume);
    };
  });
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
      {#if !compact}<p class="report-kicker">
          Visual report <span>· v{report.schema_version}</span>
        </p>{/if}
      <h2>{report.title}</h2>
      {#if !compact}<p class="report-summary">{report.summary}</p>{/if}
    </div>
    {#if !compact}<div class="report-snapshot">
        <span class="report-snapshot-dot" aria-hidden="true"></span><span
          >{hasLive ? "Live workspace + snapshots" : "Snapshot, not live"}<br
          /><time datetime={report.generated_at}
            >{new Date(report.generated_at)
              .toISOString()
              .slice(0, 16)
              .replace("T", " ")} UTC</time
          ></span
        >
      </div>{/if}
  </header>

  {#if !compact && (!report.layout || report.projects.length > 1)}
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

  {#if !compact}<div class="report-toolbar">
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
    </div>{/if}

  {#if panels.length}
    {#if report.layout}
      <ReportLayout
        {compact}
        node={report.layout}
        {panelsById}
        sources={report.sources}
        {now}
        {evidence}
        {tabSelections}
        oninspect={inspectPanel}
        {...refProps()}
        ontab={(id, value) => setFilter(`reportTab.${id}`, value)}
      />
    {/if}
    {#if remainingPanels.length}
      <div class="report-grid" class:report-layout-remainder={report.layout}>
        {#each remainingPanels as panel (panel.id)}
          <VisualReportPanel
            {compact}
            {panel}
            sources={report.sources}
            freshness={getPanelFreshness(panel, now)}
            evidenceOpen={evidence === panel.id}
            oninspect={inspectPanel}
            {...refProps()}
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
  {#if !compact}<p class="report-footnote">
      {hasLive
        ? "Live panels refresh from workspace data · Authored snapshots retain their observation time"
        : "Agent-assembled report · Source-linked claims · No automatic source refresh"}
    </p>{/if}
</section>

<!-- One preview layer for every chip in this report. -->
<AnxRefPreview bind:this={refPreview} />

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
