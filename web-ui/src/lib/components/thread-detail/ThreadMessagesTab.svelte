<script>
  import { actorRegistry, lookupActorDisplayName } from "$lib/actorSession";
  import ThreadMessageItem from "$lib/components/thread-detail/ThreadMessageItem.svelte";
  import {
    flattenMessageThreadView,
    toMessageThreadView,
  } from "$lib/messageThreadUtils";
  import { threadDetailStore } from "$lib/threadDetailStore";

  let { threadId, onMessagePost } = $props();

  let timeline = $derived($threadDetailStore.timeline);
  let timelineLoading = $derived($threadDetailStore.timelineLoading);
  let timelineError = $derived($threadDetailStore.timelineError);

  let actorName = $derived((id) => lookupActorDisplayName(id, $actorRegistry));
  let messageThreads = $derived(toMessageThreadView(timeline, { threadId }));
  let allMessages = $derived(flattenMessageThreadView(messageThreads));
  let replyTargetMessage = $derived(
    replyToEventId
      ? (allMessages.find((message) => message.id === replyToEventId) ?? null)
      : null,
  );

  let messageText = $state("");
  let replyToEventId = $state("");
  let postingMessage = $state(false);
  let postMessageError = $state("");

  let canPost = $derived(Boolean(messageText.trim()) && !postingMessage);

  function setReplyTarget(eventId) {
    replyToEventId = eventId;
  }

  function clearReplyTarget() {
    replyToEventId = "";
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
    } catch (error) {
      postMessageError = `Failed to post: ${error instanceof Error ? error.message : String(error)}`;
    } finally {
      postingMessage = false;
    }
  }
</script>

<div
  class="mt-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] p-4"
>
  {#if postMessageError}
    <p class="mb-3 rounded bg-red-500/10 px-3 py-1.5 text-[12px] text-red-400">
      {postMessageError}
    </p>
  {/if}
  <label
    class="mb-2 block text-[12px] font-medium text-[var(--ui-text-muted)]"
    for="message-text"
  >
    Message
  </label>
  <textarea
    bind:value={messageText}
    aria-label="Message"
    class="w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
    id="message-text"
    placeholder="Write a message..."
    rows="3"
  ></textarea>
  <div class="mt-2 flex items-center justify-between gap-2">
    <div
      class="flex min-w-0 items-center gap-2 text-[12px] text-[var(--ui-text-muted)]"
    >
      {#if replyToEventId}
        <span class="truncate">
          Replying to: {replyTargetMessage?.messageText
            ? replyTargetMessage.messageText.slice(0, 80)
            : "message"}
        </span>
        <button
          class="cursor-pointer shrink-0 text-indigo-400 hover:text-indigo-300"
          onclick={clearReplyTarget}
          type="button"
        >
          Clear
        </button>
      {/if}
    </div>
    <button
      class="cursor-pointer rounded bg-indigo-600 px-4 py-1.5 text-[12px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
      disabled={!canPost}
      onclick={handlePostMessage}
      type="button"
    >
      {postingMessage ? "Posting..." : "Post message"}
    </button>
  </div>
</div>

<div class="mt-4">
  <h2
    class="mb-3 text-[12px] font-semibold uppercase tracking-wider text-[var(--ui-text-muted)]"
  >
    Messages
  </h2>
  {#if timelineLoading}
    <p class="text-[13px] text-[var(--ui-text-muted)]">Loading messages...</p>
  {:else if timelineError}
    <p class="rounded bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
      {timelineError}
    </p>
  {:else if messageThreads.length === 0}
    <p class="text-[13px] text-[var(--ui-text-muted)]">No messages yet.</p>
  {:else}
    <div class="space-y-3">
      {#each messageThreads as message}
        <ThreadMessageItem
          {message}
          {threadId}
          {actorName}
          onReply={setReplyTarget}
        />
      {/each}
    </div>
  {/if}
</div>
