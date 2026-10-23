<script>
  /**
   * A loop's goal metric over time. The faint line is every measured value;
   * the solid step line is the best value so far in the goal's direction,
   * which is what a hill-climbing loop actually ratchets; the dashed line is
   * the target. Runs that kept, reverted or rejected a candidate are marked.
   *
   * `size="spark"` is the loop card's inline chart; `size="large"` is the
   * hill-climb panel, with its value range, target and first/last dates.
   */
  import { formatMetricValue } from "$lib/brief.js";
  import { goalGeometry, withUnit } from "$lib/loops.js";
  import { bucketLabels, formatTime } from "$lib/time/format.js";

  let {
    goal,
    size = "spark",
    label = "",
    /** The chart spans this many days ending now, not first-to-last point. */
    windowDays = 30,
    now = Date.now(),
  } = $props();

  const DAY_MS = 86_400_000;
  let large = $derived(size === "large");
  let width = $derived(large ? 960 : 300);
  let height = $derived(large ? 220 : 56);
  let span = $derived({ start: now - windowDays * DAY_MS, end: now });
  let geo = $derived(
    goalGeometry(goal, { width, height, pad: large ? 10 : 4, window: span }),
  );

  const day = (iso) => formatTime(iso, { style: "date" });
  let ends = $derived.by(() => {
    const start = new Date(span.start).toISOString();
    const end = new Date(span.end).toISOString();
    return bucketLabels([start, end]) ?? [day(start), day(end)];
  });
  let summary = $derived.by(() => {
    if (!goal?.points?.length) return "";
    const parts = [
      `${goal.metric}: ${goal.points.length} measurements`,
      `latest ${formatMetricValue(goal.latest ?? goal.points.at(-1).value)}`,
    ];
    if (goal.best !== null) parts.push(`best ${formatMetricValue(goal.best)}`);
    if (goal.target !== null)
      parts.push(`target ${formatMetricValue(goal.target)}`);
    parts.push(
      goal.direction === "down" ? "lower is better" : "higher is better",
    );
    return parts.join(", ");
  });
</script>

{#if geo}
  <figure class="loop-chart loop-chart--{size} min-w-0" data-loop-chart>
    <svg
      viewBox="0 0 {width} {height}"
      role="img"
      aria-label={label || summary}
      class="block h-auto w-full overflow-visible"
    >
      {#if geo.target !== null}
        <line
          class="target"
          x1="0"
          x2={width}
          y1={geo.target}
          y2={geo.target}
          vector-effect="non-scaling-stroke"
        />
      {/if}
      <polyline
        class="raw"
        points={geo.raw}
        vector-effect="non-scaling-stroke"
      />
      <polyline
        class="best"
        points={geo.best}
        vector-effect="non-scaling-stroke"
      />
      {#if geo.silence}
        <line
          class="silence"
          x1={geo.silence.x1}
          x2={geo.silence.x2}
          y1={geo.silence.y}
          y2={geo.silence.y}
          vector-effect="non-scaling-stroke"
          data-loop-chart-silence
        />
      {/if}
      {#each geo.markers as marker, i (i)}
        <circle
          class="marker marker--{marker.decision}"
          cx={marker.x}
          cy={marker.y}
          r={large ? 3.5 : 2.5}
          ><title
            >{marker.decision} · {formatMetricValue(marker.value)} · {day(
              marker.at,
            )}</title
          ></circle
        >
      {/each}
      {#if !geo.markers.some((marker) => marker.x === geo.last.x)}
        <circle
          class="latest"
          cx={geo.last.x}
          cy={geo.last.y}
          r={large ? 3 : 2.25}
        />
      {/if}
    </svg>
    {#if large}
      <figcaption
        class="mt-1 flex flex-wrap justify-between gap-x-4 text-micro tabular-nums text-fg-subtle"
      >
        <span>{ends[0]}</span>
        <span>
          range {withUnit(formatMetricValue(geo.lo), goal.unit)}–{withUnit(
            formatMetricValue(geo.hi),
            goal.unit,
          )}{#if goal.target !== null}
            · target {withUnit(formatMetricValue(goal.target), goal.unit)}{/if}
        </span>
        <span>{ends[1]}</span>
      </figcaption>
    {/if}
  </figure>
{/if}

<style>
  .raw {
    fill: none;
    stroke: var(--fg-subtle);
    stroke-width: 1;
    opacity: 0.55;
  }
  .best {
    fill: none;
    stroke: var(--accent-solid);
    stroke-width: 2;
    stroke-linejoin: round;
  }
  .loop-chart--large .best {
    stroke-width: 2.5;
  }
  .silence {
    stroke: var(--fg-subtle);
    stroke-width: 1.5;
    stroke-dasharray: 3 5;
    opacity: 0.7;
  }
  .target {
    stroke: var(--ok-text);
    stroke-width: 1;
    stroke-dasharray: 4 3;
    opacity: 0.8;
  }
  .marker--kept {
    fill: var(--accent-solid);
    stroke: var(--panel, transparent);
    stroke-width: 1;
  }
  .marker--reverted,
  .marker--rejected {
    fill: var(--panel, #000);
    stroke: var(--fg-subtle);
    stroke-width: 1.25;
  }
  .latest {
    fill: var(--accent-solid);
  }
</style>
