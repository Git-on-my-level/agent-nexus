<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";
  import { tick } from "svelte";
  import { get } from "svelte/store";

  import {
    actorRegistry,
    lookupActorDisplayName,
    principalRegistry,
  } from "$lib/actorSession";
  import { authenticatedAgent } from "$lib/authSession";
  import {
    getAccessDevMockData,
    isAccessDevPreview,
  } from "$lib/accessDevMock.js";
  import { listAllPrincipals } from "$lib/authPrincipals";
  import { coreClient } from "$lib/coreClient";
  import { enrichPrincipalsWithWakeRouting } from "$lib/principalWakeRouting.js";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import ThreadMessageItem from "$lib/components/thread-detail/ThreadMessageItem.svelte";
  import {
    flattenMessageThreadView,
    toMessageThreadView,
  } from "$lib/messageThreadUtils";
  import {
    filterMentionCandidates,
    parseActiveMention,
    taggableAgentHandlesFromPrincipals,
  } from "$lib/threadMentionUtils.js";
  import { threadDetailStore } from "$lib/threadDetailStore";
  import { workspacePath } from "$lib/workspacePaths";

  let { threadId, onMessagePost, workspaceId = "" } = $props();

  let timeline = $derived($threadDetailStore.timeline);
  let timelineLoading = $derived($threadDetailStore.timelineLoading);
  let timelineError = $derived($threadDetailStore.timelineError);
  let workspaceSlug = $derived($page.params.workspace);

  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );

  let showArchived = $state(false);
  let confirmModal = $state({ open: false, action: "", eventId: "" });
  let lifecycleBusy = $state(false);
  let lifecycleError = $state("");

  let filteredTimeline = $derived(
    (Array.isArray(timeline) ? timeline : []).filter((event) => {
      if (event.tombstoned_at) return false;
      if (!showArchived && event.archived_at) return false;
      return true;
    }),
  );
  let messageThreads = $derived(
    toMessageThreadView(filteredTimeline, { threadId }),
  );
  let allMessages = $derived(flattenMessageThreadView(messageThreads));
  let hasMessages = $derived(messageThreads.length > 0);
  let archivedMessageCount = $derived(
    (Array.isArray(timeline) ? timeline : []).filter(
      (e) =>
        String(e?.type ?? "") === "message_posted" &&
        e.archived_at &&
        !e.tombstoned_at,
    ).length,
  );
  let timelineHasAnyMessagePosted = $derived(
    (Array.isArray(timeline) ? timeline : []).some(
      (e) => String(e?.type ?? "") === "message_posted",
    ),
  );
  let hasAnyNonTombstonedMessage = $derived(
    (Array.isArray(timeline) ? timeline : []).some(
      (e) => String(e?.type ?? "") === "message_posted" && !e.tombstoned_at,
    ),
  );
  let showSyncStatus = $derived(timelineLoading && timelineHasAnyMessagePosted);
  let replyTargetMessage = $derived(
    replyToEventId
      ? (allMessages.find((message) => message.id === replyToEventId) ?? null)
      : null,
  );

  let messageText = $state("");
  let replyToEventId = $state("");
  let postingMessage = $state(false);
  let postMessageError = $state("");

  let mentionCandidates = $state([]);
  let mentionLoading = $state(false);
  let mentionOpen = $state(false);
  let mentionQuery = $state("");
  let mentionHighlight = $state(0);
  let mentionSignedIn = $state(false);
  let textareaRef = $state(null);

  let filteredMentions = $derived(
    filterMentionCandidates(mentionCandidates, mentionQuery).slice(0, 12),
  );

  let canPost = $derived(Boolean(messageText.trim()) && !postingMessage);

  async function refreshMentionCandidates() {
    if (!browser) {
      return;
    }
    mentionLoading = true;
    try {
      const agent = get(authenticatedAgent);
      const reg = get(actorRegistry);
      const principals = get(principalRegistry);
      const nameFn = (id) => lookupActorDisplayName(id, reg, principals);
      mentionSignedIn = Boolean(agent);

      if (agent) {
        const fetchedPrincipals = await listAllPrincipals(coreClient, {
          limit: 100,
        });
        const enrichedPrincipals = await enrichPrincipalsWithWakeRouting(
          fetchedPrincipals,
          {
            workspaceBindingTarget: workspaceId,
            client: coreClient,
          },
        );
        mentionCandidates = taggableAgentHandlesFromPrincipals(
          enrichedPrincipals,
          nameFn,
        );
      } else if (isAccessDevPreview) {
        mentionCandidates = taggableAgentHandlesFromPrincipals(
          getAccessDevMockData().principals,
          nameFn,
        );
      } else {
        mentionCandidates = [];
      }
    } catch {
      mentionCandidates = [];
    } finally {
      mentionLoading = false;
    }
  }

  $effect(() => {
    if (!browser) {
      return;
    }
    void $authenticatedAgent?.agent_id;
    void workspaceId;
    void refreshMentionCandidates();
  });

  function updateMentionFromTextarea() {
    const el = textareaRef;
    if (!el) {
      return;
    }
    const parsed = parseActiveMention(messageText, el.selectionStart);
    if (!parsed) {
      mentionOpen = false;
      return;
    }
    const prev = mentionQuery;
    mentionQuery = parsed.query;
    if (prev !== parsed.query) {
      mentionHighlight = 0;
    }
    mentionOpen = true;
  }

  function closeMentions() {
    mentionOpen = false;
  }

  async function insertMention(handle) {
    const el = textareaRef;
    if (!el) {
      return;
    }
    const value = messageText;
    const sel = el.selectionStart;
    const parsed = parseActiveMention(value, sel);
    if (!parsed) {
      closeMentions();
      return;
    }
    const before = value.slice(0, parsed.atIndex);
    const after = value.slice(sel);
    const insertion = `@${handle} `;
    messageText = before + insertion + after;
    closeMentions();
    await tick();
    const pos = before.length + insertion.length;
    el.focus();
    el.setSelectionRange(pos, pos);
  }

  function handleMessageKeydown(e) {
    if (!mentionOpen) {
      return;
    }
    const list = filterMentionCandidates(mentionCandidates, mentionQuery).slice(
      0,
      12,
    );
    if (e.key === "Escape") {
      e.preventDefault();
      closeMentions();
      return;
    }
    if (list.length === 0) {
      return;
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      mentionHighlight = (mentionHighlight + 1) % list.length;
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      mentionHighlight = (mentionHighlight - 1 + list.length) % list.length;
    } else if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void insertMention(list[mentionHighlight].handle);
    } else if (e.key === "Tab" && !e.shiftKey) {
      e.preventDefault();
      void insertMention(list[mentionHighlight].handle);
    }
  }

  function setReplyTarget(eventId) {
    replyToEventId = eventId;
  }

  function clearReplyTarget() {
    replyToEventId = "";
  }

  async function refreshTimeline() {
    await threadDetailStore.queueRefreshThreadDetail(threadId, {
      timeline: true,
    });
  }

  function openArchiveConfirm(eventId) {
    confirmModal = { open: true, action: "archive", eventId };
  }

  function openTombstoneConfirm(eventId) {
    confirmModal = { open: true, action: "trash", eventId };
  }

  function handleConfirm() {
    const { action, eventId } = confirmModal;
    confirmModal = { open: false, action: "", eventId: "" };
    if (action === "archive") doArchive(eventId);
    else if (action === "trash") doTombstone(eventId);
  }

  async function doArchive(eventId) {
    if (!eventId || lifecycleBusy) return;
    lifecycleBusy = true;
    lifecycleError = "";
    try {
      await coreClient.archiveEvent(eventId, {});
      await refreshTimeline();
    } catch (e) {
      lifecycleError = `Archive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      lifecycleBusy = false;
    }
  }

  async function doUnarchive(eventId) {
    if (!eventId || lifecycleBusy) return;
    lifecycleBusy = true;
    lifecycleError = "";
    try {
      await coreClient.unarchiveEvent(eventId, {});
      await refreshTimeline();
    } catch (e) {
      lifecycleError = `Unarchive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      lifecycleBusy = false;
    }
  }

  async function doTombstone(eventId) {
    if (!eventId || lifecycleBusy) return;
    lifecycleBusy = true;
    lifecycleError = "";
    try {
      await coreClient.tombstoneEvent(eventId, {});
      await refreshTimeline();
    } catch (e) {
      lifecycleError = `Trash failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      lifecycleBusy = false;
    }
  }

  async function handlePostMessage() {
    if (!messageText.trim()) {
      postMessageError = "Message text is required.";
      return;
    }
    postingMessage = true;
    postMessageError = "";
    try {
      await onMessagePost(threadId, {
        type: "message_posted",
        thread_id: threadId,
        refs: [
          `thread:${threadId}`,
          ...(replyToEventId ? [`event:${replyToEventId}`] : []),
        ],
        summary: `Message: ${messageText.trim().slice(0, 100)}`,
        payload: { text: messageText.trim() },
        provenance: { sources: ["actor_statement:ui"] },
      });
      messageText = "";
      replyToEventId = "";
      closeMentions();
    } catch (error) {
      postMessageError = `Failed to post: ${error instanceof Error ? error.message : String(error)}`;
    } finally {
      postingMessage = false;
    }
  }
</script>

<div
  class="mt-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] p-3"
>
  {#if postMessageError}
    <p class="mb-2 rounded bg-red-500/10 px-3 py-1.5 text-[12px] text-red-400">
      {postMessageError}
    </p>
  {/if}
  <label
    class="mb-1.5 block text-[12px] font-medium text-[var(--ui-text-muted)]"
    for="message-text"
  >
    Message
  </label>
  <div class="relative">
    <textarea
      bind:this={textareaRef}
      bind:value={messageText}
      aria-label="Message"
      class="w-full min-h-[4.25rem] resize-y rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
      id="message-text"
      oninput={updateMentionFromTextarea}
      onclick={updateMentionFromTextarea}
      onkeyup={updateMentionFromTextarea}
      onkeydown={handleMessageKeydown}
      placeholder="Write a message..."
      rows="2"
    ></textarea>
    {#if mentionOpen}
      <div
        class="absolute left-0 right-0 top-full z-20 mt-1 max-h-48 overflow-auto rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] py-1 shadow-lg"
        id="message-mention-list"
        role="listbox"
        aria-label="Agent handles"
      >
        {#if mentionLoading}
          <p class="px-3 py-2 text-[12px] text-[var(--ui-text-muted)]">
            Loading handles…
          </p>
        {:else if mentionCandidates.length === 0}
          {#if mentionSignedIn}
            <p class="px-3 py-2 text-[12px] text-[var(--ui-text-muted)]">
              No registered agents are taggable in this workspace. See Access to
              check registration and presence.
            </p>
          {:else}
            <p class="px-3 py-2 text-[12px] text-[var(--ui-text-muted)]">
              No agent handles in this workspace. Sign in or open Access to
              manage agents.
            </p>
          {/if}
        {:else if filteredMentions.length === 0}
          <p class="px-3 py-2 text-[12px] text-[var(--ui-text-muted)]">
            No matching agents.
          </p>
        {:else}
          {#each filteredMentions as row, i (row.handle)}
            <button
              type="button"
              class="flex w-full cursor-pointer items-baseline gap-2 px-3 py-1.5 text-left text-[12px] hover:bg-[var(--ui-panel-muted)] {i ===
              mentionHighlight
                ? 'bg-[var(--ui-panel-muted)]'
                : ''}"
              aria-selected={i === mentionHighlight}
              role="option"
              onmousedown={(e) => {
                e.preventDefault();
                void insertMention(row.handle);
              }}
            >
              <span class="font-medium text-[var(--ui-accent)]"
                >@{row.handle}</span
              >
              <span class="truncate text-[var(--ui-text-muted)]"
                >{row.displayLabel}</span
              >
              <span
                class="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium {row.presenceClass}"
                title={row.presenceSummary}
              >
                {row.presenceLabel}
              </span>
            </button>
          {/each}
        {/if}
      </div>
    {/if}
  </div>
  <div
    class="mt-1.5 flex flex-col gap-1.5 sm:flex-row sm:items-center sm:justify-between sm:gap-3"
  >
    <p
      class="text-[11px] leading-snug text-[var(--ui-text-muted)] sm:min-w-0 sm:flex-1"
    >
      Mention <code class="text-[var(--ui-text)]">@handle</code> to wake a
      registered agent in this workspace. See
      <a
        class="text-indigo-400 hover:text-indigo-300"
        href={workspacePath(workspaceSlug, "/access")}>Access</a
      >
      for agent presence and registration status.
    </p>
    <div
      class="flex shrink-0 flex-wrap items-center justify-end gap-2 sm:justify-end"
    >
      {#if replyToEventId}
        <span
          class="max-w-[14rem] truncate text-[11px] text-[var(--ui-text-muted)]"
        >
          Replying to: {replyTargetMessage?.messageText
            ? replyTargetMessage.messageText.slice(0, 80)
            : "message"}
        </span>
        <button
          class="cursor-pointer shrink-0 text-[11px] text-indigo-400 hover:text-indigo-300"
          onclick={clearReplyTarget}
          type="button"
        >
          Clear
        </button>
      {/if}
      <button
        class="cursor-pointer rounded bg-indigo-600 px-3 py-1 text-[12px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
        disabled={!canPost}
        onclick={handlePostMessage}
        type="button"
      >
        {postingMessage ? "Posting..." : "Post message"}
      </button>
    </div>
  </div>
</div>

<div class="mt-4">
  <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
    <div class="flex flex-wrap items-center gap-3">
      <h2
        class="text-[12px] font-semibold uppercase tracking-wider text-[var(--ui-text-muted)]"
      >
        Messages
      </h2>
      {#if archivedMessageCount > 0}
        <label
          class="flex items-center gap-1.5 text-[11px] text-[var(--ui-text-muted)]"
        >
          <input
            type="checkbox"
            bind:checked={showArchived}
            class="accent-[var(--ui-accent)]"
          />
          Show archived ({archivedMessageCount})
        </label>
      {/if}
    </div>
    <div class="min-h-[1rem] text-right" aria-live="polite">
      {#if showSyncStatus}
        <p class="text-[11px] text-[var(--ui-text-muted)]">Syncing…</p>
      {/if}
    </div>
  </div>
  {#if timelineError && !hasAnyNonTombstonedMessage}
    <p class="rounded bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
      {timelineError}
    </p>
  {:else if timelineLoading && !hasAnyNonTombstonedMessage}
    <p class="text-[13px] text-[var(--ui-text-muted)]">Loading messages...</p>
  {:else if !hasAnyNonTombstonedMessage}
    <p class="text-[13px] text-[var(--ui-text-muted)]">No messages yet.</p>
  {:else if !hasMessages}
    <p class="text-[13px] text-[var(--ui-text-muted)]">
      No messages in view. Turn on Show archived to see archived threads.
    </p>
  {:else}
    {#if lifecycleError}
      <p class="mb-2 rounded bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
        {lifecycleError}
      </p>
    {/if}
    {#if timelineError}
      <p class="mb-2 rounded bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
        {timelineError}
      </p>
    {/if}
    <div class="space-y-3">
      {#each messageThreads as message (message.id)}
        <ThreadMessageItem
          {message}
          {threadId}
          {actorName}
          onReply={setReplyTarget}
          onArchive={openArchiveConfirm}
          onTombstone={openTombstoneConfirm}
          onUnarchive={doUnarchive}
          {lifecycleBusy}
        />
      {/each}
    </div>
  {/if}
</div>

<ConfirmModal
  open={confirmModal.open}
  title={confirmModal.action === "trash"
    ? "Move message to trash"
    : "Archive message"}
  message={confirmModal.action === "trash"
    ? "This message and all its replies will be tombstoned. You can restore them from trash later."
    : "This message and all its replies will be archived. Toggle 'Show archived' to see them again."}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={lifecycleBusy}
  onconfirm={handleConfirm}
  oncancel={() => (confirmModal = { open: false, action: "", eventId: "" })}
/>
