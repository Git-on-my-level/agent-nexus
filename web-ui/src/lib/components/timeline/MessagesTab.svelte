<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";
  import { onMount, tick, untrack } from "svelte";
  import { get } from "svelte/store";

  import {
    actorRegistry,
    lookupActorDisplayName,
    principalRegistry,
  } from "$lib/actorSession";
  import { authenticatedAgent } from "$lib/authSession";
  import { listAllPrincipals } from "$lib/authPrincipals";
  import { coreClient } from "$lib/coreClient";
  import {
    messageTargetFromHash,
    scrollAndHighlightTarget,
  } from "$lib/deepLinkTargets";
  import {
    enrichPrincipalsWithWakeRouting,
    taggableWakeHandleForActorId,
  } from "$lib/principalWakeRouting.js";
  import AttachmentChip from "$lib/components/AttachmentChip.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import { emptyMessageEventConfirmModal } from "$lib/confirmModal.js";
  import MessageItem from "$lib/components/timeline/MessageItem.svelte";
  import { eventRefsInclude, toFlatMessageView } from "$lib/messageThreadUtils";
  import { buildPrimitiveRefRoutes, resolveRefLink } from "$lib/refLinkModel";
  import {
    filterMentionCandidates,
    parseActiveMention,
    taggableAgentHandlesFromPrincipals,
  } from "$lib/threadMentionUtils.js";
  import { getTimelineContext } from "$lib/timelineContext";
  import { workspacePath } from "$lib/workspacePaths";

  let {
    threadId,
    /** Topic id or URL scope for refresh APIs; defaults to threadId */
    postRouteScopeId = "",
    onMessagePost,
    workspaceId = "",
    /** When set (e.g. document:abc), only timeline events whose refs include this exact string are shown. */
    subjectRefFilter = "",
    /** Appended to posted message refs (deduped); e.g. `document:<id>` for doc-scoped discussion. */
    extraPostRefs = [],
    /** Replaces default zero-state copy when there are no messages in view. */
    discussionEmptyMessage = "",
    /**
     * When set, the next post is a `document_text_comment` (anchor metadata + `payload.text` instruction).
     * Cleared by the parent via `onPendingDocumentPostConsumed` after a successful post, or `onClearPendingDocumentPost` when the user dismisses.
     */
    pendingDocumentComment = null,
    onPendingDocumentPostConsumed = undefined,
    onClearPendingDocumentPost = undefined,
    /** Submit button label override (e.g. "Post comment"). */
    postButtonLabel = "",
    /**
     * Optional snapshot of the document content currently displayed by the
     * surrounding doc page. When set, anchored comments whose
     * `selected_text` no longer occurs in this content are surfaced as
     * `liveAnchorStatus = "stale"` so `MessageItem` can show a soft
     * "Text removed" chip without mutating the underlying event payload.
     * Empty string disables stale derivation.
     */
    currentDocumentContent = "",
    /**
     * Visual variant for the archive lifecycle button on each message
     * (e.g. "resolve" on the document discussion rail). See ArchiveButton.
     */
    archiveLabelKind = "archive",
    /**
     * Fired when the set of `document_text_comment` events (for `subjectRefFilter`)
     * changes, so the parent doc page can mirror quotes in the body. Includes a
     * count of non-archived anchors for the new-revision editor safeguard.
     */
    onDocumentTextAnchorContextChange = undefined,
    /**
     * On narrow viewports, pin the composer to the bottom: message list
     * scrolls above while the entry form stays in thumb reach (topic board,
     * document, and bottom drawers on mobile).
     */
    pinComposerNarrow = false,
    /**
     * Like `pinComposerNarrow` but active at all viewport widths. Used by the
     * desktop discussion rail and topic primary layout where the parent gives
     * us a constrained-height flex column. Layered on top of (and supersedes
     * for layout purposes) `pinComposerNarrow`; mobile-only safe-area padding
     * is still keyed off the media query.
     */
    pinComposer = false,
    /**
     * When pinned narrow: grow the thread region and push bubbles toward the
     * composer on short threads (collapsible drawers). Non-collapsible surfaces
     * (topic Messages) keep this false so messages stay under the tabs instead
     * of a blank band above the list.
     */
    pinComposerAlignThreadEnd = true,
    /**
     * Pin-narrow: space between list and composer (topic; drawers stay flush).
     */
    pinComposerComfortGap = false,
  } = $props();

  let pinActive = $derived(pinComposer || pinComposerNarrow);

  let subjectRefFilterNorm = $derived(String(subjectRefFilter ?? "").trim());

  /** In document-scoped discussion, hide `document:` / `document_revision:` chips (we're already on that doc). */
  let suppressDisplayDocumentId = $derived(
    subjectRefFilterNorm.startsWith("document:")
      ? subjectRefFilterNorm.slice("document:".length).trim()
      : "",
  );

  let routeScopeForPost = $derived(
    String(postRouteScopeId || threadId || "").trim(),
  );

  const timelineCtx = getTimelineContext();
  const timelineStore = timelineCtx.store;
  const timelineWorkspaceSlug = timelineCtx.workspaceSlug;
  let timeline = $derived($timelineStore.timeline);
  let timelineArtifacts = $derived($timelineStore.timelineArtifacts ?? []);
  let timelineCards = $derived($timelineStore.timelineCards ?? []);
  let timelineDocuments = $derived($timelineStore.timelineDocuments ?? []);
  let timelineNotificationReceipts = $derived(
    $timelineStore.timelineNotificationReceipts ?? {},
  );
  let timelineLoading = $derived($timelineStore.timelineLoading);
  let timelineError = $derived($timelineStore.timelineError);
  let workspaceSlug = $derived($timelineWorkspaceSlug);
  let organizationSlug = $derived($page.params.organization);

  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );

  let showArchived = $state(false);
  let confirmModal = $state(emptyMessageEventConfirmModal());
  let lifecycleBusy = $state(false);
  let lifecycleError = $state("");

  let refScopedTimeline = $derived(
    subjectRefFilterNorm
      ? (Array.isArray(timeline) ? timeline : []).filter((event) =>
          eventRefsInclude(event, subjectRefFilterNorm),
        )
      : Array.isArray(timeline)
        ? timeline
        : [],
  );

  let filteredTimeline = $derived(
    refScopedTimeline.filter((event) => {
      if (event.trashed_at) return false;
      if (!showArchived && event.archived_at) return false;
      return true;
    }),
  );
  let routeMaps = $derived(
    buildPrimitiveRefRoutes({
      artifacts: timelineArtifacts,
      events: refScopedTimeline,
      cards: timelineCards,
      documents: timelineDocuments,
      threadId,
    }),
  );

  let artifactRoutesWithComposerPending = $derived.by(() => {
    const base = routeMaps.artifactRoutesById ?? {};
    const artifacts = Object.values(pendingComposerArtifactsByRef).filter(
      (row) => row && typeof row === "object",
    );
    if (!artifacts.length) return base;
    const extra = buildPrimitiveRefRoutes({
      artifacts,
      cards: timelineCards,
      documents: timelineDocuments,
      threadId,
    }).artifactRoutesById;
    return { ...base, ...extra };
  });
  let flatMessages = $derived(
    toFlatMessageView(filteredTimeline, {
      threadId,
      suppressDisplayDocumentId,
      suppressThreadRefs: true,
      artifacts: timelineArtifacts,
      cards: timelineCards,
      documents: timelineDocuments,
      routeMaps,
      notificationReceiptsByEventId: timelineNotificationReceipts,
    }),
  );
  let allMessages = $derived(flatMessages);
  let hasMessages = $derived(flatMessages.length > 0);
  let archivedMessageCount = $derived(
    refScopedTimeline.filter(
      (e) =>
        String(e?.type ?? "") === "message_posted" &&
        e.archived_at &&
        !e.trashed_at,
    ).length,
  );
  let timelineHasAnyMessagePosted = $derived(
    refScopedTimeline.some((e) => String(e?.type ?? "") === "message_posted"),
  );
  let hasAnyNonTrashedMessage = $derived(
    refScopedTimeline.some(
      (e) => String(e?.type ?? "") === "message_posted" && !e.trashed_at,
    ),
  );
  let showSyncStatus = $derived(timelineLoading && timelineHasAnyMessagePosted);

  let messageText = $state("");
  let replyToEventId = $state("");
  /** Handle for an @ mention we prepended for the current reply target (strip when clearing reply). */
  let replyAutoMentionHandle = $state("");
  let replyTargetMessage = $derived(
    replyToEventId
      ? (allMessages.find(
          (message) => String(message?.id ?? "") === replyToEventId,
        ) ?? null)
      : null,
  );
  let replyChipDetailTitle = $derived(
    replyAutoMentionHandle
      ? `Notifying @${replyAutoMentionHandle} is pre-filled; delete that mention from the message to skip wake.`
      : "",
  );
  let replyTargetAuthorName = $derived(
    replyTargetMessage
      ? actorName(String(replyTargetMessage.actor_id ?? ""))
      : "",
  );
  let postingMessage = $state(false);
  let postMessageError = $state("");
  let attachingFile = $state(false);
  let attachmentError = $state("");
  let pendingAttachmentRefs = $state([]);
  let pendingAttachmentUpload = $state(null);
  let pendingComposerArtifactsByRef = $state(
    /** @type {Record<string, Record<string, unknown>>} */ ({}),
  );

  let mentionCandidates = $state([]);
  let mentionLoading = $state(false);
  let mentionOpen = $state(false);
  let mentionQuery = $state("");
  let mentionHighlight = $state(0);
  let mentionSignedIn = $state(false);
  let textareaRef = $state(null);
  /** Scrollport for the message list when `pinComposerNarrow` (mobile dock). */
  let messagesScrollEl = $state(/** @type {HTMLDivElement | null} */ (null));
  let handledDeepLinkKey = $state("");

  /** Nearest vertical scrollport from `messagesScrollEl` (list or parent rail). */
  function findMessagesScrollport() {
    if (!browser || !messagesScrollEl) return null;
    for (
      var el = /** @type {HTMLElement | null} */ (messagesScrollEl);
      el;
      el = el.parentElement
    ) {
      const oy = getComputedStyle(el).overflowY;
      if (
        (oy === "auto" || oy === "scroll") &&
        el.scrollHeight > el.clientHeight + 1
      ) {
        return el;
      }
    }
    return null;
  }

  /** Scroll the nearest overflow-y scrollport (list or parent rail). */
  function scrollMessagesToBottom() {
    const scrollport = findMessagesScrollport();
    if (!scrollport) return;
    scrollport.scrollTo({ top: scrollport.scrollHeight, behavior: "smooth" });
  }

  /**
   * After posting, align the new message card to the bottom of the scrollport so
   * it sits just above the composer (instead of jumping to the global end of a
   * long flat list when the new bubble is nested under a reply).
   */
  function scrollPostedMessageIntoView(eventId) {
    if (!browser || !messagesScrollEl) return;
    const id = String(eventId ?? "").trim();
    if (!id) {
      scrollMessagesToBottom();
      return;
    }
    const target = document.getElementById(`message-${id}`);
    const scrollport = findMessagesScrollport();
    if (!target || !scrollport) {
      scrollMessagesToBottom();
      return;
    }
    const align = () => {
      const elRect = target.getBoundingClientRect();
      const portRect = scrollport.getBoundingClientRect();
      const delta = elRect.bottom - portRect.bottom;
      const maxTop = Math.max(
        0,
        scrollport.scrollHeight - scrollport.clientHeight,
      );
      const nextTop = Math.min(
        maxTop,
        Math.max(0, scrollport.scrollTop + delta),
      );
      scrollport.scrollTo({ top: nextTop, behavior: "smooth" });
    };
    align();
    requestAnimationFrame(align);
  }

  function messageEventById(eventId) {
    const id = String(eventId ?? "").trim();
    if (!id) return null;
    return (
      refScopedTimeline.find(
        (event) =>
          String(event?.id ?? "") === id &&
          String(event?.type ?? "") === "message_posted",
      ) ?? null
    );
  }

  $effect(() => {
    if (!browser) return;
    const target = messageTargetFromHash($page.url.hash);
    const targetId = String(target.id ?? "").trim();
    if (!targetId) {
      handledDeepLinkKey = "";
      return;
    }

    const event = messageEventById(targetId);
    if (!event || event.trashed_at) return;
    if (event.archived_at && !showArchived) {
      showArchived = true;
      return;
    }
    if (!allMessages.some((message) => String(message?.id) === targetId)) {
      return;
    }

    const key = `${targetId}:${showArchived ? "archived" : "active"}:${allMessages.length}`;
    if (handledDeepLinkKey === key) return;
    handledDeepLinkKey = key;

    void tick().then(() => {
      const element = document.getElementById(`message-${targetId}`);
      scrollAndHighlightTarget(element, { scrollport: messagesScrollEl });
    });
  });

  let filteredMentions = $derived(
    filterMentionCandidates(mentionCandidates, mentionQuery).slice(0, 12),
  );

  let canPost = $derived(Boolean(messageText.trim()) && !postingMessage);

  function isActiveDocumentComment(
    /** @type {Record<string, unknown> | null} */ v,
  ) {
    if (!v || typeof v !== "object") {
      return false;
    }
    return Boolean(
      String(v.document_id ?? "").trim() && String(v.revision_id ?? "").trim(),
    );
  }

  let hasPendingDocumentComment = $derived(
    isActiveDocumentComment(
      /** @type {Record<string, unknown> | null} */ (pendingDocumentComment),
    ),
  );

  let postSubmitButtonText = $derived(
    postingMessage
      ? "Posting..."
      : String(postButtonLabel ?? "").trim() ||
          (hasPendingDocumentComment ? "Comment" : "Post message"),
  );

  let pendingSelectedQuote = $derived(
    hasPendingDocumentComment &&
      pendingDocumentComment &&
      typeof pendingDocumentComment === "object"
      ? String(
          /** @type {any} */ (pendingDocumentComment).selected_text ?? "",
        ).trim() || "(empty selection)"
      : "",
  );

  let pendingIsQuoteOnly = $derived(
    hasPendingDocumentComment &&
      pendingDocumentComment &&
      typeof pendingDocumentComment === "object" &&
      String(
        /** @type {any} */ (pendingDocumentComment).anchor_status ?? "",
      ).trim() === "quote_only",
  );

  /**
   * Read-side stale-anchor derivation. We don't mutate the original event
   * payload; we just compute "is this comment's quoted text still present
   * in the displayed revision?" once per render and surface it as
   * `liveAnchorStatus="stale"` on the matching `MessageItem`. This is the
   * cheap shape of Google Docs' "your comment is now suggesting on text
   * that no longer exists" treatment, without any backend changes.
   */
  let normalizedDocContent = $derived(
    String(currentDocumentContent ?? "").replace(/\r\n/g, "\n"),
  );
  let docContentSearchable = $derived(Boolean(normalizedDocContent));
  function liveAnchorStatusForMessage(message) {
    const dc = message?.documentComment;
    if (!dc) return "";
    if (!docContentSearchable) return "";
    const quote = String(dc.selected_text ?? "").trim();
    if (!quote) return "";
    if (normalizedDocContent.includes(quote)) {
      return "";
    }
    return "stale";
  }

  function buildPostedDocumentAnchors() {
    /** @type {Array<{ eventId: string, quote: string }>} */
    const out = [];
    // Match the same events the operator sees in the thread list (respects
    // "Show archived") so body underlines do not linger for resolved comments
    // when those rows are hidden.
    for (const e of filteredTimeline) {
      if (String(e?.type ?? "") !== "message_posted" || e.trashed_at) continue;
      if (String(e?.payload?.kind ?? "") !== "document_text_comment") {
        continue;
      }
      const raw = e?.payload?.document_comment;
      if (!raw || typeof raw !== "object") continue;
      const q = String(
        /** @type {Record<string, unknown>} */ (raw).selected_text ?? "",
      ).trim();
      if (!q) continue;
      const id = String(e?.id ?? "").trim();
      if (!id) continue;
      out.push({ eventId: id, quote: q });
    }
    return out;
  }

  function countActiveDocumentAnchors() {
    let n = 0;
    for (const e of refScopedTimeline) {
      if (String(e?.type ?? "") !== "message_posted") continue;
      if (e.trashed_at || e.archived_at) continue;
      if (String(e?.payload?.kind ?? "") !== "document_text_comment") {
        continue;
      }
      const raw = e?.payload?.document_comment;
      if (!raw || typeof raw !== "object") continue;
      const q = String(
        /** @type {Record<string, unknown>} */ (raw).selected_text ?? "",
      ).trim();
      if (q) n += 1;
    }
    return n;
  }

  $effect(() => {
    if (!browser) {
      return;
    }
    void refScopedTimeline;
    void showArchived;
    onDocumentTextAnchorContextChange?.({
      posted: buildPostedDocumentAnchors(),
      activeAnchoredCount: countActiveDocumentAnchors(),
    });
  });

  async function refreshMentionCandidates() {
    if (!browser) {
      return;
    }
    mentionLoading = true;
    try {
      const agent = get(authenticatedAgent);
      const reg = get(actorRegistry);
      const principals = get(principalRegistry);
      const nameFn = (id) => lookupActorDisplayName(id, reg, principals);
      mentionSignedIn = Boolean(agent);

      if (agent) {
        const fetchedPrincipals = await listAllPrincipals(coreClient, {
          limit: 100,
        });
        const enrichedPrincipals = await enrichPrincipalsWithWakeRouting(
          fetchedPrincipals,
          {
            workspaceBindingTarget: workspaceId,
            client: coreClient,
          },
        );
        mentionCandidates = taggableAgentHandlesFromPrincipals(
          enrichedPrincipals,
          nameFn,
        );
      } else {
        mentionCandidates = [];
      }
    } catch {
      mentionCandidates = [];
    } finally {
      mentionLoading = false;
    }
  }

  onMount(() => {
    if (!browser) {
      return;
    }
    let lastAgentId = "\u0000";
    const onAgentIdentityChange = () => {
      const agent = get(authenticatedAgent);
      const id = String(agent?.agent_id ?? "");
      if (id === lastAgentId) {
        return;
      }
      lastAgentId = id;
      void refreshMentionCandidates();
    };
    return authenticatedAgent.subscribe(onAgentIdentityChange);
  });

  $effect(() => {
    if (!browser) {
      return;
    }
    void workspaceId;
    untrack(() => void refreshMentionCandidates());
  });

  function updateMentionFromTextarea() {
    const el = textareaRef;
    if (!el) {
      return;
    }
    const parsed = parseActiveMention(messageText, el.selectionStart);
    if (!parsed) {
      mentionOpen = false;
      return;
    }
    const prev = mentionQuery;
    mentionQuery = parsed.query;
    if (prev !== parsed.query) {
      mentionHighlight = 0;
    }
    mentionOpen = true;
  }

  function closeMentions() {
    mentionOpen = false;
  }

  async function insertMention(handle) {
    const el = textareaRef;
    if (!el) {
      return;
    }
    const value = messageText;
    const sel = el.selectionStart;
    const parsed = parseActiveMention(value, sel);
    if (!parsed) {
      closeMentions();
      return;
    }
    const before = value.slice(0, parsed.atIndex);
    const after = value.slice(sel);
    const insertion = `@${handle} `;
    messageText = before + insertion + after;
    closeMentions();
    await tick();
    const pos = before.length + insertion.length;
    el.focus();
    el.setSelectionRange(pos, pos);
  }

  function handleMessageKeydown(e) {
    if (!mentionOpen) {
      // Outside the mention picker, Esc clears a pending document text
      // comment first (so the operator can quickly back out of "comment on
      // selection" mode without reaching for the mouse). Falls through if
      // there is nothing pending.
      if (e.key === "Escape" && hasPendingDocumentComment) {
        e.preventDefault();
        onClearPendingDocumentPost?.();
      }
      return;
    }
    const list = filterMentionCandidates(mentionCandidates, mentionQuery).slice(
      0,
      12,
    );
    if (e.key === "Escape") {
      e.preventDefault();
      closeMentions();
      return;
    }
    if (list.length === 0) {
      return;
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      mentionHighlight = (mentionHighlight + 1) % list.length;
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      mentionHighlight = (mentionHighlight - 1 + list.length) % list.length;
    } else if (e.key === "Enter" && !e.shiftKey && !e.metaKey && !e.ctrlKey) {
      e.preventDefault();
      void insertMention(list[mentionHighlight].handle);
    } else if (e.key === "Tab" && !e.shiftKey) {
      e.preventDefault();
      void insertMention(list[mentionHighlight].handle);
    }
  }

  function stripLeadingAutoMentionPrefix(text, handle) {
    const h = String(handle ?? "").trim();
    if (!h) return String(text ?? "");
    const prefix = `@${h} `;
    const t = String(text ?? "");
    return t.startsWith(prefix) ? t.slice(prefix.length) : t;
  }

  async function setReplyTarget(eventId) {
    const id = String(eventId ?? "").trim();
    const msg = id
      ? (allMessages.find((m) => String(m?.id ?? "") === id) ?? null)
      : null;
    const authorActorId = String(msg?.actor_id ?? "").trim();

    let text = messageText;
    if (replyAutoMentionHandle) {
      text = stripLeadingAutoMentionPrefix(text, replyAutoMentionHandle);
    }

    replyToEventId = id;

    const fromCandidate = mentionCandidates.find(
      (c) => String(c.actorId ?? "").trim() === authorActorId,
    );
    const handle =
      String(fromCandidate?.handle ?? "").trim() ||
      taggableWakeHandleForActorId(authorActorId, get(principalRegistry));

    replyAutoMentionHandle = handle;

    if (handle) {
      const prefix = `@${handle} `;
      if (!text.startsWith(prefix)) {
        text = prefix + text;
      }
    }

    messageText = text;
    await tick();
    const el = textareaRef;
    if (!el || !browser) return;
    el.focus();
    const caret = handle
      ? Math.min(`@${handle} `.length, text.length)
      : text.length;
    el.setSelectionRange(caret, caret);
  }

  function clearReplyTarget() {
    if (replyAutoMentionHandle) {
      messageText = stripLeadingAutoMentionPrefix(
        messageText,
        replyAutoMentionHandle,
      );
    }
    replyAutoMentionHandle = "";
    replyToEventId = "";
  }

  async function refreshTimeline() {
    await timelineCtx.refreshTimeline();
  }

  function openArchiveConfirm(eventId) {
    confirmModal = { open: true, action: "archive", eventId };
  }

  function openTrashConfirm(eventId) {
    confirmModal = { open: true, action: "trash", eventId };
  }

  function clearConfirmModal() {
    confirmModal = emptyMessageEventConfirmModal();
  }

  function handleConfirm() {
    const { action, eventId } = confirmModal;
    clearConfirmModal();
    if (action === "archive") doArchive(eventId);
    else if (action === "trash") doTrash(eventId);
  }

  async function doArchive(eventId) {
    if (!eventId || lifecycleBusy) return;
    lifecycleBusy = true;
    lifecycleError = "";
    try {
      await coreClient.archiveEvent(eventId, {});
      await refreshTimeline();
    } catch (e) {
      lifecycleError = `Archive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      lifecycleBusy = false;
    }
  }

  async function doUnarchive(eventId) {
    if (!eventId || lifecycleBusy) return;
    lifecycleBusy = true;
    lifecycleError = "";
    try {
      await coreClient.unarchiveEvent(eventId, {});
      await refreshTimeline();
    } catch (e) {
      lifecycleError = `Unarchive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      lifecycleBusy = false;
    }
  }

  async function doTrash(eventId) {
    if (!eventId || lifecycleBusy) return;
    lifecycleBusy = true;
    lifecycleError = "";
    try {
      await coreClient.trashEvent(eventId, {});
      await refreshTimeline();
    } catch (e) {
      lifecycleError = `Trash failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      lifecycleBusy = false;
    }
  }

  async function handlePostMessage() {
    if (!messageText.trim()) {
      postMessageError = "Message text is required.";
      return;
    }
    postingMessage = true;
    postMessageError = "";
    try {
      const baseRefs = [
        `thread:${threadId}`,
        ...(replyToEventId ? [`event:${replyToEventId}`] : []),
      ];
      const extra = (Array.isArray(extraPostRefs) ? extraPostRefs : [])
        .map((r) => String(r ?? "").trim())
        .filter(Boolean);
      const pendingAttachments = (
        Array.isArray(pendingAttachmentRefs) ? pendingAttachmentRefs : []
      )
        .map((r) => String(r ?? "").trim())
        .filter(Boolean);
      const docCom = hasPendingDocumentComment
        ? /** @type {Record<string, unknown>} */ (pendingDocumentComment)
        : null;
      const revRef =
        docCom && String(docCom.revision_id ?? "").trim()
          ? `document_revision:${String(docCom.revision_id).trim()}`
          : "";
      const refs = [
        ...new Set([
          ...baseRefs,
          ...extra,
          ...pendingAttachments,
          ...[revRef].filter(Boolean),
        ]),
      ];
      const trimmed = messageText.trim();
      let summary = `Message: ${trimmed.slice(0, 100)}`;
      let payload;
      if (docCom) {
        summary = `Comment on document text: ${trimmed.slice(0, 80)}`;
        payload = {
          text: trimmed,
          kind: "document_text_comment",
          document_comment: { ...docCom },
        };
      } else {
        payload = { text: trimmed };
      }
      const postResult = await onMessagePost(routeScopeForPost, {
        type: "message_posted",
        thread_id: threadId,
        thread_ref: `thread:${threadId}`,
        refs,
        summary,
        payload,
        provenance: { sources: ["event:ui"] },
      });
      messageText = "";
      replyToEventId = "";
      replyAutoMentionHandle = "";
      pendingAttachmentRefs = [];
      pendingComposerArtifactsByRef = {};
      attachmentError = "";
      closeMentions();
      if (docCom) {
        onPendingDocumentPostConsumed?.();
      }
      const postedId =
        postResult &&
        typeof postResult === "object" &&
        postResult.event &&
        postResult.event.id
          ? String(postResult.event.id)
          : "";
      await tick();
      scrollPostedMessageIntoView(postedId);
      requestAnimationFrame(() => scrollPostedMessageIntoView(postedId));
    } catch (error) {
      postMessageError = `Failed to post: ${error instanceof Error ? error.message : String(error)}`;
    } finally {
      postingMessage = false;
    }
  }

  async function handleAttachFile(event) {
    const input = event.currentTarget;
    const file = input?.files?.[0];
    if (!file || attachingFile) return;
    attachingFile = true;
    pendingAttachmentUpload = {
      original_filename: file.name || "attachment.bin",
      content_type: file.type || "application/octet-stream",
      size_bytes: file.size,
    };
    attachmentError = "";
    try {
      const refs = [
        ...new Set(
          [
            `thread:${threadId}`,
            subjectRefFilterNorm,
            ...(Array.isArray(extraPostRefs) ? extraPostRefs : []),
          ]
            .map((ref) => String(ref ?? "").trim())
            .filter(Boolean),
        ),
      ];
      const payload = await coreClient.createArtifactAttachment({
        refs,
        file,
      });
      const id = String(payload?.artifact?.id ?? "").trim();
      if (!id) {
        attachmentError = "Upload succeeded but artifact id missing.";
        return;
      }
      const ref = `artifact:${id}`;
      pendingAttachmentRefs = [...new Set([...pendingAttachmentRefs, ref])];
      const row =
        payload?.artifact && typeof payload.artifact === "object"
          ? payload.artifact
          : null;
      if (row) {
        pendingComposerArtifactsByRef = {
          ...pendingComposerArtifactsByRef,
          [ref]: /** @type {Record<string, unknown>} */ (row),
        };
      }
    } catch (error) {
      attachmentError = `Upload failed: ${error instanceof Error ? error.message : String(error)}`;
    } finally {
      attachingFile = false;
      pendingAttachmentUpload = null;
      if (input) input.value = "";
    }
  }
</script>

<div
  class="msgtab-wrap"
  class:msgtab-wrap--pin={pinActive}
  class:msgtab-wrap--pin-narrow={pinComposerNarrow && !pinComposer}
  class:msgtab-wrap--comfort-gap={pinComposerComfortGap}
  class:h-full={pinActive}
  class:min-h-0={pinActive}
>
  <div bind:this={messagesScrollEl} class="msgtab-messages flex flex-col gap-3">
    {#if archivedMessageCount > 0 || showSyncStatus}
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex flex-wrap items-center gap-2 sm:gap-3">
          {#if archivedMessageCount > 0}
            <label class="flex items-center gap-1.5 text-micro text-fg-muted">
              <input
                type="checkbox"
                bind:checked={showArchived}
                class="accent-accent"
              />
              Show archived ({archivedMessageCount})
            </label>
          {/if}
        </div>
        <div class="min-h-[1rem] text-right" aria-live="polite">
          {#if showSyncStatus}
            <p class="text-micro text-fg-muted">Syncing…</p>
          {/if}
        </div>
      </div>
    {/if}
    {#if timelineError && !hasAnyNonTrashedMessage}
      <p class="rounded bg-danger-soft px-3 py-2 text-meta text-danger-text">
        {timelineError}
      </p>
    {:else if timelineLoading && !hasAnyNonTrashedMessage}
      <p class="text-meta text-fg-muted">Loading messages...</p>
    {:else if !hasAnyNonTrashedMessage}
      <p class="py-6 text-center text-meta text-fg-muted">
        {String(discussionEmptyMessage ?? "").trim()
          ? String(discussionEmptyMessage)
          : "No messages yet. Post a message below to start the conversation."}
      </p>
    {:else if !hasMessages}
      <p class="text-meta text-fg-muted">
        No messages in view. Turn on Show archived to see archived messages.
      </p>
    {:else}
      <div
        class="msgtab-thread flex min-w-0 flex-col gap-3"
        class:msgtab-thread--pin={pinActive && pinComposerAlignThreadEnd}
      >
        {#if lifecycleError}
          <p
            class="rounded bg-danger-soft px-3 py-2 text-meta text-danger-text"
          >
            {lifecycleError}
          </p>
        {/if}
        {#if timelineError}
          <p
            class="rounded bg-danger-soft px-3 py-2 text-meta text-danger-text"
          >
            {timelineError}
          </p>
        {/if}
        <div
          class="flex min-w-0 flex-col gap-1.5 sm:gap-2"
          class:msgtab-thread-items--pin-end={pinActive &&
            pinComposerAlignThreadEnd}
        >
          {#each flatMessages as message (message.id)}
            <MessageItem
              {message}
              {threadId}
              {actorName}
              onReply={setReplyTarget}
              onArchive={openArchiveConfirm}
              onTrash={openTrashConfirm}
              onUnarchive={doUnarchive}
              {lifecycleBusy}
              {archiveLabelKind}
              artifactRoutesById={artifactRoutesWithComposerPending}
              eventRoutesById={routeMaps.eventRoutesById}
              getLiveAnchorStatusForMessage={liveAnchorStatusForMessage}
            />
          {/each}
        </div>
      </div>
    {/if}
  </div>

  <form
    class="msg-composer mt-4 rounded-md border border-line bg-panel p-3"
    onsubmit={(e) => {
      e.preventDefault();
      void handlePostMessage();
    }}
  >
    {#if postMessageError}
      <p
        class="mb-2 rounded bg-danger-soft px-3 py-1.5 text-micro text-danger-text"
      >
        {postMessageError}
      </p>
    {/if}
    {#if hasPendingDocumentComment}
      <!--
      Compact "comment on:" chip that lives directly above the textarea.
      We deliberately drop the older "Replying to selected text" heading +
      separate quote box: it doubled visual weight without adding info.
      The leading speech-bubble icon plus a single italic line of quoted
      text reads as "you are commenting on this" at a glance — closer to
      Google Docs' inline composer than a mini thread reply.
    -->
      <div
        class="mb-2 flex items-center gap-2 rounded-md border border-line bg-bg-soft px-2 py-1.5"
      >
        <svg
          class="h-3.5 w-3.5 shrink-0 text-accent"
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
        <span
          class="min-w-0 flex-1 truncate text-meta italic text-fg"
          title={pendingSelectedQuote}
        >
          {pendingSelectedQuote}
        </span>
        {#if pendingIsQuoteOnly}
          <span
            class="shrink-0 rounded bg-line-subtle px-1.5 py-0.5 text-micro text-fg-muted"
            title="Exact position not unique in this revision — comment is anchored by quote."
          >
            Quote only
          </span>
        {/if}
        <button
          type="button"
          class="shrink-0 cursor-pointer rounded p-0.5 text-fg-muted hover:bg-panel hover:text-fg"
          onclick={() => {
            onClearPendingDocumentPost?.();
          }}
          title="Clear (Esc)"
          aria-label="Clear comment selection"
        >
          <svg
            class="h-3.5 w-3.5"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            viewBox="0 0 24 24"
            aria-hidden="true"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M6 18 18 6M6 6l12 12"
            />
          </svg>
        </button>
      </div>
    {/if}
    {#if replyToEventId}
      <!--
      "Replying to" chip lives above the textarea now (same row as the
      pending-comment chip when both are present). Previously this chip
      sat beside the Post button in the footer, which crammed the help
      text + reply target + Clear + Post button into a single row that
      could not fit at typical rail widths (~280–360px). Surfacing it
      here also matches the visual hierarchy of "what you're responding
      to" → composer → action.
    -->
      <div
        class="mb-2 flex items-center gap-2 rounded-md border border-line bg-bg-soft px-2 py-1.5"
        title={replyChipDetailTitle || undefined}
      >
        <svg
          class="h-3.5 w-3.5 shrink-0 text-accent"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          viewBox="0 0 24 24"
          aria-hidden="true"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M9 14 4 9l5-5M4 9h11a5 5 0 0 1 5 5v0a5 5 0 0 1-5 5h-3"
          />
        </svg>
        <span class="shrink-0 text-micro text-fg-muted">Replying to</span>
        {#if replyTargetAuthorName}
          <span class="shrink-0 font-medium text-fg-muted"
            >{replyTargetAuthorName}</span
          >
          <span class="shrink-0 text-fg-subtle" aria-hidden="true">·</span>
        {/if}
        <span
          class="min-w-0 flex-1 truncate text-meta italic text-fg"
          title={replyTargetMessage?.messageText || "message"}
        >
          {replyTargetMessage?.messageText
            ? replyTargetMessage.messageText.slice(0, 200)
            : "message"}
        </span>
        <button
          type="button"
          class="shrink-0 cursor-pointer rounded p-0.5 text-fg-muted hover:bg-panel hover:text-fg"
          onclick={clearReplyTarget}
          title="Clear reply"
          aria-label="Clear reply target"
        >
          <svg
            class="h-3.5 w-3.5"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            viewBox="0 0 24 24"
            aria-hidden="true"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M6 18 18 6M6 6l12 12"
            />
          </svg>
        </button>
      </div>
    {/if}
    <div class="relative">
      <textarea
        bind:this={textareaRef}
        bind:value={messageText}
        aria-label="Message"
        class="w-full min-h-[4.25rem] resize-y rounded-md border border-line bg-bg-soft px-3 py-2 text-meta text-fg"
        id="message-text"
        oninput={updateMentionFromTextarea}
        onclick={updateMentionFromTextarea}
        onkeyup={updateMentionFromTextarea}
        onkeydown={handleMessageKeydown}
        placeholder={hasPendingDocumentComment
          ? "Add a comment, or @mention an agent…"
          : "Write a message..."}
        rows="2"
      ></textarea>
      {#if mentionOpen}
        <div
          class="absolute bottom-full left-0 right-0 z-20 mb-1 max-h-48 overflow-auto rounded-md border border-line bg-panel py-1"
          id="message-mention-list"
          role="listbox"
          aria-label="Agent handles"
        >
          {#if mentionLoading}
            <p class="px-3 py-2 text-micro text-fg-muted">Loading handles…</p>
          {:else if mentionCandidates.length === 0}
            {#if mentionSignedIn}
              <p class="px-3 py-2 text-micro text-fg-muted">
                No registered agents are taggable in this workspace. See Access
                to check registration and presence.
              </p>
            {:else}
              <p class="px-3 py-2 text-micro text-fg-muted">
                No agent handles in this workspace. Sign in or open Access to
                manage agents.
              </p>
            {/if}
          {:else if filteredMentions.length === 0}
            <p class="px-3 py-2 text-micro text-fg-muted">
              No matching agents.
            </p>
          {:else}
            {#each filteredMentions as row, i (row.handle)}
              <button
                type="button"
                class="flex w-full cursor-pointer items-baseline gap-2 px-3 py-1.5 text-left text-micro hover:bg-bg-soft {i ===
                mentionHighlight
                  ? 'bg-bg-soft'
                  : ''}"
                aria-selected={i === mentionHighlight}
                role="option"
                onmousedown={(e) => {
                  e.preventDefault();
                  void insertMention(row.handle);
                }}
              >
                <span class="font-medium text-accent">@{row.handle}</span>
                <span class="truncate text-fg-muted">{row.displayLabel}</span>
                <span
                  class="shrink-0 rounded px-1.5 py-0.5 text-micro font-medium {row.presenceClass}"
                  title={row.presenceSummary}
                >
                  {row.presenceLabel}
                </span>
              </button>
            {/each}
          {/if}
        </div>
      {/if}
    </div>
    <!--
    Composer footer. The form sets `container-type: inline-size` (see the
    `<style>` block below), so this row's layout responds to the actual
    composer width rather than the viewport. At narrow widths typical of
    the discussion rail we stack: help text on top, action button on its
    own row aligned right; at wider widths (topic / card pages) we put
    them side-by-side. This is what fixes the previous "Replying to: …
    Clear Post message" overlap with the @handle help text.

    Hint + attachment chips must live in `.msg-footer-main` so at wide
    widths we only lay out **two** flex items (main column vs buttons).
    If the hint paragraph were a sibling of the attachment row in the
    same horizontal flex row, it would shrink beside the chip and wrap
    one word per line.
  -->
    <div class="msg-footer mt-1.5 flex flex-col gap-2">
      <div class="msg-footer-main flex min-w-0 flex-col gap-2">
        {#if hasPendingDocumentComment}
          <!--
          On the "comment on selection" path we don't need the @handle
          explainer above the submit button — the operator just wants
          to write a comment. The single-word `@` hint keeps mentions
          discoverable for power users without dominating the composer.
        -->
          <p class="msg-hint text-micro leading-snug text-fg-muted">
            Tip: <code class="text-fg">@</code> mentions an agent · Esc clears
          </p>
        {:else}
          <p class="msg-hint text-micro leading-snug text-fg-muted">
            Mention <code class="text-fg">@handle</code> to tag a
            <a
              class="text-accent-text hover:text-accent-text"
              href={workspacePath(organizationSlug, workspaceSlug, "/access")}
              >registered agent</a
            >.
          </p>
        {/if}
        {#if pendingAttachmentRefs.length > 0 || pendingAttachmentUpload}
          <div class="flex flex-wrap items-center gap-1.5 text-micro">
            <span class="text-fg-muted">Attached</span>
            {#if pendingAttachmentUpload}
              {@const pendingResolved = resolveRefLink(
                "artifact:upload-pending",
                {
                  threadId,
                  boardId: "",
                  humanize: true,
                  artifactRoutesById: {},
                  eventRoutesById: {},
                  workspaceSlug,
                  organizationSlug,
                },
              )}
              <AttachmentChip
                resolved={pendingResolved}
                artifactOverlay={pendingAttachmentUpload}
                pending
                size="compact"
              />
            {/if}
            {#each pendingAttachmentRefs as ref (ref)}
              {@const composerResolved = resolveRefLink(ref, {
                threadId,
                boardId: "",
                humanize: true,
                artifactRoutesById: artifactRoutesWithComposerPending,
                eventRoutesById: routeMaps.eventRoutesById,
                workspaceSlug,
                organizationSlug,
              })}
              <span class="inline-flex max-w-full items-center gap-1">
                <AttachmentChip resolved={composerResolved} size="compact" />
                <button
                  class="shrink-0 text-fg-muted hover:text-fg"
                  type="button"
                  aria-label={`Remove ${ref}`}
                  onclick={() => {
                    pendingAttachmentRefs = pendingAttachmentRefs.filter(
                      (candidate) => candidate !== ref,
                    );
                    const next = { ...pendingComposerArtifactsByRef };
                    delete next[ref];
                    pendingComposerArtifactsByRef = next;
                  }}
                >
                  ×
                </button>
              </span>
            {/each}
          </div>
        {/if}
        {#if attachmentError}
          <p class="text-micro text-danger-text">{attachmentError}</p>
        {/if}
      </div>
      <div class="msg-actions flex shrink-0 items-center justify-end gap-2">
        <label
          class="inline-flex cursor-pointer items-center rounded border border-line bg-bg px-3 py-1 text-micro font-medium text-fg hover:bg-bg-soft"
        >
          {attachingFile ? "Uploading…" : "Attach file"}
          <input
            class="sr-only"
            accept="image/*,text/plain,text/markdown,text/csv,.md,.txt,.csv,.json,.pdf"
            disabled={attachingFile || postingMessage}
            onchange={handleAttachFile}
            type="file"
          />
        </label>
        <button
          class="cursor-pointer rounded bg-accent-solid px-3 py-1 text-micro font-medium text-white hover:bg-accent disabled:opacity-50"
          disabled={!canPost}
          type="submit"
        >
          {postSubmitButtonText}
        </button>
      </div>
    </div>
  </form>
</div>

<ConfirmModal
  open={confirmModal.open}
  title={confirmModal.action === "trash"
    ? "Move message to trash"
    : "Archive message"}
  message={confirmModal.action === "trash"
    ? "This message and all its replies will be moved to trash. You can restore them later."
    : "This message and all its replies will be archived. Toggle 'Show archived' to see them again."}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={lifecycleBusy}
  onconfirm={handleConfirm}
  oncancel={clearConfirmModal}
/>

<style>
  /* Container query: lets the composer footer adapt to the actual width
     of the composer (rail vs full page) without a JS resize observer. */
  .msg-composer {
    container-type: inline-size;
    container-name: msg-form;
  }
  @container msg-form (min-width: 22rem) {
    .msg-footer {
      flex-direction: row;
      align-items: flex-start;
      justify-content: space-between;
      gap: 0.75rem;
    }
    .msg-footer-main {
      flex: 1 1 0%;
      min-width: 0;
    }
  }

  /* `--pin` activates at all widths (rail, primary, board dock when used).
     `--pin-narrow` keeps the legacy mobile-only behavior for callers that
     have not migrated. They share the same selectors except where noted. */
  .msgtab-wrap--pin {
    display: flex;
    min-height: 0;
    flex: 1 1 auto;
    flex-direction: column;
  }

  .msgtab-wrap--pin .msgtab-messages {
    flex: 1 1 auto;
    min-height: 0;
    min-width: 0;
    overscroll-behavior-y: contain;
    overflow-x: hidden;
    overflow-y: auto;
    padding: 0.875rem 0.75rem 0.75rem;
    scroll-padding-top: 0.5rem;
    scroll-padding-bottom: 0.5rem;
  }

  .msgtab-wrap--pin .msgtab-thread--pin {
    flex: 1 0 auto;
    min-height: 0;
  }

  .msgtab-wrap--pin .msgtab-thread-items--pin-end {
    flex: 1 0 auto;
    justify-content: flex-end;
    padding-bottom: 0.75rem;
  }

  .msgtab-wrap--pin.msgtab-wrap--comfort-gap .msgtab-messages {
    padding-top: 1rem;
    padding-bottom: 1rem;
  }

  .msgtab-wrap--pin.msgtab-wrap--comfort-gap .msg-composer {
    margin-top: 1rem;
  }

  .msgtab-wrap--pin .msg-composer {
    flex-shrink: 0;
    margin-top: 0;
    border-top: 1px solid var(--line);
    border-left: none;
    border-right: none;
    border-bottom: none;
    border-radius: 0;
    box-shadow: 0 -6px 20px rgba(0, 0, 0, 0.06);
    padding: 0.75rem;
  }

  @media (max-width: 1023px) {
    .msgtab-wrap--pin .msgtab-messages {
      -webkit-overflow-scrolling: touch;
      /* `scroll` establishes a scrollport more reliably than `auto` on iOS when
         height is resolved from a flex chain. */
      overflow-y: scroll;
    }

    .msgtab-wrap--pin .msg-composer {
      box-shadow: 0 -6px 20px rgba(0, 0, 0, 0.12);
      /* Keep controls above the fixed shell bottom nav (see app.css). */
      padding-bottom: calc(0.5rem + env(safe-area-inset-bottom, 0px));
    }

    /* Mobile-only legacy callers reuse the same shape as `--pin`. */
    .msgtab-wrap--pin-narrow {
      display: flex;
      min-height: 0;
      flex: 1 1 auto;
      flex-direction: column;
    }
    .msgtab-wrap--pin-narrow .msgtab-messages {
      flex: 1 1 auto;
      min-height: 0;
      min-width: 0;
      -webkit-overflow-scrolling: touch;
      overscroll-behavior-y: contain;
      overflow-x: hidden;
      overflow-y: scroll;
      padding: 0.875rem 0.75rem 0.75rem;
      scroll-padding-top: 0.5rem;
      scroll-padding-bottom: 0.5rem;
    }
    .msgtab-wrap--pin-narrow .msgtab-thread--pin {
      flex: 1 0 auto;
      min-height: 0;
    }
    .msgtab-wrap--pin-narrow .msgtab-thread-items--pin-end {
      flex: 1 0 auto;
      justify-content: flex-end;
      padding-bottom: 0.75rem;
    }
    .msgtab-wrap--pin-narrow.msgtab-wrap--comfort-gap .msgtab-messages {
      padding-top: 1rem;
      padding-bottom: 1rem;
    }
    .msgtab-wrap--pin-narrow.msgtab-wrap--comfort-gap .msg-composer {
      margin-top: 1rem;
    }
    .msgtab-wrap--pin-narrow .msg-composer {
      flex-shrink: 0;
      margin-top: 0;
      border-top: 1px solid var(--line);
      border-left: none;
      border-right: none;
      border-bottom: none;
      border-radius: 0;
      box-shadow: 0 -6px 20px rgba(0, 0, 0, 0.12);
      padding: 0.75rem 0.75rem calc(0.5rem + env(safe-area-inset-bottom, 0px));
    }
  }
</style>
