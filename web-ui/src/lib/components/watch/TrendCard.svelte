<script>
  /**
   * One Watch trend: the current value, its change from the previous period,
   * a one-line takeaway and a sparkline (or a ranked list). Every number is
   * core's; this card only words and draws it.
   */
  import {
    formatTrendValue,
    panelMax,
    sparklinePath,
    trendDelta,
    trendTakeaway,
  } from "$lib/watchTrends.js";

  let { panel, days = 30 } = $props();

  let ranked = $derived((panel.rows ?? []).length > 0);
  let delta = $derived(trendDelta(panel));
  let takeaway = $derived(trendTakeaway(panel, days));
  let max = $derived(panelMax(panel));
  let lines = $derived(
    (panel.series ?? []).map((s, i) => ({
      key: s.key,
      label: s.label,
      tone: i,
      d: sparklinePath(s.points ?? [], { max }),
    })),
  );
  let drawn = $derived(lines.some((line) => line.d));
  let chartLabel = $derived(
    `${panel.title}: ${(panel.series ?? [])
      .map(
        (s) =>
          `${s.label} ${(s.points ?? [])
            .map((p) =>
              typeof p === "number"
                ? formatTrendValue(panel.unit, p)
                : "unknown",
            )
            .join(", ")}`,
      )
      .join("; ")}`,
  );
</script>

<article
  class="trend-card"
  data-trend={panel.id}
  data-source="computed:watch_trends.{panel.id}"
>
  <header class="flex items-start justify-between gap-2">
    <h3 class="text-meta font-semibold text-fg">{panel.title}</h3>
    {#if panel.truncated}
      <span
        class="partial"
        title="This period had more records than one read covers. Older buckets show as gaps rather than zero."
        data-trend-partial>Recent only</span
      >
    {/if}
  </header>

  {#if !ranked}
    <div class="mt-1 flex flex-wrap items-baseline gap-x-2">
      <span class="value" data-trend-value
        >{formatTrendValue(panel.unit, panel.value)}</span
      >
      {#if delta}
        <span class="delta delta--{delta.tone}" data-trend-delta
          >{delta.label}</span
        >
      {/if}
    </div>
  {/if}

  <p class="mt-1 text-meta text-fg-muted" data-trend-takeaway>{takeaway}</p>

  {#if ranked}
    <ol class="mt-2 space-y-1" data-trend-rows>
      {#each panel.rows as row (row.principal?.id)}
        <li class="flex items-baseline justify-between gap-3 text-meta">
          <span class="truncate text-fg"
            >{row.principal?.display || row.principal?.id}</span
          >
          <span class="shrink-0 tabular-nums text-fg-muted">
            {formatTrendValue("count", row.value)}
            {#if typeof row.previous === "number"}<span class="text-fg-subtle"
                >· was {formatTrendValue("count", row.previous)}</span
              >{/if}
          </span>
        </li>
      {/each}
    </ol>
  {:else if drawn}
    <svg
      class="spark mt-3"
      viewBox="0 0 100 32"
      preserveAspectRatio="none"
      role="img"
      aria-label={chartLabel}
      data-trend-spark
    >
      {#each lines as line (line.key)}
        {#if line.d}
          <path class="line line--{line.tone}" d={line.d} />
        {/if}
      {/each}
    </svg>
    {#if lines.length > 1}
      <ul class="mt-1 flex flex-wrap gap-x-3 text-micro text-fg-muted">
        {#each lines as line (line.key)}
          <li class="inline-flex items-center gap-1">
            <span class="swatch swatch--{line.tone}" aria-hidden="true"
            ></span>{line.label}
          </li>
        {/each}
      </ul>
    {/if}
  {/if}
</article>

<style>
  .trend-card {
    min-width: 0;
    padding: 0.875rem 1rem;
    border: 1px solid var(--line);
    border-radius: var(--radius-md);
    background: var(--panel);
  }
  .value {
    color: var(--fg);
    font-size: 1.5rem;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    line-height: 1.2;
  }
  .delta {
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
  .partial {
    flex: none;
    border-radius: var(--radius-full);
    padding: 0 6px;
    background: var(--warn-soft);
    color: var(--warn-text);
    font-size: 11px;
    line-height: 18px;
  }
  .spark {
    display: block;
    width: 100%;
    height: 40px;
    overflow: visible;
  }
  .line {
    fill: none;
    stroke-width: 2;
    stroke-linejoin: round;
    stroke-linecap: round;
    vector-effect: non-scaling-stroke;
  }
  .line--0,
  .swatch--0 {
    stroke: var(--accent);
    background: var(--accent);
  }
  .line--1,
  .swatch--1 {
    stroke: var(--fg-subtle);
    background: var(--fg-subtle);
  }
  .line--1 {
    stroke-dasharray: 4 3;
  }
  .line--2,
  .swatch--2 {
    stroke: var(--warn-text);
    background: var(--warn-text);
  }
  .swatch {
    display: inline-block;
    width: 10px;
    height: 3px;
    border-radius: 2px;
  }
</style>
