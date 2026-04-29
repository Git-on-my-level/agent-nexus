<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import { filterTopLevelDocuments } from "$lib/documentVisibility";
  import { formatTimestamp } from "$lib/formatDate";
  import { workspacePath } from "$lib/workspacePaths";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import CompactFilterBar from "$lib/components/CompactFilterBar.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import WorkspaceResourceListRow from "$lib/components/WorkspaceResourceListRow.svelte";
  import WorkspaceListBulkToolbar from "$lib/components/WorkspaceListBulkToolbar.svelte";
  import LeadingSelectionGlyph from "$lib/components/LeadingSelectionGlyph.svelte";
  import LifecycleBadge from "$lib/components/LifecycleBadge.svelte";
  import { createWorkspaceListSelection } from "$lib/workspaceListSelection.svelte.js";

  const DOC_STATE_LABELS = {
    active: "Active",
    archived: "Archived",
    trashed: "Trashed",
  };

  const defaultDocListFilters = {
    states: ["active"],
  };

  let documents = $state([]);
  let loading = $state(false);
  let error = $state("");
  let retrying = $state(false);
  let filtersOpen = $state(false);
  let docFiltersDraft = $state({ ...defaultDocListFilters });
  let docFiltersApplied = $state({ ...defaultDocListFilters });
  let hasActiveFilters = $derived.by(() => {
    const st = docFiltersApplied.states ?? ["active"];
    return !(st.length === 1 && String(st[0]) === "active");
  });
  let archiveBusyId = $state("");
  /** @type {{ open: boolean, action: string, entityId: string, bulkIds: string[] | null }} */
  let confirmModal = $state({
    open: false,
    action: "",
    entityId: "",
    bulkIds: null,
  });
  let trashBusyId = $state("");
  let bulkBusy = $state(false);

  const docSel = createWorkspaceListSelection({ bulkBusy: () => bulkBusy });

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let scopedThreadId = $derived(
    String($page.url.searchParams.get("thread_id") ?? "").trim(),
  );
  let createOpen = $state(false);
  let creating = $state(false);
  let createError = $state("");

  let draft = $state({
    title: "",
    summary: "",
    content: "",
  });

  function workspaceHref(pathname = "/") {
    return workspacePath(organizationSlug, workspaceSlug, pathname);
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

  $effect(() => {
    documents;
    docSel.reconcileSelectionWithIds(
      documents.map((d) => String(d?.id ?? "").trim()).filter(Boolean),
    );
  });

  let allVisibleSelected = $derived(
    documents.length > 0 &&
      documents.every((d) => docSel.selectedIds.has(d.id)),
  );
  let selectedDocs = $derived(
    documents.filter((d) => docSel.selectedIds.has(d.id)),
  );

  let bulkCanArchive = $derived(
    selectedDocs.some((d) => !isDocArchived(d) && !isDocTrashed(d)),
  );
  let bulkCanUnarchive = $derived(
    selectedDocs.some((d) => isDocArchived(d) && !isDocTrashed(d)),
  );
  let bulkCanTrash = $derived(selectedDocs.some((d) => !isDocTrashed(d)));

  function selectAllVisibleDocs() {
    docSel.selectAllFromVisibleIds(documents.map((d) => d.id).filter(Boolean));
  }

  function clearDocSelection() {
    docSel.clearSelection();
  }

  function toggleSelectMode() {
    docSel.toggleSelectMode();
  }

  /** @param {number} i */
  function docIdAtVisibleIndex(i) {
    const d = documents[i];
    return String(d?.id ?? "").trim();
  }

  /** @param {number} i */
  function docHrefAtVisibleIndex(i) {
    return workspaceHref(`/docs/${docIdAtVisibleIndex(i)}`);
  }

  async function loadDocuments(isRetry = false) {
    loading = true;
    error = "";
    retrying = isRetry;
    try {
      const f = docFiltersApplied;
      const filters = {
        state: f.states ?? ["active"],
      };
      const threadFromUrl = String(scopedThreadId ?? "").trim();
      if (threadFromUrl) filters.thread_id = threadFromUrl;
      const data = await coreClient.listDocuments(filters);
      documents = filterTopLevelDocuments(data.documents);
    } catch (e) {
      error = `Failed to load documents: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      loading = false;
      retrying = false;
    }
  }

  function resetDraft() {
    draft = {
      title: "",
      summary: "",
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
      const docIn = {
        title: draft.title.trim(),
      };
      if (draft.summary.trim()) {
        docIn.summary = draft.summary.trim();
      }
      const result = await coreClient.createDocument({
        document: docIn,
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

  let docsHaveMixedLifecycle = $derived(
    documents.some((d) => {
      const s = String(d?.state ?? "")
        .trim()
        .toLowerCase();
      return s && s !== "active";
    }),
  );

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

  /** @param {string} value */
  function toggleDocLifecycleState(value) {
    const cur = [...(docFiltersDraft.states ?? ["active"])];
    const set = new Set(cur);
    if (set.has(value)) {
      if (set.size <= 1) return;
      set.delete(value);
    } else {
      set.add(value);
    }
    const order = /** @type {const} */ (["active", "archived", "trashed"]);
    docFiltersDraft = {
      ...docFiltersDraft,
      states: order.filter((s) => set.has(s)),
    };
  }

  function isDocArchived(doc) {
    const at = doc?.archived_at;
    return typeof at === "string" ? at.trim() !== "" : Boolean(at);
  }

  function isDocTrashed(doc) {
    if (doc?.state === "trashed") return true;
    const t = doc?.trashed_at;
    return typeof t === "string" ? t.trim() !== "" : Boolean(t);
  }

  function idsForBulkArchive() {
    return selectedDocs
      .filter((d) => !isDocArchived(d) && !isDocTrashed(d))
      .map((d) => d.id);
  }

  function idsForBulkUnarchive() {
    return selectedDocs
      .filter((d) => isDocArchived(d) && !isDocTrashed(d))
      .map((d) => d.id);
  }

  function idsForBulkTrash() {
    return selectedDocs.filter((d) => !isDocTrashed(d)).map((d) => d.id);
  }

  async function archiveDocument(docId) {
    const id = String(docId ?? "").trim();
    if (!id || archiveBusyId || bulkBusy) return;
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

  async function trashDocument(docId) {
    const id = String(docId ?? "").trim();
    if (!id || trashBusyId || bulkBusy) return;
    trashBusyId = id;
    error = "";
    try {
      await coreClient.trashDocument(id, {});
      confirmModal = { open: false, action: "", entityId: "", bulkIds: null };
      await loadDocuments();
    } catch (e) {
      error = `Trash failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      trashBusyId = "";
    }
  }

  async function bulkArchiveDocuments(ids) {
    const list = ids.filter(Boolean);
    if (!list.length || bulkBusy) return;
    bulkBusy = true;
    error = "";
    try {
      for (const id of list) {
        await coreClient.archiveDocument(id, {});
      }
      clearDocSelection();
      confirmModal = { open: false, action: "", entityId: "", bulkIds: null };
      await loadDocuments();
    } catch (e) {
      error = `Archive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      bulkBusy = false;
    }
  }

  async function bulkUnarchiveDocuments(ids) {
    const list = ids.filter(Boolean);
    if (!list.length || bulkBusy) return;
    bulkBusy = true;
    error = "";
    try {
      for (const id of list) {
        await coreClient.unarchiveDocument(id, {});
      }
      clearDocSelection();
      await loadDocuments();
    } catch (e) {
      error = `Unarchive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      bulkBusy = false;
    }
  }

  async function bulkTrashDocuments(ids) {
    const list = ids.filter(Boolean);
    if (!list.length || bulkBusy) return;
    bulkBusy = true;
    error = "";
    try {
      for (const id of list) {
        await coreClient.trashDocument(id, {});
      }
      clearDocSelection();
      confirmModal = { open: false, action: "", entityId: "", bulkIds: null };
      await loadDocuments();
    } catch (e) {
      error = `Trash failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      bulkBusy = false;
    }
  }

  function handleConfirm() {
    const bulkIds = confirmModal.bulkIds;
    const id = confirmModal.entityId;
    const action = confirmModal.action;
    confirmModal = { open: false, action: "", entityId: "", bulkIds: null };
    if (bulkIds && bulkIds.length > 0) {
      if (action === "archive") void bulkArchiveDocuments(bulkIds);
      else if (action === "trash") void bulkTrashDocuments(bulkIds);
      return;
    }
    if (action === "archive") void archiveDocument(id);
    else if (action === "trash") void trashDocument(id);
  }

  let confirmBulkCount = $derived(confirmModal.bulkIds?.length ?? 0);
  let confirmIsBulk = $derived(confirmBulkCount > 0);

  let confirmModalTitle = $derived.by(() => {
    if (confirmModal.action === "trash") {
      return confirmIsBulk
        ? `Move ${confirmBulkCount} documents to trash`
        : "Move to trash";
    }
    return confirmIsBulk
      ? `Archive ${confirmBulkCount} documents`
      : "Archive document";
  });

  let confirmModalMessage = $derived.by(() => {
    if (confirmModal.action === "trash") {
      return confirmIsBulk
        ? `These documents (${confirmBulkCount}) will be moved to trash. You can restore them later.`
        : "This document will be moved to trash. You can restore it later.";
    }
    return confirmIsBulk
      ? `These documents (${confirmBulkCount}) will be hidden from default views. You can unarchive them later.`
      : "This document will be hidden from default views. You can unarchive it later.";
  });

  let confirmModalBusy = $derived(
    confirmModal.action === "trash"
      ? Boolean(trashBusyId) || (confirmIsBulk && bulkBusy)
      : Boolean(archiveBusyId) || (confirmIsBulk && bulkBusy),
  );
</script>

<div class="mb-3 flex max-md:mb-2 flex-wrap items-center justify-between gap-2">
  <div>
    <h1 class="text-subtitle font-semibold text-[var(--fg)]">Docs</h1>
    {#if scopedThreadId}
      <p class="mt-1 hidden text-micro text-[var(--fg-muted)] sm:block">
        Scoped to backing thread
        <RefLink refValue={`thread:${scopedThreadId}`} humanize showRaw />
      </p>
    {/if}
  </div>
  <div class="flex flex-wrap items-center justify-end gap-1.5">
    <button
      class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-micro font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] {!documents.length &&
      !loading
        ? 'opacity-50 pointer-events-none'
        : ''}"
      onclick={toggleSelectMode}
      disabled={!documents.length && !loading}
      type="button"
      aria-pressed={docSel.selectMode}
    >
      {docSel.selectMode ? "Done" : "Select"}
    </button>
    <button
      class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-micro font-medium transition-colors {hasActiveFilters
        ? 'border-[var(--accent)]/40 bg-[var(--accent)]/10 text-[var(--accent)] hover:bg-[var(--accent)]/15'
        : 'border-[var(--line)] bg-[var(--bg-soft)] text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]'}"
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
      class="cursor-pointer inline-flex items-center gap-1.5 rounded-md bg-[var(--panel)] px-3 py-1.5 text-micro font-medium text-[var(--fg)] transition-colors hover:bg-[var(--line)] disabled:cursor-not-allowed disabled:opacity-50"
      disabled={Boolean(scopedThreadId)}
      onclick={toggleCreate}
      type="button"
      title={scopedThreadId
        ? "Clear the backing-thread scope to create a new document."
        : "Create a new document"}
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
        <div class="text-micro">
          <span class="font-medium text-[var(--fg-muted)]">Lifecycle</span>
          <fieldset
            class="mt-1 space-y-1 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-2"
          >
            {#each Object.entries(DOC_STATE_LABELS) as [value, label] (value)}
              <label
                class="flex cursor-pointer items-center gap-2 text-meta text-[var(--fg)]"
              >
                <input
                  checked={(docFiltersDraft.states ?? ["active"]).includes(
                    value,
                  )}
                  class="h-3.5 w-3.5 cursor-pointer rounded border-[var(--line)] bg-[var(--bg)] text-[var(--accent-hover)] focus:ring-2 focus:ring-[var(--accent)] focus:ring-offset-0"
                  type="checkbox"
                  onchange={() => toggleDocLifecycleState(value)}
                />
                {label}
              </label>
            {/each}
          </fieldset>
        </div>
      </div>
      <div class="mt-3 flex flex-wrap gap-1.5">
        <button
          class="cursor-pointer rounded-md bg-[var(--panel)] px-3 py-1.5 text-micro font-medium text-[var(--fg)] hover:bg-[var(--line)]"
          onclick={applyDocFilters}
          type="button"
        >
          Apply
        </button>
        <button
          class="cursor-pointer rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-1.5 text-micro font-medium text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]"
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
    class="mb-4 flex flex-wrap items-center justify-between gap-2 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2"
  >
    <p class="text-micro text-[var(--fg-muted)]">
      Showing only documents on this backing thread timeline.
    </p>
    <p class="hidden text-micro text-[var(--fg-muted)] sm:block">
      Create from the unscoped docs view. New documents always get their own
      backing thread.
    </p>
    <a
      class="text-micro font-medium text-accent-text transition-colors hover:text-accent-text"
      href={workspaceHref("/docs")}
    >
      Clear scope
    </a>
  </div>
{/if}

{#if createOpen}
  <form
    class="mb-4 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] p-4"
    onsubmit={(e) => {
      e.preventDefault();
      void handleCreate();
    }}
  >
    <h2 class="mb-3 text-meta font-semibold text-[var(--fg)]">New document</h2>
    <p class="mb-3 text-micro text-[var(--fg-muted)]">
      The title is saved together with the first revision. You can edit the body
      and add more revisions afterward.
    </p>
    <div class="grid gap-3">
      <label>
        <span class="text-micro font-medium text-[var(--fg-muted)]"
          >Title <span class="text-danger-text">*</span></span
        >
        <input
          bind:value={draft.title}
          class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg)] px-3 py-1.5 text-meta text-[var(--fg)] placeholder:text-[var(--fg-subtle)]"
          placeholder="Document title"
          type="text"
        />
      </label>
      <label>
        <span class="text-micro font-medium text-[var(--fg-muted)]"
          >Summary</span
        >
        <textarea
          bind:value={draft.summary}
          class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg)] px-3 py-2 text-meta text-[var(--fg)] placeholder:text-[var(--fg-subtle)] resize-y"
          placeholder="Optional short description for lists and the doc header"
          rows="2"
        ></textarea>
      </label>
      <label>
        <span class="text-micro font-medium text-[var(--fg-muted)]"
          >Head content (Markdown) <span class="text-danger-text">*</span></span
        >
        <textarea
          bind:value={draft.content}
          class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg)] px-3 py-2 text-meta text-[var(--fg)] placeholder:text-[var(--fg-subtle)] font-mono leading-relaxed resize-y"
          placeholder="# Document title&#10;&#10;Write your content here..."
          rows="10"
        ></textarea>
      </label>
    </div>

    {#if createError}
      <div
        class="mt-3 rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
        role="alert"
      >
        {createError}
      </div>
    {/if}
    <div class="mt-3 flex items-center gap-2">
      <button
        class="cursor-pointer rounded-md bg-accent-solid px-3 py-1.5 text-micro font-medium text-white hover:bg-accent disabled:opacity-50"
        disabled={creating}
        type="submit"
      >
        {creating ? "Creating…" : "Create doc"}
      </button>
      <button
        class="cursor-pointer rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-1.5 text-micro font-medium text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]"
        onclick={toggleCreate}
        type="button"
      >
        Cancel
      </button>
    </div>
  </form>
{/if}

{#if loading && documents.length === 0}
  <Skeleton rows={6} />
{:else if error}
  <StateError
    message={error}
    onretry={() => void loadDocuments(true)}
    {retrying}
    class="mb-4"
  />
{:else if documents.length === 0}
  <StateEmpty
    title="No docs yet"
    helper="Documents capture decisions, runbooks, and reference material. Each one keeps a full revision history."
  />
{/if}

{#snippet docRow(doc, index, showBorderTop)}
  {@const selected = docSel.selectedIds.has(doc.id)}
  {#if docSel.selectMode}
    <div
      aria-label={`${selected ? "Deselect" : "Select"} ${doc.title || doc.id}`}
      aria-pressed={selected}
      class="flex cursor-pointer items-stretch outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg-soft)] {showBorderTop
        ? 'border-t border-[var(--line)]'
        : ''} {selected
        ? 'border-l-[3px] border-l-[var(--accent)] bg-[var(--accent)]/10'
        : 'border-l-[3px] border-l-transparent hover:bg-[var(--line-subtle)]'}"
      onclick={(e) =>
        docSel.handleRowMouseEvent(
          e,
          index,
          documents.length,
          docIdAtVisibleIndex,
          docHrefAtVisibleIndex,
        )}
      onkeydown={(e) =>
        docSel.handleRowKeyboardEvent(e, index, docIdAtVisibleIndex)}
      role="button"
      tabindex="0"
    >
      <div class="flex shrink-0 items-center self-stretch pl-2 sm:pl-3">
        <LeadingSelectionGlyph {selected} />
      </div>
      <div
        class="pointer-events-none flex min-w-0 flex-1 items-start gap-3 px-3 py-2.5 sm:px-4"
      >
        <WorkspaceResourceListRow
          title={doc.title || doc.id}
          description={doc.summary ?? ""}
        >
          {#snippet badges()}
            <LifecycleBadge
              state={doc.state}
              label={DOC_STATE_LABELS[doc.state]}
              forceShow={docsHaveMixedLifecycle}
            />
          {/snippet}
        </WorkspaceResourceListRow>
        <div
          class="flex shrink-0 items-center gap-1.5 self-start pt-0.5 text-micro"
        >
          <span class="w-14 text-right text-[var(--fg-muted)]"
            >{formatTimestamp(doc.updated_at) || "—"}</span
          >
        </div>
      </div>
    </div>
  {:else}
    <div
      class="flex items-stretch {showBorderTop
        ? 'border-t border-[var(--line)]'
        : ''}"
    >
      <a
        class="flex min-w-0 flex-1 items-start gap-3 px-3 py-2.5 transition-colors hover:bg-[var(--line-subtle)] sm:px-4"
        href={workspaceHref(`/docs/${doc.id}`)}
      >
        <WorkspaceResourceListRow
          title={doc.title || doc.id}
          description={doc.summary ?? ""}
        >
          {#snippet badges()}
            <LifecycleBadge
              state={doc.state}
              label={DOC_STATE_LABELS[doc.state]}
              forceShow={docsHaveMixedLifecycle}
            />
          {/snippet}
        </WorkspaceResourceListRow>
        <div
          class="flex shrink-0 items-center gap-1.5 self-start pt-0.5 text-micro"
        >
          <span class="w-14 text-right text-[var(--fg-muted)]"
            >{formatTimestamp(doc.updated_at) || "—"}</span
          >
        </div>
      </a>
    </div>
  {/if}
{/snippet}

{#if !loading && documents.length > 0}
  {#if docSel.selectMode}
    <WorkspaceListBulkToolbar
      {allVisibleSelected}
      busy={bulkBusy}
      canArchive={bulkCanArchive}
      canTrash={bulkCanTrash}
      canUnarchive={bulkCanUnarchive}
      onArchive={() => {
        const ids = idsForBulkArchive();
        if (!ids.length) return;
        confirmModal = {
          open: true,
          action: "archive",
          entityId: "",
          bulkIds: ids,
        };
      }}
      onClear={clearDocSelection}
      onDeselectAll={clearDocSelection}
      onSelectAll={selectAllVisibleDocs}
      onTrash={() => {
        const ids = idsForBulkTrash();
        if (!ids.length) return;
        confirmModal = {
          open: true,
          action: "trash",
          entityId: "",
          bulkIds: ids,
        };
      }}
      onUnarchive={() => void bulkUnarchiveDocuments(idsForBulkUnarchive())}
      selectionChromeActive={true}
      selectedCount={docSel.selectedIds.size}
    />
  {/if}
  <div
    class="space-y-px rounded-md border border-[var(--line)] bg-[var(--bg-soft)] overflow-hidden"
  >
    {#each documents as doc, i}
      {@render docRow(doc, i, i > 0)}
    {/each}
  </div>
{/if}

<ConfirmModal
  open={confirmModal.open}
  title={confirmModalTitle}
  message={confirmModalMessage}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={confirmModalBusy}
  onconfirm={handleConfirm}
  oncancel={() =>
    (confirmModal = { open: false, action: "", entityId: "", bulkIds: null })}
/>
