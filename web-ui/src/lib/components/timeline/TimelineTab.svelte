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
  import ArchiveButton from "$lib/components/ArchiveButton.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import TrashButton from "$lib/components/TrashButton.svelte";
  import { toTimelineView, eventTypeDotClass } from "$lib/timelineUtils";

  let { threadId } = $props();

  const timelineCtx = getTimelineContext();
  const timelineStore = timelineCtx.store;
  let timeline = $derived($timelineStore.timeline);
  let timelineLoading = $derived($timelineStore.timelineLoading);
  let timelineError = $derived($timelineStore.timelineError);

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

<div>
  {#if archivedCount > 0 || (timelineLoading && hasAnyTimelineEvents)}
    <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
      <div class="flex flex-wrap items-center gap-3">
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
  {/if}
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
          class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-4 py-2.5 {event.archived_at
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
            <div class="flex shrink-0 items-center gap-0.5">
              <ArchiveButton
                archived={Boolean(event.archived_at)}
                busy={lifecycleBusy}
                onarchive={() =>
                  (confirmModal = {
                    open: true,
                    action: "archive",
                    eventId: event.id,
                  })}
                onunarchive={() => unarchiveEvent(event.id)}
              />
              <TrashButton
                busy={lifecycleBusy}
                ontrash={() =>
                  (confirmModal = {
                    open: true,
                    action: "trash",
                    eventId: event.id,
                  })}
              />
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
