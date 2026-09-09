<script>
  import { onMount } from "svelte";
  import { page } from "$app/stores";
  import { beforeNavigate, goto } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
  import { initializeAuthSession } from "$lib/authSession";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { formatTimestamp } from "$lib/formatDate";
  import { errorMessage, receiptSignal } from "$lib/pm/presentation.js";
  import { decisionIdsFromTurn } from "$lib/pm/turnDecisions.js";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
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
  let olderLoaded = false;
  let creationKey, requestKey, requestText, createdConversationId;
  let requestId = 0;
  let decisionFetch = 0;
  let pollInFlight = false;
  const pendingDecisions = new Set();
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
    }
    void loadConversation(key.split("\n")[0]);
  });
  $effect(() => {
    void loadDecisionRecords(turnDecisionIds);
  });

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
  function onComposerKeydown(event) {
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      void send();
    }
  }
  function turnStatus(turn) {
    switch (turn.status) {
      case "sending":
        return ["Message queued · awaiting PM response", "neutral"];
      case "unknown":
        return ["Dispatch uncertain · response not established", "warn"];
      case "failed":
        return ["PM response failed", "warn"];
      default:
        return [
          `Response not available · ${turn.status || "unknown"}`,
          "neutral",
        ];
    }
  }
  const prompts = [
    "What changed since I last checked?",
    "What needs my decision?",
    "Which commitments are blocked, and who acts next?",
    "Where is the evidence still uncertain?",
  ];
  onMount(() => {
    void initialize();
    // Turn status must catch up without a manual reload even when the tab sat
    // hidden through the whole poll-paused window: poll while visible, and
    // refresh immediately on becoming visible again.
    const pollNow = () => {
      if (
        !selectedId ||
        sending ||
        loading ||
        loadingOlder ||
        pollInFlight ||
        document.hidden
      )
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
  <WorkspacePageHeader title="PM">
    {#snippet subtitle()}{#if conversation}<span class="line-clamp-1"
          >{conversation.title}</span
        >{:else}Ask about progress, blockers and evidence. Decisions are
        recorded separately.{/if}{/snippet}
    {#snippet actions()}
      <details
        class="pm-history"
        bind:open={historyOpen}
        aria-label="PM conversations"
      >
        <summary class="ui-btn-secondary list-none"
          >History{#if conversations.length}<span class="ml-1 text-fg-muted"
              >{conversations.length}</span
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

  {#if activeWorkRef}
    <p class="text-micro text-fg-muted">
      About <a
        class="ui-prose-link font-mono"
        href={workspaceHref(`/work/${encodeURIComponent(activeWorkRef)}`)}
        >{activeWorkRef}</a
      >
    </p>
  {/if}

  <section class="pm-thread" aria-label="PM conversation">
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
      <p class="py-8 text-meta text-fg-muted" role="status">Loading…</p>
    {:else if !turns.length}
      <div class="mt-auto pb-2 pt-10">
        <h2 class="text-subtitle font-semibold text-fg">
          What do you want to know?
        </h2>
        <div class="mt-3 flex flex-wrap gap-2">
          {#each prompts as prompt}
            <button
              class="pm-prompt"
              type="button"
              onclick={() => {
                draft = prompt;
                document.getElementById("pm-message")?.focus();
              }}>{prompt}</button
            >
          {/each}
        </div>
      </div>
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
      class="space-y-7"
      aria-label="Conversation messages"
      role="log"
      aria-live="polite"
      aria-relevant="additions text"
    >
      {#each turns as turn (turn.id)}
        {@const [statusLabel, statusTone] = turnStatus(turn)}
        {@const proposed = decisionIdsFromTurn(turn)}
        <li class="space-y-3">
          <div class="pm-turn pm-turn--you">
            <p class="pm-turn-meta">
              You <span class="text-fg-subtle"
                >· {formatTimestamp(turn.created_at)}</span
              >
            </p>
            <p class="whitespace-pre-wrap break-words text-meta text-fg">
              {turn.text}
            </p>
          </div>
          <div class="pm-turn pm-turn--pm">
            <p class="pm-turn-meta">PM</p>
            {#if turn.response}
              <MarkdownRenderer
                source={turn.response}
                class="pm-response text-meta leading-relaxed text-fg"
              />
            {:else}
              <SignalBadge tone={statusTone}>{statusLabel}</SignalBadge>
            {/if}
            {#if turn.evidence_refs?.length}
              <ul class="mt-2 flex flex-wrap gap-1.5 text-micro">
                {#each turn.evidence_refs.filter((ref) => !ref.startsWith("decision:")) as ref}
                  <li class="rounded bg-bg-soft px-1.5 py-0.5 font-mono">
                    {#if ref.startsWith("card:")}
                      <a
                        class="ui-prose-link"
                        href={workspaceHref(`/work/${encodeURIComponent(ref)}`)}
                        >{ref}</a
                      >
                    {:else}{ref}{/if}
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
                      <span class="min-w-0 flex-1 break-words text-meta text-fg"
                        >{record?.instruction || `decision:${id}`}</span
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
                        <SignalBadge tone={signal.tone}
                          >{signal.label}</SignalBadge
                        >
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
                        <span class="text-micro text-fg-subtle">Loading…</span>
                      {/if}
                    </li>
                  {/each}
                </ul>
                {#if decisionError}
                  <p class="mt-1 text-micro text-warn-text">{decisionError}</p>
                {/if}
              </div>
            {/if}
          </div>
        </li>
      {/each}
    </ol>
  </section>

  <form class="pm-composer" onsubmit={send}>
    <label for="pm-message" class="sr-only">Message PM</label>
    <textarea
      id="pm-message"
      class="pm-composer-input"
      rows="3"
      bind:value={draft}
      required
      maxlength="16000"
      placeholder="Ask the PM… (⌘↵ to send)"
      disabled={sending}
      onkeydown={onComposerKeydown}
    ></textarea>
    <div class="flex items-center justify-between gap-3 px-3 pb-2.5">
      <p class="text-micro text-fg-subtle">
        {#if !ready && error}Sending is disabled until PM is reachable.{:else}Discussion
          does not authorize changes.{/if}
      </p>
      <button
        class="ui-btn-primary"
        type="submit"
        disabled={sending || !draft.trim() || !ready}
        >{sending ? "Sending…" : "Send message"}</button
      >
    </div>
  </form>
</WorkspacePageShell>

<style>
  :global(.pm-page) {
    display: flex;
    flex-direction: column;
    min-height: calc(100dvh - 8rem);
  }
  .pm-thread {
    flex: 1 1 auto;
    display: flex;
    flex-direction: column;
    gap: 1.25rem;
    min-width: 0;
  }
  .pm-turn {
    min-width: 0;
  }
  .pm-turn--you {
    padding-left: 0.75rem;
    border-left: 2px solid var(--line-strong);
  }
  .pm-turn--pm {
    padding-left: 0.75rem;
    border-left: 2px solid var(--accent-solid);
  }
  :global(.pm-response) {
    overflow-wrap: anywhere;
  }
  :global(.pm-response p) {
    margin: 0 0 0.6rem;
  }
  :global(.pm-response ul),
  :global(.pm-response ol) {
    margin: 0 0 0.6rem;
    padding-left: 1.25rem;
  }
  :global(.pm-response hr) {
    border: 0;
    border-top: 1px solid var(--line);
    margin: 0.75rem 0;
  }
  :global(.pm-response code) {
    font-family: var(--font-mono);
    font-size: 0.85em;
    background: var(--bg-soft);
    padding: 0.05em 0.3em;
    border-radius: 3px;
  }
  .pm-turn-meta {
    margin-bottom: 0.25rem;
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--fg-muted);
  }
  .pm-prompt {
    padding: 0.375rem 0.75rem;
    border: 1px solid var(--line);
    border-radius: var(--radius-full);
    background: transparent;
    color: var(--fg-muted);
    font-size: 13px;
    cursor: pointer;
    transition: all var(--motion-fast);
  }
  .pm-prompt:hover {
    color: var(--fg);
    border-color: var(--line-strong);
    background: var(--bg-soft);
  }
  .pm-composer {
    position: sticky;
    bottom: 0;
    margin-top: 0.5rem;
    border: 1px solid var(--line);
    border-radius: var(--radius-md);
    background: var(--panel);
    box-shadow: 0 -12px 24px -20px rgba(0, 0, 0, 0.8);
  }
  .pm-composer:focus-within {
    border-color: var(--line-strong);
  }
  .pm-composer-input {
    display: block;
    width: 100%;
    padding: 0.75rem 0.875rem 0.5rem;
    border: 0;
    background: transparent;
    color: var(--fg);
    font-size: 13px;
    line-height: 1.5;
    resize: vertical;
    min-height: 4.5rem;
  }
  .pm-composer-input:focus {
    outline: none;
    box-shadow: none;
  }
  .pm-composer-input::placeholder {
    color: var(--fg-subtle);
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
