<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import {
    TOPIC_STATUSES,
    applyBackingThreadListClientFilters,
    applyTopicListClientFilters,
    buildThreadFilterQueryParamsFromThreadListState,
    buildTopicListApiQueryParams,
    buildTopicListSearchString,
    parseTopicListSearchParams,
  } from "$lib/topicFilters";
  import { workspacePath } from "$lib/workspacePaths";
  import {
    CANONICAL_TOPIC_TYPE_LABELS,
    CANONICAL_TOPIC_TYPES,
  } from "$lib/topicTypeGlyph.js";
  import { buildTopicCreatePayloadFromDraft } from "$lib/topicCreatePayload";
  import ArchiveButton from "$lib/components/ArchiveButton.svelte";
  import CompactFilterBar from "$lib/components/CompactFilterBar.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import TrashButton from "$lib/components/TrashButton.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import TopicTypeGlyph from "$lib/components/TopicTypeGlyph.svelte";

  /** Virtual filter: active lifecycle topics (matches dashboard "Open"); distinct from `state` query. */
  const STATUS_OPEN_NOT_CLOSED = "__open__";

  const defaultFilters = {
    state: "",
    q: "",
    openOnly: false,
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
  let confirmModal = $state({ open: false, action: "", entityId: "" });
  let trashBusyId = $state("");
  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);

  /** `/topics` imports this module; `/threads` uses it directly. Data source and copy differ. */
  let listSurface = $derived.by(() => {
    const path = String($page.url.pathname ?? "").replace(/\/+$/, "");
    return path.endsWith("/topics") ? "topics" : "threads";
  });

  let backingThreads = $state([]);

  let filteredBackingThreads = $derived(
    applyBackingThreadListClientFilters(backingThreads, filters),
  );

  let topicDraft = $state({
    title: "",
    summary: "",
    type: "other",
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
      let list = response.topics ?? [];
      list = applyTopicListClientFilters(list, state);
      topics = list;
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
      type: "other",
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
    filters.state !== "" || filters.openOnly || filters.q.trim() !== "",
  );

  function statusFilterSelectValue() {
    if (filters.openOnly) return STATUS_OPEN_NOT_CLOSED;
    return filters.state;
  }

  function onStatusFilterChange(value) {
    if (value === STATUS_OPEN_NOT_CLOSED) {
      filters = { ...filters, openOnly: true, state: "" };
    } else {
      filters = { ...filters, openOnly: false, state: value };
    }
  }

  function lifecycleStateColor(state) {
    const styles = {
      active: "text-ok-text",
      archived: "text-warn-text",
      trashed: "text-slate-300",
    };
    return styles[state] ?? "text-fg-subtle";
  }

  function isTopicArchived(topic) {
    const at = topic?.archived_at;
    return typeof at === "string" ? at.trim() !== "" : Boolean(at);
  }

  async function archiveTopicRow(topicId) {
    const id = String(topicId ?? "").trim();
    if (!id || archiveBusyId) return;
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

  async function unarchiveTopicRow(topicId) {
    const id = String(topicId ?? "").trim();
    if (!id || archiveBusyId) return;
    archiveBusyId = id;
    error = "";
    try {
      await coreClient.unarchiveTopic(id, {});
      await loadTopics();
    } catch (e) {
      error = `Unarchive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      archiveBusyId = "";
    }
  }

  async function trashTopicRow(topicId) {
    const id = String(topicId ?? "").trim();
    if (!id || trashBusyId) return;
    trashBusyId = id;
    error = "";
    try {
      await coreClient.trashTopic(id, {});
      confirmModal = { open: false, action: "", entityId: "" };
      await loadTopics();
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
    if (action === "archive") void archiveTopicRow(id);
    else if (action === "trash") void trashTopicRow(id);
  }
</script>

<div class="mb-4 flex flex-wrap items-start justify-between gap-4">
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
      <label
        class="inline-flex cursor-pointer items-center gap-1.5 text-micro text-[var(--fg-muted)]"
      >
        <input
          bind:checked={showArchived}
          class="h-3.5 w-3.5 cursor-pointer rounded border-[var(--line)] bg-[var(--bg)] text-[var(--accent-hover)] focus:ring-2 focus:ring-[var(--accent)] focus:ring-offset-0"
          type="checkbox"
        />
        Show archived
      </label>
      <button
        class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-micro font-medium transition-colors {hasActiveFilters
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
        class="cursor-pointer inline-flex items-center gap-1.5 rounded-md bg-[var(--panel)] px-3 py-1.5 text-micro font-medium text-[var(--fg)] transition-colors hover:bg-[var(--line)]"
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
        class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-micro font-medium transition-colors {hasActiveFilters
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
      <a
        class="rounded-md bg-[var(--panel)] px-3 py-1.5 text-micro font-medium text-[var(--fg)] transition-colors hover:bg-[var(--line)]"
        href={workspaceHref("/topics")}>Open topics</a
      >
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
            onchange={(event) =>
              onStatusFilterChange(event.currentTarget.value)}
            value={statusFilterSelectValue()}
          >
            <option value="">All</option>
            <option value={STATUS_OPEN_NOT_CLOSED}>Open (not closed)</option>
            {#each TOPIC_STATUSES as status}<option value={status}
                >{status[0].toUpperCase() + status.slice(1)}</option
              >{/each}
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
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="text-micro sm:col-span-2">
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
        <span class="font-medium text-[var(--fg-muted)]">Type</span>
        <select
          bind:value={topicDraft.type}
          class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-2 text-meta transition-colors focus:bg-[var(--panel)]"
        >
          {#each CANONICAL_TOPIC_TYPES as t}<option value={t}
              >{CANONICAL_TOPIC_TYPE_LABELS[t]}</option
            >{/each}
        </select>
      </label>
      <label class="text-micro sm:col-span-2">
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
    <div
      class="space-y-px overflow-hidden rounded-md border border-[var(--line)] bg-[var(--bg-soft)]"
    >
      {#each topics as topic, i}
        <div
          class="flex items-stretch {i > 0
            ? 'border-t border-[var(--line)]'
            : ''}"
        >
          <a
            class="flex min-w-0 flex-1 items-center gap-3 px-3 py-2.5 transition-colors hover:bg-[var(--line-subtle)]"
            href={workspaceHref(`/topics/${encodeURIComponent(topic.id)}`)}
          >
            <TopicTypeGlyph type={topic.type} class="shrink-0" />
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <p class="truncate text-meta font-medium text-[var(--fg)]">
                  {topic.title}
                </p>
                {#if isTopicArchived(topic)}
                  <span
                    class="shrink-0 rounded bg-warn-soft px-1.5 py-0.5 text-micro font-medium text-warn-text"
                    >Archived</span
                  >
                {/if}
              </div>
              <p class="truncate text-micro text-[var(--fg-muted)]">
                {topic.current_summary ?? topic.summary ?? ""}
              </p>
            </div>
            <div class="flex shrink-0 items-center gap-1.5 text-micro">
              {#if topic.state && topic.state !== "active"}
                <span
                  class="font-medium capitalize {lifecycleStateColor(
                    topic.state,
                  )}">{topic.state}</span
                >
              {/if}
              <span class="w-14 text-right text-[var(--fg-muted)]"
                >{formatTimestamp(topic.updated_at) || "—"}</span
              >
            </div>
          </a>
          <div class="hidden shrink-0 items-center gap-1 px-2 sm:flex">
            <ArchiveButton
              archived={isTopicArchived(topic)}
              busy={Boolean(archiveBusyId) || Boolean(trashBusyId)}
              onarchive={() =>
                void (confirmModal = {
                  open: true,
                  action: "archive",
                  entityId: topic.id,
                })}
              onunarchive={() => void unarchiveTopicRow(topic.id)}
            />
            <TrashButton
              busy={Boolean(trashBusyId) || Boolean(archiveBusyId)}
              ontrash={() =>
                (confirmModal = {
                  open: true,
                  action: "trash",
                  entityId: topic.id,
                })}
            />
          </div>
        </div>
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
{:else if filteredBackingThreads.length === 0}
  <StateEmpty
    title="No threads match the current filters"
    actionLabel={hasActiveFilters ? "Clear filters" : ""}
    onclick={hasActiveFilters ? resetFilters : undefined}
  />
{:else}
  <div
    class="space-y-px overflow-hidden rounded-md border border-[var(--line)] bg-[var(--bg-soft)]"
  >
    {#each filteredBackingThreads as thread, i}
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
    title={confirmModal.action === "trash" ? "Move to trash" : "Archive topic"}
    message={confirmModal.action === "trash"
      ? "This topic will be moved to trash. You can restore it later."
      : "This topic will be hidden from default views. You can unarchive it later."}
    confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
    variant={confirmModal.action === "trash" ? "danger" : "warning"}
    busy={confirmModal.action === "trash"
      ? Boolean(trashBusyId)
      : Boolean(archiveBusyId)}
    onconfirm={handleConfirm}
    oncancel={() => (confirmModal = { open: false, action: "", entityId: "" })}
  />
{/if}
