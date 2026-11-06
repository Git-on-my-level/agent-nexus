<script>
  /**
   * One area's main chart: bars per bucket (side by side when there are two
   * bar series), an optional overlay bar drawn from the base (a subset such
   * as failed runs), and an optional line. Buckets nobody read are shaded and
   * labelled, never drawn as zero. Every number is core's.
   */
  import { bucketLabel, trendChartLabel } from "$lib/watchTrends.js";

  /**
   * @type {{
   *   panel: any,
   *   marks: { key: string, mark: "bar" | "overlay" | "line", tone: 0 | 1 }[],
   *   buckets?: string[],
   *   bucket?: string,
   * }}
   */
  let { panel, marks, buckets = [], bucket = "day" } = $props();

  const W = 300;
  const H = 100;
  const isNumber = (v) => typeof v === "number" && Number.isFinite(v);
  const f = (n) => Math.round(n * 100) / 100;

  let drawn = $derived(
    marks
      .map((m) => ({
        ...m,
        series: (panel?.series ?? []).find((s) => s.key === m.key),
      }))
      .filter((m) => m.series),
  );
  let n = $derived(
    Math.max(0, ...drawn.map((m) => m.series.points?.length ?? 0)),
  );
  let top = $derived.by(() => {
    const max = Math.max(
      0,
      ...drawn.flatMap((m) => (m.series.points ?? []).filter(isNumber)),
    );
    if (max <= 4) return Math.max(1, Math.ceil(max));
    const step = 10 ** Math.floor(Math.log10(max));
    return Math.ceil(max / (step / 2)) * (step / 2);
  });
  // Leading buckets with no known value in any series.
  let unread = $derived.by(() => {
    let i = 0;
    while (i < n && drawn.every((m) => !isNumber(m.series.points?.[i]))) i++;
    return i === n ? 0 : i;
  });
  let paths = $derived.by(() => {
    const slot = n ? W / n : W;
    const bars = drawn.filter((m) => m.mark === "bar");
    const out = [];
    for (const m of drawn) {
      const points = m.series.points ?? [];
      if (m.mark === "line") {
        let d = "";
        let pen = false;
        points.forEach((v, i) => {
          if (!isNumber(v)) {
            pen = false;
            return;
          }
          d += `${pen ? "L" : "M"}${f((i + 0.5) * slot)} ${f(H - (v / top) * H)}`;
          pen = true;
        });
        out.push({ key: m.key, kind: "line", tone: m.tone, d });
        continue;
      }
      const group = m.mark === "bar" ? Math.max(1, bars.length) : 1;
      const index = m.mark === "bar" ? bars.indexOf(m) : 0;
      const inset = slot * (n > 20 ? 0.16 : 0.2);
      const width = (slot - 2 * inset) / group;
      let d = "";
      points.forEach((v, i) => {
        if (!isNumber(v) || v <= 0) return;
        const h = (v / top) * H;
        const x = i * slot + inset + index * width;
        d += `M${f(x)} ${f(H - h)}h${f(width)}v${f(h)}h${f(-width)}Z`;
      });
      out.push({ key: m.key, kind: "bar", tone: m.tone, d });
    }
    return out;
  });
  let first = $derived(bucketLabel(buckets[0], bucket));
  let last = $derived(
    bucket === "day" && buckets.length
      ? "Today"
      : bucketLabel(buckets.at(-1), bucket),
  );
  let unreadUntil = $derived(
    unread ? bucketLabel(buckets[unread], bucket) : "",
  );
</script>

<figure class="chart" data-trend-chart={panel?.id}>
  <ul class="legend" aria-hidden="true">
    {#each drawn as m (m.key)}
      <li>
        <span class="swatch swatch--{m.tone} swatch--{m.mark}"></span>{m.series
          .label}
      </li>
    {/each}
  </ul>
  <div class="plot">
    <span class="ymax" aria-hidden="true">{top}</span>
    <svg
      viewBox="0 0 {W} {H}"
      preserveAspectRatio="none"
      role="img"
      aria-label={trendChartLabel(panel)}
    >
      <path class="grid" d="M0 0.5H{W}M0 50H{W}M0 99.5H{W}"></path>
      {#if unread}
        <rect class="unread" x="0" y="0" width={f((unread / n) * W)} height={H}
        ></rect>
      {/if}
      {#each paths as p (p.key)}
        {#if p.d}
          <path class="{p.kind} {p.kind}--{p.tone}" d={p.d}></path>
        {/if}
      {/each}
    </svg>
  </div>
  <figcaption class="axis" aria-hidden="true">
    <span>{first}</span>
    {#if unreadUntil}<span class="axis-unread" data-trend-unread
        >Before {unreadUntil}: not read</span
      >{/if}
    <span>{last}</span>
  </figcaption>
</figure>

<style>
  .chart {
    margin: 0;
    min-width: 0;
  }
  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    margin: 0 0 0.375rem;
    padding: 0;
    list-style: none;
    font-size: 12px;
    color: var(--fg-muted);
  }
  .legend li {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
  }
  .swatch {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 2px;
  }
  .swatch--line {
    height: 3px;
  }
  .swatch--0 {
    background: var(--accent);
  }
  .swatch--1 {
    background: var(--warn-text);
  }
  .plot {
    position: relative;
    padding-left: 1.5rem;
  }
  .ymax {
    position: absolute;
    top: -2px;
    left: 0;
    width: 1.25rem;
    text-align: right;
    font-size: 11px;
    line-height: 1;
    color: var(--fg-subtle);
    font-variant-numeric: tabular-nums;
    pointer-events: none;
  }
  svg {
    display: block;
    width: 100%;
    height: 7rem;
    overflow: visible;
  }
  .grid {
    fill: none;
    stroke: var(--line-subtle);
    stroke-width: 1;
    vector-effect: non-scaling-stroke;
  }
  .unread {
    fill: var(--bg-soft);
  }
  .bar--0 {
    fill: var(--accent);
  }
  .bar--1 {
    fill: var(--warn-text);
  }
  .line {
    fill: none;
    stroke-width: 2;
    stroke-linejoin: round;
    stroke-linecap: round;
    vector-effect: non-scaling-stroke;
  }
  .line--0 {
    stroke: var(--accent);
  }
  .line--1 {
    stroke: var(--warn-text);
  }
  .axis {
    display: flex;
    justify-content: space-between;
    gap: 0.5rem;
    margin-top: 0.25rem;
    padding-left: 1.5rem;
    font-size: 11px;
    color: var(--fg-subtle);
  }
  .axis-unread {
    color: var(--fg-muted);
    text-align: center;
  }
</style>
