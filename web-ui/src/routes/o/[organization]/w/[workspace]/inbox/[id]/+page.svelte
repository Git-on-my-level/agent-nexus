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

  const KIND_LABELS = {
    ask: "Ask",
    review: "Review",
    escalate: "Escalation",
  };

  const RESPONSE_PRESETS = {
    ask: ["Use this direction:", "I need more context:", "Do not proceed yet."],
    review: ["Approved.", "Rejected.", "Request changes:"],
    escalate: [
      "Acknowledged. Investigating now.",
      "Not an issue.",
      "Pause and wait for follow-up.",
    ],
  };

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

  async function submitResponse() {
    if (!item || submitting) return;
    const responseText = String(responseDraft ?? "").trim();
    if (!responseText) {
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
        response_text: responseText,
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

<div class="mx-auto max-w-3xl space-y-6 px-4 py-4 max-md:space-y-4 max-md:py-3">
  <header
    class="flex items-center justify-between border-b border-[var(--line)] pb-3 max-md:pb-2"
  >
    <div>
      <h1 class="text-display font-semibold leading-[1.3] text-fg">
        Respond to {kindLabel()}
      </h1>
      <p class="mt-2 text-body text-fg">
        Record a freeform response and notify the requesting agent when
        possible.
      </p>
    </div>
    <a
      class="rounded border border-[var(--line)] px-3 py-2 text-meta text-fg-muted hover:bg-[var(--bg-soft)]"
      href={workspaceHref("/inbox")}
    >
      Back to inbox
    </a>
  </header>

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
    <section class="space-y-4">
      <div class="rounded border border-[var(--line)] bg-[var(--bg-soft)] p-4">
        <div class="flex flex-wrap items-center gap-2">
          <span
            class="rounded border border-[var(--line)] bg-[var(--panel)] px-2 py-1 text-micro font-semibold uppercase tracking-wide text-fg-muted"
          >
            {kindLabel(item)}
          </span>
          {#if item.severity}
            <span
              class="rounded border border-danger/30 bg-danger-soft px-2 py-1 text-micro font-semibold uppercase tracking-wide text-danger-text"
            >
              {item.severity}
            </span>
          {/if}
        </div>
        <h2 class="mt-3 text-subtitle font-semibold text-fg">
          {item.title}
        </h2>
        {#if item.body}
          <p class="mt-3 whitespace-pre-wrap text-body text-fg">{item.body}</p>
        {/if}
        <div class="mt-3 text-meta text-fg-muted">
          Requested by <span class="font-mono text-mono text-fg"
            >{requesterLabel(item)}</span
          >
        </div>
        <div class="mt-3 flex flex-wrap items-center gap-2 text-meta">
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
      </div>

      <form
        class="rounded border border-[var(--line)] bg-[var(--bg-soft)] p-4"
        onsubmit={(event) => {
          event.preventDefault();
          void submitResponse();
        }}
      >
        <div
          class="rounded border border-[var(--line)] bg-[var(--panel)] px-3 py-2 text-meta text-fg"
        >
          <div class="font-semibold">Notification target</div>
          <p class="mt-1 text-fg-muted">
            {notificationStatus().message ??
              "Original requester will be notified when resolvable."}
          </p>
          <label class="mt-3 flex items-start gap-2">
            <input
              type="radio"
              bind:group={notifyMode}
              value="original"
              disabled={notificationStatus().resolvable === false}
            />
            <span>Notify original requester</span>
          </label>
          <label class="mt-2 flex items-start gap-2">
            <input type="radio" bind:group={notifyMode} value="none" />
            <span>Record without notification</span>
          </label>
          <label class="mt-2 flex items-start gap-2">
            <input type="radio" bind:group={notifyMode} value="target" />
            <span>Notify replacement target</span>
          </label>
          {#if notifyMode === "target"}
            <div
              class="mt-3 grid gap-3 border-t border-[var(--line)] pt-3 sm:grid-cols-2"
            >
              <label class="block text-meta text-fg-muted">
                Actor ID
                <input
                  class="mt-1 w-full rounded border border-[var(--line)] bg-[var(--panel)] px-3 py-2 font-mono text-mono text-fg outline-none focus:ring-2 focus:ring-[var(--accent)]"
                  bind:value={notifyTargetActorID}
                  placeholder="actor-..."
                />
              </label>
              <label class="block text-meta text-fg-muted">
                Agent ID
                <input
                  class="mt-1 w-full rounded border border-[var(--line)] bg-[var(--panel)] px-3 py-2 font-mono text-mono text-fg outline-none focus:ring-2 focus:ring-[var(--accent)]"
                  bind:value={notifyTargetAgentID}
                  placeholder="agent-..."
                />
              </label>
            </div>
          {/if}
        </div>

        <div class="mt-4 flex flex-wrap gap-2">
          {#each RESPONSE_PRESETS[itemKind(item)] ?? [] as preset}
            <button
              class="rounded border border-[var(--line)] bg-[var(--panel)] px-3 py-1.5 text-meta text-fg hover:bg-[var(--bg-soft)]"
              type="button"
              onclick={() => applyPreset(preset)}
            >
              {preset}
            </button>
          {/each}
        </div>

        <label
          class="mt-4 block text-meta text-fg-muted"
          for="human-response-input">Response</label
        >
        <textarea
          id="human-response-input"
          class="mt-2 min-h-[200px] w-full rounded border border-[var(--line)] bg-[var(--panel)] px-4 py-3 text-body text-[var(--fg)] outline-none placeholder:text-[var(--fg-muted)] focus:ring-2 focus:ring-[var(--accent)]"
          bind:value={responseDraft}
          onkeydown={handleTextareaKeydown}
          placeholder="Write the response the agent should rely on."
        ></textarea>

        {#if submitError}
          <div
            class="mt-4 rounded border border-danger/40 bg-danger-soft px-3 py-2 text-meta text-danger-text"
            role="alert"
          >
            {submitError}
          </div>
        {/if}

        <div class="mt-4 flex items-center justify-between gap-3">
          <p class="text-meta text-fg-muted">Cmd+Enter submits</p>
          <Button type="submit" variant="primary" disabled={submitting}>
            {submitting ? "Sending..." : "Send response"}
          </Button>
        </div>
      </form>
    </section>
  {/if}
</div>
