<script>
  /**
   * Watch's Loops view: initiatives that measure something, grouped by what
   * they need (`groupLoops`): waiting on you, silent too long, metric flat,
   * moving. Done folds away. Each row adds its series and flat signal.
   */
  import { groupLoops, LOOP_SILENT_SECONDS, formatSpan } from "$lib/brief.js";
  import WatchRow from "./WatchRow.svelte";

  let { initiatives = [], now = Date.now() } = $props();

  let groups = $derived(groupLoops(initiatives, now));
  let open = $derived(groups.filter((g) => g.group !== "done"));
  let done = $derived(groups.find((g) => g.group === "done") ?? null);
  let doneOpen = $state(false);

  const HINTS = {
    waiting: "A decision of yours is holding these up.",
    silent: `Past their promise, or no evidence for ${formatSpan(LOOP_SILENT_SECONDS)} or more.`,
    flat: "Reporting, but the metric has stopped moving.",
  };
</script>

{#snippet rows(group)}
  <div
    class="divide-y divide-line-subtle rounded-md border border-line bg-panel [&>*:first-child]:rounded-t-md [&>*:last-child]:rounded-b-md"
  >
    {#each group.rows as row (row.ref)}
      <WatchRow initiative={row} variant="row" {now} metrics />
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
      A loop is an initiative that measures something. Link a series to its card
      with a <code>series:</code> ref and it shows up here.
    </p>
  </div>
{:else}
  <div class="space-y-5" data-watch-loops>
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
  .loop-dot--waiting,
  .loop-dot--silent {
    background: var(--warn-text);
  }
  .loop-dot--moving {
    background: var(--accent-solid);
  }
</style>
