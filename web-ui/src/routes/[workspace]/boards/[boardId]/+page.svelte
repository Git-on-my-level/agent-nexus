<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
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
    searchDocuments as searchDocumentRecords,
    searchThreads as searchThreadRecords,
  } from "$lib/searchHelpers";
  import { workspacePath } from "$lib/workspacePaths";
  import { enrichInboxItem } from "$lib/inboxUtils";
  import {
    BOARD_STATUS_LABELS,
    boardColumnTitle,
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
  let boardPrimaryDocumentId = $state("");
  let boardLabels = $state("");
  let boardOwners = $state([]);
  let boardPinnedRefs = $state("");

  let addCardThreadId = $state("");
  let addCardColumnKey = $state("backlog");
  let addCardPinnedDocumentId = $state("");

  let manageMoveColumnKey = $state("backlog");
  let managePinnedDocumentId = $state("");

  let workspaceSlug = $derived($page.params.workspace);
  let boardId = $derived($page.params.boardId);
  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );
  let enrichedInboxItems = $derived(
    (workspace?.inbox?.items ?? []).map((item) => enrichInboxItem(item)),
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
        document.thread_id && `Thread ${document.thread_id}`,
      ]
        .filter(Boolean)
        .join(" · "),
      keywords: document.labels ?? [],
    };
  }

  async function searchThreadOptions(query) {
    const threads = await searchThreadRecords(query);
    return threads.map(toThreadOption);
  }

  async function searchDocumentOptions(query) {
    const documents = await searchDocumentRecords(query);
    return documents.map(toDocumentOption);
  }

  function syncBoardDrafts(board) {
    boardTitle = board?.title ?? "";
    boardStatus = board?.status ?? "active";
    boardPrimaryDocumentId = board?.primary_document_id ?? "";
    boardLabels = joinDelimitedValues(board?.labels ?? []);
    boardOwners = [...(board?.owners ?? [])];
    boardPinnedRefs = joinDelimitedValues(board?.pinned_refs ?? []);
  }

  function openBoardEditForm() {
    if (!workspace?.board) return;
    syncBoardDrafts(workspace.board);
    mutationError = "";
    showBoardEditForm = !showBoardEditForm;
  }

  function openAddCardForm() {
    addCardThreadId = "";
    addCardColumnKey = "backlog";
    addCardPinnedDocumentId = "";
    mutationError = "";
    showAddCardForm = !showAddCardForm;
  }

  function openCardManager(cardItem) {
    const threadId = cardItem.membership.thread_id;
    if (expandedCardId === threadId) {
      expandedCardId = "";
      return;
    }

    expandedCardId = threadId;
    manageMoveColumnKey = cardItem.membership.column_key;
    managePinnedDocumentId = cardItem.membership.pinned_document_id ?? "";
    mutationError = "";
  }

  function cardCommitments(threadId) {
    return (workspace?.commitments?.items ?? []).filter(
      (c) => String(c.thread_id ?? "") === String(threadId),
    );
  }

  function cardDocuments(threadId) {
    return (workspace?.documents?.items ?? []).filter(
      (d) => String(d.thread_id ?? "") === String(threadId),
    );
  }

  function cardInboxItems(threadId) {
    return enrichedInboxItems.filter(
      (item) => String(item.thread_id ?? "") === String(threadId),
    );
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

    const patch = {
      title,
      status: boardStatus,
      primary_document_id: boardPrimaryDocumentId.trim() || null,
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

    const threadIdValue = addCardThreadId.trim();
    if (!threadIdValue) {
      mutationError = "Pick a thread to add as a card.";
      return;
    }

    addingCard = true;
    await runBoardMutation(
      () =>
        coreClient.addBoardCard(boardId, {
          if_board_updated_at: workspace.board.updated_at,
          thread_id: threadIdValue,
          column_key: addCardColumnKey,
          ...(addCardPinnedDocumentId.trim()
            ? { pinned_document_id: addCardPinnedDocumentId.trim() }
            : {}),
        }),
      "Card added.",
      { closeAddCard: true },
    );
    addingCard = false;
  }

  async function moveCard(cardItem, payload, successMessage) {
    if (!workspace?.board) return;

    const cardId = cardItem.membership.thread_id;
    mutatingCardId = cardId;
    await runBoardMutation(
      () =>
        coreClient.moveBoardCard(boardId, cardId, {
          if_board_updated_at: workspace.board.updated_at,
          ...payload,
        }),
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
          before_thread_id: cards[index - 1].membership.thread_id,
        },
        "Card reordered.",
      );
    }

    if (direction === "down" && index < cards.length - 1) {
      await moveCard(
        cardItem,
        {
          column_key: cardItem.membership.column_key,
          after_thread_id: cards[index + 1].membership.thread_id,
        },
        "Card reordered.",
      );
    }
  }

  async function saveCardPinnedDocument(cardItem) {
    if (!workspace?.board) return;

    const cardId = cardItem.membership.thread_id;
    mutatingCardId = cardId;
    await runBoardMutation(
      () =>
        coreClient.updateBoardCard(boardId, cardId, {
          if_board_updated_at: workspace.board.updated_at,
          patch: {
            pinned_document_id: managePinnedDocumentId.trim() || null,
          },
        }),
      "Card metadata updated.",
    );
    mutatingCardId = "";
  }

  async function removeCard(cardItem) {
    if (!workspace?.board) return;

    const cardId = cardItem.membership.thread_id;
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
  {@const primaryThread = workspace.primary_thread}
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
      {#if primaryThread}
        <span class="text-[var(--ui-text-subtle)]">Thread</span>
        <a
          class="text-indigo-400 transition-colors hover:text-indigo-300"
          href={workspaceHref(
            `/threads/${encodeURIComponent(primaryThread.id)}`,
          )}
        >
          {primaryThread.title || primaryThread.id}
        </a>
        <span class="text-[var(--ui-text-subtle)]">·</span>
      {/if}
      <span>
        {workspace.board_summary?.card_count ?? workspace.cards?.count ?? 0}
        canonical cards
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
            bind:value={boardPrimaryDocumentId}
            advancedLabel="Use a manual primary document ID"
            helperText="Optional: foreground the canonical doc lineage most operators should inspect first."
            label="Primary document"
            manualLabel="Primary document ID"
            manualPlaceholder="incident-response-playbook"
            placeholder="Search documents by title, ID, or thread"
            searchFn={searchDocumentOptions}
          />

          <div
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[12px] text-[var(--ui-text-muted)]"
          >
            Primary thread is fixed in v1.
            <div class="mt-1 text-[var(--ui-text)]">
              {primaryThread?.title || board.primary_thread_id}
            </div>
            <div class="mt-1 text-[11px] text-[var(--ui-text-subtle)]">
              {board.primary_thread_id}
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
        <h2 class="text-[13px] font-medium text-[var(--ui-text)]">
          Add existing thread as a card
        </h2>
      </div>

      <div class="space-y-3 px-4 py-3">
        <div class="grid gap-3 md:grid-cols-3">
          <SearchableEntityPicker
            bind:value={addCardThreadId}
            advancedLabel="Use a manual card thread ID"
            disabledIds={[board.primary_thread_id]}
            helperText="Pick an existing canonical thread. The board primary thread cannot also be a card."
            label="Card thread"
            manualLabel="Card thread ID"
            manualPlaceholder="thread-onboarding"
            placeholder="Search threads by title, ID, or tags"
            searchFn={searchThreadOptions}
          />

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

          <SearchableEntityPicker
            bind:value={addCardPinnedDocumentId}
            advancedLabel="Use a manual pinned document ID"
            helperText="Optional: surface one canonical doc lineage directly on the card."
            label="Pinned document"
            manualLabel="Pinned document ID"
            manualPlaceholder="onboarding-guide-v1"
            placeholder="Search documents by title, ID, or thread"
            searchFn={searchDocumentOptions}
          />
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
    {@const threadId = membership.thread_id}
    {@const threadStatus = getThreadStatus(thread)}
    {@const cardFreshness = derived?.freshness}
    {@const derivedCurrent = isFreshnessCurrent(cardFreshness)}
    {@const summary = derived?.summary}
    <div
      class="group rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] transition-colors hover:border-[var(--ui-border-strong)]"
    >
      <div class="px-2.5 py-2">
        <div class="flex items-start gap-2">
          <span
            class="mt-[5px] h-2 w-2 shrink-0 rounded-full {threadStatusDotClass(
              threadStatus,
            )}"
          ></span>
          <a
            class="block min-w-0 flex-1 text-[13px] font-medium leading-snug transition-colors hover:text-indigo-300 {threadStatusColor(
              threadStatus,
            )}"
            href={workspaceHref(`/threads/${encodeURIComponent(threadId)}`)}
          >
            {thread?.title || threadId}
          </a>
        </div>

        <div class="mt-1 pl-4 text-[11px] text-[var(--ui-text-muted)]">
          {thread?.status ?? "—"} · {thread?.priority ?? "—"} · Added {formatTimestamp(
            membership.created_at,
          )}
        </div>

        <div class="mt-1.5 flex flex-wrap gap-1 pl-4">
          <span
            class="rounded px-1 py-0.5 text-[11px] {freshnessStatusTone(
              cardFreshness?.status,
            )}"
          >
            {freshnessStatusLabel(cardFreshness?.status)}
          </span>
          {#if derivedCurrent}
            {#if (summary?.open_commitment_count ?? 0) > 0}
              <span
                class="rounded bg-[var(--ui-border)] px-1 py-0.5 text-[11px] text-[var(--ui-text-muted)]"
              >
                {summary.open_commitment_count} commit.
              </span>
            {/if}
            {#if (summary?.inbox_count ?? 0) > 0}
              <span
                class="rounded bg-amber-500/10 px-1 py-0.5 text-[11px] text-amber-400"
              >
                {summary.inbox_count} inbox
              </span>
            {/if}
            {#if (summary?.decision_request_count ?? 0) > 0}
              <span
                class="rounded bg-indigo-500/10 px-1 py-0.5 text-[11px] text-indigo-400"
              >
                {summary.decision_request_count} decisions
              </span>
            {/if}
            <span
              class="rounded px-1 py-0.5 text-[11px] {staleBadgeClass(
                Boolean(summary?.stale),
              )}"
            >
              {summary?.stale ? "Thread stale" : "Fresh check-in"}
            </span>
          {:else}
            <span
              class="rounded bg-[var(--ui-border)] px-1 py-0.5 text-[11px] text-[var(--ui-text-subtle)]"
            >
              Derived counts hidden until refresh completes
            </span>
          {/if}
        </div>

        {#if backing?.pinned_document}
          <div class="mt-1.5 pl-4">
            <a
              aria-label={`Pinned doc ${backing.pinned_document.title || backing.pinned_document.id}`}
              class="inline-block rounded bg-indigo-500/10 px-1.5 py-0.5 text-[11px] text-indigo-300 transition-colors hover:text-indigo-200"
              href={workspaceHref(
                `/docs/${encodeURIComponent(backing.pinned_document.id)}`,
              )}
            >
              {backing.pinned_document.title || backing.pinned_document.id}
            </a>
          </div>
        {/if}

        <div class="mt-2 flex justify-end">
          <button
            aria-label={`Manage ${thread?.title || threadId}`}
            class="rounded px-1.5 py-1.5 text-[11px] text-[var(--ui-text-subtle)] transition-colors hover:bg-[var(--ui-border)] hover:text-[var(--ui-text-muted)]"
            onclick={() => openCardManager(cardItem)}
            type="button"
          >
            {expandedCardId === threadId ? "Close" : "Actions"}
          </button>
        </div>
      </div>

      {#if expandedCardId === threadId}
        {@const thisCommitments = cardCommitments(threadId)}
        {@const thisInbox = cardInboxItems(threadId)}
        {@const thisDocs = cardDocuments(threadId)}
        <div
          class="border-t border-[var(--ui-border)] bg-[var(--ui-panel-muted)]"
        >
          {#if thisCommitments.length > 0}
            <div class="border-b border-[var(--ui-border)] px-2.5 py-1.5">
              <p
                class="text-[11px] font-semibold uppercase tracking-wide text-[var(--ui-text-subtle)]"
              >
                Commitments
              </p>
              {#each thisCommitments as c}
                <div class="mt-1 text-[11px]">
                  <span class="text-[var(--ui-text)]">
                    {c.title || ""}{#if !c.title}<span
                        class="font-mono text-[var(--ui-text-subtle)]"
                        >{c.id}</span
                      >{/if}
                  </span>
                  <span class="text-[var(--ui-text-subtle)]">
                    · {c.status ?? "—"} · Due {formatTimestamp(c.due_at) || "—"}
                  </span>
                </div>
              {/each}
            </div>
          {/if}

          {#if thisInbox.length > 0}
            <div class="border-b border-[var(--ui-border)] px-2.5 py-1.5">
              <p
                class="text-[11px] font-semibold uppercase tracking-wide text-[var(--ui-text-subtle)]"
              >
                Inbox
              </p>
              {#each thisInbox as item}
                <div class="mt-1 text-[11px]">
                  <span class="text-amber-400">{item.urgency_label}</span>
                  <span class="text-[var(--ui-text)]">
                    {item.title || item.summary || item.id}
                  </span>
                </div>
              {/each}
            </div>
          {/if}

          {#if thisDocs.length > 0}
            <div class="border-b border-[var(--ui-border)] px-2.5 py-1.5">
              <p
                class="text-[11px] font-semibold uppercase tracking-wide text-[var(--ui-text-subtle)]"
              >
                Documents
              </p>
              {#each thisDocs as doc}
                <div class="mt-1">
                  <a
                    class="text-[11px] text-indigo-300 transition-colors hover:text-indigo-200"
                    href={workspaceHref(`/docs/${encodeURIComponent(doc.id)}`)}
                  >
                    {doc.title || doc.id}
                  </a>
                </div>
              {/each}
            </div>
          {/if}

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
                disabled={mutatingCardId === threadId}
                onclick={() =>
                  moveCard(
                    cardItem,
                    { column_key: manageMoveColumnKey },
                    "Card moved.",
                  )}
                type="button"
              >
                Move to column
              </button>
            </div>

            <SearchableEntityPicker
              bind:value={managePinnedDocumentId}
              advancedLabel="Use a manual pinned document ID"
              helperText="Update the canonical pinned doc lineage for this card."
              label="Pinned document"
              manualLabel="Pinned document ID"
              manualPlaceholder="doc-lineage-id"
              placeholder="Search documents by title, ID, or thread"
              searchFn={searchDocumentOptions}
            />
            <div class="flex justify-end">
              <button
                class="rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2.5 py-1.5 text-[11px] text-[var(--ui-text-muted)] transition-colors hover:text-[var(--ui-text)] disabled:opacity-40"
                disabled={mutatingCardId === threadId}
                onclick={() => saveCardPinnedDocument(cardItem)}
                type="button"
              >
                Save pinned doc
              </button>
            </div>

            <div class="flex items-center gap-1">
              <button
                class="rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2 py-1.5 text-[11px] text-[var(--ui-text-muted)] transition-colors hover:text-[var(--ui-text)] disabled:opacity-40"
                disabled={index === 0 || mutatingCardId === threadId}
                onclick={() => reorderCard(cardItem, cards, index, "up")}
                type="button"
              >
                Move up
              </button>
              <button
                class="rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2 py-1.5 text-[11px] text-[var(--ui-text-muted)] transition-colors hover:text-[var(--ui-text)] disabled:opacity-40"
                disabled={index === cards.length - 1 ||
                  mutatingCardId === threadId}
                onclick={() => reorderCard(cardItem, cards, index, "down")}
                type="button"
              >
                Move down
              </button>
              <div class="flex-1"></div>
              <button
                class="rounded border border-red-500/20 bg-red-500/10 px-2 py-1.5 text-[11px] text-red-400 transition-colors hover:bg-red-500/15 disabled:opacity-40"
                disabled={mutatingCardId === threadId}
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
          Commitments
        </h2>
        <p class="mt-1 text-[11px] text-[var(--ui-text-subtle)]">
          Derived obligation scan across the board's canonical thread set.
        </p>
      </div>
      <div class="px-4 py-3">
        {#if (workspace.commitments?.items ?? []).length === 0}
          <p class="text-[12px] text-[var(--ui-text-subtle)]">
            No open commitments in this board slice.
          </p>
        {:else}
          <div class="space-y-2">
            {#each workspace.commitments.items.slice(0, 6) as commitment}
              <div
                class="rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2"
              >
                <div class="text-[12px] font-medium text-[var(--ui-text)]">
                  {commitment.title || commitment.id}
                </div>
                <div class="mt-1 text-[11px] text-[var(--ui-text-subtle)]">
                  {actorName(commitment.owner)} · {commitment.status ?? "—"} · Due
                  {formatTimestamp(commitment.due_at) || "—"}
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
                  {item.urgency_label} · Thread {item.thread_id}
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
                  `/threads/${encodeURIComponent(warning.thread_id)}`,
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
