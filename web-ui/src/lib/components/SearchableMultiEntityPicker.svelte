<script>
  let {
    label,
    helperText = "",
    placeholder = "Search",
    emptyText = "No matches found.",
    advancedLabel = "Add a manual ID instead",
    manualLabel = "Manual ID",
    manualPlaceholder = "Enter an ID",
    addManualLabel = "Add ID",
    values = $bindable([]),
    items = [],
  } = $props();

  let query = $state("");
  let manualEntry = $state("");
  let selectedIdSet = $derived(
    new Set((values ?? []).map((item) => String(item))),
  );
  let selectedItems = $derived(
    (values ?? []).map((id) => {
      const matched = items.find((item) => item.id === id);
      return matched ?? { id, title: id, subtitle: "Manual ID" };
    }),
  );
  let filteredItems = $derived.by(() => {
    const needle = query.trim().toLowerCase();
    const available = (items ?? []).filter(
      (item) => !selectedIdSet.has(String(item.id)),
    );
    if (!needle) {
      return available.slice(0, 8);
    }

    return available
      .filter((item) => {
        const haystack = [
          item.id,
          item.title,
          item.subtitle,
          ...(item.keywords ?? []),
        ]
          .filter(Boolean)
          .join(" ")
          .toLowerCase();
        return haystack.includes(needle);
      })
      .slice(0, 8);
  });

  function addValue(id) {
    const next = String(id ?? "").trim();
    if (!next || selectedIdSet.has(next)) {
      return;
    }
    values = [...(values ?? []), next];
    query = "";
    manualEntry = "";
  }

  function removeValue(id) {
    values = (values ?? []).filter((item) => item !== id);
  }
</script>

<div class="space-y-2">
  <div>
    <p class="text-micro font-medium text-[var(--fg-muted)]">{label}</p>
    {#if helperText}
      <p class="mt-0.5 text-micro text-[var(--fg-muted)]">
        {helperText}
      </p>
    {/if}
  </div>

  {#if selectedItems.length > 0}
    <div class="flex flex-wrap gap-2">
      {#each selectedItems as item}
        <span
          class="inline-flex items-center gap-2 rounded-md border border-[var(--line)] bg-[var(--panel)] px-2.5 py-1 text-micro text-[var(--fg)]"
        >
          <span>{item.title || item.id}</span>
          <button
            aria-label={`Remove ${item.title || item.id}`}
            class="text-[var(--fg-muted)] transition-colors hover:text-[var(--fg)]"
            onclick={() => removeValue(item.id)}
            type="button"
          >
            ×
          </button>
        </span>
      {/each}
    </div>
  {/if}

  <label class="block">
    <span class="sr-only">{label} search</span>
    <input
      aria-label={`${label} search`}
      bind:value={query}
      class="w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-meta text-[var(--fg)] placeholder:text-[var(--fg-subtle)]"
      {placeholder}
      type="text"
    />
  </label>

  <div
    class="max-h-48 overflow-y-auto rounded-md border border-[var(--line)] bg-[var(--panel)]"
  >
    {#if filteredItems.length === 0}
      <div class="px-3 py-3 text-micro text-[var(--fg-muted)]">
        {emptyText}
      </div>
    {:else}
      {#each filteredItems as item, index}
        <button
          class="flex w-full items-start justify-between gap-3 px-3 py-2 text-left transition-colors hover:bg-[var(--line-subtle)] {index >
          0
            ? 'border-t border-[var(--line)]'
            : ''}"
          onclick={() => addValue(item.id)}
          type="button"
        >
          <div class="min-w-0">
            <p class="truncate text-micro font-medium text-[var(--fg)]">
              {item.title || item.id}
            </p>
            <p class="mt-0.5 truncate text-micro text-[var(--fg-muted)]">
              {item.id}
              {#if item.subtitle}
                · {item.subtitle}
              {/if}
            </p>
          </div>
          <span
            class="rounded bg-accent-soft px-1.5 py-0.5 text-micro text-accent-text"
          >
            Add
          </span>
        </button>
      {/each}
    {/if}
  </div>

  <details class="rounded-md border border-[var(--line)] bg-[var(--panel)]">
    <summary
      class="cursor-pointer px-3 py-2 text-micro text-[var(--fg-muted)] hover:text-[var(--fg)]"
    >
      {advancedLabel}
    </summary>
    <div
      class="space-y-2 border-t border-[var(--line)] px-3 py-3 md:flex md:items-end md:gap-2 md:space-y-0"
    >
      <label class="block flex-1 text-micro font-medium text-[var(--fg-muted)]">
        {manualLabel}
        <input
          aria-label={manualLabel}
          bind:value={manualEntry}
          class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-meta text-[var(--fg)] placeholder:text-[var(--fg-subtle)]"
          placeholder={manualPlaceholder}
          type="text"
        />
      </label>
      <button
        class="rounded-md bg-accent px-3 py-2 text-micro font-medium text-white transition-colors hover:bg-accent-hover"
        onclick={() => addValue(manualEntry)}
        type="button"
      >
        {addManualLabel}
      </button>
    </div>
  </details>
</div>
