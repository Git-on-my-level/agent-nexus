<script>
  /**
   * One Trends area (Decisions, Initiatives or Loops): a status, one plain
   * sentence, the area's main chart and its other metrics as rows. Loading,
   * failure and retry are per area, so one slow read never holds the rest.
   */
  import { areaStatus, panelReading, trendTakeaway } from "$lib/watchTrends.js";
  import Button from "$lib/components/Button.svelte";
  import TrendChart from "./TrendChart.svelte";
  import TrendMetric from "./TrendMetric.svelte";

  /** @type {{ group: { key: string, label: string }, state: { status: string, data?: any }, days?: number, onretry?: () => void }} */
  let { group, state, days = 30, onretry } = $props();

  const LAYOUT = {
    decisions: {
      hero: "asks_flow",
      noun: "asks answered",
      marks: [
        { key: "opened", mark: "bar", tone: 0 },
        { key: "answered", mark: "line", tone: 1 },
      ],
      rows: [
        { id: "answer_time", label: "Typical time to answer" },
        { id: "ask_backlog", label: "Open asks" },
      ],
    },
    initiatives: {
      hero: "work_flow",
      noun: "cards finished",
      marks: [
        { key: "started", mark: "bar", tone: 0 },
        { key: "finished", mark: "bar", tone: 1 },
      ],
      rows: [
        { id: "promises", label: "Promises kept" },
        { id: "past_promise", label: "Initiatives past their promise" },
      ],
    },
    loops: {
      hero: "loop_runs",
      noun: "runs",
      marks: [
        { key: "runs", mark: "bar", tone: 0 },
        { key: "failed", mark: "overlay", tone: 1 },
      ],
      rows: [
        { id: "loop_health", label: "Loops reporting on time" },
        { id: "loop_failed", label: "Failed to run" },
      ],
    },
  };

  let layout = $derived(LAYOUT[group.key]);
  let data = $derived(state.status === "ok" ? state.data : null);
  let bucket = $derived(data?.bucket ?? "day");
  let panels = $derived.by(() => {
    const out = Object.fromEntries((data?.panels ?? []).map((p) => [p.id, p]));
    // Failed runs ride on the runs panel; give them a row of their own.
    const runs = out.loop_runs;
    if (runs)
      out.loop_failed = {
        id: "loop_failed",
        source: "loop_runs.failed",
        title: "Failed to run",
        unit: "count",
        direction: "down_good",
        value: runs.failed ?? null,
        previous: runs.failed_previous ?? null,
        truncated: runs.truncated,
        series: (runs.series ?? []).filter((s) => s.key === "failed"),
        rows: [],
      };
    return out;
  });
  let hero = $derived(panels[layout?.hero]);
  let status = $derived(areaStatus(group.key, data?.panels ?? []));
  let reading = $derived(panelReading(hero, bucket));
  let say = $derived(
    !hero
      ? ""
      : reading.partial
        ? `${reading.text} ${layout.noun} in the ${reading.partial}. Older history on this busy workspace wasn't read.`
        : trendTakeaway(hero, days),
  );
  // Panels this layout does not place (new ids from a newer core) still show,
  // as rows after the known ones.
  let rows = $derived.by(() => {
    const placed = new Set([
      layout?.hero,
      ...(layout?.rows ?? []).map((r) => r.id),
    ]);
    const known = (layout?.rows ?? []).filter((r) => panels[r.id]);
    const extra = (data?.panels ?? [])
      .filter(
        (p) =>
          !placed.has(p.id) &&
          !(p.rows ?? []).length &&
          p.id !== "ask_sources" &&
          p.id !== "agent_runs",
      )
      .map((p) => ({ id: p.id, label: p.title }));
    return [...known, ...extra];
  });
</script>

<section
  class="area"
  aria-labelledby="trends-{group.key}"
  data-trend-group={group.key}
>
  <header class="flex items-center justify-between gap-2">
    <h2 id="trends-{group.key}" class="text-base font-semibold text-fg">
      {group.label}
    </h2>
    {#if data}
      <span
        class="status status--{status.tone}"
        data-trend-verdict={status.tone}>{status.label}</span
      >
    {/if}
  </header>

  {#if data}
    {#if hero}
      <p class="say" data-trend-takeaway>{say}</p>
      <div data-trend={hero.id} data-source="computed:watch_trends.{hero.id}">
        <TrendChart
          panel={hero}
          marks={layout.marks}
          buckets={data.buckets ?? []}
          {bucket}
        />
      </div>
    {:else if !rows.length}
      <p class="say">Nothing to chart here yet.</p>
    {/if}
    {#if rows.length}
      <div class="rows">
        {#each rows as row (row.id)}
          <TrendMetric panel={panels[row.id]} label={row.label} {bucket} />
        {/each}
      </div>
    {/if}
  {:else if state.status === "loading"}
    <div class="skeleton" aria-busy="true"></div>
    <p class="sr-only" role="status">Loading {group.label} trends…</p>
  {:else}
    <div
      class="flex flex-wrap items-center gap-3 rounded-md border border-dashed border-line px-3 py-2"
      data-trend-error
    >
      <p class="text-meta text-fg-muted">
        {state.status === "slow"
          ? "This is taking too long to work out."
          : "Couldn't load these trends."}
      </p>
      <Button variant="secondary" size="compact" onclick={onretry}>Retry</Button
      >
    </div>
  {/if}
</section>

<style>
  .area {
    display: flex;
    min-width: 0;
    flex-direction: column;
    gap: 0.875rem;
    padding: 1.125rem;
    border: 1px solid var(--line);
    border-radius: var(--radius-md);
    background: var(--panel);
  }
  .status {
    flex: none;
    border-radius: var(--radius-full);
    padding: 0.125rem 0.5rem;
    font-size: 12px;
    font-weight: 600;
  }
  .status--warn {
    background: var(--warn-soft);
    color: var(--warn-text);
  }
  .status--ok {
    background: var(--accent-soft);
    color: var(--accent-text);
  }
  .status--none {
    background: var(--bg-soft);
    color: var(--fg-muted);
  }
  .say {
    margin: 0;
    font-size: 13px;
    color: var(--fg-muted);
  }
  .rows {
    display: flex;
    flex-direction: column;
  }
  .skeleton {
    height: 14rem;
    border-radius: var(--radius-md);
    background: var(--bg-soft);
  }
</style>
