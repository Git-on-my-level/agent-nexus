<script>
  /**
   * Shared two-line list row body: title + optional badge slot, then one-line description.
   * @type {{
   *   title: string,
   *   description?: string,
   *   emptyDescription?: string,
   *   badges?: import('svelte').Snippet,
   *   titleClass?: string,
   * }}
   */
  let {
    title,
    description = "",
    emptyDescription = "No description provided.",
    badges,
    titleClass = "",
  } = $props();

  let text = $derived(String(description ?? "").trim());
</script>

<div class="min-w-0 flex-1">
  <div class="inline-flex max-w-full min-w-0 items-center gap-x-2 gap-y-1">
    <p class="min-w-0 truncate text-meta font-medium text-fg {titleClass}">
      {title}
    </p>
    {#if badges}
      <div class="flex shrink-0 items-center gap-2">
        {@render badges()}
      </div>
    {/if}
  </div>
  {#if text}
    <p class="truncate text-micro text-fg-muted">
      {text}
    </p>
  {:else if emptyDescription}
    <p class="hidden truncate text-micro text-fg-muted sm:block">
      {emptyDescription}
    </p>
  {/if}
</div>
