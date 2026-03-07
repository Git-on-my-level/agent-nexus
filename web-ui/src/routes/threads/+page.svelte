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

  function statusBadge(status) {
    const styles = {
      active: "bg-emerald-50 text-emerald-700",
      paused: "bg-amber-50 text-amber-700",
      closed: "bg-gray-100 text-gray-500",
    };
    return styles[status] ?? "bg-gray-100 text-gray-500";
  }
</script>

<div class="flex items-center justify-between mb-6">
  <h1 class="text-2xl font-semibold text-slate-900 tracking-tight">Threads</h1>
  <div class="flex items-center gap-2">
    <button
      class="inline-flex items-center gap-2 rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-medium text-slate-700 transition-all hover:bg-slate-50 hover:border-slate-300"
      onclick={() => (filtersOpen = !filtersOpen)}
      type="button"
    >
      <svg
        class="h-4 w-4"
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
      {filtersOpen ? "Hide filters" : "Filters"}
    </button>
    <button
      class="inline-flex items-center gap-2 rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white transition-all hover:bg-slate-800"
      onclick={() => (createOpen = !createOpen)}
      type="button"
    >
      {#if !createOpen}
        <svg
          class="h-4 w-4"
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
      {createOpen ? "Cancel" : "New thread"}
    </button>
  </div>
</div>

{#if error}
  <div
    class="mb-6 flex items-start gap-3 rounded-lg bg-red-50 px-4 py-4 text-sm text-red-700"
  >
    <svg
      class="mt-0.5 h-5 w-5 shrink-0 text-red-400"
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      stroke-width="2"
    >
      <path
        stroke-linecap="round"
        stroke-linejoin="round"
        d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.082 16.5c-.77.833.192 2.5 1.732 2.5z"
      />
    </svg>
    {error}
  </div>
{/if}

{#if filtersOpen}
  <div class="mb-6 rounded-xl border border-slate-200 bg-white p-5 shadow-sm">
    <div class="grid gap-4 sm:grid-cols-5">
      <label class="text-sm">
        <span class="font-medium text-slate-700">Status</span>
        <select
          bind:value={filters.status}
          class="mt-2 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm transition-all focus:bg-white focus:border-slate-300"
        >
          <option value="">All</option>
          {#each THREAD_STATUSES as status}<option value={status}
              >{status}</option
            >{/each}
        </select>
      </label>
      <label class="text-sm">
        <span class="font-medium text-slate-700">Priority</span>
        <select
          bind:value={filters.priority}
          class="mt-2 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm transition-all focus:bg-white focus:border-slate-300"
        >
          <option value="">All</option>
          {#each THREAD_PRIORITIES as priority}<option value={priority}
              >{THREAD_PRIORITY_LABELS[priority]}</option
            >{/each}
        </select>
      </label>
      <label class="text-sm">
        <span class="font-medium text-slate-700">Cadence</span>
        <select
          bind:value={filters.cadence}
          class="mt-2 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm transition-all focus:bg-white focus:border-slate-300"
        >
          <option value="">All</option>
          {#each THREAD_SCHEDULE_PRESETS as cadence}<option value={cadence}
              >{THREAD_SCHEDULE_PRESET_LABELS[cadence]}</option
            >{/each}
        </select>
      </label>
      <label class="text-sm">
        <span class="font-medium text-slate-700">Staleness</span>
        <select
          bind:value={filters.staleness}
          class="mt-2 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm transition-all focus:bg-white focus:border-slate-300"
        >
          <option value="all">All</option>
          <option value="stale">Stale</option>
          <option value="fresh">Fresh</option>
        </select>
      </label>
      <label class="text-sm">
        <span class="font-medium text-slate-700">Tags</span>
        <input
          bind:value={filters.tagInput}
          class="mt-2 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm transition-all focus:bg-white focus:border-slate-300"
          placeholder="ops, customer"
          type="text"
        />
      </label>
    </div>
    <div class="mt-4 flex gap-2">
      <button
        class="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white transition-all hover:bg-slate-800"
        onclick={applyFilters}
        type="button">Apply</button
      >
      <button
        class="rounded-lg border border-slate-200 bg-white px-4 py-2 text-sm font-medium text-slate-700 transition-all hover:bg-slate-50"
        onclick={resetFilters}
        type="button">Reset</button
      >
    </div>
  </div>
{/if}

{#if createOpen}
  <form
    class="mb-6 rounded-xl border border-slate-200 bg-white p-6 shadow-sm"
    onsubmit={(event) => {
      event.preventDefault();
      createThread();
    }}
  >
    {#if createError}
      <div class="mb-5 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700">
        {createError}
      </div>
    {/if}
    <div class="grid gap-5 sm:grid-cols-2">
      <label class="text-sm sm:col-span-2">
        <span class="font-medium text-slate-700">Title</span>
        <input
          bind:value={threadDraft.title}
          class="mt-2 w-full rounded-lg border border-slate-200 bg-slate-50 px-4 py-2.5 text-sm transition-all focus:bg-white focus:border-slate-300"
          placeholder="Thread title..."
          required
          type="text"
        />
      </label>
      <label class="text-sm">
        <span class="font-medium text-slate-700">Status</span>
        <select
          bind:value={threadDraft.status}
          class="mt-2 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 py-2.5 text-sm transition-all focus:bg-white focus:border-slate-300"
        >
          {#each THREAD_STATUSES as status}<option value={status}
              >{status}</option
            >{/each}
        </select>
      </label>
      <label class="text-sm">
        <span class="font-medium text-slate-700">Priority</span>
        <select
          bind:value={threadDraft.priority}
          class="mt-2 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 py-2.5 text-sm transition-all focus:bg-white focus:border-slate-300"
        >
          {#each THREAD_PRIORITIES as priority}<option value={priority}
              >{THREAD_PRIORITY_LABELS[priority]}</option
            >{/each}
        </select>
      </label>
      <label class="text-sm">
        <span class="font-medium text-slate-700">Schedule</span>
        <select
          bind:value={threadDraft.cadencePreset}
          class="mt-2 w-full rounded-lg border border-slate-200 bg-slate-50 px-3 py-2.5 text-sm transition-all focus:bg-white focus:border-slate-300"
        >
          {#each THREAD_SCHEDULE_PRESETS as cadence}<option value={cadence}
              >{THREAD_SCHEDULE_PRESET_LABELS[cadence]}</option
            >{/each}
        </select>
      </label>
      {#if threadDraft.cadencePreset === "custom"}
        <label class="text-sm">
          <span class="font-medium text-slate-700">Cron expression</span>
          <input
            bind:value={threadDraft.cadenceCron}
            class="mt-2 w-full rounded-lg border border-slate-200 bg-slate-50 px-4 py-2.5 text-sm transition-all focus:bg-white focus:border-slate-300"
            placeholder="0 9 * * *"
            type="text"
          />
        </label>
      {/if}
      <label class="text-sm">
        <span class="font-medium text-slate-700">Tags</span>
        <input
          bind:value={threadDraft.tagsInput}
          class="mt-2 w-full rounded-lg border border-slate-200 bg-slate-50 px-4 py-2.5 text-sm transition-all focus:bg-white focus:border-slate-300"
          placeholder="ops, customer"
          type="text"
        />
      </label>
      <label class="text-sm sm:col-span-2">
        <span class="font-medium text-slate-700">Summary</span>
        <textarea
          bind:value={threadDraft.summary}
          class="mt-2 w-full rounded-lg border border-slate-200 bg-slate-50 px-4 py-2.5 text-sm transition-all focus:bg-white focus:border-slate-300"
          placeholder="Brief description..."
          rows="3"
        ></textarea>
      </label>
    </div>
    <div class="mt-5 flex justify-end">
      <button
        class="rounded-lg bg-slate-900 px-5 py-2.5 text-sm font-medium text-white transition-all hover:bg-slate-800 disabled:opacity-50"
        disabled={creatingThread}
        type="submit"
      >
        {creatingThread ? "Creating..." : "Create thread"}
      </button>
    </div>
  </form>
{/if}

{#if loading}
  <div
    class="mt-16 flex items-center justify-center gap-3 text-sm text-slate-400"
  >
    <svg class="h-5 w-5 animate-spin" fill="none" viewBox="0 0 24 24">
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
{:else if threads.length === 0}
  <div class="mt-12 text-center">
    <svg
      class="mx-auto h-12 w-12 text-slate-300"
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      stroke-width="1.5"
    >
      <path
        stroke-linecap="round"
        stroke-linejoin="round"
        d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z"
      />
    </svg>
    <p class="mt-3 text-sm text-slate-400">
      No threads match the current filters.
    </p>
  </div>
{:else}
  <div class="space-y-2">
    {#each threads as thread}
      {@const staleness = computeStaleness(thread)}
      <a
        class="flex items-center gap-4 rounded-lg border border-slate-200 bg-white px-5 py-4 shadow-sm transition-all hover:border-slate-300 hover:shadow"
        href={`/threads/${thread.id}`}
      >
        <span
          class="flex h-2.5 w-2.5 shrink-0 rounded-full {priorityDot(
            thread.priority,
          )}"
          title={getPriorityLabel(thread.priority)}
        ></span>
        <div class="min-w-0 flex-1">
          <p class="truncate text-sm font-medium text-slate-900">
            {thread.title}
          </p>
          <p class="mt-1 truncate text-sm text-slate-500">
            {thread.current_summary}
          </p>
        </div>
        <div class="flex shrink-0 items-center gap-2 text-xs">
          <span
            class="rounded-md px-2.5 py-1 font-medium capitalize {statusBadge(
              thread.status,
            )}">{thread.status}</span
          >
          <span
            class="hidden rounded-md bg-slate-50 px-2.5 py-1 text-slate-500 sm:inline"
            >{formatCadenceLabel(thread.cadence, {
              includeExpression: false,
            })}</span
          >
          {#if (thread.tags ?? []).length > 0}
            <span
              class="hidden rounded-md bg-slate-50 px-2.5 py-1 text-slate-500 sm:inline"
              >{thread.tags[0]}{thread.tags.length > 1
                ? ` +${thread.tags.length - 1}`
                : ""}</span
            >
          {/if}
          {#if staleness.stale}
            <span
              class="rounded-md bg-red-50 px-2.5 py-1 font-medium text-red-600"
              >Stale</span
            >
          {/if}
          <span class="w-16 text-right text-slate-400"
            >{formatTimestamp(thread.updated_at) || "—"}</span
          >
        </div>
      </a>
    {/each}
  </div>
{/if}
