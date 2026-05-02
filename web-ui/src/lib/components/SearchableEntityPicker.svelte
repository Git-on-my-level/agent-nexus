<script>
  let {
    /** @type {'single' | 'multi'} */
    mode = "single",
    label,
    helperText = "",
    placeholder = "Search",
    emptyText = "No matches found.",
    advancedLabel = "Use a manual ID instead",
    manualLabel = "Manual ID",
    manualPlaceholder = "Enter an ID",
    addManualLabel = "Add ID",
    /** When false, hides raw ID entry (search only). */
    showManualEntry = true,
    /** Max rows when the search box is empty (multi only). */
    idleSuggestionLimit = 3,
    /** Max rows when the user is searching (multi only). */
    searchResultLimit = 12,
    value = $bindable(""),
    values = $bindable([]),
    items = [],
    disabledIds = [],
    searchFn = null,
  } = $props();

  let query = $state("");
  let remoteItems = $state([]);
  let searchLoading = $state(false);
  let searchError = $state("");
  let selectedItemOverride = $state(null);
  let manualEntry = $state("");
  let searchTimer = null;
  let latestSearchRequestId = 0;

  let disabledIdSet = $derived(
    new Set((disabledIds ?? []).map((item) => String(item))),
  );
  let selectedIdSet = $derived(
    new Set((values ?? []).map((item) => String(item))),
  );
  let selectedItems = $derived(
    (values ?? []).map((id) => {
      const matched = items.find((item) => item.id === id);
      return matched ?? { id, title: id, subtitle: "Manual ID" };
    }),
  );
  let availableItems = $derived(
    (items ?? []).filter((item) => !selectedIdSet.has(String(item.id))),
  );

  let selectedItem = $derived(
    mode === "single"
      ? ((items ?? []).find((item) => String(item.id) === String(value)) ??
          remoteItems.find((item) => String(item.id) === String(value)) ??
          (selectedItemOverride &&
          String(selectedItemOverride.id) === String(value)
            ? selectedItemOverride
            : null))
      : null,
  );

  $effect(() => {
    if (mode !== "single" || !searchFn) {
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
    if (mode === "multi") {
      const needle = query.trim().toLowerCase();
      const available = availableItems;
      if (!needle) {
        return available.slice(0, idleSuggestionLimit);
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
        .slice(0, searchResultLimit);
    }

    const availableItemsSingle = (
      searchFn ? remoteItems : (items ?? [])
    ).filter((item) => !disabledIdSet.has(String(item.id)));
    if (searchFn) {
      return availableItemsSingle.slice(0, 8);
    }

    const needle = query.trim().toLowerCase();
    if (!needle) {
      return availableItemsSingle.slice(0, 8);
    }

    return availableItemsSingle
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

  let showIdleSearchHint = $derived(
    mode === "multi" &&
      !query.trim() &&
      availableItems.length > idleSuggestionLimit &&
      filteredItems.length > 0,
  );

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
  {#if mode === "multi"}
    <div>
      <p class="text-micro font-medium text-fg-muted">{label}</p>
      {#if helperText}
        <p class="mt-0.5 text-micro text-fg-muted">
          {helperText}
        </p>
      {/if}
    </div>

    {#if selectedItems.length > 0}
      <div class="flex flex-wrap gap-2">
        {#each selectedItems as item}
          <span
            class="inline-flex items-center gap-2 rounded-md border border-line bg-panel px-2.5 py-1 text-micro text-fg"
          >
            <span>{item.title || item.id}</span>
            <button
              aria-label={`Remove ${item.title || item.id}`}
              class="text-fg-muted transition-colors hover:text-fg"
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
        class="ui-input"
        {placeholder}
        type="text"
      />
    </label>

    <div
      class="max-h-48 overflow-y-auto rounded-md border border-line bg-panel"
    >
      {#if filteredItems.length === 0}
        <div class="px-3 py-3 text-micro text-fg-muted">
          {emptyText}
        </div>
      {:else}
        {#each filteredItems as item, index}
          <button
            class="flex w-full items-start justify-between gap-3 px-3 py-2 text-left transition-colors hover:bg-bg-soft {index >
            0
              ? 'border-t border-line'
              : ''}"
            onclick={() => addValue(item.id)}
            type="button"
          >
            <div class="min-w-0">
              <p class="truncate text-micro font-medium text-fg">
                {item.title || item.id}
              </p>
              <p class="mt-0.5 truncate text-micro text-fg-muted">
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

    {#if showIdleSearchHint}
      <p class="text-micro text-fg-muted">Type to search for more.</p>
    {/if}

    {#if showManualEntry}
      <details class="rounded-md border border-line bg-panel">
        <summary
          class="cursor-pointer px-3 py-2 text-micro text-fg-muted hover:text-fg"
        >
          {advancedLabel}
        </summary>
        <div
          class="space-y-2 border-t border-line px-3 py-3 md:flex md:items-end md:gap-2 md:space-y-0"
        >
          <label class="block flex-1 text-micro font-medium text-fg-muted">
            {manualLabel}
            <input
              aria-label={manualLabel}
              bind:value={manualEntry}
              class="ui-input mt-1"
              placeholder={manualPlaceholder}
              type="text"
            />
          </label>
          <button
            class="rounded-md bg-accent-solid px-3 py-2 text-micro font-medium text-white transition-colors hover:bg-accent"
            onclick={() => addValue(manualEntry)}
            type="button"
          >
            {addManualLabel}
          </button>
        </div>
      </details>
    {/if}
  {:else}
    <div class="flex items-center justify-between gap-3">
      <div>
        <p class="text-micro font-medium text-fg-muted">{label}</p>
        {#if helperText}
          <p class="mt-0.5 text-micro text-fg-muted">
            {helperText}
          </p>
        {/if}
      </div>

      {#if value}
        <button
          class="rounded border border-line bg-panel px-2 py-1 text-micro text-fg-muted transition-colors hover:text-fg"
          onclick={clearSelection}
          type="button"
        >
          Clear
        </button>
      {/if}
    </div>

    {#if value}
      <div class="ui-panel px-3 py-2">
        {#if selectedItem}
          <p class="text-micro font-medium text-fg">
            {selectedItem.title || selectedItem.id}
          </p>
          <p class="mt-0.5 text-micro text-fg-muted">
            {selectedItem.id}
            {#if selectedItem.subtitle}
              · {selectedItem.subtitle}
            {/if}
          </p>
        {:else}
          <p class="text-micro font-medium text-fg">Manual ID selected</p>
          <p class="mt-0.5 font-mono text-micro text-fg-muted">
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
        class="ui-input"
        {placeholder}
        type="text"
      />
    </label>

    {#if searchLoading}
      <div class="text-micro text-fg-muted">Searching…</div>
    {/if}

    {#if searchError}
      <div class="text-micro text-danger-text">{searchError}</div>
    {/if}

    <div
      class="max-h-48 overflow-y-auto rounded-md border border-line bg-panel"
    >
      {#if filteredItems.length === 0}
        <div class="px-3 py-3 text-micro text-fg-muted">
          {emptyText}
        </div>
      {:else}
        {#each filteredItems as item, index}
          <button
            class="flex w-full items-start justify-between gap-3 px-3 py-2 text-left transition-colors hover:bg-bg-soft {index >
            0
              ? 'border-t border-line'
              : ''} {value === item.id ? 'bg-accent-soft' : ''}"
            onclick={() => chooseItem(item.id)}
            type="button"
          >
            <div class="min-w-0">
              <p class="truncate text-micro font-medium text-fg">
                {item.title || item.id}
              </p>
              <p class="mt-0.5 truncate text-micro text-fg-muted">
                {item.id}
                {#if item.subtitle}
                  · {item.subtitle}
                {/if}
              </p>
            </div>
            {#if value === item.id}
              <span
                class="rounded bg-accent-soft px-1.5 py-0.5 text-micro text-accent-text"
              >
                Selected
              </span>
            {/if}
          </button>
        {/each}
      {/if}
    </div>

    {#if showManualEntry}
      <details class="rounded-md border border-line bg-panel">
        <summary
          class="cursor-pointer px-3 py-2 text-micro text-fg-muted hover:text-fg"
        >
          {advancedLabel}
        </summary>
        <div class="space-y-2 border-t border-line px-3 py-3">
          <label class="block text-micro font-medium text-fg-muted">
            {manualLabel}
            <input
              aria-label={manualLabel}
              class="ui-input mt-1"
              oninput={(event) => {
                selectedItemOverride = null;
                value = event.currentTarget.value.trim();
              }}
              placeholder={manualPlaceholder}
              type="text"
              value={manualValue()}
            />
          </label>
          <p class="text-micro text-fg-muted">
            Use this only for expert or debugging cases when the normal picker
            is not enough.
          </p>
        </div>
      </details>
    {/if}
  {/if}
</div>
