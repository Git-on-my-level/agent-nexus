<script>
  import { browser } from "$app/environment";
  import { onMount } from "svelte";
  import { writable } from "svelte/store";

  import { coreClient } from "$lib/coreClient";
  import {
    createTimelineContext,
    setTimelineContext,
  } from "$lib/timelineContext";
  import MessagesTab from "$lib/components/timeline/MessagesTab.svelte";

  /**
   * Width preference is global (it's a layout choice for the operator), but the
   * open/closed preference is per-document so closing discussion on one
   * heavyweight doc doesn't carry over to a fresh one (per polish §N4). We
   * still fall back to the legacy global open key on first read so existing
   * users don't suddenly see every rail re-open.
   */
  const LS_WIDTH_KEY = "doc-discussion-rail-width";
  const LS_OPEN_KEY_LEGACY = "doc-discussion-rail-open";
  const WIDTH_MIN = 280;
  const WIDTH_MAX = 520;
  const WIDTH_DEFAULT = 360;

  let { doc, workspaceSlug = "", workspaceId = "" } = $props();

  let railOpen = $state(true);
  let railWidth = $state(WIDTH_DEFAULT);
  /** Tracks which doc id we last hydrated open-state for, to avoid clobbering. */
  let openHydratedFor = $state("");

  function lsOpenKeyFor(/** @type {string} */ docId) {
    return `doc-discussion-rail-open:${docId}`;
  }

  onMount(() => {
    if (!browser) return;
    const w = Number.parseInt(
      String(localStorage.getItem(LS_WIDTH_KEY) ?? ""),
      10,
    );
    if (Number.isFinite(w)) {
      railWidth = Math.min(WIDTH_MAX, Math.max(WIDTH_MIN, w));
    }
  });

  function persistOpen(next) {
    railOpen = next;
    if (!browser) return;
    const id = String(doc?.id ?? "").trim();
    if (id) {
      localStorage.setItem(lsOpenKeyFor(id), next ? "1" : "0");
    }
  }

  function clampWidth(/** @type {number} */ next) {
    return Math.min(WIDTH_MAX, Math.max(WIDTH_MIN, next));
  }

  function persistWidthToStorage(/** @type {number} */ w) {
    if (browser) {
      localStorage.setItem(LS_WIDTH_KEY, String(w));
    }
  }

  /** @type {number} */
  let resizeStartX = 0;
  /** @type {number} */
  let resizeStartWidth = 0;
  let railResizing = $state(false);

  function endRailResize(/** @type {PointerEvent | null} */ e) {
    const wasResizing = railResizing;
    if (e?.currentTarget && "hasPointerCapture" in e.currentTarget) {
      try {
        if (
          (/** @type {HTMLElement} */ (e.currentTarget)).hasPointerCapture(
            e.pointerId,
          )
        ) {
          (/** @type {HTMLElement} */ (e.currentTarget)).releasePointerCapture(
            e.pointerId,
          );
        }
      } catch {
        // ignore
      }
    }
    if (wasResizing) {
      persistWidthToStorage(railWidth);
    }
    railResizing = false;
    if (browser) {
      document.body.style.removeProperty("cursor");
      document.body.style.removeProperty("user-select");
    }
  }

  function onResizePointerDown(/** @type {PointerEvent} */ e) {
    if (e.button !== 0) return;
    e.preventDefault();
    resizeStartX = e.clientX;
    resizeStartWidth = railWidth;
    railResizing = true;
    (/** @type {HTMLElement} */ (e.currentTarget)).setPointerCapture(
      e.pointerId,
    );
    if (browser) {
      document.body.style.cursor = "ew-resize";
      document.body.style.userSelect = "none";
    }
  }

  function onResizePointerMove(/** @type {PointerEvent} */ e) {
    if (!railResizing) return;
    const dx = e.clientX - resizeStartX;
    railWidth = clampWidth(resizeStartWidth - dx);
  }

  function onResizePointerUp(/** @type {PointerEvent} */ e) {
    if (!railResizing) return;
    endRailResize(e);
  }

  /**
   * Per polish §P7: this rail intentionally creates an isolated
   * `timelineContext` rather than reusing the parent topic page's store.
   * Reasons:
   *   1. The doc page rarely also embeds the topic-wide Messages tab, so
   *      sharing buys little and adds coupling.
   *   2. The MessagesTab's `subjectRefFilter` here narrows to the document,
   *      so a shared store would still need to keep all topic events around;
   *      the isolated store stays small.
   *   3. After posting from this rail, only this rail needs to refresh; the
   *      parent topic timeline (when visible elsewhere) is not auto-refreshed.
   *      Operators returning to a topic page get a fresh load anyway.
   * If we later add cross-pane real-time sync, this comment is the place to
   * revisit and switch to a shared SSE-backed store.
   */
  const timelineWorkspaceSlug = writable("");
  const timelineApi = createTimelineContext(coreClient);

  $effect.pre(() => {
    timelineWorkspaceSlug.set(String(workspaceSlug ?? ""));
  });

  setTimelineContext({
    store: timelineApi.store,
    workspaceSlug: timelineWorkspaceSlug,
    refreshTimeline: () => timelineApi.refreshTimeline(),
  });

  let threadId = $derived(String(doc?.thread_id ?? "").trim());
  let docId = $derived(String(doc?.id ?? "").trim());
  let documentRef = $derived(docId ? `document:${docId}` : "");

  /**
   * Rehydrate `railOpen` whenever we land on a different document. Falls back
   * to the legacy global key for backward compatibility so users keep their
   * "always closed" / "always open" preference on first migration.
   */
  $effect(() => {
    if (!browser) return;
    const id = docId;
    if (!id || openHydratedFor === id) return;
    openHydratedFor = id;
    const perDoc = localStorage.getItem(lsOpenKeyFor(id));
    if (perDoc === "1") {
      railOpen = true;
      return;
    }
    if (perDoc === "0") {
      railOpen = false;
      return;
    }
    const legacy = localStorage.getItem(LS_OPEN_KEY_LEGACY);
    if (legacy === "0") railOpen = false;
    else if (legacy === "1") railOpen = true;
  });

  $effect(() => {
    if (threadId) {
      void timelineApi.loadTimeline(threadId);
    }
  });

  async function handleMessagePost(routeScopeId, event) {
    await coreClient.createEvent({ event });
    await timelineApi.refreshTimeline();
  }

  const DOC_DISCUSSION_EMPTY =
    "Discussion about this doc goes here. Posts are part of the topic's thread, filtered to this doc.";
</script>

{#if threadId && docId}
  <aside
    class="w-full border-t border-[var(--line)] bg-[var(--panel)] lg:shrink-0 lg:border-t-0 lg:border-l {railOpen
      ? 'lg:w-[var(--doc-rail-w)]'
      : 'lg:w-14'}"
    style={railOpen ? `--doc-rail-w:${railWidth}px` : undefined}
  >
    {#if !railOpen}
      <!--
        Per polish §P6: on large screens we use a square icon-only button so
        narrow rails localize cleanly (no rotated/cramped text). On mobile the
        rail collapses inline and a full-width text button is far more
        recognisable than an icon.
      -->
      <div
        class="px-3 py-2 lg:flex lg:h-full lg:min-h-[12rem] lg:items-start lg:justify-center lg:px-0 lg:py-3"
      >
        <button
          type="button"
          class="w-full text-micro font-medium text-accent-text hover:text-accent-text lg:hidden"
          onclick={() => persistOpen(true)}
        >
          Show discussion
        </button>
        <button
          type="button"
          class="hidden lg:inline-flex lg:h-8 lg:w-8 lg:cursor-pointer lg:items-center lg:justify-center lg:rounded-full lg:border lg:border-[var(--line)] lg:bg-[var(--panel)] lg:text-[var(--fg-muted)] lg:shadow-sm lg:transition-colors lg:hover:bg-[var(--bg-soft)] lg:hover:text-[var(--fg)]"
          aria-label="Show discussion"
          title="Show discussion"
          onclick={() => persistOpen(true)}
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
              d="M15 19l-7-7 7-7"
            />
          </svg>
        </button>
      </div>
    {:else}
      <div
        class="flex max-h-[min(70vh,44rem)] w-full min-w-0 flex-col lg:sticky lg:top-4 lg:max-h-[calc(100vh-5rem)] lg:flex-row"
      >
        <!-- Draggable left edge (desktop); width remains railWidth for whole aside. -->
        <div class="hidden shrink-0 lg:relative lg:block lg:w-3">
          <div
            role="separator"
            aria-orientation="vertical"
            aria-label="Drag to resize discussion panel"
            title="Drag to resize"
            class="group absolute inset-y-0 -left-1 z-[1] flex w-3 cursor-ew-resize touch-none select-none items-stretch pl-0.5 {railResizing
              ? 'ring-1 ring-[var(--line)]'
              : ''}"
            onpointerdown={onResizePointerDown}
            onpointermove={onResizePointerMove}
            onpointerup={onResizePointerUp}
            onpointercancel={onResizePointerUp}
            onlostpointercapture={() => endRailResize(null)}
          >
            <div
              class="mx-auto h-full w-px bg-[var(--line)] transition-opacity group-hover:opacity-100 {railResizing
                ? 'bg-accent-text opacity-100'
                : 'opacity-60'}"
            ></div>
          </div>
        </div>
        <div class="flex min-h-0 min-w-0 flex-1 flex-col">
          <div
            class="flex shrink-0 items-center justify-between gap-2 border-b border-[var(--line)] px-2 py-2 pr-2.5"
          >
            <h2 class="min-w-0 text-meta font-medium text-[var(--fg)]">
              Discussion
            </h2>
            <button
              type="button"
              class="inline-flex h-8 w-8 shrink-0 cursor-pointer items-center justify-center rounded-full border border-[var(--line)] bg-[var(--panel)] text-[var(--fg-muted)] shadow-sm transition-colors hover:bg-[var(--bg-soft)] hover:text-[var(--fg)]"
              onclick={() => persistOpen(false)}
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
          <div class="min-h-0 flex-1 overflow-y-auto px-3 py-3">
            <MessagesTab
              {threadId}
              postRouteScopeId={docId}
              workspaceId={String(workspaceId ?? "")}
              onMessagePost={handleMessagePost}
              subjectRefFilter={documentRef}
              extraPostRefs={[documentRef]}
              discussionEmptyMessage={DOC_DISCUSSION_EMPTY}
            />
          </div>
        </div>
      </div>
    {/if}
  </aside>
{/if}
