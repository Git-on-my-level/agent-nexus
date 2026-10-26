<script>
  /**
   * Watch → Trends: is the workspace getting better or worse? Three
   * independent reads (decisions, initiatives, loops), each bounded and
   * each with its own loading, failure and retry, so one slow group never
   * holds the others on a spinner.
   */
  import { createWorkspaceReadClient } from "$lib/coreClient";
  import { readWithDeadline } from "$lib/liveReports.js";
  import { TREND_GROUPS } from "$lib/watchTrends.js";
  import Button from "$lib/components/Button.svelte";
  import TrendCard from "./TrendCard.svelte";

  let { days = 30 } = $props();

  /** @type {Record<string, { status: string, data?: any }>} */
  let groups = $state({});

  async function load(key, range, closing) {
    groups[key] = { status: "loading" };
    try {
      const data = await readWithDeadline(
        (signal) =>
          createWorkspaceReadClient(signal).getWatchTrends({
            group: key,
            range: `${range}d`,
          }),
        { signal: closing },
      );
      if (closing.aborted) return;
      if (!Array.isArray(data?.panels)) throw new Error("bad response");
      groups[key] = { status: "ok", data };
    } catch (error) {
      if (closing.aborted) return;
      groups[key] = {
        status:
          error?.body?.error?.code === "restricted_trends"
            ? "restricted"
            : error?.name === "TimeoutError"
              ? "slow"
              : "failed",
      };
    }
  }

  let closing = null;
  $effect(() => {
    const range = days;
    const controller = new AbortController();
    closing = controller;
    for (const group of TREND_GROUPS)
      void load(group.key, range, controller.signal);
    return () => controller.abort();
  });
  const retry = (key) => closing && void load(key, days, closing.signal);
</script>

<div class="space-y-6" data-watch-trends>
  {#each TREND_GROUPS as group (group.key)}
    {@const state = groups[group.key] ?? { status: "loading" }}
    <section aria-labelledby="trends-{group.key}" data-trend-group={group.key}>
      <h2
        id="trends-{group.key}"
        class="mb-2 text-micro font-semibold uppercase tracking-wide text-fg-subtle"
      >
        {group.label}
      </h2>
      {#if state.status === "ok"}
        {#if state.data.panels.length}
          <div class="trend-grid">
            {#each state.data.panels as panel (panel.id)}
              <TrendCard {panel} days={state.data.range_days ?? days} />
            {/each}
          </div>
        {:else}
          <p class="text-meta text-fg-muted">Nothing to chart here yet.</p>
        {/if}
      {:else if state.status === "restricted"}
        <p class="text-meta text-fg-muted" data-trend-restricted>
          Trends are unavailable with your current access. You can still follow
          work in Initiatives and Loops.
        </p>
      {:else if state.status === "loading"}
        <div class="trend-grid" aria-busy="true">
          {#each [0, 1, 2] as i (i)}
            <div class="trend-skeleton"></div>
          {/each}
        </div>
        <p class="sr-only" role="status">Loading {group.label} trends…</p>
      {:else}
        <div
          class="flex flex-wrap items-center gap-3 rounded-md border border-dashed border-line px-4 py-3"
          data-trend-error
        >
          <p class="text-meta text-fg-muted">
            {state.status === "slow"
              ? "This is taking too long to work out."
              : "Couldn't load these trends."}
          </p>
          <Button
            variant="secondary"
            size="compact"
            onclick={() => retry(group.key)}>Retry</Button
          >
        </div>
      {/if}
    </section>
  {/each}
</div>

<style>
  .trend-grid {
    display: grid;
    gap: 0.75rem;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 17rem), 1fr));
  }
  .trend-skeleton {
    height: 9.5rem;
    border: 1px solid var(--line-subtle);
    border-radius: var(--radius-md);
    background: var(--bg-soft);
  }
</style>
