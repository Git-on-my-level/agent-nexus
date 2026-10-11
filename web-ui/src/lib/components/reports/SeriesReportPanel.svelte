<script>
  import InfoTip from "$lib/components/InfoTip.svelte";
  import Time from "$lib/time/Time.svelte";
  import { formatTime } from "$lib/time/format.js";
  import { seriesChartData } from "$lib/seriesChart.js";
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
  let chartData = $derived(seriesChartData(panel, { now }));
  /*
   * One sentence, not four lines: which adapter pushes this series, from
   * where, how often, and when it last arrived.
   */
  let sourceTip = $derived(
    provenance
      ? `Pushed by ${provenance.adapter || "an adapter"} on ${provenance.host || "an unnamed host"}, ${provenance.resolution || "raw"} resolution. Last point ${formatTime(provenance.last_push, { now, style: "exact" }) || "not received yet"}.`
      : "This panel reads a published series. Its adapter and host appear after the first successful read.",
  );
  let timeline = $derived(
    observation?.status === "stale"
      ? (observation.data?.items ?? [])
      : showData
        ? (panel.data?.items ?? [])
        : [],
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
            class="grid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-x-3 border-b border-line-subtle pb-2 last:border-0 last:pb-0"
          >
            <Time
              value={item.at}
              {now}
              verb="observed"
              class="text-micro text-fg-muted"
            />
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
    <!--
      Core can only materialize one line per label set on a time axis. The
      panel's own declaration — bars, a stack, readable series names — lives
      in its authored fallback, and this applies it to the live read.
    -->
    <ReportChart data={chartData} title={panel.title} />
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
<!--
  Where the numbers come from, as a glyph rather than a fold. The reader
  needs the adapter and the host about once; the fold spent a line on
  `▶ Where this number comes from` under every panel to offer it.
-->
<p class="mt-2 flex items-center gap-1.5 text-micro text-fg-subtle">
  {panel.source.series}<InfoTip
    label="Where this number comes from"
    text={sourceTip}
  />
</p>
{#if observation?.truncated}<p class="text-micro text-fg-muted">
    Showing a bounded selection. Narrow the labels or range for more detail.
  </p>{/if}
