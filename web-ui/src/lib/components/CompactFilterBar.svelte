<script>
  /**
   * Shared bordered panel for list-page filters (inbox, topics, artifacts).
   * Render controls as {@render children()}; keep a single clear/apply row inside.
   */
  let { testId = undefined, children } = $props();
</script>

<div
  class="mb-4 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] p-3"
  data-testid={testId}
  data-ui="compact-filter-bar"
>
  {@render children?.()}
</div>
