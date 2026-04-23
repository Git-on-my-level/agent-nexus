<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";
  import { onDestroy, onMount } from "svelte";

  import RefLink from "$lib/components/RefLink.svelte";
  import { coreClient } from "$lib/coreClient";
  import { parseTimestampMs } from "$lib/dateUtils";
  import { buildTimelineRefLabelHints, eventTypeDotClass, toTimelineView } from "$lib/timelineUtils";
  import { filterTopLevelDocuments } from "$lib/documentVisibility";
  import { formatTimestamp } from "$lib/formatDate";
  import {
    buildHomeChangeCards,
    computeNextHomeHandoffMarker,
    filterHomeTimelineEvents,
    readHomeHandoffMarker,
    selectHomeInboxPreview,
    writeHomeHandoffMarker,
  } from "$lib/homeHandoff";
  import {
    getInboxCategoryLabel,
    getInboxSubjectLabel,
    splitTypedRef,
  } from "$lib/inboxUtils";
  import { replayWorkspaceTour } from "$lib/tourState";
  import { workspacePath } from "$lib/workspacePaths";

  const emptySectionState = {
    status: "idle",
    error: "",
    items: [],
  };

  const HOME_REF_PRIORITY = [
    "topic",
    "thread",
    "board",
    "document",
    "artifact",
    "card",
    "inbox",
    "url",
    "event",
  ];

  const POLL_INTERVAL_MS = 30_000;

  let loading = $state(true);
  let refreshedAt = $state("");
  let inboxState = $state({ ...emptySectionState });
  let topicsState = $state({ ...emptySectionState });
  let boardsState = $state({ ...emptySectionState });
  let docsState = $state({ ...emptySectionState });
  let artifactsState = $state({ ...emptySectionState });
  let eventsState = $state({ ...emptySectionState });
  let handoffMarkerIso = $state("");
  let handoffMarkerLoaded = $state(!browser);

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);

  let canReplayTour = $derived(
    topicsState.status === "ready" && topicsState.items.length === 0,
  );

  let homeChangeCards = $derived(
    buildHomeChangeCards({
      inboxItems: inboxState.items,
      topics: topicsState.items,
      boards: boardsState.items,
      documents: docsState.items,
      artifacts: artifactsState.items,
      markerIso: handoffMarkerIso,
    }),
  );

  let handoffTimelineEvents = $derived(
    filterHomeTimelineEvents(eventsState.items, {
      markerIso: handoffMarkerIso,
      limit: 10,
    }),
  );

  let timelineLabelHints = $derived(
    buildHomeRefLabelHints({
      topics: topicsState.items,
      boards: boardsState.items,
      documents: docsState.items,
      artifacts: artifactsState.items,
    }),
  );

  let handoffTimelineView = $derived(
    toTimelineView(handoffTimelineEvents, {
      labelHints: timelineLabelHints,
    }),
  );

  let inboxPreviewItems = $derived(
    selectHomeInboxPreview(inboxState.items, { limit: 3 }),
  );

  let handoffHasChanges = $derived(
    homeChangeCards.some((card) => card.count > 0) ||
      handoffTimelineView.length > 0,
  );

  let handoffIntro = $derived(
    !handoffMarkerLoaded
      ? "Loading handoff status…"
      : handoffMarkerIso
        ? `Since you marked this workspace read ${formatTimestamp(handoffMarkerIso) || handoffMarkerIso}.`
        : "Showing all recent workspace changes until you mark this handoff read.",
  );

  let handoffUpdatedCopy = $derived(
    refreshedAt
      ? `Updated ${formatTimestamp(refreshedAt)}`
      : loading
        ? "Loading..."
        : "",
  );

  let handoffDataErrors = $derived(
    [
      inboxState,
      topicsState,
      boardsState,
      docsState,
      artifactsState,
      eventsState,
    ]
      .filter((state) => state.status === "error" && state.error)
      .map((state) => state.error),
  );

  let pollTimer;

  $effect(() => {
    if (!browser || !organizationSlug || !workspaceSlug) return;
    handoffMarkerIso = readHomeHandoffMarker(organizationSlug, workspaceSlug);
    handoffMarkerLoaded = true;
  });

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

    const [
      inboxResult,
      topicsResult,
      boardsResult,
      docsResult,
      artifactsResult,
      eventsResult,
    ] = await Promise.allSettled([
      coreClient.listInboxItems({ view: "items" }),
      coreClient.listTopics({}),
      coreClient.listBoards({}),
      coreClient.listDocuments({}),
      coreClient.listArtifacts({}),
      coreClient.listEvents(),
    ]);

    inboxState = toSectionState(inboxResult, "items", "Failed to load inbox");
    topicsState = toSectionState(
      topicsResult,
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
    artifactsState = toSectionState(
      artifactsResult,
      "artifacts",
      "Failed to load artifacts",
    );
    eventsState = toSectionState(
      eventsResult,
      "events",
      "Failed to load workspace events",
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

  function toIdRecord(items = []) {
    return Object.fromEntries(
      items
        .map((item) => [String(item?.id ?? "").trim(), item])
        .filter(([id]) => id),
    );
  }

  function buildHomeRefLabelHints({
    topics = [],
    boards = [],
    documents = [],
    artifacts = [],
  } = {}) {
    const hints = buildTimelineRefLabelHints(
      toIdRecord(artifacts),
      toIdRecord(documents),
      {},
    );

    for (const topic of topics) {
      const topicId = String(topic?.id ?? "").trim();
      const threadId = String(topic?.thread_id ?? "").trim();
      const title = String(topic?.title ?? "").trim();
      if (topicId && title) hints[`topic:${topicId}`] = title;
      if (threadId && title) hints[`thread:${threadId}`] = title;
    }

    for (const board of boards) {
      const boardId = String(board?.id ?? "").trim();
      const title = String(board?.title ?? "").trim();
      if (boardId && title) hints[`board:${boardId}`] = title;
    }

    return hints;
  }

  function workspaceHref(pathname = "/") {
    return workspacePath(organizationSlug, workspaceSlug, pathname);
  }

  function inboxPreviewHref(item) {
    const id = String(item?.id ?? "").trim();
    return id ? workspaceHref(`/inbox/${id}`) : workspaceHref("/inbox");
  }

  function homeEventPrimaryRef(event) {
    const refs = Array.isArray(event?.refs) ? event.refs : [];

    for (const prefix of HOME_REF_PRIORITY) {
      const matched = refs.find((refValue) => splitTypedRef(refValue).prefix === prefix);
      if (matched) return matched;
    }

    return "";
  }

  function countCardClass(count) {
    return count > 0
      ? "border-[var(--line)] bg-[var(--bg-soft)]"
      : "border-[var(--line)] bg-[var(--panel)]";
  }

  function countValueClass(count) {
    return count > 0 ? "text-[var(--fg)]" : "text-[var(--fg-muted)]";
  }

  function resolveMarkAsRead() {
    const nextMarker = computeNextHomeHandoffMarker({
      markerIso: handoffMarkerIso,
      inboxItems: inboxState.items,
      topics: topicsState.items,
      boards: boardsState.items,
      documents: docsState.items,
      artifacts: artifactsState.items,
      events: eventsState.items,
      now: Date.now(),
    });

    handoffMarkerIso = nextMarker;
    writeHomeHandoffMarker(organizationSlug, workspaceSlug, nextMarker);
  }

  function latestTimelineTimestamp() {
    return handoffTimelineEvents.reduce((latest, event) => {
      const parsed = parseTimestampMs(event?.ts);
      return Number.isFinite(parsed) ? Math.max(latest, parsed) : latest;
    }, Number.NEGATIVE_INFINITY);
  }

  function isMarkAsReadDisabled() {
    return !handoffMarkerLoaded || loading || handoffDataErrors.length > 0;
  }

  function timestampToIso(timestampMs) {
    return Number.isFinite(timestampMs) ? new Date(timestampMs).toISOString() : "";
  }
</script>

<div class="space-y-6 min-w-0 max-w-full" data-tour="home">
  <div
    class="flex flex-wrap items-start justify-between gap-x-4 gap-y-3 min-w-0"
  >
    <div class="min-w-0">
      <h1 class="text-subtitle font-semibold text-[var(--fg)]">Home</h1>
      <p class="mt-0.5 text-meta text-[var(--fg-muted)]">
        {handoffIntro}
      </p>
      {#if handoffUpdatedCopy}
        <p class="mt-1 text-micro text-[var(--fg-muted)]">
          {handoffUpdatedCopy}
        </p>
      {/if}
    </div>
    <div class="flex shrink-0 flex-wrap items-center gap-2">
      {#if canReplayTour}
        <button
          class="dashboard-tour-button"
          onclick={() => replayWorkspaceTour()}
          type="button"
          title="Replay the 60-second workspace tour"
        >
          <svg
            class="dashboard-tour-button__icon"
            xmlns="http://www.w3.org/2000/svg"
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <circle cx="12" cy="12" r="9" />
            <path d="M9 9l6 3l-6 3z" fill="currentColor" stroke="none" />
          </svg>
          Take the tour
        </button>
      {/if}
      <button
        class="cursor-pointer rounded-md border border-[var(--line)] px-2.5 py-1.5 text-meta font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] disabled:cursor-not-allowed disabled:opacity-60"
        data-testid="home-mark-read"
        onclick={resolveMarkAsRead}
        disabled={isMarkAsReadDisabled()}
        type="button"
        title={
          handoffMarkerIso
            ? "Update the handoff marker to everything currently represented on Home"
            : "Set your first handoff marker for this workspace"
        }
      >
        Mark as read
      </button>
      <button
        class="cursor-pointer rounded-md border border-[var(--line)] px-2.5 py-1.5 text-meta font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)]"
        onclick={loadDashboard}
        type="button"
      >
        Refresh
      </button>
    </div>
  </div>

  <section
    class="rounded-xl border border-[var(--line)] bg-[var(--panel)] p-4 sm:p-5"
    data-testid="home-change-counts"
  >
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h2 class="text-meta font-semibold text-[var(--fg)]">What changed</h2>
        <p class="mt-1 text-micro text-[var(--fg-muted)]">
          {#if handoffMarkerIso}
            Since you marked this workspace read
          {:else}
            Everything available in this workspace right now
          {/if}
        </p>
      </div>
      {#if Number.isFinite(latestTimelineTimestamp())}
        <p class="text-micro text-[var(--fg-muted)]">
          Latest event {formatTimestamp(timestampToIso(latestTimelineTimestamp()))}
        </p>
      {/if}
    </div>

    {#if loading && inboxState.status === "idle" && topicsState.status === "idle"}
      <p class="mt-4 text-meta text-[var(--fg-muted)]">Loading handoff summary…</p>
    {:else}
      <div class="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {#each homeChangeCards as card}
          <div
            class={`rounded-lg border px-3.5 py-3 ${countCardClass(card.count)}`}
          >
            <p class="text-micro font-medium text-[var(--fg-muted)]">
              {card.label}
            </p>
            <p class={`mt-2 text-title font-semibold ${countValueClass(card.count)}`}>
              {card.count}
            </p>
          </div>
        {/each}
      </div>

      {#if handoffMarkerIso && !handoffHasChanges}
        <p class="mt-4 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-meta text-[var(--fg-muted)]">
          Nothing new since you marked this workspace read.
        </p>
      {/if}

      {#if handoffDataErrors.length > 0}
        <p class="mt-4 rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text">
          {handoffDataErrors[0]}
        </p>
      {/if}
    {/if}
  </section>

  <div class="grid gap-5 xl:grid-cols-[minmax(0,1.8fr)_minmax(18rem,1fr)] min-w-0">
    <section
      class="min-w-0 rounded-xl border border-[var(--line)] bg-[var(--panel)] p-4 sm:p-5"
      data-testid="home-happenings"
    >
      <div class="flex items-center justify-between gap-3">
        <div>
          <h2 class="text-meta font-semibold text-[var(--fg)]">What’s happened</h2>
          <p class="mt-1 text-micro text-[var(--fg-muted)]">
            High-signal workspace activity since your last handoff mark
          </p>
        </div>
      </div>

      {#if loading && eventsState.status === "idle"}
        <p class="mt-4 text-meta text-[var(--fg-muted)]">Loading workspace activity…</p>
      {:else if eventsState.status === "error"}
        <p class="mt-4 rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text">
          {eventsState.error}
        </p>
      {:else if handoffTimelineView.length === 0}
        <p class="mt-4 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-3 text-meta text-[var(--fg-muted)]">
          {#if handoffMarkerIso}
            Nothing new has landed in the workspace timeline since your last handoff mark.
          {:else}
            No workspace events yet.
          {/if}
        </p>
      {:else}
        <div class="mt-4 space-y-3">
          {#each handoffTimelineView as event (event.id)}
            {@const primaryRef = homeEventPrimaryRef(event)}
            <div class="rounded-lg border border-[var(--line)] bg-[var(--bg-soft)] px-4 py-3">
              <div class="flex items-start gap-3">
                <span
                  class={`mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full ${eventTypeDotClass(event.rawType)}`}
                  title={event.typeLabel}
                ></span>
                <div class="min-w-0 flex-1">
                  <p class="text-meta font-medium text-[var(--fg)]">
                    {event.summary || event.typeLabel}
                  </p>
                  <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-micro text-[var(--fg-muted)]">
                    <span>{event.typeLabel}</span>
                    <span>•</span>
                    <span>{formatTimestamp(event.ts) || "—"}</span>
                    {#if primaryRef}
                      <span>•</span>
                      <RefLink
                        refValue={primaryRef}
                        humanize
                        labelHints={timelineLabelHints}
                      />
                    {/if}
                  </div>
                </div>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </section>

    <section
      class="min-w-0 rounded-xl border border-[var(--line)] bg-[var(--panel)] p-4 sm:p-5"
      data-testid="home-inbox-preview"
    >
      <div class="flex items-center justify-between gap-2">
        <div>
          <h2 class="text-meta font-semibold text-[var(--fg)]">Inbox preview</h2>
          <p class="mt-1 text-micro text-[var(--fg-muted)]">
            Top unresolved items right now
          </p>
        </div>
        <a
          class="text-micro font-medium text-[var(--fg-muted)] transition-colors hover:text-[var(--fg)]"
          href={workspaceHref("/inbox")}
        >
          View inbox
        </a>
      </div>

      {#if loading && inboxState.status === "idle"}
        <p class="mt-4 text-meta text-[var(--fg-muted)]">Loading inbox preview…</p>
      {:else if inboxState.status === "error"}
        <p class="mt-4 rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text">
          {inboxState.error}
        </p>
      {:else if inboxPreviewItems.length === 0}
        <p class="mt-4 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-3 text-meta text-[var(--fg-muted)]">
          Inbox is clear.
        </p>
      {:else}
        <div class="mt-4 space-y-2">
          {#each inboxPreviewItems as item}
            <a
              class="block rounded-lg border border-[var(--line)] bg-[var(--bg-soft)] px-3.5 py-3 transition-colors hover:bg-[var(--line-subtle)]"
              href={inboxPreviewHref(item)}
            >
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0 flex-1">
                  <p class="truncate text-meta font-medium text-[var(--fg)]">
                    {item.title}
                  </p>
                  <p class="mt-1 text-micro text-[var(--fg-muted)]">
                    {getInboxSubjectLabel(item) || "Workspace item"} · {formatTimestamp(item.source_event_time) || "—"}
                  </p>
                </div>
                <span class="shrink-0 rounded px-1.5 py-0.5 text-micro font-medium text-[var(--fg)] bg-[var(--line)]">
                  {getInboxCategoryLabel(item.category)}
                </span>
              </div>
            </a>
          {/each}
        </div>
      {/if}
    </section>
  </div>
</div>

<style>
  .dashboard-tour-button {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.375rem 0.65rem;
    font-size: 0.8rem;
    font-weight: 600;
    line-height: 1;
    color: var(--accent-text, var(--accent));
    background: color-mix(in srgb, var(--accent) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--accent) 35%, transparent);
    border-radius: 0.375rem;
    cursor: pointer;
    transition:
      background-color 140ms ease,
      border-color 140ms ease,
      transform 120ms ease;
  }
  .dashboard-tour-button:hover {
    background: color-mix(in srgb, var(--accent) 20%, transparent);
    border-color: color-mix(in srgb, var(--accent) 55%, transparent);
  }
  .dashboard-tour-button:active {
    transform: translateY(1px);
  }
  .dashboard-tour-button:focus-visible {
    outline: 2px solid color-mix(in srgb, var(--accent) 65%, transparent);
    outline-offset: 2px;
  }
  .dashboard-tour-button__icon {
    flex: none;
  }
</style>
