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
      p0: "bg-red-500",
      p1: "bg-orange-400",
      p2: "bg-blue-400",
      p3: "bg-gray-300",
    };
    return colors[priority] ?? "bg-gray-300";
  }

  function statusColor(status) {
    const colors = {
      active: "text-emerald-600",
      paused: "text-amber-600",
      closed: "text-gray-400",
    };
    return colors[status] ?? "text-gray-500";
  }
</script>

<div class="flex items-center justify-between">
  <h1 class="text-lg font-semibold text-gray-900">Threads</h1>
  <div class="flex items-center gap-2">
    <button
      class="rounded-md px-3 py-1.5 text-xs font-medium text-gray-600 transition-colors hover:bg-gray-100"
      onclick={() => (filtersOpen = !filtersOpen)}
      type="button"
    >
      {filtersOpen ? "Hide filters" : "Filters"}
    </button>
    <button
      class="rounded-md bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-indigo-500"
      onclick={() => (createOpen = !createOpen)}
      type="button"
    >
      {createOpen ? "Cancel" : "New thread"}
    </button>
  </div>
</div>

{#if error}
  <p class="mt-3 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">
    {error}
  </p>
{/if}

{#if filtersOpen}
  <div class="mt-3 rounded-lg border border-gray-200 bg-white p-3">
    <div class="grid gap-2 sm:grid-cols-5">
      <label class="text-xs text-gray-500">
        Status
        <select
          bind:value={filters.status}
          class="mt-1 w-full rounded border border-gray-200 px-2 py-1 text-sm"
        >
          <option value="">All</option>
          {#each THREAD_STATUSES as status}<option value={status}
              >{status}</option
            >{/each}
        </select>
      </label>
      <label class="text-xs text-gray-500">
        Priority
        <select
          bind:value={filters.priority}
          class="mt-1 w-full rounded border border-gray-200 px-2 py-1 text-sm"
        >
          <option value="">All</option>
          {#each THREAD_PRIORITIES as priority}<option value={priority}
              >{THREAD_PRIORITY_LABELS[priority]}</option
            >{/each}
        </select>
      </label>
      <label class="text-xs text-gray-500">
        Cadence
        <select
          bind:value={filters.cadence}
          class="mt-1 w-full rounded border border-gray-200 px-2 py-1 text-sm"
        >
          <option value="">All</option>
          {#each THREAD_SCHEDULE_PRESETS as cadence}<option value={cadence}
              >{THREAD_SCHEDULE_PRESET_LABELS[cadence]}</option
            >{/each}
        </select>
      </label>
      <label class="text-xs text-gray-500">
        Staleness
        <select
          bind:value={filters.staleness}
          class="mt-1 w-full rounded border border-gray-200 px-2 py-1 text-sm"
        >
          <option value="all">All</option>
          <option value="stale">Stale</option>
          <option value="fresh">Fresh</option>
        </select>
      </label>
      <label class="text-xs text-gray-500">
        Tags (match all)
        <input
          bind:value={filters.tagInput}
          class="mt-1 w-full rounded border border-gray-200 px-2 py-1 text-sm"
          placeholder="ops, customer"
          type="text"
        />
        <span class="mt-1 block text-[11px] text-gray-400">
          Comma-separated; a thread must contain every tag.
        </span>
      </label>
    </div>
    <div class="mt-2 flex gap-2">
      <button
        class="rounded bg-gray-900 px-3 py-1 text-xs font-medium text-white hover:bg-gray-700"
        onclick={applyFilters}
        type="button">Apply</button
      >
      <button
        class="rounded px-3 py-1 text-xs text-gray-500 hover:bg-gray-100"
        onclick={resetFilters}
        type="button">Reset</button
      >
    </div>
  </div>
{/if}

{#if createOpen}
  <form
    class="mt-3 rounded-lg border border-gray-200 bg-white p-4"
    onsubmit={(event) => {
      event.preventDefault();
      createThread();
    }}
  >
    {#if createError}
      <p class="mb-3 rounded bg-red-50 px-3 py-1.5 text-xs text-red-700">
        {createError}
      </p>
    {/if}
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="text-xs font-medium text-gray-600 sm:col-span-2">
        Title
        <input
          bind:value={threadDraft.title}
          class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
          required
          type="text"
        />
      </label>
      <label class="text-xs font-medium text-gray-600">
        Status
        <select
          bind:value={threadDraft.status}
          class="mt-1 w-full rounded border border-gray-200 px-2 py-1.5 text-sm"
        >
          {#each THREAD_STATUSES as status}<option value={status}
              >{status}</option
            >{/each}
        </select>
      </label>
      <label class="text-xs font-medium text-gray-600">
        Priority
        <select
          bind:value={threadDraft.priority}
          class="mt-1 w-full rounded border border-gray-200 px-2 py-1.5 text-sm"
        >
          {#each THREAD_PRIORITIES as priority}<option value={priority}
              >{THREAD_PRIORITY_LABELS[priority]}</option
            >{/each}
        </select>
      </label>
      <label class="text-xs font-medium text-gray-600">
        Schedule
        <select
          bind:value={threadDraft.cadencePreset}
          class="mt-1 w-full rounded border border-gray-200 px-2 py-1.5 text-sm"
        >
          {#each THREAD_SCHEDULE_PRESETS as cadence}<option value={cadence}
              >{THREAD_SCHEDULE_PRESET_LABELS[cadence]}</option
            >{/each}
        </select>
      </label>
      {#if threadDraft.cadencePreset === "custom"}
        <label class="text-xs font-medium text-gray-600">
          Cron expression
          <input
            bind:value={threadDraft.cadenceCron}
            class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
            placeholder="0 9 * * *"
            type="text"
          />
          <span class="mt-1 block text-[11px] text-gray-400">
            Use five cron fields in server timezone.
          </span>
        </label>
      {/if}
      <label class="text-xs font-medium text-gray-600">
        Tags
        <input
          bind:value={threadDraft.tagsInput}
          class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
          placeholder="ops, customer"
          type="text"
        />
      </label>
      <label class="text-xs font-medium text-gray-600 sm:col-span-2">
        Summary
        <textarea
          bind:value={threadDraft.summary}
          class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
          rows="2"
        ></textarea>
      </label>
    </div>
    <button
      class="mt-3 rounded-md bg-indigo-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
      disabled={creatingThread}
      type="submit"
    >
      {creatingThread ? "Creating..." : "Create thread"}
    </button>
  </form>
{/if}

{#if loading}
  <p class="mt-6 text-sm text-gray-400">Loading threads...</p>
{:else if threads.length === 0}
  <p class="mt-6 text-sm text-gray-400">
    No threads match the current filters.
  </p>
{:else}
  <div class="mt-4 overflow-hidden rounded-lg border border-gray-200 bg-white">
    {#each threads as thread, i}
      {@const staleness = computeStaleness(thread)}
      <a
        class="flex items-center gap-3 border-b border-gray-100 px-4 py-3 transition-colors hover:bg-gray-50 {i ===
        threads.length - 1
          ? 'border-b-0'
          : ''}"
        href={`/threads/${thread.id}`}
      >
        <span
          class="flex h-2 w-2 shrink-0 rounded-full {priorityDot(
            thread.priority,
          )}"
          title={getPriorityLabel(thread.priority)}
        ></span>
        <div class="min-w-0 flex-1">
          <p class="truncate text-sm font-medium text-gray-900">
            {thread.title}
          </p>
          <p class="mt-0.5 truncate text-xs text-gray-500">
            {thread.current_summary}
          </p>
        </div>
        <div class="flex shrink-0 items-center gap-3 text-xs">
          <span class="capitalize {statusColor(thread.status)}"
            >{thread.status}</span
          >
          <span
            class="hidden rounded bg-gray-100 px-1.5 py-0.5 text-gray-500 sm:inline"
            >{formatCadenceLabel(thread.cadence, {
              includeExpression: false,
            })}</span
          >
          {#if (thread.tags ?? []).length > 0}
            <span
              class="hidden rounded bg-gray-100 px-1.5 py-0.5 text-gray-500 sm:inline"
              >{thread.tags[0]}{thread.tags.length > 1
                ? ` +${thread.tags.length - 1}`
                : ""}</span
            >
          {/if}
          {#if staleness.stale}
            <span class="rounded bg-red-50 px-1.5 py-0.5 text-red-600"
              >Stale</span
            >
          {/if}
          <span class="w-14 text-right text-gray-400"
            >{formatTimestamp(thread.updated_at) || "—"}</span
          >
        </div>
      </a>
    {/each}
  </div>
{/if}
