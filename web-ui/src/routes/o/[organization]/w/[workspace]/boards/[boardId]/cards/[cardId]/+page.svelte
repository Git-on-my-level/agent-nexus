<script>
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
  import CardDetailModalInner from "$lib/components/CardDetailModalInner.svelte";
  import { coreClient } from "$lib/coreClient";
  import { workspacePath } from "$lib/workspacePaths";
  import { readEnumSearchParam } from "$lib/urlState";

  let { data } = $props();

  const CARD_PAGE_TABS = ["overview", "resolution", "timeline", "revisions"];

  let workspace = $state(null);
  let loading = $state(false);
  let error = $state("");
  let mutationNotice = $state("");
  let mutationError = $state("");
  let conflictWarning = $state("");

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
      items.find((item) => boardCardStableId(item?.membership) === cardId) ??
      null
    );
  });

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

  function workspaceHref(pathname = "/") {
    return workspacePath(organizationSlug, workspaceSlug, pathname);
  }

  async function closeCardPage() {
    await goto(workspaceHref(`/boards/${encodeURIComponent(boardId)}`));
  }

  async function loadWorkspace() {
    loading = true;
    error = "";
    try {
      workspace = await coreClient.getBoardWorkspace(boardId);
    } catch (e) {
      error = `Failed to load card: ${e instanceof Error ? e.message : String(e)}`;
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

      mutationError = formatMutationError("Card write failed", e);
    }
  }

  async function moveCard(cardItem, payload, successMessage) {
    if (!workspace?.board) return;

    const id = boardCardStableId(cardItem.membership);
    const nextPayload = {
      if_board_updated_at: workspace.board.updated_at,
      ...payload,
    };
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

  $effect(() => {
    if (workspaceSlug && boardId) {
      void loadWorkspace();
    }
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
  <div
    class="rounded-md border border-[var(--line)] bg-[var(--panel)] px-4 py-6"
  >
    <p class="text-meta font-medium text-[var(--fg)]">Card not found.</p>
    <a
      class="mt-3 inline-flex rounded-md border border-[var(--line)] px-3 py-1.5 text-meta text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] hover:text-[var(--fg)]"
      href={workspaceHref(`/boards/${encodeURIComponent(boardId)}`)}
    >
      Back to board
    </a>
  </div>
{/if}
