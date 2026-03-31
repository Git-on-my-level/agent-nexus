<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { threadDetailStore } from "$lib/threadDetailStore";
  import { getPriorityLabel } from "$lib/threadFilters";
  import { workspacePath } from "$lib/workspacePaths";

  let { threadId = "" } = $props();

  let snapshot = $derived($threadDetailStore.snapshot);
  let staleness = $derived(threadDetailStore.getStaleness(snapshot));
  let workspaceSlug = $derived($page.params.workspace);
  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );

  let trashConfirmOpen = $state(false);
  let lifecycleBusy = $state(false);

  async function refreshThread() {
    if (!threadId) return;
    await threadDetailStore.queueRefreshThreadDetail(threadId, {
      workspace: true,
      timeline: true,
      workOrders: true,
    });
  }

  async function handleArchive() {
    if (!threadId || lifecycleBusy || snapshot?.tombstoned_at) return;
    lifecycleBusy = true;
    try {
      await coreClient.archiveThread(threadId, {});
      await refreshThread();
    } finally {
      lifecycleBusy = false;
    }
  }

  async function handleUnarchive() {
    if (!threadId || lifecycleBusy || snapshot?.tombstoned_at) return;
    lifecycleBusy = true;
    try {
      await coreClient.unarchiveThread(threadId, {});
      await refreshThread();
    } finally {
      lifecycleBusy = false;
    }
  }

  async function handleTombstone() {
    if (!threadId || lifecycleBusy) return;
    lifecycleBusy = true;
    try {
      await coreClient.tombstoneThread(threadId, {});
      trashConfirmOpen = false;
      await goto(workspacePath(workspaceSlug, "/threads"));
    } finally {
      lifecycleBusy = false;
    }
  }

  async function handleRestore() {
    if (!threadId || lifecycleBusy) return;
    lifecycleBusy = true;
    try {
      await coreClient.restoreThread(threadId, {});
      await refreshThread();
    } finally {
      lifecycleBusy = false;
    }
  }

  $effect(() => {
    threadId;
    trashConfirmOpen = false;
  });
</script>

<nav
  class="mb-3 flex items-center gap-1.5 text-[13px] text-[var(--ui-text-muted)]"
  aria-label="Breadcrumb"
>
  <a
    class="hover:text-[var(--ui-text)]"
    href={workspacePath(workspaceSlug, "/threads")}>Threads</a
  >
  <span class="text-[var(--ui-text-subtle)]">/</span>
  <span class="truncate text-[var(--ui-text)]" aria-current="page"
    >{snapshot?.title || ""}</span
  >
</nav>

{#if snapshot?.tombstoned_at}
  <div
    class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-[13px] text-red-400"
  >
    <div class="min-w-0 flex-1">
      <div class="flex items-center gap-2 font-semibold">
        <span>⚠</span>
        <span>This thread has been tombstoned</span>
      </div>
      {#if snapshot.tombstone_reason}
        <p class="mt-2">Reason: {snapshot.tombstone_reason}</p>
      {/if}
      <p class="mt-1 text-[11px] text-red-400/80">
        Tombstoned {#if snapshot.tombstoned_by}by {actorName(
            snapshot.tombstoned_by,
          )}{/if}
        {#if snapshot.tombstoned_at}
          at {formatTimestamp(snapshot.tombstoned_at)}
        {/if}
      </p>
    </div>
    <button
      class="shrink-0 cursor-pointer rounded-md border border-red-500/40 bg-red-500/15 px-2 py-1 text-[12px] font-medium text-red-400 hover:bg-red-500/25 disabled:opacity-50"
      disabled={lifecycleBusy}
      onclick={handleRestore}
      type="button"
    >
      {lifecycleBusy ? "…" : "Restore"}
    </button>
  </div>
{:else if snapshot?.archived_at}
  <div
    class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-[13px] text-amber-400"
  >
    <p class="min-w-0 flex-1">
      This thread was archived on {formatTimestamp(snapshot.archived_at) ||
        "—"}{#if snapshot.archived_by}
        by {actorName(snapshot.archived_by)}{/if}.
    </p>
    <button
      class="shrink-0 cursor-pointer rounded-md border border-amber-500/40 bg-amber-500/15 px-2 py-1 text-[12px] font-medium text-amber-400 hover:bg-amber-500/25 disabled:opacity-50"
      disabled={lifecycleBusy}
      onclick={handleUnarchive}
      type="button"
    >
      {lifecycleBusy ? "…" : "Unarchive"}
    </button>
  </div>
{/if}

{#if snapshot}
  <div class="flex items-start justify-between gap-4">
    <h1 class="text-lg font-semibold text-[var(--ui-text)]">
      {snapshot.title}
    </h1>
    <div
      class="flex shrink-0 flex-wrap items-center justify-end gap-2 text-[12px]"
    >
      {#if staleness}
        <span
          class="rounded px-2 py-0.5 {staleness.stale
            ? 'bg-rose-500/10 text-rose-400'
            : 'bg-emerald-500/10 text-emerald-400'}"
        >
          {staleness.label}
        </span>
      {/if}
      <span
        class="rounded bg-[var(--ui-border)] px-2 py-0.5 capitalize text-[var(--ui-text-muted)]"
        >{snapshot.status}</span
      >
      <span
        class="rounded bg-[var(--ui-border)] px-2 py-0.5 text-[var(--ui-text-muted)]"
        >{getPriorityLabel(snapshot.priority)}</span
      >
      {#if !snapshot.tombstoned_at && threadId}
        {#if !snapshot.archived_at}
          <button
            class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--ui-text)] hover:bg-[var(--ui-border)] disabled:opacity-50"
            disabled={lifecycleBusy}
            onclick={handleArchive}
            type="button"
          >
            Archive
          </button>
        {/if}
        {#if trashConfirmOpen}
          <button
            class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-2 py-1 text-[12px] font-medium text-[var(--ui-text-muted)] hover:bg-[var(--ui-border)]"
            onclick={() => (trashConfirmOpen = false)}
            type="button"
          >
            Cancel
          </button>
          <button
            class="cursor-pointer rounded-md border border-red-500/40 bg-red-500/15 px-2 py-1 text-[12px] font-medium text-red-400 hover:bg-red-500/25 disabled:opacity-50"
            disabled={lifecycleBusy}
            onclick={handleTombstone}
            type="button"
          >
            Confirm
          </button>
        {:else}
          <button
            aria-label="Move thread to trash"
            class="cursor-pointer rounded-md p-1.5 text-[var(--ui-text-muted)] hover:bg-[var(--ui-border)] hover:text-red-400 disabled:opacity-50"
            disabled={lifecycleBusy}
            onclick={() => (trashConfirmOpen = true)}
            type="button"
          >
            <svg
              class="h-4 w-4"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
              />
            </svg>
          </button>
        {/if}
      {/if}
    </div>
  </div>
{/if}
