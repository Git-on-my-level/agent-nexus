<script>
  import { onMount } from "svelte";
  import { goto, replaceState } from "$app/navigation";
  import { page } from "$app/stores";

  import Button from "$lib/components/Button.svelte";
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
  import { boardRecordFromBoardsListRow } from "$lib/searchHelpers";
  import { readEnumSearchParam, withUpdatedSearchParams } from "$lib/urlState";
  import {
    documentLifecycleLabel,
    documentLifecyclePillClass,
    documentResourceState,
  } from "$lib/documentVisibility";
  import LeadingSelectionGlyph from "$lib/components/LeadingSelectionGlyph.svelte";
  import WorkspaceListBulkToolbar from "$lib/components/WorkspaceListBulkToolbar.svelte";
  import { splitTypedRef } from "$lib/inboxUtils";
  import { createWorkspaceListSelection } from "$lib/workspaceListSelection.svelte.js";
  import { workspacePath } from "$lib/workspacePaths";

  const TRASH_TAB_IDS = ["artifacts", "documents", "topics", "boards", "cards"];

  /** @typedef {"artifacts"|"documents"|"topics"|"boards"|"cards"} TrashEntityType */

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);

  let bulkTrashBusy = $state(false);
  let restoreBulkOpen = $state(false);
  let restoreBulkBusy = $state(false);

  const trashSel = createWorkspaceListSelection({
    bulkBusy: () =>
      bulkTrashBusy ||
      Boolean(busyItemId) ||
      purgeModalBusy ||
      purgeAllBusy ||
      restoreBulkBusy,
  });

  /** @param {string} pathname */
  function workspaceHref(pathname = "/") {
    return workspacePath(organizationSlug, workspaceSlug, pathname);
  }

  /** @param {*} card */
  function cardTrashNavigateHref(card) {
    const id = String(card?.id ?? "").trim();
    if (!id) return workspaceHref("/trash");
    const rawBoard = String(card?.board_ref ?? "").trim();
    const { prefix, id: boardId } = splitTypedRef(rawBoard);
    if (prefix === "board" && boardId) {
      return workspaceHref(`/boards/${boardId}?card=${encodeURIComponent(id)}`);
    }
    return workspaceHref("/trash");
  }

  /** @param {number} idx */
  function trashIdAtVisibleIndex(idx) {
    return String(activeItems[idx]?.id ?? "").trim();
  }

  /** @param {number} idx */
  function trashHrefAtVisibleIndex(idx) {
    const id = trashIdAtVisibleIndex(idx);
    if (!id) return workspaceHref("/trash");
    switch (activeTab) {
      case "artifacts":
        return workspaceHref(`/artifacts/${id}`);
      case "documents":
        return workspaceHref(`/docs/${id}`);
      case "topics":
        return workspaceHref(`/topics/${encodeURIComponent(id)}`);
      case "boards":
        return workspaceHref(`/boards/${id}`);
      case "cards":
        return cardTrashNavigateHref(activeItems[idx]);
      default:
        return workspaceHref("/trash");
    }
  }

  let artifacts = $state([]);
  let documents = $state([]);
  let threads = $state([]);
  let boards = $state([]);
  let cards = $state([]);

  let activeTab = $state("artifacts");
  let loading = $state(true);
  let error = $state("");
  let purgeModal = $state({
    open: false,
    type: "",
    id: "",
    bulkIds: /** @type {string[] | null} */ (null),
  });
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
  function actorName(id) {
    return lookupActorDisplayName(id, $actorRegistry, $principalRegistry);
  }

  function itemBusyKey(type, id) {
    return `${type}:${String(id ?? "").trim()}`;
  }

  let selectedTrashEntities = $derived(
    activeItems.filter((item) =>
      trashSel.selectedIds.has(String(item?.id ?? "").trim()),
    ),
  );

  let allTrashVisibleSelected = $derived(
    activeItems.length > 0 &&
      activeItems.every((item) =>
        trashSel.selectedIds.has(String(item?.id ?? "").trim()),
      ),
  );

  /** Cards tab hides row actions unless devActorMode — mirror for bulk toolbar. */
  let trashBulkRestoreEligible = $derived.by(() => {
    if (activeTab === "cards" && !$devActorMode) return false;
    return true;
  });

  let trashBulkPurgeEligible = $derived.by(() => {
    if (!isHumanPrincipal) return false;
    if (activeTab === "topics") return false;
    if (activeTab === "cards" && !$devActorMode) return false;
    return true;
  });

  let trashBulkUxEnabled = $derived.by(() => {
    if (activeTab === "cards" && !$devActorMode) return false;
    return true;
  });

  function selectAllVisibleTrash() {
    trashSel.selectAllFromVisibleIds(
      activeItems.map((item) => String(item?.id ?? "").trim()).filter(Boolean),
    );
  }

  function clearTrashSelection() {
    trashSel.clearSelection();
  }

  function defaultActorBody() {
    const body = {};
    if (!getAuthenticatedActorId()) {
      body.actor_id = getSelectedActorId();
    }
    return body;
  }

  /** @param {TrashEntityType} type */
  async function purgeOneEntity(type, id) {
    const body = defaultActorBody();
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
        throw new Error("Unsupported purge type");
    }
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
    trashSel.exitSelectionMode();
    purgeModal = { open: false, type: "", id: "", bulkIds: null };
    purgeAllOpen = false;
    await goto(withUpdatedSearchParams($page.url, { tab: tabId }), {
      replaceState: true,
      noScroll: true,
      keepFocus: true,
    });
  }

  function threadLifecycleColor(state) {
    const styles = {
      active: "text-ok-text",
      archived: "text-warn-text",
      trashed: "text-danger-text",
    };
    return styles[state] ?? "text-[var(--fg-muted)]";
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
        coreClient.listArtifacts({ state: ["trashed"] }),
        coreClient.listDocuments({ state: ["trashed"] }),
        coreClient.listTopics({ state: ["trashed"] }),
        coreClient.listBoards({ state: ["trashed"] }),
        coreClient.listCards({ state: ["archived"] }),
        coreClient.listCards({ state: ["trashed"] }),
      ]);
      artifacts = artifactResult.artifacts ?? [];
      documents = docResult.documents ?? [];
      threads = (topicResult.topics ?? []).filter(
        (topic) =>
          Boolean(topic?.archived_at) ||
          Boolean(topic?.trashed_at) ||
          String(topic?.state ?? "").trim() === "archived" ||
          String(topic?.state ?? "").trim() === "trashed",
      );
      boards = (boardResult.boards ?? []).map(boardRecordFromBoardsListRow);
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
        (card) => Boolean(card?.archived_at) || Boolean(card?.trashed_at),
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

  $effect(() => {
    activeTab;
    trashSel.exitSelectionMode();
  });

  $effect(() => {
    activeItems;
    trashSel.reconcileSelectionWithIds(
      activeItems.map((item) => String(item?.id ?? "").trim()).filter(Boolean),
    );
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

  async function confirmPurgeConfirmed() {
    const rawType = purgeModal.type;
    const type = /** @type {TrashEntityType} */ (rawType);
    const bulkIds = purgeModal.bulkIds;
    const ids =
      Array.isArray(bulkIds) && bulkIds.length > 0
        ? [
            ...new Set(
              bulkIds.map((x) => String(x ?? "").trim()).filter(Boolean),
            ),
          ]
        : [String(purgeModal.id ?? "").trim()].filter(Boolean);
    if (!ids.length || purgeModalBusy) return;
    purgeModalBusy = true;
    const multi = ids.length > 1;
    if (multi) bulkTrashBusy = true;
    if (!multi) busyItemId = itemBusyKey(rawType, ids[0]);
    error = "";
    try {
      for (const id of ids) {
        await purgeOneEntity(type, id);
      }
      purgeModal = { open: false, type: "", id: "", bulkIds: null };
      trashSel.exitSelectionMode();
      await loadTrash();
    } catch (e) {
      error = `Permanent delete failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      purgeModalBusy = false;
      bulkTrashBusy = false;
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
    const tabType = /** @type {TrashEntityType} */ (activeTab);
    for (const item of items) {
      const id = String(item?.id ?? "").trim();
      if (!id) continue;
      try {
        await purgeOneEntity(tabType, id);
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

  function entityPluralTab(tab) {
    switch (tab) {
      case "artifacts":
        return "artifacts";
      case "documents":
        return "documents";
      case "topics":
        return "topics";
      case "boards":
        return "boards";
      case "cards":
        return "cards";
      default:
        return "items";
    }
  }

  let purgeConfirmMessage = $derived.by(() => {
    const bulk = purgeModal.bulkIds;
    const n = Array.isArray(bulk) ? bulk.length : 0;
    if (n > 1) {
      return `Permanently delete ${n} ${entityPluralTab(purgeModal.type)}? This cannot be undone.`;
    }
    return purgeConfirmLabel(purgeModal.type);
  });

  let restoreBulkConfirmMessage = $derived(
    `Restore ${trashSel.selectedIds.size} ${entityPluralTab(activeTab)} from trash? Items return to their normal lists.`,
  );

  function requestBulkRestore() {
    if (!trashBulkRestoreEligible || trashSel.selectedIds.size === 0) return;
    restoreBulkOpen = true;
  }

  async function confirmBulkRestore() {
    const t = /** @type {TrashEntityType} */ (activeTab);
    const ids = selectedTrashEntities
      .map((it) => String(it?.id ?? "").trim())
      .filter(Boolean);
    if (!ids.length || restoreBulkBusy) return;
    restoreBulkBusy = true;
    bulkTrashBusy = true;
    error = "";
    try {
      for (const id of ids) {
        switch (t) {
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
            break;
        }
      }
      restoreBulkOpen = false;
      trashSel.exitSelectionMode();
      await loadTrash();
    } catch (e) {
      error = `Restore failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      restoreBulkBusy = false;
      bulkTrashBusy = false;
    }
  }

  function requestBulkPurge() {
    if (!trashBulkPurgeEligible || trashSel.selectedIds.size === 0) return;
    const ids = selectedTrashEntities
      .map((it) => String(it?.id ?? "").trim())
      .filter(Boolean);
    if (!ids.length) return;
    purgeModal = {
      open: true,
      type: activeTab,
      id: "",
      bulkIds: ids,
    };
  }
</script>

<div class="mb-3 flex max-md:mb-2 items-start justify-between gap-4">
  <div>
    <h1 class="text-subtitle font-semibold text-[var(--fg)]">Trash</h1>
    <p class="mt-0.5 text-micro text-[var(--fg-muted)]">
      Trashed items available for restore or permanent deletion. Restore returns
      them to their normal lists; permanent delete removes supported resource
      types (human principals only). Topics can be restored but not permanently
      deleted from this surface yet. Trashed events and messages are restored
      from within their timeline view.
    </p>
  </div>
  <div class="shrink-0 flex flex-wrap items-center gap-2 justify-end">
    {#if !loading && activeItems.length > 0 && trashBulkUxEnabled}
      <button
        type="button"
        class="inline-flex h-8 cursor-pointer items-center rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 text-micro font-medium text-[var(--fg)] transition-colors hover:bg-[var(--line-subtle)] disabled:cursor-not-allowed disabled:opacity-50"
        disabled={Boolean(busyItemId) ||
          purgeAllBusy ||
          purgeModalBusy ||
          bulkTrashBusy ||
          restoreBulkBusy}
        onclick={() => trashSel.toggleSelectMode()}
      >
        {trashSel.selectMode ? "Done" : "Select"}
      </button>
    {/if}
    {#if isHumanPrincipal && !loading && activeItems.length > 0 && activeTab !== "topics" && (activeTab !== "cards" || $devActorMode)}
      <div class="shrink-0">
        <Button
          variant="destructive"
          size="compact"
          disabled={Boolean(busyItemId) || purgeAllBusy}
          onclick={() => {
            purgeAllOpen = true;
          }}
        >
          Permanently delete all ({activeItems.length})
        </Button>
      </div>
    {/if}
  </div>
</div>

<div class="mb-4 flex gap-0 border-b border-[var(--line)]" role="tablist">
  {#each tabs as tab (tab.id)}
    <button
      class="cursor-pointer px-3 py-2 text-meta font-medium transition-colors {activeTab ===
      tab.id
        ? 'border-b-2 border-[var(--accent)] text-[var(--fg)]'
        : 'text-[var(--fg-muted)] hover:text-[var(--fg)]'}"
      onclick={() => void switchTab(tab.id)}
      role="tab"
      aria-selected={activeTab === tab.id}
      type="button"
    >
      {tab.label}
      {#if tab.count > 0}
        <span class="ml-1 text-micro text-[var(--fg-muted)]">({tab.count})</span
        >
      {/if}
    </button>
  {/each}
</div>

{#if error}
  <div
    class="mb-4 rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
  >
    {error}
  </div>
{/if}

{#if !loading && activeItems.length > 0 && trashSel.selectMode && trashBulkUxEnabled}
  <WorkspaceListBulkToolbar
    selectionChromeActive={true}
    selectedCount={trashSel.selectedIds.size}
    busy={bulkTrashBusy ||
      restoreBulkBusy ||
      Boolean(busyItemId) ||
      purgeModalBusy}
    canRestore={trashBulkRestoreEligible}
    canTrash={trashBulkPurgeEligible}
    onRestore={() => requestBulkRestore()}
    onTrash={() => requestBulkPurge()}
    trashLabel="Permanently delete"
    onClear={clearTrashSelection}
    onSelectAll={selectAllVisibleTrash}
    onDeselectAll={clearTrashSelection}
    allVisibleSelected={allTrashVisibleSelected}
  />
{/if}

{#if loading}
  <div
    class="mt-12 flex items-center justify-center gap-2 text-meta text-[var(--fg-muted)]"
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
    <p class="text-meta font-medium text-[var(--fg-muted)]">
      {emptyCategoryMessage(activeTab)}
    </p>
  </div>
{/if}

{#if !loading && activeTab === "artifacts" && artifacts.length > 0}
  <div
    class="space-y-px rounded-md border border-[var(--line)] bg-[var(--bg-soft)] overflow-hidden"
  >
    {#each artifacts as artifact, i (artifact.id)}
      {@const selected = trashSel.selectedIds.has(String(artifact.id).trim())}
      {@const borderTop = i > 0 ? "border-t border-[var(--line)]" : ""}
      {#if trashSel.selectMode}
        <div
          class="transition-colors hover:bg-[var(--line-subtle)] {borderTop}"
        >
          <div
            aria-label={`${selected ? "Deselect" : "Select"} ${rowHeading(artifact)}`}
            aria-pressed={selected}
            class="flex cursor-pointer items-stretch outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg-soft)] {selected
              ? 'border-l-[3px] border-l-[var(--accent)] bg-[var(--accent)]/10'
              : 'border-l-[3px] border-l-transparent'}"
            onclick={(e) =>
              trashSel.handleRowMouseEvent(
                e,
                i,
                artifacts.length,
                trashIdAtVisibleIndex,
                trashHrefAtVisibleIndex,
              )}
            onkeydown={(e) =>
              trashSel.handleRowKeyboardEvent(e, i, trashIdAtVisibleIndex)}
            role="button"
            tabindex="0"
          >
            <div class="flex shrink-0 items-center self-stretch pl-2 sm:pl-3">
              <LeadingSelectionGlyph {selected} />
            </div>
            <div
              class="pointer-events-none flex min-w-0 flex-1 flex-col gap-3 py-3 pr-4 sm:pr-5 pl-2"
            >
              <div class="flex flex-wrap items-center gap-2">
                <span
                  class="inline-flex rounded px-1.5 py-0.5 text-micro font-semibold {kindColor(
                    artifact.kind,
                  )}"
                >
                  {kindLabel(artifact.kind)}
                </span>
                <span class="text-meta font-medium text-[var(--fg)]">
                  {rowHeading(artifact)}
                </span>
              </div>

              <div
                class="grid gap-x-4 gap-y-1 text-micro text-[var(--fg-muted)] sm:grid-cols-2 xl:grid-cols-3"
              >
                <div>
                  <span class="text-[var(--fg-muted)]">Created</span>
                  {formatTimestamp(artifact.created_at) || "—"}
                  <span class="text-[var(--fg-subtle)]"> · </span>
                  {actorName(artifact.created_by)}
                </div>
                <div>
                  <span class="text-[var(--fg-muted)]">Trashed</span>
                  {formatTimestamp(artifact.trashed_at) || "—"}
                  <span class="text-[var(--fg-subtle)]"> · </span>
                  {actorName(artifact.trashed_by)}
                </div>
                <div class="sm:col-span-2 xl:col-span-1">
                  <span class="text-[var(--fg-muted)]">Reason</span>
                  {trashReason(artifact)}
                </div>
              </div>
            </div>
          </div>
        </div>
      {:else}
        <a
          class="group block px-4 py-2.5 text-left outline-none transition-colors hover:bg-[var(--line-subtle)] focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-inset focus-visible:ring-offset-0 {borderTop}"
          href={workspaceHref(`/artifacts/${artifact.id}`)}
        >
          <div class="flex flex-wrap items-center gap-2">
            <span
              class="inline-flex rounded px-1.5 py-0.5 text-micro font-semibold {kindColor(
                artifact.kind,
              )}"
            >
              {kindLabel(artifact.kind)}
            </span>
            <span
              class="text-meta font-medium text-[var(--fg)] group-hover:text-[var(--fg)]"
              >{rowHeading(artifact)}</span
            >
          </div>
          <div
            class="mt-1 grid gap-x-4 gap-y-0.5 text-micro text-[var(--fg-muted)] sm:grid-cols-2 xl:grid-cols-3"
          >
            <div class="min-w-0 sm:col-span-1">
              <span class="text-[var(--fg-muted)]">Created</span>
              {formatTimestamp(artifact.created_at) || "—"}
              <span class="text-[var(--fg-subtle)]"> · </span>
              {actorName(artifact.created_by)}
            </div>
            <div class="min-w-0 sm:col-span-1">
              <span class="text-[var(--fg-muted)]">Trashed</span>
              {formatTimestamp(artifact.trashed_at) || "—"}
              <span class="text-[var(--fg-subtle)]"> · </span>
              {actorName(artifact.trashed_by)}
            </div>
            <div class="min-w-0 sm:col-span-2 xl:col-span-1">
              <span class="text-[var(--fg-muted)]">Reason</span>
              <span class="line-clamp-2">{trashReason(artifact)}</span>
            </div>
          </div>
        </a>
      {/if}
    {/each}
  </div>
{/if}

{#if !loading && activeTab === "documents" && documents.length > 0}
  <div
    class="space-y-px rounded-md border border-[var(--line)] bg-[var(--bg-soft)] overflow-hidden"
  >
    {#each documents as doc, i (doc.id)}
      {@const docState = documentResourceState(doc)}
      {@const selected = trashSel.selectedIds.has(String(doc.id).trim())}
      {@const borderTop = i > 0 ? "border-t border-[var(--line)]" : ""}
      {#if trashSel.selectMode}
        <div
          class="transition-colors hover:bg-[var(--line-subtle)] {borderTop}"
        >
          <div
            aria-label={`${selected ? "Deselect" : "Select"} ${documentTitle(doc)}`}
            aria-pressed={selected}
            class="flex cursor-pointer items-stretch outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg-soft)] {selected
              ? 'border-l-[3px] border-l-[var(--accent)] bg-[var(--accent)]/10'
              : 'border-l-[3px] border-l-transparent'}"
            onclick={(e) =>
              trashSel.handleRowMouseEvent(
                e,
                i,
                documents.length,
                trashIdAtVisibleIndex,
                trashHrefAtVisibleIndex,
              )}
            onkeydown={(e) =>
              trashSel.handleRowKeyboardEvent(e, i, trashIdAtVisibleIndex)}
            role="button"
            tabindex="0"
          >
            <div class="flex shrink-0 items-center self-stretch pl-2 sm:pl-3">
              <LeadingSelectionGlyph {selected} />
            </div>
            <div
              class="pointer-events-none flex min-w-0 flex-1 flex-col gap-3 py-3 pr-4 sm:pr-5 pl-2"
            >
              <div class="flex flex-wrap items-center gap-2">
                {#if docState}
                  <span
                    class="inline-flex rounded px-1.5 py-0.5 text-micro font-semibold {documentLifecyclePillClass(
                      docState,
                    )}">{documentLifecycleLabel(docState)}</span
                  >
                {/if}
                <span class="text-meta font-medium text-[var(--fg)]">
                  {documentTitle(doc)}
                </span>
              </div>
              <div
                class="grid gap-x-4 gap-y-1 text-micro text-[var(--fg-muted)] sm:grid-cols-2 xl:grid-cols-3"
              >
                <div>
                  <span class="text-[var(--fg-muted)]">Created</span>
                  {formatTimestamp(doc.created_at) || "—"}
                  <span class="text-[var(--fg-subtle)]"> · </span>
                  {actorName(doc.created_by)}
                </div>
                <div>
                  <span class="text-[var(--fg-muted)]">Trashed</span>
                  {formatTimestamp(doc.trashed_at) || "—"}
                  <span class="text-[var(--fg-subtle)]"> · </span>
                  {actorName(doc.trashed_by)}
                </div>
                <div class="sm:col-span-2 xl:col-span-1">
                  <span class="text-[var(--fg-muted)]">Reason</span>
                  {trashReason(doc)}
                </div>
              </div>
            </div>
          </div>
        </div>
      {:else}
        <a
          class="group block px-4 py-2.5 text-left outline-none transition-colors hover:bg-[var(--line-subtle)] focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-inset focus-visible:ring-offset-0 {borderTop}"
          href={workspaceHref(`/docs/${doc.id}`)}
        >
          <div class="flex flex-wrap items-center gap-2">
            {#if docState}
              <span
                class="inline-flex rounded px-1.5 py-0.5 text-micro font-semibold {documentLifecyclePillClass(
                  docState,
                )}">{documentLifecycleLabel(docState)}</span
              >
            {/if}
            <span class="text-meta font-medium text-[var(--fg)]"
              >{documentTitle(doc)}</span
            >
          </div>
          <div
            class="mt-1 grid gap-x-4 gap-y-0.5 text-micro text-[var(--fg-muted)] sm:grid-cols-2 xl:grid-cols-3"
          >
            <div class="min-w-0 sm:col-span-1">
              <span class="text-[var(--fg-muted)]">Created</span>
              {formatTimestamp(doc.created_at) || "—"}
              <span class="text-[var(--fg-subtle)]"> · </span>
              {actorName(doc.created_by)}
            </div>
            <div class="min-w-0 sm:col-span-1">
              <span class="text-[var(--fg-muted)]">Trashed</span>
              {formatTimestamp(doc.trashed_at) || "—"}
              <span class="text-[var(--fg-subtle)]"> · </span>
              {actorName(doc.trashed_by)}
            </div>
            <div class="min-w-0 sm:col-span-2 xl:col-span-1">
              <span class="text-[var(--fg-muted)]">Reason</span>
              <span class="line-clamp-2">{trashReason(doc)}</span>
            </div>
          </div>
        </a>
      {/if}
    {/each}
  </div>
{/if}

{#if !loading && activeTab === "topics" && threads.length > 0}
  <div
    class="space-y-px rounded-md border border-[var(--line)] bg-[var(--bg-soft)] overflow-hidden"
  >
    {#each threads as thread, i (thread.id)}
      {@const selected = trashSel.selectedIds.has(String(thread.id).trim())}
      {@const borderTop = i > 0 ? "border-t border-[var(--line)]" : ""}
      {#if trashSel.selectMode}
        <div
          class="transition-colors hover:bg-[var(--line-subtle)] {borderTop}"
        >
          <div
            aria-label={`${selected ? "Deselect" : "Select"} ${String(thread?.title ?? "").trim() || thread.id}`}
            aria-pressed={selected}
            class="flex cursor-pointer items-stretch outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg-soft)] {selected
              ? 'border-l-[3px] border-l-[var(--accent)] bg-[var(--accent)]/10'
              : 'border-l-[3px] border-l-transparent'}"
            onclick={(e) =>
              trashSel.handleRowMouseEvent(
                e,
                i,
                threads.length,
                trashIdAtVisibleIndex,
                trashHrefAtVisibleIndex,
              )}
            onkeydown={(e) =>
              trashSel.handleRowKeyboardEvent(e, i, trashIdAtVisibleIndex)}
            role="button"
            tabindex="0"
          >
            <div class="flex shrink-0 items-center self-stretch pl-2 sm:pl-3">
              <LeadingSelectionGlyph {selected} />
            </div>
            <div
              class="pointer-events-none flex min-w-0 flex-1 flex-col gap-3 py-3 pr-4 sm:pr-5 pl-2"
            >
              <div class="flex flex-wrap items-center gap-2">
                <span class="text-meta font-medium text-[var(--fg)]">
                  {String(thread?.title ?? "").trim() || thread.id}
                </span>
                {#if thread.state}
                  <span
                    class="rounded bg-[var(--panel)] px-1.5 py-0.5 text-micro font-medium capitalize {threadLifecycleColor(
                      thread.state,
                    )}"
                    >{BOARD_STATUS_LABELS[thread.state] ?? thread.state}</span
                  >
                {/if}
              </div>
              {#if topicSummary(thread)}
                <p class="text-micro text-[var(--fg-muted)]">
                  {topicSummary(thread)}
                </p>
              {/if}
              <div
                class="grid gap-x-4 gap-y-1 text-micro text-[var(--fg-muted)] sm:grid-cols-2 xl:grid-cols-3"
              >
                <div>
                  <span class="text-[var(--fg-muted)]">Created</span>
                  {formatTimestamp(threadCreatedAt(thread)) || "—"}
                  {#if thread.created_by}
                    <span class="text-[var(--fg-subtle)]"> · </span>
                    {actorName(thread.created_by)}
                  {/if}
                </div>
                <div>
                  <span class="text-[var(--fg-muted)]">Trashed</span>
                  {formatTimestamp(thread.trashed_at) || "—"}
                  <span class="text-[var(--fg-subtle)]"> · </span>
                  {actorName(thread.trashed_by)}
                </div>
                <div class="sm:col-span-2 xl:col-span-1">
                  <span class="text-[var(--fg-muted)]">Reason</span>
                  {trashReason(thread)}
                </div>
              </div>
            </div>
          </div>
        </div>
      {:else}
        <a
          class="group block px-4 py-2.5 text-left outline-none transition-colors hover:bg-[var(--line-subtle)] focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-inset focus-visible:ring-offset-0 {borderTop}"
          href={workspaceHref(`/topics/${encodeURIComponent(thread.id)}`)}
        >
          <div class="flex flex-wrap items-center gap-2">
            <span class="text-meta font-medium text-[var(--fg)]">
              {String(thread?.title ?? "").trim() || thread.id}
            </span>
            {#if thread.state}
              <span
                class="rounded bg-[var(--panel)] px-1.5 py-0.5 text-micro font-medium capitalize {threadLifecycleColor(
                  thread.state,
                )}">{BOARD_STATUS_LABELS[thread.state] ?? thread.state}</span
              >
            {/if}
          </div>
          {#if topicSummary(thread)}
            <p class="mt-0.5 line-clamp-2 text-micro text-[var(--fg-muted)]">
              {topicSummary(thread)}
            </p>
          {/if}
          <div
            class="mt-1 grid gap-x-4 gap-y-0.5 text-micro text-[var(--fg-muted)] sm:grid-cols-2 xl:grid-cols-3"
          >
            <div class="min-w-0 sm:col-span-1">
              <span class="text-[var(--fg-muted)]">Created</span>
              {formatTimestamp(threadCreatedAt(thread)) || "—"}
              {#if thread.created_by}
                <span class="text-[var(--fg-subtle)]"> · </span>
                {actorName(thread.created_by)}
              {/if}
            </div>
            <div class="min-w-0 sm:col-span-1">
              <span class="text-[var(--fg-muted)]">Trashed</span>
              {formatTimestamp(thread.trashed_at) || "—"}
              <span class="text-[var(--fg-subtle)]"> · </span>
              {actorName(thread.trashed_by)}
            </div>
            <div class="min-w-0 sm:col-span-2 xl:col-span-1">
              <span class="text-[var(--fg-muted)]">Reason</span>
              <span class="line-clamp-2">{trashReason(thread)}</span>
            </div>
          </div>
        </a>
      {/if}
    {/each}
  </div>
{/if}

{#if !loading && activeTab === "boards" && boards.length > 0}
  <div
    class="space-y-px rounded-md border border-[var(--line)] bg-[var(--bg-soft)] overflow-hidden"
  >
    {#each boards as board, i (board.id)}
      {@const selected = trashSel.selectedIds.has(String(board.id).trim())}
      {@const borderTop = i > 0 ? "border-t border-[var(--line)]" : ""}
      {#if trashSel.selectMode}
        <div
          class="transition-colors hover:bg-[var(--line-subtle)] {borderTop}"
        >
          <div
            aria-label={`${selected ? "Deselect" : "Select"} ${String(board?.title ?? "").trim() || board.id}`}
            aria-pressed={selected}
            class="flex cursor-pointer items-stretch outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg-soft)] {selected
              ? 'border-l-[3px] border-l-[var(--accent)] bg-[var(--accent)]/10'
              : 'border-l-[3px] border-l-transparent'}"
            onclick={(e) =>
              trashSel.handleRowMouseEvent(
                e,
                i,
                boards.length,
                trashIdAtVisibleIndex,
                trashHrefAtVisibleIndex,
              )}
            onkeydown={(e) =>
              trashSel.handleRowKeyboardEvent(e, i, trashIdAtVisibleIndex)}
            role="button"
            tabindex="0"
          >
            <div class="flex shrink-0 items-center self-stretch pl-2 sm:pl-3">
              <LeadingSelectionGlyph {selected} />
            </div>
            <div
              class="pointer-events-none flex min-w-0 flex-1 flex-col gap-3 py-3 pr-4 sm:pr-5 pl-2"
            >
              <div class="flex flex-wrap items-center gap-2">
                <span class="text-meta font-medium text-[var(--fg)]">
                  {String(board?.title ?? "").trim() || board.id}
                </span>
                {#if board.state}
                  <span
                    class="rounded bg-[var(--panel)] px-1.5 py-0.5 text-micro font-medium text-[var(--fg-muted)]"
                    >{BOARD_STATUS_LABELS[board.state] ?? board.state}</span
                  >
                {/if}
              </div>
              <div
                class="grid gap-x-4 gap-y-1 text-micro text-[var(--fg-muted)] sm:grid-cols-2 xl:grid-cols-3"
              >
                <div>
                  <span class="text-[var(--fg-muted)]">Created</span>
                  {formatTimestamp(board.created_at) || "—"}
                  <span class="text-[var(--fg-subtle)]"> · </span>
                  {actorName(board.created_by)}
                </div>
                <div>
                  <span class="text-[var(--fg-muted)]">Trashed</span>
                  {formatTimestamp(board.trashed_at) || "—"}
                  <span class="text-[var(--fg-subtle)]"> · </span>
                  {actorName(board.trashed_by)}
                </div>
                <div class="sm:col-span-2 xl:col-span-1">
                  <span class="text-[var(--fg-muted)]">Reason</span>
                  {trashReason(board)}
                </div>
              </div>
            </div>
          </div>
        </div>
      {:else}
        <a
          class="group block px-4 py-2.5 text-left outline-none transition-colors hover:bg-[var(--line-subtle)] focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-inset focus-visible:ring-offset-0 {borderTop}"
          href={workspaceHref(`/boards/${board.id}`)}
        >
          <div class="flex flex-wrap items-center gap-2">
            <span class="text-meta font-medium text-[var(--fg)]">
              {String(board?.title ?? "").trim() || board.id}
            </span>
            {#if board.state}
              <span
                class="rounded bg-[var(--panel)] px-1.5 py-0.5 text-micro font-medium text-[var(--fg-muted)]"
                >{BOARD_STATUS_LABELS[board.state] ?? board.state}</span
              >
            {/if}
          </div>
          <div
            class="mt-1 grid gap-x-4 gap-y-0.5 text-micro text-[var(--fg-muted)] sm:grid-cols-2 xl:grid-cols-3"
          >
            <div class="min-w-0 sm:col-span-1">
              <span class="text-[var(--fg-muted)]">Created</span>
              {formatTimestamp(board.created_at) || "—"}
              <span class="text-[var(--fg-subtle)]"> · </span>
              {actorName(board.created_by)}
            </div>
            <div class="min-w-0 sm:col-span-1">
              <span class="text-[var(--fg-muted)]">Trashed</span>
              {formatTimestamp(board.trashed_at) || "—"}
              <span class="text-[var(--fg-subtle)]"> · </span>
              {actorName(board.trashed_by)}
            </div>
            <div class="min-w-0 sm:col-span-2 xl:col-span-1">
              <span class="text-[var(--fg-muted)]">Reason</span>
              <span class="line-clamp-2">{trashReason(board)}</span>
            </div>
          </div>
        </a>
      {/if}
    {/each}
  </div>
{/if}

{#if !loading && activeTab === "cards" && cards.length > 0}
  <div
    class="space-y-px rounded-md border border-[var(--line)] bg-[var(--bg-soft)] overflow-hidden"
  >
    {#each cards as card, i (card.id)}
      {@const selected = trashSel.selectedIds.has(String(card.id).trim())}
      {@const borderTop = i > 0 ? "border-t border-[var(--line)]" : ""}
      {#if trashSel.selectMode}
        <div
          class="transition-colors hover:bg-[var(--line-subtle)] {borderTop}"
        >
          <div
            aria-label={`${selected ? "Deselect" : "Select"} ${String(card?.title ?? "").trim() || card.id}`}
            aria-pressed={selected}
            class="flex cursor-pointer items-stretch outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg-soft)] {selected
              ? 'border-l-[3px] border-l-[var(--accent)] bg-[var(--accent)]/10'
              : 'border-l-[3px] border-l-transparent'}"
            onclick={(e) =>
              trashSel.handleRowMouseEvent(
                e,
                i,
                cards.length,
                trashIdAtVisibleIndex,
                trashHrefAtVisibleIndex,
              )}
            onkeydown={(e) =>
              trashSel.handleRowKeyboardEvent(e, i, trashIdAtVisibleIndex)}
            role="button"
            tabindex="0"
          >
            <div class="flex shrink-0 items-center self-stretch pl-2 sm:pl-3">
              <LeadingSelectionGlyph {selected} />
            </div>
            <div
              class="pointer-events-none flex min-w-0 flex-1 flex-col gap-3 py-3 pr-4 sm:pr-5 pl-2"
            >
              <div class="flex flex-wrap items-center gap-2">
                <span class="text-meta font-medium text-[var(--fg)]">
                  {String(card?.title ?? "").trim() || card.id}
                </span>
                {#if card.risk}
                  <span
                    class="rounded bg-[var(--panel)] px-1.5 py-0.5 text-micro font-medium text-[var(--fg-muted)]"
                    >{String(card.risk).trim()}</span
                  >
                {/if}
                {#if card.resolution}
                  <span
                    class="rounded bg-[var(--panel)] px-1.5 py-0.5 text-micro font-medium text-[var(--fg-muted)]"
                    >{String(card.resolution).trim()}</span
                  >
                {/if}
              </div>
              {#if card.summary}
                <p class="text-micro text-[var(--fg-muted)]">
                  {card.summary}
                </p>
              {/if}
              <div
                class="grid gap-x-4 gap-y-1 text-micro text-[var(--fg-muted)] sm:grid-cols-2 xl:grid-cols-3"
              >
                <div>
                  <span class="text-[var(--fg-muted)]">Created</span>
                  {formatTimestamp(card.created_at) || "—"}
                  <span class="text-[var(--fg-subtle)]"> · </span>
                  {actorName(card.created_by)}
                </div>
                <div>
                  <span class="text-[var(--fg-muted)]">Archived</span>
                  {formatTimestamp(card.archived_at) || "—"}
                  <span class="text-[var(--fg-subtle)]"> · </span>
                  {actorName(card.archived_by)}
                </div>
                <div class="sm:col-span-2 xl:col-span-1">
                  <span class="text-[var(--fg-muted)]">Trashed</span>
                  {formatTimestamp(card.trashed_at) || "—"}
                  <span class="text-[var(--fg-subtle)]"> · </span>
                  {actorName(card.trashed_by)}
                </div>
                <div class="sm:col-span-2 xl:col-span-1">
                  <span class="text-[var(--fg-muted)]">Reason</span>
                  {trashReason(card)}
                </div>
              </div>
              <div class="flex flex-wrap items-center gap-2 text-micro">
                {#if card.board_ref}
                  <span
                    class="rounded bg-[var(--panel)] px-1.5 py-0.5 font-medium text-[var(--fg-muted)]"
                  >
                    Board: {card.board_ref}
                  </span>
                {/if}
                {#if card.topic_ref}
                  <span
                    class="rounded bg-[var(--panel)] px-1.5 py-0.5 font-medium text-[var(--fg-muted)]"
                  >
                    Topic: {card.topic_ref}
                  </span>
                {/if}
                {#if card.document_ref}
                  <span
                    class="rounded bg-[var(--panel)] px-1.5 py-0.5 font-medium text-[var(--fg-muted)]"
                  >
                    Doc: {card.document_ref}
                  </span>
                {/if}
                {#if Array.isArray(card.related_refs)}
                  {#each card.related_refs as refValue (refValue)}
                    <RefLink {refValue} threadId={card.thread_id} />
                  {/each}
                {/if}
              </div>
            </div>
          </div>
        </div>
      {:else}
        <div class="flex flex-col {borderTop}">
          <a
            class="group block px-4 py-2.5 text-left outline-none transition-colors hover:bg-[var(--line-subtle)] focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-inset focus-visible:ring-offset-0"
            href={cardTrashNavigateHref(card)}
          >
            <div class="flex flex-wrap items-center gap-2">
              <span class="text-meta font-medium text-[var(--fg)]">
                {String(card?.title ?? "").trim() || card.id}
              </span>
              {#if card.risk}
                <span
                  class="rounded bg-[var(--panel)] px-1.5 py-0.5 text-micro font-medium text-[var(--fg-muted)]"
                  >{String(card.risk).trim()}</span
                >
              {/if}
              {#if card.resolution}
                <span
                  class="rounded bg-[var(--panel)] px-1.5 py-0.5 text-micro font-medium text-[var(--fg-muted)]"
                  >{String(card.resolution).trim()}</span
                >
              {/if}
            </div>
            {#if card.summary}
              <p class="mt-0.5 line-clamp-2 text-micro text-[var(--fg-muted)]">
                {card.summary}
              </p>
            {/if}
            <div
              class="mt-1 grid gap-x-4 gap-y-0.5 text-micro text-[var(--fg-muted)] sm:grid-cols-2 xl:grid-cols-3"
            >
              <div class="min-w-0 sm:col-span-1">
                <span class="text-[var(--fg-muted)]">Created</span>
                {formatTimestamp(card.created_at) || "—"}
                <span class="text-[var(--fg-subtle)]"> · </span>
                {actorName(card.created_by)}
              </div>
              <div class="min-w-0 sm:col-span-1">
                <span class="text-[var(--fg-muted)]">Archived</span>
                {formatTimestamp(card.archived_at) || "—"}
                <span class="text-[var(--fg-subtle)]"> · </span>
                {actorName(card.archived_by)}
              </div>
              <div class="min-w-0 sm:col-span-2 xl:col-span-1">
                <span class="text-[var(--fg-muted)]">Trashed</span>
                {formatTimestamp(card.trashed_at) || "—"}
                <span class="text-[var(--fg-subtle)]"> · </span>
                {actorName(card.trashed_by)}
              </div>
              <div class="min-w-0 sm:col-span-2 xl:col-span-1">
                <span class="text-[var(--fg-muted)]">Reason</span>
                <span class="line-clamp-2">{trashReason(card)}</span>
              </div>
            </div>
          </a>
          {#if card.board_ref || card.topic_ref || card.document_ref || (Array.isArray(card.related_refs) && card.related_refs.length > 0)}
            <div
              class="border-t border-[var(--line)] px-4 py-2 text-micro text-[var(--fg-muted)]"
            >
              <div class="flex flex-wrap items-center gap-2">
                {#if card.board_ref}
                  <span
                    class="rounded bg-[var(--panel)] px-1.5 py-0.5 font-medium text-[var(--fg-muted)]"
                  >
                    Board: {card.board_ref}
                  </span>
                {/if}
                {#if card.topic_ref}
                  <span
                    class="rounded bg-[var(--panel)] px-1.5 py-0.5 font-medium text-[var(--fg-muted)]"
                  >
                    Topic: {card.topic_ref}
                  </span>
                {/if}
                {#if card.document_ref}
                  <span
                    class="rounded bg-[var(--panel)] px-1.5 py-0.5 font-medium text-[var(--fg-muted)]"
                  >
                    Doc: {card.document_ref}
                  </span>
                {/if}
                {#if Array.isArray(card.related_refs)}
                  {#each card.related_refs as refValue (refValue)}
                    <RefLink {refValue} threadId={card.thread_id} />
                  {/each}
                {/if}
              </div>
            </div>
          {/if}
        </div>
      {/if}
    {/each}
  </div>
{/if}

<ConfirmModal
  open={purgeModal.open}
  title="Permanently delete"
  message={purgeConfirmMessage}
  confirmLabel="Permanently delete"
  variant="danger"
  busy={purgeModalBusy}
  onconfirm={() => void confirmPurgeConfirmed()}
  oncancel={() =>
    (purgeModal = {
      open: false,
      type: "",
      id: "",
      bulkIds: null,
    })}
/>

<ConfirmModal
  open={restoreBulkOpen}
  title="Restore selected"
  message={restoreBulkConfirmMessage}
  confirmLabel="Restore"
  variant="warning"
  busy={restoreBulkBusy}
  onconfirm={() => void confirmBulkRestore()}
  oncancel={() => {
    restoreBulkOpen = false;
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
