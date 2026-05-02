<script>
  import { parseRef, renderRef } from "$lib/typedRefs";
  import Button from "$lib/components/Button.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { coreClient } from "$lib/coreClient";
  import { buildPrimitiveRefRoutes } from "$lib/refLinkModel";

  let {
    value = $bindable(""),
    suggestions = [],
    boardId = "",
    threadId = "",
    artifactRoutesById = {},
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
    /** When non-empty, show a file upload that creates `artifact:` refs scoped to these typed refs. */
    attachContextRefs = [],
    /** When true, do not render `artifact:` rows as chips (files are shown elsewhere, e.g. card Attachments). */
    suppressArtifactChipList = false,
    /** When true, hide the inline file picker (caller provides upload elsewhere). */
    hideAttachFileControl = false,
    /** Passed through for humanized compact labels (e.g. thread titles). */
    labelHints = /** @type {Record<string, string>} */ ({}),
    /** Subcopy when refs exist but none are rendered as chips (only hidden artifacts). */
    artifactOnlyRedirectHint = "File attachments are listed under Attachments.",
  } = $props();

  let uploadComposerArtifactsByRef = $state(
    /** @type {Record<string, Record<string, unknown>>} */ ({}),
  );

  let mergedArtifactRoutesById = $derived.by(() => {
    const rows = Object.values(uploadComposerArtifactsByRef).filter(
      (row) => row && typeof row === "object",
    );
    const fromUpload = rows.length
      ? buildPrimitiveRefRoutes({
          artifacts: rows,
          events: [],
          cards: [],
          documents: [],
          threadId: String(threadId ?? "").trim(),
        }).artifactRoutesById
      : {};
    return { ...(artifactRoutesById ?? {}), ...fromUpload };
  });
  let localError = $state("");
  let candidateRef = $state("");
  let showAdvanced = $state(false);
  let attachBusy = $state(false);
  let pendingAttachUpload = $state(null);
  let attachError = $state("");

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
  let refsForChipList = $derived.by(() => {
    if (!suppressArtifactChipList) return refs;
    return refs.filter(
      (r) =>
        !String(r ?? "")
          .trim()
          .toLowerCase()
          .startsWith("artifact:"),
    );
  });
  let chipListArtifactOnlyHidden = $derived(
    suppressArtifactChipList && refs.length > 0 && refsForChipList.length === 0,
  );
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
    if (String(refValue ?? "").startsWith("artifact:")) {
      const next = { ...uploadComposerArtifactsByRef };
      delete next[refValue];
      uploadComposerArtifactsByRef = next;
    }
  }

  function addSuggestion(refValue) {
    void addRef(refValue);
  }

  async function onAttachSelected(event) {
    const input = event.currentTarget;
    const file = input?.files?.[0];
    if (
      !file ||
      !Array.isArray(attachContextRefs) ||
      attachContextRefs.length === 0
    ) {
      return;
    }
    attachBusy = true;
    pendingAttachUpload = {
      original_filename: file.name || "attachment.bin",
      content_type: file.type || "application/octet-stream",
      size_bytes: file.size,
    };
    attachError = "";
    try {
      const payload = await coreClient.createArtifactAttachment({
        refs: attachContextRefs,
        file,
      });
      const id = payload?.artifact?.id;
      if (!id) {
        attachError = "Upload succeeded but artifact id missing.";
        return;
      }
      const ref = `artifact:${id}`;
      addRef(ref);
      const row =
        payload?.artifact && typeof payload.artifact === "object"
          ? payload.artifact
          : null;
      if (row) {
        uploadComposerArtifactsByRef = {
          ...uploadComposerArtifactsByRef,
          [ref]: /** @type {Record<string, unknown>} */ (row),
        };
      }
    } catch (e) {
      attachError = e instanceof Error ? e.message : String(e);
    } finally {
      attachBusy = false;
      pendingAttachUpload = null;
      input.value = "";
    }
  }
</script>

{#if helperText}
  <p class="mt-1 text-micro text-fg-muted">{helperText}</p>
{/if}

<div class="mt-1.5 rounded-md border border-line bg-bg-soft p-2.5">
  {#if refs.length === 0 && !pendingAttachUpload}
    <p class="text-micro text-fg-muted">{emptyText}</p>
  {:else}
    <div class="flex flex-wrap gap-1.5">
      {#if pendingAttachUpload}
        <RefLink
          refValue="artifact:upload-pending"
          {boardId}
          threadId={String(threadId ?? "").trim()}
          humanize
          attachmentOverlay={pendingAttachUpload}
          attachmentPending
          attachmentChipSize="tight"
        />
      {/if}
      {#if chipListArtifactOnlyHidden && !pendingAttachUpload}
        <p class="text-micro text-fg-muted">
          {artifactOnlyRedirectHint}
        </p>
      {/if}
      {#each refsForChipList as refValue (refValue)}
        <span
          class="inline-flex max-w-full min-h-[26px] items-stretch overflow-hidden rounded-md border border-line bg-bg text-micro"
        >
          <span class="flex min-w-0 flex-1 items-center">
            <RefLink
              variant="compact"
              {refValue}
              {boardId}
              composerRowEmbed={true}
              threadId={String(threadId ?? "").trim()}
              humanize
              showRaw={false}
              {labelHints}
              artifactRoutesById={mergedArtifactRoutesById}
              attachmentChipSize="tight"
            />
          </span>
          <button
            aria-label={`Remove ${refValue}`}
            class="flex shrink-0 cursor-pointer items-center justify-center border-l border-line px-2 text-fg-muted transition-colors hover:bg-bg-soft hover:text-fg focus-visible:z-10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-solid focus-visible:ring-inset"
            onclick={() => removeRef(refValue)}
            type="button"
          >
            ×
          </button>
        </span>
      {/each}
    </div>
  {/if}

  <div class="mt-2 flex flex-wrap items-center gap-2">
    <input
      aria-label={addInputLabel}
      bind:value={candidateRef}
      class="box-border h-9 min-w-[14rem] flex-1 rounded-md border border-line bg-bg-soft px-3 py-0 text-meta leading-snug text-fg"
      onkeydown={(event) => {
        if (event.key === "Enter") {
          event.preventDefault();
          addCandidate();
        }
      }}
      placeholder={addInputPlaceholder}
      type="text"
    />
    <Button
      variant="secondary"
      size="compact"
      class="!h-9 min-h-9 shrink-0"
      onclick={addCandidate}
    >
      {addButtonLabel}
    </Button>
    {#if !hideAttachFileControl && Array.isArray(attachContextRefs) && attachContextRefs.length > 0}
      <label
        class="inline-flex h-9 min-h-9 cursor-pointer items-center gap-2 rounded-md border border-line bg-bg px-3 text-micro text-fg hover:bg-bg-soft"
      >
        <span>{attachBusy ? "Uploading…" : "Attach file"}</span>
        <input
          class="sr-only"
          accept="image/*,text/plain,text/markdown,text/csv,.md,.txt,.csv,.json,.pdf"
          disabled={attachBusy}
          onchange={onAttachSelected}
          type="file"
        />
      </label>
    {/if}
  </div>

  {#if attachError}
    <p class="mt-1.5 text-micro text-danger-text">{attachError}</p>
  {/if}

  {#if localError}
    <p class="mt-1.5 text-micro text-danger-text">{localError}</p>
  {/if}

  {#if normalizedSuggestions.length > 0}
    <div class="mt-2.5">
      <p
        class="text-micro font-medium uppercase tracking-[0.06em] text-fg-muted"
      >
        Quick picks
      </p>
      <div class="mt-1.5 flex flex-wrap gap-1.5">
        {#each normalizedSuggestions as suggestion}
          <Button
            variant="secondary"
            size="compact"
            onclick={() => addSuggestion(suggestion.value)}
          >
            {suggestion.label}
          </Button>
        {/each}
      </div>
    </div>
  {/if}

  <Button
    variant="ghost"
    size="compact"
    aria-controls="guided-refs-advanced"
    aria-expanded={showAdvanced}
    onclick={() => {
      showAdvanced = !showAdvanced;
    }}
  >
    {showAdvanced ? hideAdvancedToggleLabel : advancedToggleLabel}
  </Button>

  <div id="guided-refs-advanced">
    {#if showAdvanced}
      <label class="mt-2 block text-micro font-medium text-fg-muted"
        >{advancedLabel}
        <textarea
          aria-label={textareaAriaLabel}
          bind:value
          class="mt-1.5 w-full rounded-md border border-line bg-bg-soft px-3 py-2 text-meta text-fg"
          rows={advancedRows}
        ></textarea></label
      >
      <p class="mt-1 text-micro text-fg-muted">{advancedHint}</p>
    {/if}
  </div>
</div>

{#if fieldError}
  <p class="mt-1.5 text-micro text-danger-text">{fieldError}</p>
{/if}
