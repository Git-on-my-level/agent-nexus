<script>
  /**
   * A grid of cards, for the Overview's initiatives section and the live
   * report panel of the same name.
   *
   * A thin wrapper: `workSummaryCards.js` decides the order and the blocks,
   * and `WorkSummaryCard` draws each card. This file owns the grid and the
   * two folds, nothing about what a card says.
   *
   * The order is the point. Cards are sorted by attention — blocked, at risk,
   * stale, on track, done — so the top-left card is always the one that most
   * needs a decision. Finished cards and ones with no plan are real but are
   * not what a dashboard is for, so they collapse at the bottom instead of
   * pushing live work off the first screen.
   */
  import { page } from "$app/stores";

  import { coreClient } from "$lib/coreClient";
  import { prefetchWork } from "$lib/workCache.js";
  import WorkSummaryCard from "$lib/components/WorkSummaryCard.svelte";
  import { groupedWorkSummaryCards } from "$lib/workSummaryCards.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";

  let { items = [], now = Date.now() } = $props();

  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  /*
   * Pointing at a card reads it, so the click has nothing left to wait for.
   * Costs nothing when the card is already cached or already in flight.
   */
  function prefetch(ref) {
    if (ref) void prefetchWork(ref, coreClient);
  }

  let grouped = $derived(
    groupedWorkSummaryCards(items, {
      now,
      href: (ref) => workspaceHref(`/tasks/${encodeURIComponent(ref)}`),
    }),
  );
</script>

<!--
  `data-initiative-group` marks the block, and sits on the fold rather than on
  the grid inside it: a collapsed `<details>` hides its own children, so an
  attribute on the inner list is invisible until somebody opens the fold —
  which is exactly when a check for "is the Done block there" cannot see it.
-->
{#snippet grid(cards, label)}
  <ul class="cards" aria-label={label}>
    {#each cards as card (card.ref)}
      <li><WorkSummaryCard {card} {now} onprefetch={prefetch} /></li>
    {/each}
  </ul>
{/snippet}

{#if grouped.attention.length}
  <div data-initiative-group="attention">
    {@render grid(grouped.attention, "Open initiatives")}
  </div>
{:else if !grouped.done.length && !grouped.noPlan.length}
  <p class="cards-empty">No open initiatives.</p>
{/if}

{#if grouped.done.length}
  <details class="cards-fold" data-initiative-group="done">
    <summary>Done <span>({grouped.done.length})</span> </summary>
    {@render grid(grouped.done, "Finished initiatives")}
  </details>
{/if}

{#if grouped.noPlan.length}
  <details class="cards-fold" data-initiative-group="no-plan">
    <summary>No plan <span>({grouped.noPlan.length})</span> </summary>
    {@render grid(grouped.noPlan, "Initiatives with no plan")}
  </details>
{/if}

<style>
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(17rem, 1fr));
    gap: 10px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .cards-empty {
    margin: 0;
    color: var(--fg-muted);
    font-size: 12px;
  }
  /* Done and planless cards: present, counted, out of the way. */
  .cards-fold {
    margin-top: 10px;
    border-top: 1px solid var(--line-subtle);
  }
  .cards-fold summary {
    cursor: pointer;
    padding: 8px 0;
    color: var(--fg-muted);
    font-size: 11px;
  }
  .cards-fold summary span {
    color: var(--fg-subtle);
  }
  .cards-fold[open] summary {
    margin-bottom: 8px;
  }
</style>
