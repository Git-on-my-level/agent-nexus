<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import GuidedTypedRefsInput from "$lib/components/GuidedTypedRefsInput.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
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
    searchDocuments as searchDocumentRecords,
    searchTopics as searchTopicRecords,
  } from "$lib/searchHelpers";
  import { workspacePath } from "$lib/workspacePaths";
  import { enrichInboxItem } from "$lib/inboxUtils";
  import {
    BOARD_STATUS_LABELS,
    boardBackingThreadId,
    boardCardLinkedThreadId,
    boardCardStableId,
    boardColumnTitle,
    firstBoardDocumentId,
    freshnessStatusLabel,
    freshnessStatusTone,
    groupBoardWorkspaceCards,
    isFreshnessCurrent,
    joinDelimitedValues,
    parseDelimitedValues,
  } from "$lib/boardUtils";

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
  let expandedCardId = $state("");
  let mutatingCardId = $state("");
  let backlogOpen = $state(false);
  let doneOpen = $state(false);
  let confirmModal = $state({ open: false, action: "" });
  let boardLifecycleBusy = $state(false);

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

  let manageMoveColumnKey = $state("backlog");
  let manageTitle = $state("");
  let manageSummary = $state("");
  let manageThreadId = $state("");
  let manageDocumentId = $state("");
  let manageRisk = $state("medium");
  let manageResolution = $state("");
  let manageResolutionRefs = $state("");
  let manageRelatedRefs = $state("");
  let manageAssignees = $state([]);
  let manageDueAt = $state("");
  let manageDefinitionOfDone = $state("");

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

  function toThreadOption(thread) {
    return {
      id: thread.id,
      title: thread.title || thread.id,
      subtitle: [thread.status, thread.priority].filter(Boolean).join(" · "),
      keywords: [thread.type, ...(thread.tags ?? [])],
    };
  }

  function toDocumentOption(document) {
    return {
      id: document.id,
      title: document.title || document.id,
      subtitle: [
        document.status,
        document.thread_id && `Topic ${document.thread_id}`,
      ]
        .filter(Boolean)
        .join(" · "),
      keywords: document.labels ?? [],
    };
  }

  async function searchThreadOptions(query) {
    const threads = await searchTopicRecords(query);
    return threads.map(toThreadOption);
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

  function normalizeResolutionForEdit(raw) {
    const r = String(raw ?? "").trim();
    if (!r || r === "unresolved") return "";
    if (r === "completed") return "done";
    if (r === "cancelled") return "canceled";
    return r;
  }

  function syncCardDrafts(cardItem) {
    const card = cardItem?.membership ?? {};
    manageTitle = card.title ?? "";
    manageSummary = card.summary ?? "";
    manageThreadId = card.thread_id ?? card.parent_thread ?? "";
    manageDocumentId = String(
      card.document_ref ?? card.pinned_document_id ?? "",
    )
      .replace(/^document:/, "")
      .trim();
    manageRisk = card.risk ?? "medium";
    manageResolution = normalizeResolutionForEdit(card.resolution);
    manageResolutionRefs = joinDelimitedValues(card.resolution_refs ?? []);
    manageRelatedRefs = joinDelimitedValues(card.related_refs ?? []);
    manageAssignees = [...(card.assignee_refs ?? [])];
    manageDueAt = card.due_at ?? "";
    manageDefinitionOfDone = joinDelimitedValues(card.definition_of_done ?? []);
    manageMoveColumnKey = card.column_key ?? "backlog";
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

  function cardResolutionTone(resolution) {
    switch (String(resolution ?? "").trim()) {
      case "done":
      case "completed":
        return "text-emerald-300 bg-emerald-500/10";
      case "canceled":
      case "cancelled":
        return "text-slate-300 bg-slate-500/10";
      case "superseded":
        return "text-amber-300 bg-amber-500/10";
      default:
        return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
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

  function openCardManager(cardItem) {
    const cardKey = boardCardStableId(cardItem.membership);
    if (expandedCardId === cardKey) {
      expandedCardId = "";
      return;
    }

    expandedCardId = cardKey;
    syncCardDrafts(cardItem);
    mutationError = "";
  }

  function threadStatusDotClass(status) {
    switch (status) {
      case "done":
        return "bg-emerald-400";
      case "canceled":
        return "bg-gray-500";
      case "paused":
        return "bg-amber-400";
      case "stale":
        return "bg-orange-400";
      case "very-stale":
        return "bg-red-400";
      default:
        return "bg-blue-400";
    }
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
      if (options.closeCardManager) expandedCardId = "";

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

    const title = addCardTitle.trim();
    const summary = addCardSummary.trim();
    const threadId = addCardThreadId.trim();
    if (!title && !threadId) {
      mutationError = "Enter a card title or pick a backing thread.";
      return;
    }

    addingCard = true;
    await runBoardMutation(
      () =>
        coreClient.addBoardCard(boardId, {
          if_board_updated_at: workspace.board.updated_at,
          title,
          summary: summary || title,
          thread_id: threadId || null,
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
          related_refs: String(addCardRelatedRefs ?? "")
            .split(/\r?\n|,/)
            .map((item) => item.trim())
            .filter(Boolean),
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
    mutatingCardId = cardId;
    const nextPayload = {
      if_board_updated_at: workspace.board.updated_at,
      ...payload,
    };
    if (
      String(nextPayload.column_key ?? "").trim() === "done" &&
      !nextPayload.resolution
    ) {
      nextPayload.resolution = "done";
      const refs = String(manageResolutionRefs ?? "")
        .split(/\r?\n|,/)
        .map((item) => item.trim())
        .filter(Boolean);
      if (refs.length > 0) {
        nextPayload.resolution_refs = refs;
      }
    }
    await runBoardMutation(
      () => coreClient.moveBoardCard(boardId, cardId, nextPayload),
      successMessage,
    );
    mutatingCardId = "";
  }

  async function reorderCard(cardItem, cards, index, direction) {
    if (direction === "up" && index > 0) {
      await moveCard(
        cardItem,
        {
          column_key: cardItem.membership.column_key,
          before_card_ref: `card:${boardCardStableId(cards[index - 1].membership)}`,
        },
        "Card reordered.",
      );
    }

    if (direction === "down" && index < cards.length - 1) {
      await moveCard(
        cardItem,
        {
          column_key: cardItem.membership.column_key,
          after_card_ref: `card:${boardCardStableId(cards[index + 1].membership)}`,
        },
        "Card reordered.",
      );
    }
  }

  async function saveCardDetails(cardItem) {
    if (!workspace?.board) return;

    const cardId = boardCardStableId(cardItem.membership);
    mutatingCardId = cardId;
    await runBoardMutation(
      () =>
        coreClient.updateBoardCard(boardId, cardId, {
          if_board_updated_at: workspace.board.updated_at,
          patch: {
            title: manageTitle.trim(),
            summary: manageSummary.trim() || manageTitle.trim(),
            thread_id: manageThreadId.trim() || null,
            document_ref: manageDocumentId.trim()
              ? `document:${manageDocumentId.trim()}`
              : null,
            assignee_refs: [...manageAssignees],
            risk: manageRisk,
            resolution: manageResolution.trim() || null,
            resolution_refs: String(manageResolutionRefs ?? "")
              .split(/\r?\n|,/)
              .map((item) => item.trim())
              .filter(Boolean),
            related_refs: String(manageRelatedRefs ?? "")
              .split(/\r?\n|,/)
              .map((item) => item.trim())
              .filter(Boolean),
            due_at: manageDueAt.trim() || null,
            definition_of_done: String(manageDefinitionOfDone ?? "")
              .split(/\r?\n|,/)
              .map((item) => item.trim())
              .filter(Boolean),
          },
        }),
      "Card details updated.",
    );
    mutatingCardId = "";
  }

  async function removeCard(cardItem) {
    if (!workspace?.board) return;

    const cardId = boardCardStableId(cardItem.membership);
    mutatingCardId = cardId;
    await runBoardMutation(
      () =>
        coreClient.removeBoardCard(boardId, cardId, {
          if_board_updated_at: workspace.board.updated_at,
        }),
      "Card removed.",
      { closeCardManager: true },
    );
    mutatingCardId = "";
  }

  function statusColor(status) {
    if (status === "active") return "text-emerald-400 bg-emerald-500/10";
    if (status === "paused") return "text-amber-300 bg-amber-500/10";
    if (status === "closed") return "text-slate-300 bg-slate-500/10";
    return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
  }

  function getThreadStatus(thread) {
    if (!thread) return "unknown";
    if (thread.status === "done") return "done";
    if (thread.status === "canceled") return "canceled";
    if (thread.status === "paused") return "paused";
    if (thread.staleness === "stale") return "stale";
    if (thread.staleness === "very-stale") return "very-stale";
    return "active";
  }

  /** Visual status for the card row: thread staleness when linked, else artifact status. */
  function boardCardRowStatus(membership, thread) {
    const resolution = String(membership?.resolution ?? "").trim();
    if (resolution === "done" || resolution === "completed") return "done";
    if (resolution === "canceled" || resolution === "cancelled")
      return "canceled";
    if (resolution === "superseded") return "paused";
    if (thread) return getThreadStatus(thread);
    const s = String(membership?.status ?? "").trim();
    if (s === "done") return "done";
    if (s === "cancelled") return "canceled";
    return "active";
  }

  function boardCardHeaderTitle(membership, thread) {
    const cardTitle = String(membership?.title ?? "").trim();
    if (cardTitle) return cardTitle;
    const threadTitle = String(thread?.title ?? "").trim();
    if (threadTitle) return threadTitle;
    return boardCardStableId(membership);
  }

  function threadStatusColor(status) {
    switch (status) {
      case "done":
        return "text-emerald-400";
      case "canceled":
        return "text-[var(--ui-text-muted)]";
      case "paused":
        return "text-amber-400";
      case "stale":
        return "text-orange-400";
      case "very-stale":
        return "text-red-400";
      default:
        return "text-[var(--ui-text)]";
    }
  }

  function staleBadgeClass(stale) {
    return stale
      ? "text-red-300 bg-red-500/10"
      : "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
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

  async function handleArchiveBoard() {
    if (!boardId || boardLifecycleBusy || workspace?.board?.tombstoned_at)
      return;
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
    if (!boardId || boardLifecycleBusy || workspace?.board?.tombstoned_at)
      return;
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
    else if (action === "trash") handleTombstoneBoard();
  }

  async function handleTombstoneBoard() {
    if (!boardId || boardLifecycleBusy) return;
    boardLifecycleBusy = true;
    try {
      await coreClient.tombstoneBoard(boardId, {});
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

  {#if board.tombstoned_at}
    <div
      class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-[13px] text-red-400"
    >
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2 font-semibold">
          <span>⚠</span>
          <span>This board has been tombstoned</span>
        </div>
        {#if board.tombstone_reason}
          <p class="mt-2">Reason: {board.tombstone_reason}</p>
        {/if}
        <p class="mt-1 text-[11px] text-red-400/80">
          Tombstoned {#if board.tombstoned_by}by {actorName(
              board.tombstoned_by,
            )}{/if}
          {#if board.tombstoned_at}
            at {formatTimestamp(board.tombstoned_at)}
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

      {#if !board.tombstoned_at}
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
      {#if backingThread}
        <span class="text-[var(--ui-text-subtle)]">Topic</span>
        <a
          class="text-indigo-400 transition-colors hover:text-indigo-300"
          href={workspaceHref(
            `/topics/${encodeURIComponent(backingThread.id)}`,
          )}
        >
          {backingThread.title || backingThread.id}
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
            placeholder="Search documents by title, ID, or thread"
            searchFn={searchDocumentOptions}
          />

          <div
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[12px] text-[var(--ui-text-muted)]"
          >
            Backing thread is fixed after board creation.
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
            addInputPlaceholder="thread:thread-q2-initiative"
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
            advancedLabel="Use a manual backing thread ID"
            disabledIds={[backingThreadId].filter(Boolean)}
            helperText="Optional: link this card to an existing thread."
            label="Backing thread"
            manualLabel="Backing thread ID"
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
            placeholder="Search documents by title, ID, or thread"
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
              addInputPlaceholder="topic:thread-summer-menu"
              addButtonLabel="Add ref"
              emptyText="No related refs yet."
              helperText="Optional typed refs for topics, documents, artifacts, or boards."
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

  {#snippet renderCard(cardItem, index, cards)}
    {@const membership = cardItem.membership}
    {@const backing = cardItem.backing}
    {@const derived = cardItem.derived}
    {@const thread = backing?.thread}
    {@const linkedThreadId = boardCardLinkedThreadId(membership)}
    {@const hasResolvedThread = Boolean(thread)}
    {@const showThreadNav = hasResolvedThread && Boolean(linkedThreadId)}
    {@const cardRowId = boardCardStableId(membership)}
    {@const rowStatus = boardCardRowStatus(membership, thread)}
    {@const headerTitle = boardCardHeaderTitle(membership, thread)}
    {@const cardFreshness = derived?.freshness}
    {@const derivedCurrent = isFreshnessCurrent(cardFreshness)}
    {@const summary = derived?.summary}
    {@const cardResolution = String(membership?.resolution ?? "").trim()}
    {@const cardSummary = String(membership?.summary ?? "").trim()}
    {@const cardDueAt = String(membership?.due_at ?? "").trim()}
    {@const threadLinkRef =
      linkedThreadId.trim() !== "" ? `thread:${linkedThreadId}` : ""}
    {@const topicRef = String(membership?.topic_ref ?? "").trim()}
    {@const documentRef = String(membership?.document_ref ?? "").trim()}
    {@const assigneeRefs = Array.isArray(membership?.assignee_refs)
      ? membership.assignee_refs
      : []}
    {@const resolutionRefs = Array.isArray(membership?.resolution_refs)
      ? membership.resolution_refs
      : []}
    {@const relatedRefs = Array.isArray(membership?.related_refs)
      ? membership.related_refs
      : []}
    {@const doD = Array.isArray(membership?.definition_of_done)
      ? membership.definition_of_done
      : []}
    <div
      id={`card-${cardRowId}`}
      class="group rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] transition-colors hover:border-[var(--ui-border-strong)]"
    >
      <div class="px-2.5 py-2">
        <div class="flex items-start gap-2">
          <span
            aria-hidden="true"
            class="mt-[5px] h-2 w-2 shrink-0 rounded-full {threadStatusDotClass(
              rowStatus,
            )}"
          ></span>
          <div class="min-w-0 flex-1">
            {#if showThreadNav}
              <a
                class="block truncate text-[13px] font-medium leading-snug transition-colors hover:text-indigo-300 {threadStatusColor(
                  rowStatus,
                )}"
                href={workspaceHref(
                  `/topics/${encodeURIComponent(linkedThreadId)}`,
                )}
              >
                {headerTitle}
              </a>
            {:else}
              <span
                class="block truncate text-[13px] font-medium leading-snug {threadStatusColor(
                  rowStatus,
                )}"
              >
                {headerTitle}
              </span>
            {/if}
            <div class="mt-1 flex flex-wrap items-center gap-1">
              <span
                class="rounded px-1 py-0.5 text-[11px] font-medium {cardResolutionTone(
                  cardResolution,
                )}"
              >
                {cardResolutionLabel(cardResolution)}
              </span>
              {#if cardDueAt}
                <span
                  class="rounded bg-[var(--ui-border)] px-1 py-0.5 text-[11px] text-[var(--ui-text-muted)]"
                >
                  Due {formatTimestamp(cardDueAt) || "—"}
                </span>
              {/if}
              {#if cardSummary}
                <span class="truncate text-[11px] text-[var(--ui-text-muted)]">
                  {cardSummary}
                </span>
              {/if}
            </div>
          </div>
        </div>

        <div class="mt-1 flex flex-wrap items-center gap-1 pl-4">
          {#if String(membership?.status ?? "").trim()}
            <span
              class="rounded bg-[var(--ui-border)] px-1 py-0.5 text-[10px] font-medium uppercase tracking-wide text-[var(--ui-text-muted)]"
            >
              {String(membership.status).trim()}
            </span>
          {/if}
          {#if String(membership?.risk ?? "").trim()}
            <span
              class="rounded bg-[var(--ui-border)] px-1 py-0.5 text-[10px] font-medium text-[var(--ui-text-muted)]"
            >
              {String(membership.risk).trim()}
            </span>
          {/if}
          {#if cardFreshness}
            <span
              class="rounded px-1 py-0.5 text-[11px] {freshnessStatusTone(
                cardFreshness.status,
              )}"
            >
              {freshnessStatusLabel(cardFreshness.status)}
            </span>
          {/if}
          {#if derivedCurrent}
            {#if (summary?.related_topic_count ?? 0) > 0}
              <span
                class="rounded bg-[var(--ui-border)] px-1 py-0.5 text-[11px] text-[var(--ui-text-muted)]"
              >
                {summary.related_topic_count} related {summary.related_topic_count ===
                1
                  ? "topic"
                  : "topics"}
              </span>
            {/if}
            {#if (summary?.document_count ?? 0) > 0}
              <span
                class="rounded bg-indigo-500/10 px-1 py-0.5 text-[11px] text-indigo-300"
              >
                {summary.document_count} doc {summary.document_count === 1
                  ? "ref"
                  : "refs"}
              </span>
            {/if}
            {#if (summary?.inbox_count ?? 0) > 0}
              <span
                class="rounded bg-amber-500/10 px-1 py-0.5 text-[11px] text-amber-400"
              >
                {summary.inbox_count} inbox
              </span>
            {/if}
            <span
              class="rounded px-1 py-0.5 text-[11px] {staleBadgeClass(
                Boolean(summary?.stale),
              )}"
            >
              {summary?.stale ? "Topic stale" : "Fresh check-in"}
            </span>
          {/if}
        </div>

        <div
          class="mt-1.5 flex flex-wrap gap-1 pl-4 text-[11px] text-[var(--ui-text-muted)]"
        >
          {#if threadLinkRef}
            <RefLink
              refValue={threadLinkRef}
              threadId={linkedThreadId}
              {boardId}
              showRaw
            />
          {/if}
          {#if topicRef}
            <RefLink
              refValue={topicRef}
              threadId={linkedThreadId}
              {boardId}
              showRaw
            />
          {/if}
          {#if documentRef}
            <RefLink refValue={documentRef} {boardId} showRaw />
          {/if}
        </div>

        {#if assigneeRefs.length > 0}
          <div
            class="mt-1.5 flex flex-wrap gap-1 pl-4 text-[11px] text-[var(--ui-text-subtle)]"
          >
            {#each assigneeRefs as assigneeRef}
              <span class="rounded bg-[var(--ui-border)] px-1.5 py-0.5">
                {actorName(String(assigneeRef).replace(/^actor:/, ""))}
              </span>
            {/each}
          </div>
        {/if}

        {#if resolutionRefs.length > 0}
          <div class="mt-1.5 pl-4 text-[11px] text-[var(--ui-text-muted)]">
            <div class="text-[var(--ui-text-subtle)]">Resolution evidence</div>
            <div class="mt-1 flex flex-wrap gap-1">
              {#each resolutionRefs as refValue}
                <RefLink {refValue} {boardId} showRaw />
              {/each}
            </div>
          </div>
        {/if}

        {#if relatedRefs.length > 0}
          <div class="mt-1.5 pl-4 text-[11px] text-[var(--ui-text-muted)]">
            <div class="text-[var(--ui-text-subtle)]">Related refs</div>
            <div class="mt-1 flex flex-wrap gap-1">
              {#each relatedRefs as refValue}
                <RefLink {refValue} {boardId} showRaw />
              {/each}
            </div>
          </div>
        {/if}

        {#if doD.length > 0}
          <div class="mt-1.5 pl-4 text-[11px] text-[var(--ui-text-muted)]">
            <div class="text-[var(--ui-text-subtle)]">Definition of done</div>
            <ul class="mt-1 list-disc space-y-0.5 pl-4">
              {#each doD as item}
                <li>{item}</li>
              {/each}
            </ul>
          </div>
        {/if}

        <div class="mt-2 flex justify-end">
          <button
            aria-label={`Manage ${headerTitle}`}
            class="rounded px-1.5 py-1.5 text-[11px] text-[var(--ui-text-subtle)] transition-colors hover:bg-[var(--ui-border)] hover:text-[var(--ui-text-muted)]"
            onclick={() => openCardManager(cardItem)}
            type="button"
          >
            {expandedCardId === cardRowId ? "Close" : "Actions"}
          </button>
        </div>
      </div>

      {#if expandedCardId === cardRowId}
        <div
          class="border-t border-[var(--ui-border)] bg-[var(--ui-panel-muted)]"
        >
          <div class="grid gap-3 px-2.5 py-2 md:grid-cols-2">
            <label class="text-[11px] text-[var(--ui-text-muted)]">
              Card title
              <input
                bind:value={manageTitle}
                class="mt-0.5 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2 py-1 text-[12px] text-[var(--ui-text)]"
                type="text"
              />
            </label>
            <label class="text-[11px] text-[var(--ui-text-muted)]">
              Risk
              <select
                bind:value={manageRisk}
                class="mt-0.5 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2 py-1 text-[12px] text-[var(--ui-text)]"
              >
                <option value="low">Low</option>
                <option value="medium">Medium</option>
                <option value="high">High</option>
                <option value="critical">Critical</option>
              </select>
            </label>

            <label
              class="text-[11px] text-[var(--ui-text-muted)] md:col-span-2"
            >
              Summary
              <textarea
                bind:value={manageSummary}
                class="mt-0.5 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2 py-1 text-[12px] text-[var(--ui-text)]"
                rows="3"
              ></textarea>
            </label>

            <label class="text-[11px] text-[var(--ui-text-muted)]">
              Resolution
              <select
                bind:value={manageResolution}
                class="mt-0.5 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2 py-1 text-[12px] text-[var(--ui-text)]"
              >
                <option value="">Open</option>
                <option value="done">Done</option>
                <option value="canceled">Canceled</option>
              </select>
            </label>

            <label class="text-[11px] text-[var(--ui-text-muted)]">
              Due date
              <input
                bind:value={manageDueAt}
                class="mt-0.5 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2 py-1 text-[12px] text-[var(--ui-text)]"
                type="datetime-local"
              />
            </label>

            <SearchableEntityPicker
              bind:value={manageDocumentId}
              advancedLabel="Use a manual document ID"
              helperText="Optional canonical document ref for this card."
              label="Document"
              manualLabel="Document ID"
              manualPlaceholder="incident-response-playbook"
              placeholder="Search documents by title, ID, or thread"
              searchFn={searchDocumentOptions}
            />

            <SearchableMultiEntityPicker
              bind:values={manageAssignees}
              advancedLabel="Add a manual assignee ID"
              helperText="Assignee refs stay visible on the card detail panel."
              items={actorOptions}
              label="Assignees"
              manualLabel="Assignee ID"
              manualPlaceholder="actor-ops-ai"
              placeholder="Search actors by name, ID, or tags"
            />

            <label
              class="text-[11px] text-[var(--ui-text-muted)] md:col-span-2"
            >
              Definition of done
              <textarea
                bind:value={manageDefinitionOfDone}
                class="mt-0.5 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2 py-1 text-[12px] text-[var(--ui-text)]"
                rows="3"
              ></textarea>
            </label>

            <div class="md:col-span-2">
              <p class="text-[11px] text-[var(--ui-text-muted)]">
                Related refs
              </p>
              <GuidedTypedRefsInput
                bind:value={manageRelatedRefs}
                {boardId}
                addInputLabel="Add related ref"
                addInputPlaceholder="topic:thread-summer-menu"
                addButtonLabel="Add ref"
                emptyText="No related refs yet."
                helperText="Use typed refs to connect the card to topics, documents, artifacts, or boards."
                textareaAriaLabel="Card related refs"
              />
            </div>

            <div class="md:col-span-2">
              <p class="text-[11px] text-[var(--ui-text-muted)]">
                Resolution evidence
              </p>
              <GuidedTypedRefsInput
                bind:value={manageResolutionRefs}
                {boardId}
                addInputLabel="Add resolution ref"
                addInputPlaceholder="artifact:receipt-123"
                addButtonLabel="Add ref"
                emptyText="No resolution evidence yet."
                helperText="Use typed refs that evidence the closeout state."
                textareaAriaLabel="Card resolution refs"
              />
            </div>
          </div>

          <div class="space-y-2 px-2.5 py-2">
            <div class="flex items-end gap-1.5">
              <label
                class="min-w-0 flex-1 text-[11px] text-[var(--ui-text-muted)]"
              >
                Move to column
                <select
                  bind:value={manageMoveColumnKey}
                  class="mt-0.5 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2 py-1 text-[12px] text-[var(--ui-text)]"
                >
                  {#each board.column_schema as moveColumn}
                    <option value={moveColumn.key}>
                      {moveColumn.title ||
                        boardColumnTitle(moveColumn.key, board.column_schema)}
                    </option>
                  {/each}
                </select>
              </label>
              <button
                class="rounded bg-indigo-600 px-2.5 py-1.5 text-[11px] font-medium text-white transition-colors hover:bg-indigo-500 disabled:opacity-40"
                disabled={mutatingCardId === cardRowId}
                onclick={() =>
                  moveCard(
                    cardItem,
                    { column_key: manageMoveColumnKey },
                    "Card moved.",
                  )}
                type="button"
              >
                Move
              </button>
            </div>

            <div class="flex flex-wrap items-center gap-2">
              <button
                class="rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2.5 py-1.5 text-[11px] text-[var(--ui-text-muted)] transition-colors hover:text-[var(--ui-text)] disabled:opacity-40"
                disabled={mutatingCardId === cardRowId}
                onclick={() => saveCardDetails(cardItem)}
                type="button"
              >
                Save card details
              </button>
              <button
                class="rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2.5 py-1.5 text-[11px] text-[var(--ui-text-muted)] transition-colors hover:text-[var(--ui-text)] disabled:opacity-40"
                disabled={index === 0 || mutatingCardId === cardRowId}
                onclick={() => reorderCard(cardItem, cards, index, "up")}
                type="button"
              >
                Move up
              </button>
              <button
                class="rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2.5 py-1.5 text-[11px] text-[var(--ui-text-muted)] transition-colors hover:text-[var(--ui-text)] disabled:opacity-40"
                disabled={index === cards.length - 1 ||
                  mutatingCardId === cardRowId}
                onclick={() => reorderCard(cardItem, cards, index, "down")}
                type="button"
              >
                Move down
              </button>
              <div class="flex-1"></div>
              <button
                class="rounded border border-red-500/20 bg-red-500/10 px-2 py-1.5 text-[11px] text-red-400 transition-colors hover:bg-red-500/15 disabled:opacity-40"
                disabled={mutatingCardId === cardRowId}
                onclick={() => removeCard(cardItem)}
                type="button"
              >
                Remove card
              </button>
            </div>
          </div>
        </div>
      {/if}
    </div>
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
            {#each backlogCards as cardItem, index}
              {@render renderCard(cardItem, index, backlogCards)}
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
              class="min-w-[1.25rem] rounded-full bg-[var(--ui-border)] px-1.5 py-0.5 text-center text-[11px] text-[var(--ui-text-subtle)]"
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
              {#each cards as cardItem, index}
                {@render renderCard(cardItem, index, cards)}
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
            {#each doneCards as cardItem, index}
              {@render renderCard(cardItem, index, doneCards)}
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
          Workspace documents
        </h2>
        <p class="mt-1 text-[11px] text-[var(--ui-text-subtle)]">
          Canonical doc lineages linked from the board's primary thread and
          cards.
        </p>
      </div>
      <div class="px-4 py-3">
        {#if (workspace.documents?.items ?? []).length === 0}
          <p class="text-[12px] text-[var(--ui-text-subtle)]">
            No linked doc lineages yet.
          </p>
        {:else}
          <div class="space-y-2">
            {#each workspace.documents.items.slice(0, 6) as document}
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
            {#each resolvedCards.slice(0, 6) as cardItem}
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
          Derived risk and decision signals for the board's canonical thread
          set.
        </p>
      </div>
      <div class="px-4 py-3">
        {#if enrichedInboxItems.length === 0}
          <p class="text-[12px] text-[var(--ui-text-subtle)]">
            No active derived inbox items.
          </p>
        {:else}
          <div class="space-y-2">
            {#each enrichedInboxItems.slice(0, 6) as item}
              <div
                class="rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2"
              >
                <div class="text-[12px] font-medium text-[var(--ui-text)]">
                  {item.title || item.summary || item.id}
                </div>
                <div class="mt-1 text-[11px] text-[var(--ui-text-subtle)]">
                  {item.urgency_label} · Topic {item.thread_id}
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </section>
  </div>

  {#if boardWarnings.length > 0}
    <section
      class="mt-4 rounded-md border border-amber-500/20 bg-amber-500/10 px-4 py-3"
    >
      <h2 class="text-[13px] font-medium text-amber-100">Warnings</h2>
      <div class="mt-2 space-y-1.5">
        {#each boardWarnings as warning}
          <div class="text-[12px] text-amber-100">
            {warning.message || "Workspace warning"}
            {#if warning.thread_id}
              <a
                class="ml-1 font-medium text-amber-200 underline transition-colors hover:text-amber-100"
                href={workspaceHref(
                  `/topics/${encodeURIComponent(warning.thread_id)}`,
                )}
              >
                {warning.thread_id}
              </a>
            {/if}
          </div>
        {/each}
      </div>
    </section>
  {/if}
{/if}

<ConfirmModal
  open={confirmModal.open}
  title={confirmModal.action === "trash" ? "Move to trash" : "Archive board"}
  message={confirmModal.action === "trash"
    ? "This board will be tombstoned. You can restore it from trash later."
    : "This board will be hidden from default views. You can unarchive it later."}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={boardLifecycleBusy}
  onconfirm={handleConfirm}
  oncancel={() => (confirmModal = { open: false, action: "" })}
/>
