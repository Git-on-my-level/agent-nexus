<script>
  import { onMount } from "svelte";
  import { page } from "$app/stores";
  import { beforeNavigate, goto } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
  import { initializeAuthSession } from "$lib/authSession";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { formatTimestamp } from "$lib/formatDate";
  import { errorMessage } from "$lib/pm/presentation.js";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
  let conversations = $state([]),
    conversation = $state(null),
    turns = $state([]),
    loading = $state(true),
    sending = $state(false),
    ready = $state(false),
    error = $state(""),
    draft = $state(""),
    partial = $state(false);
  let creationKey, requestKey, requestText, createdConversationId;
  let requestId = 0;
  let pollInFlight = false;
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let selectedId = $derived($page.url.searchParams.get("conversation") || "");
  let workRef = $derived($page.url.searchParams.get("work_ref") || "");
  let selectedKey = $derived(`${selectedId}\n${workRef}`);
  let activeWorkRef = $derived(conversation?.work_ref || workRef);
  beforeNavigate(({ cancel }) => {
    if (
      draft.trim() &&
      !sending &&
      !window.confirm("Leave with an unsent PM message?")
    )
      cancel();
  });
  $effect(() => {
    const key = selectedKey;
    if (ready) {
      createdConversationId = "";
      creationKey = "";
      requestKey = "";
      requestText = "";
      draft = "";
      void loadConversation(key.split("\n")[0]);
    }
  });
  async function loadList() {
    const result = await coreClient.listPmConversations();
    conversations = result.items || [];
    partial = Boolean(result.has_more);
  }
  async function loadConversation(id = selectedId, quiet = false) {
    const ticket = ++requestId;
    if (!quiet) {
      loading = true;
      conversation = null;
      turns = [];
    }
    error = "";
    try {
      if (id) {
        const result = await coreClient.getPmConversation(id);
        if (ticket !== requestId) return;
        conversation = result.conversation;
        turns = result.turns || [];
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
    event.preventDefault();
    const text = draft.trim();
    if (!text || sending) return;
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
      if (selectedId !== id)
        await goto(workspaceHref(`/pm?conversation=${encodeURIComponent(id)}`));
      else await loadConversation(id, true);
    } catch (err) {
      error = errorMessage(err);
    } finally {
      sending = false;
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
    const timer = setInterval(async () => {
      if (!selectedId || sending || loading || pollInFlight || document.hidden)
        return;
      pollInFlight = true;
      try {
        await loadConversation(selectedId, true);
      } finally {
        pollInFlight = false;
      }
    }, 5000);
    return () => {
      requestId++;
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
<svelte:head><title>PM conversation · Agent Nexus</title></svelte:head>
<WorkspacePageShell>
  <WorkspacePageHeader title="Your project manager"
    >{#snippet subtitle()}Review commitments, resolve decisions, and follow the
      evidence.{/snippet}{#snippet actions()}<a
        class="ui-btn-secondary"
        href={workspaceHref("/decisions")}>Decisions & receipts</a
      ><a class="ui-btn-secondary" href={workspaceHref("/pm")}
        >New conversation</a
      >{/snippet}</WorkspacePageHeader
  >
  <div class="grid min-h-[34rem] gap-4 lg:grid-cols-[15rem_minmax(0,1fr)]">
    <aside
      class="rounded-md border border-line bg-panel"
      aria-label="PM conversations"
    >
      <details class="lg:hidden">
        <summary
          class="cursor-pointer px-3 py-2.5 text-meta font-medium text-fg"
          >Conversations ({conversations.length})</summary
        >
        <nav
          class="max-h-56 overflow-y-auto border-t border-line p-2"
          aria-label="Conversation history"
        >
          {#each conversations as item}<a
              class="block rounded px-2 py-2 text-meta {item.id === selectedId
                ? 'bg-bg-soft text-fg'
                : 'text-fg-muted hover:bg-panel-hover'}"
              href={workspaceHref(
                `/pm?conversation=${encodeURIComponent(item.id)}`,
              )}
              aria-current={item.id === selectedId ? "page" : undefined}
              >{item.title}</a
            >{/each}
        </nav>
      </details>
      <div class="hidden lg:block">
        <h2
          class="border-b border-line px-3 py-3 text-micro font-semibold text-fg-muted"
        >
          CONVERSATIONS
        </h2>
        <nav
          class="max-h-[38rem] overflow-y-auto p-2"
          aria-label="Conversation history"
        >
          {#each conversations as item}<a
              class="block rounded px-2 py-2.5 text-meta {item.id === selectedId
                ? 'bg-bg-soft text-fg'
                : 'text-fg-muted hover:bg-panel-hover'}"
              href={workspaceHref(
                `/pm?conversation=${encodeURIComponent(item.id)}`,
              )}
              aria-current={item.id === selectedId ? "page" : undefined}
              ><span class="line-clamp-2 break-words">{item.title}</span><span
                class="mt-1 block text-micro text-fg-muted"
                >{formatTimestamp(item.created_at)}</span
              ></a
            >{:else}<p class="p-2 text-micro text-fg-muted">
              Your conversations will appear here.
            </p>{/each}
        </nav>
      </div>
      {#if partial}<p
          class="border-t border-line p-3 text-micro text-warn-text"
        >
          This is a partial conversation history.
        </p>{/if}
    </aside>
    <section
      class="flex min-w-0 flex-col overflow-hidden rounded-md border border-line bg-panel"
      aria-label="PM conversation"
    >
      <header
        class="flex flex-wrap items-center gap-2 border-b border-line px-4 py-3"
      >
        <h2 class="text-meta font-semibold text-fg">
          {conversation?.title || "Review your work"}
        </h2>
        {#if activeWorkRef}<a
            class="ml-auto break-words text-micro text-accent-text hover:underline"
            href={workspaceHref(`/work/${encodeURIComponent(activeWorkRef)}`)}
            >{activeWorkRef}</a
          >{:else}<span class="ml-auto text-micro text-fg-muted"
            >Workspace context</span
          >{/if}
      </header>
      <div class="flex-1 space-y-5 p-4 sm:p-5">
        {#if error}<StateError
            title="PM request could not be completed"
            message={error}
            onretry={() =>
              ready ? loadConversation(selectedId, true) : initialize()}
          />
          <p class="text-micro text-warn-text">
            Unsent text is retained. A queued message is not an answer or a
            completed action.
          </p>{/if}
        {#if loading}<p class="text-fg-muted" role="status">
            Loading conversation…
          </p>{:else if !turns.length}<div class="py-6">
            <h3 class="text-subtitle font-semibold text-fg">
              What needs your attention?
            </h3>
            <p class="mt-2 max-w-xl text-meta text-fg-muted">
              Ask about progress, blockers, priorities, or evidence. PM uses the
              authorized workspace context and records decisions separately from
              discussion.
            </p>
            <div class="mt-5 grid gap-2 sm:grid-cols-2">
              {#each prompts as prompt}<button
                  class="rounded-md border border-line p-3 text-left text-meta text-fg-muted hover:bg-panel-hover"
                  onclick={() => {
                    draft = prompt;
                    document.getElementById("pm-message")?.focus();
                  }}>{prompt}</button
                >{/each}
            </div>
          </div>{/if}
        <ol class="space-y-5" aria-label="Conversation messages">
          {#each turns as turn (turn.id)}<li class="space-y-3">
              <div
                class="ml-auto max-w-[94%] rounded-md border border-line bg-bg-soft p-3 sm:max-w-[85%]"
              >
                <p class="mb-1 text-micro font-medium text-fg-muted">
                  You · {formatTimestamp(turn.created_at)}
                </p>
                <p class="whitespace-pre-wrap break-words text-meta text-fg">
                  {turn.text}
                </p>
              </div>
              <div class="max-w-[96%] border-l-2 border-accent-solid pl-3">
                <p class="mb-1 text-micro font-semibold text-fg-muted">PM</p>
                {#if turn.response}<p
                    class="whitespace-pre-wrap break-words text-meta leading-relaxed text-fg"
                  >
                    {turn.response}
                  </p>{:else}<SignalBadge
                    tone={turn.status === "unknown" || turn.status === "failed"
                      ? "warn"
                      : "neutral"}
                    >{turn.status === "sending"
                      ? "Message queued · awaiting PM response"
                      : turn.status === "unknown"
                        ? "Dispatch uncertain · response not established"
                        : turn.status === "failed"
                          ? "PM response failed"
                          : `Response not available · ${turn.status || "unknown"}`}</SignalBadge
                  >{/if}{#if turn.evidence_refs?.length}<ul
                    class="mt-2 flex flex-wrap gap-2 text-micro text-fg-muted"
                  >
                    {#each turn.evidence_refs as ref}<li
                        class="break-all rounded bg-bg-soft px-2 py-1"
                      >
                        {#if ref.startsWith("card:")}<a
                            class="text-accent-text hover:underline"
                            href={workspaceHref(
                              `/work/${encodeURIComponent(ref)}`,
                            )}>{ref}</a
                          >{:else}{ref}{/if}
                      </li>{/each}
                  </ul>{/if}
              </div>
            </li>{/each}
        </ol>
      </div>
      <form class="border-t border-line bg-bg-soft p-4" onsubmit={send}>
        <label for="pm-message" class="text-micro font-medium text-fg-muted"
          >Message PM</label
        ><textarea
          id="pm-message"
          class="ui-input mt-2 min-h-24 resize-y"
          rows="3"
          bind:value={draft}
          required
          maxlength="16000"
          placeholder="Ask a question or describe the decision you need to make…"
          disabled={sending}
        ></textarea>
        <div class="mt-2 flex flex-wrap items-center justify-between gap-2">
          <p class="max-w-lg text-micro text-fg-muted">
            Discussion does not authorize source changes. Decisions and delivery
            receipts remain inspectable.
          </p>
          <button
            class="ui-btn-primary"
            type="submit"
            disabled={sending || !draft.trim() || !ready}
            >{sending ? "Sending message…" : "Send message"}</button
          >
        </div>
      </form>
    </section>
  </div>
</WorkspacePageShell>
