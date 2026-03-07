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

  function filterButtonClass(filterName) {
    const active = urgencyFilter === filterName;
    if (active) {
      return "bg-gray-300 text-gray-900";
    }
    return "bg-gray-100 text-gray-600 hover:bg-gray-200";
  }
</script>

<div class="flex items-baseline justify-between gap-4 mb-4">
  <div>
    <h1 class="text-lg font-semibold text-gray-900">Inbox</h1>
    <p class="text-[13px] text-gray-500">
      Prioritized for human triage. Urgency is inferred from category and source event age.
    </p>
  </div>
  <span
    class="inline-flex items-center gap-1.5 rounded-md bg-gray-200 px-2.5 py-1.5 text-[13px] font-semibold text-gray-700"
    data-testid="inbox-triage-header"
  >
    {totalItems} open
  </span>
</div>

<div class="flex gap-2 mb-4" data-testid="urgency-summary-immediate">
  <div class="flex-1 rounded-md border border-gray-200 bg-gray-100 px-3 py-2">
    <p class="text-[11px] font-medium text-red-400">Immediate</p>
    <p class="text-lg font-semibold text-gray-900">{urgencySummary.immediate}</p>
  </div>
  <div class="flex-1 rounded-md border border-gray-200 bg-gray-100 px-3 py-2" data-testid="urgency-summary-high">
    <p class="text-[11px] font-medium text-amber-400">High</p>
    <p class="text-lg font-semibold text-gray-900">{urgencySummary.high}</p>
  </div>
  <div class="flex-1 rounded-md border border-gray-200 bg-gray-100 px-3 py-2" data-testid="urgency-summary-normal">
    <p class="text-[11px] font-medium text-gray-400">Normal</p>
    <p class="text-lg font-semibold text-gray-900">{urgencySummary.normal}</p>
  </div>
</div>

<div class="flex flex-wrap gap-1.5 mb-5" data-testid="inbox-filter-bar">
  {#each [["all", `All (${totalItems})`], ["immediate", `Immediate (${urgencySummary.immediate})`], ["high", `High (${urgencySummary.high})`], ["aging", "Aging 24h+"]] as [value, label]}
    <button
      class="rounded-md border border-gray-200 px-2.5 py-1.5 text-[12px] font-medium transition-colors {filterButtonClass(value)}"
      onclick={() => { urgencyFilter = value; }}
      type="button"
    >
      {label}
    </button>
  {/each}
</div>

{#if error}
  <div class="mb-4 rounded-md bg-red-500/10 px-3 py-2.5 text-[13px] text-red-400">
    {error}
  </div>
{/if}

{#if loading}
  <div class="mt-12 flex items-center justify-center gap-2 text-[13px] text-gray-400">
    <svg class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
      <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
      <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
    </svg>
    Loading inbox...
  </div>
{:else if totalItems === 0}
  <div class="mt-8 text-center py-8" data-testid="inbox-empty-state">
    <h2 class="text-[13px] font-semibold text-gray-900">Inbox is clear</h2>
    <p class="mt-1 text-[13px] text-gray-500">
      No triage items are pending. New exceptions, risks, or decisions will appear here.
    </p>
  </div>
{:else if !hasFilteredItems}
  <div class="mt-8 text-center py-8" data-testid="inbox-filter-empty-state">
    <h2 class="text-[13px] font-semibold text-gray-900">No items match this view</h2>
    <p class="mt-1 text-[13px] text-gray-500">
      Try switching back to <span class="font-semibold">All</span> to see the full queue.
    </p>
    <button
      class="mt-3 rounded-md border border-gray-200 bg-gray-100 px-3 py-1.5 text-[13px] font-medium text-gray-600 hover:bg-gray-200"
      onclick={() => { urgencyFilter = "all"; }}
      type="button"
    >
      Show all
    </button>
  </div>
{:else}
  <div class="space-y-5">
    {#each visibleGroups as group}
      <section data-testid={`inbox-group-${group.category}`}>
        <div class="mb-2 flex items-center gap-2">
          <h2 class="text-[12px] font-semibold uppercase tracking-wide text-gray-400">
            {getInboxCategoryLabel(group.category)}
          </h2>
          <span class="text-[11px] text-gray-300">{group.items.length}</span>
        </div>

        <div class="space-y-2">
          {#each group.items as item}
            <article
              class="rounded-md border border-gray-200 border-l-[3px] bg-gray-100 px-4 py-3 {urgencyBorderClass(item.urgency_level)}"
              data-testid={`inbox-card-${item.id}`}
            >
              <div class="flex items-center gap-2 text-[11px]">
                <span class="inline-flex h-1.5 w-1.5 rounded-full {urgencyDot(item.urgency_level)}"></span>
                <span class="font-medium text-gray-500">{item.urgency_label}</span>
                <span class="text-gray-300">{item.age_label}</span>
                {#if item.has_source_event_time}
                  <span class="text-gray-300">
                    {formatTimestamp(item.source_event_time)}
                  </span>
                {/if}
              </div>

              <h3 class="mt-1.5 text-[13px] font-semibold text-gray-900 leading-snug">
                {item.title}
              </h3>

              {#if item.recommended_action}
                <div class="mt-2 rounded bg-gray-50 px-3 py-2">
                  <p class="text-[11px] font-medium text-gray-400 uppercase tracking-wide">
                    Recommended
                  </p>
                  <p class="mt-0.5 text-[13px] text-gray-700">{item.recommended_action}</p>
                </div>
              {/if}

              <div class="mt-2 flex flex-wrap items-center gap-2 text-[11px]">
                {#if item.thread_id}
                  <a
                    class="font-medium text-gray-500 hover:text-gray-700 transition-colors"
                    href={`/threads/${item.thread_id}`}
                  >Thread</a>
                {/if}
                {#if item.commitment_id}
                  <a
                    class="font-medium text-gray-500 hover:text-gray-700 transition-colors"
                    href={item.thread_id ? `/threads/${item.thread_id}#commitment-card-${item.commitment_id}` : `/threads#commitment-card-${item.commitment_id}`}
                  >Commitment</a>
                {/if}
                {#each item.refs ?? [] as refValue}
                  <RefLink {refValue} threadId={item.thread_id} />
                {/each}
              </div>

              <div class="mt-3 flex items-center gap-2">
                <button
                  aria-label="Acknowledge"
                  class="rounded-md border border-gray-200 bg-gray-100 px-3 py-1.5 text-[12px] font-medium text-gray-600 transition-colors hover:bg-gray-200 disabled:opacity-50"
                  disabled={Boolean(ackInFlightById[item.id])}
                  onclick={() => acknowledgeItem(item)}
                  type="button"
                >
                  {ackInFlightById[item.id] ? "Dismissing..." : "Dismiss"}
                </button>
                <button
                  class="rounded-md bg-gray-200 px-3 py-1.5 text-[12px] font-medium text-gray-900 transition-colors hover:bg-gray-300"
                  onclick={() => toggleDecisionForm(item, !getDecisionForm(item.id).open)}
                  type="button"
                >
                  {getDecisionForm(item.id).open ? "Close form" : "Decide"}
                </button>
              </div>

              {#if postedDecisionByInboxItem[item.id]}
                <div class="mt-2 text-[12px] text-emerald-400">
                  Decision recorded.
                  <a
                    class="font-medium underline"
                    href={`/threads/${item.thread_id}#event-${postedDecisionByInboxItem[item.id].id}`}
                  >
                    View in timeline
                  </a>
                </div>
              {/if}

              {#if getDecisionForm(item.id).open}
                <form
                  class="mt-3 rounded-md border border-gray-200 bg-gray-50 p-3"
                  data-testid={`decision-form-${item.id}`}
                  onsubmit={(event) => { event.preventDefault(); void recordDecision(item); }}
                >
                  <p class="text-[12px] text-gray-500 mb-2">
                    Record a decision for this item. Creates a `decision_made` event on the linked thread.
                  </p>
                  <label class="block text-[12px] font-medium text-gray-600" for={`decision-summary-${item.id}`}>
                    Decision summary
                  </label>
                  <input
                    class="mt-1 w-full rounded-md border border-gray-200 bg-gray-100 px-3 py-2 text-[13px] transition-colors"
                    id={`decision-summary-${item.id}`}
                    oninput={(event) => updateDecisionField(item.id, "summary", event.currentTarget.value)}
                    placeholder="What was decided?"
                    value={getDecisionForm(item.id).summary}
                  />
                  {#if getDecisionFormError(item.id)}
                    <p class="mt-1 text-[11px] text-red-400">{getDecisionFormError(item.id)}</p>
                  {/if}
                  <label class="mt-2 block text-[12px] font-medium text-gray-600" for={`decision-notes-${item.id}`}>
                    Notes <span class="font-normal text-gray-400">optional</span>
                  </label>
                  <textarea
                    class="mt-1 w-full rounded-md border border-gray-200 bg-gray-100 px-3 py-2 text-[13px] transition-colors"
                    id={`decision-notes-${item.id}`}
                    oninput={(event) => updateDecisionField(item.id, "notes", event.currentTarget.value)}
                    placeholder="Additional context..."
                    rows="2"
                  >{getDecisionForm(item.id).notes}</textarea>
                  <div class="mt-2 flex justify-end">
                    <button
                      class="rounded-md bg-gray-200 px-3 py-1.5 text-[12px] font-medium text-gray-900 hover:bg-gray-300 disabled:opacity-50"
                      disabled={Boolean(decisionInFlightById[item.id])}
                      type="submit"
                    >
                      {decisionInFlightById[item.id] ? "Recording..." : "Record decision"}
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
