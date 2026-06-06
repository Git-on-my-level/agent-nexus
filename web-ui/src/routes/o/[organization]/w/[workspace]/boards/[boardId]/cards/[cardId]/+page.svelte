<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import {
    actorRegistry,
    lookupActorDisplayName,
    principalRegistry,
  } from "$lib/actorSession";
  import {
    boardCardStableId,
    sortedColumnPeersStableIds,
  } from "$lib/boardUtils";
  import { normalizeBoardMovePlacementAnchors } from "$lib/boardCardMove.js";
  import CardDetailModalInner from "$lib/components/CardDetailModalInner.svelte";
  import { coreClient } from "$lib/coreClient";
  import { resourceRouteSegment } from "$lib/resourceIdentity.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { readEnumSearchParam } from "$lib/urlState";

  let { data } = $props();

  const CARD_PAGE_TABS = ["overview", "resolution", "timeline", "revisions"];

  let workspace = $state(null);
  let loading = $state(false);
  let error = $state("");
  // On desktop the standalone card page is redundant with (and worse than) the
  // board's card modal, so we redirect to the board's `?card=` deep-link. The
  // dedicated route stays the canonical shareable link and the real experience
  // on mobile, where there is no modal.
  let redirectingToBoardModal = $state(false);
  let mutationNotice = $state("");
  let mutationError = $state("");
  let conflictWarning = $state("");
  let cardLoadRequestId = 0;
  let loadedCardRouteKey = $state("");

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let boardId = $derived($page.params.boardId);
  let cardId = $derived($page.params.cardId);

  let cardPageDetailTab = $derived(
    readEnumSearchParam($page.url.searchParams, "tab", CARD_PAGE_TABS, ""),
  );

  function handleCardPageDetailTabChange(tab) {
    const t = String(tab ?? "").trim();
    const u = new URL($page.url.href);
    if (t === "overview" || t === "") {
      u.searchParams.delete("tab");
    } else if (CARD_PAGE_TABS.includes(t)) {
      u.searchParams.set("tab", t);
    }
    void goto(`${u.pathname}${u.search}`, {
      replaceState: true,
      keepFocus: true,
      noScroll: true,
    });
  }

  let selectedCard = $derived.by(() => {
    const items = workspace?.cards?.items ?? [];
    return (
      items.find((item) => {
        const membership = item?.membership;
        return (
          boardCardStableId(membership) === cardId ||
          resourceRouteSegment(membership, "card") === cardId
        );
      }) ?? null
    );
  });
  let boardRouteSegment = $derived(
    resourceRouteSegment(workspace?.board, "board") || boardId,
  );

  let mobileCardDetailColumnPeers = $derived.by(() => {
    const ws = workspace;
    const card = selectedCard;
    const b = ws?.board;
    if (!ws?.cards || !card?.membership || !b) return [];
    return sortedColumnPeersStableIds(
      ws.cards,
      b.column_schema,
      card.membership.column_key,
    );
  });

  function actorName(id) {
    return lookupActorDisplayName(id, $actorRegistry, $principalRegistry);
  }

  let workspaceHref = $derived(
    bindWorkspaceHref(organizationSlug, workspaceSlug),
  );

  async function closeCardPage() {
    await goto(
      workspaceHref(`/boards/${encodeURIComponent(boardRouteSegment)}`),
    );
  }

  function cardRouteKey(targetBoardId = boardId, targetCardId = cardId) {
    return [organizationSlug, workspaceSlug, targetBoardId, targetCardId].map(
      (v) => String(v ?? ""),
    );
  }

  function serializeCardRouteKey(routeKey) {
    return routeKey.join("\u001f");
  }

  function isCurrentCardLoad(requestId, routeKey) {
    const current = cardRouteKey();
    return (
      requestId === cardLoadRequestId &&
      routeKey.every((segment, index) => segment === current[index])
    );
  }

  async function loadWorkspace(targetBoardId = boardId, targetCardId = cardId) {
    if (!targetBoardId || !targetCardId) return;
    const requestId = ++cardLoadRequestId;
    const routeKey = cardRouteKey(targetBoardId, targetCardId);
    const serializedRouteKey = serializeCardRouteKey(routeKey);
    const routeChanged =
      loadedCardRouteKey && loadedCardRouteKey !== serializedRouteKey;
    loading = true;
    error = "";
    loadedCardRouteKey = serializedRouteKey;
    if (
      routeChanged ||
      String(workspace?.board?.id ?? "") !== String(targetBoardId) ||
      boardCardStableId(selectedCard?.membership) !== String(targetCardId)
    ) {
      workspace = null;
      mutationNotice = "";
      mutationError = "";
      conflictWarning = "";
    }
    try {
      const loadedWorkspace = await coreClient.getBoardWorkspace(targetBoardId);
      if (!isCurrentCardLoad(requestId, routeKey)) return;
      workspace = loadedWorkspace;
    } catch (e) {
      if (!isCurrentCardLoad(requestId, routeKey)) return;
      error = `Failed to load card: ${e instanceof Error ? e.message : String(e)}`;
      workspace = null;
    } finally {
      if (isCurrentCardLoad(requestId, routeKey)) {
        loading = false;
      }
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
    await loadWorkspace(boardId, cardId);
  }

  async function runBoardMutation(action, successMessage) {
    clearMutationMessages();

    try {
      await action();
      await loadWorkspace(boardId, cardId);
      mutationNotice = successMessage;
    } catch (e) {
      if (e?.status === 409) {
        await handleBoardConflict();
        return;
      }

      mutationError = formatMutationError("Card write failed", e);
    }
  }

  async function moveCard(cardItem, payload, successMessage) {
    if (!workspace?.board) return;

    const id = boardCardStableId(cardItem.membership);
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
        clearMutationMessages();
        mutationError =
          "Cannot move card to Done: add at least one artifact or event resolution ref first.";
        return;
      }
      delete nextPayload.resolution;
      if (incomingRefs.length === 0 && mergedRefs.length > 0) {
        nextPayload.resolution_refs = mergedRefs;
      }
    }
    await runBoardMutation(
      () => coreClient.moveBoardCard(boardId, id, nextPayload),
      successMessage,
    );
  }

  async function saveCardDetails(cardItem, patch) {
    if (!workspace?.board) return;

    const id = boardCardStableId(cardItem.membership);
    const ifUpdatedAt = String(cardItem.membership?.updated_at ?? "").trim();
    if (!ifUpdatedAt) {
      mutationError =
        "Cannot save card details: missing card timestamp. Refresh the card and try again.";
      return;
    }
    await runBoardMutation(
      () =>
        coreClient.updateBoardCard(boardId, id, {
          if_updated_at: ifUpdatedAt,
          patch,
        }),
      "Card details updated.",
    );
  }

  async function reviseCardContent(cardItem, payload) {
    const id = boardCardStableId(cardItem.membership);
    await runBoardMutation(
      () => coreClient.updateCardContent(id, payload),
      "Card revision saved.",
    );
  }

  async function removeCard(cardItem) {
    if (!workspace?.board) return;

    const id = boardCardStableId(cardItem.membership);
    await runBoardMutation(
      () =>
        coreClient.removeBoardCard(boardId, id, {
          if_board_updated_at: workspace.board.updated_at,
        }),
      "Card removed.",
    );
    await closeCardPage();
  }

  function isDesktopViewport() {
    return Boolean(
      browser && window.matchMedia?.("(min-width: 1024px)")?.matches,
    );
  }

  $effect(() => {
    if (!browser || !boardId || !cardId) return;
    if (!isDesktopViewport()) return;
    redirectingToBoardModal = true;
    const params = new URLSearchParams();
    params.set("card", cardId);
    const tab = String($page.url.searchParams.get("tab") ?? "").trim();
    if (tab) params.set("tab", tab);
    const base = workspaceHref(`/boards/${encodeURIComponent(boardId)}`);
    void goto(`${base}?${params.toString()}`, { replaceState: true });
  });

  $effect(() => {
    if (redirectingToBoardModal || isDesktopViewport()) return;
    if (workspaceSlug && boardId && cardId) {
      const routeKey = serializeCardRouteKey(cardRouteKey(boardId, cardId));
      if (routeKey !== loadedCardRouteKey) {
        void loadWorkspace(boardId, cardId);
      }
    }
  });
</script>

{#if redirectingToBoardModal}
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
    Opening card...
  </div>
{:else if loading}
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
    Loading card...
  </div>
{:else if error}
  <div
    class="mb-4 rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
  >
    {error}
  </div>
{:else if workspace && selectedCard}
  {#if mutationNotice || conflictWarning || mutationError}
    <div class="mb-3 space-y-2">
      {#if mutationNotice}
        <div class="rounded-md bg-ok-soft px-3 py-2 text-micro text-ok-text">
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

  <CardDetailModalInner
    cardItem={selectedCard}
    {boardId}
    board={workspace.board ?? null}
    {workspaceSlug}
    workspaceId={data?.workspaceId ?? ""}
    primaryTopic={workspace.primary_topic ?? null}
    columnPeerStableIds={mobileCardDetailColumnPeers}
    {actorName}
    requestedDetailTab={cardPageDetailTab}
    onDetailTabChange={handleCardPageDetailTabChange}
    onclose={closeCardPage}
    onmovecard={moveCard}
    onsavecard={saveCardDetails}
    onrevisecard={reviseCardContent}
    onremovecard={removeCard}
    presentation="page"
  />
{:else}
  <div class="rounded-md border border-line bg-panel px-4 py-6">
    <p class="text-meta font-medium text-fg">Card not found.</p>
    <a
      class="mt-3 inline-flex rounded-md border border-line px-3 py-1.5 text-meta text-fg-muted transition-colors hover:bg-line-subtle hover:text-fg"
      href={workspaceHref(`/boards/${encodeURIComponent(boardRouteSegment)}`)}
    >
      Back to board
    </a>
  </div>
{/if}
