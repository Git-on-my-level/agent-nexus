<script>
  import { humanActorIdSet } from "$lib/humanActors.js";
  import { onMount, tick, untrack } from "svelte";
  import { page } from "$app/stores";
  import { beforeNavigate, goto } from "$app/navigation";
  import { coreClient, createInboxSourceClient } from "$lib/coreClient";
  import {
    actorDisplayLabel,
    actorRegistry,
    agentRegistry,
    findAgentSummary,
    principalRegistry,
    selectedActorId,
  } from "$lib/actorSession";
  import {
    authenticatedAgent,
    isHumanWorkspacePrincipal,
  } from "$lib/authSession";
  import { restartSession } from "$lib/workspaceBootstrap";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { formatAbsoluteDateTime, formatTimestamp } from "$lib/formatDate";
  import Time from "$lib/time/Time.svelte";
  import {
    errorMessage,
    humanizeInstants,
    isNexusOwned,
    isSessionExpired,
    readErrorExplanation,
    sentenceCase,
    taskDetailPath,
    workKey,
  } from "$lib/pm/presentation.js";
  import { navIconPath } from "$lib/icons.js";
  import { openCommandPalette } from "$lib/stores/commandPalette.js";
  import { INBOX_CATEGORY_LABELS, splitTypedRef } from "$lib/inboxUtils.js";
  import {
    INBOX_MAILBOXES,
    buildInboxRows,
    filterMailbox,
    formatWait,
    inboxItemIsReminder,
    inboxItemNeedsResponse,
    inboxRowBadge,
    rowMatchesWorkRef,
    rowWaitMs,
  } from "$lib/inboxMailbox.js";
  import {
    describeUpdateEvent,
    eventPhrase,
    eventPhraseParts,
  } from "$lib/inboxDigest.js";
  import {
    invalidateInboxContext,
    loadInboxContext,
  } from "$lib/inboxContext.js";
  import { invalidateAskDetail, loadAskDetail } from "$lib/askDetail.js";
  import {
    askDeliveryModel,
    askEvidenceModel,
    askRefForInboxItem,
    linkifyAskEvidence,
    NEEDS_CONTEXT_OUTCOME,
    supportsNeedsContext,
  } from "$lib/askDelivery.js";
  import {
    liveWorkspaceEvents,
    liveInboxChanges,
  } from "$lib/liveWorkspaceEvents.js";
  import {
    inboxNeedsYouCount,
    claimInboxCount,
    publishInboxCount,
  } from "$lib/inboxCount.js";
  import {
    applyResponseOverlay,
    captureInboxResponseBinding,
    defaultNotifyMode,
    flushInboxResponse,
    dismissInboxResponseFailure,
    hasPendingInboxResponse,
    inboxResponseFailures,
    inboxResponseOverlay,
    retryInboxResponse,
    onInboxResponseCommitted,
    queueInboxResponse,
    stashInboxRestore,
    undoInboxResponse,
  } from "$lib/inboxResponseQueue.js";
  import {
    loadInboxSources,
    mergeInboxItems,
    mergeInboxSnapshot,
    hasCompleteInboxHistory,
  } from "$lib/inboxSources.js";
  import { createInboxOrder } from "$lib/inboxOrder.js";
  import { MAX_KEYED_PROPOSALS } from "$lib/inboxProposalChoice.js";
  import {
    inboxShortcutAction,
    inboxShortcutList,
    otherDialogOpen,
  } from "$lib/inboxShortcuts.js";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import SkeletonInboxRow from "$lib/components/state/SkeletonInboxRow.svelte";
  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
  import { pmFeaturesVisible } from "$lib/pm/onboardingState.js";
  import { pmPresence } from "$lib/pm/presence.js";
  import DecisionPanel from "$lib/components/pm/DecisionPanel.svelte";
  import KeyboardShortcutsDialog from "$lib/components/KeyboardShortcutsDialog.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import InboxActorName from "$lib/components/inbox/InboxActorName.svelte";
  import InboxContextStrip from "$lib/components/inbox/InboxContextStrip.svelte";
  import InboxDelivery from "$lib/components/inbox/InboxDelivery.svelte";
  import InboxDocPanel from "$lib/components/inbox/InboxDocPanel.svelte";
  import InboxEvidence from "$lib/components/inbox/InboxEvidence.svelte";
  import InboxRespondPanel from "$lib/components/inbox/InboxRespondPanel.svelte";
  import InboxUndoToast from "$lib/components/inbox/InboxUndoToast.svelte";

  import { readerScope, readerScopeKey } from "$lib/readerScope.js";
  import {
    readWorkspaceView,
    writeWorkspaceView,
    workspaceViewRevision,
    onWorkspaceViewsDenied,
    onWorkspaceViewChanged,
  } from "$lib/workspaceViewCache.js";
  import {
    reliableRead,
    isTransientReadError,
    handleReadAccessDenied,
    isReadAccessDenied,
  } from "$lib/reliableRead.js";
  import { commitInboxView } from "$lib/inboxViewCache.js";

  let reconnecting = $state(false);
  let confirmed = $state(false);
  let decisions = $state([]);
  let actions = $state([]);
  let work = $state([]);
  let inboxItems = $state([]);
  let openInboxItems = $state([]);
  let completedInboxItems = $state([]);
  let updates = $state([]);
  let loading = $state(true);
  let busy = $state(false);
  let busyWith = $state("");
  let error = $state("");
  let actionError = $state("");
  let notice = $state("");
  let answer = $state("");
  let choice = $state("");
  let reply = $state("");
  let chosen = $state("");
  // Core accepts an access decision from a person only.
  let decidesAccess = $derived(isHumanWorkspacePrincipal($authenticatedAgent));
  let helpOpen = $state(false);
  let requestId = 0;
  let loadController;
  let selectionRequest = 0;
  let selectedController;
  const archivedWorkRefs = new Set();
  let decisionResolvedFor = $state("");
  let staleExpanded = $state(false);
  let ready = $state(false);
  let truncated = $state(false);
  let streamPartial = $state(false);
  let receiptsUnavailable = $state(false);
  let noticeElement = $state(null);
  let detailPane = $state(null);
  /*
   * The respond panel owns which suggestion is highlighted, so the number keys
   * go through it rather than clicking a button: a click sends, where the first
   * press of a number only selects.
   */
  let respondPanel = $state(null);
  // After an action, keyboard focus lands on the outcome, not on <body>.
  $effect(() => {
    if (notice && noticeElement) noticeElement.focus();
  });
  let now = $state(Date.now());

  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  /*
   * Gates the PM affordances and wording only, never a read: a proposal filed
   * earlier still waits for a yes whether or not a PM is running now, and the
   * Inbox is the only place to answer it.
   */
  let pmState = $derived(
    $pmPresence.workspace === ($page.data?.workspace?.slug ?? "")
      ? $pmPresence
      : null,
  );
  let pmVisible = $derived(pmFeaturesVisible(pmState));
  let mailbox = $derived.by(() => {
    const value = $page.url.searchParams.get("mailbox") || "";
    return INBOX_MAILBOXES.some(([key]) => key === value) ? value : "needs-you";
  });
  let urlItem = $derived($page.url.searchParams.get("item") || "");
  // "Inbox for this task" links narrow the list to one task.
  let workRef = $derived(
    String($page.url.searchParams.get("work_ref") || "").trim(),
  );
  // Holds the answered row until goto can pin it in the URL. Without this,
  // updating `decisions` moves the row out of the current mailbox and the
  // pane goes empty, which clears the notice.
  let heldItem = $state("");
  // The row the reader chose (by link, key or URL). Below lg the list and
  // the detail are separate screens; only an explicit choice opens detail.
  let explicitId = $derived(urlItem || heldItem);

  function actorName(id) {
    const raw = String(id ?? "").trim();
    if (!raw) return "";
    const label = actorDisplayLabel(raw, $actorRegistry, $principalRegistry);
    // An id that resolves to itself is not a name.
    return label && label !== raw && label !== raw.replace(/^actor:/, "")
      ? label
      : "";
  }

  let rows = $derived(
    buildInboxRows({
      decisions,
      actions,
      receiptsUnavailable,
      work,
      inboxItems: applyResponseOverlay(
        inboxItems,
        $inboxResponseOverlay,
        now,
        $inboxResponseFailures,
      ),
      updates,
      now,
      currentActorId: $selectedActorId || "",
      humanIds: humanActorIdSet($actorRegistry, $principalRegistry),
      actorName,
      agentName: (id) => findAgentSummary(id, $agentRegistry)?.display_name,
    }),
  );
  let scoped = $derived(
    workRef ? rows.filter((row) => rowMatchesWorkRef(row, workRef)) : rows,
  );
  let mailboxRows = $derived(filterMailbox(scoped, mailbox));
  const orderInboxRows = createInboxOrder();
  let presentation = $derived(
    ready
      ? orderInboxRows(`${mailbox}:${workRef}`, mailboxRows, staleExpanded)
      : { currentRows: [], staleRows: [], lateRows: [], added: 0 },
  );
  let staleRows = $derived(presentation.staleRows);
  let currentRows = $derived(presentation.currentRows);
  let lateRows = $derived(presentation.lateRows);
  let visible = $derived([
    ...currentRows,
    ...(staleExpanded ? staleRows : []),
    ...lateRows,
  ]);
  // A direct link remains visible even when its task normally lives folded.
  $effect(() => {
    if (explicitId && staleRows.some((row) => row.id === explicitId))
      staleExpanded = true;
  });
  let handled = $derived(filterMailbox(scoped, "handled"));
  let counts = $derived({
    "needs-you": Math.max(
      filterMailbox(scoped, "needs-you").length,
      !workRef &&
        !confirmed &&
        $inboxNeedsYouCount.workspace === $page.params.workspace
        ? $inboxNeedsYouCount.count || 0
        : 0,
    ),
    watching: filterMailbox(scoped, "watching").length,
    handled: handled.length,
  });
  /*
   * When the reader last finished something, for the empty state. It is the
   * newest row already in the handled mailbox, so it costs no request; an
   * inbox with nothing handled yet simply says nothing.
   */
  let lastHandledAt = $derived.by(() => {
    const times = handled
      // When it was answered, not when it was asked: a row's `time` is the
      // source event, so a month-old question answered this morning would
      // have read "last handled 30d ago".
      .map((row) =>
        Date.parse(
          row.item?.responded_at || row.item?.completed_at || row.time || "",
        ),
      )
      .filter((value) => Number.isFinite(value));
    return times.length ? new Date(Math.max(...times)).toISOString() : "";
  });
  let workRefTitle = $derived.by(() => {
    if (!workRef) return "";
    const task = work.find(
      (item) => workKey(item) === workRef || item?.handle === workRef,
    );
    return String(task?.title || "").trim() || "this task";
  });

  // Without an explicit choice the pane shows the first row and stays on it
  // while the list refreshes underneath, so a live update never swaps the
  // item the reader is typing a reply to.
  let stickyId = $state("");
  let stickyMailbox = $state("");
  $effect(() => {
    const list = visible;
    const box = mailbox;
    if (!ready) return;
    untrack(() => {
      const keep =
        box === stickyMailbox &&
        stickyId &&
        list.some((row) => row.id === stickyId);
      if (!keep) stickyId = list[0]?.id || "";
      stickyMailbox = box;
    });
  });
  let selectedId = $derived(explicitId || stickyId);
  let selected = $derived(
    selectedId ? rows.find((row) => row.id === selectedId) || null : null,
  );
  let selectedDecision = $derived(
    selected?.kind === "decision" ? selected.item : null,
  );
  let selectedWork = $derived(
    selectedDecision?.work_ref
      ? work.find((item) => workKey(item) === selectedDecision.work_ref) || null
      : null,
  );
  let selectedTaskTitle = $derived(String(selectedWork?.title || ""));
  let action = $derived(
    actions.find(
      (item) =>
        item.id === selectedDecision?.action_id ||
        item.decision_id === selectedDecision?.id,
    ),
  );
  let selectedIndex = $derived(
    visible.findIndex((item) => item.id === selected?.id),
  );

  // Restores a draft after Undo, once the restored row is selected again.
  let restoreDraft = null;
  let previousSelectedId = $state("");
  $effect(() => {
    const id = selected?.id || "";
    if (id === previousSelectedId) return;
    previousSelectedId = id;
    untrack(() => {
      answer = "";
      choice = "";
      reply = "";
      chosen = "";
      notice = "";
      supersededHref = "";
      if (restoreDraft && restoreDraft.id === id) {
        reply = restoreDraft.reply;
        chosen = restoreDraft.chosen;
        restoreDraft = null;
      }
    });
  });
  $effect(() => {
    const itemId = selectedId;
    if (!ready || !itemId?.startsWith("decision:")) return;
    const id = itemId.slice("decision:".length);
    if (id) {
      void untrack(() => loadSelected(id));
      return () => {
        selectionRequest++;
        selectedController?.abort();
      };
    }
  });

  // Context strip: what the selected item blocks and the latest note.
  let context = $state(null);
  let contextFor = $state("");
  let contextLoading = $state(false);
  let contextEpoch = $state(0);
  $effect(() => {
    const id = selectedId;
    void contextEpoch;
    const row = untrack(() => selected);
    if (!row || row.kind !== "inbox") {
      context = null;
      contextFor = "";
      contextLoading = false;
      return;
    }
    if (contextFor !== id) {
      context = null;
      contextFor = id;
    }
    contextLoading = true;
    void loadInboxContext(row.item, row.subject).then(
      (value) => {
        if (contextFor !== id) return;
        context = value;
        contextLoading = false;
      },
      () => {
        if (contextFor === id) contextLoading = false;
      },
    );
  });
  /*
   * The ask behind the selected item: its durable outcome (status, staleness,
   * the task decision, delivery rows) and its own event, which is where the
   * authoring evidence lives. Two bounded point reads for the one item the
   * reader selected — never one per row, so a page of Handled items costs the
   * page rather than its rows.
   */
  let askDetail = $state(null);
  let askDetailFor = $state("");
  let askDetailLoading = $state(false);
  /*
   * Tracked, not untracked: a `?item=` deep link selects a row before the first
   * page of rows has arrived, so the first run has no row to read. The ask ref
   * is what has to be a dependency — `selectedId` does not change when the row
   * behind it finally loads, and an effect that only watched it never issued
   * the read at all. This is the Overview's own link into an ask.
   */
  let selectedAskRef = $derived(
    selected?.kind === "inbox" ? askRefForInboxItem(selected.item) : "",
  );
  $effect(() => {
    const askRef = selectedAskRef;
    const id = selectedId;
    void contextEpoch;
    if (!askRef) {
      askDetail = null;
      askDetailFor = "";
      askDetailLoading = false;
      return;
    }
    const item = untrack(() => selected?.item);
    if (askDetailFor !== id) {
      askDetail = null;
      askDetailFor = id;
    }
    askDetailLoading = true;
    void loadAskDetail(item).then(
      (value) => {
        if (askDetailFor !== id) return;
        askDetail = value;
        askDetailLoading = false;
      },
      () => {
        if (askDetailFor === id) askDetailLoading = false;
      },
    );
  });
  let askEvidence = $derived(
    selected?.kind === "inbox"
      ? askEvidenceModel({
          item: selected.item,
          event: askDetail?.event,
          subjectRef: selected.subject?.ref ?? "",
        })
      : null,
  );
  let askDelivery = $derived(
    askDetail?.outcome
      ? askDeliveryModel(askDetail.outcome, {
          now,
          nextActorLabel: actorName(askDetail.outcome.task_outcome?.next_actor),
        })
      : null,
  );
  /*
   * The question, with every name the ask itself backed with evidence turned
   * into a link. Nothing is guessed: a name only becomes a link when the ask
   * carries a matching ref or pull-request URL.
   */
  let askBody = $derived(
    selected?.kind === "inbox" && askEvidence && !askEvidence.empty
      ? linkifyAskEvidence(selected.body, askEvidence, { hrefFor: refHref })
      : selected?.body || "",
  );
  /* The ask this one replaces, when the agent re-asked after a context request. */
  let supersedesLink = $derived.by(() => {
    const ref = askEvidence?.supersedes || "";
    if (!ref.startsWith("event:")) return null;
    const earlier = rows.find(
      (row) => row.kind === "inbox" && askRefForInboxItem(row.item) === ref,
    );
    // The event search matches a bare id; `event:<id>` is a ref, not an id, and
    // matched nothing.
    const eventId = ref.slice("event:".length);
    return {
      ref,
      href: earlier
        ? href({ item: earlier.id })
        : `${workspaceHref("/events")}?q=${encodeURIComponent(eventId)}`,
      label: earlier?.title || ref,
    };
  });
  /* An answered item that was sent back for context rather than answered. */
  let selectedSentBack = $derived(
    String(selected?.item?.outcome ?? "") === NEEDS_CONTEXT_OUTCOME,
  );
  /** The evidence document open in the side panel, or "". */
  let docPanelRef = $state("");
  /*
   * Titles the side panel has already read. A document is never in `work`, so
   * without this its evidence row shows a bare id; the panel reads the title
   * anyway, and handing it back costs no request.
   *
   * A title is a read, so it belongs to the reader and workspace it was read
   * for and is dropped with them. Keeping it would label a document for a
   * reader who cannot open it — the same scope the ask cache keeps.
   */
  let docTitles = $state({});
  $effect(() => {
    // A document opened as one item's evidence is not the next item's, and a
    // title read as one reader is not another reader's to see.
    void selectedId;
    void $page.params.workspace;
    void $selectedActorId;
    untrack(() => {
      docPanelRef = "";
      docTitles = {};
    });
  });

  const OPERATOR_SUBJECTS = new Set(["card", "document", "topic"]);
  let contextSubject = $derived.by(() => {
    const subject = selected?.kind === "inbox" ? selected.subject : null;
    // Threads and boards are not operator nouns; they get no subject line.
    if (!subject || !OPERATOR_SUBJECTS.has(subject.kind)) return null;
    const document = context?.document;
    if (subject.kind === "document") {
      return {
        title: String(document?.title || subject.title),
        status: document?.head_revision_number
          ? `v${document.head_revision_number}`
          : "",
      };
    }
    return { title: subject.title, summary: subject.summary };
  });
  let contextRelation = $derived.by(() => {
    const kind = String(selected?.category ?? "").toLowerCase();
    const subjectKind = selected?.subject?.kind;
    if (kind === "review") return "Review of";
    if (subjectKind === "card" && inboxItemNeedsResponse(selected?.item))
      return "Blocks";
    return "On";
  });

  function subjectHref(subject) {
    return refHref(subject?.ref);
  }

  /** Where a native ref lives in this workspace, or "" when it has no page. */
  function refHref(value) {
    const { prefix, id } = splitTypedRef(value);
    if (!prefix || !id) return "";
    if (prefix === "card")
      return workspaceHref(`/tasks/${encodeURIComponent(`${prefix}:${id}`)}`);
    if (prefix === "document")
      return workspaceHref(`/docs/${encodeURIComponent(id)}`);
    // Projects and boards have no operator page; the ref still renders, as text.
    return "";
  }

  function href(changes) {
    const params = new URLSearchParams($page.url.searchParams);
    for (const [key, value] of Object.entries(changes)) {
      if (value) params.set(key, value);
      else params.delete(key);
    }
    const query = params.toString();
    return query
      ? `${workspaceHref("/inbox")}?${query}`
      : workspaceHref("/inbox");
  }

  async function loadSelected(id) {
    const scope = readerScopeKey();
    const ticket = ++selectionRequest;
    selectedController?.abort();
    const controller = new AbortController();
    selectedController = controller;
    const client = createInboxSourceClient(controller.signal);
    const timer = setTimeout(
      () => controller.abort(new Error("Requested item loading timed out")),
      5_000,
    );
    try {
      let item = decisions.find((entry) => entry.id === id);
      if (!item) {
        item = await reliableRead(() => client.getPmDecision(id), {
          signal: controller.signal,
          cacheScope: scope,
        });
        if (ticket !== selectionRequest || archivedWorkRefs.has(item.work_ref))
          return;
        decisions = [...decisions.filter((entry) => entry.id !== id), item];
      }
      if (
        item?.action_id &&
        !actions.some((entry) => entry.id === item.action_id)
      ) {
        const receipt = await reliableRead(
          () => client.getPmAction(item.action_id),
          {
            signal: controller.signal,
            cacheScope: scope,
          },
        );
        if (ticket !== selectionRequest || archivedWorkRefs.has(item.work_ref))
          return;
        actions = [
          ...actions.filter((entry) => entry.id !== receipt.id),
          receipt,
        ];
      }
    } catch (err) {
      if (ticket === selectionRequest) actionError = errorMessage(err);
    } finally {
      clearTimeout(timer);
      if (ticket === selectionRequest) decisionResolvedFor = `decision:${id}`;
    }
  }

  beforeNavigate(({ cancel, type }) => {
    // recordAnswer pins the answered row with goto() while busy is still
    // true; cancelling that navigation drops the Approved/Declined notice.
    if (busy && type !== "goto") {
      cancel();
      return;
    }
    // Full-page unloads are covered by onbeforeunload; a confirm() here would
    // be blocked by the browser during beforeunload anyway.
    if (type === "leave") return;
    if (
      answer.trim() &&
      !busy &&
      !window.confirm("Leave without recording this decision?")
    )
      cancel();
  });

  /**
   * Loads every source. A quiet load (live update, a committed response)
   * keeps the list on screen and never clears an error the reader has not
   * seen resolved.
   */
  let refreshPending = $state(false);
  let loadErrorText = $state("");
  async function load({ quiet = false } = {}) {
    if (quiet && loading) {
      refreshPending = true;
      return;
    }
    const scope = readerScopeKey();
    const cacheKey = `${scope}:inbox`;
    const cacheRevision = workspaceViewRevision();
    const ticket = ++requestId;
    loadController?.abort();
    loadController = new AbortController();
    loading = true;
    const deniedOptions = { cacheScope: scope };
    if (!quiet) {
      error = "";
      loadErrorText = "";
      actionError = "";
    }
    try {
      // The workspace shell mounts this page only after session bootstrap.
      // Refreshing it here adds a serial identity round trip on navigation
      // and every live reload. Each source request still authenticates in core;
      // session maintenance and recovery belong to the shell and proxy.
      const applySources = (results) => {
        if (ticket !== requestId || scope !== readerScopeKey()) return;
        const denial = results.find((result) =>
          isReadAccessDenied(result.reason),
        );
        if (denial) handleReadAccessDenied(denial.reason, deniedOptions);
        if (ticket !== requestId) return;
        const failure = results.find((result) => result.reason);
        let nextError = failure ? errorMessage(failure.reason) : "";
        // A refused session will refuse the retry too; offer sign-in instead.
        sessionExpired = results.some(
          (result) => Boolean(result.reason) && isSessionExpired(result.reason),
        );
        if (results[0].status === "fulfilled") {
          decisions = mergeInboxSnapshot(
            decisions,
            results[0].value.items || [],
            results[0].complete,
          );
        } else if (results[0].status === "rejected")
          nextError = errorMessage(results[0].reason);
        // Lists follow cursors up to a bound. Counts drawn from a capped list
        // are lower bounds, and the reader must be told so rather than shown a total.
        truncated = results.some(
          (result) =>
            result.status !== "fulfilled" ||
            result.complete === false ||
            (result.status === "fulfilled" &&
              (result.value?.has_more === true ||
                Boolean(result.value?.next_cursor))),
        );
        if (results[1].status === "fulfilled") {
          actions = mergeInboxSnapshot(
            actions,
            results[1].value.items || [],
            results[1].complete,
          );
          receiptsUnavailable = !results[1].complete;
        } else if (results[1].status === "pending") {
          receiptsUnavailable = true;
        } else if (sessionExpired) {
          // The receipts are not in doubt, the session is; keep the last
          // classification and let the banner say what to do.
          nextError = nextError || errorMessage(results[1].reason);
        } else if (results[1].status === "rejected") {
          // Without receipts, an answered decision cannot be classified; say
          // so rather than quietly filing everything under Watching.
          receiptsUnavailable = true;
          nextError = nextError || errorMessage(results[1].reason);
        }
        // Keep validated work during refreshes. New work needs both histories
        // to finish: an answer to a blocked card may be on a later page.
        const historyComplete = hasCompleteInboxHistory(results);
        if (results[2].status === "fulfilled" && historyComplete) {
          work = mergeInboxSnapshot(
            work,
            results[2].value.work || [],
            results[2].complete,
            "ref",
          );
        } else if (results[2].status === "rejected")
          nextError = nextError || errorMessage(results[2].reason);
        // Replace the histories together. Dropping a known answer while an
        // older open snapshot remains would resurrect the answered ask.
        if (results[3].status === "fulfilled")
          openInboxItems = mergeInboxSnapshot(
            openInboxItems,
            results[3].value.items || [],
            historyComplete,
          );
        if (results[4].status === "fulfilled")
          completedInboxItems = mergeInboxSnapshot(
            completedInboxItems,
            results[4].value.items || [],
            historyComplete,
          );
        const hiddenRefs = new Set([
          ...archivedWorkRefs,
          ...(results[2].value?.archived_refs || []),
        ]);
        work = work.filter((item) => !hiddenRefs.has(item.ref));
        decisions = decisions.filter((item) => !hiddenRefs.has(item.work_ref));
        actions = actions.filter((item) => !hiddenRefs.has(item.work_ref));
        inboxItems = mergeInboxItems(openInboxItems, completedInboxItems);
        if (results[3].status === "rejected") {
          nextError = nextError || errorMessage(results[3].reason);
        } else if (results[4].status === "rejected") {
          nextError = nextError || errorMessage(results[4].reason);
        }
        if (results[5].status === "fulfilled") {
          updates = mergeInboxSnapshot(
            updates,
            results[5].value?.groups || [],
            results[5].complete,
            "group_ref",
          );
        }
        const failures = results.filter((result) => result.reason);
        if (
          nextError &&
          failures.some((result) => !isTransientReadError(result.reason))
        ) {
          error = nextError;
          loadErrorText = nextError;
        } else if (
          quiet &&
          loadErrorText &&
          error === loadErrorText &&
          results.every(
            (result) => result.status === "fulfilled" && !result.reason,
          )
        ) {
          // The failure a live reload recovered from is no longer true.
          error = "";
          loadErrorText = "";
        }
        confirmed = results
          .slice(0, 5)
          .every((result) => result.status === "fulfilled" && result.complete);
        ready =
          ready ||
          confirmed ||
          results
            .slice(0, 5)
            .every((result) => result.status === "fulfilled") ||
          decisions.length > 0 ||
          actions.length > 0 ||
          inboxItems.length > 0 ||
          work.length > 0;
      };
      if (!ready) {
        const cached = readWorkspaceView(cacheKey);
        if (cached) applySources(cached);
      }
      await reliableRead(
        async (signal) => {
          const results = await loadInboxSources({
            onProgress: applySources,
            signal,
          });
          if (ticket !== requestId || scope !== readerScopeKey()) return;
          const failure =
            results.find((result) => isReadAccessDenied(result.reason)) ||
            results.find((result) => result.reason);
          if (failure) throw failure.reason;
          if (
            cacheRevision === workspaceViewRevision() &&
            results
              .slice(0, 5)
              .every(
                (result) => result.status === "fulfilled" && result.complete,
              )
          )
            writeWorkspaceView(cacheKey, results);
          reconnecting = false;
        },
        {
          ...deniedOptions,
          signal: loadController.signal,
          onRetry: () => {
            reconnecting = true;
          },
        },
      );
    } catch (err) {
      if (
        ticket === requestId &&
        scope === readerScopeKey() &&
        !loadController.signal.aborted
      ) {
        error = errorMessage(err);
        loadErrorText = error;
        reconnecting = false;
      }
    } finally {
      if (ticket === requestId) {
        loading = false;
        if (refreshPending) {
          refreshPending = false;
          scheduleLiveRefresh();
        }
      }
    }
  }

  // Live updates: any workspace event may move a row, so reload quietly,
  // coalescing bursts. A reload never runs under an action in flight.
  const LIVE_REFRESH_DELAY_MS = 600;
  let liveTimer = null;
  function scheduleLiveRefresh() {
    clearTimeout(liveTimer);
    liveTimer = setTimeout(() => {
      liveTimer = null;
      if (busy) {
        scheduleLiveRefresh();
        return;
      }
      invalidateInboxContext();
      invalidateAskDetail();
      contextEpoch += 1;
      void load({ quiet: true });
    }, LIVE_REFRESH_DELAY_MS);
  }

  async function recordAnswer(event) {
    event.preventDefault();
    if (!selectedDecision || busy || !answer.trim() || !choice) return;
    busy = true;
    const scope = readerScopeKey();
    error = "";
    try {
      const result = await coreClient.answerPmDecision(selectedDecision.id, {
        revision: selectedDecision.revision,
        approve: choice === "approve",
        text: answer.trim(),
      });
      commitInboxView(scope, { decision: result });
      if (scope !== readerScopeKey()) return;
      requestId++;
      loadController?.abort();
      selectionRequest++;
      selectedController?.abort();
      decisionResolvedFor = `decision:${result.id}`;
      loading = false;
      refreshPending = false;
      const approved = choice === "approve";
      const pinId = `decision:${result.id}`;
      const wasUnpinned = !urlItem;
      if (wasUnpinned) heldItem = pinId;
      decisions = decisions.map((item) =>
        item.id === result.id ? result : item,
      );
      answer = "";
      choice = "";
      notice = approved ? "Approved." : "Declined.";
      // The answered row leaves this mailbox. Hold it, then pin it in the
      // URL so a reload keeps the same row (and the notice) in view.
      if (wasUnpinned) {
        const moved = rows.find((row) => row.id === pinId);
        await goto(
          href({
            item: pinId,
            mailbox: moved?.mailbox || mailbox,
          }),
          { replaceState: true, noScroll: true, keepFocus: true },
        );
        heldItem = "";
      }
      if (result.action_id) {
        try {
          const receipt = await coreClient.getPmAction(result.action_id);
          actions = [
            ...actions.filter((item) => item.id !== receipt.id),
            receipt,
          ];
        } catch (err) {
          actionError = errorMessage(err);
        }
        // A Nexus-owned change has no one to deliver to but core itself, so
        // approving applies it in the same breath. Source-owned requests keep
        // the explicit deliver step, because delivery there has a receipt.
        if (approved && selectedWork && isNexusOwned(selectedWork)) {
          try {
            let applied = await coreClient.dispatchPmDecision(result.id);
            // Core reports the write, then verifies it by reading the
            // canonical record back; for Nexus-owned work both are local.
            if (applied.status !== "failed") {
              applied = await coreClient.reconcilePmAction(applied.id);
            }
            actions = [
              ...actions.filter((item) => item.id !== applied.id),
              applied,
            ];
            notice =
              applied.status === "verified"
                ? "Approved and applied."
                : applied.status === "failed"
                  ? `Approved, but applying it failed: ${applied.receipt?.detail || "see the receipt below"}.`
                  : "Approved. Delivery is in progress.";
          } catch (err) {
            actionError = errorMessage(err);
            // Core records why (stale revision, no path); show that record,
            // not the pre-dispatch snapshot with a live Deliver button.
            await refreshReceipt();
          }
        }
      }
    } catch (err) {
      if (errorCode(err) === "source_revision_changed") {
        // Core refused a knowingly stale approval; this page was behind.
        // Reload so the row shows why, and leave the reader the decline.
        const reason = String(
          err?.body?.error?.details?.reason ?? err?.details?.reason ?? "",
        );
        error =
          reason === "work_missing"
            ? "The task this proposal refers to no longer exists, so it cannot be approved. You can still decline it."
            : reason === "already_at_target"
              ? "The task is already where this proposal asks, so there is nothing to approve. You can dismiss it."
              : "The task changed after this was proposed, so approving it is refused. Decline it or wait for a fresh proposal.";
        void load();
        return;
      }
      error = supersededMessage(err) || errorMessage(err);
    } finally {
      busy = false;
    }
  }

  /**
   * A 409 on an answer or delivery can mean the PM replaced this proposal;
   * core names the replacement in the error details.
   */
  function supersededMessage(err) {
    const details =
      err?.body?.error?.details ?? err?.details?.details ?? err?.details ?? {};
    const replacement = String(details?.superseded_by ?? "").trim();
    if (!replacement) return "";
    notice = "";
    supersededHref = href({ item: `decision:${replacement}` });
    // The replacement can be the reader's own board move; say who.
    const by = String(details?.superseded_by_proposed_by ?? "").trim();
    const origin = String(details?.superseded_by_origin_kind ?? "").trim();
    const who =
      by && by === ($selectedActorId || "")
        ? "You replaced this proposal (by moving the task)"
        : origin === "human" && by
          ? `${actorDisplayLabel(by, $actorRegistry, $principalRegistry) || "Someone"} replaced this proposal`
          : "The PM replaced this proposal";
    return `${who} before it was answered. Open the replacement to decide on it.`;
  }
  function errorCode(err) {
    return String(
      err?.body?.error?.code ?? err?.details?.error?.code ?? err?.code ?? "",
    );
  }
  let supersededHref = $state("");
  let sessionExpired = $state(false);
  function signInAgain() {
    restartSession({
      organizationSlug: $page.params.organization,
      workspaceSlug: $page.params.workspace,
      hostedMode: $page.data?.shellCapabilities?.mode === "hosted",
      workspaceId: $page.data?.workspace?.workspaceId,
      currentAppPath: "/inbox",
      search: $page.url.search,
    });
  }

  async function deliver() {
    if (!selectedDecision || busy) return;
    busy = true;
    busyWith = "deliver";
    error = "";
    notice = "";
    try {
      const result = await coreClient.dispatchPmDecision(selectedDecision.id);
      actions = [...actions.filter((item) => item.id !== result.id), result];
      // The dispatch call succeeds even when the delivery it records did not.
      const status = String(result?.status ?? "");
      notice =
        status === "failed"
          ? "Delivery failed; the receipt below says why."
          : status === "verified"
            ? "Delivered and read back."
            : status === "delivered"
              ? "Delivered; waiting for the source to read it back."
              : "Delivery request recorded.";
    } catch (err) {
      const raw = errorMessage(err);
      if (errorCode(err) === "source_revision_changed") {
        // Terminal, and the receipt below says so; a Retry would contradict it.
        const reason = String(
          err?.body?.error?.details?.reason ?? err?.details?.reason ?? "",
        );
        notice =
          reason === "work_missing"
            ? "Not delivered: the task this approval refers to no longer exists. Nothing was sent; acknowledge it to file it under Handled."
            : reason === "work_read_failed"
              ? "Not delivered: the task could not be read just now. Nothing was sent; try again shortly."
              : "Not delivered: the task changed after this was approved. A fresh proposal and approval are needed.";
        await refreshReceipt();
        return;
      }
      // Core has no executor for this source yet: the approval is intact and
      // the action stays pending. That is not an outage.
      error =
        supersededMessage(err) ||
        (/not configured|unavailable/i.test(raw)
          ? "No delivery path is configured for this source yet. The approved request stays pending until one is."
          : raw);
      await refreshReceipt();
    } finally {
      busy = false;
      busyWith = "";
    }
  }

  async function refreshReceipt() {
    if (!selectedDecision?.action_id) return;
    try {
      const result = await coreClient.getPmAction(selectedDecision.action_id);
      actions = [...actions.filter((item) => item.id !== result.id), result];
    } catch (err) {
      actionError = errorMessage(err);
    }
  }

  async function acknowledge() {
    if (!action || busy) return;
    busy = true;
    busyWith = "acknowledge";
    actionError = "";
    try {
      const result = await coreClient.acknowledgePmAction(action.id);
      actions = actions.map((item) => (item.id === result.id ? result : item));
      notice = "Acknowledged. It now sits under Handled.";
    } catch (err) {
      actionError = errorMessage(err);
    } finally {
      busy = false;
      busyWith = "";
    }
  }

  async function reconcile() {
    if (!action || busy) return;
    busy = true;
    busyWith = "reconcile";
    actionError = "";
    try {
      const result = await coreClient.reconcilePmAction(action.id);
      actions = actions.map((item) => (item.id === result.id ? result : item));
      notice = "Read-back finished.";
    } catch (err) {
      actionError = errorMessage(err);
    } finally {
      busy = false;
      busyWith = "";
    }
  }

  async function markUpdateRead(group) {
    const ref = String(group?.group_ref ?? "").trim();
    if (!ref) return;
    const next = neighbourId(`update:${ref}`);
    try {
      await coreClient.markHomeRead({ group_ref: ref });
      updates = updates.filter((entry) => entry.group_ref !== ref);
      if (explicitId === `update:${ref}`) await select(next);
    } catch (err) {
      error = errorMessage(err);
    }
  }

  /** The row to land on once `id` leaves the list: the next, else the previous. */
  function neighbourId(id, excludedRef = "") {
    const index = visible.findIndex((row) => row.id === id);
    if (index < 0) return "";
    const survives = (row) => !excludedRef || row.ref !== excludedRef;
    return (
      visible.slice(index + 1).find(survives)?.id ||
      visible.slice(0, index).reverse().find(survives)?.id ||
      ""
    );
  }

  async function select(id, { scroll = false } = {}) {
    await goto(href({ item: id || "" }), {
      replaceState: true,
      keepFocus: true,
      noScroll: true,
    });
    if (!scroll || !id) return;
    await tick();
    const escaped = globalThis.CSS?.escape ? CSS.escape(id) : id;
    document
      .querySelector(`[data-inbox-row="${escaped}"]`)
      ?.scrollIntoView?.({ block: "nearest" });
  }

  /**
   * Answers an inbox item behind the undo toast and moves on to the next
   * row. The committed call is the one the standalone page makes: the text,
   * and the requester notified when core can reach them.
   */
  function respondInbox(
    row,
    text,
    {
      acknowledge = false,
      proposal = "",
      outcome = "answered",
      // The composer as it was when the reader chose. A suggestion sends
      // after a short flash, and by then they may have moved to another row
      // whose draft is not the one Undo has to hand back.
      draft = reply,
      from = mailbox,
      // Where it is going, captured with it: the reader can switch workspace
      // inside the flash, and the answer still belongs to this one.
      binding = undefined,
    } = {},
  ) {
    const item = row?.item;
    const body = String(text ?? "").trim();
    if (!item?.id || !body || busy) return;
    const who = row.requester?.name || "";
    const request = acknowledge
      ? { response_text: body, outcome: "acknowledged", notify_mode: "none" }
      : { response_text: body, outcome, notify_mode: defaultNotifyMode(item) };
    /*
     * A context request is not an answer, and the toast must not claim it was
     * one: the ask goes back to whoever wrote it, to be re-asked with what was
     * missing.
     */
    const sentMessage =
      outcome === NEEDS_CONTEXT_OUTCOME
        ? who
          ? `Sent back to ${who} for context`
          : "Sent back for context"
        : who
          ? `Sent to ${who}`
          : "Response sent";
    const next = neighbourId(row.id);
    queueInboxResponse({
      itemId: item.id,
      item,
      binding,
      request,
      message: acknowledge ? "Acknowledged" : sentMessage,
      restore: {
        origin: "pane",
        rowId: row.id,
        mailbox: from,
        reply: acknowledge || proposal ? draft : body,
        chosen: proposal,
      },
    });
    // Answering the row on screen moves on from it; answering one the reader
    // has already left alone leaves their place where it is.
    if (row.id !== selected?.id) return;
    reply = "";
    chosen = "";
    void select(next, { scroll: true });
  }

  function acknowledgeInbox(row) {
    respondInbox(row, "Acknowledged from inbox", { acknowledge: true });
  }

  function undoLastResponse() {
    const entry = undoInboxResponse();
    if (!entry) return;
    const restore = entry.restore || {};
    if (restore.origin === "item" && restore.href) {
      stashInboxRestore(entry.itemId, restore);
      void goto(restore.href);
      return;
    }
    const rowId = restore.rowId || "";
    if (!rowId) return;
    if (selected?.id === rowId) {
      reply = restore.reply || "";
      chosen = restore.chosen || "";
    } else {
      restoreDraft = {
        id: rowId,
        reply: restore.reply || "",
        chosen: restore.chosen || "",
      };
    }
    void goto(href({ mailbox: restore.mailbox || "needs-you", item: rowId }), {
      replaceState: true,
      keepFocus: true,
      noScroll: true,
    });
  }

  function paneElement(selector) {
    return detailPane?.querySelector(selector) || null;
  }

  function handleKeydown(event) {
    const shortcut = inboxShortcutAction(event, {
      helpOpen,
      modalOpen: !helpOpen && otherDialogOpen(),
    });
    if (!shortcut) return;
    switch (shortcut.type) {
      case "help":
        helpOpen = true;
        break;
      case "close-help":
        helpOpen = false;
        break;
      case "undo":
        // Nothing waiting: leave ⌘Z to the browser.
        if (!hasPendingInboxResponse()) return;
        undoLastResponse();
        break;
      case "next":
      case "previous": {
        if (!visible.length) return;
        const step = shortcut.type === "next" ? 1 : -1;
        const index =
          selectedIndex < 0
            ? 0
            : Math.min(visible.length - 1, Math.max(0, selectedIndex + step));
        void select(visible[index].id, { scroll: true });
        break;
      }
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
        const field = paneElement("textarea:not([disabled])");
        if (!field) return;
        field.focus();
        break;
      }
      case "done": {
        const control = paneElement(
          '[data-inbox-shortcut="done"]:not([disabled])',
        );
        if (!control) return;
        control.click();
        break;
      }
      case "open": {
        const link = paneElement('a[data-inbox-shortcut="open"]');
        if (!link) return;
        link.click();
        break;
      }
      default:
        return;
    }
    event.preventDefault();
  }

  async function archiveStaleTask(row) {
    if (busy || !row.stale || !isNexusOwned(row.item)) return;
    const { prefix, id } = splitTypedRef(row.ref);
    if (prefix !== "card" || !id) return;
    busy = true;
    const scope = readerScopeKey();
    actionError = "";
    try {
      await coreClient.archiveCard(id, {
        ...(Number.isInteger(row.item.version)
          ? { if_version: row.item.version }
          : {}),
      });
      commitInboxView(scope, { archivedRef: row.ref });
      if (scope !== readerScopeKey()) return;
      // Invalidate feed reads and prevent pending single-item reads from
      // restoring this task's decisions, without cancelling unrelated reads.
      requestId++;
      loadController?.abort();
      loading = false;
      refreshPending = false;
      archivedWorkRefs.add(row.ref);
      // Navigate while the old row still exists. Related decisions also leave
      // after archive, so select only a row that will survive the removal.
      if (selectedId === row.id || selected?.ref === row.ref) {
        selectionRequest++;
        heldItem = "";
        await select(neighbourId(selectedId, row.ref));
      }
      work = work.filter((item) => workKey(item) !== row.ref);
      decisions = decisions.filter((item) => item.work_ref !== row.ref);
      actions = actions.filter((item) => item.work_ref !== row.ref);
      // Flush selection effects before setting the outcome they normally clear.
      await tick();
      notice = "Task archived. You can restore it from Archive.";
      scheduleLiveRefresh();
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  function inboxKindLabel(row) {
    const kind = String(row?.category ?? "").toLowerCase();
    return (INBOX_CATEGORY_LABELS[kind] ?? kind ?? "").toUpperCase();
  }

  function taskBlockers(item) {
    const raw = item?.blockers ?? item?.blocked_by ?? [];
    return (Array.isArray(raw) ? raw : [raw])
      .map((value) =>
        typeof value === "string" ? value : String(value?.summary ?? ""),
      )
      .map((value) => value.trim())
      .filter(Boolean);
  }

  function taskLastChecked(item) {
    const observed = item?.freshness?.last_observed_at;
    return observed ? formatTimestamp(observed) : "never";
  }

  /** "3h 12m", and whether it is long enough to colour. */
  function waitFor(row) {
    const ms = rowWaitMs(row, now);
    return {
      text: formatWait(ms),
      long: Number.isFinite(ms) && ms >= 60 * 60 * 1000,
    };
  }

  function updateEvents(group) {
    return (Array.isArray(group?.events) ? group.events : [])
      .slice(0, 12)
      .map((event) => ({
        event,
        described: describeUpdateEvent(event, {
          titleFor: (ref) =>
            work.find((item) => workKey(item) === ref)?.title || "",
        }),
      }));
  }

  function eventHref(described) {
    const { prefix, id } = splitTypedRef(described?.objectRef);
    if (prefix === "card")
      return workspaceHref(`/tasks/${encodeURIComponent(described.objectRef)}`);
    if (prefix === "document")
      return workspaceHref(`/docs/${encodeURIComponent(id)}`);
    return "";
  }

  // A receipt that is still moving (pending, sending, unknown) is refreshed
  // on its own; nothing else on the page changes without the reader.
  const RECEIPT_IN_FLIGHT = new Set(["pending_delivery", "sending", "unknown"]);
  function refreshIfInFlight() {
    if (busy || !action || !RECEIPT_IN_FLIGHT.has(String(action.status)))
      return;
    void refreshReceipt();
  }

  // The sidebar count is this page's Needs you tab while the page is open.
  $effect(() => {
    if (!ready) return;
    const count = rows.filter((row) => row.mailbox === "needs-you").length;
    // An incomplete empty result cannot erase a known obligation. Nonempty
    // partial snapshots and optimistic answers still update both counts.
    if (!confirmed && truncated && count === 0) return;
    publishInboxCount(
      $page.params.workspace,
      count,
      truncated || streamPartial,
    );
  });

  onMount(() => {
    const scope = readerScopeKey();
    const stopDenied = onWorkspaceViewsDenied(scope, (denial) => {
      if (scope !== readerScopeKey()) return;
      requestId++;
      loadController?.abort();
      selectionRequest++;
      selectedController?.abort();
      decisions = [];
      actions = [];
      work = [];
      inboxItems = [];
      openInboxItems = [];
      completedInboxItems = [];
      updates = [];
      ready = false;
      confirmed = false;
      loading = false;
      reconnecting = false;
      refreshPending = false;
      error = errorMessage(denial);
      sessionExpired = isSessionExpired(denial);
      publishInboxCount($page.params.workspace, null);
    });
    const stopScope = readerScope.subscribe((next) => {
      if (next !== scope) {
        requestId++;
        loadController?.abort();
        decisions = [];
        actions = [];
        work = [];
        inboxItems = [];
        openInboxItems = [];
        completedInboxItems = [];
        updates = [];
        ready = false;
        confirmed = false;
      }
    });
    let mounted = true;
    const stopCache = onWorkspaceViewChanged((key, snapshot) => {
      if (key !== `${scope}:inbox` || !snapshot) return;
      // Let the originating handler choose its next row before replacing data.
      queueMicrotask(() => {
        if (!mounted || scope !== readerScopeKey()) return;
        // A denial or sign-out may have purged it since the notification.
        const latest = readWorkspaceView(key);
        if (!latest) return;
        requestId++;
        loadController?.abort();
        selectionRequest++;
        selectedController?.abort();
        loading = false;
        refreshPending = false;
        decisions = latest[0].value?.items || [];
        actions = latest[1].value?.items || [];
        work = latest[2].value?.work || [];
        openInboxItems = latest[3].value?.items || [];
        completedInboxItems = latest[4].value?.items || [];
        inboxItems = mergeInboxItems(openInboxItems, completedInboxItems);
        updates = latest[5].value?.groups || [];
        receiptsUnavailable = !latest[1].complete;
        confirmed = latest.every(
          (result) =>
            result.status === "fulfilled" && result.complete !== false,
        );
      });
    });
    void load();
    const releaseCount = claimInboxCount();
    const stopLive = liveWorkspaceEvents({
      client: coreClient,
      onChange: () => scheduleLiveRefresh(),
    });
    const stopInbox = liveInboxChanges({
      client: coreClient,
      onChange: (changes) => {
        for (const change of changes) {
          if (change.type === "inbox_page") streamPartial = change.partial;
        }
        scheduleLiveRefresh();
      },
    });
    const stopCommitted = onInboxResponseCommitted((itemId) => {
      // The Handled row this becomes is the same ask, so its cached pre-answer
      // outcome has to go even when the live stream is down.
      invalidateAskDetail(
        inboxItems.find((item) => item.id === itemId) ?? null,
      );
      // Keep the server-confirmed answer beyond the temporary overlay, until
      // complete histories replace it. Retained work must stay suppressed too.
      const answered = applyResponseOverlay(
        inboxItems.filter((item) => item.id === itemId),
        { [itemId]: $inboxResponseOverlay[itemId] },
        Date.now(),
      );
      completedInboxItems = mergeInboxSnapshot(
        completedInboxItems,
        answered,
        false,
      );
      // Invalidate reads begun before the commit so they cannot restore it.
      requestId++;
      loadController?.abort();
      loading = false;
      refreshPending = false;
      openInboxItems = openInboxItems.filter((item) => item.id !== itemId);
      inboxItems = mergeInboxItems(openInboxItems, completedInboxItems);
      scheduleLiveRefresh();
    });
    const timer = setInterval(() => {
      now = Date.now();
      refreshIfInFlight();
    }, 15_000);
    const onVisible = () => {
      if (!document.hidden) refreshIfInFlight();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      mounted = false;
      stopCache();
      stopDenied();
      stopScope();
      requestId++;
      loadController?.abort();
      selectionRequest++;
      selectedController?.abort();
      clearInterval(timer);
      clearTimeout(liveTimer);
      stopLive();
      stopInbox();
      stopCommitted();
      releaseCount();
      document.removeEventListener("visibilitychange", onVisible);
    };
  });
</script>

<svelte:window
  onkeydown={handleKeydown}
  onbeforeunload={(event) => {
    // A response inside its undo window goes out now rather than being lost
    // with the tab; the prompt gives it time to land.
    if (hasPendingInboxResponse()) {
      void flushInboxResponse();
      event.preventDefault();
      event.returnValue = "";
      return;
    }
    if (answer.trim()) {
      event.preventDefault();
      event.returnValue = "";
    }
  }}
/>
<svelte:head><title>Inbox · Agent Nexus</title></svelte:head>
<WorkspacePageShell data-tour="inbox">
  <WorkspacePageHeader title="Inbox">
    {#snippet actions()}
      <button
        class="ui-icon-btn"
        onclick={openCommandPalette}
        aria-label="Search workspace"
        aria-keyshortcuts="Meta+K"
        title="Search workspace (⌘K)"
        type="button"
      >
        <svg
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="1.5"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d={navIconPath("search")} />
        </svg>
      </button>
      {#if pmVisible}
        <a class="ui-btn-secondary" href={workspaceHref("/pm")}>Ask PM</a>
      {/if}
    {/snippet}
  </WorkspacePageHeader>
  <nav class="flex flex-wrap items-center gap-1" aria-label="Inbox mailbox">
    {#each INBOX_MAILBOXES as [key, title]}
      <a
        class="rounded-md px-2.5 py-1.5 text-meta {mailbox === key
          ? 'bg-bg-soft font-medium text-fg'
          : 'text-fg-muted hover:bg-panel-hover hover:text-fg'}"
        href={href({ mailbox: key, item: "" })}
        aria-current={mailbox === key ? "page" : undefined}
        >{title}{#if counts[key]}<span class="ml-1.5 text-micro text-fg-subtle"
            >{counts[key]}{truncated || streamPartial ? "+" : ""}</span
          >{/if}</a
      >
    {/each}
    {#if workRef}
      <span
        class="ml-1 inline-flex max-w-full items-center gap-1.5 rounded-md border border-line px-2 py-1 text-micro text-fg-muted"
        data-inbox-work-filter
      >
        <span class="min-w-0 truncate"
          >Only <span class="text-fg">{workRefTitle}</span></span
        >
        <a
          class="shrink-0 text-fg-subtle hover:text-fg"
          href={href({ work_ref: "", item: "" })}
          aria-label="Show the whole inbox">×</a
        >
      </span>
    {/if}
    {#if presentation.added}
      <span class="ml-2 text-micro text-fg-subtle" data-inbox-more-loaded>
        {presentation.added} more loaded
      </span>
    {/if}
    {#if truncated || streamPartial}
      <span class="ml-2 text-micro text-fg-subtle"
        >Not everything is loaded; the counts are lower bounds.</span
      >
    {/if}
    <span class="ml-auto text-micro text-fg-muted" role="status">
      {reconnecting ? "Reconnecting…" : loading && ready ? "Refreshing…" : ""}
    </span>
  </nav>
  {#if error}
    <StateError
      message={error}
      onretry={sessionExpired ? signInAgain : load}
      retryLabel={sessionExpired ? "Sign in again" : "Retry"}
      retrying={loading}
    />
    {#if supersededHref}
      <a class="ui-prose-link text-meta" href={supersededHref}
        >Open the replacement</a
      >
    {/if}
  {/if}
  {#if notice}
    <p
      class="text-micro text-fg-muted outline-none"
      role="status"
      tabindex="-1"
      bind:this={noticeElement}
    >
      {notice}
    </p>
  {/if}
  {#if !ready}
    <div
      class="rounded-md border border-line p-4"
      data-inbox-loading
      aria-busy="true"
    >
      <p class="mb-3 text-meta text-fg-muted" role="status">
        {explicitId ? "Loading requested item…" : "Loading inbox…"}
      </p>
      <SkeletonInboxRow
        count={Math.min(
          10,
          Math.max(
            1,
            $inboxNeedsYouCount.workspace === $page.params.workspace
              ? ($inboxNeedsYouCount.count ?? 5)
              : 5,
          ),
        )}
      />
    </div>
  {:else}
    {@const showDetail = Boolean(selectedId)}
    {@const showDoc = Boolean(selectedId && docPanelRef)}
    <div
      class="grid overflow-hidden rounded-md border border-line bg-panel {visible.length
        ? 'lg:min-h-[30rem]'
        : ''} {showDoc
        ? 'lg:grid-cols-[minmax(13rem,0.7fr)_minmax(0,1.1fr)_minmax(0,1fr)]'
        : showDetail
          ? 'lg:grid-cols-[minmax(16rem,0.9fr)_minmax(0,1.4fr)]'
          : ''}"
    >
      <section
        class="min-w-0 border-line {showDetail ? 'lg:border-r' : ''} {explicitId
          ? 'hidden lg:block'
          : ''}"
        aria-label="Inbox list"
      >
        {#snippet inboxRow(row)}
          {@const badge = inboxRowBadge(row, now)}
          {@const wait = row.mailbox === "needs-you" ? waitFor(row) : null}
          <li class="flex items-center">
            <a
              class="flex h-[52px] min-w-0 flex-1 flex-col justify-center gap-0.5 border-l-2 px-4 {selectedId ===
              row.id
                ? 'border-accent bg-bg-soft'
                : 'border-transparent hover:bg-panel-hover'}"
              href={href({ item: row.id })}
              data-inbox-row={row.id}
              data-testid={row.kind === "inbox" && row.item?.id
                ? `inbox-row-${row.item.id}`
                : `inbox-row-${row.id}`}
              aria-current={selectedId === row.id ? "page" : undefined}
            >
              <div class="flex min-w-0 items-center gap-2">
                <span
                  class="min-w-0 flex-1 truncate text-meta font-medium text-fg"
                  >{row.title}</span
                >
                {#if badge}
                  <SignalBadge tone={badge.tone} class="shrink-0"
                    >{badge.label}</SignalBadge
                  >
                {/if}
              </div>
              <div
                class="flex min-w-0 items-center gap-2 text-micro text-fg-muted"
              >
                <span class="min-w-0 flex-1 truncate"
                  >{#if row.kind === "inbox" && row.requesterLabel}<span
                      class="text-fg">{row.requesterLabel}</span
                    >{#if row.source}{" "}· {row.source}{/if}{:else}{row.source ||
                      row.kind}{/if}</span
                >
                {#if wait?.text}
                  <span
                    class="shrink-0 tabular-nums {wait.long
                      ? 'text-warn-text'
                      : ''}"
                    title={row.waitingSince
                      ? `Waiting since ${formatAbsoluteDateTime(row.waitingSince)}`
                      : undefined}
                    data-inbox-wait>{wait.text}</span
                  >
                {:else if row.time}
                  <Time value={row.time} class="shrink-0 tabular-nums" />
                {/if}
              </div>
            </a>
            {#if row.stale && isNexusOwned(row.item) && splitTypedRef(row.ref).prefix === "card"}
              <button
                class="ui-btn-secondary mr-3 shrink-0"
                type="button"
                disabled={busy}
                aria-label={`Archive ${row.title}`}
                onclick={() => archiveStaleTask(row)}>Archive</button
              >
            {/if}
          </li>
        {/snippet}
        <ul class="divide-y divide-line-subtle">
          {#each currentRows as row (row.id)}
            {@render inboxRow(row)}
          {:else}
            <!--
              An empty inbox is good news, and good news is one line. It used to
              be a sentence and a link run together inside a half-screen of
              whitespace; it is a short headline, what is still being watched,
              and when the reader last finished something — centred, and no
              taller than it needs to be (the grid drops its minimum height
              when there is nothing to list).
            -->
            {#if (!confirmed || streamPartial) && !staleRows.length && !lateRows.length}
              <li class="p-4" aria-busy="true">
                <SkeletonInboxRow count={1} />
              </li>
            {:else if !staleRows.length && !lateRows.length}
              <li class="px-5 py-8">
                <div
                  class="mx-auto flex max-w-sm flex-col items-center gap-1 text-center"
                  data-inbox-empty={mailbox}
                >
                  {#if mailbox === "needs-you"}
                    <p class="text-meta font-medium text-fg">You're clear.</p>
                    {#if counts.watching}
                      <a
                        class="text-micro text-accent-text hover:underline"
                        href={href({ mailbox: "watching", item: "" })}
                        data-inbox-empty-watching
                        >{counts.watching}
                        {counts.watching === 1 ? "thing" : "things"} being watched</a
                      >
                    {/if}
                  {:else if mailbox === "watching"}
                    <p class="text-meta font-medium text-fg">
                      Nothing is waiting on a source or a delivery.
                    </p>
                  {:else}
                    <p class="text-meta font-medium text-fg">
                      Nothing handled yet.
                    </p>
                    <p class="text-micro text-fg-muted">
                      Answered decisions and dismissed items land here.
                    </p>
                  {/if}
                  {#if lastHandledAt && mailbox === "needs-you"}
                    <p
                      class="text-micro text-fg-subtle"
                      data-inbox-empty-handled
                    >
                      Last handled <Time value={lastHandledAt} />
                    </p>
                  {/if}
                </div>
              </li>
            {/if}
          {/each}
        </ul>
        {#if staleRows.length}
          <div class="border-t border-line-subtle" data-inbox-stale>
            <button
              type="button"
              class="w-full px-4 py-3 text-left text-meta text-fg-muted hover:bg-panel-hover"
              aria-expanded={staleExpanded}
              aria-controls="inbox-stale-tasks"
              onclick={() => (staleExpanded = !staleExpanded)}
            >
              Stale ({staleRows.length})
            </button>
            {#if staleExpanded}
              <ul id="inbox-stale-tasks" class="divide-y divide-line-subtle">
                {#each staleRows as row (row.id)}
                  {@render inboxRow(row)}
                {/each}
              </ul>
            {/if}
          </div>
        {/if}
        {#if lateRows.length}
          <ul class="divide-y divide-line-subtle border-t border-line-subtle">
            {#each lateRows as row (row.id)}
              {@render inboxRow(row)}
            {/each}
          </ul>
        {/if}
      </section>
      {#if showDetail}
        <section
          class="flex min-w-0 flex-col {explicitId ? '' : 'hidden lg:flex'}"
          aria-label="Selected inbox item"
          bind:this={detailPane}
        >
          <div
            class="flex items-center gap-3 border-b border-line-subtle px-4 py-2 text-micro"
          >
            <a class="text-accent-text lg:hidden" href={href({ item: "" })}
              >← List</a
            >
            {#if selectedIndex > 0}
              <a
                class="text-fg-muted hover:text-fg"
                href={href({ item: visible[selectedIndex - 1].id })}
                aria-keyshortcuts="K"
                title="Previous (K)">Previous</a
              >
            {/if}
            {#if selectedIndex >= 0 && selectedIndex < visible.length - 1}
              <a
                class="text-fg-muted hover:text-fg"
                href={href({ item: visible[selectedIndex + 1].id })}
                aria-keyshortcuts="J"
                title="Next (J)">Next</a
              >
            {/if}

            <span class="ml-auto shrink-0 tabular-nums text-fg-subtle"
              >{selectedIndex >= 0
                ? `${selectedIndex + 1} of ${visible.length}`
                : ""}</span
            >
          </div>
          <div class="min-w-0 flex-1">
            {#if selected?.kind === "decision"}
              <DecisionPanel
                selected={selectedDecision}
                taskTitle={selectedTaskTitle}
                work={selectedWork}
                {action}
                {busyWith}
                actorLabel={(id) =>
                  id
                    ? actorDisplayLabel(id, $actorRegistry, $principalRegistry)
                    : ""}
                currentActorId={$selectedActorId || ""}
                pmHref={pmVisible ? workspaceHref("/pm") : ""}
                replacement={selectedDecision?.superseded_by
                  ? decisions.find(
                      (item) => item.id === selectedDecision.superseded_by,
                    ) || null
                  : null}
                workHref={workspaceHref(
                  taskDetailPath({ ref: selectedDecision.work_ref }),
                )}
                {busy}
                {actionError}
                bind:answer
                bind:choice
                onAnswer={recordAnswer}
                onDeliver={deliver}
                onReconcile={reconcile}
                onAcknowledge={acknowledge}
                onRefreshReceipt={refreshReceipt}
              />
            {:else if selected?.kind === "task"}
              {@const taskItem = selected.item}
              {@const blockers = taskBlockers(taskItem)}
              {@const wait =
                selected.mailbox === "needs-you" ? waitFor(selected) : null}
              <div class="space-y-4 p-4 sm:p-5">
                {#if wait?.text}
                  <p class="text-micro text-fg-muted">
                    {taskItem.work_summary ? "Task age" : "Blocked for"}
                    <span
                      class="font-medium {wait.long
                        ? 'text-warn-text'
                        : 'text-fg'}">{wait.text}</span
                    >
                  </p>
                {/if}
                <h2 class="text-subtitle text-fg [overflow-wrap:anywhere]">
                  {selected.title}
                </h2>
                {#if blockers.length}
                  <div>
                    <p class="ui-label">Blocked by</p>
                    <ul
                      class="space-y-1 text-meta text-fg [overflow-wrap:anywhere]"
                    >
                      {#each blockers as blocker}
                        <li>{blocker}</li>
                      {/each}
                    </ul>
                  </div>
                {/if}
                {#if taskItem?.next_action}
                  <p class="text-meta text-fg [overflow-wrap:anywhere]">
                    {#if taskItem.next_actor}<span class="text-fg-muted"
                        >{actorName(taskItem.next_actor) || taskItem.next_actor} —
                      </span>{/if}{taskItem.next_action}
                  </p>
                {/if}
                {#if !isNexusOwned(taskItem)}
                  <p class="text-micro text-fg-muted [overflow-wrap:anywhere]">
                    Last checked {taskLastChecked(
                      taskItem,
                    )}{#if taskItem?.refresh?.last_error?.message}
                      <span class="text-warn-text">
                        · {humanizeInstants(
                          readErrorExplanation(taskItem.refresh.last_error),
                        )}</span
                      >{/if}
                  </p>
                {/if}
                <div class="flex flex-wrap gap-2">
                  {#if pmVisible}
                    <a
                      class="ui-btn-primary"
                      href={`${workspaceHref("/pm")}?work_ref=${encodeURIComponent(selected.ref)}`}
                      >Ask PM about this</a
                    >
                  {/if}
                  <a
                    class={pmVisible ? "ui-btn-secondary" : "ui-btn-primary"}
                    href={workspaceHref(taskDetailPath(taskItem))}
                    data-inbox-shortcut="open">Open task</a
                  >
                </div>
              </div>
            {:else if selected?.kind === "inbox"}
              {@const needsResponse = inboxItemNeedsResponse(selected.item)}
              {@const reminder = inboxItemIsReminder(selected.item)}
              {@const wait = needsResponse ? waitFor(selected) : null}
              <div class="space-y-4 p-4 sm:p-5">
                <div
                  class="flex flex-wrap items-center gap-x-2 gap-y-1 text-micro text-fg-muted"
                >
                  {#if inboxKindLabel(selected)}
                    <span class="ui-label mb-0">{inboxKindLabel(selected)}</span
                    >
                  {/if}
                  {#if selected.severity}
                    <SignalBadge
                      tone={String(selected.severity).toLowerCase() ===
                      "critical"
                        ? "danger"
                        : "warn"}>{sentenceCase(selected.severity)}</SignalBadge
                    >
                  {/if}
                  {#if needsResponse}
                    <span class="min-w-0 [overflow-wrap:anywhere]"
                      ><InboxActorName
                        name={selected.requester?.name}
                        id={selected.requester?.id}
                      />
                      {#if wait?.text}has been blocked for <span
                          class="font-medium {wait.long
                            ? 'text-warn-text'
                            : 'text-fg'}"
                          title={`Asked ${formatAbsoluteDateTime(selected.waitingSince)}`}
                          data-inbox-blocked-for>{wait.text}</span
                        >{:else}is waiting on you{/if}</span
                    >
                  {:else if !reminder}
                    <!-- A reminder has no requester. `InboxActorName` renders
                         the word "someone" for an empty one, which asserted a
                         person who is not waiting on anything. -->
                    <span class="min-w-0 [overflow-wrap:anywhere]"
                      >from <InboxActorName
                        name={selected.requester?.name}
                        id={selected.requester?.id}
                      /></span
                    >
                  {/if}
                </div>
                <h2 class="text-subtitle text-fg [overflow-wrap:anywhere]">
                  {selected.title}
                </h2>
                <InboxContextStrip
                  relation={contextRelation}
                  subject={contextSubject}
                  {now}
                  subjectHref={subjectHref(selected.subject)}
                  note={context?.note || null}
                  noteAuthor={context?.note
                    ? actorName(context.note.actorId) ||
                      (context.note.byRequester
                        ? selected.requester?.name || ""
                        : "")
                    : ""}
                  loading={contextLoading && !context}
                  presenceActorId={selected.requester?.id || ""}
                />
                {#if askDelivery?.isStale && needsResponse}
                  <!-- From the ask read, which is the only thing that knows:
                       core measures the subject card's last change against the
                       deployment's staleness window, and does not put the
                       result on the row. So this says what was measured, not
                       where the row ended up. -->
                  <p class="text-micro text-fg-muted" data-inbox-ask-stale>
                    The task behind this ask has not changed in a while. It is
                    still answerable.
                  </p>
                {/if}
                {#if supersedesLink}
                  <p class="text-micro text-fg-muted">
                    Re-asked (supersedes
                    <a
                      class="ui-prose-link"
                      href={supersedesLink.href}
                      data-inbox-supersedes={supersedesLink.ref}
                      >{supersedesLink.label}</a
                    >)
                  </p>
                {/if}
                {#if askBody}
                  <MarkdownRenderer
                    source={askBody}
                    class="text-meta leading-relaxed text-fg [overflow-wrap:anywhere]"
                    organizationSlug={$page.params.organization}
                    workspaceSlug={$page.params.workspace}
                  />
                {/if}
                <InboxEvidence
                  evidence={askEvidence}
                  hrefFor={refHref}
                  labelFor={(ref) =>
                    work.find((item) => workKey(item) === ref)?.title ||
                    docTitles[ref] ||
                    ""}
                  onOpenDoc={(ref) =>
                    (docPanelRef = docPanelRef === ref ? "" : ref)}
                  openDocRef={docPanelRef}
                />
                {#if selected.responseError}
                  <!--
                    The send failed and nothing was recorded. The item is still
                    here, still unanswered, and says why — rather than sliding
                    back into the list minutes later with no explanation.
                  -->
                  <div
                    class="rounded-md border border-danger bg-danger-soft px-3 py-2"
                    role="alert"
                    data-inbox-response-error
                  >
                    <p class="text-meta text-fg">
                      <span class="font-medium text-danger-text"
                        >Your response was not sent.</span
                      >
                      {selected.responseError}
                    </p>
                    <p class="mt-1.5 flex flex-wrap items-center gap-3">
                      <button
                        class="ui-btn-secondary"
                        type="button"
                        onclick={() =>
                          void retryInboxResponse(selected.item?.id)}
                        >Retry send</button
                      >
                      <button
                        class="text-micro text-fg-muted hover:text-fg"
                        type="button"
                        onclick={() =>
                          dismissInboxResponseFailure(selected.item?.id)}
                        >Answer it again instead</button
                      >
                    </p>
                  </div>
                {/if}
                {#if needsResponse}
                  <InboxRespondPanel
                    bind:this={respondPanel}
                    kind={selected.category}
                    access={selected.access}
                    canDecideAccess={decidesAccess}
                    proposals={selected.responseProposals}
                    needsContext={supportsNeedsContext(selected.item)}
                    itemKey={selected.id}
                    bind:draft={reply}
                    {chosen}
                    {busy}
                    sendContext={() => ({
                      id: selected.id,
                      draft: reply,
                      mailbox,
                      /*
                       * Where this answer is going, decided now: the sender is
                       * bound to this workspace and this reader, and the flash
                       * is long enough for the reader to switch away from both.
                       */
                      binding: captureInboxResponseBinding(),
                    })}
                    onSend={(text, outcome, context) => {
                      /*
                       * The row the answer was chosen on, not whatever is on
                       * screen when it lands: a suggestion sends after a
                       * short flash, and the reader can move on inside it.
                       */
                      const row =
                        rows.find((entry) => entry.id === context?.id) ||
                        selected;
                      respondInbox(row, text, {
                        outcome,
                        proposal: row.responseProposals?.includes(text)
                          ? text
                          : "",
                        draft: context?.draft ?? reply,
                        from: context?.mailbox ?? mailbox,
                        binding: context?.binding,
                      });
                    }}
                    onAcknowledge={selected.access
                      ? null
                      : () => acknowledgeInbox(selected)}
                  >
                    {#snippet after()}
                      <a
                        class="ui-btn-secondary"
                        href={workspaceHref(
                          `/inbox/${encodeURIComponent(selected.item.id)}`,
                        )}>Open item</a
                      >
                    {/snippet}
                  </InboxRespondPanel>
                {:else if reminder}
                  <!--
                    Nobody answers a review reminder. Core drops it from every
                    inbox read once the report is revised, archived, trashed
                    or unpinned, so refreshing the panel is what closes it —
                    Reply and Acknowledge had nothing to act on.
                  -->
                  <div
                    class="space-y-3 rounded-md border border-line-subtle bg-bg-soft px-3 py-3"
                    data-inbox-reminder
                  >
                    <p class="text-meta text-fg-muted">
                      This closes itself when the panel is refreshed or
                      converted to a live one. There is nothing to answer.
                    </p>
                    <div class="flex flex-wrap gap-2">
                      {#if subjectHref(selected.subject)}
                        <a
                          class="ui-btn-primary"
                          href={subjectHref(selected.subject)}
                          data-inbox-shortcut="open">Open dashboard</a
                        >
                      {/if}
                      <!-- Every other branch offers this; without it the item
                           page is reachable only by typing its URL. -->
                      <a
                        class="ui-btn-secondary"
                        href={workspaceHref(
                          `/inbox/${encodeURIComponent(selected.item.id)}`,
                        )}>Open item</a
                      >
                    </div>
                  </div>
                {:else}
                  <div
                    class="space-y-1 rounded-md border border-line-subtle bg-bg-soft px-3 py-2"
                    data-inbox-answer
                  >
                    <p class="text-micro text-fg-muted">
                      {#if selectedSentBack}<span data-inbox-sent-back
                          >Sent back for context</span
                        >{#if selected.responder}
                          by <InboxActorName
                            name={selected.responder.name}
                            id={selected.responder.id}
                          />{/if}{:else if selected.responder?.id && selected.responder.id === $selectedActorId}You
                        answered{:else if selected.responder}Answered by <InboxActorName
                          name={selected.responder.name}
                          id={selected.responder.id}
                        />{:else}Answered{/if}{#if selected.item?.responded_at}{" "}<Time
                          value={selected.item.responded_at}
                        />{/if}
                    </p>
                    {#if selected.item?.response_text}
                      <MarkdownRenderer
                        source={selected.item.response_text}
                        class="text-meta text-fg [overflow-wrap:anywhere]"
                      />
                    {/if}
                  </div>
                  <InboxDelivery
                    model={askDelivery}
                    taskSummary={selected.subject?.summary ?? null}
                    taskTitle={selected.subject?.title ?? ""}
                    taskHref={subjectHref(selected.subject)}
                    loading={askDetailLoading}
                    {now}
                  />
                  <a
                    class="ui-btn-secondary inline-flex"
                    href={workspaceHref(
                      `/inbox/${encodeURIComponent(selected.item.id)}`,
                    )}>Open item</a
                  >
                {/if}
              </div>
            {:else if selected?.kind === "update"}
              {@const entries = updateEvents(selected.item)}
              <div class="space-y-4 p-4 sm:p-5">
                <div class="space-y-1">
                  <h2 class="text-subtitle text-fg [overflow-wrap:anywhere]">
                    {selected.title}
                  </h2>
                  <p class="text-meta text-fg-muted [overflow-wrap:anywhere]">
                    {selected.source}
                  </p>
                </div>
                {#if entries.length}
                  <ol class="space-y-2" data-inbox-update-events>
                    {#each entries as { event, described } (event.id)}
                      {@const link = eventHref(described)}
                      <li class="text-meta text-fg-muted">
                        <p class="[overflow-wrap:anywhere]">
                          <span class="font-medium text-fg"
                            >{described.actorId &&
                            described.actorId === $selectedActorId
                              ? "You"
                              : actorName(described.actorId) || "Someone"}</span
                          >
                          {#if link && described.objectTitle}
                            {@const parts = eventPhraseParts(described)}
                            {parts.before}<a
                              class="text-fg hover:underline"
                              href={link}>{parts.object}</a
                            >{parts.after}
                          {:else}
                            {eventPhrase(described)}
                          {/if}
                          <span class="text-fg-subtle">
                            ·
                            <Time value={described.ts} /></span
                          >
                        </p>
                        {#if described.excerpt}
                          <p
                            class="mt-0.5 line-clamp-2 border-l-2 border-line pl-2 text-micro [overflow-wrap:anywhere]"
                          >
                            {described.excerpt}
                          </p>
                        {/if}
                      </li>
                    {/each}
                  </ol>
                  {#if (Number(selected.count) || 0) > entries.length}
                    <p class="text-micro text-fg-subtle">
                      {Number(selected.count) - entries.length} more not shown.
                      <a
                        class="ui-prose-link"
                        href={`${workspaceHref("/events")}?q=${encodeURIComponent(selected.ref || "")}`}
                        >Full history</a
                      >
                    </p>
                  {/if}
                {/if}
                <div class="flex flex-wrap gap-2">
                  <button
                    class="ui-btn-secondary"
                    type="button"
                    data-inbox-shortcut="done"
                    aria-keyshortcuts="E"
                    title="Mark read (E)"
                    onclick={() => markUpdateRead(selected.item)}
                    >Mark read</button
                  >
                </div>
              </div>
            {:else if explicitId}
              <p class="p-6 text-meta text-fg-muted">
                {#if loading || (explicitId.startsWith("decision:") && decisionResolvedFor !== explicitId)}
                  <span role="status">Loading requested item…</span>
                {:else if truncated || actionError}
                  This item could not be loaded. Retry or open the task
                  directly.
                {:else}
                  This item is not in the loaded mailbox.
                {/if}
              </p>
            {/if}
          </div>
          {#if selected}
            {@const proposalCount =
              selected.kind === "inbox" && inboxItemNeedsResponse(selected.item)
                ? Math.min(
                    MAX_KEYED_PROPOSALS,
                    selected.responseProposals.length,
                  )
                : 0}
            {@const canOpen =
              selected.kind === "task" ||
              (selected.kind === "decision" &&
                !selectedDecision?.work_missing) ||
              (selected.kind === "inbox" &&
                Boolean(contextSubject && subjectHref(selected.subject)))}
            <p
              class="hidden flex-wrap items-center gap-x-3 gap-y-1 border-t border-line-subtle px-4 py-2 text-micro text-fg-subtle lg:flex"
              data-inbox-key-hints
            >
              {#if proposalCount}
                <span
                  ><kbd class="inbox-kbd"
                    >{proposalCount > 1 ? `1–${proposalCount}` : "1"}</kbd
                  > select, press again to send</span
                >
                <span><kbd class="inbox-kbd">R</kbd> reply</span>
                <span><kbd class="inbox-kbd">E</kbd> acknowledge</span>
              {:else if selected.kind === "update"}
                <span><kbd class="inbox-kbd">E</kbd> mark read</span>
              {/if}
              <span
                ><kbd class="inbox-kbd">J</kbd>/<kbd class="inbox-kbd">K</kbd> move</span
              >
              {#if canOpen}
                <span><kbd class="inbox-kbd">O</kbd> open</span>
              {/if}
              <button
                class="ml-auto hover:text-fg"
                type="button"
                onclick={() => (helpOpen = true)}
                ><kbd class="inbox-kbd">?</kbd> all shortcuts</button
              >
            </p>
          {/if}
        </section>
        <!--
          Evidence read beside the question. Below lg it is a sheet over the
          page rather than a third column: a phone has one column, and pushing
          the doc under a long ask body put it where nobody would look.
        -->
        <InboxDocPanel
          ref={docPanelRef}
          hrefFor={refHref}
          onClose={() => (docPanelRef = "")}
          onTitle={(ref, docTitle) =>
            (docTitles = { ...docTitles, [ref]: docTitle })}
          organizationSlug={$page.params.organization}
          workspaceSlug={$page.params.workspace}
        />
      {/if}
    </div>
  {/if}
  <KeyboardShortcutsDialog
    bind:open={helpOpen}
    shortcuts={inboxShortcutList()}
    title="Inbox shortcuts"
  />
  <InboxUndoToast onUndo={undoLastResponse} />
</WorkspacePageShell>

<style>
  .inbox-kbd {
    display: inline-block;
    min-width: 1rem;
    padding: 0 0.25rem;
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    font-family: var(--font-sans);
    font-weight: 500;
    font-size: 11px;
    line-height: 16px;
    text-align: center;
    color: var(--fg-muted);
  }
</style>
