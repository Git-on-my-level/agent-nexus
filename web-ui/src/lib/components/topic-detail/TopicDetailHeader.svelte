<script>
  import { page } from "$app/stores";

  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import ActorLabel from "$lib/components/ActorLabel.svelte";
  import ResourceShareMenu from "$lib/components/ResourceShareMenu.svelte";
  import WorkspaceResourceTopRow from "$lib/components/WorkspaceResourceTopRow.svelte";
  import { formatAbsoluteDateTime, formatTimestamp } from "$lib/formatDate";
  import { topicDetailStore } from "$lib/topicDetailStore";
  import {
    resourceCopyValue,
    resourceDisplayLabel,
  } from "$lib/resourceIdentity.js";
  import { workspacePath } from "$lib/workspacePaths";

  // This surface is thread inspection only. Topic lifecycle (archive / trash /
  // restore) used to live here behind a `detailAsTopic` flag that no route
  // could ever set — there is no `/topics` route and nothing sets
  // `detailScope`, so every one of those handlers was unreachable. Lifecycle
  // now belongs to the CLI; see anx-ui-spec.md 2.2.
  let { threadId = "", dense = false } = $props();

  let topic = $derived($topicDetailStore.topic);
  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  function actorName(id) {
    return lookupActorDisplayName(id, $actorRegistry, $principalRegistry);
  }
</script>

<WorkspaceResourceTopRow
  breadcrumbAriaLabel="Breadcrumb and topic status"
  {dense}
>
  {#snippet breadcrumb()}
    <a
      class="shrink-0 transition-colors hover:text-fg"
      href={workspacePath(organizationSlug, workspaceSlug, "/threads")}
      >Threads</a
    >
    <span class="shrink-0 text-fg-subtle">/</span>
    <h1
      class="min-w-0 shrink truncate text-[length:inherit] font-[length:inherit] text-fg-muted"
      title={resourceDisplayLabel(topic, threadId)}
    >
      {resourceDisplayLabel(topic, threadId)}
    </h1>
  {/snippet}
  {#snippet actions()}
    {#if topic?.id}
      <ResourceShareMenu
        resourceId={resourceCopyValue("topic", topic)}
        resourceLabel="topic ref"
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
        <span>This thread is in trash</span>
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
          <!-- No "at": formatTimestamp is relative under 7 days ("3h ago"). -->
          <span title={formatAbsoluteDateTime(topic.trashed_at)}
            >{formatTimestamp(topic.trashed_at)}</span
          >
        {/if}
      </p>
    </div>
    <p class="shrink-0 max-w-xs text-micro text-danger-text">
      This diagnostic view is read-only. Restore with
      <code>anx topics restore</code>.
    </p>
  </div>
{:else if topic?.archived_at}
  <div
    class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-md border border-warn bg-warn-soft px-3 py-2 text-meta text-warn-text"
  >
    <p class="flex min-w-0 flex-1 flex-wrap items-center gap-x-1">
      <!-- No "on" before formatTimestamp: it returns a relative string ("3h
           ago") under 7 days and an absolute date beyond, so "archived on 3h
           ago" read wrong. Without "on" both forms read correctly, and the
           exact instant is available from the title. -->
      <span
        class="text-warn-text"
        title={formatAbsoluteDateTime(topic.archived_at)}
      >
        This thread was archived {formatTimestamp(topic.archived_at) || "—"}
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
    <p class="shrink-0 max-w-xs text-micro text-warn-text">
      This diagnostic view is read-only. Unarchive with
      <code>anx topics unarchive</code>.
    </p>
  </div>
{/if}
