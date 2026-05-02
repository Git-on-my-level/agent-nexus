<script>
  import { browser } from "$app/environment";
  import { onMount } from "svelte";
  import { writable } from "svelte/store";

  import { actorRegistry } from "$lib/actorSession";
  import {
    boardCardHeaderTitle,
    boardCardStableId,
    boardColumnTitle,
    freshnessStatusLabel,
    freshnessStatusTone,
    joinDelimitedValues,
    parseDelimitedValues,
  } from "$lib/boardUtils";
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
  import { buildPrimitiveRefRoutes } from "$lib/refLinkModel";
  import {
    orderPickerOptionsByRecent,
    readRecentAssigneeIds,
    touchRecentAssigneeIds,
  } from "$lib/recentAssignees.js";
  import { toActorPickerOptions } from "$lib/systemActor.js";
  import { boardCardInspectNav } from "$lib/topicRouteUtils";
  import {
    createTimelineContext,
    setTimelineContext,
  } from "$lib/timelineContext";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import { inlineEditEscape } from "$lib/actions/inlineEditEscape.js";
  import IdsIntegrityDisclosure from "$lib/components/IdsIntegrityDisclosure.svelte";
  import GuidedTypedRefsInput from "$lib/components/GuidedTypedRefsInput.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import DiscussionDrawer from "$lib/components/DiscussionDrawer.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import ResourceShareMenu from "$lib/components/ResourceShareMenu.svelte";
  import SearchableEntityPicker from "$lib/components/SearchableEntityPicker.svelte";
  import TimelineTab from "$lib/components/timeline/TimelineTab.svelte";
  import {
    diffCardRevisionAgainstParent,
    humanizeRevisionFieldKey,
  } from "$lib/textDiff.js";

  let {
    cardItem,
    boardId,
    board,
    workspaceSlug,
    workspaceId = "",
    /** @type {{ id?: string, title?: string } | null | undefined} */
    primaryTopic = null,
    /** Stable ids of cards in this column (board order, same as the column stack). */
    columnPeerStableIds = [],
    actorName,
    onclose,
    onmovecard,
    onsavecard,
    onrevisecard = async () => {},
    onremovecard,
    presentation = "modal",
    /** When set (overview|resolution|timeline|revisions), syncs active tab from URL. */
    requestedDetailTab = "",
    onDetailTabChange = undefined,
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

  const CDM_TAB = new Set(["overview", "resolution", "timeline", "revisions"]);

  /** @param {string} tab */
  function normalizeIncomingTab(tab) {
    const t = String(tab ?? "").trim();
    return CDM_TAB.has(t) ? t : "overview";
  }

  let previousCardKey = $state("");

  let cardMenuOpen = $state(false);
  /** @type {HTMLElement | undefined} */
  let cardActionsMenuEl = $state(undefined);

  let linkedThreadId = $derived(
    String(membership?.thread_id ?? backing?.thread_id ?? "").trim(),
  );
  let cardKey = $derived(boardCardStableId(membership));

  $effect(() => {
    if (!cardKey) {
      cdmDetailPane = "overview";
      previousCardKey = "";
      return;
    }
    const nextTab = normalizeIncomingTab(requestedDetailTab);
    if (cardKey !== previousCardKey) {
      previousCardKey = cardKey;
      cdmDetailPane = nextTab;
      return;
    }
    cdmDetailPane = nextTab;
  });

  let cardRevisions = $state([]);
  let revisionsLoading = $state(false);
  let revisionsError = $state("");
  let removeCardConfirmOpen = $state(false);

  let cardInspectNav = $derived(boardCardInspectNav(membership, thread));
  let headerTitle = $derived(boardCardHeaderTitle(membership, thread));
  let summaryText = $derived(String(membership?.summary ?? "").trim());

  let cardTopicThreadRef = $derived.by(() => {
    const nav = cardInspectNav;
    if (!nav) return "";
    return nav.kind === "topic"
      ? `topic:${nav.segment}`
      : `thread:${nav.segment}`;
  });

  /** Suppress redundant topic/thread row when already listed in Related refs composer. */
  let duplicateTopicThreadNavLink = $derived.by(() => {
    const ct = cardTopicThreadRef.trim();
    if (!ct) return false;
    return parseDelimitedValues(editRelatedRefs).includes(ct);
  });

  let assigneeSuggestVersion = $state(0);
  let assigneeActorOptions = $derived.by(() => {
    void assigneeSuggestVersion;
    const base = toActorPickerOptions($actorRegistry);
    return orderPickerOptionsByRecent(base, readRecentAssigneeIds());
  });

  let backingThreadId = $derived(String(board?.thread_id ?? "").trim());

  /** @type {Set<string>} */
  let savingFields = $state(new Set());
  /** @type {Record<string, string>} */
  let fieldErrors = $state({});
  let summaryEditing = $state(false);
  let dodEditing = $state(false);

  /** @param {string} field */
  function isSaving(field) {
    return savingFields.has(field);
  }

  /** @param {string} field */
  /** @param {boolean} active */
  function setSaving(field, active) {
    const next = new Set(savingFields);
    if (active) next.add(field);
    else next.delete(field);
    savingFields = next;
  }

  /** @param {string} field */
  function clearFieldErr(field) {
    if (!(field in fieldErrors)) return;
    const next = { ...fieldErrors };
    delete next[field];
    fieldErrors = next;
  }

  /** @param {string} field @param {string} msg */
  function setFieldErr(field, msg) {
    fieldErrors = { ...fieldErrors, [field]: msg };
  }

  /** @returns {string} */
  function headRevisionIdFromMembership() {
    return String(membership?.head_revision_ref ?? "")
      .replace(/^card_revision:/, "")
      .trim();
  }

  let editTitle = $state("");
  let editSummary = $state("");
  let editThreadId = $state("");
  let editDocumentId = $state("");
  let editAssignees = $state([]);
  let editRisk = $state("medium");
  let editResolutionRefs = $state("");
  let editRelatedRefs = $state("");
  let editDueAt = $state("");
  let editDefinitionOfDone = $state("");
  let moveColumnKey = $state("");

  /** @typedef {"related" | "resolution"} CardAttachTarget */
  /** @type {CardAttachTarget | null} */
  let cardAttachBusy = $state(null);
  let cardAttachError = $state("");

  const CARD_ATTACHMENT_ACCEPT =
    "image/*,text/plain,text/markdown,text/csv,.md,.txt,.csv,.json,.pdf";

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

  function uniqueTypedRefs(items) {
    return [
      ...new Set(
        (items ?? []).map((item) => String(item ?? "").trim()).filter(Boolean),
      ),
    ];
  }

  function mergeTypedRefField(existingRaw, ref) {
    const r = String(ref ?? "").trim();
    if (!r) return existingRaw;
    const items = parseDelimitedValues(existingRaw);
    if (items.includes(r)) return existingRaw;
    return joinDelimitedValues([...items, r]);
  }

  /** True when ref draft autosave should run (not on timeline/revisions). */
  function cdmPaneAllowsRefDraftAutosave(pane) {
    return pane === "overview" || pane === "resolution";
  }

  function typedRefsOnlyWithPrefix(refs, prefix) {
    const p = String(prefix ?? "");
    return (refs ?? [])
      .map((x) => String(x ?? "").trim())
      .filter((x) => x.startsWith(p));
  }

  function syncCardDraftsFromItem(item) {
    const m = item?.membership ?? {};
    editTitle = String(m.title ?? "").trim();
    editSummary = String(m.summary ?? "").trim();
    editThreadId = String(m.thread_id ?? "").trim();
    editDocumentId = documentIdFromRef(m.document_ref);
    editAssignees = [...(m.assignee_refs ?? [])].map((x) => String(x).trim());
    editRisk = String(m.risk ?? "medium").trim() || "medium";
    editResolutionRefs = joinDelimitedValues(m.resolution_refs ?? []);
    editRelatedRefs = joinDelimitedValues(m.related_refs ?? []);
    editDueAt = isoToDatetimeLocal(m.due_at ?? "");
    editDefinitionOfDone = joinDelimitedValues(m.definition_of_done ?? []);
    moveColumnKey = String(m.column_key ?? "").trim() || "backlog";
  }

  $effect(() => {
    void cardItem;
    summaryEditing = false;
    dodEditing = false;
    cardMenuOpen = false;
    fieldErrors = {};
    syncCardDraftsFromItem(cardItem);
  });

  function currentMembershipColumnKey() {
    return String(cardItem?.membership?.column_key ?? "").trim() || "backlog";
  }

  function handleColumnSelectChange() {
    if (moveColumnKey === currentMembershipColumnKey()) return;
    void onmovecard(cardItem, { column_key: moveColumnKey }, "Card moved.");
  }

  let peerUpState = $derived.by(() => {
    const peers = columnPeerStableIds ?? [];
    const idx = peers.findIndex((id) => id === cardKey);
    return {
      peers,
      idx,
      beforeId: idx > 0 ? String(peers[idx - 1] ?? "").trim() || "" : "",
    };
  });

  function handleMoveUpInColumn() {
    const { idx, beforeId } = peerUpState;
    if (idx <= 0 || !beforeId) return;
    void onmovecard(
      cardItem,
      {
        column_key: currentMembershipColumnKey(),
        before_card_id: beforeId,
      },
      "Card moved.",
    );
  }

  $effect(() => {
    if (cdmDetailPane !== "timeline") return;
    if (cardKey) void timelineApi.loadTimeline(cardKey, { asCard: true });
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
      const rows = Array.isArray(result.revisions) ? result.revisions : [];
      cardRevisions = [...rows].sort(
        (a, b) =>
          Number(b?.revision_number ?? 0) - Number(a?.revision_number ?? 0),
      );
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

  /** @returns {Promise<string>} */
  async function fallbackTitleWhenEmpty(trimmed) {
    let resolved = trimmed;
    const threadLookup = editThreadId.trim();
    if (!resolved && threadLookup) {
      try {
        const topics = await searchTopicRecords(threadLookup);
        const match =
          topics.find(
            (t) => backingThreadIdFromTopicRecord(t) === threadLookup,
          ) ?? topics[0];
        resolved = String(match?.title ?? "").trim() || threadLookup;
      } catch {
        resolved = threadLookup;
      }
    }
    return resolved;
  }

  /** @returns {string[]} */
  function relatedRefsForPersist() {
    const arr = [...parseDelimitedValues(editRelatedRefs)];
    const tid = String(editThreadId || membership?.thread_id || "").trim();
    if (tid) {
      const token = `thread:${tid}`;
      if (!arr.includes(token)) arr.push(token);
    }
    return arr;
  }

  /** @param {Record<string, unknown>} patch */
  /** @param {string} field */
  async function persistMembershipPatch(patch, field) {
    if (!membership || !cardItem || !Object.keys(patch).length) return;
    setSaving(field, true);
    clearFieldErr(field);
    try {
      await onsavecard(cardItem, patch);
      if (
        field === "assignees" &&
        browser &&
        Array.isArray(patch.assignee_refs)
      ) {
        touchRecentAssigneeIds(patch.assignee_refs);
        assigneeSuggestVersion += 1;
      }
      cardRevisions = [];
      cardAttachError = "";
    } catch (e) {
      setFieldErr(
        field,
        e instanceof Error ? e.message : String(e ?? "Save failed"),
      );
    } finally {
      setSaving(field, false);
    }
  }

  /** @param {Record<string, unknown>} rev */
  /** @param {string} field */
  async function persistRevisionPatch(rev, field) {
    if (!membership || !cardItem || !Object.keys(rev).length) return;
    const base = headRevisionIdFromMembership();
    if (!base) {
      setFieldErr(
        field,
        "Cannot determine base card revision. Refresh the board and try again.",
      );
      return;
    }
    setSaving(field, true);
    clearFieldErr(field);
    try {
      await onrevisecard(cardItem, { if_base_revision: base, revision: rev });
      cardRevisions = [];
      cardAttachError = "";
    } catch (e) {
      setFieldErr(
        field,
        e instanceof Error ? e.message : String(e ?? "Save failed"),
      );
    } finally {
      setSaving(field, false);
    }
  }

  async function commitTitleField() {
    if (!membership) return;
    let t = editTitle.trim();
    t = await fallbackTitleWhenEmpty(t);
    if (!t) {
      setFieldErr("title", "Card title is required.");
      syncCardDraftsFromItem(cardItem);
      return;
    }
    editTitle = t;
    if (t === String(membership.title ?? "").trim()) {
      clearFieldErr("title");
      return;
    }
    await persistRevisionPatch({ title: t }, "title");
  }

  async function commitSummaryField() {
    summaryEditing = false;
    if (!membership) return;
    const s =
      editSummary.trim() ||
      editTitle.trim() ||
      (await fallbackTitleWhenEmpty(""));
    if (!s) {
      setFieldErr("summary", "Summary cannot be empty.");
      return;
    }
    if (s === String(membership.summary ?? "").trim()) {
      clearFieldErr("summary");
      return;
    }
    await persistRevisionPatch({ summary: s }, "summary");
  }

  async function commitDodField() {
    dodEditing = false;
    if (!membership) return;
    const dodDraft = parseDelimitedValues(editDefinitionOfDone);
    const dodMem = [...(membership.definition_of_done ?? [])].map((x) =>
      String(x).trim(),
    );
    if (normalizeRefList(dodDraft) === normalizeRefList(dodMem)) {
      clearFieldErr("definition_of_done");
      return;
    }
    await persistRevisionPatch(
      { definition_of_done: dodDraft },
      "definition_of_done",
    );
  }

  async function flushRelatedRefsToServer() {
    if (!membership) return;
    const relDraft = relatedRefsForPersist();
    const relMem = [...(membership.related_refs ?? [])].map((x) =>
      String(x).trim(),
    );
    if (normalizeRefList(relDraft) === normalizeRefList(relMem)) return;
    await persistMembershipPatch({ related_refs: relDraft }, "related_refs");
  }

  async function flushResolutionRefsToServer() {
    if (!membership) return;
    const draft = parseDelimitedValues(editResolutionRefs);
    const mem = [...(membership.resolution_refs ?? [])].map((x) =>
      String(x).trim(),
    );
    if (normalizeRefList(draft) === normalizeRefList(mem)) return;
    await persistMembershipPatch({ resolution_refs: draft }, "resolution_refs");
  }

  let relatedSaveTimer = 0;
  let resolutionRefSaveTimer = 0;

  $effect(() => {
    if (!cdmPaneAllowsRefDraftAutosave(cdmDetailPane) || !membership) return;
    void editRelatedRefs;
    void membership.related_refs;
    window.clearTimeout(relatedSaveTimer);
    relatedSaveTimer = window.setTimeout(
      () => void flushRelatedRefsToServer(),
      550,
    );
    return () => window.clearTimeout(relatedSaveTimer);
  });

  $effect(() => {
    if (!cdmPaneAllowsRefDraftAutosave(cdmDetailPane) || !membership) return;
    void editResolutionRefs;
    void membership.resolution_refs;
    window.clearTimeout(resolutionRefSaveTimer);
    resolutionRefSaveTimer = window.setTimeout(
      () => void flushResolutionRefsToServer(),
      550,
    );
    return () => window.clearTimeout(resolutionRefSaveTimer);
  });

  $effect(() => {
    if (!cardMenuOpen || !browser) return;
    /** @param {PointerEvent} ev */
    function onDoc(ev) {
      const t = /** @type {Node | undefined} */ (ev.target ?? undefined);
      const el = cardActionsMenuEl;
      if (!t || !(el instanceof HTMLElement)) cardMenuOpen = false;
      else if (!el.contains(t)) cardMenuOpen = false;
    }
    const id = window.requestAnimationFrame(() => {
      document.addEventListener("pointerdown", onDoc, true);
    });
    return () => {
      window.cancelAnimationFrame(id);
      document.removeEventListener("pointerdown", onDoc, true);
    };
  });

  async function persistRiskImmediate() {
    await persistMembershipPatch({ risk: editRisk }, "risk");
  }

  async function persistDueBlur() {
    if (!membership) return;
    const dueDraft = editDueAt.trim() ? datetimeLocalToIso(editDueAt) : null;
    const dueMem = String(membership.due_at ?? "").trim() || null;
    if (dueDraft === dueMem) return;
    await persistMembershipPatch({ due_at: dueDraft }, "due");
  }

  async function persistDocumentBlur() {
    if (!membership) return;
    const docDraft = editDocumentId.trim();
    const nextDoc = docDraft ? `document:${docDraft}` : null;
    const prevDocRaw = String(membership.document_ref ?? "").trim();
    const prevDoc = prevDocRaw || null;
    if (nextDoc === prevDoc) return;
    await persistMembershipPatch({ document_ref: nextDoc }, "document");
  }

  async function persistAssigneesBlur() {
    if (!membership) return;
    const draftAssign = [...editAssignees].map((x) => String(x).trim());
    const memAssign = [...(membership.assignee_refs ?? [])].map((x) =>
      String(x).trim(),
    );
    if (normalizeRefList(draftAssign) === normalizeRefList(memAssign)) return;
    await persistMembershipPatch({ assignee_refs: draftAssign }, "assignees");
  }

  async function persistThreadBlur() {
    if (!membership) return;
    const next = editThreadId.trim();
    const prev = String(membership.thread_id ?? "").trim();
    if (next === prev) return;
    await persistMembershipPatch({ thread_id: next }, "thread");
  }

  function handleBackdropClick(e) {
    if (e.target === e.currentTarget) {
      onclose();
    }
  }

  function pickDetailPane(
    /** @type {"overview" | "resolution" | "timeline" | "revisions"} */ pane,
  ) {
    cdmDetailPane = pane;
    onDetailTabChange?.(pane);
  }

  $effect(() => {
    if (!browser || presentation !== "modal") return;
    /** @param {KeyboardEvent} e */
    function onDocKeydown(e) {
      if (e.key !== "Escape") return;
      if (cardMenuOpen) {
        e.preventDefault();
        e.stopPropagation();
        cardMenuOpen = false;
        return;
      }
      e.preventDefault();
      e.stopPropagation();
      onclose();
    }
    document.addEventListener("keydown", onDocKeydown, true);
    return () => document.removeEventListener("keydown", onDocKeydown, true);
  });

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
    return () => {
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
  let cardAttachContextRefs = $derived(
    uniqueTypedRefs([
      linkedThreadId ? `thread:${linkedThreadId}` : "",
      boardId ? `board:${boardId}` : "",
      cardKey ? `card:${cardKey}` : "",
    ]),
  );
  let resolutionRefsList = $derived(
    Array.isArray(membership?.resolution_refs)
      ? membership.resolution_refs
      : [],
  );

  /** Matches server rule for completing work in the done lane. */
  function refIsTerminalEvidence(ref) {
    const s = String(ref ?? "")
      .trim()
      .toLowerCase();
    return s.startsWith("artifact:") || s.startsWith("event:");
  }
  let membershipColumnIsDone = $derived(
    currentMembershipColumnKey() === "done",
  );
  let hasTerminalEvidenceDraft = $derived.by(() => {
    if (resolutionRefsList.some(refIsTerminalEvidence)) return true;
    return parseDelimitedValues(editResolutionRefs).some(refIsTerminalEvidence);
  });
  let doneColumnOptionDisabled = $derived(
    !membershipColumnIsDone && !hasTerminalEvidenceDraft,
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

  let relatedArtifactRefs = $derived.by(() =>
    typedRefsOnlyWithPrefix(parseDelimitedValues(editRelatedRefs), "artifact:"),
  );

  let resolutionArtifactRefs = $derived.by(() =>
    typedRefsOnlyWithPrefix(
      parseDelimitedValues(editResolutionRefs),
      "artifact:",
    ),
  );

  /**
   * @param {Event & { currentTarget: HTMLInputElement }} event
   * @param {CardAttachTarget} target
   */
  async function handleCardAttachPick(event, target) {
    const input = event.currentTarget;
    const file = input?.files?.[0];
    if (!file || cardAttachBusy) return;
    cardAttachBusy = target;
    cardAttachError = "";
    try {
      const payload = await coreClient.createArtifactAttachment({
        refs: cardAttachContextRefs,
        file,
      });
      const id = String(payload?.artifact?.id ?? "").trim();
      if (!id) {
        cardAttachError = "Upload succeeded but artifact id was missing.";
        return;
      }
      const ref = `artifact:${id}`;
      if (target === "related") {
        editRelatedRefs = mergeTypedRefField(editRelatedRefs, ref);
        await flushRelatedRefsToServer();
      } else {
        editResolutionRefs = mergeTypedRefField(editResolutionRefs, ref);
        await flushResolutionRefsToServer();
      }
    } catch (e) {
      cardAttachError =
        e instanceof Error
          ? e.message
          : String(e ?? "Attachment upload failed");
    } finally {
      cardAttachBusy = null;
      input.value = "";
    }
  }

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

  /** Artifact ids extracted from refs shown on this modal (related + resolution). */
  function artifactIdsFromTypedRefs(refs) {
    const ids = new Set();
    for (const ref of refs) {
      const s = String(ref ?? "").trim();
      if (!s.startsWith("artifact:")) continue;
      const id = s.slice("artifact:".length).trim();
      if (id) ids.add(id);
    }
    return [...ids];
  }

  let modalArtifactRoutesById = $state(
    /** @type {Record<string, Record<string, unknown>>} */ ({}),
  );

  $effect(() => {
    if (!browser) return;
    const fromServer = [...dedupedRelatedRefs, ...resolutionRefsList];
    const fromDraft = [
      ...parseDelimitedValues(editRelatedRefs),
      ...parseDelimitedValues(editResolutionRefs),
    ];
    const ids = artifactIdsFromTypedRefs([
      ...new Set([...fromServer, ...fromDraft]),
    ]);
    if (!ids.length) {
      modalArtifactRoutesById = {};
      return;
    }
    let cancelled = false;
    void (async () => {
      try {
        const resp = await coreClient.listArtifacts({
          ids: ids.join(","),
          state: ["active", "archived", "trashed"],
        });
        const rows = Array.isArray(resp?.artifacts) ? resp.artifacts : [];
        if (cancelled) return;
        modalArtifactRoutesById = buildPrimitiveRefRoutes({
          artifacts: rows,
          events: [],
          cards: [],
          documents: [],
          threadId: linkedThreadId,
        }).artifactRoutesById;
      } catch {
        if (!cancelled) modalArtifactRoutesById = {};
      }
    })();
    return () => {
      cancelled = true;
    };
  });

  let showSummary = $derived(Boolean(summaryText));

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
    class="cdm-modal-actions-footer relative z-30 shrink-0 border-t border-line bg-panel px-4 py-2"
  >
    <div
      class="flex min-w-0 max-w-full flex-wrap items-center gap-2 md:flex-nowrap"
    >
      <label
        class="flex min-w-0 flex-1 items-center gap-2 md:max-w-72 md:flex-none"
      >
        <span class="shrink-0 text-micro text-fg-muted" aria-hidden="true"
          >Column</span
        >
        <select
          bind:value={moveColumnKey}
          onchange={handleColumnSelectChange}
          aria-label="Column"
          class="min-w-0 flex-1 cursor-pointer rounded-md border border-line bg-bg-soft px-2 py-1 pr-7 text-meta text-fg focus:outline-none focus:ring-1 focus:ring-accent"
        >
          {#each board?.column_schema ?? [] as column (column.key)}
            <option
              value={column.key}
              disabled={column.key === "done" && doneColumnOptionDisabled}
            >
              {column.title ||
                boardColumnTitle(column.key, board?.column_schema ?? [])}
            </option>
          {/each}
        </select>
      </label>
      <button
        type="button"
        class="shrink-0 rounded-md px-2 py-1 text-micro text-fg-muted transition-colors hover:bg-bg-soft hover:text-fg disabled:cursor-not-allowed disabled:opacity-40"
        disabled={peerUpState.idx <= 0}
        onclick={handleMoveUpInColumn}
      >
        Move up
      </button>
    </div>
  </div>
{/snippet}

{#snippet propertyLabel(text)}
  <span class="cdm-prop-label shrink-0 select-none text-micro text-fg-muted"
    >{text}</span
  >
{/snippet}

{#snippet propertiesRail()}
  <aside class="cdm-rail flex flex-col gap-0.5">
    <div class="cdm-prop-row">
      {@render propertyLabel("Priority")}
      <div class="flex min-w-0 flex-1 items-center gap-1">
        <select
          id={`cdm-risk-${cardKey}`}
          bind:value={editRisk}
          onchange={() => void persistRiskImmediate()}
          aria-label="Risk"
          class="cdm-prop-control"
          disabled={isSaving("risk")}
        >
          <option value="low">Low</option>
          <option value="medium">Medium</option>
          <option value="high">High</option>
          <option value="critical">Critical</option>
        </select>
        {#if isSaving("risk")}
          <span
            class="inline-block size-2 animate-pulse rounded-full bg-accent"
            title="Saving…"
          ></span>
        {/if}
      </div>
    </div>
    {#if fieldErrors.risk}
      <p class="px-2 text-micro text-danger-text">{fieldErrors.risk}</p>
    {/if}

    <div class="cdm-prop-row">
      {@render propertyLabel("Due")}
      <input
        id={`cdm-due-${cardKey}`}
        bind:value={editDueAt}
        onblur={() => void persistDueBlur()}
        aria-label="Due date"
        class="cdm-prop-control"
        type="datetime-local"
        disabled={isSaving("due")}
      />
    </div>
    {#if fieldErrors.due}
      <p class="px-2 text-micro text-danger-text">{fieldErrors.due}</p>
    {/if}

    <div
      class="cdm-prop-stack"
      role="group"
      aria-label="Assignees"
      onfocusout={(e) => {
        const r = /** @type {HTMLElement | null} */ (e.relatedTarget);
        if (!(e.currentTarget instanceof HTMLElement)) return;
        if (r?.nodeType === 1 && e.currentTarget.contains(r)) return;
        void persistAssigneesBlur();
      }}
    >
      <SearchableEntityPicker
        mode="multi"
        bind:values={editAssignees}
        helperText=""
        items={assigneeActorOptions}
        label="Assignees"
        placeholder="Search people and agents"
        showManualEntry={false}
      />
      {#if isSaving("assignees")}
        <p class="mt-1 text-micro text-fg-muted">Updating assignees…</p>
      {/if}
      {#if fieldErrors.assignees}
        <p class="mt-1 text-micro text-danger-text">{fieldErrors.assignees}</p>
      {/if}
    </div>

    <div
      class="cdm-prop-stack"
      role="group"
      aria-label="Document"
      onfocusout={(e) => {
        const r = /** @type {HTMLElement | null} */ (e.relatedTarget);
        if (!(e.currentTarget instanceof HTMLElement)) return;
        if (r?.nodeType === 1 && e.currentTarget.contains(r)) return;
        void persistDocumentBlur();
      }}
    >
      <SearchableEntityPicker
        bind:value={editDocumentId}
        helperText=""
        label="Document"
        placeholder="Link a document"
        searchFn={searchDocumentOptions}
        showManualEntry={false}
      />
      {#if isSaving("document")}
        <p class="mt-1 text-micro text-fg-muted">Updating document…</p>
      {/if}
      {#if fieldErrors.document}
        <p class="mt-1 text-micro text-danger-text">{fieldErrors.document}</p>
      {/if}
    </div>
  </aside>
{/snippet}

{#snippet saveSpinner(kind)}
  {#if isSaving(kind)}
    <span class="text-micro text-fg-muted" aria-live="polite">Saving…</span>
  {/if}
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
      ? "cdm-panel page-dock-layout--embedded-modal-chat"
      : "cdm-panel cdm-page-panel page-dock-layout page-dock-layout--mobile-only page-dock-layout--fixed-mobile-chat page-dock-layout--card-page-chat"}
    data-card-detail-presentation={presentation}
    data-discussion-dock-host={linkedThreadId ? "" : undefined}
  >
    <div
      class="sticky top-0 z-10 border-b border-line bg-panel px-4 pt-2 sm:px-6 sm:pt-2.5"
    >
      <div class="flex items-center justify-between gap-3">
        <div class="min-w-0 flex-1">
          <h2 class="sr-only">{headerTitle}</h2>
          <div
            class="flex min-w-0 items-baseline gap-1.5 text-micro text-fg-muted"
          >
            <span>Board</span>
            <span class="truncate text-fg">{board?.title ?? boardId}</span>
          </div>
        </div>
        <div class="relative flex shrink-0 items-center gap-1">
          {#if cardKey}
            <ResourceShareMenu resourceId={cardKey} rawRecord={cardItem} />
          {/if}
          <div class="relative" bind:this={cardActionsMenuEl}>
            <button
              type="button"
              aria-label="More card actions"
              aria-expanded={cardMenuOpen}
              aria-haspopup="menu"
              onclick={() => (cardMenuOpen = !cardMenuOpen)}
              class="shrink-0 rounded-md border border-line px-2 py-1.5 text-fg-muted transition-colors hover:bg-line-subtle hover:text-fg"
            >
              <span class="sr-only">More card actions</span>
              <svg
                class="h-4 w-4"
                fill="currentColor"
                viewBox="0 0 24 24"
                aria-hidden="true"
              >
                <circle cx="5" cy="12" r="1.75" />
                <circle cx="12" cy="12" r="1.75" />
                <circle cx="19" cy="12" r="1.75" />
              </svg>
            </button>
            {#if cardMenuOpen}
              <div
                class="absolute right-0 z-40 mt-1 min-w-[10rem] rounded-md border border-line bg-panel py-0.5 text-meta shadow-lg"
                role="menu"
                tabindex="-1"
              >
                <button
                  type="button"
                  role="menuitem"
                  onclick={() => {
                    cardMenuOpen = false;
                    removeCardConfirmOpen = true;
                  }}
                  class="block w-full cursor-pointer px-3 py-1.5 text-left text-meta text-danger-text hover:bg-bg-soft"
                >
                  Remove card
                </button>
              </div>
            {/if}
          </div>
          <button
            type="button"
            class="shrink-0 rounded-md border border-line p-1.5 text-fg-muted transition-colors hover:bg-line-subtle hover:text-fg"
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
        class="relative mt-3 flex flex-wrap gap-0 border-b border-line"
        aria-label="Card sections"
        role="tablist"
      >
        <button
          type="button"
          role="tab"
          data-cdm-pane-tab="overview"
          tabindex={cdmDetailPane === "overview" ? 0 : -1}
          aria-selected={cdmDetailPane === "overview"}
          class={`relative inline-flex cursor-pointer border-0 border-b-2 border-transparent bg-transparent px-3 py-2 text-meta font-medium transition-colors ${cdmDetailPane === "overview" ? "border-accent text-fg" : "text-fg-muted hover:text-fg"}`}
          onpointerdown={() => pickDetailPane("overview")}
          onclick={() => pickDetailPane("overview")}
        >
          Overview
        </button>
        <button
          type="button"
          role="tab"
          data-cdm-pane-tab="resolution"
          data-testid="cdm-tab-resolution"
          tabindex={cdmDetailPane === "resolution" ? 0 : -1}
          aria-selected={cdmDetailPane === "resolution"}
          class={`relative inline-flex cursor-pointer border-0 border-b-2 border-transparent bg-transparent px-3 py-2 text-meta font-medium transition-colors ${cdmDetailPane === "resolution" ? "border-accent text-fg" : "text-fg-muted hover:text-fg"}`}
          onpointerdown={() => pickDetailPane("resolution")}
          onclick={() => pickDetailPane("resolution")}
        >
          Resolution
        </button>
        <button
          type="button"
          role="tab"
          data-cdm-pane-tab="timeline"
          data-testid="cdm-tab-timeline"
          tabindex={cdmDetailPane === "timeline" ? 0 : -1}
          aria-selected={cdmDetailPane === "timeline"}
          class={`relative inline-flex cursor-pointer border-0 border-b-2 border-transparent bg-transparent px-3 py-2 text-meta font-medium transition-colors ${cdmDetailPane === "timeline" ? "border-accent text-fg" : "text-fg-muted hover:text-fg"}`}
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
          class={`relative inline-flex cursor-pointer border-0 border-b-2 border-transparent bg-transparent px-3 py-2 text-meta font-medium transition-colors ${cdmDetailPane === "revisions" ? "border-accent text-fg" : "text-fg-muted hover:text-fg"}`}
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

    <div class="cdm-scroll page-dock-scroll">
      {#if cdmDetailPane === "overview"}
        <div
          class="grid grid-cols-1 md:grid-cols-[minmax(0,1fr)_18rem]"
          data-cdm-panel="overview"
        >
          <!-- Main -->
          <div
            class="order-1 flex min-h-0 min-w-0 flex-col gap-4 px-5 pb-4 pt-5 sm:px-8 sm:pt-7 md:border-r md:border-line"
          >
            {#if Object.keys(fieldErrors).length > 0}
              <div
                class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
              >
                {#each Object.entries(fieldErrors) as [fid, ferr] (`${fid}:${ferr}`)}
                  <div>{ferr}</div>
                {/each}
              </div>
            {/if}

            <!-- Title -->
            <div>
              <input
                bind:value={editTitle}
                data-anx-mod-enter-commit="blur"
                use:inlineEditEscape={{
                  disabled: isSaving("title"),
                  onRevert: () => syncCardDraftsFromItem(cardItem),
                  onAfter: (el) => el.blur(),
                }}
                onblur={() => void commitTitleField()}
                onkeydown={(ev) => {
                  if (ev.key === "Enter") {
                    ev.preventDefault();
                    ev.currentTarget.blur();
                  }
                }}
                placeholder="Untitled card"
                class="cdm-title-input"
                aria-label="Card title"
                disabled={isSaving("title")}
              />
              {#if isSaving("title") || fieldErrors.title}
                <div class="mt-1 flex flex-wrap items-center gap-2 text-micro">
                  {@render saveSpinner("title")}
                  {#if fieldErrors.title}<span class="text-danger-text"
                      >{fieldErrors.title}</span
                    >{/if}
                </div>
              {/if}
            </div>

            <!-- Summary (description) -->
            <section>
              {#if summaryEditing}
                <!-- svelte-ignore a11y_autofocus -->
                <textarea
                  autofocus
                  bind:value={editSummary}
                  data-anx-mod-enter-commit="blur"
                  use:inlineEditEscape={{
                    disabled: isSaving("summary"),
                    onRevert: () => syncCardDraftsFromItem(cardItem),
                    onAfter: () => {
                      summaryEditing = false;
                    },
                  }}
                  onblur={() => void commitSummaryField()}
                  class="cdm-prose-input min-h-[7rem]"
                  aria-label="Card summary"
                  placeholder="Write a description…"
                  disabled={isSaving("summary")}
                ></textarea>
                <div class="mt-1 flex min-h-[0.75rem]">
                  {@render saveSpinner("summary")}
                </div>
              {:else}
                <button
                  type="button"
                  class="cdm-prose-display group w-full cursor-text rounded-sm text-left"
                  onclick={() => (summaryEditing = true)}
                  disabled={isSaving("summary")}
                  aria-label="Edit summary"
                >
                  {#if showSummary}
                    <MarkdownRenderer
                      source={summaryText}
                      class="markdown-rendered--card-content"
                    />
                  {:else}
                    <span class="text-meta text-fg-muted"
                      >Write a description…</span
                    >
                  {/if}
                </button>
              {/if}
              {#if fieldErrors.summary}
                <p class="mt-1 text-micro text-danger-text">
                  {fieldErrors.summary}
                </p>
              {/if}
            </section>

            <!-- Supporting attachments (related refs only) -->
            <section>
              <div class="mb-1 flex items-baseline justify-between gap-2">
                <h3
                  class="text-[11px] font-semibold uppercase tracking-wide text-fg-muted"
                >
                  Attachments
                </h3>
                <div class="flex items-center gap-2">
                  <label
                    class="inline-flex cursor-pointer items-center gap-1 rounded-md px-1.5 py-0.5 text-micro text-fg-muted transition-colors hover:bg-bg-soft hover:text-fg {!cardAttachContextRefs.length ||
                    cardAttachBusy
                      ? 'pointer-events-none opacity-40'
                      : ''}"
                  >
                    <svg
                      class="h-3 w-3"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      stroke-width="2.5"
                      aria-hidden="true"
                    >
                      <path
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        d="M12 4v16m8-8H4"
                      />
                    </svg>
                    <span
                      >{cardAttachBusy === "related"
                        ? "Uploading…"
                        : "Add"}</span
                    >
                    <input
                      class="sr-only"
                      accept={CARD_ATTACHMENT_ACCEPT}
                      disabled={Boolean(cardAttachBusy) ||
                        !cardAttachContextRefs.length}
                      onchange={(e) =>
                        void handleCardAttachPick(
                          /** @type {any} */ (e),
                          "related",
                        )}
                      type="file"
                    />
                  </label>
                </div>
              </div>
              {#if relatedArtifactRefs.length > 0}
                <ul class="flex min-w-0 flex-wrap gap-1.5">
                  {#each relatedArtifactRefs as ref (ref)}
                    <li class="min-w-0 text-micro">
                      <RefLink
                        variant="compact"
                        refValue={ref}
                        threadId={linkedThreadId}
                        {boardId}
                        humanize
                        showRaw
                        labelHints={refLabelHints}
                        artifactRoutesById={modalArtifactRoutesById}
                      />
                    </li>
                  {/each}
                </ul>
              {:else}
                <p class="text-micro text-fg-muted">No attachments yet.</p>
              {/if}
              {#if cardAttachError}
                <p class="mt-1 text-micro text-danger-text">
                  {cardAttachError}
                </p>
              {/if}
            </section>

            <!-- References (collapsed group) -->
            <details class="cdm-disclosure" open={relatedRefsList.length > 0}>
              <summary
                class="cursor-pointer text-[11px] font-semibold uppercase tracking-wide text-fg-muted marker:text-fg-muted hover:text-fg"
              >
                Related refs
              </summary>
              <div class="mt-2">
                <GuidedTypedRefsInput
                  bind:value={editRelatedRefs}
                  {boardId}
                  threadId={linkedThreadId}
                  artifactRoutesById={modalArtifactRoutesById}
                  labelHints={refLabelHints}
                  suppressArtifactChipList={true}
                  hideAttachFileControl={true}
                  addInputLabel="Add related ref"
                  addInputPlaceholder="topic:summer-menu-rollout"
                  addButtonLabel="Add ref"
                  emptyText="No related refs yet."
                  helperText=""
                  textareaAriaLabel="Card related refs"
                  fieldError={fieldErrors.related_refs}
                  attachContextRefs={cardAttachContextRefs}
                />
              </div>
            </details>

            <!-- Mobile properties: between content and footer -->
            <details
              class="order-2 -mx-5 mt-1 border-t border-line px-5 pt-3 sm:-mx-8 sm:px-8 md:hidden"
            >
              <summary
                class="cursor-pointer text-[11px] font-semibold uppercase tracking-wide text-fg-muted marker:text-fg-muted"
              >
                Properties
              </summary>
              <div class="mt-2">
                {@render propertiesRail()}
              </div>
            </details>

            <!-- Advanced + meta -->
            <details class="cdm-disclosure">
              <summary
                class="cursor-pointer text-[11px] font-semibold uppercase tracking-wide text-fg-muted marker:text-fg-muted hover:text-fg"
              >
                Advanced
              </summary>
              <div
                class="mt-3 space-y-3"
                role="group"
                aria-label="Topic or thread"
                onfocusout={(e) => {
                  const r = /** @type {HTMLElement | null} */ (e.relatedTarget);
                  if (!(e.currentTarget instanceof HTMLElement)) return;
                  if (r?.nodeType === 1 && e.currentTarget.contains(r)) return;
                  void persistThreadBlur();
                }}
              >
                <SearchableEntityPicker
                  bind:value={editThreadId}
                  advancedLabel="Use a manual thread ID"
                  disabledIds={[backingThreadId].filter(Boolean)}
                  helperText="Changing this updates the card threading context."
                  label="Topic or thread"
                  manualLabel="Thread ID"
                  manualPlaceholder="thread-onboarding"
                  placeholder="Search topics by title or ID"
                  searchFn={searchThreadOptions}
                />
                {#if fieldErrors.thread}
                  <p class="text-micro text-danger-text">
                    {fieldErrors.thread}
                  </p>
                {/if}
                <IdsIntegrityDisclosure
                  rows={cardIntegrityRows}
                  rawJson={cardRawJson}
                  rawJsonCopyLabel="Copy card JSON"
                />
              </div>
            </details>

            <!-- Compact meta footer -->
            {#if cardFreshness || derivedSummary?.latest_activity_at || nonZeroDerivedCounts.length > 0 || (cardInspectNav && cardTopicThreadRef && !duplicateTopicThreadNavLink) || membership?.updated_at}
              <div
                class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-line pt-3 text-micro text-fg-muted"
              >
                {#if cardInspectNav && cardTopicThreadRef && !duplicateTopicThreadNavLink}
                  <span class="flex items-baseline gap-1">
                    <span
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
                      artifactRoutesById={modalArtifactRoutesById}
                    />
                  </span>
                {/if}
                {#if cardFreshness}
                  <span class="flex items-center gap-1">
                    <span>Projection</span>
                    <span
                      class="rounded-md px-1 font-medium {freshnessStatusTone(
                        cardFreshness.status,
                      )}">{freshnessStatusLabel(cardFreshness.status)}</span
                    >
                  </span>
                {/if}
                {#if derivedSummary?.latest_activity_at}
                  <span
                    >Activity {formatTimestamp(
                      derivedSummary.latest_activity_at,
                    ) || "—"}</span
                  >
                {/if}
                {#if membership?.updated_at}
                  <span
                    >Updated {formatTimestamp(
                      membership.updated_at,
                    )}{#if membership?.updated_by}
                      · {actorName(membership.updated_by)}{/if}</span
                  >
                {/if}
                {#each nonZeroDerivedCounts as { label, count } (label)}
                  <span class="flex items-baseline gap-1">
                    <span>{label}</span>
                    <span class="font-medium text-fg">{count}</span>
                  </span>
                {/each}
              </div>
            {/if}
          </div>

          <!-- Desktop rail -->
          <div
            class="order-2 hidden min-w-0 px-5 pb-4 pt-5 sm:px-6 md:block md:overflow-visible"
          >
            {@render propertiesRail()}
          </div>
        </div>
      {:else if cdmDetailPane === "resolution"}
        <div
          class="grid grid-cols-1 md:grid-cols-[minmax(0,1fr)_18rem]"
          data-cdm-panel="resolution"
        >
          <div
            class="order-1 flex min-h-0 min-w-0 flex-col gap-4 px-5 pb-4 pt-5 sm:px-8 sm:pt-7 md:border-r md:border-line"
          >
            {#if Object.keys(fieldErrors).length > 0}
              <div
                class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
              >
                {#each Object.entries(fieldErrors) as [fid, ferr] (`${fid}:${ferr}`)}
                  <div>{ferr}</div>
                {/each}
              </div>
            {/if}

            {#if doneColumnOptionDisabled && !membershipColumnIsDone}
              <p
                class="rounded-md border border-line bg-bg-soft px-3 py-2 text-micro text-fg-muted"
              >
                Moving to <span class="text-fg">Done</span> requires at least
                one
                <span class="font-mono text-fg">artifact:</span> or
                <span class="font-mono text-fg">event:</span> ref in resolution refs
                (for example uploaded evidence).
              </p>
            {/if}

            <!-- Definition of done -->
            <section>
              <h3
                class="mb-1 text-[11px] font-semibold uppercase tracking-wide text-fg-muted"
              >
                Definition of done
              </h3>
              {#if dodEditing}
                <!-- svelte-ignore a11y_autofocus -->
                <textarea
                  autofocus
                  bind:value={editDefinitionOfDone}
                  data-anx-mod-enter-commit="blur"
                  use:inlineEditEscape={{
                    disabled: isSaving("definition_of_done"),
                    onRevert: () => syncCardDraftsFromItem(cardItem),
                    onAfter: () => {
                      dodEditing = false;
                    },
                  }}
                  onblur={() => void commitDodField()}
                  class="cdm-prose-input cdm-prose-input--section-align min-h-[5rem]"
                  aria-label="Definition of done (one idea per line)"
                  disabled={isSaving("definition_of_done")}
                  placeholder="One criterion per line"
                ></textarea>
                {@render saveSpinner("definition_of_done")}
                {#if fieldErrors.definition_of_done}
                  <p class="mt-1 text-micro text-danger-text">
                    {fieldErrors.definition_of_done}
                  </p>
                {/if}
              {:else}
                <button
                  type="button"
                  class="cdm-prose-display cdm-prose-display--section-align w-full cursor-text rounded-sm text-left"
                  onclick={() => (dodEditing = true)}
                  disabled={isSaving("definition_of_done")}
                  aria-label="Edit definition of done"
                >
                  {#if dodItems.length > 0}
                    <ul class="list-inside list-disc space-y-1 text-meta">
                      {#each dodItems as line (line)}
                        <li>{line}</li>
                      {/each}
                    </ul>
                  {:else}
                    <span class="text-meta text-fg-muted">Add criteria…</span>
                  {/if}
                </button>
              {/if}
            </section>

            <!-- Evidence (resolution artifact refs) -->
            <section>
              <div class="mb-1 flex items-baseline justify-between gap-2">
                <h3
                  class="text-[11px] font-semibold uppercase tracking-wide text-fg-muted"
                >
                  Evidence
                </h3>
                <label
                  class="inline-flex cursor-pointer items-center gap-1 rounded-md px-1.5 py-0.5 text-micro text-fg-muted transition-colors hover:bg-bg-soft hover:text-fg {!cardAttachContextRefs.length ||
                  cardAttachBusy
                    ? 'pointer-events-none opacity-40'
                    : ''}"
                  title="Upload file as resolution evidence"
                >
                  <svg
                    class="h-3 w-3"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2.5"
                    aria-hidden="true"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M12 4v16m8-8H4"
                    />
                  </svg>
                  <span
                    >{cardAttachBusy === "resolution"
                      ? "Uploading…"
                      : "Evidence"}</span
                  >
                  <input
                    class="sr-only"
                    accept={CARD_ATTACHMENT_ACCEPT}
                    disabled={Boolean(cardAttachBusy) ||
                      !cardAttachContextRefs.length}
                    onchange={(e) =>
                      void handleCardAttachPick(
                        /** @type {any} */ (e),
                        "resolution",
                      )}
                    type="file"
                  />
                </label>
              </div>
              {#if resolutionArtifactRefs.length > 0}
                <ul class="flex min-w-0 flex-wrap gap-1.5">
                  {#each resolutionArtifactRefs as ref (ref)}
                    <li class="min-w-0 text-micro">
                      <RefLink
                        variant="compact"
                        refValue={ref}
                        threadId={linkedThreadId}
                        {boardId}
                        humanize
                        showRaw
                        labelHints={refLabelHints}
                        artifactRoutesById={modalArtifactRoutesById}
                      />
                    </li>
                  {/each}
                </ul>
              {:else}
                <p class="text-micro text-fg-muted">No evidence files yet.</p>
              {/if}
              {#if cardAttachError}
                <p class="mt-1 text-micro text-danger-text">
                  {cardAttachError}
                </p>
              {/if}
            </section>

            <section>
              <h3
                class="mb-2 text-[11px] font-semibold uppercase tracking-wide text-fg-muted"
              >
                Resolution refs
              </h3>
              <GuidedTypedRefsInput
                bind:value={editResolutionRefs}
                {boardId}
                threadId={linkedThreadId}
                artifactRoutesById={modalArtifactRoutesById}
                labelHints={refLabelHints}
                suppressArtifactChipList={true}
                hideAttachFileControl={true}
                addInputLabel="Add resolution ref"
                addInputPlaceholder="artifact:supporting-context"
                addButtonLabel="Add ref"
                emptyText="No resolution refs yet."
                helperText=""
                textareaAriaLabel="Card resolution refs"
                fieldError={fieldErrors.resolution_refs}
                attachContextRefs={cardAttachContextRefs}
              />
            </section>

            <details
              class="order-2 -mx-5 mt-1 border-t border-line px-5 pt-3 sm:-mx-8 sm:px-8 md:hidden"
            >
              <summary
                class="cursor-pointer text-[11px] font-semibold uppercase tracking-wide text-fg-muted marker:text-fg-muted"
              >
                Properties
              </summary>
              <div class="mt-2">
                {@render propertiesRail()}
              </div>
            </details>
          </div>

          <div
            class="order-2 hidden min-w-0 px-5 pb-4 pt-5 sm:px-6 md:block md:overflow-visible"
          >
            {@render propertiesRail()}
          </div>
        </div>
      {:else if cdmDetailPane === "timeline"}
        <div class="px-4 pb-4 pt-1" data-cdm-panel="timeline">
          {#if cardKey}
            <TimelineTab threadId={linkedThreadId} {boardId} compact />
          {:else}
            <p class="text-meta text-fg-muted">No card identity.</p>
          {/if}
        </div>
      {:else if cdmDetailPane === "revisions"}
        <div class="px-4 pb-4 pt-2" data-cdm-panel="revisions">
          <div class="mb-2 flex items-center justify-between gap-3">
            <p class="text-[11px] uppercase tracking-wide text-fg-muted">
              Head revision {membership?.head_revision_number ?? ""}
            </p>
            <button
              type="button"
              class="rounded-md px-1.5 py-0.5 text-[11px] text-fg-muted hover:bg-bg-soft hover:text-fg"
              onclick={() => loadCardRevisions(cardKey)}
            >
              Refresh
            </button>
          </div>
          {#if revisionsLoading}
            <p class="text-meta text-fg-muted">Loading revisions...</p>
          {:else if revisionsError}
            <p class="text-meta text-red-400">{revisionsError}</p>
          {:else if cardRevisions.length === 0}
            <p class="text-meta text-fg-muted">No revisions found.</p>
          {:else}
            <ol
              class="overflow-hidden rounded-md border border-line bg-panel divide-y divide-line"
            >
              {#each cardRevisions as rev, i (String(rev?.revision_id ?? rev?.id ?? i))}
                {@const parent = cardRevisions[i + 1] ?? null}
                {@const delta = diffCardRevisionAgainstParent(parent, rev)}
                {@const changedSummary = delta
                  .map((d) => humanizeRevisionFieldKey(d.field))
                  .join(", ")}
                <li class="px-2 py-1.5">
                  <div
                    class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5"
                  >
                    <div class="flex min-w-0 flex-1 items-baseline gap-1.5">
                      <span
                        class="shrink-0 text-[12px] font-semibold tabular-nums text-fg"
                        >r{rev.revision_number}</span
                      >
                      <span
                        class="min-w-0 flex-1 truncate text-[12px] text-fg-muted"
                      >
                        {#if parent == null}
                          Initial revision
                        {:else if delta.length === 0}
                          No tracked field changes
                        {:else}
                          Changed <span class="text-fg">{changedSummary}</span>
                        {/if}
                      </span>
                    </div>
                    {#if rev.artifact_ref}
                      <span
                        class="inline-flex max-w-full min-w-0 items-center gap-1"
                      >
                        <span
                          class="shrink-0 whitespace-nowrap text-[10px] leading-none text-fg-muted"
                          >View:</span
                        >
                        <RefLink
                          refValue={String(rev.artifact_ref)}
                          threadId={linkedThreadId}
                          {boardId}
                          humanize
                          labelHints={refLabelHints}
                          artifactRoutesById={modalArtifactRoutesById}
                          attachmentChipSize="tight"
                        />
                      </span>
                    {/if}
                    <span
                      class="shrink-0 whitespace-nowrap text-[11px] tabular-nums text-fg-muted"
                      >{actorName(rev.created_by)} · {formatTimestamp(
                        rev.created_at,
                      )}</span
                    >
                  </div>

                  {#if delta.length > 0}
                    <div class="mt-0.5 pl-[1.375rem] space-y-0.5">
                      {#each delta as block (block.field)}
                        <div class="min-w-0">
                          {#if delta.length > 1}
                            <p
                              class="text-[10px] font-semibold uppercase tracking-wide text-fg-muted"
                            >
                              {humanizeRevisionFieldKey(block.field)}
                            </p>
                          {/if}
                          <div class="font-mono text-[11px] leading-snug">
                            {#each block.lines as ln, li (li + ln.kind)}
                              <p
                                class="break-words {ln.kind === 'add'
                                  ? 'text-ok-text'
                                  : 'text-danger-text'}"
                              >
                                <span class="select-none mr-1 text-fg-muted"
                                  >{ln.kind === "add" ? "+" : "−"}</span
                                >{ln.text}
                              </p>
                            {/each}
                          </div>
                        </div>
                      {/each}
                    </div>
                  {/if}
                </li>
              {/each}
            </ol>
          {/if}
        </div>
      {/if}
    </div>

    {#if linkedThreadId}
      <div class="page-dock-feed">
        <DiscussionDrawer
          layout="dock"
          dockPlacement="embedded"
          threadId={linkedThreadId}
          {workspaceId}
          {workspaceSlug}
          label="Discussion"
          storageKey={`card-discussion:${cardKey}`}
          resizeStorageKey={`card-discussion-v2:${cardKey}`}
          narrowEdgeToEdge
        />
      </div>
    {/if}

    {@render cardActionsFooter()}
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
    max-width: 60rem;
    flex-direction: column;
    overflow: hidden;
    border-radius: 0.5rem;
    border: 1px solid var(--line);
    background: var(--panel);
    box-shadow: var(--shadow-modal);
  }

  /* ---- Notion-style title input ---- */
  :global(.cdm-title-input) {
    display: block;
    width: 100%;
    border: 0;
    background: transparent;
    padding: 0.125rem 0.25rem;
    margin-left: -0.25rem;
    font-size: 22px;
    font-weight: 600;
    line-height: 1.25;
    letter-spacing: -0.01em;
    color: var(--fg);
    border-radius: 0.25rem;
  }
  :global(.cdm-title-input::placeholder) {
    color: var(--fg-muted);
    font-weight: 600;
  }
  :global(.cdm-title-input:focus) {
    outline: none;
    background: var(--bg-soft);
  }

  /* ---- Notion-style prose editor (summary, DoD) ---- */
  :global(.cdm-prose-input) {
    display: block;
    width: 100%;
    border: 0;
    background: var(--bg-soft);
    padding: 0.5rem 0.625rem;
    margin-left: -0.625rem;
    margin-right: -0.625rem;
    color: var(--fg);
    font-size: 13px;
    line-height: 1.55;
    border-radius: 0.375rem;
    resize: vertical;
  }
  :global(.cdm-prose-input::placeholder) {
    color: var(--fg-muted);
  }
  :global(.cdm-prose-input:focus) {
    outline: none;
    box-shadow: 0 0 0 1px var(--accent);
  }

  /* Under explicit section headings (e.g. Definition of done); keep flush with Evidence / refs. */
  :global(.cdm-prose-input--section-align) {
    margin-left: 0;
    margin-right: 0;
  }

  :global(.cdm-prose-display) {
    display: block;
    border: 0;
    background: transparent;
    padding: 0.25rem 0.375rem;
    margin-left: -0.375rem;
    margin-right: -0.375rem;
    color: var(--fg);
    font-size: 13px;
    line-height: 1.55;
    transition: background-color 120ms ease;
  }
  :global(.cdm-prose-display:hover:not(:disabled)) {
    background: var(--bg-soft);
  }

  :global(.cdm-prose-display--section-align) {
    margin-left: 0;
    margin-right: 0;
  }

  /* ---- Right-rail property rows (Linear-style compact) ---- */
  :global(.cdm-rail) {
    font-size: 12px;
  }
  :global(.cdm-prop-row) {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.25rem 0;
    min-height: 2rem;
  }
  :global(.cdm-prop-label) {
    width: 5.5rem;
    flex-shrink: 0;
  }
  :global(.cdm-prop-control) {
    flex: 1;
    min-width: 0;
    border: 1px solid transparent;
    background: transparent;
    color: var(--fg);
    font-size: 12px;
    padding: 0.25rem 0.375rem;
    border-radius: 0.25rem;
    transition:
      background 120ms ease,
      border-color 120ms ease;
  }
  :global(.cdm-prop-control:hover:not(:disabled)) {
    background: var(--bg-soft);
  }
  :global(.cdm-prop-control:focus) {
    outline: none;
    background: var(--bg-soft);
    border-color: var(--accent);
  }
  :global(.cdm-prop-stack) {
    padding: 0.5rem 0;
    border-top: 1px dashed var(--line);
    margin-top: 0.25rem;
  }
  :global(.cdm-prop-stack:first-of-type) {
    border-top: 0;
    margin-top: 0;
  }

  :global(.cdm-disclosure > summary) {
    list-style: none;
    user-select: none;
  }
  :global(.cdm-disclosure > summary::-webkit-details-marker) {
    display: none;
  }
  :global(.cdm-disclosure > summary)::before {
    content: "▸";
    display: inline-block;
    margin-right: 0.375rem;
    transition: transform 120ms ease;
    color: var(--fg-muted);
    font-size: 9px;
  }
  :global(.cdm-disclosure[open] > summary)::before {
    transform: rotate(90deg);
  }

  .cdm-scroll {
    flex: 1;
    min-height: 0;
    -webkit-overflow-scrolling: touch;
    overscroll-behavior-y: contain;
    overflow-y: auto;
    scroll-padding-block-start: 0.5rem;
  }

  .cdm-page {
    min-height: 0;
  }

  .cdm-page-panel {
    min-height: calc(100dvh - 9rem);
    max-height: none;
    box-shadow: none;
  }

  @media (max-width: 1023px) {
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
      height: calc(100dvh - 6.75rem);
      min-height: 0;
      max-height: calc(100dvh - 6.75rem);
      border-inline: 0;
      border-radius: 0;
    }
  }
</style>
