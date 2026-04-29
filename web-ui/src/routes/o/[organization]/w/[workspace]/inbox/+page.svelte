<script>
  import { afterNavigate, goto } from "$app/navigation";
  import { page } from "$app/stores";

  import CompactFilterBar from "$lib/components/CompactFilterBar.svelte";
  import Button from "$lib/components/Button.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import SkeletonInboxRow from "$lib/components/state/SkeletonInboxRow.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatAbsoluteDateTime } from "$lib/formatDate";
  import { workspacePath } from "$lib/workspacePaths";
  import {
    INBOX_CATEGORY_ORDER,
    INBOX_CATEGORY_LABELS,
    INBOX_URGENCY_LEVELS,
    INBOX_URGENCY_LABELS,
    enrichInboxItem,
    getInboxCategoryLabel,
    normalizeInboxCategory,
    getInboxSubjectId,
    getInboxSubjectKind,
    getInboxSubjectLabel,
    getInboxSubjectRef,
    splitTypedRef,
    groupInboxItems,
    summarizeInboxUrgency,
  } from "$lib/inboxUtils";
  import { inboxTopicRouteSegment } from "$lib/topicRouteUtils";

  let loading = $state(false);
  let completedLoading = $state(false);
  let completedLoadingMore = $state(false);
  let error = $state("");
  let retrying = $state(false);
  let items = $state([]);
  let completedItems = $state([]);
  let completedNextCursor = $state("");

  let urgencyFilter = $state("all");
  let categoryFilter = $state("all");
  let filtersOpen = $state(false);

  let completedKindFilter = $state("all");
  let completedWindowDays = $state(30);

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);

  let inboxTab = $derived.by(() => {
    const s = String($page.url.searchParams.get("status") ?? "")
      .trim()
      .toLowerCase();
    return s === "completed" ? "completed" : "open";
  });

  let respondedBanner = $derived.by(() => {
    const eventId = String($page.url.searchParams.get("responded") ?? "").trim();
    const threadId = String($page.url.searchParams.get("responded_thread") ?? "").trim();
    if (!eventId) return null;
    const notifyQueued = $page.url.searchParams.get("notify_queued") === "1";
    const notifyRecorded = $page.url.searchParams.get("notify_recorded") === "1";
    return { eventId, threadId, notifyQueued, notifyRecorded };
  });

  let totalItems = $derived(items.length);
  let enrichedItems = $derived(items.map((item) => enrichInboxItem(item)));
  let urgencySummary = $derived(summarizeInboxUrgency(items));
  let filteredItems = $derived(
    enrichedItems.filter((item) => {
      if (
        categoryFilter !== "all" &&
        String(item?.category ?? "") !== categoryFilter
      ) {
        return false;
      }
      if (urgencyFilter === "all") return true;
      return item.urgency_level === urgencyFilter;
    }),
  );
  let groupedItems = $derived(groupInboxItems(filteredItems));
  let visibleGroups = $derived(
    groupedItems.filter((group) => group.items.length > 0),
  );
  let hasFilteredItems = $derived(filteredItems.length > 0);

  let hasActiveFilters = $derived(
    categoryFilter !== "all" || urgencyFilter !== "all",
  );

  function workspaceHref(pathname = "/") {
    return workspacePath(organizationSlug, workspaceSlug, pathname);
  }

  function relatedBoardHref(item) {
    const boardRef = (item?.related_refs ?? []).find(
      (ref) => splitTypedRef(ref).prefix === "board",
    );
    const { id: boardId } = splitTypedRef(boardRef);
    if (!boardId) return "";
    const base = workspaceHref(`/boards/${boardId}`);
    const subjectRef = getInboxSubjectRef(item);
    const subj = splitTypedRef(subjectRef);
    if (subj.prefix === "card" && subj.id) {
      return `${base}?card=${encodeURIComponent(subj.id)}`;
    }
    return base;
  }

  function inboxItemHref(item) {
    const subjectRef = getInboxSubjectRef(item);
    const { prefix, id } = splitTypedRef(subjectRef);

    if (prefix === "topic") {
      const segment = inboxTopicRouteSegment(item) || id;
      return segment
        ? workspaceHref(`/topics/${encodeURIComponent(segment)}`)
        : workspaceHref("/inbox");
    }
    if (prefix === "thread") {
      return id
        ? workspaceHref(`/threads/${encodeURIComponent(id)}`)
        : workspaceHref("/inbox");
    }
    if (prefix === "board") {
      return workspaceHref(`/boards/${id}`);
    }
    if (prefix === "document") {
      return workspaceHref(`/docs/${id}`);
    }
    if (prefix === "card") {
      return relatedBoardHref(item) || workspaceHref("/inbox");
    }

    return workspaceHref("/inbox");
  }

  function inboxActionThreadId(item) {
    const explicitThreadId = String(item?.thread_id ?? "").trim();
    if (explicitThreadId) {
      return explicitThreadId;
    }

    const subjectRef = getInboxSubjectRef(item);
    const { prefix, id } = splitTypedRef(subjectRef);
    if (prefix === "thread") {
      return id;
    }

    return "";
  }

  function completedTimelineHref(row) {
    const tid = String(row.thread_id ?? "").trim();
    let eventId = "";
    const ref = String(row.response_event_ref ?? "").trim();
    if (ref.startsWith("event:")) {
      eventId = ref.slice("event:".length).trim();
    }
    if (!tid || !eventId) return "";
    return `${workspaceHref(`/threads/${encodeURIComponent(tid)}`)}#event-${encodeURIComponent(eventId)}`;
  }

  function completedDetailHref(row) {
    const id = String(row?.id ?? "").trim();
    if (!id) return workspaceHref("/inbox");
    return workspaceHref(`/inbox/${encodeURIComponent(id)}`);
  }

  function snippet(text, max = 140) {
    const s = String(text ?? "").trim();
    if (!s) return "";
    return s.length <= max ? s : `${s.slice(0, max)}…`;
  }

  function syncCompletedFiltersFromUrl() {
    const params = $page.url.searchParams;
    const rawKind = String(params.get("completed_kind") ?? "").trim().toLowerCase();
    const allowedKinds = ["all", "ask", "review", "escalate", "unknown"];
    completedKindFilter = allowedKinds.includes(rawKind) ? rawKind : "all";
    const rawWin = String(params.get("window_days") ?? "").trim();
    if (rawWin === "7" || rawWin === "30" || rawWin === "0") {
      completedWindowDays = Number(rawWin);
    }
  }

  $effect(() => {
    void $page.url.searchParams;
    syncCompletedFiltersFromUrl();
  });

  $effect(() => {
    const params = $page.url.searchParams;
    const rawCategory = String(params.get("category") ?? "").trim();
    const rawUrgency = String(params.get("urgency") ?? "").trim();

    const normalizedCategory =
      rawCategory === "" ? "" : normalizeInboxCategory(rawCategory);
    categoryFilter =
      normalizedCategory && INBOX_CATEGORY_ORDER.includes(normalizedCategory)
        ? normalizedCategory
        : "all";

    urgencyFilter =
      rawUrgency && INBOX_URGENCY_LEVELS.includes(rawUrgency)
        ? rawUrgency
        : "all";

    if (rawCategory || rawUrgency) {
      filtersOpen = true;
    }
  });

  function buildFilterUrl() {
    const params = new URLSearchParams();
    if (categoryFilter !== "all") params.set("category", categoryFilter);
    if (urgencyFilter !== "all") params.set("urgency", urgencyFilter);
    const qs = params.toString();
    const base = workspaceHref("/inbox");
    return qs ? `${base}?${qs}` : base;
  }

  async function applyFilters() {
    await goto(buildFilterUrl(), {
      replaceState: true,
      noScroll: true,
      keepFocus: true,
    });
  }

  async function resetFilters() {
    await goto(workspaceHref("/inbox"), {
      replaceState: true,
      noScroll: true,
      keepFocus: true,
    });
  }

  async function applyCompletedFilters() {
    const params = new URLSearchParams();
    params.set("status", "completed");
    params.set("completed_kind", completedKindFilter);
    params.set("window_days", String(completedWindowDays));
    await goto(`${workspaceHref("/inbox")}?${params}`, {
      replaceState: true,
      noScroll: true,
      keepFocus: true,
    });
  }

  function setUrgencyFromCard(level) {
    urgencyFilter = urgencyFilter === level ? "all" : level;
    applyFilters();
  }

  async function dismissRespondBanner() {
    const url = new URL($page.url);
    url.searchParams.delete("responded");
    url.searchParams.delete("responded_thread");
    url.searchParams.delete("notify_queued");
    url.searchParams.delete("notify_recorded");
    await goto(`${url.pathname}${url.search}`, {
      replaceState: true,
      noScroll: true,
      keepFocus: true,
    });
  }

  async function gotoCompletedTab() {
    await goto(workspaceHref("/inbox?status=completed"), {
      replaceState: false,
      noScroll: false,
      keepFocus: false,
    });
  }

  async function loadOpenInbox(isRetry = false) {
    loading = true;
    error = "";
    retrying = isRetry;

    try {
      const response = await coreClient.listInboxItems({
        status: "open",
      });
      items = response.items ?? [];
    } catch (loadError) {
      const reason =
        loadError instanceof Error ? loadError.message : String(loadError);
      error = `Failed to load inbox: ${reason}`;
    } finally {
      loading = false;
      retrying = false;
    }
  }

  async function loadCompletedInbox(reset = true) {
    if (reset) {
      completedLoading = true;
      completedNextCursor = "";
    } else {
      completedLoadingMore = true;
    }
    error = "";

    try {
      const query = {
        status: "completed",
        limit: 50,
        window_days: Number(completedWindowDays),
      };
      if (completedKindFilter !== "all") {
        query.completed_kind = completedKindFilter;
      }
      if (!reset && completedNextCursor) {
        query.cursor = completedNextCursor;
      }
      const response = await coreClient.listInboxItems(query);
      const rows = response.items ?? [];
      completedNextCursor = String(response.next_cursor ?? "").trim();
      if (reset) {
        completedItems = rows;
      } else {
        completedItems = [...completedItems, ...rows];
      }
    } catch (loadError) {
      const reason =
        loadError instanceof Error ? loadError.message : String(loadError);
      error = `Failed to load completed inbox: ${reason}`;
    } finally {
      completedLoading = false;
      completedLoadingMore = false;
    }
  }

  async function reloadForTab() {
    syncCompletedFiltersFromUrl();
    if (inboxTab === "completed") {
      await loadCompletedInbox(true);
    } else {
      await loadOpenInbox(false);
    }
  }

  afterNavigate(() => {
    syncCompletedFiltersFromUrl();
    void reloadForTab();
  });

  function urgencyDot(level) {
    if (level === "immediate") return "bg-danger";
    if (level === "high") return "bg-warn-text";
    return "bg-line-strong";
  }

  function urgencyBorderClass(level) {
    if (level === "immediate") return "border-l-danger-text";
    if (level === "high") return "border-l-warn-text";
    return "border-l-transparent";
  }

  function urgencyCardClass(level) {
    const active = urgencyFilter === level;
    if (active) return "ring-1 ring-[var(--accent)] border-[var(--accent)]";
    return "border-[var(--line)] hover:border-[var(--fg-subtle)]";
  }

  function categoryBadgeClass(category) {
    if (category === "ask") return "text-accent-text";
    if (category === "escalate") return "text-warn-text";
    if (category === "review") return "text-sky-400";
    return "text-[var(--fg-muted)]";
  }

  function inboxItemKind(item) {
    const explicit = String(item?.kind ?? "")
      .trim()
      .toLowerCase();
    if (explicit) return explicit;
    return String(item?.category ?? "unknown")
      .trim()
      .toLowerCase();
  }

  function inboxKindPillLabel(item) {
    const kind = inboxItemKind(item);
    if (kind === "ask") return "ASK";
    if (kind === "review") return "REVIEW";
    if (kind === "escalate") return "ESCALATE";
    return kind.toUpperCase();
  }

  function askItemHref(item) {
    const inboxItemID = String(item?.id ?? "").trim();
    if (!inboxItemID) return workspaceHref("/inbox");
    return workspaceHref(`/inbox/${encodeURIComponent(inboxItemID)}`);
  }

  function askActorLabel(item) {
    const actorID = String(
      item?.requester_label ??
        item?.requester_agent_id ??
        item?.requester_actor_id ??
        "",
    ).trim();
    if (!actorID) return "unknown session";
    return actorID;
  }
</script>

<div
  class="mb-3 flex max-md:mb-2 flex-wrap items-center justify-between gap-3 border-b border-[var(--line)] pb-3 max-md:pb-2"
>
  <div class="flex flex-wrap items-center gap-3">
    <h1 class="text-subtitle font-semibold text-[var(--fg)]">Inbox</h1>
    <div
      class="inline-flex rounded-md border border-[var(--line)] bg-[var(--panel)] p-0.5 text-micro font-semibold"
      role="tablist"
      aria-label="Inbox scope"
      data-testid="inbox-tab-scope"
    >
      <a
        role="tab"
        aria-selected={inboxTab === "open"}
        class="rounded px-2.5 py-1 transition-colors {inboxTab === 'open'
          ? 'bg-[var(--accent)]/15 text-[var(--accent)]'
          : 'text-[var(--fg-muted)] hover:text-[var(--fg)]'}"
        href={workspaceHref("/inbox")}
        data-testid="inbox-tab-open"
      >
        Open
      </a>
      <a
        role="tab"
        aria-selected={inboxTab === "completed"}
        class="rounded px-2.5 py-1 transition-colors {inboxTab === 'completed'
          ? 'bg-[var(--accent)]/15 text-[var(--accent)]'
          : 'text-[var(--fg-muted)] hover:text-[var(--fg)]'}"
        href={workspaceHref("/inbox?status=completed")}
        data-testid="inbox-tab-completed"
      >
        Completed
      </a>
    </div>
  </div>
  <div class="flex items-center gap-2">
    {#if inboxTab === "open"}
      <button
        class="cursor-pointer inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-micro font-medium transition-colors {hasActiveFilters
          ? 'border-[var(--accent)]/40 bg-[var(--accent)]/10 text-[var(--accent)] hover:bg-[var(--accent)]/15'
          : 'border-[var(--line)] bg-[var(--bg-soft)] text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]'}"
        onclick={() => (filtersOpen = !filtersOpen)}
        type="button"
        data-testid="inbox-filters-toggle"
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
        {hasActiveFilters ? "Filtered" : "Filter"}
      </button>
      <span
        class="inline-flex h-7 items-center gap-1.5 rounded-md px-2.5 text-micro font-semibold tabular-nums leading-none {totalItems >
        0
          ? 'bg-[var(--accent)]/10 text-[var(--accent)]'
          : 'bg-[var(--panel)] text-[var(--fg-muted)]'}"
        data-testid="inbox-triage-header"
      >
        {totalItems} open
      </span>
    {:else}
      <span
        class="inline-flex h-7 items-center rounded-md px-2.5 text-micro font-semibold tabular-nums leading-none bg-[var(--panel)] text-[var(--fg-muted)]"
        data-testid="inbox-completed-count"
      >
        {completedItems.length} shown
      </span>
    {/if}
  </div>
</div>

{#if respondedBanner && inboxTab === "open"}
  <div
    class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-ok/40 bg-ok-soft px-3 py-2.5 text-meta text-ok-text"
    role="status"
    data-testid="inbox-response-banner"
  >
    <div class="space-y-1">
      <div class="font-semibold">Response recorded.</div>
      <div class="text-micro text-ok-text/90">
        {#if respondedBanner.notifyQueued}
          Notification queued for delivery.
        {:else if respondedBanner.notifyRecorded}
          Recorded without notification (notify none or unresolved target).
        {:else}
          Notification status unavailable for this submission.
        {/if}
      </div>
      <div class="flex flex-wrap gap-x-3 gap-y-1 text-micro">
        {#if respondedBanner.threadId && respondedBanner.eventId}
          <a
            class="font-medium underline hover:text-ok-text"
            href={`${workspaceHref(`/threads/${encodeURIComponent(respondedBanner.threadId)}`)}#event-${encodeURIComponent(respondedBanner.eventId)}`}
          >
            View timeline event
          </a>
        {/if}
        <button
          type="button"
          class="font-medium underline hover:text-ok-text"
          onclick={() => void gotoCompletedTab()}
        >
          View in Completed
        </button>
      </div>
    </div>
    <button
      type="button"
      class="shrink-0 text-micro font-medium text-ok-text/80 hover:text-ok-text"
      onclick={() => void dismissRespondBanner()}
    >
      Dismiss
    </button>
  </div>
{/if}

{#if inboxTab === "open" && filtersOpen}
  <CompactFilterBar testId="inbox-filter-panel">
    {#snippet children()}
      <div
        class="flex flex-wrap items-end gap-3 sm:flex-nowrap sm:items-end sm:gap-4"
      >
        <label class="min-w-[11rem] flex-1 text-micro sm:min-w-[13rem]">
          <span class="font-medium text-[var(--fg-muted)]">Category</span>
          <select
            class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
            value={categoryFilter}
            onchange={(e) => {
              categoryFilter = e.currentTarget.value;
              applyFilters();
            }}
            data-testid="inbox-category-filter"
          >
            <option value="all">All</option>
            {#each INBOX_CATEGORY_ORDER as cat}
              <option value={cat}>{INBOX_CATEGORY_LABELS[cat]}</option>
            {/each}
          </select>
        </label>
        <label class="min-w-[11rem] flex-1 text-micro sm:min-w-[13rem]">
          <span class="font-medium text-[var(--fg-muted)]">Urgency</span>
          <select
            class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
            value={urgencyFilter}
            onchange={(e) => {
              urgencyFilter = e.currentTarget.value;
              applyFilters();
            }}
            data-testid="inbox-urgency-filter"
          >
            <option value="all">All</option>
            {#each INBOX_URGENCY_LEVELS as level}
              <option value={level}>{INBOX_URGENCY_LABELS[level]}</option>
            {/each}
          </select>
        </label>
        {#if hasActiveFilters}
          <Button
            variant="secondary"
            size="compact"
            onclick={resetFilters}
            class="sm:ml-auto"
          >
            Clear filters
          </Button>
        {/if}
      </div>
    {/snippet}
  </CompactFilterBar>
{/if}

{#if inboxTab === "completed"}
  <CompactFilterBar testId="inbox-completed-filter-panel">
    {#snippet children()}
      <div
        class="flex flex-wrap items-end gap-3 sm:flex-nowrap sm:items-end sm:gap-4"
      >
        <label class="min-w-[11rem] flex-1 text-micro sm:min-w-[13rem]">
          <span class="font-medium text-[var(--fg-muted)]">Kind</span>
          <select
            class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
            bind:value={completedKindFilter}
            data-testid="inbox-completed-kind-filter"
          >
            <option value="all">All</option>
            <option value="ask">Ask</option>
            <option value="review">Review</option>
            <option value="escalate">Escalate</option>
            <option value="unknown">Unknown</option>
          </select>
        </label>
        <label class="min-w-[11rem] flex-1 text-micro sm:min-w-[13rem]">
          <span class="font-medium text-[var(--fg-muted)]">Time window</span>
          <select
            class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
            bind:value={completedWindowDays}
            data-testid="inbox-completed-window-filter"
          >
            <option value={7}>Last 7 days</option>
            <option value={30}>Last 30 days</option>
            <option value={0}>All time</option>
          </select>
        </label>
        <Button
          variant="secondary"
          size="compact"
          onclick={() => void applyCompletedFilters()}
          class="sm:ml-auto"
        >
          Apply
        </Button>
      </div>
    {/snippet}
  </CompactFilterBar>
{/if}

{#if error}
  <StateError
    message={error}
    onretry={() => void reloadForTab()}
    {retrying}
    class="mb-4"
  />
{/if}

{#if inboxTab === "open"}
  <div class="flex gap-1.5 mb-4" data-testid="urgency-summary-strip">
    <button
      class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-micro font-medium transition-colors {urgencyCardClass(
        'immediate',
      )} {urgencySummary.immediate > 0
        ? 'bg-danger-soft'
        : 'bg-[var(--bg-soft)]'}"
      onclick={() => setUrgencyFromCard("immediate")}
      type="button"
      data-testid="urgency-summary-immediate"
    >
      <span class="inline-block h-1.5 w-1.5 rounded-full bg-danger shrink-0"
      ></span>
      <span class="text-danger-text">Immediate</span>
      <span
        class="tabular-nums {urgencySummary.immediate > 0
          ? 'text-danger-text'
          : 'text-[var(--fg-subtle)]'}">{urgencySummary.immediate}</span
      >
    </button>
    <button
      class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-micro font-medium transition-colors {urgencyCardClass(
        'high',
      )} {urgencySummary.high > 0 ? 'bg-warn/5' : 'bg-[var(--bg-soft)]'}"
      onclick={() => setUrgencyFromCard("high")}
      type="button"
      data-testid="urgency-summary-high"
    >
      <span class="inline-block h-1.5 w-1.5 rounded-full bg-warn-text shrink-0"
      ></span>
      <span class="text-warn-text">High</span>
      <span
        class="tabular-nums {urgencySummary.high > 0
          ? 'text-warn-text'
          : 'text-[var(--fg-subtle)]'}">{urgencySummary.high}</span
      >
    </button>
    <button
      class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-micro font-medium transition-colors bg-[var(--bg-soft)] {urgencyCardClass(
        'normal',
      )}"
      onclick={() => setUrgencyFromCard("normal")}
      type="button"
      data-testid="urgency-summary-normal"
    >
      <span class="inline-block h-1.5 w-1.5 rounded-full bg-fg-muted shrink-0"
      ></span>
      <span class="text-[var(--fg-muted)]">Normal</span>
      <span class="tabular-nums text-[var(--fg-subtle)]"
        >{urgencySummary.normal}</span
      >
    </button>
  </div>
{/if}

{#if inboxTab === "open"}
  {#if loading && items.length === 0}
    <SkeletonInboxRow count={5} />
  {:else if totalItems === 0 && !error}
    <StateEmpty
      title="Inbox is clear"
      helper="Nothing needs attention right now."
    />
  {:else if !hasFilteredItems && totalItems > 0}
    <div class="mt-8 text-center py-12" data-testid="inbox-filter-empty-state">
      <div
        class="inline-flex items-center justify-center w-12 h-12 rounded-full bg-[var(--panel)] mb-3"
      >
        <svg
          class="h-6 w-6 text-[var(--fg-subtle)]"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="1.5"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M12 3c2.755 0 5.455.232 8.083.678.533.09.917.556.917 1.096v1.044a2.25 2.25 0 01-.659 1.591l-5.432 5.432a2.25 2.25 0 00-.659 1.591v2.927a2.25 2.25 0 01-1.244 2.013L9.75 21v-6.568a2.25 2.25 0 00-.659-1.591L3.659 7.409A2.25 2.25 0 013 5.818V4.774c0-.54.384-1.006.917-1.096A48.32 48.32 0 0112 3z"
          />
        </svg>
      </div>
      <h2 class="text-body font-semibold text-[var(--fg)]">
        No items match this view
      </h2>
      <p class="mt-1 text-meta text-[var(--fg-muted)]">
        Try switching back to <span class="font-semibold">All</span> to see the full
        queue.
      </p>
      <Button
        variant="secondary"
        size="compact"
        onclick={resetFilters}
        class="mt-4"
      >
        Clear filters
      </Button>
    </div>
  {:else if hasFilteredItems}
    <div class="space-y-4">
      {#each visibleGroups as group}
        <section data-testid={`inbox-group-${group.category}`}>
          <div class="mb-1.5 flex items-center gap-1.5">
            <h2
              class="text-micro font-semibold uppercase tracking-wider {categoryBadgeClass(
                group.category,
              )}"
            >
              {getInboxCategoryLabel(group.category)}
            </h2>
            <span class="text-micro text-[var(--fg-subtle)] tabular-nums"
              >{group.items.length}</span
            >
          </div>

          <div class="space-y-1.5">
            {#each group.items as item}
              <article
                class="rounded-md border border-[var(--line)] border-l-[3px] bg-[var(--bg-soft)] px-3 py-2.5 transition-colors hover:bg-[var(--panel)] {urgencyBorderClass(
                  item.urgency_level,
                )}"
                data-testid={`inbox-card-${item.id}`}
              >
                <div class="flex items-center justify-between gap-2 text-micro">
                  <div class="flex min-w-0 items-center gap-1.5">
                    <span
                      class="inline-flex items-center rounded px-1.5 py-0.5 text-micro font-semibold tracking-wide {inboxItemKind(
                        item,
                      ) === 'ask'
                        ? 'bg-accent-soft text-accent-text'
                        : 'bg-[var(--line)] text-[var(--fg-muted)]'}"
                    >
                      {inboxKindPillLabel(item)}
                    </span>
                    <span
                      class="inline-flex h-1.5 w-1.5 shrink-0 rounded-full {urgencyDot(
                        item.urgency_level,
                      )}"
                    ></span>
                    <span class="font-medium text-[var(--fg-muted)]"
                      >{item.urgency_label}</span
                    >
                    {#if item.age_label}
                      <span
                        class="text-[var(--fg-muted)]"
                        title={item.has_source_event_time
                          ? formatAbsoluteDateTime(item.source_event_time)
                          : undefined}>&middot; {item.age_label}</span
                      >
                    {/if}
                  </div>
                </div>

                <div class="mt-1 flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                  <h3
                    class="text-meta font-semibold text-[var(--fg)] leading-snug"
                  >
                    {item.title}
                  </h3>
                  {#if getInboxSubjectLabel(item)}
                    <a
                      class="shrink-0 rounded bg-[var(--line)] px-1.5 py-0.5 text-micro font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] hover:text-[var(--fg)]"
                      href={inboxItemHref(item)}
                    >
                      {getInboxSubjectLabel(item)}
                    </a>
                  {/if}
                </div>
                <div
                  class="mt-1 text-micro text-[var(--fg-muted)]"
                  data-testid={`inbox-requester-meta-${item.id}`}
                >
                  Requested by
                  <span class="font-mono text-meta text-[var(--fg)]">
                    {askActorLabel(item)}
                  </span>
                  {#if item.age_label}
                    &middot; requested {item.age_label.replace(" old", "")} ago
                  {/if}
                </div>

                {#if getInboxSubjectRef(item) || (item.related_refs ?? []).length > 0}
                  <div
                    class="mt-1.5 flex flex-wrap items-center gap-1 text-micro"
                  >
                    {#if getInboxSubjectRef(item)}
                      {@const subjectId = getInboxSubjectId(item)}
                      <span
                        class="inline-flex items-center gap-1 rounded bg-[var(--panel)] px-1.5 py-0.5 font-medium text-[var(--fg-muted)]"
                        title={getInboxSubjectRef(item)}
                      >
                        <span>
                          {getInboxSubjectKind(item)
                            ? `${getInboxSubjectKind(item)}:`
                            : "Subject:"}
                        </span>
                        <span
                          >{subjectId.length > 10
                            ? `${subjectId.slice(0, 10)}…`
                            : subjectId}</span
                        >
                      </span>
                    {/if}
                    {#each item.related_refs ?? [] as refValue}
                      <RefLink
                        {refValue}
                        threadId={inboxActionThreadId(item)}
                        humanize
                      />
                    {/each}
                  </div>
                {/if}

                <div class="mt-2 flex flex-wrap items-center gap-1.5">
                  <Button
                    variant="primary"
                    size="compact"
                    href={askItemHref(item)}
                  >
                    Respond
                  </Button>
                </div>
              </article>
            {/each}
          </div>
        </section>
      {/each}
    </div>
  {/if}
{:else}
  {#if completedLoading && completedItems.length === 0}
    <SkeletonInboxRow count={5} />
  {:else if completedItems.length === 0 && !error}
    <StateEmpty
      title="No completed responses yet"
      helper="Responses you submit appear here after they are recorded."
    />
  {:else}
    <div class="space-y-2">
      {#each completedItems as row}
        <article
          class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2.5 transition-colors hover:bg-[var(--panel)]"
          data-testid={`inbox-completed-card-${row.id}`}
        >
          <div class="flex flex-wrap items-center gap-2 text-micro">
            <span
              class="inline-flex items-center rounded px-1.5 py-0.5 font-semibold tracking-wide bg-[var(--line)] text-[var(--fg-muted)]"
            >
              {String(row.kind ?? "").toUpperCase()}
            </span>
            {#if row.responded_at}
              <span class="text-[var(--fg-muted)] tabular-nums">
                {formatAbsoluteDateTime(row.responded_at)}
              </span>
            {/if}
            {#if row.responding_actor_id}
              <span class="text-[var(--fg-muted)]">
                · <span class="font-mono text-[var(--fg)]">{row.responding_actor_id}</span>
              </span>
            {/if}
          </div>
          <h3 class="mt-1 text-meta font-semibold text-[var(--fg)] leading-snug">
            {row.title}
          </h3>
          {#if row.response_text}
            <p class="mt-1 text-micro text-[var(--fg-muted)] leading-snug">
              {snippet(row.response_text)}
            </p>
          {/if}
          {#if row.original_request_missing}
            <p class="mt-1 text-micro text-warn-text">
              Original request details are unavailable for this entry.
            </p>
          {/if}
          <div class="mt-2 flex flex-wrap gap-2">
            <Button variant="secondary" size="compact" href={completedDetailHref(row)}>
              Details
            </Button>
            {#if completedTimelineHref(row)}
              <Button
                variant="secondary"
                size="compact"
                href={completedTimelineHref(row)}
              >
                Timeline event
              </Button>
            {/if}
          </div>
        </article>
      {/each}
      {#if completedNextCursor}
        <div class="flex justify-center pt-2">
          <Button
            variant="secondary"
            size="compact"
            disabled={completedLoadingMore}
            onclick={() => void loadCompletedInbox(false)}
          >
            {completedLoadingMore ? "Loading…" : "Load more"}
          </Button>
        </div>
      {/if}
    </div>
  {/if}
{/if}
