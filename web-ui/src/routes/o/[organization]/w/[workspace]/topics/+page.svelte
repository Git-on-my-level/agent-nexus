<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import {
    buildThreadFilterQueryParamsFromThreadListState,
    buildTopicListApiQueryParams,
    buildTopicListSearchString,
    parseTopicListSearchParams,
  } from "$lib/topicFilters";
  import { BOARD_LIFECYCLE_STATE_LABELS } from "$lib/boardUtils";
  import { workspacePath } from "$lib/workspacePaths";
  import { buildTopicCreatePayloadFromDraft } from "$lib/topicCreatePayload";
  import CompactFilterBar from "$lib/components/CompactFilterBar.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import WorkspaceResourceListRow from "$lib/components/WorkspaceResourceListRow.svelte";
  import WorkspaceListBulkToolbar from "$lib/components/WorkspaceListBulkToolbar.svelte";
  import LeadingSelectionGlyph from "$lib/components/LeadingSelectionGlyph.svelte";
  import Button from "$lib/components/Button.svelte";

  const defaultFilters = {
    state: "active",
    q: "",
  };

  let filters = $state({ ...defaultFilters });
  let loading = $state(false);
  let error = $state("");
  let retrying = $state(false);
  let topics = $state([]);
  let createOpen = $state(false);
  let creatingTopic = $state(false);
  let createError = $state("");
  let filtersOpen = $state(false);
  let showArchived = $state(false);
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
  /** @type {Set<string>} */
  let selectedTopicIds = $state(new Set());
  let topicSelectMode = $state(false);
  /** @type {number | null} */
  let topicSelectionAnchorIndex = $state(null);
  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);

  /** `/topics` imports this module; `/threads` uses it directly. Data source and copy differ. */
  let listSurface = $derived.by(() => {
    const path = String($page.url.pathname ?? "").replace(/\/+$/, "");
    return path.endsWith("/topics") ? "topics" : "threads";
  });

  let backingThreads = $state([]);

  let topicDraft = $state({
    title: "",
    summary: "",
  });

  function workspaceHref(pathname = "/") {
    return workspacePath(organizationSlug, workspaceSlug, pathname);
  }

  /** @param {string} ref */
  function topicSegmentFromTypedRef(ref) {
    const s = String(ref ?? "").trim();
    if (!s.startsWith("topic:")) return "";
    return s.slice("topic:".length).trim();
  }

  async function loadBackingThreads(isRetry = false) {
    loading = true;
    error = "";
    retrying = isRetry;
    try {
      const query = buildThreadFilterQueryParamsFromThreadListState(filters);
      const response = await coreClient.listThreads(query);
      backingThreads = response.threads ?? [];
    } catch (loadError) {
      const reason =
        loadError instanceof Error ? loadError.message : String(loadError);
      error = `Failed to load threads: ${reason}`;
    } finally {
      loading = false;
      retrying = false;
    }
  }

  $effect(() => {
    workspaceSlug;
    listSurface;
    if (listSurface === "threads") {
      const parsed = parseTopicListSearchParams($page.url.searchParams);
      filters = { ...defaultFilters, ...parsed };
      if ([...$page.url.searchParams.keys()].length > 0) {
        filtersOpen = true;
      }
      void loadBackingThreads();
      return;
    }

    showArchived;
    const parsed = parseTopicListSearchParams($page.url.searchParams);
    filters = { ...defaultFilters, ...parsed };
    if ([...$page.url.searchParams.keys()].length > 0) {
      filtersOpen = true;
    }
    void loadTopicsFromState(parsed);
  });

  async function loadTopicsFromState(state, isRetry = false) {
    loading = true;
    error = "";
    retrying = isRetry;

    try {
      const query = buildTopicListApiQueryParams(state, {
        includeArchived: showArchived,
      });
      const response = await coreClient.listTopics(query);
      topics = response.topics ?? [];
    } catch (loadError) {
      const reason =
        loadError instanceof Error ? loadError.message : String(loadError);
      error = `Failed to load topics: ${reason}`;
    } finally {
      loading = false;
      retrying = false;
    }
  }

  async function loadTopics() {
    await loadTopicsFromState(filters);
  }

  $effect(() => {
    if (listSurface !== "topics") {
      selectedTopicIds = new Set();
      topicSelectMode = false;
      topicSelectionAnchorIndex = null;
    }
  });

  $effect(() => {
    if (listSurface !== "topics") return;
    topics;
    const valid = new Set(
      topics.map((t) => String(t?.id ?? "").trim()).filter(Boolean),
    );
    const next = new Set([...selectedTopicIds].filter((id) => valid.has(id)));
    if (next.size !== selectedTopicIds.size) {
      selectedTopicIds = next;
    }
  });

  let allTopicsVisibleSelected = $derived(
    listSurface === "topics" &&
      topics.length > 0 &&
      topics.every((t) => selectedTopicIds.has(t.id)),
  );
  let selectedTopics = $derived(
    listSurface === "topics"
      ? topics.filter((t) => selectedTopicIds.has(t.id))
      : [],
  );

  let bulkTopicsCanArchive = $derived(
    selectedTopics.some((t) => !isTopicArchived(t) && !isTopicTrashed(t)),
  );
  let bulkTopicsCanUnarchive = $derived(
    selectedTopics.some((t) => isTopicArchived(t) && !isTopicTrashed(t)),
  );
  let bulkTopicsCanTrash = $derived(
    selectedTopics.some((t) => !isTopicTrashed(t)),
  );

  function selectAllVisibleTopics() {
    selectedTopicIds = new Set(topics.map((t) => t.id).filter(Boolean));
  }

  function clearTopicSelection() {
    selectedTopicIds = new Set();
  }

  function toggleTopicSelectMode() {
    topicSelectMode = !topicSelectMode;
    if (!topicSelectMode) {
      clearTopicSelection();
      topicSelectionAnchorIndex = null;
    }
  }

  function applyTopicRangeFromIndices(fromIndex, toIndex) {
    const lo = Math.min(fromIndex, toIndex);
    const hi = Math.max(fromIndex, toIndex);
    const next = new Set(selectedTopicIds);
    for (let i = lo; i <= hi; i++) {
      const t = topics[i];
      if (t?.id) next.add(t.id);
    }
    selectedTopicIds = next;
  }

  /** @param {MouseEvent} e */
  function handleTopicRowClick(topic, index, e) {
    if (!topicSelectMode || bulkBusy) return;
    const href = workspaceHref(`/topics/${encodeURIComponent(topic.id)}`);
    const ce = /** @type {MouseEvent & { detail?: number }} */ (e);
    if ((ce.detail ?? 1) >= 2) {
      void goto(href);
      return;
    }
    if (e.shiftKey && topicSelectionAnchorIndex !== null) {
      applyTopicRangeFromIndices(topicSelectionAnchorIndex, index);
      return;
    }
    const id = topic.id;
    const next = new Set(selectedTopicIds);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selectedTopicIds = next;
    topicSelectionAnchorIndex = index;
  }

  /** @param {KeyboardEvent} e */
  function topicRowKeydown(topic, index, e) {
    if (!topicSelectMode || bulkBusy) return;
    if (e.key !== " " && e.key !== "Enter") return;
    e.preventDefault();
    if (e.shiftKey && topicSelectionAnchorIndex !== null) {
      applyTopicRangeFromIndices(topicSelectionAnchorIndex, index);
      return;
    }
    const id = topic.id;
    const next = new Set(selectedTopicIds);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selectedTopicIds = next;
    topicSelectionAnchorIndex = index;
  }

  $effect(() => {
    if (!topicSelectMode || listSurface !== "topics") return;
    /** @param {KeyboardEvent} ev */
    function onTopicKey(ev) {
      if (ev.key !== "Escape") return;
      topicSelectMode = false;
      clearTopicSelection();
      topicSelectionAnchorIndex = null;
    }
    document.addEventListener("keydown", onTopicKey);
    return () => document.removeEventListener("keydown", onTopicKey);
  });

  function topicIdsForBulkArchive() {
    return selectedTopics
      .filter((t) => !isTopicArchived(t) && !isTopicTrashed(t))
      .map((t) => t.id);
  }

  function topicIdsForBulkUnarchive() {
    return selectedTopics
      .filter((t) => isTopicArchived(t) && !isTopicTrashed(t))
      .map((t) => t.id);
  }

  function topicIdsForBulkTrash() {
    return selectedTopics.filter((t) => !isTopicTrashed(t)).map((t) => t.id);
  }

  async function applyFilters() {
    const qs = buildTopicListSearchString(filters);
    const path =
      listSurface === "topics"
        ? workspaceHref("/topics")
        : workspaceHref("/threads");
    await goto(`${path}${qs ? `?${qs}` : ""}`, {
      replaceState: true,
      noScroll: true,
      keepFocus: true,
    });
  }

  async function resetFilters() {
    const path =
      listSurface === "topics"
        ? workspaceHref("/topics")
        : workspaceHref("/threads");
    await goto(path, {
      replaceState: true,
      noScroll: true,
      keepFocus: true,
    });
  }

  function resetTopicDraft() {
    topicDraft = {
      title: "",
      summary: "",
    };
  }

  async function createTopic() {
    if (!topicDraft.title.trim()) {
      createError = "Topic title is required.";
      return;
    }

    creatingTopic = true;
    createError = "";

    try {
      await coreClient.createTopic(
        buildTopicCreatePayloadFromDraft(topicDraft),
      );

      createOpen = false;
      resetTopicDraft();
      await loadTopics();
    } catch (submitError) {
      const reason =
        submitError instanceof Error
          ? submitError.message
          : String(submitError);
      createError = `Failed to create topic: ${reason}`;
    } finally {
      creatingTopic = false;
    }
  }

  let hasActiveFilters = $derived(
    showArchived || filters.state !== "active" || filters.q.trim() !== "",
  );

  function topicStatePillTone(state) {
    if (state === "active") return "text-ok-text bg-ok-soft";
    if (state === "archived") return "text-warn-text bg-warn-soft";
    if (state === "trashed") return "text-slate-300 bg-slate-500/10";
    return "text-[var(--fg-muted)] bg-[var(--line)]";
  }

  function isTopicArchived(topic) {
    const at = topic?.archived_at;
    return typeof at === "string" ? at.trim() !== "" : Boolean(at);
  }

  function isTopicTrashed(topic) {
    return topic?.state === "trashed";
  }

  async function archiveTopicRow(topicId) {
    const id = String(topicId ?? "").trim();
    if (!id || archiveBusyId || bulkBusy) return;
    archiveBusyId = id;
    error = "";
    try {
      await coreClient.archiveTopic(id, {});
      await loadTopics();
    } catch (e) {
      error = `Archive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      archiveBusyId = "";
    }
  }

  async function trashTopicRow(topicId) {
    const id = String(topicId ?? "").trim();
    if (!id || trashBusyId || bulkBusy) return;
    trashBusyId = id;
    error = "";
    try {
      await coreClient.trashTopic(id, {});
      confirmModal = { open: false, action: "", entityId: "", bulkIds: null };
      await loadTopics();
    } catch (e) {
      error = `Trash failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      trashBusyId = "";
    }
  }

  async function bulkArchiveTopics(ids) {
    const list = ids.filter(Boolean);
    if (!list.length || bulkBusy) return;
    bulkBusy = true;
    error = "";
    try {
      for (const id of list) {
        await coreClient.archiveTopic(id, {});
      }
      clearTopicSelection();
      confirmModal = { open: false, action: "", entityId: "", bulkIds: null };
      await loadTopics();
    } catch (e) {
      error = `Archive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      bulkBusy = false;
    }
  }

  async function bulkUnarchiveTopics(ids) {
    const list = ids.filter(Boolean);
    if (!list.length || bulkBusy) return;
    bulkBusy = true;
    error = "";
    try {
      for (const id of list) {
        await coreClient.unarchiveTopic(id, {});
      }
      clearTopicSelection();
      await loadTopics();
    } catch (e) {
      error = `Unarchive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      bulkBusy = false;
    }
  }

  async function bulkTrashTopics(ids) {
    const list = ids.filter(Boolean);
    if (!list.length || bulkBusy) return;
    bulkBusy = true;
    error = "";
    try {
      for (const id of list) {
        await coreClient.trashTopic(id, {});
      }
      clearTopicSelection();
      confirmModal = { open: false, action: "", entityId: "", bulkIds: null };
      await loadTopics();
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
      if (action === "archive") void bulkArchiveTopics(bulkIds);
      else if (action === "trash") void bulkTrashTopics(bulkIds);
      return;
    }
    if (action === "archive") void archiveTopicRow(id);
    else if (action === "trash") void trashTopicRow(id);
  }

  let topicConfirmBulkCount = $derived(confirmModal.bulkIds?.length ?? 0);
  let topicConfirmIsBulk = $derived(topicConfirmBulkCount > 0);

  let topicConfirmModalTitle = $derived.by(() => {
    if (confirmModal.action === "trash") {
      return topicConfirmIsBulk
        ? `Move ${topicConfirmBulkCount} topics to trash`
        : "Move to trash";
    }
    return topicConfirmIsBulk
      ? `Archive ${topicConfirmBulkCount} topics`
      : "Archive topic";
  });

  let topicConfirmModalMessage = $derived.by(() => {
    if (confirmModal.action === "trash") {
      return topicConfirmIsBulk
        ? `These topics (${topicConfirmBulkCount}) will be moved to trash. You can restore them later.`
        : "This topic will be moved to trash. You can restore it later.";
    }
    return topicConfirmIsBulk
      ? `These topics (${topicConfirmBulkCount}) will be hidden from default views. You can unarchive them later.`
      : "This topic will be hidden from default views. You can unarchive it later.";
  });

  let topicConfirmModalBusy = $derived(
    confirmModal.action === "trash"
      ? Boolean(trashBusyId) || (topicConfirmIsBulk && bulkBusy)
      : Boolean(archiveBusyId) || (topicConfirmIsBulk && bulkBusy),
  );
</script>

<div class="mb-3 flex max-md:mb-2 flex-wrap items-start justify-between gap-4">
  <div class="min-w-0 flex-1">
    <h1 class="text-subtitle font-semibold text-[var(--fg)]">
      {listSurface === "topics" ? "Topics" : "Threads"}
    </h1>
    {#if listSurface === "topics"}
      <!-- subtitle removed; heading is self-evident -->
    {:else}
      <p class="mt-1 hidden text-micro text-[var(--fg-muted)] sm:block">
        Diagnostic list of append-only backing threads (timelines). Not every
        thread is a topic; prefer
        <a
          class="text-accent-text transition-colors hover:text-accent-text"
          href={workspaceHref("/topics")}>Topics</a
        >
        for triage and planning.
      </p>
    {/if}
  </div>
  <div class="flex flex-wrap items-center justify-end gap-2 sm:gap-1.5">
    {#if listSurface === "topics"}
      <button
        class="cursor-pointer inline-flex h-7 items-center gap-1.5 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 text-micro font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] {topics.length ===
          0 && !loading
          ? 'pointer-events-none opacity-50'
          : ''}"
        onclick={toggleTopicSelectMode}
        disabled={topics.length === 0 && !loading}
        type="button"
        aria-pressed={topicSelectMode}
      >
        {topicSelectMode ? "Done" : "Select"}
      </button>
      <button
        class="cursor-pointer inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-micro font-medium transition-colors {hasActiveFilters
          ? 'border-[var(--accent)]/40 bg-[var(--accent)]/10 text-[var(--accent)] hover:bg-[var(--accent)]/15'
          : 'border-[var(--line)] bg-[var(--bg-soft)] text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]'}"
        onclick={() => (filtersOpen = !filtersOpen)}
        type="button"
        data-testid="topics-filters-toggle"
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
        class="cursor-pointer inline-flex h-7 items-center gap-1.5 rounded-md bg-[var(--panel)] px-3 text-micro font-medium text-[var(--fg)] transition-colors hover:bg-[var(--line)]"
        onclick={() => (createOpen = !createOpen)}
        type="button"
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
        {createOpen ? "Cancel" : "New topic"}
      </button>
    {:else}
      <button
        class="cursor-pointer inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-micro font-medium transition-colors {hasActiveFilters
          ? 'border-[var(--accent)]/40 bg-[var(--accent)]/10 text-[var(--accent)] hover:bg-[var(--accent)]/15'
          : 'border-[var(--line)] bg-[var(--bg-soft)] text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]'}"
        onclick={() => (filtersOpen = !filtersOpen)}
        type="button"
        data-testid="topics-filters-toggle"
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
      <Button
        variant="secondary"
        size="compact"
        class="rounded-md font-medium bg-[var(--panel)] hover:bg-[var(--line)]"
        href={workspaceHref("/topics")}
      >
        Open topics
      </Button>
    {/if}
  </div>
</div>

{#if error}
  <StateError
    message={error}
    onretry={() =>
      void (listSurface === "topics"
        ? loadTopicsFromState(filters, true)
        : loadBackingThreads(true))}
    {retrying}
    class="mb-4"
  />
{/if}

{#if (listSurface === "topics" || listSurface === "threads") && filtersOpen}
  <CompactFilterBar testId="topics-filter-panel">
    {#snippet children()}
      <div class="grid gap-3 sm:grid-cols-2">
        <label class="text-micro">
          <span class="font-medium text-[var(--fg-muted)]">State</span>
          <select
            class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
            bind:value={filters.state}
          >
            {#each Object.entries(BOARD_LIFECYCLE_STATE_LABELS) as [value, label]}
              <option {value}>{label}</option>
            {/each}
          </select>
        </label>
        <label class="text-micro sm:col-span-1">
          <span class="font-medium text-[var(--fg-muted)]">Search</span>
          <input
            bind:value={filters.q}
            class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
            placeholder="Title or id…"
            type="search"
            autocomplete="off"
          />
        </label>
      </div>
      {#if listSurface === "topics"}
        <label
          class="mt-3 inline-flex cursor-pointer items-center gap-1.5 text-micro text-[var(--fg-muted)]"
        >
          <input
            bind:checked={showArchived}
            class="h-3.5 w-3.5 cursor-pointer rounded border-[var(--line)] bg-[var(--bg)] text-[var(--accent-hover)] focus:ring-2 focus:ring-[var(--accent)] focus:ring-offset-0"
            type="checkbox"
            data-testid="topics-show-archived"
          />
          Show archived
        </label>
      {/if}
      <div class="mt-3 flex gap-1.5">
        <button
          class="cursor-pointer rounded-md bg-[var(--panel)] px-3 py-1.5 text-micro font-medium text-[var(--fg)] hover:bg-[var(--line)]"
          onclick={applyFilters}
          type="button">Apply</button
        >
        <button
          class="cursor-pointer rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-1.5 text-micro font-medium text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]"
          onclick={resetFilters}
          type="button">Clear filters</button
        >
      </div>
    {/snippet}
  </CompactFilterBar>
{/if}

{#if listSurface === "topics" && createOpen}
  <form
    class="mb-4 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] p-4"
    onsubmit={(event) => {
      event.preventDefault();
      createTopic();
    }}
  >
    {#if createError}
      <div
        class="mb-3 rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
      >
        {createError}
      </div>
    {/if}
    <div class="grid gap-3">
      <label class="text-micro">
        <span class="font-medium text-[var(--fg-muted)]">Title</span>
        <input
          bind:value={topicDraft.title}
          class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-meta transition-colors focus:bg-[var(--panel)]"
          placeholder="Topic title..."
          required
          type="text"
        />
      </label>
      <label class="text-micro">
        <span class="font-medium text-[var(--fg-muted)]">Summary</span>
        <textarea
          bind:value={topicDraft.summary}
          class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-meta transition-colors focus:bg-[var(--panel)]"
          placeholder="Brief description..."
          rows="2"
        ></textarea>
      </label>
    </div>
    <div class="mt-3 flex justify-end">
      <button
        class="cursor-pointer rounded-md bg-accent-solid px-4 py-2 text-micro font-medium text-white hover:bg-accent disabled:opacity-50"
        disabled={creatingTopic}
        type="submit"
      >
        {creatingTopic ? "Creating…" : "Create topic"}
      </button>
    </div>
  </form>
{/if}

{#if listSurface === "topics"}
  {#if loading && topics.length === 0}
    <Skeleton rows={8} />
  {:else if topics.length === 0 && !error}
    <StateEmpty
      title={hasActiveFilters
        ? "No topics match the current filters"
        : "No topics yet"}
      helper={hasActiveFilters
        ? "Try adjusting or clearing the current filters."
        : "Topics track ongoing work — a project, an incident, a decision, or a recurring process. Create one to start the conversation."}
      actionLabel={hasActiveFilters ? "Clear filters" : ""}
      onclick={hasActiveFilters ? resetFilters : undefined}
    />
  {:else}
    {#snippet topicRow(topic, index, showBorderTop)}
      {@const selected = selectedTopicIds.has(topic.id)}
      {#if topicSelectMode}
        <div
          aria-label={`${selected ? "Deselect" : "Select"} ${topic.title || topic.id}`}
          aria-pressed={selected}
          class="flex cursor-pointer items-stretch outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg-soft)] {showBorderTop
            ? 'border-t border-[var(--line)]'
            : ''} {selected
            ? 'border-l-[3px] border-l-[var(--accent)] bg-[var(--accent)]/10'
            : 'border-l-[3px] border-l-transparent hover:bg-[var(--line-subtle)]'}"
          onclick={(e) => handleTopicRowClick(topic, index, e)}
          onkeydown={(e) => topicRowKeydown(topic, index, e)}
          role="button"
          tabindex="0"
        >
          <div
            class="flex shrink-0 items-center self-stretch pr-1 pl-2 sm:pl-3"
          >
            <LeadingSelectionGlyph {selected} />
          </div>
          <div
            class="pointer-events-none flex min-w-0 flex-1 items-center gap-3 px-3 py-2.5"
          >
            <WorkspaceResourceListRow
              title={topic.title}
              description={topic.current_summary ?? topic.summary ?? ""}
            >
              {#snippet badges()}
                {#if topic.state}
                  <span
                    class="inline-flex rounded px-1.5 py-0.5 text-micro font-semibold capitalize {topicStatePillTone(
                      topic.state,
                    )}">{topic.state}</span
                  >
                {/if}
                {#if isTopicArchived(topic) && topic.state !== "archived"}
                  <span
                    class="rounded bg-warn-soft px-1.5 py-0.5 text-micro font-medium text-warn-text"
                    >Archived</span
                  >
                {/if}
              {/snippet}
            </WorkspaceResourceListRow>
            <div
              class="flex shrink-0 items-center gap-1.5 self-start pt-0.5 text-micro"
            >
              <span class="w-14 text-right text-[var(--fg-muted)]"
                >{formatTimestamp(topic.updated_at) || "—"}</span
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
            class="flex min-w-0 flex-1 items-center gap-3 px-3 py-2.5 transition-colors hover:bg-[var(--line-subtle)]"
            href={workspaceHref(`/topics/${encodeURIComponent(topic.id)}`)}
          >
            <WorkspaceResourceListRow
              title={topic.title}
              description={topic.current_summary ?? topic.summary ?? ""}
            >
              {#snippet badges()}
                {#if topic.state}
                  <span
                    class="inline-flex rounded px-1.5 py-0.5 text-micro font-semibold capitalize {topicStatePillTone(
                      topic.state,
                    )}">{topic.state}</span
                  >
                {/if}
                {#if isTopicArchived(topic) && topic.state !== "archived"}
                  <span
                    class="rounded bg-warn-soft px-1.5 py-0.5 text-micro font-medium text-warn-text"
                    >Archived</span
                  >
                {/if}
              {/snippet}
            </WorkspaceResourceListRow>
            <div
              class="flex shrink-0 items-center gap-1.5 self-start pt-0.5 text-micro"
            >
              <span class="w-14 text-right text-[var(--fg-muted)]"
                >{formatTimestamp(topic.updated_at) || "—"}</span
              >
            </div>
          </a>
        </div>
      {/if}
    {/snippet}
    {#if topicSelectMode}
      <WorkspaceListBulkToolbar
        allVisibleSelected={allTopicsVisibleSelected}
        busy={bulkBusy}
        canArchive={bulkTopicsCanArchive}
        canTrash={bulkTopicsCanTrash}
        canUnarchive={bulkTopicsCanUnarchive}
        onArchive={() => {
          const ids = topicIdsForBulkArchive();
          if (!ids.length) return;
          confirmModal = {
            open: true,
            action: "archive",
            entityId: "",
            bulkIds: ids,
          };
        }}
        onClear={clearTopicSelection}
        onDeselectAll={clearTopicSelection}
        onSelectAll={selectAllVisibleTopics}
        onTrash={() => {
          const ids = topicIdsForBulkTrash();
          if (!ids.length) return;
          confirmModal = {
            open: true,
            action: "trash",
            entityId: "",
            bulkIds: ids,
          };
        }}
        onUnarchive={() => void bulkUnarchiveTopics(topicIdsForBulkUnarchive())}
        selectionChromeActive={true}
        selectedCount={selectedTopicIds.size}
      />
    {/if}
    <div
      class="space-y-px overflow-hidden rounded-md border border-[var(--line)] bg-[var(--bg-soft)]"
    >
      {#each topics as topic, i}
        {@render topicRow(topic, i, i > 0)}
      {/each}
    </div>
  {/if}
{:else if loading && backingThreads.length === 0}
  <Skeleton rows={6} />
{:else if backingThreads.length === 0 && !error}
  <StateEmpty
    title="No threads returned"
    helper="Backing threads are append-only timelines. Not every thread is a topic."
  />
{:else if backingThreads.length === 0}
  <StateEmpty
    title="No threads match the current filters"
    actionLabel={hasActiveFilters ? "Clear filters" : ""}
    onclick={hasActiveFilters ? resetFilters : undefined}
  />
{:else}
  <div
    class="space-y-px overflow-hidden rounded-md border border-[var(--line)] bg-[var(--bg-soft)]"
  >
    {#each backingThreads as thread, i}
      {@const topicSeg = topicSegmentFromTypedRef(thread.topic_ref)}
      <div
        class="flex items-stretch {i > 0
          ? 'border-t border-[var(--line)]'
          : ''}"
      >
        <a
          class="flex min-w-0 flex-1 flex-col gap-0.5 px-3 py-2.5 transition-colors hover:bg-[var(--line-subtle)]"
          href={workspaceHref(`/threads/${encodeURIComponent(thread.id)}`)}
        >
          <div class="flex flex-wrap items-center gap-2">
            <p class="truncate text-meta font-medium text-[var(--fg)]">
              {thread.title || thread.id}
            </p>
            {#if thread.state === "archived"}
              <span
                class="shrink-0 rounded bg-warn-soft px-1.5 py-0.5 text-micro font-medium text-warn-text"
                >Archived</span
              >
            {/if}
          </div>
          <p class="truncate font-mono text-micro text-[var(--fg-muted)]">
            {thread.id}
          </p>
          {#if topicSeg}
            <p class="truncate text-micro text-[var(--fg-muted)]">
              Linked topic:
              <span class="text-[var(--fg)]">{topicSeg}</span>
            </p>
          {:else}
            <p class="truncate text-micro text-[var(--fg-muted)]">
              No topic ref (non-topic or internal timeline)
            </p>
          {/if}
          <p class="text-micro text-[var(--fg-muted)]">
            Updated {formatTimestamp(thread.updated_at) || "—"}
          </p>
        </a>
        {#if topicSeg}
          <div
            class="flex shrink-0 items-center border-l border-[var(--line)] px-2"
          >
            <span class="text-micro font-medium">
              <RefLink refValue={`topic:${topicSeg}`} humanize showRaw />
            </span>
          </div>
        {/if}
      </div>
    {/each}
  </div>
{/if}

{#if listSurface === "topics"}
  <ConfirmModal
    open={confirmModal.open}
    title={topicConfirmModalTitle}
    message={topicConfirmModalMessage}
    confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
    variant={confirmModal.action === "trash" ? "danger" : "warning"}
    busy={topicConfirmModalBusy}
    onconfirm={handleConfirm}
    oncancel={() =>
      (confirmModal = { open: false, action: "", entityId: "", bulkIds: null })}
  />
{/if}
