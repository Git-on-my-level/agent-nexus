<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import Button from "$lib/components/Button.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import IdsIntegrityDisclosure from "$lib/components/IdsIntegrityDisclosure.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import ResourceShareMenu from "$lib/components/ResourceShareMenu.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { splitTypedRef } from "$lib/inboxUtils";
  import { workspacePath } from "$lib/workspacePaths";
  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import DocumentDiscussionRail from "$lib/components/document-detail/DocumentDiscussionRail.svelte";
  import { buildDocumentCommentFields } from "$lib/documentCommentAnchor.js";
  import {
    applyDocumentCommentHighlights,
    clearDocumentCommentMarks,
  } from "$lib/documentCommentHighlight.js";
  import { docCommentBodyHover } from "$lib/stores/docCommentBodyRailSync.js";
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
    labels: "",
  });
  let saving = $state(false);
  let saveError = $state("");
  let loadingSelectedRevisionKey = $state("");
  let metadataExpanded = $state(false);
  let confirmModal = $state({ open: false, action: "" });
  let docLifecycleBusy = $state(false);
  /**
   * Per polish §P8: Archive and Trash collapse into a single "More actions"
   * kebab so "New revision" is the only competing primary in the doc header.
   */
  let moreActionsOpen = $state(false);
  let moreActionsRoot = $state(null);
  /** Selection stash + discussion rail for document text comments */
  let docBodyMarkdownRoot = $state(null);
  /** Gutter column aligned with the body (for anchored-comment dots) */
  let docCommentGutterRoot = $state(/** @type {HTMLElement | null} */ (null));
  let gutterDocCommentDots = $state(
    /** @type {Array<{ eventId: string, topPx: number }>} */ ([]),
  );
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
    function onDocKey(e) {
      if (e.key === "Escape") moreActionsOpen = false;
    }
    window.document.addEventListener("pointerdown", onDocPointerDown, true);
    window.document.addEventListener("keydown", onDocKey, true);
    return () => {
      window.document.removeEventListener(
        "pointerdown",
        onDocPointerDown,
        true,
      );
      window.document.removeEventListener("keydown", onDocKey, true);
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
        label: "Document ID",
        value: String(d.id),
        copyLabel: "Copy document ID",
      });
    }
    if (rev?.revision_id) {
      rows.push({
        label: "Revision ID",
        value: String(rev.revision_id),
        copyLabel: "Copy revision ID",
      });
    }
    const threadId = String(d.thread_id ?? "").trim();
    if (threadId) {
      rows.push({
        label: "Thread ID",
        value: threadId,
        copyLabel: "Copy thread ID",
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

  function workspaceHref(pathname = "/") {
    return workspacePath(organizationSlug, workspaceSlug, pathname);
  }

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

  async function loadDocument(targetId) {
    if (!targetId) return;
    loading = true;
    loadError = "";
    loadedDocumentId = targetId;
    revisions = [];
    selectedRevision = null;
    historyLoading = false;
    historyOpen = false;
    editOpen = false;
    try {
      const result = await coreClient.getDocument(targetId);
      document = result.document ?? null;
      headRevision = result.revision ?? null;
      if (!document) {
        loadError = "Document not found.";
      }
    } catch (e) {
      loadError = `Failed to load document: ${e instanceof Error ? e.message : String(e)}`;
      document = null;
      headRevision = null;
    } finally {
      loading = false;
    }
  }

  async function loadHistory() {
    if (!documentId || revisions.length > 0) {
      historyOpen = !historyOpen;
      return;
    }
    historyOpen = true;
    historyLoading = true;
    try {
      const result = await coreClient.getDocumentHistory(documentId);
      revisions = (result.revisions ?? []).slice().reverse();
    } catch {
      revisions = [];
    } finally {
      historyLoading = false;
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
      labels: (document?.labels ?? []).join(", "),
    };
    saveError = "";
    editOpen = true;
    historyOpen = false;
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
      const labels = editDraft.labels
        .split(",")
        .map((l) => l.trim())
        .filter(Boolean);

      const docPatch = {};
      if (
        editDraft.title.trim() &&
        editDraft.title.trim() !== document?.title
      ) {
        docPatch.title = editDraft.title.trim();
      }
      const labelsChanged =
        JSON.stringify(labels) !== JSON.stringify(document?.labels ?? []);
      if (labelsChanged) {
        docPatch.labels = labels;
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
    const rects = range.getClientRects();
    if (!rects || rects.length === 0) {
      return null;
    }
    const forward = isSelectionForward(sel);
    const anchorRect = forward ? rects[rects.length - 1] : rects[0];
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
   * we didn't re-apply glow here the rail-card/gutter hover state would
   * silently desync until the operator nudges their pointer.
   */
  function applyDocCommentGlow() {
    const root = /** @type {HTMLElement | null} */ (docBodyMarkdownRoot);
    if (!root) return;
    const activeId = $docCommentBodyHover ?? "";
    const marks = root.querySelectorAll(
      "mark.js-doc-comment-mark[data-event-id]",
    );
    for (const m of Array.from(marks)) {
      const el = /** @type {HTMLElement} */ (m);
      const id = String(m.getAttribute("data-event-id") ?? "").trim();
      if (m.classList.contains("is-pending")) continue;
      if (!m.classList.contains("is-posted")) continue;
      if (activeId && id === activeId) {
        el.style.backgroundColor =
          "color-mix(in oklab, var(--accent) 24%, transparent)";
        el.style.borderBottomStyle = "solid";
      } else {
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
      requestAnimationFrame(() => {
        recalcGutterDocCommentDots();
      });
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
   * Bidirectional hover sync: body marks carry `data-event-id` (set in
   * `applyDocumentCommentHighlights`); the rail highlights the matching
   * `MessageItem` via `docCommentBodyHover`.
   */
  $effect(() => {
    const root = /** @type {HTMLElement | null} */ (docBodyMarkdownRoot);
    if (typeof window === "undefined" || !root) {
      return;
    }
    function onPointerOver(/** @type {PointerEvent} */ e) {
      if (!(e.target instanceof Element)) {
        return;
      }
      const m = e.target.closest("mark.js-doc-comment-mark[data-event-id]");
      docCommentBodyHover.set(m?.getAttribute("data-event-id")?.trim() || null);
    }
    function onPointerLeave() {
      docCommentBodyHover.set(null);
    }
    root.addEventListener("pointerover", onPointerOver);
    root.addEventListener("pointerleave", onPointerLeave);
    return () => {
      root.removeEventListener("pointerover", onPointerOver);
      root.removeEventListener("pointerleave", onPointerLeave);
      docCommentBodyHover.set(null);
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
    discussionOpenSignal += 1;
    // Stop the floating pill from lingering over the freshly-cleared
    // selection while the operator is now writing in the rail.
    docSelectionPillPos = null;
  }

  function clearDocumentTextComment() {
    pendingDocumentComment = null;
  }

  function recalcGutterDocCommentDots() {
    if (typeof window === "undefined") {
      return;
    }
    const gut = /** @type {HTMLElement | null} */ (docCommentGutterRoot);
    const body = /** @type {HTMLElement | null} */ (docBodyMarkdownRoot);
    if (!gut || !body) {
      gutterDocCommentDots = [];
      return;
    }
    const posted = documentAnchorContext.posted;
    if (posted.length === 0) {
      gutterDocCommentDots = [];
      return;
    }
    const gRect = gut.getBoundingClientRect();
    // We now wrap each text-node fragment of a quote in its own <mark> for
    // reliable multi-line highlighting (see documentCommentHighlight.js).
    // That means a single comment can produce N marks, and we'd otherwise
    // render a stack of N gutter pips for it. Dedupe by event id, anchoring
    // each pip to the *top-most* mark fragment for that comment.
    /** @type {Map<string, number>} */
    const minTopByEvent = new Map();
    const marks = body.querySelectorAll(
      "mark.js-doc-comment-mark[data-event-id]",
    );
    for (const mark of Array.from(marks)) {
      const id = String(mark.getAttribute("data-event-id") ?? "").trim();
      if (!id) {
        continue;
      }
      const mRect = mark.getBoundingClientRect();
      const topPx = mRect.top - gRect.top + mRect.height / 2;
      if (!Number.isFinite(topPx)) continue;
      const prev = minTopByEvent.get(id);
      if (prev === undefined || topPx < prev) {
        minTopByEvent.set(id, topPx);
      }
    }
    /** @type {Array<{ eventId: string, topPx: number }>} */
    const out = [];
    for (const [id, topPx] of minTopByEvent.entries()) {
      out.push({ eventId: id, topPx });
    }
    out.sort((a, b) => a.topPx - b.topPx);
    gutterDocCommentDots = out;
  }

  function fromGutterFocusAnchor(/** @type {string} */ eventId) {
    if (!eventId) {
      return;
    }
    const wdoc = window.document;
    if (!wdoc) {
      return;
    }
    const msg = wdoc.getElementById(`message-${eventId}`);
    if (msg) {
      msg.scrollIntoView({ behavior: "smooth", block: "nearest" });
    }
    docCommentBodyHover.set(eventId);
    const body = /** @type {HTMLElement | null} */ (docBodyMarkdownRoot);
    if (body) {
      for (const m of body.querySelectorAll(
        "mark.js-doc-comment-mark[data-event-id]",
      )) {
        if (m.getAttribute("data-event-id") === eventId) {
          m.scrollIntoView({ behavior: "smooth", block: "center" });
          const el = /** @type {HTMLElement} */ (m);
          const before = el.style.outline;
          const beforeW = el.style.outlineWidth;
          el.style.outline = "2px solid var(--accent)";
          el.style.outlineOffset = "1px";
          window.setTimeout(() => {
            el.style.outline = before;
            el.style.outlineWidth = beforeW;
          }, 800);
          break;
        }
      }
    }
  }

  $effect(() => {
    if (typeof window === "undefined") {
      return;
    }
    const onMove = () => {
      recalcGutterDocCommentDots();
    };
    window.addEventListener("scroll", onMove, true);
    window.addEventListener("resize", onMove);
    return () => {
      window.removeEventListener("scroll", onMove, true);
      window.removeEventListener("resize", onMove);
    };
  });
</script>

<nav
  class="mb-3 flex items-center gap-1.5 text-micro text-[var(--fg-muted)]"
  aria-label="Breadcrumb"
>
  <a
    class="transition-colors hover:text-[var(--fg)]"
    href={workspaceHref("/docs")}>Docs</a
  >
  {#if parentTopic}
    <span class="text-[var(--fg-subtle)]">/</span>
    <a
      class="max-w-[16rem] truncate transition-colors hover:text-[var(--fg)]"
      href={workspaceHref(`/topics/${encodeURIComponent(parentTopic.id)}`)}
      title={parentTopic.title}
    >
      {parentTopic.title}
    </a>
  {/if}
  <span class="text-[var(--fg-subtle)]">/</span>
  <span
    class="truncate text-[var(--fg-muted)]"
    aria-current="page"
    title={document?.title || documentId}
  >
    {document?.title || documentId}
  </span>
</nav>

{#if loading}
  <div
    class="mt-8 flex items-center justify-center gap-2 text-meta text-[var(--fg-muted)]"
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
  <div class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text">
    {loadError}
  </div>
{:else if document}
  {#if document.trashed_at}
    <div
      class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-danger/30 bg-danger-soft px-3 py-2 text-meta text-danger-text"
    >
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2 font-semibold">
          <span>⚠</span>
          <span>This document is in trash</span>
        </div>
        {#if document.trash_reason}
          <p class="mt-2">Reason: {document.trash_reason}</p>
        {/if}
        <p class="mt-1 text-micro text-danger-text/80">
          Trashed {#if document.trashed_by}by {actorName(
              document.trashed_by,
            )}{/if}
          {#if document.trashed_at}
            at {formatTimestamp(document.trashed_at)}
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
      class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-warn/30 bg-warn-soft px-3 py-2 text-meta text-warn-text"
    >
      <p class="min-w-0 flex-1">
        This document was archived on {formatTimestamp(document.archived_at) ||
          "—"}{#if document.archived_by}
          by {actorName(document.archived_by)}{/if}.
      </p>
      <button
        class="shrink-0 cursor-pointer rounded-md border border-warn/40 bg-warn-soft px-2 py-1 text-micro font-medium text-warn-text hover:bg-warn/25 disabled:opacity-50"
        disabled={docLifecycleBusy}
        onclick={handleUnarchiveDocument}
        type="button"
      >
        {docLifecycleBusy ? "…" : "Unarchive"}
      </button>
    </div>
  {/if}

  <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:gap-4">
    <div class="min-w-0 flex-1">
      <div class="flex gap-4">
        <div class="min-w-0 flex-1">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0 flex-1">
              <h1 class="text-subtitle font-semibold text-[var(--fg)]">
                {document.title || ""}{#if !document.title}<span
                    class="font-mono text-[var(--fg-muted)]">{document.id}</span
                  >{/if}
              </h1>
              <div class="mt-1 flex flex-wrap items-center gap-1.5 text-micro">
                {#if document.state}
                  <span
                    class="rounded px-1.5 py-0.5 font-medium {document.state ===
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
                {#each document.labels ?? [] as label}
                  <span
                    class="rounded bg-[var(--line)] px-1.5 py-0.5 text-micro text-[var(--fg-muted)]"
                    >{label}</span
                  >
                {/each}
                <span class="text-[var(--fg-subtle)]">·</span>
                <span class="text-[var(--fg-muted)]"
                  >v{displayedRevision?.revision_number ?? "\u2014"}</span
                >
                <span class="text-[var(--fg-subtle)]">·</span>
                <span class="text-[var(--fg-muted)]"
                  >{formatTimestamp(displayedRevision?.created_at) || "—"}</span
                >
                <span class="text-[var(--fg-subtle)]">·</span>
                <span class="text-[var(--fg-muted)]"
                  >by {actorName(displayedRevision?.created_by)}</span
                >
              </div>
              {#if documentTopicRefValue}
                <p
                  class="mt-0.5 flex flex-wrap items-baseline gap-x-1.5 text-micro text-[var(--fg-muted)]"
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
            </div>
            {#if !document.trashed_at}
              <div class="flex shrink-0 items-center gap-1.5">
                <ResourceShareMenu
                  resourceId={document.id}
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
                    <svg
                      class="h-3.5 w-3.5"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      stroke-width="2"
                    >
                      <path
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"
                      />
                    </svg>
                    New revision
                  </Button>
                {:else}
                  <span
                    class="inline-flex items-center gap-1 rounded-md border border-[var(--line)] px-2.5 py-1.5 text-micro text-[var(--fg-muted)]"
                    title="Content type '{headContentType}' can only be updated via the CLI or API"
                  >
                    <svg
                      class="h-3.5 w-3.5"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      stroke-width="2"
                    >
                      <path
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                      />
                    </svg>
                    {headContentType} — edit via CLI
                  </span>
                {/if}
                <Button
                  variant="secondary"
                  size="compact"
                  onclick={loadHistory}
                  type="button"
                >
                  <svg
                    class="h-3.5 w-3.5"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                    stroke-width="2"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
                    />
                  </svg>
                  Revision history
                </Button>
                <div bind:this={moreActionsRoot} class="relative">
                  <button
                    type="button"
                    class="inline-flex h-7 w-7 cursor-pointer items-center justify-center rounded-md border border-line bg-transparent text-fg-muted transition-colors hover:bg-panel-hover hover:text-fg disabled:cursor-not-allowed disabled:opacity-50"
                    aria-label="More actions"
                    aria-haspopup="menu"
                    aria-expanded={moreActionsOpen}
                    disabled={docLifecycleBusy}
                    onclick={toggleMoreActions}
                  >
                    <svg
                      class="h-4 w-4"
                      fill="currentColor"
                      viewBox="0 0 24 24"
                      aria-hidden="true"
                    >
                      <circle cx="12" cy="5" r="1.75" />
                      <circle cx="12" cy="12" r="1.75" />
                      <circle cx="12" cy="19" r="1.75" />
                    </svg>
                  </button>
                  {#if moreActionsOpen}
                    <div
                      class="absolute right-0 z-50 mt-1 min-w-[10rem] rounded-md border border-[var(--line)] bg-[var(--panel)] py-1 shadow-lg"
                      role="menu"
                    >
                      {#if !document.archived_at}
                        <button
                          type="button"
                          role="menuitem"
                          class="block w-full px-3 py-2 text-left text-micro text-[var(--fg)] hover:bg-[var(--line-subtle)] disabled:opacity-50"
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
                        class="block w-full px-3 py-2 text-left text-micro text-danger-text hover:bg-[var(--line-subtle)] disabled:opacity-50"
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
              </div>
            {/if}
          </div>

          {#if editOpen}
            <form
              class="mt-3 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] p-4"
              onsubmit={(e) => {
                e.preventDefault();
                void handleSave();
              }}
            >
              {#if documentAnchorContext.activeAnchoredCount > 0}
                <p
                  class="mb-3 rounded-md border border-warn/25 bg-warn-soft px-3 py-2 text-micro text-warn-text"
                  role="status"
                >
                  {documentAnchorContext.activeAnchoredCount}
                  {documentAnchorContext.activeAnchoredCount === 1
                    ? " comment is"
                    : " comments are"}
                  anchored to text in this revision. Removing quoted text will mark
                  {documentAnchorContext.activeAnchoredCount === 1
                    ? "it"
                    : "them"} as “Text removed.”
                </p>
              {/if}
              <div class="mb-3">
                <button
                  class="cursor-pointer flex w-full items-center gap-2 text-left"
                  onclick={() => (metadataExpanded = !metadataExpanded)}
                  type="button"
                >
                  <svg
                    class="h-3 w-3 text-[var(--fg-muted)] transition-transform {metadataExpanded
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
                  <span class="text-micro font-medium text-[var(--fg-muted)]"
                    >Metadata</span
                  >
                </button>
                {#if !metadataExpanded}
                  <p
                    class="mt-1 ml-5 truncate text-micro text-[var(--fg-muted)]"
                  >
                    Title: {editDraft.title || "—"} · Labels: {editDraft.labels ||
                      "none"}
                  </p>
                {/if}
                {#if metadataExpanded}
                  <div class="mt-2 ml-5 grid gap-3 sm:grid-cols-2">
                    <label class="sm:col-span-2">
                      <span
                        class="text-micro font-medium text-[var(--fg-muted)]"
                        >Title</span
                      >
                      <input
                        bind:value={editDraft.title}
                        class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg)] px-3 py-1.5 text-meta text-[var(--fg)]"
                        type="text"
                      />
                    </label>
                    <label>
                      <span
                        class="text-micro font-medium text-[var(--fg-muted)]"
                        >Labels (comma-separated)</span
                      >
                      <input
                        bind:value={editDraft.labels}
                        class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg)] px-3 py-1.5 text-meta text-[var(--fg)] placeholder:text-[var(--fg-subtle)]"
                        placeholder="ops, runbook"
                        type="text"
                      />
                    </label>
                  </div>
                {/if}
              </div>

              <label>
                <span class="text-micro font-medium text-[var(--fg-muted)]"
                  >Content (Markdown) <span class="text-danger-text">*</span
                  ></span
                >
                <textarea
                  bind:value={editDraft.content}
                  class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg)] px-3 py-2 text-meta text-[var(--fg)] font-mono leading-relaxed resize-y"
                  rows="20"
                ></textarea>
              </label>

              <div class="mt-3 flex items-center gap-2">
                <Button
                  type="submit"
                  variant="primary"
                  size="compact"
                  disabled={saving}
                >
                  {saving ? "Saving…" : "Save revision"}
                </Button>
                <Button
                  variant="secondary"
                  size="compact"
                  onclick={closeEdit}
                  type="button"
                >
                  Cancel
                </Button>
              </div>

              {#if saveError}
                <div
                  class="mt-3 rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
                  role="alert"
                >
                  {saveError}
                </div>
              {/if}
              <p class="mt-2 text-micro text-[var(--fg-muted)]">
                Base revision: <span class="font-mono"
                  >{headRevision?.revision_id ?? "—"}</span
                > — optimistic concurrency is enforced.
              </p>
            </form>
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

          <div class="mt-3 flex min-w-0 items-stretch gap-0">
            {#if document.thread_id}
              <!--
                Comment gutter: a thin vertical column to the left of the doc
                body that hosts a small chat-bubble icon for each anchored
                comment. The dotted underline in the body shows *that* a
                comment exists; the gutter icon shows *how many* comments
                this doc has, gives a clickable jump target, and orients
                operators on long pages without scanning the body for
                underlines. Hover/focus also light up the matching rail
                card and body mark via `docCommentBodyHover`.
              -->
              <div
                bind:this={docCommentGutterRoot}
                class="relative hidden shrink-0 overflow-visible lg:block lg:w-5"
              >
                {#each gutterDocCommentDots as dot (dot.eventId)}
                  <button
                    type="button"
                    class="group absolute left-0 -translate-y-1/2 cursor-pointer rounded-full p-0.5 text-[var(--accent)] transition-colors hover:bg-[var(--bg-soft)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-[var(--accent)]"
                    style={`top: ${dot.topPx}px;`}
                    title="Jump to comment"
                    aria-label="Jump to anchored comment"
                    onclick={() => fromGutterFocusAnchor(dot.eventId)}
                    onmouseenter={() => docCommentBodyHover.set(dot.eventId)}
                    onmouseleave={() => docCommentBodyHover.set(null)}
                    onfocus={() => docCommentBodyHover.set(dot.eventId)}
                    onblur={() => docCommentBodyHover.set(null)}
                  >
                    <svg
                      class="h-3.5 w-3.5 opacity-70 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100"
                      fill="currentColor"
                      viewBox="0 0 24 24"
                      aria-hidden="true"
                    >
                      <path
                        d="M4 5a3 3 0 0 1 3-3h10a3 3 0 0 1 3 3v8a3 3 0 0 1-3 3h-4l-4.4 3.3A1 1 0 0 1 7 18.5V16H7a3 3 0 0 1-3-3V5Z"
                      />
                    </svg>
                  </button>
                {/each}
              </div>
            {/if}
            <div
              class="min-w-0 flex-1 rounded-md border border-[var(--line)] bg-[var(--bg-soft)]"
            >
              <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
              <div
                bind:this={docBodyMarkdownRoot}
                class="js-doc-markdown-body px-4 py-3"
                role="region"
                aria-label="Document body"
                onmouseup={refreshStashedDocSelection}
              >
                {#if displayedContent}
                  <MarkdownRenderer
                    source={displayedContent}
                    class="text-meta leading-relaxed text-[var(--fg)]"
                  />
                {:else}
                  <p class="text-meta text-[var(--fg-muted)]">(No content)</p>
                {/if}
              </div>
            </div>
          </div>

          <div class="mt-6 border-t border-[var(--line)] pt-4">
            <IdsIntegrityDisclosure
              rows={docIntegrityRows}
              rawJson={docRawJson}
              rawJsonCopyLabel="Copy document JSON"
            />
          </div>
        </div>

        {#if historyOpen}
          <aside class="w-72 shrink-0">
            <div
              class="sticky top-4 rounded-md border border-[var(--line)] bg-[var(--bg-soft)]"
            >
              <div
                class="flex items-center justify-between border-b border-[var(--line)] px-4 py-2.5"
              >
                <h2 class="text-meta font-medium text-[var(--fg)]">
                  Revision history
                </h2>
                <button
                  class="cursor-pointer text-[var(--fg-muted)] hover:text-[var(--fg)]"
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

              {#if historyLoading}
                <div
                  class="flex items-center gap-2 px-4 py-4 text-micro text-[var(--fg-muted)]"
                >
                  <svg
                    class="h-3.5 w-3.5 animate-spin"
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
                  Loading revision history...
                </div>
              {:else if revisions.length === 0}
                <p class="px-4 py-4 text-micro text-[var(--fg-muted)]">
                  No earlier revisions found.
                </p>
              {:else}
                <div class="max-h-[calc(100vh-12rem)] overflow-y-auto">
                  {#each revisions as rev, i}
                    {@const isHead =
                      rev.revision_id === headRevision?.revision_id}
                    {@const isSelected =
                      displayedRevision?.revision_id === rev.revision_id}
                    <button
                      class="w-full text-left px-4 py-3 transition-colors hover:bg-[var(--line-subtle)] {i >
                      0
                        ? 'border-t border-[var(--line)]'
                        : ''} {isSelected ? 'bg-[var(--line-subtle)]' : ''}"
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
                                : 'bg-[var(--fg-subtle)]'}"
                          ></div>
                          {#if i < revisions.length - 1}
                            <div
                              class="absolute top-3 h-full w-px bg-[var(--line)]"
                            ></div>
                          {/if}
                        </div>
                        <div class="min-w-0 flex-1">
                          <p class="text-micro font-medium text-[var(--fg)]">
                            {#if isHead}Current version{:else}Version {rev.revision_number}{/if}
                          </p>
                          <p class="text-micro text-[var(--fg-muted)]">
                            {formatTimestamp(rev.created_at)} · {actorName(
                              rev.created_by,
                            )}
                          </p>
                          {#if rev.revision_hash}
                            <p
                              class="mt-0.5 font-mono text-micro text-[var(--fg-muted)]"
                            >
                              {rev.revision_hash.slice(0, 12)}...
                            </p>
                          {/if}
                        </div>
                      </div>
                    </button>
                  {/each}
                </div>
              {/if}
            </div>
          </aside>
        {/if}
      </div>
    </div>
    {#if document.thread_id}
      <DocumentDiscussionRail
        doc={document}
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
        class="pointer-events-auto inline-flex h-8 items-center gap-1.5 rounded-full border border-[var(--line)] bg-[var(--panel)] pl-2 pr-2.5 text-micro font-medium text-[var(--fg)] shadow-md transition-colors hover:bg-[var(--bg-soft)]"
        onmousedown={(e) => {
          // Prevent the click from collapsing the user's selection before
          // we capture it. mousedown fires before mouseup → selectionchange.
          e.preventDefault();
        }}
        onclick={beginDocumentTextComment}
        title="Comment on selection (⌘⌥M)"
      >
        <svg
          class="h-3.5 w-3.5 text-[var(--accent)]"
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
  <div class="mt-8 text-center text-meta text-[var(--fg-muted)]">
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
