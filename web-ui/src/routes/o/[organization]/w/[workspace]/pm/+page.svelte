<script>
  import { onMount, tick, untrack } from "svelte";
  import { page } from "$app/stores";
  import { beforeNavigate, goto } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
  import { initializeAuthSession } from "$lib/authSession";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { formatAbsoluteDateTime, formatTimestamp } from "$lib/formatDate";
  import { resolveRefLink } from "$lib/refLinkModel.js";
  import {
    decisionTitle,
    errorMessage,
    receiptSignal,
  } from "$lib/pm/presentation.js";
  import { decisionIdsFromTurn } from "$lib/pm/turnDecisions.js";
  import {
    clockTime,
    evidenceRefsForTurn,
    hasPendingTurn,
    isNearBottom,
    startsTimeGroup,
    turnState,
  } from "$lib/pm/chatModel.js";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
  import ReceiptSignal from "$lib/components/pm/ReceiptSignal.svelte";
  import RefChip from "$lib/components/RefChip.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";

  let conversations = $state([]),
    conversation = $state(null),
    turns = $state([]),
    loading = $state(true),
    sending = $state(false),
    ready = $state(false),
    error = $state(""),
    draft = $state(""),
    partial = $state(false),
    historyOpen = $state(false);
  let decisionRecords = $state({});
  let decisionError = $state("");
  let conversationsCursor = $state("");
  let turnsCursor = $state("");
  let loadingOlder = $state(false);
  let loadingConversations = $state(false);
  let now = $state(Date.now());
  let reducedMotion = $state(false);
  let atBottom = $state(true);
  let threadElement = $state(null);
  let composerElement = $state(null);
  let olderLoaded = false;
  let creationKey, requestKey, requestText, createdConversationId;
  let requestId = 0;
  let decisionFetch = 0;
  let pollInFlight = false;
  let anchored = false;
  const pendingDecisions = new Set();
  const retryKeys = new Map();
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let selectedId = $derived($page.url.searchParams.get("conversation") || "");
  let workRef = $derived($page.url.searchParams.get("work_ref") || "");
  let selectedKey = $derived(`${selectedId}\n${workRef}`);
  let activeWorkRef = $derived(conversation?.work_ref || workRef);
  let turnDecisionIds = $derived(
    turns.flatMap((turn) => decisionIdsFromTurn(turn)),
  );
  let waiting = $derived(hasPendingTurn(turns, now));
  let showJump = $derived(Boolean(turns.length) && !atBottom);

  beforeNavigate(({ cancel }) => {
    if (sending) {
      cancel();
      return;
    }
    if (
      draft.trim() &&
      !sending &&
      !window.confirm("Leave with an unsent PM message?")
    )
      cancel();
  });

  let conversationScope;
  $effect(() => {
    const key = selectedKey;
    if (!ready) return;
    const switched =
      conversationScope !== undefined && conversationScope !== key;
    conversationScope = key;
    historyOpen = false;
    if (switched) {
      createdConversationId = "";
      creationKey = "";
      requestKey = "";
      requestText = "";
      draft = "";
      anchored = false;
    }
    void loadConversation(key.split("\n")[0]);
  });
  $effect(() => {
    void loadDecisionRecords(turnDecisionIds);
  });
  // A pending turn shows its own elapsed wait; nothing else needs a clock.
  $effect(() => {
    if (!waiting) return;
    const timer = setInterval(() => {
      now = Date.now();
    }, 1000);
    return () => clearInterval(timer);
  });
  // Auto-grow: reset then measure, capped so the thread keeps most of the height.
  $effect(() => {
    const value = draft;
    const element = composerElement;
    if (!element) return;
    void value;
    element.style.height = "auto";
    const measured = Number(element.scrollHeight) || 0;
    if (measured > 0) element.style.height = `${Math.min(measured, 200)}px`;
  });
  // Stick to the newest turn while the reader is already at the bottom. Only a
  // new turn (or an answer arriving on one) may move the view — the reader's own
  // scroll position is read untracked so scrolling never re-pins them.
  $effect(() => {
    const signature = turns
      .map((turn) => `${turn.id}:${turn.response ? 1 : 0}`)
      .join("|");
    const element = threadElement;
    if (!element || !signature) return;
    untrack(() => {
      if (loadingOlder) return;
      if (!anchored) {
        anchored = true;
        void tick().then(() => scrollToLatest("auto"));
        return;
      }
      if (!atBottom) return;
      void tick().then(() => scrollToLatest(reducedMotion ? "auto" : "smooth"));
    });
  });

  function scrollToLatest(behavior = "auto") {
    const element = threadElement;
    if (!element) return;
    if (typeof element.scrollTo === "function")
      element.scrollTo({ top: element.scrollHeight, behavior });
    else element.scrollTop = element.scrollHeight;
    atBottom = true;
  }
  function onThreadScroll() {
    atBottom = isNearBottom(threadElement);
  }

  async function loadList(append = false) {
    loadingConversations = true;
    try {
      const result = await coreClient.listPmConversations({
        limit: 50,
        cursor: append ? conversationsCursor : undefined,
      });
      conversations = [
        ...new Map(
          [...(append ? conversations : []), ...(result.items || [])].map(
            (item) => [item.id, item],
          ),
        ).values(),
      ];
      conversationsCursor = result.next_cursor || "";
      partial = Boolean(result.has_more);
    } finally {
      loadingConversations = false;
    }
  }
  async function moreConversations() {
    try {
      await loadList(true);
    } catch (err) {
      error = errorMessage(err);
    }
  }
  async function olderTurns() {
    if (!selectedId || !turnsCursor || loadingOlder) return;
    const ticket = requestId;
    const element = threadElement;
    const previousHeight = element?.scrollHeight ?? 0;
    const previousTop = element?.scrollTop ?? 0;
    loadingOlder = true;
    try {
      const result = await coreClient.getPmConversation(selectedId, {
        limit: 100,
        cursor: turnsCursor,
      });
      if (ticket !== requestId) return;
      turns = [
        ...new Map(
          [...(result.turns || []), ...turns].map((turn) => [turn.id, turn]),
        ).values(),
      ];
      turnsCursor = result.next_cursor || "";
      olderLoaded = true;
      // Prepending must not move the reader: restore by the height the
      // prepended turns added.
      await tick();
      if (element)
        element.scrollTop =
          previousTop + (element.scrollHeight - previousHeight);
    } catch (err) {
      if (ticket === requestId) error = errorMessage(err);
    } finally {
      loadingOlder = false;
    }
  }
  async function loadDecisionRecords(ids) {
    const ticket = decisionFetch;
    const missing = ids.filter(
      (id) => id && !(id in decisionRecords) && !pendingDecisions.has(id),
    );
    if (!missing.length) return;
    for (const id of missing) pendingDecisions.add(id);
    await Promise.all(
      missing.map(async (id) => {
        try {
          const record = await coreClient.getPmDecision(id);
          if (ticket === decisionFetch) decisionRecords[id] = record;
        } catch (err) {
          if (ticket === decisionFetch) {
            decisionRecords[id] = null;
            decisionError = errorMessage(err);
          }
        } finally {
          pendingDecisions.delete(id);
        }
      }),
    );
  }
  async function loadConversation(id = selectedId, quiet = false) {
    const ticket = ++requestId;
    if (!quiet) {
      loading = true;
      conversation = null;
      turns = [];
      turnsCursor = "";
      olderLoaded = false;
      anchored = false;
    }
    error = "";
    try {
      if (id) {
        const result = await coreClient.getPmConversation(id, { limit: 100 });
        if (ticket !== requestId) return;
        conversation = result.conversation;
        turns = quiet
          ? [
              ...new Map(
                [...turns, ...(result.turns || [])].map((turn) => [
                  turn.id,
                  turn,
                ]),
              ).values(),
            ]
          : result.turns || [];
        if (!quiet || !olderLoaded) turnsCursor = result.next_cursor || "";
      }
    } catch (err) {
      if (ticket === requestId) error = errorMessage(err);
    } finally {
      if (ticket === requestId) loading = false;
    }
  }
  async function initialize() {
    loading = true;
    error = "";
    try {
      await initializeAuthSession({
        fetchFn: globalThis.fetch.bind(globalThis),
        workspaceSlug: $page.params.workspace,
        authDriver: "pm-conversation",
      });
      await loadList();
      ready = true;
    } catch (err) {
      error = errorMessage(err);
      loading = false;
    }
  }
  async function send(event) {
    event?.preventDefault?.();
    const text = draft.trim();
    if (!text || sending || !ready) return;
    sending = true;
    error = "";
    if (!requestKey || text !== requestText) {
      requestKey = crypto.randomUUID();
      requestText = text;
    }
    try {
      let id = selectedId || createdConversationId;
      if (!id) {
        creationKey ||= crypto.randomUUID();
        const result = await coreClient.createPmConversation({
          request_key: creationKey,
          title: text.slice(0, 100),
          ...(workRef ? { work_ref: workRef } : {}),
        });
        if (!result.id)
          throw new Error(
            "Conversation creation did not return an identifier. Retry with the same request.",
          );
        id = result.id;
        createdConversationId = id;
      }
      const turn = await coreClient.sendPmMessage(id, {
        request_key: requestKey,
        text,
      });
      if (!turn.id)
        throw new Error(
          "The message receipt is incomplete. Your draft is retained; retry uses the same request key.",
        );
      draft = "";
      requestKey = "";
      requestText = "";
      await loadList();
      if (selectedId !== id) {
        sending = false;
        await goto(workspaceHref(`/pm?conversation=${encodeURIComponent(id)}`));
      } else await loadConversation(id, true);
    } catch (err) {
      error = errorMessage(err);
    } finally {
      sending = false;
    }
  }
  /**
   * Resend a turn the PM never answered. The key is stable per turn, so
   * repeated retries of the same message stay one intent at core.
   */
  async function retryTurn(turn) {
    const text = String(turn?.text ?? "").trim();
    if (!text || sending || !ready) return;
    if (!retryKeys.has(turn.id)) retryKeys.set(turn.id, crypto.randomUUID());
    requestKey = retryKeys.get(turn.id);
    requestText = text;
    draft = text;
    await send();
  }
  function onComposerKeydown(event) {
    if (event.key !== "Enter" || event.isComposing || event.shiftKey) return;
    event.preventDefault();
    void send();
  }
  function useSuggestion(prompt) {
    draft = prompt;
    document.getElementById("pm-message")?.focus();
  }
  /**
   * Evidence chips reuse the shared ref model for labels and hrefs; `card:` and
   * `work:` keep the task route this page has always used.
   */
  function evidenceLink(ref) {
    const resolved = resolveRefLink(ref, {
      organizationSlug: $page.params.organization,
      workspaceSlug: $page.params.workspace,
    });
    if (ref.startsWith("card:") || ref.startsWith("work:"))
      return {
        ...resolved,
        href: workspaceHref(`/tasks/${encodeURIComponent(ref)}`),
        isExternal: false,
      };
    return resolved;
  }
  const prompts = [
    "What needs my decision?",
    "What changed since I last checked?",
    "Which commitments are blocked, and who acts next?",
    "Where is the evidence still uncertain?",
  ];
  const RUNBOOK_HREF = "/cli/docs/runbook.md#pm-runner-anx-pm-serve";
  onMount(() => {
    void initialize();
    const motion = globalThis.matchMedia?.("(prefers-reduced-motion: reduce)");
    reducedMotion = Boolean(motion?.matches);
    const onMotionChange = (event) => {
      reducedMotion = Boolean(event.matches);
    };
    motion?.addEventListener?.("change", onMotionChange);
    // A pending turn resolves in the background whether or not this tab is in
    // front: keep polling while hidden, and refresh the moment it comes back.
    const pollNow = () => {
      if (!selectedId || sending || loading || loadingOlder || pollInFlight)
        return;
      pollInFlight = true;
      void loadConversation(selectedId, true).finally(() => {
        pollInFlight = false;
      });
    };
    const onVisibilityChange = () => {
      if (!document.hidden) pollNow();
    };
    document.addEventListener("visibilitychange", onVisibilityChange);
    const timer = setInterval(pollNow, 5000);
    return () => {
      requestId++;
      decisionFetch++;
      motion?.removeEventListener?.("change", onMotionChange);
      document.removeEventListener("visibilitychange", onVisibilityChange);
      clearInterval(timer);
    };
  });
</script>

<svelte:window
  onbeforeunload={(event) => {
    if (draft.trim()) {
      event.preventDefault();
      event.returnValue = "";
    }
  }}
/>
<svelte:head><title>PM · Agent Nexus</title></svelte:head>
<WorkspacePageShell class="pm-page">
  <div class="pm-head">
    <WorkspacePageHeader title="Ask PM">
      {#snippet subtitle()}Ask about your tasks. If the PM proposes a change,
        you approve it in Inbox.{/snippet}
      {#snippet actions()}
        <details
          class="pm-history"
          bind:open={historyOpen}
          aria-label="PM conversations"
        >
          <summary class="ui-btn-secondary list-none"
            >History{#if conversations.length && !partial}<span
                class="ml-1 text-fg-muted">{conversations.length}</span
              >{/if}</summary
          >
          <div class="pm-history-panel" role="presentation">
            <nav aria-label="Conversation history">
              {#each conversations as item (item.id)}
                <a
                  class="pm-history-item {item.id === selectedId
                    ? 'pm-history-item--active'
                    : ''}"
                  href={workspaceHref(
                    `/pm?conversation=${encodeURIComponent(item.id)}`,
                  )}
                  aria-current={item.id === selectedId ? "page" : undefined}
                >
                  <span class="line-clamp-1 break-words">{item.title}</span>
                  <span class="text-micro text-fg-subtle"
                    >{formatTimestamp(item.created_at)}</span
                  >
                </a>
              {:else}
                <p class="px-3 py-3 text-micro text-fg-muted">
                  No conversations yet.
                </p>
              {/each}
            </nav>
            {#if partial || conversationsCursor}
              <div class="border-t border-line px-3 py-2 text-micro">
                {#if conversationsCursor}
                  <button
                    class="ui-prose-link"
                    onclick={moreConversations}
                    disabled={loadingConversations}
                    type="button"
                    >{loadingConversations
                      ? "Loading…"
                      : "More conversations"}</button
                  >
                {:else if partial}
                  <span class="text-warn-text">Partial history</span>
                {/if}
              </div>
            {/if}
          </div>
        </details>
        <a class="ui-btn-secondary" href={workspaceHref("/pm")}>New</a>
      {/snippet}
    </WorkspacePageHeader>

    {#if conversation?.title || activeWorkRef}
      <p class="pm-context">
        {#if conversation?.title}<span class="line-clamp-1 min-w-0"
            >{conversation.title}</span
          >{/if}
        {#if activeWorkRef}<a
            class="ui-prose-link shrink-0 font-mono"
            href={workspaceHref(`/tasks/${encodeURIComponent(activeWorkRef)}`)}
            >{activeWorkRef}</a
          >{/if}
      </p>
    {/if}
  </div>

  <!-- A scrollable region must be reachable by keyboard (axe scrollable-region-focusable). -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div
    class="pm-thread"
    bind:this={threadElement}
    onscroll={onThreadScroll}
    aria-label="PM conversation"
    role="region"
    tabindex="0"
  >
    <div class="pm-thread-inner">
      {#if error}
        <div
          role="alert"
          class="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
        >
          <span class="min-w-0 flex-1 break-words">{error}</span>
          <button
            class="ui-prose-link text-micro"
            type="button"
            onclick={() =>
              ready ? loadConversation(selectedId, true) : initialize()}
            >Retry</button
          >
        </div>
      {/if}

      {#if loading}
        <p class="text-meta text-fg-muted" role="status">Loading…</p>
      {:else if !turns.length}
        <p class="pm-empty">
          Nothing asked yet. Pick a starter below, or type a question.
        </p>
      {/if}

      {#if turnsCursor}
        <button
          class="ui-prose-link w-fit text-micro"
          type="button"
          onclick={olderTurns}
          disabled={loadingOlder}
          >{loadingOlder ? "Loading older messages…" : "Older messages"}</button
        >
      {/if}

      <ol
        class="pm-turns"
        aria-label="Conversation messages"
        role="log"
        aria-live="polite"
        aria-relevant="additions text"
      >
        {#each turns as turn, index (turn.id)}
          {@const view = turnState(turn, now)}
          {@const proposed = decisionIdsFromTurn(turn)}
          {@const evidence = evidenceRefsForTurn(turn)}
          {@const clock = clockTime(turn.created_at)}
          {@const grouped = startsTimeGroup(turn, turns[index - 1]) && clock}
          <li class="pm-pair">
            {#if grouped}
              <p
                class="pm-group-head"
                title={formatAbsoluteDateTime(turn.created_at)}
              >
                PM · {clock}
              </p>
            {/if}
            <div class="pm-you">
              <p class="pm-bubble">{turn.text}</p>
              {#if clock}
                <span
                  class="pm-bubble-time"
                  title={formatAbsoluteDateTime(turn.created_at)}>{clock}</span
                >
              {/if}
            </div>
            <div class="pm-answer">
              {#if view.kind === "answered"}
                <MarkdownRenderer
                  source={turn.response}
                  class="pm-response text-meta text-fg"
                />
              {:else if view.kind === "pending"}
                <p class="pm-status" role="status">
                  {#if reducedMotion}
                    <span>Thinking…</span>
                  {:else}
                    <span class="pm-dots" aria-hidden="true"
                      ><i></i><i></i><i></i></span
                    >
                    <span
                      >Thinking{view.elapsed ? ` · ${view.elapsed}` : ""}</span
                    >
                  {/if}
                </p>
                {#if view.stalled}
                  <p class="pm-status-note">
                    No reply yet — the PM runner may be off. <a
                      class="ui-prose-link"
                      href={RUNBOOK_HREF}>How to start it</a
                    >
                  </p>
                {/if}
              {:else}
                <p
                  class="text-meta {view.kind === 'failed'
                    ? 'text-danger-text'
                    : 'text-fg-muted'}"
                >
                  {view.kind === "failed"
                    ? view.detail || "The PM did not answer."
                    : "Delivery uncertain"}
                  <button
                    class="ui-prose-link ml-2 text-micro"
                    type="button"
                    disabled={sending || !ready}
                    onclick={() => retryTurn(turn)}>Retry</button
                  >
                </p>
              {/if}
              {#if evidence.length}
                <ul class="pm-evidence" aria-label="Evidence for this reply">
                  {#each evidence as ref (ref)}
                    {@const link = evidenceLink(ref)}
                    <li class="flex min-w-0">
                      <RefChip
                        href={link.href}
                        external={link.isExternal}
                        title={link.raw}>{link.label}</RefChip
                      >
                    </li>
                  {/each}
                </ul>
              {/if}
              {#if proposed.length}
                <div class="mt-3 border-t border-line-subtle pt-2">
                  <p
                    class="text-micro font-semibold uppercase tracking-wide text-fg-muted"
                  >
                    Proposed decisions
                  </p>
                  <ul
                    class="mt-1 divide-y divide-line-subtle"
                    aria-label="Decisions proposed in this reply"
                  >
                    {#each proposed as id (id)}
                      {@const record = decisionRecords[id]}
                      <li
                        class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 py-1.5"
                      >
                        <span
                          class="min-w-0 flex-1 break-words text-meta text-fg"
                          >{record
                            ? decisionTitle(record)
                            : `decision:${id}`}</span
                        >
                        {#if record?.work_ref}
                          <a
                            class="font-mono text-micro text-fg-muted hover:text-accent-text"
                            href={workspaceHref(
                              `/tasks/${encodeURIComponent(record.work_ref)}`,
                            )}>{record.work_ref}</a
                          >
                        {/if}
                        {#if record}
                          {@const signal = receiptSignal(record.status)}
                          <ReceiptSignal {signal} />
                          {#if record.status === "awaiting_answer"}
                            <a
                              class="ui-prose-link text-micro"
                              href={workspaceHref(
                                `/inbox?item=decision:${encodeURIComponent(id)}`,
                              )}>Answer</a
                            >
                          {/if}
                        {:else if record === null}
                          <SignalBadge tone="neutral">Unavailable</SignalBadge>
                        {:else}
                          <span class="text-micro text-fg-subtle">Loading…</span
                          >
                        {/if}
                      </li>
                    {/each}
                  </ul>
                  {#if decisionError}
                    <p class="mt-1 text-micro text-warn-text">
                      {decisionError}
                    </p>
                  {/if}
                </div>
              {/if}
            </div>
          </li>
        {/each}
      </ol>
    </div>
  </div>

  <div class="pm-foot">
    {#if showJump}
      <button
        class="pm-jump"
        type="button"
        onclick={() => scrollToLatest(reducedMotion ? "auto" : "smooth")}
        >Jump to latest ↓</button
      >
    {/if}
    <form class="pm-composer" onsubmit={send}>
      {#if !turns.length && !loading}
        <div class="pm-suggestions" role="group" aria-label="Starter questions">
          {#each prompts as prompt}
            <button
              class="pm-chip"
              type="button"
              onclick={() => useSuggestion(prompt)}>{prompt}</button
            >
          {/each}
        </div>
      {/if}
      <label for="pm-message" class="sr-only">Message PM</label>
      <textarea
        id="pm-message"
        class="pm-composer-input"
        rows="1"
        bind:this={composerElement}
        bind:value={draft}
        required
        maxlength="16000"
        placeholder="Ask the PM…"
        disabled={sending}
        onkeydown={onComposerKeydown}
      ></textarea>
      <div class="pm-composer-foot">
        {#if !ready && error}
          <p class="pm-composer-note">
            Sending is disabled until PM is reachable.
          </p>
        {/if}
        <kbd class="pm-kbd">⌘↵</kbd>
        <button
          class="pm-send"
          type="submit"
          aria-label="Send message"
          disabled={sending || !draft.trim() || !ready}
        >
          <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false">
            <path
              d="M8 13V3.5M8 3.5 4 7.5M8 3.5l4 4"
              fill="none"
              stroke="currentColor"
              stroke-width="1.6"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </button>
      </div>
    </form>
  </div>
</WorkspacePageShell>

<style>
  /* The thread scrolls, not the page: the shell must give up its own scroll. */
  :global(.shell-main-scroll:has(.pm-page)) {
    overflow: hidden;
    padding-bottom: 0;
    display: flex;
    flex-direction: column;
  }
  :global(.shell-main-scroll:has(.pm-page) > .shell-content) {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  /* The compact shell's bottom nav is fixed; the composer must clear it. */
  @media (max-width: 1023px) {
    :global(.shell-main-scroll:has(.pm-page)) {
      padding-bottom: calc(3.25rem + env(safe-area-inset-bottom, 0px));
    }
  }
  :global(.pm-page) {
    flex: 1 1 auto;
    display: grid;
    grid-template-rows: auto 1fr auto;
    height: 100%;
    min-height: 0;
  }
  /* The shell wrapper's `space-y-5` would fight the grid rows. */
  :global(.pm-page > * + *) {
    margin-block-start: 0;
  }

  .pm-head,
  .pm-thread-inner,
  .pm-composer,
  .pm-foot {
    width: min(46rem, 100%);
    margin-inline: auto;
  }
  .pm-context {
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
    margin-top: 0.375rem;
    font-size: 11px;
    color: var(--fg-subtle);
  }

  .pm-thread {
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: 16px 0 24px;
  }
  .pm-thread-inner {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }
  .pm-empty {
    font-size: 13px;
    color: var(--fg-muted);
  }
  .pm-turns {
    display: flex;
    flex-direction: column;
    gap: 24px;
    min-width: 0;
  }
  .pm-pair {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }
  .pm-group-head {
    font-size: 11px;
    color: var(--fg-subtle);
    font-variant-numeric: tabular-nums;
  }

  .pm-you {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 2px;
    min-width: 0;
  }
  .pm-bubble {
    max-width: 80%;
    padding: 8px 12px;
    border: 1px solid var(--line);
    border-radius: 10px 10px 4px 10px;
    background: var(--panel);
    color: var(--fg);
    font-size: 13px;
    line-height: 18px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .pm-bubble-time {
    font-size: 11px;
    color: var(--fg-subtle);
    font-variant-numeric: tabular-nums;
    opacity: 0;
    transition: opacity var(--motion-fast);
  }
  .pm-pair:hover .pm-bubble-time,
  .pm-pair:focus-within .pm-bubble-time {
    opacity: 1;
  }
  @media (hover: none) {
    .pm-bubble-time {
      opacity: 1;
    }
  }

  .pm-answer {
    min-width: 0;
    font-size: 13px;
    line-height: 22px;
    color: var(--fg);
  }
  :global(.pm-response) {
    overflow-wrap: anywhere;
    line-height: 22px;
  }
  :global(.pm-response p),
  :global(.pm-response ul),
  :global(.pm-response ol) {
    margin: 0 0 12px;
  }
  :global(.pm-response ul),
  :global(.pm-response ol) {
    padding-left: 20px;
  }
  :global(.pm-response > :last-child) {
    margin-bottom: 0;
  }
  :global(.pm-response hr) {
    border: 0;
    border-top: 1px solid var(--line);
    margin: 12px 0;
  }
  :global(.pm-response code) {
    font-family: var(--font-mono);
    font-size: 0.85em;
    background: var(--bg-soft);
    padding: 0.05em 0.3em;
    border-radius: 3px;
  }

  .pm-status {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 18px;
    font-size: 11px;
    color: var(--fg-subtle);
    font-variant-numeric: tabular-nums;
  }
  .pm-status-note {
    margin-top: 4px;
    font-size: 11px;
    color: var(--fg-muted);
  }
  .pm-dots {
    display: inline-flex;
    gap: 3px;
  }
  .pm-dots i {
    width: 4px;
    height: 4px;
    border-radius: 999px;
    background: var(--fg-subtle);
    animation: pm-dot 1.2s infinite ease-in-out;
  }
  .pm-dots i:nth-child(2) {
    animation-delay: 0.2s;
  }
  .pm-dots i:nth-child(3) {
    animation-delay: 0.4s;
  }
  @keyframes pm-dot {
    0%,
    100% {
      opacity: 0.3;
    }
    50% {
      opacity: 1;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .pm-dots i {
      animation: none;
      opacity: 0.6;
    }
  }

  .pm-evidence {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 8px;
    min-width: 0;
  }

  .pm-foot {
    position: relative;
    padding-bottom: 8px;
  }
  .pm-jump {
    position: absolute;
    left: 50%;
    bottom: calc(100% + 12px);
    transform: translateX(-50%);
    height: 28px;
    padding: 0 12px;
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--panel);
    color: var(--fg-muted);
    font-size: 11px;
    white-space: nowrap;
    cursor: pointer;
  }
  .pm-jump:hover {
    color: var(--fg);
    border-color: var(--line-strong);
  }

  .pm-composer {
    border: 1px solid var(--line);
    border-radius: var(--radius-md);
    background: var(--panel);
    padding: 0;
  }
  .pm-composer:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .pm-suggestions {
    display: flex;
    gap: 6px;
    overflow-x: auto;
    scrollbar-width: none;
    padding: 8px 12px 0;
  }
  .pm-suggestions::-webkit-scrollbar {
    display: none;
  }
  .pm-chip {
    flex: 0 0 auto;
    height: 24px;
    padding: 0 8px;
    border: 1px solid var(--line-subtle);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--fg-muted);
    font-size: 11px;
    white-space: nowrap;
    cursor: pointer;
    transition: all var(--motion-fast);
  }
  .pm-chip:hover {
    color: var(--fg);
    border-color: var(--line);
    background: var(--bg-soft);
  }
  .pm-composer-input {
    display: block;
    width: 100%;
    padding: 10px 12px;
    border: 0;
    background: transparent;
    color: var(--fg);
    font-size: 13px;
    line-height: 20px;
    min-height: 40px;
    max-height: 200px;
    resize: none;
    overflow-y: auto;
  }
  .pm-composer-input:focus {
    outline: none;
    box-shadow: none;
  }
  .pm-composer-input::placeholder {
    color: var(--fg-subtle);
  }
  .pm-composer-foot {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    height: 32px;
    padding: 0 12px 8px;
  }
  .pm-composer-note {
    margin-right: auto;
    min-width: 0;
    font-size: 11px;
    color: var(--fg-subtle);
  }
  .pm-kbd {
    font-family: inherit;
    font-size: 11px;
    color: var(--fg-subtle);
  }
  .pm-send {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    flex: 0 0 auto;
    border: 0;
    border-radius: var(--radius);
    background: var(--accent-solid);
    color: var(--fg);
    cursor: pointer;
  }
  .pm-send svg {
    width: 16px;
    height: 16px;
  }
  .pm-send:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .pm-history {
    position: relative;
  }
  .pm-history summary {
    cursor: pointer;
  }
  .pm-history summary::-webkit-details-marker {
    display: none;
  }
  .pm-history-panel {
    position: absolute;
    right: 0;
    top: calc(100% + 0.375rem);
    z-index: 30;
    width: min(22rem, 90vw);
    max-height: 24rem;
    overflow-y: auto;
    border: 1px solid var(--line);
    border-radius: var(--radius-md);
    background: var(--panel);
    box-shadow: var(--shadow-menu);
    padding: 0.25rem;
  }
  .pm-history-item {
    display: grid;
    gap: 0.125rem;
    padding: 0.5rem 0.625rem;
    border-radius: var(--radius);
    color: var(--fg-muted);
    font-size: 13px;
    text-decoration: none;
  }
  .pm-history-item:hover {
    background: var(--panel-hover);
    color: var(--fg);
  }
  .pm-history-item--active {
    background: var(--bg-soft);
    color: var(--fg);
  }
</style>
