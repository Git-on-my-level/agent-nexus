<script>
  import BoardCard from "$lib/components/BoardCard.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import {
    boardCardStableId,
    boardColumnTitle,
    groupBoardWorkspaceCards,
  } from "$lib/boardUtils";
  import {
    beforeCardIdForInsert,
    computeDropPlacement,
    dropAfterCardIdAtFullInsert,
    dropBeforeCardIdAtFullInsert,
    filterBoardCardItems,
    sortColumnCards,
  } from "$lib/boardCardMove.js";

  let {
    workspace,
    board,
    boardId = "",
    currentActorId = "",
    disabled = false,
    filters = { q: "", mineOnly: false, dueFilter: "", risk: [] },
    sortMode = "rank",
    onopencard = () => {},
    oncarddrop = async () => {},
    oninlinecreate = async () => {},
    createCardHref = "",
  } = $props();

  const DRAG_THRESHOLD_PX = 6;
  /** @type {{ cardId: string, cardItem: object, pointerId: number, startX: number, startY: number } | null} */
  let dragSession = $state(null);
  /** @type {{ columnKey: string, fullInsertIndex: number, insertIndex: number } | null} */
  let dropTarget = $state(null);
  let dragGhost = $state({ x: 0, y: 0, visible: false });
  let pointerMoved = $state(false);
  let suppressClick = $state(false);
  /** @type {Record<string, boolean>} */
  let composingColumn = $state({});
  /** @type {Record<string, string>} */
  let composeTitle = $state({});
  /** @type {Record<string, boolean>} */
  let composeBusy = $state({});
  let a11yMessage = $state("");

  const columnSchema = $derived(board?.column_schema ?? []);
  const filteredItems = $derived(
    filterBoardCardItems(
      workspace?.cards?.items ?? [],
      filters,
      currentActorId,
    ),
  );
  const cardsByColumn = $derived(
    groupBoardWorkspaceCards({ items: filteredItems }, columnSchema),
  );
  const boardIsEmpty = $derived((workspace?.cards?.items ?? []).length === 0);

  /** @param {string} columnKey @returns {object[]} */
  function sortedColumnCards(columnKey) {
    return sortColumnCards(cardsByColumn[columnKey] ?? [], sortMode);
  }

  /** @param {string} columnKey @returns {string[]} */
  function displayedColumnIds(columnKey) {
    return sortedColumnCards(columnKey)
      .map((c) => boardCardStableId(c.membership))
      .filter(Boolean);
  }

  /** @param {string[]} a @param {string[]} b */
  function sameOrder(a, b) {
    if (a.length !== b.length) return false;
    return a.every((v, i) => v === b[i]);
  }

  /** @param {PointerEvent} e @param {object} cardItem */
  function handleCardPointerDown(e, cardItem) {
    if (disabled) return;
    if (e.button != null && e.button !== 0) return;
    const cardId = boardCardStableId(cardItem?.membership);
    if (!cardId) return;
    dragSession = {
      cardId,
      cardItem,
      pointerId: e.pointerId,
      startX: e.clientX,
      startY: e.clientY,
    };
    pointerMoved = false;
    window.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", handlePointerUp);
    window.addEventListener("pointercancel", handlePointerCancel);
  }

  /** @param {PointerEvent} e */
  function handlePointerMove(e) {
    if (!dragSession || e.pointerId !== dragSession.pointerId) return;
    const dx = e.clientX - dragSession.startX;
    const dy = e.clientY - dragSession.startY;
    if (!pointerMoved && Math.hypot(dx, dy) < DRAG_THRESHOLD_PX) {
      return;
    }
    if (!pointerMoved) {
      pointerMoved = true;
      // Prevent text selection once a real drag starts.
      window.getSelection?.()?.removeAllRanges?.();
    }
    e.preventDefault();
    dragGhost = { x: e.clientX, y: e.clientY, visible: true };
    updateDropTarget(e.clientX, e.clientY);
  }

  function teardownDragListeners() {
    window.removeEventListener("pointermove", handlePointerMove);
    window.removeEventListener("pointerup", handlePointerUp);
    window.removeEventListener("pointercancel", handlePointerCancel);
  }

  function handlePointerCancel() {
    teardownDragListeners();
    dragSession = null;
    dropTarget = null;
    dragGhost = { x: 0, y: 0, visible: false };
    pointerMoved = false;
  }

  /**
   * Resolve which column the pointer is over using bounding-box geometry.
   * Geometry is immune to overlays (e.g. the drag ghost) that can otherwise
   * intercept `document.elementFromPoint`, which previously made drops no-op.
   * @param {number} x @param {number} y
   * @returns {HTMLElement | null}
   */
  function columnBodyAtPoint(x, y) {
    const bodies = /** @type {HTMLElement[]} */ ([
      ...document.querySelectorAll("[data-board-column-body]"),
    ]);
    let fallback = /** @type {HTMLElement | null} */ (null);
    for (const body of bodies) {
      const rect = body.getBoundingClientRect();
      if (rect.width === 0 && rect.height === 0) continue;
      const withinX = x >= rect.left && x <= rect.right;
      if (!withinX) continue;
      if (y >= rect.top && y <= rect.bottom) return body;
      // Pointer is in this column horizontally but above/below the body
      // (e.g. over the header). Treat it as this column as a fallback.
      fallback = body;
    }
    return fallback;
  }

  /** @param {number} x @param {number} y */
  function updateDropTarget(x, y) {
    if (!dragSession) {
      dropTarget = null;
      return;
    }
    const columnEl = columnBodyAtPoint(x, y);
    if (!(columnEl instanceof HTMLElement)) {
      dropTarget = null;
      return;
    }
    const columnKey = columnEl.getAttribute("data-column-key") ?? "";
    if (!columnKey) {
      dropTarget = null;
      return;
    }
    const { fullInsertIndex, peerInsertIndex } = computeDropPlacement(
      columnEl,
      y,
      dragSession.cardId,
    );
    dropTarget = {
      columnKey,
      fullInsertIndex,
      insertIndex: peerInsertIndex,
    };
  }

  /** @param {PointerEvent} e */
  async function handlePointerUp(e) {
    teardownDragListeners();

    if (
      dragSession &&
      pointerMoved &&
      typeof e?.clientX === "number" &&
      typeof e?.clientY === "number"
    ) {
      updateDropTarget(e.clientX, e.clientY);
    }

    const session = dragSession;
    const target = dropTarget;
    const moved = pointerMoved;
    dragSession = null;
    dropTarget = null;
    dragGhost = { x: 0, y: 0, visible: false };
    pointerMoved = false;

    if (!session || !moved || !target) {
      return;
    }

    suppressClick = true;
    window.setTimeout(() => {
      suppressClick = false;
    }, 0);

    const fromColumn = String(
      session.cardItem?.membership?.column_key ?? "",
    ).trim();
    const toColumn = target.columnKey;

    const peers = displayedColumnIds(toColumn).filter(
      (id) => id !== session.cardId,
    );
    const insertIndex = Math.max(0, Math.min(target.insertIndex, peers.length));
    const beforeCardId = beforeCardIdForInsert(peers, insertIndex);

    if (fromColumn === toColumn) {
      const currentOrder = displayedColumnIds(fromColumn);
      const nextOrder = [
        ...peers.slice(0, insertIndex),
        session.cardId,
        ...peers.slice(insertIndex),
      ];
      if (sameOrder(currentOrder, nextOrder)) {
        return;
      }
    }

    await oncarddrop(session.cardItem, {
      column_key: toColumn,
      before_card_id: beforeCardId,
      from_column: fromColumn,
    });

    const colTitle = boardColumnTitle(toColumn, columnSchema);
    a11yMessage = `Card moved to ${colTitle}`;
  }

  /** @param {object} cardItem */
  function handleCardClick(cardItem) {
    if (suppressClick || dragGhost.visible) return;
    onopencard(cardItem);
  }

  /** @param {string} columnKey */
  function startCompose(columnKey) {
    composingColumn = { ...composingColumn, [columnKey]: true };
    composeTitle = { ...composeTitle, [columnKey]: "" };
  }

  /** @param {string} columnKey */
  function cancelCompose(columnKey) {
    composingColumn = { ...composingColumn, [columnKey]: false };
    composeTitle = { ...composeTitle, [columnKey]: "" };
  }

  /** @param {string} columnKey */
  async function submitCompose(columnKey) {
    const title = String(composeTitle[columnKey] ?? "").trim();
    if (!title || composeBusy[columnKey]) return;
    composeBusy = { ...composeBusy, [columnKey]: true };
    try {
      await oninlinecreate({ title, column_key: columnKey });
      cancelCompose(columnKey);
    } finally {
      composeBusy = { ...composeBusy, [columnKey]: false };
    }
  }

  /** @param {KeyboardEvent} e @param {object} cardItem @param {string} columnKey @param {number} index @param {object[]} columnCards */
  async function handleCardKeyboardMove(
    e,
    cardItem,
    columnKey,
    index,
    columnCards,
  ) {
    if (disabled) return;
    const cols = columnSchema.map((c) => c.key);
    const colIdx = cols.indexOf(columnKey);
    if (colIdx < 0) return;
    const cardId = boardCardStableId(cardItem.membership);
    const peers = displayedColumnIds(columnKey).filter((id) => id !== cardId);

    if (e.key === "ArrowUp" && index > 0) {
      e.preventDefault();
      await oncarddrop(cardItem, {
        column_key: columnKey,
        before_card_id: beforeCardIdForInsert(peers, index - 1),
        from_column: columnKey,
      });
    } else if (e.key === "ArrowDown" && index < columnCards.length - 1) {
      e.preventDefault();
      await oncarddrop(cardItem, {
        column_key: columnKey,
        before_card_id: beforeCardIdForInsert(peers, index + 1),
        from_column: columnKey,
      });
    } else if (e.key === "ArrowLeft" && colIdx > 0) {
      e.preventDefault();
      await oncarddrop(cardItem, {
        column_key: cols[colIdx - 1],
        before_card_id: "",
        from_column: columnKey,
      });
    } else if (e.key === "ArrowRight" && colIdx < cols.length - 1) {
      e.preventDefault();
      await oncarddrop(cardItem, {
        column_key: cols[colIdx + 1],
        before_card_id: "",
        from_column: columnKey,
      });
    }
  }
</script>

<div class="board-kanban" data-testid="board-kanban">
  <div class="sr-only" aria-live="polite">{a11yMessage}</div>

  {#if boardIsEmpty}
    <StateEmpty
      title="No cards on this board yet"
      helper="Add a card to get started, or drag work into columns as it progresses."
      actionLabel="Create card"
      actionHref={createCardHref}
      class="!border-dashed"
    />
  {:else}
    <div
      class="flex gap-3 overflow-x-auto pb-4"
      data-testid="board-kanban-lane"
    >
      {#each columnSchema as column (column.key)}
        {@const columnKey = column.key}
        {@const cards = sortedColumnCards(columnKey)}
        {@const isBlocked = columnKey === "blocked"}
        {@const wipLimit = column.wip_limit}
        {@const overWip =
          wipLimit != null && wipLimit > 0 && cards.length > wipLimit}
        <div
          class="flex min-w-[260px] max-w-[320px] flex-1 flex-col rounded-md border border-line bg-bg-soft"
          data-board-column
          data-column-key={columnKey}
        >
          <div class="flex items-center gap-2 border-b border-line px-3 py-2.5">
            <div class="flex min-w-0 flex-1 items-center gap-2">
              <h3
                class="truncate text-micro font-semibold uppercase tracking-wide {isBlocked &&
                cards.length > 0
                  ? 'text-warn-text'
                  : 'text-fg-muted'}"
              >
                {column.title || boardColumnTitle(columnKey, columnSchema)}
              </h3>
              <span
                class="min-w-[1.25rem] rounded px-1.5 py-0.5 text-center text-micro font-semibold tabular-nums {overWip
                  ? 'bg-danger-soft text-danger-text'
                  : isBlocked && cards.length > 0
                    ? 'bg-warn-soft text-warn-text'
                    : 'bg-line text-fg-muted'}"
                title={wipLimit != null && wipLimit > 0
                  ? `WIP limit ${wipLimit}`
                  : undefined}
              >
                {cards.length}{#if wipLimit != null && wipLimit > 0}/{wipLimit}{/if}
              </span>
            </div>

            <button
              type="button"
              class="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded text-fg-muted hover:bg-line-subtle hover:text-fg"
              aria-label={`Add card to ${column.title || columnKey}`}
              onclick={() => startCompose(columnKey)}
            >
              <span class="text-lg leading-none">+</span>
            </button>
          </div>

          <div
            class="board-column-body flex flex-1 flex-col"
            data-board-column-body
            data-column-key={columnKey}
            style="max-height: calc(100vh - 240px); min-height: 120px;"
          >
            <div
              class="flex-1 space-y-2 overflow-y-auto px-2 py-2 {dropTarget?.columnKey ===
                columnKey && cards.length === 0
                ? 'rounded-md ring-2 ring-accent/40'
                : ''}"
            >
              {#if cards.length === 0}
                <div
                  class="flex items-center justify-center rounded-md border border-dashed border-line px-3 py-10 text-micro text-fg-muted {dropTarget?.columnKey ===
                  columnKey
                    ? 'border-accent bg-accent-soft/20 text-fg'
                    : ''}"
                >
                  {dropTarget?.columnKey === columnKey
                    ? "Drop here"
                    : "No cards"}
                </div>
              {:else}
                <ul class="space-y-2" role="list">
                  {#each cards as cardItem, cardIndex (boardCardStableId(cardItem.membership))}
                    {@const cardId = boardCardStableId(cardItem.membership)}
                    {@const isDragging = dragSession?.cardId === cardId}
                    {@const showDropBefore =
                      dropTarget?.columnKey === columnKey &&
                      !isDragging &&
                      dropBeforeCardIdAtFullInsert(
                        cards,
                        dropTarget?.fullInsertIndex ?? -1,
                      ) === cardId}
                    {@const showDropAfter =
                      dropTarget?.columnKey === columnKey &&
                      dropAfterCardIdAtFullInsert(
                        cards,
                        dropTarget?.fullInsertIndex ?? -1,
                      ) === cardId}
                    <li>
                      <BoardCard
                        {cardItem}
                        {boardId}
                        dragging={isDragging}
                        dropBefore={showDropBefore}
                        dropAfter={showDropAfter}
                        onclick={() => handleCardClick(cardItem)}
                        ondragstart={(e) => handleCardPointerDown(e, cardItem)}
                        onboardkeydown={(e) =>
                          void handleCardKeyboardMove(
                            e,
                            cardItem,
                            columnKey,
                            cardIndex,
                            cards,
                          )}
                      />
                    </li>
                  {/each}
                </ul>
              {/if}
            </div>

            <div class="shrink-0 border-t border-line px-2 py-2">
              {#if composingColumn[columnKey]}
                <form
                  class="space-y-1.5"
                  onsubmit={(e) => {
                    e.preventDefault();
                    void submitCompose(columnKey);
                  }}
                >
                  <!-- svelte-ignore a11y_autofocus -->
                  <input
                    bind:value={composeTitle[columnKey]}
                    class="w-full rounded-md border border-line bg-panel px-2 py-1.5 text-meta focus:outline-none focus:ring-1 focus:ring-accent"
                    placeholder="Card title"
                    autofocus
                  />
                  <div class="flex gap-1">
                    <button
                      type="submit"
                      class="rounded-md bg-accent px-2 py-1 text-micro font-medium text-white disabled:opacity-50"
                      disabled={composeBusy[columnKey]}
                    >
                      {composeBusy[columnKey] ? "Adding…" : "Add"}
                    </button>
                    <button
                      type="button"
                      class="rounded-md px-2 py-1 text-micro text-fg-muted hover:bg-line-subtle"
                      onclick={() => cancelCompose(columnKey)}
                    >
                      Cancel
                    </button>
                  </div>
                </form>
              {:else}
                <button
                  type="button"
                  class="flex w-full items-center gap-1 rounded-md px-1.5 py-1 text-micro text-fg-muted transition-colors hover:bg-line-subtle hover:text-fg"
                  onclick={() => startCompose(columnKey)}
                >
                  <span class="text-base leading-none">+</span>
                  Add card
                </button>
              {/if}
            </div>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

{#if dragGhost.visible && dragSession}
  <div
    class="pointer-events-none fixed z-[100] -translate-x-1/2 -translate-y-1/2 rounded-md border border-accent bg-panel px-3 py-2 text-meta font-medium text-fg shadow-lg"
    style="left: {dragGhost.x}px; top: {dragGhost.y}px;"
  >
    {String(dragSession.cardItem?.membership?.title ?? "Card").trim() || "Card"}
  </div>
{/if}

<style>
  .board-column-body {
    min-height: 0;
  }
</style>
