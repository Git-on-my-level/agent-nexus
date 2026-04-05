<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { topicDetailStore } from "$lib/topicDetailStore";
  import { getPriorityLabel } from "$lib/topicFilters";
  import { workspacePath } from "$lib/workspacePaths";

  let { threadId = "", detailAsTopic = true } = $props();

  let topic = $derived($topicDetailStore.topic);
  let staleness = $derived(topicDetailStore.getStaleness(topic));
  let workspaceSlug = $derived($page.params.workspace);
  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );

  let confirmModal = $state({ open: false, action: "" });
  let lifecycleBusy = $state(false);

  async function refreshThread() {
    if (!threadId) return;
    await topicDetailStore.queueRefreshTopicDetail(threadId, {
      workspace: true,
      timeline: true,
    });
  }

  async function handleArchive() {
    if (!threadId || lifecycleBusy || topic?.trashed_at || !detailAsTopic)
      return;
    lifecycleBusy = true;
    try {
      await coreClient.archiveTopic(threadId, {});
      await refreshThread();
    } finally {
      lifecycleBusy = false;
    }
  }

  async function handleUnarchive() {
    confirmModal = { open: false, action: "" };
    if (!threadId || lifecycleBusy || topic?.trashed_at || !detailAsTopic)
      return;
    lifecycleBusy = true;
    try {
      await coreClient.unarchiveTopic(threadId, {});
      await refreshThread();
    } finally {
      lifecycleBusy = false;
    }
  }

  function handleConfirm() {
    const action = confirmModal.action;
    confirmModal = { open: false, action: "" };
    if (action === "archive") handleArchive();
    else if (action === "trash") handleTrash();
  }

  async function handleTrash() {
    if (!threadId || lifecycleBusy || !detailAsTopic) return;
    lifecycleBusy = true;
    try {
      await coreClient.trashTopic(threadId, {});
      await goto(workspacePath(workspaceSlug, "/topics"));
    } finally {
      lifecycleBusy = false;
    }
  }

  async function handleRestore() {
    confirmModal = { open: false, action: "" };
    if (!threadId || lifecycleBusy || !detailAsTopic) return;
    lifecycleBusy = true;
    try {
      await coreClient.restoreTopic(threadId, {});
      await refreshThread();
    } finally {
      lifecycleBusy = false;
    }
  }

  $effect(() => {
    threadId;
    confirmModal = { open: false, action: "" };
  });
</script>

<nav
  class="mb-3 flex items-center gap-1.5 text-[13px] text-[var(--ui-text-muted)]"
  aria-label="Breadcrumb"
>
  <a
    class="hover:text-[var(--ui-text)]"
    href={workspacePath(workspaceSlug, detailAsTopic ? "/topics" : "/threads")}
    >{detailAsTopic ? "Topics" : "Topic (thread view)"}</a
  >
  <span class="text-[var(--ui-text-subtle)]">/</span>
  <span class="truncate text-[var(--ui-text)]" aria-current="page"
    >{topic?.title || ""}</span
  >
</nav>

{#if topic?.trashed_at}
  <div
    class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-[13px] text-red-400"
  >
    <div class="min-w-0 flex-1">
      <div class="flex items-center gap-2 font-semibold">
        <span>⚠</span>
        <span>This topic is in trash</span>
      </div>
      {#if topic.trash_reason}
        <p class="mt-2">Reason: {topic.trash_reason}</p>
      {/if}
      <p class="mt-1 text-[11px] text-red-400/80">
        Trashed {#if topic.trashed_by}by {actorName(topic.trashed_by)}{/if}
        {#if topic.trashed_at}
          at {formatTimestamp(topic.trashed_at)}
        {/if}
      </p>
    </div>
    {#if detailAsTopic}
      <button
        class="shrink-0 cursor-pointer rounded-md border border-red-500/40 bg-red-500/15 px-2 py-1 text-[12px] font-medium text-red-400 hover:bg-red-500/25 disabled:opacity-50"
        disabled={lifecycleBusy}
        onclick={handleRestore}
        type="button"
      >
        {lifecycleBusy ? "…" : "Restore"}
      </button>
    {:else}
      <p class="shrink-0 max-w-xs text-[11px] text-red-400/80">
        Restore and lifecycle changes use the topic route; this thread view is
        read-only here.
      </p>
    {/if}
  </div>
{:else if topic?.archived_at}
  <div
    class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-[13px] text-amber-400"
  >
    <p class="min-w-0 flex-1">
      This {detailAsTopic ? "topic" : "thread"} was archived on {formatTimestamp(
        topic.archived_at,
      ) || "—"}{#if topic.archived_by}
        by {actorName(topic.archived_by)}{/if}.
    </p>
    {#if detailAsTopic}
      <button
        class="shrink-0 cursor-pointer rounded-md border border-amber-500/40 bg-amber-500/15 px-2 py-1 text-[12px] font-medium text-amber-400 hover:bg-amber-500/25 disabled:opacity-50"
        disabled={lifecycleBusy}
        onclick={handleUnarchive}
        type="button"
      >
        {lifecycleBusy ? "…" : "Unarchive"}
      </button>
    {:else}
      <p class="shrink-0 max-w-xs text-[11px] text-amber-400/80">
        Unarchive from the topic route; thread views here are read-only.
      </p>
    {/if}
  </div>
{/if}

{#if topic}
  <div class="flex items-start justify-between gap-4">
    <h1 class="text-lg font-semibold text-[var(--ui-text)]">
      {topic.title}
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
        >{topic.status}</span
      >
      <span
        class="rounded bg-[var(--ui-border)] px-2 py-0.5 text-[var(--ui-text-muted)]"
        >{getPriorityLabel(topic.priority)}</span
      >
      {#if detailAsTopic && !topic.trashed_at && threadId}
        {#if !topic.archived_at}
          <button
            aria-label="Archive"
            class="cursor-pointer rounded-md p-1.5 text-[var(--ui-text-muted)] hover:bg-[var(--ui-border)] hover:text-[var(--ui-accent)] disabled:opacity-50"
            disabled={lifecycleBusy}
            onclick={() => (confirmModal = { open: true, action: "archive" })}
            type="button"
          >
            <svg
              class="h-4 w-4"
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
        <button
          aria-label="Move topic to trash"
          class="cursor-pointer rounded-md p-1.5 text-[var(--ui-text-muted)] hover:bg-[var(--ui-border)] hover:text-red-400 disabled:opacity-50"
          disabled={lifecycleBusy}
          onclick={() => (confirmModal = { open: true, action: "trash" })}
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
              d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0"
            />
          </svg>
        </button>
      {/if}
    </div>
  </div>
{/if}

<ConfirmModal
  open={confirmModal.open}
  title={confirmModal.action === "trash"
    ? "Move to trash"
    : detailAsTopic
      ? "Archive topic"
      : "Archive thread"}
  message={confirmModal.action === "trash"
    ? "This topic will be moved to trash. You can restore it later."
    : `This ${detailAsTopic ? "topic" : "thread"} will be hidden from default views. You can unarchive it later.`}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={lifecycleBusy}
  onconfirm={handleConfirm}
  oncancel={() => (confirmModal = { open: false, action: "" })}
/>
