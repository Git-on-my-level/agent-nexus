<script>
  /**
   * Compact toolbar when one or more list rows are selected.
   * Optional selection chrome: empty strip with “Select all”, or links beside counts.
   */
  let {
    selectedCount = 0,
    busy = false,
    canArchive = false,
    canUnarchive = false,
    canTrash = false,
    canRestore = false,
    onClear = () => {},
    onArchive = () => {},
    onUnarchive = () => {},
    onTrash = () => {},
    onRestore = () => {},
    trashLabel = "Move to trash",
    selectionChromeActive = false,
    allVisibleSelected = false,
    onSelectAll = () => {},
    onDeselectAll = () => {},
    extraActions,
  } = $props();
</script>

{#if selectionChromeActive && selectedCount === 0}
  <div
    class="mb-2 flex flex-wrap items-center justify-end rounded-md border border-line bg-bg-soft px-3 py-1.5"
  >
    <button
      class="inline-flex h-7 cursor-pointer items-center rounded-md px-2 text-micro font-medium text-accent-text transition-colors hover:bg-line-subtle hover:text-accent-text disabled:cursor-not-allowed disabled:opacity-50"
      disabled={busy}
      onclick={onSelectAll}
      type="button"
    >
      Select all
    </button>
  </div>
{/if}

{#if selectedCount > 0}
  <div
    class="mb-2 flex flex-wrap items-center justify-between gap-2 rounded-md border border-line bg-bg-soft px-3 py-1.5"
    role="toolbar"
    aria-label="Bulk actions"
  >
    <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
      <span class="inline-flex h-7 items-center text-micro font-medium text-fg">
        {selectedCount} selected
      </span>
      {#if selectionChromeActive}
        {#if !allVisibleSelected}
          <button
            class="inline-flex h-7 cursor-pointer items-center rounded-md px-2 text-micro font-medium text-accent-text transition-colors hover:bg-line-subtle hover:text-accent-text disabled:cursor-not-allowed disabled:opacity-50"
            disabled={busy}
            onclick={onSelectAll}
            type="button"
          >
            Select all
          </button>
        {/if}
        <button
          class="inline-flex h-7 cursor-pointer items-center rounded-md px-2 text-micro font-medium text-fg-muted transition-colors hover:bg-line-subtle hover:text-fg disabled:cursor-not-allowed disabled:opacity-50"
          disabled={busy}
          onclick={onDeselectAll}
          type="button"
        >
          Deselect all
        </button>
      {/if}
    </div>
    <div class="flex flex-wrap items-center gap-1.5">
      {#if canRestore}
        <button
          class="inline-flex h-7 cursor-pointer items-center rounded-md border border-line bg-panel px-2.5 text-micro font-medium text-fg transition-colors hover:bg-line disabled:cursor-not-allowed disabled:opacity-50"
          disabled={busy}
          onclick={onRestore}
          type="button"
        >
          Restore
        </button>
      {/if}
      {#if canArchive}
        <button
          class="inline-flex h-7 cursor-pointer items-center rounded-md border border-line bg-panel px-2.5 text-micro font-medium text-fg transition-colors hover:bg-line disabled:cursor-not-allowed disabled:opacity-50"
          disabled={busy}
          onclick={onArchive}
          type="button"
        >
          Archive
        </button>
      {/if}
      {#if canUnarchive}
        <button
          class="inline-flex h-7 cursor-pointer items-center rounded-md border border-line bg-panel px-2.5 text-micro font-medium text-fg transition-colors hover:bg-line disabled:cursor-not-allowed disabled:opacity-50"
          disabled={busy}
          onclick={onUnarchive}
          type="button"
        >
          Unarchive
        </button>
      {/if}
      {#if canTrash}
        <button
          class="inline-flex h-7 cursor-pointer items-center rounded-md border border-danger-text bg-danger-soft px-2.5 text-micro font-medium text-danger-text transition-colors hover:bg-danger-soft disabled:cursor-not-allowed disabled:opacity-50"
          disabled={busy}
          onclick={onTrash}
          type="button"
        >
          {trashLabel}
        </button>
      {/if}
      {#if extraActions}{@render extraActions()}{/if}
      <button
        class="inline-flex h-7 cursor-pointer items-center rounded-md px-2.5 text-micro font-medium text-fg-muted transition-colors hover:bg-line-subtle hover:text-fg disabled:cursor-not-allowed disabled:opacity-50"
        disabled={busy}
        onclick={onClear}
        type="button"
      >
        Clear
      </button>
    </div>
  </div>
{/if}
