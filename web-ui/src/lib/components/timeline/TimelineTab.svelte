<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";
  import { tick } from "svelte";

  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import { emptyTimelineConfirmModal } from "$lib/confirmModal.js";
  import { coreClient } from "$lib/coreClient";
  import {
    scrollAndHighlightTarget,
    timelineTargetFromHash,
  } from "$lib/deepLinkTargets";
  import { getTimelineContext } from "$lib/timelineContext";
  import {
    actorRegistry,
    lookupActorDisplayName,
    principalRegistry,
  } from "$lib/actorSession";
  import { formatTimestamp } from "$lib/formatDate";
  import ArchiveButton from "$lib/components/ArchiveButton.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import LeadingSelectionGlyph from "$lib/components/LeadingSelectionGlyph.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import { buildPrimitiveRefRoutes } from "$lib/refLinkModel";
  import TrashButton from "$lib/components/TrashButton.svelte";
  import WorkspaceListBulkToolbar from "$lib/components/WorkspaceListBulkToolbar.svelte";
  import {
    buildTimelineRefLabelHints,
    normalizeDocumentRevisionsInput,
    toTimelineView,
    eventTypeDotClass,
  } from "$lib/timelineUtils";

  let { threadId, boardId = "", compact = false } = $props();

  const timelineCtx = getTimelineContext();
  const timelineStore = timelineCtx.store;
  let timeline = $derived($timelineStore.timeline);
  let timelineArtifacts = $derived($timelineStore.timelineArtifacts ?? []);
  let timelineCards = $derived($timelineStore.timelineCards ?? []);
  let timelineDocuments = $derived($timelineStore.timelineDocuments ?? []);
  let timelineDocumentRevisions = $derived(
    $timelineStore.timelineDocumentRevisions ?? [],
  );
  let timelineLoading = $derived($timelineStore.timelineLoading);
  let timelineError = $derived($timelineStore.timelineError);

  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );

  let routeMaps = $derived(
    buildPrimitiveRefRoutes({
      artifacts: timelineArtifacts,
      events: timeline,
      cards: timelineCards,
      documents: timelineDocuments,
      threadId,
    }),
  );
  let labelHints = $derived(
    buildTimelineRefLabelHints(
      timelineArtifacts,
      timelineDocuments,
      normalizeDocumentRevisionsInput(timelineDocumentRevisions),
    ),
  );
  let timelineView = $derived(
    toTimelineView(timeline, {
      threadId,
      artifacts: timelineArtifacts,
      cards: timelineCards,
      documents: timelineDocuments,
      documentRevisions: timelineDocumentRevisions,
      labelHints,
      routeMaps,
    }),
  );
  let hasAnyTimelineEvents = $derived(timelineView.length > 0);

  let showArchived = $state(false);
  let timelineSelectMode = $state(false);
  /** @type {Set<string>} */
  let selectedTimelineIds = $state(new Set());
  let confirmModal = $state(emptyTimelineConfirmModal());
  let lifecycleBusy = $state(false);
  let lifecycleError = $state("");
  let handledDeepLinkKey = $state("");

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

  let selectedTimelineEvents = $derived(
    filteredTimeline.filter((e) =>
      selectedTimelineIds.has(String(e?.id ?? "")),
    ),
  );

  let allTimelineVisibleSelected = $derived(
    filteredTimeline.length > 0 &&
      filteredTimeline.every((e) =>
        selectedTimelineIds.has(String(e?.id ?? "")),
      ),
  );

  let bulkTimelineCanArchive = $derived(
    selectedTimelineEvents.some((e) => !e.archived_at),
  );
  let bulkTimelineCanUnarchive = $derived(
    selectedTimelineEvents.some((e) => Boolean(e.archived_at)),
  );

  let confirmBulkCount = $derived(confirmModal.bulkIds?.length ?? 0);
  let confirmTimelineIsBulk = $derived(confirmBulkCount > 0);

  let confirmModalTitle = $derived.by(() => {
    if (!confirmModal.open) return "";
    if (confirmTimelineIsBulk) {
      return confirmModal.action === "trash"
        ? `Move ${confirmBulkCount} events to trash`
        : `Archive ${confirmBulkCount} events`;
    }
    if (confirmModal.action === "trash") {
      return confirmModal.eventRawType === "message_posted"
        ? "Move message to trash"
        : "Move event to trash";
    }
    return confirmModal.eventRawType === "message_posted"
      ? "Archive message"
      : "Archive event";
  });

  let confirmModalMessage = $derived.by(() => {
    if (!confirmModal.open) return "";
    if (confirmTimelineIsBulk) {
      return confirmModal.action === "trash"
        ? `The selected events (${confirmBulkCount}) will be moved to trash. You can restore them later. Posted messages move with their replies.`
        : `The selected events (${confirmBulkCount}) will be hidden from the timeline. Turn on Show archived to see them again.`;
    }
    if (confirmModal.action === "trash") {
      return confirmModal.eventRawType === "message_posted"
        ? "This message and all its replies will be moved to trash. You can restore them later."
        : "This event will be moved to trash. You can restore it later.";
    }
    return confirmModal.eventRawType === "message_posted"
      ? "This message and all its replies will be archived. Toggle 'Show archived' to see them again."
      : "This event will be hidden from the timeline. You can show archived events to see it again.";
  });

  function timelineMetaFull(event) {
    return `${actorName(event.actor_id)} · ${formatTimestamp(event.ts) || "—"}`;
  }

  $effect(() => {
    const valid = new Set(
      filteredTimeline.map((e) => String(e?.id ?? "").trim()).filter(Boolean),
    );
    const next = new Set(
      [...selectedTimelineIds].filter((id) => valid.has(id)),
    );
    if (
      next.size !== selectedTimelineIds.size ||
      [...next].some((id) => !selectedTimelineIds.has(id))
    ) {
      selectedTimelineIds = next;
    }
  });

  $effect(() => {
    if (!browser || !timelineSelectMode) return;
    /** @param {KeyboardEvent} ev */
    function onEsc(ev) {
      if (ev.key !== "Escape") return;
      timelineSelectMode = false;
      selectedTimelineIds = new Set();
    }
    document.addEventListener("keydown", onEsc);
    return () => document.removeEventListener("keydown", onEsc);
  });

  function timelineEventById(eventId) {
    const id = String(eventId ?? "").trim();
    if (!id) return null;
    return timelineView.find((event) => String(event?.id ?? "") === id) ?? null;
  }

  $effect(() => {
    if (!browser) return;
    const target = timelineTargetFromHash($page.url.hash);
    const targetId = String(target.id ?? "").trim();
    if (!targetId) {
      handledDeepLinkKey = "";
      return;
    }

    const event = timelineEventById(targetId);
    if (!event || event.trashed_at) return;
    if (event.archived_at && !showArchived) {
      showArchived = true;
      return;
    }
    if (!filteredTimeline.some((item) => String(item?.id) === targetId)) {
      return;
    }

    const key = `${targetId}:${showArchived ? "archived" : "active"}:${filteredTimeline.length}`;
    if (handledDeepLinkKey === key) return;
    handledDeepLinkKey = key;

    void tick().then(() => {
      const element = document.getElementById(`event-${targetId}`);
      scrollAndHighlightTarget(element);
    });
  });

  async function refreshTimeline() {
    await timelineCtx.refreshTimeline();
  }

  function toggleTimelineSelectMode() {
    timelineSelectMode = !timelineSelectMode;
    if (!timelineSelectMode) selectedTimelineIds = new Set();
  }

  function clearTimelineSelection() {
    selectedTimelineIds = new Set();
  }

  function selectAllVisibleTimelineEvents() {
    selectedTimelineIds = new Set(
      filteredTimeline.map((e) => String(e?.id ?? "").trim()).filter(Boolean),
    );
  }

  /** @param {string} eventId */
  function toggleTimelineEventSelected(eventId) {
    if (lifecycleBusy) return;
    const id = String(eventId ?? "").trim();
    if (!id) return;
    const next = new Set(selectedTimelineIds);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selectedTimelineIds = next;
  }

  async function bulkUnarchiveTimelineEvents(ids) {
    if (!ids.length || lifecycleBusy) return;
    lifecycleBusy = true;
    lifecycleError = "";
    try {
      for (const id of ids) {
        await coreClient.unarchiveEvent(id, {});
      }
      await refreshTimeline();
      clearTimelineSelection();
    } catch (e) {
      lifecycleError = `Unarchive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      lifecycleBusy = false;
    }
  }

  async function bulkArchiveTimelineEvents(ids) {
    if (!ids.length || lifecycleBusy) return;
    lifecycleBusy = true;
    lifecycleError = "";
    try {
      for (const id of ids) {
        await coreClient.archiveEvent(id, {});
      }
      await refreshTimeline();
      clearTimelineSelection();
    } catch (e) {
      lifecycleError = `Archive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      lifecycleBusy = false;
    }
  }

  async function bulkTrashTimelineEvents(ids) {
    if (!ids.length || lifecycleBusy) return;
    lifecycleBusy = true;
    lifecycleError = "";
    try {
      for (const id of ids) {
        await coreClient.trashEvent(id, {});
      }
      await refreshTimeline();
      clearTimelineSelection();
    } catch (e) {
      lifecycleError = `Trash failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      lifecycleBusy = false;
    }
  }

  function handleConfirm() {
    const { action, eventId } = confirmModal;
    const bulkSrc = confirmModal.bulkIds;
    const bulk =
      Array.isArray(bulkSrc) && bulkSrc.length > 0
        ? bulkSrc.map((id) => String(id ?? "").trim()).filter(Boolean)
        : null;

    confirmModal = emptyTimelineConfirmModal();

    if (bulk?.length) {
      if (action === "archive") void bulkArchiveTimelineEvents(bulk);
      else if (action === "trash") void bulkTrashTimelineEvents(bulk);
      return;
    }
    if (action === "archive") void archiveEvent(eventId);
    else if (action === "trash") void trashEvent(eventId);
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

<div class="flex flex-col gap-1">
  {#if hasAnyTimelineEvents}
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex flex-wrap items-center gap-3">
        {#if archivedCount > 0}
          <label class="flex items-center gap-1.5 text-micro text-fg-muted">
            <input
              type="checkbox"
              bind:checked={showArchived}
              class="accent-accent"
            />
            Show archived ({archivedCount})
          </label>
        {/if}
        <button
          aria-pressed={timelineSelectMode}
          class="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-line bg-bg-soft px-2.5 text-micro font-medium text-fg-muted transition-colors hover:bg-line-subtle disabled:cursor-not-allowed disabled:opacity-50"
          disabled={lifecycleBusy || filteredTimeline.length === 0}
          onclick={toggleTimelineSelectMode}
          type="button"
        >
          {timelineSelectMode ? "Done" : "Select"}
        </button>
      </div>
      <div class="min-h-[1rem] text-right" aria-live="polite">
        {#if timelineLoading && hasAnyTimelineEvents}
          <p class="text-micro text-fg-muted">Syncing…</p>
        {/if}
      </div>
    </div>
  {/if}
  {#if timelineError && !hasAnyTimelineEvents}
    <p class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text">
      {timelineError}
    </p>
  {:else if timelineLoading && !hasAnyTimelineEvents}
    <p class="text-meta text-fg-muted">Loading timeline...</p>
  {:else if !hasAnyTimelineEvents}
    <p class="text-meta text-fg-muted">No events yet.</p>
  {:else}
    <div class="flex min-w-0 flex-col gap-1">
      {#if timelineError}
        <p
          class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
        >
          {timelineError}
        </p>
      {/if}
      {#if lifecycleError}
        <p
          class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
        >
          {lifecycleError}
        </p>
      {/if}
      {#if timelineSelectMode}
        <WorkspaceListBulkToolbar
          allVisibleSelected={allTimelineVisibleSelected}
          busy={lifecycleBusy}
          canArchive={bulkTimelineCanArchive}
          canTrash={selectedTimelineEvents.length > 0}
          canUnarchive={bulkTimelineCanUnarchive}
          onArchive={() => {
            const ids = selectedTimelineEvents
              .filter((e) => !e.archived_at)
              .map((e) => String(e.id))
              .filter(Boolean);
            if (!ids.length) return;
            confirmModal = {
              open: true,
              action: "archive",
              eventId: "",
              eventRawType: "",
              bulkIds: ids,
            };
          }}
          onClear={clearTimelineSelection}
          onDeselectAll={clearTimelineSelection}
          onSelectAll={selectAllVisibleTimelineEvents}
          onTrash={() => {
            const ids = selectedTimelineEvents
              .map((e) => String(e.id))
              .filter(Boolean);
            if (!ids.length) return;
            confirmModal = {
              open: true,
              action: "trash",
              eventId: "",
              eventRawType: "",
              bulkIds: ids,
            };
          }}
          onUnarchive={() => {
            const ids = selectedTimelineEvents
              .filter((e) => Boolean(e.archived_at))
              .map((e) => String(e.id))
              .filter(Boolean);
            if (!ids.length) return;
            void bulkUnarchiveTimelineEvents(ids);
          }}
          selectedCount={selectedTimelineEvents.length}
          selectionChromeActive={true}
        />
      {/if}
      <ul
        class="flex min-w-0 flex-col divide-y divide-line overflow-hidden rounded-md border border-line bg-panel"
      >
        {#each filteredTimeline as event (event.id)}
          {@const eventSelected = selectedTimelineIds.has(String(event.id))}
          <li
            class="group relative flex min-w-0 {compact
              ? 'gap-1.5 px-2 py-1.5'
              : 'gap-2 px-3 py-2.5'} {event.archived_at ? 'opacity-60' : ''}"
            id={`event-${event.id}`}
          >
            {#if timelineSelectMode}
              <div
                aria-label={eventSelected ? "Deselect event" : "Select event"}
                aria-pressed={eventSelected}
                class="flex w-7 shrink-0 cursor-pointer items-start justify-center {compact
                  ? 'pt-0.5'
                  : 'pt-1'}"
                onclick={() => toggleTimelineEventSelected(event.id)}
                onkeydown={(e) => {
                  if (e.key !== "Enter" && e.key !== " ") return;
                  e.preventDefault();
                  toggleTimelineEventSelected(event.id);
                }}
                role="button"
                tabindex="0"
              >
                <LeadingSelectionGlyph selected={eventSelected} />
              </div>
            {/if}
            <div
              class="flex min-w-0 flex-1 flex-col {compact
                ? 'gap-0.5'
                : 'gap-1'}"
            >
              <div
                class="flex min-w-0 items-baseline {compact
                  ? 'gap-1.5'
                  : 'gap-2'}"
              >
                <span
                  class="relative top-px h-1.5 w-1.5 shrink-0 rounded-full {eventTypeDotClass(
                    event.rawType,
                  )}"
                  title={event.typeLabel}
                  aria-hidden="true"
                ></span>
                <p
                  class="min-w-0 flex-1 truncate font-semibold tracking-tight text-fg {compact
                    ? 'text-[11px] leading-tight'
                    : 'text-[12px]'}"
                  title={event.headline}
                >
                  {event.headline}
                </p>
                <span
                  class="max-w-[42%] shrink-0 truncate text-[11px] text-fg-muted"
                  title={timelineMetaFull(event)}
                >
                  {#if compact}
                    <span class="tabular-nums"
                      >{formatTimestamp(event.ts) || "—"}</span
                    ><span class="text-fg-muted"
                      >{" "}· {actorName(event.actor_id)}</span
                    >
                  {:else}
                    {actorName(event.actor_id)} · {formatTimestamp(event.ts) ||
                      "—"}
                  {/if}
                </span>
                {#if !timelineSelectMode}
                  <div
                    class="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100"
                  >
                    <ArchiveButton
                      archived={Boolean(event.archived_at)}
                      busy={lifecycleBusy}
                      onarchive={() =>
                        (confirmModal = {
                          open: true,
                          action: "archive",
                          eventId: event.id,
                          eventRawType: event.rawType,
                          bulkIds: null,
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
                          eventRawType: event.rawType,
                          bulkIds: null,
                        })}
                    />
                  </div>
                {/if}
              </div>

              <div class={compact ? "mt-0 pl-2.5" : "mt-1 pl-3.5"}>
                {#if event.changePreviewLines?.length}
                  <div
                    class="flex min-w-0 flex-col {compact
                      ? 'gap-0 text-[11px]'
                      : 'gap-0.5 text-[12px]'} leading-snug text-fg-muted"
                  >
                    {#each event.changePreviewLines as line (line)}
                      <p class="break-words">{line}</p>
                    {/each}
                  </div>
                {:else if event.useMarkdownSummary}
                  <MarkdownRenderer
                    source={event.summary}
                    class="{compact
                      ? 'text-[11px] leading-snug'
                      : 'text-[12px] leading-snug'} text-fg {compact
                      ? 'line-clamp-2'
                      : ''}"
                  />
                {:else if String(event.summary ?? "").trim() && String(event.summary ?? "").trim() !== event.headline}
                  <p
                    class="whitespace-pre-wrap break-words leading-snug text-fg-muted {compact
                      ? 'text-[11px] line-clamp-2'
                      : 'text-[12px]'}"
                  >
                    {String(event.summary ?? "").trim()}
                  </p>
                {/if}

                {#if event.refs.length > 0}
                  <details
                    class="{compact
                      ? 'mt-0.5'
                      : 'mt-1.5'} [&_summary::-webkit-details-marker]:hidden"
                  >
                    <summary
                      class="inline-flex cursor-pointer select-none items-center gap-1 text-[11px] text-fg-muted hover:text-fg"
                    >
                      <span
                        aria-hidden="true"
                        class="inline-block transition-transform">▸</span
                      >
                      {event.refs.length}
                      {event.refs.length === 1 ? "ref" : "refs"}
                    </summary>
                    <ul
                      class="{compact
                        ? 'mt-1'
                        : 'mt-1.5'} flex min-w-0 flex-col gap-1 border-l border-line pl-2"
                    >
                      {#each event.refs as refValue (refValue)}
                        <li class="min-w-0">
                          <RefLink
                            variant="compact"
                            refValue={String(refValue)}
                            {threadId}
                            {boardId}
                            humanize
                            showRaw
                            {labelHints}
                            artifactRoutesById={routeMaps.artifactRoutesById}
                            eventRoutesById={routeMaps.eventRoutesById}
                            attachmentChipSize="compact"
                          />
                        </li>
                      {/each}
                    </ul>
                  </details>
                {/if}

                {#if !event.isKnownType}
                  <details class={compact ? "mt-0.5" : "mt-1.5"}>
                    <summary
                      class="cursor-pointer text-[11px] text-fg-muted hover:text-fg"
                      >Details</summary
                    >
                    <pre
                      class="mt-1 overflow-auto rounded bg-bg-soft p-2 text-[11px] text-fg-muted">{JSON.stringify(
                        event.payload ?? {},
                        null,
                        2,
                      )}</pre>
                  </details>
                {/if}
              </div>
            </div>
          </li>
        {/each}
      </ul>
    </div>
  {/if}
</div>

<ConfirmModal
  open={confirmModal.open}
  title={confirmModalTitle}
  message={confirmModalMessage}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={lifecycleBusy}
  onconfirm={handleConfirm}
  oncancel={() => (confirmModal = emptyTimelineConfirmModal())}
/>
