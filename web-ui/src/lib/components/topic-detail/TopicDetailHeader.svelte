<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import ActorLabel from "$lib/components/ActorLabel.svelte";
  import { dismissOnEscape } from "$lib/actions/dismissOnEscape.js";
  import Button from "$lib/components/Button.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import Icon from "$lib/components/Icon.svelte";
  import ResourceShareMenu from "$lib/components/ResourceShareMenu.svelte";
  import WorkspaceResourceTopRow from "$lib/components/WorkspaceResourceTopRow.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { topicDetailStore } from "$lib/topicDetailStore";
  import {
    resourceCopyValue,
    resourceDisplayLabel,
  } from "$lib/resourceIdentity.js";
  import { workspacePath } from "$lib/workspacePaths";

  let {
    threadId = "",
    detailAsTopic = true,
    dense = false,
    showDesktop = true,
  } = $props();

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
  let moreActionsOpen = $state(false);
  let moreActionsRoot = $state(null);

  function toggleMoreActions() {
    moreActionsOpen = !moreActionsOpen;
  }
  function closeMoreActions() {
    moreActionsOpen = false;
  }

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

  $effect(() => {
    if (!moreActionsOpen) return;
    function onDocPointerDown(e) {
      if (
        moreActionsRoot &&
        e.target instanceof Node &&
        !moreActionsRoot.contains(e.target)
      ) {
        moreActionsOpen = false;
      }
    }
    window.document.addEventListener("pointerdown", onDocPointerDown, true);
    return () => {
      window.document.removeEventListener(
        "pointerdown",
        onDocPointerDown,
        true,
      );
    };
  });
</script>

{#snippet topicDesktop()}
  <h1
    class="min-w-0 text-subtitle font-semibold {topic?.title
      ? 'text-fg'
      : 'text-fg-subtle italic'}"
  >
    {topic?.title || "Untitled topic"}
  </h1>
  {#if topicSummary}
    <p class="line-clamp-2 text-meta text-fg-muted" title={topicSummary}>
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
        <ActorLabel
          label={actorName(topic.created_by)}
          seed={topic.created_by}
          size="xs"
          prefix="by"
          nameClass="text-micro text-fg-subtle"
        />
      {/if}
    </p>
  {/if}
{/snippet}

<WorkspaceResourceTopRow
  breadcrumbAriaLabel="Breadcrumb and topic status"
  desktopAriaLabel="Topic details"
  {dense}
  {showDesktop}
  desktop={topic ? topicDesktop : undefined}
>
  {#snippet breadcrumb()}
    <a
      class="shrink-0 transition-colors hover:text-fg"
      href={workspacePath(
        organizationSlug,
        workspaceSlug,
        detailAsTopic ? "/topics" : "/threads",
      )}>{detailAsTopic ? "Topics" : "Topic (thread view)"}</a
    >
    <span class="shrink-0 text-fg-subtle">/</span>
    <span
      class="min-w-0 shrink truncate text-fg-muted"
      aria-current="page"
      title={resourceDisplayLabel(topic, threadId)}
    >
      {resourceDisplayLabel(topic, threadId)}
    </span>
  {/snippet}
  {#snippet actions()}
    {#if topic?.id}
      <ResourceShareMenu
        resourceId={resourceCopyValue("topic", topic)}
        resourceLabel="topic ref"
      />
    {/if}
    {#if topic && detailAsTopic && !topic.trashed_at && threadId}
      <div
        bind:this={moreActionsRoot}
        class="relative"
        use:dismissOnEscape={{
          enabled: moreActionsOpen,
          onDismiss: closeMoreActions,
        }}
      >
        <button
          type="button"
          class="inline-flex h-7 w-7 cursor-pointer items-center justify-center rounded-md border border-line bg-transparent text-fg-muted transition-colors hover:bg-panel-hover hover:text-fg disabled:cursor-not-allowed disabled:opacity-50"
          aria-label="More actions"
          aria-haspopup="menu"
          aria-expanded={moreActionsOpen}
          disabled={lifecycleBusy}
          onclick={toggleMoreActions}
        >
          <Icon name="kebab" class="h-4 w-4" />
        </button>
        {#if moreActionsOpen}
          <div
            class="absolute right-0 z-50 mt-1 min-w-[10rem] rounded-md border border-line bg-panel py-1 shadow-lg"
            role="menu"
          >
            {#if !topic.archived_at}
              <button
                type="button"
                role="menuitem"
                class="block w-full px-3 py-2 text-left text-micro text-fg hover:bg-panel-hover disabled:cursor-not-allowed disabled:opacity-50"
                disabled={lifecycleBusy}
                onclick={() => {
                  closeMoreActions();
                  confirmModal = { open: true, action: "archive" };
                }}
              >
                Archive
              </button>
            {/if}
            <button
              type="button"
              role="menuitem"
              class="block w-full px-3 py-2 text-left text-micro text-danger-text hover:bg-panel-hover disabled:cursor-not-allowed disabled:opacity-50"
              disabled={lifecycleBusy}
              onclick={() => {
                closeMoreActions();
                confirmModal = { open: true, action: "trash" };
              }}
            >
              Move to trash
            </button>
          </div>
        {/if}
      </div>
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
      <p
        class="mt-1 flex flex-wrap items-center gap-x-1 text-micro text-danger-text"
      >
        <span>Trashed</span>
        {#if topic.trashed_by}
          <ActorLabel
            label={actorName(topic.trashed_by)}
            seed={topic.trashed_by}
            size="xs"
            prefix="by"
            nameClass="text-micro text-danger-text"
          />
        {/if}
        {#if topic.trashed_at}
          <span>at {formatTimestamp(topic.trashed_at)}</span>
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
    <p class="flex min-w-0 flex-1 flex-wrap items-center gap-x-1">
      <span class="text-warn-text">
        This {detailAsTopic ? "topic" : "thread"} was archived on {formatTimestamp(
          topic.archived_at,
        ) || "—"}
      </span>
      {#if topic.archived_by}
        <ActorLabel
          label={actorName(topic.archived_by)}
          seed={topic.archived_by}
          size="xs"
          prefix="by"
          nameClass="text-micro text-warn-text"
        />
      {/if}
      <span class="text-warn-text">.</span>
    </p>
    {#if detailAsTopic}
      <Button
        variant="secondary"
        size="compact"
        class="border-warn text-warn-text hover:bg-warn-soft"
        disabled={lifecycleBusy}
        onclick={handleUnarchive}
      >
        {lifecycleBusy ? "…" : "Unarchive"}
      </Button>
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
