<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import { filterTopLevelDocuments } from "$lib/documentVisibility";
  import { formatTimestamp } from "$lib/formatDate";
  import { workspacePath } from "$lib/workspacePaths";
  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import ArchiveButton from "$lib/components/ArchiveButton.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import TrashButton from "$lib/components/TrashButton.svelte";
  import CompactFilterBar from "$lib/components/CompactFilterBar.svelte";
  import { parseDelimitedValues } from "$lib/boardUtils";

  const DOC_STATE_LABELS = {
    active: "Active",
    archived: "Archived",
    trashed: "Trashed",
  };

  const defaultDocListFilters = {
    labels: "",
    showArchived: false,
  };

  let documents = $state([]);
  let loading = $state(false);
  let error = $state("");
  let filtersOpen = $state(false);
  let docFiltersDraft = $state({ ...defaultDocListFilters });
  let docFiltersApplied = $state({ ...defaultDocListFilters });
  let hasActiveFilters = $derived.by(() => {
    const f = docFiltersApplied;
    return f.showArchived || Boolean(f.labels.trim());
  });
  let archiveBusyId = $state("");
  let confirmModal = $state({ open: false, action: "", entityId: "" });
  let trashBusyId = $state("");
  let workspaceSlug = $derived($page.params.workspace);
  let scopedThreadId = $derived(
    String($page.url.searchParams.get("thread_id") ?? "").trim(),
  );
  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );

  let groupByLabel = $state(
    browser && localStorage.getItem("oar-docs-group-by-label") === "true",
  );
  let collapsedGroups = $state(new Set());

  let groupedDocs = $derived.by(() => {
    if (!groupByLabel) return null;
    /** @type {Record<string, typeof documents>} */
    const groups = {};
    for (const doc of documents) {
      const label = (doc.labels ?? [])[0] || "__ungrouped__";
      if (!groups[label]) groups[label] = [];
      groups[label].push(doc);
    }
    return Object.entries(groups).sort(([a], [b]) => {
      if (a === "__ungrouped__") return 1;
      if (b === "__ungrouped__") return -1;
      return a.localeCompare(b);
    });
  });

  function toggleGrouping() {
    groupByLabel = !groupByLabel;
    collapsedGroups = new Set();
    if (browser)
      localStorage.setItem("oar-docs-group-by-label", String(groupByLabel));
  }

  function toggleGroup(label) {
    const next = new Set(collapsedGroups);
    if (next.has(label)) next.delete(label);
    else next.add(label);
    collapsedGroups = next;
  }

  let createOpen = $state(false);
  let creating = $state(false);
  let createError = $state("");

  let draft = $state({
    id: "",
    title: "",
    labels: "",
    content: "",
  });

  function workspaceHref(pathname = "/") {
    return workspacePath(workspaceSlug, pathname);
  }

  $effect(() => {
    workspaceSlug;
    scopedThreadId;
    if (scopedThreadId && createOpen) {
      createOpen = false;
      createError = "";
      resetDraft();
    }
    if (workspaceSlug) {
      void loadDocuments();
    }
  });

  async function loadDocuments() {
    loading = true;
    error = "";
    try {
      const f = docFiltersApplied;
      const filters = {};
      const threadFromUrl = String(scopedThreadId ?? "").trim();
      if (threadFromUrl) filters.thread_id = threadFromUrl;
      if (f.showArchived) filters.include_archived = "true";
      const labels = parseDelimitedValues(f.labels);
      if (labels.length > 0) filters.label = labels;
      const data = await coreClient.listDocuments(filters);
      documents = filterTopLevelDocuments(data.documents);
    } catch (e) {
      error = `Failed to load documents: ${e instanceof Error ? e.message : String(e)}`;
      documents = [];
    } finally {
      loading = false;
    }
  }

  function resetDraft() {
    draft = {
      id: "",
      title: "",
      labels: "",
      content: "",
    };
  }

  function toggleCreate() {
    if (scopedThreadId) {
      return;
    }
    createOpen = !createOpen;
    if (!createOpen) {
      createError = "";
      resetDraft();
    }
  }

  async function handleCreate() {
    if (!draft.title.trim()) {
      createError = "Title is required.";
      return;
    }
    if (!draft.content.trim()) {
      createError = "Content is required.";
      return;
    }

    creating = true;
    createError = "";

    try {
      const labels = draft.labels
        .split(",")
        .map((l) => l.trim())
        .filter(Boolean);

      const docPayload = {
        title: draft.title.trim(),
        labels,
      };
      if (draft.id.trim()) docPayload.id = draft.id.trim();

      const result = await coreClient.createDocument({
        document: docPayload,
        content: draft.content.trim(),
        content_type: "text",
      });

      const newDocId = result.document?.id;
      createOpen = false;
      resetDraft();

      if (newDocId) {
        await goto(workspaceHref(`/docs/${newDocId}`));
      } else {
        await loadDocuments();
      }
    } catch (e) {
      createError = `Failed to create document: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      creating = false;
    }
  }

  function docStateColor(state) {
    if (state === "active") return "text-emerald-400 bg-emerald-500/10";
    if (state === "archived") return "text-amber-400 bg-amber-500/10";
    if (state === "trashed") return "text-slate-300 bg-slate-500/10";
    return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
  }

  function applyDocFilters() {
    docFiltersApplied = { ...docFiltersDraft };
    void loadDocuments();
  }

  function resetDocFilters() {
    docFiltersDraft = { ...defaultDocListFilters };
    docFiltersApplied = { ...defaultDocListFilters };
    filtersOpen = false;
    void loadDocuments();
  }

  function isDocArchived(doc) {
    const at = doc?.archived_at;
    return typeof at === "string" ? at.trim() !== "" : Boolean(at);
  }

  async function archiveDocument(docId) {
    const id = String(docId ?? "").trim();
    if (!id || archiveBusyId) return;
    archiveBusyId = id;
    error = "";
    try {
      await coreClient.archiveDocument(id, {});
      await loadDocuments();
    } catch (e) {
      error = `Archive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      archiveBusyId = "";
    }
  }

  async function unarchiveDocument(docId) {
    const id = String(docId ?? "").trim();
    if (!id || archiveBusyId) return;
    archiveBusyId = id;
    error = "";
    try {
      await coreClient.unarchiveDocument(id, {});
      await loadDocuments();
    } catch (e) {
      error = `Unarchive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      archiveBusyId = "";
    }
  }

  async function trashDocument(docId) {
    const id = String(docId ?? "").trim();
    if (!id || trashBusyId) return;
    trashBusyId = id;
    error = "";
    try {
      await coreClient.trashDocument(id, {});
      confirmModal = { open: false, action: "", entityId: "" };
      await loadDocuments();
    } catch (e) {
      error = `Trash failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      trashBusyId = "";
    }
  }

  function handleConfirm() {
    const id = confirmModal.entityId;
    const action = confirmModal.action;
    confirmModal = { open: false, action: "", entityId: "" };
    if (action === "archive") void archiveDocument(id);
    else if (action === "trash") void trashDocument(id);
  }
</script>

<div class="flex items-center justify-between mb-4">
  <div>
    <h1 class="text-lg font-semibold text-[var(--ui-text)]">Docs</h1>
    {#if scopedThreadId}
      <p class="mt-1 text-[12px] text-[var(--ui-text-muted)]">
        Scoped to backing thread
        <a
          class="text-indigo-400 transition-colors hover:text-indigo-300"
          href={workspaceHref(`/threads/${encodeURIComponent(scopedThreadId)}`)}
        >
          {scopedThreadId}
        </a>
      </p>
    {/if}
  </div>
  <div class="flex flex-wrap items-center justify-end gap-2 sm:gap-1.5">
    <button
      class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-[12px] font-medium transition-colors {hasActiveFilters
        ? 'border-[var(--ui-accent)]/40 bg-[var(--ui-accent)]/10 text-[var(--ui-accent)] hover:bg-[var(--ui-accent)]/15'
        : 'border-[var(--ui-border)] bg-[var(--ui-bg-soft)] text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)]'}"
      onclick={() => {
        if (!filtersOpen) {
          docFiltersDraft = { ...docFiltersApplied };
        }
        filtersOpen = !filtersOpen;
      }}
      type="button"
      data-testid="docs-filters-toggle"
    >
      <svg
        class="h-3.5 w-3.5"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M3 4a1 1 0 011-1h16a1 1 0 011 1v2.586a1 1 0 01-.293.707l-6.414 6.414a1 1 0 00-.293.707V17l-4 4v-6.586a1 1 0 00-.293-.707L3.293 7.293A1 1 0 013 6.586V4z"
        />
      </svg>
      {hasActiveFilters ? "Filtered" : "Filters"}
    </button>
    <button
      class="cursor-pointer inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-[12px] font-medium transition-colors {groupByLabel
        ? 'bg-[var(--ui-accent-strong)] text-white'
        : 'bg-[var(--ui-panel)] text-[var(--ui-text-muted)] hover:bg-[var(--ui-border)]'}"
      onclick={toggleGrouping}
      type="button"
      title="Group by label"
      aria-label="Group by label"
      aria-pressed={groupByLabel}
    >
      <svg
        class="h-3.5 w-3.5"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M3 7h4m0 0V3m0 4L3 3m18 4h-4m0 0V3m0 4l4-4M3 17h4m0 0v4m0-4L3 21m18-4h-4m0 0v4m0-4l4 4"
        />
      </svg>
    </button>
    <button
      class="cursor-pointer inline-flex items-center gap-1.5 rounded-md bg-[var(--ui-panel)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text)] transition-colors hover:bg-[var(--ui-border)] disabled:cursor-not-allowed disabled:opacity-50"
      disabled={Boolean(scopedThreadId)}
      onclick={toggleCreate}
      type="button"
      title={scopedThreadId
        ? "Clear the backing-thread scope to create a new document lineage."
        : "Create a new document lineage"}
    >
      {#if !createOpen}
        <svg
          class="h-3.5 w-3.5"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="2"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M12 4v16m8-8H4"
          />
        </svg>
      {/if}
      {createOpen ? "Cancel" : "New doc"}
    </button>
  </div>
</div>

{#if filtersOpen}
  <CompactFilterBar testId="docs-filter-panel">
    {#snippet children()}
      <div class="grid gap-3">
        <label class="text-[12px]">
          <span class="font-medium text-[var(--ui-text-muted)]"
            >Labels (comma-separated, any match)</span
          >
          <input
            bind:value={docFiltersDraft.labels}
            class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1.5 text-[13px] transition-colors focus:bg-[var(--ui-panel)]"
            placeholder="ops, runbook"
            type="text"
          />
        </label>
        <label
          class="inline-flex cursor-pointer items-center gap-1.5 text-[12px] text-[var(--ui-text-muted)]"
        >
          <input
            bind:checked={docFiltersDraft.showArchived}
            class="h-3.5 w-3.5 cursor-pointer rounded border-[var(--ui-border)] bg-[var(--ui-bg)] text-[var(--ui-accent-strong)] focus:ring-2 focus:ring-[var(--ui-accent)] focus:ring-offset-0"
            type="checkbox"
          />
          Show archived
        </label>
      </div>
      <div class="mt-3 flex flex-wrap gap-1.5">
        <button
          class="cursor-pointer rounded-md bg-[var(--ui-panel)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text)] hover:bg-[var(--ui-border)]"
          onclick={applyDocFilters}
          type="button"
        >
          Apply
        </button>
        <button
          class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)]"
          onclick={resetDocFilters}
          type="button"
        >
          Clear filters
        </button>
      </div>
    {/snippet}
  </CompactFilterBar>
{/if}

{#if scopedThreadId}
  <div
    class="mb-4 flex items-center justify-between rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2"
  >
    <p class="text-[12px] text-[var(--ui-text-muted)]">
      Showing only documents on this backing thread timeline.
    </p>
    <p class="text-[12px] text-[var(--ui-text-muted)]">
      Create from the unscoped docs view. New document lineages always get their
      own backing thread.
    </p>
    <a
      class="text-[12px] font-medium text-indigo-400 transition-colors hover:text-indigo-300"
      href={workspaceHref("/docs")}
    >
      Clear scope
    </a>
  </div>
{/if}

{#if createOpen}
  <form
    class="mb-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] p-4"
    onsubmit={(e) => {
      e.preventDefault();
      void handleCreate();
    }}
  >
    <h2 class="mb-3 text-[13px] font-semibold text-[var(--ui-text)]">
      New doc lineage
    </h2>
    <p class="mb-3 text-[12px] text-[var(--ui-text-muted)]">
      Create the lineage metadata and its first head revision together.
    </p>
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="sm:col-span-2">
        <span class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Title <span class="text-red-400">*</span></span
        >
        <input
          bind:value={draft.title}
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-3 py-1.5 text-[13px] text-[var(--ui-text)] placeholder:text-[var(--ui-text-subtle)]"
          placeholder="Document title"
          type="text"
        />
      </label>
      <label>
        <span class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >ID (optional)</span
        >
        <input
          bind:value={draft.id}
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-3 py-1.5 text-[13px] text-[var(--ui-text)] placeholder:text-[var(--ui-text-subtle)]"
          placeholder="auto-generated if empty"
          type="text"
        />
      </label>
      <label>
        <span class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Labels (comma-separated)</span
        >
        <input
          bind:value={draft.labels}
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-3 py-1.5 text-[13px] text-[var(--ui-text)] placeholder:text-[var(--ui-text-subtle)]"
          placeholder="e.g. ops, runbook"
          type="text"
        />
      </label>
      <label class="sm:col-span-2">
        <span class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Head content (Markdown) <span class="text-red-400">*</span></span
        >
        <textarea
          bind:value={draft.content}
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-3 py-2 text-[13px] text-[var(--ui-text)] placeholder:text-[var(--ui-text-subtle)] font-mono leading-relaxed resize-y"
          placeholder="# Document title&#10;&#10;Write your content here..."
          rows="10"
        ></textarea>
      </label>
    </div>

    {#if createError}
      <div
        class="mt-3 rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
        role="alert"
      >
        {createError}
      </div>
    {/if}
    <div class="mt-3 flex items-center gap-2">
      <button
        class="cursor-pointer rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
        disabled={creating}
        type="submit"
      >
        {creating ? "Creating…" : "Create doc"}
      </button>
      <button
        class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)]"
        onclick={toggleCreate}
        type="button"
      >
        Cancel
      </button>
    </div>
  </form>
{/if}

{#if loading}
  <div
    class="mt-12 flex items-center justify-center gap-2 text-[13px] text-[var(--ui-text-muted)]"
  >
    <svg class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
      <circle
        class="opacity-25"
        cx="12"
        cy="12"
        r="10"
        stroke="currentColor"
        stroke-width="4"
      ></circle>
      <path
        class="opacity-75"
        fill="currentColor"
        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
      ></path>
    </svg>
    Loading documents...
  </div>
{:else if error}
  <div class="mb-4 rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
    {error}
  </div>
{:else if documents.length === 0}
  <div class="mt-8 text-center">
    <p class="text-[13px] font-medium text-[var(--ui-text-muted)]">
      No docs yet
    </p>
    <p class="mt-1 text-[13px] text-[var(--ui-text-muted)]">
      No doc lineages yet. Create one to start a head revision and revision
      history.
    </p>
  </div>
{/if}

{#snippet docRow(doc, showBorderTop)}
  <div
    class="flex items-start justify-between gap-3 px-4 py-3 transition-colors hover:bg-[var(--ui-border-subtle)] {showBorderTop
      ? 'border-t border-[var(--ui-border)]'
      : ''}"
  >
    <a class="min-w-0 flex-1" href={workspaceHref(`/docs/${doc.id}`)}>
      <div class="flex flex-wrap items-center gap-2">
        {#if doc.state}
          <span
            class="inline-flex rounded px-1.5 py-0.5 text-[11px] font-semibold {docStateColor(
              doc.state,
            )}">{DOC_STATE_LABELS[doc.state] ?? doc.state}</span
          >
        {/if}
        {#each (doc.labels ?? []).slice(0, 3) as label}
          <span
            class="rounded bg-[var(--ui-border)] px-1.5 py-0.5 text-[10px] text-[var(--ui-text-muted)]"
            >{label}</span
          >
        {/each}
      </div>
      <p class="mt-1 truncate text-[13px] font-medium text-[var(--ui-text)]">
        {doc.title || doc.id}
      </p>
      <p class="text-[11px] text-[var(--ui-text-muted)]">
        Head v{doc.head_revision_number} · Updated {formatTimestamp(
          doc.updated_at,
        ) || "—"} by {actorName(doc.updated_by)}
      </p>
    </a>
    <div
      class="hidden shrink-0 items-center gap-1 sm:flex"
      role="presentation"
      onclick={(e) => e.stopPropagation()}
    >
      <span class="mr-1 shrink-0 text-[11px] text-[var(--ui-text-muted)]">
        {doc.head_revision_number} revision{doc.head_revision_number === 1
          ? ""
          : "s"}
      </span>
      <ArchiveButton
        archived={isDocArchived(doc)}
        busy={Boolean(archiveBusyId) || Boolean(trashBusyId)}
        onarchive={() =>
          (confirmModal = {
            open: true,
            action: "archive",
            entityId: doc.id,
          })}
        onunarchive={() => void unarchiveDocument(doc.id)}
      />
      <TrashButton
        busy={Boolean(trashBusyId) || Boolean(archiveBusyId)}
        ontrash={() =>
          (confirmModal = {
            open: true,
            action: "trash",
            entityId: doc.id,
          })}
      />
    </div>
  </div>
{/snippet}

{#if !loading && documents.length > 0}
  {#if groupByLabel && groupedDocs}
    <div class="space-y-2">
      {#each groupedDocs as [label, docs]}
        {@const collapsed = collapsedGroups.has(label)}
        {@const displayLabel =
          label === "__ungrouped__"
            ? "Ungrouped"
            : label.charAt(0).toUpperCase() + label.slice(1)}
        <div
          class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
        >
          <button
            class="cursor-pointer flex w-full items-center gap-2 px-4 py-2 text-left transition-colors hover:bg-[var(--ui-border-subtle)]"
            onclick={() => toggleGroup(label)}
            type="button"
          >
            <svg
              class="h-3 w-3 text-[var(--ui-text-muted)] transition-transform {collapsed
                ? ''
                : 'rotate-90'}"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M9 5l7 7-7 7"
              />
            </svg>
            <span class="text-[12px] font-medium text-[var(--ui-text)]"
              >{displayLabel}</span
            >
            <span
              class="rounded bg-[var(--ui-border)] px-1.5 py-0.5 text-[10px] text-[var(--ui-text-muted)]"
              >{docs.length}</span
            >
          </button>
          {#if !collapsed}
            {#each docs as doc, i}
              {@render docRow(doc, i > 0)}
            {/each}
          {/if}
        </div>
      {/each}
    </div>
  {:else}
    <div
      class="space-y-px rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
    >
      {#each documents as doc, i}
        {@render docRow(doc, i > 0)}
      {/each}
    </div>
  {/if}
{/if}

<ConfirmModal
  open={confirmModal.open}
  title={confirmModal.action === "trash" ? "Move to trash" : "Archive document"}
  message={confirmModal.action === "trash"
    ? "This document will be moved to trash. You can restore it later."
    : "This document will be hidden from default views. You can unarchive it later."}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={confirmModal.action === "trash"
    ? Boolean(trashBusyId)
    : Boolean(archiveBusyId)}
  onconfirm={handleConfirm}
  oncancel={() => (confirmModal = { open: false, action: "", entityId: "" })}
/>
