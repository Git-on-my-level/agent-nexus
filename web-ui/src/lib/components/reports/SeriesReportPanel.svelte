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
  let sourceTip = $derived(isPivot ? pivotTip : defaultTip(provenance));
  let timeline = $derived(
    observation?.status === "stale"
      ? (observation.data?.items ?? [])
      : showData
        ? (panel.data?.items ?? [])
        : [],
  );
  /**
   * The pivoted table: one row per label set, one column per series, each
   * series cell carrying its own observation time and staleness. Rows arrive
   * sorted by their row keys; the reader can re-sort any column.
   */
  let isPivot = $derived(panel.type === "live-table");
  let pivotColumns = $derived(isPivot ? (panel.data?.columns ?? []) : []);
  let pivotMore = $derived(isPivot ? (panel.data?.more ?? 0) : 0);
  let sortColumn = $state(null);
  let sortDirection = $state("asc");
  let sortedRows = $derived.by(() => {
    const rows = panel.data?.rows ?? [];
    if (!isPivot || sortColumn === null) return rows;
    const direction = sortDirection === "desc" ? -1 : 1;
    return [...rows].sort((a, b) => {
      const left = a.cells?.[sortColumn]?.text ?? "";
      const right = b.cells?.[sortColumn]?.text ?? "";
      const leftNumber = Number(left);
      const rightNumber = Number(right);
      const numeric =
        left !== "" &&
        right !== "" &&
        Number.isFinite(leftNumber) &&
        Number.isFinite(rightNumber);
      const order = numeric
        ? leftNumber - rightNumber
        : left.localeCompare(right);
      return order * direction;
    });
  });
  function toggleSort(index) {
    if (sortColumn === index) {
      sortDirection = sortDirection === "asc" ? "desc" : "asc";
    } else {
      sortColumn = index;
      sortDirection = "asc";
    }
  }
  /** A cell with no observation in range reads as an em dash, never zero. */
  const cellText = (cell) => (cell?.text ? cell.text : "—");
  const cellTitle = (cell) =>
    cell?.observed_at
      ? `Last observed ${formatTime(cell.observed_at, { now, style: "exact" })}`
      : "";
  let sourceSeries = $derived(
    (panel.source?.columns ?? []).map((column) => column.series).join(" · "),
  );
  function defaultTip(provenance) {
    return provenance
      ? `Pushed by ${provenance.adapter || "an adapter"} on ${provenance.host || "an unnamed host"}, ${provenance.resolution || "raw"} resolution. Last point ${formatTime(provenance.last_push, { now, style: "exact" }) || "not received yet"}.`
      : "This panel reads a published series. Its adapter and host appear after the first successful read.";
  }
  let pivotTip = $derived.by(() => {
    const entries = Array.isArray(provenance) ? provenance : [];
    if (!entries.length)
      return "This panel reads published series. Their adapters and hosts appear after the first successful read.";
    return entries
      .map(
        (entry) =>
          `${entry.series || "A series"} is pushed by ${entry.adapter || "an adapter"} on ${entry.host || "an unnamed host"}, ${entry.resolution || "raw"} resolution. Last point ${formatTime(entry.last_push, { now, style: "exact" }) || "not received yet"}.`,
      )
      .join("\n");
  });
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
      : observation?.message || "Live series unavailable"}
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
  {:else if isPivot}
    <!--
      One row per label set: the pivoted answer to "what is the state of every
      loop, host or initiative" that a Time/Series/Value point table turns into
      soup. Every series cell says how old its own value is, so one quiet
      publisher never blurs into the rows that are fine.
    -->
    <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
    <div
      class="pivot-table-scroll"
      tabindex="0"
      role="region"
      aria-label={`${panel.title} table`}
    >
      <table class="pivot-table">
        <thead>
          <tr>
            {#each pivotColumns as column, index (index)}
              <th scope="col">
                <button
                  type="button"
                  class="pivot-sort"
                  aria-label={`Sort by ${column.label}`}
                  onclick={() => toggleSort(index)}
                >
                  {column.label}<span class="pivot-arrow" aria-hidden="true"
                    >{sortColumn === index
                      ? sortDirection === "asc"
                        ? " ↑"
                        : " ↓"
                      : ""}</span
                  >
                </button>
              </th>
            {/each}
          </tr>
        </thead>
        <tbody>
          {#each sortedRows as row, index (index)}
            <tr>
              {#each row.cells ?? [] as cell, cellIndex (cellIndex)}
                <td>
                  {#if cellIndex < pivotColumns.length && pivotColumns[cellIndex].kind === "series"}
                    <span class="pivot-value">{cellText(cell)}</span>
                    {#if cell?.stale}<span
                        class="pivot-stale"
                        title={`${cellTitle(cell)} — past this series' expected interval`}
                        >stale</span
                      >{/if}
                  {:else}{cellText(cell)}{/if}
                </td>
              {/each}
            </tr>
          {/each}
        </tbody>
        {#if pivotMore > 0}
          <tfoot>
            <tr>
              <td colspan={pivotColumns.length}>
                {pivotMore} more rows not shown — narrow the labels or range to see
                them.
              </td>
            </tr>
          </tfoot>
        {/if}
      </table>
    </div>
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
  {#if isPivot}{sourceSeries}{:else}{panel.source.series}{/if}<InfoTip
    label="Where this number comes from"
    text={sourceTip}
  />
</p>
{#if observation?.truncated}<p class="text-micro text-fg-muted">
    Showing a bounded selection. Narrow the labels or range for more detail.
  </p>{/if}

<style>
  .pivot-table-scroll {
    overflow-x: auto;
    max-width: 100%;
  }
  .pivot-table {
    width: 100%;
    border-collapse: collapse;
    text-align: left;
    font-size: 12px;
  }
  .pivot-table th {
    color: var(--fg-muted);
    font-weight: 500;
    padding: 0 12px 10px 0;
    border-bottom: 1px solid var(--line);
    white-space: nowrap;
  }
  .pivot-table td {
    padding: 10px 12px 10px 0;
    border-bottom: 1px solid var(--line-subtle);
    vertical-align: top;
    min-width: 90px;
    overflow-wrap: anywhere;
  }
  .pivot-table tfoot td {
    border-bottom: 0;
    color: var(--fg-muted);
    font-size: 11px;
  }
  .pivot-sort {
    font: inherit;
    color: inherit;
    padding: 0;
  }
  .pivot-arrow {
    color: var(--accent-text);
  }
  .pivot-value {
    white-space: nowrap;
  }
  /*
   * A stale cell is a word, not a colour alone, and it never hides the value
   * it qualifies: the reader decides how much a quiet publisher's last
   * observation is still worth.
   */
  .pivot-stale {
    margin-left: 6px;
    border: 1px solid var(--warn);
    border-radius: 4px;
    padding: 1px 5px;
    font-size: 10px;
    line-height: 1.4;
    color: var(--warn-text);
    white-space: nowrap;
  }
</style>
