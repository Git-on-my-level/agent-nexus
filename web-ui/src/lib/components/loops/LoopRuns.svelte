<script>
  /**
   * A loop's recent runs as a strip of dots, oldest to newest: filled for a
   * run that acted, outlined for one that only measured, small for a no-op,
   * red for an infrastructure error. A run that kept a candidate gets the
   * accent; one that reverted or rejected it gets a ring. Read left to right
   * it answers "is this loop doing anything" without a table.
   */
  import { formatAgo } from "$lib/time/format.js";
  import { runText } from "$lib/loops.js";

  let { runs = [], now = Date.now(), source = "" } = $props();
</script>

{#if runs.length}
  <ol
    class="flex items-center gap-[3px]"
    aria-label="Last {runs.length} {runs.length === 1
      ? 'run'
      : 'runs'}, oldest first"
    data-loop-runs
    data-source={source || undefined}
  >
    {#each runs as run, i (i)}
      <li
        class="run run--{run.outcome} {run.decision
          ? `run--${run.decision}`
          : ''}"
        title="{runText(run)} · {formatAgo(run.at, now)}"
      >
        <span class="sr-only">{runText(run)}, {formatAgo(run.at, now)}</span>
      </li>
    {/each}
  </ol>
{/if}

<style>
  .run {
    width: 9px;
    height: 9px;
    border-radius: 999px;
    flex: none;
    box-sizing: border-box;
  }
  .run--acted {
    background: var(--fg-muted);
  }
  .run--measured {
    border: 1.5px solid var(--fg-subtle);
  }
  .run--no_op {
    width: 5px;
    height: 5px;
    margin: 0 2px;
    background: var(--fg-subtle);
    opacity: 0.6;
  }
  .run--infra_error {
    background: var(--danger-text);
  }
  .run--kept {
    background: var(--accent-solid);
    border: none;
  }
  .run--reverted,
  .run--rejected {
    background: transparent;
    border: 1.5px solid var(--warn-text);
  }
</style>
