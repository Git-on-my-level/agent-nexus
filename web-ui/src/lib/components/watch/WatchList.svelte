<script>
  /** Watch as a list sorted by attention. No-promise and Done start folded. */
  import { groupWatch } from "$lib/brief.js";
  import WatchRow from "./WatchRow.svelte";

  let { initiatives = [], now = Date.now() } = $props();

  const COLLAPSED = new Set(["no_promise", "done"]);
  /* Groups worth a full card each: what is moving or has slipped. The rest
     stay compact rows, folded until asked for. */
  const RICH = new Set(["moving", "past_promise"]);
  let groups = $derived(groupWatch(initiatives));
  let opened = $state({});
  const isOpen = (state) => opened[state] ?? !COLLAPSED.has(state);
  let open = $derived(groups.filter((g) => !COLLAPSED.has(g.state)));
  let folded = $derived(groups.filter((g) => COLLAPSED.has(g.state)));
</script>

{#snippet rows(group)}
  {#if RICH.has(group.state)}
    <div
      class="grid grid-cols-1 gap-3 xl:grid-cols-2"
      data-watch-cards={group.state}
    >
      {#each group.rows as row (row.ref)}
        <WatchRow initiative={row} variant="rich" {now} metrics />
      {/each}
    </div>
  {:else}
    <div
      class="divide-y divide-line-subtle rounded-md border border-line bg-panel [&>*:first-child]:rounded-t-md [&>*:last-child]:rounded-b-md"
    >
      {#each group.rows as row (row.ref)}
        <WatchRow initiative={row} variant="row" {now} />
      {/each}
    </div>
  {/if}
{/snippet}

<div class="space-y-5" data-watch-list>
  {#each open as group (group.state)}
    <section
      data-watch-group={group.state}
      aria-labelledby="watch-group-{group.state}"
    >
      <h2
        id="watch-group-{group.state}"
        class="ui-label flex items-baseline gap-1.5"
      >
        <span class="watch-dot watch-dot--{group.state}" aria-hidden="true"
        ></span>{group.label}
        <span class="font-normal tabular-nums text-fg-subtle"
          >{group.rows.length}</span
        >
      </h2>
      {@render rows(group)}
    </section>
  {/each}
  {#if folded.length}
    <div
      class="space-y-1 {open.length ? 'border-t border-line-subtle pt-3' : ''}"
    >
      {#each folded as group (group.state)}
        <section data-watch-group={group.state}>
          <button
            type="button"
            class="ui-label mb-0 flex items-center gap-1.5 rounded-sm py-1 hover:text-fg focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent"
            aria-expanded={isOpen(group.state)}
            onclick={() => (opened[group.state] = !isOpen(group.state))}
          >
            <svg
              class="h-2.5 w-2.5 transition-transform {isOpen(group.state)
                ? 'rotate-90'
                : ''}"
              viewBox="0 0 20 20"
              fill="currentColor"
              aria-hidden="true"><path d="M7 4l7 6-7 6V4z" /></svg
            >
            {group.label}
            <span class="font-normal tabular-nums">{group.rows.length}</span>
          </button>
          {#if isOpen(group.state)}
            <div class="mb-3 mt-1">{@render rows(group)}</div>
          {/if}
        </section>
      {/each}
    </div>
  {/if}
</div>

<style>
  .watch-dot {
    display: inline-block;
    width: 6px;
    height: 6px;
    margin-right: 2px;
    border-radius: 999px;
    background: var(--fg-subtle);
    transform: translateY(-1px);
  }
  .watch-dot--past_promise {
    background: var(--warn-text);
  }
  .watch-dot--moving {
    background: var(--accent-solid);
  }
</style>
