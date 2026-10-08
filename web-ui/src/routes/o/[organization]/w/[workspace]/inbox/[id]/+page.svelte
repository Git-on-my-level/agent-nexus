<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { onDestroy, onMount } from "svelte";

  import { accessRequestFromInboxItem } from "$lib/accessGrant.js";
  import {
    authenticatedAgent,
    isHumanWorkspacePrincipal,
  } from "$lib/authSession";
  import Button from "$lib/components/Button.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import AttachmentChip from "$lib/components/AttachmentChip.svelte";
  import { dismissOnEscape } from "$lib/actions/dismissOnEscape.js";
  import InboxActorName from "$lib/components/inbox/InboxActorName.svelte";
  import InboxContextStrip from "$lib/components/inbox/InboxContextStrip.svelte";
  import InboxRespondPanel from "$lib/components/inbox/InboxRespondPanel.svelte";
  import InboxUndoToast from "$lib/components/inbox/InboxUndoToast.svelte";
  import KeyboardShortcutsDialog from "$lib/components/KeyboardShortcutsDialog.svelte";
  import {
    actorDisplayLabel,
    actorRegistry,
    agentRegistry,
    findAgentSummary,
    principalRegistry,
  } from "$lib/actorSession";
  import { coreClient } from "$lib/coreClient";
  import { threadTimelineEventHref } from "$lib/deepLinkTargets";
  import { formatAbsoluteDateTime } from "$lib/formatDate";
  import { loadInboxContext } from "$lib/inboxContext.js";
  import { inboxItemIsReminder, inboxItemSubject } from "$lib/inboxMailbox.js";
  import {
    defaultNotifyMode,
    dismissInboxResponseFailure,
    flushInboxResponse,
    hasPendingInboxResponse,
    inboxResponseFailures,
    queueInboxResponse,
    retryInboxResponse,
    takeInboxRestore,
    undoInboxResponse,
  } from "$lib/inboxResponseQueue.js";
  import {
    inboxShortcutAction,
    inboxShortcutList,
    otherDialogOpen,
  } from "$lib/inboxShortcuts.js";
  import {
    INBOX_CATEGORY_LABELS,
    decodeInboxItemId,
    inboxItemMailboxId,
  } from "$lib/inboxUtils";
  import { MAX_KEYED_PROPOSALS } from "$lib/inboxProposalChoice.js";
  import { formatShortcut } from "$lib/keyboardHints.js";
  import { label as phaseLabel, sentenceCase } from "$lib/pm/presentation.js";
  import { buildPrimitiveRefRoutes, resolveRefLink } from "$lib/refLinkModel";
  import { searchActors } from "$lib/searchHelpers";
  import { bindWorkspaceHref } from "$lib/workspacePaths";

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let inboxItemID = $derived(decodeInboxItemId($page.params.id));

  let loading = $state(false);
  let loadError = $state("");
  let item = $state(null);
  /*
   * The last send for this item, if it failed. The item stays on screen
   * unanswered and says why; nothing is filed as handled until core says so.
   */
  let responseError = $derived(
    item?.id ? ($inboxResponseFailures[item.id]?.error ?? "") : "",
  );
  let responseDraft = $state("");
  let notifyMode = $state("original");
  let notifyTargetActorID = $state("");
  let notifyTargetAgentID = $state("");

  let submitError = $state("");
  let attachingResponseFile = $state(false);
  let pendingResponseAttachmentUpload = $state(null);
  let responseAttachmentError = $state("");
  let responseAttachmentRefs = $state([]);
  let responseComposerArtifactsByRef = $state(
    /** @type {Record<string, Record<string, unknown>>} */ ({}),
  );
  let autosaveInterval = null;
  let notifyTargetQuery = $state("");
  let notifyTargetResults = $state([]);
  let notifyTargetSelected = $state(null);
  let notifyTargetSearchTimer = null;
  let notifyTargetSearchSeq = 0;
  let notifyTargetMenuOpen = $state(false);
  let inboxLoadSeq = 0;
  let loadedInboxRouteKey = $state("");
  let chosen = $state("");
  /* The respond panel owns which suggestion is highlighted; see the pane. */
  let respondPanel = $state(null);
  let helpOpen = $state(false);
  let subject = $state(null);
  let context = $state(null);
  let contextLoading = $state(false);

  // One map, shared with the Inbox pane: two copies meant the same item read
  // "Review due" in the list and "REPORT_REVIEW" on its own page.
  const KIND_LABELS = INBOX_CATEGORY_LABELS;

  let proposalStrings = $derived(
    Array.isArray(item?.response_proposals)
      ? item.response_proposals
          .map((s) => String(s ?? "").trim())
          .filter(Boolean)
      : [],
  );
  let inboxComposerArtifactRoutes = $derived.by(() => {
    const artifacts = Object.values(responseComposerArtifactsByRef).filter(
      (row) => row && typeof row === "object",
    );
    if (!artifacts.length) return {};
    return buildPrimitiveRefRoutes({
      artifacts,
      events: [],
      cards: [],
      documents: [],
      threadId: String(item?.thread_id ?? "").trim(),
    }).artifactRoutesById;
  });

  let inboxRefs = $derived.by(() => {
    const refs = [];
    const seen = new Set();
    const add = (value) => {
      const ref = String(value ?? "").trim();
      if (!ref || seen.has(ref)) return;
      seen.add(ref);
      refs.push(ref);
    };
    add(item?.subject_ref);
    for (const ref of Array.isArray(item?.related_refs)
      ? item.related_refs
      : []) {
      add(ref);
    }
    return refs;
  });

  let notifyTargetPopupOpen = $derived(
    notifyTargetMenuOpen && notifyTargetResults.length > 0,
  );
  let isCompleted = $derived(String(item?.status ?? "").trim() === "completed");
  let isReminder = $derived(inboxItemIsReminder(item));
  /**
   * The dashboard a reminder is about.
   *
   * Read from `subject_ref` on the item rather than from the loaded
   * `subject`, which arrives on a later request: a reminder always names its
   * document, and the one action it offers should not wait on a round trip.
   */
  let reminderHref = $derived(
    isReminder ? subjectHref(inboxItemSubject(item, {})) : "",
  );
  let workspaceHref = $derived(
    bindWorkspaceHref(organizationSlug, workspaceSlug),
  );

  function completedTimelineHref(value = item) {
    const tid = String(value?.thread_id ?? "").trim();
    let eventId = "";
    const ref = String(value?.response_event_ref ?? "").trim();
    if (ref.startsWith("event:")) {
      eventId = ref.slice("event:".length).trim();
    }
    if (!tid || !eventId) return "";
    return threadTimelineEventHref({
      threadId: tid,
      eventId,
      workspaceHref,
    });
  }

  function inboxRouteKey(workspace = workspaceSlug, id = inboxItemID) {
    return `${String(workspace ?? "").trim()}:${String(id ?? "").trim()}`;
  }

  function draftStorageKey(workspace = workspaceSlug, id = inboxItemID) {
    return `anx.human-response.draft:${workspace}:${id}`;
  }

  // A request from an agent for a grant. The panel then offers only the two
  // decisions core accepts, behind the same confirmation the Access page asks.
  let accessRequest = $derived(accessRequestFromInboxItem(item));
  /** How many suggestions have a number key, for the hint line. */
  let keyedProposalCount = $derived(
    accessRequest ? 0 : Math.min(MAX_KEYED_PROPOSALS, proposalStrings.length),
  );
  // Core accepts an access decision from a person only.
  let decidesAccess = $derived(isHumanWorkspacePrincipal($authenticatedAgent));

  function itemKind(value = item) {
    return String(value?.kind ?? value?.category ?? "unknown")
      .trim()
      .toLowerCase();
  }

  function kindLabel(value = item) {
    const kind = itemKind(value);
    return KIND_LABELS[kind] ?? (kind || "Inbox item");
  }

  function notificationStatus(value = item) {
    return value?.notification_target_status ?? {};
  }

  function actorName(id) {
    const raw = String(id ?? "").trim();
    if (!raw) return "";
    const label = actorDisplayLabel(raw, $actorRegistry, $principalRegistry);
    return label && label !== raw && label !== raw.replace(/^actor:/, "")
      ? label
      : "";
  }

  function requesterId(value = item) {
    return (
      String(value?.requester_actor_id ?? "").trim() ||
      String(value?.requester_agent_id ?? "").trim()
    );
  }

  function requesterName(value = item) {
    return (
      String(
        findAgentSummary(requesterId(value), $agentRegistry)?.display_name ??
          "",
      ).trim() ||
      String(value?.requester_label ?? "").trim() ||
      actorName(requesterId(value))
    );
  }

  function notifyTargetLabel() {
    if (notifyTargetSelected) {
      return notifyTargetSelected.display_name || notifyTargetSelected.id || "";
    }
    const actorID = String(notifyTargetActorID ?? "").trim();
    const agentID = String(notifyTargetAgentID ?? "").trim();
    return actorID || agentID || "";
  }

  function chooseNotifyTarget(actor) {
    notifyTargetSelected = actor;
    notifyTargetActorID = String(actor?.id ?? "");
    notifyTargetAgentID = "";
    notifyTargetQuery = "";
    notifyTargetResults = [];
    notifyTargetMenuOpen = false;
  }

  function clearNotifyTarget() {
    notifyTargetSelected = null;
    notifyTargetActorID = "";
    notifyTargetAgentID = "";
    notifyTargetQuery = "";
    notifyTargetResults = [];
  }

  function clearNotifyTargetSearchTimer() {
    if (notifyTargetSearchTimer) {
      clearTimeout(notifyTargetSearchTimer);
      notifyTargetSearchTimer = null;
    }
  }

  function resetRouteLocalState() {
    clearNotifyTargetSearchTimer();
    notifyTargetSearchSeq += 1;
    loadError = "";
    submitError = "";
    responseDraft = "";
    notifyMode = "original";
    notifyTargetActorID = "";
    notifyTargetAgentID = "";
    notifyTargetQuery = "";
    notifyTargetResults = [];
    notifyTargetSelected = null;
    notifyTargetMenuOpen = false;
    attachingResponseFile = false;
    pendingResponseAttachmentUpload = null;
    responseAttachmentError = "";
    responseAttachmentRefs = [];
    responseComposerArtifactsByRef = {};
    chosen = "";
    subject = null;
    context = null;
    contextLoading = false;
  }

  function handleNotifyTargetInput(event) {
    const value = event.currentTarget.value;
    notifyTargetQuery = value;
    notifyTargetMenuOpen = true;
    clearNotifyTargetSearchTimer();
    const needle = value.trim();
    if (!needle) {
      notifyTargetResults = [];
      return;
    }
    const seq = ++notifyTargetSearchSeq;
    notifyTargetSearchTimer = setTimeout(async () => {
      try {
        const results = await searchActors(needle, 8);
        if (seq !== notifyTargetSearchSeq) return;
        notifyTargetResults = Array.isArray(results) ? results : [];
      } catch {
        if (seq !== notifyTargetSearchSeq) return;
        notifyTargetResults = [];
      }
    }, 200);
  }

  function notifyDescription() {
    if (notifyMode === "none") return "No one will be notified";
    if (notifyMode === "target") {
      const label = notifyTargetLabel();
      return label ? `Notify ${label}` : "Notify someone else";
    }
    return `Notify ${requesterName() || "the requester"}`;
  }

  async function loadItem(
    workspace = workspaceSlug,
    id = inboxItemID,
    seq = ++inboxLoadSeq,
  ) {
    const routeKey = inboxRouteKey(workspace, id);
    loading = true;
    resetRouteLocalState();
    item = null;
    loadedInboxRouteKey = "";

    try {
      const response = await coreClient.getInboxItem(id);
      if (seq !== inboxLoadSeq || routeKey !== inboxRouteKey()) return;
      const loaded = response.item ?? null;
      if (!loaded) {
        loadError = "Inbox item not found.";
        return;
      }
      item = loaded;
      loadedInboxRouteKey = routeKey;
      notifyMode = defaultNotifyMode(loaded);
      if (browser) {
        const cached = localStorage.getItem(draftStorageKey(workspace, id));
        if (cached != null) responseDraft = cached;
      }
      applyRestore(takeInboxRestore(loaded.id));
      void loadContext(loaded, routeKey);
    } catch (error) {
      if (seq !== inboxLoadSeq || routeKey !== inboxRouteKey()) return;
      if (error?.status === 404) {
        loadError =
          "This item is no longer in the open inbox. It may already be handled.";
        return;
      }
      loadError =
        error instanceof Error
          ? `Failed to load inbox item: ${error.message}`
          : String(error);
    } finally {
      if (seq === inboxLoadSeq && routeKey === inboxRouteKey()) {
        loading = false;
      }
    }
  }

  /** Composer state an Undo hands back, so nothing typed or chosen is lost. */
  function applyRestore(state) {
    if (!state) return;
    responseDraft = String(state.draft ?? "");
    chosen = String(state.chosen ?? "");
    notifyMode = state.notifyMode || notifyMode;
    notifyTargetSelected = state.notifyTargetSelected ?? null;
    notifyTargetActorID = String(state.notifyTargetActorID ?? "");
    notifyTargetAgentID = String(state.notifyTargetAgentID ?? "");
    responseAttachmentRefs = Array.isArray(state.attachmentRefs)
      ? [...state.attachmentRefs]
      : [];
    responseComposerArtifactsByRef = { ...(state.artifactsByRef ?? {}) };
  }

  async function loadContext(loaded, routeKey) {
    let next = inboxItemSubject(loaded);
    subject = next;
    contextLoading = true;
    try {
      if (next?.kind === "card") {
        try {
          const response = await coreClient.getWork(next.ref);
          const task = response?.work ?? null;
          if (task) {
            next = inboxItemSubject(loaded, { work: [task] });
            if (routeKey === inboxRouteKey()) subject = next;
          }
        } catch {
          // The title from the item stands.
        }
      }
      const value = await loadInboxContext(loaded, next);
      if (routeKey === inboxRouteKey()) context = value;
    } finally {
      if (routeKey === inboxRouteKey()) contextLoading = false;
    }
  }

  function subjectHref(value) {
    const ref = String(value?.ref ?? "");
    if (value?.kind === "card")
      return workspaceHref(`/tasks/${encodeURIComponent(ref)}`);
    if (value?.kind === "document")
      return workspaceHref(
        `/docs/${encodeURIComponent(ref.slice("document:".length))}`,
      );
    return "";
  }

  const OPERATOR_SUBJECTS = new Set(["card", "document", "topic"]);
  let contextSubject = $derived.by(() => {
    // Threads and boards are not operator nouns; they get no subject line.
    if (!subject || !OPERATOR_SUBJECTS.has(subject.kind)) return null;
    if (subject.kind === "document") {
      const document = context?.document;
      return {
        title: String(document?.title || subject.title),
        status: document?.head_revision_number
          ? `v${document.head_revision_number}`
          : "",
      };
    }
    return {
      title: subject.title,
      status: subject.phase ? phaseLabel(subject.phase) : "",
    };
  });
  let contextRelation = $derived(
    itemKind(item) === "review"
      ? "Review of"
      : subject?.kind === "card" && !isCompleted
        ? "Blocks"
        : "On",
  );

  /**
   * The item a response belongs to, and the composer state that goes with it,
   * as they are right now.
   *
   * A suggested response sends after a short flash, and the reader can switch
   * to another item inside it. `loadItem` nulls `item` and resets the composer
   * before the next one arrives, so a send that read this page's state at that
   * moment answered nothing at all. The answer carries its own context
   * instead; see `sendContext` on the respond panel.
   */
  function responseContext() {
    return {
      itemId: String(item?.id ?? ""),
      item,
      href: $page.url.pathname,
      draft: responseDraft,
      proposals: proposalStrings,
      attachmentRefs: responseAttachmentRefs,
      artifactsByRef: responseComposerArtifactsByRef,
      notifyMode,
      notifyTargetSelected,
      notifyTargetActorID,
      notifyTargetAgentID,
      who:
        notifyMode === "target"
          ? notifyTargetLabel()
          : notifyMode === "none"
            ? ""
            : requesterName(),
    };
  }

  /**
   * Queues the response behind the undo toast and returns to the Inbox. The
   * committed request is this page's `inbox.respond` call, unchanged.
   */
  function submitResponseWithText(
    responseText,
    { acknowledge = false, outcome = "answered", context = null } = {},
  ) {
    const sending = context ?? responseContext();
    if (!sending.itemId) return;
    const text = String(responseText ?? "").trim();
    if (!text) {
      submitError = "Response text is required.";
      return;
    }
    const targetActorID = String(sending.notifyTargetActorID ?? "").trim();
    const targetAgentID = String(sending.notifyTargetAgentID ?? "").trim();
    if (
      !acknowledge &&
      sending.notifyMode === "target" &&
      !targetActorID &&
      !targetAgentID
    ) {
      submitError = "Replacement target requires an actor ID or agent ID.";
      return;
    }

    submitError = "";
    const request = acknowledge
      ? { response_text: text, outcome: "acknowledged", notify_mode: "none" }
      : {
          response_text: text,
          outcome,
          related_refs: sending.attachmentRefs,
          notify_mode: sending.notifyMode,
          notify_target_actor_id:
            sending.notifyMode === "target" && targetActorID
              ? targetActorID
              : undefined,
          notify_target_agent_id:
            sending.notifyMode === "target" && targetAgentID
              ? targetAgentID
              : undefined,
        };
    const proposal = sending.proposals.includes(text) ? text : "";
    queueInboxResponse({
      itemId: sending.itemId,
      // The item that was answered, which is not necessarily the one on
      // screen: the overlay files it under Handled, so it has to be the one
      // the reader chose on.
      item: sending.item,
      request,
      message: acknowledge
        ? "Acknowledged"
        : sending.who
          ? `Sent to ${sending.who}`
          : "Response recorded",
      restore: {
        origin: "item",
        href: sending.href,
        draft: acknowledge || proposal ? sending.draft : text,
        chosen: proposal,
        notifyMode: sending.notifyMode,
        notifyTargetSelected: sending.notifyTargetSelected,
        notifyTargetActorID: sending.notifyTargetActorID,
        notifyTargetAgentID: sending.notifyTargetAgentID,
        attachmentRefs: sending.attachmentRefs,
        artifactsByRef: sending.artifactsByRef,
      },
    });
    // Clearing the composer and leaving belong to the item that was answered.
    // When the reader has already moved on, this page is showing something
    // else and must be left exactly as they left it.
    if (sending.itemId !== String(item?.id ?? "")) return;
    if (browser) localStorage.removeItem(draftStorageKey());
    responseDraft = "";
    chosen = "";
    responseAttachmentRefs = [];
    responseComposerArtifactsByRef = {};
    responseAttachmentError = "";
    void goto(workspaceHref("/inbox"));
  }

  function undoLastResponse() {
    const entry = undoInboxResponse();
    if (!entry) return;
    const restore = entry.restore || {};
    if (restore.origin === "item" && entry.itemId === item?.id) {
      applyRestore(restore);
      return;
    }
    if (restore.origin === "item" && restore.href) {
      void goto(restore.href);
      return;
    }
    void goto(
      `${workspaceHref("/inbox")}?item=${encodeURIComponent(restore.rowId || "")}`,
    );
  }

  function handleKeydown(event) {
    const shortcut = inboxShortcutAction(event, {
      helpOpen,
      modalOpen: !helpOpen && otherDialogOpen(),
    });
    if (!shortcut) return;
    const root = document.querySelector("[data-inbox-item-page]");
    const find = (selector) => root?.querySelector(selector) || null;
    switch (shortcut.type) {
      case "help":
        helpOpen = true;
        break;
      case "close-help":
        helpOpen = false;
        break;
      case "undo":
        if (!hasPendingInboxResponse()) return;
        undoLastResponse();
        break;
      case "proposal": {
        if (!respondPanel?.pressProposalKey(shortcut.index)) return;
        break;
      }
      case "clear-choice": {
        // Escape that cleared nothing is not ours; let it keep travelling.
        if (!respondPanel?.clearProposalChoice()) return;
        break;
      }
      case "reply": {
        const field = find('[data-inbox-shortcut="reply"]');
        if (!field) return;
        field.focus();
        break;
      }
      case "done": {
        const control = find('[data-inbox-shortcut="done"]:not([disabled])');
        if (!control) return;
        control.click();
        break;
      }
      case "open": {
        const link = find('a[data-inbox-shortcut="open"]');
        if (!link) return;
        link.click();
        break;
      }
      default:
        return;
    }
    event.preventDefault();
  }

  async function handleAttachResponseFile(event) {
    const input = event.currentTarget;
    const file = input?.files?.[0];
    if (!file || attachingResponseFile) return;
    attachingResponseFile = true;
    pendingResponseAttachmentUpload = {
      original_filename: file.name || "attachment.bin",
      content_type: file.type || "application/octet-stream",
      size_bytes: file.size,
    };
    responseAttachmentError = "";
    try {
      const threadRef = String(item?.thread_id ?? "").trim()
        ? `thread:${String(item.thread_id).trim()}`
        : "";
      const refs =
        inboxRefs.length > 0 ? inboxRefs : [threadRef].filter(Boolean);
      if (refs.length === 0) {
        responseAttachmentError = "No valid refs are available for this item.";
        return;
      }
      const payload = await coreClient.createArtifactAttachment({ refs, file });
      const id = String(payload?.artifact?.id ?? "").trim();
      if (!id) {
        responseAttachmentError = "Upload succeeded but artifact id missing.";
        return;
      }
      const ref = `artifact:${id}`;
      responseAttachmentRefs = [...new Set([...responseAttachmentRefs, ref])];
      const row =
        payload?.artifact && typeof payload.artifact === "object"
          ? payload.artifact
          : null;
      if (row) {
        responseComposerArtifactsByRef = {
          ...responseComposerArtifactsByRef,
          [ref]: /** @type {Record<string, unknown>} */ (row),
        };
      }
    } catch (error) {
      responseAttachmentError = `Upload failed: ${error instanceof Error ? error.message : String(error)}`;
    } finally {
      attachingResponseFile = false;
      pendingResponseAttachmentUpload = null;
      if (input) input.value = "";
    }
  }

  onMount(() => {
    autosaveInterval = setInterval(() => {
      if (
        !browser ||
        !item ||
        isCompleted ||
        loadedInboxRouteKey !== inboxRouteKey()
      ) {
        return;
      }
      localStorage.setItem(draftStorageKey(), String(responseDraft ?? ""));
    }, 2000);
  });

  $effect(() => {
    const workspace = workspaceSlug;
    const id = inboxItemID;
    void loadItem(workspace, id);
  });

  onDestroy(() => {
    if (autosaveInterval != null) clearInterval(autosaveInterval);
    clearNotifyTargetSearchTimer();
  });
</script>

<svelte:window
  onkeydown={handleKeydown}
  onbeforeunload={(event) => {
    if (hasPendingInboxResponse()) {
      void flushInboxResponse();
      event.preventDefault();
      event.returnValue = "";
    }
  }}
/>

<div
  class="mx-auto max-w-3xl space-y-3 px-4 py-4 max-md:px-3 max-md:py-3"
  data-inbox-item-page
>
  <div class="flex items-center justify-between">
    <a
      class="text-meta text-fg-muted hover:text-fg max-md:text-micro"
      href={item
        ? `${workspaceHref("/inbox")}?${new URLSearchParams(
            isCompleted
              ? { mailbox: "handled", item: inboxItemMailboxId(item) }
              : { item: inboxItemMailboxId(item) },
          )}`
        : workspaceHref("/inbox")}
    >
      ← Back to inbox
    </a>
  </div>

  {#if loading}
    <div class="rounded border border-line bg-bg-soft p-4">
      <Skeleton rows={4} />
    </div>
  {:else if loadError && !item}
    <StateError
      message={loadError}
      onretry={() => void loadItem()}
      retrying={loading}
    />
  {:else if item}
    <section class="space-y-4 max-md:space-y-3">
      <header class="space-y-2 max-md:space-y-1.5">
        <div
          class="flex flex-wrap items-center gap-x-2 gap-y-1 text-micro text-fg-muted"
        >
          <span class="ui-label mb-0">{kindLabel(item).toUpperCase()}</span>
          {#if item.severity}
            <span
              class="ui-badge {String(item.severity).toLowerCase() ===
              'critical'
                ? 'ui-badge--danger'
                : 'ui-badge--warn'}">{sentenceCase(item.severity)}</span
            >
          {/if}
          {#if !isReminder}
            <!-- A reminder has no requester. `InboxActorName` renders the word
                 "someone" for an empty one, which asserted a person who is not
                 waiting on anything. -->
            <span class="min-w-0 [overflow-wrap:anywhere]">
              from <InboxActorName name={requesterName()} id={requesterId()} />
            </span>
          {/if}
        </div>
        <!--
          Subjects are written by the asking agent and routinely carry a bare
          64-char correlation id, a ref or a URL — nothing for the browser to
          break on. Without this the title escapes the column and scrolls the
          page sideways at phone width. Matches the inbox list pane, whose
          detail headings were hardened the same way.
        -->
        <h1
          class="text-subtitle font-semibold leading-tight text-fg [overflow-wrap:anywhere]"
        >
          {item.title}
        </h1>
        <InboxContextStrip
          relation={contextRelation}
          subject={contextSubject}
          subjectHref={subjectHref(subject)}
          note={context?.note || null}
          noteAuthor={context?.note
            ? actorName(context.note.actorId) ||
              (context.note.byRequester ? requesterName() : "")
            : ""}
          loading={contextLoading && !context}
          presenceActorId={requesterId()}
        />
        {#if item.body}
          <div
            class="rounded-md border border-line bg-panel px-3 py-2 text-meta leading-relaxed text-fg"
          >
            <MarkdownRenderer source={item.body} />
          </div>
        {/if}
        {#if inboxRefs.length > 0}
          <div
            class="flex flex-wrap items-center gap-2 text-micro max-md:gap-x-2 max-md:gap-y-1"
          >
            {#each inboxRefs.slice(0, 4) as refValue}
              <RefLink
                {refValue}
                threadId={item.thread_id}
                humanize
                artifactRoutesById={inboxComposerArtifactRoutes}
              />
            {/each}
            {#if inboxRefs.length > 4}
              <span class="text-fg-muted">+{inboxRefs.length - 4} more</span>
            {/if}
          </div>
        {/if}
      </header>

      {#if isCompleted}
        <div
          class="space-y-3 rounded-md border border-line bg-bg-soft px-4 py-3 text-meta text-fg"
          data-testid="inbox-completed-detail"
        >
          {#if item.original_request_missing}
            <p class="text-micro text-warn-text">
              Original request details are unavailable for this entry.
            </p>
          {/if}
          <p class="text-micro text-fg-muted [overflow-wrap:anywhere]">
            {#if item.responding_actor_id}Answered by <InboxActorName
                name={actorName(item.responding_actor_id)}
                id={item.responding_actor_id}
              />{:else}Answered{/if}{#if item.responded_at}{" "}{formatAbsoluteDateTime(
                item.responded_at,
              )}{/if}
          </p>
          <div>
            <div
              class="text-micro font-medium uppercase tracking-wide text-fg-muted"
            >
              Final response
            </div>
            <MarkdownRenderer
              source={item.response_text ?? ""}
              class="mt-1 text-meta text-fg [overflow-wrap:anywhere]"
            />
          </div>
          <div class="flex flex-wrap gap-2 pt-1">
            {#if completedTimelineHref()}
              <Button
                variant="secondary"
                size="compact"
                href={completedTimelineHref()}
              >
                Timeline event
              </Button>
            {/if}
            <Button
              variant="secondary"
              size="compact"
              href={`${workspaceHref("/inbox")}?mailbox=handled`}
            >
              View Handled
            </Button>
          </div>
        </div>
      {:else if isReminder}
        <!--
          A review reminder has no requester and no response. Core drops it
          from every inbox read once the report is revised, archived, trashed
          or unpinned, so refreshing the panel is what closes it.
        -->
        <div
          class="space-y-3 rounded-md border border-line bg-bg-soft px-4 py-3 text-meta text-fg"
          data-testid="inbox-reminder-detail"
        >
          <p class="text-fg-muted">
            This closes itself when the panel is refreshed or converted to a
            live one. There is nothing to answer.
          </p>
          {#if reminderHref}
            <!-- A real link, as the Inbox pane renders it: this is navigation,
                 and it should open in a new tab like any other. -->
            <a class="ui-btn-primary inline-flex" href={reminderHref}
              >Open dashboard</a
            >
          {/if}
        </div>
      {:else}
        {#if responseError}
          <div
            class="mb-3 rounded-md border border-danger bg-danger-soft px-3 py-2"
            role="alert"
            data-inbox-response-error
          >
            <p class="text-meta text-fg">
              <span class="font-medium text-danger-text"
                >Your response was not sent.</span
              >
              {responseError}
            </p>
            <p class="mt-1.5 flex flex-wrap items-center gap-3">
              <button
                class="ui-btn-secondary"
                type="button"
                onclick={() => void retryInboxResponse(item.id)}
                >Retry send</button
              >
              <button
                class="text-micro text-fg-muted hover:text-fg"
                type="button"
                onclick={() => dismissInboxResponseFailure(item.id)}
                >Answer it again instead</button
              >
            </p>
          </div>
        {/if}
        <InboxRespondPanel
          bind:this={respondPanel}
          kind={itemKind(item)}
          access={accessRequest}
          canDecideAccess={decidesAccess}
          proposals={proposalStrings}
          itemKey={String(item?.id ?? "")}
          bind:draft={responseDraft}
          {chosen}
          replyId="human-response-input"
          replyLabel="Your response"
          placeholder="Write the response the agent should rely on."
          tall
          sendLabel="Send response"
          sendContext={responseContext}
          onSend={(text, outcome, context) =>
            submitResponseWithText(text, { outcome, context })}
          onAcknowledge={accessRequest
            ? null
            : () =>
                submitResponseWithText("Acknowledged from inbox", {
                  acknowledge: true,
                })}
        >
          {#snippet after()}
            <span class="ml-auto hidden text-micro text-fg-subtle sm:inline">
              {#if keyedProposalCount}{keyedProposalCount > 1
                  ? `1–${keyedProposalCount}`
                  : "1"} select, press again to send ·
              {/if}{formatShortcut("Enter")} to send · ? for shortcuts</span
            >
          {/snippet}
          {#snippet extras()}
            <div class="space-y-2">
              <div class="flex flex-wrap items-center gap-2">
                <label
                  class="inline-flex cursor-pointer items-center rounded border border-line bg-panel px-3 py-1.5 text-micro font-medium text-fg hover:bg-bg-soft"
                >
                  {attachingResponseFile ? "Uploading…" : "Attach file"}
                  <input
                    class="sr-only"
                    accept="image/*,text/plain,text/markdown,text/csv,.md,.txt,.csv,.json,.pdf"
                    disabled={attachingResponseFile}
                    onchange={handleAttachResponseFile}
                    type="file"
                  />
                </label>
                {#if pendingResponseAttachmentUpload}
                  {@const pendingResolved = resolveRefLink(
                    "artifact:upload-pending",
                    {
                      threadId: String(item?.thread_id ?? "").trim(),
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
                    artifactOverlay={pendingResponseAttachmentUpload}
                    pending
                    size="compact"
                  />
                {/if}
                {#each responseAttachmentRefs as ref (ref)}
                  {@const composerResolved = resolveRefLink(ref, {
                    threadId: String(item?.thread_id ?? "").trim(),
                    boardId: "",
                    humanize: true,
                    artifactRoutesById: inboxComposerArtifactRoutes,
                    eventRoutesById: {},
                    workspaceSlug,
                    organizationSlug,
                  })}
                  <span class="inline-flex max-w-full items-center gap-1">
                    <AttachmentChip
                      resolved={composerResolved}
                      size="compact"
                    />
                    <button
                      class="shrink-0 text-fg-muted hover:text-fg"
                      type="button"
                      aria-label={`Remove ${ref}`}
                      onclick={() => {
                        responseAttachmentRefs = responseAttachmentRefs.filter(
                          (candidate) => candidate !== ref,
                        );
                        const next = { ...responseComposerArtifactsByRef };
                        delete next[ref];
                        responseComposerArtifactsByRef = next;
                      }}
                    >
                      ×
                    </button>
                  </span>
                {/each}
              </div>
              {#if responseAttachmentError}
                <p class="text-micro text-danger-text">
                  {responseAttachmentError}
                </p>
              {/if}
            </div>

            <div
              class="rounded border border-line bg-bg-soft px-3 py-2 text-meta max-md:space-y-2"
            >
              <div
                class="flex flex-wrap items-center gap-x-3 gap-y-2 max-md:block"
              >
                <span
                  class="min-w-0 text-fg-muted [overflow-wrap:anywhere] max-md:block max-md:text-micro"
                  >{notifyDescription()}</span
                >
                <div
                  class="ml-auto flex flex-wrap items-center gap-1 max-md:mt-2 max-md:grid max-md:grid-cols-3 max-md:rounded-md max-md:border max-md:border-line max-md:bg-panel max-md:p-1"
                >
                  <button
                    class="rounded px-2 py-1 text-micro font-medium max-md:py-1.5 {notifyMode ===
                    'original'
                      ? 'bg-accent-soft text-accent'
                      : 'text-fg-muted hover:text-fg'}"
                    type="button"
                    disabled={notificationStatus().resolvable === false}
                    onclick={() => {
                      notifyMode = "original";
                      clearNotifyTarget();
                    }}
                  >
                    Original requester
                  </button>
                  <button
                    class="rounded px-2 py-1 text-micro font-medium max-md:py-1.5 {notifyMode ===
                    'target'
                      ? 'bg-accent-soft text-accent'
                      : 'text-fg-muted hover:text-fg'}"
                    type="button"
                    onclick={() => {
                      notifyMode = "target";
                      notifyTargetMenuOpen = true;
                    }}
                  >
                    Someone else
                  </button>
                  <button
                    class="rounded px-2 py-1 text-micro font-medium max-md:py-1.5 {notifyMode ===
                    'none'
                      ? 'bg-accent-soft text-accent'
                      : 'text-fg-muted hover:text-fg'}"
                    type="button"
                    onclick={() => {
                      notifyMode = "none";
                      clearNotifyTarget();
                    }}
                  >
                    No one
                  </button>
                </div>
              </div>
              {#if notifyMode === "target"}
                <div class="relative mt-2">
                  {#if notifyTargetSelected}
                    <div
                      class="flex items-center gap-2 rounded border border-line bg-panel px-2 py-1.5"
                    >
                      <span
                        class="inline-flex min-w-0 items-center gap-1.5 rounded bg-accent-soft px-2 py-0.5 text-micro text-accent [overflow-wrap:anywhere]"
                      >
                        @{notifyTargetSelected.display_name ||
                          notifyTargetSelected.id}
                      </span>
                      <button
                        class="ml-auto text-micro text-fg-muted hover:text-fg"
                        type="button"
                        onclick={clearNotifyTarget}
                      >
                        Clear
                      </button>
                    </div>
                  {:else}
                    <input
                      class="w-full rounded border border-line bg-panel px-2 py-1.5 text-meta text-fg outline-none placeholder:text-fg-muted focus:ring-2 focus:ring-accent"
                      type="text"
                      placeholder="Search people or agents…"
                      role="combobox"
                      aria-label="Notify someone else"
                      aria-controls={notifyTargetPopupOpen
                        ? "notify-target-results"
                        : undefined}
                      aria-expanded={notifyTargetPopupOpen}
                      aria-autocomplete="list"
                      value={notifyTargetQuery}
                      oninput={handleNotifyTargetInput}
                      onfocus={() => (notifyTargetMenuOpen = true)}
                    />
                    {#if notifyTargetPopupOpen}
                      <!-- A results list that floats over the composer: mark it
                         as a listbox so it reads (and is dismissed) as a
                         popup rather than as chrome covering Send response. -->
                      <div
                        id="notify-target-results"
                        role="listbox"
                        aria-label="Notification targets"
                        class="absolute left-0 right-0 top-full z-10 mt-1 max-h-56 overflow-y-auto rounded border border-line bg-panel shadow-lg"
                        use:dismissOnEscape={{
                          enabled: true,
                          onDismiss: () => (notifyTargetMenuOpen = false),
                        }}
                      >
                        {#each notifyTargetResults as actor (actor.id)}
                          <button
                            class="flex w-full items-center gap-2 px-3 py-2 text-left text-meta hover:bg-bg-soft"
                            type="button"
                            role="option"
                            aria-selected={false}
                            onclick={() => chooseNotifyTarget(actor)}
                          >
                            <span
                              class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-accent-soft text-micro font-semibold text-accent"
                            >
                              {(actor.display_name || actor.id || "?")
                                .slice(0, 1)
                                .toUpperCase()}
                            </span>
                            <span class="min-w-0 flex-1">
                              <span class="block truncate text-fg"
                                >{actor.display_name || actor.id}</span
                              >
                              <span
                                class="block truncate font-mono text-micro text-fg-muted"
                                >{actor.id}</span
                              >
                            </span>
                          </button>
                        {/each}
                      </div>
                    {/if}
                  {/if}
                </div>
              {/if}
            </div>

            {#if submitError}
              <div
                class="rounded border border-danger bg-danger-soft px-3 py-2 text-meta text-danger-text"
                role="alert"
              >
                {submitError}
              </div>
            {/if}
          {/snippet}
        </InboxRespondPanel>
      {/if}
    </section>
  {/if}
  <KeyboardShortcutsDialog
    bind:open={helpOpen}
    shortcuts={inboxShortcutList().filter(
      ([action]) => !/^(Next|Previous) item$/.test(action),
    )}
    title="Inbox shortcuts"
  />
  <InboxUndoToast onUndo={undoLastResponse} />
</div>
