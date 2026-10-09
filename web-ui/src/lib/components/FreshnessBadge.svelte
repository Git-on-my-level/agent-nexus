<script>
  /**
   * How long ago this last moved, against how long it was supposed to be.
   *
   * `3d` in green means a backlog card updated well inside its fortnight.
   * `3d` in red means something in progress has been silent for three times
   * as long as it should be. The age alone could not tell those apart, which
   * is why the plain `AgeBadge` is now only for places with no cadence to
   * measure against (a ref preview, a one-off instant).
   *
   * The expectation and the exact instant ride in the tooltip and in the
   * accessible name, so the badge itself stays two characters wide.
   */
  import { tooltip } from "$lib/actions/tooltip.js";
  import { freshnessModel } from "$lib/freshness.js";
  import { clockNow, retainClock } from "$lib/time/clock.svelte.js";

  let {
    /** ISO instant it last moved. */
    at = "",
    /** `in_progress` | `initiative` | `waiting` | `backlog` | `closed` */
    kind = "in_progress",
    /** Per-item override, when something knows better than the default. */
    expectationHours = null,
    /** The row itself, read for a projection-supplied expectation. */
    row = null,
    /** What happened then: "moved", "updated", "checked". */
    verb = "updated",
    /** Reference time. Omit it to follow the shared clock. */
    now = undefined,
    class: extraClass = "",
  } = $props();

  let live = $derived(now === undefined || now === null);
  let current = $derived(live ? clockNow() : Number(now));

  $effect(() => {
    if (!live) return;
    return retainClock();
  });

  let model = $derived(
    freshnessModel(at, { kind, expectationHours, row, verb, now: current }),
  );
</script>

{#if model}
  <time
    class="ui-badge ui-badge--{model.tone} freshness-badge {extraClass}"
    datetime={model.at}
    data-freshness={model.state}
    data-freshness-kind={kind}
    title={model.title}
    aria-label={model.title}
    use:tooltip={model.title}>{model.age}</time
  >
{/if}

<style>
  .freshness-badge {
    flex: none;
    font-variant-numeric: tabular-nums;
    /* No `cursor: help`: the question-mark cursor was the only sign that a
       tooltip was coming, back when it took a second to arrive. */
  }
</style>
