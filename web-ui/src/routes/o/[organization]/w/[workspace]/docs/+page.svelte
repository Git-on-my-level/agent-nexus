<script>
  import { get } from "svelte/store";
  import { durableForm } from "$lib/actions/durableForm.js";
  import { enqueueAuthoring, mutationOutbox } from "$lib/mutationOutbox.js";
  import { localWorkScope } from "$lib/localWorkScope.js";
  import DocumentLifecycleWork from "$lib/components/DocumentLifecycleWork.svelte";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import { filterTopLevelDocuments } from "$lib/documentVisibility";
  import {
    actorDirectoryLoaded,
    actorRegistry,
    agentDirectoryLoaded,
    agentRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import { buildOwnerDisplay } from "$lib/ownerDisplay.js";
  import Time from "$lib/time/Time.svelte";
  import { formatShortcut } from "$lib/keyboardHints.js";
  import { navIconPath } from "$lib/icons.js";
  import { openCommandPalette } from "$lib/stores/commandPalette.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { TOPIC_STATUSES } from "$lib/topicFilters";
  import { BOARD_LIFECYCLE_STATE_LABELS } from "$lib/boardUtils";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import LifecycleStateFilter from "$lib/components/LifecycleStateFilter.svelte";
  import WorkspaceListFilters from "$lib/components/WorkspaceListFilters.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import SkeletonList from "$lib/components/state/SkeletonList.svelte";
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
  import { createWorkspaceResourceLifecycleController } from "$lib/workspaceResourceLifecycle.svelte.js";
  import { createWorkspaceListSelection } from "$lib/workspaceListSelection.svelte.js";
  import {
    DOC_LIST_EVENT_TYPES,
    liveWorkspaceEvents,
  } from "$lib/liveWorkspaceEvents.js";
  import { onMount } from "svelte";

  /**
   * Agent-written comments are frequently "Update on <document title>". A row
   * that already shows the title gains nothing from a second line repeating
   * it, so a comment that is the title plus filler is not shown.
   */
  const COMMENT_FILLER = new Set([
    "a",
    "about",
    "an",
    "change",
    "changed",
    "changes",
    "comment",
    "for",
    "new",
    "note",
    "on",
    "re",
    "the",
    "to",
    "update",
    "updated",
    "updates",
  ]);
  function commentRestatesTitle(body, title) {
    const normalizedTitle = String(title ?? "")
      .trim()
      .toLowerCase();
    if (!normalizedTitle) return false;
    const normalizedBody = String(body ?? "")
      .trim()
      .toLowerCase();
    if (!normalizedBody.includes(normalizedTitle)) return false;
    const remainder = normalizedBody
      .replace(normalizedTitle, " ")
      .replace(/[^a-z0-9]+/g, " ")
      .trim();
    if (!remainder) return true;
    return remainder.split(/\s+/).every((word) => COMMENT_FILLER.has(word));
  }
  import { absoluteUrl } from "$lib/absoluteUrl.js";
  import {
    resourceDisplayLabel,
    resourceRouteSegment,
  } from "$lib/resourceIdentity.js";
  import { usePageTitle } from "$lib/pageTitle.svelte.js";

  let documents = $state([]);
  // Search pages followed beyond the first before the list says "more".
  const SEARCH_PAGE_CEILING = 5;
  let searchTruncated = $state(false);
  let loading = $state(false);
  let error = $state("");
  let retrying = $state(false);
  let activeDocumentListLoadToken = 0;
  let lifecycle = $state();
  let lifecycleWork = $state();

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
  const createTarget = "document:new";
  let savedCreate = $derived(
    $mutationOutbox.find((entry) => entry.target === createTarget),
  );
  function restoreCreate(value) {
    draft = {
      title: value?.title ?? "",
      summary: value?.summary ?? "",
      content: value?.content ?? "",
    };
  }
  onMount(() => {
    const confirmed = async (event) => {
      const entry = event.detail;
      if (entry.scope !== get(localWorkScope) || entry.target !== createTarget)
        return;
      const id = resourceRouteSegment(entry.result?.document, "document");
      if (
        !Object.values(draft).some(Boolean) ||
        JSON.stringify({ ...draft }) === JSON.stringify(entry.draft)
      ) {
        createOpen = false;
        if (id) await goto(workspaceHref(`/docs/${id}`));
        else await loadDocuments();
      }
    };
    window.addEventListener("anx:mutation-confirmed", confirmed);
    return () =>
      window.removeEventListener("anx:mutation-confirmed", confirmed);
  });
  let createError = $state("");
  // The search lives in the URL (?q=) so a result page can be shared and
  // the back button restores it; the input holds the draft until submit.
  let searchQueryTrimmed = $derived(
    String($page.url.searchParams.get("q") ?? "").trim(),
  );
  let searchDraft = $state("");
  $effect(() => {
    searchDraft = searchQueryTrimmed;
  });
  /*
   * The lifecycle filter lives in the URL too (repeated `state=`, the same
   * shape `/threads` uses), so a list showing archived docs is shareable and
   * the back button restores it. It used to be page-local draft state that
   * only a second click on Apply committed.
   */
  let docStates = $derived.by(() => {
    const requested = new Set(
      $page.url.searchParams
        .getAll("state")
        .map((value) => String(value ?? "").trim())
        .filter((value) => TOPIC_STATUSES.includes(value)),
    );
    const ordered = TOPIC_STATUSES.filter((value) => requested.has(value));
    return ordered.length ? ordered : ["active"];
  });
  let hasActiveFilters = $derived(
    !(docStates.length === 1 && docStates[0] === "active"),
  );
  let filterCount = $derived(hasActiveFilters ? 1 : 0);

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
    searchQueryTrimmed;
    docStates;
    if (scopedThreadId && createOpen) {
      createOpen = false;
      createError = "";
      restoreCreate(null);
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
    actions: {},
    queue: (action, ids) =>
      lifecycleWork.queue(
        ids.map((id) => documents.find((doc) => doc.id === id)),
        action,
      ),
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
  /**
   * Who last changed a doc, from `updated_by` on the row we already have and
   * the actor directory the shell already loaded: no request per row.
   */
  let ownerCellFor = $derived(
    buildOwnerDisplay({
      actors: $actorRegistry,
      principals: $principalRegistry,
      agents: $agentRegistry,
      actorsLoaded: $actorDirectoryLoaded,
      agentsLoaded: $agentDirectoryLoaded,
    }),
  );
  function docAuthorCell(doc) {
    return ownerCellFor(doc?.updated_by || doc?.created_by);
  }

  /** Truncate to a quiet single-line preview (adds an ellipsis when cut). */
  function previewText(value, max = 80) {
    const text = String(value ?? "").trim();
    return text.length <= max ? text : `${text.slice(0, max).trimEnd()}…`;
  }

  function isHttpUrl(value) {
    return /^https?:\/\//i.test(String(value ?? "").trim());
  }

  async function loadDocuments(isRetry = false, { live = false } = {}) {
    const loadToken = ++activeDocumentListLoadToken;
    const loadWorkspaceSlug = workspaceSlug;
    const loadScopedThreadId = scopedThreadId;
    loading = true;
    if (!live) error = "";
    retrying = isRetry;
    try {
      const q = searchQueryTrimmed;
      let data;
      if (q) {
        // /docs/search takes q/knowledge/tag/cursor/limit only (no state or
        // thread_id per contracts/anx-openapi.yaml), so the lifecycle filter
        // and thread scope are dropped while searching.
        // Search pages at 50; follow the cursor so a common word does not
        // quietly show only its first page.
        data = await coreClient.searchDocuments({ q });
        let cursor = data?.next_cursor || "";
        for (let pages = 0; cursor && pages < SEARCH_PAGE_CEILING; pages += 1) {
          const more = await coreClient.searchDocuments({ q, cursor });
          data = {
            ...data,
            documents: [...(data.documents || []), ...(more?.documents || [])],
            next_cursor: more?.next_cursor || "",
          };
          cursor = data.next_cursor;
        }
        searchTruncated = Boolean(cursor);
      } else {
        const filters = {
          state: docStates,
        };
        const threadFromUrl = String(scopedThreadId ?? "").trim();
        if (threadFromUrl) filters.thread_id = threadFromUrl;
        data = await coreClient.listDocuments(filters);
        searchTruncated = false;
      }
      if (
        loadToken !== activeDocumentListLoadToken ||
        loadWorkspaceSlug !== workspaceSlug ||
        loadScopedThreadId !== scopedThreadId
      ) {
        return;
      }
      documents = filterTopLevelDocuments(data.documents);
      error = "";
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

  function toggleCreate() {
    if (scopedThreadId) {
      return;
    }
    createOpen = !createOpen;
    if (!createOpen) {
      createError = "";
    }
  }

  async function handleCreate() {
    if (creating || savedCreate) return;
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
      await enqueueAuthoring({
        operation: "docs.create",
        body: {
          document: docIn,
          content: draft.content.trim(),
          content_type: "text",
        },
        target: createTarget,
        draft: { ...draft },
        href: $page.url.pathname,
        title: "Saved document",
      });
    } catch {
      createError =
        "Your document is still here. Review the saved work above, or copy it before closing this tab.";
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

  /** Apply the lifecycle filter by navigating; the list re-reads from the URL. */
  function applyDocStates(nextStates) {
    const params = new URLSearchParams($page.url.search);
    params.delete("state");
    const isDefault = nextStates.length === 1 && nextStates[0] === "active";
    if (!isDefault) {
      for (const value of nextStates) params.append("state", value);
    }
    const search = params.toString();
    void goto(workspaceHref(`/docs${search ? `?${search}` : ""}`), {
      replaceState: true,
      noScroll: true,
      keepFocus: true,
    });
  }

  function resetDocFilters() {
    applyDocStates(["active"]);
  }

  /** @param {string} value */
  function toggleDocLifecycleState(value) {
    const set = new Set(docStates);
    if (set.has(value)) {
      if (set.size <= 1) return;
      set.delete(value);
    } else {
      set.add(value);
    }
    applyDocStates(TOPIC_STATUSES.filter((s) => set.has(s)));
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

  // The list follows the event stream: a new doc, a revision or a comment
  // re-reads it. Comments on other threads (tasks, inbox) are ignored.
  onMount(() =>
    liveWorkspaceEvents({
      client: coreClient,
      types: DOC_LIST_EVENT_TYPES,
      filter: (event) =>
        event.type !== "message_posted" ||
        (Array.isArray(event.refs) &&
          event.refs.some((ref) => String(ref).startsWith("document:"))) ||
        documents.some(
          (doc) => doc?.thread_id && doc.thread_id === event.thread_id,
        ),
      onChange: () => {
        // Selecting rows for a bulk action pins the list until it is done.
        if (docSel.selectMode || createOpen) return;
        void loadDocuments(false, { live: true });
      },
    }),
  );

  let confirmModalTitle = $derived(lifecycle.confirmTitle());
  let confirmModalMessage = $derived(lifecycle.confirmMessage());
  let confirmModalBusy = $derived(lifecycle.confirmBusy());

  usePageTitle(() => "Docs · Agent Nexus");
</script>

<WorkspacePageShell data-tour="docs">
  <DocumentLifecycleWork
    bind:this={lifecycleWork}
    target="documents:lifecycle"
    href={$page.url.pathname + $page.url.search}
    onconfirmed={() => void loadDocuments()}
  />

  <WorkspacePageHeader title="Docs">
    {#snippet actions()}
      <!-- Every list header carries workspace search, because ⌘K is not
           reachable on a phone: the sidebar that holds it is hidden below
           1024px, and without this Docs was the one list with no way out of
           itself. -->
      <button
        class="ui-icon-btn"
        onclick={openCommandPalette}
        aria-label="Search workspace"
        aria-keyshortcuts="Meta+K"
        title="Search workspace (⌘K)"
        type="button"
      >
        <svg
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="1.5"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d={navIconPath("search")} />
        </svg>
      </button>
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

  <form
    class="flex max-w-md gap-2"
    onsubmit={(event) => {
      event.preventDefault();
      const q = String(searchDraft ?? "").trim();
      const params = new URLSearchParams($page.url.search);
      if (q) params.set("q", q);
      else params.delete("q");
      const search = params.toString();
      void goto(workspaceHref(`/docs${search ? `?${search}` : ""}`), {
        replaceState: true,
        keepFocus: true,
        noScroll: true,
      });
    }}
  >
    <label class="sr-only" for="docs-search">Search documents</label>
    <input
      id="docs-search"
      class="ui-input min-w-0 flex-1"
      type="search"
      bind:value={searchDraft}
      placeholder="Search documents…"
    />
    <button class="ui-btn-secondary" type="submit">Search</button>
  </form>

  {#if scopedThreadId}
    <!-- Raw refs are unbroken; without this they are clipped by the shell. -->
    <p
      class="-mt-2 mb-1 hidden text-micro text-fg-muted [overflow-wrap:anywhere] sm:block"
    >
      Scoped to backing thread
      <RefLink refValue={`thread:${scopedThreadId}`} humanize showRaw />
    </p>
  {/if}

  <WorkspaceListFilters
    activeCount={filterCount}
    toggleTestId="docs-filters-toggle"
    panelTestId="docs-filter-panel"
    openWhenFiltered={hasActiveFilters}
    onclear={resetDocFilters}
  >
    {#snippet children()}
      <LifecycleStateFilter
        states={docStates}
        ontoggle={toggleDocLifecycleState}
      />
    {/snippet}
  </WorkspaceListFilters>

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
    <MutationSendStatus target={createTarget} />
    <form
      use:durableForm={{
        target: createTarget,
        value: { ...draft },
        read: () => ({ ...draft }),
        onRestore: restoreCreate,
      }}
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
            placeholder="# Document title&#10;&#10;Write your content here…"
            rows="10"
          ></textarea>
        </label>
      </div>

      {#if createError}
        <div
          class="mt-3 rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text [overflow-wrap:anywhere]"
          role="alert"
        >
          {createError}
        </div>
      {/if}
      <div class="mt-3 flex items-center gap-2">
        <button
          class="cursor-pointer rounded-md bg-accent-solid px-3 py-1.5 text-micro font-medium text-white hover:bg-accent disabled:opacity-50"
          disabled={creating || Boolean(savedCreate)}
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
    <SkeletonList rows={6} lines={1} rowClass="h-[34px]" />
  {:else if error}
    <StateError
      message={error}
      onretry={() => void loadDocuments(true)}
      {retrying}
      class="mb-4"
    />
  {:else if documents.length === 0}
    {#if searchQueryTrimmed}
      <StateEmpty
        title="No matching docs"
        helper={`No documents match "${searchQueryTrimmed}". Search covers titles, bodies, and comments.`}
      />
    {:else}
      <StateEmpty
        title="No docs yet"
        helper="Documents capture decisions, runbooks, and reference material. Each one keeps a full version history."
      />
    {/if}
  {/if}

  {#snippet docEnrichment(doc, sourceAsLink)}
    {@const source = String(doc?.source ?? "").trim()}
    {@const tags = (Array.isArray(doc?.tags) ? doc.tags : [])
      .map((tag) => String(tag ?? "").trim())
      .filter(Boolean)}
    {#if source || tags.length > 0}
      <div class="mt-1 flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1">
        {#if source}
          {#if sourceAsLink && isHttpUrl(source)}
            <a
              class="inline-flex min-w-0 max-w-[20rem] items-center gap-1 font-mono text-micro text-fg-muted transition-colors hover:text-accent-text"
              href={source}
              target="_blank"
              rel="noreferrer"
              title={source}
            >
              <span aria-hidden="true">↗</span>
              <span class="truncate">{source}</span>
            </a>
          {:else}
            <span
              class="min-w-0 max-w-[20rem] truncate font-mono text-micro text-fg-subtle"
              title={source}
            >
              {source}
            </span>
          {/if}
        {/if}
        {#each tags as tag (tag)}
          {#if tag === "knowledge"}
            <span
              class="inline-flex shrink-0 rounded bg-accent-soft px-1.5 py-0.5 text-micro font-semibold text-accent-text"
            >
              Knowledge
            </span>
          {:else}
            <!-- Tags are free text: a long one has to truncate, not spill the row. -->
            <span
              class="inline-block max-w-full truncate rounded bg-line px-1.5 py-0.5 align-middle text-micro font-medium text-fg-muted"
              title={tag}
            >
              {tag}
            </span>
          {/if}
        {/each}
      </div>
    {/if}
  {/snippet}

  {#snippet docAuthor(doc)}
    {@const author = docAuthorCell(doc)}
    {#if author && !author.pending}
      <!-- A doc row said when it changed and never who. The name comes from
           data the row already carries; a name still loading is left out
           rather than shown as a raw id and corrected. -->
      <span class="whitespace-nowrap" title={author.title}>
        by {author.name}
      </span>
    {/if}
  {/snippet}

  {#snippet docLastComment(doc)}
    {@const comment = doc?.last_comment}
    {@const commentBody = String(comment?.body ?? "").trim()}
    {#if commentBody && !commentRestatesTitle(commentBody, resourceDisplayLabel(doc))}
      <p class="mt-1 flex min-w-0 items-baseline gap-1.5 text-micro">
        <span class="shrink-0 text-fg-subtle">Last comment</span>
        <span class="min-w-0 truncate text-fg-muted" title={commentBody}>
          {previewText(commentBody)}
        </span>
        <span aria-hidden="true" class="shrink-0 text-fg-subtle">·</span>
        <span class="shrink-0 tabular-nums text-fg-muted">
          <Time value={comment.created_at} fallback="—" />
        </span>
      </p>
    {/if}
  {/snippet}

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
                    label={BOARD_LIFECYCLE_STATE_LABELS[doc.state]}
                    forceShow={docsHaveMixedLifecycle}
                  />
                  {#if doc.head_revision_number != null}
                    <span
                      class="font-mono text-micro tabular-nums text-fg-subtle"
                      title="Latest version"
                    >
                      v{doc.head_revision_number}
                    </span>
                  {/if}
                {/snippet}
              </WorkspaceResourceListRow>
              <span
                class="hidden shrink-0 self-start whitespace-nowrap pt-0.5 text-right text-micro text-fg-muted sm:inline"
                >Updated <Time value={doc.updated_at} fallback="—" />
                {@render docAuthor(doc)}</span
              >
            </div>
            <p class="mt-0.5 text-micro text-fg-muted sm:hidden">
              Updated <Time value={doc.updated_at} fallback="—" />
              {@render docAuthor(doc)}
            </p>
            {@render docEnrichment(doc, false)}
            {@render docLastComment(doc)}
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
          <div
            class="min-w-0 flex-1 px-3 py-2.5 transition-colors hover:bg-panel-hover sm:px-4"
          >
            <div class="flex min-w-0 items-start justify-between gap-2">
              <a
                class="block min-w-0 flex-1"
                href={workspaceHref(
                  `/docs/${encodeURIComponent(resourceRouteSegment(doc, "document"))}`,
                )}
              >
                <WorkspaceResourceListRow
                  title={resourceDisplayLabel(doc)}
                  description={doc.summary ?? ""}
                  titleClass="group-hover/row:text-accent-text transition-colors"
                >
                  {#snippet badges()}
                    <LifecycleBadge
                      state={doc.state}
                      label={BOARD_LIFECYCLE_STATE_LABELS[doc.state]}
                      forceShow={docsHaveMixedLifecycle}
                    />
                    {#if doc.head_revision_number != null}
                      <span
                        class="font-mono text-micro tabular-nums text-fg-subtle"
                        title="Latest version"
                      >
                        v{doc.head_revision_number}
                      </span>
                    {/if}
                  {/snippet}
                </WorkspaceResourceListRow>
              </a>
              <div class="shrink-0 sm:hidden">
                <CopyButton
                  value={docLink}
                  iconOnly
                  icon="link"
                  label="Copy document link"
                  size="sm"
                />
              </div>
            </div>
            <p class="mt-0.5 text-micro text-fg-muted sm:hidden">
              Updated <Time value={doc.updated_at} fallback="—" />
              {@render docAuthor(doc)}
            </p>
            {@render docEnrichment(doc, true)}
            {@render docLastComment(doc)}
          </div>
        {/snippet}
        {#snippet meta()}
          <!-- Same words as the phone row: a bare "7 min ago" at the end of a
               row does not say which of a doc's several timestamps it is. -->
          <span
            class="hidden whitespace-nowrap text-right text-fg-muted sm:inline"
            >Updated <Time value={doc.updated_at} fallback="—" />
            {@render docAuthor(doc)}</span
          >
        {/snippet}
        {#snippet actions()}
          <div class="hidden sm:block">
            <CopyButton
              value={docLink}
              iconOnly
              icon="link"
              label="Copy document link"
              size="sm"
            />
          </div>
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
    {#if searchTruncated}
      <p class="mt-2 text-micro text-fg-muted" role="status">
        Showing the first {documents.length} matches. Narrow the search to see the
        rest.
      </p>
    {/if}
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
