<script>
  /** Watch as a list sorted by attention. No-promise and Done start folded. */
  import { groupWatch } from "$lib/brief.js";
  import WatchRow from "./WatchRow.svelte";

  let { initiatives = [], now = Date.now() } = $props();

  const COLLAPSED = new Set(["no_promise", "done"]);
  let groups = $derived(groupWatch(initiatives));
  let opened = $state({});
  const isOpen = (state) => opened[state] ?? !COLLAPSED.has(state);
</script>

<div class="space-y-4" data-watch-list>
  {#each groups.filter((g) => !COLLAPSED.has(g.state)) as group (group.state)}
    <section data-watch-group={group.state}>
      <h2 class="mb-1 text-meta font-semibold text-fg">{group.label}</h2>
      <div class="rounded-md border border-border bg-panel">
        {#each group.rows as row (row.ref)}
          <WatchRow initiative={row} variant="row" {now} />
        {/each}
      </div>
    </section>
  {/each}
  <div class="space-y-2">
    {#each groups.filter((g) => COLLAPSED.has(g.state)) as group (group.state)}
      <section data-watch-group={group.state}>
        <button
          type="button"
          class="flex items-center gap-1 text-meta font-semibold text-fg hover:underline"
          aria-expanded={isOpen(group.state)}
          onclick={() => (opened[group.state] = !isOpen(group.state))}
        >
          <span aria-hidden="true">{isOpen(group.state) ? "⌄" : "›"}</span>
          {group.label} ({group.rows.length})
        </button>
        {#if isOpen(group.state)}
          <div class="mt-1 rounded-md border border-border bg-panel">
            {#each group.rows as row (row.ref)}
              <WatchRow initiative={row} variant="row" {now} />
            {/each}
          </div>
        {/if}
      </section>
    {/each}
  </div>
</div>
