<script>
  import Self from "$lib/components/thread-detail/ThreadMessageItem.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { formatTimestamp } from "$lib/formatDate";

  let { message, threadId, actorName, onReply, depth = 0 } = $props();
</script>

<article
  class={`rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-4 py-3 ${depth > 0 ? "bg-[var(--ui-panel-muted)]" : ""}`}
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
    <button
      class="shrink-0 cursor-pointer rounded px-2 py-0.5 text-[12px] text-[var(--ui-text-muted)] hover:bg-[var(--ui-bg-soft)] hover:text-[var(--ui-text)]"
      onclick={() => onReply(message.id)}
      type="button"
    >
      Reply
    </button>
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
      {#each message.children as child}
        <Self
          message={child}
          {threadId}
          {actorName}
          {onReply}
          depth={depth + 1}
        />
      {/each}
    </div>
  {/if}
</article>
