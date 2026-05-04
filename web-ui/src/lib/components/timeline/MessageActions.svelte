<script>
  import ArchiveButton from "$lib/components/ArchiveButton.svelte";

  let {
    message,
    onReply,
    onArchive = null,
    onTrash = null,
    onUnarchive = null,
    lifecycleBusy = false,
    archiveLabelKind = "archive",
    children,
  } = $props();

  let canReply = $derived(
    Boolean(onReply) && !message?.archived_at && !message?.trashed_at,
  );
  let canArchive = $derived(
    Boolean(onArchive) && !message?.archived_at && !message?.trashed_at,
  );
  let canUnarchive = $derived(
    Boolean(onUnarchive) &&
      Boolean(message?.archived_at) &&
      !message?.trashed_at,
  );
  let canTrash = $derived(Boolean(onTrash) && !message?.trashed_at);

  // Lifecycle actions stay visible so message rows remain discoverable and
  // keyboard-accessible. Keep the empty context list until there is a separate
  // visible overflow trigger for secondary actions.
  let contextMenuItems = $derived([]);
</script>

{#snippet visible()}
  {#if canArchive || canUnarchive}
    <ArchiveButton
      archived={Boolean(message?.archived_at)}
      busy={lifecycleBusy}
      kind={archiveLabelKind}
      onarchive={() => onArchive?.(message.id)}
      onunarchive={() => onUnarchive?.(message.id)}
    />
  {/if}
  {#if canReply}
    <button
      class="inline-flex h-6 w-6 shrink-0 cursor-pointer items-center justify-center rounded text-fg-muted transition-colors hover:bg-bg-soft hover:text-fg"
      onclick={() => onReply(message.id)}
      type="button"
      title="Reply"
      aria-label="Reply"
    >
      <svg
        class="h-3.5 w-3.5"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
        aria-hidden="true"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M3 10h10a5 5 0 0 1 0 10M3 10l4-4M3 10l4 4"
        />
      </svg>
    </button>
  {/if}
  {#if canTrash}
    <button
      class="inline-flex h-6 w-6 shrink-0 cursor-pointer items-center justify-center rounded text-fg-muted transition-colors hover:bg-danger-soft hover:text-danger-text disabled:cursor-not-allowed disabled:opacity-50"
      onclick={() => onTrash?.(message.id)}
      type="button"
      title="Move to trash"
      aria-label="Move to trash"
      disabled={Boolean(lifecycleBusy)}
    >
      <svg
        class="h-3.5 w-3.5"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
        aria-hidden="true"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M6 7h12m-9 0V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2m-7 3v8m4-8v8m4-11-.8 12.1A2 2 0 0 1 13.2 21H10.8a2 2 0 0 1-2-1.9L8 7"
        />
      </svg>
    </button>
  {/if}
{/snippet}

{@render children?.(contextMenuItems, visible)}
