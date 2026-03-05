<script>
  import { onMount } from "svelte";

  import RefLink from "$lib/components/RefLink.svelte";
  import { coreClient } from "$lib/coreClient";
  import { groupInboxItems, getInboxCategoryLabel } from "$lib/inboxUtils";

  let loading = $state(false);
  let error = $state("");
  let items = $state([]);
  let ackInFlightById = $state({});
  let decisionInFlightById = $state({});
  let decisionFormsById = $state({});
  let postedDecisionByThread = $state({});

  let groupedItems = $derived(groupInboxItems(items));
  let totalItems = $derived(items.length);

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

  function toggleDecisionForm(itemId, open) {
    decisionFormsById = {
      ...decisionFormsById,
      [itemId]: {
        ...getDecisionForm(itemId),
        open,
      },
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

    if (!item.thread_id) {
      error = "Cannot record decision: no linked thread.";
      return;
    }

    if (!draft.summary.trim()) {
      error = "Decision summary is required.";
      return;
    }

    decisionInFlightById = { ...decisionInFlightById, [item.id]: true };

    try {
      const response = await coreClient.createEvent({
        event: {
          type: "decision_made",
          thread_id: item.thread_id,
          refs: item.refs ?? [],
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

      postedDecisionByThread = {
        ...postedDecisionByThread,
        [item.thread_id]: response.event,
      };

      toggleDecisionForm(item.id, false);
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
</script>

<div class="flex items-center justify-between">
  <h1 class="text-lg font-semibold text-gray-900">Inbox</h1>
  {#if totalItems > 0}
    <span
      class="rounded-full bg-indigo-100 px-2.5 py-0.5 text-xs font-medium text-indigo-700"
      >{totalItems}</span
    >
  {/if}
</div>

{#if error}
  <p class="mt-3 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">
    {error}
  </p>
{/if}

{#if loading}
  <p class="mt-6 text-sm text-gray-400">Loading inbox...</p>
{:else}
  <div class="mt-4 space-y-5">
    {#each groupedItems as group}
      <section>
        <h2
          class="mb-2 text-xs font-semibold uppercase tracking-wider text-gray-400"
        >
          {getInboxCategoryLabel(group.category)}
        </h2>

        {#if group.items.length === 0}
          <p
            class="rounded-lg border border-gray-200 bg-white px-4 py-3 text-sm text-gray-400"
          >
            Nothing here.
          </p>
        {:else}
          <div
            class="overflow-hidden rounded-lg border border-gray-200 bg-white"
          >
            {#each group.items as item, i}
              <div
                class="border-b border-gray-100 px-4 py-3 {i ===
                group.items.length - 1
                  ? 'border-b-0'
                  : ''}"
              >
                <div class="flex items-start justify-between gap-3">
                  <div class="min-w-0 flex-1">
                    <p class="text-sm font-medium text-gray-900">
                      {item.title}
                    </p>
                    <p class="mt-0.5 text-xs text-gray-500">
                      {item.recommended_action}
                    </p>
                  </div>
                  <div class="flex shrink-0 items-center gap-1.5">
                    <button
                      class="rounded px-2.5 py-1 text-xs font-medium text-gray-600 transition-colors hover:bg-gray-100 disabled:opacity-50"
                      disabled={Boolean(ackInFlightById[item.id])}
                      onclick={() => acknowledgeItem(item)}
                      type="button"
                    >
                      {ackInFlightById[item.id] ? "..." : "Ack"}
                    </button>
                    <button
                      class="rounded px-2.5 py-1 text-xs font-medium text-indigo-600 transition-colors hover:bg-indigo-50"
                      onclick={() =>
                        toggleDecisionForm(
                          item.id,
                          !getDecisionForm(item.id).open,
                        )}
                      type="button"
                    >
                      {getDecisionForm(item.id).open ? "Cancel" : "Decide"}
                    </button>
                  </div>
                </div>

                <div class="mt-2 flex flex-wrap items-center gap-2 text-xs">
                  {#if item.thread_id}
                    <a
                      class="text-indigo-600 hover:text-indigo-800"
                      href={`/threads/${item.thread_id}`}>View thread</a
                    >
                  {/if}
                  {#if item.commitment_id}
                    <a
                      class="text-indigo-600 hover:text-indigo-800"
                      href={item.thread_id
                        ? `/threads/${item.thread_id}#commitment-${item.commitment_id}`
                        : `/threads#commitment-${item.commitment_id}`}
                      >View commitment</a
                    >
                  {/if}
                  {#each item.refs ?? [] as refValue}
                    <RefLink {refValue} threadId={item.thread_id} />
                  {/each}
                </div>

                {#if postedDecisionByThread[item.thread_id]}
                  <p class="mt-2 text-xs text-emerald-600">
                    Decision recorded.
                    <a
                      class="underline"
                      href={`/threads/${item.thread_id}#event-${postedDecisionByThread[item.thread_id].id}`}
                      >View in timeline</a
                    >
                  </p>
                {/if}

                {#if getDecisionForm(item.id).open}
                  <form
                    class="mt-3 rounded-md border border-gray-200 bg-gray-50 p-3"
                    onsubmit={(event) => {
                      event.preventDefault();
                      recordDecision(item);
                    }}
                  >
                    <label
                      class="block text-xs font-medium text-gray-600"
                      for={`decision-summary-${item.id}`}
                    >
                      Decision summary
                    </label>
                    <input
                      class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
                      id={`decision-summary-${item.id}`}
                      oninput={(event) =>
                        updateDecisionField(
                          item.id,
                          "summary",
                          event.currentTarget.value,
                        )}
                      value={getDecisionForm(item.id).summary}
                    />
                    <label
                      class="mt-2 block text-xs font-medium text-gray-600"
                      for={`decision-notes-${item.id}`}
                    >
                      Notes (optional)
                    </label>
                    <textarea
                      class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
                      id={`decision-notes-${item.id}`}
                      oninput={(event) =>
                        updateDecisionField(
                          item.id,
                          "notes",
                          event.currentTarget.value,
                        )}
                      rows="2">{getDecisionForm(item.id).notes}</textarea
                    >
                    <button
                      class="mt-2 rounded bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
                      disabled={Boolean(decisionInFlightById[item.id])}
                      type="submit"
                    >
                      {decisionInFlightById[item.id]
                        ? "Recording..."
                        : "Submit decision"}
                    </button>
                  </form>
                {/if}
              </div>
            {/each}
          </div>
        {/if}
      </section>
    {/each}
  </div>
{/if}
