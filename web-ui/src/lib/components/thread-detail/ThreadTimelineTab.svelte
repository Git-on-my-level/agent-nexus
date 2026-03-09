<script>
  import { threadDetailStore } from "$lib/threadDetailStore";
  import { actorRegistry, lookupActorDisplayName } from "$lib/actorSession";
  import { formatTimestamp } from "$lib/formatDate";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { toTimelineView } from "$lib/timelineUtils";

  let { threadId, onMessagePost } = $props();

  let timeline = $derived($threadDetailStore.timeline);
  let timelineLoading = $derived($threadDetailStore.timelineLoading);
  let timelineError = $derived($threadDetailStore.timelineError);

  let actorName = $derived((id) => lookupActorDisplayName(id, $actorRegistry));

  let timelineView = $derived(toTimelineView(timeline, { threadId }));

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

<div class="mt-4 rounded-lg border border-gray-200 bg-white p-4">
  {#if postMessageError}<p
      class="mb-3 rounded bg-red-50 px-3 py-1.5 text-xs text-red-700"
    >
      {postMessageError}
    </p>{/if}
  <textarea
    bind:value={messageText}
    class="w-full rounded border border-gray-200 px-3 py-2 text-sm"
    id="message-text"
    placeholder="Write a message..."
    rows="2"
  ></textarea>
  <div class="mt-2 flex items-center justify-between gap-2">
    <div class="flex items-center gap-2 text-xs text-gray-400">
      {#if replyToEventId}
        <span>Replying to event</span>
        <button
          class="text-indigo-600 hover:text-indigo-800"
          onclick={clearReplyTarget}
          type="button">Clear</button
        >
      {/if}
    </div>
    <button
      class="rounded bg-indigo-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
      disabled={!canPost}
      onclick={handlePostMessage}
      type="button"
    >
      {postingMessage ? "Posting..." : "Post"}
    </button>
  </div>
</div>

<div class="mt-4">
  <h2 class="mb-3 text-xs font-semibold uppercase tracking-wider text-gray-400">
    Timeline
  </h2>
  {#if timelineLoading}
    <p class="text-sm text-gray-400">Loading timeline...</p>
  {:else if timelineError}
    <p class="rounded bg-red-50 px-3 py-2 text-sm text-red-700">
      {timelineError}
    </p>
  {:else if timelineView.length === 0}
    <p class="text-sm text-gray-400">No events yet.</p>
  {:else}
    <div class="space-y-1">
      {#each timelineView as event}
        <div
          class="group rounded-md border border-gray-200 bg-white px-4 py-2.5"
          id={`event-${event.id}`}
        >
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0 flex-1">
              <MarkdownRenderer
                source={event.summary}
                class="text-sm text-gray-900"
              />
              <p class="mt-0.5 text-xs text-gray-400">
                {actorName(event.actor_id)} · {event.typeLabel} · {formatTimestamp(
                  event.ts,
                ) || "—"}
              </p>
            </div>
            <button
              class="shrink-0 rounded px-2 py-0.5 text-xs text-gray-400 opacity-0 transition-opacity hover:bg-gray-100 hover:text-gray-600 group-hover:opacity-100"
              onclick={() => setReplyTarget(event.id)}
              type="button">Reply</button
            >
          </div>

          {#if event.changedFields.length > 0}
            <div class="mt-1.5 flex flex-wrap gap-1 text-xs">
              {#each event.changedFields as field}
                <span class="rounded bg-gray-100 px-1.5 py-0.5 text-gray-500"
                  >{field}</span
                >
              {/each}
            </div>
          {/if}

          {#if event.refs.length > 0}
            <div class="mt-1.5 flex flex-wrap gap-1.5 text-xs">
              {#each event.refs as refValue}<RefLink
                  {refValue}
                  {threadId}
                />{/each}
            </div>
          {/if}

          {#if !event.isKnownType}
            <details class="mt-1.5">
              <summary class="cursor-pointer text-xs text-gray-400"
                >Details</summary
              >
              <pre
                class="mt-1 overflow-auto rounded bg-gray-50 p-2 text-[11px] text-gray-600">{JSON.stringify(
                  event.payload ?? {},
                  null,
                  2,
                )}</pre>
            </details>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>
