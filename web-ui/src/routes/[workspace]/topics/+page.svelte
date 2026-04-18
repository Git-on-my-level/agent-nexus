<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import {
    TOPIC_SCHEDULE_PRESETS,
    TOPIC_SCHEDULE_PRESET_LABELS,
    TOPIC_PRIORITIES,
    TOPIC_PRIORITY_LABELS,
    TOPIC_STATUSES,
    applyTopicListClientFilters,
    buildThreadFilterQueryParamsFromThreadListState,
    buildTopicListApiQueryParams,
    buildTopicListSearchString,
    computeStaleness,
    formatCadenceLabel,
    getPriorityLabel,
    parseTopicListSearchParams,
    validateCadenceSelection,
  } from "$lib/topicFilters";
  import { workspacePath } from "$lib/workspacePaths";
  import { describeCron } from "$lib/topicPatch";
  import ArchiveButton from "$lib/components/ArchiveButton.svelte";
  import CompactFilterBar from "$lib/components/CompactFilterBar.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import TrashButton from "$lib/components/TrashButton.svelte";

  /** Virtual filter: non-closed topics (matches dashboard "Open"); distinct from status=active|paused. */
  const STATUS_OPEN_NOT_CLOSED = "__open__";
  /** Virtual filter: P0 and P1 (matches dashboard "High priority"); distinct from single priority. */
  const PRIORITY_HIGH_TIER = "__high_tier__";

  const defaultFilters = {
    status: "",
    priority: "",
    cadence: "",
    staleness: "all",
    tagInput: "",
    openOnly: false,
    highPriorityTier: false,
  };

  let filters = $state({ ...defaultFilters });
  let loading = $state(false);
  let error = $state("");
  let topics = $state([]);
  let createOpen = $state(false);
  let creatingTopic = $state(false);
  let createError = $state("");
  let filtersOpen = $state(false);
  let showArchived = $state(false);
  let archiveBusyId = $state("");
  let confirmModal = $state({ open: false, action: "", entityId: "" });
  let trashBusyId = $state("");
  let workspaceSlug = $derived($page.params.workspace);

  /** `/topics` imports this module; `/threads` uses it directly. Data source and copy differ. */
  let listSurface = $derived.by(() => {
    const path = String($page.url.pathname ?? "").replace(/\/+$/, "");
    return path.endsWith("/topics") ? "topics" : "threads";
  });

  let backingThreads = $state([]);

  let filteredBackingThreads = $derived(
    applyTopicListClientFilters(backingThreads, filters),
  );

  let topicDraft = $state({
    title: "",
    summary: "",
    status: "active",
    priority: "p2",
    cadencePreset: "weekly",
    cadenceCron: "",
    tagsInput: "",
  });

  function workspaceHref(pathname = "/") {
    return workspacePath(workspaceSlug, pathname);
  }

  /** @param {string} ref */
  function topicSegmentFromTypedRef(ref) {
    const s = String(ref ?? "").trim();
    if (!s.startsWith("topic:")) return "";
    return s.slice("topic:".length).trim();
  }

  async function loadBackingThreads() {
    loading = true;
    error = "";
    try {
      const query = buildThreadFilterQueryParamsFromThreadListState(filters);
      const response = await coreClient.listThreads(query);
      backingThreads = response.threads ?? [];
    } catch (loadError) {
      const reason =
        loadError instanceof Error ? loadError.message : String(loadError);
      error = `Failed to load threads: ${reason}`;
      backingThreads = [];
    } finally {
      loading = false;
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

  async function loadTopicsFromState(state) {
    loading = true;
    error = "";

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
      topics = [];
    } finally {
      loading = false;
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
      status: "active",
      priority: "p2",
      cadencePreset: "weekly",
      cadenceCron: "",
      tagsInput: "",
    };
  }

  /** Map list UI status to canonical topic.status for POST /topics. */
  function threadStatusToTopicStatus(status) {
    switch (String(status ?? "").trim()) {
      case "paused":
        return "blocked";
      case "closed":
        return "resolved";
      default:
        return "active";
    }
  }

  function buildCreateTopicPayloadFromDraft() {
    const summary = topicDraft.summary.trim() || "No summary provided.";
    return {
      topic: {
        type: "other",
        status: threadStatusToTopicStatus(topicDraft.status),
        title: topicDraft.title.trim(),
        summary,
        owner_refs: [],
        document_refs: [],
        board_refs: [],
        related_refs: [],
        provenance: {
          sources: ["actor_statement:ui"],
        },
      },
    };
  }

  async function createTopic() {
    if (!topicDraft.title.trim()) {
      createError = "Topic title is required.";
      return;
    }
    const cadenceError = validateCadenceSelection({
      preset: topicDraft.cadencePreset,
      customCron: topicDraft.cadenceCron,
    });
    if (cadenceError) {
      createError = cadenceError;
      return;
    }

    creatingTopic = true;
    createError = "";

    try {
      await coreClient.createTopic(buildCreateTopicPayloadFromDraft());

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
    filters.status !== "" ||
      filters.priority !== "" ||
      filters.cadence !== "" ||
      filters.staleness !== "all" ||
      filters.tagInput.trim() !== "" ||
      filters.openOnly ||
      filters.highPriorityTier,
  );

  function statusFilterSelectValue() {
    if (filters.openOnly) return STATUS_OPEN_NOT_CLOSED;
    return filters.status;
  }

  function onStatusFilterChange(value) {
    if (value === STATUS_OPEN_NOT_CLOSED) {
      filters = { ...filters, openOnly: true, status: "" };
    } else {
      filters = { ...filters, openOnly: false, status: value };
    }
  }

  function priorityFilterSelectValue() {
    if (filters.highPriorityTier) return PRIORITY_HIGH_TIER;
    return filters.priority;
  }

  function onPriorityFilterChange(value) {
    if (value === PRIORITY_HIGH_TIER) {
      filters = { ...filters, highPriorityTier: true, priority: "" };
    } else {
      filters = { ...filters, highPriorityTier: false, priority: value };
    }
  }

  function priorityDot(priority) {
    const colors = {
      p0: "bg-danger",
      p1: "bg-warn-text",
      p2: "bg-blue-400",
      p3: "bg-line-strong",
    };
    return colors[priority] ?? "bg-line-strong";
  }

  function statusColor(status) {
    const styles = {
      active: "text-ok-text",
      paused: "text-warn-text",
      closed: "text-slate-300",
      blocked: "text-warn-text",
      resolved: "text-slate-300",
      proposed: "text-[var(--fg-muted)]",
      archived: "text-slate-300",
    };
    return styles[status] ?? "text-fg-subtle";
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
  <div
    class="mb-4 rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
    role="alert"
  >
    {error}
  </div>
{/if}

{#if (listSurface === "topics" || listSurface === "threads") && filtersOpen}
  <CompactFilterBar testId="topics-filter-panel">
    {#snippet children()}
      <div class="grid gap-3 sm:grid-cols-5">
        <label class="text-micro">
          <span class="font-medium text-[var(--fg-muted)]">Status</span>
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
        <label class="text-micro">
          <span class="font-medium text-[var(--fg-muted)]">Priority</span>
          <select
            class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
            onchange={(event) =>
              onPriorityFilterChange(event.currentTarget.value)}
            value={priorityFilterSelectValue()}
          >
            <option value="">All</option>
            <option value={PRIORITY_HIGH_TIER}>High (P0 &amp; P1)</option>
            {#each TOPIC_PRIORITIES as priority}<option value={priority}
                >{TOPIC_PRIORITY_LABELS[priority]}</option
              >{/each}
          </select>
        </label>
        <label class="text-micro">
          <span class="font-medium text-[var(--fg-muted)]">Cadence</span>
          <select
            bind:value={filters.cadence}
            class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
          >
            <option value="">All</option>
            {#each TOPIC_SCHEDULE_PRESETS as cadence}<option value={cadence}
                >{TOPIC_SCHEDULE_PRESET_LABELS[cadence]}</option
              >{/each}
          </select>
        </label>
        <label class="text-micro">
          <span class="font-medium text-[var(--fg-muted)]">Staleness</span>
          <select
            bind:value={filters.staleness}
            class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
          >
            <option value="all">All</option>
            <option value="stale">Stale</option>
            <option value="fresh">Fresh</option>
          </select>
        </label>
        <label class="text-micro">
          <span class="font-medium text-[var(--fg-muted)]">Tags</span>
          <input
            bind:value={filters.tagInput}
            class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
            placeholder="ops, customer"
            type="text"
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
        <span class="font-medium text-[var(--fg-muted)]">Status</span>
        <select
          bind:value={topicDraft.status}
          class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-2 text-meta transition-colors focus:bg-[var(--panel)]"
        >
          {#each TOPIC_STATUSES as status}<option value={status}
              >{status[0].toUpperCase() + status.slice(1)}</option
            >{/each}
        </select>
      </label>
      <label class="text-micro">
        <span class="font-medium text-[var(--fg-muted)]">Priority</span>
        <select
          bind:value={topicDraft.priority}
          class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-2 text-meta transition-colors focus:bg-[var(--panel)]"
        >
          {#each TOPIC_PRIORITIES as priority}<option value={priority}
              >{TOPIC_PRIORITY_LABELS[priority]}</option
            >{/each}
        </select>
      </label>
      <label class="text-micro">
        <span class="font-medium text-[var(--fg-muted)]">Schedule</span>
        <select
          bind:value={topicDraft.cadencePreset}
          class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-2 text-meta transition-colors focus:bg-[var(--panel)]"
        >
          {#each TOPIC_SCHEDULE_PRESETS as cadence}<option value={cadence}
              >{TOPIC_SCHEDULE_PRESET_LABELS[cadence]}</option
            >{/each}
        </select>
      </label>
      {#if topicDraft.cadencePreset === "custom"}
        <label class="text-micro">
          <span class="font-medium text-[var(--fg-muted)]"
            >Cron expression</span
          >
          <input
            bind:value={topicDraft.cadenceCron}
            class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-meta transition-colors focus:bg-[var(--panel)]"
            placeholder="0 9 * * *"
            type="text"
          />
          {#if describeCron(topicDraft.cadenceCron)}
            <span class="mt-1 block text-micro text-[var(--fg-muted)]">
              {describeCron(topicDraft.cadenceCron)}
            </span>
          {/if}
        </label>
      {/if}
      <label class="text-micro">
        <span class="font-medium text-[var(--fg-muted)]">Tags</span>
        <input
          bind:value={topicDraft.tagsInput}
          class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-meta transition-colors focus:bg-[var(--panel)]"
          placeholder="ops, customer"
          type="text"
        />
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
        class="cursor-pointer rounded-md bg-accent px-4 py-2 text-micro font-medium text-white hover:bg-accent-hover disabled:opacity-50"
        disabled={creatingTopic}
        type="submit"
      >
        {creatingTopic ? "Creating..." : "Create topic"}
      </button>
    </div>
  </form>
{/if}

{#if listSurface === "topics"}
  {#if loading}
    <div
      class="mt-12 flex items-center justify-center gap-2 text-meta text-[var(--fg-muted)]"
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
      Loading topics...
    </div>
  {:else if topics.length === 0}
    <div class="mt-8 text-center">
      <p class="text-meta text-[var(--fg-muted)]">
        No topics match the current filters.
      </p>
      {#if hasActiveFilters}
        <button
          class="mt-3 cursor-pointer rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-1.5 text-micro font-medium text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]"
          onclick={resetFilters}
          type="button"
        >
          Clear filters
        </button>
      {/if}
    </div>
  {:else}
    <div
      class="space-y-px overflow-hidden rounded-md border border-[var(--line)] bg-[var(--bg-soft)]"
    >
      {#each topics as topic, i}
        {@const staleness = computeStaleness(topic)}
        <div
          class="flex items-stretch {i > 0
            ? 'border-t border-[var(--line)]'
            : ''}"
        >
          <a
            class="flex min-w-0 flex-1 items-center gap-3 px-3 py-2.5 transition-colors hover:bg-[var(--line-subtle)]"
            href={workspaceHref(`/topics/${encodeURIComponent(topic.id)}`)}
          >
            <span
              class="flex h-2 w-2 shrink-0 rounded-full {priorityDot(
                topic.priority,
              )}"
              title={getPriorityLabel(topic.priority)}
            ></span>
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <p
                  class="truncate text-meta font-medium text-[var(--fg)]"
                >
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
              {#if topic.status && topic.status !== "active"}
                <span class="font-medium capitalize {statusColor(topic.status)}"
                  >{topic.status}</span
                >
              {/if}
              <span
                class="hidden rounded border border-[var(--line)] px-1.5 py-0.5 text-micro text-[var(--fg-muted)] sm:inline"
                >{formatCadenceLabel(topic.cadence, {
                  includeExpression: false,
                })}</span
              >
              {#if (topic.tags ?? []).length > 0}
                <span
                  class="hidden rounded bg-[var(--panel)] px-1.5 py-0.5 text-[var(--fg-muted)] sm:inline"
                  >{topic.tags[0]}{topic.tags.length > 1
                    ? ` +${topic.tags.length - 1}`
                    : ""}</span
                >
              {/if}
              {#if staleness.stale}
                <span
                  class="rounded bg-danger-soft px-1.5 py-0.5 font-medium text-danger-text"
                  >Stale</span
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
{:else if loading}
  <div
    class="mt-12 flex items-center justify-center gap-2 text-meta text-[var(--fg-muted)]"
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
    Loading threads...
  </div>
{:else if backingThreads.length === 0}
  <div class="mt-8 text-center">
    <p class="text-meta text-[var(--fg-muted)]">No threads returned.</p>
  </div>
{:else if filteredBackingThreads.length === 0}
  <div class="mt-8 text-center">
    <p class="text-meta text-[var(--fg-muted)]">
      No threads match the current filters.
    </p>
    {#if hasActiveFilters}
      <button
        class="mt-3 cursor-pointer rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-1.5 text-micro font-medium text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]"
        onclick={resetFilters}
        type="button"
      >
        Clear filters
      </button>
    {/if}
  </div>
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
            {#if thread.status === "archived"}
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
            <a
              class="text-micro font-medium text-accent-text transition-colors hover:text-accent-text"
              href={workspaceHref(`/topics/${encodeURIComponent(topicSeg)}`)}
              >Topic</a
            >
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
