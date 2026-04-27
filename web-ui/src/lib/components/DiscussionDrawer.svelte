<script>
  import { browser } from "$app/environment";
  import { writable } from "svelte/store";

  /** Inert store so `$derived` / `$state` can subscribe when the drawer uses `useParentTimelineContext`. */
  const emptyTimelineStore = writable({
    timeline: [],
    timelineLoading: false,
    timelineError: "",
  });

  import { coreClient } from "$lib/coreClient";
  import {
    createTimelineContext,
    setTimelineContext,
  } from "$lib/timelineContext";
  import MessagesTab from "$lib/components/timeline/MessagesTab.svelte";

  /**
   * A self-contained discussion panel (optionally collapsible on narrow viewports).
   * Manages its own isolated timelineContext by default; topic Messages uses
   * `useParentTimelineContext` to keep the page-level topic detail store.
   */
  let {
    threadId,
    /** Forwarded to MessagesTab; refresh/list scope (e.g. topic URL id vs thread id). */
    postRouteScopeId = "",
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
     * Short-thread list alignment when pinned narrow. Collapsible drawers pin
     * bubbles toward the composer; always-open topic Messages uses false (see
     * MessagesTab `pinComposerAlignThreadEnd`).
     */
    pinComposerAlignThreadEnd = undefined,
    /**
     * When the parent sets a max-height (e.g. doc discussion dock), flex to
     * fill it so the message list gets a real scrollport instead of growing
     * with content.
     */
    expandFillsParent = false,
    /** Full-width panel on small screens (cancels horizontal inset). */
    narrowEdgeToEdge = false,
    /**
     * When false, the toggle row is hidden and the thread is always shown
     * (topic Messages tab).
     */
    collapsible = true,
    /**
     * When true, do not create or register an isolated timeline — use the
     * parent `setTimelineContext` (e.g. topic detail page). Requires
     * `onMessagePost` for the same refresh semantics as the host page.
     */
    useParentTimelineContext = false,
    /**
     * Host handler when `useParentTimelineContext` is true (topic: refresh
     * topic detail store). Ignored when using an isolated timeline.
     */
    onMessagePost: hostOnMessagePost = undefined,
    /**
     * Optional override for the localStorage key used when persisting narrow
     * panel height. Defaults to `storageKey`. Non-collapsible surfaces (topic
     * Messages) do not resize and ignore this.
     */
    resizeStorageKey = "",
  } = $props();

  let pinComposerAlignThreadEndResolved = $derived(
    pinComposerAlignThreadEnd !== undefined
      ? pinComposerAlignThreadEnd
      : collapsible,
  );

  /** Always created so prop branching does not capture `useParentTimelineContext` at init. */
  const isolatedTimelineApi = createTimelineContext(coreClient);

  const wsSlug = writable("");

  $effect.pre(() => {
    if (useParentTimelineContext) return;
    wsSlug.set(String(workspaceSlug ?? ""));
  });

  $effect.pre(() => {
    if (useParentTimelineContext) return;
    setTimelineContext({
      store: isolatedTimelineApi.store,
      workspaceSlug: wsSlug,
      refreshTimeline: () => isolatedTimelineApi.refreshTimeline(),
    });
  });

  let lsKey = $derived(storageKey ? `discussion-drawer:${storageKey}` : "");
  let open = $state(false);
  let everLoaded = $state(false);
  let lastOpenSignal = $state(0);

  let showOpen = $derived(!collapsible || open);

  function setOpen(next) {
    open = next;
    if (collapsible && lsKey && browser) {
      localStorage.setItem(lsKey, next ? "1" : "0");
    }
    if (next && threadId && !everLoaded && !useParentTimelineContext) {
      everLoaded = true;
      void isolatedTimelineApi.loadTimeline(threadId);
    }
  }

  function toggle() {
    if (!collapsible) return;
    setOpen(!open);
  }

  $effect(() => {
    if (!collapsible && threadId && !useParentTimelineContext && !everLoaded) {
      everLoaded = true;
      void isolatedTimelineApi.loadTimeline(threadId);
    }
  });

  // Hydrate open state from localStorage once the key is known.
  $effect(() => {
    if (!browser || !lsKey || !collapsible) return;
    const v = localStorage.getItem(lsKey);
    if (v === "1") setOpen(true);
    else if (v === "0") open = false;
    else if (defaultOpen) setOpen(true);
  });

  // Respond to programmatic open signals from the parent.
  $effect(() => {
    if (!collapsible) return;
    const n = Number(openSignal ?? 0) || 0;
    if (n > lastOpenSignal) {
      lastOpenSignal = n;
      setOpen(true);
    }
  });

  const timelineStore = $derived(
    useParentTimelineContext ? emptyTimelineStore : isolatedTimelineApi.store,
  );
  let allTimeline = $derived(
    Array.isArray($timelineStore.timeline) ? $timelineStore.timeline : [],
  );
  let messageCount = $derived(
    allTimeline.filter(
      (e) => String(e?.type ?? "") === "message_posted" && !e?.trashed_at,
    ).length,
  );

  async function handleMessagePost(routeScopeId, event) {
    if (useParentTimelineContext && hostOnMessagePost) {
      return await hostOnMessagePost(routeScopeId, event);
    }
    const result = await coreClient.createEvent({ event });
    if (!useParentTimelineContext) {
      await isolatedTimelineApi.refreshTimeline();
    }
    return result;
  }

  let surfaceEl = $state(/** @type {HTMLDivElement | null} */ (null));

  let dockLayoutHost = $derived(
    browser && surfaceEl
      ? surfaceEl.closest(".page-dock-layout--fixed-mobile-chat")
      : null,
  );

  let heightLsKey = $derived.by(() => {
    if (!collapsible) return "";
    const rs = String(resizeStorageKey ?? "").trim();
    if (rs) return `discussion-drawer-h:${rs}`;
    const sk = String(storageKey ?? "").trim();
    if (sk) return `discussion-drawer-h:${sk}`;
    return "";
  });

  function clampPanelHeight(px, viewportH) {
    const v =
      viewportH || (typeof window !== "undefined" ? window.innerHeight : 640);
    const minH = Math.min(Math.max(v * 0.26, 168), v * 0.42);
    const maxH = Math.max(v * 0.55, v - 56);
    return Math.round(Math.min(maxH, Math.max(minH, px)));
  }

  $effect(() => {
    if (!browser || !dockLayoutHost || !heightLsKey) return;
    const raw = localStorage.getItem(heightLsKey);
    if (!raw) return;
    const n = Number.parseInt(raw, 10);
    if (!Number.isFinite(n) || n < 1) return;
    dockLayoutHost.style.setProperty(
      "--mobile-chat-panel-height",
      `${clampPanelHeight(n)}px`,
    );
  });

  let showResizeGrip = $derived(
    Boolean(collapsible && open && browser && dockLayoutHost && heightLsKey),
  );

  /** Pointer session for unified header (not UI state; plain object avoids runes guard on `let`). */
  const headerGesture = {
    ptrId: /** @type {number | null} */ (null),
    ptrStartY: 0,
    ptrStartX: 0,
    resizeActive: false,
    resizeStartH: 0,
    suppressClick: false,
  };

  /** @param {PointerEvent & { currentTarget: HTMLButtonElement }} e */
  function onHeaderPointerDown(e) {
    if (e.button !== 0 || !showResizeGrip || !dockLayoutHost) return;
    const feed = surfaceEl?.closest(".page-dock-feed");
    if (!feed) return;
    headerGesture.ptrId = e.pointerId;
    headerGesture.ptrStartY = e.clientY;
    headerGesture.ptrStartX = e.clientX;
    headerGesture.resizeActive = false;
    headerGesture.resizeStartH = feed.getBoundingClientRect().height;
  }

  /** @param {PointerEvent & { currentTarget: HTMLButtonElement }} e */
  function onHeaderPointerMove(e) {
    if (
      headerGesture.ptrId !== e.pointerId ||
      !showResizeGrip ||
      !dockLayoutHost
    )
      return;
    const dy = headerGesture.ptrStartY - e.clientY;
    const dx = e.clientX - headerGesture.ptrStartX;
    if (!headerGesture.resizeActive) {
      if (Math.abs(dy) < 10 || Math.abs(dy) <= Math.abs(dx)) return;
      headerGesture.resizeActive = true;
      try {
        e.currentTarget.setPointerCapture(e.pointerId);
      } catch {
        /* ignore */
      }
    }
    if (headerGesture.resizeActive) {
      const vh = window.innerHeight;
      const next = clampPanelHeight(headerGesture.resizeStartH + dy, vh);
      dockLayoutHost.style.setProperty(
        "--mobile-chat-panel-height",
        `${next}px`,
      );
    }
  }

  /** @param {PointerEvent & { currentTarget: HTMLButtonElement }} e */
  function onHeaderPointerUp(e) {
    if (headerGesture.ptrId !== e.pointerId) return;
    headerGesture.ptrId = null;
    const el = e.currentTarget;
    if (headerGesture.resizeActive) {
      try {
        el.releasePointerCapture(e.pointerId);
      } catch {
        /* ignore */
      }
      headerGesture.resizeActive = false;
      const feed = surfaceEl?.closest(".page-dock-feed");
      if (feed && dockLayoutHost && heightLsKey) {
        const h = clampPanelHeight(feed.getBoundingClientRect().height);
        dockLayoutHost.style.setProperty(
          "--mobile-chat-panel-height",
          `${h}px`,
        );
        localStorage.setItem(heightLsKey, String(h));
      }
      headerGesture.suppressClick = true;
      return;
    }
    headerGesture.resizeActive = false;
    const dy = Math.abs(headerGesture.ptrStartY - e.clientY);
    const dx = Math.abs(headerGesture.ptrStartX - e.clientX);
    if (dy < 10 && dx < 10) {
      toggle();
      headerGesture.suppressClick = true;
    }
  }

  /** @param {PointerEvent & { currentTarget: HTMLButtonElement }} e */
  function onHeaderPointerCancel(e) {
    if (headerGesture.ptrId !== e.pointerId) return;
    headerGesture.ptrId = null;
    if (headerGesture.resizeActive) {
      try {
        e.currentTarget.releasePointerCapture(e.pointerId);
      } catch {
        /* ignore */
      }
    }
    headerGesture.resizeActive = false;
  }

  /** @param {MouseEvent & { currentTarget: HTMLButtonElement }} e */
  function onHeaderClick(e) {
    if (headerGesture.suppressClick) {
      e.preventDefault();
      e.stopPropagation();
      headerGesture.suppressClick = false;
      return;
    }
    toggle();
  }

  /** Topic dock: full-width on narrow but keep vertical padding; drawers stay flush when collapsible. */
  let openPanelPadding = $derived.by(() => {
    const base = "px-3 pb-3 pt-3";
    if (!narrowEdgeToEdge) return base;
    if (collapsible) return `${base} max-lg:px-0 max-lg:pb-0 max-lg:pt-0`;
    /* Topic: keep side-to-side width; light vertical padding so the sheet doesn’t waste panel height. */
    return `${base} max-lg:px-0 max-lg:pt-1 max-lg:pb-1`;
  });
</script>

{#if threadId}
  <div
    bind:this={surfaceEl}
    class="dd-surface flex min-h-0 flex-col border-t border-[var(--line)] bg-[var(--panel)] {!collapsible
      ? 'max-lg:border-t-0 lg:border-0 lg:bg-transparent'
      : ''} {expandFillsParent && showOpen
      ? 'max-lg:flex max-lg:h-full max-lg:min-h-0 max-lg:flex-1 max-lg:flex-col max-lg:overflow-hidden'
      : ''}"
    data-mobile-chat-expanded={showOpen ? "" : undefined}
  >
    {#if collapsible}
      <!-- One bar: tap toggles; when expanded on fixed dock, vertical drag resizes. -->
      <button
        type="button"
        class="flex w-full min-w-0 touch-manipulation items-center gap-2 py-2 text-left transition-colors hover:bg-[var(--line-subtle)] {narrowEdgeToEdge
          ? 'max-lg:px-4 px-3'
          : 'px-3'} {showResizeGrip
          ? 'max-lg:select-none max-lg:touch-none'
          : ''}"
        aria-expanded={open}
        aria-label={showResizeGrip
          ? `${label}. Tap to collapse, or drag vertically to resize.`
          : open
            ? `${label}. Tap to collapse.`
            : `${label}. Tap to expand.`}
        onclick={onHeaderClick}
        onpointerdown={onHeaderPointerDown}
        onpointermove={onHeaderPointerMove}
        onpointerup={onHeaderPointerUp}
        onpointercancel={onHeaderPointerCancel}
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
        <span
          class="min-w-0 flex-1 truncate text-micro font-medium text-[var(--fg)]"
          >{label}</span
        >
        {#if messageCount > 0}
          <span class="shrink-0 text-micro text-[var(--fg-muted)]"
            >{messageCount}</span
          >
        {/if}
      </button>
    {/if}

    {#if showOpen}
      <div
        class="min-h-0 flex-1 border-t border-[var(--line)] max-lg:flex max-lg:min-h-0 max-lg:flex-col max-lg:overflow-hidden {!collapsible
          ? 'max-lg:border-t-0 lg:border-0'
          : ''} {expandFillsParent && showOpen
          ? 'max-lg:max-h-none max-lg:flex-1'
          : 'max-lg:max-h-[min(72dvh,30rem)]'} {openPanelPadding} {!collapsible
          ? 'lg:px-0 lg:pb-0 lg:pt-0'
          : ''}"
      >
        <div
          class="min-h-0 w-full min-w-0 max-lg:min-h-0 max-lg:flex-1 max-lg:overflow-hidden lg:max-h-[min(60vh,36rem)] lg:overflow-y-auto lg:pr-0.5 {!collapsible
            ? 'lg:max-h-none lg:overflow-visible lg:pr-0'
            : ''}"
        >
          <MessagesTab
            {threadId}
            {postRouteScopeId}
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
            pinComposerAlignThreadEnd={pinComposerAlignThreadEndResolved}
          />
        </div>
      </div>
    {/if}
  </div>
{/if}
