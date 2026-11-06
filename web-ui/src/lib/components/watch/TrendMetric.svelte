<script>
  /**
   * One metric row in a Trends area: label, headline value (or the total over
   * the part that was read), a sparkline and the change from last period.
   */
  import {
    panelMax,
    panelReading,
    sparklinePath,
    trendChartLabel,
  } from "$lib/watchTrends.js";

  /** @type {{ panel: any, label?: string, bucket?: string }} */
  let { panel, label = panel?.title ?? "", bucket = "day" } = $props();

  let reading = $derived(panelReading(panel, bucket));
  // Promises kept is a share drawn from kept and missed counts: spark the
  // share per bucket, not the kept count.
  let spark = $derived.by(() => {
    const series = panel?.series ?? [];
    const kept = series.find((s) => s.key === "kept")?.points;
    const missed = series.find((s) => s.key === "missed")?.points;
    if (panel?.id === "promises" && kept && missed) {
      // Running share over the period, so quiet days don't break the line.
      let k = 0;
      let m = 0;
      const points = kept.map((v, i) => {
        if (typeof v !== "number" || typeof missed[i] !== "number") return null;
        k += v;
        m += missed[i];
        return k + m > 0 ? (100 * k) / (k + m) : null;
      });
      return { points, max: 100 };
    }
    return { points: series[0]?.points ?? [], max: panelMax(panel) };
  });
  let line = $derived(
    sparklinePath(spark.points, { width: 100, height: 28, max: spark.max }),
  );
</script>

<div
  class="metric"
  data-trend={panel.id}
  data-source="computed:watch_trends.{panel.source ?? panel.id}"
>
  <div class="min-w-0">
    <div class="label">{label}</div>
    <div class="value" data-trend-value>{reading.text}</div>
    {#if reading.partial}
      <div class="partial" data-trend-partial>{reading.partial}</div>
    {/if}
  </div>
  {#if line}
    <svg
      class="spark"
      viewBox="0 0 100 28"
      preserveAspectRatio="none"
      role="img"
      aria-label={trendChartLabel(panel)}
      data-trend-spark
    >
      <path d={line}></path>
    </svg>
  {:else}
    <span></span>
  {/if}
  <span
    class="delta delta--{reading.delta?.tone ?? 'flat'}"
    data-trend-delta={reading.delta ? "" : undefined}
    >{reading.delta?.label ?? ""}</span
  >
</div>

<style>
  .metric {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 4rem 4rem;
    align-items: center;
    gap: 0.75rem;
    padding: 0.625rem 0;
    border-top: 1px solid var(--line-subtle);
  }
  .label {
    font-size: 13px;
    color: var(--fg-muted);
  }
  .value {
    font-size: 1.125rem;
    font-weight: 600;
    line-height: 1.25;
    color: var(--fg);
    font-variant-numeric: tabular-nums;
  }
  .partial {
    font-size: 11px;
    color: var(--fg-subtle);
  }
  .spark {
    display: block;
    width: 4rem;
    height: 1.75rem;
    overflow: visible;
  }
  .spark path {
    fill: none;
    stroke: var(--accent);
    stroke-width: 1.75;
    stroke-linejoin: round;
    vector-effect: non-scaling-stroke;
  }
  .delta {
    text-align: right;
    font-size: 12px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .delta--good {
    color: var(--ok-text);
  }
  .delta--bad {
    color: var(--danger-text);
  }
  .delta--flat {
    color: var(--fg-muted);
  }
</style>
