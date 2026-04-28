<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { onDestroy, onMount } from "svelte";

  import Button from "$lib/components/Button.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import { coreClient } from "$lib/coreClient";
  import { searchActors } from "$lib/searchHelpers";
  import { workspacePath } from "$lib/workspacePaths";

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let inboxItemID = $derived($page.params.id);

  let loading = $state(false);
  let loadError = $state("");
  let item = $state(null);
  let responseDraft = $state("");
  let notifyMode = $state("original");
  let notifyTargetActorID = $state("");
  let notifyTargetAgentID = $state("");
  let submitting = $state(false);
  let submitError = $state("");
  let autosaveInterval = null;
  let notifyTargetQuery = $state("");
  let notifyTargetResults = $state([]);
  let notifyTargetSelected = $state(null);
  let notifyTargetSearchTimer = null;
  let notifyTargetSearchSeq = 0;
  let notifyTargetMenuOpen = $state(false);

  const KIND_LABELS = {
    ask: "Ask",
    review: "Review",
    escalate: "Escalation",
  };

  let proposalStrings = $derived(
    Array.isArray(item?.response_proposals)
      ? item.response_proposals
          .map((s) => String(s ?? "").trim())
          .filter(Boolean)
      : [],
  );
  function workspaceHref(pathname = "/") {
    return workspacePath(organizationSlug, workspaceSlug, pathname);
  }

  function draftStorageKey() {
    return `anx.human-response.draft:${workspaceSlug}:${inboxItemID}`;
  }

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

  function requesterLabel(value = item) {
    return (
      String(value?.requester_label ?? "").trim() ||
      String(value?.requester_agent_id ?? "").trim() ||
      String(value?.requester_actor_id ?? "").trim() ||
      "unknown requester"
    );
  }

  function applyPreset(text) {
    responseDraft = text;
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

  function handleNotifyTargetInput(event) {
    const value = event.currentTarget.value;
    notifyTargetQuery = value;
    notifyTargetMenuOpen = true;
    if (notifyTargetSearchTimer) {
      clearTimeout(notifyTargetSearchTimer);
      notifyTargetSearchTimer = null;
    }
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
    return `Notify ${requesterLabel()}`;
  }

  function handleTextareaKeydown(event) {
    if (event.key !== "Enter" || !event.metaKey) return;
    event.preventDefault();
    void submitResponse();
  }

  async function loadItem() {
    loading = true;
    loadError = "";
    submitError = "";
    item = null;

    try {
      const response = await coreClient.getInboxItem(inboxItemID);
      const loaded = response.item ?? null;
      if (!loaded) {
        loadError = "Inbox item not found.";
        return;
      }
      item = loaded;
      notifyMode =
        notificationStatus(loaded).resolvable === false ? "none" : "original";
      if (browser) {
        const cached = localStorage.getItem(draftStorageKey());
        if (cached != null) responseDraft = cached;
      }
    } catch (error) {
      loadError =
        error instanceof Error
          ? `Failed to load inbox item: ${error.message}`
          : String(error);
    } finally {
      loading = false;
    }
  }

  async function submitResponseWithText(responseText) {
    if (!item || submitting) return;
    const text = String(responseText ?? "").trim();
    if (!text) {
      submitError = "Response text is required.";
      return;
    }
    const targetActorID = String(notifyTargetActorID ?? "").trim();
    const targetAgentID = String(notifyTargetAgentID ?? "").trim();
    if (notifyMode === "target" && !targetActorID && !targetAgentID) {
      submitError = "Replacement target requires an actor ID or agent ID.";
      return;
    }

    submitError = "";
    submitting = true;
    try {
      await coreClient.respondInboxItem(inboxItemID, {
        response_text: text,
        notify_mode: notifyMode,
        notify_target_actor_id:
          notifyMode === "target" && targetActorID ? targetActorID : undefined,
        notify_target_agent_id:
          notifyMode === "target" && targetAgentID ? targetAgentID : undefined,
      });
      if (browser) localStorage.removeItem(draftStorageKey());
      responseDraft = "";
      await goto(workspaceHref("/inbox"), {
        replaceState: false,
        noScroll: false,
        keepFocus: false,
      });
    } catch (error) {
      submitError =
        error instanceof Error
          ? `Failed to submit response: ${error.message}`
          : String(error);
    } finally {
      submitting = false;
    }
  }

  async function submitResponse() {
    await submitResponseWithText(responseDraft);
  }

  onMount(() => {
    void loadItem();
    autosaveInterval = setInterval(() => {
      if (!browser || !item) return;
      localStorage.setItem(draftStorageKey(), String(responseDraft ?? ""));
    }, 2000);
  });

  onDestroy(() => {
    if (autosaveInterval != null) clearInterval(autosaveInterval);
  });
</script>

<div class="mx-auto max-w-3xl space-y-4 px-4 py-4 max-md:py-3">
  <div class="flex items-center justify-between">
    <a
      class="text-meta text-fg-muted hover:text-fg"
      href={workspaceHref("/inbox")}
    >
      ← Back to inbox
    </a>
  </div>

  {#if loading}
    <div class="rounded border border-[var(--line)] bg-[var(--bg-soft)] p-4">
      <Skeleton rows={4} />
    </div>
  {:else if loadError && !item}
    <StateError
      message={loadError}
      onretry={() => void loadItem()}
      retrying={loading}
    />
  {:else if item}
    <section class="space-y-5">
      <header class="space-y-2">
        <div class="flex flex-wrap items-center gap-2 text-micro">
          <span
            class="rounded border border-[var(--line)] bg-[var(--panel)] px-2 py-0.5 font-semibold uppercase tracking-wide text-fg-muted"
          >
            {kindLabel(item)}
          </span>
          {#if item.severity}
            <span
              class="rounded border border-danger/30 bg-danger-soft px-2 py-0.5 font-semibold uppercase tracking-wide text-danger-text"
            >
              {item.severity}
            </span>
          {/if}
          <span class="text-fg-muted">
            from <span class="font-mono text-mono text-fg"
              >{requesterLabel(item)}</span
            >
          </span>
        </div>
        <h1 class="text-subtitle font-semibold leading-tight text-fg">
          {item.title}
        </h1>
        {#if item.body}
          <p class="whitespace-pre-wrap text-meta text-fg">{item.body}</p>
        {/if}
        {#if item.subject_ref || (Array.isArray(item.related_refs) && item.related_refs.length > 0)}
          <div class="flex flex-wrap items-center gap-2 text-micro">
            {#if item.subject_ref}
              <RefLink
                refValue={item.subject_ref}
                threadId={item.thread_id}
                humanize
              />
            {/if}
            {#each Array.isArray(item.related_refs) ? item.related_refs.slice(0, 4) : [] as refValue}
              <RefLink {refValue} threadId={item.thread_id} humanize />
            {/each}
          </div>
        {/if}
      </header>

      <form
        class="space-y-4"
        onsubmit={(event) => {
          event.preventDefault();
          void submitResponse();
        }}
      >
        {#if itemKind(item) === "review"}
          <div class="flex flex-wrap gap-2">
            <button
              class="rounded border border-[var(--line)] bg-[var(--panel)] px-3 py-1.5 text-meta font-semibold text-fg hover:bg-[var(--bg-soft)] disabled:opacity-50"
              type="button"
              disabled={submitting}
              onclick={() => void submitResponseWithText("Approved.")}
            >
              Approve
            </button>
            <button
              class="rounded border border-[var(--line)] bg-[var(--panel)] px-3 py-1.5 text-meta font-semibold text-fg hover:bg-[var(--bg-soft)] disabled:opacity-50"
              type="button"
              disabled={submitting}
              onclick={() => void submitResponseWithText("Rejected.")}
            >
              Reject
            </button>
          </div>
        {/if}

        {#if proposalStrings.length > 0}
          <div class="space-y-2">
            <div
              class="text-micro font-medium uppercase tracking-wide text-fg-muted"
            >
              Suggested responses
            </div>
            <div class="space-y-2">
              {#each proposalStrings as proposal, index (proposal)}
                {@const isRecommended = index === 0}
                {@const isSelected = responseDraft.trim() === proposal.trim()}
                <button
                  class="group block w-full rounded border bg-[var(--panel)] px-3 py-2 text-left text-meta text-fg transition hover:bg-[var(--bg-soft)] {isSelected
                    ? 'border-[var(--accent)] ring-1 ring-[var(--accent)] bg-[var(--accent)]/5'
                    : 'border-[var(--line)]'}"
                  type="button"
                  onclick={() => applyPreset(proposal)}
                >
                  <span
                    class="flex w-full flex-col items-start gap-1.5 text-left"
                  >
                    {#if isRecommended}
                      <span
                        class="shrink-0 rounded border border-[var(--accent)]/40 bg-[var(--accent)]/15 px-2 py-0.5 text-micro font-semibold uppercase tracking-wide text-[var(--accent)]"
                      >
                        Recommended
                      </span>
                    {/if}
                    <span class="w-full whitespace-pre-wrap">{proposal}</span>
                  </span>
                </button>
              {/each}
            </div>
          </div>
        {/if}

        <div>
          <label
            class="block text-micro font-medium uppercase tracking-wide text-fg-muted"
            for="human-response-input">Your response</label
          >
          <textarea
            id="human-response-input"
            class="mt-2 min-h-[200px] w-full rounded border border-[var(--line)] bg-[var(--panel)] px-3 py-2 text-meta text-[var(--fg)] outline-none placeholder:text-[var(--fg-muted)] focus:ring-2 focus:ring-[var(--accent)]"
            bind:value={responseDraft}
            onkeydown={handleTextareaKeydown}
            placeholder="Write the response the agent should rely on."
          ></textarea>
        </div>

        <div
          class="rounded border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-meta"
        >
          <div class="flex flex-wrap items-center gap-x-3 gap-y-2">
            <span class="text-fg-muted">{notifyDescription()}</span>
            <div class="ml-auto flex flex-wrap items-center gap-1">
              <button
                class="rounded px-2 py-1 text-micro font-medium {notifyMode ===
                'original'
                  ? 'bg-[var(--accent)]/15 text-[var(--accent)]'
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
                class="rounded px-2 py-1 text-micro font-medium {notifyMode ===
                'target'
                  ? 'bg-[var(--accent)]/15 text-[var(--accent)]'
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
                class="rounded px-2 py-1 text-micro font-medium {notifyMode ===
                'none'
                  ? 'bg-[var(--accent)]/15 text-[var(--accent)]'
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
                  class="flex items-center gap-2 rounded border border-[var(--line)] bg-[var(--panel)] px-2 py-1.5"
                >
                  <span
                    class="inline-flex items-center gap-1.5 rounded bg-[var(--accent)]/15 px-2 py-0.5 text-micro text-[var(--accent)]"
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
                  class="w-full rounded border border-[var(--line)] bg-[var(--panel)] px-2 py-1.5 text-meta text-fg outline-none placeholder:text-fg-muted focus:ring-2 focus:ring-[var(--accent)]"
                  type="text"
                  placeholder="Search people or agents…"
                  value={notifyTargetQuery}
                  oninput={handleNotifyTargetInput}
                  onfocus={() => (notifyTargetMenuOpen = true)}
                />
                {#if notifyTargetMenuOpen && notifyTargetResults.length > 0}
                  <div
                    class="absolute left-0 right-0 top-full z-10 mt-1 max-h-56 overflow-y-auto rounded border border-[var(--line)] bg-[var(--panel)] shadow-lg"
                  >
                    {#each notifyTargetResults as actor (actor.id)}
                      <button
                        class="flex w-full items-center gap-2 px-3 py-2 text-left text-meta hover:bg-[var(--bg-soft)]"
                        type="button"
                        onclick={() => chooseNotifyTarget(actor)}
                      >
                        <span
                          class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-[var(--accent)]/15 text-micro font-semibold text-[var(--accent)]"
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
            class="rounded border border-danger/40 bg-danger-soft px-3 py-2 text-meta text-danger-text"
            role="alert"
          >
            {submitError}
          </div>
        {/if}

        <div class="flex items-center justify-between gap-3">
          <p class="text-meta text-fg-muted">⌘+Enter to submit</p>
          <Button type="submit" variant="primary" disabled={submitting}>
            {submitting ? "Sending..." : "Send response"}
          </Button>
        </div>
      </form>
    </section>
  {/if}
</div>
