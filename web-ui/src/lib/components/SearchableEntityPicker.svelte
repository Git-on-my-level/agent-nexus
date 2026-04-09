<script>
  let {
    label,
    helperText = "",
    placeholder = "Search",
    emptyText = "No matches found.",
    advancedLabel = "Use a manual ID instead",
    manualLabel = "Manual ID",
    manualPlaceholder = "Enter an ID",
    value = $bindable(""),
    items = [],
    disabledIds = [],
    searchFn = null,
  } = $props();

  let query = $state("");
  let remoteItems = $state([]);
  let searchLoading = $state(false);
  let searchError = $state("");
  let selectedItemOverride = $state(null);
  let searchTimer = null;
  let latestSearchRequestId = 0;
  let disabledIdSet = $derived(
    new Set((disabledIds ?? []).map((item) => String(item))),
  );
  let selectedItem = $derived(
    (items ?? []).find((item) => String(item.id) === String(value)) ??
      remoteItems.find((item) => String(item.id) === String(value)) ??
      (selectedItemOverride && String(selectedItemOverride.id) === String(value)
        ? selectedItemOverride
        : null),
  );

  $effect(() => {
    if (!searchFn) {
      remoteItems = [];
      searchLoading = false;
      searchError = "";
      return;
    }

    const needle = query.trim();
    if (searchTimer) {
      clearTimeout(searchTimer);
      searchTimer = null;
    }

    if (!needle) {
      remoteItems = [];
      searchLoading = false;
      searchError = "";
      return;
    }

    const requestID = ++latestSearchRequestId;
    searchLoading = true;
    searchError = "";
    searchTimer = setTimeout(async () => {
      try {
        const results = (await searchFn(needle)) ?? [];
        if (requestID !== latestSearchRequestId) {
          return;
        }
        remoteItems = results;
      } catch (error) {
        if (requestID !== latestSearchRequestId) {
          return;
        }
        const reason = error instanceof Error ? error.message : String(error);
        searchError = `Search failed: ${reason}`;
        remoteItems = [];
      } finally {
        if (requestID === latestSearchRequestId) {
          searchLoading = false;
        }
      }
    }, 300);

    return () => {
      if (searchTimer) {
        clearTimeout(searchTimer);
        searchTimer = null;
      }
    };
  });

  let filteredItems = $derived.by(() => {
    const availableItems = (searchFn ? remoteItems : (items ?? [])).filter(
      (item) => !disabledIdSet.has(String(item.id)),
    );
    if (searchFn) {
      return availableItems.slice(0, 8);
    }

    const needle = query.trim().toLowerCase();
    if (!needle) {
      return availableItems.slice(0, 8);
    }

    return availableItems
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

  function chooseItem(id) {
    value = String(id ?? "").trim();
    selectedItemOverride =
      filteredItems.find((item) => String(item.id) === value) ?? null;
    query = "";
    searchError = "";
  }

  function clearSelection() {
    value = "";
    selectedItemOverride = null;
    query = "";
    searchError = "";
  }

  function manualValue() {
    return selectedItem ? "" : value;
  }
</script>

<div class="space-y-2">
  <div class="flex items-center justify-between gap-3">
    <div>
      <p class="text-[12px] font-medium text-[var(--ui-text-muted)]">{label}</p>
      {#if helperText}
        <p class="mt-0.5 text-[11px] text-[var(--ui-text-muted)]">
          {helperText}
        </p>
      {/if}
    </div>

    {#if value}
      <button
        class="rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2 py-1 text-[11px] text-[var(--ui-text-muted)] transition-colors hover:text-[var(--ui-text)]"
        onclick={clearSelection}
        type="button"
      >
        Clear
      </button>
    {/if}
  </div>

  {#if value}
    <div
      class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-3 py-2"
    >
      {#if selectedItem}
        <p class="text-[12px] font-medium text-[var(--ui-text)]">
          {selectedItem.title || selectedItem.id}
        </p>
        <p class="mt-0.5 text-[11px] text-[var(--ui-text-muted)]">
          {selectedItem.id}
          {#if selectedItem.subtitle}
            · {selectedItem.subtitle}
          {/if}
        </p>
      {:else}
        <p class="text-[12px] font-medium text-[var(--ui-text)]">
          Manual ID selected
        </p>
        <p class="mt-0.5 font-mono text-[11px] text-[var(--ui-text-muted)]">
          {value}
        </p>
      {/if}
    </div>
  {/if}

  <label class="block">
    <span class="sr-only">{label} search</span>
    <input
      aria-label={`${label} search`}
      bind:value={query}
      class="w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)] placeholder:text-[var(--ui-text-subtle)]"
      {placeholder}
      type="text"
    />
  </label>

  {#if searchLoading}
    <div class="text-[11px] text-[var(--ui-text-muted)]">Searching…</div>
  {/if}

  {#if searchError}
    <div class="text-[11px] text-red-400">{searchError}</div>
  {/if}

  <div
    class="max-h-48 overflow-y-auto rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
  >
    {#if filteredItems.length === 0}
      <div class="px-3 py-3 text-[12px] text-[var(--ui-text-muted)]">
        {emptyText}
      </div>
    {:else}
      {#each filteredItems as item, index}
        <button
          class="flex w-full items-start justify-between gap-3 px-3 py-2 text-left transition-colors hover:bg-[var(--ui-border-subtle)] {index >
          0
            ? 'border-t border-[var(--ui-border)]'
            : ''} {value === item.id ? 'bg-indigo-500/10' : ''}"
          onclick={() => chooseItem(item.id)}
          type="button"
        >
          <div class="min-w-0">
            <p class="truncate text-[12px] font-medium text-[var(--ui-text)]">
              {item.title || item.id}
            </p>
            <p class="mt-0.5 truncate text-[11px] text-[var(--ui-text-muted)]">
              {item.id}
              {#if item.subtitle}
                · {item.subtitle}
              {/if}
            </p>
          </div>
          {#if value === item.id}
            <span
              class="rounded bg-indigo-500/10 px-1.5 py-0.5 text-[11px] text-indigo-300"
            >
              Selected
            </span>
          {/if}
        </button>
      {/each}
    {/if}
  </div>

  <details
    class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
  >
    <summary
      class="cursor-pointer px-3 py-2 text-[11px] text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]"
    >
      {advancedLabel}
    </summary>
    <div class="space-y-2 border-t border-[var(--ui-border)] px-3 py-3">
      <label class="block text-[12px] font-medium text-[var(--ui-text-muted)]">
        {manualLabel}
        <input
          aria-label={manualLabel}
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)] placeholder:text-[var(--ui-text-subtle)]"
          oninput={(event) => {
            selectedItemOverride = null;
            value = event.currentTarget.value.trim();
          }}
          placeholder={manualPlaceholder}
          type="text"
          value={manualValue()}
        />
      </label>
      <p class="text-[11px] text-[var(--ui-text-muted)]">
        Use this only for expert or debugging cases when the normal picker is
        not enough.
      </p>
    </div>
  </details>
</div>
