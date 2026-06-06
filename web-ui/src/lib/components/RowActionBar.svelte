<script>
  /**
   * Hover/focus-revealed action cluster for cards and list rows.
   * On touch devices (no hover) the bar stays visible.
   */

  /** @type {{
   *   groupName?: 'row' | 'msg',
   *   class?: string,
   *   children?: import('svelte').Snippet,
   * }} */
  let { groupName = "row", class: extraClass = "", children } = $props();

  // Full literal class names so Tailwind's content scanner emits them.
  // Interpolating the group name into the variant is not statically
  // detectable, so that rule would never be generated.
  const HOVER_REVEAL = {
    row: "group-hover/row:opacity-100",
    msg: "group-hover/msg:opacity-100",
  };

  let hoverClass = $derived(HOVER_REVEAL[groupName] ?? HOVER_REVEAL.row);
</script>

<div
  class="row-action-bar absolute right-1.5 top-1.5 z-10 flex items-center gap-0.5 rounded-md border border-line bg-panel/95 px-0.5 opacity-0 shadow-sm transition-opacity duration-150 focus-within:opacity-100 {hoverClass} {extraClass}"
>
  {@render children?.()}
</div>

<style>
  @media (hover: none) {
    :global(.row-action-bar) {
      opacity: 1;
    }
  }
</style>
