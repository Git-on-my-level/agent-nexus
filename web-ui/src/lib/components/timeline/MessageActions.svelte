<script>
  import ArchiveButton from "$lib/components/ArchiveButton.svelte";
  import Icon from "$lib/components/Icon.svelte";

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
      <Icon name="reply" class="h-3.5 w-3.5" />
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
      <Icon name="trash" class="h-3.5 w-3.5" />
    </button>
  {/if}
{/snippet}

{@render children?.(contextMenuItems, visible)}
