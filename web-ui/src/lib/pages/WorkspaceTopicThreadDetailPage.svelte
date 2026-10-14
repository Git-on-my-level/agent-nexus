<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { onMount } from "svelte";
  import { get, derived } from "svelte/store";

  import { coreClient } from "$lib/coreClient";
  import { localWorkScope } from "$lib/localWorkScope.js";
  import { topicDetailStore } from "$lib/topicDetailStore";
  import { setTimelineContext } from "$lib/timelineContext";
  import {
    readEnumSearchParamWithAliases,
    withUpdatedSearchParams,
  } from "$lib/urlState";

  import TopicDetailHeader from "$lib/components/topic-detail/TopicDetailHeader.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import WorkspaceResourceTabList from "$lib/components/WorkspaceResourceTabList.svelte";
  import TopicOverviewTab from "$lib/components/topic-detail/TopicOverviewTab.svelte";
  import TopicDocumentsPanel from "$lib/components/topic-detail/TopicDocumentsPanel.svelte";
  import DiscussionDrawer from "$lib/components/DiscussionDrawer.svelte";
  import { topicDiscussionSurface } from "$lib/discussionSurface";
  import TimelineTab from "$lib/components/timeline/TimelineTab.svelte";
  import { usePageTitle } from "$lib/pageTitle.svelte.js";

  const TOPIC_DETAIL_TABS = ["messages", "about", "documents", "timeline"];
  const TOPIC_TAB_ALIASES = { overview: "about" };

  let { data } = $props();
  /** Canonical id for the URL (backing thread id on /threads/…). */
  let threadId = $derived(
    data?.topicId || $page.params.topicId || $page.params.threadId,
  );

  const timelineSlice = derived(topicDetailStore, ($s) => ({
    timeline: $s.timeline,
    timelineLoading: $s.timelineLoading,
    timelineError: $s.timelineError,
    timelineArtifacts: $s.timelineArtifacts ?? [],
    timelineCards: $s.timelineCards ?? [],
    timelineDocuments: $s.timelineDocuments ?? [],
    timelineDocumentRevisions: $s.timelineDocumentRevisions ?? [],
    timelineThreads: $s.timelineThreads ?? [],
    timelineNotificationReceipts: $s.timelineNotificationReceipts ?? {},
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

  let workspaceSlug = $derived(String($page.params.workspace ?? ""));

  let topicLoading = $derived($topicDetailStore.topicLoading);
  let topicError = $derived($topicDetailStore.topicError);

  let requestedTab = $derived(
    readEnumSearchParamWithAliases(
      $page.url.searchParams,
      "tab",
      TOPIC_DETAIL_TABS,
      TOPIC_TAB_ALIASES,
      "",
    ),
  );
  let activeTab = $derived(requestedTab || "messages");

  let documentCount = $derived($topicDetailStore.documents.length);

  let topicTabItems = $derived([
    { id: "messages", label: "Messages" },
    { id: "about", label: "About" },
    { id: "documents", label: "Docs", badge: documentCount },
    { id: "timeline", label: "Timeline" },
  ]);

  const STREAM_RECONNECT_DELAY_MS = 1_500;
  const RECONCILE_INTERVAL_MS = 120_000;

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

    if (!routeId) {
      return;
    }

    const coordination = {
      stopEventStream: () => {},
      stopReceiptStream: () => {},
      reconcileTimer: null,
    };
    let cancelled = false;

    void (async () => {
      await topicDetailStore.fullRefresh(routeId);
      if (cancelled) return;

      const state = get(topicDetailStore);
      const streamThreadId = String(state.topic?.id ?? "").trim() || routeId;
      const latestKnownEventId = getLatestKnownEventId(state.timeline);
      coordination.stopEventStream = startThreadEventStream(
        streamThreadId,
        routeId,
        latestKnownEventId,
      );
      coordination.stopReceiptStream =
        startNotificationReceiptStream(streamThreadId);
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
      coordination.stopEventStream();
      coordination.stopReceiptStream();
      if (coordination.reconcileTimer) {
        clearInterval(coordination.reconcileTimer);
      }
    };
  });

  // Display refresh must outlive the About tab. The editor is destroyed when
  // leaving About, but confirmation still has to update the header snapshot.
  onMount(() => {
    const confirmed = (event) => {
      const topicId =
        /^topic:(.+)$/.exec(String(topic?.topic_ref ?? ""))?.[1] || "";
      const target = topicId ? `topic:${topicId}:metadata` : "";
      if (
        event.detail.scope !== get(localWorkScope) ||
        event.detail.target !== target
      )
        return;
      if (!threadId) return;
      void topicDetailStore.queueRefreshTopicDetail(threadId, {
        workspace: true,
        timeline: true,
      });
    };
    window.addEventListener("anx:mutation-confirmed", confirmed);
    return () =>
      window.removeEventListener("anx:mutation-confirmed", confirmed);
  });

  /** @param {string} routeScopeId topic id or thread id for listTopicTimeline / refresh coalescing */
  async function handleMessagePost(routeScopeId, result) {
    await topicDetailStore.queueRefreshTopicDetail(routeScopeId, {
      workspace: true,
      timeline: true,
    });
    return result;
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
        await coreClient.streamEvents({
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
        if (error?.status === 401 || error?.status === 403) {
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

  function startNotificationReceiptStream(streamThreadId) {
    let stopped = false;
    let reconnectTimer;
    let controller = null;
    let lastEventId = "";

    const connect = async () => {
      if (stopped) return;
      controller = new AbortController();
      try {
        await coreClient.streamNotificationReceipts({
          threadId: streamThreadId,
          lastEventId,
          signal: controller.signal,
          onReceipt: (receipt, message) => {
            if (message?.id) {
              lastEventId = message.id;
            }
            topicDetailStore.patchNotificationReceipt(receipt);
          },
        });
      } catch (error) {
        if (error?.name === "AbortError" || stopped) {
          return;
        }
        if (error?.status === 401 || error?.status === 403) {
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
      void topicDetailStore.loadTimeline(threadId);
    }
  });

  async function setActiveTab(tabId) {
    await goto(withUpdatedSearchParams($page.url, { tab: tabId }), {
      noScroll: true,
      keepFocus: true,
    });
  }

  usePageTitle(() => `${topic?.title || "Thread"} · Agent Nexus`);
</script>

<!--
  When the Messages tab is active we wrap the page in `page-dock-layout` so
  the composer stays pinned to the bottom of the page area (viewport on
  mobile, topic column on desktop) with the same chrome as boards and docs.
  Other tabs render in normal page flow.
-->
{#if topicLoading}
  <TopicDetailHeader {threadId} dense />
  <div aria-busy="true">
    <p class="sr-only" role="status">Loading thread…</p>
    <Skeleton rows={4} class="mt-3 max-w-2xl" />
  </div>
{:else if topicError}
  <TopicDetailHeader {threadId} dense />
  <StateError
    title="This thread didn't load"
    message={topicError}
    onretry={() => void topicDetailStore.fullRefresh(threadId)}
  />
{:else if !topic}
  <TopicDetailHeader {threadId} dense />
  <p class="text-meta text-fg-muted">Thread not found.</p>
{:else}
  {@const isMessagesTab = activeTab === "messages"}
  <div
    class={isMessagesTab
      ? "page-dock-layout page-dock-layout--fixed-mobile-chat page-dock-layout--topic-messages"
      : ""}
  >
    <div class={isMessagesTab ? "page-dock-head max-lg:px-4" : "contents"}>
      <TopicDetailHeader {threadId} dense />

      <WorkspaceResourceTabList
        ariaLabel="Topic sections"
        tabs={topicTabItems}
        {activeTab}
        onTabChange={(tabId) => void setActiveTab(tabId)}
        dense
      />
    </div>

    {#if activeTab === "about"}
      <div role="tabpanel" tabindex="0">
        {#key topic.topic_ref || threadId}
          <TopicOverviewTab {threadId} />
        {/key}
      </div>
    {/if}

    {#if activeTab === "documents"}
      <div role="tabpanel" tabindex="0">
        <TopicDocumentsPanel {threadId} />
      </div>
    {/if}
    {#if activeTab === "messages"}
      <div class="page-dock-feed" role="tabpanel" tabindex="0">
        <DiscussionDrawer
          {...topicDiscussionSurface(topic)}
          postRouteScopeId={threadId}
          workspaceId={data?.workspaceId ?? ""}
          {workspaceSlug}
          useParentTimelineContext={true}
          onMessagePost={handleMessagePost}
          pinComposerAlignThreadEnd={false}
        />
      </div>
    {/if}

    {#if activeTab === "timeline"}
      <div class="pt-1" role="tabpanel" tabindex="0">
        <TimelineTab threadId={String(topic.id)} />
      </div>
    {/if}
  </div>
{/if}
