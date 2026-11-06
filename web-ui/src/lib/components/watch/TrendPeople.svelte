<script>
  /** A ranked Trends panel (who asks most, most active agents) as bars. */
  import { formatTrendValue } from "$lib/watchTrends.js";

  /** @type {{ panel: any }} */
  let { panel } = $props();

  let rows = $derived(panel?.rows ?? []);
  let top = $derived(Math.max(1, ...rows.map((r) => r.value ?? 0)));
  const was = (row) =>
    typeof row.previous !== "number"
      ? ""
      : row.previous === 0
        ? "new this period"
        : row.previous === row.value
          ? "same as before"
          : `${row.previous < row.value ? "up" : "down"} from ${formatTrendValue("count", row.previous)}`;
</script>

<article
  class="people"
  data-trend={panel.id}
  data-source="computed:watch_trends.{panel.id}"
>
  <h3 class="text-meta font-semibold text-fg">{panel.title}</h3>
  {#if rows.length}
    <ol class="mt-3 space-y-2" data-trend-rows>
      {#each rows as row (row.principal?.id)}
        <li class="row">
          <span class="name">{row.principal?.display || row.principal?.id}</span
          >
          <span class="track" aria-hidden="true"
            ><span
              class="fill"
              style="width: {Math.round((row.value / top) * 100)}%"
            ></span></span
          >
          <span class="count">
            {formatTrendValue("count", row.value)}
            {#if was(row)}<span class="was">{`· ${was(row)}`}</span>{/if}
          </span>
        </li>
      {/each}
    </ol>
  {:else}
    <p class="mt-2 text-meta text-fg-muted" data-trend-takeaway>
      {panel.truncated
        ? "Too busy to rank everyone in this period."
        : "Nobody yet in this period."}
    </p>
  {/if}
</article>

<style>
  .people {
    min-width: 0;
    padding: 1rem 1.125rem;
    border: 1px solid var(--line);
    border-radius: var(--radius-md);
    background: var(--panel);
  }
  .row {
    display: grid;
    grid-template-columns: minmax(0, 9rem) minmax(0, 1fr) auto;
    align-items: center;
    gap: 0.75rem;
    font-size: 13px;
  }
  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--fg);
  }
  .track {
    display: block;
    height: 0.5rem;
    border-radius: 3px;
    background: var(--bg-soft);
    overflow: hidden;
  }
  .fill {
    display: block;
    height: 100%;
    border-radius: 3px;
    background: var(--accent);
  }
  .count {
    color: var(--fg);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .was {
    color: var(--fg-subtle);
  }
</style>
