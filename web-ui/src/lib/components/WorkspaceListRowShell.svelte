<script>
  /**
   * List row shell: `group/row` hover highlight with a right-edge slot that
   * swaps resting metadata (e.g. timestamp) for quick actions on hover/focus,
   * plus a right-click / long-press context menu. Quick-actions stay a sibling
   * of the row link (no nested interactives).
   */

  import ContextMenuHost from "$lib/components/ContextMenuHost.svelte";

  /** @type {{
   *   class?: string,
   *   row?: import('svelte').Snippet,
   *   meta?: import('svelte').Snippet | null,
   *   actions?: import('svelte').Snippet | null,
   *   contextMenuItems?: Array<{ key: string, label: string, onSelect: () => void, danger?: boolean, disabled?: boolean }>,
   * }} */
  let {
    class: extraClass = "",
    row,
    meta = null,
    actions = null,
    contextMenuItems = [],
  } = $props();

  let hasContextMenu = $derived(
    Array.isArray(contextMenuItems) && contextMenuItems.length > 0,
  );
</script>

<ContextMenuHost items={contextMenuItems} disabled={!hasContextMenu}>
  <div class="group/row relative flex min-w-0 items-stretch {extraClass}">
    {@render row?.()}
    {#if meta || actions}
      <div
        class="relative flex shrink-0 items-start justify-end pt-2.5 pr-3 sm:pr-4"
      >
        {#if meta}
          <div
            class="text-micro transition-opacity {actions
              ? 'group-hover/row:opacity-0 group-focus-within/row:opacity-0'
              : ''}"
          >
            {@render meta()}
          </div>
        {/if}
        {#if actions}
          <div
            class="absolute inset-0 flex items-start justify-end pt-2 pr-3 opacity-0 transition-opacity duration-150 group-focus-within/row:opacity-100 group-hover/row:opacity-100 sm:pr-4"
          >
            {@render actions()}
          </div>
        {/if}
      </div>
    {/if}
  </div>
</ContextMenuHost>
