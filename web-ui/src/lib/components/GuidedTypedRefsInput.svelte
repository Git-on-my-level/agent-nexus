<script>
  import { page } from "$app/stores";
  import { resolveRefLink } from "$lib/refLinkModel";
  import { parseRef, renderRef } from "$lib/typedRefs";

  let {
    value = $bindable(""),
    suggestions = [],
    boardId = "",
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
  let resolvedRefs = $derived(
    refs.map((refValue) =>
      resolveRefLink(refValue, {
        workspaceSlug: $page.params.workspace,
        boardId,
      }),
    ),
  );

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
  <p class="mt-1 text-[11px] text-[var(--fg-muted)]">{helperText}</p>
{/if}

<div
  class="mt-1.5 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] p-2.5"
>
  {#if refs.length === 0}
    <p class="text-[11px] text-[var(--fg-muted)]">{emptyText}</p>
  {:else}
    <div class="flex flex-wrap gap-1.5">
      {#each resolvedRefs as resolved}
        <span
          class="inline-flex items-center gap-1 rounded-md border border-accent/20 bg-accent-soft px-2 py-0.5 text-[11px] text-accent-text"
        >
          {#if resolved.isLink}
            <a
              class="hover:text-accent-text"
              href={resolved.href}
              rel={resolved.isExternal ? "noreferrer noopener" : undefined}
              target={resolved.isExternal ? "_blank" : undefined}
            >
              {resolved.primaryLabel}
            </a>
          {:else}
            <span>{resolved.primaryLabel}</span>
          {/if}
          <button
            aria-label={`Remove ${resolved.raw}`}
            class="cursor-pointer rounded px-1 text-[11px] text-accent-text transition-colors hover:bg-accent-soft hover:text-accent-text"
            onclick={() => removeRef(resolved.raw)}
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
      class="min-w-[14rem] flex-1 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-[13px] text-[var(--fg)]"
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
      class="cursor-pointer rounded-md border border-[var(--line-strong)] bg-[var(--bg-soft)] px-3 py-2 text-[11px] font-medium text-[var(--fg)] transition-colors hover:bg-[var(--line-subtle)]"
      onclick={addCandidate}
      type="button"
    >
      {addButtonLabel}
    </button>
  </div>

  {#if localError}
    <p class="mt-1.5 text-[11px] text-danger-text">{localError}</p>
  {/if}

  {#if normalizedSuggestions.length > 0}
    <div class="mt-2.5">
      <p
        class="text-[11px] font-medium uppercase tracking-[0.06em] text-[var(--fg-muted)]"
      >
        Quick picks
      </p>
      <div class="mt-1.5 flex flex-wrap gap-1.5">
        {#each normalizedSuggestions as suggestion}
          <button
            class="cursor-pointer rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1 text-[11px] text-[var(--fg-muted)] transition-colors hover:border-accent/30 hover:text-accent-text"
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
    aria-controls="guided-refs-advanced"
    aria-expanded={showAdvanced}
    class="mt-2 cursor-pointer rounded-md px-2 py-1 text-[11px] text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] hover:text-[var(--fg)]"
    onclick={() => {
      showAdvanced = !showAdvanced;
    }}
    type="button"
  >
    {showAdvanced ? hideAdvancedToggleLabel : advancedToggleLabel}
  </button>

  <div id="guided-refs-advanced">
    {#if showAdvanced}
      <label
        class="mt-2 block text-[11px] font-medium text-[var(--fg-muted)]"
        >{advancedLabel}
        <textarea
          aria-label={textareaAriaLabel}
          bind:value
          class="mt-1.5 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-[13px] text-[var(--fg)]"
          rows={advancedRows}
        ></textarea></label
      >
      <p class="mt-1 text-[11px] text-[var(--fg-muted)]">{advancedHint}</p>
    {/if}
  </div>
</div>

{#if fieldError}
  <p class="mt-1.5 text-[11px] text-danger-text">{fieldError}</p>
{/if}
