<script>
  import { page } from "$app/stores";
  import { onMount, onDestroy } from "svelte";

  import { coreClient } from "$lib/coreClient";
  import { filterTopLevelDocuments } from "$lib/documentVisibility";
  import {
    buildInboxCategorySummary,
    buildTopicHealthSummary,
    selectRecentlyUpdatedTopics,
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
  import { getPriorityLabel } from "$lib/topicFilters";
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
  let topicsState = $state({ ...emptySectionState });
  let boardsState = $state({ ...emptySectionState });
  let docsState = $state({ ...emptySectionState });
  let workspaceSlug = $derived($page.params.workspace);

  let inboxSummary = $derived(buildInboxCategorySummary(inboxState.items));
  let topInboxItems = $derived(sortInboxItems(inboxState.items).slice(0, 3));

  let topicHealth = $derived(buildTopicHealthSummary(topicsState.items));
  let recentTopics = $derived(
    selectRecentlyUpdatedTopics(topicsState.items, 5),
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
        coreClient.listTopics({}),
        coreClient.listBoards({}),
        coreClient.listDocuments({}),
      ]);

    inboxState = toSectionState(inboxResult, "items", "Failed to load inbox");
    topicsState = toSectionState(
      threadResult,
      "topics",
      "Failed to load topics",
    );
    boardsState = toSectionState(
      boardsResult,
      "boards",
      "Failed to load boards",
    );
    docsState = toSectionState(
      docsResult,
      "documents",
      "Failed to load documents",
    );

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

    if (prefix === "topic") {
      return workspacePath(workspaceSlug, `/topics/${id}`);
    }
    if (prefix === "thread") {
      return workspacePath(workspaceSlug, `/threads/${id}`);
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
      if (!boardId) return workspacePath(workspaceSlug, "/inbox");
      const base = workspacePath(workspaceSlug, `/boards/${boardId}`);
      return id ? `${base}?card=${encodeURIComponent(id)}` : base;
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

  function topicsQueryHref(queryPairs) {
    const params = new URLSearchParams(queryPairs);
    const qs = params.toString();
    const base = workspaceHref("/topics");
    return qs ? `${base}?${qs}` : base;
  }

  function priorityBadge(priority) {
    const styles = {
      p0: "text-danger-text",
      p1: "text-warn-text",
      p2: "text-blue-400",
      p3: "text-fg-subtle",
    };
    return styles[priority] ?? "text-fg-subtle";
  }

  function boardStatusColor(status) {
    if (status === "active") return "text-ok-text bg-ok-soft";
    if (status === "paused") return "text-warn-text bg-warn-soft";
    if (status === "closed") return "text-slate-300 bg-slate-500/10";
    return "text-[var(--fg-muted)] bg-[var(--line)]";
  }

  function docStatusColor(status) {
    if (status === "active") return "text-ok-text bg-ok-soft";
    if (status === "draft") return "text-warn-text bg-warn-soft";
    return "text-[var(--fg-muted)] bg-[var(--line)]";
  }

  function inboxCategoryCountColor(category, count) {
    if (count === 0) return "text-[var(--fg)]";
    if (category === "action_needed") return "text-accent-text";
    if (category === "risk_exception") return "text-warn-text";
    if (category === "attention") return "text-sky-400";
    return "text-[var(--fg)]";
  }

  function inboxCategoryLabelColor(category, count) {
    if (count === 0) return "text-[var(--fg-muted)]";
    if (category === "action_needed") return "text-accent-text";
    if (category === "risk_exception") return "text-warn-text";
    if (category === "attention") return "text-sky-400";
    return "text-[var(--fg-muted)]";
  }

  function inboxCategoryBadgeClass(category) {
    if (category === "action_needed") return "text-accent-text bg-accent-soft";
    if (category === "risk_exception") return "text-warn-text bg-warn-soft";
    if (category === "attention") return "text-sky-400 bg-sky-500/10";
    return "text-[var(--fg-muted)] bg-[var(--line)]";
  }
</script>

<div class="space-y-6 min-w-0 max-w-full">
  <div
    class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-2 min-w-0"
  >
    <div class="min-w-0">
      <h1 class="text-subtitle font-semibold text-[var(--fg)]">Dashboard</h1>
      <p class="mt-0.5 text-meta text-[var(--fg-muted)]">
        {#if refreshedAt}
          Updated {formatTimestamp(refreshedAt)}
        {:else if loading}
          Loading...
        {/if}
      </p>
    </div>
    <div class="flex shrink-0 flex-wrap items-center gap-2">
      <button
        class="cursor-pointer rounded-md border border-[var(--line)] px-2.5 py-1.5 text-meta font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)]"
        onclick={loadDashboard}
        type="button"
      >
        Refresh
      </button>
    </div>
  </div>

  <div class="grid gap-5 xl:grid-cols-[1fr_1.5fr] min-w-0">
    <section class="min-w-0">
      <div class="flex items-center justify-between gap-2 mb-2">
        <h2 class="text-meta font-semibold text-[var(--fg)]">Inbox</h2>
        <a
          class="text-micro font-medium text-[var(--fg-muted)] hover:text-[var(--fg)] transition-colors"
          href={workspaceHref("/inbox")}>View all</a
        >
      </div>
      {#if loading && inboxState.status === "idle"}
        <div
          class="flex items-center gap-2 py-6 text-meta text-[var(--fg-muted)]"
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
        <p
          class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
        >
          {inboxState.error}
        </p>
      {:else if inboxState.items.length === 0}
        <p class="text-meta text-[var(--fg-muted)] py-3">
          Nothing needs attention right now.
        </p>
      {:else}
        <div
          class="mb-3 grid gap-2 [grid-template-columns:repeat(auto-fit,minmax(7.75rem,1fr))]"
        >
          {#each inboxSummary as summary}
            <a
              class="min-w-0 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-2 text-center transition-colors hover:bg-[var(--line-subtle)]"
              href={inboxCategoryHref(summary.category)}
            >
              <p
                class="text-micro font-medium leading-snug text-balance hyphens-manual {inboxCategoryLabelColor(
                  summary.category,
                  summary.count,
                )}"
              >
                {summary.label}
              </p>
              <p
                class="text-subtitle font-semibold {inboxCategoryCountColor(
                  summary.category,
                  summary.count,
                )}"
              >
                {summary.count}
              </p>
            </a>
          {/each}
        </div>

        <div
          class="space-y-px rounded-md border border-[var(--line)] bg-[var(--bg-soft)] overflow-hidden"
        >
          {#each topInboxItems as item, i}
            <a
              class="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-[var(--line-subtle)] {i >
              0
                ? 'border-t border-[var(--line)]'
                : ''}"
              href={inboxItemTarget(item)}
            >
              <div class="min-w-0 flex-1">
                <p class="truncate text-meta font-medium text-[var(--fg)]">
                  {item.title}
                </p>
                {#if getInboxSubjectLabel(item)}
                  <p class="text-micro text-[var(--fg-muted)]">
                    {getInboxSubjectLabel(item)}
                  </p>
                {/if}
              </div>
              <span
                class="shrink-0 rounded px-1.5 py-0.5 text-micro font-medium {inboxCategoryBadgeClass(
                  item.category,
                )}"
              >
                {getInboxCategoryLabel(item.category)}
              </span>
            </a>
          {/each}
        </div>
      {/if}
    </section>

    <section class="min-w-0">
      <div class="flex items-center justify-between gap-2 mb-2">
        <h2 class="text-meta font-semibold text-[var(--fg)]">Topic health</h2>
        <a
          class="text-micro font-medium text-[var(--fg-muted)] hover:text-[var(--fg)] transition-colors"
          href={workspaceHref("/topics")}>View all</a
        >
      </div>
      {#if loading && topicsState.status === "idle"}
        <div
          class="flex items-center gap-2 py-6 text-meta text-[var(--fg-muted)]"
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
      {:else if topicsState.status === "error"}
        <p
          class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
        >
          {topicsState.error}
        </p>
      {:else if topicsState.items.length === 0}
        <p class="text-meta text-[var(--fg-muted)] py-3">
          No topics yet. They'll appear here as work begins.
        </p>
      {:else}
        <div
          class="mb-3 grid gap-2 [grid-template-columns:repeat(auto-fit,minmax(7rem,1fr))]"
        >
          <a
            class="min-w-0 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-2 text-center transition-colors hover:bg-[var(--line-subtle)]"
            href={topicsQueryHref([["open", "1"]])}
          >
            <p class="text-micro font-medium text-[var(--fg-muted)]">Open</p>
            <p class="text-subtitle font-semibold text-[var(--fg)]">
              {topicHealth.openCount}
            </p>
          </a>
          <a
            class="min-w-0 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-2 text-center transition-colors hover:bg-[var(--line-subtle)]"
            href={topicsQueryHref([["stale", "true"]])}
          >
            <p
              class="text-micro font-medium {topicHealth.staleCount > 0
                ? 'text-warn-text'
                : 'text-[var(--fg-muted)]'}"
            >
              Stale
            </p>
            <p
              class="text-subtitle font-semibold {topicHealth.staleCount > 0
                ? 'text-warn-text'
                : 'text-[var(--fg)]'}"
            >
              {topicHealth.staleCount}
            </p>
          </a>
          <a
            class="min-w-0 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-2 text-center transition-colors hover:bg-[var(--line-subtle)]"
            href={topicsQueryHref([["high_priority", "1"]])}
          >
            <p
              class="text-micro font-medium leading-snug text-balance hyphens-manual {topicHealth.highPriorityCount >
              0
                ? 'text-danger-text'
                : 'text-[var(--fg-muted)]'}"
            >
              High priority
            </p>
            <p
              class="text-subtitle font-semibold {topicHealth.highPriorityCount >
              0
                ? 'text-danger-text'
                : 'text-[var(--fg)]'}"
            >
              {topicHealth.highPriorityCount}
            </p>
          </a>
          <a
            class="min-w-0 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-2 text-center transition-colors hover:bg-[var(--line-subtle)]"
            href={workspaceHref("/topics")}
          >
            <p class="text-micro font-medium text-[var(--fg-muted)]">Total</p>
            <p class="text-subtitle font-semibold text-[var(--fg)]">
              {topicHealth.totalCount}
            </p>
          </a>
        </div>

        <div
          class="space-y-px rounded-md border border-[var(--line)] bg-[var(--bg-soft)] overflow-hidden"
        >
          {#each recentTopics as topic, i}
            <a
              class="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-[var(--line-subtle)] {i >
              0
                ? 'border-t border-[var(--line)]'
                : ''}"
              href={workspaceHref(`/topics/${topic.id}`)}
            >
              <div class="min-w-0 flex-1">
                <p class="truncate text-meta font-medium text-[var(--fg)]">
                  {topic.title}
                </p>
                <p class="text-micro text-[var(--fg-muted)]">
                  Updated {formatTimestamp(topic.updated_at)}
                </p>
              </div>
              <span
                class="text-micro font-medium {priorityBadge(topic.priority)}"
                >{getPriorityLabel(topic.priority)}</span
              >
            </a>
          {/each}
        </div>
      {/if}
    </section>
  </div>

  <div class="grid gap-5 xl:grid-cols-2 min-w-0">
    <section class="min-w-0">
      <div class="flex items-center justify-between gap-2 mb-2">
        <h2 class="text-meta font-semibold text-[var(--fg)]">Active boards</h2>
        <a
          class="text-micro font-medium text-[var(--fg-muted)] hover:text-[var(--fg)] transition-colors"
          href={workspaceHref("/boards")}>View all</a
        >
      </div>

      {#if loading && boardsState.status === "idle"}
        <div
          class="flex items-center gap-2 py-6 text-meta text-[var(--fg-muted)]"
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
        <p
          class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
        >
          {boardsState.error}
        </p>
      {:else if recentBoards.length === 0}
        <p class="text-meta text-[var(--fg-muted)] py-3">
          No active boards yet.
        </p>
      {:else}
        <div
          class="space-y-px rounded-md border border-[var(--line)] bg-[var(--bg-soft)] overflow-hidden"
        >
          {#each recentBoards as entry, i}
            {@const board = entry.board}
            {@const summary = entry.summary}
            <a
              class="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-[var(--line-subtle)] {i >
              0
                ? 'border-t border-[var(--line)]'
                : ''}"
              href={workspaceHref(`/boards/${board.id}`)}
            >
              <span
                class="shrink-0 inline-flex rounded px-1.5 py-0.5 text-micro font-semibold {boardStatusColor(
                  board.status,
                )}"
              >
                {BOARD_STATUS_LABELS[board.status] ?? board.status}
              </span>
              <div class="min-w-0 flex-1">
                <p class="truncate text-meta font-medium text-[var(--fg)]">
                  {board.title || board.id}
                </p>
                <p class="text-micro text-[var(--fg-muted)]">
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

    <section class="min-w-0">
      <div class="flex items-center justify-between gap-2 mb-2">
        <h2 class="text-meta font-semibold text-[var(--fg)]">Recent Docs</h2>
        <a
          class="text-micro font-medium text-[var(--fg-muted)] hover:text-[var(--fg)] transition-colors"
          href={workspaceHref("/docs")}>View all</a
        >
      </div>

      {#if loading && docsState.status === "idle"}
        <div
          class="flex items-center gap-2 py-6 text-meta text-[var(--fg-muted)]"
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
        <p
          class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
        >
          {docsState.error}
        </p>
      {:else if recentDocs.length === 0}
        <p class="text-meta text-[var(--fg-muted)] py-3">No documents yet.</p>
      {:else}
        <div
          class="space-y-px rounded-md border border-[var(--line)] bg-[var(--bg-soft)] overflow-hidden"
        >
          {#each recentDocs as doc, i}
            <a
              class="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-[var(--line-subtle)] {i >
              0
                ? 'border-t border-[var(--line)]'
                : ''}"
              href={workspaceHref(`/docs/${doc.id}`)}
            >
              <span
                class="shrink-0 inline-flex rounded px-1.5 py-0.5 text-micro font-semibold {docStatusColor(
                  doc.status,
                )}"
              >
                {DOC_STATUS_LABELS[doc.status] ?? doc.status}
              </span>
              <div class="min-w-0 flex-1">
                <p class="truncate text-meta font-medium text-[var(--fg)]">
                  {doc.title || doc.id}
                </p>
                <p class="text-micro text-[var(--fg-muted)]">
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
