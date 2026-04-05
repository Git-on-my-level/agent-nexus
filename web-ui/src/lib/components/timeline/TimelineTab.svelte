<script>
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import { coreClient } from "$lib/coreClient";
  import { getTimelineContext } from "$lib/timelineContext";
  import {
    actorRegistry,
    lookupActorDisplayName,
    principalRegistry,
  } from "$lib/actorSession";
  import { formatTimestamp } from "$lib/formatDate";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { toTimelineView, eventTypeDotClass } from "$lib/timelineUtils";

  let { threadId } = $props();

  const timelineCtx = getTimelineContext();
  let timeline = $derived($timelineCtx.store.timeline);
  let timelineLoading = $derived($timelineCtx.store.timelineLoading);
  let timelineError = $derived($timelineCtx.store.timelineError);

  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );

  let timelineView = $derived(toTimelineView(timeline, { threadId }));
  let hasAnyTimelineEvents = $derived(timelineView.length > 0);

  let showArchived = $state(false);
  let confirmModal = $state({ open: false, action: "", eventId: "" });
  let lifecycleBusy = $state(false);
  let lifecycleError = $state("");

  let filteredTimeline = $derived(
    timelineView.filter((event) => {
      if (event.trashed_at) return false;
      if (!showArchived && event.archived_at) return false;
      return true;
    }),
  );

  let archivedCount = $derived(
    timelineView.filter((e) => e.archived_at && !e.trashed_at).length,
  );

  async function refreshTimeline() {
    await timelineCtx.refreshTimeline();
  }

  function handleConfirm() {
    const { action, eventId } = confirmModal;
    confirmModal = { open: false, action: "", eventId: "" };
    if (action === "archive") archiveEvent(eventId);
    else if (action === "trash") trashEvent(eventId);
  }

  async function archiveEvent(eventId) {
    if (!eventId || lifecycleBusy) return;
    lifecycleBusy = true;
    lifecycleError = "";
    try {
      await coreClient.archiveEvent(eventId, {});
      await refreshTimeline();
    } catch (e) {
      lifecycleError = `Archive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      lifecycleBusy = false;
    }
  }

  async function unarchiveEvent(eventId) {
    if (!eventId || lifecycleBusy) return;
    lifecycleBusy = true;
    lifecycleError = "";
    try {
      await coreClient.unarchiveEvent(eventId, {});
      await refreshTimeline();
    } catch (e) {
      lifecycleError = `Unarchive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      lifecycleBusy = false;
    }
  }

  async function trashEvent(eventId) {
    if (!eventId || lifecycleBusy) return;
    lifecycleBusy = true;
    lifecycleError = "";
    try {
      await coreClient.trashEvent(eventId, {});
      await refreshTimeline();
    } catch (e) {
      lifecycleError = `Trash failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      lifecycleBusy = false;
    }
  }
</script>

<div class="mt-4">
  <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
    <div class="flex flex-wrap items-center gap-3">
      <h2
        class="text-[12px] font-semibold uppercase tracking-wider text-[var(--ui-text-muted)]"
      >
        Timeline
      </h2>
      {#if archivedCount > 0}
        <label
          class="flex items-center gap-1.5 text-[11px] text-[var(--ui-text-muted)]"
        >
          <input
            type="checkbox"
            bind:checked={showArchived}
            class="accent-[var(--ui-accent)]"
          />
          Show archived ({archivedCount})
        </label>
      {/if}
    </div>
    <div class="min-h-[1rem] text-right" aria-live="polite">
      {#if timelineLoading && hasAnyTimelineEvents}
        <p class="text-[11px] text-[var(--ui-text-muted)]">Syncing…</p>
      {/if}
    </div>
  </div>
  {#if timelineError && !hasAnyTimelineEvents}
    <p class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
      {timelineError}
    </p>
  {:else if timelineLoading && !hasAnyTimelineEvents}
    <p class="text-[13px] text-[var(--ui-text-muted)]">Loading timeline...</p>
  {:else if !hasAnyTimelineEvents}
    <p class="text-[13px] text-[var(--ui-text-muted)]">No events yet.</p>
  {:else}
    {#if timelineError}
      <p
        class="mb-2 rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400"
      >
        {timelineError}
      </p>
    {/if}
    {#if lifecycleError}
      <p
        class="mb-2 rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400"
      >
        {lifecycleError}
      </p>
    {/if}
    <div class="space-y-1">
      {#each filteredTimeline as event (event.id)}
        <div
          class="group rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-4 py-2.5 {event.archived_at
            ? 'opacity-60'
            : ''}"
          id={`event-${event.id}`}
        >
          <div class="flex items-start justify-between gap-3">
            <div class="flex min-w-0 flex-1 items-start gap-2.5">
              <span
                class="mt-1.5 h-2 w-2 shrink-0 rounded-full {eventTypeDotClass(
                  event.rawType,
                )}"
                title={event.typeLabel}
              ></span>
              <div class="min-w-0 flex-1">
                <MarkdownRenderer
                  source={event.summary}
                  class="text-[13px] text-[var(--ui-text)]"
                />
                <p class="mt-0.5 text-[12px] text-[var(--ui-text-muted)]">
                  {actorName(event.actor_id)} · {event.typeLabel} · {formatTimestamp(
                    event.ts,
                  ) || "—"}
                </p>
              </div>
            </div>
            <div
              class="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100"
            >
              {#if event.archived_at}
                <button
                  aria-label="Unarchive"
                  class="cursor-pointer rounded p-1 text-[var(--ui-text-muted)] hover:bg-[var(--ui-border)] hover:text-[var(--ui-accent)] disabled:opacity-50"
                  disabled={lifecycleBusy}
                  onclick={() => unarchiveEvent(event.id)}
                  type="button"
                >
                  <svg
                    class="h-3.5 w-3.5"
                    fill="currentColor"
                    viewBox="0 0 24 24"
                    aria-hidden="true"
                  >
                    <path
                      d="M20.25 7.5l-.625 10.632a2.25 2.25 0 01-2.247 2.118H6.622a2.25 2.25 0 01-2.247-2.118L3.75 7.5m8.25 3v6.75m0 0l-3-3m3 3l3-3M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125z"
                    />
                  </svg>
                </button>
              {:else}
                <button
                  aria-label="Archive"
                  class="cursor-pointer rounded p-1 text-[var(--ui-text-muted)] hover:bg-[var(--ui-border)] hover:text-[var(--ui-accent)] disabled:opacity-50"
                  disabled={lifecycleBusy}
                  onclick={() =>
                    (confirmModal = {
                      open: true,
                      action: "archive",
                      eventId: event.id,
                    })}
                  type="button"
                >
                  <svg
                    class="h-3.5 w-3.5"
                    fill="currentColor"
                    viewBox="0 0 24 24"
                    aria-hidden="true"
                  >
                    <path
                      d="M20.25 7.5l-.625 10.632a2.25 2.25 0 01-2.247 2.118H6.622a2.25 2.25 0 01-2.247-2.118L3.75 7.5m8.25 3v6.75m0 0l-3-3m3 3l3-3M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125z"
                    />
                  </svg>
                </button>
              {/if}
              <button
                aria-label="Move to trash"
                class="cursor-pointer rounded p-1 text-[var(--ui-text-muted)] hover:bg-[var(--ui-border)] hover:text-red-400 disabled:opacity-50"
                disabled={lifecycleBusy}
                onclick={() =>
                  (confirmModal = {
                    open: true,
                    action: "trash",
                    eventId: event.id,
                  })}
                type="button"
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
                    d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0"
                  />
                </svg>
              </button>
            </div>
          </div>

          {#if event.changedFields.length > 0}
            <div class="mt-1.5 flex flex-wrap gap-1 text-[12px]">
              {#each event.changedFields as field}
                <span
                  class="rounded bg-[var(--ui-border)] px-1.5 py-0.5 text-[var(--ui-text-muted)]"
                  >{field}</span
                >
              {/each}
            </div>
          {/if}

          {#if event.refs.length > 0}
            <div class="mt-1.5 flex flex-wrap gap-1.5 text-[12px]">
              {#each event.refs as refValue}<RefLink
                  {refValue}
                  {threadId}
                />{/each}
            </div>
          {/if}

          {#if !event.isKnownType}
            <details class="mt-1.5">
              <summary
                class="cursor-pointer text-[12px] text-[var(--ui-text-muted)]"
                >Details</summary
              >
              <pre
                class="mt-1 overflow-auto rounded bg-[var(--ui-bg-soft)] p-2 text-[11px] text-[var(--ui-text-muted)]">{JSON.stringify(
                  event.payload ?? {},
                  null,
                  2,
                )}</pre>
            </details>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<ConfirmModal
  open={confirmModal.open}
  title={confirmModal.action === "trash"
    ? "Move event to trash"
    : "Archive event"}
  message={confirmModal.action === "trash"
    ? "This event will be moved to trash. You can restore it later."
    : "This event will be hidden from the timeline. You can show archived events to see it again."}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={lifecycleBusy}
  onconfirm={handleConfirm}
  oncancel={() => (confirmModal = { open: false, action: "", eventId: "" })}
/>
