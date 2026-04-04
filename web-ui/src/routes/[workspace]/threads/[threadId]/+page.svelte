<script>
  import { goto } from "$app/navigation";
  import { onMount, onDestroy } from "svelte";
  import { get } from "svelte/store";
  import { page } from "$app/stores";

  import { coreClient } from "$lib/coreClient";
  import { threadDetailStore } from "$lib/threadDetailStore";
  import { readEnumSearchParam, withUpdatedSearchParams } from "$lib/urlState";

  import ThreadDetailHeader from "$lib/components/thread-detail/ThreadDetailHeader.svelte";
  import ThreadOverviewTab from "$lib/components/thread-detail/ThreadOverviewTab.svelte";
  import ThreadBoardsPanel from "$lib/components/thread-detail/ThreadBoardsPanel.svelte";
  import ThreadDocumentsPanel from "$lib/components/thread-detail/ThreadDocumentsPanel.svelte";
  import ThreadCommitmentsPanel from "$lib/components/thread-detail/ThreadCommitmentsPanel.svelte";
  import ThreadMessagesTab from "$lib/components/thread-detail/ThreadMessagesTab.svelte";
  import ThreadWorkTab from "$lib/components/thread-detail/ThreadWorkTab.svelte";
  import ThreadTimelineTab from "$lib/components/thread-detail/ThreadTimelineTab.svelte";

  const THREAD_DETAIL_TABS = ["overview", "work", "messages", "timeline"];

  let { data } = $props();
  let threadId = $derived($page.params.topicId || $page.params.threadId);

  let snapshot = $derived($threadDetailStore.snapshot);
  let snapshotLoading = $derived($threadDetailStore.snapshotLoading);
  let snapshotError = $derived($threadDetailStore.snapshotError);

  let requestedTab = $derived(
    readEnumSearchParam($page.url.searchParams, "tab", THREAD_DETAIL_TABS, ""),
  );
  let activeTab = $derived(
    requestedTab ||
      ($page.url.searchParams.get("compose") === "work-order"
        ? "work"
        : "overview"),
  );

  let conflictWarning = $state("");
  let editNotice = $state("");

  const STREAM_RECONNECT_DELAY_MS = 1_500;
  const RECONCILE_INTERVAL_MS = 120_000;
  const liveCoordination = {
    reconcileTimer: null,
    stopThreadStream: () => {},
  };

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

  onMount(async () => {
    await threadDetailStore.fullRefresh(threadId);
    const latestKnownEventId = getLatestKnownEventId(
      get(threadDetailStore).timeline,
    );
    liveCoordination.stopThreadStream = startThreadEventStream(
      threadId,
      latestKnownEventId,
    );
    liveCoordination.reconcileTimer = setInterval(
      () =>
        threadDetailStore.queueRefreshThreadDetail(threadId, {
          workspace: true,
          timeline: true,
          workOrders: true,
        }),
      RECONCILE_INTERVAL_MS,
    );
  });

  onDestroy(() => {
    liveCoordination.stopThreadStream();
    clearInterval(liveCoordination.reconcileTimer);
  });

  async function handleSaveThread(threadId, patch, ifUpdatedAt) {
    conflictWarning = "";
    editNotice = "";
    try {
      const response = await coreClient.updateThread(threadId, {
        patch,
        if_updated_at: ifUpdatedAt,
      });
      threadDetailStore.setSnapshot(
        response.thread ?? $threadDetailStore.snapshot,
      );
      editNotice = "Changes saved.";
    } catch (error) {
      if (error?.status === 409) {
        conflictWarning =
          "Topic was updated elsewhere. Reloaded — reapply your changes.";
        await threadDetailStore.queueRefreshThreadDetail(threadId, {
          workspace: true,
          timeline: true,
          workOrders: true,
        });
      } else {
        throw error;
      }
    }
  }

  async function handleCreateCommitment(threadId, commitment) {
    await coreClient.createCommitment({ commitment });
    await threadDetailStore.queueRefreshThreadDetail(threadId, {
      workspace: true,
      timeline: true,
    });
  }

  async function handleSaveCommitment(commitmentId, payload) {
    await coreClient.updateCommitment(commitmentId, payload);
    await threadDetailStore.queueRefreshThreadDetail(threadId, {
      workspace: true,
      timeline: true,
    });
  }

  async function handleWorkOrderSubmit(threadId, artifact, packet, requestKey) {
    const response = await coreClient.createWorkOrder({
      request_key: requestKey,
      artifact,
      packet,
    });
    await threadDetailStore.queueRefreshThreadDetail(threadId, {
      workspace: true,
      timeline: true,
      workOrders: true,
    });
    return response;
  }

  async function handleReceiptSubmit(threadId, artifact, packet, requestKey) {
    const response = await coreClient.createReceipt({
      request_key: requestKey,
      artifact,
      packet,
    });
    await threadDetailStore.queueRefreshThreadDetail(threadId, {
      workspace: true,
      timeline: true,
      workOrders: true,
    });
    return response;
  }

  async function handleMessagePost(threadId, event) {
    await coreClient.createEvent({ event });
    await threadDetailStore.queueRefreshThreadDetail(threadId, {
      workspace: true,
      timeline: true,
      workOrders: true,
    });
  }

  function startThreadEventStream(threadId, initialLastEventId = "") {
    let stopped = false;
    let reconnectTimer;
    let controller = null;
    let lastEventId = String(initialLastEventId ?? "").trim();

    const connect = async () => {
      if (stopped) return;
      controller = new AbortController();
      try {
        await coreClient.streamThreadEvents({
          threadId,
          lastEventId,
          signal: controller.signal,
          onEvent: async (message) => {
            if (message?.id) {
              lastEventId = message.id;
            }
            if (message?.event !== "event") {
              return;
            }
            await threadDetailStore.queueRefreshThreadDetail(threadId, {
              workspace: true,
              timeline: true,
              workOrders: true,
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
      void threadDetailStore.loadTimeline(threadId);
    }
  });

  async function setActiveTab(tabId) {
    await goto(withUpdatedSearchParams($page.url, { tab: tabId }), {
      noScroll: true,
      keepFocus: true,
    });
  }
</script>

<ThreadDetailHeader {threadId} />

{#if snapshotLoading}
  <p class="text-[13px] text-[var(--ui-text-muted)]">Loading...</p>
{:else if snapshotError}
  <p class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
    {snapshotError}
  </p>
{:else if !snapshot}
  <p class="text-[13px] text-[var(--ui-text-muted)]">Topic not found.</p>
{:else}
  <div
    class="mt-3 flex gap-0 border-b border-[var(--ui-border)]"
    aria-label="Topic sections"
    role="tablist"
  >
    {#each [["overview", "Overview"], ["work", "Work"], ["messages", "Messages"], ["timeline", "Timeline"]] as [tabId, tabLabel]}
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
      <ThreadOverviewTab
        {threadId}
        onSave={handleSaveThread}
        {conflictWarning}
        {editNotice}
      />
      <ThreadBoardsPanel {threadId} />
      <ThreadDocumentsPanel {threadId} />
      <ThreadCommitmentsPanel
        {threadId}
        onCommitmentSave={handleSaveCommitment}
        onCommitmentCreate={handleCreateCommitment}
      />
    </div>
  {/if}

  {#if activeTab === "work"}
    <div role="tabpanel" tabindex="0">
      <ThreadWorkTab
        {threadId}
        onWorkOrderSubmit={handleWorkOrderSubmit}
        onReceiptSubmit={handleReceiptSubmit}
      />
    </div>
  {/if}

  {#if activeTab === "messages"}
    <div role="tabpanel" tabindex="0">
      <ThreadMessagesTab
        {threadId}
        onMessagePost={handleMessagePost}
        workspaceId={data?.workspaceId ?? ""}
      />
    </div>
  {/if}

  {#if activeTab === "timeline"}
    <div role="tabpanel" tabindex="0">
      <ThreadTimelineTab {threadId} />
    </div>
  {/if}
{/if}
