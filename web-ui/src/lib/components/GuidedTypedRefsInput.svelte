<script>
  import { parseRef, renderRef } from "$lib/typedRefs";

  let {
    value = $bindable(""),
    suggestions = [],
    addInputLabel = "Add typed ref",
    addInputPlaceholder = "artifact:artifact-123",
    addButtonLabel = "Add ref",
    helperText = "",
    emptyText = "No refs added yet.",
    fieldError = "",
    textareaAriaLabel = "Typed refs (comma/newline separated)",
    advancedLabel = "Advanced raw input",
    advancedToggleLabel = "Use advanced raw input",
    hideAdvancedToggleLabel = "Hide advanced raw input",
    advancedHint = "Paste typed refs separated by commas or new lines.",
    advancedRows = 3,
  } = $props();

  let candidateRef = $state("");
  let localError = $state("");
  let showAdvanced = $state(false);

  function parseRefs(rawValue) {
    return String(rawValue ?? "")
      .split(/\r?\n|,/)
      .map((item) => item.trim())
      .filter(Boolean);
  }

  function normalizeRef(rawValue) {
    const trimmed = String(rawValue ?? "").trim();
    if (!trimmed) return "";
    const parsed = parseRef(trimmed);
    if (!parsed.prefix || !parsed.value) return "";
    return renderRef(parsed);
  }

  function buildSuggestions(rawSuggestions) {
    const seen = new Set();
    const normalized = [];

    rawSuggestions.forEach((item) => {
      const valueCandidate =
        typeof item === "string" ? item : String(item?.value ?? "");
      const value = normalizeRef(valueCandidate);
      if (!value || seen.has(value)) return;
      seen.add(value);
      normalized.push({
        value,
        label:
          typeof item === "string"
            ? value
            : String(item?.label ?? "").trim() || value,
      });
    });

    return normalized;
  }

  let refs = $derived(parseRefs(value));
  let normalizedSuggestions = $derived(buildSuggestions(suggestions));

  function writeRefs(items) {
    value = items.join("\n");
  }

  function addRef(rawValue) {
    const normalized = normalizeRef(rawValue);
    if (!normalized) {
      localError =
        "Use a typed ref like artifact:artifact-123 or event:event-42.";
      return false;
    }

    if (refs.includes(normalized)) {
      localError = "";
      return false;
    }

    writeRefs([...refs, normalized]);
    localError = "";
    return true;
  }

  function addCandidate() {
    if (addRef(candidateRef)) {
      candidateRef = "";
    }
  }

  function removeRef(refValue) {
    writeRefs(refs.filter((item) => item !== refValue));
    localError = "";
  }

  function addSuggestion(refValue) {
    void addRef(refValue);
  }
</script>

{#if helperText}
  <p class="mt-1 text-[11px] text-gray-500">{helperText}</p>
{/if}

<div class="mt-1.5 rounded-md border border-gray-200 bg-gray-50 p-2.5">
  {#if refs.length === 0}
    <p class="text-xs text-gray-500">{emptyText}</p>
  {:else}
    <div class="flex flex-wrap gap-1.5">
      {#each refs as refValue}
        <span
          class="inline-flex items-center gap-1 rounded-md border border-indigo-100 bg-indigo-50 px-2 py-0.5 text-xs text-indigo-700"
        >
          <span>{refValue}</span>
          <button
            aria-label={`Remove ${refValue}`}
            class="rounded px-1 text-[11px] text-indigo-500 transition-colors hover:bg-indigo-100 hover:text-indigo-700"
            onclick={() => removeRef(refValue)}
            type="button"
          >
            x
          </button>
        </span>
      {/each}
    </div>
  {/if}

  <div class="mt-2 flex flex-wrap gap-2">
    <input
      aria-label={addInputLabel}
      bind:value={candidateRef}
      class="min-w-[14rem] flex-1 rounded-md border border-gray-200 bg-white px-3 py-2 text-sm"
      onkeydown={(event) => {
        if (event.key === "Enter") {
          event.preventDefault();
          addCandidate();
        }
      }}
      placeholder={addInputPlaceholder}
      type="text"
    />
    <button
      class="rounded-md border border-gray-300 bg-white px-3 py-2 text-xs font-medium text-gray-700 transition-colors hover:bg-gray-100"
      onclick={addCandidate}
      type="button"
    >
      {addButtonLabel}
    </button>
  </div>

  {#if localError}
    <p class="mt-1.5 text-xs text-red-700">{localError}</p>
  {/if}

  {#if normalizedSuggestions.length > 0}
    <div class="mt-2.5">
      <p
        class="text-[11px] font-medium uppercase tracking-[0.06em] text-gray-400"
      >
        Quick picks
      </p>
      <div class="mt-1.5 flex flex-wrap gap-1.5">
        {#each normalizedSuggestions as suggestion}
          <button
            class="rounded-full border border-gray-200 bg-white px-2.5 py-1 text-xs text-gray-600 transition-colors hover:border-indigo-200 hover:text-indigo-700"
            onclick={() => addSuggestion(suggestion.value)}
            type="button"
          >
            {suggestion.label}
          </button>
        {/each}
      </div>
    </div>
  {/if}

  <button
    class="mt-2 rounded-md px-2 py-1 text-xs text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700"
    onclick={() => {
      showAdvanced = !showAdvanced;
    }}
    type="button"
  >
    {showAdvanced ? hideAdvancedToggleLabel : advancedToggleLabel}
  </button>

  {#if showAdvanced}
    <label class="mt-2 block text-xs font-medium text-gray-600"
      >{advancedLabel}
      <textarea
        aria-label={textareaAriaLabel}
        bind:value
        class="mt-1.5 w-full rounded-md border border-gray-200 bg-white px-3 py-2 text-sm"
        rows={advancedRows}
      ></textarea></label
    >
    <p class="mt-1 text-[11px] text-gray-500">{advancedHint}</p>
  {/if}
</div>

{#if fieldError}
  <p class="mt-1.5 text-xs text-red-700">{fieldError}</p>
{/if}
