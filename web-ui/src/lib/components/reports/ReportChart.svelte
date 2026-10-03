<script>
  import { onMount } from "svelte";
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
  const option = $derived(buildReportChartOption(data, appearance));
  const table = $derived(reportChartRows(data));

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
    let chart;
    let observer;
    const element = host;
    const resize = () => {
      if (element.clientWidth > 0)
        chart?.resize({
          width: element.clientWidth,
          height: element.clientHeight || 320,
        });
    };
    try {
      chart = renderer.createReportChart(element);
      chart.setOption(option, { notMerge: true });
      if (typeof ResizeObserver !== "undefined") {
        observer = new ResizeObserver(resize);
        observer.observe(element);
      }
      window.addEventListener("resize", resize);
      failed = false;
      ready = true;
    } catch {
      failed = true;
      ready = false;
      chart?.dispose();
      chart = null;
    }
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", resize);
      chart?.dispose();
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
