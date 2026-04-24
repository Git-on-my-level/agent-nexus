<script>
  import { writable } from "svelte/store";

  import { boardBackingThreadId } from "$lib/boardUtils";
  import { coreClient } from "$lib/coreClient";
  import {
    createTimelineContext,
    setTimelineContext,
  } from "$lib/timelineContext";
  import MessagesTab from "$lib/components/timeline/MessagesTab.svelte";

  /**
   * Board-level thread (announcements) above columns.
   *
   * Per polish §P7 the timeline context is intentionally isolated rather than
   * shared with any parent topic store: the board-level thread is a separate
   * conversation and merging would mean every board page hauls in the parent
   * topic's full timeline. After posting from this strip we refresh only the
   * isolated store; revisiting the parent topic will reload its own timeline.
   *
   * Per polish §P10 the timeline fetch is deferred until the operator opens
   * the strip the first time (or posts a message). Most boards will never
   * expand it, so eagerly fetching N events per board view is wasteful.
   *
   * Pin per message: deferred until core exposes a pin API (no UI-only
   * persistence).
   */
  let { board, workspaceSlug = "", workspaceId = "" } = $props();

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

  let threadId = $derived(boardBackingThreadId(board));
  let expanded = $state(false);
  let timelineEverLoaded = $state(false);

  function expandAndLoad() {
    expanded = !expanded;
    if (expanded && threadId && !timelineEverLoaded) {
      timelineEverLoaded = true;
      void timelineApi.loadTimeline(threadId);
    }
  }

  const timelineStore = timelineApi.store;
  let timeline = $derived($timelineStore.timeline);
  let timelineLoading = $derived($timelineStore.timelineLoading);

  function eventTimeMs(e) {
    const ts = e?.ts;
    if (ts == null || ts === "") {
      return 0;
    }
    const ms = Date.parse(String(ts));
    return Number.isFinite(ms) ? ms : 0;
  }

  function messageBodyPreview(event) {
    const t =
      typeof event?.payload?.text === "string" ? event.payload.text.trim() : "";
    if (t) {
      return t;
    }
    const s = String(event?.summary ?? "").trim();
    if (s.startsWith("Message: ")) {
      return s.slice("Message: ".length).trim();
    }
    return s || "—";
  }

  function truncate(s, max = 100) {
    const str = String(s ?? "");
    if (str.length <= max) {
      return str;
    }
    return `${str.slice(0, max - 1)}…`;
  }

  let boardMessageEvents = $derived(
    (Array.isArray(timeline) ? timeline : []).filter(
      (e) => String(e?.type ?? "") === "message_posted" && !e?.trashed_at,
    ),
  );

  let messageCount = $derived(boardMessageEvents.length);

  let previewLines = $derived.by(() => {
    const list = [...boardMessageEvents];
    list.sort((a, b) => eventTimeMs(a) - eventTimeMs(b));
    const newest = list.slice(-3).reverse();
    return newest.map((e) => truncate(messageBodyPreview(e), 90));
  });

  async function handleMessagePost(routeScopeId, event) {
    await coreClient.createEvent({ event });
    await timelineApi.refreshTimeline();
  }

  const DISCUSSION_EMPTY =
    "Board-wide updates and discussion live here, separate from individual card threads. Post a short note the whole board should see — triage callouts, column policy, or sprint boundaries.";
</script>

{#if threadId}
  <div
    class="mb-3 rounded-md border border-[var(--line)] bg-[var(--panel)]"
    data-board-feed
  >
    <button
      type="button"
      class="flex w-full items-start gap-2 px-3 py-2.5 text-left transition-colors hover:bg-[var(--line-subtle)]"
      aria-expanded={expanded}
      aria-controls="board-feed-messages-panel"
      id="board-feed-toggle"
      onclick={expandAndLoad}
    >
      <svg
        class="mt-0.5 h-3.5 w-3.5 shrink-0 text-[var(--fg-muted)] transition-transform {expanded
          ? 'rotate-90'
          : ''}"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
        aria-hidden="true"><path d="M9 5l7 7-7 7" /></svg
      >
      <div class="min-w-0 flex-1">
        <div
          class="text-micro font-medium text-[var(--fg)]"
          id="board-feed-label"
        >
          Board announcements
        </div>
        {#if !expanded}
          <div
            class="mt-1 flex flex-col gap-1 sm:flex-row sm:items-baseline sm:gap-x-2"
          >
            {#if !timelineEverLoaded}
              <p class="text-micro text-[var(--fg-muted)]">
                Open to view announcements
              </p>
            {:else if timelineLoading && messageCount === 0}
              <p class="text-micro text-[var(--fg-muted)]">Loading messages…</p>
            {:else if messageCount === 0}
              <p class="text-micro text-[var(--fg-muted)]">No messages yet</p>
            {:else}
              <p
                class="text-micro text-[var(--fg-muted)]"
                data-board-feed-count
              >
                {messageCount}
                {messageCount === 1 ? "message" : "messages"}
              </p>
              {#if previewLines.length > 0}
                <ul
                  class="min-w-0 list-none space-y-0.5 text-micro text-[var(--fg-muted)]"
                >
                  {#each previewLines as line, i (i)}
                    <li class="truncate" title={line}>
                      {line}
                    </li>
                  {/each}
                </ul>
              {/if}
            {/if}
          </div>
        {/if}
      </div>
    </button>

    {#if expanded}
      <div
        class="border-t border-[var(--line)] px-3 py-3"
        id="board-feed-messages-panel"
        role="region"
        aria-labelledby="board-feed-label"
      >
        <div class="max-h-[min(50vh,28rem)] min-h-0 overflow-y-auto pr-0.5">
          <MessagesTab
            {threadId}
            postRouteScopeId={threadId}
            onMessagePost={handleMessagePost}
            workspaceId={String(workspaceId ?? "")}
            discussionEmptyMessage={DISCUSSION_EMPTY}
            allowActivityInterleave={false}
          />
        </div>
      </div>
    {/if}
  </div>
{/if}
