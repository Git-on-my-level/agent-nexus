<script>
  import { onMount } from "svelte";
  import { goto, replaceState } from "$app/navigation";
  import { page } from "$app/stores";

  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
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
  import { getPriorityLabel } from "$lib/topicFilters";
  import { readEnumSearchParam, withUpdatedSearchParams } from "$lib/urlState";

  const DOC_STATUS_LABELS = { draft: "Draft", active: "Active" };
  const TRASH_TAB_IDS = ["artifacts", "documents", "topics", "boards", "cards"];

  let artifacts = $state([]);
  let documents = $state([]);
  let threads = $state([]);
  let boards = $state([]);
  let cards = $state([]);

  let activeTab = $state("artifacts");
  let loading = $state(true);
  let error = $state("");
  let purgeModal = $state({ open: false, type: "", id: "" });
  let purgeModalBusy = $state(false);
  let busyItemId = $state("");
  let purgeAllOpen = $state(false);
  let purgeAllBusy = $state(false);

  let tabs = $derived([
    { id: "artifacts", label: "Artifacts", count: artifacts.length },
    { id: "documents", label: "Docs", count: documents.length },
    { id: "topics", label: "Topics", count: threads.length },
    { id: "boards", label: "Boards", count: boards.length },
    { id: "cards", label: "Cards", count: cards.length },
  ]);

  let activeItems = $derived.by(() => {
    switch (activeTab) {
      case "artifacts":
        return artifacts;
      case "documents":
        return documents;
      case "topics":
        return threads;
      case "boards":
        return boards;
      case "cards":
        return cards;
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

  $effect(() => {
    const raw = String($page.url.searchParams.get("tab") ?? "").trim();
    const normalized = readEnumSearchParam(
      $page.url.searchParams,
      "tab",
      TRASH_TAB_IDS,
      "",
    );
    if (raw && !normalized) {
      replaceState(withUpdatedSearchParams($page.url, { tab: "" }), {});
      return;
    }
    activeTab = normalized || "artifacts";
  });

  async function switchTab(tabId) {
    if (!TRASH_TAB_IDS.includes(tabId)) return;
    purgeModal = { open: false, type: "", id: "" };
    purgeAllOpen = false;
    await goto(withUpdatedSearchParams($page.url, { tab: tabId }), {
      replaceState: true,
      noScroll: true,
      keepFocus: true,
    });
  }

  function docStatusColor(status) {
    if (status === "active") return "text-emerald-400 bg-emerald-500/10";
    if (status === "draft") return "text-amber-400 bg-amber-500/10";
    return "text-[var(--ui-text-muted)] bg-[var(--ui-panel)]";
  }

  function threadStatusColor(status) {
    const styles = {
      active: "text-emerald-400",
      blocked: "text-amber-400",
      resolved: "text-sky-400",
      archived: "text-slate-300",
      paused: "text-amber-400",
      closed: "text-slate-300",
      proposed: "text-[var(--ui-text-muted)]",
    };
    return styles[status] ?? "text-gray-500";
  }

  async function loadTrash() {
    loading = true;
    error = "";
    try {
      const [
        artifactResult,
        docResult,
        topicResult,
        boardResult,
        archivedCardResult,
        trashedCardResult,
      ] = await Promise.all([
        coreClient.listArtifacts({ trashed_only: "true" }),
        coreClient.listDocuments({ trashed_only: "true" }),
        coreClient.listTopics({ trashed_only: "true" }),
        coreClient.listBoards({ trashed_only: "true" }),
        coreClient.listCards({ archived_only: "true" }),
        coreClient.listCards({ trashed_only: "true" }),
      ]);
      artifacts = artifactResult.artifacts ?? [];
      documents = docResult.documents ?? [];
      threads = (topicResult.topics ?? []).filter(
        (topic) =>
          Boolean(topic?.archived_at) ||
          Boolean(topic?.trashed_at) ||
          String(topic?.status ?? "").trim() === "archived",
      );
      boards = (boardResult.boards ?? []).map((item) => item.board);
      const cardById = new Map();
      for (const c of archivedCardResult.cards ?? []) {
        const id = String(c?.id ?? "").trim();
        if (id) cardById.set(id, c);
      }
      for (const c of trashedCardResult.cards ?? []) {
        const id = String(c?.id ?? "").trim();
        if (id) cardById.set(id, c);
      }
      cards = [...cardById.values()].filter(
        (card) =>
          Boolean(card?.archived_at) ||
          Boolean(card?.trashed_at) ||
          String(card?.status ?? "").trim() === "archived",
      );
    } catch (e) {
      error = `Failed to load trash: ${e instanceof Error ? e.message : String(e)}`;
      artifacts = [];
      documents = [];
      threads = [];
      boards = [];
      cards = [];
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

  function topicSummary(topic) {
    return String(topic?.summary ?? topic?.current_summary ?? "").trim();
  }

  function trashReason(entity) {
    const r = String(entity?.trash_reason ?? "").trim();
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
        case "topics":
          await coreClient.restoreTopic(id, {});
          break;
        case "boards":
          await coreClient.restoreBoard(id, {});
          break;
        case "cards":
          await coreClient.restoreCard(id, {});
          break;
        default:
          return;
      }
      purgeModal = { open: false, type: "", id: "" };
      await loadTrash();
    } catch (e) {
      error = `Restore failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      busyItemId = "";
    }
  }

  async function confirmPurgeEntity(type, rawId) {
    const id = String(rawId ?? "").trim();
    if (!id || purgeModalBusy) return;
    purgeModalBusy = true;
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
        case "boards":
          await coreClient.purgeBoard(id, body);
          break;
        case "cards":
          await coreClient.purgeCard(id, body);
          break;
        default:
          return;
      }
      purgeModal = { open: false, type: "", id: "" };
      await loadTrash();
    } catch (e) {
      error = `Permanent delete failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      purgeModalBusy = false;
      busyItemId = "";
    }
  }

  function entitySingular(tab) {
    switch (tab) {
      case "artifacts":
        return "artifact";
      case "documents":
        return "document";
      case "topics":
        return "topic";
      case "boards":
        return "board";
      case "cards":
        return "card";
      default:
        return "item";
    }
  }

  function emptyCategoryMessage(tab) {
    switch (tab) {
      case "artifacts":
        return "No trashed artifacts in this category";
      case "documents":
        return "No trashed docs in this category";
      case "topics":
        return "No trashed topics in this category";
      case "boards":
        return "No trashed boards in this category";
      case "cards":
        return "No archived or trashed cards in this category";
      default:
        return "No trashed items in this category";
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
          case "boards":
            await coreClient.purgeBoard(id, body);
            break;
          case "cards":
            await coreClient.purgeCard(id, body);
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
      error = `Permanent delete completed with ${failed} failure${failed > 1 ? "s" : ""}`;
    }
    await loadTrash();
  }

  function purgeConfirmLabel(type) {
    switch (type) {
      case "artifacts":
        return "Permanently delete this artifact? This cannot be undone.";
      case "documents":
        return "Permanently delete this document? This cannot be undone.";
      case "topics":
        return "Permanently delete this topic? This cannot be undone.";
      case "boards":
        return "Permanently delete this board? This cannot be undone.";
      case "cards":
        return "Permanently delete this card? This cannot be undone.";
      default:
        return "Permanently delete this item? This cannot be undone.";
    }
  }
</script>

<div class="mb-4 flex items-start justify-between gap-4">
  <div>
    <h1 class="text-lg font-semibold text-[var(--ui-text)]">Trash</h1>
    <p class="mt-0.5 text-[12px] text-[var(--ui-text-muted)]">
      Trashed items available for restore or permanent deletion. Restore returns
      them to their normal lists; permanent delete removes supported resource
      types (human principals only). Topics can be restored but not permanently
      deleted from this surface yet. Trashed events and messages are restored
      from within their timeline view.
    </p>
  </div>
  {#if isHumanPrincipal && !loading && activeItems.length > 0 && activeTab !== "topics" && (activeTab !== "cards" || $devActorMode)}
    <div class="shrink-0">
      <button
        class="cursor-pointer rounded-md border border-red-500/40 bg-red-500/10 px-2.5 py-1.5 text-[12px] font-medium text-red-400 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-50"
        disabled={Boolean(busyItemId) || purgeAllBusy}
        onclick={() => {
          purgeAllOpen = true;
        }}
        type="button"
      >
        Permanently delete all ({activeItems.length})
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
      onclick={() => void switchTab(tab.id)}
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
    Loading trashed items...
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
                <span class="text-[var(--ui-text-muted)]">Created</span>
                {formatTimestamp(artifact.created_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(artifact.created_by)}
              </div>
              <div>
                <span class="text-[var(--ui-text-muted)]">Trashed</span>
                {formatTimestamp(artifact.trashed_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(artifact.trashed_by)}
              </div>
              <div class="sm:col-span-2 xl:col-span-1">
                <span class="text-[var(--ui-text-muted)]">Reason</span>
                {trashReason(artifact)}
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
                <button
                  class="cursor-pointer rounded-md border border-red-500/40 bg-red-500/10 px-2.5 py-1.5 text-[12px] font-medium text-red-400 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-50"
                  disabled={Boolean(busyItemId)}
                  onclick={() => {
                    purgeModal = {
                      open: true,
                      type: "artifacts",
                      id: artifact.id,
                    };
                  }}
                  type="button"
                >
                  Permanently delete
                </button>
              {/if}
            </div>
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
                <span class="text-[var(--ui-text-muted)]">Created</span>
                {formatTimestamp(doc.created_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(doc.created_by)}
              </div>
              <div>
                <span class="text-[var(--ui-text-muted)]">Trashed</span>
                {formatTimestamp(doc.trashed_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(doc.trashed_by)}
              </div>
              <div class="sm:col-span-2 xl:col-span-1">
                <span class="text-[var(--ui-text-muted)]">Reason</span>
                {trashReason(doc)}
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
                <button
                  class="cursor-pointer rounded-md border border-red-500/40 bg-red-500/10 px-2.5 py-1.5 text-[12px] font-medium text-red-400 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-50"
                  disabled={Boolean(busyItemId)}
                  onclick={() => {
                    purgeModal = { open: true, type: "documents", id: doc.id };
                  }}
                  type="button"
                >
                  Permanently delete
                </button>
              {/if}
            </div>
          </div>
        </div>
      </div>
    {/each}
  </div>
{/if}

{#if !loading && activeTab === "topics" && threads.length > 0}
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
            {#if topicSummary(thread)}
              <p class="mt-1 text-[12px] text-[var(--ui-text-muted)]">
                {topicSummary(thread)}
              </p>
            {/if}
            <div
              class="mt-2 grid gap-x-4 gap-y-1 text-[11px] text-[var(--ui-text-muted)] sm:grid-cols-2 xl:grid-cols-3"
            >
              <div>
                <span class="text-[var(--ui-text-muted)]">Created</span>
                {formatTimestamp(threadCreatedAt(thread)) || "—"}
                {#if thread.created_by}
                  <span class="text-[var(--ui-text-subtle)]"> · </span>
                  {actorName(thread.created_by)}
                {/if}
              </div>
              <div>
                <span class="text-[var(--ui-text-muted)]">Trashed</span>
                {formatTimestamp(thread.trashed_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(thread.trashed_by)}
              </div>
              <div class="sm:col-span-2 xl:col-span-1">
                <span class="text-[var(--ui-text-muted)]">Reason</span>
                {trashReason(thread)}
              </div>
            </div>
          </div>
          <div class="flex shrink-0 flex-col items-stretch gap-2 lg:items-end">
            <div class="flex flex-wrap justify-end gap-1.5">
              <button
                class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--ui-text)] transition-colors hover:bg-[var(--ui-border)] disabled:cursor-not-allowed disabled:opacity-50"
                disabled={busyItemId === itemBusyKey("topics", thread.id)}
                onclick={() => restoreEntity("topics", thread.id)}
                type="button"
              >
                Restore
              </button>
            </div>
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
                <span class="text-[var(--ui-text-muted)]">Created</span>
                {formatTimestamp(board.created_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(board.created_by)}
              </div>
              <div>
                <span class="text-[var(--ui-text-muted)]">Trashed</span>
                {formatTimestamp(board.trashed_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(board.trashed_by)}
              </div>
              <div class="sm:col-span-2 xl:col-span-1">
                <span class="text-[var(--ui-text-muted)]">Reason</span>
                {trashReason(board)}
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
                <button
                  class="cursor-pointer rounded-md border border-red-500/40 bg-red-500/10 px-2.5 py-1.5 text-[12px] font-medium text-red-400 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-50"
                  disabled={Boolean(busyItemId)}
                  onclick={() => {
                    purgeModal = { open: true, type: "boards", id: board.id };
                  }}
                  type="button"
                >
                  Permanently delete
                </button>
              {/if}
            </div>
          </div>
        </div>
      </div>
    {/each}
  </div>
{/if}

{#if !loading && activeTab === "cards" && cards.length > 0}
  <div
    class="space-y-px rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
  >
    {#each cards as card, i}
      <div
        class="px-4 py-3 {i > 0 ? 'border-t border-[var(--ui-border)]' : ''}"
      >
        <div
          class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between"
        >
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <span class="text-[13px] font-medium text-[var(--ui-text)]">
                {String(card?.title ?? "").trim() || card.id}
              </span>
              {#if card.risk}
                <span
                  class="rounded bg-[var(--ui-panel)] px-1.5 py-0.5 text-[11px] font-medium text-[var(--ui-text-muted)]"
                  >{String(card.risk).trim()}</span
                >
              {/if}
              {#if card.resolution}
                <span
                  class="rounded bg-[var(--ui-panel)] px-1.5 py-0.5 text-[11px] font-medium text-[var(--ui-text-muted)]"
                  >{String(card.resolution).trim()}</span
                >
              {/if}
            </div>
            {#if card.summary}
              <p class="mt-1 text-[12px] text-[var(--ui-text-muted)]">
                {card.summary}
              </p>
            {/if}
            <div
              class="mt-2 grid gap-x-4 gap-y-1 text-[11px] text-[var(--ui-text-muted)] sm:grid-cols-2 xl:grid-cols-3"
            >
              <div>
                <span class="text-[var(--ui-text-muted)]">Created</span>
                {formatTimestamp(card.created_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(card.created_by)}
              </div>
              <div>
                <span class="text-[var(--ui-text-muted)]">Archived</span>
                {formatTimestamp(card.archived_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(card.archived_by)}
              </div>
              <div class="sm:col-span-2 xl:col-span-1">
                <span class="text-[var(--ui-text-muted)]">Trashed</span>
                {formatTimestamp(card.trashed_at) || "—"}
                <span class="text-[var(--ui-text-subtle)]"> · </span>
                {actorName(card.trashed_by)}
              </div>
              <div class="sm:col-span-2 xl:col-span-1">
                <span class="text-[var(--ui-text-muted)]">Reason</span>
                {trashReason(card)}
              </div>
            </div>
            <div class="mt-2 flex flex-wrap items-center gap-2 text-[11px]">
              {#if card.board_ref}
                <span
                  class="rounded bg-[var(--ui-panel)] px-1.5 py-0.5 font-medium text-[var(--ui-text-muted)]"
                >
                  Board: {card.board_ref}
                </span>
              {/if}
              {#if card.topic_ref}
                <span
                  class="rounded bg-[var(--ui-panel)] px-1.5 py-0.5 font-medium text-[var(--ui-text-muted)]"
                >
                  Topic: {card.topic_ref}
                </span>
              {/if}
              {#if card.document_ref}
                <span
                  class="rounded bg-[var(--ui-panel)] px-1.5 py-0.5 font-medium text-[var(--ui-text-muted)]"
                >
                  Doc: {card.document_ref}
                </span>
              {/if}
              {#if Array.isArray(card.related_refs)}
                {#each card.related_refs as refValue}
                  <RefLink {refValue} threadId={card.thread_id} />
                {/each}
              {/if}
            </div>
          </div>
          {#if $devActorMode}
            <div
              class="flex shrink-0 flex-col items-stretch gap-2 lg:items-end"
            >
              <div class="flex flex-wrap justify-end gap-1.5">
                <button
                  class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--ui-text)] transition-colors hover:bg-[var(--ui-border)] disabled:cursor-not-allowed disabled:opacity-50"
                  disabled={busyItemId === itemBusyKey("cards", card.id)}
                  onclick={() => restoreEntity("cards", card.id)}
                  type="button"
                >
                  Restore
                </button>
                {#if isHumanPrincipal}
                  <button
                    class="cursor-pointer rounded-md border border-red-500/40 bg-red-500/10 px-2.5 py-1.5 text-[12px] font-medium text-red-400 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-50"
                    disabled={Boolean(busyItemId)}
                    onclick={() => {
                      purgeModal = { open: true, type: "cards", id: card.id };
                    }}
                    type="button"
                  >
                    Permanently delete
                  </button>
                {/if}
              </div>
            </div>
          {/if}
        </div>
      </div>
    {/each}
  </div>
{/if}

<ConfirmModal
  open={purgeModal.open}
  title="Permanently delete"
  message={purgeConfirmLabel(purgeModal.type)}
  confirmLabel="Permanently delete"
  variant="danger"
  busy={purgeModalBusy}
  onconfirm={() => void confirmPurgeEntity(purgeModal.type, purgeModal.id)}
  oncancel={() => {
    purgeModal = { open: false, type: "", id: "" };
  }}
/>

<ConfirmModal
  open={purgeAllOpen}
  title="Empty trash"
  message="Permanently delete all {activeItems.length} {entitySingular(
    activeTab,
  )}{activeItems.length === 1 ? '' : 's'} in this tab. This cannot be undone."
  confirmLabel="Permanently delete all"
  variant="danger"
  busy={purgeAllBusy}
  typedConfirmation="Empty trash"
  onconfirm={() => void purgeAll()}
  oncancel={() => {
    purgeAllOpen = false;
  }}
/>
