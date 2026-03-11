<script>
  import { onMount, onDestroy } from "svelte";
  import { page } from "$app/stores";

  import { actorRegistry, replaceActorRegistry } from "$lib/actorSession";
  import { coreClient } from "$lib/coreClient";
  import { threadDetailStore } from "$lib/threadDetailStore";

  import ThreadDetailHeader from "$lib/components/thread-detail/ThreadDetailHeader.svelte";
  import ThreadOverviewTab from "$lib/components/thread-detail/ThreadOverviewTab.svelte";
  import ThreadDocumentsPanel from "$lib/components/thread-detail/ThreadDocumentsPanel.svelte";
  import ThreadCommitmentsPanel from "$lib/components/thread-detail/ThreadCommitmentsPanel.svelte";
  import ThreadWorkTab from "$lib/components/thread-detail/ThreadWorkTab.svelte";
  import ThreadTimelineTab from "$lib/components/thread-detail/ThreadTimelineTab.svelte";

  let threadId = $derived($page.params.threadId);

  let snapshot = $derived($threadDetailStore.snapshot);
  let snapshotLoading = $derived($threadDetailStore.snapshotLoading);
  let snapshotError = $derived($threadDetailStore.snapshotError);

  let activeTab = $state("overview");

  let conflictWarning = $state("");
  let editNotice = $state("");

  const STREAM_RECONNECT_DELAY_MS = 1_500;
  const RECONCILE_INTERVAL_MS = 120_000;
  let reconcileTimer;
  let stopThreadStream = () => {};

  onMount(async () => {
    await ensureActorRegistry();
    await threadDetailStore.fullRefresh(threadId);
    stopThreadStream = startThreadEventStream(threadId);
    reconcileTimer = setInterval(
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
    stopThreadStream();
    clearInterval(reconcileTimer);
  });

  async function ensureActorRegistry() {
    if ($actorRegistry.length > 0) return;
    try {
      replaceActorRegistry((await coreClient.listActors()).actors ?? []);
    } catch (error) {
      void error;
    }
  }

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
          "Thread was updated elsewhere. Reloaded — reapply your changes.";
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

  function startThreadEventStream(threadId) {
    let stopped = false;
    let reconnectTimer;
    let controller = null;
    let lastEventId = "";

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
    if ($page.url.searchParams.get("compose") === "work-order") {
      activeTab = "work";
    }
  });

  $effect(() => {
    if (activeTab === "timeline" && threadId) {
      void threadDetailStore.loadTimeline(threadId);
    }
  });
</script>

<ThreadDetailHeader {threadId} />

{#if snapshotLoading}
  <p class="text-[13px] text-[var(--ui-text-muted)]">Loading...</p>
{:else if snapshotError}
  <p class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
    {snapshotError}
  </p>
{:else if !snapshot}
  <p class="text-[13px] text-[var(--ui-text-muted)]">Thread not found.</p>
{:else}
  <nav
    class="mt-3 flex gap-0 border-b border-[var(--ui-border)]"
    aria-label="Thread sections"
  >
    {#each [["overview", "Overview"], ["work", "Work"], ["timeline", "Timeline"]] as [tabId, tabLabel]}
      <button
        class={`relative cursor-pointer px-3 py-2 text-[13px] font-medium transition-colors ${activeTab === tabId ? "text-[var(--ui-text)]" : "text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]"}`}
        onclick={() => (activeTab = tabId)}
        type="button"
      >
        {tabLabel}
        {#if activeTab === tabId}
          <span
            class="pointer-events-none absolute inset-x-0 -bottom-px h-0.5 bg-indigo-500"
          ></span>
        {/if}
      </button>
    {/each}
  </nav>

  {#if activeTab === "overview"}
    <ThreadOverviewTab
      {threadId}
      onSave={handleSaveThread}
      {conflictWarning}
      {editNotice}
    />
    <ThreadDocumentsPanel {threadId} />
    <ThreadCommitmentsPanel
      {threadId}
      onCommitmentSave={handleSaveCommitment}
      onCommitmentCreate={handleCreateCommitment}
    />
  {/if}

  {#if activeTab === "work"}
    <ThreadWorkTab
      {threadId}
      onWorkOrderSubmit={handleWorkOrderSubmit}
      onReceiptSubmit={handleReceiptSubmit}
    />
  {/if}

  {#if activeTab === "timeline"}
    <ThreadTimelineTab {threadId} onMessagePost={handleMessagePost} />
  {/if}
{/if}
