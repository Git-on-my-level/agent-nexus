<script>
  import { browser } from "$app/environment";
  import { onMount } from "svelte";
  import { writable } from "svelte/store";

  /** @typedef {'messages' | 'secondary'} DiscussionSideTab */

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

  const RAIL_WIDTH_LS_LEGACY = "doc-discussion-rail-width";
  const RAIL_W_MIN = 280;
  const RAIL_W_MAX = 520;
  const RAIL_W_DEFAULT = 360;
  const RAIL_W_COLLAPSED = 64;

  /**
   * A self-contained discussion panel (optionally collapsible on narrow viewports).
   * Manages its own isolated timelineContext by default; topic Messages uses
   * `useParentTimelineContext` to keep the page-level topic detail store.
   *
   * `layout`:
   * - `dock` — bottom collapsible strip (default).
   * - `rail` — full shell (`lg`+) right aside with width resize; below uses dock chrome.
   * - `primary` — full pane, always expanded (topic Messages).
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
    /** Visual / structural mode; see module comment. Escape hatches below override when set. */
    layout = "dock",
    /**
     * Dock placement for collapsible bottom drawers:
     * - `viewport`: fixed/sticky page dock controlled by app.css (doc mobile, board feed).
     * - `embedded`: in-flow dock inside a bounded host such as the card modal.
     */
    dockPlacement = "viewport",
    /**
     * Namespace for rail width persistence (`discussion-drawer-w:…`).
     * Defaults to `storageKey` when empty.
     */
    widthStorageKey = "",
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
    pinComposerNarrow,
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
    expandFillsParent,
    /** Full-width panel on small screens (cancels horizontal inset). */
    narrowEdgeToEdge = false,
    /**
     * When false, the toggle row is hidden and the thread is always shown
     * (topic Messages tab). Prefer `layout="primary"` at call sites.
     */
    collapsible,
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
    /**
     * Optional second body (e.g. document revisions). When set, header shows
     * Discussion vs this tab; only one panel is visible at a time.
     */
    secondaryPanel = undefined,
    secondaryTabLabel = "Revisions",
    /** @type {DiscussionSideTab} */
    sideTab = "messages",
    onSideTabChange = undefined,
    /**
     * Optional: run when expanding the rail from the collapsed strip with the
     * secondary tab selected (e.g. load revision list). Errors are ignored.
     */
    prepareSecondaryPanel = undefined,
    /**
     * When the host already knows the non-trashed message_posted count for this
     * thread (e.g. board workspace `derived.timeline_message_count`), pass it so
     * the header badge can render before the timeline fetch completes.
     */
    prefetchedMessageCount = undefined,
  } = $props();

  let hasSecondaryPanel = $derived(typeof secondaryPanel === "function");

  /** @param {DiscussionSideTab} tab */
  function pickSideTab(tab) {
    if (!hasSecondaryPanel) return;
    if (tab === "secondary" || tab === "messages") {
      onSideTabChange?.(tab);
    }
  }

  function openRailCollapsed(/** @type {DiscussionSideTab} */ tab) {
    setOpen(true);
    pickSideTab(tab);
  }

  function openRailCollapsedSecondary() {
    setOpen(true);
    pickSideTab("secondary");
    void Promise.resolve(prepareSecondaryPanel?.()).catch(() => {});
  }

  let collapsibleEff = $derived(collapsible ?? layout !== "primary");
  let dockPlacementEff = $derived(
    layout === "dock" && dockPlacement === "embedded" ? "embedded" : "viewport",
  );
  let embeddedDockEff = $derived(dockPlacementEff === "embedded");
  let expandFillsParentEff = $derived(
    expandFillsParent ?? (layout === "primary" || embeddedDockEff),
  );
  let pinComposerNarrowEff = $derived(pinComposerNarrow ?? true);
  /**
   * Pin the composer inside a height-bounded flex column: topic Messages,
   * board/doc drawers with `expandFillsParent`, embedded card drawers, and
   * the discussion rail.
   */
  let pinComposerEff = $derived(
    layout === "primary" || (layout === "dock" && expandFillsParentEff),
  );

  let pinComposerAlignThreadEndResolved = $derived(
    pinComposerAlignThreadEnd !== undefined
      ? pinComposerAlignThreadEnd
      : collapsibleEff,
  );

  /** Side rail (Google Docs–style) from `lg` up; bottom dock below that. */
  let isRailViewport = $state(false);
  onMount(() => {
    if (!browser) return;
    const mq = window.matchMedia("(min-width: 1024px)");
    isRailViewport = mq.matches;
    const onMq = () => {
      isRailViewport = mq.matches;
    };
    mq.addEventListener("change", onMq);
    return () => mq.removeEventListener("change", onMq);
  });

  let showRailAside = $derived(layout === "rail" && isRailViewport);

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
  let lastOpenSignal = $state(0);

  let showOpen = $derived(!collapsibleEff || open);

  let railWidth = $state(RAIL_W_DEFAULT);
  let railResizing = $state(false);
  /** @type {number} */
  let railResizeStartX = 0;
  /** @type {number} */
  let railResizeStartWidth = 0;

  let widthLsKey = $derived.by(() => {
    const wk = String(widthStorageKey ?? "").trim();
    const sk = String(storageKey ?? "").trim();
    const ns = wk || sk;
    return ns ? `discussion-drawer-w:${ns}` : "";
  });

  function clampRailWidth(/** @type {number} */ px) {
    return Math.min(RAIL_W_MAX, Math.max(RAIL_W_MIN, Math.round(px)));
  }

  $effect(() => {
    if (!browser || !widthLsKey || layout !== "rail") return;
    const raw = localStorage.getItem(widthLsKey);
    let w = Number.parseInt(String(raw ?? ""), 10);
    if (!Number.isFinite(w) || w < 1) {
      const leg = Number.parseInt(
        String(localStorage.getItem(RAIL_WIDTH_LS_LEGACY) ?? ""),
        10,
      );
      w = Number.isFinite(leg) ? leg : RAIL_W_DEFAULT;
    }
    railWidth = clampRailWidth(w);
  });

  $effect(() => {
    if (!browser || layout !== "rail" || !showRailAside) return;
    const reserved = open ? railWidth : RAIL_W_COLLAPSED;
    document.documentElement.style.setProperty(
      "--doc-discussion-rail-w",
      `${reserved}px`,
    );
    return () => {
      document.documentElement.style.removeProperty("--doc-discussion-rail-w");
    };
  });

  function persistRailWidth(/** @type {number} */ w) {
    if (browser && widthLsKey) {
      localStorage.setItem(widthLsKey, String(clampRailWidth(w)));
    }
  }

  function endRailResize(/** @type {PointerEvent | null} */ e) {
    const wasResizing = railResizing;
    if (e?.currentTarget && "hasPointerCapture" in e.currentTarget) {
      try {
        if (
          /** @type {HTMLElement} */ (e.currentTarget).hasPointerCapture(
            e.pointerId,
          )
        ) {
          /** @type {HTMLElement} */ (e.currentTarget).releasePointerCapture(
            e.pointerId,
          );
        }
      } catch {
        /* ignore */
      }
    }
    if (wasResizing) {
      persistRailWidth(railWidth);
    }
    railResizing = false;
    if (browser) {
      document.body.style.removeProperty("cursor");
      document.body.style.removeProperty("user-select");
    }
  }

  /** @param {PointerEvent} e */
  function onRailResizePointerDown(e) {
    if (e.button !== 0) return;
    e.preventDefault();
    railResizeStartX = e.clientX;
    railResizeStartWidth = railWidth;
    railResizing = true;
    /** @type {HTMLElement} */ (e.currentTarget).setPointerCapture(e.pointerId);
    if (browser) {
      document.body.style.cursor = "ew-resize";
      document.body.style.userSelect = "none";
    }
  }

  /** @param {PointerEvent} e */
  function onRailResizePointerMove(e) {
    if (!railResizing) return;
    const dx = e.clientX - railResizeStartX;
    railWidth = clampRailWidth(railResizeStartWidth - dx);
  }

  /** @param {PointerEvent} e */
  function onRailResizePointerUp(e) {
    if (!railResizing) return;
    endRailResize(e);
  }

  function setOpen(next) {
    open = next;
    if (collapsibleEff && lsKey && browser) {
      localStorage.setItem(lsKey, next ? "1" : "0");
    }
  }

  function toggle() {
    if (!collapsibleEff) return;
    setOpen(!open);
  }

  // Load the backing thread timeline as soon as `threadId` is known so header
  // badges stay accurate without requiring expand / tab focus first.
  let lastIsolatedTimelineThreadId = $state("");
  $effect(() => {
    if (useParentTimelineContext) return;
    const tid = String(threadId ?? "").trim();
    if (!tid) {
      lastIsolatedTimelineThreadId = "";
      return;
    }
    if (tid === lastIsolatedTimelineThreadId) return;
    lastIsolatedTimelineThreadId = tid;
    void isolatedTimelineApi.loadTimeline(tid);
  });
  // Hydrate open state from localStorage once the key is known.
  $effect(() => {
    if (!browser || !lsKey || !collapsibleEff) return;
    const v = localStorage.getItem(lsKey);
    if (v === "1") setOpen(true);
    else if (v === "0") open = false;
    else if (defaultOpen) setOpen(true);
  });

  // Respond to programmatic open signals from the parent.
  $effect(() => {
    if (!collapsibleEff) return;
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
  let prefetchMsgCount = $derived.by(() => {
    const n = Number(prefetchedMessageCount);
    if (!Number.isFinite(n)) return 0;
    return Math.max(0, Math.floor(n));
  });
  let messageCount = $derived(
    allTimeline.filter(
      (e) => String(e?.type ?? "") === "message_posted" && !e?.trashed_at,
    ).length,
  );
  let displayMessageCount = $derived(Math.max(messageCount, prefetchMsgCount));
  let railBadgeText = $derived(
    displayMessageCount > 0 ? String(Math.min(displayMessageCount, 99)) : "",
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

  /** Narrow dock: persists `--mobile-chat-panel-height` on the layout host. */
  const DOCK_HEIGHT_HOST_SELECTOR =
    ".page-dock-layout--fixed-mobile-chat, [data-discussion-dock-host]";

  let dockLayoutHost = $derived(
    browser && surfaceEl ? surfaceEl.closest(DOCK_HEIGHT_HOST_SELECTOR) : null,
  );

  let heightLsKey = $derived.by(() => {
    if (!collapsibleEff) return "";
    const rs = String(resizeStorageKey ?? "").trim();
    if (rs) return `discussion-drawer-h:${rs}`;
    const sk = String(storageKey ?? "").trim();
    if (sk) return `discussion-drawer-h:${sk}`;
    return "";
  });

  function dockHeightBounds(viewportH) {
    const v =
      viewportH || (typeof window !== "undefined" ? window.innerHeight : 640);
    const minH = Math.min(Math.max(v * 0.26, 168), v * 0.42);
    let maxH = Math.max(v * 0.55, v - 56);

    if (embeddedDockEff && dockLayoutHost) {
      const hostHeight = dockLayoutHost.getBoundingClientRect().height;
      if (Number.isFinite(hostHeight) && hostHeight > 0) {
        // Embedded card/modal drawers resize inside their host; leave enough
        // chrome visible to collapse the drawer and use host actions.
        const hostMax = Math.max(minH, hostHeight - 160);
        maxH = Math.min(maxH, hostMax);
      }
    }

    return { minH, maxH: Math.max(minH, maxH) };
  }

  function clampPanelHeight(px, viewportH) {
    const { minH, maxH } = dockHeightBounds(viewportH);
    return Math.round(Math.min(maxH, Math.max(minH, px)));
  }

  $effect(() => {
    if (!browser || !dockLayoutHost || !heightLsKey) return;
    const raw = localStorage.getItem(heightLsKey);
    if (!raw) {
      dockLayoutHost.style.removeProperty("--mobile-chat-panel-height");
      return;
    }
    const n = Number.parseInt(raw, 10);
    if (!Number.isFinite(n) || n < 1) {
      dockLayoutHost.style.removeProperty("--mobile-chat-panel-height");
      return;
    }
    dockLayoutHost.style.setProperty(
      "--mobile-chat-panel-height",
      `${clampPanelHeight(n)}px`,
    );
  });

  let showResizeGrip = $derived(
    Boolean(collapsibleEff && open && browser && dockLayoutHost && heightLsKey),
  );

  /** Pointer session for unified header (not UI state; plain object avoids runes guard on `let`). */
  const headerGesture = {
    ptrId: /** @type {number | null} */ (null),
    ptrStartY: 0,
    ptrStartX: 0,
    resizeActive: false,
    resizeStartH: 0,
    resizeCurrentH: 0,
    suppressClick: false,
  };

  /** @param {EventTarget | null} t */
  function dualDockResizeExemptTarget(t) {
    if (!(t instanceof Element)) return false;
    if (t.closest("[data-dd-dock-chevron]")) return true;
    if (t.closest('button[role="tab"]')) return true;
    return false;
  }

  /** @param {PointerEvent & { currentTarget: HTMLElement }} e */
  function onDualDockHeaderPointerDown(e) {
    if (dualDockResizeExemptTarget(e.target)) return;
    onHeaderPointerDown(e);
  }

  /** @param {PointerEvent & { currentTarget: HTMLElement }} e */
  function onDualDockHeaderPointerMove(e) {
    onHeaderPointerMove(e);
  }

  /** @param {PointerEvent & { currentTarget: HTMLElement }} e */
  function onDualDockHeaderPointerUp(e) {
    onHeaderPointerUp(e);
  }

  /** @param {PointerEvent & { currentTarget: HTMLElement }} e */
  function onDualDockHeaderPointerCancel(e) {
    onHeaderPointerCancel(e);
  }

  /** @param {PointerEvent & { currentTarget: HTMLElement }} e */
  function onHeaderPointerDown(e) {
    if (e.button !== 0 || !showResizeGrip || !dockLayoutHost) return;
    const feed = surfaceEl?.closest(".page-dock-feed");
    if (!feed) return;
    headerGesture.ptrId = e.pointerId;
    headerGesture.ptrStartY = e.clientY;
    headerGesture.ptrStartX = e.clientX;
    headerGesture.resizeActive = false;
    const feedRect = feed.getBoundingClientRect();
    headerGesture.resizeStartH = feedRect.height;
    headerGesture.resizeCurrentH = feedRect.height;
  }

  /** @param {PointerEvent & { currentTarget: HTMLElement }} e */
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
      const rawNext = headerGesture.resizeStartH + dy;
      const next = clampPanelHeight(rawNext, vh);
      headerGesture.resizeCurrentH = next;
      dockLayoutHost.style.setProperty(
        "--mobile-chat-panel-height",
        `${next}px`,
      );
    }
  }

  /** @param {PointerEvent & { currentTarget: HTMLElement }} e */
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
        const h = clampPanelHeight(
          headerGesture.resizeCurrentH || feed.getBoundingClientRect().height,
        );
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

  /** @param {PointerEvent & { currentTarget: HTMLElement }} e */
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
    if (embeddedDockEff) return "px-0 pb-0 pt-0";
    if (!narrowEdgeToEdge) return base;
    if (collapsibleEff) return `${base} max-lg:px-0 max-lg:pb-0 max-lg:pt-0`;
    /* Topic: keep side-to-side width; light vertical padding so the sheet doesn’t waste panel height. */
    return `${base} max-lg:px-0 max-lg:pt-1 max-lg:pb-1`;
  });
</script>

{#if threadId}
  {#if showRailAside}
    <aside
      class="dd-rail lg:fixed lg:inset-y-0 lg:right-0 lg:z-20 lg:border-l lg:border-line lg:bg-panel {open
        ? 'lg:w-[var(--dd-rail-w)]'
        : 'lg:w-16'}"
      style={open ? `--dd-rail-w:${railWidth}px` : undefined}
    >
      {#if !open}
        {#if hasSecondaryPanel}
          <div
            class="hidden min-h-0 lg:flex lg:h-dvh lg:min-h-[20rem] lg:w-full lg:flex-col lg:items-stretch"
            role="group"
            aria-label={`Open ${label.toLowerCase()} or ${secondaryTabLabel.toLowerCase()} panel`}
          >
            <button
              type="button"
              class="flex min-h-0 flex-1 cursor-pointer flex-col items-center justify-center gap-2 px-2 py-3 text-fg-muted transition-colors hover:bg-panel-hover hover:text-fg"
              aria-label={`Show ${label.toLowerCase()}${displayMessageCount > 0 ? `, ${displayMessageCount} ${displayMessageCount === 1 ? "comment" : "comments"}` : ""}`}
              title={label}
              onclick={() => openRailCollapsed("messages")}
            >
              <span
                class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-line bg-bg-soft text-accent-text"
                aria-hidden="true"
              >
                <svg
                  class="h-4 w-4"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  viewBox="0 0 24 24"
                  aria-hidden="true"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M21 12a8 8 0 0 1-11.6 7.15L4 20l1-4.2A8 8 0 1 1 21 12Z"
                  />
                </svg>
              </span>
              <span
                class="min-h-0 [writing-mode:vertical-rl] rotate-180 text-micro font-semibold tracking-normal text-fg"
              >
                {label}
              </span>
              {#if railBadgeText}
                <span
                  class="inline-flex min-h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-accent-soft px-1 text-[0.65rem] font-semibold leading-none text-accent-text"
                >
                  {railBadgeText}
                </span>
              {/if}
            </button>
            <div class="h-px w-full shrink-0 bg-line" role="separator"></div>
            <button
              type="button"
              class="flex min-h-0 flex-1 cursor-pointer flex-col items-center justify-center gap-2 px-2 py-3 text-fg-muted transition-colors hover:bg-panel-hover hover:text-fg"
              aria-label={`Show ${secondaryTabLabel.toLowerCase()}`}
              title={secondaryTabLabel}
              onclick={() => openRailCollapsedSecondary()}
            >
              <span
                class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-line bg-bg-soft text-fg-muted"
                aria-hidden="true"
              >
                <svg
                  class="h-4 w-4"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  viewBox="0 0 24 24"
                  aria-hidden="true"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
                  />
                </svg>
              </span>
              <span
                class="min-h-0 [writing-mode:vertical-rl] rotate-180 text-micro font-semibold tracking-normal text-fg"
              >
                {secondaryTabLabel}
              </span>
            </button>
          </div>
        {:else}
          <button
            type="button"
            class="hidden lg:flex lg:h-dvh lg:min-h-[20rem] lg:w-full lg:cursor-pointer lg:flex-col lg:items-center lg:justify-start lg:gap-3 lg:px-2 lg:py-4 lg:text-fg-muted lg:transition-colors lg:hover:bg-panel-hover lg:hover:text-fg"
            aria-label={`Show ${label.toLowerCase()}${displayMessageCount > 0 ? `, ${displayMessageCount} ${displayMessageCount === 1 ? "comment" : "comments"}` : ""}`}
            title={`Show ${label.toLowerCase()}`}
            onclick={() => setOpen(true)}
          >
            <span
              class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-line bg-bg-soft text-accent-text"
              aria-hidden="true"
            >
              <svg
                class="h-4 w-4"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                viewBox="0 0 24 24"
                aria-hidden="true"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  d="M21 12a8 8 0 0 1-11.6 7.15L4 20l1-4.2A8 8 0 1 1 21 12Z"
                />
              </svg>
            </span>
            <span
              class="mt-1 min-h-0 [writing-mode:vertical-rl] rotate-180 text-micro font-semibold tracking-normal text-fg"
            >
              Comments
            </span>
            {#if railBadgeText}
              <span
                class="mt-1 inline-flex min-h-5 min-w-5 items-center justify-center rounded-full bg-accent-soft px-1 text-[0.65rem] font-semibold leading-none text-accent-text"
              >
                {railBadgeText}
              </span>
            {/if}
          </button>
        {/if}
      {:else}
        <div
          class="flex w-full min-w-0 flex-col max-lg:max-h-[min(70vh,44rem)] lg:h-dvh lg:max-h-dvh lg:flex-row"
        >
          <div class="hidden shrink-0 lg:relative lg:block lg:w-3">
            <div
              role="separator"
              aria-orientation="vertical"
              aria-label="Drag to resize discussion panel"
              title="Drag to resize"
              class="group absolute inset-y-0 -left-1 z-[1] flex w-3 cursor-ew-resize touch-none select-none items-stretch pl-0.5 {railResizing
                ? 'ring-1 ring-line'
                : ''}"
              onpointerdown={onRailResizePointerDown}
              onpointermove={onRailResizePointerMove}
              onpointerup={onRailResizePointerUp}
              onpointercancel={onRailResizePointerUp}
              onlostpointercapture={() => endRailResize(null)}
            >
              <div
                class="mx-auto h-full w-px bg-line transition-opacity group-hover:opacity-100 {railResizing
                  ? 'bg-accent-text opacity-100'
                  : 'opacity-60'}"
              ></div>
            </div>
          </div>
          <div class="flex min-h-0 min-w-0 flex-1 flex-col">
            <div
              class="flex shrink-0 items-center justify-between gap-1 border-b border-line px-3 py-1.5"
            >
              {#if hasSecondaryPanel}
                <div
                  class="flex min-w-0 flex-1 items-center gap-0.5"
                  role="tablist"
                  aria-label="Document sidebar panels"
                >
                  <button
                    type="button"
                    role="tab"
                    aria-selected={sideTab === "messages"}
                    class="min-w-0 flex-1 rounded-md px-2 py-1 text-center text-micro font-medium transition-colors {sideTab ===
                    'messages'
                      ? 'bg-line-subtle text-fg'
                      : 'text-fg-muted hover:bg-bg-soft hover:text-fg'}"
                    onclick={() => pickSideTab("messages")}
                  >
                    {label}
                  </button>
                  <button
                    type="button"
                    role="tab"
                    aria-selected={sideTab === "secondary"}
                    class="min-w-0 flex-1 rounded-md px-2 py-1 text-center text-micro font-medium transition-colors {sideTab ===
                    'secondary'
                      ? 'bg-line-subtle text-fg'
                      : 'text-fg-muted hover:bg-bg-soft hover:text-fg'}"
                    onclick={() => pickSideTab("secondary")}
                  >
                    {secondaryTabLabel}
                  </button>
                </div>
              {:else}
                <h2 class="min-w-0 truncate text-micro font-medium text-fg">
                  {label}
                </h2>
              {/if}
              <button
                type="button"
                class="inline-flex h-7 w-7 shrink-0 cursor-pointer items-center justify-center rounded-md text-fg-muted transition-colors hover:bg-bg-soft hover:text-fg"
                onclick={() => setOpen(false)}
                aria-label="Hide discussion"
                title="Hide discussion"
              >
                <svg
                  class="h-4 w-4"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  viewBox="0 0 24 24"
                  aria-hidden="true"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M9 5l7 7-7 7"
                  />
                </svg>
              </button>
            </div>
            <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
              {#if !hasSecondaryPanel || sideTab === "messages"}
                <MessagesTab
                  {threadId}
                  postRouteScopeId={postRouteScopeId || threadId}
                  onMessagePost={handleMessagePost}
                  workspaceId={String(workspaceId ?? "")}
                  {subjectRefFilter}
                  {extraPostRefs}
                  discussionEmptyMessage={emptyMessage}
                  {pendingDocumentComment}
                  {onPendingDocumentPostConsumed}
                  {onClearPendingDocumentPost}
                  {currentDocumentContent}
                  {archiveLabelKind}
                  {onDocumentTextAnchorContextChange}
                  pinComposer={true}
                  pinComposerNarrow={false}
                  pinComposerAlignThreadEnd={pinComposerAlignThreadEndResolved}
                />
              {:else if secondaryPanel}
                <div
                  class="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden"
                >
                  {@render secondaryPanel()}
                </div>
              {/if}
            </div>
          </div>
        </div>
      {/if}
    </aside>
  {:else}
    <div
      bind:this={surfaceEl}
      class="dd-surface flex min-h-0 flex-col border-t border-line bg-panel {!collapsibleEff
        ? 'max-lg:border-t-0 lg:border-0 lg:bg-transparent'
        : ''} {expandFillsParentEff && showOpen
        ? layout === 'rail'
          ? 'max-lg:flex max-lg:h-full max-lg:min-h-0 max-lg:flex-1 max-lg:flex-col max-lg:overflow-hidden'
          : 'max-lg:flex max-lg:h-full max-lg:min-h-0 max-lg:flex-1 max-lg:flex-col max-lg:overflow-hidden lg:flex lg:h-full lg:min-h-0 lg:flex-1 lg:flex-col lg:overflow-hidden'
        : ''} {(layout === 'primary' ||
        (layout === 'dock' && expandFillsParentEff)) &&
      showOpen
        ? 'lg:h-full lg:min-h-0 lg:flex-1 lg:overflow-hidden'
        : ''} {layout === 'rail' ? 'lg:hidden' : ''}"
      data-mobile-chat-expanded={showOpen ? "" : undefined}
      data-discussion-dock-placement={dockPlacementEff}
    >
      {#if collapsibleEff}
        {#if hasSecondaryPanel}
          <div
            role="group"
            aria-label="Discussion panel header"
            class="flex w-full min-w-0 touch-manipulation items-center gap-1 py-2 {narrowEdgeToEdge
              ? 'max-lg:px-4 px-3'
              : 'px-3'} {showResizeGrip
              ? 'cursor-ns-resize select-none max-lg:touch-none'
              : ''}"
            title={showResizeGrip
              ? "Drag up or down to resize the panel (try the grip or the empty area)"
              : undefined}
            onpointerdown={showResizeGrip
              ? onDualDockHeaderPointerDown
              : undefined}
            onpointermove={showResizeGrip
              ? onDualDockHeaderPointerMove
              : undefined}
            onpointerup={showResizeGrip ? onDualDockHeaderPointerUp : undefined}
            onpointercancel={showResizeGrip
              ? onDualDockHeaderPointerCancel
              : undefined}
          >
            <button
              type="button"
              data-dd-dock-chevron
              class="inline-flex h-8 w-8 shrink-0 cursor-pointer items-center justify-center rounded-md text-fg-muted transition-colors hover:bg-line-subtle"
              aria-expanded={open}
              aria-label={showResizeGrip
                ? `${label}. Tap to collapse, or drag elsewhere on the bar to resize.`
                : open
                  ? `${label}. Tap to collapse.`
                  : `${label}. Tap to expand.`}
              onclick={onHeaderClick}
            >
              <svg
                class="h-3.5 w-3.5 shrink-0 transition-transform {open
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
            </button>
            {#if showResizeGrip}
              <span
                class="flex shrink-0 flex-col items-center justify-center gap-1 px-0.5 opacity-60"
                aria-hidden="true"
              >
                <span class="h-0.5 w-6 rounded-full bg-fg-muted"></span>
                <span class="h-0.5 w-6 rounded-full bg-fg-muted"></span>
                <span class="h-0.5 w-6 rounded-full bg-fg-muted"></span>
              </span>
            {/if}
            <div
              class="flex min-w-0 flex-1 items-center gap-0.5"
              role="tablist"
              aria-label="Document sidebar panels"
            >
              <button
                type="button"
                role="tab"
                aria-selected={sideTab === "messages"}
                aria-label={displayMessageCount > 0
                  ? `${label}, ${displayMessageCount} ${displayMessageCount === 1 ? "comment" : "comments"}`
                  : label}
                class="min-w-0 flex-1 rounded-md px-2 py-1.5 text-center text-micro font-medium transition-colors {sideTab ===
                'messages'
                  ? 'bg-line-subtle text-fg'
                  : 'text-fg-muted hover:bg-bg-soft hover:text-fg'}"
                onclick={() => {
                  if (!open) setOpen(true);
                  pickSideTab("messages");
                }}
              >
                <span class="inline-flex items-center justify-center gap-1.5">
                  <span>{label}</span>
                  {#if displayMessageCount > 0}
                    <span
                      class="inline-flex shrink-0 items-center gap-0.5 text-fg-muted"
                      aria-hidden="true"
                    >
                      <svg
                        class="h-3 w-3"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                        stroke-width="2"
                      >
                        <path
                          stroke-linejoin="round"
                          d="M21 12a8 8 0 0 1-11.6 7.15L4 20l1-4.2A8 8 0 1 1 21 12Z"
                        />
                      </svg>
                      <span class="text-[0.65rem] font-semibold tabular-nums">
                        {displayMessageCount > 99
                          ? "99+"
                          : String(displayMessageCount)}
                      </span>
                    </span>
                  {/if}
                </span>
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={sideTab === "secondary"}
                class="min-w-0 flex-1 rounded-md px-2 py-1.5 text-center text-micro font-medium transition-colors {sideTab ===
                'secondary'
                  ? 'bg-line-subtle text-fg'
                  : 'text-fg-muted hover:bg-bg-soft hover:text-fg'}"
                onclick={() => {
                  if (!open) setOpen(true);
                  pickSideTab("secondary");
                }}
              >
                {secondaryTabLabel}
              </button>
            </div>
          </div>
        {:else}
          <!-- One bar: tap toggles; when expanded on fixed dock, vertical drag resizes. -->
          <button
            type="button"
            class="flex w-full min-w-0 touch-manipulation items-center gap-2 py-2 text-left transition-colors hover:bg-line-subtle {narrowEdgeToEdge
              ? 'max-lg:px-4 px-3'
              : 'px-3'} {showResizeGrip
              ? 'cursor-ns-resize select-none touch-none'
              : ''}"
            aria-expanded={open}
            aria-label={showResizeGrip
              ? `${label}. Tap to collapse, or drag vertically to resize.${displayMessageCount > 0 ? ` ${displayMessageCount} comments.` : ""}`
              : open
                ? `${label}. Tap to collapse.${displayMessageCount > 0 ? ` ${displayMessageCount} comments.` : ""}`
                : `${label}. Tap to expand.${displayMessageCount > 0 ? ` ${displayMessageCount} comments.` : ""}`}
            onclick={onHeaderClick}
            onpointerdown={onHeaderPointerDown}
            onpointermove={onHeaderPointerMove}
            onpointerup={onHeaderPointerUp}
            onpointercancel={onHeaderPointerCancel}
          >
            <svg
              class="h-3.5 w-3.5 shrink-0 text-fg-muted transition-transform {open
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
            <span class="flex min-w-0 flex-1 items-center gap-2">
              <span class="truncate text-micro font-medium text-fg"
                >{label}</span
              >
              {#if displayMessageCount > 0}
                <span
                  class="inline-flex shrink-0 items-center gap-0.5 text-fg-muted"
                  aria-hidden="true"
                >
                  <svg
                    class="h-3 w-3"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path
                      stroke-linejoin="round"
                      d="M21 12a8 8 0 0 1-11.6 7.15L4 20l1-4.2A8 8 0 1 1 21 12Z"
                    />
                  </svg>
                  <span class="text-[0.65rem] font-semibold tabular-nums">
                    {displayMessageCount > 99
                      ? "99+"
                      : String(displayMessageCount)}
                  </span>
                </span>
              {/if}
            </span>
          </button>
        {/if}
      {/if}

      {#if showOpen}
        <div
          class="min-h-0 flex-1 border-t border-line {`max-lg:flex max-lg:min-h-0 max-lg:flex-col max-lg:overflow-hidden ${!collapsibleEff ? 'max-lg:border-t-0 lg:border-0' : ''} ${expandFillsParentEff && showOpen ? 'max-lg:max-h-none max-lg:flex-1' : 'max-lg:max-h-[min(72dvh,30rem)]'}`} {layout ===
            'primary' ||
          (layout === 'dock' && expandFillsParentEff)
            ? 'lg:flex lg:min-h-0 lg:flex-1 lg:flex-col lg:overflow-hidden'
            : ''} {openPanelPadding} {!collapsibleEff
            ? 'lg:px-0 lg:pb-0 lg:pt-0'
            : ''}"
        >
          <div
            class="min-h-0 w-full min-w-0 {layout === 'rail'
              ? `max-lg:min-h-0 max-lg:flex-1 max-lg:overflow-hidden ${!collapsibleEff ? 'max-lg:max-h-none max-lg:overflow-visible max-lg:pr-0' : ''}`
              : layout === 'dock' && expandFillsParentEff
                ? 'max-lg:min-h-0 max-lg:flex-1 max-lg:overflow-hidden lg:flex lg:min-h-0 lg:flex-1 lg:flex-col lg:overflow-hidden lg:pr-0.5'
                : `max-lg:min-h-0 max-lg:flex-1 max-lg:overflow-hidden lg:max-h-[min(60vh,36rem)] lg:overflow-y-auto lg:pr-0.5 ${!collapsibleEff ? 'lg:max-h-none lg:overflow-visible lg:pr-0' : ''}`} {layout ===
            'primary'
              ? 'lg:flex lg:min-h-0 lg:flex-1 lg:flex-col lg:overflow-hidden'
              : ''}"
          >
            {#if !hasSecondaryPanel || sideTab === "messages"}
              <MessagesTab
                {threadId}
                postRouteScopeId={postRouteScopeId || threadId}
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
                pinComposer={pinComposerEff}
                pinComposerNarrow={pinComposerNarrowEff}
                pinComposerAlignThreadEnd={pinComposerAlignThreadEndResolved}
              />
            {:else if secondaryPanel}
              <div
                class="min-h-0 min-w-0 max-lg:flex-1 max-lg:overflow-y-auto lg:h-full lg:min-h-0 lg:overflow-y-auto"
              >
                {@render secondaryPanel()}
              </div>
            {/if}
          </div>
        </div>
      {/if}
    </div>
  {/if}
{/if}

<style>
  /*
   * Embedded dock placement is the shared "doc mobile chat, but inside a
   * bounded surface" mode. Hosts provide `data-discussion-dock-host`; this
   * component owns the feed/surface flex wiring so card-like surfaces do not
   * copy a local draggable chat layout.
   */
  :global(
    [data-discussion-dock-host]:has([data-discussion-dock-placement="embedded"])
  ) {
    position: relative;
    min-height: 0;
    --mobile-chat-panel-height: min(32dvh, 19rem);
  }

  :global(
    [data-discussion-dock-host]:has([data-discussion-dock-placement="embedded"])
      .page-dock-scroll
  ) {
    position: relative;
    z-index: 0;
    flex: 1 1 auto;
    min-height: 0;
    overflow-x: hidden;
    overflow-y: auto;
    -webkit-overflow-scrolling: touch;
    padding-bottom: 0.75rem;
  }

  :global(
    [data-discussion-dock-host]:has([data-discussion-dock-placement="embedded"])
      .page-dock-feed
  ) {
    position: relative;
    left: 0;
    right: 0;
    bottom: auto;
    isolation: isolate;
    /*
     * The expanded embedded dock can visually overlap modal chrome below it
     * (for example the card status row), so it must sit above the modal footer.
     */
    z-index: 35;
    display: flex;
    width: 100%;
    max-width: 100%;
    min-height: 0;
    flex-direction: column;
    box-sizing: border-box;
    margin: 0;
    overflow: hidden;
    box-shadow: none;
  }

  :global(
    [data-discussion-dock-host]:has([data-discussion-dock-placement="embedded"])
      .page-dock-feed:has(
        [data-discussion-dock-placement="embedded"]:not(
          [data-mobile-chat-expanded]
        )
      )
  ) {
    height: auto;
    max-height: none;
  }

  :global(
    [data-discussion-dock-host]:has([data-discussion-dock-placement="embedded"])
      .page-dock-feed:has(
        [data-discussion-dock-placement="embedded"][data-mobile-chat-expanded]
      )
  ) {
    height: var(--mobile-chat-panel-height);
    max-height: var(--mobile-chat-panel-height);
  }

  :global(
    [data-discussion-dock-host]:has([data-discussion-dock-placement="embedded"])
      .page-dock-feed
      > *
  ) {
    display: flex;
    min-height: 0;
    flex: 0 1 auto;
    flex-direction: column;
  }

  :global(
    [data-discussion-dock-host]:has([data-discussion-dock-placement="embedded"])
      .page-dock-feed:has(
        [data-discussion-dock-placement="embedded"][data-mobile-chat-expanded]
      )
      > *
  ) {
    height: 100%;
    max-height: 100%;
    min-height: 0;
    flex: 1 1 auto;
  }

  :global(
    [data-discussion-dock-host]:has([data-discussion-dock-placement="embedded"])
      .page-dock-feed
      [data-discussion-dock-placement="embedded"]
  ) {
    min-height: 0;
    flex: 0 1 auto;
    background: var(--panel);
  }

  :global(
    [data-discussion-dock-host]:has([data-discussion-dock-placement="embedded"])
      .page-dock-feed
      [data-discussion-dock-placement="embedded"][data-mobile-chat-expanded]
  ) {
    height: 100%;
    max-height: 100%;
    flex: 1 1 auto;
  }
</style>
