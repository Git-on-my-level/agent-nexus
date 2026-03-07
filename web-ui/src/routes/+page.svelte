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

<div class="space-y-5">
  <header
    class="rounded-2xl border border-teal-100/80 bg-gradient-to-br from-teal-50 via-white to-sky-50 p-5 shadow-[0_12px_24px_rgba(2,132,199,0.08)]"
  >
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <p
          class="text-xs font-semibold uppercase tracking-[0.11em] text-teal-700"
        >
          Home Dashboard
        </p>
        <h1 class="mt-1 text-2xl font-semibold text-slate-900">
          What needs attention now?
        </h1>
        <p class="mt-1 text-sm text-slate-600">
          Start with urgent inbox items, then check thread health and recent
          evidence.
        </p>
      </div>
      <div class="flex items-center gap-2">
        <span class="text-xs text-slate-500">
          {#if refreshedAt}
            Updated {formatTimestamp(refreshedAt)}
          {:else if loading}
            Loading dashboard...
          {/if}
        </span>
        <button
          class="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 transition-colors hover:bg-slate-50"
          onclick={loadDashboard}
          type="button"
        >
          Refresh
        </button>
      </div>
    </div>
    <div class="mt-3 flex flex-wrap gap-2 text-xs">
      <a
        class="rounded-full border border-slate-200 bg-white px-3 py-1 text-slate-600 transition-colors hover:border-slate-300 hover:text-slate-800"
        href="/inbox"
      >
        Review Inbox
      </a>
      <a
        class="rounded-full border border-slate-200 bg-white px-3 py-1 text-slate-600 transition-colors hover:border-slate-300 hover:text-slate-800"
        href="/threads"
      >
        Open Threads
      </a>
      <a
        class="rounded-full border border-slate-200 bg-white px-3 py-1 text-slate-600 transition-colors hover:border-slate-300 hover:text-slate-800"
        href="/artifacts"
      >
        Inspect Artifacts
      </a>
    </div>
  </header>

  <div class="grid gap-4 xl:grid-cols-3">
    <section
      class="rounded-xl border border-slate-200/80 bg-white p-4 shadow-[0_1px_3px_rgba(15,35,52,0.08)] xl:col-span-1"
    >
      <div class="flex items-center justify-between gap-3">
        <h2 class="text-base font-semibold text-slate-900">Attention queue</h2>
        <a
          class="text-xs font-medium text-teal-700 hover:text-teal-800"
          href="/inbox">Open inbox</a
        >
      </div>

      {#if inboxState.status === "error"}
        <p class="mt-3 rounded-lg bg-rose-50 px-3 py-2 text-sm text-rose-700">
          {inboxState.error}
        </p>
      {:else if inboxState.items.length === 0}
        <p class="mt-3 text-sm text-slate-500">
          Inbox is clear right now. You can still check Threads for follow-ups.
        </p>
      {:else}
        <div class="mt-3 grid grid-cols-3 gap-2">
          {#each inboxSummary as summary}
            <a
              class="rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-2 text-center transition-colors hover:border-slate-300"
              href="/inbox"
            >
              <p class="text-[11px] text-slate-500">{summary.label}</p>
              <p class="mt-0.5 text-lg font-semibold text-slate-900">
                {summary.count}
              </p>
            </a>
          {/each}
        </div>

        <div class="mt-3 space-y-2">
          {#each topInboxItems as item}
            <a
              class="block rounded-lg border border-slate-200/80 px-3 py-2 transition-colors hover:border-slate-300 hover:bg-slate-50"
              href={inboxItemTarget(item)}
            >
              <p class="truncate text-sm font-medium text-slate-900">
                {item.title}
              </p>
              <p class="mt-0.5 text-xs text-slate-500">
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
      class="rounded-xl border border-slate-200/80 bg-white p-4 shadow-[0_1px_3px_rgba(15,35,52,0.08)] xl:col-span-2"
    >
      <div class="flex items-center justify-between gap-3">
        <h2 class="text-base font-semibold text-slate-900">Thread health</h2>
        <a
          class="text-xs font-medium text-teal-700 hover:text-teal-800"
          href="/threads">Open threads</a
        >
      </div>

      {#if threadsState.status === "error"}
        <p class="mt-3 rounded-lg bg-rose-50 px-3 py-2 text-sm text-rose-700">
          {threadsState.error}
          <span class="block mt-1 text-rose-600"
            >You can still work from Inbox and Artifacts.</span
          >
        </p>
      {:else if threadsState.items.length === 0}
        <p class="mt-3 text-sm text-slate-500">
          No threads yet. Create one from the Threads page when work starts.
        </p>
      {:else}
        <div class="mt-3 grid gap-2 sm:grid-cols-4">
          <a
            class="rounded-lg border border-slate-200 bg-slate-50 p-3"
            href="/threads"
          >
            <p class="text-xs text-slate-500">Open threads</p>
            <p class="mt-1 text-xl font-semibold text-slate-900">
              {threadHealth.openCount}
            </p>
          </a>
          <a
            class="rounded-lg border border-amber-200 bg-amber-50 p-3"
            href="/threads"
          >
            <p class="text-xs text-amber-700">Stale check-ins</p>
            <p class="mt-1 text-xl font-semibold text-amber-800">
              {threadHealth.staleCount}
            </p>
          </a>
          <a
            class="rounded-lg border border-rose-200 bg-rose-50 p-3"
            href="/threads"
          >
            <p class="text-xs text-rose-700">High priority</p>
            <p class="mt-1 text-xl font-semibold text-rose-800">
              {threadHealth.highPriorityCount}
            </p>
          </a>
          <a
            class="rounded-lg border border-slate-200 bg-slate-50 p-3"
            href="/threads"
          >
            <p class="text-xs text-slate-500">Total threads</p>
            <p class="mt-1 text-xl font-semibold text-slate-900">
              {threadHealth.totalCount}
            </p>
          </a>
        </div>

        <div class="mt-3 space-y-2">
          {#each recentThreads as thread}
            <a
              class="flex items-center gap-2 rounded-lg border border-slate-200/80 px-3 py-2 transition-colors hover:border-slate-300 hover:bg-slate-50"
              href={`/threads/${thread.id}`}
            >
              <div class="min-w-0 flex-1">
                <p class="truncate text-sm font-medium text-slate-900">
                  {thread.title}
                </p>
                <p class="mt-0.5 text-xs text-slate-500">
                  Updated {formatTimestamp(thread.updated_at)}
                </p>
              </div>
              <span
                class={`rounded-md px-2 py-0.5 text-[11px] font-medium ${priorityBadge(thread.priority)}`}
                >{getPriorityLabel(thread.priority)}</span
              >
            </a>
          {/each}
        </div>
      {/if}
    </section>
  </div>

  <section
    class="rounded-xl border border-slate-200/80 bg-white p-4 shadow-[0_1px_3px_rgba(15,35,52,0.08)]"
  >
    <div class="flex items-center justify-between gap-3">
      <h2 class="text-base font-semibold text-slate-900">Recent artifacts</h2>
      <a
        class="text-xs font-medium text-teal-700 hover:text-teal-800"
        href="/artifacts">Open artifacts</a
      >
    </div>

    {#if artifactsState.status === "error"}
      <p class="mt-3 rounded-lg bg-rose-50 px-3 py-2 text-sm text-rose-700">
        {artifactsState.error}
      </p>
    {:else if artifactsState.items.length === 0}
      <p class="mt-3 text-sm text-slate-500">
        No artifacts yet. Work orders, receipts, and reviews will appear here.
      </p>
    {:else}
      <div class="mt-3 grid gap-2 sm:grid-cols-4">
        <a
          class="rounded-lg border border-slate-200 bg-slate-50 p-3"
          href="/artifacts"
        >
          <p class="text-xs text-slate-500">Reviews</p>
          <p class="mt-1 text-xl font-semibold text-slate-900">
            {artifactKindSummary.review}
          </p>
        </a>
        <a
          class="rounded-lg border border-slate-200 bg-slate-50 p-3"
          href="/artifacts"
        >
          <p class="text-xs text-slate-500">Receipts</p>
          <p class="mt-1 text-xl font-semibold text-slate-900">
            {artifactKindSummary.receipt}
          </p>
        </a>
        <a
          class="rounded-lg border border-slate-200 bg-slate-50 p-3"
          href="/artifacts"
        >
          <p class="text-xs text-slate-500">Work orders</p>
          <p class="mt-1 text-xl font-semibold text-slate-900">
            {artifactKindSummary.work_order}
          </p>
        </a>
        <a
          class="rounded-lg border border-slate-200 bg-slate-50 p-3"
          href="/artifacts"
        >
          <p class="text-xs text-slate-500">Other docs</p>
          <p class="mt-1 text-xl font-semibold text-slate-900">
            {artifactKindSummary.other}
          </p>
        </a>
      </div>

      <div class="mt-3 space-y-2">
        {#each recentArtifacts as artifact}
          <a
            class="flex items-center gap-2 rounded-lg border border-slate-200/80 px-3 py-2 transition-colors hover:border-slate-300 hover:bg-slate-50"
            href={`/artifacts/${artifact.id}`}
          >
            <span
              class="shrink-0 rounded-md bg-slate-100 px-2 py-0.5 text-[11px] font-medium text-slate-700"
              >{artifact.kind}</span
            >
            <div class="min-w-0 flex-1">
              <p class="truncate text-sm font-medium text-slate-900">
                {artifact.summary || artifact.id}
              </p>
              <p class="mt-0.5 text-xs text-slate-500">
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
