<script>
  import { page } from "$app/stores";
  import { onMount, onDestroy } from "svelte";

  import { coreClient } from "$lib/coreClient";
  import { filterTopLevelDocuments } from "$lib/documentVisibility";
  import {
    buildInboxCategorySummary,
    buildThreadHealthSummary,
    inboxSummarySentence,
    selectRecentlyUpdatedThreads,
    threadHealthSentence,
  } from "$lib/dashboardSummary";
  import { formatTimestamp } from "$lib/formatDate";
  import {
    getInboxCategoryLabel,
    getInboxSubjectLabel,
    getInboxSubjectRef,
    splitTypedRef,
    sortInboxItems,
  } from "$lib/inboxUtils";
  import { workspacePath } from "$lib/workspacePaths";
  import { getPriorityLabel } from "$lib/threadFilters";
  import { BOARD_STATUS_LABELS } from "$lib/boardUtils";

  const emptySectionState = {
    status: "idle",
    error: "",
    items: [],
  };

  const DOC_STATUS_LABELS = { draft: "Draft", active: "Active" };

  let loading = $state(true);
  let refreshedAt = $state("");
  let inboxState = $state({ ...emptySectionState });
  let threadsState = $state({ ...emptySectionState });
  let boardsState = $state({ ...emptySectionState });
  let docsState = $state({ ...emptySectionState });
  let workspaceSlug = $derived($page.params.workspace);

  let inboxSummary = $derived(buildInboxCategorySummary(inboxState.items));
  let topInboxItems = $derived(sortInboxItems(inboxState.items).slice(0, 5));

  let threadHealth = $derived(buildThreadHealthSummary(threadsState.items));
  let recentThreads = $derived(
    selectRecentlyUpdatedThreads(threadsState.items, 5),
  );

  let recentBoards = $derived(
    boardsState.items
      .filter((entry) => entry?.board?.status === "active")
      .slice(0, 5),
  );

  let recentDocs = $derived(
    [...docsState.items]
      .sort((a, b) => {
        const ta = Date.parse(a?.updated_at ?? "");
        const tb = Date.parse(b?.updated_at ?? "");
        if (Number.isFinite(tb) && Number.isFinite(ta)) return tb - ta;
        if (Number.isFinite(tb)) return 1;
        if (Number.isFinite(ta)) return -1;
        return 0;
      })
      .slice(0, 5),
  );

  const POLL_INTERVAL_MS = 30_000;
  let pollTimer;

  onMount(async () => {
    await loadDashboard();
    pollTimer = setInterval(() => loadDashboard(), POLL_INTERVAL_MS);
  });

  onDestroy(() => {
    clearInterval(pollTimer);
  });

  async function loadDashboard() {
    const isInitial = !refreshedAt;
    if (isInitial) loading = true;

    const [inboxResult, threadResult, boardsResult, docsResult] =
      await Promise.allSettled([
        coreClient.listInboxItems({ view: "items" }),
        coreClient.listThreads({}),
        coreClient.listBoards({}),
        coreClient.listDocuments({}),
      ]);

    inboxState = toSectionState(inboxResult, "items", "Failed to load inbox");
    threadsState = toSectionState(
      threadResult,
      "threads",
      "Failed to load threads",
    );
    boardsState = toSectionState(
      boardsResult,
      "boards",
      "Failed to load boards",
    );
    docsState = toSectionState(docsResult, "documents", "Failed to load docs");

    refreshedAt = new Date().toISOString();
    loading = false;
  }

  function toSectionState(result, key, fallbackLabel) {
    if (result.status === "fulfilled") {
      const items =
        key === "documents"
          ? filterTopLevelDocuments(result.value?.[key])
          : (result.value?.[key] ?? []);
      return {
        status: "ready",
        error: "",
        items,
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
    const subjectRef = getInboxSubjectRef(item);
    const { prefix, id } = splitTypedRef(subjectRef);

    if (prefix === "topic" || prefix === "thread") {
      return workspacePath(workspaceSlug, `/topics/${id}`);
    }
    if (prefix === "board") {
      return workspacePath(workspaceSlug, `/boards/${id}`);
    }
    if (prefix === "document") {
      return workspacePath(workspaceSlug, `/docs/${id}`);
    }
    if (prefix === "card") {
      const boardRef = (item?.related_refs ?? []).find(
        (ref) => splitTypedRef(ref).prefix === "board",
      );
      const { id: boardId } = splitTypedRef(boardRef);
      return boardId
        ? workspacePath(workspaceSlug, `/boards/${boardId}`)
        : workspacePath(workspaceSlug, "/inbox");
    }
    return workspacePath(workspaceSlug, "/inbox");
  }

  function workspaceHref(pathname = "/") {
    return workspacePath(workspaceSlug, pathname);
  }

  function inboxCategoryHref(category) {
    const params = new URLSearchParams();
    if (category) {
      params.set("category", String(category));
    }
    const qs = params.toString();
    const base = workspaceHref("/inbox");
    return qs ? `${base}?${qs}` : base;
  }

  function threadsQueryHref(queryPairs) {
    const params = new URLSearchParams(queryPairs);
    const qs = params.toString();
    const base = workspaceHref("/topics");
    return qs ? `${base}?${qs}` : base;
  }

  function priorityBadge(priority) {
    const styles = {
      p0: "text-red-400",
      p1: "text-amber-400",
      p2: "text-blue-400",
      p3: "text-gray-400",
    };
    return styles[priority] ?? "text-gray-400";
  }

  function boardStatusColor(status) {
    if (status === "active") return "text-emerald-400 bg-emerald-500/10";
    if (status === "paused") return "text-amber-300 bg-amber-500/10";
    if (status === "closed") return "text-slate-300 bg-slate-500/10";
    return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
  }

  function docStatusColor(status) {
    if (status === "active") return "text-emerald-400 bg-emerald-500/10";
    if (status === "draft") return "text-amber-400 bg-amber-500/10";
    return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
  }
</script>

<div class="space-y-6">
  <div class="flex items-baseline justify-between gap-4">
    <div>
      <h1 class="text-lg font-semibold text-[var(--ui-text)]">Dashboard</h1>
      <p class="mt-0.5 text-[13px] text-[var(--ui-text-muted)]">
        {#if refreshedAt}
          Updated {formatTimestamp(refreshedAt)}
        {:else if loading}
          Loading...
        {/if}
      </p>
    </div>
    <div class="flex items-center gap-2">
      <button
        class="cursor-pointer rounded-md border border-[var(--ui-border)] px-2.5 py-1.5 text-[13px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)]"
        onclick={loadDashboard}
        type="button"
      >
        Refresh
      </button>
      <a
        class="rounded-md bg-[var(--ui-panel)] px-3 py-1.5 text-[13px] font-medium text-[var(--ui-text)] transition-colors hover:bg-[var(--ui-border)]"
        href={workspaceHref("/inbox")}
      >
        Review inbox
      </a>
    </div>
  </div>

  <div class="grid gap-5 lg:grid-cols-[1fr_1.5fr]">
    <section>
      <div class="flex items-center justify-between gap-2 mb-2">
        <h2 class="text-[13px] font-semibold text-[var(--ui-text)]">Inbox</h2>
        <a
          class="text-[12px] font-medium text-[var(--ui-text-muted)] hover:text-[var(--ui-text)] transition-colors"
          href={workspaceHref("/inbox")}>View all</a
        >
      </div>
      {#if inboxState.status === "ready"}
        <p class="text-[13px] text-gray-500 mt-1 mb-2">
          {inboxSummarySentence(inboxSummary)}
        </p>
      {/if}

      {#if loading && inboxState.status === "idle"}
        <div
          class="flex items-center gap-2 py-6 text-[13px] text-[var(--ui-text-muted)]"
        >
          <svg class="h-3.5 w-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
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
          Loading...
        </div>
      {:else if inboxState.status === "error"}
        <p class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
          {inboxState.error}
        </p>
      {:else if inboxState.items.length === 0}
        <p class="text-[13px] text-[var(--ui-text-muted)] py-3">
          Nothing needs attention right now.
        </p>
      {:else}
        <div class="flex gap-2 mb-3">
          {#each inboxSummary as summary}
            <a
              class="flex-1 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-center transition-colors hover:bg-[var(--ui-border-subtle)]"
              href={inboxCategoryHref(summary.category)}
            >
              <p class="text-[11px] font-medium text-[var(--ui-text-muted)]">
                {summary.label}
              </p>
              <p class="text-lg font-semibold text-[var(--ui-text)]">
                {summary.count}
              </p>
            </a>
          {/each}
        </div>

        <div
          class="space-y-px rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
        >
          {#each topInboxItems as item, i}
            <a
              class="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-[var(--ui-border-subtle)] {i >
              0
                ? 'border-t border-[var(--ui-border)]'
                : ''}"
              href={inboxItemTarget(item)}
            >
              <div class="min-w-0 flex-1">
                <p
                  class="truncate text-[13px] font-medium text-[var(--ui-text)]"
                >
                  {item.title}
                </p>
                <p class="text-[11px] text-[var(--ui-text-muted)]">
                  {getInboxCategoryLabel(item.category)}
                </p>
                {#if getInboxSubjectLabel(item)}
                  <p class="text-[11px] text-[var(--ui-text-subtle)]">
                    {getInboxSubjectLabel(item)}
                  </p>
                {/if}
              </div>
            </a>
          {/each}
        </div>
      {/if}
    </section>

    <section>
      <div class="flex items-center justify-between gap-2 mb-2">
        <h2 class="text-[13px] font-semibold text-[var(--ui-text)]">
          Topic health
        </h2>
        <a
          class="text-[12px] font-medium text-[var(--ui-text-muted)] hover:text-[var(--ui-text)] transition-colors"
          href={workspaceHref("/topics")}>View all</a
        >
      </div>
      {#if threadsState.status === "ready"}
        <p class="text-[13px] text-gray-500 mt-1 mb-2">
          {threadHealthSentence(threadHealth)}
        </p>
      {/if}

      {#if loading && threadsState.status === "idle"}
        <div
          class="flex items-center gap-2 py-6 text-[13px] text-[var(--ui-text-muted)]"
        >
          <svg class="h-3.5 w-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
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
          Loading...
        </div>
      {:else if threadsState.status === "error"}
        <p class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
          {threadsState.error}
        </p>
      {:else if threadsState.items.length === 0}
        <p class="text-[13px] text-[var(--ui-text-muted)] py-3">
          No topics yet. They'll appear here as work begins.
        </p>
      {:else}
        <div class="flex gap-2 mb-3">
          <a
            class="flex-1 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-center transition-colors hover:bg-[var(--ui-border-subtle)]"
            href={threadsQueryHref([["open", "1"]])}
          >
            <p class="text-[11px] font-medium text-[var(--ui-text-muted)]">
              Open
            </p>
            <p class="text-lg font-semibold text-[var(--ui-text)]">
              {threadHealth.openCount}
            </p>
          </a>
          <a
            class="flex-1 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-center transition-colors hover:bg-[var(--ui-border-subtle)]"
            href={threadsQueryHref([["stale", "true"]])}
          >
            <p
              class="text-[11px] font-medium {threadHealth.staleCount > 0
                ? 'text-amber-400'
                : 'text-[var(--ui-text-muted)]'}"
            >
              Stale
            </p>
            <p
              class="text-lg font-semibold {threadHealth.staleCount > 0
                ? 'text-amber-400'
                : 'text-[var(--ui-text)]'}"
            >
              {threadHealth.staleCount}
            </p>
          </a>
          <a
            class="flex-1 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-center transition-colors hover:bg-[var(--ui-border-subtle)]"
            href={threadsQueryHref([["high_priority", "1"]])}
          >
            <p
              class="text-[11px] font-medium {threadHealth.highPriorityCount > 0
                ? 'text-red-400'
                : 'text-[var(--ui-text-muted)]'}"
            >
              High priority
            </p>
            <p
              class="text-lg font-semibold {threadHealth.highPriorityCount > 0
                ? 'text-red-400'
                : 'text-[var(--ui-text)]'}"
            >
              {threadHealth.highPriorityCount}
            </p>
          </a>
          <a
            class="flex-1 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-center transition-colors hover:bg-[var(--ui-border-subtle)]"
            href={workspaceHref("/topics")}
          >
            <p class="text-[11px] font-medium text-[var(--ui-text-muted)]">
              Total
            </p>
            <p class="text-lg font-semibold text-[var(--ui-text)]">
              {threadHealth.totalCount}
            </p>
          </a>
        </div>

        <div
          class="space-y-px rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
        >
          {#each recentThreads as thread, i}
            <a
              class="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-[var(--ui-border-subtle)] {i >
              0
                ? 'border-t border-[var(--ui-border)]'
                : ''}"
              href={workspaceHref(`/topics/${thread.id}`)}
            >
              <div class="min-w-0 flex-1">
                <p
                  class="truncate text-[13px] font-medium text-[var(--ui-text)]"
                >
                  {thread.title}
                </p>
                <p class="text-[11px] text-[var(--ui-text-muted)]">
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

  <div class="grid gap-5 lg:grid-cols-2">
    <section>
      <div class="flex items-center justify-between gap-2 mb-2">
        <h2 class="text-[13px] font-semibold text-[var(--ui-text)]">
          Active boards
        </h2>
        <a
          class="text-[12px] font-medium text-[var(--ui-text-muted)] hover:text-[var(--ui-text)] transition-colors"
          href={workspaceHref("/boards")}>View all</a
        >
      </div>

      {#if loading && boardsState.status === "idle"}
        <div
          class="flex items-center gap-2 py-6 text-[13px] text-[var(--ui-text-muted)]"
        >
          <svg class="h-3.5 w-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
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
          Loading...
        </div>
      {:else if boardsState.status === "error"}
        <p class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
          {boardsState.error}
        </p>
      {:else if recentBoards.length === 0}
        <p class="text-[13px] text-[var(--ui-text-muted)] py-3">
          No active boards yet.
        </p>
      {:else}
        <div
          class="space-y-px rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
        >
          {#each recentBoards as entry, i}
            {@const board = entry.board}
            {@const summary = entry.summary}
            <a
              class="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-[var(--ui-border-subtle)] {i >
              0
                ? 'border-t border-[var(--ui-border)]'
                : ''}"
              href={workspaceHref(`/boards/${board.id}`)}
            >
              <span
                class="shrink-0 inline-flex rounded px-1.5 py-0.5 text-[11px] font-semibold {boardStatusColor(
                  board.status,
                )}"
              >
                {BOARD_STATUS_LABELS[board.status] ?? board.status}
              </span>
              <div class="min-w-0 flex-1">
                <p
                  class="truncate text-[13px] font-medium text-[var(--ui-text)]"
                >
                  {board.title || board.id}
                </p>
                <p class="text-[11px] text-[var(--ui-text-muted)]">
                  {#if summary?.card_count != null}
                    {summary.card_count} card{summary.card_count === 1
                      ? ""
                      : "s"} ·
                  {/if}
                  Updated {formatTimestamp(board.updated_at) || "—"}
                </p>
              </div>
            </a>
          {/each}
        </div>
      {/if}
    </section>

    <section>
      <div class="flex items-center justify-between gap-2 mb-2">
        <h2 class="text-[13px] font-semibold text-[var(--ui-text)]">
          Recent docs
        </h2>
        <a
          class="text-[12px] font-medium text-[var(--ui-text-muted)] hover:text-[var(--ui-text)] transition-colors"
          href={workspaceHref("/docs")}>View all</a
        >
      </div>

      {#if loading && docsState.status === "idle"}
        <div
          class="flex items-center gap-2 py-6 text-[13px] text-[var(--ui-text-muted)]"
        >
          <svg class="h-3.5 w-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
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
          Loading...
        </div>
      {:else if docsState.status === "error"}
        <p class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
          {docsState.error}
        </p>
      {:else if recentDocs.length === 0}
        <p class="text-[13px] text-[var(--ui-text-muted)] py-3">No docs yet.</p>
      {:else}
        <div
          class="space-y-px rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
        >
          {#each recentDocs as doc, i}
            <a
              class="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-[var(--ui-border-subtle)] {i >
              0
                ? 'border-t border-[var(--ui-border)]'
                : ''}"
              href={workspaceHref(`/docs/${doc.id}`)}
            >
              <span
                class="shrink-0 inline-flex rounded px-1.5 py-0.5 text-[11px] font-semibold {docStatusColor(
                  doc.status,
                )}"
              >
                {DOC_STATUS_LABELS[doc.status] ?? doc.status}
              </span>
              <div class="min-w-0 flex-1">
                <p
                  class="truncate text-[13px] font-medium text-[var(--ui-text)]"
                >
                  {doc.title || doc.id}
                </p>
                <p class="text-[11px] text-[var(--ui-text-muted)]">
                  v{doc.head_revision_number} · Updated {formatTimestamp(
                    doc.updated_at,
                  ) || "—"}
                  {#if (doc.labels ?? []).length > 0}
                    · {doc.labels.slice(0, 2).join(", ")}
                  {/if}
                </p>
              </div>
            </a>
          {/each}
        </div>
      {/if}
    </section>
  </div>
</div>
