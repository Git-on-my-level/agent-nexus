<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import BoardFeedStrip from "$lib/components/BoardFeedStrip.svelte";
  import BoardKanban from "$lib/components/BoardKanban.svelte";
  import WorkspaceResourceTopRow from "$lib/components/WorkspaceResourceTopRow.svelte";
  import CardDetailModal from "$lib/components/CardDetailModal.svelte";
  import MarkDoneModal from "$lib/components/MarkDoneModal.svelte";
  import IdsIntegrityDisclosure from "$lib/components/IdsIntegrityDisclosure.svelte";
  import ActorLabel from "$lib/components/ActorLabel.svelte";
  import Icon from "$lib/components/Icon.svelte";
  import ResourceShareMenu from "$lib/components/ResourceShareMenu.svelte";
  import Button from "$lib/components/Button.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import {
    actorRegistry,
    getSelectedActorId,
    lookupActorDisplayName,
    principalRegistry,
  } from "$lib/actorSession";
  import {
    applyOptimisticCardMove,
    BOARD_CARD_SORT_OPTIONS,
    buildCardResolvedAttestationEvent,
    cardHasTerminalEvidence,
    normalizeBoardMovePlacementAnchors,
  } from "$lib/boardCardMove.js";
  import { getAuthenticatedActorId } from "$lib/authSession";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { bindWorkspaceHref, workspacePath } from "$lib/workspacePaths";
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
    boardCardHeaderTitle,
    boardCardStableId,
    freshnessStatusLabel,
    freshnessStatusTone,
    sortedColumnPeersStableIds,
  } from "$lib/boardUtils";
  import { readEnumSearchParam, withUpdatedSearchParams } from "$lib/urlState";
  import {
    resourceCopyValue,
    resourceDisplayLabel,
    resourceRouteSegment,
  } from "$lib/resourceIdentity.js";

  let { data } = $props();

  const CARD_MODAL_TABS = ["overview", "resolution", "timeline", "revisions"];

  let workspace = $state(null);
  let loading = $state(false);
  let mutating = $state(false);
  let error = $state("");
  let mutationError = $state("");
  let conflictWarning = $state("");

  let markDoneModal = $state({
    open: false,
    cardItem: /** @type {object | null} */ (null),
    movePayload: /** @type {Record<string, unknown> | null} */ (null),
    busy: false,
  });
  let confirmModal = $state({ open: false, action: "" });
  let boardLifecycleBusy = $state(false);
  let boardMoreOpen = $state(false);
  let boardMoreRoot = $state(/** @type {HTMLDivElement | null} */ (null));
  let detailModalCard = $state(null);

  let filtersOpen = $state(false);
  let filterRoot = $state(/** @type {HTMLDivElement | null} */ (null));
  let sortOpen = $state(false);
  let sortRoot = $state(/** @type {HTMLDivElement | null} */ (null));
  /** @type {"rank" | "updated" | "due" | "risk"} */
  let boardSortMode = $state("rank");
  const emptyFilters = () => ({
    q: "",
    mineOnly: false,
    dueFilter: "",
    risk: /** @type {string[]} */ ([]),
  });
  let filterDraft = $state(emptyFilters());
  let filterApplied = $state(emptyFilters());
  let hasActiveFilters = $derived(
    Boolean(filterApplied.q.trim()) ||
      filterApplied.mineOnly ||
      Boolean(filterApplied.dueFilter) ||
      filterApplied.risk.length > 0,
  );
  let boardSortLabel = $derived(
    BOARD_CARD_SORT_OPTIONS.find((o) => o.value === boardSortMode)?.label ??
      "Manual",
  );
  let hasCustomSort = $derived(boardSortMode !== "rank");

  let previousBoardId = $state("");
  let previousCardUrlParam = $state("");
  let boardLoadRequestId = 0;
  let loadedBoardRouteKey = $state("");
  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let boardId = $derived($page.params.boardId);
  function actorName(id) {
    return lookupActorDisplayName(id, $actorRegistry, $principalRegistry);
  }
  let enrichedInboxItems = $derived(
    (workspace?.inbox?.items ?? []).map((item) => enrichInboxItem(item)),
  );

  let cardDetailColumnPeerIds = $derived.by(() => {
    const ws = workspace;
    const card = detailModalCard;
    const board = ws?.board;
    if (!ws?.cards || !card?.membership || !board) return [];
    return sortedColumnPeersStableIds(
      ws.cards,
      board.column_schema,
      card.membership.column_key,
    );
  });
  let workspaceHref = $derived(
    bindWorkspaceHref(organizationSlug, workspaceSlug),
  );
  let boardRouteSegment = $derived(
    resourceRouteSegment(workspace?.board, "board") || boardId,
  );
  let currentActorId = $derived(
    getAuthenticatedActorId() || getSelectedActorId(workspaceSlug) || "",
  );

  /** Keeps `$page.url` aligned with shallow card query-param changes (`replaceState` alone can leave `$page` stale). */
  let boardCardDetailTab = $derived(
    readEnumSearchParam($page.url.searchParams, "tab", CARD_MODAL_TABS, ""),
  );

  function navigateBoardCardParam(updates = {}) {
    const next = withUpdatedSearchParams($page.url, updates);
    void goto(next, {
      replaceState: true,
      keepFocus: true,
      noScroll: true,
    });
  }

  function handleBoardCardDetailTabChange(tab) {
    const t = String(tab ?? "").trim();
    if (t === "overview" || t === "") {
      navigateBoardCardParam({ tab: "" });
      return;
    }
    if (CARD_MODAL_TABS.includes(t)) {
      navigateBoardCardParam({ tab: t });
    }
  }

  function openCardDetailModal(cardItem) {
    const id =
      resourceRouteSegment(cardItem?.membership, "card") ||
      boardCardStableId(cardItem?.membership);
    if (!id) return;
    if (browser && window.matchMedia?.("(max-width: 1023px)")?.matches) {
      void goto(
        workspaceHref(
          `/boards/${encodeURIComponent(boardRouteSegment)}/cards/${encodeURIComponent(id)}`,
        ),
      );
      return;
    }
    detailModalCard = cardItem;
    navigateBoardCardParam({ card: id, tab: "" });
  }

  function closeCardDetailModal() {
    detailModalCard = null;
    navigateBoardCardParam({ card: "", tab: "" });
  }

  function boardRouteKey(id = boardId) {
    return [organizationSlug, workspaceSlug, id].map((v) => String(v ?? ""));
  }

  function serializeBoardRouteKey(routeKey) {
    return routeKey.join("\u001f");
  }

  function isCurrentBoardLoad(requestId, routeKey) {
    const current = boardRouteKey();
    return (
      requestId === boardLoadRequestId &&
      routeKey.every((segment, index) => segment === current[index])
    );
  }

  async function loadWorkspace(targetBoardId = boardId, options = {}) {
    if (!targetBoardId) return;
    const silent = Boolean(options.silent);
    const requestId = ++boardLoadRequestId;
    const routeKey = boardRouteKey(targetBoardId);
    const serializedRouteKey = serializeBoardRouteKey(routeKey);
    const routeChanged =
      loadedBoardRouteKey && loadedBoardRouteKey !== serializedRouteKey;
    if (!silent) {
      loading = true;
      error = "";
    }
    loadedBoardRouteKey = serializedRouteKey;
    if (
      !silent &&
      (routeChanged ||
        String(workspace?.board?.id ?? "") !== String(targetBoardId))
    ) {
      workspace = null;
      detailModalCard = null;
      mutationError = "";
      conflictWarning = "";
    }
    try {
      const loadedWorkspace = await coreClient.getBoardWorkspace(targetBoardId);
      if (!isCurrentBoardLoad(requestId, routeKey)) return;
      workspace = loadedWorkspace;
    } catch (e) {
      if (!isCurrentBoardLoad(requestId, routeKey)) return;
      if (!silent) {
        error = `Failed to load board: ${e instanceof Error ? e.message : String(e)}`;
        workspace = null;
      }
    } finally {
      if (isCurrentBoardLoad(requestId, routeKey) && !silent) {
        loading = false;
      }
    }
  }

  function clearMutationMessages() {
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
    mutationError = "";
    await loadWorkspace(boardId);
  }

  async function runBoardMutation(action, successMessage, options = {}) {
    clearMutationMessages();
    const optimisticApply = options.optimisticApply;
    const snapshot = optimisticApply
      ? structuredClone($state.snapshot(workspace))
      : null;

    if (optimisticApply) {
      workspace = optimisticApply(workspace);
    }

    mutating = true;
    try {
      await action();
      await loadWorkspace(boardId, { silent: true });
    } catch (e) {
      if (snapshot) {
        workspace = snapshot;
      }
      if (e?.status === 409) {
        await handleBoardConflict();
        return;
      }
      mutationError = formatMutationError("Board write failed", e);
    } finally {
      mutating = false;
    }
  }

  function buildMovePayload(cardItem, payload) {
    if (!workspace?.board) return null;
    const nextPayload = normalizeBoardMovePlacementAnchors(workspace?.cards, {
      if_board_updated_at: workspace.board.updated_at,
      ...payload,
    });
    const columnKey = String(nextPayload.column_key ?? "").trim();
    if (columnKey === "done") {
      const memb = cardItem?.membership ?? {};
      const membRefs = [...(memb.resolution_refs ?? [])]
        .map((r) => String(r ?? "").trim())
        .filter(Boolean);
      const incomingRefs = Array.isArray(nextPayload.resolution_refs)
        ? [...nextPayload.resolution_refs]
            .map((r) => String(r ?? "").trim())
            .filter(Boolean)
        : [];
      const mergedRefs = incomingRefs.length > 0 ? incomingRefs : [...membRefs];
      if (mergedRefs.length === 0) {
        return null;
      }
      delete nextPayload.resolution;
      if (incomingRefs.length === 0 && mergedRefs.length > 0) {
        nextPayload.resolution_refs = mergedRefs;
      }
    }
    return nextPayload;
  }

  async function resolveCardWithAttestation(cardItem, movePayload, note = "") {
    const eventBody = buildCardResolvedAttestationEvent({
      cardItem,
      board: workspace?.board,
      boardId,
      note,
    });
    const result = await coreClient.createEvent({ event: eventBody });
    const eventId = String(result?.event?.id ?? "").trim();
    if (!eventId) {
      throw new Error("Attestation event was created without an id.");
    }
    const resolutionRefs = [
      `event:${eventId}`,
      ...(cardItem?.membership?.resolution_refs ?? [])
        .map((r) => String(r ?? "").trim())
        .filter(Boolean),
    ];
    return {
      ...movePayload,
      resolution_refs: [...new Set(resolutionRefs)],
    };
  }

  async function executeCardMove(cardItem, payload, successMessage) {
    if (!workspace?.board) return;
    const cardId = boardCardStableId(cardItem.membership);
    const nextPayload = buildMovePayload(cardItem, payload);
    if (!nextPayload) return false;

    await runBoardMutation(
      () => coreClient.moveBoardCard(boardId, cardId, nextPayload),
      successMessage,
      {
        optimisticApply: (ws) =>
          applyOptimisticCardMove(ws, cardId, {
            column_key: String(payload.column_key ?? ""),
            before_card_id: String(payload.before_card_id ?? ""),
          }),
      },
    );
    return true;
  }

  async function moveCard(cardItem, payload, successMessage) {
    const columnKey = String(payload?.column_key ?? "").trim();
    if (
      columnKey === "done" &&
      !cardHasTerminalEvidence(cardItem?.membership, payload?.resolution_refs)
    ) {
      markDoneModal = {
        open: true,
        cardItem,
        movePayload: { ...payload },
        busy: false,
      };
      return;
    }
    await executeCardMove(cardItem, payload, successMessage);
  }

  async function handleCardDrop(cardItem, payload) {
    const toColumn = String(payload?.column_key ?? "").trim();
    const message = toColumn === "done" ? "Card marked done." : "Card moved.";
    await moveCard(
      cardItem,
      {
        column_key: payload?.column_key,
        before_card_id: payload?.before_card_id,
      },
      message,
    );
  }

  async function handleMarkDoneConfirm(note) {
    const pending = markDoneModal;
    if (!pending.cardItem || !pending.movePayload) return;
    markDoneModal = { ...pending, busy: true };
    try {
      const payload = await resolveCardWithAttestation(
        pending.cardItem,
        pending.movePayload,
        note,
      );
      markDoneModal = {
        open: false,
        cardItem: null,
        movePayload: null,
        busy: false,
      };
      await executeCardMove(pending.cardItem, payload, "Card marked done.");
    } catch (e) {
      markDoneModal = { ...pending, busy: false };
      mutationError = formatMutationError("Could not mark card done", e);
    }
  }

  async function handleInlineCreate({ title, column_key }) {
    if (!workspace?.board) return;
    const trimmed = String(title ?? "").trim();
    if (!trimmed) return;
    await runBoardMutation(async () => {
      await coreClient.addBoardCard(boardId, {
        if_board_updated_at: workspace.board.updated_at,
        title: trimmed,
        summary: "",
        column_key: String(column_key ?? "backlog"),
        risk: "medium",
        resolution: null,
        resolution_refs: [],
        related_refs: [],
        assignee_refs: [],
      });
    }, "Card added.");
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

  async function reviseCardContent(cardItem, payload) {
    if (!workspace?.board) return;

    const cardId = boardCardStableId(cardItem.membership);
    await runBoardMutation(
      () => coreClient.updateCardContent(cardId, payload),
      "Card revision saved.",
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
    return "text-fg-muted bg-line";
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
      const routeKey = serializeBoardRouteKey(boardRouteKey(boardId));
      if (routeKey !== loadedBoardRouteKey) {
        void loadWorkspace(boardId);
      }
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
    const tabParam = String($page.url.searchParams.get("tab") ?? "").trim();
    if (!cardParam && tabParam) {
      navigateBoardCardParam({ tab: "" });
    }
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

    const match = items.find((c) => {
      const membership = c.membership;
      return (
        boardCardStableId(membership) === cardParam ||
        resourceRouteSegment(membership, "card") === cardParam
      );
    });
    if (match) {
      detailModalCard = match;
    } else {
      detailModalCard = null;
      navigateBoardCardParam({ card: "", tab: "" });
    }
  });

  async function handleArchiveBoard() {
    if (!boardId || boardLifecycleBusy || workspace?.board?.trashed_at) return;
    boardLifecycleBusy = true;
    try {
      await coreClient.archiveBoard(boardId, {});
      await loadWorkspace(boardId);
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
      await loadWorkspace(boardId);
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
      await loadWorkspace(boardId);
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

  let docsCount = $derived((workspace?.documents?.items ?? []).length);
  let reviewInboxCount = $derived(enrichedInboxItems.length);
  let boardMoreBadgeCount = $derived(docsCount + reviewInboxCount);

  function toggleSort() {
    sortOpen = !sortOpen;
  }

  /** @param {"rank" | "updated" | "due" | "risk"} mode */
  function setBoardSort(mode) {
    boardSortMode = mode;
    sortOpen = false;
  }

  function toggleFilters() {
    if (!filtersOpen)
      filterDraft = { ...filterApplied, risk: [...filterApplied.risk] };
    filtersOpen = !filtersOpen;
  }

  function applyFilters() {
    filterApplied = { ...filterDraft, risk: [...filterDraft.risk] };
    filtersOpen = false;
  }

  function resetFilters() {
    filterDraft = emptyFilters();
    filterApplied = emptyFilters();
    filtersOpen = false;
  }

  /** @param {string} value */
  function toggleRiskFilter(value) {
    const set = new Set(filterDraft.risk);
    if (set.has(value)) set.delete(value);
    else set.add(value);
    filterDraft = { ...filterDraft, risk: [...set] };
  }

  $effect(() => {
    if (!sortOpen) return;
    function onDocPointerDown(/** @type {PointerEvent} */ e) {
      if (
        sortRoot &&
        e.target instanceof Node &&
        !sortRoot.contains(e.target)
      ) {
        sortOpen = false;
      }
    }
    function onDocKey(/** @type {KeyboardEvent} */ e) {
      if (e.key === "Escape") sortOpen = false;
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

  $effect(() => {
    if (!filtersOpen) return;
    function onDocPointerDown(/** @type {PointerEvent} */ e) {
      if (
        filterRoot &&
        e.target instanceof Node &&
        !filterRoot.contains(e.target)
      ) {
        filtersOpen = false;
      }
    }
    function onDocKey(/** @type {KeyboardEvent} */ e) {
      if (e.key === "Escape") filtersOpen = false;
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

{#if loading && !workspace}
  <div
    class="mt-12 flex items-center justify-center gap-2 text-meta text-fg-muted"
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
  {@const boardWarnings = workspace.warnings?.items ?? []}

  {@const boardFreshness = workspace.projection_freshness}

  {#if board.trashed_at}
    <div
      class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-md border border-danger bg-danger-soft px-3 py-2 text-meta text-danger-text"
    >
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2 font-semibold">
          <span>⚠</span>
          <span>This board is in trash</span>
        </div>
        {#if board.trash_reason}
          <p class="mt-2">Reason: {board.trash_reason}</p>
        {/if}
        <p
          class="mt-1 flex flex-wrap items-center gap-x-1 text-micro text-danger-text"
        >
          <span>Trashed</span>
          {#if board.trashed_by}
            <ActorLabel
              label={actorName(board.trashed_by)}
              seed={board.trashed_by}
              size="xs"
              prefix="by"
              nameClass="text-micro text-danger-text"
            />
          {/if}
          {#if board.trashed_at}
            <span>at {formatTimestamp(board.trashed_at)}</span>
          {/if}
        </p>
      </div>
      <Button
        variant="destructive"
        size="compact"
        disabled={boardLifecycleBusy}
        onclick={handleRestoreBoard}
      >
        {boardLifecycleBusy ? "…" : "Restore"}
      </Button>
    </div>
  {:else if board.archived_at}
    <div
      class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-md border border-warn bg-warn-soft px-3 py-2 text-meta text-warn-text"
    >
      <p class="flex min-w-0 flex-1 flex-wrap items-center gap-x-1">
        <span class="text-warn-text">
          This board was archived on {formatTimestamp(board.archived_at) || "—"}
        </span>
        {#if board.archived_by}
          <ActorLabel
            label={actorName(board.archived_by)}
            seed={board.archived_by}
            size="xs"
            prefix="by"
            nameClass="text-micro text-warn-text"
          />
        {/if}
        <span class="text-warn-text">.</span>
      </p>
      <Button
        variant="secondary"
        size="compact"
        class="border-warn text-warn-text hover:bg-warn-soft"
        disabled={boardLifecycleBusy}
        onclick={handleUnarchiveBoard}
      >
        {boardLifecycleBusy ? "…" : "Unarchive"}
      </Button>
    </div>
  {/if}

  {#snippet boardDesktop()}
    <h1 class="min-w-0 text-subtitle font-semibold text-fg">
      {resourceDisplayLabel(board)}
    </h1>
    {#if String(board.summary ?? "").trim()}
      <p
        class="line-clamp-3 text-[13px] text-fg-muted"
        title={String(board.summary).trim()}
      >
        {String(board.summary).trim()}
      </p>
    {/if}
    <div
      class="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-micro text-fg-muted"
    >
      {#if boardInspectNav && showBoardContextLink}
        <span class="text-fg-muted"
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
        <span class="text-fg-subtle">·</span>
      {/if}
      <span>
        {workspace.board_summary?.card_count ?? workspace.cards?.count ?? 0}
        cards
      </span>
      <span class="text-fg-subtle">·</span>
      <span>
        {workspace.board_summary?.resolved_card_count ?? 0} resolved
      </span>
      <span class="text-fg-subtle">·</span>
      <span>
        {workspace.board_summary?.unresolved_card_count ?? 0} open
      </span>
      <span class="text-fg-subtle">·</span>
      <span>Board updated {formatTimestamp(board.updated_at) || "—"}</span>
      {#if board.owners?.length > 0}
        <span class="text-fg-subtle">·</span>
        <span class="inline-flex flex-wrap items-center gap-1">
          <span class="text-fg-muted">Owners</span>
          {#each board.owners.slice(0, 3) as owner (owner)}
            <ActorLabel
              label={actorName(owner)}
              seed={owner}
              size="xs"
              nameClass="text-micro text-fg-muted"
            />
          {/each}
          {#if board.owners.length > 3}
            <span class="text-micro text-fg-muted"
              >+{board.owners.length - 3}</span
            >
          {/if}
        </span>
      {/if}
    </div>
  {/snippet}

  <div
    class="page-dock-layout page-dock-layout--fixed-mobile-chat page-dock-layout--board-viewport-chat"
  >
    <div class="page-dock-scroll">
      <div class="mb-1 max-md:mb-0.5">
        <WorkspaceResourceTopRow
          breadcrumbAriaLabel="Breadcrumb and board status"
          desktopAriaLabel="Board details"
          dense
          showDesktop={false}
          desktop={boardDesktop}
        >
          {#snippet breadcrumb()}
            <a
              class="shrink-0 text-fg-muted transition-colors hover:text-fg"
              href={workspaceHref("/boards")}>Boards</a
            >
            <span class="shrink-0 text-fg-subtle">/</span>
            <div class="flex min-h-0 min-w-0 flex-1 items-center gap-1.5">
              <span
                class="min-w-0 shrink truncate text-fg-muted"
                aria-current="page">{resourceDisplayLabel(board, boardId)}</span
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
              <div bind:this={sortRoot} class="relative">
                <button
                  type="button"
                  class="inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-micro font-medium transition-colors {hasCustomSort
                    ? 'border-accent bg-accent-soft text-accent-text'
                    : 'border-line bg-transparent text-fg-muted hover:bg-panel-hover hover:text-fg'}"
                  aria-haspopup="menu"
                  aria-expanded={sortOpen}
                  aria-label="Sort cards"
                  onclick={toggleSort}
                  data-testid="board-sort-toggle"
                >
                  <Icon name="sort" class="h-3.5 w-3.5" />
                  {hasCustomSort ? boardSortLabel : "Sort"}
                </button>
                {#if sortOpen}
                  <div
                    class="absolute right-0 z-50 mt-1 min-w-[8.5rem] rounded-md border border-line bg-panel py-1 shadow-lg"
                    role="menu"
                    aria-label="Sort cards"
                    data-testid="board-sort-menu"
                  >
                    {#each BOARD_CARD_SORT_OPTIONS as option (option.value)}
                      <button
                        type="button"
                        role="menuitemradio"
                        aria-checked={boardSortMode === option.value}
                        class="flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left text-micro hover:bg-panel-hover {boardSortMode ===
                        option.value
                          ? 'text-fg'
                          : 'text-fg-muted'}"
                        onclick={() => setBoardSort(option.value)}
                      >
                        {option.label}
                        {#if boardSortMode === option.value}
                          <Icon
                            name="check"
                            class="h-3.5 w-3.5 text-accent-text"
                          />
                        {/if}
                      </button>
                    {/each}
                  </div>
                {/if}
              </div>
              <div bind:this={filterRoot} class="relative">
                <button
                  type="button"
                  class="inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-micro font-medium transition-colors {hasActiveFilters
                    ? 'border-accent bg-accent-soft text-accent-text'
                    : 'border-line bg-transparent text-fg-muted hover:bg-panel-hover hover:text-fg'}"
                  aria-haspopup="dialog"
                  aria-expanded={filtersOpen}
                  onclick={toggleFilters}
                  data-testid="board-filters-toggle"
                >
                  <Icon name="filter" class="h-3.5 w-3.5" />
                  {hasActiveFilters ? "Filtered" : "Filter"}
                </button>
                {#if filtersOpen}
                  <div
                    class="absolute right-0 z-50 mt-1 w-80 rounded-md border border-line bg-panel p-3 shadow-lg"
                    role="dialog"
                    aria-label="Filter cards"
                    data-testid="board-filter-panel"
                  >
                    <div class="space-y-3">
                      <label class="block text-micro">
                        <span class="font-medium text-fg-muted">Search</span>
                        <input
                          bind:value={filterDraft.q}
                          class="mt-1 w-full rounded-md border border-line bg-bg-soft px-2.5 py-1.5 text-meta"
                          placeholder="Title or summary"
                          type="search"
                        />
                      </label>
                      <div class="flex items-end gap-3">
                        <label class="flex-1 text-micro">
                          <span class="font-medium text-fg-muted">Due</span>
                          <select
                            bind:value={filterDraft.dueFilter}
                            class="mt-1 w-full rounded-md border border-line bg-bg-soft px-2.5 py-1.5 text-meta"
                          >
                            <option value="">Any</option>
                            <option value="overdue">Overdue</option>
                            <option value="soon">Due within 7 days</option>
                            <option value="none">No due date</option>
                          </select>
                        </label>
                        <label
                          class="flex cursor-pointer items-center gap-2 pb-1.5 text-meta text-fg"
                        >
                          <input
                            type="checkbox"
                            bind:checked={filterDraft.mineOnly}
                            class="h-3.5 w-3.5 rounded border-line"
                          />
                          My cards
                        </label>
                      </div>
                      <fieldset class="text-micro">
                        <legend class="font-medium text-fg-muted"
                          >Priority</legend
                        >
                        <div class="mt-1.5 flex flex-wrap gap-3">
                          {#each ["critical", "high", "medium", "low"] as risk (risk)}
                            <label
                              class="flex cursor-pointer items-center gap-1.5 capitalize"
                            >
                              <input
                                type="checkbox"
                                checked={filterDraft.risk.includes(risk)}
                                onchange={() => toggleRiskFilter(risk)}
                                class="h-3.5 w-3.5 rounded border-line"
                              />
                              {risk}
                            </label>
                          {/each}
                        </div>
                      </fieldset>
                    </div>
                    <div class="mt-3 flex gap-1.5">
                      <button
                        type="button"
                        class="rounded-md bg-accent px-3 py-1.5 text-micro font-medium text-white"
                        onclick={applyFilters}
                      >
                        Apply
                      </button>
                      <button
                        type="button"
                        class="rounded-md border border-line px-3 py-1.5 text-micro text-fg-muted hover:bg-line-subtle"
                        onclick={resetFilters}
                      >
                        Clear
                      </button>
                    </div>
                  </div>
                {/if}
              </div>
              <ResourceShareMenu
                resourceId={resourceCopyValue("board", board)}
                resourceLabel="board ref"
                rawRecord={board}
              />
              <Button
                variant="primary"
                size="compact"
                class="rounded-md"
                href={workspaceHref(
                  `/boards/${encodeURIComponent(boardRouteSegment)}/cards/new`,
                )}
              >
                Add card
              </Button>
              <div bind:this={boardMoreRoot} class="relative">
                <button
                  type="button"
                  class="relative inline-flex h-7 w-7 cursor-pointer items-center justify-center rounded-md border border-line bg-transparent text-fg-muted transition-colors hover:bg-panel-hover hover:text-fg disabled:cursor-not-allowed disabled:opacity-50"
                  aria-label="More actions"
                  aria-haspopup="menu"
                  aria-expanded={boardMoreOpen}
                  disabled={boardLifecycleBusy}
                  onclick={toggleBoardMore}
                >
                  <Icon name="kebab" class="h-4 w-4" />
                  {#if boardMoreBadgeCount > 0}
                    <span
                      class="absolute -right-1 -top-1 inline-flex h-4 min-w-[1rem] items-center justify-center rounded-full bg-accent px-1 text-[10px] font-semibold leading-none text-white"
                      aria-hidden="true"
                    >
                      {boardMoreBadgeCount > 99 ? "99+" : boardMoreBadgeCount}
                    </span>
                  {/if}
                </button>
                {#if boardMoreOpen}
                  <div
                    class="absolute right-0 z-50 mt-1 max-h-[min(32rem,80vh)] w-80 overflow-y-auto rounded-md border border-line bg-panel shadow-lg"
                    role="menu"
                  >
                    <a
                      role="menuitem"
                      class="block w-full px-3 py-2.5 text-left text-micro font-medium text-fg hover:bg-panel-hover"
                      href={workspaceHref(
                        `/boards/${encodeURIComponent(boardRouteSegment)}/edit`,
                      )}
                      onclick={closeBoardMore}
                    >
                      Board settings
                    </a>

                    <div class="border-t border-line px-3 py-2.5">
                      <div class="flex items-center justify-between">
                        <h3
                          class="text-micro font-semibold uppercase tracking-wide text-fg-muted"
                        >
                          Workspace docs
                        </h3>
                        <span
                          class="rounded bg-line px-1.5 py-0.5 text-[10px] font-semibold tabular-nums text-fg-muted"
                        >
                          {docsCount}
                        </span>
                      </div>
                      {#if docsCount === 0}
                        <p class="mt-1.5 text-micro text-fg-muted">
                          No linked doc lineages yet.
                        </p>
                      {:else}
                        <div class="mt-1.5 space-y-1.5">
                          {#each workspace.documents.items.slice(0, BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT) as document (document.id)}
                            <a
                              class="block rounded border border-line bg-bg-soft px-2.5 py-1.5 text-micro transition-colors hover:border-line-strong"
                              href={workspaceHref(
                                `/docs/${encodeURIComponent(resourceRouteSegment(document, "document"))}`,
                              )}
                              onclick={closeBoardMore}
                            >
                              <div class="truncate font-medium text-fg">
                                {resourceDisplayLabel(document)}
                              </div>
                              <div class="mt-0.5 text-[11px] text-fg-muted">
                                Head v{document.head_revision_number ?? "—"} · Updated
                                {formatTimestamp(document.updated_at) || "—"}
                              </div>
                            </a>
                          {/each}
                        </div>
                        {#if docsCount > BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT}
                          <p class="mt-1.5 text-[11px] text-fg-muted">
                            Showing {BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT} of {docsCount}
                          </p>
                        {/if}
                      {/if}
                    </div>

                    <div class="border-t border-line px-3 py-2.5">
                      <div class="flex items-center justify-between">
                        <h3
                          class="text-micro font-semibold uppercase tracking-wide text-fg-muted"
                        >
                          Review inbox
                        </h3>
                        <span
                          class="rounded bg-line px-1.5 py-0.5 text-[10px] font-semibold tabular-nums text-fg-muted"
                        >
                          {reviewInboxCount}
                        </span>
                      </div>
                      {#if reviewInboxCount === 0}
                        <p class="mt-1.5 text-micro text-fg-muted">
                          No active derived inbox items.
                        </p>
                      {:else}
                        <div class="mt-1.5 space-y-1.5">
                          {#each enrichedInboxItems.slice(0, BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT) as item (item.id)}
                            {@const inboxResourceLine =
                              formatInboxItemBoardPanelResourceLine(item)}
                            <div
                              class="rounded border border-line bg-bg-soft px-2.5 py-1.5"
                            >
                              <div
                                class="truncate text-micro font-medium text-fg"
                              >
                                {item.title ||
                                  item.summary ||
                                  item.ref ||
                                  item.id}
                              </div>
                              <div class="mt-0.5 text-[11px] text-fg-muted">
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
                        {#if reviewInboxCount > BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT}
                          <p class="mt-1.5 text-[11px] text-fg-muted">
                            Showing {BOARD_WORKSPACE_PANEL_PREVIEW_LIMIT} of {reviewInboxCount}
                          </p>
                        {/if}
                      {/if}
                    </div>

                    <div class="border-t border-line py-1">
                      {#if !board.archived_at}
                        <button
                          type="button"
                          role="menuitem"
                          class="block w-full px-3 py-2 text-left text-micro text-fg hover:bg-panel-hover disabled:cursor-not-allowed disabled:opacity-50"
                          disabled={boardLifecycleBusy}
                          onclick={() => {
                            closeBoardMore();
                            confirmModal = { open: true, action: "archive" };
                          }}
                        >
                          Archive
                        </button>
                      {/if}
                      <button
                        type="button"
                        role="menuitem"
                        class="block w-full px-3 py-2 text-left text-micro text-danger-text hover:bg-panel-hover disabled:cursor-not-allowed disabled:opacity-50"
                        disabled={boardLifecycleBusy}
                        onclick={() => {
                          closeBoardMore();
                          confirmModal = { open: true, action: "trash" };
                        }}
                      >
                        Move to trash
                      </button>
                    </div>

                    <div class="border-t border-line p-2">
                      <IdsIntegrityDisclosure
                        rows={[
                          {
                            label: "Board ref",
                            value: resourceCopyValue("board", board),
                            copyLabel: "Copy board ref",
                          },
                        ]}
                        rawJson={JSON.stringify(board, null, 2)}
                        rawJsonCopyLabel="Copy board JSON"
                      />
                    </div>
                  </div>
                {/if}
              </div>
            {/if}
          {/snippet}
        </WorkspaceResourceTopRow>
      </div>

      <!-- Notification alerts -->
      {#if conflictWarning || mutationError}
        <div class="mb-3 space-y-2">
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

      <section class="mb-3">
        <BoardKanban
          {workspace}
          {board}
          {boardId}
          {currentActorId}
          filters={filterApplied}
          sortMode={boardSortMode}
          disabled={mutating || Boolean(board.trashed_at)}
          createCardHref={workspaceHref(
            `/boards/${encodeURIComponent(boardRouteSegment)}/cards/new`,
          )}
          onopencard={openCardDetailModal}
          oncarddrop={handleCardDrop}
          oninlinecreate={handleInlineCreate}
        />
      </section>

      {#if boardWarnings.length > 0}
        <section
          class="mt-4 rounded-md border border-warn bg-warn-soft px-4 py-3"
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
  columnPeerStableIds={cardDetailColumnPeerIds}
  {actorName}
  requestedDetailTab={boardCardDetailTab}
  onDetailTabChange={handleBoardCardDetailTabChange}
  onclose={closeCardDetailModal}
  onmovecard={moveCard}
  onsavecard={saveCardDetails}
  onrevisecard={reviseCardContent}
  onremovecard={removeCard}
/>

<MarkDoneModal
  open={markDoneModal.open}
  cardTitle={boardCardHeaderTitle(
    markDoneModal.cardItem?.membership,
    markDoneModal.cardItem?.backing?.thread,
  )}
  busy={markDoneModal.busy}
  onconfirm={handleMarkDoneConfirm}
  oncancel={() =>
    (markDoneModal = {
      open: false,
      cardItem: null,
      movePayload: null,
      busy: false,
    })}
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
