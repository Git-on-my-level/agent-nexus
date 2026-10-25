<script>
  /**
   * The swimlane board: one lane per top-level initiative, its children in
   * state columns, and an Ungrouped tray of new cards above.
   *
   * Reads: the brief (lanes, rollups, loops, budget, fold suggestions, shared
   * with the nav badge) and one bounded `GET /work/ungrouped` page, re-read
   * when the brief refreshes. Nothing is read per lane.
   *
   * Writes go through the device outbox: group, fold, make it an initiative,
   * and their undos. A mouse can drag a tray row onto a lane; every drag has
   * a button or a select that does the same, and touch only uses those.
   *
   * A lane's subtitle is the initiative's outcome when core sends one, else
   * its next step. A proposed initiative (still in Backlog) is drawn dashed
   * with a "proposed" tag. The done column holds what entered Done in the
   * last seven days (core's `done_at`); older finishes are a count.
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
    splitDone,
    trayRows,
  } from "$lib/swimlanes.js";
  import {
    enqueueFailureMessage,
    enqueueGrouping,
    groupingDraft,
    groupingTitle,
    GROUPING_DRAFT_TARGET,
    isGrouping,
  } from "$lib/workGrouping.js";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import SwimlaneTray from "./SwimlaneTray.svelte";
  import {
    childSegments,
    childTone,
    coveredRefs,
    decisionIndex,
    needsYouItems,
    nextLine,
    outcomeLine,
    segmentsLabel,
    shortCodes,
  } from "$lib/watchRollup.js";

  let {
    workspaceHref,
    now = Date.now(),
    boardHref = "",
    /** Off where the page header already links to Decide. */
    decideLink = true,
  } = $props();

  const TRAY_LIMIT = 20;

  const store = briefStore();
  let brief = $derived($store.data);
  let briefFailed = $derived(Boolean($store.error));

  let lanes = $derived(buildLanes(brief?.initiatives));
  let loops = $derived(brief?.loops ?? []);
  let budget = $derived(brief?.initiativeBudget ?? null);
  /** Open decisions by the refs they name, once per brief. */
  let decisions = $derived(decisionIndex(brief));
  let waitingDecisions = $derived(countWaitingDecisions(brief?.decisions));
  let summary = $derived.by(() => {
    const inside = lanes.reduce((sum, lane) => sum + lane.rollup.total, 0);
    const more = brief?.truncated.initiatives ? "+" : "";
    // "4 initiatives + 1 proposed": a proposed one is not counted twice.
    const proposed = lanes.filter((lane) => lane.proposed).length;
    const settled = lanes.length - proposed;
    const count = `${settled}${more} ${settled === 1 ? "initiative" : "initiatives"}${proposed ? ` + ${proposed} proposed` : ""}`;
    // A phone has room for what to do next, not the inventory.
    if (phone)
      return [
        count,
        waitingDecisions
          ? `${waitingDecisions} ${waitingDecisions === 1 ? "decision" : "decisions"} waiting`
          : "",
      ]
        .filter(Boolean)
        .join(" · ");
    return [
      count,
      `${inside} ${inside === 1 ? "card" : "cards"} inside them`,
      loops.length
        ? `${loops.length} ${loops.length === 1 ? "loop" : "loops"} on their own tab`
        : "",
    ]
      .filter(Boolean)
      .join(" · ");
  });
  const href = (ref) =>
    workspaceHref(`/initiatives/${encodeURIComponent(ref)}`);
  const decisionHref = (decision) =>
    workspaceHref(`/decide/${encodeURIComponent(decision.inboxItemId)}`);

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
  // A tray that never loaded has no count to show.
  let trayCount = $derived(
    tray.error && !tray.rows.length
      ? ""
      : ` · ${tray.rows.length}${tray.hasMore ? "+" : ""}`,
  );
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
    } catch (error) {
      saveError = enqueueFailureMessage(error);
      return;
    }
    notice = { text: `${groupingTitle(draft)}. Saved, sending…` };
    if (keyboard) void tick().then(() => noticeElement?.focus());
  }
  const group = (row, lane, options) =>
    send(groupingDraft("group", row, lane), options);
  const promote = (row) =>
    send(
      groupingDraft("promote", row, null, { version: row.promote?.version }),
    );
  const fold = (suggestion) =>
    send(
      groupingDraft(
        "fold",
        { ref: suggestion.ref, title: suggestion.title },
        { ref: suggestion.into, title: suggestion.intoTitle },
      ),
    );

  /** The plan step a confirmed group added, once the brief shows it. */
  function addedStep({ verb, work, parent }) {
    if (verb !== "group") return "";
    return (
      lanes
        .find((lane) => lane.ref === parent?.ref)
        ?.rollup.children.find((child) => child.ref === work.ref)?.stepId ?? ""
    );
  }

  /** Undo for a confirmed change; group, fold and promote each have one. */
  const UNDO = { group: "ungroup", fold: "unfold", promote: "unpromote" };
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
      case "promote":
        return `“${work.title}” is an initiative now.`;
      case "unpromote":
        return `“${work.title}” is back in Ungrouped.`;
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
  // A phone shows each lane as a card of the lines that matter; the full
  // cells open on a tap. The desktop board folds only finished lanes.
  const laneCollapsed = (lane) =>
    collapsed[lane.ref] ?? (phone || lane.state === "done");
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
      event.target.closest("a, button, select, label, input, [data-tray-move]")
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
          ? () =>
              send(
                groupingDraft(undo, draft.work, draft.parent, {
                  stepId: addedStep(draft),
                  // An unpromote is fenced to the version the promote made.
                  version: entry.result?.work?.version,
                }),
              )
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

  /** One colour per child tone; the bar and the chips agree. */
  const TONE = {
    needs: "bg-danger",
    slipping: "bg-warn",
    moving: "bg-accent",
    done: "bg-ok",
    not_started: "bg-fg-subtle",
  };
  const SEG = { ...TONE, not_started: "bg-line" };

  /**
   * One lane, ready to draw: its cells with the reader's open asks at the
   * top of the needs-you column, the asks that cover a child standing in
   * for that child's own chip.
   */
  /** "+ 6 cards": what the phone card does not list yet. */
  function phoneMore(lane, view) {
    // An ask about a child lists that child too.
    const children = new Set(lane.rollup.children.map((child) => child.ref));
    const listed = new Set(
      view.phoneLines
        .map((item) => item.ref)
        .filter((ref) => children.has(ref)),
    );
    const rest = Math.max(0, lane.rollup.total - listed.size);
    return rest
      ? `+ ${rest} ${listed.size ? "more" : rest === 1 ? "card" : "cards"}`
      : "Show cards";
  }
  /** A workstream chip's count: "T1 T2 P86 · 9", or the bare count. */
  function streamCount(child) {
    if (!child.rollup) return "workstream";
    const codes = shortCodes(child.rollup);
    return codes ? `${codes} · ${child.rollup.total}` : `${child.rollup.total}`;
  }
  function laneView(lane) {
    const cells = laneCells(lane.rollup);
    const done = splitDone(cells.done, now);
    cells.done = done.recent;
    const needs = needsYouItems(lane, decisions);
    const asks = needs.filter((item) => item.kind === "ask");
    const covered = coveredRefs(asks.map((item) => item.decision));
    const stuck = [
      ...asks,
      ...cells.stuck
        .filter((child) => !covered.has(child.ref))
        .map((child) => ({ kind: "card", key: child.ref, child })),
    ];
    const moving = cells.moving.filter((child) => !child.needsViewer);
    return {
      cells,
      stuck,
      needs,
      slipping:
        lane.rollup.pastPromise || (lane.state === "past_promise" ? 1 : 0),
      doneEarlier: done.earlier.length,
      segments: childSegments(lane.rollup),
      line: nextLine(lane, now),
      outcome: outcomeLine(lane),
      // Phone: what waits on the reader, and beside it the first thing
      // moving. A lane with nothing for the reader is its bar alone.
      phoneLines: [
        ...needs.map((item) => ({ ...item, label: "you", tone: "needs" })),
        ...moving.slice(0, needs.length ? 1 : 0).map((child) => ({
          kind: "card",
          key: child.ref,
          ref: child.ref,
          title: child.title,
          label: child.pastPromise
            ? "slipping"
            : child.rollup?.total
              ? `${child.rollup.total} · moving`
              : "in progress",
          tone: child.pastPromise ? "slipping" : "moving",
        })),
      ],
    };
  }
</script>

{#snippet trayBody(idPrefix)}
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
      onPromote={promote}
      onGrab={phone ? null : grab}
      {idPrefix}
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
      {#if decideLink && waitingDecisions}
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
          class="lane-grid hidden border-b border-line text-micro font-medium uppercase tracking-wide text-fg-muted md:grid"
          aria-hidden="true"
        >
          <span class="px-3 py-2.5">Initiative</span>
          {#each LANE_COLUMNS as column (column.key)}
            <span class="px-3 py-2.5">{column.heading ?? column.label}</span>
          {/each}
        </div>
        <div class="flex flex-col max-md:gap-3">
          {#each lanes as lane (lane.ref)}
            {@const isCollapsed = laneCollapsed(lane)}
            {@const view = laneView(lane)}
            {@const cells = view.cells}
            {@const more = hiddenChildren(lane.rollup)}
            <section
              class="lane-grid border-line md:border-b md:last:border-b-0 max-md:block max-md:rounded-lg max-md:border max-md:bg-panel max-md:px-3.5 max-md:py-3 {dropRef ===
              lane.ref
                ? 'outline outline-2 -outline-offset-2 outline-accent'
                : ''}"
              aria-labelledby="lane-title-{lane.ref}"
              data-lane-ref={lane.ref}
              data-lane-collapsed={isCollapsed ? "true" : "false"}
              data-lane-proposed={lane.proposed ? "true" : undefined}
            >
              <div
                class="min-w-0 md:bg-bg-soft md:p-3"
                class:lane-proposed={lane.proposed}
              >
                <div class="flex items-start gap-2">
                  <h2
                    id="lane-title-{lane.ref}"
                    class="min-w-0 flex-1 text-meta font-semibold text-fg [overflow-wrap:anywhere] md:text-body"
                  >
                    <a class="hover:underline" href={href(lane.ref)}
                      >{lane.title}</a
                    >
                  </h2>
                  <!-- Phone: one tag says the most urgent thing. -->
                  {#if view.needs.length}
                    <span
                      class="tag inline-flex bg-danger-soft text-danger-text md:hidden"
                      data-source="computed:initiatives.rollup.needing_viewer"
                      >needs you</span
                    >
                  {:else if lane.proposed}
                    <span
                      class="tag inline-flex bg-accent-soft text-accent-text md:hidden"
                      data-source="computed:initiatives.proposed">proposed</span
                    >
                  {:else if view.slipping}
                    <span
                      class="tag inline-flex bg-warn-soft text-warn-text md:hidden"
                      data-source="computed:initiatives.rollup.past_promise"
                      >slipping</span
                    >
                  {/if}
                  <button
                    type="button"
                    class="ui-icon-btn -mr-1 -mt-1 shrink-0 max-md:hidden"
                    aria-expanded={!isCollapsed}
                    aria-controls={isCollapsed
                      ? undefined
                      : `lane-cells-${lane.ref}`}
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
                {#if view.outcome}
                  <p
                    class="mt-0.5 text-micro text-fg-muted [overflow-wrap:anywhere]"
                    data-source="computed:initiatives.outcome"
                    data-lane-outcome
                  >
                    {view.outcome}
                  </p>
                {:else if view.line.length}
                  <p
                    class="mt-0.5 text-micro text-fg-muted [overflow-wrap:anywhere]"
                    data-lane-next
                  >
                    {#each view.line as part, i (i)}{#if i > 0}<span
                          aria-hidden="true"
                          class="mx-1 text-fg-subtle">·</span
                        >{/if}<span
                        class={part.tone === "warn" ? "text-warn-text" : ""}
                        data-source="computed:{part.field}">{part.text}</span
                      >{/each}
                  </p>
                {/if}
                <div
                  class="mt-2 flex h-1.5 gap-0.5"
                  role="img"
                  aria-label={view.segments.length
                    ? segmentsLabel(lane.rollup)
                    : "No cards yet"}
                  data-source="computed:initiatives.rollup.by_state"
                  data-lane-progress
                >
                  {#each view.segments as segment, i (i)}
                    <span
                      class="rounded-sm {SEG[segment.tone]}"
                      style="flex: {segment.flex} 1 0%"
                    ></span>
                  {:else}
                    <span class="flex-1 rounded-sm bg-line"></span>
                  {/each}
                </div>
                <div
                  class="mt-2 flex flex-wrap items-center gap-1.5 max-md:hidden"
                >
                  {#if lane.proposed}
                    <span
                      class="tag inline-flex bg-accent-soft text-accent-text"
                      data-source="computed:initiatives.proposed"
                      data-lane-proposed-tag>proposed</span
                    >
                  {/if}
                  {#if view.needs.length}
                    <span
                      class="tag inline-flex bg-danger-soft text-danger-text"
                      data-source="computed:initiatives.rollup.needing_viewer"
                      data-lane-needs>{view.needs.length} need you</span
                    >
                  {/if}
                  {#if view.slipping}
                    <span
                      class="tag inline-flex bg-warn-soft text-warn-text"
                      data-source={lane.rollup.pastPromise
                        ? "computed:initiatives.rollup.past_promise"
                        : "computed:initiatives.state"}
                      data-lane-slipping>{view.slipping} slipping</span
                    >
                  {/if}
                  {#if !view.needs.length && !view.slipping && !lane.proposed}
                    <span
                      class="text-micro text-fg-muted"
                      data-source="computed:initiatives.rollup.total"
                      >{lane.rollup.total}
                      {lane.rollup.total === 1 ? "card" : "cards"}</span
                    >
                  {/if}
                </div>
                <!-- Phone: the lines that matter, then the rest on a tap. -->
                {#if phone}
                  <ul class="mt-1 md:hidden" data-lane-phone-lines>
                    {#each view.phoneLines as item (item.key)}
                      <li class="phone-line">
                        <a
                          class="min-w-0 flex-1 text-fg [overflow-wrap:anywhere]"
                          href={item.kind === "ask"
                            ? decisionHref(item.decision)
                            : href(item.ref)}
                          data-work-ref={item.kind === "card"
                            ? item.ref
                            : undefined}
                          data-ask-ref={item.kind === "ask"
                            ? item.decision.ref
                            : undefined}>{item.title}</a
                        >
                        <span
                          class="shrink-0 text-micro {item.tone === 'needs'
                            ? 'text-danger-text'
                            : item.tone === 'slipping'
                              ? 'text-warn-text'
                              : 'text-fg-muted'}">{item.label}</span
                        >
                      </li>
                    {/each}
                    {#if lane.rollup.total || view.stuck.length > view.needs.length}
                      <li>
                        <button
                          type="button"
                          class="phone-line w-full text-left text-fg-muted"
                          aria-expanded={!isCollapsed}
                          aria-controls={isCollapsed
                            ? undefined
                            : `lane-cells-${lane.ref}`}
                          data-lane-phone-toggle
                          onclick={() => toggleLane(lane)}
                          ><span class="flex-1"
                            >{isCollapsed
                              ? phoneMore(lane, view)
                              : "Fewer"}</span
                          ><span
                            aria-hidden="true"
                            class="transition-transform {isCollapsed
                              ? ''
                              : 'rotate-90'}">›</span
                          ></button
                        >
                      </li>
                    {/if}
                  </ul>
                {/if}
              </div>
              {#if isCollapsed}
                <!-- Collapsed: how many sit in each column, still in place. -->
                {#each LANE_COLUMNS as column (column.key)}
                  <div
                    class="hidden px-3 py-3 text-micro text-fg-muted md:block"
                    data-source="computed:initiatives.rollup.children"
                  >
                    {(column.key === "stuck" ? view.stuck : cells[column.key])
                      .length || "—"}
                  </div>
                {/each}
              {:else if !lane.rollup.total && !view.stuck.length}
                <p
                  id="lane-cells-{lane.ref}"
                  class="pt-2 text-micro text-fg-muted md:col-span-4 md:px-3 md:py-3"
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
                    {@const rows =
                      column.key === "stuck"
                        ? view.stuck
                        : cells[column.key].map((child) => ({
                            kind: "card",
                            key: child.ref,
                            child,
                          }))}
                    {@const open = openCells[cellKey(lane, column.key)]}
                    <div
                      class="flex min-w-0 flex-col gap-1.5 pt-2 md:px-3 md:py-3 {rows.length
                        ? ''
                        : 'max-md:hidden'}"
                      data-lane-column={column.key}
                    >
                      <span
                        class="text-micro font-medium text-fg-muted md:hidden"
                        >{column.heading ?? column.label} · {rows.length}</span
                      >
                      {#each open ? rows : rows.slice(0, CELL_PREVIEW) as item (item.key)}
                        {#if item.kind === "ask"}
                          <a
                            class="chip border-danger"
                            href={decisionHref(item.decision)}
                            data-ask-ref={item.decision.ref}
                          >
                            <span
                              class="h-[7px] w-[7px] shrink-0 rounded-full bg-danger"
                              aria-hidden="true"
                            ></span>
                            <span
                              class="min-w-0 flex-[1_1_6rem] [overflow-wrap:anywhere]"
                              >{item.title}</span
                            >
                            <span class="chip-n text-danger-text"
                              >ask · you</span
                            >
                          </a>
                        {:else}
                          {@const child = item.child}
                          {@const tone = childTone(child)}
                          {@const stream = lane.rollup.workstreams.some(
                            (w) => w.ref === child.ref,
                          )}
                          <a
                            class="chip {child.needsViewer
                              ? 'border-danger'
                              : 'border-line'}"
                            href={href(child.ref)}
                            data-work-ref={child.ref}
                          >
                            <span
                              class="h-[7px] w-[7px] shrink-0 rounded-full {TONE[
                                tone
                              ]}"
                              aria-hidden="true"
                            ></span>
                            <!-- In a narrow column the label wraps under the
                                 title rather than spilling out of the card. -->
                            <span
                              class="min-w-0 flex-[1_1_6rem] [overflow-wrap:anywhere]"
                              >{child.title}</span
                            >
                            {#if child.needsViewer}
                              <span class="chip-n text-danger-text">you</span>
                            {:else if child.pastPromise}
                              <span class="chip-n text-warn-text"
                                >past promise</span
                              >
                            {:else if stream}
                              <span
                                class="chip-n chip-n--codes"
                                data-source="computed:initiatives.rollup.workstreams.rollup"
                                data-stream-count>{streamCount(child)}</span
                              >
                            {/if}
                          </a>
                        {/if}
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
                      {#if column.key === "done" && view.doneEarlier}
                        <span
                          class="text-micro text-fg-muted"
                          data-source="computed:initiatives.rollup.children.done_at"
                          data-lane-done-earlier
                          >{view.doneEarlier} done earlier</span
                        >
                      {:else if !rows.length}
                        <span
                          class="hidden text-micro text-fg-muted md:block"
                          aria-hidden="true">—</span
                        >
                      {/if}
                    </div>
                  {/each}
                  {#if more || lane.rollup.hasMore}
                    <a
                      class="pt-2 text-micro text-accent-text hover:underline md:col-span-4 md:col-start-2 md:-mt-1 md:px-3 md:pb-3 md:pt-0"
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

    <!-- Below the lanes, as in the mockups: the board first, then what
         still needs a home. -->
    {#if tray.rows.length || tray.error}
      <section
        class="hidden rounded-lg border border-dashed border-line-strong bg-bg-soft px-3.5 py-3 md:block"
        aria-labelledby="swimlane-tray-title"
        data-swimlane-tray
      >
        <h2
          id="swimlane-tray-title"
          class="flex flex-wrap items-baseline gap-x-2.5 text-meta"
        >
          <span class="text-body font-semibold text-fg"
            >Ungrouped{trayCount}</span
          >
          <span class="text-fg-muted"
            >New cards land here until they're grouped. Suggestions come from
            shared links, repository, project and title prefix; anyone can take
            one, or drag a card onto a lane.</span
          >
        </h2>
        {@render trayBody("tray")}
      </section>
      <button
        type="button"
        class="flex items-center justify-between rounded-lg border border-dashed border-line-strong px-3.5 py-2.5 text-left text-meta md:hidden"
        aria-haspopup="dialog"
        data-swimlane-tray-open
        onclick={openTray}
        ><span
          ><span class="font-semibold text-fg">Ungrouped{trayCount}</span><span
            class="text-fg-muted">{" · tap to group"}</span
          ></span
        ><span aria-hidden="true" class="text-fg-muted">›</span></button
      >
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
            aria-controls={loopsOpen ? "swimlane-loops-list" : undefined}
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
          Ungrouped{trayCount}
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
      {@render trayBody("sheet")}
    </div>
  </div>
{/if}

<style>
  /* 260px for the initiative, then the four state columns. */
  @media (min-width: 768px) {
    .lane-grid {
      display: grid;
      grid-template-columns: 260px repeat(4, minmax(0, 1fr));
    }
  }
  /* No display here: the phone-only tags rely on Tailwind's md:hidden. */
  /* Proposed: a dashed lane, as in the Board mockup. */
  @media (min-width: 768px) {
    .lane-proposed {
      outline: 1px dashed var(--accent-solid);
      outline-offset: -6px;
    }
  }
  .tag {
    align-items: center;
    flex: none;
    border-radius: 6px;
    padding: 2px 7px;
    font-size: 11px;
    font-weight: 600;
    white-space: nowrap;
  }
  .chip {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 2px 7px;
    min-height: 2.25rem;
    padding: 6px 8px;
    border-width: 1px;
    border-radius: 8px;
    background: var(--bg-soft);
    color: var(--fg);
    font-size: 12.5px;
    line-height: 1.3;
  }
  .chip:hover {
    background: var(--panel-hover);
  }
  .chip-n {
    margin-left: auto;
    flex: none;
    font-size: 11px;
    color: var(--fg-muted);
    white-space: nowrap;
  }
  /* A workstream's codes can outgrow a narrow column: they shrink and wrap
     inside the chip, right-aligned, rather than spill past its edge. */
  .chip-n--codes {
    flex: 0 1 auto;
    min-width: 0;
    max-width: 100%;
    white-space: normal;
    overflow-wrap: anywhere;
    text-align: right;
  }
  .phone-line {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    min-height: 2.25rem;
    padding: 5px 0;
    border-top: 1px solid var(--line-subtle);
    font-size: 13px;
  }
</style>
