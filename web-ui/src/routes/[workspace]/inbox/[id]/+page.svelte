<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { onDestroy, onMount } from "svelte";

  import RefLink from "$lib/components/RefLink.svelte";
  import { coreClient } from "$lib/coreClient";
  import {
    appPath,
    WORKSPACE_HEADER,
    workspacePath,
  } from "$lib/workspacePaths";

  let workspaceSlug = $derived($page.params.workspace);
  let inboxItemID = $derived($page.params.id);

  let loading = $state(false);
  let loadError = $state("");
  let askItem = $state(null);

  let answerDraft = $state("");
  let saveAsDecision = $state(false);
  let notifyAskingAgent = $state(true);
  let submitting = $state(false);
  let submitError = $state("");
  let submitMessage = $state("");

  let autosaveInterval = null;

  function workspaceHref(pathname = "/") {
    return workspacePath(workspaceSlug, pathname);
  }

  function draftStorageKey() {
    return `anx.ask.draft:${workspaceSlug}:${inboxItemID}`;
  }

  function coverageHintGloss(rawHint) {
    const hint = String(rawHint ?? "")
      .trim()
      .toLowerCase();
    if (!hint) {
      return "No explicit coverage hint was provided.";
    }
    if (hint.startsWith("thin")) {
      return "The agent had very little prior evidence when it asked this.";
    }
    if (hint.startsWith("partial") || hint.startsWith("medium")) {
      return "The agent had partial context, but likely needed human judgment.";
    }
    if (hint.startsWith("strong") || hint.startsWith("high")) {
      return "The agent had substantial context and is asking for final direction.";
    }
    return "Coverage hint is advisory context from the asking agent envelope.";
  }

  function hasTopicRef(item) {
    const subjectRef = String(item?.subject_ref ?? "").trim();
    if (subjectRef.startsWith("topic:")) {
      return true;
    }
    const relatedRefs = Array.isArray(item?.related_refs)
      ? item.related_refs
      : [];
    return relatedRefs.some((ref) =>
      String(ref ?? "")
        .trim()
        .startsWith("topic:"),
    );
  }

  function firstTopicRef(subjectRef, relatedRefs) {
    const normalizedSubjectRef = String(subjectRef ?? "").trim();
    if (normalizedSubjectRef.startsWith("topic:")) {
      return normalizedSubjectRef;
    }

    if (!Array.isArray(relatedRefs)) {
      return "";
    }

    for (const ref of relatedRefs) {
      const normalizedRef = String(ref ?? "").trim();
      if (normalizedRef.startsWith("topic:")) {
        return normalizedRef;
      }
    }

    return "";
  }

  function handleTextareaKeydown(event) {
    if (event.key !== "Enter") return;
    if (!event.metaKey) return;
    event.preventDefault();
    void submitResponse();
  }

  async function loadAskItem() {
    loading = true;
    loadError = "";
    submitError = "";
    submitMessage = "";
    askItem = null;

    try {
      const response = await coreClient.getInboxItem(inboxItemID);
      const item = response.item ?? null;
      if (!item) {
        loadError = "Ask item not found.";
        return;
      }
      if (
        String(item.kind ?? "")
          .trim()
          .toLowerCase() !== "ask"
      ) {
        loadError = "This inbox item is not an ASK item.";
        return;
      }
      askItem = item;
      saveAsDecision = hasTopicRef(item);
      notifyAskingAgent = true;

      if (browser) {
        const cached = localStorage.getItem(draftStorageKey());
        if (cached != null) {
          answerDraft = cached;
        }
      }
    } catch (error) {
      loadError =
        error instanceof Error
          ? `Failed to load ask item: ${error.message}`
          : String(error);
    } finally {
      loading = false;
    }
  }

  async function submitResponse() {
    if (!askItem || submitting) return;

    const answer = String(answerDraft ?? "").trim();
    if (!answer) {
      submitError = "Answer is required.";
      return;
    }

    submitError = "";
    submitMessage = "";
    submitting = true;

    try {
      const response = await fetch(
        appPath(`/inbox/${encodeURIComponent(inboxItemID)}/respond`),
        {
          method: "POST",
          headers: {
            "content-type": "application/json",
            accept: "application/json",
            [WORKSPACE_HEADER]: workspaceSlug,
          },
          body: JSON.stringify({
            answer,
            save_as_decision: saveAsDecision,
            notify_asking_agent: notifyAskingAgent,
          }),
        },
      );

      const body = await response.json().catch(() => ({}));
      if (!response.ok) {
        const message =
          body?.error?.message ||
          `Failed to submit ask response (HTTP ${response.status}).`;
        submitError = String(message);
        return;
      }

      const queued = Boolean(body?.notify?.queued);
      if (queued) {
        submitMessage = "Queued — will deliver when agent reconnects.";
      } else if (notifyAskingAgent) {
        submitMessage = "Response sent and asking agent notified.";
      } else {
        submitMessage = "Response saved.";
      }

      if (browser) {
        localStorage.removeItem(draftStorageKey());
      }
      answerDraft = "";
      await goto(workspaceHref("/inbox"), {
        replaceState: false,
        noScroll: false,
        keepFocus: false,
      });
    } catch (error) {
      submitError =
        error instanceof Error
          ? `Failed to submit ask response: ${error.message}`
          : String(error);
    } finally {
      submitting = false;
    }
  }

  onMount(() => {
    void loadAskItem();

    autosaveInterval = setInterval(() => {
      if (!browser || !askItem) return;
      localStorage.setItem(draftStorageKey(), String(answerDraft ?? ""));
    }, 2000);
  });

  onDestroy(() => {
    if (autosaveInterval != null) {
      clearInterval(autosaveInterval);
      autosaveInterval = null;
    }
  });
</script>

<div class="mx-auto max-w-3xl space-y-6 px-4 py-6">
  <header
    class="flex items-center justify-between border-b border-[var(--line)] pb-4"
  >
    <div>
      <h1 class="text-display font-semibold leading-[1.3] text-fg">
        Answer ask
      </h1>
      <p class="mt-2 text-body text-fg">
        Capture a concrete answer for the asking agent.
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
    <div
      class="rounded border border-[var(--line)] bg-[var(--bg-soft)] p-4 text-meta text-fg-muted"
    >
      Loading ask context...
    </div>
  {:else if loadError}
    <div
      class="rounded border border-danger/40 bg-danger-soft p-4 text-meta text-danger-text"
      role="alert"
    >
      {loadError}
    </div>
  {:else if askItem}
    <section class="space-y-4">
      <div
        class="rounded border border-[var(--line)] bg-[var(--bg-soft)] p-4"
      >
        <h2 class="text-subtitle font-semibold text-fg">
          Context the agent saw
        </h2>
        <p class="mt-3 text-body text-fg">
          {askItem.query_text ?? askItem.title}
        </p>
        <div class="mt-3 text-meta text-fg-muted">
          Asking session:
          <span class="font-mono text-mono text-fg"
            >{askItem.asking_agent_id || "unknown session"}</span
          >
        </div>
        <div
          class="mt-3 rounded border border-[var(--line)] bg-white p-3 text-meta text-fg-muted"
        >
          <p>
            <span class="font-semibold text-fg">coverage_hint</span>
            <span class="font-mono text-mono text-fg">
              {askItem.coverage_hint || "n/a"}</span
            >
          </p>
          <p class="mt-2">{coverageHintGloss(askItem.coverage_hint)}</p>
        </div>
        {#if Array.isArray(askItem.related_refs) && askItem.related_refs.length > 0}
          <div class="mt-3 flex flex-wrap items-center gap-2 text-meta">
            {#each askItem.related_refs.slice(0, 2) as refValue}
              <RefLink {refValue} threadId={askItem.thread_id} humanize />
            {/each}
          </div>
        {/if}
      </div>

      <form
        class="rounded border border-[var(--line)] bg-[var(--bg-soft)] p-4"
        onsubmit={(event) => {
          event.preventDefault();
          void submitResponse();
        }}
      >
        <label class="block text-meta text-fg-muted" for="ask-response-input"
          >Your answer</label
        >
        <textarea
          id="ask-response-input"
          class="mt-2 min-h-[200px] w-full rounded border border-[var(--line)] bg-white px-4 py-3 text-body text-fg outline-none focus:ring-2 focus:ring-accent"
          bind:value={answerDraft}
          onkeydown={handleTextareaKeydown}
          placeholder="Write the answer the next agent should rely on."
        ></textarea>

        <div class="mt-4 space-y-2 text-meta text-fg">
          <label class="flex items-start gap-2">
            <input type="checkbox" bind:checked={saveAsDecision} class="mt-1" />
            <span>
              Save as decision in topic
              <span class="font-mono"
                >"{firstTopicRef(askItem.subject_ref, askItem.related_refs) ||
                  "(none)"}"</span
              >
            </span>
          </label>
          <label class="flex items-start gap-2">
            <input
              type="checkbox"
              bind:checked={notifyAskingAgent}
              class="mt-1"
            />
            <span>
              Notify {askItem.asking_agent_id || "session"} (will wake if idle)
            </span>
          </label>
        </div>

        {#if submitError}
          <div
            class="mt-4 rounded border border-danger/40 bg-danger-soft px-3 py-2 text-meta text-danger-text"
            role="alert"
          >
            {submitError}
          </div>
        {/if}
        {#if submitMessage}
          <div
            class="mt-4 rounded border border-[var(--line)] bg-white px-3 py-2 text-meta text-fg"
          >
            {submitMessage}
          </div>
        {/if}

        <div class="mt-4 flex items-center justify-between gap-3">
          <p class="text-meta text-fg-muted">Cmd+Enter submits</p>
          <Button type="submit" variant="primary" disabled={submitting}>{submitting ? "Sending…" : "Send answer"}</Button>
        </div>
      </form>
    </section>
  {/if}
</div>
