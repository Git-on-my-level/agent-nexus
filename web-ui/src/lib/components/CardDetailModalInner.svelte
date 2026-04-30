<script>
  import { browser } from "$app/environment";
  import { onMount } from "svelte";
  import { writable } from "svelte/store";

  import {
    actorRegistry,
    lookupActorDisplayName,
    principalRegistry,
  } from "$lib/actorSession";
  import {
    boardCardHeaderTitle,
    boardCardStableId,
    boardColumnTitle,
    freshnessStatusLabel,
    freshnessStatusTone,
    joinDelimitedValues,
    parseDelimitedValues,
  } from "$lib/boardUtils";
  import {
    cardResolutionLabel,
    cardResolutionTone,
    dueDateDisplay,
    isOverdue,
  } from "$lib/cardDisplayUtils";
  import { coreClient } from "$lib/coreClient";
  import {
    formatTimestamp,
    isoToDatetimeLocal,
    datetimeLocalToIso,
  } from "$lib/formatDate";
  import {
    backingThreadIdFromTopicRecord,
    searchDocuments as searchDocumentRecords,
    searchTopics as searchTopicRecords,
    topicSearchResultToPickerOption,
    documentSearchPickerSubtitle,
  } from "$lib/searchHelpers";
  import { toActorPickerOptions } from "$lib/systemActor.js";
  import { boardCardInspectNav } from "$lib/topicRouteUtils";
  import {
    createTimelineContext,
    setTimelineContext,
  } from "$lib/timelineContext";
  import Button from "$lib/components/Button.svelte";
  import CompactRefLink from "$lib/components/CompactRefLink.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import IdsIntegrityDisclosure from "$lib/components/IdsIntegrityDisclosure.svelte";
  import GuidedTypedRefsInput from "$lib/components/GuidedTypedRefsInput.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import DiscussionDrawer from "$lib/components/DiscussionDrawer.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import ResourceShareMenu from "$lib/components/ResourceShareMenu.svelte";
  import SearchableEntityPicker from "$lib/components/SearchableEntityPicker.svelte";
  import SearchableMultiEntityPicker from "$lib/components/SearchableMultiEntityPicker.svelte";
  import TimelineTab from "$lib/components/timeline/TimelineTab.svelte";

  let {
    cardItem,
    boardId,
    board,
    workspaceSlug,
    workspaceId = "",
    /** @type {{ id?: string, title?: string } | null | undefined} */
    primaryTopic = null,
    actorName,
    onclose,
    onmovecard,
    onsavecard,
    onrevisecard = async () => {},
    onremovecard,
    presentation = "modal",
  } = $props();

  const timelineWorkspaceSlug = writable("");
  const timelineApi = createTimelineContext(coreClient);

  $effect.pre(() => {
    timelineWorkspaceSlug.set(String(workspaceSlug ?? ""));
  });

  setTimelineContext({
    store: timelineApi.store,
    workspaceSlug: timelineWorkspaceSlug,
    refreshTimeline: () => timelineApi.refreshTimeline(),
  });

  let membership = $derived(cardItem?.membership);
  let backing = $derived(cardItem?.backing);
  let derived = $derived(cardItem?.derived);
  let thread = $derived(backing?.thread);
  let cdmDetailPane = $state("overview");
  let cardRevisions = $state([]);
  let revisionsLoading = $state(false);
  let revisionsError = $state("");
  let previousCardKey = $state("");
  let removeCardConfirmOpen = $state(false);

  let linkedThreadId = $derived(
    String(membership?.thread_id ?? backing?.thread_id ?? "").trim(),
  );
  let cardKey = $derived(boardCardStableId(membership));

  let cardInspectNav = $derived(boardCardInspectNav(membership, thread));
  let headerTitle = $derived(boardCardHeaderTitle(membership, thread));
  let cardResolution = $derived(String(membership?.resolution ?? "").trim());
  let summaryText = $derived(String(membership?.summary ?? "").trim());

  let assigneeRefs = $derived(
    Array.isArray(membership?.assignee_refs) ? membership.assignee_refs : [],
  );
  let assigneeNames = $derived.by(() => {
    const actors = $actorRegistry;
    const principals = $principalRegistry;
    return assigneeRefs.map((ref) => {
      const id = String(ref ?? "")
        .replace(/^actor:/, "")
        .trim();
      return lookupActorDisplayName(id, actors, principals);
    });
  });

  let cardTopicThreadRef = $derived.by(() => {
    const nav = cardInspectNav;
    if (!nav) return "";
    return nav.kind === "topic"
      ? `topic:${nav.segment}`
      : `thread:${nav.segment}`;
  });

  let actorOptions = $derived(toActorPickerOptions($actorRegistry));

  let backingThreadId = $derived(String(board?.thread_id ?? "").trim());

  let editOpen = $state(false);
  let editAdvanced = $state(false);
  let savingCard = $state(false);
  let saveError = $state("");
  let editTitle = $state("");
  let editSummary = $state("");
  let editThreadId = $state("");
  let editDocumentId = $state("");
  let editAssignees = $state([]);
  let editRisk = $state("medium");
  let editResolution = $state("");
  let editResolutionRefs = $state("");
  let editRelatedRefs = $state("");
  let editDueAt = $state("");
  let editDefinitionOfDone = $state("");
  let moveColumnKey = $state("");

  function documentIdFromRef(ref) {
    const s = String(ref ?? "").trim();
    if (s.startsWith("document:")) return s.slice("document:".length).trim();
    return s;
  }

  function normalizeRefList(arr) {
    return [
      ...new Set((arr ?? []).map((x) => String(x).trim()).filter(Boolean)),
    ]
      .sort()
      .join("\0");
  }

  function syncCardDraftsFromItem(item) {
    const m = item?.membership ?? {};
    editTitle = String(m.title ?? "").trim();
    editSummary = String(m.summary ?? "").trim();
    editThreadId = String(m.thread_id ?? "").trim();
    editDocumentId = documentIdFromRef(m.document_ref);
    editAssignees = [...(m.assignee_refs ?? [])].map((x) => String(x).trim());
    editRisk = String(m.risk ?? "medium").trim() || "medium";
    editResolution = String(m.resolution ?? "").trim();
    editResolutionRefs = joinDelimitedValues(m.resolution_refs ?? []);
    editRelatedRefs = joinDelimitedValues(m.related_refs ?? []);
    editDueAt = isoToDatetimeLocal(m.due_at ?? "");
    editDefinitionOfDone = joinDelimitedValues(m.definition_of_done ?? []);
    moveColumnKey = String(m.column_key ?? "").trim() || "backlog";
    saveError = "";
  }

  $effect(() => {
    void cardItem;
    editOpen = false;
    syncCardDraftsFromItem(cardItem);
  });

  function currentMembershipColumnKey() {
    return String(cardItem?.membership?.column_key ?? "").trim() || "backlog";
  }

  function handleColumnSelectChange() {
    if (moveColumnKey === currentMembershipColumnKey()) return;
    void onmovecard(cardItem, { column_key: moveColumnKey }, "Card moved.");
  }

  $effect(() => {
    if (!cardKey) {
      cdmDetailPane = "overview";
      previousCardKey = "";
      return;
    }
    if (cardKey !== previousCardKey) {
      cdmDetailPane = "overview";
      previousCardKey = cardKey;
    }
  });

  $effect(() => {
    if (cdmDetailPane !== "timeline") return;
    if (linkedThreadId) void timelineApi.loadTimeline(linkedThreadId);
  });

  $effect(() => {
    if (cdmDetailPane !== "revisions") return;
    if (cardKey) void loadCardRevisions(cardKey);
  });

  async function loadCardRevisions(cardId) {
    revisionsLoading = true;
    revisionsError = "";
    try {
      const result = await coreClient.getCardHistory(cardId);
      cardRevisions = (result.revisions ?? []).slice().reverse();
    } catch (e) {
      revisionsError = e instanceof Error ? e.message : String(e);
      cardRevisions = [];
    } finally {
      revisionsLoading = false;
    }
  }

  async function searchThreadOptions(query) {
    const threads = await searchTopicRecords(query);
    return threads.map(topicSearchResultToPickerOption);
  }

  async function searchDocumentOptions(query) {
    const documents = await searchDocumentRecords(query);
    return documents.map((document) => ({
      id: document.id,
      title: document.title || document.id,
      subtitle: documentSearchPickerSubtitle(document),
      keywords: [],
    }));
  }

  function buildCardPatch(m, draft) {
    const patch = {};
    const docDraft = draft.documentId.trim();
    const nextDoc = docDraft ? `document:${docDraft}` : null;
    const prevDocRaw = String(m.document_ref ?? "").trim();
    const prevDoc = prevDocRaw || null;
    if (nextDoc !== prevDoc) {
      patch.document_ref = nextDoc;
    }

    const draftAssign = [...draft.assignees].map((x) => String(x).trim());
    const memAssign = [...(m.assignee_refs ?? [])].map((x) => String(x).trim());
    if (normalizeRefList(draftAssign) !== normalizeRefList(memAssign)) {
      patch.assignee_refs = draftAssign;
    }

    if (draft.risk !== String(m.risk ?? "").trim()) {
      patch.risk = draft.risk;
    }

    const resDraft = draft.resolution.trim() || null;
    const resMem = String(m.resolution ?? "").trim() || null;
    if (resDraft !== resMem) {
      patch.resolution = resDraft;
    }

    const relDraft = parseDelimitedValues(draft.relatedRefs);
    const relMem = [...(m.related_refs ?? [])].map((x) => String(x).trim());
    if (normalizeRefList(relDraft) !== normalizeRefList(relMem)) {
      patch.related_refs = relDraft;
    }

    const resRefDraft = parseDelimitedValues(draft.resolutionRefs);
    const resRefMem = [...(m.resolution_refs ?? [])].map((x) =>
      String(x).trim(),
    );
    if (normalizeRefList(resRefDraft) !== normalizeRefList(resRefMem)) {
      patch.resolution_refs = resRefDraft;
    }

    const dueDraft = draft.dueAt.trim()
      ? datetimeLocalToIso(draft.dueAt)
      : null;
    const dueMem = String(m.due_at ?? "").trim() || null;
    if (dueDraft !== dueMem) {
      patch.due_at = dueDraft;
    }

    return patch;
  }

  async function handleSave() {
    if (!membership) return;
    savingCard = true;
    saveError = "";
    try {
      let resolvedTitle = editTitle.trim();
      const threadId = editThreadId.trim();
      if (!resolvedTitle && threadId) {
        try {
          const topics = await searchTopicRecords(threadId);
          const match =
            topics.find(
              (t) => backingThreadIdFromTopicRecord(t) === threadId,
            ) ?? topics[0];
          resolvedTitle = String(match?.title ?? "").trim() || threadId;
        } catch {
          resolvedTitle = threadId;
        }
      }
      if (!resolvedTitle) {
        saveError = "Card title is required.";
        return;
      }

      const related_refs = parseDelimitedValues(editRelatedRefs);
      if (threadId) {
        const token = `thread:${threadId}`;
        if (!related_refs.includes(token)) {
          related_refs.push(token);
        }
      }

      const draft = {
        title: resolvedTitle,
        summary: editSummary.trim() || resolvedTitle,
        documentId: editDocumentId,
        assignees: editAssignees,
        risk: editRisk,
        resolution: editResolution,
        resolutionRefs: editResolutionRefs,
        relatedRefs: joinDelimitedValues(related_refs),
        dueAt: editDueAt,
        definitionOfDone: editDefinitionOfDone,
      };

      const patch = buildCardPatch(membership, draft);
      const dodDraft = parseDelimitedValues(draft.definitionOfDone);
      const dodMem = [...(membership.definition_of_done ?? [])].map((x) =>
        String(x).trim(),
      );
      const contentPatch = {};
      if (draft.title.trim() !== String(membership.title ?? "").trim()) {
        contentPatch.title = draft.title.trim();
      }
      if (draft.summary.trim() !== String(membership.summary ?? "").trim()) {
        contentPatch.summary = draft.summary.trim();
      }
      if (normalizeRefList(dodDraft) !== normalizeRefList(dodMem)) {
        contentPatch.definition_of_done = dodDraft;
      }
      const contentPayload = {};
      if (Object.keys(contentPatch).length > 0) {
        const baseRevision = String(membership.head_revision_ref ?? "").replace(
          /^card_revision:/,
          "",
        );
        if (!baseRevision) {
          saveError =
            "Cannot determine base card revision. Refresh the board and try again.";
          return;
        }
        contentPayload.if_base_revision = baseRevision;
        contentPayload.revision = contentPatch;
      }
      if (Object.keys(patch).length > 0) {
        await onsavecard(cardItem, patch);
      }
      if (Object.keys(contentPayload).length > 0) {
        await onrevisecard(cardItem, contentPayload);
      }
      if (
        Object.keys(patch).length === 0 &&
        Object.keys(contentPayload).length === 0
      ) {
        editOpen = false;
        return;
      }

      cardRevisions = [];
      editOpen = false;
    } catch (e) {
      saveError = e instanceof Error ? e.message : String(e ?? "Save failed");
    } finally {
      savingCard = false;
    }
  }

  function beginEdit() {
    syncCardDraftsFromItem(cardItem);
    saveError = "";
    editAdvanced = false;
    editOpen = true;
  }

  function cancelEdit() {
    syncCardDraftsFromItem(cardItem);
    saveError = "";
    editOpen = false;
  }

  function handleBackdropClick(e) {
    if (e.target === e.currentTarget) {
      onclose();
    }
  }

  function pickDetailPane(
    /** @type {"overview" | "timeline" | "revisions"} */ pane,
  ) {
    cdmDetailPane = pane;
  }

  onMount(() => {
    if (!browser) return;
    let scrollY = 0;
    const previousBodyStyle = {};
    if (presentation === "modal") {
      scrollY = window.scrollY;
      for (const property of ["overflow", "position", "top", "width"]) {
        previousBodyStyle[property] = document.body.style[property];
      }
      document.body.style.overflow = "hidden";
      document.body.style.position = "fixed";
      document.body.style.top = `-${scrollY}px`;
      document.body.style.width = "100%";
    }
    function onKeydown(e) {
      if (presentation !== "modal") return;
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        onclose();
      }
    }
    document.addEventListener("keydown", onKeydown, true);
    return () => {
      document.removeEventListener("keydown", onKeydown, true);
      if (presentation !== "modal") return;
      for (const [property, value] of Object.entries(previousBodyStyle)) {
        document.body.style[property] = value;
      }
      window.scrollTo(0, scrollY);
    };
  });

  let derivedSummary = $derived(derived?.summary);
  let cardFreshness = $derived(derived?.freshness);
  let dodItems = $derived(
    Array.isArray(membership?.definition_of_done)
      ? membership.definition_of_done
      : [],
  );
  let relatedRefsList = $derived(
    Array.isArray(membership?.related_refs) ? membership.related_refs : [],
  );
  let resolutionRefsList = $derived(
    Array.isArray(membership?.resolution_refs)
      ? membership.resolution_refs
      : [],
  );

  let dedupedRelatedRefs = $derived.by(() => {
    const tid = linkedThreadId;
    const nav = cardInspectNav;
    return relatedRefsList.filter((ref) => {
      const s = String(ref ?? "").trim();
      if (tid && s === `thread:${tid}`) return false;
      if (nav?.kind === "topic" && s === `topic:${nav.segment}`) return false;
      if (nav?.kind === "thread" && s === `thread:${nav.segment}`) return false;
      return true;
    });
  });

  let refLabelHints = $derived.by(() => {
    const hints = {};
    const t = thread;
    if (t && typeof t === "object") {
      const title = String(t.title ?? "").trim();
      if (title) {
        if (t.id) hints[`thread:${t.id}`] = title;
        const topicRef = String(t.topic_ref ?? "").trim();
        if (topicRef) hints[topicRef] = title;
      }
    }
    const pt = primaryTopic;
    if (pt && typeof pt === "object" && pt.id) {
      const ptitle = String(pt.title ?? "").trim();
      hints[`topic:${pt.id}`] = ptitle || pt.id;
    }
    return hints;
  });

  let showSummary = $derived(
    Boolean(summaryText) && summaryText !== headerTitle,
  );

  let cardIntegrityRows = $derived.by(() => {
    const m = membership;
    if (!m) return [];
    const rows = [];
    const cid = boardCardStableId(m);
    if (cid) {
      rows.push({
        label: "Card ID",
        value: cid,
        copyLabel: "Copy card ID",
      });
    }
    const bid = String(boardId ?? "").trim();
    if (bid) {
      rows.push({
        label: "Board ID",
        value: bid,
        copyLabel: "Copy board ID",
      });
    }
    if (linkedThreadId) {
      rows.push({
        label: "Thread ID",
        value: linkedThreadId,
        copyLabel: "Copy thread ID",
      });
    }
    return rows;
  });
  let cardRawJson = $derived(cardItem ? JSON.stringify(cardItem, null, 2) : "");

  let nonZeroDerivedCounts = $derived.by(() => {
    if (!derivedSummary || typeof derivedSummary !== "object") return [];
    const entries = [
      { label: "Open cards", count: derivedSummary.open_card_count },
      {
        label: "Decision requests",
        count: derivedSummary.decision_request_count,
      },
      { label: "Decisions", count: derivedSummary.decision_count },
      {
        label: "Recommendations",
        count: derivedSummary.recommendation_count,
      },
      { label: "Documents", count: derivedSummary.document_count },
      { label: "Inbox", count: derivedSummary.inbox_count },
    ];
    return entries.filter((e) => e.count != null && e.count > 0);
  });
</script>

{#snippet cardActionsFooter()}
  <div
    class="shrink-0 border-t border-[var(--line)] bg-[var(--panel)] px-4 py-3"
  >
    <div
      class="flex flex-col gap-3 md:flex-row md:flex-wrap md:items-end md:justify-between"
    >
      <div
        class="flex min-w-0 max-w-full items-stretch rounded-md border border-[var(--line)] bg-[var(--bg-soft)] md:w-60"
      >
        <span
          class="flex shrink-0 items-center border-r border-[var(--line)] px-2.5 py-1.5 text-micro text-[var(--fg-muted)]"
          aria-hidden="true"
        >
          Column
        </span>
        <select
          bind:value={moveColumnKey}
          onchange={handleColumnSelectChange}
          aria-label="Column"
          class="min-w-0 flex-1 cursor-pointer rounded-r-md border-0 bg-transparent px-2 py-1.5 pr-7 text-meta text-[var(--fg)] focus:outline-none focus:ring-2 focus:ring-inset focus:ring-[var(--accent)]"
        >
          {#each board?.column_schema ?? [] as column (column.key)}
            <option value={column.key}>
              {column.title ||
                boardColumnTitle(column.key, board?.column_schema ?? [])}
            </option>
          {/each}
        </select>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        {#if editOpen}
          <Button
            variant="primary"
            size="compact"
            disabled={savingCard}
            onclick={() => void handleSave()}
          >
            {savingCard ? "Saving…" : "Save card details"}
          </Button>
          <Button variant="secondary" size="compact" onclick={cancelEdit}>
            Cancel
          </Button>
        {:else}
          <Button variant="secondary" size="compact" onclick={beginEdit}>
            Edit card
          </Button>
        {/if}
        <Button
          variant="destructive"
          size="compact"
          onclick={() => {
            removeCardConfirmOpen = true;
          }}
        >
          Remove card
        </Button>
      </div>
    </div>
  </div>
{/snippet}

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class={presentation === "modal" ? "cdm-backdrop" : "cdm-page"}
  data-testid={presentation === "modal" ? "cdm-dialog" : "card-detail-page"}
  role={presentation === "modal" ? "dialog" : undefined}
  aria-modal={presentation === "modal" ? "true" : undefined}
  aria-label="Card details"
>
  {#if presentation === "modal"}
    <div class="cdm-overlay" onclick={handleBackdropClick}></div>
  {/if}
  <div
    class={presentation === "modal"
      ? "cdm-panel"
      : "cdm-panel cdm-page-panel page-dock-layout page-dock-layout--mobile-only page-dock-layout--fixed-mobile-chat"}
  >
    <div class="sticky top-0 z-10 bg-[var(--panel)] px-3 pt-2 sm:px-4 sm:pt-3">
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="truncate text-subtitle font-semibold text-[var(--fg)]">
              {headerTitle}
            </h2>
            <span
              class="rounded-md px-1.5 py-0.5 text-micro font-medium {cardResolutionTone(
                cardResolution,
              )}"
            >
              {cardResolutionLabel(cardResolution)}
            </span>
            {#if membership?.due_at}
              <span
                class="rounded-md px-1.5 py-0.5 text-micro {isOverdue(
                  membership.due_at,
                )
                  ? 'bg-danger-soft text-danger-text'
                  : 'bg-[var(--line)] text-[var(--fg-muted)]'}"
              >
                Due {dueDateDisplay(membership.due_at) || "—"}
              </span>
            {/if}
          </div>
          {#if assigneeNames.length > 0}
            <div class="mt-2 flex flex-wrap items-center gap-1">
              <span class="text-micro text-[var(--fg-muted)]">Assigned</span>
              {#each assigneeNames as name (name)}
                <span
                  class="max-w-[10rem] truncate rounded-md bg-[var(--line)] px-1.5 py-0.5 text-micro text-[var(--fg-muted)]"
                  title={name}
                >
                  {name}
                </span>
              {/each}
            </div>
          {/if}
          <div class="mt-2 text-micro text-[var(--fg-muted)]">
            <span class="text-[var(--fg-muted)]">Board</span>
            {board?.title ?? boardId}
          </div>
        </div>
        <div class="flex shrink-0 items-center gap-1">
          {#if cardKey}
            <ResourceShareMenu resourceId={cardKey} rawRecord={cardItem} />
          {/if}
          <button
            type="button"
            class="shrink-0 rounded-md border border-[var(--line)] p-1.5 text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] hover:text-[var(--fg)]"
            onclick={() => onclose()}
            aria-label={presentation === "modal" ? "Close" : "Back to board"}
          >
            {#if presentation === "modal"}
              <svg
                class="h-4 w-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-width="2"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  d="M6 18L18 6M6 6l12 12"
                />
              </svg>
            {:else}
              <svg
                class="h-4 w-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-width="2"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  d="M10.5 19.5 3 12m0 0 7.5-7.5M3 12h18"
                />
              </svg>
            {/if}
          </button>
        </div>
      </div>

      <div
        class="relative mt-3 flex flex-wrap gap-0 border-b border-[var(--line)]"
        aria-label="Card sections"
        role="tablist"
      >
        <button
          type="button"
          role="tab"
          data-cdm-pane-tab="overview"
          tabindex={cdmDetailPane === "overview" ? 0 : -1}
          aria-selected={cdmDetailPane === "overview"}
          class={`relative inline-flex cursor-pointer border-0 border-b-2 border-transparent bg-transparent px-3 py-2 text-meta font-medium transition-colors ${cdmDetailPane === "overview" ? "border-accent text-[var(--fg)]" : "text-[var(--fg-muted)] hover:text-[var(--fg)]"}`}
          onpointerdown={() => pickDetailPane("overview")}
          onclick={() => pickDetailPane("overview")}
        >
          Overview
        </button>
        <button
          type="button"
          role="tab"
          data-cdm-pane-tab="timeline"
          data-testid="cdm-tab-timeline"
          tabindex={cdmDetailPane === "timeline" ? 0 : -1}
          aria-selected={cdmDetailPane === "timeline"}
          class={`relative inline-flex cursor-pointer border-0 border-b-2 border-transparent bg-transparent px-3 py-2 text-meta font-medium transition-colors ${cdmDetailPane === "timeline" ? "border-accent text-[var(--fg)]" : "text-[var(--fg-muted)] hover:text-[var(--fg)]"}`}
          onpointerdown={() => pickDetailPane("timeline")}
          onclick={() => pickDetailPane("timeline")}
        >
          Timeline
        </button>
        <button
          type="button"
          role="tab"
          data-cdm-pane-tab="revisions"
          tabindex={cdmDetailPane === "revisions" ? 0 : -1}
          aria-selected={cdmDetailPane === "revisions"}
          class={`relative inline-flex cursor-pointer border-0 border-b-2 border-transparent bg-transparent px-3 py-2 text-meta font-medium transition-colors ${cdmDetailPane === "revisions" ? "border-accent text-[var(--fg)]" : "text-[var(--fg-muted)] hover:text-[var(--fg)]"}`}
          onpointerdown={() => pickDetailPane("revisions")}
          onclick={() => pickDetailPane("revisions")}
        >
          Revisions
        </button>
      </div>
      <span class="hidden" data-testid="cdm-section-tab-val"
        >{cdmDetailPane}</span
      >
    </div>

    <div
      class={presentation === "modal"
        ? "cdm-scroll"
        : "cdm-scroll page-dock-scroll"}
    >
      {#if cdmDetailPane === "overview"}
        <div class="p-4" data-cdm-panel="overview">
          {#if editOpen}
            <div class="space-y-3">
              {#if saveError}
                <p
                  class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
                >
                  {saveError}
                </p>
              {/if}
              <label
                class="block text-micro font-medium text-[var(--fg-muted)]"
              >
                Title
                <input
                  bind:value={editTitle}
                  class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-meta text-[var(--fg)] focus:border-[var(--accent)] focus:outline-none"
                  type="text"
                />
              </label>

              <label
                class="block text-micro font-medium text-[var(--fg-muted)]"
              >
                Summary
                <textarea
                  bind:value={editSummary}
                  class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-1.5 text-meta text-[var(--fg)] focus:border-[var(--accent)] focus:outline-none"
                  rows="3"
                ></textarea>
              </label>

              <div class="flex flex-wrap gap-3">
                <label class="text-micro font-medium text-[var(--fg-muted)]">
                  Resolution
                  <select
                    bind:value={editResolution}
                    class="mt-1 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-1.5 text-meta text-[var(--fg)]"
                  >
                    <option value="">Open</option>
                    <option value="done">Done</option>
                    <option value="canceled">Canceled</option>
                  </select>
                </label>

                <label class="text-micro font-medium text-[var(--fg-muted)]">
                  Due date
                  <input
                    bind:value={editDueAt}
                    class="mt-1 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-1.5 text-meta text-[var(--fg)]"
                    type="datetime-local"
                  />
                </label>
              </div>

              <button
                type="button"
                class="flex items-center gap-1.5 text-micro text-[var(--fg-muted)] transition-colors hover:text-[var(--fg)]"
                onclick={() => (editAdvanced = !editAdvanced)}
              >
                <svg
                  class="h-3.5 w-3.5 transition-transform {editAdvanced
                    ? 'rotate-90'
                    : ''}"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                  stroke-width="2"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M9 5l7 7-7 7"
                  />
                </svg>
                {editAdvanced ? "Fewer options" : "More options"}
              </button>

              {#if editAdvanced}
                <div
                  class="space-y-3 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] p-3"
                >
                  <div class="grid gap-3 md:grid-cols-2">
                    <SearchableEntityPicker
                      bind:value={editThreadId}
                      advancedLabel="Use a manual thread ID"
                      disabledIds={[backingThreadId].filter(Boolean)}
                      helperText="Optional: pick a topic or paste a thread ID."
                      label="Topic or thread"
                      manualLabel="Thread ID"
                      manualPlaceholder="thread-onboarding"
                      placeholder="Search topics by title or ID"
                      searchFn={searchThreadOptions}
                    />
                    <SearchableEntityPicker
                      bind:value={editDocumentId}
                      advancedLabel="Use a manual document ID"
                      helperText="Optional document lineage on the card."
                      label="Document"
                      manualLabel="Document ID"
                      manualPlaceholder="onboarding-guide-v1"
                      placeholder="Search documents by title, ID, or timeline ID"
                      searchFn={searchDocumentOptions}
                    />
                    <SearchableMultiEntityPicker
                      bind:values={editAssignees}
                      advancedLabel="Add a manual assignee ID"
                      helperText="Optional assignees."
                      items={actorOptions}
                      label="Assignees"
                      manualLabel="Assignee ID"
                      manualPlaceholder="actor-ops-ai"
                      placeholder="Search actors by name, ID, or tags"
                    />
                    <label
                      class="text-micro font-medium text-[var(--fg-muted)]"
                    >
                      Risk
                      <select
                        bind:value={editRisk}
                        class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg)] px-3 py-1.5 text-meta text-[var(--fg)]"
                      >
                        <option value="low">Low</option>
                        <option value="medium">Medium</option>
                        <option value="high">High</option>
                        <option value="critical">Critical</option>
                      </select>
                    </label>
                    <label
                      class="text-micro font-medium text-[var(--fg-muted)] md:col-span-2"
                    >
                      Definition of done
                      <textarea
                        bind:value={editDefinitionOfDone}
                        class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg)] px-3 py-1.5 text-meta text-[var(--fg)]"
                        rows="3"
                      ></textarea>
                    </label>
                    <div class="md:col-span-2">
                      <p class="text-micro font-medium text-[var(--fg-muted)]">
                        Related refs
                      </p>
                      <GuidedTypedRefsInput
                        bind:value={editRelatedRefs}
                        {boardId}
                        addInputLabel="Add related ref"
                        addInputPlaceholder="topic:summer-menu-rollout"
                        addButtonLabel="Add ref"
                        emptyText="No related refs yet."
                        helperText="Typed refs (topic:, document:, thread:, …)."
                        textareaAriaLabel="Card related refs"
                      />
                    </div>
                    <div class="md:col-span-2">
                      <p class="text-micro font-medium text-[var(--fg-muted)]">
                        Resolution evidence
                      </p>
                      <GuidedTypedRefsInput
                        bind:value={editResolutionRefs}
                        {boardId}
                        addInputLabel="Add resolution ref"
                        addInputPlaceholder="artifact:supporting-context"
                        addButtonLabel="Add ref"
                        emptyText="No resolution evidence yet."
                        helperText="Refs that evidence resolution."
                        textareaAriaLabel="Card resolution refs"
                      />
                    </div>
                  </div>
                </div>
              {/if}
            </div>
          {:else}
            <div class="space-y-4 text-meta text-[var(--fg)]">
              {#if showSummary}
                <section>
                  <h3
                    class="mb-1.5 text-micro font-medium text-[var(--fg-muted)]"
                  >
                    Summary
                  </h3>
                  <div
                    class="card-content-block rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2"
                  >
                    <MarkdownRenderer
                      source={summaryText}
                      class="markdown-rendered--card-content"
                    />
                  </div>
                </section>
              {/if}

              {#if dodItems.length > 0}
                <section>
                  <h3
                    class="mb-1.5 text-micro font-medium text-[var(--fg-muted)]"
                  >
                    Definition of done
                  </h3>
                  <ul class="list-inside list-disc space-y-1 text-micro">
                    {#each dodItems as line (line)}
                      <li>{line}</li>
                    {/each}
                  </ul>
                </section>
              {/if}

              {#if dedupedRelatedRefs.length > 0}
                <section>
                  <h3
                    class="mb-1.5 text-micro font-medium text-[var(--fg-muted)]"
                  >
                    Related refs
                  </h3>
                  <ul class="flex min-w-0 flex-wrap gap-1.5">
                    {#each dedupedRelatedRefs as ref (ref)}
                      <li class="min-w-0 text-micro">
                        <CompactRefLink
                          refValue={ref}
                          {boardId}
                          humanize
                          showRaw
                          labelHints={refLabelHints}
                        />
                      </li>
                    {/each}
                  </ul>
                </section>
              {/if}

              {#if resolutionRefsList.length > 0}
                <section>
                  <h3
                    class="mb-1.5 text-micro font-medium text-[var(--fg-muted)]"
                  >
                    Resolution refs
                  </h3>
                  <ul class="flex min-w-0 flex-wrap gap-1.5">
                    {#each resolutionRefsList as ref (ref)}
                      <li class="min-w-0 text-micro">
                        <CompactRefLink
                          refValue={ref}
                          {boardId}
                          humanize
                          showRaw
                          labelHints={refLabelHints}
                        />
                      </li>
                    {/each}
                  </ul>
                </section>
              {/if}

              <section class="space-y-2 text-micro">
                <div class="flex flex-wrap items-center gap-x-4 gap-y-1.5">
                  <span class="flex items-center gap-1.5">
                    <span class="text-[var(--fg-muted)]">Risk</span>
                    <span class="font-medium capitalize text-[var(--fg)]">
                      {String(membership?.risk ?? "—")}
                    </span>
                  </span>
                  {#if cardFreshness}
                    <span class="flex items-center gap-1.5">
                      <span class="text-[var(--fg-muted)]">Projection</span>
                      <span
                        class="rounded-md px-1.5 py-0.5 font-medium {freshnessStatusTone(
                          cardFreshness.status,
                        )}"
                      >
                        {freshnessStatusLabel(cardFreshness.status)}
                      </span>
                      {#if cardFreshness.generated_at}
                        <span class="text-[var(--fg-muted)]">
                          · {formatTimestamp(cardFreshness.generated_at)}
                        </span>
                      {/if}
                    </span>
                  {/if}
                  {#if derivedSummary?.latest_activity_at}
                    <span class="flex items-center gap-1.5">
                      <span class="text-[var(--fg-muted)]">Activity</span>
                      <span class="text-[var(--fg)]">
                        {formatTimestamp(derivedSummary.latest_activity_at) ||
                          "—"}
                      </span>
                    </span>
                  {/if}
                </div>
                {#if nonZeroDerivedCounts.length > 0}
                  <div class="flex flex-wrap gap-1.5">
                    {#each nonZeroDerivedCounts as { label, count } (label)}
                      <span
                        class="rounded-md bg-[var(--line)] px-1.5 py-0.5 text-micro"
                      >
                        <span class="text-[var(--fg-muted)]">{label}</span>
                        <span class="font-medium text-[var(--fg)]">
                          {count}
                        </span>
                      </span>
                    {/each}
                  </div>
                {/if}
              </section>

              {#if cardInspectNav && cardTopicThreadRef}
                <div
                  class="flex flex-wrap items-baseline gap-x-1.5 gap-y-0.5 text-micro"
                >
                  <span class="text-[var(--fg-muted)]"
                    >{cardInspectNav.kind === "topic"
                      ? "Topic"
                      : "Thread"}</span
                  >
                  <RefLink
                    refValue={cardTopicThreadRef}
                    threadId={linkedThreadId}
                    {boardId}
                    humanize
                    showRaw
                    labelHints={refLabelHints}
                  />
                </div>
              {/if}

              {#if membership?.updated_at}
                <p class="text-micro text-[var(--fg-muted)]">
                  Card updated {formatTimestamp(membership.updated_at)}
                  {#if membership?.updated_by}
                    <span class="text-[var(--fg-muted)]">
                      · {actorName(membership.updated_by)}
                    </span>
                  {/if}
                </p>
              {/if}

              <div class="mt-2">
                <IdsIntegrityDisclosure
                  rows={cardIntegrityRows}
                  rawJson={cardRawJson}
                  rawJsonCopyLabel="Copy card JSON"
                />
              </div>
            </div>
          {/if}
        </div>
      {:else if cdmDetailPane === "timeline"}
        <div class="px-4 pb-4 pt-1" data-cdm-panel="timeline">
          {#if linkedThreadId}
            <TimelineTab threadId={linkedThreadId} />
          {:else}
            <p class="text-meta text-[var(--fg-muted)]">
              This card has no backing thread; timeline requires a linked
              thread.
            </p>
          {/if}
        </div>
      {:else if cdmDetailPane === "revisions"}
        <div class="space-y-3 px-4 pb-4 pt-3" data-cdm-panel="revisions">
          <div class="flex items-center justify-between gap-3">
            <p class="text-meta text-[var(--fg-muted)]">
              Head revision {membership?.head_revision_number ?? ""}
            </p>
            <button
              type="button"
              class="rounded-md border border-[var(--line)] px-2 py-1 text-micro text-[var(--fg-muted)] hover:text-[var(--fg)]"
              onclick={() => loadCardRevisions(cardKey)}
            >
              Refresh
            </button>
          </div>
          {#if revisionsLoading}
            <p class="text-meta text-[var(--fg-muted)]">Loading revisions...</p>
          {:else if revisionsError}
            <p class="text-meta text-red-400">{revisionsError}</p>
          {:else if cardRevisions.length === 0}
            <p class="text-meta text-[var(--fg-muted)]">No revisions found.</p>
          {:else}
            <ol class="space-y-2">
              {#each cardRevisions as rev}
                <li class="rounded-md border border-[var(--line)] p-3">
                  <div class="flex items-start justify-between gap-3">
                    <div>
                      <p class="text-meta font-medium text-[var(--fg)]">
                        Revision {rev.revision_number}
                      </p>
                      <p class="text-micro text-[var(--fg-muted)]">
                        {formatTimestamp(rev.created_at)} by {actorName(
                          rev.created_by,
                        )}
                      </p>
                    </div>
                    {#if rev.artifact_ref}
                      <RefLink refValue={rev.artifact_ref} />
                    {/if}
                  </div>
                  {#if rev.title}
                    <p class="mt-2 text-meta font-medium text-[var(--fg)]">
                      {rev.title}
                    </p>
                  {/if}
                  {#if rev.summary}
                    <div class="mt-2 text-meta text-[var(--fg-muted)]">
                      <MarkdownRenderer source={rev.summary} />
                    </div>
                  {/if}
                </li>
              {/each}
            </ol>
          {/if}
        </div>
      {/if}
      {#if presentation === "page"}
        {@render cardActionsFooter()}
      {/if}
    </div>

    {#if linkedThreadId}
      <div class={presentation === "modal" ? "contents" : "page-dock-feed"}>
        <DiscussionDrawer
          layout="dock"
          threadId={linkedThreadId}
          {workspaceId}
          {workspaceSlug}
          label="Discussion"
          storageKey={`card-discussion:${cardKey}`}
          expandFillsParent
          narrowEdgeToEdge
        />
      </div>
    {/if}

    {#if presentation === "modal"}
      {@render cardActionsFooter()}
    {/if}
  </div>
</div>

<ConfirmModal
  open={removeCardConfirmOpen}
  title="Remove card"
  message="Remove this card from the board? The card will be moved to trash."
  confirmLabel="Remove card"
  variant="danger"
  onconfirm={() => {
    removeCardConfirmOpen = false;
    onremovecard(cardItem);
  }}
  oncancel={() => {
    removeCardConfirmOpen = false;
  }}
/>

<style>
  .cdm-backdrop {
    position: fixed;
    inset: 0;
    z-index: 50;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
    overscroll-behavior: contain;
  }

  .cdm-overlay {
    position: absolute;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
  }

  .cdm-panel {
    position: relative;
    z-index: 1;
    display: flex;
    height: min(90vh, 900px);
    height: min(90dvh, 900px);
    max-height: min(90vh, 900px);
    max-height: min(90dvh, 900px);
    width: 100%;
    max-width: 42rem;
    flex-direction: column;
    overflow: hidden;
    border-radius: 0.375rem;
    border: 1px solid var(--line);
    background: var(--panel);
    box-shadow: var(--shadow-modal);
  }

  .cdm-scroll {
    flex: 1;
    min-height: 0;
    -webkit-overflow-scrolling: touch;
    overscroll-behavior-y: contain;
    overflow-y: auto;
  }

  .cdm-page {
    min-height: 0;
  }

  .cdm-page-panel {
    min-height: calc(100dvh - 9rem);
    max-height: none;
    box-shadow: none;
  }

  @media (max-width: 767px) {
    .cdm-backdrop {
      align-items: stretch;
      padding: max(0.75rem, env(safe-area-inset-top, 0px))
        max(0.75rem, env(safe-area-inset-right, 0px))
        max(0.75rem, env(safe-area-inset-bottom, 0px))
        max(0.75rem, env(safe-area-inset-left, 0px));
    }

    .cdm-panel {
      height: calc(100vh - 1.5rem);
      height: calc(100dvh - 1.5rem);
      max-height: none;
    }

    .cdm-page-panel {
      min-height: calc(100dvh - 6.75rem);
      border-inline: 0;
      border-radius: 0;
    }
  }
</style>
