<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { get, derived } from "svelte/store";
  import { page } from "$app/stores";

  import { coreClient } from "$lib/coreClient";
  import { topicDetailStore } from "$lib/topicDetailStore";
  import { setTimelineContext } from "$lib/timelineContext";
  import { readEnumSearchParam, withUpdatedSearchParams } from "$lib/urlState";

  import TopicDetailHeader from "$lib/components/topic-detail/TopicDetailHeader.svelte";
  import TopicOverviewTab from "$lib/components/topic-detail/TopicOverviewTab.svelte";
  import TopicBoardsPanel from "$lib/components/topic-detail/TopicBoardsPanel.svelte";
  import TopicDocumentsPanel from "$lib/components/topic-detail/TopicDocumentsPanel.svelte";
  import MessagesTab from "$lib/components/timeline/MessagesTab.svelte";
  import TimelineTab from "$lib/components/timeline/TimelineTab.svelte";

  const TOPIC_DETAIL_TABS = ["overview", "messages", "timeline"];

  let { data } = $props();
  /** Canonical id for the URL (topic id on /topics/…, else backing thread id on /threads/…). */
  let threadId = $derived(
    data?.topicId || $page.params.topicId || $page.params.threadId,
  );
  let detailAsTopic = $derived(
    data?.detailScope === "topic" || Boolean($page.params.topicId),
  );

  const timelineSlice = derived(topicDetailStore, ($s) => ({
    timeline: $s.timeline,
    timelineLoading: $s.timelineLoading,
    timelineError: $s.timelineError,
  }));

  const timelineWorkspaceSlug = derived(page, ($p) =>
    String($p.params.workspace ?? ""),
  );

  setTimelineContext({
    store: timelineSlice,
    workspaceSlug: timelineWorkspaceSlug,
    refreshTimeline: async () => {
      const p = get(page);
      const id = String(p.params.topicId || p.params.threadId || "").trim();
      if (!id) return;
      await topicDetailStore.queueRefreshTopicDetail(id, { timeline: true });
    },
  });

  let topic = $derived($topicDetailStore.topic);

  let topicLoading = $derived($topicDetailStore.topicLoading);
  let topicError = $derived($topicDetailStore.topicError);

  let requestedTab = $derived(
    readEnumSearchParam($page.url.searchParams, "tab", TOPIC_DETAIL_TABS, ""),
  );
  let activeTab = $derived(requestedTab || "overview");

  let conflictWarning = $state("");
  let editNotice = $state("");

  const STREAM_RECONNECT_DELAY_MS = 1_500;
  const RECONCILE_INTERVAL_MS = 120_000;

  const TOPIC_TYPES = new Set([
    "initiative",
    "objective",
    "decision",
    "incident",
    "risk",
    "request",
    "note",
    "other",
  ]);

  function topicIdFromTopic(topicRow) {
    const ref = String(topicRow?.topic_ref ?? "").trim();
    const match = /^topic:(.+)$/.exec(ref);
    return match ? match[1].trim() : "";
  }

  function threadTypeToTopicType(type) {
    const t = String(type ?? "").trim();
    if (TOPIC_TYPES.has(t)) return t;
    return "other";
  }

  /** Maps overview edits to canonical TopicPatchInput fields. */
  function topicEditPatchToTopicPatch(topicEditPatch) {
    const out = {};
    if (topicEditPatch.title !== undefined) out.title = topicEditPatch.title;
    if (topicEditPatch.current_summary !== undefined) {
      out.summary = topicEditPatch.current_summary;
    }
    if (topicEditPatch.type !== undefined) {
      out.type = threadTypeToTopicType(topicEditPatch.type);
    }
    if (topicEditPatch.status !== undefined) {
      const s = String(topicEditPatch.status ?? "").trim();
      if (s) out.status = s;
    }
    return out;
  }

  function getLatestKnownEventId(events) {
    let latestEventId = "";
    let latestEventTs = Number.NEGATIVE_INFINITY;

    for (const event of Array.isArray(events) ? events : []) {
      const eventId = String(event?.id ?? "").trim();
      if (!eventId) continue;

      const eventTs = Date.parse(String(event?.ts ?? ""));
      if (!Number.isFinite(eventTs)) {
        if (!latestEventId || eventId.localeCompare(latestEventId) > 0) {
          latestEventId = eventId;
        }
        continue;
      }

      if (
        eventTs > latestEventTs ||
        (eventTs === latestEventTs && eventId.localeCompare(latestEventId) > 0)
      ) {
        latestEventId = eventId;
        latestEventTs = eventTs;
      }
    }

    return latestEventId;
  }

  /** Live SSE + periodic reconcile: re-run when the topic/thread route identity changes (client navigations). */
  $effect(() => {
    if (!browser) return;

    const routeId = String(
      data?.topicId || $page.params.topicId || $page.params.threadId || "",
    ).trim();
    const asTopic =
      data?.detailScope === "topic" || Boolean($page.params.topicId);

    if (!routeId) {
      return;
    }

    const coordination = { stopStream: () => {}, reconcileTimer: null };
    let cancelled = false;

    void (async () => {
      await topicDetailStore.fullRefresh(routeId, { asTopic });
      if (cancelled) return;

      if (get(topicDetailStore).detailAsTopic) {
        await topicDetailStore.loadTimeline(routeId);
      }
      if (cancelled) return;

      const state = get(topicDetailStore);
      const streamThreadId = String(state.topic?.id ?? "").trim() || routeId;
      const latestKnownEventId = getLatestKnownEventId(state.timeline);
      coordination.stopStream = startThreadEventStream(
        streamThreadId,
        routeId,
        latestKnownEventId,
      );
      coordination.reconcileTimer = setInterval(
        () =>
          topicDetailStore.queueRefreshTopicDetail(routeId, {
            workspace: true,
            timeline: true,
          }),
        RECONCILE_INTERVAL_MS,
      );
    })();

    return () => {
      cancelled = true;
      coordination.stopStream();
      if (coordination.reconcileTimer) {
        clearInterval(coordination.reconcileTimer);
      }
    };
  });

  async function handleSaveTopic(threadId, patch, ifUpdatedAt) {
    conflictWarning = "";
    editNotice = "";
    const topicRow = get(topicDetailStore).topic;
    const topicId = topicIdFromTopic(topicRow);
    if (!topicId) {
      throw new Error(
        "Missing topic reference on this topic row; cannot save edits.",
      );
    }
    const topicPatch = topicEditPatchToTopicPatch(patch);
    if (Object.keys(topicPatch).length === 0) {
      editNotice = "No topic-level fields changed.";
      return;
    }
    try {
      await coreClient.updateTopic(topicId, {
        patch: {
          ...topicPatch,
          provenance: { sources: ["actor_statement:ui"] },
        },
        if_updated_at: ifUpdatedAt,
      });
      await topicDetailStore.queueRefreshTopicDetail(threadId, {
        workspace: true,
        timeline: true,
      });
      editNotice = "Changes saved.";
    } catch (error) {
      if (error?.status === 409) {
        conflictWarning =
          "Topic was updated elsewhere. Reloaded — reapply your changes.";
        await topicDetailStore.queueRefreshTopicDetail(threadId, {
          workspace: true,
          timeline: true,
        });
      } else {
        throw error;
      }
    }
  }

  /** @param {string} routeScopeId topic id or thread id for listTopicTimeline / refresh coalescing */
  async function handleMessagePost(routeScopeId, event) {
    await coreClient.createEvent({ event });
    await topicDetailStore.queueRefreshTopicDetail(routeScopeId, {
      workspace: true,
      timeline: true,
    });
  }

  function startThreadEventStream(
    streamThreadId,
    refreshRouteId,
    initialLastEventId = "",
  ) {
    let stopped = false;
    let reconnectTimer;
    let controller = null;
    let lastEventId = String(initialLastEventId ?? "").trim();

    const connect = async () => {
      if (stopped) return;
      if (!lastEventId) {
        lastEventId = getLatestKnownEventId(get(topicDetailStore).timeline);
      }
      controller = new AbortController();
      try {
        await coreClient.streamThreadEvents({
          threadId: streamThreadId,
          lastEventId,
          signal: controller.signal,
          onEvent: async (message) => {
            if (message?.id) {
              lastEventId = message.id;
            }
            if (message?.event !== "event") {
              return;
            }
            await topicDetailStore.queueRefreshTopicDetail(refreshRouteId, {
              workspace: true,
              timeline: true,
            });
          },
        });
      } catch (error) {
        if (error?.name === "AbortError" || stopped) {
          return;
        }
      }

      if (!stopped) {
        reconnectTimer = setTimeout(connect, STREAM_RECONNECT_DELAY_MS);
      }
    };

    void connect();

    return () => {
      stopped = true;
      controller?.abort();
      clearTimeout(reconnectTimer);
    };
  }

  $effect(() => {
    if ((activeTab === "messages" || activeTab === "timeline") && threadId) {
      void topicDetailStore.loadTimeline(threadId, {
        asTopic: detailAsTopic,
      });
    }
  });

  async function setActiveTab(tabId) {
    await goto(withUpdatedSearchParams($page.url, { tab: tabId }), {
      noScroll: true,
      keepFocus: true,
    });
  }
</script>

<TopicDetailHeader {threadId} {detailAsTopic} />

{#if topicLoading}
  <p class="text-[13px] text-[var(--ui-text-muted)]">Loading...</p>
{:else if topicError}
  <p class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
    {topicError}
  </p>
{:else if !topic}
  <p class="text-[13px] text-[var(--ui-text-muted)]">
    {detailAsTopic ? "Topic not found." : "Thread not found."}
  </p>
{:else}
  <div
    class="mt-3 flex gap-0 border-b border-[var(--ui-border)]"
    aria-label="Topic sections"
    role="tablist"
  >
    {#each [["overview", "Overview"], ["messages", "Messages"], ["timeline", "Timeline"]] as [tabId, tabLabel]}
      <button
        class={`relative cursor-pointer px-3 py-2 text-[13px] font-medium transition-colors ${activeTab === tabId ? "text-[var(--ui-text)]" : "text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]"}`}
        onclick={() => void setActiveTab(tabId)}
        type="button"
        role="tab"
        aria-selected={activeTab === tabId}
        tabindex={activeTab === tabId ? 0 : -1}
      >
        {tabLabel}
        {#if activeTab === tabId}
          <span
            class="pointer-events-none absolute inset-x-0 -bottom-px h-0.5 bg-indigo-500"
          ></span>
        {/if}
      </button>
    {/each}
  </div>

  {#if activeTab === "overview"}
    <div role="tabpanel" tabindex="0">
      <TopicOverviewTab
        {threadId}
        onSave={handleSaveTopic}
        {conflictWarning}
        {editNotice}
      />
      <TopicBoardsPanel {threadId} />
      <TopicDocumentsPanel {threadId} />
    </div>
  {/if}

  {#if activeTab === "messages"}
    <div role="tabpanel" tabindex="0">
      <MessagesTab
        threadId={String(topic.id)}
        postRouteScopeId={threadId}
        onMessagePost={handleMessagePost}
        workspaceId={data?.workspaceId ?? ""}
      />
    </div>
  {/if}

  {#if activeTab === "timeline"}
    <div role="tabpanel" tabindex="0">
      <TimelineTab threadId={String(topic.id)} />
    </div>
  {/if}
{/if}
