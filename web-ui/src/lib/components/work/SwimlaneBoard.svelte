<script>
  /**
   * The swimlane board: one lane per top-level initiative, its children in
   * state columns, and an Ungrouped tray of new cards above.
   *
   * Reads: the brief (lanes, rollups, loops, budget, fold suggestions, shared
   * with the nav badge) and one bounded `GET /work/ungrouped` page, re-read
   * when the brief refreshes. Nothing is read per lane.
   *
   * Writes go through the device outbox: group, fold, and their undos. A
   * mouse can drag a tray row onto a lane; every drag has a button or a
   * select that does the same, and touch only uses those.
   *
   * Below `md` lanes stack as collapsible sections and the tray opens as a
   * sheet.
   */
  import { onMount, tick } from "svelte";
  import { briefStore, mountBrief, refreshBrief } from "$lib/briefStore.js";
  import { countWaitingDecisions } from "$lib/brief.js";
  import { coreClient } from "$lib/coreClient";
  import { localWorkScope } from "$lib/localWorkScope.js";
  import { mutationOutbox } from "$lib/mutationOutbox.js";
  import { durableForm } from "$lib/actions/durableForm.js";
  import { dismissOnEscape } from "$lib/actions/dismissOnEscape.js";
  import { DRAG_THRESHOLD_PX } from "$lib/workBoardDrag.js";
  import {
    budgetLabel,
    buildLanes,
    CELL_PREVIEW,
    foldSuggestions,
    hiddenChildren,
    LANE_COLUMNS,
    laneCells,
    overBudget,
    progressSegments,
    trayRows,
  } from "$lib/swimlanes.js";
  import {
    enqueueGrouping,
    groupingDraft,
    groupingTitle,
    GROUPING_DRAFT_TARGET,
    isGrouping,
  } from "$lib/workGrouping.js";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import SwimlaneTray from "./SwimlaneTray.svelte";

  let { workspaceHref, now = Date.now(), boardHref = "" } = $props();

  const TRAY_LIMIT = 20;

  const store = briefStore();
  let brief = $derived($store.data);
  let briefFailed = $derived(Boolean($store.error));

  let lanes = $derived(buildLanes(brief?.initiatives));
  let loops = $derived(brief?.loops ?? []);
  let budget = $derived(brief?.initiativeBudget ?? null);
  let waitingDecisions = $derived(countWaitingDecisions(brief?.decisions));
  let summary = $derived.by(() => {
    const inside = lanes.reduce((sum, lane) => sum + lane.rollup.total, 0);
    const more = brief?.truncated.initiatives ? "+" : "";
    return [
      `${lanes.length}${more} ${lanes.length === 1 ? "initiative" : "initiatives"}`,
      `${inside} ${inside === 1 ? "card" : "cards"} inside them`,
      loops.length
        ? `${loops.length} ${loops.length === 1 ? "loop" : "loops"}`
        : "",
    ]
      .filter(Boolean)
      .join(" · ");
  });
  const href = (ref) =>
    workspaceHref(`/initiatives/${encodeURIComponent(ref)}`);

  // ---- Tray ---------------------------------------------------------------
  let tray = $state({ rows: [], hasMore: false, loaded: false, error: "" });
  let trayTicket = 0;
  async function loadTray() {
    const ticket = ++trayTicket;
    const scope = $localWorkScope;
    try {
      const result = await coreClient.listUngroupedWork({ limit: TRAY_LIMIT });
      if (ticket !== trayTicket || scope !== $localWorkScope) return;
      tray = {
        rows: trayRows(result),
        hasMore: Boolean(result?.has_more),
        loaded: true,
        error: "",
      };
    } catch (error) {
      if (ticket !== trayTicket || scope !== $localWorkScope) return;
      // Core's text stays out of the page; the banner carries fixed copy.
      tray = {
        ...tray,
        loaded: true,
        error: String(error?.status || "failed"),
      };
    }
  }
  // The tray follows the brief: the brief's live refresh is already bounded,
  // so the tray is read at most as often, and never per lane.
  const trayRead = { at: "" };
  $effect(() => {
    const at = String($store.updatedAt || "");
    if (!at || at === trayRead.at) return;
    trayRead.at = at;
    void loadTray();
  });

  // ---- Outbox ---------------------------------------------------------------
  let groupingEntries = $derived(
    ($mutationOutbox ?? []).filter(
      (entry) => isGrouping(entry) && entry.state !== "superseded",
    ),
  );
  let pending = $derived(
    new Set(groupingEntries.map((entry) => entry.draft?.work?.ref)),
  );
  let folds = $derived(
    foldSuggestions(brief?.nextActions, brief?.initiatives).filter(
      (suggestion) => !pending.has(suggestion.ref),
    ),
  );
  let saveError = $state("");
  let notice = $state(null);
  let noticeElement = $state(null);

  async function send(draft, { keyboard = true } = {}) {
    saveError = "";
    try {
      // Persisted before it is sent; a storage failure stops the send.
      await enqueueGrouping(draft, { href: boardHref || undefined });
      saved = null;
    } catch {
      saveError =
        "Couldn’t save this on your device, so it was not sent. Try again.";
      return;
    }
    notice = { text: `${groupingTitle(draft)}. Saved, sending…` };
    if (keyboard) void tick().then(() => noticeElement?.focus());
  }
  const group = (row, lane, options) =>
    send(groupingDraft("group", row, lane), options);
  const fold = (suggestion) =>
    send(
      groupingDraft(
        "fold",
        { ref: suggestion.ref, title: suggestion.title },
        { ref: suggestion.into, title: suggestion.intoTitle },
      ),
    );

  /** Undo for a confirmed change; group and fold each have one. */
  const UNDO = { group: "ungroup", fold: "unfold" };
  function doneText(draft) {
    const { work, parent } = draft;
    switch (draft.verb) {
      case "group":
        return `Grouped “${work.title}” under “${parent?.title}”.`;
      case "fold":
        return `Folded “${work.title}” into “${parent?.title}”.`;
      case "unfold":
        return `Unfolded “${work.title}”. It is its own initiative again.`;
      case "ungroup":
        return `Took “${work.title}” out of “${parent?.title}”. It is back in Ungrouped.`;
      default:
        return groupingTitle(draft);
    }
  }

  // A saved grouping handed back by Edit on an outbox entry that needs
  // attention. Kept on the device until sent again or discarded.
  let saved = $state(null);
  function restoreSaved(value, reason) {
    saved = reason === "privacy" ? null : value;
  }

  // ---- Lanes ----------------------------------------------------------------
  let phone = $state(false);
  let collapsed = $state({});
  const laneCollapsed = (lane) =>
    collapsed[lane.ref] ??
    (lane.state === "done" ||
      (phone && !lane.rollup.needingViewer && lane.state !== "past_promise"));
  function toggleLane(lane) {
    collapsed = { ...collapsed, [lane.ref]: !laneCollapsed(lane) };
  }
  let openCells = $state({});
  const cellKey = (lane, column) => `${lane.ref}\0${column}`;

  let trayOpen = $state(false);
  let trayDialog = $state(null);
  let trayOpener = null;
  function openTray(event) {
    trayOpener = event.currentTarget;
    trayOpen = true;
    void tick().then(() => trayDialog?.focus());
  }
  function closeTray() {
    trayOpen = false;
    trayOpener?.focus?.();
  }
  let foldsOpen = $state(false);
  let loopsOpen = $state(false);

  // ---- Drag a tray row onto a lane -------------------------------------------
  let boardEl = $state();
  /** @type {{ row: object, pointerId: number, startX: number, startY: number, active: boolean } | null} */
  let drag = $state(null);
  let dragX = $state(0);
  let dragY = $state(0);
  let dropRef = $state("");

  function grab(event, row) {
    if (event.pointerType === "touch") return;
    if (event.button != null && event.button !== 0) return;
    if (
      event.target instanceof Element &&
      event.target.closest("a, button, select, label, input")
    )
      return;
    drag = {
      row,
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      active: false,
    };
    window.addEventListener("pointermove", onMove, true);
    window.addEventListener("pointerup", onUp, true);
    window.addEventListener("pointercancel", stopDrag, true);
  }
  function onMove(event) {
    if (!drag || event.pointerId !== drag.pointerId) return;
    if (
      !drag.active &&
      Math.hypot(event.clientX - drag.startX, event.clientY - drag.startY) <
        DRAG_THRESHOLD_PX
    )
      return;
    if (!drag.active) {
      drag = { ...drag, active: true };
      window.getSelection?.()?.removeAllRanges?.();
    }
    event.preventDefault();
    dragX = event.clientX;
    dragY = event.clientY;
    const lane = document
      .elementFromPoint(event.clientX, event.clientY)
      ?.closest("[data-lane-ref]");
    dropRef =
      lane && boardEl?.contains(lane) ? lane.getAttribute("data-lane-ref") : "";
  }
  function onUp(event) {
    if (!drag || event.pointerId !== drag.pointerId) return;
    const { row, active } = drag;
    const lane = active ? lanes.find((entry) => entry.ref === dropRef) : null;
    stopDrag();
    if (lane) void group(row, lane, { keyboard: false });
  }
  function stopDrag() {
    window.removeEventListener("pointermove", onMove, true);
    window.removeEventListener("pointerup", onUp, true);
    window.removeEventListener("pointercancel", stopDrag, true);
    drag = null;
    dropRef = "";
  }

  onMount(() => {
    const release = mountBrief();
    const query = window.matchMedia?.("(max-width: 767px)");
    const syncPhone = () => {
      phone = Boolean(query?.matches);
    };
    syncPhone();
    query?.addEventListener?.("change", syncPhone);
    const confirmed = (event) => {
      const entry = event.detail;
      if (!isGrouping(entry) || entry.scope !== $localWorkScope) return;
      void refreshBrief();
      void loadTray();
      const draft = entry.draft;
      const undo = UNDO[draft.verb];
      notice = {
        text: doneText(draft),
        undo: undo
          ? () => send(groupingDraft(undo, draft.work, draft.parent))
          : null,
      };
    };
    window.addEventListener("anx:mutation-confirmed", confirmed);
    return () => {
      release();
      stopDrag();
      query?.removeEventListener?.("change", syncPhone);
      window.removeEventListener("anx:mutation-confirmed", confirmed);
    };
  });

  const DOT = {
    not_started: "bg-fg-subtle",
    moving: "bg-accent",
    stuck: "bg-warn",
    done: "bg-ok",
  };
  const columnLabel = (key) =>
    LANE_COLUMNS.find((column) => column.key === key).label.toLowerCase();
</script>

{#snippet trayBody()}
  {#if tray.error}
    <StateError
      title="Ungrouped cards didn’t load"
      message="Check your connection and try again."
      onretry={loadTray}
    />
  {:else}
    <SwimlaneTray
      rows={tray.rows}
      hasMore={tray.hasMore}
      {lanes}
      {pending}
      {now}
      onGroup={(row, lane) => group(row, lane)}
      onGrab={phone ? null : grab}
    />
  {/if}
{/snippet}

<div class="flex flex-col gap-3" bind:this={boardEl} data-swimlane-board>
  <div
    use:durableForm={{
      target: GROUPING_DRAFT_TARGET,
      value: saved,
      read: () => saved,
      onRestore: restoreSaved,
    }}
  >
    {#each groupingEntries as entry (entry.key)}
      <MutationSendStatus {entry} />
    {/each}
    {#if saved}
      <div
        class="flex flex-wrap items-center gap-2 rounded-md bg-bg-soft px-3 py-2 text-meta"
        role="status"
        data-saved-grouping
      >
        <span class="min-w-0 flex-1">Saved: {groupingTitle(saved)}.</span>
        <button
          type="button"
          class="ui-btn-primary"
          disabled={pending.has(saved.work?.ref)}
          onclick={() => send(saved)}>Send again</button
        >
        <button
          type="button"
          class="ui-prose-link text-micro"
          onclick={() => (saved = null)}>Discard</button
        >
      </div>
    {/if}
  </div>
  {#if saveError}
    <p class="text-meta text-danger-text" role="alert">{saveError}</p>
  {/if}
  {#if notice}
    <p
      class="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md bg-bg-soft px-3 py-2 text-meta text-fg outline-none"
      role="status"
      tabindex="-1"
      bind:this={noticeElement}
      data-grouping-notice
    >
      <span class="min-w-0 flex-1">{notice.text}</span>
      {#if notice.undo}
        <button
          class="ui-prose-link text-micro"
          type="button"
          onclick={() => {
            const undo = notice.undo;
            notice = null;
            undo();
          }}>Undo</button
        >
      {/if}
      <button
        class="ui-prose-link text-micro"
        type="button"
        onclick={() => (notice = null)}>Dismiss</button
      >
    </p>
  {/if}

  {#if !brief && !briefFailed}
    <div aria-busy="true" data-swimlanes-loading>
      <p class="sr-only" role="status">Loading the board…</p>
      <div class="ui-skeleton-reveal flex flex-col gap-2" aria-hidden="true">
        {#each [3, 2, 2] as cards, lane (lane)}
          <div
            class="flex flex-col gap-2 rounded-md border border-line bg-panel p-3 md:flex-row"
          >
            <span
              class="block h-14 animate-pulse rounded bg-panel-hover md:w-56"
            ></span>
            {#each Array.from({ length: cards }, (_, i) => i) as i (i)}
              <span
                class="block h-9 flex-1 animate-pulse rounded border border-line bg-bg-soft"
              ></span>
            {/each}
          </div>
        {/each}
      </div>
    </div>
  {:else if !brief}
    <StateError
      title="The board didn’t load"
      message="Check your connection and try again."
      retrying={$store.refreshing}
      onretry={() => refreshBrief()}
    />
  {:else}
    {#if briefFailed}
      <p class="text-micro text-warn-text" role="status">
        Showing the board as last loaded. It may be out of date.
      </p>
    {/if}

    <div class="flex flex-wrap items-center gap-2 text-meta">
      <p class="min-w-0 flex-1 text-fg-muted" data-swimlane-summary>
        {summary}
      </p>
      {#if budget}
        <button
          type="button"
          class="rounded-md border px-2.5 py-1 text-micro font-medium {overBudget(
            budget,
          )
            ? 'border-warn bg-warn-soft text-warn-text'
            : 'border-line text-fg-muted'}"
          data-source="computed:initiative_budget"
          data-initiative-budget
          aria-expanded={overBudget(budget) ? foldsOpen : undefined}
          disabled={!overBudget(budget)}
          onclick={() => (foldsOpen = !foldsOpen)}
          title={budget.truncated
            ? "At least this many. Some initiatives may be outside the count."
            : undefined}
          >{budgetLabel(budget)}{overBudget(budget)
            ? " · suggest folds"
            : ""}</button
        >
      {/if}
      {#if waitingDecisions}
        <a
          class="rounded-md border border-warn px-2.5 py-1 text-micro text-warn-text"
          href={workspaceHref("/decide")}>{waitingDecisions} in Decide →</a
        >
      {/if}
    </div>

    {#if foldsOpen && overBudget(budget)}
      <section
        class="rounded-md border border-line bg-panel p-3 text-meta"
        aria-label="Fold suggestions"
        data-source="computed:next_actions"
        data-fold-suggestions
      >
        <p class="text-fg">
          {budget.count}{budget.truncated ? "+" : ""} initiatives, against a target
          of {budget.target}. Folding a card keeps its evidence, asks and
          promise on the initiative it joins, and can be undone.
        </p>
        {#if folds.length}
          <ul class="mt-2 divide-y divide-line-subtle">
            {#each folds as suggestion (suggestion.ref)}
              <li class="flex flex-wrap items-center gap-2 py-2">
                <span class="min-w-0 flex-1 [overflow-wrap:anywhere]"
                  >“{suggestion.title}” into “{suggestion.intoTitle}”{#if suggestion.reason}<span
                      class="text-fg-muted">{` · ${suggestion.reason}`}</span
                    >{/if}</span
                >
                <button
                  type="button"
                  class="ui-btn-secondary px-2.5 py-1 text-micro"
                  data-fold={suggestion.ref}
                  onclick={() => fold(suggestion)}
                  >Fold into {suggestion.intoTitle}</button
                >
              </li>
            {/each}
          </ul>
        {:else}
          <p class="mt-2 text-fg-muted">
            No fold suggestions right now. Group new cards from Ungrouped
            instead.
          </p>
        {/if}
      </section>
    {/if}

    {#if tray.rows.length || tray.error}
      <section
        class="hidden rounded-md border border-dashed border-line-strong bg-bg-soft px-3 py-2 md:block"
        aria-labelledby="swimlane-tray-title"
        data-swimlane-tray
      >
        <h2 id="swimlane-tray-title" class="text-meta">
          <span class="font-semibold text-fg"
            >Ungrouped · {tray.rows.length}{tray.hasMore ? "+" : ""}</span
          >
          <span class="text-fg-muted">
            New cards wait here until they join an initiative. Drag one onto a
            lane, or take a suggested home.</span
          >
        </h2>
        {@render trayBody()}
      </section>
      <button
        type="button"
        class="flex items-center justify-between rounded-md border border-dashed border-line-strong bg-bg-soft px-3 py-2.5 text-left text-meta md:hidden"
        aria-haspopup="dialog"
        data-swimlane-tray-open
        onclick={openTray}
        ><span
          ><span class="font-semibold text-fg"
            >Ungrouped · {tray.rows.length}{tray.hasMore ? "+" : ""}</span
          ><span class="text-fg-muted">{" · tap to group"}</span></span
        ><span aria-hidden="true" class="text-fg-muted">›</span></button
      >
    {/if}

    {#if !lanes.length}
      <section class="py-12 text-center" data-swimlanes-empty>
        <h2 class="text-subtitle font-semibold text-fg">No initiatives yet</h2>
        <p class="mx-auto mt-2 max-w-md text-meta text-fg-muted">
          {tray.rows.length
            ? "Cards are waiting in Ungrouped. Create an initiative to group them under."
            : "Create an initiative. Cards grouped under it show up here as a lane."}
        </p>
        <a
          class="ui-prose-link mt-4 inline-block text-meta"
          href={workspaceHref("/initiatives/new")}>Create initiative</a
        >
      </section>
    {:else}
      <section
        class="overflow-hidden rounded-md border border-line bg-panel max-md:border-0 max-md:bg-transparent"
        aria-label="Initiative lanes"
      >
        <div
          class="hidden grid-cols-[15rem_repeat(4,minmax(0,1fr))] border-b border-line text-micro font-medium uppercase tracking-wide text-fg-muted md:grid"
          aria-hidden="true"
        >
          <span class="px-3 py-2">Initiative</span>
          {#each LANE_COLUMNS as column (column.key)}
            <span class="px-3 py-2">{column.label}</span>
          {/each}
        </div>
        <div class="flex flex-col max-md:gap-2">
          {#each lanes as lane (lane.ref)}
            {@const isCollapsed = laneCollapsed(lane)}
            {@const cells = laneCells(lane.rollup)}
            {@const segments = progressSegments(lane.rollup.byState)}
            {@const more = hiddenChildren(lane.rollup)}
            <section
              class="border-line md:grid md:grid-cols-[15rem_repeat(4,minmax(0,1fr))] md:border-b md:last:border-b-0 max-md:rounded-md max-md:border max-md:bg-panel {dropRef ===
              lane.ref
                ? 'outline outline-2 -outline-offset-2 outline-accent'
                : ''}"
              aria-labelledby="lane-title-{lane.ref}"
              data-lane-ref={lane.ref}
              data-lane-collapsed={isCollapsed ? "true" : "false"}
            >
              <div class="min-w-0 p-3 md:bg-bg-soft">
                <div class="flex items-start gap-2">
                  <h3
                    id="lane-title-{lane.ref}"
                    class="min-w-0 flex-1 text-meta font-semibold text-fg [overflow-wrap:anywhere]"
                  >
                    <a class="hover:underline" href={href(lane.ref)}
                      >{lane.title}</a
                    >
                  </h3>
                  <button
                    type="button"
                    class="ui-icon-btn -mr-1 -mt-1 shrink-0"
                    aria-expanded={!isCollapsed}
                    aria-controls="lane-cells-{lane.ref}"
                    aria-label="{isCollapsed
                      ? 'Expand'
                      : 'Collapse'} {lane.title}"
                    data-lane-toggle
                    onclick={() => toggleLane(lane)}
                    ><svg
                      class="h-4 w-4 transition-transform {isCollapsed
                        ? '-rotate-90'
                        : ''}"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      stroke-width="2"
                      aria-hidden="true"><path d="M6 9l6 6 6-6" /></svg
                    ></button
                  >
                </div>
                <div
                  class="mt-2 flex h-1.5 gap-0.5 overflow-hidden rounded-sm bg-panel-hover"
                  role="img"
                  aria-label={segments.length
                    ? segments
                        .map((s) => `${s.count} ${columnLabel(s.key)}`)
                        .join(", ")
                    : "No cards yet"}
                  data-source="computed:initiatives.rollup.by_state"
                  data-lane-progress
                >
                  {#each segments as segment (segment.key)}
                    <span
                      class={DOT[segment.key]}
                      style="flex: {segment.share} 1 0%"
                    ></span>
                  {/each}
                </div>
                <div class="mt-2 flex flex-wrap items-center gap-1.5">
                  <span class="text-micro text-fg-muted"
                    >{lane.rollup.total}
                    {lane.rollup.total === 1 ? "card" : "cards"}</span
                  >
                  {#if lane.rollup.needingViewer}
                    <span
                      class="rounded bg-danger-soft px-1.5 text-micro font-semibold text-danger-text"
                      data-source="computed:initiatives.rollup.needing_viewer"
                      >{lane.rollup.needingViewer} need you</span
                    >
                  {/if}
                  {#if lane.rollup.pastPromise}
                    <span
                      class="rounded bg-warn-soft px-1.5 text-micro font-semibold text-warn-text"
                      data-source="computed:initiatives.rollup.past_promise"
                      >{lane.rollup.pastPromise} past promise</span
                    >
                  {/if}
                  {#if lane.state === "past_promise" && !lane.rollup.pastPromise}
                    <span
                      class="rounded bg-warn-soft px-1.5 text-micro font-semibold text-warn-text"
                      data-source="computed:initiatives.state"
                      >Past its promise</span
                    >
                  {/if}
                </div>
              </div>
              {#if isCollapsed}
                <!-- Collapsed: how many sit in each column, still in place. -->
                {#each LANE_COLUMNS as column (column.key)}
                  <div
                    class="hidden px-3 py-3 text-micro text-fg-muted md:block"
                    data-source="computed:initiatives.rollup.children"
                  >
                    {cells[column.key].length || "—"}
                  </div>
                {/each}
              {:else if !lane.rollup.total}
                <p
                  id="lane-cells-{lane.ref}"
                  class="px-3 pb-3 text-micro text-fg-muted md:col-span-4 md:py-3"
                >
                  Nothing grouped here yet.{phone
                    ? ""
                    : " Drag a card from Ungrouped onto this lane."}
                </p>
              {:else}
                <div
                  id="lane-cells-{lane.ref}"
                  class="contents"
                  data-source="computed:initiatives.rollup.children"
                >
                  {#each LANE_COLUMNS as column (column.key)}
                    {@const rows = cells[column.key]}
                    {@const open = openCells[cellKey(lane, column.key)]}
                    <div
                      class="flex min-w-0 flex-col gap-1.5 px-3 pb-3 md:py-3 {rows.length
                        ? ''
                        : 'max-md:hidden'}"
                      data-lane-column={column.key}
                    >
                      <span
                        class="text-micro font-medium text-fg-muted md:hidden"
                        >{column.label} · {rows.length}</span
                      >
                      {#each open ? rows : rows.slice(0, CELL_PREVIEW) as child (child.ref)}
                        <a
                          class="flex min-h-9 items-center gap-2 rounded-md border bg-bg-soft px-2 py-1.5 text-meta text-fg hover:bg-panel-hover {child.needsViewer
                            ? 'border-danger'
                            : 'border-line'}"
                          href={href(child.ref)}
                          data-work-ref={child.ref}
                        >
                          <span
                            class="h-2 w-2 shrink-0 rounded-full {child.needsViewer
                              ? 'bg-danger'
                              : DOT[column.key]}"
                            aria-hidden="true"
                          ></span>
                          <span class="min-w-0 flex-1 [overflow-wrap:anywhere]"
                            >{child.title}</span
                          >
                          {#if child.needsViewer}
                            <span class="shrink-0 text-micro text-danger-text"
                              >you</span
                            >
                          {:else if child.pastPromise}
                            <span class="shrink-0 text-micro text-warn-text"
                              >past promise</span
                            >
                          {:else if lane.rollup.workstreams.some((w) => w.ref === child.ref)}
                            <span class="shrink-0 text-micro text-fg-muted"
                              >workstream</span
                            >
                          {/if}
                        </a>
                      {/each}
                      {#if rows.length > CELL_PREVIEW}
                        <button
                          type="button"
                          class="self-start text-micro text-accent-text hover:underline"
                          aria-expanded={Boolean(open)}
                          onclick={() =>
                            (openCells = {
                              ...openCells,
                              [cellKey(lane, column.key)]: !open,
                            })}
                          >{open
                            ? "Show fewer"
                            : `+ ${rows.length - CELL_PREVIEW} more`}</button
                        >
                      {/if}
                    </div>
                  {/each}
                  {#if more || lane.rollup.hasMore}
                    <a
                      class="px-3 pb-3 text-micro text-accent-text hover:underline md:col-span-4 md:col-start-2 md:-mt-1"
                      href={href(lane.ref)}
                      data-source="computed:initiatives.rollup.has_more"
                      data-lane-more
                      >{more
                        ? `${more} more in this initiative →`
                        : "More in this initiative →"}</a
                    >
                  {/if}
                </div>
              {/if}
            </section>
          {/each}
        </div>
      </section>
    {/if}

    {#if loops.length}
      <section
        class="rounded-md border border-line bg-panel"
        aria-labelledby="swimlane-loops-title"
        data-swimlane-loops
      >
        <h2 id="swimlane-loops-title">
          <button
            type="button"
            class="flex w-full items-center justify-between px-3 py-2.5 text-left text-meta"
            aria-expanded={loopsOpen}
            aria-controls="swimlane-loops-list"
            onclick={() => (loopsOpen = !loopsOpen)}
            ><span
              ><span class="font-semibold text-fg">Loops · {loops.length}</span
              ><span class="text-fg-muted"
                >{" · recurring work, not counted as initiatives"}</span
              ></span
            ><svg
              class="h-4 w-4 text-fg-muted transition-transform {loopsOpen
                ? ''
                : '-rotate-90'}"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
              aria-hidden="true"><path d="M6 9l6 6 6-6" /></svg
            ></button
          >
        </h2>
        {#if loopsOpen}
          <ul
            id="swimlane-loops-list"
            class="divide-y divide-line-subtle border-t border-line"
          >
            {#each loops as loop (loop.ref)}
              <li class="flex items-center gap-2 px-3 py-2 text-meta">
                <a
                  class="min-w-0 flex-1 text-fg hover:underline [overflow-wrap:anywhere]"
                  href={href(loop.ref)}>{loop.title}</a
                >
                {#if loop.loop?.health}
                  <span
                    class="shrink-0 text-micro text-fg-muted"
                    data-source="computed:loops.loop">{loop.loop.health}</span
                  >
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}
  {/if}
</div>

{#if drag?.active}
  <div
    class="pointer-events-none fixed z-50 max-w-xs rounded-md border border-accent bg-panel px-3 py-2 text-meta text-fg shadow-lg"
    style="left: {dragX + 12}px; top: {dragY + 12}px"
    aria-hidden="true"
    data-swimlane-drag
  >
    {drag.row.title}
    <span class="block text-micro text-fg-muted"
      >{dropRef
        ? `Group under ${lanes.find((lane) => lane.ref === dropRef)?.title}`
        : "Drop on a lane to group"}</span
    >
  </div>
{/if}

{#if trayOpen}
  <div
    class="fixed inset-0 z-50 bg-black/60 md:hidden"
    role="presentation"
    onclick={(event) => {
      if (event.target === event.currentTarget) closeTray();
    }}
  >
    <div
      bind:this={trayDialog}
      class="absolute inset-x-0 bottom-0 max-h-[80vh] overflow-y-auto rounded-t-lg border-t border-line bg-panel px-4 pb-6 pt-3 outline-none"
      role="dialog"
      aria-modal="true"
      aria-labelledby="swimlane-sheet-title"
      tabindex="-1"
      use:dismissOnEscape={{ onDismiss: closeTray }}
      data-swimlane-tray-sheet
    >
      <div class="flex items-center justify-between gap-2">
        <h2 id="swimlane-sheet-title" class="text-subtitle text-fg">
          Ungrouped · {tray.rows.length}{tray.hasMore ? "+" : ""}
        </h2>
        <button
          type="button"
          class="ui-prose-link text-meta"
          onclick={closeTray}>Done</button
        >
      </div>
      <p class="mt-1 text-micro text-fg-muted">
        New cards wait here until they join an initiative.
      </p>
      {@render trayBody()}
    </div>
  </div>
{/if}
