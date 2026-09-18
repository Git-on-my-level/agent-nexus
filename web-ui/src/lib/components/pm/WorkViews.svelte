<script>
  import { tick } from "svelte";
  import { flip } from "svelte/animate";
  import WorkCard from "./WorkCard.svelte";
  import SignalBadge from "./SignalBadge.svelte";
  import ActorLabel from "$lib/components/ActorLabel.svelte";
  import {
    actorDisplayLabel,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import {
    phaseGroups,
    label,
    PHASE_LABELS,
    isNexusOwned,
    workKey,
    taskDetailPath,
    workFreshness,
    sortWorkBoardItems,
  } from "$lib/pm/presentation.js";
  import { formatTimestamp, formatAbsoluteDateTime } from "$lib/formatDate";
  import {
    DRAG_THRESHOLD_PX,
    columnAtPoint,
    columnSlots,
    flipDuration,
    insertIndexAtY,
  } from "$lib/workBoardDrag.js";
  let {
    records = [],
    view = "table",
    workspaceHref,
    now = Date.now(),
    requested = {},
    requestedDecisions = {},
    boardTitles = {},
    // The records are one page of the list; a column count is then a lower
    // bound, and says so the way the table header does.
    truncated = false,
    onMove,
  } = $props();
  let groups = $derived(
    phaseGroups(records)
      .filter(
        (group) =>
          group.items.length ||
          ["backlog", "in_progress", "blocked", "review", "done"].includes(
            group.key,
          ),
      )
      .map((group) =>
        view === "board"
          ? { ...group, items: sortWorkBoardItems(group.items) }
          : group,
      ),
  );
  const href = (work) => workspaceHref(taskDetailPath(work));
  let focusedKey = $state("");

  /**
   * Status is a dot plus its name. Only the two states a reader has to act on
   * — blocked and done — are loud enough to earn a badge; badging all six made
   * "Backlog" shout as loudly as "Blocked".
   */
  const DOT_CLASS = {
    in_progress: "bg-accent",
    review: "bg-accent",
    ready: "bg-fg-muted",
  };
  function dotClass(phase) {
    return DOT_CLASS[phase] ?? "bg-line-strong";
  }
  /**
   * A source can report a state Nexus has no name for. Printing the raw token
   * ("vendor_waiting") at a reader is worse than printing the source's own
   * words, so an unfamiliar phase shows `native_status` when the source sent
   * one. The status column used to carry both on two lines.
   */
  function statusText(work) {
    const phase = work?.phase || "unknown";
    if (PHASE_LABELS[phase]) return PHASE_LABELS[phase];
    return String(work?.source?.native_status ?? "").trim() || label(phase);
  }
  function badgeTone(phase) {
    if (phase === "blocked") return "warn";
    if (phase === "done") return "ok";
    return "";
  }

  function boardLabel(work) {
    const ref = String(work?.board_ref ?? "").trim();
    return boardTitles[ref] || ref.replace(/^board:/, "") || "—";
  }

  /**
   * The last time we read this task from its source. A task Nexus owns has no
   * source to read, so it says so rather than reporting "never checked" as if
   * something were wrong — and never in the internal vocabulary ("Nexus-owned").
   */
  function lastChecked(work, tick = now) {
    // `tick` is the page's 30s clock: naming it here is what makes the
    // relative labels recompute when it advances.
    void tick;
    const observed = work?.freshness?.last_observed_at;
    if (observed) {
      return {
        text: formatTimestamp(observed),
        title: formatAbsoluteDateTime(observed),
        datetime: observed,
        muted: false,
      };
    }
    if (isNexusOwned(work)) {
      return { text: "created here", title: "", datetime: "", muted: true };
    }
    return { text: "never", title: "", datetime: "", muted: true };
  }

  const ARROW_SKIPPED_PHASES = new Set(["blocked", "cancelled"]);
  function orderedPhases() {
    return groups.map((group) => group.key);
  }

  /** @type {{ work: object, key: string, pointerId: number, startX: number, startY: number, offsetX: number, offsetY: number, width: number, height: number, x: number, y: number, fromPhase: string, target: HTMLElement | null } | null} */
  let drag = $state(null);
  /** @type {{ phase: string, index: number } | null} */
  let hover = $state(null);
  let pointerMoved = $state(false);
  // After release, keep the hole until the parent list matches it. Clearing
  // drag first restores the origin column for a frame, then the drop lands —
  // the whole board flashes.
  let landing = $state(false);
  let suppressClick = $state(false);
  let pointerX = $state(0);
  let pointerY = $state(0);

  function slotsFor(group) {
    if (!drag || !pointerMoved) {
      return group.items.map((work) => ({ key: workKey(work), work }));
    }
    return columnSlots(group.items, drag.key, group.key, hover);
  }

  function teardownDragListeners() {
    window.removeEventListener("pointermove", handlePointerMove, true);
    window.removeEventListener("pointerup", handlePointerUp, true);
    window.removeEventListener("pointercancel", handlePointerCancel, true);
    window.removeEventListener("dragstart", handleCardDragStart, true);
  }

  function releasePointer(session) {
    const id = session?.pointerId;
    const target = session?.target;
    if (target && typeof id === "number" && target.hasPointerCapture?.(id)) {
      try {
        target.releasePointerCapture(id);
      } catch {
        // Capture is already gone after pointercancel or navigation.
      }
    }
  }

  function clearDrag() {
    releasePointer(drag);
    teardownDragListeners();
    drag = null;
    hover = null;
    pointerMoved = false;
    landing = false;
  }

  function commitDrop(session, phase, index) {
    onMove(session.work, phase, { index, pointer: true });
  }

  function slotFlipDuration(distance) {
    if (drag && pointerMoved && !landing) return flipDuration(distance);
    return 0;
  }

  function updateHover(x, y) {
    if (!drag) {
      hover = null;
      return;
    }
    const columnEl = columnAtPoint(x, y);
    if (!(columnEl instanceof HTMLElement)) {
      hover = null;
      return;
    }
    const phase = columnEl.getAttribute("data-work-phase") || "";
    if (!phase) {
      hover = null;
      return;
    }
    hover = { phase, index: insertIndexAtY(columnEl, y, drag.key) };
  }

  /** @param {PointerEvent} event @param {object} work */
  function handleCardPointerDown(event, work) {
    if (event.button != null && event.button !== 0) return;
    if (
      event.target instanceof HTMLElement &&
      event.target.closest("a[href]")
    ) {
      const cardLink = event.currentTarget.querySelector("a[href]");
      if (event.target.closest("a[href]") !== cardLink) return;
    }
    const slot = event.currentTarget;
    if (!(slot instanceof HTMLElement)) return;
    const rect = slot.getBoundingClientRect();
    drag = {
      work,
      key: workKey(work),
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      offsetX: event.clientX - rect.left,
      offsetY: event.clientY - rect.top,
      width: rect.width,
      height: rect.height,
      x: event.clientX,
      y: event.clientY,
      fromPhase: work.phase || "unknown",
      target: slot,
    };
    pointerX = event.clientX;
    pointerY = event.clientY;
    pointerMoved = false;
    hover = null;
    window.addEventListener("pointermove", handlePointerMove, {
      capture: true,
      passive: false,
    });
    window.addEventListener("pointerup", handlePointerUp, true);
    window.addEventListener("pointercancel", handlePointerCancel, true);
    window.addEventListener("dragstart", handleCardDragStart, true);
  }

  /** @param {DragEvent} event */
  function handleCardDragStart(event) {
    // Links are draggable by default. If the browser starts an HTML5 drag,
    // it fires pointercancel and our pointer session dies with no drop.
    event.preventDefault();
  }

  /** @param {PointerEvent} event */
  function handlePointerMove(event) {
    if (!drag || event.pointerId !== drag.pointerId) return;
    const dx = event.clientX - drag.startX;
    const dy = event.clientY - drag.startY;
    if (!pointerMoved && Math.hypot(dx, dy) < DRAG_THRESHOLD_PX) return;
    if (!pointerMoved) {
      pointerMoved = true;
      window.getSelection?.()?.removeAllRanges?.();
      try {
        drag.target?.setPointerCapture(event.pointerId);
      } catch {
        // Capture is optional; window listeners still see bubbling pointer events.
      }
    }
    event.preventDefault();
    pointerX = event.clientX;
    pointerY = event.clientY;
    updateHover(event.clientX, event.clientY);
  }

  /** @param {PointerEvent} event */
  function handlePointerCancel(event) {
    if (drag && pointerMoved) {
      handlePointerUp(event);
      return;
    }
    clearDrag();
  }

  /** @param {PointerEvent} event */
  function handlePointerUp(event) {
    if (drag && pointerMoved) {
      updateHover(event.clientX, event.clientY);
    }
    const session = drag;
    const target = hover;
    const moved = pointerMoved;
    if (!session || !moved) {
      clearDrag();
      return;
    }
    suppressClick = true;
    window.setTimeout(() => {
      suppressClick = false;
    }, 0);
    const phase = target?.phase;
    const fromIndex = (
      groups.find((group) => group.key === session.fromPhase)?.items || []
    ).findIndex((item) => workKey(item) === session.key);
    if (
      !phase ||
      !onMove ||
      (phase === session.fromPhase && target.index === fromIndex)
    ) {
      clearDrag();
      return;
    }
    // Confirm() must not sit under a frozen overlay. Nexus-owned drops are
    // optimistic: hold the hole until records catch up, then drop the ghost.
    if (!isNexusOwned(session.work)) {
      clearDrag();
      commitDrop(session, phase, target.index);
      return;
    }
    releasePointer(session);
    teardownDragListeners();
    landing = true;
    try {
      commitDrop(session, phase, target.index);
    } finally {
      void tick().then(() => {
        clearDrag();
      });
    }
  }

  /** @param {MouseEvent} event */
  function handleCardClickCapture(event) {
    if (!suppressClick) return;
    event.preventDefault();
    event.stopPropagation();
  }

  function handleCardKey(event, work) {
    if (event.key === "Enter") {
      // Only act when the card container itself has focus; a focused inner
      // link (task or Requested badge) activates natively.
      if (event.target !== event.currentTarget) return;
      const link = event.currentTarget.querySelector("a[href]");
      if (link) {
        event.preventDefault();
        link.click();
      }
      return;
    }
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    const current = work.phase || "unknown";
    // Blocked and cancelled are states, not steps: an arrow walks the
    // workflow and never files someone an obligation by accident. A card
    // already in such a state can still step out of it.
    const keys = orderedPhases().filter(
      (key) => !ARROW_SKIPPED_PHASES.has(key) || key === current,
    );
    const index = keys.indexOf(current);
    const nextIndex =
      event.key === "ArrowRight"
        ? Math.min(keys.length - 1, Math.max(0, index) + 1)
        : Math.max(0, (index < 0 ? 0 : index) - 1);
    const nextPhase = keys[nextIndex];
    if (!nextPhase || nextPhase === current || !onMove) return;
    const ref = work.ref;
    // The card re-renders in another column; keep the keyboard on it so a
    // second arrow press moves it again instead of dropping focus on body.
    Promise.resolve(onMove(work, nextPhase)).then(async () => {
      await tick();
      // A move can open a form (evidence for Done); the field it focused
      // keeps the keyboard, otherwise typing would land on the card.
      const active = document.activeElement;
      if (
        active &&
        active !== document.body &&
        active.matches("input, textarea, select, [contenteditable='true']")
      )
        return;
      document
        .querySelector(`[data-work-ref="${CSS.escape(ref)}"][tabindex="0"]`)
        ?.focus();
    });
  }
</script>

{#if view === "board"}
  <!-- Scroll regions must be keyboard-focusable for horizontal navigation. -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div
    class="board-scroll flex gap-3 overflow-x-auto pb-3 {drag && pointerMoved
      ? 'select-none'
      : ''}"
    role="region"
    aria-label="Task board grouped by phase"
    tabindex="0"
  >
    <p id="task-board-card-help" class="sr-only">
      Drag a task between phases, or focus it and press the left or right arrow
      to move it. Enter opens the task. Moving a task that lives in another
      tracker asks for confirmation and files a request for you to approve.
    </p>
    {#each groups as group (group.key)}
      {@const slots = slotsFor(group)}
      <section
        class="w-72 shrink-0 rounded-md bg-bg-soft p-2 xl:w-auto xl:min-w-[10.5rem] xl:flex-1 {hover?.phase ===
        group.key
          ? 'ring-1 ring-accent/40'
          : ''}"
        aria-label={group.label}
        data-work-phase-column
        data-work-phase={group.key}
      >
        <div class="mb-2 flex items-baseline justify-between gap-2 px-1 py-1">
          <h2 class="ui-label mb-0 truncate">{group.label}</h2>
          <span
            class="shrink-0 text-micro tabular-nums text-fg-subtle"
            title={truncated ? "At least; not every task is loaded" : undefined}
            >{group.items.length}{truncated ? "+" : ""}</span
          >
        </div>
        <div class="flex min-h-[4.5rem] flex-col gap-2">
          {#each slots as slot (slot.key)}
            {@const work = slot.work}
            {@const key = slot.key}
            {@const decisionId = work ? requestedDecisions[key] : ""}
            <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
            <div
              class="work-card-slot outline-none {focusedKey === key
                ? 'ring-1 ring-accent'
                : ''}"
              data-work-slot
              data-work-ref={work ? workKey(work) : undefined}
              data-placeholder={slot.placeholder ? "" : undefined}
              tabindex={slot.placeholder ? undefined : 0}
              role={slot.placeholder ? "presentation" : "group"}
              aria-hidden={slot.placeholder ? "true" : undefined}
              aria-label={work ? work.title || "Untitled task" : undefined}
              aria-keyshortcuts={slot.placeholder
                ? undefined
                : "ArrowLeft ArrowRight Enter"}
              aria-describedby={slot.placeholder
                ? undefined
                : "task-board-card-help"}
              animate:flip={{ duration: slotFlipDuration }}
              style={slot.placeholder
                ? `height: ${drag?.height ?? 72}px`
                : undefined}
              onpointerdown={slot.placeholder
                ? undefined
                : (event) => handleCardPointerDown(event, work)}
              ondragstart={handleCardDragStart}
              onclickcapture={handleCardClickCapture}
              onfocus={slot.placeholder ? undefined : () => (focusedKey = key)}
              onkeydown={slot.placeholder
                ? undefined
                : (event) => handleCardKey(event, work)}
            >
              {#if !slot.placeholder}
                <WorkCard
                  {work}
                  href={href(work)}
                  boardTitle={boardLabel(work)}
                  requested={Boolean(requested[key] || decisionId)}
                  requestedHref={decisionId
                    ? workspaceHref(
                        `/inbox?item=decision:${encodeURIComponent(decisionId)}`,
                      )
                    : ""}
                />
              {/if}
            </div>
          {:else}
            <p class="px-2 py-6 text-center text-micro text-fg-subtle">
              Nothing here
            </p>
          {/each}
        </div>
      </section>
    {/each}
  </div>
  {#if drag && pointerMoved}
    <div
      class="pointer-events-none fixed left-0 top-0 z-[100] rounded-md shadow-lg ring-1 ring-line-strong"
      data-work-drag-overlay
      aria-hidden="true"
      style="width: {drag.width}px; transform: translate3d({pointerX -
        drag.offsetX}px, {pointerY - drag.offsetY}px, 0)"
    >
      <WorkCard
        work={drag.work}
        href={href(drag.work)}
        boardTitle={boardLabel(drag.work)}
        requested={Boolean(requested[drag.key] || requestedDecisions[drag.key])}
      />
    </div>
  {/if}
{:else}
  <!-- Scroll regions must be keyboard-focusable for horizontal navigation. -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div
    class="wide-scroll overflow-x-auto rounded-md border border-line"
    role="region"
    aria-label="Task table"
    tabindex="0"
  >
    <!--
      One DOM for both widths. Below 640px the same rows collapse into the
      two-line row the Inbox uses (title, then `board · status · time`) — a
      four-column table that scrolls sideways on a phone is a table nobody
      reads, and a second markup for mobile is a second thing to keep true.
    -->
    <table class="work-table w-full border-collapse text-left text-meta">
      <caption class="sr-only"
        >Tracked tasks with board, status, owner and when each was last checked</caption
      >
      <thead class="text-micro uppercase tracking-wide text-fg-subtle">
        <tr>
          {#each ["Task", "Board", "Status", "Owner", "Last checked"] as heading}
            <th
              scope="col"
              class="whitespace-nowrap border-b border-line px-3 py-2 font-semibold"
              >{heading}</th
            >
          {/each}
        </tr>
      </thead>
      <tbody class="divide-y divide-line-subtle bg-panel">
        {#each records as work (workKey(work))}
          {@const checked = lastChecked(work, now)}
          {@const read = workFreshness(work, now)}
          {@const tone = badgeTone(work.phase)}
          <tr
            class="h-10 align-middle hover:bg-panel-hover"
            data-work-ref={work.ref}
          >
            <th scope="row" class="min-w-56 max-w-96 px-3 py-1.5 font-normal">
              <a
                class="block truncate font-medium text-fg hover:text-accent-text"
                href={href(work)}>{work.title || "Untitled task"}</a
              >
              {#if work.next_actor || work.next_action}
                <p class="truncate text-micro text-fg-muted">
                  {[work.next_actor, work.next_action]
                    .filter(Boolean)
                    .join(" — ")}
                </p>
              {/if}
              <span class="work-row-meta text-micro text-fg-muted">
                <span class="min-w-0 flex-1 truncate"
                  >{boardLabel(work)} · {statusText(work)}</span
                >
                {#if read.key === "error"}
                  <SignalBadge tone="warn">{read.label}</SignalBadge>
                {/if}
                <span class="shrink-0 tabular-nums">{checked.text}</span>
              </span>
            </th>
            <td class="max-w-40 px-3 py-1.5">
              <span class="block truncate text-fg-muted"
                >{boardLabel(work)}</span
              >
            </td>
            <!-- A source can report a status of any length; capped and
                 truncated so one verbose one cannot push Last checked off
                 the right edge of the table for every row. -->
            <td class="max-w-48 px-3 py-1.5">
              {#if tone}
                <SignalBadge {tone}>{statusText(work)}</SignalBadge>
              {:else}
                <span
                  class="flex items-center gap-1.5 text-fg-muted"
                  title={statusText(work)}
                >
                  <span
                    class="h-1.5 w-1.5 shrink-0 rounded-full {dotClass(
                      work.phase,
                    )}"
                    aria-hidden="true"
                  ></span>
                  <span class="truncate">{statusText(work)}</span>
                </span>
              {/if}
            </td>
            <td class="max-w-40 overflow-hidden px-3 py-1.5">
              {#if work.owner}
                <ActorLabel
                  class="max-w-full"
                  label={actorDisplayLabel(
                    work.owner,
                    $actorRegistry,
                    $principalRegistry,
                  )}
                  seed={work.owner}
                  size="xs"
                  nameClass="text-meta text-fg truncate"
                />
              {:else}
                <span class="text-fg-subtle">—</span>
              {/if}
            </td>
            <td class="whitespace-nowrap px-3 py-1.5">
              {#if read.key === "error"}
                <SignalBadge tone="warn">{read.label}</SignalBadge>
                <span class="ml-1.5 text-fg-subtle">{checked.text}</span>
              {:else if checked.datetime}
                <time
                  class="tabular-nums text-fg-muted"
                  datetime={checked.datetime}
                  title={checked.title}>{checked.text}</time
                >
              {:else}
                <span class="text-fg-subtle">{checked.text}</span>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<style>
  /* Two-line rows below 640px: same rows, no second markup. */
  .work-row-meta {
    display: none;
  }

  @media (max-width: 639px) {
    .work-table,
    .work-table tbody,
    .work-table tr,
    .work-table th[scope="row"] {
      display: block;
    }

    .work-table thead,
    .work-table td {
      display: none;
    }

    .work-table tr {
      height: auto;
    }

    .work-table th[scope="row"] {
      max-width: none;
      min-width: 0;
      padding: 8px 12px;
    }

    .work-row-meta {
      display: flex;
      align-items: center;
      gap: 8px;
      min-width: 0;
    }
  }

  /*
   * The horizontal scrollbar is the only cue that columns continue off screen
   * — on the board, and on the table between 640px and the width where all
   * five columns fit; the 6px overlay bar the rest of the app uses is
   * invisible here.
   */
  .board-scroll,
  .wide-scroll {
    scrollbar-width: auto;
  }

  .board-scroll::-webkit-scrollbar,
  .wide-scroll::-webkit-scrollbar {
    height: 10px;
  }

  .board-scroll::-webkit-scrollbar-thumb,
  .wide-scroll::-webkit-scrollbar-thumb {
    background: var(--line-strong);
    border-radius: 999px;
  }

  .board-scroll::-webkit-scrollbar-track,
  .wide-scroll::-webkit-scrollbar-track {
    background: var(--bg-soft);
    border-radius: 999px;
  }

  .work-card-slot {
    cursor: grab;
    touch-action: none;
  }

  .board-scroll.select-none :global(.work-card-slot) {
    cursor: grabbing;
  }
</style>
