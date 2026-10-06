<script>
  import { ageTitle, formatAge } from "$lib/ageBadge.js";
  import ReportChart from "./ReportChart.svelte";
  import ReportDetails from "./ReportDetails.svelte";
  let { panel, freshness, now = Date.now() } = $props();
  /**
   * Newest first, as core returns them.
   *
   * Read from the observation rather than from `panel.data`, which
   * `withSeriesObservation` empties for any status but `ok`. A timeline takes
   * no authored fallback, and an irregular stream — releases, deploys — is
   * `stale` between events by definition: core still returns the events it
   * has, and blanking the panel exactly when the last release matters would
   * be the opposite of the point.
   */
  let observation = $derived(panel.seriesObservation);
  let showData = $derived(panel.seriesFallback || observation?.status === "ok");
  let provenance = $derived(observation?.provenance);
  let timeline = $derived(
    observation?.data?.items ?? (showData ? (panel.data?.items ?? []) : []),
  );
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
{#if showData || (panel.type === "live-timeline" && timeline.length)}
  {#if panel.type === "live-timeline"}
    <!--
      Every observation an adapter published, newest first — a release, a
      deploy. Deliberately not binned: a timeline that averaged two deploys in
      the same hour into one row would be hiding the thing it exists to show.
    -->
    {#if timeline.length}
      <ol class="grid gap-2 text-meta">
        {#each timeline as item, index (index)}
          <li
            class="grid grid-cols-[2.5rem_minmax(0,1fr)] items-baseline gap-x-3 border-b border-line-subtle pb-2 last:border-0 last:pb-0"
          >
            <time
              class="text-micro tabular-nums text-fg-muted"
              datetime={item.at}
              title={ageTitle(item.at, "observed", now)}
              >{formatAge(item.at, now)}</time
            >
            <span class="min-w-0 [overflow-wrap:anywhere]"
              >{item.value}{#if item.label}<span class="text-fg-muted">
                  · {item.label}</span
                >{/if}</span
            >
          </li>
        {/each}
      </ol>
    {:else}
      <p class="text-fg-muted">No observations in this range.</p>
    {/if}
  {:else if panel.type === "chart"}
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
