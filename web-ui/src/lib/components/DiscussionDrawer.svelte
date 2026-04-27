<script>
  import { browser } from "$app/environment";
  import { writable } from "svelte/store";

  import { coreClient } from "$lib/coreClient";
  import {
    createTimelineContext,
    setTimelineContext,
  } from "$lib/timelineContext";
  import MessagesTab from "$lib/components/timeline/MessagesTab.svelte";

  /**
   * A self-contained collapsible discussion panel.
   * Manages its own isolated timelineContext and lazy-loads the backing thread
   * on first open. Designed to live at the bottom of any content area.
   */
  let {
    threadId,
    workspaceId = "",
    workspaceSlug = "",
    /** Header label shown in the collapsed/expanded toggle bar. */
    label = "Discussion",
    /**
     * Used to namespace the localStorage open/close preference.
     * E.g. "board-feed:thread-abc" or "doc-discussion:doc-xyz".
     * If empty, open state is not persisted.
     */
    storageKey = "",
    /** Zero-state copy for MessagesTab when there are no messages. */
    emptyMessage = "",
    /** Start open without a user interaction. */
    defaultOpen = false,
    /**
     * Increment to programmatically force the drawer open (e.g. when the
     * parent triggers a document text comment). Same pattern as
     * DocumentDiscussionRail.openSignal.
     */
    openSignal = 0,
    /** Forwarded to MessagesTab: only show events whose refs include this. */
    subjectRefFilter = "",
    /** Forwarded to MessagesTab: extra refs appended to posted messages. */
    extraPostRefs = [],
    /** Forwarded to MessagesTab for pending document text comments. */
    pendingDocumentComment = null,
    onPendingDocumentPostConsumed = undefined,
    onClearPendingDocumentPost = undefined,
    /** Forwarded to MessagesTab for stale anchor detection. */
    currentDocumentContent = "",
    /** Forwarded to MessagesTab. "archive" | "resolve" etc. */
    archiveLabelKind = "archive",
    /** Forwarded to MessagesTab for document comment highlights. */
    onDocumentTextAnchorContextChange = undefined,
    /**
     * Narrow viewports: keep the message list in a scroll region and pin the
     * composer to the bottom of the drawer (thumb reach on phones).
     */
    pinComposerNarrow = true,
    /**
     * When the parent sets a max-height (e.g. doc discussion dock), flex to
     * fill it so the message list gets a real scrollport instead of growing
     * with content.
     */
    expandFillsParent = false,
    /** Full-width panel on small screens (cancels horizontal inset). */
    narrowEdgeToEdge = false,
  } = $props();

  // Each DiscussionDrawer owns an isolated timeline context so it doesn't
  // bleed into sibling components that manage their own timelines (e.g. the
  // desktop side rail in DocumentDiscussionRail).
  const wsSlug = writable("");
  const timelineApi = createTimelineContext(coreClient);

  $effect.pre(() => {
    wsSlug.set(String(workspaceSlug ?? ""));
  });

  setTimelineContext({
    store: timelineApi.store,
    workspaceSlug: wsSlug,
    refreshTimeline: () => timelineApi.refreshTimeline(),
  });

  let lsKey = $derived(storageKey ? `discussion-drawer:${storageKey}` : "");
  // open starts false; the hydration effect below applies defaultOpen + localStorage.
  let open = $state(false);
  let everLoaded = $state(false);
  let lastOpenSignal = $state(0);

  function setOpen(next) {
    open = next;
    if (lsKey && browser) {
      localStorage.setItem(lsKey, next ? "1" : "0");
    }
    if (next && threadId && !everLoaded) {
      everLoaded = true;
      void timelineApi.loadTimeline(threadId);
    }
  }

  function toggle() {
    setOpen(!open);
  }

  // Hydrate open state from localStorage once the key is known.
  $effect(() => {
    if (!browser || !lsKey) return;
    const v = localStorage.getItem(lsKey);
    if (v === "1") setOpen(true);
    else if (v === "0") open = false;
    else if (defaultOpen) setOpen(true);
  });

  // Respond to programmatic open signals from the parent.
  $effect(() => {
    const n = Number(openSignal ?? 0) || 0;
    if (n > lastOpenSignal) {
      lastOpenSignal = n;
      setOpen(true);
    }
  });

  const timelineStore = timelineApi.store;
  let allTimeline = $derived(
    Array.isArray($timelineStore.timeline) ? $timelineStore.timeline : [],
  );
  let messageCount = $derived(
    allTimeline.filter(
      (e) => String(e?.type ?? "") === "message_posted" && !e?.trashed_at,
    ).length,
  );

  async function handleMessagePost(_routeScopeId, event) {
    await coreClient.createEvent({ event });
    await timelineApi.refreshTimeline();
  }
</script>

{#if threadId}
  <div
    class="dd-surface flex min-h-0 flex-col border-t border-[var(--line)] bg-[var(--panel)] {expandFillsParent &&
    open
      ? 'max-lg:flex max-lg:h-full max-lg:min-h-0 max-lg:flex-1 max-lg:flex-col max-lg:overflow-hidden'
      : ''}"
  >
    <!-- Collapsed/expanded toggle bar — consistent chrome across all surfaces -->
    <button
      type="button"
      class="flex w-full items-center gap-2 py-2.5 text-left transition-colors hover:bg-[var(--line-subtle)] {narrowEdgeToEdge
        ? 'max-lg:px-4 px-3'
        : 'px-3'}"
      aria-expanded={open}
      onclick={toggle}
    >
      <svg
        class="h-3.5 w-3.5 shrink-0 text-[var(--fg-muted)] transition-transform {open
          ? 'rotate-180'
          : ''}"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
        aria-hidden="true"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M19 9l-7 7-7-7"
        />
      </svg>
      <span class="flex-1 text-micro font-medium text-[var(--fg)]">{label}</span
      >
      {#if messageCount > 0}
        <span class="text-micro text-[var(--fg-muted)]">{messageCount}</span>
      {/if}
    </button>

    {#if open}
      <div
        class="min-h-0 flex-1 border-t border-[var(--line)] max-lg:flex max-lg:min-h-0 max-lg:flex-col max-lg:overflow-hidden {expandFillsParent
          ? 'max-lg:max-h-none max-lg:flex-1'
          : 'max-lg:max-h-[min(72dvh,30rem)]'} {narrowEdgeToEdge
          ? 'max-lg:px-0 max-lg:pb-0 max-lg:pt-0 px-3 pb-3 pt-3'
          : 'px-3 pb-3 pt-3'}"
      >
        <div
          class="min-h-0 w-full min-w-0 max-lg:min-h-0 max-lg:flex-1 max-lg:overflow-hidden lg:max-h-[min(60vh,36rem)] lg:overflow-y-auto lg:pr-0.5"
        >
          <MessagesTab
            {threadId}
            onMessagePost={handleMessagePost}
            {workspaceId}
            {subjectRefFilter}
            {extraPostRefs}
            {pendingDocumentComment}
            {onPendingDocumentPostConsumed}
            {onClearPendingDocumentPost}
            {currentDocumentContent}
            {archiveLabelKind}
            {onDocumentTextAnchorContextChange}
            discussionEmptyMessage={emptyMessage}
            {pinComposerNarrow}
          />
        </div>
      </div>
    {/if}
  </div>
{/if}
