<script>
  import { goto, replaceState } from "$app/navigation";
  import { page } from "$app/stores";
  import BoardCard from "$lib/components/BoardCard.svelte";
  import CardDetailModal from "$lib/components/CardDetailModal.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import GuidedTypedRefsInput from "$lib/components/GuidedTypedRefsInput.svelte";
  import SearchableEntityPicker from "$lib/components/SearchableEntityPicker.svelte";
  import SearchableMultiEntityPicker from "$lib/components/SearchableMultiEntityPicker.svelte";
  import {
    actorRegistry,
    lookupActorDisplayName,
    principalRegistry,
  } from "$lib/actorSession";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import {
    backingThreadIdFromTopicRecord,
    searchDocuments as searchDocumentRecords,
    searchTopics as searchTopicRecords,
    topicSearchResultToPickerOption,
  } from "$lib/searchHelpers";
  import { workspacePath } from "$lib/workspacePaths";
  import {
    enrichInboxItem,
    formatInboxItemBoardPanelResourceLine,
  } from "$lib/inboxUtils";
  import {
    boardWorkspaceInspectNav,
    warningInspectNav,
  } from "$lib/topicRouteUtils";
  import RefLink from "$lib/components/RefLink.svelte";
  import {
    BOARD_STATUS_LABELS,
    BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT,
    boardBackingThreadId,
    boardCardStableId,
    boardColumnTitle,
    firstBoardDocumentId,
    freshnessStatusLabel,
    freshnessStatusTone,
    groupBoardWorkspaceCards,
    joinDelimitedValues,
    parseDelimitedValues,
  } from "$lib/boardUtils";
  import { withUpdatedSearchParams } from "$lib/urlState";

  let workspace = $state(null);
  let loading = $state(false);
  let error = $state("");
  let mutationNotice = $state("");
  let mutationError = $state("");
  let conflictWarning = $state("");

  let showBoardEditForm = $state(false);
  let showAddCardForm = $state(false);
  let updatingBoard = $state(false);
  let addingCard = $state(false);
  let backlogOpen = $state(false);
  let doneOpen = $state(false);
  let confirmModal = $state({ open: false, action: "" });
  let boardLifecycleBusy = $state(false);
  let detailModalCard = $state(null);

  let boardTitle = $state("");
  let boardStatus = $state("active");
  let boardDocumentId = $state("");
  let boardLabels = $state("");
  let boardOwners = $state([]);
  let boardPinnedRefs = $state("");

  let addCardTitle = $state("");
  let addCardSummary = $state("");
  let addCardThreadId = $state("");
  let addCardColumnKey = $state("backlog");
  let addCardDocumentId = $state("");
  let addCardRisk = $state("medium");
  let addCardResolution = $state("");
  let addCardResolutionRefs = $state("");
  let addCardRelatedRefs = $state("");
  let addCardAssignees = $state([]);
  let addCardDueAt = $state("");
  let addCardDefinitionOfDone = $state("");

  let previousBoardId = $state("");
  let previousCardUrlParam = $state("");
  let workspaceSlug = $derived($page.params.workspace);
  let boardId = $derived($page.params.boardId);
  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );
  let enrichedInboxItems = $derived(
    (workspace?.inbox?.items ?? []).map((item) => enrichInboxItem(item)),
  );
  let resolvedCards = $derived(
    (workspace?.cards?.items ?? []).filter((card) => {
      const r = String(card?.membership?.resolution ?? "").trim();
      return r === "done" || r === "canceled";
    }),
  );
  let actorOptions = $derived(
    $actorRegistry.map((actor) => ({
      id: actor.id,
      title: actor.display_name || actor.id,
      subtitle: actor.id,
      keywords: actor.tags ?? [],
    })),
  );

  function workspaceHref(pathname = "/") {
    return workspacePath(workspaceSlug, pathname);
  }

  function toDocumentOption(document) {
    return {
      id: document.id,
      title: document.title || document.id,
      subtitle: [
        document.state,
        document.thread_id && `Timeline ${document.thread_id}`,
      ]
        .filter(Boolean)
        .join(" · "),
      keywords: document.labels ?? [],
    };
  }

  async function searchThreadOptions(query) {
    const threads = await searchTopicRecords(query);
    return threads.map(topicSearchResultToPickerOption);
  }

  async function searchDocumentOptions(query) {
    const documents = await searchDocumentRecords(query);
    return documents.map(toDocumentOption);
  }

  function syncBoardDrafts(board) {
    boardTitle = board?.title ?? "";
    boardStatus = board?.status ?? "active";
    boardDocumentId = firstBoardDocumentId(board);
    boardLabels = joinDelimitedValues(board?.labels ?? []);
    boardOwners = [...(board?.owners ?? [])];
    boardPinnedRefs = joinDelimitedValues(board?.pinned_refs ?? []);
  }

  function openCardDetailModal(cardItem) {
    const id = boardCardStableId(cardItem?.membership);
    if (!id) return;
    detailModalCard = cardItem;
    replaceState(withUpdatedSearchParams($page.url, { card: id }), {});
  }

  function closeCardDetailModal() {
    detailModalCard = null;
    replaceState(withUpdatedSearchParams($page.url, { card: "" }), {});
  }

  function cardResolutionLabel(resolution) {
    switch (String(resolution ?? "").trim()) {
      case "done":
      case "completed":
        return "Done";
      case "canceled":
      case "cancelled":
        return "Canceled";
      case "superseded":
        return "Superseded";
      default:
        return "Open";
    }
  }

  function openBoardEditForm() {
    if (!workspace?.board) return;
    syncBoardDrafts(workspace.board);
    mutationError = "";
    showBoardEditForm = !showBoardEditForm;
  }

  function openAddCardForm() {
    addCardTitle = "";
    addCardSummary = "";
    addCardThreadId = "";
    addCardColumnKey = "backlog";
    addCardDocumentId = "";
    addCardRisk = "medium";
    addCardResolution = "";
    addCardResolutionRefs = "";
    addCardRelatedRefs = "";
    addCardAssignees = [];
    addCardDueAt = "";
    addCardDefinitionOfDone = "";
    mutationError = "";
    showAddCardForm = !showAddCardForm;
  }

  async function loadWorkspace() {
    loading = true;
    error = "";
    try {
      workspace = await coreClient.getBoardWorkspace(boardId);
      syncBoardDrafts(workspace?.board);
    } catch (e) {
      error = `Failed to load board: ${e instanceof Error ? e.message : String(e)}`;
      workspace = null;
    } finally {
      loading = false;
    }
  }

  function clearMutationMessages() {
    mutationNotice = "";
    mutationError = "";
    conflictWarning = "";
  }

  function formatMutationError(prefix, err) {
    const reason =
      err?.details || (err instanceof Error ? err.message : String(err));
    return `${prefix}: ${reason}`;
  }

  async function handleBoardConflict() {
    conflictWarning =
      "Board was updated elsewhere. Reloaded latest board state. Reapply your change.";
    mutationNotice = "";
    mutationError = "";
    await loadWorkspace();
  }

  async function runBoardMutation(action, successMessage, options = {}) {
    clearMutationMessages();

    try {
      await action();
      await loadWorkspace();

      if (options.closeBoardEdit) showBoardEditForm = false;
      if (options.closeAddCard) showAddCardForm = false;

      mutationNotice = successMessage;
    } catch (e) {
      if (e?.status === 409) {
        await handleBoardConflict();
        return;
      }

      mutationError = formatMutationError("Board write failed", e);
    }
  }

  async function submitBoardUpdate() {
    if (!workspace?.board) return;

    const title = boardTitle.trim();
    if (!title) {
      mutationError = "Board title is required.";
      return;
    }

    const docId = boardDocumentId.trim();
    const patch = {
      title,
      status: boardStatus,
      document_refs: docId ? [`document:${docId}`] : [],
      labels: parseDelimitedValues(boardLabels),
      owners: [...boardOwners],
      pinned_refs: parseDelimitedValues(boardPinnedRefs),
    };

    updatingBoard = true;
    await runBoardMutation(
      () =>
        coreClient.updateBoard(boardId, {
          if_updated_at: workspace.board.updated_at,
          patch,
        }),
      "Board updated.",
      { closeBoardEdit: true },
    );
    updatingBoard = false;
  }

  async function submitAddCard() {
    if (!workspace?.board) return;

    let resolvedTitle = addCardTitle.trim();
    const summary = addCardSummary.trim();
    const threadId = addCardThreadId.trim();
    if (!resolvedTitle && threadId) {
      try {
        const topics = await searchTopicRecords(threadId);
        const match =
          topics.find((t) => backingThreadIdFromTopicRecord(t) === threadId) ??
          topics[0];
        resolvedTitle = String(match?.title ?? "").trim() || threadId;
      } catch {
        resolvedTitle = threadId;
      }
    }
    if (!resolvedTitle && !threadId) {
      mutationError =
        "Enter a card title, or link a topic or backing thread (timeline ID).";
      return;
    }
    if (!resolvedTitle) {
      mutationError = "Card title is required.";
      return;
    }

    const related_refs = String(addCardRelatedRefs ?? "")
      .split(/\r?\n|,/)
      .map((item) => item.trim())
      .filter(Boolean);
    if (threadId) {
      const token = `thread:${threadId}`;
      if (!related_refs.includes(token)) {
        related_refs.push(token);
      }
    }

    addingCard = true;
    await runBoardMutation(
      () =>
        coreClient.addBoardCard(boardId, {
          if_board_updated_at: workspace.board.updated_at,
          title: resolvedTitle,
          summary: summary || resolvedTitle,
          column_key: addCardColumnKey,
          document_ref: addCardDocumentId.trim()
            ? `document:${addCardDocumentId.trim()}`
            : null,
          assignee_refs: [...addCardAssignees],
          risk: addCardRisk,
          resolution: addCardResolution.trim() || null,
          resolution_refs: String(addCardResolutionRefs ?? "")
            .split(/\r?\n|,/)
            .map((item) => item.trim())
            .filter(Boolean),
          related_refs,
          due_at: addCardDueAt.trim() || null,
          definition_of_done: String(addCardDefinitionOfDone ?? "")
            .split(/\r?\n|,/)
            .map((item) => item.trim())
            .filter(Boolean),
        }),
      "Card added.",
      { closeAddCard: true },
    );
    addingCard = false;
  }

  async function moveCard(cardItem, payload, successMessage) {
    if (!workspace?.board) return;

    const cardId = boardCardStableId(cardItem.membership);
    const nextPayload = {
      if_board_updated_at: workspace.board.updated_at,
      ...payload,
    };
    if (
      String(nextPayload.column_key ?? "").trim() === "done" &&
      !nextPayload.resolution
    ) {
      nextPayload.resolution = "done";
    }
    await runBoardMutation(
      () => coreClient.moveBoardCard(boardId, cardId, nextPayload),
      successMessage,
    );
  }

  async function saveCardDetails(cardItem, patch) {
    if (!workspace?.board) return;

    const cardId = boardCardStableId(cardItem.membership);
    await runBoardMutation(
      () =>
        coreClient.updateBoardCard(boardId, cardId, {
          if_board_updated_at: workspace.board.updated_at,
          patch,
        }),
      "Card details updated.",
    );
  }

  async function removeCard(cardItem) {
    if (!workspace?.board) return;

    const cardId = boardCardStableId(cardItem.membership);
    await runBoardMutation(
      () =>
        coreClient.removeBoardCard(boardId, cardId, {
          if_board_updated_at: workspace.board.updated_at,
        }),
      "Card removed.",
    );
    closeCardDetailModal();
  }

  function statusColor(status) {
    if (status === "active") return "text-emerald-400 bg-emerald-500/10";
    if (status === "paused") return "text-amber-400 bg-amber-500/10";
    if (status === "closed") return "text-slate-300 bg-slate-500/10";
    return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
  }

  function boardProjectionMessage(freshness) {
    switch (String(freshness?.status ?? "").trim()) {
      case "current":
        return "Derived board summaries are current. Canonical board membership and backing refs are aligned with the latest materialized scan.";
      case "pending":
        return "Derived summaries are being refreshed. Treat canonical board membership as authoritative until the scan catches up.";
      case "error":
        return "Derived summaries failed to refresh. Canonical board membership remains trustworthy, but scan counts may be behind.";
      case "missing":
        return "Derived summaries have not been materialized yet. Canonical board membership is available now; derived scan details are not.";
      default:
        return "Canonical board membership is available, but derived scan freshness is unknown.";
    }
  }

  $effect(() => {
    if (workspaceSlug && boardId) {
      void loadWorkspace();
    }
  });

  $effect(() => {
    boardId;
    confirmModal = { open: false, action: "" };
  });

  $effect(() => {
    const id = boardId;
    if (previousBoardId && previousBoardId !== id) {
      detailModalCard = null;
    }
    previousBoardId = id;
  });

  $effect(() => {
    const cardParam = String($page.url.searchParams.get("card") ?? "").trim();
    if (previousCardUrlParam && !cardParam) {
      detailModalCard = null;
    }
    previousCardUrlParam = cardParam;
  });

  $effect(() => {
    const cardParam = String($page.url.searchParams.get("card") ?? "").trim();
    if (!cardParam) {
      return;
    }
    const items = workspace?.cards?.items;
    if (!items) return;

    const match = items.find(
      (c) => boardCardStableId(c.membership) === cardParam,
    );
    if (match) {
      detailModalCard = match;
    } else {
      detailModalCard = null;
      replaceState(withUpdatedSearchParams($page.url, { card: "" }), {});
    }
  });

  async function handleArchiveBoard() {
    if (!boardId || boardLifecycleBusy || workspace?.board?.trashed_at) return;
    boardLifecycleBusy = true;
    try {
      await coreClient.archiveBoard(boardId, {});
      await loadWorkspace();
    } finally {
      boardLifecycleBusy = false;
    }
  }

  async function handleUnarchiveBoard() {
    confirmModal = { open: false, action: "" };
    if (!boardId || boardLifecycleBusy || workspace?.board?.trashed_at) return;
    boardLifecycleBusy = true;
    try {
      await coreClient.unarchiveBoard(boardId, {});
      await loadWorkspace();
    } finally {
      boardLifecycleBusy = false;
    }
  }

  function handleConfirm() {
    const action = confirmModal.action;
    confirmModal = { open: false, action: "" };
    if (action === "archive") handleArchiveBoard();
    else if (action === "trash") handleTrashBoard();
  }

  async function handleTrashBoard() {
    if (!boardId || boardLifecycleBusy) return;
    boardLifecycleBusy = true;
    try {
      await coreClient.trashBoard(boardId, {});
      confirmModal = { open: false, action: "" };
      await goto(workspacePath(workspaceSlug, "/boards"));
    } finally {
      boardLifecycleBusy = false;
    }
  }

  async function handleRestoreBoard() {
    confirmModal = { open: false, action: "" };
    if (!boardId || boardLifecycleBusy) return;
    boardLifecycleBusy = true;
    try {
      await coreClient.restoreBoard(boardId, {});
      await loadWorkspace();
    } finally {
      boardLifecycleBusy = false;
    }
  }
</script>

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
    Loading board...
  </div>
{:else if error}
  <div class="mb-4 rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
    {error}
  </div>
{:else if workspace}
  {@const board = workspace.board}
  {@const backingThreadId = boardBackingThreadId(board)}
  {@const boardInspectNav = boardWorkspaceInspectNav(workspace)}
  {@const backingThread =
    workspace.backing_thread ??
    (backingThreadId ? { id: backingThreadId, title: backingThreadId } : null)}
  {@const cardsByColumn = groupBoardWorkspaceCards(
    workspace.cards,
    board.column_schema,
  )}
  {@const boardWarnings = workspace.warnings?.items ?? []}

  {@const boardFreshness = workspace.projection_freshness}
  {@const activeColumns = board.column_schema.filter(
    (c) => c.key !== "backlog" && c.key !== "done",
  )}
  {@const backlogCards = cardsByColumn["backlog"] ?? []}
  {@const doneCards = cardsByColumn["done"] ?? []}

  {#if board.trashed_at}
    <div
      class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-[13px] text-red-400"
    >
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2 font-semibold">
          <span>⚠</span>
          <span>This board is in trash</span>
        </div>
        {#if board.trash_reason}
          <p class="mt-2">Reason: {board.trash_reason}</p>
        {/if}
        <p class="mt-1 text-[11px] text-red-400/80">
          Trashed {#if board.trashed_by}by {actorName(board.trashed_by)}{/if}
          {#if board.trashed_at}
            at {formatTimestamp(board.trashed_at)}
          {/if}
        </p>
      </div>
      <button
        class="shrink-0 cursor-pointer rounded-md border border-red-500/40 bg-red-500/15 px-2 py-1 text-[12px] font-medium text-red-400 hover:bg-red-500/25 disabled:opacity-50"
        disabled={boardLifecycleBusy}
        onclick={handleRestoreBoard}
        type="button"
      >
        {boardLifecycleBusy ? "…" : "Restore"}
      </button>
    </div>
  {:else if board.archived_at}
    <div
      class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-[13px] text-amber-400"
    >
      <p class="min-w-0 flex-1">
        This board was archived on {formatTimestamp(board.archived_at) ||
          "—"}{#if board.archived_by}
          by {actorName(board.archived_by)}{/if}.
      </p>
      <button
        class="shrink-0 cursor-pointer rounded-md border border-amber-500/40 bg-amber-500/15 px-2 py-1 text-[12px] font-medium text-amber-400 hover:bg-amber-500/25 disabled:opacity-50"
        disabled={boardLifecycleBusy}
        onclick={handleUnarchiveBoard}
        type="button"
      >
        {boardLifecycleBusy ? "…" : "Unarchive"}
      </button>
    </div>
  {/if}

  <div class="mb-3">
    <div class="flex items-center gap-2 text-[12px]">
      <a
        class="text-[var(--ui-text-muted)] transition-colors hover:text-[var(--ui-text)]"
        href={workspaceHref("/boards")}
      >
        Boards
      </a>
      <span class="text-[var(--ui-text-subtle)]">/</span>
      <span class="text-[var(--ui-text-muted)]">
        {workspace?.board?.title || boardId}
      </span>
    </div>

    <div class="mt-1.5 flex items-center justify-between gap-3">
      <div class="flex min-w-0 items-center gap-2">
        <h1 class="truncate text-lg font-semibold text-[var(--ui-text)]">
          {board.title || board.id}
        </h1>
        {#if board.status}
          <span
            class="shrink-0 rounded px-1.5 py-0.5 text-[11px] font-semibold {statusColor(
              board.status,
            )}"
          >
            {BOARD_STATUS_LABELS[board.status] ?? board.status}
          </span>
        {/if}
        <span
          class="shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium {freshnessStatusTone(
            boardFreshness?.status,
          )}"
          title={`${boardProjectionMessage(boardFreshness)} Generated ${formatTimestamp(workspace.generated_at) || "—"}.`}
        >
          {freshnessStatusLabel(boardFreshness?.status)}
        </span>
      </div>

      {#if !board.trashed_at}
        <div class="flex shrink-0 flex-wrap items-center justify-end gap-2">
          {#if !board.archived_at}
            <button
              aria-label="Archive"
              class="cursor-pointer rounded-md p-1.5 text-[var(--ui-text-muted)] hover:bg-[var(--ui-border)] hover:text-[var(--ui-accent)] disabled:opacity-50"
              disabled={boardLifecycleBusy}
              onclick={() => (confirmModal = { open: true, action: "archive" })}
              type="button"
            >
              <svg
                class="h-4 w-4"
                fill="currentColor"
                viewBox="0 0 24 24"
                aria-hidden="true"
              >
                <path
                  d="M20.25 7.5l-.625 10.632a2.25 2.25 0 01-2.247 2.118H6.622a2.25 2.25 0 01-2.247-2.118L3.75 7.5m8.25 3v6.75m0 0l-3-3m3 3l3-3M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125z"
                />
              </svg>
            </button>
          {/if}
          <button
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] hover:text-[var(--ui-text)]"
            onclick={openBoardEditForm}
            type="button"
          >
            {showBoardEditForm ? "Close" : "Edit"}
          </button>
          <button
            class="rounded-md bg-indigo-600 px-2.5 py-1.5 text-[12px] font-medium text-white transition-colors hover:bg-indigo-500"
            onclick={openAddCardForm}
            type="button"
          >
            {showAddCardForm ? "Close" : "Add card"}
          </button>
          <button
            aria-label="Move board to trash"
            class="cursor-pointer rounded-md p-1.5 text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border)] hover:text-red-400 disabled:opacity-50"
            disabled={boardLifecycleBusy}
            onclick={() => (confirmModal = { open: true, action: "trash" })}
            type="button"
          >
            <svg
              class="h-4 w-4"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0"
              />
            </svg>
          </button>
        </div>
      {/if}
    </div>

    <!-- Single context line -->
    <div
      class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-[var(--ui-text-muted)]"
    >
      {#if backingThread && boardInspectNav}
        <span class="text-[var(--ui-text-subtle)]"
          >{boardInspectNav.kind === "topic" ? "Topic" : "Backing thread"}</span
        >
        <a
          class="text-indigo-400 transition-colors hover:text-indigo-300"
          href={workspaceHref(
            boardInspectNav.kind === "topic"
              ? `/topics/${encodeURIComponent(boardInspectNav.segment)}`
              : `/threads/${encodeURIComponent(boardInspectNav.segment)}`,
          )}
        >
          {backingThread.title || boardInspectNav.segment}
        </a>
        <span class="text-[var(--ui-text-subtle)]">·</span>
      {/if}
      <span>
        {workspace.board_summary?.card_count ?? workspace.cards?.count ?? 0}
        cards
      </span>
      <span class="text-[var(--ui-text-subtle)]">·</span>
      <span>
        {workspace.board_summary?.resolved_card_count ?? 0} resolved
      </span>
      <span class="text-[var(--ui-text-subtle)]">·</span>
      <span>
        {workspace.board_summary?.unresolved_card_count ?? 0} open
      </span>
      <span class="text-[var(--ui-text-subtle)]">·</span>
      <span>Board updated {formatTimestamp(board.updated_at) || "—"}</span>
      {#if board.owners?.length > 0}
        <span class="text-[var(--ui-text-subtle)]">·</span>
        <span
          >Owners {board.owners
            .map((owner) => actorName(owner))
            .join(", ")}</span
        >
      {/if}
    </div>
  </div>

  <!-- Notification alerts -->
  {#if mutationNotice || conflictWarning || mutationError}
    <div class="mb-3 space-y-2">
      {#if mutationNotice}
        <div
          class="rounded-md bg-emerald-500/10 px-3 py-2 text-[12px] text-emerald-400"
        >
          {mutationNotice}
        </div>
      {/if}
      {#if conflictWarning}
        <div
          class="rounded-md bg-amber-500/10 px-3 py-2 text-[12px] text-amber-400"
        >
          {conflictWarning}
        </div>
      {/if}
      {#if mutationError}
        <div
          class="rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
        >
          {mutationError}
        </div>
      {/if}
    </div>
  {/if}

  <!-- Board edit form (collapsible) -->
  {#if showBoardEditForm}
    <section
      class="mb-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
    >
      <div class="border-b border-[var(--ui-border)] px-4 py-2.5">
        <h2 class="text-[13px] font-medium text-[var(--ui-text)]">
          Edit board metadata
        </h2>
      </div>

      <div class="space-y-3 px-4 py-3">
        <div class="grid gap-3 md:grid-cols-2">
          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Board title
            <input
              bind:value={boardTitle}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-1.5 text-[13px] text-[var(--ui-text)]"
              type="text"
            />
          </label>

          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Status
            <select
              bind:value={boardStatus}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-1.5 text-[13px] text-[var(--ui-text)]"
            >
              {#each Object.entries(BOARD_STATUS_LABELS) as [value, label]}
                <option {value}>{label}</option>
              {/each}
            </select>
          </label>

          <SearchableEntityPicker
            bind:value={boardDocumentId}
            advancedLabel="Use a manual document ID"
            helperText="Optional: add or replace the board document ref surfaced in board refs."
            label="Board document"
            manualLabel="Document ID"
            manualPlaceholder="incident-response-playbook"
            placeholder="Search documents by title, ID, or timeline ID"
            searchFn={searchDocumentOptions}
          />

          <div
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[12px] text-[var(--ui-text-muted)]"
          >
            Board thread is fixed after creation (append-only event timeline).
            <div class="mt-1 text-[var(--ui-text)]">
              {backingThread?.title || backingThreadId}
            </div>
            <div class="mt-1 text-[11px] text-[var(--ui-text-subtle)]">
              {backingThreadId}
            </div>
          </div>
        </div>

        <div class="grid gap-3 md:grid-cols-2">
          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Labels
            <textarea
              bind:value={boardLabels}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-1.5 text-[13px] text-[var(--ui-text)]"
              rows="3"
            ></textarea>
          </label>

          <SearchableMultiEntityPicker
            bind:values={boardOwners}
            advancedLabel="Add a manual owner ID"
            helperText="Owners are canonical board metadata and stay visible across board list and detail views."
            items={actorOptions}
            label="Owners"
            manualLabel="Owner ID"
            manualPlaceholder="actor-ops-ai"
            placeholder="Search actors by name, ID, or tags"
          />
        </div>

        <div>
          <p class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Pinned refs
          </p>
          <GuidedTypedRefsInput
            bind:value={boardPinnedRefs}
            addInputLabel="Add board pinned ref"
            addInputPlaceholder="thread:board-q2-initiative"
            addButtonLabel="Add ref"
            emptyText="No pinned refs yet."
            helperText="These refs are shown at the top of the board."
            textareaAriaLabel="Board pinned refs"
          />
        </div>

        <div class="flex flex-wrap gap-2">
          <button
            class="rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white transition-colors hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-60"
            disabled={updatingBoard}
            onclick={submitBoardUpdate}
            type="button"
          >
            {updatingBoard ? "Saving..." : "Save board"}
          </button>
          <button
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] hover:text-[var(--ui-text)]"
            onclick={() => {
              showBoardEditForm = false;
              mutationError = "";
            }}
            type="button"
          >
            Cancel
          </button>
        </div>
      </div>
    </section>
  {/if}

  <!-- Add card form (collapsible) -->
  {#if showAddCardForm}
    <section
      class="mb-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
    >
      <div class="border-b border-[var(--ui-border)] px-4 py-2.5">
        <h2 class="text-[13px] font-medium text-[var(--ui-text)]">Add card</h2>
      </div>

      <div class="space-y-3 px-4 py-3">
        <div class="grid gap-3 md:grid-cols-2">
          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Card title
            <input
              bind:value={addCardTitle}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-1.5 text-[13px] text-[var(--ui-text)]"
              type="text"
            />
          </label>

          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Target column
            <select
              bind:value={addCardColumnKey}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-1.5 text-[13px] text-[var(--ui-text)]"
            >
              {#each board.column_schema as column}
                <option value={column.key}>
                  {column.title ||
                    boardColumnTitle(column.key, board.column_schema)}
                </option>
              {/each}
            </select>
          </label>

          <label
            class="text-[12px] font-medium text-[var(--ui-text-muted)] md:col-span-2"
          >
            Summary
            <textarea
              bind:value={addCardSummary}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-1.5 text-[13px] text-[var(--ui-text)]"
              rows="3"
            ></textarea>
          </label>

          <SearchableEntityPicker
            bind:value={addCardThreadId}
            advancedLabel="Use a manual thread ID"
            disabledIds={[backingThreadId].filter(Boolean)}
            helperText="Optional: pick a topic (binds its backing thread) or paste a thread ID. Add further typed refs in Related refs."
            label="Topic or backing thread"
            manualLabel="Thread ID"
            manualPlaceholder="thread-onboarding"
            placeholder="Search topics by title, ID, or tags"
            searchFn={searchThreadOptions}
          />

          <SearchableEntityPicker
            bind:value={addCardDocumentId}
            advancedLabel="Use a manual document ID"
            helperText="Optional: surface one canonical doc lineage directly on the card."
            label="Document"
            manualLabel="Document ID"
            manualPlaceholder="onboarding-guide-v1"
            placeholder="Search documents by title, ID, or timeline ID"
            searchFn={searchDocumentOptions}
          />

          <SearchableMultiEntityPicker
            bind:values={addCardAssignees}
            advancedLabel="Add a manual assignee ID"
            helperText="Optional assignees for the card."
            items={actorOptions}
            label="Assignees"
            manualLabel="Assignee ID"
            manualPlaceholder="actor-ops-ai"
            placeholder="Search actors by name, ID, or tags"
          />

          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Risk
            <select
              bind:value={addCardRisk}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-1.5 text-[13px] text-[var(--ui-text)]"
            >
              <option value="low">Low</option>
              <option value="medium">Medium</option>
              <option value="high">High</option>
              <option value="critical">Critical</option>
            </select>
          </label>

          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Resolution
            <select
              bind:value={addCardResolution}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-1.5 text-[13px] text-[var(--ui-text)]"
            >
              <option value="">Open</option>
              <option value="done">Done</option>
              <option value="canceled">Canceled</option>
            </select>
          </label>

          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Due date
            <input
              bind:value={addCardDueAt}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-1.5 text-[13px] text-[var(--ui-text)]"
              type="datetime-local"
            />
          </label>

          <label
            class="text-[12px] font-medium text-[var(--ui-text-muted)] md:col-span-2"
          >
            Definition of done
            <textarea
              bind:value={addCardDefinitionOfDone}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-1.5 text-[13px] text-[var(--ui-text)]"
              rows="3"
            ></textarea>
          </label>

          <div class="md:col-span-2">
            <p class="text-[12px] font-medium text-[var(--ui-text-muted)]">
              Related refs
            </p>
            <GuidedTypedRefsInput
              bind:value={addCardRelatedRefs}
              {boardId}
              addInputLabel="Add related ref"
              addInputPlaceholder="topic:summer-menu-rollout"
              addButtonLabel="Add ref"
              emptyText="No related refs yet."
              helperText="Optional typed refs (topic:, document:, board:, thread:, …)."
              textareaAriaLabel="Card related refs"
            />
          </div>

          <div class="md:col-span-2">
            <p class="text-[12px] font-medium text-[var(--ui-text-muted)]">
              Resolution evidence
            </p>
            <GuidedTypedRefsInput
              bind:value={addCardResolutionRefs}
              {boardId}
              addInputLabel="Add resolution ref"
              addInputPlaceholder="artifact:receipt-123"
              addButtonLabel="Add ref"
              emptyText="No resolution evidence yet."
              helperText="Optional typed refs that evidence the card's resolution."
              textareaAriaLabel="Card resolution refs"
            />
          </div>
        </div>

        <div class="flex flex-wrap gap-2">
          <button
            class="rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white transition-colors hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-60"
            disabled={addingCard}
            onclick={submitAddCard}
            type="button"
          >
            {addingCard ? "Adding..." : "Add card"}
          </button>
          <button
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] hover:text-[var(--ui-text)]"
            onclick={() => {
              showAddCardForm = false;
              mutationError = "";
            }}
            type="button"
          >
            Cancel
          </button>
        </div>
      </div>
    </section>
  {/if}

  {#snippet renderCard(cardItem)}
    <BoardCard
      {cardItem}
      {boardId}
      onclick={() => openCardDetailModal(cardItem)}
    />
  {/snippet}

  <section class="mb-3">
    <div class="mb-2 flex items-baseline justify-between gap-3">
      <div>
        <h2 class="text-[13px] font-medium text-[var(--ui-text)]">
          Visual progress map
        </h2>
        <p class="mt-1 text-[12px] text-[var(--ui-text-muted)]">
          Canonical board membership drives the columns below. Derived scan
          badges only appear when their freshness is current.
        </p>
      </div>
    </div>

    <div
      class="mb-3 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
    >
      <button
        class="flex w-full items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-[var(--ui-border-subtle)]"
        onclick={() => {
          backlogOpen = !backlogOpen;
        }}
        type="button"
      >
        <svg
          class="h-3.5 w-3.5 shrink-0 text-[var(--ui-text-muted)] transition-transform {backlogOpen
            ? 'rotate-90'
            : ''}"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="2"><path d="M9 5l7 7-7 7" /></svg
        >
        <span class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Backlog</span
        >
        <span
          class="rounded bg-[var(--ui-border)] px-1.5 py-0.5 text-[11px] text-[var(--ui-text-subtle)]"
          >{backlogCards.length}</span
        >
      </button>
      {#if backlogOpen}
        <div class="space-y-2 border-t border-[var(--ui-border)] px-3 py-2">
          {#if backlogCards.length === 0}
            <p class="text-[11px] text-[var(--ui-text-subtle)]">No cards</p>
          {:else}
            {#each backlogCards as cardItem}
              {@render renderCard(cardItem)}
            {/each}
          {/if}
        </div>
      {/if}
    </div>

    <div class="flex gap-3 overflow-x-auto pb-4">
      {#each activeColumns as column}
        {@const cards = cardsByColumn[column.key] ?? []}
        <div
          class="flex min-w-[260px] flex-1 flex-col rounded-md bg-[var(--ui-panel-muted)]"
        >
          <div class="flex items-center justify-between px-3 py-2.5">
            <h3
              class="text-[11px] font-semibold uppercase tracking-wide text-[var(--ui-text-muted)]"
            >
              {column.title ||
                boardColumnTitle(column.key, board.column_schema)}
            </h3>
            <span
              class="min-w-[1.25rem] rounded bg-[var(--ui-border)] px-1.5 py-0.5 text-center text-[11px] text-[var(--ui-text-subtle)]"
            >
              {cards.length}
            </span>
          </div>
          <div
            class="flex-1 space-y-2 overflow-y-auto px-2 pb-2"
            style="max-height: calc(100vh - 260px); min-height: 120px;"
          >
            {#if cards.length === 0}
              <div
                class="flex items-center justify-center rounded-md border border-dashed border-[var(--ui-border)] px-3 py-10 text-[11px] text-[var(--ui-text-subtle)]"
              >
                No cards
              </div>
            {:else}
              {#each cards as cardItem}
                {@render renderCard(cardItem)}
              {/each}
            {/if}
          </div>
        </div>
      {/each}
    </div>

    <div
      class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
    >
      <button
        class="flex w-full items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-[var(--ui-border-subtle)]"
        onclick={() => {
          doneOpen = !doneOpen;
        }}
        type="button"
      >
        <svg
          class="h-3.5 w-3.5 shrink-0 text-[var(--ui-text-muted)] transition-transform {doneOpen
            ? 'rotate-90'
            : ''}"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="2"><path d="M9 5l7 7-7 7" /></svg
        >
        <span class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Done</span
        >
        <span
          class="rounded bg-[var(--ui-border)] px-1.5 py-0.5 text-[11px] text-[var(--ui-text-subtle)]"
          >{doneCards.length}</span
        >
      </button>
      {#if doneOpen}
        <div class="space-y-2 border-t border-[var(--ui-border)] px-3 py-2">
          {#if doneCards.length === 0}
            <p class="text-[11px] text-[var(--ui-text-subtle)]">No cards</p>
          {:else}
            {#each doneCards as cardItem}
              {@render renderCard(cardItem)}
            {/each}
          {/if}
        </div>
      {/if}
    </div>
  </section>

  <div class="grid gap-3 lg:grid-cols-3">
    <section
      class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
    >
      <div class="border-b border-[var(--ui-border)] px-4 py-2.5">
        <h2 class="text-[13px] font-medium text-[var(--ui-text)]">
          Workspace docs
        </h2>
        <p class="mt-1 text-[11px] text-[var(--ui-text-subtle)]">
          Canonical doc lineages linked from this board's backing thread
          timeline and cards.
        </p>
      </div>
      <div class="px-4 py-3">
        {#if (workspace.documents?.items ?? []).length === 0}
          <p class="text-[12px] text-[var(--ui-text-subtle)]">
            No linked doc lineages yet.
          </p>
        {:else}
          <div class="space-y-2">
            {#each workspace.documents.items.slice(0, BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT) as document}
              <a
                class="block rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[12px] transition-colors hover:border-[var(--ui-border-strong)]"
                href={workspaceHref(`/docs/${encodeURIComponent(document.id)}`)}
              >
                <div class="font-medium text-[var(--ui-text)]">
                  {document.title || document.id}
                </div>
                <div class="mt-1 text-[11px] text-[var(--ui-text-subtle)]">
                  Head v{document.head_revision_number ?? "—"} · Updated {formatTimestamp(
                    document.updated_at,
                  ) || "—"}
                </div>
              </a>
            {/each}
          </div>
          {#if (workspace.documents?.items ?? []).length > BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT}
            <p class="mt-2 text-[11px] text-[var(--ui-text-muted)]">
              Showing {BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT} of {workspace
                .documents.items.length}
            </p>
          {/if}
        {/if}
      </div>
    </section>

    <section
      class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
    >
      <div class="border-b border-[var(--ui-border)] px-4 py-2.5">
        <h2 class="text-[13px] font-medium text-[var(--ui-text)]">
          Resolved cards
        </h2>
        <p class="mt-1 text-[11px] text-[var(--ui-text-subtle)]">
          First-class cards with explicit resolution state and evidence refs.
        </p>
      </div>
      <div class="px-4 py-3">
        {#if resolvedCards.length === 0}
          <p class="text-[12px] text-[var(--ui-text-subtle)]">
            No resolved cards in this board slice.
          </p>
        {:else}
          <div class="space-y-2">
            {#each resolvedCards.slice(0, BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT) as cardItem}
              {@const membership = cardItem.membership}
              <div
                class="rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2"
              >
                <div class="text-[12px] font-medium text-[var(--ui-text)]">
                  {membership.title || membership.id}
                </div>
                <div class="mt-1 text-[11px] text-[var(--ui-text-subtle)]">
                  {cardResolutionLabel(membership.resolution)} · Due
                  {formatTimestamp(membership.due_at) || "—"}
                  {#if Array.isArray(membership.resolution_refs) && membership.resolution_refs.length > 0}
                    · {membership.resolution_refs.length} evidence refs
                  {/if}
                </div>
              </div>
            {/each}
          </div>
          {#if resolvedCards.length > BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT}
            <p class="mt-2 text-[11px] text-[var(--ui-text-muted)]">
              Showing {BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT} of {resolvedCards.length}
            </p>
          {/if}
        {/if}
      </div>
    </section>

    <section
      class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
    >
      <div class="border-b border-[var(--ui-border)] px-4 py-2.5">
        <h2 class="text-[13px] font-medium text-[var(--ui-text)]">
          Review inbox
        </h2>
        <p class="mt-1 text-[11px] text-[var(--ui-text-subtle)]">
          Derived risk and decision signals for resources tied to this board
          (backing threads).
        </p>
      </div>
      <div class="px-4 py-3">
        {#if enrichedInboxItems.length === 0}
          <p class="text-[12px] text-[var(--ui-text-subtle)]">
            No active derived inbox items.
          </p>
        {:else}
          <div class="space-y-2">
            {#each enrichedInboxItems.slice(0, BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT) as item}
              {@const inboxResourceLine =
                formatInboxItemBoardPanelResourceLine(item)}
              <div
                class="rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2"
              >
                <div class="text-[12px] font-medium text-[var(--ui-text)]">
                  {item.title || item.summary || item.id}
                </div>
                <div class="mt-1 text-[11px] text-[var(--ui-text-subtle)]">
                  {item.urgency_label}
                  {#if inboxResourceLine}
                    · {inboxResourceLine}
                  {/if}
                </div>
              </div>
            {/each}
          </div>
          {#if enrichedInboxItems.length > BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT}
            <p class="mt-2 text-[11px] text-[var(--ui-text-muted)]">
              Showing {BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT} of {enrichedInboxItems.length}
            </p>
          {/if}
        {/if}
      </div>
    </section>
  </div>

  {#if boardWarnings.length > 0}
    <section
      class="mt-4 rounded-md border border-amber-500/20 bg-amber-500/10 px-4 py-3"
    >
      <h2 class="text-[13px] font-medium text-amber-400">Warnings</h2>
      <div class="mt-2 space-y-1.5">
        {#each boardWarnings as warning}
          <div class="text-[12px] text-amber-400">
            {warning.message || "Workspace warning"}
            {#if warning.topic_id || warning.thread_id}
              {@const warnNav = warningInspectNav(warning)}
              {#if warnNav}
                {@const warnRef =
                  warnNav.kind === "topic"
                    ? `topic:${warnNav.segment}`
                    : `thread:${warnNav.segment}`}
                <span
                  class="ml-1 inline [&_a]:font-medium [&_a]:text-amber-300 [&_a]:underline [&_a]:transition-colors [&_a:hover]:text-amber-200"
                >
                  <RefLink refValue={warnRef} {boardId} humanize showRaw />
                </span>
              {/if}
            {/if}
          </div>
        {/each}
      </div>
    </section>
  {/if}
{/if}

<CardDetailModal
  open={detailModalCard !== null}
  cardItem={detailModalCard}
  {boardId}
  board={workspace?.board ?? null}
  {workspaceSlug}
  primaryTopic={workspace?.primary_topic ?? null}
  {actorName}
  onclose={closeCardDetailModal}
  onmovecard={moveCard}
  onsavecard={saveCardDetails}
  onremovecard={removeCard}
/>

<ConfirmModal
  open={confirmModal.open}
  title={confirmModal.action === "trash" ? "Move to trash" : "Archive board"}
  message={confirmModal.action === "trash"
    ? "This board will be moved to trash. You can restore it later."
    : "This board will be hidden from default views. You can unarchive it later."}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={boardLifecycleBusy}
  onconfirm={handleConfirm}
  oncancel={() => (confirmModal = { open: false, action: "" })}
/>
