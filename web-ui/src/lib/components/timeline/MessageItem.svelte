<script>
  import ArchiveButton from "$lib/components/ArchiveButton.svelte";
  import Self from "$lib/components/timeline/MessageItem.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import TrashButton from "$lib/components/TrashButton.svelte";
  import { formatTimestamp } from "$lib/formatDate";

  const MAX_REPLY_DEPTH = 48;

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
  class={`rounded-md border border-[var(--line)] bg-[var(--panel)] px-4 py-3 ${depth > 0 ? "bg-[var(--bg-soft)]" : ""} ${message.archived_at ? "opacity-60" : ""}`}
  id={`message-${message.id}`}
>
  <div class="flex items-start justify-between gap-3">
    <div class="min-w-0 flex-1">
      <MarkdownRenderer
        source={message.messageText || message.summary || "Untitled message"}
        class="text-[13px] text-[var(--fg)]"
      />
      <p class="mt-1 text-[12px] text-[var(--fg-muted)]">
        {actorName(message.actor_id)} · {formatTimestamp(message.ts) || "—"}
      </p>
    </div>
    <div class="flex shrink-0 items-center gap-0.5">
      {#if !message.trashed_at && ((!message.archived_at && onArchive) || (message.archived_at && onUnarchive))}
        <ArchiveButton
          archived={Boolean(message.archived_at)}
          busy={lifecycleBusy}
          onarchive={() => onArchive?.(message.id)}
          onunarchive={() => onUnarchive?.(message.id)}
        />
      {/if}
      {#if onTrash && !message.trashed_at}
        <TrashButton busy={lifecycleBusy} ontrash={() => onTrash(message.id)} />
      {/if}
      {#if !message.archived_at && !message.trashed_at}
        <button
          class="cursor-pointer rounded px-2 py-0.5 text-[12px] text-[var(--fg-muted)] hover:bg-[var(--bg-soft)] hover:text-[var(--fg)]"
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
        <RefLink {refValue} {threadId} humanize showRaw />
      {/each}
    </div>
  {/if}

  {#if message.children.length > 0 && depth < MAX_REPLY_DEPTH}
    <!-- -mx-4 cancels this article's horizontal padding so nested rows use the full card
      width; only the left border + pl indent the thread. Reply buttons stay on the
         same right edge as the root message. -->
    <div
      class="mt-3 -mx-4 space-y-2 border-l border-[var(--line)] pl-2.5 sm:pl-3"
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
