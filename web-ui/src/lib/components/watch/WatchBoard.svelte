<script>
  /**
   * Watch by stage: columns are placement, which people and agents set.
   * Computed state renders on each card exactly as on the list row.
   *
   * The interaction is the old task board's: a card lifts after a few pixels
   * of mouse travel and follows the pointer, the column under it opens a
   * hole where it will land, neighbours slide out of the way, and on release
   * the card sits in the hole at once while the move is sent. Arrow keys move
   * a focused card a column at a time; Enter opens it.
   *
   * Touch never starts a drag, so every card also has a "Move to" select.
   *
   * Below `md` the board is one column at a time: a stage switcher with
   * counts picks the column, rather than five 16rem columns scrolled
   * sideways behind a narrow window.
   */
  import { tick } from "svelte";
  import { flip } from "svelte/animate";
  import { groupBoard, PLACEMENTS, PLACEMENT_LABELS } from "$lib/brief.js";
  import {
    columnAtPoint,
    insertIndexAtY,
    DRAG_THRESHOLD_PX,
    flipDuration,
  } from "$lib/workBoardDrag.js";
  import WatchRow from "./WatchRow.svelte";

  let {
    initiatives = [],
    now = Date.now(),
    onMove = null,
    /** Refs whose column is still held by an unconfirmed move. */
    held = undefined,
  } = $props();

  /**
   * Where a dropped card sits until the page's rows show that column.
   * The page keeps the column from the outbox until core confirms or the
   * move fails; a brief tick by itself does not put the card back.
   * `token` is the attempt that may settle this landing.
   * @type {{ ref: string, placement: string, before: string, token: number, pending: boolean } | null}
   */
  let landed = $state(null);
  /** Latest attempt per card. An older settle must not clear a newer landing. */
  const settleToken = new Map();
  $effect(() => {
    if (!landed || landed.pending) return;
    const current = initiatives.find((row) => row.ref === landed.ref);
    if (!current || current.placement === landed.placement) {
      landed = null;
      return;
    }
    // The page dropped the hold: the move confirmed into the brief, or it failed.
    if (Array.isArray(held) && !held.includes(landed.ref)) landed = null;
  });

  let columns = $derived.by(() => {
    const groups = groupBoard(
      landed
        ? initiatives.map((row) =>
            row.ref === landed.ref
              ? { ...row, placement: landed.placement }
              : row,
          )
        : initiatives,
    );
    if (!landed) return groups;
    return groups.map((group) => {
      if (group.placement !== landed.placement) return group;
      const moved = group.rows.find((row) => row.ref === landed.ref);
      if (!moved) return group;
      const rest = group.rows.filter((row) => row.ref !== landed.ref);
      const at = landed.before
        ? rest.findIndex((row) => row.ref === landed.before)
        : -1;
      rest.splice(at < 0 ? rest.length : at, 0, moved);
      return { ...group, rows: rest };
    });
  });

  /** Phone: the one stage on screen. Defaults to the first with cards. */
  let chosenStage = $state("");
  let stage = $derived(
    chosenStage ||
      columns.find((column) => column.rows.length)?.placement ||
      PLACEMENTS[0],
  );

  /** @type {{ row: object, pointerId: number, startX: number, startY: number, offsetX: number, offsetY: number, width: number, height: number, target: HTMLElement | null } | null} */
  let drag = $state(null);
  let dragging = $state(false);
  /** @type {{ placement: string, index: number } | null} */
  let hover = $state(null);
  let pointerX = $state(0);
  let pointerY = $state(0);
  let suppressClick = false;

  const PLACEHOLDER = "__watch-drag-hole";

  /** The column's cards, minus the one in the air, plus a hole under it. */
  function slotsFor(column) {
    const slots = column.rows
      .filter((row) => !(dragging && drag?.row.ref === row.ref))
      .map((row) => ({ key: row.ref, row }));
    if (!dragging || hover?.placement !== column.placement) return slots;
    const index = Math.max(0, Math.min(hover.index, slots.length));
    slots.splice(index, 0, { key: PLACEHOLDER, placeholder: true });
    return slots;
  }

  function releasePointer(session) {
    const id = session?.pointerId;
    const target = session?.target;
    if (target && typeof id === "number" && target.hasPointerCapture?.(id)) {
      try {
        target.releasePointerCapture(id);
      } catch {
        // Already released after a cancel or a navigation.
      }
    }
  }

  function stop() {
    releasePointer(drag);
    window.removeEventListener("pointermove", onPointerMove, true);
    window.removeEventListener("pointerup", onPointerUp, true);
    window.removeEventListener("pointercancel", onPointerCancel, true);
    window.removeEventListener("dragstart", preventNativeDrag, true);
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

  function preventNativeDrag(event) {
    // Links drag natively; a browser drag cancels the pointer session.
    event.preventDefault();
  }

  function onPointerDown(event, row) {
    // A horizontal flick scrolls the board. Touch placement uses Move to.
    if (event.pointerType === "touch") return;
    if (event.button != null && event.button !== 0) return;
    if (event.target instanceof Element) {
      const control = event.target.closest("a, select, button, label");
      // The title link is the card's grip; any other control is not.
      if (control && !control.hasAttribute("data-watch-title")) return;
    }
    const slot = event.currentTarget;
    const rect =
      slot instanceof HTMLElement ? slot.getBoundingClientRect() : null;
    drag = {
      row,
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      offsetX: rect ? event.clientX - rect.left : 0,
      offsetY: rect ? event.clientY - rect.top : 0,
      width: rect?.width || 256,
      height: rect?.height || 96,
      target: slot instanceof HTMLElement ? slot : null,
    };
    pointerX = event.clientX;
    pointerY = event.clientY;
    window.addEventListener("pointermove", onPointerMove, {
      capture: true,
      passive: false,
    });
    window.addEventListener("pointerup", onPointerUp, true);
    window.addEventListener("pointercancel", onPointerCancel, true);
    window.addEventListener("dragstart", preventNativeDrag, true);
  }

  function onPointerMove(event) {
    if (!drag || event.pointerId !== drag.pointerId) return;
    if (
      !dragging &&
      Math.hypot(event.clientX - drag.startX, event.clientY - drag.startY) <
        DRAG_THRESHOLD_PX
    )
      return;
    if (!dragging) {
      dragging = true;
      window.getSelection?.()?.removeAllRanges?.();
      try {
        drag.target?.setPointerCapture?.(event.pointerId);
      } catch {
        // Optional; the window listeners still see the pointer.
      }
    }
    event.preventDefault();
    pointerX = event.clientX;
    pointerY = event.clientY;
    updateHover(event.clientX, event.clientY);
  }

  function onPointerCancel(event) {
    if (dragging) {
      onPointerUp(event);
      return;
    }
    stop();
  }

  function onPointerUp(event) {
    const session = drag;
    if (!session || !dragging) {
      stop();
      return;
    }
    updateHover(event.clientX, event.clientY);
    const target = hover;
    suppressClick = true;
    setTimeout(() => (suppressClick = false), 0);
    if (!target) {
      stop();
      return;
    }
    const peers = (
      columns.find((c) => c.placement === target.placement)?.rows ?? []
    ).filter((r) => r.ref !== session.row.ref);
    // The hole is visual only. Watch order inside a column is attention
    // order, so the move itself always appends.
    const before = peers[target.index]?.ref ?? "";
    const from = columns.find((c) => c.placement === session.row.placement);
    const fromIndex = (from?.rows ?? []).findIndex(
      (r) => r.ref === session.row.ref,
    );
    stop();
    if (
      target.placement === session.row.placement &&
      target.index === fromIndex
    )
      return;
    commit(session.row, target.placement, before);
  }

  /**
   * Land the card where it was dropped, then send the move. The page owns
   * the move itself; if it declines (Done needs evidence, a confirm was
   * cancelled, the save failed) the card goes back where the brief has it.
   * Only this attempt's settle may clear the landing — a slower, older
   * request must not snap a newer drop back.
   */
  async function commit(row, placement, before) {
    // Order inside a column is attention order, computed; only the stage
    // is a person's to set. The move appends; `before` is the visual hole.
    if (!onMove || placement === row.placement) return;
    const token = (settleToken.get(row.ref) ?? 0) + 1;
    settleToken.set(row.ref, token);
    landed = { ref: row.ref, placement, before, token, pending: true };
    let accepted = false;
    try {
      accepted = (await onMove(row, placement, "")) === true;
    } catch {
      accepted = false;
    }
    if (settleToken.get(row.ref) !== token) return;
    if (!accepted) {
      if (landed?.token === token) landed = null;
      return;
    }
    await tick();
    if (settleToken.get(row.ref) !== token || landed?.token !== token) return;
    const current = initiatives.find((item) => item.ref === row.ref);
    if (current?.placement === placement) {
      landed = null;
      return;
    }
    if (Array.isArray(held) && !held.includes(row.ref)) {
      landed = null;
      return;
    }
    landed = { ...landed, pending: false };
  }

  function swallowClick(event) {
    if (!suppressClick) return;
    event.preventDefault();
    event.stopPropagation();
  }

  function onCardKey(event, row) {
    if (event.repeat) {
      if (event.key === "ArrowLeft" || event.key === "ArrowRight")
        event.preventDefault();
      return;
    }
    if (event.target !== event.currentTarget) return;
    if (event.key === "Enter") {
      const link = event.currentTarget.querySelector("a[data-watch-title]");
      if (link) {
        event.preventDefault();
        link.click();
      }
      return;
    }
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    const index = PLACEMENTS.indexOf(row.placement);
    const next =
      PLACEMENTS[
        event.key === "ArrowRight"
          ? Math.min(PLACEMENTS.length - 1, index + 1)
          : Math.max(0, index - 1)
      ];
    if (!next || next === row.placement) return;
    const ref = row.ref;
    void commit(row, next, "").then(async () => {
      await tick();
      const active = document.activeElement;
      if (
        active &&
        active !== document.body &&
        active.matches("input, textarea, select, [contenteditable='true']")
      )
        return;
      document
        .querySelector(
          `[data-work-slot][data-work-ref="${globalThis.CSS?.escape?.(ref) ?? ref.replace(/["\\]/g, "\\$&")}"]`,
        )
        ?.focus();
    });
  }
</script>

<div
  class="-mt-1 mb-3 flex gap-0.5 rounded-md border border-line bg-bg-soft p-0.5 md:hidden"
  role="group"
  aria-label="Stage"
  data-watch-stage-switcher
>
  {#each columns as column (column.placement)}
    <button
      type="button"
      class="flex min-h-11 min-w-0 flex-auto flex-col items-center justify-center rounded px-1 py-1 leading-tight transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent {stage ===
      column.placement
        ? 'bg-panel-hover text-fg shadow-sm'
        : 'text-fg-muted'}"
      aria-pressed={stage === column.placement}
      data-watch-stage-tab={column.placement}
      onclick={() => (chosenStage = column.placement)}
    >
      <span class="text-body font-medium tabular-nums"
        >{column.rows.length}</span
      >
      <span class="max-w-full truncate text-[11px] tracking-tight"
        >{column.label}</span
      >
    </button>
  {/each}
</div>
<!-- Scroll regions must be keyboard-focusable for horizontal navigation. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div
  class="board-scroll relative -mx-1 flex gap-3 overflow-x-auto px-1 pb-3 pt-0.5 {dragging
    ? 'select-none'
    : ''}"
  data-watch-board
  data-dragging={dragging ? "" : undefined}
  onclickcapture={swallowClick}
  role="region"
  aria-label="Initiatives by stage"
  tabindex="0"
>
  <p id="watch-board-help" class="sr-only">
    Drag an initiative between stages, or focus it and press the left or right
    arrow to move it. Enter opens it. Each card also has a Move to menu.
  </p>
  {#each columns as column (column.placement)}
    {@const slots = slotsFor(column)}
    <section
      class="column flex w-64 shrink-0 flex-col rounded-md bg-bg-soft p-2 max-md:w-full xl:w-auto xl:min-w-[11.5rem] xl:flex-1 xl:basis-0 {stage ===
      column.placement
        ? ''
        : 'max-md:hidden'}"
      class:column--over={dragging && hover?.placement === column.placement}
      data-work-phase-column
      data-work-phase={column.placement}
      data-watch-column={column.placement}
      aria-label={column.label}
    >
      <!-- On a phone the stage switcher above names the column. -->
      <div
        class="mb-1.5 flex items-baseline justify-between gap-2 px-1 py-1 max-md:sr-only"
      >
        <h2 class="ui-label mb-0 truncate">{column.label}</h2>
        <span class="shrink-0 text-micro tabular-nums text-fg-subtle"
          >{column.rows.length}</span
        >
      </div>
      <div class="flex min-h-[4.5rem] flex-1 flex-col gap-2">
        {#each slots as slot (slot.key)}
          {@const row = slot.row}
          <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
          <div
            class={slot.placeholder
              ? "hole rounded-md"
              : `slot rounded-md outline-none focus-visible:ring-2 focus-visible:ring-accent ${
                  landed?.pending && landed.ref === row.ref ? "is-saving" : ""
                }`}
            aria-busy={landed?.pending && landed.ref === row?.ref
              ? "true"
              : undefined}
            data-work-slot
            data-work-ref={row?.ref}
            data-placeholder={slot.placeholder ? "" : undefined}
            aria-hidden={slot.placeholder ? "true" : undefined}
            style={slot.placeholder
              ? `height: ${drag?.height ?? 96}px`
              : undefined}
            tabindex={slot.placeholder ? undefined : 0}
            role={slot.placeholder ? "presentation" : "group"}
            aria-label={row?.title}
            aria-keyshortcuts={slot.placeholder
              ? undefined
              : "ArrowLeft ArrowRight Enter"}
            aria-describedby={slot.placeholder ? undefined : "watch-board-help"}
            animate:flip={{ duration: flipDuration }}
            onpointerdown={slot.placeholder
              ? undefined
              : (event) => onPointerDown(event, row)}
            onkeydown={slot.placeholder
              ? undefined
              : (event) => onCardKey(event, row)}
          >
            {#if row}
              <WatchRow initiative={row} variant="card" {now}>
                {#snippet footer()}
                  <label
                    class="move flex items-center justify-end gap-1.5 text-micro text-fg-subtle"
                  >
                    <span aria-hidden="true">Move to</span>
                    <select
                      class="min-w-0 cursor-pointer rounded-sm border border-line bg-bg-soft px-1 py-0.5 text-micro text-fg-muted max-md:min-h-11 max-md:px-2 max-md:text-meta hover:border-line-strong hover:text-fg focus-visible:outline focus-visible:outline-1 focus-visible:outline-accent"
                      value={row.placement}
                      aria-label="Move {row.title} to"
                      onchange={(event) => {
                        const to = event.currentTarget.value;
                        event.currentTarget.value = row.placement;
                        if (to && to !== row.placement)
                          void commit(row, to, "");
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
            {/if}
          </div>
        {:else}
          <p
            class="rounded-md border border-dashed border-line-subtle px-2 py-5 text-center text-micro text-fg-subtle"
          >
            Nothing here
          </p>
        {/each}
      </div>
    </section>
  {/each}
</div>
{#if dragging && drag}
  <div
    class="pointer-events-none fixed left-0 top-0 z-[100] rotate-[1deg] rounded-md shadow-xl ring-1 ring-line-strong"
    data-watch-drag-overlay
    aria-hidden="true"
    style="width: {drag.width}px; transform: translate3d({pointerX -
      drag.offsetX}px, {pointerY - drag.offsetY}px, 0)"
  >
    <WatchRow initiative={drag.row} variant="card" {now} />
  </div>
{/if}

<style>
  /*
   * The horizontal scrollbar is the only cue that columns continue off
   * screen below the width where all five fit; the 6px overlay bar the rest
   * of the app uses is invisible here.
   */
  .board-scroll {
    scrollbar-width: auto;
  }
  .board-scroll::-webkit-scrollbar {
    height: 10px;
  }
  .board-scroll::-webkit-scrollbar-thumb {
    background: var(--line-strong);
    border-radius: 999px;
  }
  .board-scroll::-webkit-scrollbar-track {
    background: var(--bg-soft);
    border-radius: 999px;
  }
  .column--over {
    box-shadow: inset 0 0 0 1px
      color-mix(in srgb, var(--accent-solid) 55%, transparent);
  }
  .hole {
    border: 1px dashed color-mix(in srgb, var(--accent-solid) 60%, transparent);
    background: color-mix(in srgb, var(--accent-solid) 8%, transparent);
  }
  /* Landed where it was dropped; the move is still on its way to core. */
  .is-saving {
    opacity: 0.7;
    transition: opacity 150ms ease;
  }
  .board-scroll:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: 2px;
  }
  /* Grab only where a mouse can drag; a phone scrolls. */
  @media (hover: hover) and (pointer: fine) {
    .slot {
      cursor: grab;
    }
    [data-dragging] .slot {
      cursor: grabbing;
    }
    /*
     * The move menu is for touch and keyboard; a mouse has the drag. It
     * floats over the card's bottom corner on hover or focus rather than
     * reserving a line on every card.
     */
    .slot {
      position: relative;
    }
    .slot :global(.move) {
      position: absolute;
      right: 8px;
      bottom: 8px;
      padding: 2px 2px 2px 8px;
      border-radius: 6px;
      background: var(--panel-hover);
      box-shadow: -8px 0 8px var(--panel-hover);
      opacity: 0;
      pointer-events: none;
      transition: opacity 120ms ease;
    }
    .slot:hover :global(.move),
    .slot:focus-within :global(.move) {
      opacity: 1;
      pointer-events: auto;
    }
  }
</style>
