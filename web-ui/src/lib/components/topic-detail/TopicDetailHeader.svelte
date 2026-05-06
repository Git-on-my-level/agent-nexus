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
  import TrashButton from "$lib/components/TrashButton.svelte";
  import WorkspaceResourceTopRow from "$lib/components/WorkspaceResourceTopRow.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { topicDetailStore } from "$lib/topicDetailStore";
  import { BOARD_LIFECYCLE_STATE_LABELS } from "$lib/boardUtils";
  import {
    resourceCopyValue,
    resourceDisplayLabel,
  } from "$lib/resourceIdentity.js";
  import { workspacePath } from "$lib/workspacePaths";

  function topicLifecycleBadgeClass(state) {
    if (state === "active") return "bg-ok-soft text-ok-text";
    if (state === "archived") return "bg-warn-soft text-warn-text";
    if (state === "trashed") return "bg-line text-fg-muted";
    return "bg-line text-fg-muted";
  }

  let { threadId = "", detailAsTopic = true } = $props();

  let topic = $derived($topicDetailStore.topic);
  let topicSummary = $derived(
    String(topic?.summary ?? topic?.current_summary ?? "").trim(),
  );
  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  function actorName(id) {
    return lookupActorDisplayName(id, $actorRegistry, $principalRegistry);
  }

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

{#snippet topicDesktop()}
  <h1
    class="min-w-0 text-title font-semibold {topic?.title
      ? 'text-fg'
      : 'text-fg-subtle italic'}"
  >
    {topic?.title || "Untitled topic"}
  </h1>
  {#if topicSummary}
    <p class="line-clamp-3 text-[13px] text-fg-muted" title={topicSummary}>
      {topicSummary}
    </p>
  {/if}
  {#if topic}
    <p
      class="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-micro text-fg-subtle"
    >
      <span class="whitespace-nowrap"
        >Updated {formatTimestamp(topic.updated_at) || "—"}</span
      >
      {#if topic.created_by}
        <span aria-hidden="true">·</span>
        <span class="min-w-0 whitespace-nowrap"
          >by {actorName(topic.created_by)}</span
        >
      {/if}
    </p>
  {/if}
{/snippet}

<WorkspaceResourceTopRow
  breadcrumbAriaLabel="Breadcrumb and topic status"
  desktopAriaLabel="Topic details"
  desktop={topic ? topicDesktop : undefined}
>
  {#snippet breadcrumb()}
    <a
      class="shrink-0 hover:text-fg"
      href={workspacePath(
        organizationSlug,
        workspaceSlug,
        detailAsTopic ? "/topics" : "/threads",
      )}>{detailAsTopic ? "Topics" : "Topic (thread view)"}</a
    >
    <span class="shrink-0 text-fg-subtle">/</span>
    <div class="flex min-h-0 min-w-0 flex-1 items-center gap-1.5">
      <span class="min-w-0 shrink truncate text-fg" aria-current="page">
        {resourceDisplayLabel(topic, threadId)}
      </span>
      >
      {#if topic}
        <span
          class="shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium capitalize leading-none sm:px-2 sm:py-0.5 sm:text-micro {topicLifecycleBadgeClass(
            topic.state,
          )}">{BOARD_LIFECYCLE_STATE_LABELS[topic.state] ?? topic.state}</span
        >
      {/if}
    </div>
  {/snippet}
  {#snippet actions()}
    {#if topic?.id}
      <ResourceShareMenu
        resourceId={resourceCopyValue("topic", topic)}
        resourceLabel="topic ref"
        rawRecord={topic}
      />
    {/if}
    {#if topic && detailAsTopic && !topic.trashed_at && threadId}
      {#if !topic.archived_at}
        <ArchiveButton
          busy={lifecycleBusy}
          size="md"
          onarchive={() => (confirmModal = { open: true, action: "archive" })}
        />
      {/if}
      <TrashButton
        busy={lifecycleBusy}
        size="md"
        ontrash={() => (confirmModal = { open: true, action: "trash" })}
      />
    {/if}
  {/snippet}
</WorkspaceResourceTopRow>

{#if topic?.trashed_at}
  <div
    class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-md border border-danger bg-danger-soft px-3 py-2 text-meta text-danger-text"
  >
    <div class="min-w-0 flex-1">
      <div class="flex items-center gap-2 font-semibold">
        <span>⚠</span>
        <span>This topic is in trash</span>
      </div>
      {#if topic.trash_reason}
        <p class="mt-2">Reason: {topic.trash_reason}</p>
      {/if}
      <p class="mt-1 text-micro text-danger-text">
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
      <p class="shrink-0 max-w-xs text-micro text-danger-text">
        Restore and lifecycle changes use the topic route; this thread view is
        read-only here.
      </p>
    {/if}
  </div>
{:else if topic?.archived_at}
  <div
    class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-md border border-warn bg-warn-soft px-3 py-2 text-meta text-warn-text"
  >
    <p class="min-w-0 flex-1">
      This {detailAsTopic ? "topic" : "thread"} was archived on {formatTimestamp(
        topic.archived_at,
      ) || "—"}{#if topic.archived_by}{" by "}{actorName(
          topic.archived_by,
        )}{/if}.
    </p>
    {#if detailAsTopic}
      <button
        class="shrink-0 cursor-pointer rounded-md border border-warn bg-warn-soft px-2 py-1 text-micro font-medium text-warn-text hover:bg-warn-soft disabled:opacity-50"
        disabled={lifecycleBusy}
        onclick={handleUnarchive}
        type="button"
      >
        {lifecycleBusy ? "…" : "Unarchive"}
      </button>
    {:else}
      <p class="shrink-0 max-w-xs text-micro text-warn-text">
        Unarchive from the topic route; thread views here are read-only.
      </p>
    {/if}
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
