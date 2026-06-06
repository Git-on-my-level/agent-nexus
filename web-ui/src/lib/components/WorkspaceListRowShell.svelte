<script>
  /**
   * List row shell: `group/row` hover highlight + optional overlaid action bar.
   * Keeps quick-actions as a sibling of the row link (no nested interactives).
   */

  import RowActionBar from "$lib/components/RowActionBar.svelte";

  /** @type {{
   *   class?: string,
   *   row?: import('svelte').Snippet,
   *   actions?: import('svelte').Snippet | null,
   * }} */
  let { class: extraClass = "", row, actions = null } = $props();
</script>

<div class="group/row relative flex min-w-0 items-stretch {extraClass}">
  {@render row?.()}
  {#if actions}
    <RowActionBar groupName="row" class="top-2">
      {@render actions()}
    </RowActionBar>
  {/if}
</div>
