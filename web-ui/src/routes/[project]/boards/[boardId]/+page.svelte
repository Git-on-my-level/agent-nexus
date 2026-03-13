<script>
  import { page } from "$app/stores";
  import GuidedTypedRefsInput from "$lib/components/GuidedTypedRefsInput.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { projectPath } from "$lib/projectPaths";
  import { lookupActorDisplayName, actorRegistry } from "$lib/actorSession";
  import {
    BOARD_STATUS_LABELS,
    CANONICAL_BOARD_COLUMNS,
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
      supportError =
        `Failed to load board controls: ${e instanceof Error ? e.message : String(e)}`;
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
    if (status === "archived") {
      return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
    }
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

<div class="mb-4">
  <div class="flex items-center gap-2">
    <a
      class="text-[12px] text-[var(--ui-text-muted)] transition-colors hover:text-[var(--ui-text)]"
      href={projectHref("/boards")}
    >
      Boards
    </a>
    <span class="text-[12px] text-[var(--ui-text-subtle)]">/</span>
    <span class="text-[12px] text-[var(--ui-text-muted)]">
      {workspace?.board?.title || boardId}
    </span>
  </div>
</div>

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

  <div class="mb-4 space-y-2">
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
      <div class="rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400">
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

  <div class="mb-6 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]">
    <div class="border-b border-[var(--ui-border)] px-4 py-3">
      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <div class="mb-2 flex flex-wrap items-center gap-2">
            {#if board.status}
              <span
                class="inline-flex rounded px-1.5 py-0.5 text-[11px] font-semibold {statusColor(
                  board.status,
                )}"
              >
                {BOARD_STATUS_LABELS[board.status] ?? board.status}
              </span>
            {/if}
            {#each board.labels ?? [] as label}
              <span
                class="rounded bg-[var(--ui-border)] px-1.5 py-0.5 text-[10px] text-[var(--ui-text-muted)]"
              >
                {label}
              </span>
            {/each}
          </div>

          <h1 class="text-xl font-semibold text-[var(--ui-text)]">
            {board.title || board.id}
          </h1>
          <p class="mt-1 text-[12px] text-[var(--ui-text-muted)]">
            Updated {formatTimestamp(board.updated_at) || "—"} by {actorName(
              board.updated_by,
            )}
          </p>
        </div>

        <div class="flex flex-wrap justify-end gap-2">
          <button
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] hover:text-[var(--ui-text)]"
            onclick={openBoardEditForm}
            type="button"
          >
            {showBoardEditForm ? "Close edit" : "Edit board"}
          </button>
          <button
            class="rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white transition-colors hover:bg-indigo-500"
            onclick={openAddCardForm}
            type="button"
          >
            {showAddCardForm ? "Close add card" : "Add card"}
          </button>
        </div>
      </div>
    </div>

    <div class="space-y-4 px-4 py-3">
      {#if board.owners?.length > 0}
        <div class="text-[12px] text-[var(--ui-text-muted)]">
          Owned by
          <span class="text-[var(--ui-text)]">
            {board.owners.map((owner) => actorName(owner)).join(", ")}
          </span>
        </div>
      {/if}

      <div class="flex flex-wrap gap-3 text-[12px]">
        {#if primaryThread}
          <div
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2"
          >
            <span class="text-[var(--ui-text-muted)]">Primary thread:</span>
            <a
              class="ml-1 text-indigo-300 transition-colors hover:text-indigo-200"
              href={projectHref(`/threads/${encodeURIComponent(primaryThread.id)}`)}
            >
              {primaryThread.id}
            </a>
            {#if primaryThread.title}
              <span class="ml-1 text-[var(--ui-text-subtle)]">
                - {primaryThread.title}
              </span>
            {/if}
          </div>
        {/if}

        {#if workspace.primary_document}
          <div
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2"
          >
            <span class="text-[var(--ui-text-muted)]">Primary doc:</span>
            <a
              class="ml-1 text-indigo-300 transition-colors hover:text-indigo-200"
              href={projectHref(
                `/docs/${encodeURIComponent(workspace.primary_document.id)}`,
              )}
            >
              {workspace.primary_document.title || workspace.primary_document.id}
            </a>
          </div>
        {/if}

        <div
          class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2"
        >
          <span class="text-[var(--ui-text-muted)]">Cards:</span>
          <span class="ml-1 text-[var(--ui-text)]">
            {workspace.board_summary?.card_count ?? workspace.cards?.count ?? 0}
          </span>
        </div>
      </div>

      {#if board.pinned_refs?.length > 0}
        <div>
          <h2
            class="mb-2 text-[11px] font-semibold uppercase tracking-wide text-[var(--ui-text-muted)]"
          >
            Pinned refs
          </h2>
          <div class="flex flex-wrap gap-2">
            {#each board.pinned_refs as ref}
              <span
                class="rounded bg-[var(--ui-border)] px-2 py-1 text-[12px] text-[var(--ui-text)]"
              >
                <RefLink refValue={ref} humanize={true} showRaw={true} />
              </span>
            {/each}
          </div>
        </div>
      {/if}
    </div>
  </div>

  {#if showBoardEditForm}
    <section class="mb-5 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]">
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
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
              type="text"
            />
          </label>

          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Status
            <select
              bind:value={boardStatus}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
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
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
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

          <div class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[12px] text-[var(--ui-text-muted)]">
            Primary thread is fixed in v1.
            <div class="mt-1 text-[var(--ui-text)]">{board.primary_thread_id}</div>
          </div>
        </div>

        <div class="grid gap-3 md:grid-cols-2">
          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Labels
            <textarea
              bind:value={boardLabels}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
              rows="3"
            ></textarea>
          </label>

          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Owners
            <textarea
              bind:value={boardOwners}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
              rows="3"
            ></textarea>
          </label>
        </div>

        <div>
          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Pinned refs
          </label>
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

  {#if showAddCardForm}
    <section class="mb-5 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]">
      <div class="border-b border-[var(--ui-border)] px-4 py-2.5">
        <h2 class="text-[13px] font-medium text-[var(--ui-text)]">
          Add existing thread as a card
        </h2>
      </div>

      <div class="space-y-3 px-4 py-3">
        <div class="grid gap-3 md:grid-cols-2">
          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Thread ID
            <input
              bind:value={addCardThreadId}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
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
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
            >
              {#each board.column_schema as column}
                <option value={column.key}>
                  {column.title || boardColumnTitle(column.key, board.column_schema)}
                </option>
              {/each}
            </select>
          </label>

          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
            Pinned document ID
            <input
              bind:value={addCardPinnedDocumentId}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
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

  <div class="space-y-6">
    {#each board.column_schema as column}
      {@const cards = cardsByColumn[column.key] ?? []}
      <section>
        <div class="mb-2 flex items-center justify-between">
          <h2 class="text-[13px] font-semibold text-[var(--ui-text)]">
            {column.title || boardColumnTitle(column.key, board.column_schema)}
          </h2>
          <span
            class="rounded bg-[var(--ui-border)] px-2 py-0.5 text-[11px] text-[var(--ui-text-muted)]"
          >
            {cards.length}
          </span>
        </div>

        {#if cards.length === 0}
          <div
            class="rounded-md border border-dashed border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-4 py-6 text-center text-[12px] text-[var(--ui-text-muted)]"
          >
            No cards in {(
              column.title || boardColumnTitle(column.key, board.column_schema)
            ).toLowerCase()}
          </div>
        {:else}
          <div
            class="space-y-px overflow-hidden rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
          >
            {#each cards as cardItem, index}
              {@const card = cardItem.card}
              {@const thread = cardItem.thread}
              {@const threadStatus = getThreadStatus(thread)}
              <div
                class="{index > 0 ? 'border-t border-[var(--ui-border)]' : ''}"
              >
                <div class="px-4 py-3">
                  <div class="flex items-start justify-between gap-3">
                    <div class="min-w-0 flex-1">
                      <div class="flex items-start justify-between gap-3">
                        <div class="min-w-0 flex-1">
                          <a
                            class="truncate text-[13px] font-medium transition-colors hover:text-indigo-300 {threadStatusColor(
                              threadStatus,
                            )}"
                            href={projectHref(
                              `/threads/${encodeURIComponent(card.thread_id)}`,
                            )}
                          >
                            {thread?.title || card.thread_id}
                          </a>
                          <p
                            class="mt-0.5 text-[11px] text-[var(--ui-text-muted)]"
                          >
                            {thread?.status ?? "unknown"} · {thread?.priority ?? "—"}
                            · Added {formatTimestamp(card.created_at)}
                          </p>
                          <p
                            class="mt-1 text-[11px] text-[var(--ui-text-subtle)]"
                          >
                            {cardItem.summary?.open_commitment_count ?? 0} open
                            commitments · {cardItem.summary?.document_count ?? 0}
                            docs
                          </p>
                        </div>

                        <div class="flex shrink-0 flex-col items-end gap-2">
                          {#if cardItem.pinned_document}
                            <a
                              class="rounded bg-indigo-500/10 px-1.5 py-0.5 text-[10px] text-indigo-300 transition-colors hover:text-indigo-200"
                              href={projectHref(
                                `/docs/${encodeURIComponent(cardItem.pinned_document.id)}`,
                              )}
                            >
                              {cardItem.pinned_document.title ||
                                cardItem.pinned_document.id}
                            </a>
                          {/if}
                          <button
                            aria-label={`Manage ${thread?.title || card.thread_id}`}
                            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2.5 py-1 text-[11px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] hover:text-[var(--ui-text)]"
                            onclick={() => openCardManager(cardItem)}
                            type="button"
                          >
                            {expandedCardId === card.thread_id ? "Close actions" : "Manage"}
                          </button>
                        </div>
                      </div>
                    </div>
                  </div>

                  {#if expandedCardId === card.thread_id}
                    <div
                      class="mt-3 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] p-3"
                    >
                      <div class="grid gap-3 md:grid-cols-2">
                        <label
                          class="text-[12px] font-medium text-[var(--ui-text-muted)]"
                        >
                          Move to column
                          <select
                            bind:value={manageMoveColumnKey}
                            class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
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

                        <label
                          class="text-[12px] font-medium text-[var(--ui-text-muted)]"
                        >
                          Pinned document ID
                          <input
                            bind:value={managePinnedDocumentId}
                            class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
                            list="board-detail-document-options"
                            type="text"
                          />
                          {#if documentHint(managePinnedDocumentId)}
                            <span
                              class="mt-1 block text-[11px] text-[var(--ui-text-subtle)]"
                            >
                              {documentHint(managePinnedDocumentId)}
                            </span>
                          {/if}
                        </label>
                      </div>

                      <div class="mt-3 flex flex-wrap gap-2">
                        <button
                          class="rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white transition-colors hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-60"
                          disabled={mutatingCardId === card.thread_id}
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
                        <button
                          class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] hover:text-[var(--ui-text)] disabled:cursor-not-allowed disabled:opacity-60"
                          disabled={index === 0 || mutatingCardId === card.thread_id}
                          onclick={() => reorderCard(cardItem, cards, index, "up")}
                          type="button"
                        >
                          Move up
                        </button>
                        <button
                          class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] hover:text-[var(--ui-text)] disabled:cursor-not-allowed disabled:opacity-60"
                          disabled={index === cards.length - 1 || mutatingCardId === card.thread_id}
                          onclick={() =>
                            reorderCard(cardItem, cards, index, "down")}
                          type="button"
                        >
                          Move down
                        </button>
                        <button
                          class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] hover:text-[var(--ui-text)] disabled:cursor-not-allowed disabled:opacity-60"
                          disabled={mutatingCardId === card.thread_id}
                          onclick={() => saveCardPinnedDocument(cardItem)}
                          type="button"
                        >
                          Save pinned doc
                        </button>
                        <button
                          class="rounded-md border border-red-500/20 bg-red-500/10 px-3 py-1.5 text-[12px] font-medium text-red-400 transition-colors hover:bg-red-500/15 disabled:cursor-not-allowed disabled:opacity-60"
                          disabled={mutatingCardId === card.thread_id}
                          onclick={() => removeCard(cardItem)}
                          type="button"
                        >
                          Remove card
                        </button>
                      </div>
                    </div>
                  {/if}
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </section>
    {/each}
  </div>
{/if}
