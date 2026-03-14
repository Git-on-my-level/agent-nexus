<script>
  import { page } from "$app/stores";
  import GuidedTypedRefsInput from "$lib/components/GuidedTypedRefsInput.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { projectPath } from "$lib/projectPaths";
  import { lookupActorDisplayName, actorRegistry } from "$lib/actorSession";
  import { enrichInboxItem } from "$lib/inboxUtils";
  import {
    BOARD_STATUS_LABELS,
    boardColumnTitle,
    groupBoardWorkspaceCards,
    joinDelimitedValues,
    parseDelimitedValues,
  } from "$lib/boardUtils";

  let workspace = $state(null);
  let loading = $state(false);
  let error = $state("");
  let supportError = $state("");
  let mutationNotice = $state("");
  let mutationError = $state("");
  let conflictWarning = $state("");
  let availableThreads = $state([]);
  let availableDocuments = $state([]);

  let showBoardEditForm = $state(false);
  let showAddCardForm = $state(false);
  let updatingBoard = $state(false);
  let addingCard = $state(false);
  let expandedCardId = $state("");
  let mutatingCardId = $state("");

  let boardTitle = $state("");
  let boardStatus = $state("active");
  let boardPrimaryDocumentId = $state("");
  let boardLabels = $state("");
  let boardOwners = $state("");
  let boardPinnedRefs = $state("");

  let addCardThreadId = $state("");
  let addCardColumnKey = $state("backlog");
  let addCardPinnedDocumentId = $state("");

  let manageMoveColumnKey = $state("backlog");
  let managePinnedDocumentId = $state("");

  let projectSlug = $derived($page.params.project);
  let boardId = $derived($page.params.boardId);
  let actorName = $derived((id) => lookupActorDisplayName(id, $actorRegistry));
  let enrichedInboxItems = $derived(
    (workspace?.inbox?.items ?? []).map((item) => enrichInboxItem(item)),
  );

  function projectHref(pathname = "/") {
    return projectPath(projectSlug, pathname);
  }

  function syncBoardDrafts(board) {
    boardTitle = board?.title ?? "";
    boardStatus = board?.status ?? "active";
    boardPrimaryDocumentId = board?.primary_document_id ?? "";
    boardLabels = joinDelimitedValues(board?.labels ?? []);
    boardOwners = joinDelimitedValues(board?.owners ?? []);
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
    const threadId = cardItem.card.thread_id;
    if (expandedCardId === threadId) {
      expandedCardId = "";
      return;
    }

    expandedCardId = threadId;
    manageMoveColumnKey = cardItem.card.column_key;
    managePinnedDocumentId = cardItem.card.pinned_document_id ?? "";
    mutationError = "";
  }

  function threadHint(threadId) {
    const thread = availableThreads.find((item) => item.id === threadId);
    return thread?.title ?? "";
  }

  function documentHint(documentId) {
    const document = availableDocuments.find((item) => item.id === documentId);
    return document?.title ?? "";
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

  async function loadSupportData() {
    supportError = "";
    try {
      const [threadData, documentData] = await Promise.all([
        coreClient.listThreads({}),
        coreClient.listDocuments({}),
      ]);
      availableThreads = threadData.threads ?? [];
      availableDocuments = documentData.documents ?? [];
    } catch (e) {
      supportError = `Failed to load board controls: ${e instanceof Error ? e.message : String(e)}`;
      availableThreads = [];
      availableDocuments = [];
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
      owners: parseDelimitedValues(boardOwners),
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
      mutationError = "Thread ID is required to add a card.";
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

    const cardId = cardItem.card.thread_id;
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
          column_key: cardItem.card.column_key,
          before_thread_id: cards[index - 1].card.thread_id,
        },
        "Card reordered.",
      );
    }

    if (direction === "down" && index < cards.length - 1) {
      await moveCard(
        cardItem,
        {
          column_key: cardItem.card.column_key,
          after_thread_id: cards[index + 1].card.thread_id,
        },
        "Card reordered.",
      );
    }
  }

  async function saveCardPinnedDocument(cardItem) {
    if (!workspace?.board) return;

    const cardId = cardItem.card.thread_id;
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

    const cardId = cardItem.card.thread_id;
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

  $effect(() => {
    if (projectSlug && boardId) {
      void loadWorkspace();
      void loadSupportData();
    }
  });
</script>

<datalist id="board-detail-thread-options">
  {#each availableThreads as thread}
    <option value={thread.id}>{thread.title}</option>
  {/each}
</datalist>

<datalist id="board-detail-document-options">
  {#each availableDocuments as document}
    <option value={document.id}>{document.title}</option>
  {/each}
</datalist>

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

  <!-- Board header — minimal: title, status, actions, one context line -->
  <div class="mb-3">
    <div class="flex items-center gap-2 text-[12px]">
      <a
        class="text-[var(--ui-text-muted)] transition-colors hover:text-[var(--ui-text)]"
        href={projectHref("/boards")}
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
      </div>

      <div class="flex shrink-0 gap-2">
        <button
          class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2.5 py-1 text-[12px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] hover:text-[var(--ui-text)]"
          onclick={openBoardEditForm}
          type="button"
        >
          {showBoardEditForm ? "Close" : "Edit"}
        </button>
        <button
          class="rounded-md bg-indigo-600 px-2.5 py-1 text-[12px] font-medium text-white transition-colors hover:bg-indigo-500"
          onclick={openAddCardForm}
          type="button"
        >
          {showAddCardForm ? "Close" : "+ Card"}
        </button>
      </div>
    </div>

    <!-- Single context line -->
    <div
      class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] text-[var(--ui-text-muted)]"
    >
      {#if primaryThread}
        <span class="text-[var(--ui-text-subtle)]">Thread</span>
        <a
          class="text-indigo-400 transition-colors hover:text-indigo-300"
          href={projectHref(
            `/threads/${encodeURIComponent(primaryThread.id)}`,
          )}
        >
          {primaryThread.title || primaryThread.id}
        </a>
        <span class="text-[var(--ui-text-subtle)]">·</span>
      {/if}
      <span>
        {workspace.board_summary?.card_count ?? workspace.cards?.count ?? 0} cards
      </span>
      <span class="text-[var(--ui-text-subtle)]">·</span>
      <span>Updated {formatTimestamp(board.updated_at) || "—"}</span>
    </div>
  </div>

  <!-- Notification alerts -->
  {#if mutationNotice || conflictWarning || mutationError || supportError}
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
      {#if supportError}
        <div
          class="rounded-md bg-amber-500/10 px-3 py-2 text-[12px] text-amber-400"
        >
          {supportError}
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

          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Primary document ID
            <input
              bind:value={boardPrimaryDocumentId}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-1.5 text-[13px] text-[var(--ui-text)]"
              list="board-detail-document-options"
              placeholder="incident-response-playbook"
              type="text"
            />
            {#if documentHint(boardPrimaryDocumentId)}
              <span class="mt-1 block text-[11px] text-[var(--ui-text-subtle)]">
                {documentHint(boardPrimaryDocumentId)}
              </span>
            {/if}
          </label>

          <div
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[12px] text-[var(--ui-text-muted)]"
          >
            Primary thread is fixed in v1.
            <div class="mt-1 text-[var(--ui-text)]">
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

          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Owners
            <textarea
              bind:value={boardOwners}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-1.5 text-[13px] text-[var(--ui-text)]"
              rows="3"
            ></textarea>
          </label>
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
          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Thread ID
            <input
              bind:value={addCardThreadId}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-1.5 text-[13px] text-[var(--ui-text)]"
              list="board-detail-thread-options"
              placeholder="thread-onboarding"
              type="text"
            />
            {#if threadHint(addCardThreadId)}
              <span class="mt-1 block text-[11px] text-[var(--ui-text-subtle)]">
                {threadHint(addCardThreadId)}
              </span>
            {/if}
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

          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Pinned document ID
            <input
              bind:value={addCardPinnedDocumentId}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-1.5 text-[13px] text-[var(--ui-text)]"
              list="board-detail-document-options"
              placeholder="onboarding-guide-v1"
              type="text"
            />
            {#if documentHint(addCardPinnedDocumentId)}
              <span class="mt-1 block text-[11px] text-[var(--ui-text-subtle)]">
                {documentHint(addCardPinnedDocumentId)}
              </span>
            {/if}
          </label>
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

  <!-- ═══ KANBAN BOARD ═══ -->
  <div class="flex gap-3 overflow-x-auto pb-4">
    {#each board.column_schema as column}
      {@const cards = cardsByColumn[column.key] ?? []}
      <div
        class="flex min-w-[220px] flex-1 flex-col rounded-md bg-[var(--ui-panel-muted)]"
      >
        <!-- Column header -->
        <div
          class="flex items-center justify-between px-3 py-2.5"
        >
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

        <!-- Card list -->
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
              {@const card = cardItem.card}
              {@const thread = cardItem.thread}
              {@const threadStatus = getThreadStatus(thread)}

              <div
                class="group rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] transition-colors hover:border-[var(--ui-border-strong)]"
              >
                <div class="px-2.5 py-2">
                  <!-- Title row -->
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
                      href={projectHref(
                        `/threads/${encodeURIComponent(card.thread_id)}`,
                      )}
                    >
                      {thread?.title || card.thread_id}
                    </a>
                  </div>

                  <!-- Meta -->
                  <div
                    class="mt-1 pl-4 text-[11px] text-[var(--ui-text-muted)]"
                  >
                    {thread?.status ?? "unknown"} · {thread?.priority ??
                      "—"}
                    · {formatTimestamp(card.created_at)}
                  </div>

                  <!-- Compact stat badges -->
                  <div class="mt-1.5 flex flex-wrap gap-1 pl-4">
                    {#if (cardItem.summary?.open_commitment_count ?? 0) > 0}
                      <span
                        class="rounded bg-[var(--ui-border)] px-1 py-0.5 text-[10px] text-[var(--ui-text-muted)]"
                      >
                        {cardItem.summary.open_commitment_count} commit.
                      </span>
                    {/if}
                    {#if (cardItem.summary?.inbox_count ?? 0) > 0}
                      <span
                        class="rounded bg-amber-500/10 px-1 py-0.5 text-[10px] text-amber-400"
                      >
                        {cardItem.summary.inbox_count} inbox
                      </span>
                    {/if}
                    {#if (cardItem.summary?.decision_request_count ?? 0) > 0}
                      <span
                        class="rounded bg-indigo-500/10 px-1 py-0.5 text-[10px] text-indigo-400"
                      >
                        {cardItem.summary.decision_request_count} decisions
                      </span>
                    {/if}
                    <span
                      class="rounded px-1 py-0.5 text-[10px] {staleBadgeClass(
                        Boolean(cardItem.summary?.stale),
                      )}"
                    >
                      {cardItem.summary?.stale ? "Stale" : "Fresh"}
                    </span>
                  </div>

                  <!-- Pinned document -->
                  {#if cardItem.pinned_document}
                    <div class="mt-1.5 pl-4">
                      <a
                        class="inline-block rounded bg-indigo-500/10 px-1.5 py-0.5 text-[10px] text-indigo-300 transition-colors hover:text-indigo-200"
                        href={projectHref(
                          `/docs/${encodeURIComponent(cardItem.pinned_document.id)}`,
                        )}
                      >
                        {cardItem.pinned_document.title ||
                          cardItem.pinned_document.id}
                      </a>
                    </div>
                  {/if}

                  <!-- Manage toggle -->
                  <div class="mt-2 flex justify-end">
                    <button
                      aria-label={`Manage ${thread?.title || card.thread_id}`}
                      class="rounded px-1.5 py-0.5 text-[11px] text-[var(--ui-text-subtle)] transition-colors hover:bg-[var(--ui-border)] hover:text-[var(--ui-text-muted)]"
                      onclick={() => openCardManager(cardItem)}
                      type="button"
                    >
                      {expandedCardId === card.thread_id
                        ? "Close"
                        : "Actions"}
                    </button>
                  </div>
                </div>

                <!-- Expanded card detail + actions -->
                {#if expandedCardId === card.thread_id}
                  {@const thisCommitments = cardCommitments(card.thread_id)}
                  {@const thisInbox = cardInboxItems(card.thread_id)}
                  {@const thisDocs = cardDocuments(card.thread_id)}
                  <div
                    class="border-t border-[var(--ui-border)] bg-[var(--ui-panel-muted)]"
                  >
                    <!-- Per-card commitments -->
                    {#if thisCommitments.length > 0}
                      <div class="border-b border-[var(--ui-border)] px-2.5 py-1.5">
                        <p class="text-[10px] font-semibold uppercase tracking-wide text-[var(--ui-text-subtle)]">
                          Commitments
                        </p>
                        {#each thisCommitments as c}
                          <div class="mt-1 text-[11px]">
                            <span class="text-[var(--ui-text)]">
                              {c.title || c.id}
                            </span>
                            <span class="text-[var(--ui-text-subtle)]">
                              · {c.status ?? "?"} · Due {formatTimestamp(c.due_at) || "—"}
                            </span>
                          </div>
                        {/each}
                      </div>
                    {/if}

                    <!-- Per-card inbox items -->
                    {#if thisInbox.length > 0}
                      <div class="border-b border-[var(--ui-border)] px-2.5 py-1.5">
                        <p class="text-[10px] font-semibold uppercase tracking-wide text-[var(--ui-text-subtle)]">
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

                    <!-- Per-card documents -->
                    {#if thisDocs.length > 0}
                      <div class="border-b border-[var(--ui-border)] px-2.5 py-1.5">
                        <p class="text-[10px] font-semibold uppercase tracking-wide text-[var(--ui-text-subtle)]">
                          Documents
                        </p>
                        {#each thisDocs as doc}
                          <div class="mt-1">
                            <a
                              class="text-[11px] text-indigo-300 transition-colors hover:text-indigo-200"
                              href={projectHref(
                                `/docs/${encodeURIComponent(doc.id)}`,
                              )}
                            >
                              {doc.title || doc.id}
                            </a>
                          </div>
                        {/each}
                      </div>
                    {/if}

                    <!-- Card actions -->
                    <div class="space-y-2 px-2.5 py-2">
                      <div class="flex items-end gap-1.5">
                        <label
                          class="min-w-0 flex-1 text-[11px] text-[var(--ui-text-muted)]"
                        >
                          Move to
                          <select
                            bind:value={manageMoveColumnKey}
                            class="mt-0.5 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2 py-1 text-[12px] text-[var(--ui-text)]"
                          >
                            {#each board.column_schema as moveColumn}
                              <option value={moveColumn.key}>
                                {moveColumn.title ||
                                  boardColumnTitle(
                                    moveColumn.key,
                                    board.column_schema,
                                  )}
                              </option>
                            {/each}
                          </select>
                        </label>
                        <button
                          class="rounded bg-indigo-600 px-2.5 py-1 text-[11px] font-medium text-white transition-colors hover:bg-indigo-500 disabled:opacity-40"
                          disabled={mutatingCardId === card.thread_id}
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

                      <div class="flex items-end gap-1.5">
                        <label
                          class="min-w-0 flex-1 text-[11px] text-[var(--ui-text-muted)]"
                        >
                          Pinned doc
                          <input
                            bind:value={managePinnedDocumentId}
                            class="mt-0.5 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2 py-1 text-[12px] text-[var(--ui-text)]"
                            list="board-detail-document-options"
                            type="text"
                          />
                        </label>
                        <button
                          class="rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2.5 py-1 text-[11px] text-[var(--ui-text-muted)] transition-colors hover:text-[var(--ui-text)] disabled:opacity-40"
                          disabled={mutatingCardId === card.thread_id}
                          onclick={() => saveCardPinnedDocument(cardItem)}
                          type="button"
                        >
                          Save
                        </button>
                      </div>

                      <div class="flex items-center gap-1">
                        <button
                          class="rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2 py-1 text-[11px] text-[var(--ui-text-muted)] transition-colors hover:text-[var(--ui-text)] disabled:opacity-40"
                          disabled={index === 0 ||
                            mutatingCardId === card.thread_id}
                          onclick={() =>
                            reorderCard(cardItem, cards, index, "up")}
                          type="button"
                        >
                          ↑
                        </button>
                        <button
                          class="rounded border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2 py-1 text-[11px] text-[var(--ui-text-muted)] transition-colors hover:text-[var(--ui-text)] disabled:opacity-40"
                          disabled={index === cards.length - 1 ||
                            mutatingCardId === card.thread_id}
                          onclick={() =>
                            reorderCard(cardItem, cards, index, "down")}
                          type="button"
                        >
                          ↓
                        </button>
                        <div class="flex-1"></div>
                        <button
                          class="rounded border border-red-500/20 bg-red-500/10 px-2 py-1 text-[11px] text-red-400 transition-colors hover:bg-red-500/15 disabled:opacity-40"
                          disabled={mutatingCardId === card.thread_id}
                          onclick={() => removeCard(cardItem)}
                          type="button"
                        >
                          Remove
                        </button>
                      </div>
                    </div>
                  </div>
                {/if}
              </div>
            {/each}
          {/if}
        </div>
      </div>
    {/each}
  </div>

  <!-- Warnings (shown as inline alerts if any) -->
  {#if boardWarnings.length > 0}
    <div class="mt-4 space-y-1.5">
      {#each boardWarnings as warning}
        <div
          class="rounded-md bg-amber-500/10 px-3 py-2 text-[12px] text-amber-100"
        >
          {warning.message || "Workspace warning"}
          {#if warning.thread_id}
            <a
              class="ml-1 font-medium text-amber-200 underline transition-colors hover:text-amber-100"
              href={projectHref(
                `/threads/${encodeURIComponent(warning.thread_id)}`,
              )}
            >
              {warning.thread_id}
            </a>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
{/if}
