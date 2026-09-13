<script>
  import { onMount, untrack } from "svelte";
  import { page } from "$app/stores";
  import { beforeNavigate } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
  import {
    actorDisplayLabel,
    actorRegistry,
    principalRegistry,
    selectedActorId,
  } from "$lib/actorSession";
  import { initializeAuthSession } from "$lib/authSession";
  import { restartSession } from "$lib/workspaceBootstrap";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { formatTimestamp } from "$lib/formatDate";
  import {
    errorMessage,
    humanizeInstants,
    isNexusOwned,
    isSessionExpired,
    taskDetailPath,
    workKey,
  } from "$lib/pm/presentation.js";
  import { navIconPath } from "$lib/icons.js";
  import { openCommandPalette } from "$lib/stores/commandPalette.js";
  import { INBOX_CATEGORY_LABELS } from "$lib/inboxUtils.js";
  import {
    INBOX_MAILBOXES,
    buildInboxRows,
    filterMailbox,
    inboxItemNeedsResponse,
    inboxRowBadge,
  } from "$lib/inboxMailbox.js";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
  import DecisionPanel from "$lib/components/pm/DecisionPanel.svelte";

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
  let requestId = 0;
  let selectionRequest = 0;
  let ready = $state(false);
  let truncated = $state(false);
  let receiptsUnavailable = $state(false);
  let noticeElement = $state(null);
  // After an action, keyboard focus lands on the outcome, not on <body>.
  $effect(() => {
    if (notice && noticeElement) noticeElement.focus();
  });
  let now = $state(Date.now());

  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let mailbox = $derived($page.url.searchParams.get("mailbox") || "needs-you");
  let selectedId = $derived($page.url.searchParams.get("item") || "");

  let rows = $derived(
    buildInboxRows({
      decisions,
      actions,
      receiptsUnavailable,
      work,
      inboxItems,
      updates,
      now,
      currentActorId: $selectedActorId || "",
    }),
  );
  let visible = $derived(filterMailbox(rows, mailbox));
  let counts = $derived({
    "needs-you": filterMailbox(rows, "needs-you").length,
    watching: filterMailbox(rows, "watching").length,
    handled: filterMailbox(rows, "handled").length,
  });
  let selected = $derived(
    selectedId
      ? rows.find((row) => row.id === selectedId) || null
      : visible[0] || null,
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

  let previousSelectedId = $state("");
  $effect(() => {
    const id = selected?.id || "";
    if (id === previousSelectedId) return;
    previousSelectedId = id;
    untrack(() => {
      answer = "";
      choice = "";
      reply = "";
      notice = "";
      supersededHref = "";
    });
  });
  $effect(() => {
    const itemId = selectedId;
    if (!ready || !itemId?.startsWith("decision:")) return;
    const id = itemId.slice("decision:".length);
    if (id) void untrack(() => loadSelected(id));
  });

  function href(changes) {
    const params = new URLSearchParams($page.url.searchParams);
    for (const [key, value] of Object.entries(changes)) {
      if (value) params.set(key, value);
      else params.delete(key);
    }
    return `${workspaceHref("/inbox")}?${params}`;
  }

  /**
   * The attention surface must not hide an obligation on page two. Follow
   * cursors up to a bound; past it, say so instead of claiming completeness.
   */
  async function listAllPages(fetchPage, key, maxPages = 8) {
    const collected = [];
    let cursor;
    let more = false;
    for (let page = 0; page < maxPages; page += 1) {
      const result = await fetchPage(cursor);
      collected.push(...(Array.isArray(result?.[key]) ? result[key] : []));
      cursor = result?.next_cursor || "";
      more = Boolean(cursor) || result?.has_more === true;
      if (!cursor) break;
    }
    return { [key]: collected, has_more: more && Boolean(cursor) };
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
    if (busy) {
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

  async function load() {
    const ticket = ++requestId;
    loading = true;
    error = "";
    actionError = "";
    try {
      await initializeAuthSession({
        fetchFn: globalThis.fetch.bind(globalThis),
        workspaceSlug: $page.params.workspace,
        authDriver: "inbox",
      });
      const results = await Promise.allSettled([
        listAllPages(
          (cursor) => coreClient.listPmDecisions({ limit: 50, cursor }),
          "items",
        ),
        listAllPages(
          (cursor) => coreClient.listPmActions({ limit: 50, cursor }),
          "items",
        ),
        listAllPages(
          (cursor) => coreClient.listWork({ limit: 50, cursor }),
          "work",
        ),
        coreClient.listInboxItems({ status: "open", limit: 50 }),
        coreClient.listInboxItems({ status: "completed", limit: 50 }),
        coreClient.getHomeUnread(),
      ]);
      // A refused session will refuse the retry too; offer sign-in instead.
      sessionExpired = results.some(
        (result) =>
          result.status === "rejected" && isSessionExpired(result.reason),
      );
      if (ticket !== requestId) return;
      if (results[0].status === "fulfilled") {
        decisions = results[0].value.items || [];
      } else error = errorMessage(results[0].reason);
      // Each list is one page. Counts drawn from partial pages are lower
      // bounds, and the reader must be told so rather than shown a total.
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
        error = error || errorMessage(results[1].reason);
      } else {
        // Without receipts, an answered decision cannot be classified; say
        // so rather than quietly filing everything under Watching.
        receiptsUnavailable = true;
        error = error || errorMessage(results[1].reason);
      }
      if (results[2].status === "fulfilled") {
        work = results[2].value.work || [];
      } else {
        error = error || errorMessage(results[2].reason);
      }
      const openItems =
        results[3].status === "fulfilled" ? results[3].value.items || [] : [];
      const completedItems =
        results[4].status === "fulfilled" ? results[4].value.items || [] : [];
      inboxItems = [
        ...new Map(
          [...openItems, ...completedItems]
            .filter((item) => item?.id)
            .map((item) => [item.id, item]),
        ).values(),
      ];
      if (results[3].status === "rejected") {
        error = error || errorMessage(results[3].reason);
      } else if (results[4].status === "rejected") {
        error = error || errorMessage(results[4].reason);
      }
      if (results[5].status === "fulfilled") {
        updates = results[5].value.groups || [];
      }
    } catch (err) {
      if (ticket === requestId) error = errorMessage(err);
    } finally {
      if (ticket === requestId) {
        loading = false;
        ready = true;
      }
    }
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
      decisions = decisions.map((item) =>
        item.id === result.id ? result : item,
      );
      const approved = choice === "approve";
      answer = "";
      choice = "";
      notice = approved ? "Approved." : "Declined.";
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

  async function dismissInbox(item) {
    if (!item?.id || busy) return;
    busy = true;
    error = "";
    try {
      await coreClient.respondInboxItem(item.id, {
        response_text: "Dismissed from inbox",
        notify_mode: "none",
      });
      inboxItems = inboxItems.map((entry) =>
        entry.id === item.id
          ? {
              ...entry,
              status: "completed",
              responded_at: new Date().toISOString(),
            }
          : entry,
      );
      notice = "Dismissed from inbox only.";
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }

  async function markUpdateRead(group) {
    const ref = String(group?.group_ref ?? "").trim();
    if (!ref) return;
    try {
      await coreClient.markHomeRead({ group_ref: ref });
      updates = updates.filter((entry) => entry.group_ref !== ref);
    } catch (err) {
      error = errorMessage(err);
    }
  }

  /**
   * Sends one response and moves the row to Handled, without leaving the pane.
   * Same call the standalone item route makes; the proposals are the whole
   * point of the item, so they belong where the item is read.
   */
  async function respondInbox(item, text) {
    const body = String(text ?? "").trim();
    if (!item?.id || !body || busy) return;
    busy = true;
    error = "";
    try {
      await coreClient.respondInboxItem(item.id, {
        response_text: body,
        notify_mode: "none",
      });
      inboxItems = inboxItems.map((entry) =>
        entry.id === item.id
          ? {
              ...entry,
              status: "completed",
              responded_at: new Date().toISOString(),
              response_text: body,
            }
          : entry,
      );
      reply = "";
      notice = "Response sent.";
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

  // A receipt that is still moving (pending, sending, unknown) is refreshed
  // on its own; nothing else on the page changes without the reader.
  const RECEIPT_IN_FLIGHT = new Set(["pending_delivery", "sending", "unknown"]);
  function refreshIfInFlight() {
    if (busy || !action || !RECEIPT_IN_FLIGHT.has(String(action.status)))
      return;
    void refreshReceipt();
  }
  onMount(() => {
    void load();
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
      document.removeEventListener("visibilitychange", onVisible);
    };
  });
</script>

<svelte:window
  onbeforeunload={(event) => {
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
      <button class="ui-btn-secondary" onclick={load} disabled={loading || busy}
        >{loading ? "Loading inbox…" : "Reload"}</button
      >
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
    <div
      class="grid min-h-[30rem] overflow-hidden rounded-md border border-line bg-panel lg:grid-cols-[minmax(16rem,0.9fr)_minmax(0,1.4fr)]"
    >
      <section
        class="min-w-0 border-line lg:border-r {selectedId
          ? 'hidden lg:block'
          : ''}"
        aria-label="Inbox list"
      >
        <ul class="divide-y divide-line-subtle">
          {#each visible as row (row.id)}
            {@const badge = inboxRowBadge(row, now)}
            <li>
              <a
                class="flex h-[52px] min-w-0 flex-col justify-center gap-0.5 border-l-2 px-4 {selected?.id ===
                row.id
                  ? 'border-accent bg-bg-soft'
                  : 'border-transparent hover:bg-panel-hover'}"
                href={href({ item: row.id })}
                data-inbox-row={row.id}
                data-testid={row.kind === "inbox" && row.item?.id
                  ? `inbox-row-${row.item.id}`
                  : `inbox-row-${row.id}`}
                aria-current={selected?.id === row.id ? "page" : undefined}
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
                    >{row.source || row.kind}{#if row.requesterLabel}{" "}· from {row.requesterLabel}{/if}</span
                  >
                  {#if row.time}
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
      <section
        class="min-w-0 {selectedId ? '' : 'hidden lg:block'}"
        aria-label="Selected inbox item"
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
              href={href({ item: visible[selectedIndex - 1].id })}>Previous</a
            >
          {/if}
          {#if selectedIndex >= 0 && selectedIndex < visible.length - 1}
            <a
              class="text-fg-muted hover:text-fg"
              href={href({ item: visible[selectedIndex + 1].id })}>Next</a
            >
          {/if}

          <span class="ml-auto shrink-0 tabular-nums text-fg-subtle"
            >{selectedIndex >= 0
              ? `${selectedIndex + 1} of ${visible.length}`
              : ""}</span
          >
        </div>
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
          <div class="space-y-4 p-4 sm:p-5">
            <h2 class="text-subtitle text-fg">{selected.title}</h2>
            {#if blockers.length}
              <div>
                <p class="ui-label">Blocked by</p>
                <ul class="space-y-1 text-meta text-fg">
                  {#each blockers as blocker}
                    <li>{blocker}</li>
                  {/each}
                </ul>
              </div>
            {/if}
            {#if taskItem?.next_action}
              <p class="text-meta text-fg">
                {#if taskItem.next_actor}<span class="text-fg-muted"
                    >{taskItem.next_actor} —
                  </span>{/if}{taskItem.next_action}
              </p>
            {/if}
            <p class="text-micro text-fg-muted">
              Last checked {taskLastChecked(
                taskItem,
              )}{#if taskItem?.refresh?.last_error?.message}
                <span class="text-warn-text">
                  · {humanizeInstants(
                    taskItem.refresh.last_error.message,
                  )}</span
                >{/if}
            </p>
            <div class="flex flex-wrap gap-2">
              <a
                class="ui-btn-primary"
                href={`${workspaceHref("/pm")}?work_ref=${encodeURIComponent(selected.ref)}`}
                >Ask PM about this</a
              >
              <a
                class="ui-btn-secondary"
                href={workspaceHref(taskDetailPath(taskItem))}>Open task</a
              >
            </div>
          </div>
        {:else if selected?.kind === "inbox"}
          <div class="space-y-4 p-4 sm:p-5">
            <div class="flex flex-wrap items-center gap-2">
              {#if inboxKindLabel(selected)}
                <span class="ui-label mb-0">{inboxKindLabel(selected)}</span>
              {/if}
              {#if selected.severity}
                <SignalBadge
                  tone={String(selected.severity).toLowerCase() === "critical"
                    ? "danger"
                    : "warn"}>{selected.severity}</SignalBadge
                >
              {/if}
              {#if selected.requesterLabel}
                <span class="text-micro text-fg-muted"
                  >from {selected.requesterLabel}</span
                >
              {/if}
            </div>
            <h2 class="text-subtitle text-fg">{selected.title}</h2>
            {#if selected.body}
              <p class="whitespace-pre-wrap text-meta leading-relaxed text-fg">
                {selected.body}
              </p>
            {/if}
            {#if inboxItemNeedsResponse(selected.item)}
              {#if selected.responseProposals.length}
                <div>
                  <p class="ui-label">Send one of these</p>
                  <div class="flex flex-wrap gap-2">
                    {#each selected.responseProposals as proposal}
                      <button
                        class="ui-btn-secondary"
                        onclick={() => respondInbox(selected.item, proposal)}
                        disabled={busy}
                        type="button">{proposal}</button
                      >
                    {/each}
                  </div>
                </div>
              {/if}
              <form
                class="space-y-2"
                onsubmit={(event) => {
                  event.preventDefault();
                  void respondInbox(selected.item, reply);
                }}
              >
                <label class="ui-label" for="inbox-reply">Reply</label>
                <textarea
                  id="inbox-reply"
                  class="ui-input min-h-20"
                  bind:value={reply}
                  placeholder="Reply…"
                ></textarea>
                <div class="flex flex-wrap gap-2">
                  <button
                    class="ui-btn-primary"
                    type="submit"
                    disabled={busy || !reply.trim()}>Send reply</button
                  >
                  <a
                    class="ui-btn-secondary"
                    href={workspaceHref(
                      `/inbox/${encodeURIComponent(selected.item.id)}`,
                    )}>Open item</a
                  >
                  <button
                    class="ui-btn-secondary"
                    onclick={() => dismissInbox(selected.item)}
                    disabled={busy}
                    type="button">Dismiss from Inbox</button
                  >
                </div>
              </form>
            {:else}
              <a
                class="ui-btn-secondary inline-flex"
                href={workspaceHref(
                  `/inbox/${encodeURIComponent(selected.item.id)}`,
                )}>Open item</a
              >
            {/if}
          </div>
        {:else if selected?.kind === "update"}
          <div class="space-y-4 p-4 sm:p-5">
            <h2 class="text-subtitle text-fg">{selected.title}</h2>
            <p class="text-meta text-fg-muted">
              {selected.count || 0} updates since you last looked{#if selected.item?.newest_event?.summary}.
                Newest: {selected.item.newest_event.summary}{/if}
            </p>
            <div class="flex flex-wrap gap-2">
              <a
                class="ui-btn-secondary"
                href={`${workspaceHref("/events")}?q=${encodeURIComponent(selected.ref || "")}`}
                >Open in audit log</a
              >
              <button
                class="ui-btn-secondary"
                onclick={() => markUpdateRead(selected.item)}>Mark read</button
              >
            </div>
          </div>
        {:else if selectedId || visible.length}
          <p class="p-6 text-meta text-fg-muted">
            {selectedId
              ? "This item is not in the loaded mailbox."
              : "Select a row."}
          </p>
        {/if}
      </section>
    </div>
  {/if}
</WorkspacePageShell>
