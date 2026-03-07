<script>
  import { onMount } from "svelte";

  import RefLink from "$lib/components/RefLink.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import {
    enrichInboxItem,
    getInboxCategoryLabel,
    groupInboxItems,
    summarizeInboxUrgency,
  } from "$lib/inboxUtils";

  let loading = $state(false);
  let error = $state("");
  let items = $state([]);
  let ackInFlightById = $state({});
  let decisionInFlightById = $state({});
  let decisionFormsById = $state({});
  let decisionFormErrorsById = $state({});
  let postedDecisionByInboxItem = $state({});
  let urgencyFilter = $state("all");

  let totalItems = $derived(items.length);
  let enrichedItems = $derived(items.map((item) => enrichInboxItem(item)));
  let urgencySummary = $derived(summarizeInboxUrgency(items));
  let filteredItems = $derived(
    enrichedItems.filter((item) => {
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
    const suggestedSummary = existing.summary.trim()
      ? existing.summary
      : `Decision: ${String(item.title ?? "").trim()}`;

    decisionFormsById = {
      ...decisionFormsById,
      [item.id]: {
        ...existing,
        open,
        summary: open ? suggestedSummary : existing.summary,
      },
    };
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

  async function acknowledgeItem(item) {
    const previousItems = items;
    error = "";
    ackInFlightById = { ...ackInFlightById, [item.id]: true };

    items = items.filter((candidate) => candidate.id !== item.id);

    try {
      await coreClient.ackInboxItem({
        thread_id: item.thread_id,
        inbox_item_id: item.id,
      });
    } catch (ackError) {
      const reason =
        ackError instanceof Error ? ackError.message : String(ackError);
      error = `Failed to acknowledge item: ${reason}`;
      items = previousItems;
    } finally {
      ackInFlightById = { ...ackInFlightById, [item.id]: false };
    }
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

  function urgencyBadgeClass(level) {
    if (level === "immediate") {
      return "border-red-200 bg-red-50 text-red-700";
    }
    if (level === "high") {
      return "border-amber-200 bg-amber-50 text-amber-700";
    }
    return "border-slate-200 bg-slate-50 text-slate-600";
  }

  function urgencyCardClass(level) {
    if (level === "immediate") return "border-red-200";
    if (level === "high") return "border-amber-200";
    return "border-slate-200";
  }

  function categoryIcon(category) {
    if (category === "decision_needed") return "decision";
    if (category === "exception") return "exception";
    if (category === "commitment_risk") return "risk";
    return "default";
  }

  function filterButtonClass(filterName) {
    const active = urgencyFilter === filterName;
    if (active) {
      return "border-slate-900 bg-slate-900 text-white";
    }
    return "border-slate-200 bg-white text-slate-700 hover:bg-slate-50 hover:border-slate-300";
  }
</script>

<header
  class="rounded-xl border border-slate-200 bg-white px-6 py-5 shadow-sm mb-6"
>
  <div
    class="flex flex-wrap items-start justify-between gap-4"
    data-testid="inbox-triage-header"
  >
    <div>
      <h1 class="text-2xl font-semibold text-slate-900 tracking-tight">
        Inbox
      </h1>
      <p class="mt-2 text-sm text-slate-600 leading-relaxed">
        Prioritized for human triage. Urgency is inferred from category and
        source event age.
      </p>
    </div>
    <div
      class="inline-flex items-center gap-2 rounded-lg bg-slate-50 px-4 py-2.5"
    >
      <span class="text-xs uppercase tracking-wide font-medium text-slate-500"
        >Open</span
      >
      <span class="text-xl font-semibold text-slate-900">{totalItems}</span>
    </div>
  </div>

  <div class="mt-5 grid gap-3 sm:grid-cols-3">
    <div
      class="rounded-lg border border-red-200 bg-red-50 px-4 py-3"
      data-testid="urgency-summary-immediate"
    >
      <p class="text-xs uppercase tracking-wide font-medium text-red-600">
        Immediate
      </p>
      <p class="mt-1 text-2xl font-semibold text-red-700">
        {urgencySummary.immediate}
      </p>
    </div>
    <div
      class="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3"
      data-testid="urgency-summary-high"
    >
      <p class="text-xs uppercase tracking-wide font-medium text-amber-600">
        High
      </p>
      <p class="mt-1 text-2xl font-semibold text-amber-700">
        {urgencySummary.high}
      </p>
    </div>
    <div
      class="rounded-lg border border-slate-200 bg-slate-50 px-4 py-3"
      data-testid="urgency-summary-normal"
    >
      <p class="text-xs uppercase tracking-wide font-medium text-slate-500">
        Normal
      </p>
      <p class="mt-1 text-2xl font-semibold text-slate-700">
        {urgencySummary.normal}
      </p>
    </div>
  </div>

  <div class="mt-5 flex flex-wrap gap-2" data-testid="inbox-filter-bar">
    <button
      class={`rounded-lg border px-4 py-2 text-sm font-medium transition-all ${filterButtonClass("all")}`}
      onclick={() => {
        urgencyFilter = "all";
      }}
      type="button"
    >
      All ({totalItems})
    </button>
    <button
      class={`rounded-lg border px-4 py-2 text-sm font-medium transition-all ${filterButtonClass("immediate")}`}
      onclick={() => {
        urgencyFilter = "immediate";
      }}
      type="button"
    >
      Immediate ({urgencySummary.immediate})
    </button>
    <button
      class={`rounded-lg border px-4 py-2 text-sm font-medium transition-all ${filterButtonClass("high")}`}
      onclick={() => {
        urgencyFilter = "high";
      }}
      type="button"
    >
      High ({urgencySummary.high})
    </button>
    <button
      class={`rounded-lg border px-4 py-2 text-sm font-medium transition-all ${filterButtonClass("aging")}`}
      onclick={() => {
        urgencyFilter = "aging";
      }}
      type="button"
    >
      Aging 24h+
    </button>
  </div>
</header>

{#if error}
  <div
    class="mb-6 flex items-start gap-3 rounded-lg bg-red-50 px-5 py-4 text-sm text-red-700"
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
    Loading inbox...
  </div>
{:else if totalItems === 0}
  <section
    class="rounded-xl border border-dashed border-slate-300 bg-white px-8 py-12 text-center"
    data-testid="inbox-empty-state"
  >
    <h2 class="text-lg font-semibold text-slate-900">Inbox is clear</h2>
    <p class="mt-2 text-sm text-slate-600 leading-relaxed">
      No triage items are pending right now. New exceptions, risks, or decisions
      will appear here.
    </p>
  </section>
{:else if !hasFilteredItems}
  <section
    class="rounded-xl border border-dashed border-slate-300 bg-white px-8 py-12 text-center"
    data-testid="inbox-filter-empty-state"
  >
    <h2 class="text-lg font-semibold text-slate-900">
      No items match this view
    </h2>
    <p class="mt-2 text-sm text-slate-600 leading-relaxed">
      Try switching back to <span class="font-semibold">All</span> to see the full
      queue.
    </p>
    <button
      class="mt-5 rounded-lg border border-slate-200 bg-white px-4 py-2.5 text-sm font-medium text-slate-700 transition-all hover:bg-slate-50 hover:border-slate-300"
      onclick={() => {
        urgencyFilter = "all";
      }}
      type="button"
    >
      Show all inbox items
    </button>
  </section>
{:else}
  <div class="space-y-6">
    {#each visibleGroups as group}
      <section data-testid={`inbox-group-${group.category}`}>
        <div class="mb-3 flex items-center gap-2">
          {#if categoryIcon(group.category) === "decision"}
            <svg
              class="h-4 w-4 text-indigo-400"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M8.228 9c.549 1.165 2.03 2 3.772 2 2.21 0 4 1.343 4 3 0 1.4-1.278 2.575-3.006 2.907-.542.104-.994.54-.994 1.093m0 3h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
          {:else if categoryIcon(group.category) === "exception"}
            <svg
              class="h-4 w-4 text-red-400"
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
          {:else if categoryIcon(group.category) === "risk"}
            <svg
              class="h-4 w-4 text-amber-400"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
          {:else}
            <svg
              class="h-4 w-4 text-slate-400"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M7 8h10M7 12h4m1 8l-4-4H5a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v8a2 2 0 01-2 2h-3l-4 4z"
              />
            </svg>
          {/if}
          <h2
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            {getInboxCategoryLabel(group.category)}
          </h2>
          <span
            class="inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-slate-100 px-2 text-xs font-medium text-slate-600"
          >
            {group.items.length}
          </span>
        </div>

        <div class="space-y-3">
          {#each group.items as item}
            <article
              class={`rounded-xl border bg-white px-5 py-4 shadow-sm transition-all hover:shadow ${urgencyCardClass(item.urgency_level)}`}
              data-testid={`inbox-card-${item.id}`}
            >
              <div class="flex flex-wrap items-center gap-2">
                <span
                  class={`inline-flex items-center rounded-full border px-3 py-1 text-xs font-semibold ${urgencyBadgeClass(item.urgency_level)}`}
                >
                  {item.urgency_label}
                </span>
                <span
                  class="inline-flex items-center rounded-full border border-slate-200 bg-slate-50 px-3 py-1 text-xs font-medium text-slate-600"
                >
                  {item.age_label}
                </span>
                {#if item.has_source_event_time}
                  <span class="text-xs text-slate-400">
                    Source: {formatTimestamp(item.source_event_time)}
                  </span>
                {/if}
              </div>

              <h3
                class="mt-3 text-base font-semibold leading-tight text-slate-900"
              >
                {item.title}
              </h3>

              {#if item.recommended_action}
                <div
                  class="mt-3 rounded-lg border border-slate-200 bg-slate-50 px-4 py-3"
                >
                  <p
                    class="text-xs font-semibold uppercase tracking-wide text-slate-500"
                  >
                    Recommended action
                  </p>
                  <p class="mt-1 text-sm text-slate-700">
                    {item.recommended_action}
                  </p>
                </div>
              {/if}

              <div class="mt-3 flex flex-wrap items-center gap-2 text-xs">
                {#if item.thread_id}
                  <a
                    class="inline-flex items-center gap-1 font-medium text-slate-600 hover:text-slate-900 transition-colors"
                    href={`/threads/${item.thread_id}`}
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
                        d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1"
                      />
                    </svg>
                    Thread
                  </a>
                {/if}
                {#if item.commitment_id}
                  <a
                    class="inline-flex items-center gap-1 font-medium text-slate-600 hover:text-slate-900 transition-colors"
                    href={item.thread_id
                      ? `/threads/${item.thread_id}#commitment-card-${item.commitment_id}`
                      : `/threads#commitment-card-${item.commitment_id}`}
                  >
                    Commitment
                  </a>
                {/if}
                {#each item.refs ?? [] as refValue}
                  <RefLink {refValue} threadId={item.thread_id} />
                {/each}
              </div>

              <div class="mt-4 flex flex-wrap gap-2">
                <button
                  aria-label="Acknowledge"
                  class="inline-flex items-center rounded-lg border border-slate-200 bg-white px-4 py-2 text-sm font-medium text-slate-700 transition-all hover:bg-slate-50 hover:border-slate-300 disabled:opacity-50"
                  disabled={Boolean(ackInFlightById[item.id])}
                  onclick={() => acknowledgeItem(item)}
                  type="button"
                >
                  {ackInFlightById[item.id] ? "Dismissing..." : "Dismiss"}
                </button>
                <button
                  class="inline-flex items-center rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white transition-all hover:bg-slate-800"
                  onclick={() =>
                    toggleDecisionForm(item, !getDecisionForm(item.id).open)}
                  type="button"
                >
                  {getDecisionForm(item.id).open ? "Close form" : "Decide"}
                </button>
              </div>

              {#if postedDecisionByInboxItem[item.id]}
                <div
                  class="mt-3 flex items-center gap-2 text-sm text-emerald-600"
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
                      d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
                    />
                  </svg>
                  Decision recorded.
                  <a
                    class="font-medium underline decoration-emerald-300"
                    href={`/threads/${item.thread_id}#event-${postedDecisionByInboxItem[item.id].id}`}
                  >
                    View in timeline
                  </a>
                </div>
              {/if}

              {#if getDecisionForm(item.id).open}
                <form
                  class="mt-4 rounded-lg border border-slate-200 bg-slate-50 p-4"
                  data-testid={`decision-form-${item.id}`}
                  onsubmit={(event) => {
                    event.preventDefault();
                    void recordDecision(item);
                  }}
                >
                  <p class="text-sm text-slate-600">
                    Record a clear decision for this inbox item. This creates a
                    `decision_made` event on the linked thread.
                  </p>
                  <label
                    class="mt-3 block text-sm font-medium text-slate-700"
                    for={`decision-summary-${item.id}`}
                  >
                    Decision summary
                  </label>
                  <input
                    class="mt-2 w-full rounded-lg border border-slate-200 bg-white px-4 py-2.5 text-sm transition-all focus:border-slate-300"
                    id={`decision-summary-${item.id}`}
                    oninput={(event) =>
                      updateDecisionField(
                        item.id,
                        "summary",
                        event.currentTarget.value,
                      )}
                    placeholder="What was decided?"
                    value={getDecisionForm(item.id).summary}
                  />
                  {#if getDecisionFormError(item.id)}
                    <p class="mt-1 text-xs text-red-700">
                      {getDecisionFormError(item.id)}
                    </p>
                  {/if}
                  <label
                    class="mt-3 block text-sm font-medium text-slate-700"
                    for={`decision-notes-${item.id}`}
                  >
                    Notes
                    <span class="font-normal text-slate-500">optional</span>
                  </label>
                  <textarea
                    class="mt-2 w-full rounded-lg border border-slate-200 bg-white px-4 py-2.5 text-sm transition-all focus:border-slate-300"
                    id={`decision-notes-${item.id}`}
                    oninput={(event) =>
                      updateDecisionField(
                        item.id,
                        "notes",
                        event.currentTarget.value,
                      )}
                    placeholder="Additional context..."
                    rows="3">{getDecisionForm(item.id).notes}</textarea
                  >
                  <div class="mt-4 flex justify-end">
                    <button
                      class="rounded-lg bg-slate-900 px-5 py-2.5 text-sm font-medium text-white transition-all hover:bg-slate-800 disabled:opacity-50"
                      disabled={Boolean(decisionInFlightById[item.id])}
                      type="submit"
                    >
                      {decisionInFlightById[item.id]
                        ? "Recording..."
                        : "Record decision"}
                    </button>
                  </div>
                </form>
              {/if}
            </article>
          {/each}
        </div>
      </section>
    {/each}
  </div>
{/if}
