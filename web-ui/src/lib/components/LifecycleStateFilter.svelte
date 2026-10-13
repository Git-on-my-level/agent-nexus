<script>
  /**
   * Active / Archived / Trashed, the lifecycle filter Docs and Threads share.
   * Laid out on one row so it sits beside the other controls inside
   * `WorkspaceListFilters` instead of claiming a bordered block of its own.
   *
   * `ontoggle` receives the state that was clicked; the page decides what the
   * next selection is and puts it in the URL. The last checked box cannot be
   * unchecked — an empty lifecycle means "no rows", which reads as a broken
   * list rather than a filter.
   */
  import { BOARD_LIFECYCLE_STATE_LABELS } from "$lib/boardUtils";

  let { states = ["active"], ontoggle } = $props();
</script>

<div class="flex items-center gap-2" role="group" aria-label="Lifecycle">
  <span class="ui-label mb-0">Lifecycle</span>
  {#each Object.entries(BOARD_LIFECYCLE_STATE_LABELS) as [value, label] (value)}
    <label class="flex cursor-pointer items-center gap-1.5 text-meta text-fg">
      <input
        checked={(states ?? ["active"]).includes(value)}
        class="h-3.5 w-3.5 cursor-pointer rounded border-line bg-bg text-accent-hover focus:ring-2 focus:ring-accent focus:ring-offset-0"
        type="checkbox"
        onchange={() => ontoggle?.(value)}
      />
      {label}
    </label>
  {/each}
</div>
