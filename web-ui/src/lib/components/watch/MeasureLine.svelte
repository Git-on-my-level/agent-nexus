<script>
  /**
   * The number an initiative is judged by, as the first line under its
   * title: "Monthly churn 12.4% → 10% · down 0.7 in 7d", with a sparkline
   * of the last readings. "No reading" is the loudest state a goal can be
   * in: nobody publishes the number, so nobody can say whether it is won.
   */
  import {
    measureText,
    measureView,
    sparklinePoints,
  } from "$lib/briefOutcome.js";

  let { measure } = $props();

  let view = $derived(measureView(measure));
  let line = $derived(
    sparklinePoints(measure.points.map((point) => point.value)),
  );
  const TONE = {
    ok: "text-ok-text",
    danger: "text-danger-text",
    muted: "text-fg-muted",
  };
</script>

<span
  class="mt-0.5 flex min-w-0 flex-wrap items-center gap-x-2 text-meta"
  title={measureText(measure)}
  data-source="computed:initiatives.measure"
  data-rollup-measure
  data-measure-reading={view.reading ? "true" : "false"}
>
  <span class="min-w-0 truncate text-fg">
    <span class="text-fg-muted">{view.label}</span>
    {#if view.reading}
      <b class="font-semibold tabular-nums">{view.reading}</b>
    {:else}
      <b class="font-semibold text-warn-text">No reading</b>
      <span class="text-fg-muted">{view.target}</span>
    {/if}
  </span>
  {#if view.change}
    <span class="tabular-nums {TONE[view.change.tone]}" data-measure-change
      >{view.change.text}</span
    >
  {/if}
  {#if view.stale}
    <span class="text-fg-muted" data-measure-stale>{view.stale}</span>
  {/if}
  {#if line}
    <svg
      class="spark text-fg-muted"
      viewBox="0 0 56 16"
      width="56"
      height="16"
      role="img"
      aria-label="{measure.points.length} readings"
      data-measure-sparkline
    >
      <polyline
        points={line}
        fill="none"
        stroke="currentColor"
        stroke-width="1.5"
        stroke-linecap="round"
        stroke-linejoin="round"
      />
    </svg>
  {/if}
</span>

<style>
  .spark {
    flex: none;
  }
</style>
