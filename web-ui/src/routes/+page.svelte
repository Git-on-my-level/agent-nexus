<script>
  import { onMount } from "svelte";

  import { coreClient } from "$lib/coreClient";
  import {
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
  let topInboxItems = $derived(sortInboxItems(inboxState.items).slice(0, 5));

  let threadHealth = $derived(buildThreadHealthSummary(threadsState.items));
  let recentThreads = $derived(
    selectRecentlyUpdatedThreads(threadsState.items, 5),
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
      p0: "text-red-400",
      p1: "text-amber-400",
      p2: "text-blue-600",
      p3: "text-gray-400",
    };
    return styles[priority] ?? "text-gray-400";
  }
</script>

<div class="space-y-6">
  <div class="flex items-baseline justify-between gap-4">
    <div>
      <h1 class="text-lg font-semibold text-gray-900">Dashboard</h1>
      <p class="mt-0.5 text-[13px] text-gray-500">
        {#if refreshedAt}
          Updated {formatTimestamp(refreshedAt)}
        {:else if loading}
          Loading...
        {/if}
      </p>
    </div>
    <div class="flex items-center gap-2">
      <button
        class="rounded-md border border-gray-200 px-2.5 py-1.5 text-[13px] font-medium text-gray-600 transition-colors hover:bg-gray-200"
        onclick={loadDashboard}
        type="button"
      >
        Refresh
      </button>
      <a
        class="rounded-md bg-gray-200 px-3 py-1.5 text-[13px] font-medium text-gray-900 transition-colors hover:bg-gray-300"
        href="/inbox"
      >
        Review inbox
      </a>
    </div>
  </div>

  <div class="grid gap-5 lg:grid-cols-[1fr_1.5fr]">
    <section>
      <div class="flex items-center justify-between gap-2 mb-2">
        <h2 class="text-[13px] font-semibold text-gray-900">Inbox</h2>
        <a
          class="text-[12px] font-medium text-gray-400 hover:text-gray-600 transition-colors"
          href="/inbox">View all</a
        >
      </div>

      {#if inboxState.status === "error"}
        <p class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
          {inboxState.error}
        </p>
      {:else if inboxState.items.length === 0}
        <p class="text-[13px] text-gray-400 py-3">
          Inbox clear. Check threads for follow-ups.
        </p>
      {:else}
        <div class="flex gap-2 mb-3">
          {#each inboxSummary as summary}
            <a
              class="flex-1 rounded-md border border-gray-200 bg-gray-100 px-3 py-2 text-center transition-colors hover:bg-gray-200"
              href="/inbox"
            >
              <p class="text-[11px] font-medium text-gray-400">
                {summary.label}
              </p>
              <p class="text-lg font-semibold text-gray-900">{summary.count}</p>
            </a>
          {/each}
        </div>

        <div
          class="space-y-px rounded-md border border-gray-200 bg-gray-100 overflow-hidden"
        >
          {#each topInboxItems as item, i}
            <a
              class="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-gray-200 {i >
              0
                ? 'border-t border-gray-200'
                : ''}"
              href={inboxItemTarget(item)}
            >
              <div class="min-w-0 flex-1">
                <p class="truncate text-[13px] font-medium text-gray-900">
                  {item.title}
                </p>
                <p class="text-[11px] text-gray-400">
                  {getInboxCategoryLabel(item.category)}
                </p>
              </div>
            </a>
          {/each}
        </div>
      {/if}
    </section>

    <section>
      <div class="flex items-center justify-between gap-2 mb-2">
        <h2 class="text-[13px] font-semibold text-gray-900">Thread health</h2>
        <a
          class="text-[12px] font-medium text-gray-400 hover:text-gray-600 transition-colors"
          href="/threads">View all</a
        >
      </div>

      {#if threadsState.status === "error"}
        <p class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
          {threadsState.error}
        </p>
      {:else if threadsState.items.length === 0}
        <p class="text-[13px] text-gray-400 py-3">No threads yet.</p>
      {:else}
        <div class="flex gap-2 mb-3">
          <a
            class="flex-1 rounded-md border border-gray-200 bg-gray-100 px-3 py-2 text-center transition-colors hover:bg-gray-200"
            href="/threads"
          >
            <p class="text-[11px] font-medium text-gray-400">Open</p>
            <p class="text-lg font-semibold text-gray-900">
              {threadHealth.openCount}
            </p>
          </a>
          <a
            class="flex-1 rounded-md border border-gray-200 bg-gray-100 px-3 py-2 text-center transition-colors hover:bg-gray-200"
            href="/threads"
          >
            <p
              class="text-[11px] font-medium {threadHealth.staleCount > 0
                ? 'text-amber-400'
                : 'text-gray-400'}"
            >
              Stale
            </p>
            <p
              class="text-lg font-semibold {threadHealth.staleCount > 0
                ? 'text-amber-400'
                : 'text-gray-900'}"
            >
              {threadHealth.staleCount}
            </p>
          </a>
          <a
            class="flex-1 rounded-md border border-gray-200 bg-gray-100 px-3 py-2 text-center transition-colors hover:bg-gray-200"
            href="/threads"
          >
            <p
              class="text-[11px] font-medium {threadHealth.highPriorityCount > 0
                ? 'text-red-400'
                : 'text-gray-400'}"
            >
              High priority
            </p>
            <p
              class="text-lg font-semibold {threadHealth.highPriorityCount > 0
                ? 'text-red-400'
                : 'text-gray-900'}"
            >
              {threadHealth.highPriorityCount}
            </p>
          </a>
          <a
            class="flex-1 rounded-md border border-gray-200 bg-gray-100 px-3 py-2 text-center transition-colors hover:bg-gray-200"
            href="/threads"
          >
            <p class="text-[11px] font-medium text-gray-400">Total</p>
            <p class="text-lg font-semibold text-gray-900">
              {threadHealth.totalCount}
            </p>
          </a>
        </div>

        <div
          class="space-y-px rounded-md border border-gray-200 bg-gray-100 overflow-hidden"
        >
          {#each recentThreads as thread, i}
            <a
              class="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-gray-200 {i >
              0
                ? 'border-t border-gray-200'
                : ''}"
              href={`/threads/${thread.id}`}
            >
              <div class="min-w-0 flex-1">
                <p class="truncate text-[13px] font-medium text-gray-900">
                  {thread.title}
                </p>
                <p class="text-[11px] text-gray-400">
                  Updated {formatTimestamp(thread.updated_at)}
                </p>
              </div>
              <span
                class="text-[11px] font-medium {priorityBadge(thread.priority)}"
                >{getPriorityLabel(thread.priority)}</span
              >
            </a>
          {/each}
        </div>
      {/if}
    </section>
  </div>

  <section>
    <div class="flex items-center justify-between gap-2 mb-2">
      <h2 class="text-[13px] font-semibold text-gray-900">Recent artifacts</h2>
      <a
        class="text-[12px] font-medium text-gray-400 hover:text-gray-600 transition-colors"
        href="/artifacts">View all</a
      >
    </div>

    {#if artifactsState.status === "error"}
      <p class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
        {artifactsState.error}
      </p>
    {:else if artifactsState.items.length === 0}
      <p class="text-[13px] text-gray-400 py-3">No artifacts yet.</p>
    {:else}
      <div
        class="space-y-px rounded-md border border-gray-200 bg-gray-100 overflow-hidden"
      >
        {#each recentArtifacts as artifact, i}
          <a
            class="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-gray-200 {i >
            0
              ? 'border-t border-gray-200'
              : ''}"
            href={`/artifacts/${artifact.id}`}
          >
            <span
              class="shrink-0 rounded bg-gray-200 px-1.5 py-0.5 text-[11px] font-medium text-gray-600"
              >{artifact.kind}</span
            >
            <div class="min-w-0 flex-1">
              <p class="truncate text-[13px] font-medium text-gray-900">
                {artifact.summary || artifact.id}
              </p>
            </div>
            <span class="text-[11px] text-gray-400"
              >{formatTimestamp(artifact.created_at)}</span
            >
          </a>
        {/each}
      </div>
    {/if}
  </section>
</div>
