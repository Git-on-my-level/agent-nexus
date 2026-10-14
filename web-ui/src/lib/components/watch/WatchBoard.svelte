<script>
  /**
   * Watch by stage: columns are placement, which people and agents set.
   * Computed state renders on each card exactly as on the list row.
   * Pointer drag mirrors the tasks columns; every card also has a
   * "Move to" select so a move never needs a drag.
   */
  import { groupBoard, PLACEMENTS, PLACEMENT_LABELS } from "$lib/brief.js";
  import {
    columnAtPoint,
    insertIndexAtY,
    DRAG_THRESHOLD_PX,
  } from "$lib/workBoardDrag.js";
  import WatchRow from "./WatchRow.svelte";

  let { initiatives = [], now = Date.now(), onMove = null } = $props();

  let columns = $derived(groupBoard(initiatives));
  /** @type {{ row: object, pointerId: number, startX: number, startY: number } | null} */
  let drag = $state(null);
  let dragging = $state(false);
  let hover = $state(null);
  let suppressClick = false;

  function stop() {
    window.removeEventListener("pointermove", onPointerMove, true);
    window.removeEventListener("pointerup", onPointerUp, true);
    window.removeEventListener("pointercancel", onPointerCancel, true);
    drag = null;
    dragging = false;
    hover = null;
  }

  function updateHover(x, y) {
    const columnEl = columnAtPoint(x, y);
    const placement = columnEl?.getAttribute("data-work-phase") || "";
    hover =
      columnEl && placement
        ? { placement, index: insertIndexAtY(columnEl, y, drag?.row.ref) }
        : null;
  }

  function onPointerDown(event, row) {
    if (event.button != null && event.button !== 0) return;
    if (
      event.target instanceof HTMLElement &&
      event.target.closest("a, select, button")
    )
      return;
    drag = {
      row,
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
    };
    window.addEventListener("pointermove", onPointerMove, {
      capture: true,
      passive: false,
    });
    window.addEventListener("pointerup", onPointerUp, true);
    window.addEventListener("pointercancel", onPointerCancel, true);
  }

  function onPointerMove(event) {
    if (!drag || event.pointerId !== drag.pointerId) return;
    if (
      !dragging &&
      Math.hypot(event.clientX - drag.startX, event.clientY - drag.startY) <
        DRAG_THRESHOLD_PX
    )
      return;
    dragging = true;
    event.preventDefault();
    updateHover(event.clientX, event.clientY);
  }

  function onPointerCancel() {
    stop();
  }

  function onPointerUp(event) {
    const session = drag;
    const wasDragging = dragging;
    if (session && wasDragging) {
      updateHover(event.clientX, event.clientY);
      const target = hover;
      suppressClick = true;
      setTimeout(() => (suppressClick = false), 0);
      if (target) {
        const peers = (
          columns.find((c) => c.placement === target.placement)?.rows ?? []
        ).filter((r) => r.ref !== session.row.ref);
        const before = peers[target.index]?.ref ?? "";
        stop();
        onMove?.(session.row, target.placement, before);
        return;
      }
    }
    stop();
  }

  function swallowClick(event) {
    if (!suppressClick) return;
    event.preventDefault();
    event.stopPropagation();
  }
</script>

<div
  class="flex gap-3 overflow-x-auto pb-3"
  data-watch-board
  onclickcapture={swallowClick}
  role="presentation"
>
  {#each columns as column (column.placement)}
    <section
      class="w-64 shrink-0 rounded-md bg-panel-hover/40 p-2 {dragging &&
      hover?.placement === column.placement
        ? 'ring-2 ring-accent'
        : ''}"
      data-work-phase-column
      data-work-phase={column.placement}
      data-watch-column={column.placement}
      aria-label={column.label}
    >
      <h2 class="mb-2 text-meta font-semibold text-fg">
        {column.label}
        <span class="text-fg-muted">({column.rows.length})</span>
      </h2>
      <div class="space-y-2">
        {#each column.rows as row (row.ref)}
          <div
            data-work-slot
            data-work-ref={row.ref}
            class="touch-pan-y {dragging && drag?.row.ref === row.ref
              ? 'opacity-50'
              : ''}"
            onpointerdown={(event) => onPointerDown(event, row)}
            role="presentation"
          >
            <WatchRow initiative={row} variant="card" {now}>
              {#snippet footer()}
                <label
                  class="mt-1.5 flex items-center gap-1 text-micro text-fg-muted"
                >
                  Move to
                  <select
                    class="min-w-0 rounded border border-border bg-panel px-1 py-0.5 text-micro text-fg"
                    value={row.placement}
                    aria-label="Move {row.title} to"
                    onchange={(event) => {
                      const to = event.currentTarget.value;
                      event.currentTarget.value = row.placement;
                      if (to && to !== row.placement) onMove?.(row, to, "");
                    }}
                  >
                    {#each PLACEMENTS as placement (placement)}
                      <option value={placement}
                        >{PLACEMENT_LABELS[placement]}</option
                      >
                    {/each}
                  </select>
                </label>
              {/snippet}
            </WatchRow>
          </div>
        {/each}
      </div>
    </section>
  {/each}
</div>
