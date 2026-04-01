<script>
  import { onMount } from "svelte";

  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import {
    authenticatedAgent,
    getAuthenticatedActorId,
  } from "$lib/authSession";
  import {
    actorRegistry,
    getSelectedActorId,
    lookupActorDisplayName,
    principalRegistry,
    selectedActorId,
  } from "$lib/actorSession";
  import { BOARD_STATUS_LABELS } from "$lib/boardUtils";
  import { coreClient } from "$lib/coreClient";
  import { devActorMode } from "$lib/workspaceContext";
  import { kindColor, kindLabel } from "$lib/artifactKinds";
  import { formatTimestamp } from "$lib/formatDate";
  import { getPriorityLabel } from "$lib/threadFilters";

  const DOC_STATUS_LABELS = { draft: "Draft", active: "Active" };

  let artifacts = $state([]);
  let documents = $state([]);
  let threads = $state([]);
  let boards = $state([]);

  let activeTab = $state("artifacts");
  let loading = $state(true);
  let error = $state("");
  let purgeConfirmId = $state("");
  let busyItemId = $state("");
  let purgeAllOpen = $state(false);
  let purgeAllBusy = $state(false);

  let tabs = $derived([
    { id: "artifacts", label: "Artifacts", count: artifacts.length },
    { id: "documents", label: "Documents", count: documents.length },
    { id: "threads", label: "Threads", count: threads.length },
    { id: "boards", label: "Boards", count: boards.length },
  ]);

  let activeItems = $derived.by(() => {
    switch (activeTab) {
      case "artifacts":
        return artifacts;
      case "documents":
        return documents;
      case "threads":
        return threads;
      case "boards":
        return boards;
      default:
        return [];
    }
  });

  let isHumanPrincipal = $derived.by(() => {
    if ($authenticatedAgent?.principal_kind === "human") {
      return true;
    }
    if (!$devActorMode) {
      return false;
    }
    const id = String($selectedActorId ?? "").trim();
    if (!id) {
      return false;
    }
    const actor = $actorRegistry.find((a) => a.id === id);
    return (
      Array.isArray(actor?.tags) &&
      actor.tags.some((t) => String(t).toLowerCase() === "human")
    );
  });
  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );

  function itemBusyKey(type, id) {
    return `${type}:${String(id ?? "").trim()}`;
  }

  function switchTab(tabId) {
    activeTab = tabId;
    purgeConfirmId = "";
    purgeAllOpen = false;
  }

  function docStatusColor(status) {
    if (status === "active") return "text-emerald-400 bg-emerald-500/10";
    if (status === "draft") return "text-amber-400 bg-amber-500/10";
    return "text-[var(--ui-text-muted)] bg-[var(--ui-panel)]";
  }

  function threadStatusColor(status) {
    const styles = {
      active: "text-emerald-400",
      paused: "text-amber-400",
      closed: "text-gray-400",
    };
    return styles[status] ?? "text-gray-400";
  }

  async function loadTrash() {
    loading = true;
    error = "";
    try {
      const [artifactResult, docResult, threadResult, boardResult] =
        await Promise.all([
          coreClient.listArtifacts({ tombstoned_only: "true" }),
          coreClient.listDocuments({ tombstoned_only: "true" }),
          coreClient.listThreads({ tombstoned_only: "true" }),
          coreClient.listBoards({ tombstoned_only: "true" }),
        ]);
      artifacts = artifactResult.artifacts ?? [];
      documents = docResult.documents ?? [];
      threads = threadResult.threads ?? [];
      boards = (boardResult.boards ?? []).map((item) => item.board);
    } catch (e) {
      error = `Failed to load trash: ${e instanceof Error ? e.message : String(e)}`;
      artifacts = [];
      documents = [];
      threads = [];
      boards = [];
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    void loadTrash();
  });

  function rowHeading(artifact) {
    const summary = String(artifact?.summary ?? "").trim();
    if (summary) return summary;
    return `${kindLabel(artifact?.kind)} artifact`;
  }

  function tombstoneReason(entity) {
    const r = String(entity?.tombstone_reason ?? "").trim();
    return r || "—";
  }

  function documentTitle(doc) {
    const t = String(doc?.title ?? "").trim();
    return t || String(doc?.id ?? "").trim() || "—";
  }

  function threadCreatedAt(thread) {
    const direct = thread?.created_at;
    if (direct) return direct;
    const prov = thread?.provenance;
    if (prov && typeof prov === "object" && prov.created_at) {
      return prov.created_at;
    }
    return "";
  }

  async function restoreEntity(type, rawId) {
    const id = String(rawId ?? "").trim();
    if (!id || busyItemId) return;
    busyItemId = itemBusyKey(type, id);
    error = "";
    try {
      switch (type) {
        case "artifacts":
          await coreClient.restoreArtifact(id, {});
          break;
        case "documents":
          await coreClient.restoreDocument(id, {});
          break;
        case "threads":
          await coreClient.restoreThread(id, {});
          break;
        case "boards":
          await coreClient.restoreBoard(id, {});
          break;
        default:
          return;
      }
      purgeConfirmId = "";
      await loadTrash();
    } catch (e) {
      error = `Restore failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      busyItemId = "";
    }
  }

  async function confirmPurgeEntity(type, rawId) {
    const id = String(rawId ?? "").trim();
    if (!id || busyItemId) return;
    busyItemId = itemBusyKey(type, id);
    error = "";
    try {
      const body = {};
      if (!getAuthenticatedActorId()) {
        body.actor_id = getSelectedActorId();
      }
      switch (type) {
        case "artifacts":
          await coreClient.purgeArtifact(id, body);
          break;
        case "documents":
          await coreClient.purgeDocument(id, body);
          break;
        case "threads":
          await coreClient.purgeThread(id, body);
          break;
        case "boards":
          await coreClient.purgeBoard(id, body);
          break;
        default:
          return;
      }
      purgeConfirmId = "";
      await loadTrash();
    } catch (e) {
      error = `Purge failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      busyItemId = "";
    }
  }

  function cancelPurge() {
    purgeConfirmId = "";
  }

  function entitySingular(tab) {
    switch (tab) {
      case "artifacts":
        return "artifact";
      case "documents":
        return "document";
      case "threads":
        return "thread";
      case "boards":
        return "board";
      default:
        return "item";
    }
  }

  function emptyCategoryMessage(tab) {
    switch (tab) {
      case "artifacts":
        return "No tombstoned artifacts in this category";
      case "documents":
        return "No tombstoned documents in this category";
      case "threads":
        return "No tombstoned threads in this category";
      case "boards":
        return "No tombstoned boards in this category";
      default:
        return "No tombstoned items in this category";
    }
  }

  async function purgeAll() {
    const items = activeItems;
    if (purgeAllBusy || items.length === 0) return;
    purgeAllBusy = true;
    error = "";
    let failed = 0;
    for (const item of items) {
      const id = String(item?.id ?? "").trim();
      if (!id) continue;
      try {
        const body = {};
        if (!getAuthenticatedActorId()) {
          body.actor_id = getSelectedActorId();
        }
        switch (activeTab) {
          case "artifacts":
            await coreClient.purgeArtifact(id, body);
            break;
          case "documents":
            await coreClient.purgeDocument(id, body);
            break;
          case "threads":
            await coreClient.purgeThread(id, body);
            break;
          case "boards":
            await coreClient.purgeBoard(id, body);
            break;
          default:
            break;
        }
      } catch {
        failed++;
      }
    }
    purgeAllOpen = false;
    purgeAllBusy = false;
    if (failed > 0) {
      error = `Purge completed with ${failed} failure${failed > 1 ? "s" : ""}`;
    }
    await loadTrash();
  }

  function purgeConfirmLabel(type) {
    switch (type) {
      case "artifacts":
        return "Permanently delete this artifact? This cannot be undone.";
      case "documents":
        return "Permanently delete this document? This cannot be undone.";
      case "threads":
        return "Permanently delete this thread? This cannot be undone.";
      case "boards":
        return "Permanently delete this board? This cannot be undone.";
      default:
        return "Permanently delete this item? This cannot be undone.";
    }
  }
</script>

<div class="mb-4 flex items-start justify-between gap-4">
  <div>
    <h1 class="text-lg font-semibold text-[var(--ui-text)]">Trash</h1>
    <p class="mt-0.5 text-[12px] text-[var(--ui-text-muted)]">
      Tombstoned items available for restore or permanent deletion. Restore
      returns them to their normal lists; purge permanently removes them (human
      principals only). Tombstoned events and messages are restored from within
      their thread's timeline view.
    </p>
  </div>
  {#if isHumanPrincipal && !loading && activeItems.length > 0}
    <div class="shrink-0">
      <button
        class="cursor-pointer rounded-md border border-red-500/40 bg-red-500/10 px-2.5 py-1.5 text-[12px] font-medium text-red-400 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-50"
        disabled={Boolean(busyItemId) || purgeAllBusy}
        onclick={() => {
          purgeAllOpen = true;
        }}
        type="button"
      >
        Purge all ({activeItems.length})
      </button>
    </div>
  {/if}
</div>

<div class="mb-4 flex gap-0 border-b border-[var(--ui-border)]" role="tablist">
  {#each tabs as tab}
    <button
      class="cursor-pointer px-3 py-2 text-[13px] font-medium transition-colors {activeTab ===
      tab.id
        ? 'border-b-2 border-[var(--ui-accent)] text-[var(--ui-text)]'
        : 'text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]'}"
      onclick={() => switchTab(tab.id)}
      role="tab"
      aria-selected={activeTab === tab.id}
      type="button"
    >
      {tab.label}
      {#if tab.count > 0}
        <span class="ml-1 text-[11px] text-[var(--ui-text-muted)]"
          >({tab.count})</span
        >
      {/if}
    </button>
  {/each}
</div>

{#if error}
  <div class="mb-4 rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
    {error}
  </div>
{/if}

{#if loading}
  <div
    class="mt-12 flex items-center justify-center gap-2 text-[13px] text-[var(--ui-text-muted)]"
  >
    <svg class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
      <circle
        class="opacity-25"
        cx="12"
        cy="12"
        r="10"
        stroke="currentColor"
        stroke-width="4"
      ></circle>
      <path
        class="opacity-75"
        fill="currentColor"
        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
      ></path>
    </svg>
    Loading tombstoned items...
  </div>
{:else if activeItems.length === 0 && !error}
  <div class="mt-8 text-center">
    <p class="text-[13px] font-medium text-[var(--ui-text-muted)]">
      {emptyCategoryMessage(activeTab)}
    </p>
  </div>
{/if}

{#if !loading && activeTab === "artifacts" && artifacts.length > 0}
  <div
    class="space-y-px rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
  >
    {#each artifacts as artifact, i}
      <div
        class="px-4 py-3 {i > 0 ? 'border-t border-[var(--ui-border)]' : ''}"
      >
        <div
          class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between"
        >
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <span
                class="inline-flex rounded px-1.5 py-0.5 text-[11px] font-semibold {kindColor(
                  artifact.kind,
                )}"
              >
                {kindLabel(artifact.kind)}
              </span>
              <span class="text-[13px] font-medium text-[var(--ui-text)]">
                {rowHeading(artifact)}
              </span>
            </div>

            <div
              class="mt-2 grid gap-x-4 gap-y-1 text-[11px] text-[var(--ui-text-muted)] sm:grid-cols-2 xl:grid-cols-3"
            >
              <div>
                <span class="text-[var(--ui-text-subtle)]">Created</span>
                {formatTimestamp(artifact.created_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(artifact.created_by)}
              </div>
              <div>
                <span class="text-[var(--ui-text-subtle)]">Tombstoned</span>
                {formatTimestamp(artifact.tombstoned_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(artifact.tombstoned_by)}
              </div>
              <div class="sm:col-span-2 xl:col-span-1">
                <span class="text-[var(--ui-text-subtle)]">Reason</span>
                {tombstoneReason(artifact)}
              </div>
            </div>
          </div>

          <div class="flex shrink-0 flex-col items-stretch gap-2 lg:items-end">
            <div class="flex flex-wrap justify-end gap-1.5">
              <button
                class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--ui-text)] transition-colors hover:bg-[var(--ui-border)] disabled:cursor-not-allowed disabled:opacity-50"
                disabled={busyItemId === itemBusyKey("artifacts", artifact.id)}
                onclick={() => restoreEntity("artifacts", artifact.id)}
                type="button"
              >
                Restore
              </button>
              {#if isHumanPrincipal}
                {#if purgeConfirmId !== itemBusyKey("artifacts", artifact.id)}
                  <button
                    class="cursor-pointer rounded-md border border-red-500/40 bg-red-500/10 px-2.5 py-1.5 text-[12px] font-medium text-red-400 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-50"
                    disabled={Boolean(busyItemId)}
                    onclick={() => {
                      purgeConfirmId = itemBusyKey("artifacts", artifact.id);
                    }}
                    type="button"
                  >
                    Purge
                  </button>
                {/if}
              {/if}
            </div>

            {#if isHumanPrincipal && purgeConfirmId === itemBusyKey("artifacts", artifact.id)}
              <div
                class="rounded-md border border-red-500/35 bg-red-500/5 p-2.5 text-[12px]"
                role="region"
                aria-label="Confirm purge"
              >
                <p class="font-medium text-red-300">
                  {purgeConfirmLabel("artifacts")}
                </p>
                <div class="mt-2 flex flex-wrap justify-end gap-1.5">
                  <button
                    class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)]"
                    onclick={cancelPurge}
                    type="button"
                  >
                    Cancel
                  </button>
                  <button
                    class="cursor-pointer rounded-md bg-red-600 px-2.5 py-1.5 text-[12px] font-medium text-white hover:bg-red-500 disabled:cursor-not-allowed disabled:opacity-50"
                    disabled={busyItemId ===
                      itemBusyKey("artifacts", artifact.id)}
                    onclick={() => confirmPurgeEntity("artifacts", artifact.id)}
                    type="button"
                  >
                    Confirm purge
                  </button>
                </div>
              </div>
            {/if}
          </div>
        </div>
      </div>
    {/each}
  </div>
{/if}

{#if !loading && activeTab === "documents" && documents.length > 0}
  <div
    class="space-y-px rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
  >
    {#each documents as doc, i}
      <div
        class="px-4 py-3 {i > 0 ? 'border-t border-[var(--ui-border)]' : ''}"
      >
        <div
          class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between"
        >
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              {#if doc.status}
                <span
                  class="inline-flex rounded px-1.5 py-0.5 text-[11px] font-semibold {docStatusColor(
                    doc.status,
                  )}">{DOC_STATUS_LABELS[doc.status] ?? doc.status}</span
                >
              {/if}
              <span class="text-[13px] font-medium text-[var(--ui-text)]">
                {documentTitle(doc)}
              </span>
            </div>
            <div
              class="mt-2 grid gap-x-4 gap-y-1 text-[11px] text-[var(--ui-text-muted)] sm:grid-cols-2 xl:grid-cols-3"
            >
              <div>
                <span class="text-[var(--ui-text-subtle)]">Created</span>
                {formatTimestamp(doc.created_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(doc.created_by)}
              </div>
              <div>
                <span class="text-[var(--ui-text-subtle)]">Tombstoned</span>
                {formatTimestamp(doc.tombstoned_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(doc.tombstoned_by)}
              </div>
              <div class="sm:col-span-2 xl:col-span-1">
                <span class="text-[var(--ui-text-subtle)]">Reason</span>
                {tombstoneReason(doc)}
              </div>
            </div>
          </div>
          <div class="flex shrink-0 flex-col items-stretch gap-2 lg:items-end">
            <div class="flex flex-wrap justify-end gap-1.5">
              <button
                class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--ui-text)] transition-colors hover:bg-[var(--ui-border)] disabled:cursor-not-allowed disabled:opacity-50"
                disabled={busyItemId === itemBusyKey("documents", doc.id)}
                onclick={() => restoreEntity("documents", doc.id)}
                type="button"
              >
                Restore
              </button>
              {#if isHumanPrincipal}
                {#if purgeConfirmId !== itemBusyKey("documents", doc.id)}
                  <button
                    class="cursor-pointer rounded-md border border-red-500/40 bg-red-500/10 px-2.5 py-1.5 text-[12px] font-medium text-red-400 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-50"
                    disabled={Boolean(busyItemId)}
                    onclick={() => {
                      purgeConfirmId = itemBusyKey("documents", doc.id);
                    }}
                    type="button"
                  >
                    Purge
                  </button>
                {/if}
              {/if}
            </div>
            {#if isHumanPrincipal && purgeConfirmId === itemBusyKey("documents", doc.id)}
              <div
                class="rounded-md border border-red-500/35 bg-red-500/5 p-2.5 text-[12px]"
                role="region"
                aria-label="Confirm purge"
              >
                <p class="font-medium text-red-300">
                  {purgeConfirmLabel("documents")}
                </p>
                <div class="mt-2 flex flex-wrap justify-end gap-1.5">
                  <button
                    class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)]"
                    onclick={cancelPurge}
                    type="button"
                  >
                    Cancel
                  </button>
                  <button
                    class="cursor-pointer rounded-md bg-red-600 px-2.5 py-1.5 text-[12px] font-medium text-white hover:bg-red-500 disabled:cursor-not-allowed disabled:opacity-50"
                    disabled={busyItemId === itemBusyKey("documents", doc.id)}
                    onclick={() => confirmPurgeEntity("documents", doc.id)}
                    type="button"
                  >
                    Confirm purge
                  </button>
                </div>
              </div>
            {/if}
          </div>
        </div>
      </div>
    {/each}
  </div>
{/if}

{#if !loading && activeTab === "threads" && threads.length > 0}
  <div
    class="space-y-px rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
  >
    {#each threads as thread, i}
      <div
        class="px-4 py-3 {i > 0 ? 'border-t border-[var(--ui-border)]' : ''}"
      >
        <div
          class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between"
        >
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <span class="text-[13px] font-medium text-[var(--ui-text)]">
                {String(thread?.title ?? "").trim() || thread.id}
              </span>
              {#if thread.status}
                <span
                  class="rounded bg-[var(--ui-panel)] px-1.5 py-0.5 text-[11px] font-medium capitalize {threadStatusColor(
                    thread.status,
                  )}">{thread.status}</span
                >
              {/if}
              {#if thread.priority}
                <span
                  class="rounded bg-[var(--ui-panel)] px-1.5 py-0.5 text-[11px] font-medium text-[var(--ui-text-muted)]"
                  >{getPriorityLabel(thread.priority)}</span
                >
              {/if}
            </div>
            <div
              class="mt-2 grid gap-x-4 gap-y-1 text-[11px] text-[var(--ui-text-muted)] sm:grid-cols-2 xl:grid-cols-3"
            >
              <div>
                <span class="text-[var(--ui-text-subtle)]">Created</span>
                {formatTimestamp(threadCreatedAt(thread)) || "—"}
                {#if thread.created_by}
                  <span class="text-[var(--ui-text-subtle)]"> · </span>
                  {actorName(thread.created_by)}
                {/if}
              </div>
              <div>
                <span class="text-[var(--ui-text-subtle)]">Tombstoned</span>
                {formatTimestamp(thread.tombstoned_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(thread.tombstoned_by)}
              </div>
              <div class="sm:col-span-2 xl:col-span-1">
                <span class="text-[var(--ui-text-subtle)]">Reason</span>
                {tombstoneReason(thread)}
              </div>
            </div>
          </div>
          <div class="flex shrink-0 flex-col items-stretch gap-2 lg:items-end">
            <div class="flex flex-wrap justify-end gap-1.5">
              <button
                class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--ui-text)] transition-colors hover:bg-[var(--ui-border)] disabled:cursor-not-allowed disabled:opacity-50"
                disabled={busyItemId === itemBusyKey("threads", thread.id)}
                onclick={() => restoreEntity("threads", thread.id)}
                type="button"
              >
                Restore
              </button>
              {#if isHumanPrincipal}
                {#if purgeConfirmId !== itemBusyKey("threads", thread.id)}
                  <button
                    class="cursor-pointer rounded-md border border-red-500/40 bg-red-500/10 px-2.5 py-1.5 text-[12px] font-medium text-red-400 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-50"
                    disabled={Boolean(busyItemId)}
                    onclick={() => {
                      purgeConfirmId = itemBusyKey("threads", thread.id);
                    }}
                    type="button"
                  >
                    Purge
                  </button>
                {/if}
              {/if}
            </div>
            {#if isHumanPrincipal && purgeConfirmId === itemBusyKey("threads", thread.id)}
              <div
                class="rounded-md border border-red-500/35 bg-red-500/5 p-2.5 text-[12px]"
                role="region"
                aria-label="Confirm purge"
              >
                <p class="font-medium text-red-300">
                  {purgeConfirmLabel("threads")}
                </p>
                <div class="mt-2 flex flex-wrap justify-end gap-1.5">
                  <button
                    class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)]"
                    onclick={cancelPurge}
                    type="button"
                  >
                    Cancel
                  </button>
                  <button
                    class="cursor-pointer rounded-md bg-red-600 px-2.5 py-1.5 text-[12px] font-medium text-white hover:bg-red-500 disabled:cursor-not-allowed disabled:opacity-50"
                    disabled={busyItemId === itemBusyKey("threads", thread.id)}
                    onclick={() => confirmPurgeEntity("threads", thread.id)}
                    type="button"
                  >
                    Confirm purge
                  </button>
                </div>
              </div>
            {/if}
          </div>
        </div>
      </div>
    {/each}
  </div>
{/if}

{#if !loading && activeTab === "boards" && boards.length > 0}
  <div
    class="space-y-px rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
  >
    {#each boards as board, i}
      <div
        class="px-4 py-3 {i > 0 ? 'border-t border-[var(--ui-border)]' : ''}"
      >
        <div
          class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between"
        >
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <span class="text-[13px] font-medium text-[var(--ui-text)]">
                {String(board?.title ?? "").trim() || board.id}
              </span>
              {#if board.status}
                <span
                  class="rounded bg-[var(--ui-panel)] px-1.5 py-0.5 text-[11px] font-medium text-[var(--ui-text-muted)]"
                  >{BOARD_STATUS_LABELS[board.status] ?? board.status}</span
                >
              {/if}
            </div>
            <div
              class="mt-2 grid gap-x-4 gap-y-1 text-[11px] text-[var(--ui-text-muted)] sm:grid-cols-2 xl:grid-cols-3"
            >
              <div>
                <span class="text-[var(--ui-text-subtle)]">Created</span>
                {formatTimestamp(board.created_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(board.created_by)}
              </div>
              <div>
                <span class="text-[var(--ui-text-subtle)]">Tombstoned</span>
                {formatTimestamp(board.tombstoned_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(board.tombstoned_by)}
              </div>
              <div class="sm:col-span-2 xl:col-span-1">
                <span class="text-[var(--ui-text-subtle)]">Reason</span>
                {tombstoneReason(board)}
              </div>
            </div>
          </div>
          <div class="flex shrink-0 flex-col items-stretch gap-2 lg:items-end">
            <div class="flex flex-wrap justify-end gap-1.5">
              <button
                class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--ui-text)] transition-colors hover:bg-[var(--ui-border)] disabled:cursor-not-allowed disabled:opacity-50"
                disabled={busyItemId === itemBusyKey("boards", board.id)}
                onclick={() => restoreEntity("boards", board.id)}
                type="button"
              >
                Restore
              </button>
              {#if isHumanPrincipal}
                {#if purgeConfirmId !== itemBusyKey("boards", board.id)}
                  <button
                    class="cursor-pointer rounded-md border border-red-500/40 bg-red-500/10 px-2.5 py-1.5 text-[12px] font-medium text-red-400 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-50"
                    disabled={Boolean(busyItemId)}
                    onclick={() => {
                      purgeConfirmId = itemBusyKey("boards", board.id);
                    }}
                    type="button"
                  >
                    Purge
                  </button>
                {/if}
              {/if}
            </div>
            {#if isHumanPrincipal && purgeConfirmId === itemBusyKey("boards", board.id)}
              <div
                class="rounded-md border border-red-500/35 bg-red-500/5 p-2.5 text-[12px]"
                role="region"
                aria-label="Confirm purge"
              >
                <p class="font-medium text-red-300">
                  {purgeConfirmLabel("boards")}
                </p>
                <div class="mt-2 flex flex-wrap justify-end gap-1.5">
                  <button
                    class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)]"
                    onclick={cancelPurge}
                    type="button"
                  >
                    Cancel
                  </button>
                  <button
                    class="cursor-pointer rounded-md bg-red-600 px-2.5 py-1.5 text-[12px] font-medium text-white hover:bg-red-500 disabled:cursor-not-allowed disabled:opacity-50"
                    disabled={busyItemId === itemBusyKey("boards", board.id)}
                    onclick={() => confirmPurgeEntity("boards", board.id)}
                    type="button"
                  >
                    Confirm purge
                  </button>
                </div>
              </div>
            {/if}
          </div>
        </div>
      </div>
    {/each}
  </div>
{/if}

<ConfirmModal
  open={purgeAllOpen}
  title="Empty trash"
  message="Permanently delete all {activeItems.length} {entitySingular(
    activeTab,
  )}{activeItems.length === 1 ? '' : 's'} in this tab. This cannot be undone."
  confirmLabel="Purge all"
  variant="danger"
  busy={purgeAllBusy}
  typedConfirmation="Empty trash"
  onconfirm={() => void purgeAll()}
  oncancel={() => {
    purgeAllOpen = false;
  }}
/>
