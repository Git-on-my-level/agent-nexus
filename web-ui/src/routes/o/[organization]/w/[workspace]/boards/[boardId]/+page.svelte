<script>
  import { goto, replaceState } from "$app/navigation";
  import { page } from "$app/stores";
  import BoardFeedStrip from "$lib/components/BoardFeedStrip.svelte";
  import BoardCard from "$lib/components/BoardCard.svelte";
  import WorkspaceResourceTopRow from "$lib/components/WorkspaceResourceTopRow.svelte";
  import CardDetailModal from "$lib/components/CardDetailModal.svelte";
  import IdsIntegrityDisclosure from "$lib/components/IdsIntegrityDisclosure.svelte";
  import ResourceShareMenu from "$lib/components/ResourceShareMenu.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import {
    actorRegistry,
    lookupActorDisplayName,
    principalRegistry,
  } from "$lib/actorSession";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { workspacePath } from "$lib/workspacePaths";
  import {
    enrichInboxItem,
    formatInboxItemBoardPanelResourceLine,
  } from "$lib/inboxUtils";
  import {
    boardWorkspaceContextLinkLabel,
    boardWorkspaceInspectNav,
    shouldShowBoardWorkspaceContextLink,
    warningInspectNav,
  } from "$lib/topicRouteUtils";
  import RefLink from "$lib/components/RefLink.svelte";
  import {
    BOARD_STATUS_LABELS,
    BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT,
    boardCardStableId,
    boardColumnTitle,
    freshnessStatusLabel,
    freshnessStatusTone,
    groupBoardWorkspaceCards,
  } from "$lib/boardUtils";
  import { withUpdatedSearchParams } from "$lib/urlState";

  let { data } = $props();

  let workspace = $state(null);
  let loading = $state(false);
  let error = $state("");
  let mutationNotice = $state("");
  let mutationError = $state("");
  let conflictWarning = $state("");

  let backlogOpen = $state(false);
  let doneOpen = $state(false);
  let confirmModal = $state({ open: false, action: "" });
  let boardLifecycleBusy = $state(false);
  let boardMoreOpen = $state(false);
  let boardMoreRoot = $state(/** @type {HTMLDivElement | null} */ (null));
  let detailModalCard = $state(null);

  let previousBoardId = $state("");
  let previousCardUrlParam = $state("");
  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let boardId = $derived($page.params.boardId);
  function actorName(id) {
    return lookupActorDisplayName(id, $actorRegistry, $principalRegistry);
  }
  let enrichedInboxItems = $derived(
    (workspace?.inbox?.items ?? []).map((item) => enrichInboxItem(item)),
  );
  function workspaceHref(pathname = "/") {
    return workspacePath(organizationSlug, workspaceSlug, pathname);
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

  async function loadWorkspace() {
    loading = true;
    error = "";
    try {
      workspace = await coreClient.getBoardWorkspace(boardId);
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

  async function runBoardMutation(action, successMessage) {
    clearMutationMessages();

    try {
      await action();
      await loadWorkspace();

      mutationNotice = successMessage;
    } catch (e) {
      if (e?.status === 409) {
        await handleBoardConflict();
        return;
      }

      mutationError = formatMutationError("Board write failed", e);
    }
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
    const ifUpdatedAt = String(cardItem.membership?.updated_at ?? "").trim();
    if (!ifUpdatedAt) {
      mutationError =
        "Cannot save card details: missing card timestamp. Refresh the board and try again.";
      return;
    }
    await runBoardMutation(
      () =>
        coreClient.updateBoardCard(boardId, cardId, {
          if_updated_at: ifUpdatedAt,
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

  function lifecycleStateColor(state) {
    if (state === "active") return "text-ok-text bg-ok-soft";
    if (state === "archived") return "text-warn-text bg-warn-soft";
    if (state === "trashed") return "text-slate-300 bg-slate-500/10";
    return "text-[var(--fg-muted)] bg-[var(--line)]";
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
      await goto(workspacePath(organizationSlug, workspaceSlug, "/boards"));
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

  function toggleBoardMore() {
    boardMoreOpen = !boardMoreOpen;
  }
  function closeBoardMore() {
    boardMoreOpen = false;
  }

  $effect(() => {
    if (!boardMoreOpen) return;
    function onDocPointerDown(/** @type {PointerEvent} */ e) {
      if (
        boardMoreRoot &&
        e.target instanceof Node &&
        !boardMoreRoot.contains(e.target)
      ) {
        boardMoreOpen = false;
      }
    }
    function onDocKey(/** @type {KeyboardEvent} */ e) {
      if (e.key === "Escape") boardMoreOpen = false;
    }
    window.document.addEventListener("pointerdown", onDocPointerDown, true);
    window.document.addEventListener("keydown", onDocKey, true);
    return () => {
      window.document.removeEventListener(
        "pointerdown",
        onDocPointerDown,
        true,
      );
      window.document.removeEventListener("keydown", onDocKey, true);
    };
  });
</script>

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
    Loading board...
  </div>
{:else if error}
  <div
    class="mb-4 rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
  >
    {error}
  </div>
{:else if workspace}
  {@const board = workspace.board}
  {@const boardInspectNav = boardWorkspaceInspectNav(workspace)}
  {@const contextLinkLabel = boardWorkspaceContextLinkLabel(
    workspace,
    boardInspectNav,
    { organization: organizationSlug, workspace: workspaceSlug },
  )}
  {@const showBoardContextLink = shouldShowBoardWorkspaceContextLink(
    contextLinkLabel,
    boardInspectNav?.segment,
  )}
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
  {@const boardIsEmpty =
    backlogCards.length === 0 &&
    doneCards.length === 0 &&
    activeColumns.every((c) => (cardsByColumn[c.key] ?? []).length === 0)}

  {#if board.trashed_at}
    <div
      class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-md border border-danger/30 bg-danger-soft px-3 py-2 text-meta text-danger-text"
    >
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2 font-semibold">
          <span>⚠</span>
          <span>This board is in trash</span>
        </div>
        {#if board.trash_reason}
          <p class="mt-2">Reason: {board.trash_reason}</p>
        {/if}
        <p class="mt-1 text-micro text-danger-text/80">
          Trashed {#if board.trashed_by}by {actorName(board.trashed_by)}{/if}
          {#if board.trashed_at}
            at {formatTimestamp(board.trashed_at)}
          {/if}
        </p>
      </div>
      <button
        class="shrink-0 cursor-pointer rounded-md border border-danger/40 bg-danger-soft px-2 py-1 text-micro font-medium text-danger-text hover:bg-danger/25 disabled:opacity-50"
        disabled={boardLifecycleBusy}
        onclick={handleRestoreBoard}
        type="button"
      >
        {boardLifecycleBusy ? "…" : "Restore"}
      </button>
    </div>
  {:else if board.archived_at}
    <div
      class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-md border border-warn/30 bg-warn-soft px-3 py-2 text-meta text-warn-text"
    >
      <p class="min-w-0 flex-1">
        This board was archived on {formatTimestamp(board.archived_at) ||
          "—"}{#if board.archived_by}{" by "}{actorName(
            board.archived_by,
          )}{/if}.
      </p>
      <button
        class="shrink-0 cursor-pointer rounded-md border border-warn/40 bg-warn-soft px-2 py-1 text-micro font-medium text-warn-text hover:bg-warn/25 disabled:opacity-50"
        disabled={boardLifecycleBusy}
        onclick={handleUnarchiveBoard}
        type="button"
      >
        {boardLifecycleBusy ? "…" : "Unarchive"}
      </button>
    </div>
  {/if}

  {#snippet boardDesktop()}
    <h1 class="min-w-0 text-subtitle font-semibold text-[var(--fg)]">
      {board.title || board.id}
    </h1>
    {#if String(board.summary ?? "").trim()}
      <p
        class="line-clamp-3 text-[13px] text-[var(--fg-muted)]"
        title={String(board.summary).trim()}
      >
        {String(board.summary).trim()}
      </p>
    {/if}
    <div
      class="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-micro text-[var(--fg-muted)]"
    >
      {#if boardInspectNav && showBoardContextLink}
        <span class="text-[var(--fg-muted)]"
          >{boardInspectNav.kind === "topic" ? "Topic" : "Backing thread"}</span
        >
        <a
          class="text-accent-text transition-colors hover:text-accent-text"
          href={workspaceHref(
            boardInspectNav.kind === "topic"
              ? `/topics/${encodeURIComponent(boardInspectNav.segment)}`
              : `/threads/${encodeURIComponent(boardInspectNav.segment)}`,
          )}
        >
          {contextLinkLabel}
        </a>
        <span class="text-[var(--fg-subtle)]">·</span>
      {/if}
      <span>
        {workspace.board_summary?.card_count ?? workspace.cards?.count ?? 0}
        cards
      </span>
      <span class="text-[var(--fg-subtle)]">·</span>
      <span>
        {workspace.board_summary?.resolved_card_count ?? 0} resolved
      </span>
      <span class="text-[var(--fg-subtle)]">·</span>
      <span>
        {workspace.board_summary?.unresolved_card_count ?? 0} open
      </span>
      <span class="text-[var(--fg-subtle)]">·</span>
      <span>Board updated {formatTimestamp(board.updated_at) || "—"}</span>
      {#if board.owners?.length > 0}
        <span class="text-[var(--fg-subtle)]">·</span>
        <span
          >Owners {board.owners
            .map((owner) => actorName(owner))
            .join(", ")}</span
        >
      {/if}
    </div>
  {/snippet}

  <div
    class="page-dock-layout page-dock-layout--fixed-mobile-chat page-dock-layout--board-viewport-chat"
  >
    <div class="page-dock-scroll">
      <div class="mb-3 max-md:mb-2">
        <WorkspaceResourceTopRow
          breadcrumbAriaLabel="Breadcrumb and board status"
          desktopAriaLabel="Board details"
          desktop={boardDesktop}
        >
          {#snippet breadcrumb()}
            <a
              class="shrink-0 text-[var(--fg-muted)] transition-colors hover:text-[var(--fg)]"
              href={workspaceHref("/boards")}>Boards</a
            >
            <span class="shrink-0 text-[var(--fg-subtle)]">/</span>
            <div class="flex min-h-0 min-w-0 flex-1 items-center gap-1.5">
              <span
                class="min-w-0 shrink truncate text-[var(--fg-muted)]"
                aria-current="page">{board.title || boardId}</span
              >
              {#if board.state}
                <span
                  class="shrink-0 rounded px-1.5 py-0.5 text-[11px] font-semibold leading-none sm:text-micro {lifecycleStateColor(
                    board.state,
                  )}"
                >
                  {BOARD_STATUS_LABELS[board.state] ?? board.state}
                </span>
              {/if}
              <span
                class="hidden shrink-0 rounded px-1.5 py-0.5 text-micro font-medium md:inline {freshnessStatusTone(
                  boardFreshness?.status,
                )}"
                title={`${boardProjectionMessage(boardFreshness)} Generated ${formatTimestamp(workspace.generated_at) || "—"}.`}
              >
                {freshnessStatusLabel(boardFreshness?.status)}
              </span>
            </div>
          {/snippet}
          {#snippet actions()}
            {#if !board.trashed_at}
              <ResourceShareMenu resourceId={board.id} rawRecord={board} />
              <a
                class="rounded-md bg-accent-solid px-2.5 py-1.5 text-micro font-medium text-white transition-colors hover:bg-accent"
                href={workspaceHref(`/boards/${boardId}/cards/new`)}
              >
                Add card
              </a>
              <div bind:this={boardMoreRoot} class="relative">
                <button
                  type="button"
                  class="inline-flex h-7 w-7 cursor-pointer items-center justify-center rounded-md border border-line bg-transparent text-fg-muted transition-colors hover:bg-panel-hover hover:text-fg disabled:cursor-not-allowed disabled:opacity-50"
                  aria-label="More actions"
                  aria-haspopup="menu"
                  aria-expanded={boardMoreOpen}
                  disabled={boardLifecycleBusy}
                  onclick={toggleBoardMore}
                >
                  <svg
                    class="h-4 w-4"
                    fill="currentColor"
                    viewBox="0 0 24 24"
                    aria-hidden="true"
                  >
                    <circle cx="12" cy="5" r="1.75" />
                    <circle cx="12" cy="12" r="1.75" />
                    <circle cx="12" cy="19" r="1.75" />
                  </svg>
                </button>
                {#if boardMoreOpen}
                  <div
                    class="absolute right-0 z-50 mt-1 min-w-[10rem] rounded-md border border-[var(--line)] bg-[var(--panel)] py-1 shadow-lg"
                    role="menu"
                  >
                    <a
                      role="menuitem"
                      class="block w-full px-3 py-2 text-left text-micro text-[var(--fg)] hover:bg-[var(--line-subtle)]"
                      href={workspaceHref(`/boards/${boardId}/edit`)}
                      onclick={closeBoardMore}
                    >
                      Settings
                    </a>
                    {#if !board.archived_at}
                      <button
                        type="button"
                        role="menuitem"
                        class="block w-full px-3 py-2 text-left text-micro text-[var(--fg)] hover:bg-[var(--line-subtle)] disabled:opacity-50"
                        disabled={boardLifecycleBusy}
                        onclick={() => {
                          closeBoardMore();
                          confirmModal = { open: true, action: "archive" };
                        }}
                      >
                        {boardLifecycleBusy ? "…" : "Archive"}
                      </button>
                    {/if}
                    <button
                      type="button"
                      role="menuitem"
                      class="block w-full px-3 py-2 text-left text-micro text-danger-text hover:bg-[var(--line-subtle)] disabled:opacity-50"
                      disabled={boardLifecycleBusy}
                      onclick={() => {
                        closeBoardMore();
                        confirmModal = { open: true, action: "trash" };
                      }}
                    >
                      {boardLifecycleBusy ? "…" : "Move to trash"}
                    </button>
                  </div>
                {/if}
              </div>
            {/if}
          {/snippet}
        </WorkspaceResourceTopRow>
      </div>

      <!-- Notification alerts -->
      {#if mutationNotice || conflictWarning || mutationError}
        <div class="mb-3 space-y-2">
          {#if mutationNotice}
            <div
              class="rounded-md bg-ok-soft px-3 py-2 text-micro text-ok-text"
            >
              {mutationNotice}
            </div>
          {/if}
          {#if conflictWarning}
            <div
              class="rounded-md bg-warn-soft px-3 py-2 text-micro text-warn-text"
            >
              {conflictWarning}
            </div>
          {/if}
          {#if mutationError}
            <div
              class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
            >
              {mutationError}
            </div>
          {/if}
        </div>
      {/if}

      {#snippet renderCard(cardItem)}
        <BoardCard
          {cardItem}
          {boardId}
          onclick={() => openCardDetailModal(cardItem)}
        />
      {/snippet}

      <section class="mb-3">
        {#if boardIsEmpty}
          <div
            class="rounded-md border border-dashed border-[var(--line)] bg-[var(--panel)] px-6 py-16 text-center"
          >
            <p class="text-meta font-medium text-[var(--fg)]">
              No cards on this board yet
            </p>
            <p class="mt-1 text-micro text-[var(--fg-muted)]">
              Cards appear here when topics are added to the board's columns.
            </p>
          </div>
        {:else}
          <div
            class="mb-3 rounded-md border border-[var(--line)] bg-[var(--panel)]"
          >
            <button
              class="flex w-full items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-[var(--line-subtle)]"
              onclick={() => {
                backlogOpen = !backlogOpen;
              }}
              type="button"
            >
              <svg
                class="h-3.5 w-3.5 shrink-0 text-[var(--fg-muted)] transition-transform {backlogOpen
                  ? 'rotate-90'
                  : ''}"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-width="2"><path d="M9 5l7 7-7 7" /></svg
              >
              <span class="text-micro font-medium text-[var(--fg-muted)]"
                >Backlog</span
              >
              <span
                class="rounded bg-[var(--line)] px-1.5 py-0.5 text-micro text-[var(--fg-muted)]"
                >{backlogCards.length}</span
              >
            </button>
            {#if backlogOpen}
              <div class="space-y-2 border-t border-[var(--line)] px-3 py-2">
                {#if backlogCards.length === 0}
                  <p class="text-micro text-[var(--fg-muted)]">No cards</p>
                {:else}
                  {#each backlogCards as cardItem (boardCardStableId(cardItem.membership))}
                    {@render renderCard(cardItem)}
                  {/each}
                {/if}
              </div>
            {/if}
          </div>

          <div class="flex gap-3 overflow-x-auto pb-4">
            {#each activeColumns as column (column.key)}
              {@const cards = cardsByColumn[column.key] ?? []}
              {@const isBlocked = column.key === "blocked"}
              <div
                class="flex min-w-[260px] flex-1 flex-col rounded-md bg-[var(--bg-soft)]"
              >
                <div class="flex items-center justify-between px-3 py-2.5">
                  <h3
                    class="text-micro font-semibold uppercase tracking-wide {isBlocked &&
                    cards.length > 0
                      ? 'text-warn-text'
                      : 'text-[var(--fg-muted)]'}"
                  >
                    {column.title ||
                      boardColumnTitle(column.key, board.column_schema)}
                  </h3>
                  <span
                    class="min-w-[1.25rem] rounded px-1.5 py-0.5 text-center text-micro {isBlocked &&
                    cards.length > 0
                      ? 'bg-warn-soft text-warn-text'
                      : 'bg-[var(--line)] text-[var(--fg-muted)]'}"
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
                      class="flex items-center justify-center rounded-md border border-dashed border-[var(--line)] px-3 py-10 text-micro text-[var(--fg-muted)]"
                    >
                      No cards
                    </div>
                  {:else}
                    {#each cards as cardItem (boardCardStableId(cardItem.membership))}
                      {@render renderCard(cardItem)}
                    {/each}
                  {/if}
                </div>
              </div>
            {/each}
          </div>

          <div class="rounded-md border border-[var(--line)] bg-[var(--panel)]">
            <button
              class="flex w-full items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-[var(--line-subtle)]"
              onclick={() => {
                doneOpen = !doneOpen;
              }}
              type="button"
            >
              <svg
                class="h-3.5 w-3.5 shrink-0 text-[var(--fg-muted)] transition-transform {doneOpen
                  ? 'rotate-90'
                  : ''}"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-width="2"><path d="M9 5l7 7-7 7" /></svg
              >
              <span class="text-micro font-medium text-[var(--fg-muted)]"
                >Done</span
              >
              <span
                class="rounded bg-[var(--line)] px-1.5 py-0.5 text-micro text-[var(--fg-muted)]"
                >{doneCards.length}</span
              >
            </button>
            {#if doneOpen}
              <div class="space-y-2 border-t border-[var(--line)] px-3 py-2">
                {#if doneCards.length === 0}
                  <p class="text-micro text-[var(--fg-muted)]">No cards</p>
                {:else}
                  {#each doneCards as cardItem (boardCardStableId(cardItem.membership))}
                    {@render renderCard(cardItem)}
                  {/each}
                {/if}
              </div>
            {/if}
          </div>
        {/if}
      </section>

      <div class="grid gap-3 lg:grid-cols-3">
        <section
          class="rounded-md border border-[var(--line)] bg-[var(--panel)]"
        >
          <div class="border-b border-[var(--line)] px-4 py-2.5">
            <h2 class="text-meta font-medium text-[var(--fg)]">
              Workspace docs
            </h2>
          </div>
          <div class="px-4 py-3">
            {#if (workspace.documents?.items ?? []).length === 0}
              <p class="text-micro text-[var(--fg-muted)]">
                No linked doc lineages yet.
              </p>
            {:else}
              <div class="space-y-2">
                {#each workspace.documents.items.slice(0, BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT) as document (document.id)}
                  <a
                    class="block rounded border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-micro transition-colors hover:border-[var(--line-strong)]"
                    href={workspaceHref(
                      `/docs/${encodeURIComponent(document.id)}`,
                    )}
                  >
                    <div class="font-medium text-[var(--fg)]">
                      {document.title || document.id}
                    </div>
                    <div class="mt-1 text-micro text-[var(--fg-muted)]">
                      Head v{document.head_revision_number ?? "—"} · Updated {formatTimestamp(
                        document.updated_at,
                      ) || "—"}
                    </div>
                  </a>
                {/each}
              </div>
              {#if (workspace.documents?.items ?? []).length > BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT}
                <p class="mt-2 text-micro text-[var(--fg-muted)]">
                  Showing {BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT} of {workspace
                    .documents.items.length}
                </p>
              {/if}
            {/if}
          </div>
        </section>

        <section
          class="rounded-md border border-[var(--line)] bg-[var(--panel)]"
        >
          <div class="border-b border-[var(--line)] px-4 py-2.5">
            <h2 class="text-meta font-medium text-[var(--fg)]">Review inbox</h2>
            <p class="mt-1 text-micro text-[var(--fg-muted)]">
              Derived risk and decision signals for resources tied to this board
              (backing threads).
            </p>
          </div>
          <div class="px-4 py-3">
            {#if enrichedInboxItems.length === 0}
              <p class="text-micro text-[var(--fg-muted)]">
                No active derived inbox items.
              </p>
            {:else}
              <div class="space-y-2">
                {#each enrichedInboxItems.slice(0, BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT) as item (item.id)}
                  {@const inboxResourceLine =
                    formatInboxItemBoardPanelResourceLine(item)}
                  <div
                    class="rounded border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2"
                  >
                    <div class="text-micro font-medium text-[var(--fg)]">
                      {item.title || item.summary || item.id}
                    </div>
                    <div class="mt-1 text-micro text-[var(--fg-muted)]">
                      <span
                        class={item.urgency_level === "immediate"
                          ? "text-danger-text"
                          : item.urgency_level === "high"
                            ? "text-warn-text"
                            : ""}>{item.urgency_label}</span
                      >
                      {#if inboxResourceLine}
                        · {inboxResourceLine}
                      {/if}
                    </div>
                  </div>
                {/each}
              </div>
              {#if enrichedInboxItems.length > BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT}
                <p class="mt-2 text-micro text-[var(--fg-muted)]">
                  Showing {BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT} of {enrichedInboxItems.length}
                </p>
              {/if}
            {/if}
          </div>
        </section>
      </div>

      <div class="mt-4">
        <IdsIntegrityDisclosure
          rows={[
            {
              label: "Board ID",
              value: board.id,
              copyLabel: "Copy board ID",
            },
          ]}
          rawJson={JSON.stringify(board, null, 2)}
          rawJsonCopyLabel="Copy board JSON"
        />
      </div>

      {#if boardWarnings.length > 0}
        <section
          class="mt-4 rounded-md border border-warn/20 bg-warn-soft px-4 py-3"
        >
          <h2 class="text-meta font-medium text-warn-text">Warnings</h2>
          <div class="mt-2 space-y-1.5">
            {#each boardWarnings as warning (`${warning.thread_id ?? ""}:${warning.message ?? ""}`)}
              <div class="text-micro text-warn-text">
                {warning.message || "Workspace warning"}
                {#if warning.topic_id || warning.thread_id}
                  {@const warnNav = warningInspectNav(warning)}
                  {#if warnNav}
                    {@const warnRef =
                      warnNav.kind === "topic"
                        ? `topic:${warnNav.segment}`
                        : `thread:${warnNav.segment}`}
                    <span
                      class="ml-1 inline [&_a]:font-medium [&_a]:text-warn-text [&_a]:underline [&_a]:transition-colors [&_a:hover]:text-warn-text"
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
    </div>

    <div class="page-dock-feed">
      <BoardFeedStrip
        {board}
        {workspaceSlug}
        workspaceId={data?.workspaceId ?? ""}
      />
    </div>
  </div>
{/if}

<CardDetailModal
  open={detailModalCard !== null}
  cardItem={detailModalCard}
  {boardId}
  board={workspace?.board ?? null}
  {workspaceSlug}
  workspaceId={data?.workspaceId ?? ""}
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
