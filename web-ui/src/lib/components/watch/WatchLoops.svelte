<script>
  /**
   * Watch's Loops view: how much the loops are doing (`LoopTotals`), then
   * each loop as a card, grouped by what it needs (`groupLoops`): waiting on
   * you, failing, silent, not improving, moving. Done folds away. A loop
   * with receipts gets the full `LoopCard`; one that only links a series
   * keeps the rich initiative card with its metric line.
   */
  import { page } from "$app/stores";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { groupLoops, LOOP_SILENT_SECONDS, formatSpan } from "$lib/brief.js";
  import { loopTotals } from "$lib/loops.js";
  import LoopCard from "$lib/components/loops/LoopCard.svelte";
  import LoopTotals from "$lib/components/loops/LoopTotals.svelte";
  import WatchRow from "./WatchRow.svelte";

  let { initiatives = [], now = Date.now() } = $props();

  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let groups = $derived(groupLoops(initiatives, now));
  const NEEDS_LOOK = new Set(["waiting", "failing", "silent", "flat"]);
  /*
   * Run totals come from loops that record receipts; the loop count and
   * "need a look" follow the groups below, so the strip and the page agree.
   */
  let totals = $derived.by(() => {
    const rows = groups
      .filter((g) => g.group !== "done")
      .flatMap((g) => g.rows);
    return {
      ...loopTotals(rows.map((row) => row.loop).filter(Boolean)),
      loops: rows.length,
      needsAttention: groups
        .filter((g) => NEEDS_LOOK.has(g.group))
        .reduce((sum, g) => sum + g.rows.length, 0),
    };
  });
  let open = $derived(groups.filter((g) => g.group !== "done"));
  let done = $derived(groups.find((g) => g.group === "done") ?? null);
  let doneOpen = $state(false);

  const HINTS = {
    waiting: "A decision of yours is holding these up.",
    failing: "Their last runs hit infrastructure errors.",
    silent: `Overdue for a run, past their promise, or no evidence for ${formatSpan(LOOP_SILENT_SECONDS)} or more.`,
    flat: "Running, but no effect within budget or the metric has stopped moving.",
  };
</script>

{#snippet rows(group)}
  <div class="grid grid-cols-1 gap-3 xl:grid-cols-2">
    {#each group.rows as row (row.ref)}
      {#if row.loop}
        <LoopCard
          ref={row.ref}
          title={row.title}
          href={workspaceHref(`/initiatives/${encodeURIComponent(row.id)}`)}
          loop={row.loop}
          {now}
        >
          {#snippet badges()}
            {#if row.waitingOnYouCount > 0}
              <a
                class="ui-badge ui-badge--warn hover:underline"
                href={workspaceHref(
                  `/decide?initiative=${encodeURIComponent(row.ref)}`,
                )}
                data-source="computed:initiatives.waiting_on_you_count"
                >{row.waitingOnYouCount} waiting on you</a
              >
            {/if}
          {/snippet}
        </LoopCard>
      {:else}
        <WatchRow initiative={row} variant="rich" {now} metrics />
      {/if}
    {/each}
  </div>
{/snippet}

{#if !groups.length}
  <div
    class="rounded-md border border-dashed border-line px-4 py-8 text-center"
    data-watch-loops-empty
  >
    <p class="text-meta text-fg">No loops yet.</p>
    <p class="mt-1 text-micro text-fg-muted">
      A loop is an initiative that runs on a cadence. Record each run with
      <code>anx work receipt</code>, or link a series with a
      <code>series:</code> ref, and it shows up here.
    </p>
  </div>
{:else}
  <div class="space-y-5" data-watch-loops>
    {#if totals.loops}
      <LoopTotals
        {totals}
        source="computed:initiatives.loop"
        attentionNote="waiting on you, failing, silent or not improving"
      />
    {/if}
    {#each open as group (group.group)}
      <section
        data-watch-loop-group={group.group}
        aria-labelledby="watch-loops-{group.group}"
      >
        <h2
          id="watch-loops-{group.group}"
          class="ui-label flex items-baseline gap-1.5"
        >
          <span class="loop-dot loop-dot--{group.group}" aria-hidden="true"
          ></span>{group.label}
          <span class="font-normal tabular-nums text-fg-subtle"
            >{group.rows.length}</span
          >
        </h2>
        {#if HINTS[group.group]}
          <p class="-mt-1 mb-1.5 text-micro text-fg-subtle">
            {HINTS[group.group]}
          </p>
        {/if}
        {@render rows(group)}
      </section>
    {/each}
    {#if done}
      <section
        class={open.length ? "border-t border-line-subtle pt-3" : ""}
        data-watch-loop-group="done"
      >
        <button
          type="button"
          class="ui-label mb-0 flex items-center gap-1.5 rounded-sm py-1 hover:text-fg focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent"
          aria-expanded={doneOpen}
          onclick={() => (doneOpen = !doneOpen)}
        >
          <svg
            class="h-2.5 w-2.5 transition-transform {doneOpen
              ? 'rotate-90'
              : ''}"
            viewBox="0 0 20 20"
            fill="currentColor"
            aria-hidden="true"><path d="M7 4l7 6-7 6V4z" /></svg
          >
          {done.label}
          <span class="font-normal tabular-nums">{done.rows.length}</span>
        </button>
        {#if doneOpen}
          <div class="mb-3 mt-1">{@render rows(done)}</div>
        {/if}
      </section>
    {/if}
  </div>
{/if}

<style>
  .loop-dot {
    display: inline-block;
    width: 6px;
    height: 6px;
    margin-right: 2px;
    border-radius: 999px;
    background: var(--fg-subtle);
    transform: translateY(-1px);
  }
  .loop-dot--failing {
    background: var(--danger-text);
  }
  .loop-dot--waiting,
  .loop-dot--silent {
    background: var(--warn-text);
  }
  .loop-dot--moving {
    background: var(--accent-solid);
  }
</style>
