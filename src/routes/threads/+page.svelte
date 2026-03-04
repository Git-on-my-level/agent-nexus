<script>
  import { onMount } from "svelte";

  import ProvenanceBadge from "$lib/components/ProvenanceBadge.svelte";
  import UnknownObjectPanel from "$lib/components/UnknownObjectPanel.svelte";
  import { coreClient } from "$lib/coreClient";
  import {
    THREAD_CADENCES,
    THREAD_PRIORITIES,
    THREAD_STATUSES,
    buildThreadFilterRequestQuery,
    computeStaleness,
    parseTagFilterInput,
  } from "$lib/threadFilters";

  const defaultFilters = {
    status: "",
    priority: "",
    cadence: "",
    staleness: "all",
    tagInput: "",
  };

  let filters = { ...defaultFilters };
  let loading = false;
  let error = "";
  let threads = [];
  let createOpen = false;
  let creatingThread = false;
  let createError = "";

  let threadDraft = {
    title: "",
    summary: "",
    status: "active",
    priority: "p2",
    cadence: "weekly",
    tagsInput: "",
  };

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
      cadence: "weekly",
      tagsInput: "",
    };
  }

  async function createThread() {
    if (!threadDraft.title.trim()) {
      createError = "Thread title is required.";
      return;
    }

    creatingThread = true;
    createError = "";

    try {
      await coreClient.createThread({
        thread: {
          title: threadDraft.title.trim(),
          type: "case",
          status: threadDraft.status,
          priority: threadDraft.priority,
          tags: parseTagFilterInput(threadDraft.tagsInput),
          cadence: threadDraft.cadence,
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
</script>

<h1 class="text-2xl font-semibold">Threads</h1>
<p class="mt-2 max-w-3xl text-sm text-slate-700">
  Thread list supports API-backed filtering and creation. Click a title to open
  thread detail.
</p>

{#if error}
  <p
    class="mt-4 rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-800"
  >
    {error}
  </p>
{/if}

<section class="mt-6 rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
  <div class="grid gap-3 md:grid-cols-5">
    <label class="text-xs font-semibold uppercase tracking-wide text-slate-600">
      Status
      <select
        bind:value={filters.status}
        class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
      >
        <option value="">All</option>
        {#each THREAD_STATUSES as status}
          <option value={status}>{status}</option>
        {/each}
      </select>
    </label>

    <label class="text-xs font-semibold uppercase tracking-wide text-slate-600">
      Priority
      <select
        bind:value={filters.priority}
        class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
      >
        <option value="">All</option>
        {#each THREAD_PRIORITIES as priority}
          <option value={priority}>{priority}</option>
        {/each}
      </select>
    </label>

    <label class="text-xs font-semibold uppercase tracking-wide text-slate-600">
      Cadence
      <select
        bind:value={filters.cadence}
        class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
      >
        <option value="">All</option>
        {#each THREAD_CADENCES as cadence}
          <option value={cadence}>{cadence}</option>
        {/each}
      </select>
    </label>

    <label class="text-xs font-semibold uppercase tracking-wide text-slate-600">
      Staleness
      <select
        bind:value={filters.staleness}
        class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
      >
        <option value="all">All</option>
        <option value="stale">Stale only</option>
        <option value="fresh">Fresh only</option>
      </select>
    </label>

    <label class="text-xs font-semibold uppercase tracking-wide text-slate-600">
      Tags (comma-separated)
      <input
        bind:value={filters.tagInput}
        class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
        placeholder="ops,customer"
        type="text"
      />
    </label>
  </div>

  <div class="mt-3 flex flex-wrap gap-2">
    <button
      class="rounded-md bg-slate-900 px-3 py-1.5 text-xs font-semibold text-white hover:bg-slate-700"
      on:click={applyFilters}
      type="button"
    >
      Apply filters
    </button>
    <button
      class="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-100"
      on:click={resetFilters}
      type="button"
    >
      Reset
    </button>
    <button
      class="rounded-md bg-emerald-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-emerald-500"
      on:click={() => (createOpen = !createOpen)}
      type="button"
    >
      {createOpen ? "Close new thread form" : "Create thread"}
    </button>
  </div>

  {#if createOpen}
    <form
      class="mt-4 rounded-md border border-slate-200 bg-slate-50 p-3"
      on:submit|preventDefault={createThread}
    >
      {#if createError}
        <p class="mb-2 rounded-md bg-rose-50 px-2 py-1 text-xs text-rose-800">
          {createError}
        </p>
      {/if}

      <div class="grid gap-3 md:grid-cols-2">
        <label
          class="text-xs font-semibold uppercase tracking-wide text-slate-600"
        >
          Title
          <input
            bind:value={threadDraft.title}
            class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
            required
            type="text"
          />
        </label>

        <label
          class="text-xs font-semibold uppercase tracking-wide text-slate-600"
        >
          Tags
          <input
            bind:value={threadDraft.tagsInput}
            class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
            placeholder="ops,customer"
            type="text"
          />
        </label>

        <label
          class="text-xs font-semibold uppercase tracking-wide text-slate-600"
        >
          Status
          <select
            bind:value={threadDraft.status}
            class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
          >
            {#each THREAD_STATUSES as status}
              <option value={status}>{status}</option>
            {/each}
          </select>
        </label>

        <label
          class="text-xs font-semibold uppercase tracking-wide text-slate-600"
        >
          Priority
          <select
            bind:value={threadDraft.priority}
            class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
          >
            {#each THREAD_PRIORITIES as priority}
              <option value={priority}>{priority}</option>
            {/each}
          </select>
        </label>

        <label
          class="text-xs font-semibold uppercase tracking-wide text-slate-600 md:col-span-2"
        >
          Cadence
          <select
            bind:value={threadDraft.cadence}
            class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
          >
            {#each THREAD_CADENCES as cadence}
              <option value={cadence}>{cadence}</option>
            {/each}
          </select>
        </label>

        <label
          class="text-xs font-semibold uppercase tracking-wide text-slate-600 md:col-span-2"
        >
          Summary
          <textarea
            bind:value={threadDraft.summary}
            class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
            rows="3"
          ></textarea>
        </label>
      </div>

      <button
        class="mt-3 rounded-md bg-emerald-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-60"
        disabled={creatingThread}
        type="submit"
      >
        {creatingThread ? "Creating..." : "Submit thread"}
      </button>
    </form>
  {/if}
</section>

{#if loading}
  <p
    class="mt-4 rounded-md bg-white px-3 py-3 text-sm text-slate-700 shadow-sm"
  >
    Loading threads...
  </p>
{:else if threads.length === 0}
  <p
    class="mt-4 rounded-md bg-white px-3 py-3 text-sm text-slate-700 shadow-sm"
  >
    No threads matched current filters.
  </p>
{:else}
  <ul class="mt-6 space-y-4">
    {#each threads as thread}
      {@const staleness = computeStaleness(thread)}
      <li class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <a
              class="text-lg font-semibold text-slate-900 underline decoration-slate-300 underline-offset-2 hover:text-slate-700"
              href={`/threads/${thread.id}`}
            >
              {thread.title}
            </a>
            <p class="mt-1 text-xs uppercase tracking-wide text-slate-500">
              {thread.status} • {thread.priority} • {thread.cadence}
            </p>
          </div>
          <span
            class={`rounded px-2 py-1 text-xs font-semibold ${staleness.className}`}
          >
            {staleness.label}
          </span>
        </div>

        <p class="mt-3 text-sm text-slate-700">{thread.current_summary}</p>
        <p class="mt-2 text-xs text-slate-600">
          Last activity: {thread.updated_at || "unknown"}
        </p>

        <div class="mt-3">
          <ProvenanceBadge provenance={thread.provenance ?? { sources: [] }} />
        </div>

        <div class="mt-3">
          <UnknownObjectPanel objectData={thread} title="Raw Thread Snapshot" />
        </div>
      </li>
    {/each}
  </ul>
{/if}
