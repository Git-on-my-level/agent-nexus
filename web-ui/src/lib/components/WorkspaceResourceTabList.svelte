<script>
  /**
   * Shared underline tab strip for resource detail pages (topics, etc.).
   * `dense` tightens spacing and enables horizontal scroll on narrow viewports.
   */
  let {
    ariaLabel = "Sections",
    tabs = [],
    activeTab = "",
    onTabChange,
    dense = false,
  } = $props();
</script>

<div
  class="{dense
    ? 'mt-0.5'
    : 'mt-1'} flex gap-0 overflow-x-auto border-b border-line [-ms-overflow-style:none] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
  aria-label={ariaLabel}
  role="tablist"
>
  {#each tabs as tab (tab.id)}
    <button
      class={`relative shrink-0 cursor-pointer px-2 py-1 text-micro font-medium transition-colors sm:px-2.5 ${activeTab === tab.id ? "text-fg" : "text-fg-muted hover:text-fg"}`}
      onclick={() => onTabChange?.(tab.id)}
      type="button"
      role="tab"
      aria-selected={activeTab === tab.id}
      tabindex={activeTab === tab.id ? 0 : -1}
    >
      {tab.label}{#if tab.badge !== undefined && tab.badge > 0}
        <span class="ml-0.5 tabular-nums text-fg-muted">({tab.badge})</span>
      {/if}
      {#if activeTab === tab.id}
        <span
          class="pointer-events-none absolute inset-x-0 -bottom-px h-0.5 bg-accent-solid"
        ></span>
      {/if}
    </button>
  {/each}
</div>
