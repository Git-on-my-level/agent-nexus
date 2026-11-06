<script>
  /**
   * Watch → Trends: is the workspace getting better or worse? Three
   * independent reads (decisions, initiatives, loops), each bounded and
   * each with its own loading, failure and retry, so one slow group never
   * holds the others on a spinner. Laid out as a health summary: one
   * sentence on how the workspace is doing, an area card per group with its
   * status and main chart, then who needs help and who is most active.
   */
  import { createWorkspaceReadClient } from "$lib/coreClient";
  import { readWithDeadline } from "$lib/liveReports.js";
  import { TREND_GROUPS, trendsVerdict } from "$lib/watchTrends.js";
  import TrendArea from "./TrendArea.svelte";
  import TrendPeople from "./TrendPeople.svelte";

  /** @type {{ days?: number, links?: Record<string, string> }} */
  let { days = 30, links = {} } = $props();

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
  // Admission is one workspace-wide check, so a restricted reader is told once
  // rather than under every group.
  let restricted = $derived(
    TREND_GROUPS.some((group) => groups[group.key]?.status === "restricted"),
  );
  let loaded = $derived(
    Object.fromEntries(
      TREND_GROUPS.map((g) => [
        g.key,
        groups[g.key]?.status === "ok" ? groups[g.key].data.panels : undefined,
      ]),
    ),
  );
  let verdict = $derived(trendsVerdict(loaded, days));
  let people = $derived(
    [loaded.decisions, loaded.loops]
      .flatMap((panels) => panels ?? [])
      .filter((p) => p.id === "ask_sources" || p.id === "agent_runs"),
  );
</script>

<div class="space-y-5" data-watch-trends>
  {#if restricted}
    <p class="text-meta text-fg-muted" data-trend-restricted>
      Trends are worked out from every item in this workspace, so they need
      access to all of it. Some items here aren't visible to you.
    </p>
  {:else}
    {#if verdict.lead}
      <section class="verdict" aria-label="Summary" data-trend-summary>
        <p class="lead">
          {verdict.lead}{#if verdict.rest}{" "}<span class="rest"
              >{verdict.rest}</span
            >{/if}
        </p>
        {#if verdict.actions.some((a) => links[a.key])}
          <ul class="actions">
            {#each verdict.actions.filter((a) => links[a.key]) as action (action.key)}
              <li>
                <a
                  class="chip"
                  href={links[action.key]}
                  data-trend-action={action.key}>{action.label}</a
                >
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}
    <div class="areas">
      {#each TREND_GROUPS as group (group.key)}
        <TrendArea
          {group}
          state={groups[group.key] ?? { status: "loading" }}
          days={groups[group.key]?.data?.range_days ?? days}
          onretry={() => retry(group.key)}
        />
      {/each}
    </div>
    {#if people.length}
      <div class="people">
        {#each people as panel (panel.id)}
          <TrendPeople {panel} />
        {/each}
      </div>
    {/if}
  {/if}
</div>

<style>
  .verdict {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    padding: 1.125rem 1.25rem;
    border: 1px solid var(--line);
    border-radius: var(--radius-md);
    background: var(--panel);
  }
  .lead {
    margin: 0;
    max-width: 60rem;
    font-size: 1.125rem;
    font-weight: 600;
    line-height: 1.4;
    color: var(--fg);
  }
  .rest {
    font-weight: 400;
    color: var(--fg-muted);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    min-height: 2rem;
    padding: 0 0.75rem;
    border: 1px solid var(--line);
    border-radius: var(--radius-full);
    font-size: 13px;
    color: var(--fg);
    text-decoration: none;
  }
  .chip:hover {
    background: var(--panel-hover);
  }
  .areas {
    display: grid;
    gap: 0.875rem;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 17rem), 1fr));
  }
  .people {
    display: grid;
    gap: 0.875rem;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 22rem), 1fr));
  }
</style>
