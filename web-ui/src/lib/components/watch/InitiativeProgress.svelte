<script>
  /**
   * An initiative's plan progress, at a glance: a bar and its count.
   *
   * Everything here is `initiatives.progress` from the brief (I1), so the bar
   * moves the moment a fresh brief lands. Up to `SEGMENT_LIMIT` steps the bar
   * is one segment per step, the way the old Overview cards drew a plan;
   * past that a step is thinner than the gap between steps, so it becomes one
   * continuous fill.
   *
   * The tone follows the computed state: a promise that has slipped reads as
   * a warning, a finished initiative as done, anything else as moving.
   */
  let {
    progress = null,
    state = "moving",
    title = "",
    /** `row`: a fixed-width bar beside the count. `card`: full width. */
    variant = "row",
  } = $props();

  const SEGMENT_LIMIT = 12;

  let done = $derived(Math.min(progress?.done ?? 0, progress?.total ?? 0));
  let total = $derived(progress?.total ?? 0);
  let segmented = $derived(total > 0 && total <= SEGMENT_LIMIT);
  let percent = $derived(total ? Math.round((100 * done) / total) : 0);
  let tone = $derived(
    state === "done" || (total > 0 && done >= total)
      ? "done"
      : state === "past_promise"
        ? "late"
        : state === "no_promise"
          ? "idle"
          : "moving",
  );
  let label = $derived(
    `${title ? `${title}: ` : ""}${done} of ${total} ${total === 1 ? "step" : "steps"} done`,
  );
</script>

{#if progress && total > 0}
  <span
    class="progress progress--{variant}"
    data-watch-progress={tone}
    role="img"
    aria-label={label}
    title={label}
  >
    <span
      class="track"
      class:track--segmented={segmented}
      style:--steps={segmented ? total : 1}
      aria-hidden="true"
    >
      {#if segmented}
        {#each Array.from({ length: total }, (_, i) => i) as index (index)}
          <span class="seg" class:seg--done={index < done}></span>
        {/each}
      {:else}
        <span class="fill" style:width="{percent}%"></span>
      {/if}
    </span>
    <span
      class="count"
      aria-hidden="true"
      data-source="computed:initiatives.progress">{done}/{total}</span
    >
  </span>
{/if}

<style>
  .progress {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    --tone: var(--accent-solid);
  }
  .progress[data-watch-progress="done"] {
    --tone: var(--ok-text);
  }
  .progress[data-watch-progress="late"] {
    --tone: var(--warn-text);
  }
  .progress[data-watch-progress="idle"] {
    --tone: var(--fg-muted);
  }
  .progress--row .track {
    width: 88px;
  }
  .progress--card {
    display: flex;
    width: 100%;
  }
  .progress--card .track {
    flex: 1 1 auto;
  }
  .track {
    position: relative;
    display: block;
    height: 6px;
    min-width: 0;
    border-radius: 3px;
    background: var(--line);
    overflow: hidden;
  }
  .track--segmented {
    display: grid;
    grid-template-columns: repeat(var(--steps), minmax(0, 1fr));
    gap: 2px;
    background: transparent;
    overflow: visible;
  }
  .seg {
    height: 6px;
    border-radius: 2px;
    background: var(--line);
  }
  .seg--done,
  .fill {
    background: var(--tone);
  }
  .fill {
    display: block;
    height: 100%;
    border-radius: 3px;
  }
  .count {
    flex: none;
    color: var(--fg-muted);
    font-size: 11px;
    line-height: 16px;
    font-variant-numeric: tabular-nums;
  }
</style>
