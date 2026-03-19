<script>
  import { onMount } from "svelte";

  let {
    value = $bindable(""),
    searchFn,
    placeholder = "Search...",
    idField = "id",
    labelField = "title",
    showAdvanced = false,
    advancedLabel = "Or enter ID manually:",
    advancedPlaceholder = "id-here",
    onValueChange = () => {},
  } = $props();

  let searchResults = $state([]);
  let searchQuery = $state("");
  let searchDebounceTimer = null;
  let searchLoading = $state(false);
  let searchError = $state("");
  let latestSearchRequestId = 0;

  function debounceSearch(query) {
    const requestId = ++latestSearchRequestId;
    if (searchDebounceTimer) {
      clearTimeout(searchDebounceTimer);
    }

    if (!query || query.trim().length === 0) {
      searchResults = [];
      searchLoading = false;
      searchError = "";
      return;
    }

    searchDebounceTimer = setTimeout(async () => {
      searchLoading = true;
      searchError = "";

      try {
        const results = await searchFn(query.trim());
        if (requestId !== latestSearchRequestId) {
          return;
        }
        searchResults = results || [];
      } catch (e) {
        if (requestId !== latestSearchRequestId) {
          return;
        }
        searchError = `Search failed: ${e instanceof Error ? e.message : String(e)}`;
        searchResults = [];
      } finally {
        if (requestId === latestSearchRequestId) {
          searchLoading = false;
        }
      }
    }, 300);
  }

  function handleSearchInput(event) {
    searchQuery = event.target.value;
    debounceSearch(searchQuery);
  }

  function handleSelection(event) {
    const selectedId = event.target.value;
    const selectedItem = searchResults.find(
      (item) => item[idField] === selectedId,
    );
    if (selectedItem) {
      value = selectedId;
      onValueChange(selectedItem);
    }
  }

  function handleManualInput(event) {
    value = event.target.value;
    onValueChange(null);
  }

  onMount(() => {
    return () => {
      if (searchDebounceTimer) {
        clearTimeout(searchDebounceTimer);
      }
    };
  });
</script>

<div class="debounced-search-picker">
  <input
    type="text"
    class="search-input"
    {placeholder}
    value={searchQuery}
    oninput={handleSearchInput}
  />

  {#if searchLoading}
    <div class="search-status">Searching...</div>
  {/if}

  {#if searchError}
    <div class="search-error">{searchError}</div>
  {/if}

  {#if searchResults.length > 0}
    <select
      class="search-results"
      size={Math.min(searchResults.length, 5)}
      onchange={handleSelection}
    >
      {#each searchResults as item}
        <option value={item[idField]}>
          {item[labelField]} ({item[idField]})
        </option>
      {/each}
    </select>
  {/if}

  {#if value}
    <div class="selected-value">
      <span class="selected-label">Selected:</span>
      <span class="selected-id">{value}</span>
    </div>
  {/if}

  {#if showAdvanced}
    <details class="advanced-section">
      <summary class="advanced-summary">{advancedLabel}</summary>
      <input
        type="text"
        class="manual-input"
        placeholder={advancedPlaceholder}
        {value}
        oninput={handleManualInput}
      />
    </details>
  {/if}
</div>

<style>
  .debounced-search-picker {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .search-input,
  .manual-input {
    width: 100%;
    padding: 0.5rem 0.75rem;
    border-radius: 0.375rem;
    border: 1px solid var(--ui-border);
    background: var(--ui-panel-muted);
    color: var(--ui-text);
    font-size: 0.8125rem;
  }

  .search-input:focus,
  .manual-input:focus {
    outline: none;
    border-color: #6366f1;
  }

  .search-results {
    width: 100%;
    padding: 0.5rem;
    border-radius: 0.375rem;
    border: 1px solid var(--ui-border);
    background: var(--ui-panel-muted);
    color: var(--ui-text);
    font-size: 0.8125rem;
  }

  .search-results option {
    padding: 0.25rem 0.5rem;
  }

  .search-results option:hover {
    background: #6366f1;
    color: white;
  }

  .search-status {
    font-size: 0.75rem;
    color: var(--ui-text-muted);
    padding: 0.25rem 0;
  }

  .search-error {
    font-size: 0.75rem;
    color: #f87171;
    padding: 0.25rem 0;
  }

  .selected-value {
    font-size: 0.75rem;
    padding: 0.25rem 0;
    color: var(--ui-text-muted);
  }

  .selected-label {
    font-weight: 500;
  }

  .selected-id {
    margin-left: 0.25rem;
    font-family: monospace;
  }

  .advanced-section {
    margin-top: 0.5rem;
  }

  .advanced-summary {
    font-size: 0.75rem;
    color: var(--ui-text-muted);
    cursor: pointer;
    padding: 0.25rem 0;
  }

  .advanced-summary:hover {
    color: var(--ui-text);
  }
</style>
