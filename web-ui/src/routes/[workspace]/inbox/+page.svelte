<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { onDestroy, onMount } from "svelte";

  import CompactFilterBar from "$lib/components/CompactFilterBar.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatAbsoluteDateTime } from "$lib/formatDate";
  import { workspacePath } from "$lib/workspacePaths";
  import {
    INBOX_CATEGORY_ORDER,
    INBOX_CATEGORY_LABELS,
    INBOX_URGENCY_LEVELS,
    INBOX_URGENCY_LABELS,
    decisionGroundingRefForInboxItem,
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

  /** Delay before inbox mutations hit core; allows Undo before the request runs. */
  const PENDING_INBOX_ACTION_MS = 5000;

  let loading = $state(false);
  let error = $state("");
  let items = $state([]);
  let ackInFlightById = $state({});
  let pendingAckById = $state({});
  let pendingDecisionById = $state({});
  let decisionInFlightById = $state({});
  let decisionFormsById = $state({});
  let decisionFormErrorsById = $state({});
  let postedDecisionByInboxItem = $state({});
  let urgencyFilter = $state("all");
  let categoryFilter = $state("all");
  let filtersOpen = $state(false);
  let workspaceSlug = $derived($page.params.workspace);

  function cancelPendingInboxTimers() {
    for (const pending of Object.values(pendingAckById)) {
      if (pending?.timeoutId != null) {
        clearTimeout(pending.timeoutId);
      }
    }
    for (const pending of Object.values(pendingDecisionById)) {
      if (pending?.timeoutId != null) {
        clearTimeout(pending.timeoutId);
      }
    }
  }

  onDestroy(() => {
    cancelPendingInboxTimers();
  });

  let subjectContextCache = $state({});
  let subjectContextLoading = $state({});
  let subjectContextErrors = $state({});

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
    return workspacePath(workspaceSlug, pathname);
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

  function setUrgencyFromCard(level) {
    urgencyFilter = urgencyFilter === level ? "all" : level;
    applyFilters();
  }

  onMount(() => {
    void loadInbox();
  });

  async function loadInbox() {
    loading = true;
    error = "";

    try {
      const response = await coreClient.listInboxItems({ view: "items" });
      items = response.items ?? [];
    } catch (loadError) {
      const reason =
        loadError instanceof Error ? loadError.message : String(loadError);
      error = `Failed to load inbox: ${reason}`;
      items = [];
    } finally {
      loading = false;
    }
  }

  function getDecisionForm(itemId) {
    return (
      decisionFormsById[itemId] ?? {
        summary: "",
        notes: "",
        open: false,
      }
    );
  }

  function getDecisionFormError(itemId) {
    return String(decisionFormErrorsById[itemId] ?? "").trim();
  }

  function toggleDecisionForm(item, open) {
    const existing = getDecisionForm(item.id);

    decisionFormsById = {
      ...decisionFormsById,
      [item.id]: {
        ...existing,
        open,
      },
    };

    if (open) {
      loadSubjectContext(item);
    }
  }

  async function loadSubjectContext(item) {
    const subjectRef = getInboxSubjectRef(item);
    const { prefix, id } = splitTypedRef(subjectRef);
    const cacheKey = subjectRef || String(item?.id ?? "");

    if (!cacheKey || subjectContextCache[cacheKey]) return;

    subjectContextLoading = { ...subjectContextLoading, [cacheKey]: true };
    try {
      let subject = null;
      let related = {};

      if (prefix === "topic") {
        try {
          const response = await coreClient.getTopic(id);
          subject = response.topic ?? null;
        } catch (err) {
          if (err?.status !== 404) {
            throw err;
          }
          const response = await coreClient.getThread(id);
          subject = response.thread ?? null;
        }
      } else if (prefix === "thread") {
        const response = await coreClient.getThread(id);
        subject = response.thread ?? null;
      } else if (prefix === "board") {
        const response = await coreClient.getBoard(id);
        subject = response.board ?? null;
        related = {
          summary: response.summary ?? null,
        };
      } else if (prefix === "card") {
        const response = await coreClient.getCard(id);
        subject = response.card ?? response.membership ?? null;
        if (subject?.board_ref) {
          const boardId = String(subject.board_ref ?? "").replace(
            /^board:/,
            "",
          );
          if (boardId) {
            try {
              const boardResponse = await coreClient.getBoard(boardId);
              related.board = boardResponse.board ?? null;
              related.board_summary = boardResponse.summary ?? null;
            } catch {
              related.board = null;
            }
          }
        }
      } else if (prefix === "document") {
        const response = await coreClient.getDocument(id);
        subject = response.document ?? null;
      }

      subjectContextCache = {
        ...subjectContextCache,
        [cacheKey]: {
          subject,
          related,
          subject_ref: subjectRef,
        },
      };
    } catch (e) {
      subjectContextErrors = {
        ...subjectContextErrors,
        [cacheKey]: e.message || String(e),
      };
    } finally {
      subjectContextLoading = { ...subjectContextLoading, [cacheKey]: false };
    }
  }

  function setDecisionFormError(itemId, message) {
    decisionFormErrorsById = {
      ...decisionFormErrorsById,
      [itemId]: String(message ?? ""),
    };
  }

  function updateDecisionField(itemId, field, value) {
    decisionFormsById = {
      ...decisionFormsById,
      [itemId]: {
        ...getDecisionForm(itemId),
        [field]: value,
      },
    };

    if (field === "summary" && String(value ?? "").trim()) {
      setDecisionFormError(itemId, "");
    }
  }

  function acknowledgeItem(item) {
    error = "";

    const subjectRef = getInboxSubjectRef(item);
    if (!subjectRef) {
      error =
        "Cannot acknowledge: no subject reference for this inbox item (core requires subject_ref).";
      return;
    }

    items = items.filter((candidate) => candidate.id !== item.id);

    const timeoutId = setTimeout(async () => {
      pendingAckById = Object.fromEntries(
        Object.entries(pendingAckById).filter(([k]) => k !== item.id),
      );

      ackInFlightById = { ...ackInFlightById, [item.id]: true };
      try {
        const ackPayload = { inbox_item_id: item.id, subject_ref: subjectRef };
        await coreClient.ackInboxItem(ackPayload);
      } catch (ackError) {
        const reason =
          ackError instanceof Error ? ackError.message : String(ackError);
        error = `Failed to acknowledge item: ${reason}`;
        items = [...items, item];
      } finally {
        ackInFlightById = { ...ackInFlightById, [item.id]: false };
      }
    }, PENDING_INBOX_ACTION_MS);

    pendingAckById = { ...pendingAckById, [item.id]: { item, timeoutId } };
  }

  function undoAcknowledge(itemId) {
    const pending = pendingAckById[itemId];
    if (!pending) return;

    clearTimeout(pending.timeoutId);
    pendingAckById = Object.fromEntries(
      Object.entries(pendingAckById).filter(([k]) => k !== itemId),
    );

    items = [...items, pending.item];
  }

  function undoPendingDecision(itemId) {
    const pending = pendingDecisionById[itemId];
    if (!pending) return;

    clearTimeout(pending.timeoutId);
    pendingDecisionById = Object.fromEntries(
      Object.entries(pendingDecisionById).filter(([k]) => k !== itemId),
    );

    items = [...items, pending.item];
    decisionFormsById = {
      ...decisionFormsById,
      [itemId]: {
        ...getDecisionForm(itemId),
        summary: pending.summary,
        notes: pending.notes,
        open: true,
      },
    };
    loadSubjectContext(pending.item);
  }

  function recordDecision(item) {
    const draft = getDecisionForm(item.id);
    error = "";
    setDecisionFormError(item.id, "");

    const actionThreadId = inboxActionThreadId(item);

    if (!actionThreadId) {
      error = "Cannot record decision: no backing thread to attach.";
      return;
    }

    if (!draft.summary.trim()) {
      setDecisionFormError(item.id, "Decision summary is required.");
      return;
    }

    const groundingRef = decisionGroundingRefForInboxItem(item);
    if (!groundingRef) {
      error =
        "Cannot record decision: no thread grounding ref could be derived for this inbox item.";
      return;
    }

    const summary = draft.summary.trim();
    const notes = draft.notes.trim();
    const refs = Array.from(
      new Set([
        ...(Array.isArray(item.related_refs) ? item.related_refs : []),
        `inbox:${item.id}`,
        groundingRef,
      ]),
    );
    items = items.filter((candidate) => candidate.id !== item.id);
    toggleDecisionForm(item, false);
    updateDecisionField(item.id, "summary", "");
    updateDecisionField(item.id, "notes", "");

    const timeoutId = setTimeout(async () => {
      pendingDecisionById = Object.fromEntries(
        Object.entries(pendingDecisionById).filter(([k]) => k !== item.id),
      );

      decisionInFlightById = { ...decisionInFlightById, [item.id]: true };
      try {
        const response = await coreClient.createEvent({
          event: {
            type: "decision_made",
            thread_id: actionThreadId,
            refs,
            summary,
            payload: {
              notes,
              inbox_item_id: item.id,
            },
            provenance: {
              sources: ["actor_statement:ui"],
            },
          },
        });

        postedDecisionByInboxItem = {
          ...postedDecisionByInboxItem,
          [item.id]: response.event,
        };
      } catch (decisionError) {
        const reason =
          decisionError instanceof Error
            ? decisionError.message
            : String(decisionError);
        error = `Failed to record decision: ${reason}`;
        items = [...items, item];
        decisionFormsById = {
          ...decisionFormsById,
          [item.id]: {
            ...getDecisionForm(item.id),
            summary,
            notes,
            open: true,
          },
        };
        loadSubjectContext(item);
      } finally {
        decisionInFlightById = { ...decisionInFlightById, [item.id]: false };
      }
    }, PENDING_INBOX_ACTION_MS);

    pendingDecisionById = {
      ...pendingDecisionById,
      [item.id]: { item, summary, notes, timeoutId },
    };
  }

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
    if (active)
      return "ring-1 ring-[var(--accent)] border-[var(--accent)]";
    return "border-[var(--line)] hover:border-[var(--fg-subtle)]";
  }

  function categoryBadgeClass(category) {
    if (category === "action_needed") return "text-accent-text";
    if (category === "risk_exception") return "text-warn-text";
    if (category === "attention") return "text-sky-400";
    return "text-[var(--fg-muted)]";
  }

  function inboxItemKind(item) {
    const explicit = String(item?.kind ?? "")
      .trim()
      .toLowerCase();
    if (explicit) return explicit;
    return "tag";
  }

  function isAskItem(item) {
    return inboxItemKind(item) === "ask";
  }

  function inboxKindPillLabel(item) {
    const kind = inboxItemKind(item);
    if (kind === "ask") return "ASK";
    if (kind === "wake") return "WAKE";
    return "TAG";
  }

  function askItemHref(item) {
    const inboxItemID = String(item?.id ?? "").trim();
    if (!inboxItemID) return workspaceHref("/inbox");
    return workspaceHref(`/inbox/${encodeURIComponent(inboxItemID)}`);
  }

  function askActorLabel(item) {
    const actorID = String(item?.asking_agent_id ?? "").trim();
    if (!actorID) return "unknown session";
    return actorID;
  }
</script>

<div
  class="flex items-center justify-between pb-4 mb-4 border-b border-[var(--line)]"
>
  <div>
    <h1 class="text-lg font-semibold text-[var(--fg)]">Inbox</h1>
  </div>
  <div class="flex items-center gap-2">
    <button
      class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-[12px] font-medium transition-colors {hasActiveFilters
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
      class="inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-[13px] font-semibold tabular-nums {totalItems >
      0
        ? 'bg-[var(--accent)]/10 text-[var(--accent)]'
        : 'bg-[var(--panel)] text-[var(--fg-muted)]'}"
      data-testid="inbox-triage-header"
    >
      {totalItems} open
    </span>
  </div>
</div>

{#if filtersOpen}
  <CompactFilterBar testId="inbox-filter-panel">
    {#snippet children()}
      <div
        class="flex flex-wrap items-end gap-3 sm:flex-nowrap sm:items-end sm:gap-4"
      >
        <label class="min-w-[11rem] flex-1 text-[12px] sm:min-w-[13rem]">
          <span class="font-medium text-[var(--fg-muted)]">Category</span>
          <select
            class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-[13px] transition-colors focus:bg-[var(--panel)]"
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
        <label class="min-w-[11rem] flex-1 text-[12px] sm:min-w-[13rem]">
          <span class="font-medium text-[var(--fg-muted)]">Urgency</span>
          <select
            class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-[13px] transition-colors focus:bg-[var(--panel)]"
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
          <button
            class="cursor-pointer rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-1.5 text-[12px] font-medium text-[var(--fg-muted)] hover:bg-[var(--line-subtle)] sm:ml-auto"
            onclick={resetFilters}
            type="button"
          >
            Clear filters
          </button>
        {/if}
      </div>
    {/snippet}
  </CompactFilterBar>
{/if}

{#if error}
  <div
    class="mb-4 rounded-md bg-danger-soft px-3 py-2.5 text-[13px] text-danger-text"
    role="alert"
  >
    {error}
  </div>
{/if}

<div class="flex gap-1.5 mb-4" data-testid="urgency-summary-strip">
  <button
    class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-[12px] font-medium transition-colors {urgencyCardClass(
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
    class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-[12px] font-medium transition-colors {urgencyCardClass(
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
    class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-[12px] font-medium transition-colors bg-[var(--bg-soft)] {urgencyCardClass(
      'normal',
    )}"
    onclick={() => setUrgencyFromCard("normal")}
    type="button"
    data-testid="urgency-summary-normal"
  >
    <span class="inline-block h-1.5 w-1.5 rounded-full bg-bg0 shrink-0"
    ></span>
    <span class="text-[var(--fg-muted)]">Normal</span>
    <span class="tabular-nums text-[var(--fg-subtle)]"
      >{urgencySummary.normal}</span
    >
  </button>
</div>

{#if Object.keys(pendingAckById).length > 0 || Object.keys(pendingDecisionById).length > 0}
  <div class="mb-4 space-y-1.5" data-testid="inbox-pending-actions">
    {#each Object.values(pendingAckById) as pending}
      <div
        class="flex items-center justify-between gap-3 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-[12px] text-[var(--fg-muted)]"
      >
        <span class="truncate"
          >Acknowledged: <span class="font-medium text-[var(--fg)]"
            >{pending.item.title ?? pending.item.summary ?? "item"}</span
          ></span
        >
        <button
          class="cursor-pointer shrink-0 font-medium text-accent-text hover:text-accent-text"
          onclick={() => undoAcknowledge(pending.item.id)}
          type="button"
          aria-label="Undo acknowledge for {pending.item.title ??
            pending.item.summary ??
            'inbox item'}"
        >
          Undo
        </button>
      </div>
    {/each}
    {#each Object.values(pendingDecisionById) as pending}
      <div
        class="flex items-center justify-between gap-3 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-[12px] text-[var(--fg-muted)]"
      >
        <span class="truncate"
          >Decision pending: <span class="font-medium text-[var(--fg)]"
            >{pending.item.title ?? pending.item.summary ?? "item"}</span
          ></span
        >
        <button
          class="cursor-pointer shrink-0 font-medium text-accent-text hover:text-accent-text"
          onclick={() => undoPendingDecision(pending.item.id)}
          type="button"
          aria-label="Undo pending decision for {pending.item.title ??
            pending.item.summary ??
            'inbox item'}"
        >
          Undo
        </button>
      </div>
    {/each}
  </div>
{/if}

{#if loading}
  <div
    class="mt-12 flex items-center justify-center gap-2 text-[13px] text-[var(--fg-muted)]"
  >
    <svg class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
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
    Loading inbox...
  </div>
{:else if totalItems === 0}
  <div class="mt-8 text-center py-12" data-testid="inbox-empty-state">
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
          d="M9 12.75L11.25 15 15 9.75M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
        />
      </svg>
    </div>
    <h2 class="text-[14px] font-semibold text-[var(--fg)]">
      Inbox is clear
    </h2>
    <p class="mt-1 text-[13px] text-[var(--fg-muted)]">
      Nothing needs attention right now.
    </p>
  </div>
{:else if !hasFilteredItems}
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
    <h2 class="text-[14px] font-semibold text-[var(--fg)]">
      No items match this view
    </h2>
    <p class="mt-1 text-[13px] text-[var(--fg-muted)]">
      Try switching back to <span class="font-semibold">All</span> to see the full
      queue.
    </p>
    <button
      class="cursor-pointer mt-4 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-1.5 text-[13px] font-medium text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]"
      onclick={resetFilters}
      type="button"
    >
      Clear filters
    </button>
  </div>
{:else}
  <div class="space-y-4">
    {#each visibleGroups as group}
      <section data-testid={`inbox-group-${group.category}`}>
        <div class="mb-1.5 flex items-center gap-1.5">
          <h2
            class="text-[11px] font-semibold uppercase tracking-wider {categoryBadgeClass(
              group.category,
            )}"
          >
            {getInboxCategoryLabel(group.category)}
          </h2>
          <span class="text-[11px] text-[var(--fg-subtle)] tabular-nums"
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
              <div class="flex items-center justify-between gap-2 text-[11px]">
                <div class="flex min-w-0 items-center gap-1.5">
                  <span
                    class="inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-semibold tracking-wide {inboxItemKind(
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
                  class="text-[13px] font-semibold text-[var(--fg)] leading-snug"
                >
                  {item.title}
                </h3>
                {#if getInboxSubjectLabel(item)}
                  <a
                    class="shrink-0 rounded bg-[var(--line)] px-1.5 py-0.5 text-[11px] font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] hover:text-[var(--fg)]"
                    href={inboxItemHref(item)}
                  >
                    {getInboxSubjectLabel(item)}
                  </a>
                {/if}
              </div>
              {#if isAskItem(item)}
                <div
                  class="mt-1 text-[12px] text-[var(--fg-muted)]"
                  data-testid={`inbox-ask-meta-${item.id}`}
                >
                  Asked by
                  <span class="font-mono text-[13px] text-[var(--fg)]">
                    {askActorLabel(item)}
                  </span>
                  {#if item.age_label}
                    &middot; asked {item.age_label.replace(" old", "")} ago
                  {/if}
                </div>
              {/if}

              {#if getInboxSubjectRef(item) || (item.related_refs ?? []).length > 0}
                <div
                  class="mt-1.5 flex flex-wrap items-center gap-1 text-[11px]"
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
                  {#each isAskItem(item) ? (item.related_refs ?? []).slice(0, 2) : (item.related_refs ?? []) as refValue}
                    <RefLink
                      {refValue}
                      threadId={inboxActionThreadId(item)}
                      humanize
                    />
                  {/each}
                </div>
              {/if}

              <div class="mt-2 flex flex-wrap items-center gap-1.5">
                {#if isAskItem(item)}
                  <a
                    class="cursor-pointer rounded bg-accent px-2.5 py-1 text-[12px] font-medium text-white transition-colors hover:bg-accent-hover"
                    href={askItemHref(item)}
                  >
                    Answer
                  </a>
                  <button
                    class="cursor-pointer rounded border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1 text-[12px] font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] disabled:opacity-50"
                    disabled={Boolean(ackInFlightById[item.id])}
                    onclick={() => acknowledgeItem(item)}
                    type="button"
                  >
                    {ackInFlightById[item.id] ? "Deferring..." : "Defer"}
                  </button>
                  <button
                    class="cursor-pointer rounded border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1 text-[12px] font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] disabled:opacity-50"
                    disabled={Boolean(ackInFlightById[item.id])}
                    onclick={() => acknowledgeItem(item)}
                    type="button"
                  >
                    Not for me
                  </button>
                {:else}
                  <button
                    class="cursor-pointer rounded border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1 text-[12px] font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] disabled:opacity-50"
                    disabled={Boolean(ackInFlightById[item.id])}
                    onclick={() => acknowledgeItem(item)}
                    type="button"
                  >
                    {ackInFlightById[item.id]
                      ? "Acknowledging..."
                      : "Acknowledge"}
                  </button>
                  <button
                    class="cursor-pointer rounded px-2.5 py-1 text-[12px] font-medium transition-colors {getDecisionForm(
                      item.id,
                    ).open
                      ? 'border border-[var(--line)] bg-[var(--bg-soft)] text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]'
                      : 'bg-accent text-white hover:bg-accent-hover'}"
                    onclick={() =>
                      toggleDecisionForm(item, !getDecisionForm(item.id).open)}
                    type="button"
                  >
                    {getDecisionForm(item.id).open ? "Cancel" : "Decide"}
                  </button>
                {/if}
              </div>

              {#if postedDecisionByInboxItem[item.id]}
                <div
                  class="mt-2 flex items-center gap-2 rounded-md bg-ok-soft px-3 py-2 text-[12px] text-ok-text"
                >
                  <svg
                    class="h-3.5 w-3.5 shrink-0"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2.5"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M5 13l4 4L19 7"
                    />
                  </svg>
                  <span>
                    Decision recorded &mdash;
                    <a
                      class="font-medium underline hover:text-ok-text"
                      href={`${inboxItemHref(item)}#event-${postedDecisionByInboxItem[item.id].id}`}
                    >
                      view in timeline
                    </a>
                  </span>
                </div>
              {/if}

              {#if getDecisionForm(item.id).open}
                <div
                  class="mt-3 grid grid-cols-1 md:grid-cols-[3fr_2fr] gap-3"
                  data-testid={`decision-panel-${item.id}`}
                >
                  {#if getInboxSubjectRef(item)}
                    {@const subjectRef = getInboxSubjectRef(item)}
                    <div
                      class="rounded-md border border-[var(--line)] bg-[var(--panel)] p-3 min-w-0"
                    >
                      {#if subjectContextLoading[subjectRef]}
                        <div
                          class="flex items-center gap-2 text-[12px] text-[var(--fg-muted)] py-4 justify-center"
                        >
                          <svg
                            class="h-3.5 w-3.5 animate-spin"
                            fill="none"
                            viewBox="0 0 24 24"
                          >
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
                          Loading subject context…
                        </div>
                      {:else if subjectContextErrors[subjectRef]}
                        <div class="text-[12px] text-danger-text py-2">
                          Failed to load subject: {subjectContextErrors[
                            subjectRef
                          ]}
                        </div>
                      {:else if subjectContextCache[subjectRef]}
                        {@const ctx = subjectContextCache[subjectRef]}
                        {@const subject = ctx.subject}
                        {@const related = ctx.related}
                        <div class="flex items-center gap-2 mb-2">
                          <h4
                            class="text-[13px] font-semibold text-[var(--fg)] truncate min-w-0"
                          >
                            {subject?.title ??
                              subject?.summary ??
                              subject?.current_summary ??
                              subject?.id ??
                              getInboxSubjectLabel(item)}
                          </h4>
                          <span
                            class="shrink-0 rounded-md border border-[var(--line)] px-1.5 py-0.5 text-[11px] font-medium capitalize {subject?.status ===
                            'active'
                              ? 'text-ok-text'
                              : subject?.status === 'blocked' ||
                                  subject?.status === 'paused'
                                ? 'text-warn-text'
                                : subject?.status === 'archived'
                                  ? 'text-[var(--fg-muted)]'
                                  : 'text-[var(--fg-muted)]'}"
                          >
                            {subject?.status ?? "unknown"}
                          </span>
                          {#if subject?.type}
                            <span
                              class="shrink-0 text-[11px] font-medium text-[var(--fg-muted)] uppercase"
                              >{subject.type}</span
                            >
                          {/if}
                        </div>

                        {#if subject?.summary || subject?.current_summary}
                          <div
                            class="mb-2 text-[12px] text-[var(--fg)] leading-relaxed"
                          >
                            <MarkdownRenderer
                              source={subject.summary ??
                                subject.current_summary}
                              class="text-[12px]"
                            />
                          </div>
                        {/if}

                        <div class="flex flex-wrap gap-1.5">
                          {#if subject?.board_ref}
                            <span
                              class="rounded bg-[var(--line)] px-1.5 py-0.5 text-[11px] text-[var(--fg-muted)]"
                            >
                              Board: {subject.board_ref}
                            </span>
                          {/if}
                          {#if subject?.topic_ref}
                            <span
                              class="rounded bg-[var(--line)] px-1.5 py-0.5 text-[11px] text-[var(--fg-muted)]"
                            >
                              Topic: {subject.topic_ref}
                            </span>
                          {/if}
                          {#if subject?.document_ref}
                            <span
                              class="rounded bg-[var(--line)] px-1.5 py-0.5 text-[11px] text-[var(--fg-muted)]"
                            >
                              Doc: {subject.document_ref}
                            </span>
                          {/if}
                          {#if related?.board_summary}
                            <span
                              class="rounded bg-[var(--line)] px-1.5 py-0.5 text-[11px] text-[var(--fg-muted)]"
                            >
                              {related.board_summary.card_count ?? 0} cards
                            </span>
                          {/if}
                        </div>

                        {#if Array.isArray(subject?.related_refs) && subject.related_refs.length > 0}
                          <div
                            class="border-t border-[var(--line)] pt-2 mt-2"
                          >
                            <p
                              class="text-[11px] font-medium text-[var(--fg-muted)] uppercase tracking-wide mb-1.5"
                            >
                              Related refs
                            </p>
                            <div class="flex flex-wrap gap-1.5">
                              {#each subject.related_refs as refValue}
                                <RefLink
                                  {refValue}
                                  threadId={subject?.thread_id ??
                                    inboxActionThreadId(item)}
                                  humanize
                                />
                              {/each}
                            </div>
                          </div>
                        {/if}

                        <div
                          class="border-t border-[var(--line)] pt-2 mt-2"
                        >
                          <a
                            class="inline-flex items-center gap-1 text-[12px] font-medium text-accent-text hover:text-accent-text transition-colors"
                            href={inboxItemHref(item)}
                          >
                            View subject &rarr;
                          </a>
                        </div>
                      {/if}
                    </div>
                  {/if}

                  <form
                    class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] p-3 {inboxActionThreadId(
                      item,
                    )
                      ? ''
                      : 'md:col-span-2'}"
                    data-testid={`decision-form-${item.id}`}
                    onsubmit={(event) => {
                      event.preventDefault();
                      void recordDecision(item);
                    }}
                  >
                    <label
                      class="block text-[12px] font-medium text-[var(--fg-muted)]"
                      for={`decision-summary-${item.id}`}
                    >
                      Your decision
                    </label>
                    <input
                      class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-[13px] transition-colors"
                      id={`decision-summary-${item.id}`}
                      oninput={(event) =>
                        updateDecisionField(
                          item.id,
                          "summary",
                          event.currentTarget.value,
                        )}
                      placeholder="e.g., Approved emergency reorder of 500 units"
                      value={getDecisionForm(item.id).summary}
                    />
                    {#if getDecisionFormError(item.id)}
                      <p class="mt-1 text-[11px] text-danger-text">
                        {getDecisionFormError(item.id)}
                      </p>
                    {/if}
                    <label
                      class="mt-2 block text-[12px] font-medium text-[var(--fg-muted)]"
                      for={`decision-notes-${item.id}`}
                    >
                      Rationale <span
                        class="font-normal text-[var(--fg-muted)]"
                        >optional</span
                      >
                    </label>
                    <textarea
                      class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-[13px] transition-colors"
                      id={`decision-notes-${item.id}`}
                      oninput={(event) =>
                        updateDecisionField(
                          item.id,
                          "notes",
                          event.currentTarget.value,
                        )}
                      placeholder="Why this choice? Any constraints, trade-offs, or follow-ups..."
                      rows="2">{getDecisionForm(item.id).notes}</textarea
                    >
                    <div class="mt-2 flex justify-end">
                      <button
                        class="cursor-pointer rounded-md bg-accent px-3 py-1.5 text-[12px] font-medium text-white hover:bg-accent-hover disabled:opacity-50"
                        disabled={Boolean(decisionInFlightById[item.id])}
                        type="submit"
                      >
                        {decisionInFlightById[item.id]
                          ? "Recording..."
                          : "Submit decision"}
                      </button>
                    </div>
                  </form>
                </div>
              {/if}
            </article>
          {/each}
        </div>
      </section>
    {/each}
  </div>
{/if}
