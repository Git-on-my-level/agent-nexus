<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { onDestroy, onMount } from "svelte";

  import Button from "$lib/components/Button.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import AttachmentChip from "$lib/components/AttachmentChip.svelte";
  import { coreClient } from "$lib/coreClient";
  import { threadTimelineEventHref } from "$lib/deepLinkTargets";
  import { formatAbsoluteDateTime } from "$lib/formatDate";
  import { buildPrimitiveRefRoutes, resolveRefLink } from "$lib/refLinkModel";
  import { searchActors } from "$lib/searchHelpers";
  import { bindWorkspaceHref } from "$lib/workspacePaths";

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
  let attachingResponseFile = $state(false);
  let pendingResponseAttachmentUpload = $state(null);
  let responseAttachmentError = $state("");
  let responseAttachmentRefs = $state([]);
  let responseComposerArtifactsByRef = $state(
    /** @type {Record<string, Record<string, unknown>>} */ ({}),
  );
  let autosaveInterval = null;
  let notifyTargetQuery = $state("");
  let notifyTargetResults = $state([]);
  let notifyTargetSelected = $state(null);
  let notifyTargetSearchTimer = null;
  let notifyTargetSearchSeq = 0;
  let notifyTargetMenuOpen = $state(false);
  let inboxLoadSeq = 0;
  let loadedInboxRouteKey = $state("");

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
  let inboxComposerArtifactRoutes = $derived.by(() => {
    const artifacts = Object.values(responseComposerArtifactsByRef).filter(
      (row) => row && typeof row === "object",
    );
    if (!artifacts.length) return {};
    return buildPrimitiveRefRoutes({
      artifacts,
      events: [],
      cards: [],
      documents: [],
      threadId: String(item?.thread_id ?? "").trim(),
    }).artifactRoutesById;
  });

  let inboxRefs = $derived.by(() => {
    const refs = [];
    const seen = new Set();
    const add = (value) => {
      const ref = String(value ?? "").trim();
      if (!ref || seen.has(ref)) return;
      seen.add(ref);
      refs.push(ref);
    };
    add(item?.subject_ref);
    for (const ref of Array.isArray(item?.related_refs)
      ? item.related_refs
      : []) {
      add(ref);
    }
    return refs;
  });

  let isCompleted = $derived(String(item?.status ?? "").trim() === "completed");
  let workspaceHref = $derived(
    bindWorkspaceHref(organizationSlug, workspaceSlug),
  );

  function completedTimelineHref(value = item) {
    const tid = String(value?.thread_id ?? "").trim();
    let eventId = "";
    const ref = String(value?.response_event_ref ?? "").trim();
    if (ref.startsWith("event:")) {
      eventId = ref.slice("event:".length).trim();
    }
    if (!tid || !eventId) return "";
    return threadTimelineEventHref({
      threadId: tid,
      eventId,
      workspaceHref,
    });
  }

  function inboxRouteKey(workspace = workspaceSlug, id = inboxItemID) {
    return `${String(workspace ?? "").trim()}:${String(id ?? "").trim()}`;
  }

  function draftStorageKey(workspace = workspaceSlug, id = inboxItemID) {
    return `anx.human-response.draft:${workspace}:${id}`;
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

  function clearNotifyTargetSearchTimer() {
    if (notifyTargetSearchTimer) {
      clearTimeout(notifyTargetSearchTimer);
      notifyTargetSearchTimer = null;
    }
  }

  function resetRouteLocalState() {
    clearNotifyTargetSearchTimer();
    notifyTargetSearchSeq += 1;
    loadError = "";
    submitError = "";
    responseDraft = "";
    notifyMode = "original";
    notifyTargetActorID = "";
    notifyTargetAgentID = "";
    notifyTargetQuery = "";
    notifyTargetResults = [];
    notifyTargetSelected = null;
    notifyTargetMenuOpen = false;
    attachingResponseFile = false;
    pendingResponseAttachmentUpload = null;
    responseAttachmentError = "";
    responseAttachmentRefs = [];
    responseComposerArtifactsByRef = {};
  }

  function handleNotifyTargetInput(event) {
    const value = event.currentTarget.value;
    notifyTargetQuery = value;
    notifyTargetMenuOpen = true;
    clearNotifyTargetSearchTimer();
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

  async function loadItem(
    workspace = workspaceSlug,
    id = inboxItemID,
    seq = ++inboxLoadSeq,
  ) {
    const routeKey = inboxRouteKey(workspace, id);
    loading = true;
    resetRouteLocalState();
    item = null;
    loadedInboxRouteKey = "";

    try {
      const response = await coreClient.getInboxItem(id);
      if (seq !== inboxLoadSeq || routeKey !== inboxRouteKey()) return;
      const loaded = response.item ?? null;
      if (!loaded) {
        loadError = "Inbox item not found.";
        return;
      }
      item = loaded;
      loadedInboxRouteKey = routeKey;
      notifyMode =
        notificationStatus(loaded).resolvable === false ? "none" : "original";
      if (browser) {
        const cached = localStorage.getItem(draftStorageKey(workspace, id));
        if (cached != null) responseDraft = cached;
      }
    } catch (error) {
      if (seq !== inboxLoadSeq || routeKey !== inboxRouteKey()) return;
      loadError =
        error instanceof Error
          ? `Failed to load inbox item: ${error.message}`
          : String(error);
    } finally {
      if (seq === inboxLoadSeq && routeKey === inboxRouteKey()) {
        loading = false;
      }
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
      const resp = await coreClient.respondInboxItem(inboxItemID, {
        response_text: text,
        related_refs: responseAttachmentRefs,
        notify_mode: notifyMode,
        notify_target_actor_id:
          notifyMode === "target" && targetActorID ? targetActorID : undefined,
        notify_target_agent_id:
          notifyMode === "target" && targetAgentID ? targetAgentID : undefined,
      });
      if (browser) localStorage.removeItem(draftStorageKey());
      responseDraft = "";
      responseAttachmentRefs = [];
      responseComposerArtifactsByRef = {};
      responseAttachmentError = "";
      const eventId = String(resp?.event?.id ?? "").trim();
      const notify = resp?.notify ?? {};
      const requested = Boolean(notify.requested);
      const queued = Boolean(notify.queued);
      const qs = new URLSearchParams();
      qs.set("status", "open");
      if (eventId) qs.set("responded", eventId);
      const tid = String(item?.thread_id ?? "").trim();
      if (tid) qs.set("responded_thread", tid);
      if (requested && queued) qs.set("notify_queued", "1");
      else qs.set("notify_recorded", "1");
      await goto(`${workspaceHref("/inbox")}?${qs}`, {
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

  async function handleAttachResponseFile(event) {
    const input = event.currentTarget;
    const file = input?.files?.[0];
    if (!file || attachingResponseFile) return;
    attachingResponseFile = true;
    pendingResponseAttachmentUpload = {
      original_filename: file.name || "attachment.bin",
      content_type: file.type || "application/octet-stream",
      size_bytes: file.size,
    };
    responseAttachmentError = "";
    try {
      const threadRef = String(item?.thread_id ?? "").trim()
        ? `thread:${String(item.thread_id).trim()}`
        : "";
      const refs =
        inboxRefs.length > 0 ? inboxRefs : [threadRef].filter(Boolean);
      if (refs.length === 0) {
        responseAttachmentError = "No valid refs are available for this item.";
        return;
      }
      const payload = await coreClient.createArtifactAttachment({ refs, file });
      const id = String(payload?.artifact?.id ?? "").trim();
      if (!id) {
        responseAttachmentError = "Upload succeeded but artifact id missing.";
        return;
      }
      const ref = `artifact:${id}`;
      responseAttachmentRefs = [...new Set([...responseAttachmentRefs, ref])];
      const row =
        payload?.artifact && typeof payload.artifact === "object"
          ? payload.artifact
          : null;
      if (row) {
        responseComposerArtifactsByRef = {
          ...responseComposerArtifactsByRef,
          [ref]: /** @type {Record<string, unknown>} */ (row),
        };
      }
    } catch (error) {
      responseAttachmentError = `Upload failed: ${error instanceof Error ? error.message : String(error)}`;
    } finally {
      attachingResponseFile = false;
      pendingResponseAttachmentUpload = null;
      if (input) input.value = "";
    }
  }

  onMount(() => {
    autosaveInterval = setInterval(() => {
      if (
        !browser ||
        !item ||
        isCompleted ||
        loadedInboxRouteKey !== inboxRouteKey()
      ) {
        return;
      }
      localStorage.setItem(draftStorageKey(), String(responseDraft ?? ""));
    }, 2000);
  });

  $effect(() => {
    const workspace = workspaceSlug;
    const id = inboxItemID;
    void loadItem(workspace, id);
  });

  onDestroy(() => {
    if (autosaveInterval != null) clearInterval(autosaveInterval);
    clearNotifyTargetSearchTimer();
  });
</script>

<div class="mx-auto max-w-3xl space-y-3 px-4 py-4 max-md:px-3 max-md:py-3">
  <div class="flex items-center justify-between">
    <a
      class="text-meta text-fg-muted hover:text-fg max-md:text-micro"
      href={workspaceHref(isCompleted ? "/inbox?status=completed" : "/inbox")}
    >
      ← Back to inbox
    </a>
  </div>

  {#if loading}
    <div class="rounded border border-line bg-bg-soft p-4">
      <Skeleton rows={4} />
    </div>
  {:else if loadError && !item}
    <StateError
      message={loadError}
      onretry={() => void loadItem()}
      retrying={loading}
    />
  {:else if item}
    <section class="space-y-4 max-md:space-y-3">
      <header class="space-y-2 max-md:space-y-1.5">
        <div class="flex flex-wrap items-center gap-2 text-micro">
          <span
            class="rounded border border-line bg-panel px-2 py-0.5 font-semibold uppercase tracking-wide text-fg-muted"
          >
            {kindLabel(item)}
          </span>
          {#if item.severity}
            <span
              class="rounded border border-danger bg-danger-soft px-2 py-0.5 font-semibold uppercase tracking-wide text-danger-text"
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
          <div
            class="rounded-md border border-line bg-panel px-3 py-2 text-meta leading-relaxed text-fg"
          >
            <MarkdownRenderer source={item.body} />
          </div>
        {/if}
        {#if inboxRefs.length > 0}
          <div
            class="flex flex-wrap items-center gap-2 text-micro max-md:gap-x-2 max-md:gap-y-1"
          >
            {#each inboxRefs.slice(0, 4) as refValue}
              <RefLink
                {refValue}
                threadId={item.thread_id}
                humanize
                artifactRoutesById={inboxComposerArtifactRoutes}
              />
            {/each}
            {#if inboxRefs.length > 4}
              <span class="text-fg-muted">+{inboxRefs.length - 4} more</span>
            {/if}
          </div>
        {/if}
      </header>

      {#if isCompleted}
        <div
          class="space-y-3 rounded-md border border-line bg-bg-soft px-4 py-3 text-meta text-fg"
          data-testid="inbox-completed-detail"
        >
          {#if item.original_request_missing}
            <p class="text-micro text-warn-text">
              Original request details are unavailable for this entry.
            </p>
          {/if}
          {#if item.responded_at}
            <p class="text-micro text-fg-muted">
              Responded {formatAbsoluteDateTime(item.responded_at)}
            </p>
          {/if}
          {#if item.responding_actor_id}
            <p class="text-micro text-fg-muted">
              Responder{" "}
              <span class="font-mono text-fg">{item.responding_actor_id}</span>
            </p>
          {/if}
          <div>
            <div
              class="text-micro font-medium uppercase tracking-wide text-fg-muted"
            >
              Final response
            </div>
            <p class="mt-1 whitespace-pre-wrap text-meta text-fg">
              {item.response_text ?? ""}
            </p>
          </div>
          <div class="flex flex-wrap gap-2 pt-1">
            {#if completedTimelineHref()}
              <Button
                variant="secondary"
                size="compact"
                href={completedTimelineHref()}
              >
                Timeline event
              </Button>
            {/if}
            <Button
              variant="secondary"
              size="compact"
              href={workspaceHref("/inbox?status=completed")}
            >
              View Completed inbox
            </Button>
          </div>
        </div>
      {:else}
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
                class="rounded border border-line bg-panel px-3 py-1.5 text-meta font-semibold text-fg hover:bg-bg-soft disabled:opacity-50"
                type="button"
                disabled={submitting}
                onclick={() => void submitResponseWithText("Approved.")}
              >
                Approve
              </button>
              <button
                class="rounded border border-line bg-panel px-3 py-1.5 text-meta font-semibold text-fg hover:bg-bg-soft disabled:opacity-50"
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
              <div class="space-y-1.5">
                {#each proposalStrings as proposal, index (proposal)}
                  {@const isRecommended = index === 0}
                  {@const isSelected = responseDraft.trim() === proposal.trim()}
                  <button
                    class="group block w-full rounded border bg-panel px-3 py-2 text-left text-meta text-fg transition hover:bg-bg-soft max-md:px-2.5 max-md:py-1.5 max-md:text-micro max-md:leading-snug {isSelected
                      ? 'border-accent ring-1 ring-accent bg-accent-soft'
                      : 'border-line'}"
                    type="button"
                    onclick={() => applyPreset(proposal)}
                  >
                    <span
                      class="flex w-full flex-col items-start gap-1.5 text-left"
                    >
                      {#if isRecommended}
                        <span
                          class="shrink-0 rounded border border-accent bg-accent-soft px-2 py-0.5 text-micro font-semibold uppercase tracking-wide text-accent max-md:px-1.5 max-md:text-[11px]"
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
              class="mt-2 min-h-[200px] w-full rounded border border-line bg-panel px-3 py-2 text-meta text-fg outline-none placeholder:text-fg-muted focus:ring-2 focus:ring-accent max-md:min-h-[140px]"
              bind:value={responseDraft}
              placeholder="Write the response the agent should rely on."
            ></textarea>
          </div>

          <div class="space-y-2">
            <div class="flex flex-wrap items-center gap-2">
              <label
                class="inline-flex cursor-pointer items-center rounded border border-line bg-panel px-3 py-1.5 text-micro font-medium text-fg hover:bg-bg-soft"
              >
                {attachingResponseFile ? "Uploading…" : "Attach file"}
                <input
                  class="sr-only"
                  accept="image/*,text/plain,text/markdown,text/csv,.md,.txt,.csv,.json,.pdf"
                  disabled={attachingResponseFile || submitting}
                  onchange={handleAttachResponseFile}
                  type="file"
                />
              </label>
              {#if pendingResponseAttachmentUpload}
                {@const pendingResolved = resolveRefLink(
                  "artifact:upload-pending",
                  {
                    threadId: String(item?.thread_id ?? "").trim(),
                    boardId: "",
                    humanize: true,
                    artifactRoutesById: {},
                    eventRoutesById: {},
                    workspaceSlug,
                    organizationSlug,
                  },
                )}
                <AttachmentChip
                  resolved={pendingResolved}
                  artifactOverlay={pendingResponseAttachmentUpload}
                  pending
                  size="compact"
                />
              {/if}
              {#each responseAttachmentRefs as ref (ref)}
                {@const composerResolved = resolveRefLink(ref, {
                  threadId: String(item?.thread_id ?? "").trim(),
                  boardId: "",
                  humanize: true,
                  artifactRoutesById: inboxComposerArtifactRoutes,
                  eventRoutesById: {},
                  workspaceSlug,
                  organizationSlug,
                })}
                <span class="inline-flex max-w-full items-center gap-1">
                  <AttachmentChip resolved={composerResolved} size="compact" />
                  <button
                    class="shrink-0 text-fg-muted hover:text-fg"
                    type="button"
                    aria-label={`Remove ${ref}`}
                    onclick={() => {
                      responseAttachmentRefs = responseAttachmentRefs.filter(
                        (candidate) => candidate !== ref,
                      );
                      const next = { ...responseComposerArtifactsByRef };
                      delete next[ref];
                      responseComposerArtifactsByRef = next;
                    }}
                  >
                    ×
                  </button>
                </span>
              {/each}
            </div>
            {#if responseAttachmentError}
              <p class="text-micro text-danger-text">
                {responseAttachmentError}
              </p>
            {/if}
          </div>

          <div
            class="rounded border border-line bg-bg-soft px-3 py-2 text-meta max-md:space-y-2"
          >
            <div
              class="flex flex-wrap items-center gap-x-3 gap-y-2 max-md:block"
            >
              <span class="text-fg-muted max-md:block max-md:text-micro"
                >{notifyDescription()}</span
              >
              <div
                class="ml-auto flex flex-wrap items-center gap-1 max-md:mt-2 max-md:grid max-md:grid-cols-3 max-md:rounded-md max-md:border max-md:border-line max-md:bg-panel max-md:p-1"
              >
                <button
                  class="rounded px-2 py-1 text-micro font-medium max-md:py-1.5 {notifyMode ===
                  'original'
                    ? 'bg-accent-soft text-accent'
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
                  class="rounded px-2 py-1 text-micro font-medium max-md:py-1.5 {notifyMode ===
                  'target'
                    ? 'bg-accent-soft text-accent'
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
                  class="rounded px-2 py-1 text-micro font-medium max-md:py-1.5 {notifyMode ===
                  'none'
                    ? 'bg-accent-soft text-accent'
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
                    class="flex items-center gap-2 rounded border border-line bg-panel px-2 py-1.5"
                  >
                    <span
                      class="inline-flex items-center gap-1.5 rounded bg-accent-soft px-2 py-0.5 text-micro text-accent"
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
                    class="w-full rounded border border-line bg-panel px-2 py-1.5 text-meta text-fg outline-none placeholder:text-fg-muted focus:ring-2 focus:ring-accent"
                    type="text"
                    placeholder="Search people or agents…"
                    value={notifyTargetQuery}
                    oninput={handleNotifyTargetInput}
                    onfocus={() => (notifyTargetMenuOpen = true)}
                  />
                  {#if notifyTargetMenuOpen && notifyTargetResults.length > 0}
                    <div
                      class="absolute left-0 right-0 top-full z-10 mt-1 max-h-56 overflow-y-auto rounded border border-line bg-panel shadow-lg"
                    >
                      {#each notifyTargetResults as actor (actor.id)}
                        <button
                          class="flex w-full items-center gap-2 px-3 py-2 text-left text-meta hover:bg-bg-soft"
                          type="button"
                          onclick={() => chooseNotifyTarget(actor)}
                        >
                          <span
                            class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-accent-soft text-micro font-semibold text-accent"
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
              class="rounded border border-danger bg-danger-soft px-3 py-2 text-meta text-danger-text"
              role="alert"
            >
              {submitError}
            </div>
          {/if}

          <div class="flex items-center justify-between gap-3">
            <p class="text-meta text-fg-muted max-md:hidden">
              ⌘+Enter to submit
            </p>
            <Button
              class="max-md:w-full max-md:justify-center"
              type="submit"
              variant="primary"
              disabled={submitting}
            >
              {submitting ? "Sending..." : "Send response"}
            </Button>
          </div>
        </form>
      {/if}
    </section>
  {/if}
</div>
