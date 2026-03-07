<script>
  import { onMount } from "svelte";

  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import {
    THREAD_SCHEDULE_PRESETS,
    THREAD_SCHEDULE_PRESET_LABELS,
    THREAD_PRIORITIES,
    THREAD_PRIORITY_LABELS,
    THREAD_STATUSES,
    buildThreadFilterRequestQuery,
    cadenceToRequestValue,
    computeStaleness,
    formatCadenceLabel,
    getPriorityLabel,
    parseTagFilterInput,
    validateCadenceSelection,
  } from "$lib/threadFilters";

  const defaultFilters = {
    status: "",
    priority: "",
    cadence: "",
    staleness: "all",
    tagInput: "",
  };

  let filters = $state({ ...defaultFilters });
  let loading = $state(false);
  let error = $state("");
  let threads = $state([]);
  let createOpen = $state(false);
  let creatingThread = $state(false);
  let createError = $state("");
  let filtersOpen = $state(false);

  let threadDraft = $state({
    title: "",
    summary: "",
    status: "active",
    priority: "p2",
    cadencePreset: "weekly",
    cadenceCron: "",
    tagsInput: "",
  });

  onMount(async () => {
    await loadThreads();
  });

  async function loadThreads() {
    loading = true;
    error = "";

    try {
      const query = buildThreadFilterRequestQuery({
        status: filters.status,
        priority: filters.priority,
        cadence: filters.cadence,
        staleness: filters.staleness,
        tags: parseTagFilterInput(filters.tagInput),
      });

      const response = await coreClient.listThreads(query);
      threads = response.threads ?? [];
    } catch (loadError) {
      const reason =
        loadError instanceof Error ? loadError.message : String(loadError);
      error = `Failed to load threads: ${reason}`;
      threads = [];
    } finally {
      loading = false;
    }
  }

  async function applyFilters() {
    await loadThreads();
  }

  async function resetFilters() {
    filters = { ...defaultFilters };
    await loadThreads();
  }

  function resetThreadDraft() {
    threadDraft = {
      title: "",
      summary: "",
      status: "active",
      priority: "p2",
      cadencePreset: "weekly",
      cadenceCron: "",
      tagsInput: "",
    };
  }

  async function createThread() {
    if (!threadDraft.title.trim()) {
      createError = "Thread title is required.";
      return;
    }
    const cadenceError = validateCadenceSelection({
      preset: threadDraft.cadencePreset,
      customCron: threadDraft.cadenceCron,
    });
    if (cadenceError) {
      createError = cadenceError;
      return;
    }

    creatingThread = true;
    createError = "";

    try {
      const cadence = cadenceToRequestValue({
        preset: threadDraft.cadencePreset,
        customCron: threadDraft.cadenceCron,
      });
      await coreClient.createThread({
        thread: {
          title: threadDraft.title.trim(),
          type: "case",
          status: threadDraft.status,
          priority: threadDraft.priority,
          tags: parseTagFilterInput(threadDraft.tagsInput),
          cadence,
          current_summary: threadDraft.summary.trim() || "No summary provided.",
          next_actions: [
            threadDraft.summary.trim() || "Review and define next steps.",
          ],
          key_artifacts: [],
          provenance: {
            sources: ["actor_statement:ui"],
          },
        },
      });

      createOpen = false;
      resetThreadDraft();
      await loadThreads();
    } catch (submitError) {
      const reason =
        submitError instanceof Error
          ? submitError.message
          : String(submitError);
      createError = `Failed to create thread: ${reason}`;
    } finally {
      creatingThread = false;
    }
  }

  function priorityDot(priority) {
    const colors = {
      p0: "bg-red-500/100",
      p1: "bg-amber-400",
      p2: "bg-blue-400",
      p3: "bg-gray-300",
    };
    return colors[priority] ?? "bg-gray-300";
  }

  function statusColor(status) {
    const styles = {
      active: "text-emerald-400",
      paused: "text-amber-600",
      closed: "text-gray-400",
    };
    return styles[status] ?? "text-gray-400";
  }
</script>

<div class="flex items-center justify-between mb-4">
  <h1 class="text-lg font-semibold text-gray-900">Threads</h1>
  <div class="flex items-center gap-1.5">
    <button
      class="inline-flex items-center gap-1.5 rounded-md border border-gray-200 bg-gray-100 px-2.5 py-1.5 text-[12px] font-medium text-gray-600 transition-colors hover:bg-gray-200"
      onclick={() => (filtersOpen = !filtersOpen)}
      type="button"
    >
      <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
        <path stroke-linecap="round" stroke-linejoin="round" d="M3 4a1 1 0 011-1h16a1 1 0 011 1v2.586a1 1 0 01-.293.707l-6.414 6.414a1 1 0 00-.293.707V17l-4 4v-6.586a1 1 0 00-.293-.707L3.293 7.293A1 1 0 013 6.586V4z" />
      </svg>
      Filters
    </button>
    <button
      class="inline-flex items-center gap-1.5 rounded-md bg-gray-200 px-3 py-1.5 text-[12px] font-medium text-gray-900 transition-colors hover:bg-gray-300"
      onclick={() => (createOpen = !createOpen)}
      type="button"
    >
      {#if !createOpen}
        <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M12 4v16m8-8H4" />
        </svg>
      {/if}
      {createOpen ? "Cancel" : "New thread"}
    </button>
  </div>
</div>

{#if error}
  <div class="mb-4 rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
    {error}
  </div>
{/if}

{#if filtersOpen}
  <div class="mb-4 rounded-md border border-gray-200 bg-gray-100 p-3">
    <div class="grid gap-3 sm:grid-cols-5">
      <label class="text-[12px]">
        <span class="font-medium text-gray-600">Status</span>
        <select
          bind:value={filters.status}
          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-1.5 text-[13px] transition-colors focus:bg-gray-100"
        >
          <option value="">All</option>
          {#each THREAD_STATUSES as status}<option value={status}>{status}</option>{/each}
        </select>
      </label>
      <label class="text-[12px]">
        <span class="font-medium text-gray-600">Priority</span>
        <select
          bind:value={filters.priority}
          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-1.5 text-[13px] transition-colors focus:bg-gray-100"
        >
          <option value="">All</option>
          {#each THREAD_PRIORITIES as priority}<option value={priority}>{THREAD_PRIORITY_LABELS[priority]}</option>{/each}
        </select>
      </label>
      <label class="text-[12px]">
        <span class="font-medium text-gray-600">Cadence</span>
        <select
          bind:value={filters.cadence}
          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-1.5 text-[13px] transition-colors focus:bg-gray-100"
        >
          <option value="">All</option>
          {#each THREAD_SCHEDULE_PRESETS as cadence}<option value={cadence}>{THREAD_SCHEDULE_PRESET_LABELS[cadence]}</option>{/each}
        </select>
      </label>
      <label class="text-[12px]">
        <span class="font-medium text-gray-600">Staleness</span>
        <select
          bind:value={filters.staleness}
          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-1.5 text-[13px] transition-colors focus:bg-gray-100"
        >
          <option value="all">All</option>
          <option value="stale">Stale</option>
          <option value="fresh">Fresh</option>
        </select>
      </label>
      <label class="text-[12px]">
        <span class="font-medium text-gray-600">Tags</span>
        <input
          bind:value={filters.tagInput}
          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-1.5 text-[13px] transition-colors focus:bg-gray-100"
          placeholder="ops, customer"
          type="text"
        />
      </label>
    </div>
    <div class="mt-3 flex gap-1.5">
      <button
        class="rounded-md bg-gray-200 px-3 py-1.5 text-[12px] font-medium text-gray-900 hover:bg-gray-300"
        onclick={applyFilters}
        type="button">Apply</button
      >
      <button
        class="rounded-md border border-gray-200 bg-gray-100 px-3 py-1.5 text-[12px] font-medium text-gray-600 hover:bg-gray-200"
        onclick={resetFilters}
        type="button">Reset</button
      >
    </div>
  </div>
{/if}

{#if createOpen}
  <form
    class="mb-4 rounded-md border border-gray-200 bg-gray-100 p-4"
    onsubmit={(event) => { event.preventDefault(); createThread(); }}
  >
    {#if createError}
      <div class="mb-3 rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">{createError}</div>
    {/if}
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="text-[12px] sm:col-span-2">
        <span class="font-medium text-gray-600">Title</span>
        <input
          bind:value={threadDraft.title}
          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-[13px] transition-colors focus:bg-gray-100"
          placeholder="Thread title..."
          required
          type="text"
        />
      </label>
      <label class="text-[12px]">
        <span class="font-medium text-gray-600">Status</span>
        <select
          bind:value={threadDraft.status}
          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-2 text-[13px] transition-colors focus:bg-gray-100"
        >
          {#each THREAD_STATUSES as status}<option value={status}>{status}</option>{/each}
        </select>
      </label>
      <label class="text-[12px]">
        <span class="font-medium text-gray-600">Priority</span>
        <select
          bind:value={threadDraft.priority}
          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-2 text-[13px] transition-colors focus:bg-gray-100"
        >
          {#each THREAD_PRIORITIES as priority}<option value={priority}>{THREAD_PRIORITY_LABELS[priority]}</option>{/each}
        </select>
      </label>
      <label class="text-[12px]">
        <span class="font-medium text-gray-600">Schedule</span>
        <select
          bind:value={threadDraft.cadencePreset}
          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-2 text-[13px] transition-colors focus:bg-gray-100"
        >
          {#each THREAD_SCHEDULE_PRESETS as cadence}<option value={cadence}>{THREAD_SCHEDULE_PRESET_LABELS[cadence]}</option>{/each}
        </select>
      </label>
      {#if threadDraft.cadencePreset === "custom"}
        <label class="text-[12px]">
          <span class="font-medium text-gray-600">Cron expression</span>
          <input
            bind:value={threadDraft.cadenceCron}
            class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-[13px] transition-colors focus:bg-gray-100"
            placeholder="0 9 * * *"
            type="text"
          />
        </label>
      {/if}
      <label class="text-[12px]">
        <span class="font-medium text-gray-600">Tags</span>
        <input
          bind:value={threadDraft.tagsInput}
          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-[13px] transition-colors focus:bg-gray-100"
          placeholder="ops, customer"
          type="text"
        />
      </label>
      <label class="text-[12px] sm:col-span-2">
        <span class="font-medium text-gray-600">Summary</span>
        <textarea
          bind:value={threadDraft.summary}
          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-[13px] transition-colors focus:bg-gray-100"
          placeholder="Brief description..."
          rows="2"
        ></textarea>
      </label>
    </div>
    <div class="mt-3 flex justify-end">
      <button
        class="rounded-md bg-gray-200 px-4 py-2 text-[12px] font-medium text-gray-900 hover:bg-gray-300 disabled:opacity-50"
        disabled={creatingThread}
        type="submit"
      >
        {creatingThread ? "Creating..." : "Create thread"}
      </button>
    </div>
  </form>
{/if}

{#if loading}
  <div class="mt-12 flex items-center justify-center gap-2 text-[13px] text-gray-400">
    <svg class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
      <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
      <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
    </svg>
    Loading threads...
  </div>
{:else if threads.length === 0}
  <div class="mt-8 text-center">
    <p class="text-[13px] text-gray-400">No threads match the current filters.</p>
  </div>
{:else}
  <div class="space-y-px rounded-md border border-gray-200 bg-gray-100 overflow-hidden">
    {#each threads as thread, i}
      {@const staleness = computeStaleness(thread)}
      <a
        class="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-gray-200 {i > 0 ? 'border-t border-gray-200' : ''}"
        href={`/threads/${thread.id}`}
      >
        <span
          class="flex h-2 w-2 shrink-0 rounded-full {priorityDot(thread.priority)}"
          title={getPriorityLabel(thread.priority)}
        ></span>
        <div class="min-w-0 flex-1">
          <p class="truncate text-[13px] font-medium text-gray-900">{thread.title}</p>
          <p class="truncate text-[12px] text-gray-400">{thread.current_summary}</p>
        </div>
        <div class="flex shrink-0 items-center gap-1.5 text-[11px]">
          <span class="font-medium capitalize {statusColor(thread.status)}">{thread.status}</span>
          <span class="hidden text-gray-400 sm:inline">{formatCadenceLabel(thread.cadence, { includeExpression: false })}</span>
          {#if (thread.tags ?? []).length > 0}
            <span class="hidden rounded bg-gray-200 px-1.5 py-0.5 text-gray-500 sm:inline">{thread.tags[0]}{thread.tags.length > 1 ? ` +${thread.tags.length - 1}` : ""}</span>
          {/if}
          {#if staleness.stale}
            <span class="rounded bg-red-500/10 px-1.5 py-0.5 font-medium text-red-400">Stale</span>
          {/if}
          <span class="w-14 text-right text-gray-300">{formatTimestamp(thread.updated_at) || "—"}</span>
        </div>
      </a>
    {/each}
  </div>
{/if}
