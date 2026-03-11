<script>
  import { page } from "$app/stores";
  import { resolveRefLink } from "$lib/refLinkModel";
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
  let resolvedRefs = $derived(
    refs.map((refValue) =>
      resolveRefLink(refValue, {
        projectSlug: $page.params.project,
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
  <p class="mt-1 text-[11px] text-[var(--ui-text-muted)]">{helperText}</p>
{/if}

<div
  class="mt-1.5 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] p-2.5"
>
  {#if refs.length === 0}
    <p class="text-xs text-[var(--ui-text-muted)]">{emptyText}</p>
  {:else}
    <div class="flex flex-wrap gap-1.5">
      {#each resolvedRefs as resolved}
        <span
          class="inline-flex items-center gap-1 rounded-md border border-indigo-500/20 bg-indigo-500/10 px-2 py-0.5 text-xs text-indigo-400"
        >
          {#if resolved.isLink}
            <a
              class="hover:text-indigo-300"
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
            class="cursor-pointer rounded px-1 text-[11px] text-indigo-400 transition-colors hover:bg-indigo-500/20 hover:text-indigo-300"
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
      class="min-w-[14rem] flex-1 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
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
      class="cursor-pointer rounded-md border border-[var(--ui-border-strong)] bg-[var(--ui-bg-soft)] px-3 py-2 text-xs font-medium text-[var(--ui-text)] transition-colors hover:bg-[var(--ui-border-subtle)]"
      onclick={addCandidate}
      type="button"
    >
      {addButtonLabel}
    </button>
  </div>

  {#if localError}
    <p class="mt-1.5 text-xs text-red-400">{localError}</p>
  {/if}

  {#if normalizedSuggestions.length > 0}
    <div class="mt-2.5">
      <p
        class="text-[11px] font-medium uppercase tracking-[0.06em] text-[var(--ui-text-muted)]"
      >
        Quick picks
      </p>
      <div class="mt-1.5 flex flex-wrap gap-1.5">
        {#each normalizedSuggestions as suggestion}
          <button
            class="cursor-pointer rounded-full border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1 text-xs text-[var(--ui-text-muted)] transition-colors hover:border-indigo-500/30 hover:text-indigo-300"
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
    class="mt-2 cursor-pointer rounded-md px-2 py-1 text-xs text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] hover:text-[var(--ui-text)]"
    onclick={() => {
      showAdvanced = !showAdvanced;
    }}
    type="button"
  >
    {showAdvanced ? hideAdvancedToggleLabel : advancedToggleLabel}
  </button>

  {#if showAdvanced}
    <label class="mt-2 block text-xs font-medium text-[var(--ui-text-muted)]"
      >{advancedLabel}
      <textarea
        aria-label={textareaAriaLabel}
        bind:value
        class="mt-1.5 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
        rows={advancedRows}
      ></textarea></label
    >
    <p class="mt-1 text-[11px] text-[var(--ui-text-muted)]">{advancedHint}</p>
  {/if}
</div>

{#if fieldError}
  <p class="mt-1.5 text-xs text-red-400">{fieldError}</p>
{/if}
