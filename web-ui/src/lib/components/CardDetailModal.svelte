<script>
  import { writable } from "svelte/store";

  import RefLink from "$lib/components/RefLink.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import MessagesTab from "$lib/components/timeline/MessagesTab.svelte";
  import TimelineTab from "$lib/components/timeline/TimelineTab.svelte";
  import {
    boardCardStableId,
    boardCardLinkedThreadId,
    boardColumnTitle,
    CANONICAL_BOARD_COLUMNS,
    freshnessStatusLabel,
    freshnessStatusTone,
    isFreshnessCurrent,
    joinDelimitedValues,
    parseDelimitedValues,
  } from "$lib/boardUtils";
  import {
    cardResolutionLabel,
    cardResolutionTone,
    priorityBadgeClasses,
  } from "$lib/cardDisplayUtils";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { getPriorityLabel } from "$lib/topicFilters";
  import {
    createTimelineContext,
    setTimelineContext,
  } from "$lib/timelineContext";

  let {
    open = false,
    cardItem = null,
    columnPeers = [],
    boardId = "",
    board = null,
    workspaceSlug = "",
    actorName = (id) => id,
    onclose = () => {},
    onmovecard = async () => {},
    onsavecard = async () => {},
    onremovecard = async () => {},
  } = $props();

  let activeTab = $state("overview");
  let overviewEditMode = $state(false);
  let busy = $state(false);

  let manageMoveColumnKey = $state("backlog");
  let manageTitle = $state("");
  let manageSummary = $state("");
  let manageThreadId = $state("");
  let manageDocumentId = $state("");
  let manageRisk = $state("medium");
  let manageResolution = $state("");
  let manageResolutionRefs = $state("");
  let manageRelatedRefs = $state("");
  let manageAssigneesStr = $state("");
  let manageDueAt = $state("");
  let manageDefinitionOfDone = $state("");

  function normalizeResolutionForEdit(raw) {
    const r = String(raw ?? "").trim();
    if (!r || r === "unresolved") return "";
    if (r === "completed") return "done";
    if (r === "cancelled") return "canceled";
    return r;
  }

  function operatorThreadIdFromCard(card) {
    const backing = String(card.thread_id ?? "").trim();
    const refs = Array.isArray(card.related_refs) ? card.related_refs : [];
    for (const r of refs) {
      const s = String(r ?? "").trim();
      if (!s.startsWith("thread:")) continue;
      const id = s.slice("thread:".length).trim();
      if (id && id !== backing) return id;
    }
    return backing;
  }

  function normalizeAssigneeRefToken(raw) {
    const s = String(raw ?? "").trim();
    if (!s) return "";
    if (s.includes(":")) return s;
    return `actor:${s}`;
  }

  function parseAssigneeRefsFromText(raw) {
    return parseDelimitedValues(raw)
      .map(normalizeAssigneeRefToken)
      .filter(Boolean);
  }

  function syncCardDraftsFromItem(item) {
    const card = item?.membership ?? {};
    manageTitle = card.title ?? "";
    manageSummary = card.summary ?? "";
    manageThreadId = operatorThreadIdFromCard(card);
    manageDocumentId = String(card.document_ref ?? "")
      .replace(/^document:/, "")
      .trim();
    manageRisk = card.risk ?? "medium";
    manageResolution = normalizeResolutionForEdit(card.resolution);
    manageResolutionRefs = joinDelimitedValues(card.resolution_refs ?? []);
    manageRelatedRefs = joinDelimitedValues(card.related_refs ?? []);
    manageAssigneesStr = joinDelimitedValues(card.assignee_refs ?? []);
    manageDueAt = card.due_at ?? "";
    manageDefinitionOfDone = joinDelimitedValues(card.definition_of_done ?? []);
    manageMoveColumnKey = card.column_key ?? "backlog";
  }

  function boardCardHeaderTitle(membership, thread) {
    const cardTitle = String(membership?.title ?? "").trim();
    if (cardTitle) return cardTitle;
    const threadTitle = String(thread?.title ?? "").trim();
    if (threadTitle) return threadTitle;
    return boardCardStableId(membership);
  }

  function staleBadgeClass(stale) {
    return stale
      ? "text-red-400 bg-red-500/10"
      : "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
  }

  function buildCardPatch() {
    const related = parseDelimitedValues(manageRelatedRefs);
    const opThread = manageThreadId.trim();
    if (opThread) {
      const token = opThread.includes(":") ? opThread : `thread:${opThread}`;
      if (!related.includes(token)) {
        related.push(token);
      }
    }
    return {
      title: manageTitle.trim(),
      summary: manageSummary.trim() || manageTitle.trim(),
      document_ref: manageDocumentId.trim()
        ? `document:${manageDocumentId.trim()}`
        : null,
      assignee_refs: parseAssigneeRefsFromText(manageAssigneesStr),
      risk: manageRisk,
      resolution: manageResolution.trim() || null,
      resolution_refs: parseDelimitedValues(manageResolutionRefs),
      related_refs: related,
      due_at: manageDueAt.trim() || null,
      definition_of_done: parseDelimitedValues(manageDefinitionOfDone),
    };
  }

  let columnOptions = $derived(
    board?.column_schema?.length
      ? board.column_schema
      : CANONICAL_BOARD_COLUMNS,
  );

  let membership = $derived(cardItem?.membership);
  let backing = $derived(cardItem?.backing);
  let derived = $derived(cardItem?.derived);
  let thread = $derived(backing?.thread);
  let linkedThreadId = $derived(
    membership ? boardCardLinkedThreadId(membership) : "",
  );

  const cardTimelineWorkspaceSlug = writable("");
  $effect.pre(() => {
    cardTimelineWorkspaceSlug.set(workspaceSlug);
  });

  const cardTimelineCtx = createTimelineContext(coreClient);
  setTimelineContext({
    store: cardTimelineCtx.store,
    refreshTimeline: () => cardTimelineCtx.loadTimeline(linkedThreadId),
    workspaceSlug: cardTimelineWorkspaceSlug,
  });

  async function handleCardMessagePost(threadId, event) {
    await coreClient.createEvent({ event });
    await cardTimelineCtx.loadTimeline(threadId);
  }

  let headerTitle = $derived(
    membership ? boardCardHeaderTitle(membership, thread) : "",
  );
  let cardResolution = $derived(String(membership?.resolution ?? "").trim());
  let cardSummary = $derived(String(membership?.summary ?? "").trim());
  let cardDueAt = $derived(String(membership?.due_at ?? "").trim());
  let threadLinkRef = $derived(
    linkedThreadId.trim() !== "" ? `thread:${linkedThreadId}` : "",
  );
  let topicRef = $derived(String(membership?.topic_ref ?? "").trim());
  let documentRef = $derived(String(membership?.document_ref ?? "").trim());
  let assigneeRefs = $derived(
    Array.isArray(membership?.assignee_refs) ? membership.assignee_refs : [],
  );
  let resolutionRefs = $derived(
    Array.isArray(membership?.resolution_refs)
      ? membership.resolution_refs
      : [],
  );
  let relatedRefs = $derived(
    Array.isArray(membership?.related_refs) ? membership.related_refs : [],
  );
  let moveUpBeforeCardId = $derived.by(() => {
    if (!cardItem || !columnPeers?.length) {
      return "";
    }
    const id = boardCardStableId(cardItem.membership);
    const idx = columnPeers.findIndex(
      (c) => boardCardStableId(c.membership) === id,
    );
    if (idx <= 0) {
      return "";
    }
    return boardCardStableId(columnPeers[idx - 1].membership);
  });
  let doD = $derived(
    Array.isArray(membership?.definition_of_done)
      ? membership.definition_of_done
      : [],
  );
  let cardFreshness = $derived(derived?.freshness);
  let derivedCurrent = $derived(isFreshnessCurrent(cardFreshness));
  let summary = $derived(derived?.summary);
  let threadPriority = $derived(String(thread?.priority ?? "").trim());
  let priorityShort = $derived(
    threadPriority ? threadPriority.toUpperCase() : "",
  );
  let columnKey = $derived(String(membership?.column_key ?? "").trim());
  let columnTitle = $derived(
    columnKey ? boardColumnTitle(columnKey, board?.column_schema ?? []) : "",
  );

  $effect(() => {
    if (!open) {
      activeTab = "overview";
      overviewEditMode = false;
      busy = false;
      return;
    }
    if (!cardItem) return;
    syncCardDraftsFromItem(cardItem);
  });

  $effect(() => {
    if (!open || !cardItem) return;
    if (
      (activeTab === "messages" || activeTab === "timeline") &&
      linkedThreadId
    ) {
      void cardTimelineCtx.loadTimeline(linkedThreadId);
    }
  });

  $effect(() => {
    if (!open || !cardItem) return;
    function onKeydown(e) {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        onclose();
      }
    }
    document.addEventListener("keydown", onKeydown, true);
    return () => document.removeEventListener("keydown", onKeydown, true);
  });

  function handleBackdropClick(e) {
    if (e.target === e.currentTarget && !busy) {
      onclose();
    }
  }

  async function handleMove() {
    if (!cardItem || busy) return;
    busy = true;
    try {
      await onmovecard(
        cardItem,
        { column_key: manageMoveColumnKey },
        "Card moved.",
      );
    } finally {
      busy = false;
    }
  }

  async function handleMoveUp() {
    if (!cardItem || busy || !moveUpBeforeCardId) return;
    const col = String(cardItem.membership?.column_key ?? "").trim();
    if (!col) return;
    busy = true;
    try {
      await onmovecard(
        cardItem,
        { column_key: col, before_card_id: moveUpBeforeCardId },
        "Card moved.",
      );
    } finally {
      busy = false;
    }
  }

  async function handleSave() {
    if (!cardItem || busy) return;
    busy = true;
    try {
      await onsavecard(cardItem, buildCardPatch());
      overviewEditMode = false;
    } finally {
      busy = false;
    }
  }

  async function handleRemove() {
    if (!cardItem || busy) return;
    busy = true;
    try {
      await onremovecard(cardItem);
    } finally {
      busy = false;
    }
  }

  const inputClass =
    "mt-0.5 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-1.5 text-[13px] text-[var(--ui-text)]";
  const labelClass = "text-[12px] font-medium text-[var(--ui-text-muted)]";
</script>

{#if open && cardItem}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="cdm-backdrop"
    role="dialog"
    aria-modal="true"
    aria-label="Card details"
  >
    <div class="cdm-overlay" onclick={handleBackdropClick}></div>
    <div class="cdm-panel flex flex-col">
      <div
        class="sticky top-0 z-10 shrink-0 border-b border-[var(--ui-border)] bg-[var(--ui-panel)] px-5 pb-0 pt-4"
      >
        <div class="relative pr-10">
          <button
            class="absolute right-0 top-0 rounded-md p-1 text-[12px] text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border)] hover:text-[var(--ui-text)]"
            aria-label="Close"
            disabled={busy}
            onclick={() => onclose()}
            type="button"
          >
            ✕
          </button>
          <h2
            class="pr-2 text-lg font-semibold leading-snug text-[var(--ui-text)]"
          >
            {headerTitle}
          </h2>
          <div class="mt-2 flex flex-wrap items-center gap-1.5">
            <span
              class="rounded px-1.5 py-0.5 text-[11px] font-medium {cardResolutionTone(
                cardResolution,
              )}"
            >
              Resolution: {cardResolutionLabel(cardResolution)}
            </span>
            {#if threadPriority}
              <span
                class="rounded px-1.5 py-0.5 text-[11px] font-medium {priorityBadgeClasses(
                  threadPriority,
                )}"
                title={getPriorityLabel(threadPriority)}
              >
                Priority: {priorityShort}
              </span>
            {/if}
            {#if cardDueAt}
              <span
                class="rounded bg-[var(--ui-border)] px-1.5 py-0.5 text-[11px] text-[var(--ui-text-muted)]"
              >
                Due: {formatTimestamp(cardDueAt) || "—"}
              </span>
            {/if}
          </div>
          {#if assigneeRefs.length > 0}
            <div class="mt-2 flex flex-wrap gap-1">
              {#each assigneeRefs as assigneeRef}
                <span
                  class="rounded-md bg-[var(--ui-border)] px-2 py-0.5 text-[12px] text-[var(--ui-text-muted)]"
                >
                  {actorName(String(assigneeRef).replace(/^actor:/, ""))}
                </span>
              {/each}
            </div>
          {/if}
          <p class="mt-2 text-[12px] text-[var(--ui-text-muted)]">
            <span class="text-[var(--ui-text-subtle)]">Board:</span>
            {String(board?.title ?? "").trim() || boardId || "—"}
            {#if columnKey}
              <span class="text-[var(--ui-text-subtle)]"> › </span>
              <span class="text-[var(--ui-text)]"
                >{columnTitle || columnKey}</span
              >
            {/if}
          </p>
        </div>

        <div
          class="mt-3 flex gap-0 border-b border-[var(--ui-border)]"
          aria-label="Card sections"
          role="tablist"
        >
          {#each [["overview", "Overview"], ["messages", "Messages"], ["timeline", "Timeline"]] as [tabId, tabLabel]}
            <button
              class={`relative cursor-pointer px-3 py-2 text-[13px] font-medium transition-colors ${activeTab === tabId ? "text-[var(--ui-text)]" : "text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]"}`}
              onclick={() => {
                activeTab = tabId;
              }}
              type="button"
              role="tab"
              aria-selected={activeTab === tabId}
              tabindex={activeTab === tabId ? 0 : -1}
            >
              {tabLabel}
              {#if activeTab === tabId}
                <span
                  class="pointer-events-none absolute inset-x-0 -bottom-px h-0.5 bg-indigo-500"
                ></span>
              {/if}
            </button>
          {/each}
        </div>
      </div>

      <div class="cdm-scroll flex-1 overflow-y-auto px-5 pb-4 pt-3">
        {#if activeTab === "overview"}
          <div role="tabpanel" tabindex="0">
            {#if overviewEditMode}
              <div class="grid gap-3 md:grid-cols-2">
                <label class={labelClass}>
                  Card title
                  <input
                    class={inputClass}
                    type="text"
                    bind:value={manageTitle}
                  />
                </label>
                <label class={labelClass}>
                  Risk
                  <select class={inputClass} bind:value={manageRisk}>
                    <option value="low">Low</option>
                    <option value="medium">Medium</option>
                    <option value="high">High</option>
                    <option value="critical">Critical</option>
                  </select>
                </label>
                <label class="{labelClass} md:col-span-2">
                  Summary
                  <textarea
                    class={inputClass}
                    rows="4"
                    bind:value={manageSummary}
                  ></textarea>
                </label>
                <label class={labelClass}>
                  Resolution
                  <select class={inputClass} bind:value={manageResolution}>
                    <option value="">Open</option>
                    <option value="done">Done</option>
                    <option value="canceled">Canceled</option>
                  </select>
                </label>
                <label class={labelClass}>
                  Due date
                  <input
                    class={inputClass}
                    type="datetime-local"
                    bind:value={manageDueAt}
                  />
                </label>
                <label class="{labelClass} md:col-span-2">
                  Card thread ID
                  <input
                    class={inputClass}
                    type="text"
                    bind:value={manageThreadId}
                  />
                </label>
                <label class="{labelClass} md:col-span-2">
                  Document ID
                  <input
                    class={inputClass}
                    type="text"
                    bind:value={manageDocumentId}
                    placeholder="without document: prefix"
                  />
                </label>
                <label class="{labelClass} md:col-span-2">
                  Assignees (one per line or comma-separated; actor: prefix
                  optional)
                  <textarea
                    class={inputClass}
                    rows="3"
                    bind:value={manageAssigneesStr}
                  ></textarea>
                </label>
                <label class="{labelClass} md:col-span-2">
                  Definition of done
                  <textarea
                    class={inputClass}
                    rows="3"
                    bind:value={manageDefinitionOfDone}
                  ></textarea>
                </label>
                <label class="{labelClass} md:col-span-2">
                  Related refs
                  <textarea
                    class={inputClass}
                    rows="3"
                    bind:value={manageRelatedRefs}
                  ></textarea>
                </label>
                <label class="{labelClass} md:col-span-2">
                  Resolution evidence
                  <textarea
                    class={inputClass}
                    rows="3"
                    bind:value={manageResolutionRefs}
                  ></textarea>
                </label>
              </div>
            {:else}
              {#if cardSummary}
                <div class="mt-1">
                  <h3
                    class="text-[11px] font-medium uppercase tracking-wide text-[var(--ui-text-subtle)]"
                  >
                    Summary
                  </h3>
                  <div class="mt-1.5 text-[13px] text-[var(--ui-text)]">
                    <MarkdownRenderer
                      source={cardSummary}
                      class="prose prose-invert max-w-none text-[13px]"
                    />
                  </div>
                </div>
              {/if}

              {#if doD.length > 0}
                <div class="mt-4">
                  <h3
                    class="text-[11px] font-medium uppercase tracking-wide text-[var(--ui-text-subtle)]"
                  >
                    Definition of done
                  </h3>
                  <ul
                    class="mt-1.5 list-disc space-y-0.5 pl-4 text-[13px] text-[var(--ui-text)]"
                  >
                    {#each doD as item}
                      <li>{item}</li>
                    {/each}
                  </ul>
                </div>
              {/if}

              <div class="mt-4">
                <h3
                  class="text-[11px] font-medium uppercase tracking-wide text-[var(--ui-text-subtle)]"
                >
                  Refs
                </h3>
                <div class="mt-1.5 flex flex-wrap gap-2 text-[13px]">
                  {#if threadLinkRef}
                    <RefLink
                      refValue={threadLinkRef}
                      threadId={linkedThreadId}
                      {boardId}
                      showRaw
                    />
                  {/if}
                  {#if topicRef}
                    <RefLink
                      refValue={topicRef}
                      threadId={linkedThreadId}
                      {boardId}
                      showRaw
                    />
                  {/if}
                  {#if documentRef}
                    <RefLink refValue={documentRef} {boardId} showRaw />
                  {/if}
                  {#if !threadLinkRef && !topicRef && !documentRef}
                    <span class="text-[12px] text-[var(--ui-text-muted)]"
                      >—</span
                    >
                  {/if}
                </div>
              </div>

              {#if relatedRefs.length > 0}
                <div class="mt-4">
                  <h3
                    class="text-[11px] font-medium uppercase tracking-wide text-[var(--ui-text-subtle)]"
                  >
                    Related refs
                  </h3>
                  <div class="mt-1.5 flex flex-wrap gap-2">
                    {#each relatedRefs as refValue}
                      <RefLink {refValue} {boardId} showRaw />
                    {/each}
                  </div>
                </div>
              {/if}

              {#if resolutionRefs.length > 0}
                <div class="mt-4">
                  <h3
                    class="text-[11px] font-medium uppercase tracking-wide text-[var(--ui-text-subtle)]"
                  >
                    Resolution evidence
                  </h3>
                  <div class="mt-1.5 flex flex-wrap gap-2">
                    {#each resolutionRefs as refValue}
                      <RefLink {refValue} {boardId} showRaw />
                    {/each}
                  </div>
                </div>
              {/if}

              {#if String(membership?.risk ?? "").trim()}
                <div class="mt-4">
                  <h3
                    class="text-[11px] font-medium uppercase tracking-wide text-[var(--ui-text-subtle)]"
                  >
                    Risk
                  </h3>
                  <div class="mt-1.5">
                    <span
                      class="inline-block rounded-md bg-amber-500/10 px-2 py-0.5 text-[12px] font-medium text-amber-400"
                    >
                      {String(membership.risk).trim()}
                    </span>
                  </div>
                </div>
              {/if}

              {#if derivedCurrent && summary}
                <div class="mt-4">
                  <h3
                    class="text-[11px] font-medium uppercase tracking-wide text-[var(--ui-text-subtle)]"
                  >
                    Derived scan summary
                  </h3>
                  <div
                    class="mt-1.5 flex flex-wrap gap-1.5 text-[12px] text-[var(--ui-text-muted)]"
                  >
                    <span
                      class="rounded bg-[var(--ui-border)] px-1.5 py-0.5 text-[11px]"
                    >
                      {(summary?.related_topic_count ?? 0) === 1
                        ? "1 topic"
                        : `${summary?.related_topic_count ?? 0} topics`}
                    </span>
                    <span
                      class="rounded bg-indigo-500/10 px-1.5 py-0.5 text-[11px] text-indigo-300"
                    >
                      {(summary?.document_count ?? 0) === 1
                        ? "1 document"
                        : `${summary?.document_count ?? 0} documents`}
                    </span>
                    <span
                      class="rounded bg-amber-500/10 px-1.5 py-0.5 text-[11px] text-amber-400"
                    >
                      {(summary?.inbox_count ?? 0) === 1
                        ? "1 inbox"
                        : `${summary?.inbox_count ?? 0} inbox`}
                    </span>
                    <span
                      class="rounded px-1.5 py-0.5 text-[11px] {staleBadgeClass(
                        Boolean(summary?.stale),
                      )}"
                    >
                      {summary?.stale ? "Topic stale" : "Fresh check-in"}
                    </span>
                  </div>
                </div>
              {/if}

              {#if cardFreshness}
                <div class="mt-4">
                  <h3
                    class="text-[11px] font-medium uppercase tracking-wide text-[var(--ui-text-subtle)]"
                  >
                    Freshness
                  </h3>
                  <div class="mt-1.5">
                    <span
                      class="rounded px-1.5 py-0.5 text-[11px] {freshnessStatusTone(
                        cardFreshness.status,
                      )}"
                    >
                      {freshnessStatusLabel(cardFreshness.status)}
                    </span>
                  </div>
                </div>
              {/if}
            {/if}
          </div>
        {/if}

        {#if activeTab === "messages"}
          <div role="tabpanel" tabindex="0">
            {#if linkedThreadId}
              <MessagesTab
                threadId={linkedThreadId}
                onMessagePost={handleCardMessagePost}
                workspaceId=""
              />
            {:else}
              <p class="text-[13px] text-[var(--ui-text-muted)]">
                No backing thread is linked to this card.
              </p>
            {/if}
          </div>
        {/if}

        {#if activeTab === "timeline"}
          <div role="tabpanel" tabindex="0">
            {#if linkedThreadId}
              <TimelineTab threadId={linkedThreadId} />
            {:else}
              <p class="text-[13px] text-[var(--ui-text-muted)]">
                No backing thread is linked to this card.
              </p>
            {/if}
          </div>
        {/if}
      </div>

      <div
        class="shrink-0 border-t border-[var(--ui-border)] bg-[var(--ui-panel)] px-5 py-3"
      >
        <div class="flex items-end gap-1.5">
          <label class="min-w-0 flex-1 text-[11px] text-[var(--ui-text-muted)]">
            Move to column
            <select
              class="mt-0.5 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-2 py-1.5 text-[12px] text-[var(--ui-text)]"
              bind:value={manageMoveColumnKey}
              disabled={busy}
            >
              {#each columnOptions as col}
                <option value={col.key}>
                  {col.title ||
                    boardColumnTitle(col.key, board?.column_schema ?? [])}
                </option>
              {/each}
            </select>
          </label>
          <button
            class="rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white transition-colors hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
            disabled={busy}
            onclick={() => void handleMove()}
            type="button"
          >
            {busy ? "…" : "Move"}
          </button>
          <button
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] hover:text-[var(--ui-text)] disabled:cursor-not-allowed disabled:opacity-50"
            disabled={busy || !moveUpBeforeCardId}
            onclick={() => void handleMoveUp()}
            title={moveUpBeforeCardId
              ? "Order this card before its current predecessor in this column"
              : "Already at the top of this column"}
            type="button"
          >
            Move up
          </button>
        </div>
        <div class="mt-2 flex flex-wrap items-center gap-2">
          {#if overviewEditMode}
            <button
              class="rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white transition-colors hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
              disabled={busy}
              onclick={() => void handleSave()}
              type="button"
            >
              Save card details
            </button>
            <button
              class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] hover:text-[var(--ui-text)] disabled:opacity-50"
              disabled={busy}
              onclick={() => {
                overviewEditMode = false;
                syncCardDraftsFromItem(cardItem);
              }}
              type="button"
            >
              Cancel
            </button>
          {:else}
            <button
              class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] hover:text-[var(--ui-text)] disabled:opacity-50"
              disabled={busy}
              onclick={() => {
                activeTab = "overview";
                overviewEditMode = true;
              }}
              type="button"
            >
              Edit card
            </button>
          {/if}
          <div class="flex-1"></div>
          <button
            class="rounded-md px-3 py-1.5 text-[12px] font-medium text-red-400 transition-colors hover:bg-red-500/10 disabled:opacity-50"
            disabled={busy}
            onclick={() => void handleRemove()}
            type="button"
          >
            Remove card
          </button>
        </div>
      </div>
    </div>
  </div>
{/if}

<style>
  .cdm-backdrop {
    position: fixed;
    inset: 0;
    z-index: 9999;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding-top: 5vh;
  }

  .cdm-overlay {
    position: absolute;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(2px);
  }

  .cdm-panel {
    position: relative;
    width: 720px;
    max-width: calc(100vw - 2rem);
    max-height: 85vh;
    background: var(--ui-panel);
    border: 1px solid var(--ui-border);
    border-radius: 6px;
    box-shadow: 0 4px 12px -2px rgba(0, 0, 0, 0.4);
  }

  .cdm-scroll {
    min-height: 0;
  }
</style>
