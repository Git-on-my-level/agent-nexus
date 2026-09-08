<script>
  import { onMount, untrack } from "svelte";
  import { page } from "$app/stores";
  import { beforeNavigate } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
  import { initializeAuthSession } from "$lib/authSession";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { formatTimestamp } from "$lib/formatDate";
  import {
    errorMessage,
    receiptSignal,
    safeSourceHref,
  } from "$lib/pm/presentation.js";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
  let decisions = $state([]),
    actions = $state([]),
    loading = $state(true),
    busy = $state(false),
    error = $state(""),
    actionError = $state(""),
    notice = $state(""),
    partial = $state(false),
    answer = $state(""),
    choice = $state("");
  let decisionsCursor = $state("");
  let actionsCursor = $state("");
  let requestId = 0;
  let selectionRequest = 0;
  let ready = $state(false);
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let workRef = $derived($page.url.searchParams.get("work_ref") || "");
  let mailbox = $derived($page.url.searchParams.get("mailbox") || "needs-you");
  let selectedId = $derived($page.url.searchParams.get("decision") || "");
  let scoped = $derived(
    decisions.filter((item) => !workRef || item.work_ref === workRef),
  );
  let visible = $derived(
    scoped.filter(
      (item) =>
        mailbox === "all" ||
        (mailbox === "watching"
          ? item.status !== "awaiting_answer"
          : item.status === "awaiting_answer"),
    ),
  );
  let selected = $derived(
    decisions.find(
      (item) =>
        item.id === selectedId && (!workRef || item.work_ref === workRef),
    ) || (!selectedId ? visible[0] : null),
  );
  let action = $derived(
    actions.find(
      (item) =>
        item.id === selected?.action_id || item.decision_id === selected?.id,
    ),
  );
  let selectedIndex = $derived(
    visible.findIndex((item) => item.id === selected?.id),
  );
  $effect(() => {
    const id = selected?.id;
    if (id) {
      answer = "";
      choice = "";
      notice = "";
    }
  });
  $effect(() => {
    const id = selectedId;
    if (ready && id) void untrack(() => loadSelected(id));
  });
  async function loadSelected(id) {
    const ticket = ++selectionRequest;
    try {
      let item = decisions.find((entry) => entry.id === id);
      if (!item) {
        item = await coreClient.getPmDecision(id);
        if (ticket !== selectionRequest || selectedId !== id) return;
        decisions = [...decisions.filter((entry) => entry.id !== id), item];
      }
      if (
        item?.action_id &&
        !actions.some((entry) => entry.id === item.action_id)
      ) {
        const receipt = await coreClient.getPmAction(item.action_id);
        if (ticket !== selectionRequest || selectedId !== id) return;
        actions = [
          ...actions.filter((entry) => entry.id !== receipt.id),
          receipt,
        ];
      }
    } catch (err) {
      if (ticket === selectionRequest && selectedId === id)
        actionError = errorMessage(err);
    }
  }
  beforeNavigate(({ cancel }) => {
    if (busy) {
      cancel();
      return;
    }
    if (
      answer.trim() &&
      !busy &&
      !window.confirm("Leave without recording this decision?")
    )
      cancel();
  });
  function href(changes) {
    const params = new URLSearchParams($page.url.searchParams);
    for (const [key, value] of Object.entries(changes)) {
      if (value) params.set(key, value);
      else params.delete(key);
    }
    return `${workspaceHref("/decisions")}?${params}`;
  }
  async function load() {
    const ticket = ++requestId;
    loading = true;
    error = "";
    actionError = "";
    try {
      await initializeAuthSession({
        fetchFn: globalThis.fetch.bind(globalThis),
        workspaceSlug: $page.params.workspace,
        authDriver: "pm-decisions",
      });
      const results = await Promise.allSettled([
        coreClient.listPmDecisions({ limit: 50 }),
        coreClient.listPmActions({ limit: 50 }),
      ]);
      if (ticket !== requestId) return;
      if (results[0].status === "fulfilled") {
        decisions = results[0].value.items || [];
        partial = Boolean(results[0].value.has_more);
        decisionsCursor = results[0].value.next_cursor || "";
      } else error = errorMessage(results[0].reason);
      if (results[1].status === "fulfilled") {
        actions = results[1].value.items || [];
        partial ||= Boolean(results[1].value.has_more);
        actionsCursor = results[1].value.next_cursor || "";
      } else actionError = errorMessage(results[1].reason);
    } catch (err) {
      if (ticket === requestId) error = errorMessage(err);
    } finally {
      if (ticket === requestId) {
        loading = false;
        ready = true;
      }
    }
  }
  async function loadMore() {
    if (loading) return;
    const ticket = requestId;
    loading = true;
    error = "";
    actionError = "";
    const results = await Promise.allSettled([
      decisionsCursor
        ? coreClient.listPmDecisions({ limit: 50, cursor: decisionsCursor })
        : Promise.resolve(null),
      actionsCursor
        ? coreClient.listPmActions({ limit: 50, cursor: actionsCursor })
        : Promise.resolve(null),
    ]);
    if (ticket !== requestId) return;
    if (results[0].status === "fulfilled" && results[0].value) {
      decisions = [
        ...new Map(
          [...decisions, ...results[0].value.items].map((item) => [
            item.id,
            item,
          ]),
        ).values(),
      ];
      decisionsCursor = results[0].value.next_cursor || "";
    } else if (results[0].status === "rejected")
      error = errorMessage(results[0].reason);
    if (results[1].status === "fulfilled" && results[1].value) {
      actions = [
        ...new Map(
          [...actions, ...results[1].value.items].map((item) => [
            item.id,
            item,
          ]),
        ).values(),
      ];
      actionsCursor = results[1].value.next_cursor || "";
    } else if (results[1].status === "rejected")
      actionError = errorMessage(results[1].reason);
    partial = Boolean(decisionsCursor || actionsCursor);
    loading = false;
  }
  async function recordAnswer(event) {
    event.preventDefault();
    if (!selected || busy || !answer.trim() || !choice) return;
    busy = true;
    error = "";
    try {
      const result = await coreClient.answerPmDecision(selected.id, {
        revision: selected.revision,
        approve: choice === "approve",
        text: answer.trim(),
      });
      decisions = decisions.map((item) =>
        item.id === result.id ? result : item,
      );
      answer = "";
      choice = "";
      notice = "Answer recorded. Delivery and outcome are tracked separately.";
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
      }
    } catch (err) {
      error = errorMessage(err);
    } finally {
      busy = false;
    }
  }
  async function deliver() {
    if (!selected || busy) return;
    busy = true;
    error = "";
    notice = "";
    try {
      const result = await coreClient.dispatchPmDecision(selected.id);
      actions = [...actions.filter((item) => item.id !== result.id), result];
      notice =
        "Delivery request recorded. Inspect the receipt for confirmed state.";
    } catch (err) {
      error = errorMessage(err);
      await refreshReceipt();
    } finally {
      busy = false;
    }
  }
  async function refreshReceipt() {
    if (!selected?.action_id) return;
    try {
      const result = await coreClient.getPmAction(selected.action_id);
      actions = [...actions.filter((item) => item.id !== result.id), result];
    } catch (err) {
      actionError = errorMessage(err);
    }
  }
  async function reconcile() {
    if (!action || busy) return;
    busy = true;
    actionError = "";
    try {
      const result = await coreClient.reconcilePmAction(action.id);
      actions = actions.map((item) => (item.id === result.id ? result : item));
      notice = "Read-back finished. Receipt state is shown below.";
    } catch (err) {
      actionError = errorMessage(err);
    } finally {
      busy = false;
    }
  }
  onMount(() => {
    void load();
    return () => {
      requestId++;
      selectionRequest++;
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
<svelte:head><title>Decisions & receipts · Agent Nexus</title></svelte:head>
<WorkspacePageShell>
  <WorkspacePageHeader title="Decisions & receipts"
    >{#snippet subtitle()}What you decided, where it was delivered, and what
      actually changed.{/snippet}{#snippet actions()}<a
        class="ui-btn-secondary"
        href={workspaceHref("/pm")}>Ask PM</a
      ><button
        class="ui-btn-secondary"
        onclick={load}
        disabled={loading || busy}
        >{loading ? "Loading decisions…" : "Reload"}</button
      >{/snippet}</WorkspacePageHeader
  >
  <nav class="flex flex-wrap items-center gap-2" aria-label="Decision mailbox">
    {#each [["needs-you", "Needs you"], ["watching", "Watching"], ["all", "All decisions"]] as [key, title]}<a
        class="rounded-md px-3 py-2 text-meta {mailbox === key
          ? 'bg-panel text-fg'
          : 'text-fg-muted hover:bg-panel-hover'}"
        href={href({ mailbox: key, decision: "" })}
        aria-current={mailbox === key ? "page" : undefined}
        >{title}<span class="ml-2 text-micro text-fg-muted"
          >{scoped.filter(
            (item) =>
              key === "all" ||
              (key === "watching"
                ? item.status !== "awaiting_answer"
                : item.status === "awaiting_answer"),
          ).length}</span
        ></a
      >{/each}{#if workRef}<span class="ml-auto text-micro text-fg-muted"
        >{workRef}</span
      ><a
        class="text-micro text-accent-text"
        href={href({ work_ref: "", decision: "" })}>Clear work filter</a
      >{/if}
  </nav>
  {#if error}<StateError
      message={error}
      onretry={load}
      retrying={loading}
    />{/if}
  {#if partial}<p class="rounded-md bg-warn-soft p-3 text-micro text-warn-text">
      This is a partial history. Counts describe loaded records; older decisions
      or receipts may not be included.
    </p>{/if}
  {#if notice}<p
      class="rounded-md bg-bg-soft p-3 text-meta text-fg"
      role="status"
    >
      {notice}
    </p>{/if}
  {#if loading && !decisions.length}<p
      class="py-10 text-fg-muted"
      role="status"
    >
      Loading decisions and delivery receipts…
    </p>{:else}
    <div
      class="grid min-h-[32rem] overflow-hidden rounded-md border border-line bg-panel lg:grid-cols-[minmax(16rem,0.8fr)_minmax(0,1.5fr)]"
    >
      <section
        class="border-line lg:border-r {selectedId ? 'hidden lg:block' : ''}"
        aria-label="Decision list"
      >
        <h2
          class="border-b border-line px-4 py-3 text-micro font-semibold text-fg-muted"
        >
          {mailbox === "watching"
            ? "FOLLOW-THROUGH"
            : mailbox === "all"
              ? "DECISION HISTORY"
              : "AWAITING YOUR ANSWER"}
        </h2>
        <ul class="divide-y divide-line">
          {#each visible as item (item.id)}{@const state = receiptSignal(
              item.status,
            )}
            <li>
              <a
                class="block border-l-2 px-4 py-3 {selected?.id === item.id
                  ? 'border-accent bg-bg-soft'
                  : 'border-transparent hover:bg-panel-hover'}"
                href={href({ decision: item.id })}
                aria-current={selected?.id === item.id ? "page" : undefined}
                ><div
                  class="flex justify-between gap-2 text-micro text-fg-muted"
                >
                  <span class="truncate">{item.work_ref}</span><time
                    datetime={item.created_at}
                    >{formatTimestamp(item.created_at)}</time
                  >
                </div>
                <p
                  class="mt-2 line-clamp-2 break-words text-meta font-semibold text-fg"
                >
                  {item.instruction}
                </p>
                <div class="mt-2">
                  <SignalBadge tone={state.tone}>{state.label}</SignalBadge>
                </div></a
              >
            </li>{:else}<li class="px-5 py-10">
              <h3 class="text-meta font-semibold text-fg">
                {mailbox === "needs-you"
                  ? "No decisions awaiting your answer"
                  : "No decisions in this view"}
              </h3>
              <p class="mt-2 text-micro text-fg-muted">
                {mailbox === "needs-you"
                  ? "Answered decisions remain in Watching until their delivery and outcome are established."
                  : "Change the mailbox or work filter to inspect other decisions."}
              </p>
              <a
                class="mt-3 inline-block text-micro text-accent-text hover:underline"
                href={href({ mailbox: "watching", decision: "" })}
                >View follow-through</a
              >
            </li>{/each}
        </ul>
      </section>
      <section
        class="min-w-0 {selectedId ? '' : 'hidden lg:block'}"
        aria-label="Selected decision"
      >
        <div
          class="flex items-center gap-3 border-b border-line px-4 py-2.5 text-micro"
        >
          <a class="text-accent-text lg:hidden" href={href({ decision: "" })}
            >← Decision list</a
          >{#if selectedIndex > 0}<a
              class="text-fg-muted hover:text-fg"
              href={href({ decision: visible[selectedIndex - 1].id })}
              >Previous</a
            >{/if}{#if selectedIndex >= 0 && selectedIndex < visible.length - 1}<a
              class="text-fg-muted hover:text-fg"
              href={href({ decision: visible[selectedIndex + 1].id })}>Next</a
            >{/if}<span class="ml-auto text-fg-muted"
            >{selectedIndex >= 0
              ? `${selectedIndex + 1} of ${visible.length}`
              : "Decision"}</span
          >
        </div>
        {#if selected}<div class="space-y-5 p-4 sm:p-5">
            <header>
              <a
                class="text-micro text-accent-text hover:underline"
                href={workspaceHref(
                  `/work/${encodeURIComponent(selected.work_ref)}`,
                )}>{selected.work_ref}</a
              >
              <h2
                class="mt-2 whitespace-pre-wrap break-words text-subtitle font-semibold text-fg"
              >
                {selected.instruction}
              </h2>
              <dl class="mt-4 grid gap-3 text-micro sm:grid-cols-2">
                <div>
                  <dt class="text-fg-muted">Authorized scope requested</dt>
                  <dd class="mt-1 break-words text-fg">
                    {selected.scope || "Not established"}
                  </dd>
                </div>
                <div>
                  <dt class="text-fg-muted">Target source revision</dt>
                  <dd class="mt-1 break-all font-mono text-fg">
                    {selected.target_revision || "Not established"}
                  </dd>
                </div>
              </dl>
            </header>
            {#if selected.status === "awaiting_answer"}<form
                class="space-y-3 border-t border-line pt-4"
                onsubmit={recordAnswer}
              >
                <fieldset>
                  <legend class="text-meta font-semibold text-fg"
                    >Your decision</legend
                  >
                  <div class="mt-2 flex flex-wrap gap-4 text-meta text-fg">
                    <label class="flex items-center gap-2"
                      ><input
                        type="radio"
                        name="approval"
                        value="approve"
                        bind:group={choice}
                        required
                      />Authorize this scope</label
                    ><label class="flex items-center gap-2"
                      ><input
                        type="radio"
                        name="approval"
                        value="reject"
                        bind:group={choice}
                        required
                      />Decline</label
                    >
                  </div>
                </fieldset>
                <label class="block text-micro text-fg-muted"
                  >Exact response<textarea
                    class="ui-input mt-1"
                    bind:value={answer}
                    rows="4"
                    required
                    maxlength="16000"
                    placeholder="State your decision and any boundaries…"
                  ></textarea></label
                >
                <p class="text-micro text-fg-muted">
                  An answer records intent. Delivery rechecks the source
                  revision, and verification requires read-back evidence.
                </p>
                <button
                  class="ui-btn-primary"
                  type="submit"
                  disabled={busy || !choice || !answer.trim()}
                  >{busy ? "Recording answer…" : "Record decision"}</button
                >
              </form>{:else}<section class="border-t border-line pt-4">
                <h3 class="text-meta font-semibold text-fg">Recorded answer</h3>
                <p
                  class="mt-2 whitespace-pre-wrap break-words text-meta text-fg-muted"
                >
                  {selected.answer || "No answer text recorded"}
                </p>
                {#if selected.answered_by}<p
                    class="mt-2 text-micro text-fg-muted"
                  >
                    Answered by {selected.answered_by}
                  </p>{/if}
              </section>{/if}
            <section class="border-t border-line pt-4">
              <h3 class="text-meta font-semibold text-fg">
                Delivery and outcome
              </h3>
              {#if actionError}<StateError
                  message={actionError}
                  onretry={refreshReceipt}
                />{/if}
              {#if action}{@const verified =
                  action.status === "verified" &&
                  action.receipt?.independently_verified ===
                    true}{@const state = receiptSignal(
                  action.status === "verified" && !verified
                    ? "source_reported"
                    : action.status,
                )}
                <div class="mt-3">
                  <SignalBadge tone={state.tone}>{state.label}</SignalBadge>
                </div>
                <p class="mt-2 text-meta text-fg-muted">
                  {verified
                    ? "The receipt includes independent outcome verification."
                    : "Delivery or acknowledgement does not establish the intended outcome."}
                </p>
                {#if action.receipt?.detail}<p
                    class="mt-2 whitespace-pre-wrap break-words text-meta text-fg"
                  >
                    {action.receipt.detail}
                  </p>{/if}{#if safeSourceHref(action.receipt?.url)}<a
                    class="mt-2 inline-block text-meta text-accent-text hover:underline"
                    href={safeSourceHref(action.receipt.url)}
                    target="_blank"
                    rel="noreferrer">Open authoritative receipt ↗</a
                  >{/if}{#if action.receipt?.external_id}<p
                    class="mt-2 break-words text-micro text-fg-muted"
                  >
                    Source action: {action.receipt.external_id}
                  </p>{/if}
                {#if action.receipt?.evidence_refs?.length}<ul
                    class="mt-2 space-y-1 text-micro text-fg-muted"
                  >
                    {#each action.receipt.evidence_refs as ref}<li
                        class="break-all"
                      >
                        {ref}
                      </li>{/each}
                  </ul>{/if}
                <div class="mt-4 flex flex-wrap gap-2">
                  {#if action.status === "pending_delivery"}<button
                      class="ui-btn-primary"
                      onclick={deliver}
                      disabled={busy}
                      >{busy
                        ? "Requesting delivery…"
                        : "Deliver approved instruction"}</button
                    >{/if}<button
                    class="ui-btn-secondary"
                    onclick={reconcile}
                    disabled={busy}
                    >{busy
                      ? "Checking receipt…"
                      : "Check source receipt"}</button
                  >
                </div>
                <p class="mt-2 text-micro text-fg-muted">
                  Checking a receipt reads back source state; it does not resend
                  the instruction.
                </p>
                <details class="mt-4 text-micro text-fg-muted">
                  <summary class="cursor-pointer"
                    >Authorization and delivery attempts ({action.attempts
                      ?.length || 0})</summary
                  >
                  <pre
                    class="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-words rounded bg-bg-soft p-3 font-mono">{JSON.stringify(
                      {
                        authorization_basis: action.authorization_basis,
                        scope: action.scope,
                        target_revision: action.target_revision,
                        attempts: action.attempts || [],
                      },
                      null,
                      2,
                    )}</pre>
                </details>
              {:else}<p class="mt-2 text-meta text-fg-muted">
                  {selected.status === "awaiting_answer"
                    ? "No action is authorized yet."
                    : "No delivery receipt is available. An answer alone does not establish delivery."}
                </p>{/if}
            </section>
          </div>{:else}<p class="p-6 text-meta text-fg-muted">
            {selectedId
              ? "This decision is unavailable in the loaded history or work scope."
              : "Select a decision to inspect its scope and delivery."}
          </p>{/if}
      </section>
    </div>{/if}
  {#if decisionsCursor || actionsCursor}
    <button
      class="ui-btn-secondary"
      onclick={loadMore}
      disabled={loading || busy}
      >{loading
        ? "Loading more history…"
        : "Load more decisions and receipts"}</button
    >
  {/if}
</WorkspacePageShell>
