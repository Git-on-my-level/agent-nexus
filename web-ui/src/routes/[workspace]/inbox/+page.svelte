<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { onMount } from "svelte";

  import {
    actorRegistry,
    lookupActorDisplayName,
    principalRegistry,
  } from "$lib/actorSession";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatAbsoluteDateTime, formatTimestamp } from "$lib/formatDate";
  import { workspacePath } from "$lib/workspacePaths";
  import {
    INBOX_CATEGORY_ORDER,
    INBOX_CATEGORY_LABELS,
    INBOX_CATEGORY_DESCRIPTIONS,
    INBOX_URGENCY_LEVELS,
    INBOX_URGENCY_LABELS,
    enrichInboxItem,
    getInboxCategoryLabel,
    getInboxUrgencyLabel,
    groupInboxItems,
    summarizeInboxUrgency,
  } from "$lib/inboxUtils";

  let loading = $state(false);
  let error = $state("");
  let items = $state([]);
  let ackInFlightById = $state({});
  let pendingAckById = $state({});
  let decisionInFlightById = $state({});
  let decisionFormsById = $state({});
  let decisionFormErrorsById = $state({});
  let postedDecisionByInboxItem = $state({});
  let urgencyFilter = $state("all");
  let categoryFilter = $state("all");
  let filtersOpen = $state(false);
  let workspaceSlug = $derived($page.params.workspace);
  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );

  let threadContextCache = $state({});
  let threadContextLoading = $state({});
  let threadContextErrors = $state({});

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
      if (urgencyFilter === "aging") {
        return Number.isFinite(item.age_hours) && item.age_hours >= 24;
      }
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

  let activeFilterSummaryParts = $derived.by(() => {
    const parts = [];
    if (categoryFilter !== "all") {
      parts.push(getInboxCategoryLabel(categoryFilter));
    }
    if (urgencyFilter === "aging") {
      parts.push("Aging 24h+");
    } else if (urgencyFilter !== "all") {
      parts.push(getInboxUrgencyLabel(urgencyFilter));
    }
    return parts;
  });

  function workspaceHref(pathname = "/") {
    return workspacePath(workspaceSlug, pathname);
  }

  $effect(() => {
    const params = $page.url.searchParams;
    const rawCategory = String(params.get("category") ?? "").trim();
    const rawUrgency = String(params.get("urgency") ?? "").trim();

    categoryFilter =
      rawCategory && INBOX_CATEGORY_ORDER.includes(rawCategory)
        ? rawCategory
        : "all";

    const validUrgencies = [...INBOX_URGENCY_LEVELS, "aging"];
    urgencyFilter =
      rawUrgency && validUrgencies.includes(rawUrgency) ? rawUrgency : "all";

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

  onMount(async () => {
    await loadInbox();
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

    if (open && item.thread_id) {
      loadThreadContext(item.thread_id);
    }
  }

  async function loadThreadContext(threadId) {
    if (!threadId || threadContextCache[threadId]) return;
    threadContextLoading = { ...threadContextLoading, [threadId]: true };
    try {
      const [threadResponse, commitmentsResponse] = await Promise.all([
        coreClient.getThread(threadId),
        coreClient.listCommitments({ thread_id: threadId, status: "active" }),
      ]);
      threadContextCache = {
        ...threadContextCache,
        [threadId]: {
          thread: threadResponse.thread,
          commitments: commitmentsResponse.commitments ?? [],
        },
      };
    } catch (e) {
      threadContextErrors = {
        ...threadContextErrors,
        [threadId]: e.message || String(e),
      };
    } finally {
      threadContextLoading = { ...threadContextLoading, [threadId]: false };
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

    items = items.filter((candidate) => candidate.id !== item.id);

    const timeoutId = setTimeout(async () => {
      pendingAckById = Object.fromEntries(
        Object.entries(pendingAckById).filter(([k]) => k !== item.id),
      );

      ackInFlightById = { ...ackInFlightById, [item.id]: true };
      try {
        await coreClient.ackInboxItem({
          thread_id: item.thread_id,
          inbox_item_id: item.id,
        });
      } catch (ackError) {
        const reason =
          ackError instanceof Error ? ackError.message : String(ackError);
        error = `Failed to dismiss item: ${reason}`;
        items = [...items, item];
      } finally {
        ackInFlightById = { ...ackInFlightById, [item.id]: false };
      }
    }, 5000);

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

  async function recordDecision(item) {
    const draft = getDecisionForm(item.id);
    error = "";
    setDecisionFormError(item.id, "");

    if (!item.thread_id) {
      error = "Cannot record decision: no linked thread.";
      return;
    }

    if (!draft.summary.trim()) {
      setDecisionFormError(item.id, "Decision summary is required.");
      return;
    }

    decisionInFlightById = { ...decisionInFlightById, [item.id]: true };

    const refs = Array.from(
      new Set([...(item.refs ?? []), `inbox:${item.id}`]),
    );

    try {
      const response = await coreClient.createEvent({
        event: {
          type: "decision_made",
          thread_id: item.thread_id,
          refs,
          summary: draft.summary.trim(),
          payload: {
            notes: draft.notes.trim(),
            inbox_item_id: item.id,
            recommended_action: item.recommended_action ?? "",
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

      toggleDecisionForm(item, false);
      updateDecisionField(item.id, "summary", "");
      updateDecisionField(item.id, "notes", "");
      items = items.filter((candidate) => candidate.id !== item.id);
    } catch (decisionError) {
      const reason =
        decisionError instanceof Error
          ? decisionError.message
          : String(decisionError);
      error = `Failed to record decision: ${reason}`;
    } finally {
      decisionInFlightById = { ...decisionInFlightById, [item.id]: false };
    }
  }

  function urgencyDot(level) {
    if (level === "immediate") return "bg-red-500/100";
    if (level === "high") return "bg-amber-400";
    return "bg-gray-300";
  }

  function urgencyBorderClass(level) {
    if (level === "immediate") return "border-l-red-400";
    if (level === "high") return "border-l-amber-300";
    return "border-l-transparent";
  }

  function urgencyCardClass(level) {
    const active = urgencyFilter === level;
    if (active)
      return "ring-1 ring-[var(--ui-accent)] border-[var(--ui-accent)]";
    return "border-[var(--ui-border)] hover:border-[var(--ui-text-subtle)]";
  }

  function categoryBadgeClass(category) {
    if (category === "decision_needed") return "text-indigo-400";
    if (category === "intervention_needed") return "text-cyan-400";
    if (category === "exception") return "text-red-400";
    if (category === "commitment_risk") return "text-amber-400";
    return "text-[var(--ui-text-muted)]";
  }
</script>

<div class="flex items-center justify-between mb-4">
  <div>
    <h1 class="text-lg font-semibold text-[var(--ui-text)]">Inbox</h1>
    <p class="text-[13px] text-[var(--ui-text-muted)]">
      Sorted by urgency. Oldest items bubble up.
    </p>
  </div>
  <div class="flex items-center gap-1.5">
    <button
      class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)]"
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
      Filters
    </button>
    <span
      class="inline-flex items-center gap-1.5 rounded-md bg-[var(--ui-panel)] px-2.5 py-1.5 text-[13px] font-semibold text-[var(--ui-text)]"
      data-testid="inbox-triage-header"
    >
      {totalItems} open
    </span>
  </div>
</div>

{#if hasActiveFilters}
  <div
    class="mb-4 flex flex-wrap items-center gap-x-2 gap-y-1 text-[12px] text-[var(--ui-text-muted)]"
    data-testid="inbox-active-filters-summary"
  >
    <span class="font-medium text-[var(--ui-text)]">Active filters</span>
    {#each activeFilterSummaryParts as part}
      <span class="text-[var(--ui-text-subtle)]">&middot;</span>
      <span>{part}</span>
    {/each}
  </div>
{/if}

{#if filtersOpen}
  <div
    class="mb-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] p-3"
    data-testid="inbox-filter-panel"
  >
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="text-[12px]">
        <span class="font-medium text-[var(--ui-text-muted)]">Category</span>
        <select
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1.5 text-[13px] transition-colors focus:bg-[var(--ui-panel)]"
          value={categoryFilter}
          onchange={(e) => {
            categoryFilter = e.currentTarget.value;
            applyFilters();
          }}
          data-testid="inbox-category-filter"
        >
          <option value="all">All categories</option>
          {#each INBOX_CATEGORY_ORDER as cat}
            <option value={cat}>
              {INBOX_CATEGORY_LABELS[cat]} — {INBOX_CATEGORY_DESCRIPTIONS[cat]}
            </option>
          {/each}
        </select>
      </label>
      <label class="text-[12px]">
        <span class="font-medium text-[var(--ui-text-muted)]">Urgency</span>
        <select
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1.5 text-[13px] transition-colors focus:bg-[var(--ui-panel)]"
          value={urgencyFilter}
          onchange={(e) => {
            urgencyFilter = e.currentTarget.value;
            applyFilters();
          }}
          data-testid="inbox-urgency-filter"
        >
          <option value="all">All urgency levels</option>
          {#each INBOX_URGENCY_LEVELS as level}
            <option value={level}>{INBOX_URGENCY_LABELS[level]}</option>
          {/each}
          <option value="aging">Aging 24h+</option>
        </select>
      </label>
    </div>
    <div class="mt-3 flex gap-1.5">
      <button
        class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)]"
        onclick={resetFilters}
        type="button"
      >
        Reset
      </button>
    </div>
  </div>
{/if}

{#if error}
  <div
    class="mb-4 rounded-md bg-red-500/10 px-3 py-2.5 text-[13px] text-red-400"
    role="alert"
  >
    {error}
  </div>
{/if}

<div class="flex gap-2 mb-4" data-testid="urgency-summary-strip">
  <button
    class="cursor-pointer flex-1 rounded-md border bg-[var(--ui-bg-soft)] px-3 py-2 text-left transition-colors {urgencyCardClass(
      'immediate',
    )}"
    onclick={() => setUrgencyFromCard("immediate")}
    type="button"
    data-testid="urgency-summary-immediate"
  >
    <p class="text-[11px] font-medium text-red-400">Immediate</p>
    <p class="text-lg font-semibold text-[var(--ui-text)]">
      {urgencySummary.immediate}
    </p>
  </button>
  <button
    class="cursor-pointer flex-1 rounded-md border bg-[var(--ui-bg-soft)] px-3 py-2 text-left transition-colors {urgencyCardClass(
      'high',
    )}"
    onclick={() => setUrgencyFromCard("high")}
    type="button"
    data-testid="urgency-summary-high"
  >
    <p class="text-[11px] font-medium text-amber-400">High</p>
    <p class="text-lg font-semibold text-[var(--ui-text)]">
      {urgencySummary.high}
    </p>
  </button>
  <button
    class="cursor-pointer flex-1 rounded-md border bg-[var(--ui-bg-soft)] px-3 py-2 text-left transition-colors {urgencyCardClass(
      'normal',
    )}"
    onclick={() => setUrgencyFromCard("normal")}
    type="button"
    data-testid="urgency-summary-normal"
  >
    <p class="text-[11px] font-medium text-[var(--ui-text-muted)]">Normal</p>
    <p class="text-lg font-semibold text-[var(--ui-text)]">
      {urgencySummary.normal}
    </p>
  </button>
</div>

{#if Object.keys(pendingAckById).length > 0}
  <div class="mb-4 space-y-1.5">
    {#each Object.values(pendingAckById) as pending}
      <div
        class="flex items-center justify-between gap-3 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[12px] text-[var(--ui-text-muted)]"
      >
        <span class="truncate"
          >Dismissed: <span class="font-medium text-[var(--ui-text)]"
            >{pending.item.title ?? pending.item.summary ?? "item"}</span
          ></span
        >
        <button
          class="cursor-pointer shrink-0 font-medium text-indigo-600 hover:text-indigo-500"
          onclick={() => undoAcknowledge(pending.item.id)}
          type="button"
        >
          Undo
        </button>
      </div>
    {/each}
  </div>
{/if}

{#if loading}
  <div
    class="mt-12 flex items-center justify-center gap-2 text-[13px] text-[var(--ui-text-muted)]"
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
  <div class="mt-8 text-center py-8" data-testid="inbox-empty-state">
    <h2 class="text-[13px] font-semibold text-[var(--ui-text)]">
      Inbox is clear
    </h2>
    <p class="mt-1 text-[13px] text-[var(--ui-text-muted)]">
      Nothing needs attention right now.
    </p>
  </div>
{:else if !hasFilteredItems}
  <div class="mt-8 text-center py-8" data-testid="inbox-filter-empty-state">
    <h2 class="text-[13px] font-semibold text-[var(--ui-text)]">
      No items match this view
    </h2>
    <p class="mt-1 text-[13px] text-[var(--ui-text-muted)]">
      Try switching back to <span class="font-semibold">All</span> to see the full
      queue.
    </p>
    <button
      class="cursor-pointer mt-3 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-1.5 text-[13px] font-medium text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)]"
      onclick={resetFilters}
      type="button"
    >
      Clear filters
    </button>
  </div>
{:else}
  <div class="space-y-5">
    {#each visibleGroups as group}
      <section data-testid={`inbox-group-${group.category}`}>
        <div class="mb-2 flex items-center gap-2">
          <h2
            class="text-[12px] font-semibold uppercase tracking-wide {categoryBadgeClass(
              group.category,
            )}"
          >
            {getInboxCategoryLabel(group.category)}
          </h2>
          <span class="text-[11px] text-[var(--ui-text-subtle)]"
            >{group.items.length}</span
          >
        </div>

        <div class="space-y-2">
          {#each group.items as item}
            <article
              class="rounded-md border border-[var(--ui-border)] border-l-[3px] bg-[var(--ui-bg-soft)] px-4 py-3 {urgencyBorderClass(
                item.urgency_level,
              )}"
              data-testid={`inbox-card-${item.id}`}
            >
              <div class="flex items-center justify-between gap-2 text-[11px]">
                <div class="flex min-w-0 items-center gap-2">
                  <span
                    class="inline-flex h-1.5 w-1.5 shrink-0 rounded-full {urgencyDot(
                      item.urgency_level,
                    )}"
                  ></span>
                  <span class="font-medium text-[var(--ui-text-muted)]"
                    >{item.urgency_label}</span
                  >
                  {#if item.age_label}
                    <span class="text-[var(--ui-text-subtle)]"
                      >{item.age_label}</span
                    >
                  {/if}
                </div>
                {#if item.has_source_event_time}
                  <span
                    class="shrink-0 tabular-nums text-[var(--ui-text-subtle)]"
                    title={item.source_event_time}
                  >
                    {formatAbsoluteDateTime(item.source_event_time)}
                  </span>
                {/if}
              </div>

              <h3
                class="mt-1.5 text-[13px] font-semibold text-[var(--ui-text)] leading-snug"
              >
                {item.title}
              </h3>

              {#if item.recommended_action}
                <div class="mt-2 rounded bg-[var(--ui-bg-soft)] px-3 py-2">
                  <p
                    class="text-[11px] font-medium text-[var(--ui-text-muted)] uppercase tracking-wide"
                  >
                    Recommended
                  </p>
                  <MarkdownRenderer
                    source={item.recommended_action}
                    class="mt-0.5 text-[13px] text-[var(--ui-text)]"
                  />
                </div>
              {/if}

              <div class="mt-2 flex flex-wrap items-center gap-2 text-[11px]">
                {#if item.thread_id}
                  <a
                    class="inline-flex items-center gap-1 font-medium text-[var(--ui-text-muted)] hover:text-[var(--ui-text)] transition-colors"
                    href={workspaceHref(`/threads/${item.thread_id}`)}
                  >
                    <span class="text-[var(--ui-text-subtle)]">Thread:</span>
                    {item.thread_id}
                  </a>
                {/if}
                {#if item.commitment_id}
                  <a
                    class="inline-flex items-center gap-1 font-medium text-[var(--ui-text-muted)] hover:text-[var(--ui-text)] transition-colors"
                    href={item.thread_id
                      ? workspaceHref(
                          `/threads/${item.thread_id}#commitment-card-${item.commitment_id}`,
                        )
                      : workspaceHref(
                          `/threads#commitment-card-${item.commitment_id}`,
                        )}
                  >
                    <span class="text-[var(--ui-text-subtle)]">Commitment:</span
                    >
                    {item.commitment_id}
                  </a>
                {/if}
                {#each item.refs ?? [] as refValue}
                  <RefLink {refValue} threadId={item.thread_id} />
                {/each}
              </div>

              <div class="mt-3 flex items-center gap-2">
                <button
                  class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] disabled:opacity-50"
                  disabled={Boolean(ackInFlightById[item.id])}
                  onclick={() => acknowledgeItem(item)}
                  type="button"
                >
                  {ackInFlightById[item.id] ? "Dismissing..." : "Dismiss"}
                </button>
                <button
                  class="cursor-pointer rounded-md px-3 py-1.5 text-[12px] font-medium transition-colors {getDecisionForm(
                    item.id,
                  ).open
                    ? 'border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)]'
                    : 'bg-indigo-600 text-white hover:bg-indigo-500'}"
                  onclick={() =>
                    toggleDecisionForm(item, !getDecisionForm(item.id).open)}
                  type="button"
                >
                  {getDecisionForm(item.id).open ? "Cancel" : "Decide"}
                </button>
              </div>

              {#if postedDecisionByInboxItem[item.id]}
                <div
                  class="mt-2 flex items-center gap-2 rounded-md bg-emerald-500/10 px-3 py-2 text-[12px] text-emerald-400"
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
                      class="font-medium underline hover:text-emerald-300"
                      href={workspaceHref(
                        `/threads/${item.thread_id}#event-${postedDecisionByInboxItem[item.id].id}`,
                      )}
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
                  {#if item.thread_id}
                    <div
                      class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] p-3 min-w-0"
                    >
                      {#if threadContextLoading[item.thread_id]}
                        <div
                          class="flex items-center gap-2 text-[12px] text-[var(--ui-text-muted)] py-4 justify-center"
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
                          Loading thread context…
                        </div>
                      {:else if threadContextErrors[item.thread_id]}
                        <div class="text-[12px] text-red-400 py-2">
                          Failed to load thread: {threadContextErrors[
                            item.thread_id
                          ]}
                        </div>
                      {:else if threadContextCache[item.thread_id]}
                        {@const ctx = threadContextCache[item.thread_id]}
                        {@const thread = ctx.thread}
                        {@const commitments = ctx.commitments}
                        <div class="flex items-center gap-2 mb-2">
                          <h4
                            class="text-[13px] font-semibold text-[var(--ui-text)] truncate min-w-0"
                          >
                            {thread.title ?? thread.id}
                          </h4>
                          <span
                            class="shrink-0 rounded-md border border-[var(--ui-border)] px-1.5 py-0.5 text-[11px] font-medium capitalize {thread.status ===
                            'active'
                              ? 'text-emerald-400'
                              : thread.status === 'paused'
                                ? 'text-amber-400'
                                : 'text-[var(--ui-text-muted)]'}"
                          >
                            {thread.status ?? "unknown"}
                          </span>
                          {#if thread.priority}
                            <span
                              class="shrink-0 text-[11px] font-medium text-[var(--ui-text-muted)] uppercase"
                              >{thread.priority}</span
                            >
                          {/if}
                        </div>

                        {#if thread.current_summary}
                          <div
                            class="mb-2 text-[12px] text-[var(--ui-text)] leading-relaxed"
                          >
                            <MarkdownRenderer
                              source={thread.current_summary}
                              class="text-[12px]"
                            />
                          </div>
                        {/if}

                        {#if commitments.length > 0}
                          <div
                            class="border-t border-[var(--ui-border)] pt-2 mt-2"
                          >
                            <p
                              class="text-[11px] font-medium text-[var(--ui-text-muted)] uppercase tracking-wide mb-1.5"
                            >
                              Active commitments
                            </p>
                            <div class="space-y-1.5">
                              {#each commitments as commitment}
                                <div
                                  class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1.5 text-[12px]"
                                >
                                  <div class="flex items-center gap-2">
                                    <span
                                      class="font-medium text-[var(--ui-text)] truncate min-w-0"
                                      >{commitment.description ??
                                        commitment.id}</span
                                    >
                                    <span
                                      class="shrink-0 rounded px-1 py-0.5 text-[10px] font-medium capitalize border border-[var(--ui-border)] text-[var(--ui-text-muted)]"
                                      >{commitment.status ?? "open"}</span
                                    >
                                  </div>
                                  <div
                                    class="flex items-center gap-3 mt-0.5 text-[11px] text-[var(--ui-text-muted)]"
                                  >
                                    {#if commitment.owner}
                                      <span
                                        >Owner: {actorName(
                                          commitment.owner,
                                        )}</span
                                      >
                                    {/if}
                                    {#if commitment.due_by}
                                      <span
                                        >Due: {formatTimestamp(
                                          commitment.due_by,
                                        )}</span
                                      >
                                    {/if}
                                  </div>
                                </div>
                              {/each}
                            </div>
                          </div>
                        {/if}

                        <div
                          class="border-t border-[var(--ui-border)] pt-2 mt-2"
                        >
                          <a
                            class="inline-flex items-center gap-1 text-[12px] font-medium text-[var(--ui-accent)] hover:text-[var(--ui-accent-strong)] transition-colors"
                            href={workspaceHref("/threads/" + item.thread_id)}
                          >
                            View full thread &rarr;
                          </a>
                        </div>
                      {/if}
                    </div>
                  {/if}

                  <form
                    class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] p-3 {item.thread_id
                      ? ''
                      : 'md:col-span-2'}"
                    data-testid={`decision-form-${item.id}`}
                    onsubmit={(event) => {
                      event.preventDefault();
                      void recordDecision(item);
                    }}
                  >
                    <label
                      class="block text-[12px] font-medium text-[var(--ui-text-muted)]"
                      for={`decision-summary-${item.id}`}
                    >
                      Your decision
                    </label>
                    <input
                      class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[13px] transition-colors"
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
                      <p class="mt-1 text-[11px] text-red-400">
                        {getDecisionFormError(item.id)}
                      </p>
                    {/if}
                    <label
                      class="mt-2 block text-[12px] font-medium text-[var(--ui-text-muted)]"
                      for={`decision-notes-${item.id}`}
                    >
                      Rationale <span
                        class="font-normal text-[var(--ui-text-muted)]"
                        >optional</span
                      >
                    </label>
                    <textarea
                      class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[13px] transition-colors"
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
                        class="cursor-pointer rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
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
