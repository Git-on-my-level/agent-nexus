<script>
  import Self from "$lib/components/timeline/MessageItem.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { formatTimestamp } from "$lib/formatDate";

  let {
    message,
    threadId,
    actorName,
    onReply,
    onArchive = null,
    onTrash = null,
    onUnarchive = null,
    lifecycleBusy = false,
    depth = 0,
  } = $props();
</script>

<article
  class={`rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-4 py-3 ${depth > 0 ? "bg-[var(--ui-panel-muted)]" : ""} ${message.archived_at ? "opacity-60" : ""}`}
  id={`message-${message.id}`}
>
  <div class="flex items-start justify-between gap-3">
    <div class="min-w-0 flex-1">
      <MarkdownRenderer
        source={message.messageText || message.summary || "Untitled message"}
        class="text-[13px] text-[var(--ui-text)]"
      />
      <p class="mt-1 text-[12px] text-[var(--ui-text-muted)]">
        {actorName(message.actor_id)} · {formatTimestamp(message.ts) || "—"}
      </p>
    </div>
    <div class="flex shrink-0 items-center gap-0.5">
      {#if onArchive && !message.archived_at && !message.trashed_at}
        <button
          aria-label="Archive message"
          class="cursor-pointer rounded p-1 text-[var(--ui-text-muted)] hover:bg-[var(--ui-bg-soft)] hover:text-[var(--ui-accent)] disabled:opacity-50"
          disabled={lifecycleBusy}
          onclick={() => onArchive(message.id)}
          type="button"
        >
          <svg
            class="h-3.5 w-3.5"
            fill="currentColor"
            viewBox="0 0 24 24"
            aria-hidden="true"
          >
            <path
              d="M20.25 7.5l-.625 10.632a2.25 2.25 0 01-2.247 2.118H6.622a2.25 2.25 0 01-2.247-2.118L3.75 7.5m8.25 3v6.75m0 0l-3-3m3 3l3-3M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125z"
            />
          </svg>
        </button>
      {/if}
      {#if onUnarchive && message.archived_at && !message.trashed_at}
        <button
          aria-label="Unarchive message"
          class="cursor-pointer rounded p-1 text-amber-400 hover:bg-[var(--ui-bg-soft)] hover:text-amber-300 disabled:opacity-50"
          disabled={lifecycleBusy}
          onclick={() => onUnarchive(message.id)}
          type="button"
        >
          <svg
            class="h-3.5 w-3.5"
            fill="currentColor"
            viewBox="0 0 24 24"
            aria-hidden="true"
          >
            <path
              d="M20.25 7.5l-.625 10.632a2.25 2.25 0 01-2.247 2.118H6.622a2.25 2.25 0 01-2.247-2.118L3.75 7.5m8.25 3v6.75m0 0l-3-3m3 3l3-3M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125z"
            />
          </svg>
        </button>
      {/if}
      {#if onTrash && !message.trashed_at}
        <button
          aria-label="Move message to trash"
          class="cursor-pointer rounded p-1 text-[var(--ui-text-muted)] hover:bg-[var(--ui-bg-soft)] hover:text-red-400 disabled:opacity-50"
          disabled={lifecycleBusy}
          onclick={() => onTrash(message.id)}
          type="button"
        >
          <svg
            class="h-3.5 w-3.5"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="2"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0"
            />
          </svg>
        </button>
      {/if}
      {#if !message.archived_at && !message.trashed_at}
        <button
          class="cursor-pointer rounded px-2 py-0.5 text-[12px] text-[var(--ui-text-muted)] hover:bg-[var(--ui-bg-soft)] hover:text-[var(--ui-text)]"
          onclick={() => onReply(message.id)}
          type="button"
        >
          Reply
        </button>
      {/if}
    </div>
  </div>

  {#if message.displayRefs.length > 0}
    <div class="mt-2 flex flex-wrap gap-1.5 text-[12px]">
      {#each message.displayRefs as refValue}
        <RefLink {refValue} {threadId} />
      {/each}
    </div>
  {/if}

  {#if message.children.length > 0}
    <!-- -mx-4 cancels this article's horizontal padding so nested rows use the full card
      width; only the left border + pl indent the thread. Reply buttons stay on the
         same right edge as the root message. -->
    <div
      class="mt-3 -mx-4 space-y-2 border-l border-[var(--ui-border)] pl-2.5 sm:pl-3"
    >
      {#each message.children as child (child.id)}
        <Self
          message={child}
          {threadId}
          {actorName}
          {onReply}
          {onArchive}
          {onTrash}
          {onUnarchive}
          {lifecycleBusy}
          depth={depth + 1}
        />
      {/each}
    </div>
  {/if}
</article>
