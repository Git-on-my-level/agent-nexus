<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import ActorLabel from "$lib/components/ActorLabel.svelte";
  import Button from "$lib/components/Button.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import Icon from "$lib/components/Icon.svelte";
  import IdsIntegrityDisclosure from "$lib/components/IdsIntegrityDisclosure.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import ResourceShareMenu from "$lib/components/ResourceShareMenu.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import DocumentMarkdownEditor from "$lib/components/DocumentMarkdownEditor.svelte";
  import { dismissOnEscape } from "$lib/actions/dismissOnEscape.js";
  import { inlineEditEscape } from "$lib/actions/inlineEditEscape.js";
  import { extractDocumentOutline } from "$lib/markdown.js";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { splitTypedRef } from "$lib/inboxUtils";
  import { bindWorkspaceHref, workspacePath } from "$lib/workspacePaths";
  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import DocumentDiscussionRail from "$lib/components/document-detail/DocumentDiscussionRail.svelte";
  import WorkspaceResourceTopRow from "$lib/components/WorkspaceResourceTopRow.svelte";
  import { buildDocumentCommentFields } from "$lib/documentCommentAnchor.js";
  import {
    applyDocumentCommentHighlights,
    clearDocumentCommentMarks,
    eventIdsFromDocCommentMark,
  } from "$lib/documentCommentHighlight.js";
  import {
    docCommentBodyFocus,
    docCommentBodyHover,
  } from "$lib/stores/docCommentBodyRailSync.js";
  import {
    isInternalUuid,
    resourceCopyValue,
    resourceDisplayLabel,
    resourceRouteSegment,
  } from "$lib/resourceIdentity.js";
  import { tick } from "svelte";

  let { data } = $props();

  let documentId = $derived($page.params.documentId);
  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let requestedRevisionId = $derived(
    String($page.url.searchParams.get("revision") ?? "").trim(),
  );
  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );

  let document = $state(null);
  let headRevision = $state(null);
  let revisions = $state([]);
  let selectedRevision = $state(null);
  let loading = $state(false);
  let historyLoading = $state(false);
  let loadError = $state("");
  let loadedDocumentId = $state("");
  let historyOpen = $state(false);
  /** When the doc has a thread, revisions share the discussion rail (`discussion` vs `revisions`). */
  let docRailSidePanel = $state(
    /** @type {'discussion' | 'revisions'} */ ("discussion"),
  );
  /**
   * Per polish §P5 the breadcrumb shows the parent topic when the document is
   * anchored to a topic ref. We cache `{id, title}` here and only fetch when
   * the resolved topic id changes; failures fall back to the doc-only crumb.
   */
  let parentTopic = $state(
    /** @type {{ id: string, title: string } | null} */ (null),
  );
  let parentTopicLoadedFor = $state("");

  let editOpen = $state(false);
  let editDraft = $state({
    content: "",
    title: "",
  });
  let saving = $state(false);
  let saveError = $state("");
  /** Notion-style click-to-rename on the prominent document title. */
  let titleEditing = $state(false);
  let titleDraft = $state("");
  let titleSaving = $state(false);
  let titleError = $state("");
  let titleInputEl = $state(/** @type {HTMLInputElement | null} */ (null));
  /** Active table-of-contents heading id (scrollspy). */
  let activeOutlineId = $state("");
  let loadingSelectedRevisionKey = $state("");
  let confirmModal = $state({ open: false, action: "" });
  let docLifecycleBusy = $state(false);
  /**
   * Per polish §P8: Archive and Trash collapse into a single "More actions"
   * kebab so "Edit" (new revision) is the only competing primary in the doc header.
   */
  let moreActionsOpen = $state(false);
  let moreActionsRoot = $state(null);
  let documentLoadRequestId = 0;
  let documentHistoryRequestId = 0;
  let documentRouteSegment = $derived(
    resourceRouteSegment(document, "document") || documentId,
  );
  /** Selection stash + discussion rail for document text comments */
  let docBodyMarkdownRoot = $state(null);
  let docStashedSelection = $state("");
  /**
   * Position of the floating "Comment" pill while the operator has an
   * active selection in the doc body. Coordinates are page-absolute
   * (`window.scrollY/X` baked in) so the pill survives scrolling without
   * jumping. Null when no usable selection — the pill is then hidden.
   */
  let docSelectionPillPos = $state(
    /** @type {{ top: number, left: number } | null} */ (null),
  );
  let pendingDocumentComment = $state(null);
  let discussionOpenSignal = $state(0);
  /** @type {{ posted: Array<{ eventId: string, quote: string }>, activeAnchoredCount: number }} */
  let documentAnchorContext = $state({
    posted: [],
    activeAnchoredCount: 0,
  });
  function toggleMoreActions() {
    moreActionsOpen = !moreActionsOpen;
  }
  function closeMoreActions() {
    moreActionsOpen = false;
  }

  $effect(() => {
    if (!moreActionsOpen) return;
    function onDocPointerDown(e) {
      if (
        moreActionsRoot &&
        e.target instanceof Node &&
        !moreActionsRoot.contains(e.target)
      ) {
        moreActionsOpen = false;
      }
    }
    window.document.addEventListener("pointerdown", onDocPointerDown, true);
    return () => {
      window.document.removeEventListener(
        "pointerdown",
        onDocPointerDown,
        true,
      );
    };
  });
  function documentTopicRefForLink(doc) {
    if (!doc || typeof doc !== "object") return "";
    const sr = String(doc.subject_ref ?? "").trim();
    if (sr) {
      const { prefix, id } = splitTypedRef(sr);
      if (prefix === "topic" && id) return `topic:${id}`;
      if (prefix === "thread" && id) return `thread:${id}`;
      if (!sr.includes(":") && sr) return `topic:${sr}`;
    }
    const tid = String(doc.thread_id ?? "").trim();
    return tid ? `thread:${tid}` : "";
  }

  let documentTopicRefValue = $derived(
    document ? documentTopicRefForLink(document) : "",
  );

  let parentTopicId = $derived.by(() => {
    if (!document) return "";
    const sr = String(document.subject_ref ?? "").trim();
    if (!sr) return "";
    const { prefix, id } = splitTypedRef(sr);
    return prefix === "topic" && id ? String(id) : "";
  });

  $effect(() => {
    const tid = parentTopicId;
    if (!tid) {
      parentTopic = null;
      parentTopicLoadedFor = "";
      return;
    }
    if (parentTopicLoadedFor === tid) return;
    parentTopicLoadedFor = tid;
    void (async () => {
      try {
        const result = await coreClient.getTopic(tid);
        const t = result?.topic ?? result ?? null;
        if (parentTopicLoadedFor !== tid) return;
        if (t && (t.id === tid || String(t.id ?? "") === tid)) {
          parentTopic = {
            id: tid,
            title: String(t.title ?? "").trim() || tid,
          };
        } else {
          parentTopic = { id: tid, title: tid };
        }
      } catch {
        if (parentTopicLoadedFor === tid) {
          parentTopic = null;
        }
      }
    })();
  });

  let displayedContent = $derived(
    selectedRevision?.content ?? headRevision?.content ?? "",
  );
  let displayedRevision = $derived(selectedRevision ?? headRevision);
  /** Outline (H1-H3) for the table-of-contents; only shown for longer docs. */
  let docOutline = $derived(extractDocumentOutline(displayedContent));
  let showOutline = $derived(!editOpen && docOutline.length >= 3);
  /** Unsaved-content indicator for the editor footer. */
  let editorDirty = $derived(
    editOpen && editDraft.content !== (headRevision?.content ?? ""),
  );
  let isViewingOldRevision = $derived(
    selectedRevision &&
      selectedRevision.revision_id !== headRevision?.revision_id,
  );

  let docIntegrityRows = $derived.by(() => {
    const d = document;
    const rev = displayedRevision;
    if (!d) return [];
    const rows = [];
    if (rev?.content_hash) {
      rows.push({
        label: "Content hash",
        value: String(rev.content_hash),
        copyLabel: "Copy content hash",
      });
    }
    if (rev?.revision_hash) {
      rows.push({
        label: "Revision hash",
        value: String(rev.revision_hash),
        copyLabel: "Copy revision hash",
      });
    }
    if (d.id) {
      rows.push({
        label: "Document ref",
        value: resourceCopyValue("document", d),
        copyLabel: "Copy document ref",
      });
    }
    if (rev?.revision_id || rev?.ref || rev?.handle) {
      rows.push({
        label: "Revision ref",
        value:
          String(rev?.ref ?? "").trim() ||
          (String(rev?.handle ?? "").trim()
            ? `document_revision:${String(rev.handle).trim()}`
            : !isInternalUuid(rev?.revision_id)
              ? `document_revision:${String(rev.revision_id ?? "").trim()}`
              : ""),
        copyLabel: "Copy revision ref",
      });
    }
    const threadId = String(d.thread_id ?? "").trim();
    if (threadId) {
      rows.push({
        label: "Thread ref",
        value:
          String(d.thread_ref ?? "").trim() ||
          (threadId && !isInternalUuid(threadId) ? `thread:${threadId}` : ""),
        copyLabel: "Copy thread ref",
      });
    }
    const subjectRef = String(d.subject_ref ?? "").trim();
    if (subjectRef) {
      rows.push({
        label: "Subject ref",
        value: subjectRef,
        copyLabel: "Copy subject ref",
        mono: true,
      });
    }
    return rows;
  });

  let docRawJson = $derived(document ? JSON.stringify(document, null, 2) : "");

  // Only text documents can be edited in the textarea-based editor.
  // Structured and binary revisions must be updated via CLI/API.
  let headContentType = $derived(headRevision?.content_type ?? "text");
  let isTextEditable = $derived(
    headContentType === "text" || headContentType === "",
  );
  /**
   * Inline title rename is only safe on the head of a text document that is not
   * trashed: renaming appends a revision (title isn't patchable via docs.patch),
   * so we reuse the head content as the base body.
   */
  let canEditTitle = $derived(
    Boolean(document) &&
      isTextEditable &&
      !document?.trashed_at &&
      !isViewingOldRevision,
  );

  let workspaceHref = $derived(
    bindWorkspaceHref(organizationSlug, workspaceSlug),
  );

  async function setRequestedRevision(revisionId = "") {
    const next = String(revisionId ?? "").trim();
    const url = new URL($page.url);

    if (next) {
      url.searchParams.set("revision", next);
    } else {
      url.searchParams.delete("revision");
    }

    const href = `${url.pathname}${url.search}${url.hash}`;
    await goto(href, {
      replaceState: true,
      keepFocus: true,
      noScroll: true,
    });
  }

  $effect(() => {
    const id = documentId;
    if (id && id !== loadedDocumentId) loadDocument(id);
  });

  $effect(() => {
    documentId;
    confirmModal = { open: false, action: "" };
  });

  $effect(() => {
    if (!documentId || !headRevision?.revision_id) {
      return;
    }

    const revisionId = requestedRevisionId;
    if (!revisionId || revisionId === headRevision.revision_id) {
      if (selectedRevision) {
        selectedRevision = null;
      }
      return;
    }

    if (selectedRevision?.revision_id === revisionId) {
      return;
    }

    const cachedRevision = revisions.find(
      (rev) => rev.revision_id === revisionId,
    );
    if (cachedRevision?.content) {
      selectedRevision = cachedRevision;
      return;
    }

    void loadSelectedRevision(documentId, revisionId, cachedRevision ?? null);
  });

  function documentRouteKey(id = documentId) {
    return [organizationSlug, workspaceSlug, id].map((v) => String(v ?? ""));
  }

  function isCurrentDocumentLoad(requestId, routeKey) {
    const current = documentRouteKey();
    return (
      requestId === documentLoadRequestId &&
      routeKey.every((segment, index) => segment === current[index])
    );
  }

  function isCurrentDocumentRoute(routeKey) {
    const current = documentRouteKey();
    return routeKey.every((segment, index) => segment === current[index]);
  }

  async function loadDocument(targetId) {
    if (!targetId) return;
    const requestId = ++documentLoadRequestId;
    const routeKey = documentRouteKey(targetId);
    loading = true;
    loadError = "";
    loadedDocumentId = targetId;
    documentHistoryRequestId++;
    document = null;
    headRevision = null;
    revisions = [];
    selectedRevision = null;
    historyLoading = false;
    historyOpen = false;
    docRailSidePanel = "discussion";
    editOpen = false;
    try {
      const result = await coreClient.getDocument(targetId);
      if (!isCurrentDocumentLoad(requestId, routeKey)) return;
      document = result.document ?? null;
      headRevision = result.revision ?? null;
      if (!document) {
        loadError = "Document not found.";
      }
    } catch (e) {
      if (!isCurrentDocumentLoad(requestId, routeKey)) return;
      loadError = `Failed to load document: ${e instanceof Error ? e.message : String(e)}`;
      document = null;
      headRevision = null;
    } finally {
      if (isCurrentDocumentLoad(requestId, routeKey)) {
        loading = false;
      }
    }
  }

  async function openRevisionHistoryNoThread() {
    if (!documentId || document?.thread_id) return;
    if (revisions.length > 0) {
      historyOpen = !historyOpen;
      return;
    }
    historyOpen = true;
    historyLoading = true;
    const requestId = ++documentHistoryRequestId;
    const routeKey = documentRouteKey(documentId);
    try {
      const result = await coreClient.getDocumentHistory(documentId);
      if (
        requestId !== documentHistoryRequestId ||
        !isCurrentDocumentRoute(routeKey)
      )
        return;
      revisions = (result.revisions ?? []).slice().reverse();
    } catch {
      if (
        requestId !== documentHistoryRequestId ||
        !isCurrentDocumentRoute(routeKey)
      )
        return;
      revisions = [];
    } finally {
      if (
        requestId === documentHistoryRequestId &&
        isCurrentDocumentRoute(routeKey)
      ) {
        historyLoading = false;
      }
    }
  }

  /** Fetch revision list when opening the rail Revisions tab (threaded docs). */
  async function ensureRevisionHistoryForRail() {
    if (!documentId || revisions.length > 0) return;
    historyLoading = true;
    const requestId = ++documentHistoryRequestId;
    const routeKey = documentRouteKey(documentId);
    try {
      const result = await coreClient.getDocumentHistory(documentId);
      if (
        requestId !== documentHistoryRequestId ||
        !isCurrentDocumentRoute(routeKey)
      )
        return;
      revisions = (result.revisions ?? []).slice().reverse();
    } catch {
      if (
        requestId !== documentHistoryRequestId ||
        !isCurrentDocumentRoute(routeKey)
      )
        return;
      revisions = [];
    } finally {
      if (
        requestId === documentHistoryRequestId &&
        isCurrentDocumentRoute(routeKey)
      ) {
        historyLoading = false;
      }
    }
  }

  async function selectRevision(rev) {
    if (rev.revision_id === headRevision?.revision_id) {
      await setRequestedRevision("");
      return;
    }
    if (rev.content) {
      selectedRevision = rev;
    }
    await setRequestedRevision(rev.revision_id);
  }

  function returnToHead() {
    void setRequestedRevision("");
  }

  async function loadSelectedRevision(
    targetDocumentId,
    targetRevisionId,
    cachedRevision = null,
  ) {
    const requestKey = `${targetDocumentId}:${targetRevisionId}`;
    if (loadingSelectedRevisionKey === requestKey) {
      return;
    }

    loadingSelectedRevisionKey = requestKey;
    try {
      const result = await coreClient.getDocumentRevision(
        targetDocumentId,
        targetRevisionId,
      );
      if (
        documentId !== targetDocumentId ||
        requestedRevisionId !== targetRevisionId
      ) {
        return;
      }

      const loaded = result.revision ?? cachedRevision;
      if (!loaded) {
        selectedRevision = null;
        return;
      }

      selectedRevision = loaded;
      const idx = revisions.findIndex(
        (r) => r.revision_id === targetRevisionId,
      );
      if (idx >= 0) {
        revisions[idx] = { ...revisions[idx], ...loaded };
      } else if (loaded.revision_id) {
        revisions = [...revisions, loaded];
      }
    } catch {
      if (
        documentId === targetDocumentId &&
        requestedRevisionId === targetRevisionId
      ) {
        selectedRevision = cachedRevision;
      }
    } finally {
      if (loadingSelectedRevisionKey === requestKey) {
        loadingSelectedRevisionKey = "";
      }
    }
  }

  function openEdit() {
    editDraft = {
      content: headRevision?.content ?? "",
      title: document?.title ?? "",
    };
    saveError = "";
    editOpen = true;
    historyOpen = false;
    docRailSidePanel = "discussion";
  }

  function closeEdit() {
    editOpen = false;
    saveError = "";
  }

  async function handleSave() {
    if (!editDraft.content.trim()) {
      saveError = "Content is required.";
      return;
    }

    if (!headRevision?.revision_id) {
      saveError = "Cannot determine base revision. Please reload.";
      return;
    }

    saving = true;
    saveError = "";

    try {
      const docPatch = {};
      if (
        editDraft.title.trim() &&
        editDraft.title.trim() !== document?.title
      ) {
        docPatch.title = editDraft.title.trim();
      }
      const result = await coreClient.updateDocument(documentId, {
        content: editDraft.content.trim(),
        content_type: headContentType || "text",
        if_base_revision: headRevision.revision_id,
        ...(Object.keys(docPatch).length > 0 ? { document: docPatch } : {}),
      });

      document = result.document ?? document;
      headRevision = result.revision ?? headRevision;
      selectedRevision = null;
      revisions = [];
      editOpen = false;
      // Drop ?revision= so we show the new head instead of re-resolving the prior URL.
      if (requestedRevisionId) {
        await setRequestedRevision("");
      }
    } catch (e) {
      saveError = `Failed to save revision: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      saving = false;
    }
  }

  function startTitleEdit() {
    if (!canEditTitle || titleSaving) return;
    titleError = "";
    titleDraft = String(document?.title ?? "").trim();
    titleEditing = true;
    void tick().then(() => {
      titleInputEl?.focus();
      titleInputEl?.select();
    });
  }

  function cancelTitleEdit() {
    titleEditing = false;
    titleError = "";
  }

  async function commitTitleEdit() {
    if (!titleEditing) return;
    const next = titleDraft.trim();
    const current = String(document?.title ?? "").trim();
    if (!next || next === current) {
      titleEditing = false;
      titleError = "";
      return;
    }
    if (!headRevision?.revision_id) {
      titleError = "Cannot rename: missing base revision. Please reload.";
      return;
    }
    titleSaving = true;
    titleError = "";
    try {
      // Title is not patchable via docs.patch, so renaming appends a revision
      // that carries the unchanged head body plus the new title.
      const result = await coreClient.updateDocument(documentId, {
        content: headRevision.content ?? displayedContent,
        content_type: headContentType || "text",
        if_base_revision: headRevision.revision_id,
        document: { title: next },
      });
      document = result.document ?? document;
      headRevision = result.revision ?? headRevision;
      selectedRevision = null;
      revisions = [];
      titleEditing = false;
      if (requestedRevisionId) {
        await setRequestedRevision("");
      }
    } catch (e) {
      titleError = `Failed to rename: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      titleSaving = false;
    }
  }

  /**
   * Table-of-contents scrollspy: highlight the outline entry nearest the top of
   * the viewport as the operator scrolls the rendered body.
   */
  $effect(() => {
    if (typeof window === "undefined") return;
    if (!showOutline || !docBodyMarkdownRoot) {
      activeOutlineId = "";
      return;
    }
    const ids = docOutline.map((h) => h.id);
    void displayedContent;
    function recompute() {
      const root = /** @type {HTMLElement | null} */ (docBodyMarkdownRoot);
      if (!root) return;
      const passed = ids.filter((id) => {
        const el = root.querySelector(`[id="${CSS.escape(id)}"]`);
        return (
          el instanceof HTMLElement && el.getBoundingClientRect().top <= 120
        );
      });
      activeOutlineId = passed.at(-1) ?? ids[0] ?? "";
    }
    recompute();
    window.addEventListener("scroll", recompute, { passive: true });
    window.addEventListener("resize", recompute);
    return () => {
      window.removeEventListener("scroll", recompute);
      window.removeEventListener("resize", recompute);
    };
  });

  function scrollToOutline(id) {
    const root = /** @type {HTMLElement | null} */ (docBodyMarkdownRoot);
    const el = root?.querySelector(`[id="${CSS.escape(id)}"]`);
    if (el instanceof HTMLElement) {
      el.scrollIntoView({ behavior: "smooth", block: "start" });
      activeOutlineId = id;
    }
  }

  async function handleArchiveDocument() {
    if (!documentId || docLifecycleBusy || document?.trashed_at) return;
    docLifecycleBusy = true;
    try {
      await coreClient.archiveDocument(documentId, {});
      await loadDocument(documentId);
    } finally {
      docLifecycleBusy = false;
    }
  }

  async function handleUnarchiveDocument() {
    confirmModal = { open: false, action: "" };
    if (!documentId || docLifecycleBusy || document?.trashed_at) return;
    docLifecycleBusy = true;
    try {
      await coreClient.unarchiveDocument(documentId, {});
      await loadDocument(documentId);
    } finally {
      docLifecycleBusy = false;
    }
  }

  function handleConfirm() {
    const action = confirmModal.action;
    confirmModal = { open: false, action: "" };
    if (action === "archive") handleArchiveDocument();
    else if (action === "trash") handleTrashDocument();
  }

  async function handleTrashDocument() {
    if (!documentId || docLifecycleBusy) return;
    docLifecycleBusy = true;
    try {
      await coreClient.trashDocument(documentId, {});
      await goto(workspacePath(organizationSlug, workspaceSlug, "/docs"));
    } finally {
      docLifecycleBusy = false;
    }
  }

  async function handleRestoreDocument() {
    confirmModal = { open: false, action: "" };
    if (!documentId || docLifecycleBusy) return;
    docLifecycleBusy = true;
    try {
      await coreClient.restoreDocument(documentId, {});
      await loadDocument(documentId);
    } finally {
      docLifecycleBusy = false;
    }
  }

  function isSelectionInsideDocBody(sel) {
    if (!sel || sel.isCollapsed || !docBodyMarkdownRoot) {
      return false;
    }
    const root = /** @type {HTMLElement} */ (docBodyMarkdownRoot);
    for (let i = 0; i < sel.rangeCount; i++) {
      const r = sel.getRangeAt(i);
      if (!root.contains(r.commonAncestorContainer)) {
        return false;
      }
    }
    return true;
  }

  /**
   * Determine whether the live selection runs forward (anchor→focus) or
   * backward (focus→anchor). We anchor the floating pill at the *end* of
   * the user's gesture so it appears where their mouse / caret naturally
   * came to rest, rather than at the far edge of a multi-line bounding box.
   */
  function isSelectionForward(sel) {
    if (!sel?.anchorNode || !sel?.focusNode) {
      return true;
    }
    if (sel.anchorNode === sel.focusNode) {
      return sel.anchorOffset <= sel.focusOffset;
    }
    const pos = sel.anchorNode.compareDocumentPosition(sel.focusNode);
    if (pos & Node.DOCUMENT_POSITION_FOLLOWING) return true;
    if (pos & Node.DOCUMENT_POSITION_PRECEDING) return false;
    return true;
  }

  /**
   * Compute page-absolute coordinates for the floating "Comment" pill.
   *
   * For multi-line / wrapped selections, `range.getBoundingClientRect()`
   * returns the rectangle of the *widest* line, which made the pill leap
   * to the far right margin even when the operator finished selecting on
   * a short last line. Instead we walk `range.getClientRects()` (one rect
   * per visual line) and pin the pill to the line where the selection
   * actually ends — below the last line on a forward selection, above the
   * first on a backward one. This matches the Google Docs behaviour where
   * the pill follows the caret rather than the bounding box.
   *
   * `window.scrollX/Y` is baked in so the pill stays put through scrolls.
   */
  function computeSelectionPillPosition(sel) {
    if (!sel || sel.rangeCount === 0) return null;
    const range = sel.getRangeAt(sel.rangeCount - 1);
    const forward = isSelectionForward(sel);
    const rects = range.getClientRects();
    /** @type {DOMRect | undefined} */
    let anchorRect;
    if (rects && rects.length > 0) {
      const list = Array.from(rects);
      const pick = () => {
        if (forward) {
          for (let i = list.length - 1; i >= 0; i--) {
            const r = list[i];
            if (r.width > 0 || r.height > 0) return r;
          }
        } else {
          for (let i = 0; i < list.length; i++) {
            const r = list[i];
            if (r.width > 0 || r.height > 0) return r;
          }
        }
        return forward ? list[list.length - 1] : list[0];
      };
      anchorRect = pick();
    }
    // Double-click / word selection often appends a 0×0 "caret" rect as the
    // last client rect; drag selection usually does not. Empty rects also
    // happen for some engine quirks — getBoundingClientRect() is stable.
    if (!anchorRect || (anchorRect.width === 0 && anchorRect.height === 0)) {
      anchorRect = range.getBoundingClientRect();
    }
    if (!anchorRect || (anchorRect.width === 0 && anchorRect.height === 0)) {
      return null;
    }
    const PILL_GAP_Y = 6;
    const PILL_WIDTH_EST = 110;
    const top = forward
      ? anchorRect.bottom + window.scrollY + PILL_GAP_Y
      : Math.max(0, anchorRect.top + window.scrollY - PILL_GAP_Y - 32);
    const desiredLeft = forward
      ? anchorRect.right + window.scrollX - 8
      : anchorRect.left + window.scrollX - PILL_WIDTH_EST + 8;
    const viewportWidth =
      window.document.documentElement?.clientWidth ?? window.innerWidth ?? 0;
    const left =
      viewportWidth > 0
        ? Math.min(
            window.scrollX + viewportWidth - PILL_WIDTH_EST - 8,
            Math.max(window.scrollX + 8, desiredLeft),
          )
        : desiredLeft;
    return { top, left };
  }

  function refreshStashedDocSelection() {
    if (typeof window === "undefined" || !docBodyMarkdownRoot) {
      return;
    }
    // Hide the pill (and forget the stash) while the editor is open or the
    // doc is in trash — we don't want a "Comment" affordance competing with
    // the active editor textarea selection or pointing at read-only state.
    if (editOpen || document?.trashed_at) {
      docStashedSelection = "";
      docSelectionPillPos = null;
      return;
    }
    const sel = window.getSelection?.();
    if (!sel || !isSelectionInsideDocBody(sel)) {
      docStashedSelection = "";
      docSelectionPillPos = null;
      return;
    }
    docStashedSelection = String(sel.toString() ?? "");
    docSelectionPillPos = docStashedSelection.trim()
      ? computeSelectionPillPosition(sel)
      : null;
  }

  /**
   * Live-update the selection while the operator drags or uses keyboard
   * selection — `selectionchange` fires on the document, so this stays in
   * sync without polling and without an extra mouseup handler on every
   * inline element inside the rendered markdown.
   */
  $effect(() => {
    if (typeof window === "undefined") return;
    function onSelectionChange() {
      refreshStashedDocSelection();
    }
    window.document.addEventListener("selectionchange", onSelectionChange);
    return () => {
      window.document.removeEventListener("selectionchange", onSelectionChange);
    };
  });

  /**
   * ⌘⌥M / Ctrl+Alt+M starts a comment on the current selection. Matches
   * Google Docs' shortcut so muscle memory transfers. We bind it at the
   * page level rather than on the doc body so it works even when focus
   * wandered to the rail's textarea after a previous comment.
   */
  $effect(() => {
    if (typeof window === "undefined") return;
    function onKey(e) {
      const isMod = e.metaKey || e.ctrlKey;
      const isM = String(e.key ?? "").toLowerCase() === "m";
      if (!isMod || !e.altKey || !isM) return;
      if (editOpen || document?.trashed_at) return;
      const sel = window.getSelection?.();
      if (!sel || !isSelectionInsideDocBody(sel)) return;
      e.preventDefault();
      docStashedSelection = String(sel.toString() ?? "");
      beginDocumentTextComment();
    }
    window.document.addEventListener("keydown", onKey);
    return () => window.document.removeEventListener("keydown", onKey);
  });

  /**
   * Esc clears a pending document text comment from anywhere on the page
   * (the rail's composer also handles Esc when its textarea is focused;
   * this catches the case where focus is on the body or pill).
   */
  $effect(() => {
    if (typeof window === "undefined") return;
    function onKey(e) {
      if (e.key !== "Escape") return;
      if (!pendingDocumentComment) return;
      pendingDocumentComment = null;
    }
    window.document.addEventListener("keydown", onKey);
    return () => window.document.removeEventListener("keydown", onKey);
  });

  /**
   * Apply (or refresh) the active-comment glow styling on body marks. Called
   * both reactively (when hover changes) AND right after highlights are
   * regenerated, since the highlight pass tears down the old `<mark>`
   * elements and creates fresh ones with only their base inline style — if
   * we didn't re-apply glow here the rail hover state would
   * silently desync until the operator nudges their pointer.
   */
  function applyDocCommentGlow() {
    const root = /** @type {HTMLElement | null} */ (docBodyMarkdownRoot);
    if (!root) return;
    const active = $docCommentBodyHover ?? null;
    const focused = $docCommentBodyFocus ?? null;
    const activeSet = new Set([...(active ?? []), ...(focused ?? [])]);
    const marks = root.querySelectorAll(
      "mark.js-doc-comment-mark[data-event-id]",
    );
    for (const m of Array.from(marks)) {
      const el = /** @type {HTMLElement} */ (m);
      if (m.classList.contains("is-pending")) continue;
      if (!m.classList.contains("is-posted")) continue;
      const ids = eventIdsFromDocCommentMark(m);
      const isActive = ids.some((id) => activeSet.has(id));
      if (isActive) {
        el.style.backgroundColor =
          "color-mix(in oklab, var(--accent) 24%, transparent)";
        el.style.outline = "none";
        el.style.borderBottomStyle = "solid";
      } else {
        el.style.outline = "none";
        el.style.backgroundColor =
          "color-mix(in oklab, var(--accent) 8%, transparent)";
        el.style.borderBottomStyle = "dashed";
      }
    }
  }

  /**
   * Posted (dashed underline) + pending (soft fill) highlights in the rendered
   * body. Pending wins when the quote matches an existing posted comment.
   * Re-runs when the displayed revision, anchor list from the rail, or
   * pending composer state changes.
   */
  $effect(() => {
    const root = /** @type {HTMLElement | null} */ (docBodyMarkdownRoot);
    if (!root) return;
    const pending = String(pendingDocumentComment?.selected_text ?? "").trim();
    void displayedContent;
    void documentAnchorContext;
    const posted = documentAnchorContext.posted;
    void tick().then(() => {
      if (!docBodyMarkdownRoot) return;
      applyDocumentCommentHighlights(
        /** @type {HTMLElement} */ (docBodyMarkdownRoot),
        {
          posted,
          pendingQuote: pending,
        },
      );
      // Re-apply glow on the freshly minted marks so hover state survives
      // a highlight regeneration (revision/composer/rail change).
      applyDocCommentGlow();
    });
    return () => {
      if (docBodyMarkdownRoot) {
        clearDocumentCommentMarks(
          /** @type {HTMLElement} */ (docBodyMarkdownRoot),
        );
      }
    };
  });

  /**
   * Bidirectional hover sync: body marks carry `data-event-id` / `data-event-ids`
   * (set in `applyDocumentCommentHighlights`); the rail highlights the matching
   * `MessageItem` rows via `docCommentBodyHover` (a list so stacked anchors
   * highlight every thread on that range).
   */
  $effect(() => {
    const root = /** @type {HTMLElement | null} */ (docBodyMarkdownRoot);
    if (typeof window === "undefined" || !root) {
      return;
    }
    function onClick(/** @type {MouseEvent} */ e) {
      const node = e.target;
      if (!node || !(node instanceof Node)) {
        return;
      }
      const el = node instanceof Element ? node : node.parentElement;
      if (!el) {
        return;
      }
      const m = el.closest("mark.js-doc-comment-mark[data-event-id]");
      if (!m) {
        docCommentBodyFocus.set(null);
        return;
      }
      const ids = eventIdsFromDocCommentMark(m);
      if (ids.length === 0) {
        return;
      }
      e.preventDefault();
      docCommentBodyFocus.set(ids);
      docRailSidePanel = "discussion";
      discussionOpenSignal += 1;
      void tick().then(() => {
        if (typeof window === "undefined") return;
        window.requestAnimationFrame(() => {
          const firstId = ids[0];
          const target = window.document.getElementById(`message-${firstId}`);
          target?.scrollIntoView({ block: "center", behavior: "smooth" });
        });
      });
    }
    function onPointerOver(/** @type {PointerEvent} */ e) {
      const node = e.target;
      if (!node || !(node instanceof Node)) {
        return;
      }
      const el = node instanceof Element ? node : node.parentElement;
      if (!el) {
        return;
      }
      const m = el.closest("mark.js-doc-comment-mark[data-event-id]");
      if (!m) {
        docCommentBodyHover.set(null);
        return;
      }
      const ids = eventIdsFromDocCommentMark(m);
      docCommentBodyHover.set(ids.length > 0 ? ids : null);
    }
    function onPointerLeave() {
      docCommentBodyHover.set(null);
    }
    root.addEventListener("click", onClick);
    root.addEventListener("pointerover", onPointerOver);
    root.addEventListener("pointerleave", onPointerLeave);
    return () => {
      root.removeEventListener("click", onClick);
      root.removeEventListener("pointerover", onPointerOver);
      root.removeEventListener("pointerleave", onPointerLeave);
      docCommentBodyHover.set(null);
      docCommentBodyFocus.set(null);
    };
  });

  /**
   * Reactive driver for the glow: when hover or the markdown root changes we
   * just re-apply via the shared helper. Highlight regeneration is handled in
   * the highlight `$effect` above so the freshly created marks pick up the
   * current active id immediately.
   */
  $effect(() => {
    void docBodyMarkdownRoot;
    void $docCommentBodyHover;
    void $docCommentBodyFocus;
    applyDocCommentGlow();
  });

  function beginDocumentTextComment() {
    const t = String(docStashedSelection ?? "").trim();
    if (!t || !document?.id || !displayedRevision?.revision_id) {
      return;
    }
    const fields = buildDocumentCommentFields({
      source: displayedContent,
      selectedText: t,
      documentId: document.id,
      revisionId: displayedRevision.revision_id,
      contentHash: String(displayedRevision.content_hash ?? "").trim(),
      isHeadRevision: !isViewingOldRevision,
    });
    pendingDocumentComment = {
      document_id: fields.document_id,
      revision_id: fields.revision_id,
      content_hash: fields.content_hash,
      selected_text: fields.selected_text,
      context_before: fields.context_before,
      context_after: fields.context_after,
      start_offset: fields.start_offset,
      end_offset: fields.end_offset,
      anchor_status: fields.anchor_status,
    };
    docRailSidePanel = "discussion";
    discussionOpenSignal += 1;
    // Stop the floating pill from lingering over the freshly-cleared
    // selection while the operator is now writing in the rail.
    docSelectionPillPos = null;
  }

  function clearDocumentTextComment() {
    pendingDocumentComment = null;
  }
</script>

{#if loading}
  <nav
    class="mb-2 flex min-w-0 items-center gap-1.5 text-micro text-fg-muted"
    aria-label="Breadcrumb"
  >
    <a
      class="shrink-0 transition-colors hover:text-fg"
      href={workspaceHref("/docs")}>Docs</a
    >
    <span class="shrink-0 text-fg-subtle">/</span>
    <span class="min-w-0 truncate text-fg-muted">{documentId}</span>
  </nav>
  <div
    class="mt-8 flex items-center justify-center gap-2 text-meta text-fg-muted"
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
    Loading...
  </div>
{:else if loadError}
  <nav
    class="mb-2 flex min-w-0 items-center gap-1.5 text-micro text-fg-muted"
    aria-label="Breadcrumb"
  >
    <a
      class="shrink-0 transition-colors hover:text-fg"
      href={workspaceHref("/docs")}>Docs</a
    >
    <span class="shrink-0 text-fg-subtle">/</span>
    <span class="min-w-0 truncate text-fg-muted">{documentId}</span>
  </nav>
  <div class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text">
    {loadError}
  </div>
{:else if document}
  {#if document.trashed_at}
    <div
      class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-md border border-danger bg-danger-soft px-3 py-2 text-meta text-danger-text"
    >
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2 font-semibold">
          <span>⚠</span>
          <span>This document is in trash</span>
        </div>
        {#if document.trash_reason}
          <p class="mt-2">Reason: {document.trash_reason}</p>
        {/if}
        <p
          class="mt-1 flex flex-wrap items-center gap-x-1 text-micro text-danger-text"
        >
          <span>Trashed</span>
          {#if document.trashed_by}
            <ActorLabel
              label={actorName(document.trashed_by)}
              seed={document.trashed_by}
              size="xs"
              prefix="by"
              nameClass="text-micro text-danger-text"
            />
          {/if}
          {#if document.trashed_at}
            <span>at {formatTimestamp(document.trashed_at)}</span>
          {/if}
        </p>
      </div>
      <Button
        variant="destructive"
        size="compact"
        disabled={docLifecycleBusy}
        onclick={handleRestoreDocument}
        type="button"
      >
        {docLifecycleBusy ? "…" : "Restore"}
      </Button>
    </div>
  {:else if document.archived_at}
    <div
      class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-md border border-warn bg-warn-soft px-3 py-2 text-meta text-warn-text"
    >
      <p class="flex min-w-0 flex-1 flex-wrap items-center gap-x-1">
        <span class="text-warn-text">
          This document was archived on {formatTimestamp(
            document.archived_at,
          ) || "—"}
        </span>
        {#if document.archived_by}
          <ActorLabel
            label={actorName(document.archived_by)}
            seed={document.archived_by}
            size="xs"
            prefix="by"
            nameClass="text-micro text-warn-text"
          />
        {/if}
        <span class="text-warn-text">.</span>
      </p>
      <Button
        variant="secondary"
        size="compact"
        class="border-warn text-warn-text hover:bg-warn-soft"
        disabled={docLifecycleBusy}
        onclick={handleUnarchiveDocument}
      >
        {docLifecycleBusy ? "…" : "Unarchive"}
      </Button>
    </div>
  {/if}

  {#snippet docRevisionPanelBody()}
    {#snippet revisionRows()}
      {#each revisions as rev, i}
        {@const isHead = rev.revision_id === headRevision?.revision_id}
        {@const isSelected = displayedRevision?.revision_id === rev.revision_id}
        <button
          class="w-full px-3 py-3 text-left transition-colors hover:bg-line-subtle {i >
          0
            ? 'border-t border-line'
            : ''} {isSelected ? 'bg-line-subtle' : ''}"
          onclick={() => selectRevision(rev)}
          type="button"
        >
          <div class="flex items-center gap-2">
            <div class="relative flex flex-col items-center">
              <div
                class="h-2.5 w-2.5 rounded-full {isHead
                  ? 'bg-ok-text'
                  : isSelected
                    ? 'bg-accent-text'
                    : 'bg-fg-subtle'}"
              ></div>
              {#if i < revisions.length - 1}
                <div class="absolute top-3 h-full w-px bg-line"></div>
              {/if}
            </div>
            <div class="min-w-0 flex-1">
              <p class="text-meta font-medium text-fg">
                {#if isHead}Current version{:else}Version {rev.revision_number}{/if}
              </p>
              <p
                class="flex flex-wrap items-center gap-x-1 text-micro text-fg-muted"
              >
                <span>{formatTimestamp(rev.created_at)}</span>
                <span aria-hidden="true">·</span>
                <ActorLabel
                  label={actorName(rev.created_by)}
                  seed={rev.created_by}
                  size="xs"
                  nameClass="text-micro text-fg-muted"
                />
              </p>
              {#if rev.revision_hash}
                <p class="mt-0.5 truncate font-mono text-micro text-fg-muted">
                  {rev.revision_hash.slice(0, 12)}...
                </p>
              {/if}
            </div>
          </div>
        </button>
      {/each}
    {/snippet}
    {#if historyLoading}
      <div class="flex items-center gap-2 px-3 py-4 text-micro text-fg-muted">
        <svg class="h-3.5 w-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
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
        Loading revision history...
      </div>
    {:else if revisions.length === 0}
      <p class="px-3 py-4 text-micro text-fg-muted">
        No earlier revisions found.
      </p>
    {:else if document.thread_id}
      {@render revisionRows()}
    {:else}
      <div
        class="max-h-[calc(100vh-12rem)] overflow-y-auto max-lg:max-h-[min(58dvh,28rem)]"
      >
        {@render revisionRows()}
      </div>
    {/if}
  {/snippet}

  {#snippet docDesktop()}
    <h1 class="min-w-0 text-subtitle font-semibold text-fg">
      {resourceDisplayLabel(document, documentId)}
    </h1>
    {#if String(document.summary ?? "").trim()}
      <p
        class="line-clamp-3 text-[13px] text-fg-muted"
        title={String(document.summary).trim()}
      >
        {String(document.summary).trim()}
      </p>
    {/if}
    <div class="mt-1 flex flex-wrap items-center gap-1.5 text-micro">
      {#if document.state}
        <span
          class="rounded px-1.5 py-0.5 font-medium {document.state === 'active'
            ? 'text-ok-text bg-ok-soft'
            : document.state === 'trashed'
              ? 'text-danger-text bg-danger-soft'
              : 'text-warn-text bg-warn-soft'}"
          >{{
            active: "Active",
            archived: "Archived",
            trashed: "Trashed",
          }[document.state] ?? document.state}</span
        >
      {/if}
      {#if document.state}
        <span class="text-fg-subtle">·</span>
      {/if}
      <span class="text-fg-muted"
        >v{displayedRevision?.revision_number ?? "\u2014"}</span
      >
      <span class="text-fg-subtle">·</span>
      <span class="text-fg-muted"
        >{formatTimestamp(displayedRevision?.created_at) || "—"}</span
      >
      <span class="text-fg-subtle">·</span>
      <span class="text-fg-muted"
        >by {actorName(displayedRevision?.created_by)}</span
      >
    </div>
    {#if documentTopicRefValue}
      <p
        class="mt-0.5 flex flex-wrap items-baseline gap-x-1.5 text-micro text-fg-muted"
      >
        <span>Topic / thread</span>
        <RefLink
          refValue={documentTopicRefValue}
          threadId={document.thread_id}
          humanize
          showRaw
        />
      </p>
    {/if}
  {/snippet}

  <!--
    Compact shell (&lt;1024px / max-lg): `page-dock-layout` pins discussion at the
    bottom like boards / topic. From `lg` up the shell is full-width with a
    fixed right rail (`DiscussionDrawer layout="rail"`); `--mobile-only` releases
    the dock at that breakpoint only.
  -->
  <div
    class="doc-detail-layout flex flex-col gap-0 lg:flex-row lg:items-start lg:gap-0 {document.thread_id
      ? 'doc-detail-layout--with-rail page-dock-layout page-dock-layout--mobile-only page-dock-layout--fixed-mobile-chat'
      : ''}"
  >
    <div
      class="doc-detail-main min-w-0 flex-1 {document.thread_id
        ? 'page-dock-scroll lg:pt-3 lg:pb-10'
        : ''}"
    >
      <div class="doc-detail-content-row flex gap-4">
        <div class="doc-detail-content min-w-0 flex-1">
          <WorkspaceResourceTopRow
            breadcrumbAriaLabel="Breadcrumb and document status"
            desktopAriaLabel="Document details"
            dense
            showDesktop={false}
            desktop={docDesktop}
          >
            {#snippet breadcrumb()}
              <a
                class="shrink-0 transition-colors hover:text-fg"
                href={workspaceHref("/docs")}>Docs</a
              >
              {#if parentTopic}
                <span class="shrink-0 text-fg-subtle">/</span>
                <a
                  class="min-w-0 max-w-[5.5rem] shrink truncate sm:max-w-[12rem] transition-colors hover:text-fg"
                  href={workspaceHref(
                    `/topics/${encodeURIComponent(resourceRouteSegment(parentTopic, "topic"))}`,
                  )}
                  title={parentTopic.title}
                >
                  {parentTopic.title}
                </a>
              {/if}
              <span class="shrink-0 text-fg-subtle">/</span>
              <div class="flex min-h-0 min-w-0 flex-1 items-center gap-1.5">
                <span
                  class="min-w-0 shrink truncate text-fg-muted"
                  aria-current="page"
                  title={resourceDisplayLabel(document, documentId)}
                >
                  {resourceDisplayLabel(document, documentId)}
                </span>
                {#if document.state}
                  <span
                    class="shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium leading-none sm:px-2 sm:py-0.5 sm:text-micro {document.state ===
                    'active'
                      ? 'text-ok-text bg-ok-soft'
                      : document.state === 'trashed'
                        ? 'text-danger-text bg-danger-soft'
                        : 'text-warn-text bg-warn-soft'}"
                    >{{
                      active: "Active",
                      archived: "Archived",
                      trashed: "Trashed",
                    }[document.state] ?? document.state}</span
                  >
                {/if}
              </div>
            {/snippet}
            {#snippet actions()}
              {#if !document.trashed_at}
                <ResourceShareMenu
                  resourceId={resourceCopyValue("document", document)}
                  resourceLabel="document ref"
                  rawRecord={document}
                  contentHash={headRevision?.content_hash
                    ? String(headRevision.content_hash)
                    : ""}
                />
                {#if isTextEditable}
                  <Button
                    variant="primary"
                    size="compact"
                    onclick={openEdit}
                    type="button"
                  >
                    <Icon name="pencil" class="h-3.5 w-3.5" />
                    Edit
                  </Button>
                {:else}
                  <span
                    class="inline-flex max-w-[5rem] items-center gap-1 truncate rounded-md border border-line px-1.5 py-1 text-[10px] text-fg-muted sm:max-w-none sm:px-2.5 sm:py-1.5 sm:text-micro lg:inline-flex"
                    title="Content type '{headContentType}' can only be updated via the CLI or API"
                  >
                    <Icon name="info" class="h-3.5 w-3.5 shrink-0" />
                    {headContentType} — edit via CLI
                  </span>
                {/if}
                <div
                  bind:this={moreActionsRoot}
                  class="relative"
                  use:dismissOnEscape={{
                    enabled: moreActionsOpen,
                    onDismiss: closeMoreActions,
                  }}
                >
                  <button
                    type="button"
                    class="inline-flex h-7 w-7 cursor-pointer items-center justify-center rounded-md border border-line bg-transparent text-fg-muted transition-colors hover:bg-panel-hover hover:text-fg disabled:cursor-not-allowed disabled:opacity-50"
                    aria-label="More actions"
                    aria-haspopup="menu"
                    aria-expanded={moreActionsOpen}
                    disabled={docLifecycleBusy}
                    onclick={toggleMoreActions}
                  >
                    <Icon name="kebab" class="h-4 w-4" />
                  </button>
                  {#if moreActionsOpen}
                    <div
                      class="absolute right-0 z-50 mt-1 min-w-[10rem] rounded-md border border-line bg-panel py-1 shadow-lg"
                      role="menu"
                    >
                      <a
                        role="menuitem"
                        class="block w-full px-3 py-2 text-left text-micro text-fg hover:bg-panel-hover"
                        href={workspaceHref(
                          `/docs/${encodeURIComponent(documentRouteSegment)}/edit`,
                        )}
                        onclick={closeMoreActions}
                      >
                        Settings
                      </a>
                      {#if !document.thread_id}
                        <button
                          type="button"
                          role="menuitem"
                          class="block w-full px-3 py-2 text-left text-micro text-fg hover:bg-panel-hover"
                          onclick={() => {
                            closeMoreActions();
                            void openRevisionHistoryNoThread();
                          }}
                        >
                          Revision history
                        </button>
                      {/if}
                      {#if !document.archived_at}
                        <button
                          type="button"
                          role="menuitem"
                          class="block w-full px-3 py-2 text-left text-micro text-fg hover:bg-panel-hover disabled:cursor-not-allowed disabled:opacity-50"
                          disabled={docLifecycleBusy}
                          onclick={() => {
                            closeMoreActions();
                            confirmModal = { open: true, action: "archive" };
                          }}
                        >
                          Archive
                        </button>
                      {/if}
                      <button
                        type="button"
                        role="menuitem"
                        class="block w-full px-3 py-2 text-left text-micro text-danger-text hover:bg-panel-hover disabled:cursor-not-allowed disabled:opacity-50"
                        disabled={docLifecycleBusy}
                        onclick={() => {
                          closeMoreActions();
                          confirmModal = { open: true, action: "trash" };
                        }}
                      >
                        Move to trash
                      </button>
                    </div>
                  {/if}
                </div>
              {/if}
            {/snippet}
          </WorkspaceResourceTopRow>

          <div class="mt-3 mb-1">
            {#if titleEditing}
              <input
                bind:this={titleInputEl}
                bind:value={titleDraft}
                class="w-full border-none bg-transparent p-0 text-display font-semibold text-fg outline-none placeholder:text-fg-subtle"
                placeholder="Untitled document"
                aria-label="Document title"
                onkeydown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    void commitTitleEdit();
                  }
                }}
                onblur={() => void commitTitleEdit()}
                use:inlineEditEscape={{ onRevert: cancelTitleEdit }}
              />
            {:else if canEditTitle}
              <button
                type="button"
                class="group/title -ml-1 flex max-w-full items-center gap-2 rounded px-1 py-0.5 text-left transition-colors hover:bg-panel-hover"
                onclick={startTitleEdit}
                aria-label="Rename document"
                title="Rename document"
              >
                <h1 class="min-w-0 truncate text-display font-semibold text-fg">
                  {resourceDisplayLabel(document, documentId)}
                </h1>
                {#if titleSaving}
                  <svg
                    class="h-4 w-4 shrink-0 animate-spin text-fg-muted"
                    fill="none"
                    viewBox="0 0 24 24"
                  >
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
                {:else}
                  <Icon
                    name="pencil"
                    class="h-4 w-4 shrink-0 text-fg-subtle opacity-0 transition-opacity group-hover/title:opacity-100"
                  />
                {/if}
              </button>
            {:else}
              <h1 class="min-w-0 text-display font-semibold text-fg">
                {resourceDisplayLabel(document, documentId)}
              </h1>
            {/if}
            {#if titleError}
              <p class="mt-1 text-micro text-danger-text" role="alert">
                {titleError}
              </p>
            {/if}
            {#if !titleEditing && String(document.summary ?? "").trim()}
              <p class="mt-1 text-meta text-fg-muted">
                {String(document.summary).trim()}
              </p>
            {/if}
            <p
              class="mt-1.5 flex flex-wrap items-center gap-x-1.5 text-micro text-fg-subtle"
            >
              <span class="text-fg-muted"
                >v{displayedRevision?.revision_number ?? "—"}</span
              >
              <span aria-hidden="true">·</span>
              <span
                >{formatTimestamp(displayedRevision?.created_at) || "—"}</span
              >
              <span aria-hidden="true">·</span>
              <ActorLabel
                label={actorName(displayedRevision?.created_by)}
                seed={displayedRevision?.created_by}
                size="xs"
                prefix="by"
                nameClass="text-micro text-fg-subtle"
              />
            </p>
          </div>

          {#if editOpen}
            <div class="mt-4">
              <DocumentMarkdownEditor
                bind:value={editDraft.content}
                {saving}
                {saveError}
                dirty={editorDirty}
                baseRevisionId={headRevision?.revision_id ?? ""}
                onsave={() => void handleSave()}
                oncancel={closeEdit}
              />
            </div>
          {/if}

          {#if isViewingOldRevision}
            <div
              class="mt-3 flex items-center gap-2 rounded-md bg-warn-soft px-3 py-2 text-micro text-warn-text"
            >
              <span
                >Viewing revision {selectedRevision.revision_number} from {formatTimestamp(
                  selectedRevision.created_at,
                )}</span
              >
              <button
                class="cursor-pointer ml-auto font-medium underline"
                onclick={returnToHead}
                type="button">Return to current</button
              >
            </div>
          {/if}

          {#if !editOpen}
            <div class="mt-5 flex min-w-0 gap-8">
              <div class="min-w-0 flex-1">
                <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
                <div
                  bind:this={docBodyMarkdownRoot}
                  class="js-doc-markdown-body mx-auto max-w-[46rem]"
                  role="region"
                  aria-label="Document body"
                  onmouseup={refreshStashedDocSelection}
                >
                  {#if displayedContent}
                    <MarkdownRenderer
                      source={displayedContent}
                      class="markdown-rendered--doc text-fg"
                    />
                  {:else}
                    <p class="text-meta text-fg-muted">(No content)</p>
                  {/if}
                </div>
              </div>
              {#if showOutline}
                <aside class="hidden w-56 shrink-0 xl:block">
                  <nav class="sticky top-6" aria-label="Document outline">
                    <p
                      class="mb-2 px-3 text-micro font-semibold uppercase tracking-wide text-fg-subtle"
                    >
                      On this page
                    </p>
                    <ul class="border-l border-line">
                      {#each docOutline as heading (heading.id)}
                        <li>
                          <button
                            type="button"
                            class="block w-full truncate border-l-2 py-1 pr-2 text-left text-micro transition-colors -ml-px {activeOutlineId ===
                            heading.id
                              ? 'border-accent text-fg'
                              : 'border-transparent text-fg-muted hover:text-fg'}"
                            style={`padding-left: ${0.75 + (heading.level - 1) * 0.75}rem;`}
                            onclick={() => scrollToOutline(heading.id)}
                            title={heading.text}
                          >
                            {heading.text}
                          </button>
                        </li>
                      {/each}
                    </ul>
                  </nav>
                </aside>
              {/if}
            </div>
          {/if}

          <div class="mt-6 border-t border-line pt-4">
            <IdsIntegrityDisclosure
              rows={docIntegrityRows}
              rawJson={docRawJson}
              rawJsonCopyLabel="Copy document JSON"
            />
          </div>
        </div>

        {#if historyOpen && !document.thread_id}
          <aside
            class="shrink-0 lg:w-72 max-lg:fixed max-lg:inset-x-3 max-lg:top-[5.75rem] max-lg:z-40 max-lg:w-auto"
          >
            <div
              class="rounded-md border border-line bg-bg-soft shadow-lg lg:sticky lg:top-4"
            >
              <div
                class="flex items-center justify-between border-b border-line px-4 py-2.5 max-lg:px-3"
              >
                <h2 class="text-meta font-medium text-fg">Revision history</h2>
                <button
                  class="cursor-pointer text-fg-muted hover:text-fg"
                  onclick={() => (historyOpen = false)}
                  type="button"
                  aria-label="Close history"
                >
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
                </button>
              </div>

              {@render docRevisionPanelBody()}
            </div>
          </aside>
        {/if}
      </div>
    </div>
    {#if document.thread_id}
      <!--
        On compact widths the feed holds the dock; from `lg` up `lg:contents`
        lets the rail aside participate in the row flow while fixed positioning
        handles layout.
      -->
      <div class="page-dock-feed lg:contents">
        <DocumentDiscussionRail
          doc={document}
          bind:docSidePanel={docRailSidePanel}
          revisionPanel={docRevisionPanelBody}
          prepareRevisionHistory={ensureRevisionHistoryForRail}
          {workspaceSlug}
          workspaceId={data?.workspaceId ?? ""}
          openSignal={discussionOpenSignal}
          {pendingDocumentComment}
          onPendingDocumentPostConsumed={clearDocumentTextComment}
          onClearPendingDocumentPost={clearDocumentTextComment}
          currentDocumentContent={displayedContent}
          onDocumentTextAnchorContextChange={(ctx) => {
            documentAnchorContext = ctx;
          }}
        />
      </div>
    {/if}
  </div>

  {#if !document.trashed_at && !editOpen && document.thread_id && docSelectionPillPos && String(docStashedSelection ?? "").trim()}
    <!--
      Google Docs–style floating "Comment" pill that follows the selection.
      Page-absolute so it stays put through scroll; pointer-events-auto on
      the button itself, none on the container so it never steals clicks
      from surrounding text. The pill dismisses itself the next time
      `selectionchange` fires with no usable selection (handled in the
      effect that keeps `docSelectionPillPos` in sync).
    -->
    <div
      class="pointer-events-none absolute z-30"
      style={`top: ${docSelectionPillPos.top}px; left: ${docSelectionPillPos.left}px;`}
    >
      <button
        type="button"
        class="pointer-events-auto inline-flex h-8 items-center gap-1.5 rounded-full border border-line bg-panel pl-2 pr-2.5 text-micro font-medium text-fg shadow-md transition-colors hover:bg-bg-soft"
        onmousedown={(e) => {
          // Prevent the click from collapsing the user's selection before
          // we capture it. mousedown fires before mouseup → selectionchange.
          e.preventDefault();
        }}
        onclick={beginDocumentTextComment}
        title="Comment on selection (⌘⌥M)"
      >
        <svg
          class="h-3.5 w-3.5 text-accent"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          viewBox="0 0 24 24"
          aria-hidden="true"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M8 12h8M8 8h8m-8 8h5m-9 3.5V6a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H9l-5 3.5Z"
          />
        </svg>
        Comment
      </button>
    </div>
  {/if}
{:else}
  <div class="mt-8 text-center text-meta text-fg-muted">
    Document not found.
  </div>
{/if}

<ConfirmModal
  open={confirmModal.open}
  title={confirmModal.action === "trash" ? "Move to trash" : "Archive document"}
  message={confirmModal.action === "trash"
    ? "This document will be moved to trash. You can restore it later."
    : "This document will be hidden from default views. You can unarchive it later."}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={docLifecycleBusy}
  onconfirm={handleConfirm}
  oncancel={() => (confirmModal = { open: false, action: "" })}
/>
