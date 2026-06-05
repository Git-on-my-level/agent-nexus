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
          d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0"
        />
      </svg>
    </button>
  {/if}
{/snippet}

{@render children?.(contextMenuItems, visible)}
