<script>
  import ReportChart from "./ReportChart.svelte";
  import ReportDetails from "./ReportDetails.svelte";
  let { panel, freshness } = $props();
  let observation = $derived(panel.seriesObservation);
  let showData = $derived(panel.seriesFallback || observation?.status === "ok");
  let provenance = $derived(observation?.provenance);
</script>

{#if panel.seriesFallback}
  <p class="mb-3 text-micro text-warn-text">As of {panel.fallback.as_of}</p>
{:else if freshness === "stale"}
  <p class="mb-3 text-micro text-warn-text">
    Stale since {observation?.stale_since ||
      observation?.fresh_until ||
      panel.observed_at ||
      "the first expected push"}
  </p>
{:else if observation?.status !== "ok"}
  <p class="text-fg-muted">
    {observation?.status === "loading"
      ? "Reading series…"
      : "Live series unavailable"}
  </p>
{/if}
{#if showData}
  {#if panel.type === "chart"}
    <ReportChart data={panel.data} title={panel.title} />
  {:else if panel.type === "metric"}
    <p class="text-title font-semibold">
      {panel.data.value}
      <span class="text-body text-fg-muted">{panel.data.unit ?? ""}</span>
    </p>
  {:else if panel.type === "metric-strip"}
    <ReportDetails {panel} />
  {:else if ["table", "evidence-table"].includes(panel.type)}
    <!-- Keyboard access to horizontally scrollable observations. -->
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
            >{#each panel.data.columns as column}<th scope="col">{column}</th
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
  {:else if panel.type === "metric-chart"}
    <ul>
      {#each panel.data.points as point}<li>
          {point.label}: {point.value}
          {panel.data.unit}
        </li>{/each}
    </ul>
  {/if}
{/if}
<details class="mt-3 text-micro text-fg-muted">
  <summary>Where this number comes from</summary>
  <p>Series: {panel.source.series}</p>
  {#if provenance}
    <p>Adapter: {provenance.adapter} · Host: {provenance.host}</p>
    <p>Last push: {provenance.last_push ?? "No points yet"}</p>
    <p>Resolution: {provenance.resolution}</p>
  {:else}<p>
      Provenance will be available after a successful series read.
    </p>{/if}
</details>
{#if observation?.truncated}<p class="text-micro text-fg-muted">
    Showing a bounded selection. Narrow the labels or range for more detail.
  </p>{/if}
