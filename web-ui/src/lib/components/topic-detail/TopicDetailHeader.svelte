<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import ArchiveButton from "$lib/components/ArchiveButton.svelte";
  import Button from "$lib/components/Button.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import ResourceShareMenu from "$lib/components/ResourceShareMenu.svelte";
  import TopicTypeGlyph from "$lib/components/TopicTypeGlyph.svelte";
  import TrashButton from "$lib/components/TrashButton.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { topicDetailStore } from "$lib/topicDetailStore";
  import { formatCadenceLabel, getPriorityLabel } from "$lib/topicFilters";
  import { workspacePath } from "$lib/workspacePaths";

  function topicStatusBadgeClass(status) {
    // Tone scale follows status semantics rather than ad-hoc Tailwind palette
    // (per polish §N6) so themes can recolor via CSS tokens.
    if (status === "active") return "bg-ok-soft text-ok-text";
    if (status === "paused" || status === "blocked")
      return "bg-warn-soft text-warn-text";
    return "bg-[var(--line)] text-[var(--fg-muted)]";
  }

  let { threadId = "", detailAsTopic = true } = $props();

  let topic = $derived($topicDetailStore.topic);
  let staleness = $derived(topicDetailStore.getStaleness(topic));
  let organizationSlug = $derived($page.params.organization);
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
      await goto(workspacePath(organizationSlug, workspaceSlug, "/topics"));
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
  class="mb-3 flex items-center gap-1.5 text-meta text-[var(--fg-muted)]"
  aria-label="Breadcrumb"
>
  <a
    class="hover:text-[var(--fg)]"
    href={workspacePath(
      organizationSlug,
      workspaceSlug,
      detailAsTopic ? "/topics" : "/threads",
    )}>{detailAsTopic ? "Topics" : "Topic (thread view)"}</a
  >
  <span class="text-[var(--fg-subtle)]">/</span>
  <span class="truncate text-[var(--fg)]" aria-current="page"
    >{topic?.title || ""}</span
  >
</nav>

{#if topic?.trashed_at}
  <div
    class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-danger/30 bg-danger-soft px-3 py-2 text-meta text-danger-text"
  >
    <div class="min-w-0 flex-1">
      <div class="flex items-center gap-2 font-semibold">
        <span>⚠</span>
        <span>This topic is in trash</span>
      </div>
      {#if topic.trash_reason}
        <p class="mt-2">Reason: {topic.trash_reason}</p>
      {/if}
      <p class="mt-1 text-micro text-danger-text/80">
        Trashed {#if topic.trashed_by}by {actorName(topic.trashed_by)}{/if}
        {#if topic.trashed_at}
          at {formatTimestamp(topic.trashed_at)}
        {/if}
      </p>
    </div>
    {#if detailAsTopic}
      <Button
        variant="destructive"
        size="compact"
        disabled={lifecycleBusy}
        onclick={handleRestore}
      >
        {lifecycleBusy ? "…" : "Restore"}
      </Button>
    {:else}
      <p class="shrink-0 max-w-xs text-micro text-danger-text/80">
        Restore and lifecycle changes use the topic route; this thread view is
        read-only here.
      </p>
    {/if}
  </div>
{:else if topic?.archived_at}
  <div
    class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-warn/30 bg-warn-soft px-3 py-2 text-meta text-warn-text"
  >
    <p class="min-w-0 flex-1">
      This {detailAsTopic ? "topic" : "thread"} was archived on {formatTimestamp(
        topic.archived_at,
      ) || "—"}{#if topic.archived_by}
        by {actorName(topic.archived_by)}{/if}.
    </p>
    {#if detailAsTopic}
      <button
        class="shrink-0 cursor-pointer rounded-md border border-warn/40 bg-warn-soft px-2 py-1 text-micro font-medium text-warn-text hover:bg-warn/25 disabled:opacity-50"
        disabled={lifecycleBusy}
        onclick={handleUnarchive}
        type="button"
      >
        {lifecycleBusy ? "…" : "Unarchive"}
      </button>
    {:else}
      <p class="shrink-0 max-w-xs text-micro text-warn-text/80">
        Unarchive from the topic route; thread views here are read-only.
      </p>
    {/if}
  </div>
{/if}

{#if topic}
  <div class="flex flex-col gap-2">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="flex min-w-0 flex-1 items-start gap-2">
        <TopicTypeGlyph type={topic.type} class="mt-0.5" />
        <div class="min-w-0 flex-1">
          <div class="flex min-w-0 flex-wrap items-center gap-2">
            <h1
              class="min-w-0 flex-1 truncate text-title font-semibold text-[var(--fg)]"
            >
              {topic.title}
            </h1>
            <span
              class="shrink-0 rounded px-2 py-0.5 text-micro font-medium capitalize {topicStatusBadgeClass(
                topic.status,
              )}">{topic.status}</span
            >
          </div>
          {#if String(topic.current_summary ?? "").trim()}
            <p
              class="mt-1 line-clamp-2 text-meta text-[var(--fg-muted)]"
              title={String(topic.current_summary).trim()}
            >
              {String(topic.current_summary).trim()}
            </p>
          {/if}
        </div>
      </div>
      <div
        class="flex shrink-0 flex-wrap items-center justify-end gap-2 text-micro"
      >
        {#if topic?.id}
          <ResourceShareMenu resourceId={topic.id} rawRecord={topic} />
        {/if}
        {#if detailAsTopic && !topic.trashed_at && threadId}
          {#if !topic.archived_at}
            <ArchiveButton
              busy={lifecycleBusy}
              size="md"
              onarchive={() =>
                (confirmModal = { open: true, action: "archive" })}
            />
          {/if}
          <TrashButton
            busy={lifecycleBusy}
            size="md"
            ontrash={() => (confirmModal = { open: true, action: "trash" })}
          />
        {/if}
      </div>
    </div>
    <p
      class="flex flex-wrap items-center gap-x-2 gap-y-1 text-meta text-[var(--fg-muted)]"
    >
      <span class="whitespace-nowrap">{getPriorityLabel(topic.priority)}</span>
      <span class="text-[var(--fg-subtle)]" aria-hidden="true">·</span>
      <span class="whitespace-nowrap"
        >{formatCadenceLabel(topic.cadence, { includeExpression: false })}</span
      >
      <span class="text-[var(--fg-subtle)]" aria-hidden="true">·</span>
      <span class="whitespace-nowrap"
        >Updated {formatTimestamp(topic.updated_at) || "—"}</span
      >
      {#if topic.created_by}
        <span class="text-[var(--fg-subtle)]" aria-hidden="true">·</span>
        <span class="min-w-0 whitespace-nowrap"
          >by {actorName(topic.created_by)}</span
        >
      {/if}
      {#if staleness}
        <span class="text-[var(--fg-subtle)]" aria-hidden="true">·</span>
        <span
          class="whitespace-nowrap rounded px-1.5 py-0.5 text-micro font-medium {staleness.stale
            ? 'bg-rose-500/10 text-rose-400'
            : 'bg-ok-soft text-ok-text'}"
        >
          {staleness.label}
        </span>
      {/if}
    </p>
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
