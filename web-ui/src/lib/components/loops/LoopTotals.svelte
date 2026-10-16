<script>
  /**
   * How much the loops are doing, in one row: how many there are, how many
   * effects they produced this week (and against last week, when known),
   * what their runs did, how many goals improved, and how many need a look.
   * The run mix is one stacked bar so acted vs. no-op vs. error reads at a
   * glance.
   */
  import { RUN_OUTCOME_TEXT } from "$lib/loops.js";

  let {
    totals,
    source = "",
    attentionNote = "failing, overdue or stalled",
  } = $props();

  let runs = $derived(totals?.runs7d ?? {});
  let runTotal = $derived(
    (runs.acted ?? 0) +
      (runs.measured ?? 0) +
      (runs.no_op ?? 0) +
      (runs.infra_error ?? 0),
  );
  let trend = $derived.by(() => {
    const prev = totals?.effectsPrev7d;
    if (prev === null || prev === undefined) return null;
    const diff = totals.effects7d - prev;
    return {
      diff,
      text:
        diff === 0
          ? "same as last week"
          : `${diff > 0 ? "+" : "−"}${Math.abs(diff)} vs last week`,
    };
  });
  const SEGMENTS = ["acted", "measured", "no_op", "infra_error"];
</script>

{#if totals}
  <dl
    class="grid grid-cols-2 gap-px overflow-hidden rounded-md border border-line bg-line sm:grid-cols-4"
    data-loop-totals
    data-source={source || undefined}
  >
    <div class="tile">
      <dt>Loops</dt>
      <dd class="value">{totals.loops}</dd>
    </div>
    <div class="tile">
      <dt>Effects, 7 days</dt>
      <dd class="value">{totals.effects7d}</dd>
      {#if trend}
        <dd class="note" class:up={trend.diff > 0} class:down={trend.diff < 0}>
          {trend.text}
        </dd>
      {:else if totals.improved7d !== null && totals.improved7d !== undefined}
        <dd class="note">{totals.improved7d} goals improved</dd>
      {/if}
    </div>
    <div class="tile">
      <dt>Runs, 7 days</dt>
      <dd class="value">{runTotal}</dd>
      {#if runTotal}
        <dd class="mt-1.5">
          <span
            class="flex h-1.5 overflow-hidden rounded-full bg-line"
            aria-hidden="true"
          >
            {#each SEGMENTS as outcome (outcome)}
              {#if runs[outcome]}
                <span
                  class="seg seg--{outcome}"
                  style:width="{(100 * runs[outcome]) / runTotal}%"
                ></span>
              {/if}
            {/each}
          </span>
          <span class="note mt-1 block">
            {SEGMENTS.filter((outcome) => runs[outcome])
              .map((outcome) => `${runs[outcome]} ${RUN_OUTCOME_TEXT[outcome]}`)
              .join(" · ")}
          </span>
        </dd>
      {/if}
    </div>
    <div class="tile">
      <dt>Need a look</dt>
      <dd class="value" class:warn={totals.needsAttention > 0}>
        {totals.needsAttention}
      </dd>
      <dd class="note">{attentionNote}</dd>
    </div>
  </dl>
{/if}

<style>
  .tile {
    background: var(--panel);
    padding: 0.625rem 0.875rem;
    min-width: 0;
  }
  dt {
    font-size: 0.6875rem;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--fg-subtle);
  }
  .value {
    font-size: 1.5rem;
    font-weight: 600;
    line-height: 1.2;
    font-variant-numeric: tabular-nums;
    color: var(--fg);
  }
  .note {
    font-size: 0.75rem;
    color: var(--fg-muted);
    font-variant-numeric: tabular-nums;
  }
  .value.warn,
  .note.down {
    color: var(--warn-text);
  }
  .note.up {
    color: var(--ok-text);
  }
  .seg--acted {
    background: var(--accent-solid);
  }
  .seg--measured {
    background: var(--fg-subtle);
  }
  .seg--no_op {
    background: var(--line-strong, var(--fg-subtle));
    opacity: 0.6;
  }
  .seg--infra_error {
    background: var(--danger-text);
  }
</style>
