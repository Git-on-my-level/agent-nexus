<script>
  import { onMount } from "svelte";
  import {
    buildTooltipElement,
    previousPointValues,
    reportLegendModel,
    reportTooltipModel,
  } from "$lib/reportChartInteraction.js";
  import {
    buildReportChartOption,
    reportChartRows,
  } from "$lib/visualReportCharts.js";

  let { data, title = "Chart" } = $props();
  let host = $state();
  let renderer = $state(null);
  let failed = $state(false);
  let ready = $state(false);
  let appearance = $state({});
  let chart = $state(null);
  const option = $derived(buildReportChartOption(data, appearance));
  const table = $derived(reportChartRows(data));

  /**
   * The legend lives here rather than inside the canvas: it has to wrap instead
   * of paginating, its items have to be real buttons, and a click pins a series
   * instead of hiding it.
   */
  const legend = $derived(reportLegendModel(data, appearance.bg));

  /** Pinned stays until clicked again; hovered is transient. */
  let pinnedKey = $state("");
  let hoveredKey = $state("");
  let activeKey = $derived(hoveredKey || pinnedKey);

  /**
   * Which series the cursor is nearest, so the tooltip can lead with it. An
   * axis tooltip covers every series at once and ECharts does not say which one
   * the pointer is closest to, so we track it from the series' own hover
   * events; `reportTooltipModel` falls back to the largest value when nothing
   * has been hovered.
   */
  let hoveredSeriesIndex = $state(null);

  function tooltipFormatter(params) {
    const points = Array.isArray(params) ? params : [params];
    const model = reportTooltipModel({
      points,
      hoveredSeriesIndex,
      previous: previousPointValues(data?.option?.series, points[0]?.dataIndex),
    });
    return buildTooltipElement(model, appearance) ?? "";
  }

  /**
   * Highlight the active series and mute the others. With nothing active every
   * series is downplayed back to its normal state, so unpinning restores the
   * chart rather than leaving a series stuck emphasised.
   */
  $effect(() => {
    const instance = chart;
    const key = activeKey;
    if (!instance || !legend.show) return;
    for (const item of legend.items) {
      const target = item.dataName
        ? { seriesIndex: item.seriesIndex, name: item.dataName }
        : { seriesIndex: item.seriesIndex };
      instance.dispatchAction({
        type: key && item.key === key ? "highlight" : "downplay",
        ...target,
      });
    }
  });

  function togglePin(key) {
    pinnedKey = pinnedKey === key ? "" : key;
  }

  onMount(() => {
    let disposed = false;
    const readAppearance = () => {
      if (!host) return;
      const style = getComputedStyle(host);
      appearance = Object.fromEntries(
        Object.entries({
          fg: "--fg",
          muted: "--fg-muted",
          line: "--line",
          strong: "--line-strong",
          panel: "--panel",
          bg: "--bg",
        }).map(([key, token]) => [key, style.getPropertyValue(token).trim()]),
      );
    };
    if (host) readAppearance();
    const themeObserver = new MutationObserver(readAppearance);
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class", "style", "data-theme"],
    });
    import("./reportChartRenderer.js")
      .then((module) => {
        if (!disposed) renderer = module;
      })
      .catch(() => {
        if (!disposed) failed = true;
      });
    return () => {
      disposed = true;
      themeObserver.disconnect();
    };
  });

  $effect(() => {
    if (!host || !renderer || !option) return;
    let instance;
    let observer;
    const element = host;
    const resize = () => {
      if (element.clientWidth > 0)
        instance?.resize({
          width: element.clientWidth,
          height: element.clientHeight || 320,
        });
    };
    try {
      instance = renderer.createReportChart(element);
      // The formatter is attached here rather than in `buildReportChartOption`
      // so that function stays pure and serializable for its tests.
      instance.setOption(
        {
          ...option,
          tooltip: { ...option.tooltip, formatter: tooltipFormatter },
        },
        { notMerge: true },
      );
      instance.on("mouseover", (event) => {
        hoveredSeriesIndex = Number.isInteger(event?.seriesIndex)
          ? event.seriesIndex
          : null;
      });
      instance.on("globalout", () => {
        hoveredSeriesIndex = null;
      });
      if (typeof ResizeObserver !== "undefined") {
        observer = new ResizeObserver(resize);
        observer.observe(element);
      }
      window.addEventListener("resize", resize);
      failed = false;
      ready = true;
      chart = instance;
    } catch {
      failed = true;
      ready = false;
      instance?.dispose();
      instance = null;
      chart = null;
    }
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", resize);
      instance?.dispose();
      chart = null;
    };
  });
</script>

<figure data-report-chart class="report-chart" aria-label={title}>
  {#if option}
    <div
      bind:this={host}
      class="chart-surface"
      class:chart-unavailable={failed}
      role="img"
      aria-label={`${title}. Chart values are available in the data table below.`}
    ></div>
    {#if failed}
      <p class="chart-notice" role="status">
        The chart could not be displayed. Its values are available below.
      </p>
    {:else if !ready}
      <p class="chart-notice">
        Preparing chart. The data table is available below.
      </p>
    {/if}
    {#if legend.show && !failed}
      <!--
        Wraps rather than paginates, so no series is hidden behind a pager.
        Clicking pins a series; hovering highlights it transiently. Neither
        hides anything, which is what the built-in legend's click did.
      -->
      <ul class="chart-legend" aria-label={`${title} series`}>
        {#each legend.items as item (item.key)}
          <li>
            <button
              type="button"
              class="chart-legend__item"
              class:chart-legend__item--pinned={pinnedKey === item.key}
              aria-pressed={pinnedKey === item.key}
              onclick={() => togglePin(item.key)}
              onmouseenter={() => (hoveredKey = item.key)}
              onmouseleave={() => (hoveredKey = "")}
              onfocus={() => (hoveredKey = item.key)}
              onblur={() => (hoveredKey = "")}
            >
              <span
                class="chart-legend__swatch"
                style:background={item.color}
                aria-hidden="true"
              ></span>
              <span class="chart-legend__name">{item.name}</span>
            </button>
          </li>
        {/each}
      </ul>
    {/if}
    {#if data.caption}<figcaption>{data.caption}</figcaption>{/if}
    <details class="chart-data" open={failed}>
      <summary>View chart data <span>({table.rows.length} rows)</span></summary>
      <!-- Keyboard access is required for the scrollable data table. -->
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div
        class="chart-table-scroll"
        tabindex="0"
        role="region"
        aria-label={`${title} data`}
      >
        <table>
          <caption>{title} values</caption>
          <thead>
            <tr
              >{#each table.columns as column}<th scope="col">{column}</th
                >{/each}</tr
            >
          </thead>
          <tbody>
            {#each table.rows as row}
              <tr
                >{#each row as value}<td>{value}</td>{/each}</tr
              >
            {/each}
          </tbody>
        </table>
      </div>
    </details>
  {:else}
    <p class="chart-notice">This chart uses unsupported or invalid data.</p>
  {/if}
</figure>

<style>
  .report-chart {
    margin: 0;
    min-width: 0;
    color: var(--fg);
  }
  .chart-surface {
    width: 100%;
    min-width: 0;
    height: clamp(280px, 28vw, 360px);
    overflow: hidden;
  }
  .chart-unavailable {
    display: none;
  }
  /*
   * The tooltip element is built in JS and inserted by ECharts, so it never
   * picks up Svelte's scoped class. These stay `:global` here, beside the
   * component that owns them, rather than moving to app.css.
   */
  :global(.report-tooltip) {
    display: grid;
    gap: 2px;
    min-width: 11rem;
    font-size: 12px;
  }
  :global(.report-tooltip__axis) {
    color: var(--fg-muted);
    font-size: 11px;
  }
  :global(.report-tooltip__lead) {
    display: flex;
    align-items: baseline;
    gap: 6px;
    font-weight: 600;
  }
  :global(.report-tooltip__lead .report-tooltip__name) {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  :global(.report-tooltip__share),
  :global(.report-tooltip__delta),
  :global(.report-tooltip__more) {
    color: var(--fg-muted);
    font-size: 11px;
    font-weight: 400;
  }
  :global(.report-tooltip__delta[data-direction="up"]) {
    color: var(--ok-text, var(--accent-text));
  }
  :global(.report-tooltip__delta[data-direction="down"]) {
    color: var(--warn-text);
  }
  :global(.report-tooltip__rest) {
    display: grid;
    gap: 1px;
    margin: 4px 0 0;
    padding: 0;
    list-style: none;
  }
  :global(.report-tooltip__rest li) {
    display: flex;
    align-items: baseline;
    gap: 6px;
    color: var(--fg-muted);
    font-size: 11px;
  }
  :global(.report-tooltip__rest .report-tooltip__name) {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  :global(.report-tooltip__swatch) {
    flex: none;
    width: 10px;
    height: 7px;
    border-radius: 2px;
  }

  .chart-legend {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 12px;
    margin: 8px 0 0;
    padding: 0;
    list-style: none;
  }
  .chart-legend__item {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    max-width: 14rem;
    padding: 2px 4px;
    border: 0;
    border-radius: 4px;
    background: none;
    color: var(--fg-muted);
    font-size: 11px;
    cursor: pointer;
  }
  .chart-legend__item:hover,
  .chart-legend__item--pinned {
    color: var(--fg);
    background: var(--bg-soft);
  }
  .chart-legend__item:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: 1px;
  }
  .chart-legend__swatch {
    flex: none;
    width: 12px;
    height: 8px;
    border-radius: 2px;
  }
  .chart-legend__name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  figcaption,
  .chart-notice {
    color: var(--fg-muted);
    font-size: 12px;
    line-height: 1.6;
    margin: 8px 0 0;
    overflow-wrap: anywhere;
  }
  .chart-data {
    border-top: 1px solid var(--line-subtle);
    margin-top: 14px;
  }
  summary {
    cursor: pointer;
    padding: 10px 0;
    color: var(--fg-muted);
    font-size: 11px;
  }
  summary span {
    color: var(--fg-subtle);
    margin-left: 4px;
  }
  summary:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .chart-table-scroll {
    max-height: 340px;
    overflow: auto;
  }
  table {
    width: 100%;
    text-align: left;
    border-collapse: collapse;
    font-size: 11px;
  }
  caption {
    text-align: left;
    color: var(--fg-muted);
    padding-bottom: 8px;
  }
  th,
  td {
    border-bottom: 1px solid var(--line-subtle);
    padding: 7px 8px;
    min-width: 50px;
    max-width: 220px;
    overflow-wrap: anywhere;
  }
  th {
    color: var(--fg-muted);
    font-weight: 500;
    background: var(--panel);
    position: sticky;
    top: 0;
  }
  td {
    font-variant-numeric: tabular-nums;
  }
</style>
