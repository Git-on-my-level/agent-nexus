<script>
  import { humanActorIdSet } from "$lib/humanActors.js";
  import { onMount, tick, untrack } from "svelte";
  import { page } from "$app/stores";
  import { beforeNavigate, goto } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
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
    initializeAuthSession,
    isHumanWorkspacePrincipal,
  } from "$lib/authSession";
  import { restartSession } from "$lib/workspaceBootstrap";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { formatAbsoluteDateTime, formatTimestamp } from "$lib/formatDate";
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
  import { liveWorkspaceEvents } from "$lib/liveWorkspaceEvents.js";
  import { claimInboxCount, publishInboxCount } from "$lib/inboxCount.js";
  import {
    applyResponseOverlay,
    defaultNotifyMode,
    flushInboxResponse,
    hasPendingInboxResponse,
    inboxResponseOverlay,
    onInboxResponseCommitted,
    queueInboxResponse,
    stashInboxRestore,
    undoInboxResponse,
  } from "$lib/inboxResponseQueue.js";
  import { loadInboxSources, mergeInboxItems } from "$lib/inboxSources.js";
  import {
    inboxShortcutAction,
    inboxShortcutList,
    otherDialogOpen,
  } from "$lib/inboxShortcuts.js";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
  import DecisionPanel from "$lib/components/pm/DecisionPanel.svelte";
  import KeyboardShortcutsDialog from "$lib/components/KeyboardShortcutsDialog.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import InboxActorName from "$lib/components/inbox/InboxActorName.svelte";
  import InboxContextStrip from "$lib/components/inbox/InboxContextStrip.svelte";
  import InboxRespondPanel from "$lib/components/inbox/InboxRespondPanel.svelte";
  import InboxUndoToast from "$lib/components/inbox/InboxUndoToast.svelte";

  let decisions = $state([]);
  let actions = $state([]);
  let work = $state([]);
  let inboxItems = $state([]);
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
  let selectionRequest = 0;
  let ready = $state(false);
  let truncated = $state(false);
  let receiptsUnavailable = $state(false);
  let noticeElement = $state(null);
  let detailPane = $state(null);
  // After an action, keyboard focus lands on the outcome, not on <body>.
  $effect(() => {
    if (notice && noticeElement) noticeElement.focus();
  });
  let now = $state(Date.now());

  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
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
      inboxItems: applyResponseOverlay(inboxItems, $inboxResponseOverlay, now),
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
  let visible = $derived(filterMailbox(scoped, mailbox));
  let counts = $derived({
    "needs-you": filterMailbox(scoped, "needs-you").length,
    watching: filterMailbox(scoped, "watching").length,
    handled: filterMailbox(scoped, "handled").length,
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
    if (id) void untrack(() => loadSelected(id));
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
    return { title: subject.title, status: subject.phaseLabel };
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
    if (!subject?.ref) return "";
    const { prefix, id } = splitTypedRef(subject.ref);
    if (prefix === "card")
      return workspaceHref(`/tasks/${encodeURIComponent(subject.ref)}`);
    if (prefix === "document")
      return workspaceHref(`/docs/${encodeURIComponent(id)}`);
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
    const ticket = ++selectionRequest;
    try {
      let item = decisions.find((entry) => entry.id === id);
      if (!item) {
        item = await coreClient.getPmDecision(id);
        if (ticket !== selectionRequest) return;
        decisions = [...decisions.filter((entry) => entry.id !== id), item];
      }
      if (
        item?.action_id &&
        !actions.some((entry) => entry.id === item.action_id)
      ) {
        const receipt = await coreClient.getPmAction(item.action_id);
        if (ticket !== selectionRequest) return;
        actions = [
          ...actions.filter((entry) => entry.id !== receipt.id),
          receipt,
        ];
      }
    } catch (err) {
      if (ticket === selectionRequest) actionError = errorMessage(err);
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
  let loadErrorText = $state("");
  async function load({ quiet = false } = {}) {
    const ticket = ++requestId;
    if (!quiet) {
      loading = true;
      error = "";
      loadErrorText = "";
      actionError = "";
    }
    try {
      await initializeAuthSession({
        fetchFn: globalThis.fetch.bind(globalThis),
        workspaceSlug: $page.params.workspace,
        authDriver: "inbox",
      });
      const results = await loadInboxSources();
      if (ticket !== requestId) return;
      let nextError = "";
      // A refused session will refuse the retry too; offer sign-in instead.
      sessionExpired = results.some(
        (result) =>
          result.status === "rejected" && isSessionExpired(result.reason),
      );
      if (results[0].status === "fulfilled") {
        decisions = results[0].value.items || [];
      } else nextError = errorMessage(results[0].reason);
      // Lists follow cursors up to a bound. Counts drawn from a capped list
      // are lower bounds, and the reader must be told so rather than shown a total.
      truncated = results.some(
        (result) =>
          result.status === "fulfilled" &&
          (result.value?.has_more === true ||
            Boolean(result.value?.next_cursor)),
      );
      if (results[1].status === "fulfilled") {
        actions = results[1].value.items || [];
        receiptsUnavailable = false;
      } else if (sessionExpired) {
        // The receipts are not in doubt, the session is; keep the last
        // classification and let the banner say what to do.
        nextError = nextError || errorMessage(results[1].reason);
      } else {
        // Without receipts, an answered decision cannot be classified; say
        // so rather than quietly filing everything under Watching.
        receiptsUnavailable = true;
        nextError = nextError || errorMessage(results[1].reason);
      }
      if (results[2].status === "fulfilled") {
        work = results[2].value.work || [];
      } else {
        nextError = nextError || errorMessage(results[2].reason);
      }
      const openItems =
        results[3].status === "fulfilled" ? results[3].value.items || [] : [];
      const completedItems =
        results[4].status === "fulfilled" ? results[4].value.items || [] : [];
      if (!quiet || results[3].status === "fulfilled") {
        inboxItems = mergeInboxItems(openItems, completedItems);
      }
      if (results[3].status === "rejected") {
        nextError = nextError || errorMessage(results[3].reason);
      } else if (results[4].status === "rejected") {
        nextError = nextError || errorMessage(results[4].reason);
      }
      if (results[5].status === "fulfilled") {
        updates = results[5].value.groups || [];
      }
      if (nextError) {
        error = nextError;
        loadErrorText = nextError;
      } else if (quiet && loadErrorText && error === loadErrorText) {
        // The failure a live reload recovered from is no longer true.
        error = "";
        loadErrorText = "";
      }
    } catch (err) {
      if (ticket === requestId && !quiet) error = errorMessage(err);
    } finally {
      if (ticket === requestId) {
        loading = false;
        ready = true;
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
      contextEpoch += 1;
      void load({ quiet: true });
    }, LIVE_REFRESH_DELAY_MS);
  }

  async function recordAnswer(event) {
    event.preventDefault();
    if (!selectedDecision || busy || !answer.trim() || !choice) return;
    busy = true;
    error = "";
    try {
      const result = await coreClient.answerPmDecision(selectedDecision.id, {
        revision: selectedDecision.revision,
        approve: choice === "approve",
        text: answer.trim(),
      });
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
  function neighbourId(id) {
    const index = visible.findIndex((row) => row.id === id);
    if (index < 0) return "";
    return visible[index + 1]?.id || visible[index - 1]?.id || "";
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
    { acknowledge = false, proposal = "", outcome = "answered" } = {},
  ) {
    const item = row?.item;
    const body = String(text ?? "").trim();
    if (!item?.id || !body || busy) return;
    const who = row.requester?.name || "";
    const request = acknowledge
      ? { response_text: body, outcome: "acknowledged", notify_mode: "none" }
      : { response_text: body, outcome, notify_mode: defaultNotifyMode(item) };
    const next = neighbourId(row.id);
    const draft = reply;
    queueInboxResponse({
      itemId: item.id,
      request,
      message: acknowledge
        ? "Acknowledged"
        : who
          ? `Sent to ${who}`
          : "Response sent",
      restore: {
        origin: "pane",
        rowId: row.id,
        mailbox,
        reply: acknowledge || proposal ? draft : body,
        chosen: proposal,
      },
    });
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
        const button = paneElement(
          `[data-inbox-proposal="${shortcut.index}"]:not([disabled])`,
        );
        if (!button) return;
        button.click();
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
    publishInboxCount($page.params.workspace, count, truncated);
  });

  onMount(() => {
    void load();
    const releaseCount = claimInboxCount();
    const stopLive = liveWorkspaceEvents({
      client: coreClient,
      onChange: () => scheduleLiveRefresh(),
    });
    const stopCommitted = onInboxResponseCommitted(() => scheduleLiveRefresh());
    const timer = setInterval(() => {
      now = Date.now();
      refreshIfInFlight();
    }, 15_000);
    const onVisible = () => {
      if (!document.hidden) refreshIfInFlight();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      requestId++;
      selectionRequest++;
      clearInterval(timer);
      clearTimeout(liveTimer);
      stopLive();
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
      <a class="ui-btn-secondary" href={workspaceHref("/pm")}>Ask PM</a>
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
            >{counts[key]}{truncated ? "+" : ""}</span
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
    {#if truncated}
      <span class="ml-2 text-micro text-fg-subtle"
        >Not everything is loaded; the counts are lower bounds.</span
      >
    {/if}
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
  {#if loading && !rows.length}
    <p class="py-10 text-center text-meta text-fg-muted" role="status">
      Loading inbox…
    </p>
  {:else}
    {@const showDetail = Boolean(selectedId)}
    <div
      class="grid overflow-hidden rounded-md border border-line bg-panel lg:min-h-[30rem] {showDetail
        ? 'lg:grid-cols-[minmax(16rem,0.9fr)_minmax(0,1.4fr)]'
        : ''}"
    >
      <section
        class="min-w-0 border-line {showDetail ? 'lg:border-r' : ''} {explicitId
          ? 'hidden lg:block'
          : ''}"
        aria-label="Inbox list"
      >
        <ul class="divide-y divide-line-subtle">
          {#each visible as row (row.id)}
            {@const badge = inboxRowBadge(row, now)}
            {@const wait = row.mailbox === "needs-you" ? waitFor(row) : null}
            <li>
              <a
                class="flex h-[52px] min-w-0 flex-col justify-center gap-0.5 border-l-2 px-4 {selectedId ===
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
                    <time class="shrink-0 tabular-nums" datetime={row.time}
                      >{formatTimestamp(row.time)}</time
                    >
                  {/if}
                </div>
              </a>
            </li>
          {:else}
            <li class="px-5 py-10 text-center">
              {#if mailbox === "needs-you"}
                <p class="text-meta font-medium text-fg">
                  You're clear.{#if counts.watching}
                    <a
                      class="ui-prose-link"
                      href={href({ mailbox: "watching", item: "" })}
                      >{counts.watching}
                      {counts.watching === 1 ? "thing is" : "things are"} being watched.</a
                    >{/if}
                </p>
              {:else if mailbox === "watching"}
                <p class="text-meta font-medium text-fg">
                  Nothing is waiting on a source or a delivery.
                </p>
              {:else}
                <p class="text-meta font-medium text-fg">
                  Nothing handled yet. Answered decisions and dismissed items
                  land here.
                </p>
              {/if}
            </li>
          {/each}
        </ul>
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
                pmHref={workspaceHref("/pm")}
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
                    Blocked for <span
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
                  <a
                    class="ui-btn-primary"
                    href={`${workspaceHref("/pm")}?work_ref=${encodeURIComponent(selected.ref)}`}
                    >Ask PM about this</a
                  >
                  <a
                    class="ui-btn-secondary"
                    href={workspaceHref(taskDetailPath(taskItem))}
                    data-inbox-shortcut="open">Open task</a
                  >
                </div>
              </div>
            {:else if selected?.kind === "inbox"}
              {@const needsResponse = inboxItemNeedsResponse(selected.item)}
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
                  {:else}
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
                {#if selected.body}
                  <MarkdownRenderer
                    source={selected.body}
                    class="text-meta leading-relaxed text-fg [overflow-wrap:anywhere]"
                  />
                {/if}
                {#if needsResponse}
                  <InboxRespondPanel
                    kind={selected.category}
                    access={selected.access}
                    canDecideAccess={decidesAccess}
                    proposals={selected.responseProposals}
                    bind:draft={reply}
                    {chosen}
                    {busy}
                    onSend={(text, outcome) =>
                      respondInbox(selected, text, {
                        outcome,
                        proposal: selected.responseProposals.includes(text)
                          ? text
                          : "",
                      })}
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
                {:else}
                  <div
                    class="space-y-1 rounded-md border border-line-subtle bg-bg-soft px-3 py-2"
                    data-inbox-answer
                  >
                    <p class="text-micro text-fg-muted">
                      {#if selected.responder?.id && selected.responder.id === $selectedActorId}You
                        answered{:else if selected.responder}Answered by <InboxActorName
                          name={selected.responder.name}
                          id={selected.responder.id}
                        />{:else}Answered{/if}{#if selected.item?.responded_at}{" "}<time
                          datetime={selected.item.responded_at}
                          >{formatTimestamp(selected.item.responded_at)}</time
                        >{/if}
                    </p>
                    {#if selected.item?.response_text}
                      <MarkdownRenderer
                        source={selected.item.response_text}
                        class="text-meta text-fg [overflow-wrap:anywhere]"
                      />
                    {/if}
                  </div>
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
                            <time datetime={described.ts}
                              >{formatTimestamp(described.ts)}</time
                            ></span
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
                This item is not in the loaded mailbox.
              </p>
            {/if}
          </div>
          {#if selected}
            {@const proposalCount =
              selected.kind === "inbox" && inboxItemNeedsResponse(selected.item)
                ? Math.min(5, selected.responseProposals.length)
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
                  > send</span
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
