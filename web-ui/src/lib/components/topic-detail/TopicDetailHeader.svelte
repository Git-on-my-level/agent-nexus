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
  import Time from "$lib/time/Time.svelte";
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
  <!--
    Stacked below `sm`. `flex-wrap` does not save a `flex-1 min-w-0` child from
    a `max-w-xs` sibling: with no min-content floor the first column shrinks to
    nothing instead of wrapping, so at 390px the whole "Trashed by … 3h ago"
    line had zero width and read as missing.
  -->
  <div
    class="mb-4 flex flex-col items-start justify-between gap-3 rounded-md border border-danger bg-danger-soft px-3 py-2 text-meta text-danger-text sm:flex-row sm:items-center"
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
          <!-- No "at": a recent instant reads "3 h ago", which already has its preposition. -->
          <Time value={topic.trashed_at} />
        {/if}
      </p>
    </div>
    <p class="max-w-xs shrink-0 text-micro text-danger-text">
      This diagnostic view is read-only. Restore with
      <code>anx topics restore</code>.
    </p>
  </div>
{:else if topic?.archived_at}
  <div
    class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-md border border-warn bg-warn-soft px-3 py-2 text-meta text-warn-text"
  >
    <p class="flex min-w-0 flex-1 flex-wrap items-center gap-x-1">
      <!-- No "on": a recent instant reads "3 h ago". The exact local time
           stays on the timestamp's own title. -->
      <span class="text-warn-text">
        This thread was archived <Time value={topic.archived_at} fallback="—" />
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
