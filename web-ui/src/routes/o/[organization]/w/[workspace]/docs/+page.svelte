<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import { filterTopLevelDocuments } from "$lib/documentVisibility";
  import { formatTimestamp } from "$lib/formatDate";
  import { formatShortcut } from "$lib/keyboardHints.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import CompactFilterBar from "$lib/components/CompactFilterBar.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import CopyButton from "$lib/components/CopyButton.svelte";
  import WorkspaceListRowShell from "$lib/components/WorkspaceListRowShell.svelte";
  import { copyText } from "$lib/clipboard.js";
  import WorkspaceResourceListRow from "$lib/components/WorkspaceResourceListRow.svelte";
  import WorkspaceListBulkToolbar from "$lib/components/WorkspaceListBulkToolbar.svelte";
  import LeadingSelectionGlyph from "$lib/components/LeadingSelectionGlyph.svelte";
  import LifecycleBadge from "$lib/components/LifecycleBadge.svelte";
  import InlineWorkspaceMetricStrip from "$lib/components/InlineWorkspaceMetricStrip.svelte";
  import { createWorkspaceResourceLifecycleController } from "$lib/workspaceResourceLifecycle.svelte.js";
  import { createWorkspaceListSelection } from "$lib/workspaceListSelection.svelte.js";
  import { documentListMetricItems } from "$lib/workspaceRowMetrics.js";
  import { absoluteUrl } from "$lib/absoluteUrl.js";
  import {
    resourceDisplayLabel,
    resourceRouteSegment,
  } from "$lib/resourceIdentity.js";

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
  let activeDocumentListLoadToken = 0;
  let hasActiveFilters = $derived.by(() => {
    const st = docFiltersApplied.states ?? ["active"];
    return !(st.length === 1 && String(st[0]) === "active");
  });
  let lifecycle = $state();

  const docSel = createWorkspaceListSelection({
    bulkBusy: () => lifecycle?.bulkBusy ?? false,
  });

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

  let workspaceHref = $derived(
    bindWorkspaceHref(organizationSlug, workspaceSlug),
  );

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

  lifecycle = createWorkspaceResourceLifecycleController({
    resourceSingular: "document",
    resourcePlural: "documents",
    selectedItems: () => selectedDocs,
    idFor: (doc) => doc?.id,
    isArchived: isDocArchived,
    isTrashed: isDocTrashed,
    actions: {
      archive: (id) => coreClient.archiveDocument(id, {}),
      unarchive: (id) => coreClient.unarchiveDocument(id, {}),
      trash: (id) => coreClient.trashDocument(id, {}),
    },
    reload: () => loadDocuments(),
    clearSelection: () => clearDocSelection(),
    setError: (message) => {
      error = message;
    },
  });

  let bulkCanArchive = $derived(lifecycle.canArchive());
  let bulkCanUnarchive = $derived(lifecycle.canUnarchive());
  let bulkCanTrash = $derived(lifecycle.canTrash());

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
    const segment = resourceRouteSegment(documents[i], "document");
    return workspaceHref(`/docs/${encodeURIComponent(segment)}`);
  }

  async function loadDocuments(isRetry = false) {
    const loadToken = ++activeDocumentListLoadToken;
    const loadWorkspaceSlug = workspaceSlug;
    const loadScopedThreadId = scopedThreadId;
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
      if (
        loadToken !== activeDocumentListLoadToken ||
        loadWorkspaceSlug !== workspaceSlug ||
        loadScopedThreadId !== scopedThreadId
      ) {
        return;
      }
      documents = filterTopLevelDocuments(data.documents);
    } catch (e) {
      if (
        loadToken !== activeDocumentListLoadToken ||
        loadWorkspaceSlug !== workspaceSlug ||
        loadScopedThreadId !== scopedThreadId
      ) {
        return;
      }
      error = `Failed to load documents: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      if (
        loadToken === activeDocumentListLoadToken &&
        loadWorkspaceSlug === workspaceSlug &&
        loadScopedThreadId === scopedThreadId
      ) {
        loading = false;
        retrying = false;
      }
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

      const newDocId = resourceRouteSegment(result.document, "document");
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

  let confirmModalTitle = $derived(lifecycle.confirmTitle());
  let confirmModalMessage = $derived(lifecycle.confirmMessage());
  let confirmModalBusy = $derived(lifecycle.confirmBusy());
</script>

<WorkspacePageShell>
  <WorkspacePageHeader title="Docs">
    {#snippet actions()}
      <button
        class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border border-line bg-bg-soft px-2.5 py-1.5 text-micro font-medium text-fg-muted transition-colors hover:bg-line-subtle {!documents.length &&
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
          ? 'border-accent bg-accent-soft text-accent hover:bg-accent-soft'
          : 'border-line bg-bg-soft text-fg-muted hover:bg-line-subtle'}"
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
        class="cursor-pointer inline-flex items-center gap-1.5 rounded-md bg-panel px-3 py-1.5 text-micro font-medium text-fg transition-colors hover:bg-line disabled:cursor-not-allowed disabled:opacity-50"
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
    {/snippet}
  </WorkspacePageHeader>

  {#if scopedThreadId}
    <p class="-mt-2 mb-1 hidden text-micro text-fg-muted sm:block">
      Scoped to backing thread
      <RefLink refValue={`thread:${scopedThreadId}`} humanize showRaw />
    </p>
  {/if}

  {#if filtersOpen}
    <CompactFilterBar testId="docs-filter-panel">
      {#snippet children()}
        <div class="grid gap-3">
          <div class="text-micro">
            <span class="font-medium text-fg-muted">Lifecycle</span>
            <fieldset
              class="mt-1 space-y-1 rounded-md border border-line bg-bg-soft px-2.5 py-2"
            >
              {#each Object.entries(DOC_STATE_LABELS) as [value, label] (value)}
                <label
                  class="flex cursor-pointer items-center gap-2 text-meta text-fg"
                >
                  <input
                    checked={(docFiltersDraft.states ?? ["active"]).includes(
                      value,
                    )}
                    class="h-3.5 w-3.5 cursor-pointer rounded border-line bg-bg text-accent-hover focus:ring-2 focus:ring-accent focus:ring-offset-0"
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
            class="cursor-pointer rounded-md bg-panel px-3 py-1.5 text-micro font-medium text-fg hover:bg-line"
            onclick={applyDocFilters}
            type="button"
          >
            Apply
          </button>
          <button
            class="cursor-pointer rounded-md border border-line bg-bg-soft px-3 py-1.5 text-micro font-medium text-fg-muted hover:bg-line-subtle"
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
      class="mb-4 flex flex-wrap items-center justify-between gap-2 rounded-md border border-line bg-bg-soft px-3 py-2"
    >
      <p class="text-micro text-fg-muted">
        Showing only documents on this backing thread timeline.
      </p>
      <p class="hidden text-micro text-fg-muted sm:block">
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
      class="mb-4 rounded-md border border-line bg-bg-soft p-4"
      data-anx-save-scope
      onsubmit={(e) => {
        e.preventDefault();
        void handleCreate();
      }}
    >
      <h2 class="mb-3 text-meta font-semibold text-fg">New document</h2>
      <p class="mb-3 text-micro text-fg-muted">
        The title is saved together with the first revision. You can edit the
        body and add more revisions afterward.
      </p>
      <div class="grid gap-3">
        <label>
          <span class="text-micro font-medium text-fg-muted"
            >Title <span class="text-danger-text">*</span></span
          >
          <input
            bind:value={draft.title}
            class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 text-meta text-fg placeholder:text-fg-subtle"
            placeholder="Document title"
            type="text"
          />
        </label>
        <label>
          <span class="text-micro font-medium text-fg-muted">Summary</span>
          <textarea
            bind:value={draft.summary}
            class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-2 text-meta text-fg placeholder:text-fg-subtle resize-y"
            placeholder="Optional short description for lists and the doc header"
            rows="2"
          ></textarea>
        </label>
        <label>
          <span class="text-micro font-medium text-fg-muted"
            >Head content (Markdown) <span class="text-danger-text">*</span
            ></span
          >
          <textarea
            bind:value={draft.content}
            class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-2 text-meta text-fg placeholder:text-fg-subtle font-mono leading-relaxed resize-y"
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
          data-anx-save-shortcut
          aria-keyshortcuts="Meta+S Control+S"
          title={`Create doc (${formatShortcut("S")})`}
        >
          {creating ? "Creating…" : "Create doc"}
        </button>
        <button
          class="cursor-pointer rounded-md border border-line bg-bg-soft px-3 py-1.5 text-micro font-medium text-fg-muted hover:bg-line-subtle"
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
        aria-label={`${selected ? "Deselect" : "Select"} ${resourceDisplayLabel(doc)}`}
        aria-pressed={selected}
        class="flex cursor-pointer items-stretch outline-none transition-colors focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-bg-soft {showBorderTop
          ? 'border-t border-line'
          : ''} {selected
          ? 'border-l-[3px] border-l-accent bg-accent-soft'
          : 'border-l-[3px] border-l-transparent hover:bg-line-subtle'}"
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
          <div class="min-w-0 flex-1">
            <div class="flex min-w-0 items-start justify-between gap-3">
              <WorkspaceResourceListRow
                title={resourceDisplayLabel(doc)}
                description={doc.summary ?? ""}
              >
                {#snippet badges()}
                  <LifecycleBadge
                    state={doc.state}
                    label={DOC_STATE_LABELS[doc.state]}
                    forceShow={docsHaveMixedLifecycle}
                  />
                  {#if doc.head_revision_number != null}
                    <span
                      class="font-mono text-micro tabular-nums text-fg-subtle"
                      title="Head revision"
                    >
                      v{doc.head_revision_number}
                    </span>
                  {/if}
                {/snippet}
              </WorkspaceResourceListRow>
              <div
                class="flex shrink-0 items-center gap-1.5 self-start pt-0.5 text-micro"
              >
                <span class="w-14 text-right text-fg-muted"
                  >{formatTimestamp(doc.updated_at) || "—"}</span
                >
              </div>
            </div>
            <InlineWorkspaceMetricStrip items={documentListMetricItems(doc)} />
          </div>
        </div>
      </div>
    {:else}
      {@const docLink = absoluteUrl(
        workspaceHref(
          `/docs/${encodeURIComponent(resourceRouteSegment(doc, "document"))}`,
        ),
      )}
      <WorkspaceListRowShell
        class={showBorderTop ? "border-t border-line" : ""}
        contextMenuItems={[
          {
            key: "copy-link",
            label: "Copy link",
            onSelect: () => void copyText(docLink),
          },
        ]}
      >
        {#snippet row()}
          <a
            class="flex min-w-0 flex-1 items-start gap-3 px-3 py-2.5 transition-colors hover:bg-panel-hover sm:px-4"
            href={workspaceHref(
              `/docs/${encodeURIComponent(resourceRouteSegment(doc, "document"))}`,
            )}
          >
            <div class="min-w-0 flex-1">
              <WorkspaceResourceListRow
                title={resourceDisplayLabel(doc)}
                description={doc.summary ?? ""}
                titleClass="group-hover/row:text-accent-text transition-colors"
              >
                {#snippet badges()}
                  <LifecycleBadge
                    state={doc.state}
                    label={DOC_STATE_LABELS[doc.state]}
                    forceShow={docsHaveMixedLifecycle}
                  />
                  {#if doc.head_revision_number != null}
                    <span
                      class="font-mono text-micro tabular-nums text-fg-subtle"
                      title="Head revision"
                    >
                      v{doc.head_revision_number}
                    </span>
                  {/if}
                {/snippet}
              </WorkspaceResourceListRow>
              <InlineWorkspaceMetricStrip
                items={documentListMetricItems(doc)}
              />
            </div>
          </a>
        {/snippet}
        {#snippet meta()}
          <span class="w-14 text-right text-fg-muted"
            >{formatTimestamp(doc.updated_at) || "—"}</span
          >
        {/snippet}
        {#snippet actions()}
          <CopyButton
            value={docLink}
            iconOnly
            icon="link"
            label="Copy document link"
            size="sm"
          />
        {/snippet}
      </WorkspaceListRowShell>
    {/if}
  {/snippet}

  {#if !loading && documents.length > 0}
    {#if docSel.selectMode}
      <WorkspaceListBulkToolbar
        {allVisibleSelected}
        busy={lifecycle.bulkBusy}
        canArchive={bulkCanArchive}
        canTrash={bulkCanTrash}
        canUnarchive={bulkCanUnarchive}
        onArchive={() => {
          const ids = lifecycle.idsForArchive();
          if (!ids.length) return;
          lifecycle.openBulkConfirm("archive", ids);
        }}
        onClear={clearDocSelection}
        onDeselectAll={clearDocSelection}
        onSelectAll={selectAllVisibleDocs}
        onTrash={() => {
          const ids = lifecycle.idsForTrash();
          if (!ids.length) return;
          lifecycle.openBulkConfirm("trash", ids);
        }}
        onUnarchive={() =>
          void lifecycle.runBulk("unarchive", lifecycle.idsForUnarchive())}
        selectionChromeActive={true}
        selectedCount={docSel.selectedIds.size}
      />
    {/if}
    <div
      class="space-y-px rounded-md border border-line bg-bg-soft overflow-hidden"
    >
      {#each documents as doc, i}
        {@render docRow(doc, i, i > 0)}
      {/each}
    </div>
  {/if}

  <ConfirmModal
    open={lifecycle.confirmModal.open}
    title={confirmModalTitle}
    message={confirmModalMessage}
    confirmLabel={lifecycle.confirmModal.action === "trash"
      ? "Trash"
      : "Archive"}
    variant={lifecycle.confirmModal.action === "trash" ? "danger" : "warning"}
    busy={confirmModalBusy}
    onconfirm={() => lifecycle.handleConfirm()}
    oncancel={() => lifecycle.closeConfirm()}
  />
</WorkspacePageShell>
