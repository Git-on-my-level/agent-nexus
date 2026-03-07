<script>
  import { onMount } from "svelte";

  import { coreClient } from "$lib/coreClient";
  import {
    buildArtifactKindSummary,
    buildInboxCategorySummary,
    buildThreadHealthSummary,
    selectRecentArtifacts,
    selectRecentlyUpdatedThreads,
  } from "$lib/dashboardSummary";
  import { formatTimestamp } from "$lib/formatDate";
  import { getInboxCategoryLabel, sortInboxItems } from "$lib/inboxUtils";
  import { getPriorityLabel } from "$lib/threadFilters";

  const emptySectionState = {
    status: "idle",
    error: "",
    items: [],
  };

  let loading = $state(true);
  let refreshedAt = $state("");
  let inboxState = $state({ ...emptySectionState });
  let threadsState = $state({ ...emptySectionState });
  let artifactsState = $state({ ...emptySectionState });

  let inboxSummary = $derived(buildInboxCategorySummary(inboxState.items));
  let topInboxItems = $derived(sortInboxItems(inboxState.items).slice(0, 4));

  let threadHealth = $derived(buildThreadHealthSummary(threadsState.items));
  let recentThreads = $derived(
    selectRecentlyUpdatedThreads(threadsState.items, 5),
  );

  let artifactKindSummary = $derived(
    buildArtifactKindSummary(artifactsState.items),
  );
  let recentArtifacts = $derived(
    selectRecentArtifacts(artifactsState.items, 5),
  );

  onMount(async () => {
    await loadDashboard();
  });

  async function loadDashboard() {
    loading = true;

    const [inboxResult, threadResult, artifactResult] =
      await Promise.allSettled([
        coreClient.listInboxItems({ view: "items" }),
        coreClient.listThreads({}),
        coreClient.listArtifacts({}),
      ]);

    inboxState = toSectionState(inboxResult, "items", "Failed to load inbox");
    threadsState = toSectionState(
      threadResult,
      "threads",
      "Failed to load threads",
    );
    artifactsState = toSectionState(
      artifactResult,
      "artifacts",
      "Failed to load artifacts",
    );

    refreshedAt = new Date().toISOString();
    loading = false;
  }

  function toSectionState(result, key, fallbackLabel) {
    if (result.status === "fulfilled") {
      return {
        status: "ready",
        error: "",
        items: result.value?.[key] ?? [],
      };
    }

    const reason =
      result.reason instanceof Error
        ? result.reason.message
        : String(result.reason ?? "Unknown error");

    return {
      status: "error",
      error: `${fallbackLabel}: ${reason}`,
      items: [],
    };
  }

  function inboxItemTarget(item) {
    if (item?.thread_id) {
      return `/threads/${item.thread_id}`;
    }

    return "/inbox";
  }

  function priorityBadge(priority) {
    const styles = {
      p0: "bg-red-100 text-red-700",
      p1: "bg-amber-100 text-amber-700",
      p2: "bg-blue-100 text-blue-700",
      p3: "bg-slate-100 text-slate-700",
    };

    return styles[priority] ?? "bg-slate-100 text-slate-700";
  }
</script>

<div class="space-y-6">
  <header
    class="rounded-xl border border-slate-200 bg-white p-6 shadow-[0_1px_3px_rgba(0,0,0,0.04)]"
  >
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div>
        <p class="text-xs font-semibold uppercase tracking-wide text-slate-500">
          Dashboard
        </p>
        <h1 class="mt-1 text-2xl font-semibold text-slate-900 tracking-tight">
          What needs attention?
        </h1>
        <p class="mt-2 text-sm text-slate-600 leading-relaxed">
          Start with urgent inbox items, then review thread health and recent
          evidence.
        </p>
      </div>
      <div class="flex items-center gap-3">
        <span class="text-xs text-slate-500">
          {#if refreshedAt}
            Updated {formatTimestamp(refreshedAt)}
          {:else if loading}
            Loading...
          {/if}
        </span>
        <button
          class="rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm font-medium text-slate-700 transition-all hover:bg-slate-50 hover:border-slate-300"
          onclick={loadDashboard}
          type="button"
        >
          Refresh
        </button>
      </div>
    </div>
    <div class="mt-5 flex flex-wrap gap-2">
      <a
        class="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white transition-all hover:bg-slate-800"
        href="/inbox"
      >
        Review Inbox
      </a>
      <a
        class="rounded-lg border border-slate-200 bg-white px-4 py-2 text-sm font-medium text-slate-700 transition-all hover:bg-slate-50 hover:border-slate-300"
        href="/threads"
      >
        Open Threads
      </a>
      <a
        class="rounded-lg border border-slate-200 bg-white px-4 py-2 text-sm font-medium text-slate-700 transition-all hover:bg-slate-50 hover:border-slate-300"
        href="/artifacts"
      >
        Inspect Artifacts
      </a>
    </div>
  </header>

  <div class="grid gap-6 xl:grid-cols-3">
    <section
      class="rounded-xl border border-slate-200 bg-white p-5 shadow-[0_1px_3px_rgba(0,0,0,0.04)] xl:col-span-1"
    >
      <div class="flex items-center justify-between gap-3 mb-4">
        <h2 class="text-base font-semibold text-slate-900">Attention Queue</h2>
        <a
          class="text-sm font-medium text-slate-600 hover:text-slate-900 transition-colors"
          href="/inbox">View all</a
        >
      </div>

      {#if inboxState.status === "error"}
        <p class="mt-4 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700">
          {inboxState.error}
        </p>
      {:else if inboxState.items.length === 0}
        <p class="mt-4 text-sm text-slate-500">
          Inbox is clear. Check Threads for follow-ups.
        </p>
      {:else}
        <div class="mt-4 grid grid-cols-3 gap-3">
          {#each inboxSummary as summary}
            <a
              class="rounded-lg border border-slate-200 bg-slate-50 px-3 py-3 text-center transition-all hover:bg-slate-100 hover:border-slate-300"
              href="/inbox"
            >
              <p class="text-xs text-slate-500 font-medium">{summary.label}</p>
              <p class="mt-1 text-xl font-semibold text-slate-900">
                {summary.count}
              </p>
            </a>
          {/each}
        </div>

        <div class="mt-4 space-y-2">
          {#each topInboxItems as item}
            <a
              class="block rounded-lg border border-slate-200 px-4 py-3 transition-all hover:border-slate-300 hover:bg-slate-50"
              href={inboxItemTarget(item)}
            >
              <p class="truncate text-sm font-medium text-slate-900">
                {item.title}
              </p>
              <p class="mt-1 text-xs text-slate-500">
                {getInboxCategoryLabel(item.category)}
                {#if item.source_event_time}
                  · {formatTimestamp(item.source_event_time)}
                {/if}
              </p>
            </a>
          {/each}
        </div>
      {/if}
    </section>

    <section
      class="rounded-xl border border-slate-200 bg-white p-5 shadow-[0_1px_3px_rgba(0,0,0,0.04)] xl:col-span-2"
    >
      <div class="flex items-center justify-between gap-3 mb-4">
        <h2 class="text-base font-semibold text-slate-900">Thread Health</h2>
        <a
          class="text-sm font-medium text-slate-600 hover:text-slate-900 transition-colors"
          href="/threads">View all</a
        >
      </div>

      {#if threadsState.status === "error"}
        <p class="mt-4 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700">
          {threadsState.error}
        </p>
      {:else if threadsState.items.length === 0}
        <p class="mt-4 text-sm text-slate-500">
          No threads yet. Create one from the Threads page when work starts.
        </p>
      {:else}
        <div class="mt-4 grid gap-3 sm:grid-cols-4">
          <a
            class="rounded-lg border border-slate-200 bg-slate-50 p-4 transition-all hover:bg-slate-100"
            href="/threads"
          >
            <p class="text-xs text-slate-500 font-medium">Open threads</p>
            <p class="mt-1 text-2xl font-semibold text-slate-900">
              {threadHealth.openCount}
            </p>
          </a>
          <a
            class="rounded-lg border border-amber-200 bg-amber-50 p-4 transition-all hover:bg-amber-100"
            href="/threads"
          >
            <p class="text-xs text-amber-700 font-medium">Stale check-ins</p>
            <p class="mt-1 text-2xl font-semibold text-amber-800">
              {threadHealth.staleCount}
            </p>
          </a>
          <a
            class="rounded-lg border border-red-200 bg-red-50 p-4 transition-all hover:bg-red-100"
            href="/threads"
          >
            <p class="text-xs text-red-700 font-medium">High priority</p>
            <p class="mt-1 text-2xl font-semibold text-red-800">
              {threadHealth.highPriorityCount}
            </p>
          </a>
          <a
            class="rounded-lg border border-slate-200 bg-slate-50 p-4 transition-all hover:bg-slate-100"
            href="/threads"
          >
            <p class="text-xs text-slate-500 font-medium">Total threads</p>
            <p class="mt-1 text-2xl font-semibold text-slate-900">
              {threadHealth.totalCount}
            </p>
          </a>
        </div>

        <div class="mt-4 space-y-2">
          {#each recentThreads as thread}
            <a
              class="flex items-center gap-3 rounded-lg border border-slate-200 px-4 py-3 transition-all hover:border-slate-300 hover:bg-slate-50"
              href={`/threads/${thread.id}`}
            >
              <div class="min-w-0 flex-1">
                <p class="truncate text-sm font-medium text-slate-900">
                  {thread.title}
                </p>
                <p class="mt-1 text-xs text-slate-500">
                  Updated {formatTimestamp(thread.updated_at)}
                </p>
              </div>
              <span
                class={`rounded-md px-2.5 py-1 text-xs font-medium ${priorityBadge(thread.priority)}`}
                >{getPriorityLabel(thread.priority)}</span
              >
            </a>
          {/each}
        </div>
      {/if}
    </section>
  </div>

  <section
    class="rounded-xl border border-slate-200 bg-white p-5 shadow-[0_1px_3px_rgba(0,0,0,0.04)]"
  >
    <div class="flex items-center justify-between gap-3 mb-4">
      <h2 class="text-base font-semibold text-slate-900">Recent Artifacts</h2>
      <a
        class="text-sm font-medium text-slate-600 hover:text-slate-900 transition-colors"
        href="/artifacts">View all</a
      >
    </div>

    {#if artifactsState.status === "error"}
      <p class="mt-4 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700">
        {artifactsState.error}
      </p>
    {:else if artifactsState.items.length === 0}
      <p class="mt-4 text-sm text-slate-500">
        No artifacts yet. Work orders, receipts, and reviews will appear here.
      </p>
    {:else}
      <div class="mt-4 grid gap-3 sm:grid-cols-4">
        <a
          class="rounded-lg border border-slate-200 bg-slate-50 p-4 transition-all hover:bg-slate-100"
          href="/artifacts"
        >
          <p class="text-xs text-slate-500 font-medium">Reviews</p>
          <p class="mt-1 text-2xl font-semibold text-slate-900">
            {artifactKindSummary.review}
          </p>
        </a>
        <a
          class="rounded-lg border border-slate-200 bg-slate-50 p-4 transition-all hover:bg-slate-100"
          href="/artifacts"
        >
          <p class="text-xs text-slate-500 font-medium">Receipts</p>
          <p class="mt-1 text-2xl font-semibold text-slate-900">
            {artifactKindSummary.receipt}
          </p>
        </a>
        <a
          class="rounded-lg border border-slate-200 bg-slate-50 p-4 transition-all hover:bg-slate-100"
          href="/artifacts"
        >
          <p class="text-xs text-slate-500 font-medium">Work orders</p>
          <p class="mt-1 text-2xl font-semibold text-slate-900">
            {artifactKindSummary.work_order}
          </p>
        </a>
        <a
          class="rounded-lg border border-slate-200 bg-slate-50 p-4 transition-all hover:bg-slate-100"
          href="/artifacts"
        >
          <p class="text-xs text-slate-500 font-medium">Other docs</p>
          <p class="mt-1 text-2xl font-semibold text-slate-900">
            {artifactKindSummary.other}
          </p>
        </a>
      </div>

      <div class="mt-4 space-y-2">
        {#each recentArtifacts as artifact}
          <a
            class="flex items-center gap-3 rounded-lg border border-slate-200 px-4 py-3 transition-all hover:border-slate-300 hover:bg-slate-50"
            href={`/artifacts/${artifact.id}`}
          >
            <span
              class="shrink-0 rounded-md bg-slate-100 px-2.5 py-1 text-xs font-medium text-slate-700"
              >{artifact.kind}</span
            >
            <div class="min-w-0 flex-1">
              <p class="truncate text-sm font-medium text-slate-900">
                {artifact.summary || artifact.id}
              </p>
              <p class="mt-1 text-xs text-slate-500">
                {artifact.thread_id ? `${artifact.thread_id} · ` : ""}Updated {formatTimestamp(
                  artifact.created_at,
                )}
              </p>
            </div>
          </a>
        {/each}
      </div>
    {/if}
  </section>
</div>
