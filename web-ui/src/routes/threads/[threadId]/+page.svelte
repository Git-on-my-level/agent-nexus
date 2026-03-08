<script>
  import { onMount } from "svelte";
  import { page } from "$app/stores";

  import { actorRegistry } from "$lib/actorSession";
  import { coreClient } from "$lib/coreClient";
  import { threadDetailStore } from "$lib/threadDetailStore";

  import ThreadDetailHeader from "$lib/components/thread-detail/ThreadDetailHeader.svelte";
  import ThreadOverviewTab from "$lib/components/thread-detail/ThreadOverviewTab.svelte";
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

  onMount(async () => {
    await ensureActorRegistry();
    await threadDetailStore.fullRefresh(threadId);
  });

  async function ensureActorRegistry() {
    if ($actorRegistry.length > 0) return;
    try {
      actorRegistry.set((await coreClient.listActors()).actors ?? []);
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
        await threadDetailStore.refreshThreadDetail(threadId, {
          snapshot: true,
          timeline: true,
        });
      } else {
        throw error;
      }
    }
  }

  async function handleCreateCommitment(threadId, commitment) {
    await coreClient.createCommitment({ commitment });
    await threadDetailStore.refreshThreadDetail(threadId, {
      snapshot: true,
      commitments: true,
      timeline: true,
    });
  }

  async function handleSaveCommitment(commitmentId, payload) {
    await coreClient.updateCommitment(commitmentId, payload);
    await threadDetailStore.refreshThreadDetail(threadId, {
      snapshot: true,
      commitments: true,
      timeline: true,
    });
  }

  async function handleWorkOrderSubmit(threadId, artifact, packet) {
    await coreClient.createWorkOrder({ artifact, packet });
    await threadDetailStore.refreshThreadDetail(threadId, {
      timeline: true,
      workOrders: true,
    });
  }

  async function handleReceiptSubmit(threadId, artifact, packet) {
    await coreClient.createReceipt({ artifact, packet });
    await threadDetailStore.refreshThreadDetail(threadId, {
      timeline: true,
      workOrders: true,
    });
  }

  async function handleMessagePost(threadId, event) {
    await coreClient.createEvent({ event });
    await threadDetailStore.refreshThreadDetail(threadId, {
      snapshot: true,
      timeline: true,
    });
  }

  $effect(() => {
    if ($page.url.searchParams.get("compose") === "work-order") {
      activeTab = "work";
    }
  });
</script>

<ThreadDetailHeader {threadId} onEditClick={() => {}} />

{#if snapshotLoading}
  <p class="text-sm text-gray-400">Loading...</p>
{:else if snapshotError}
  <p class="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">
    {snapshotError}
  </p>
{:else if !snapshot}
  <p class="text-sm text-gray-400">Thread not found.</p>
{:else}
  <nav
    class="mt-3 flex gap-0 border-b border-gray-200"
    aria-label="Thread sections"
  >
    {#each [["overview", "Overview"], ["work", "Work"], ["timeline", "Timeline"]] as [tabId, tabLabel]}
      <button
        class={`relative px-3 py-2 text-sm font-medium transition-colors ${activeTab === tabId ? "text-gray-900" : "text-gray-400 hover:text-gray-600"}`}
        onclick={() => (activeTab = tabId)}
        type="button"
      >
        {tabLabel}
        {#if activeTab === tabId}
          <span class="absolute inset-x-0 -bottom-px h-0.5 bg-indigo-600"
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
