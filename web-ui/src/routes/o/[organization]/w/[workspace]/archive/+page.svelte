<script>
  import { onMount } from "svelte";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { resourceRouteSegment } from "$lib/resourceIdentity.js";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import { listAllPages } from "$lib/inboxSources.js";

  let groups = $state(null);
  let error = $state("");
  let href = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  async function load() {
    error = "";
    try {
      const [boards, cards, topics, docs] = await Promise.all([
        coreClient.listBoards({ state: ["archived"] }),
        coreClient.listCards({ state: ["archived"] }),
        listAllPages(
          (cursor) =>
            coreClient.listTopics({ state: ["archived"], limit: 200, cursor }),
          "topics",
        ),
        listAllPages(
          (cursor) =>
            coreClient.listDocuments({
              state: ["archived"],
              limit: 200,
              cursor,
            }),
          "documents",
        ),
      ]);
      groups = [
        {
          title: "Boards",
          items: (boards.boards || []).map((item) => item.board || item),
          kind: "board",
          route: "/threads/",
        },
        {
          title: "Cards",
          items: cards.cards || [],
          kind: "card",
          route: "/tasks/",
        },
        {
          title: "Topics",
          items: topics.topics || [],
          kind: "topic",
          route: "/threads/",
          partial: topics.has_more,
        },
        {
          title: "Documents",
          items: docs.documents || [],
          kind: "document",
          route: "/docs/",
          partial: docs.has_more,
        },
      ];
    } catch (e) {
      error = e instanceof Error ? e.message : "Archive could not be loaded.";
    }
  }
  onMount(() => {
    void load();
  });
</script>

<svelte:head><title>Archive · Agent Nexus</title></svelte:head>
<WorkspacePageShell>
  <!-- No subtitle: "Archived work and context remain available here" under a
       heading reading "Archive" is the heading again. -->
  <WorkspacePageHeader title="Archive" />
  {#if error}<StateError
      title="Archive is unavailable"
      message={error}
      onretry={load}
    />
  {:else if !groups}<Skeleton rows={6} />
  {:else}
    {#each groups as group (group.kind)}
      <section class="rounded-md border border-line bg-panel">
        <h2 class="border-b border-line px-3 py-2 text-subtitle">
          {group.title}
        </h2>
        {#if group.partial}<p class="px-3 py-2 text-micro text-warn-text">
            Some archived items are not shown.
          </p>{/if}
        {#if !group.items.length}<p class="px-3 py-3 text-meta text-fg-muted">
            Nothing archived.
          </p>{/if}
        <ul class="divide-y divide-line-subtle">
          {#each group.items as item (item.ref || item.id)}
            <li>
              <a
                class="block px-3 py-3 text-meta hover:bg-panel-hover"
                href={href(
                  group.route +
                    encodeURIComponent(
                      group.kind === "board" || group.kind === "topic"
                        ? item.thread_id ||
                            String(item.thread_ref || "").replace(
                              /^thread:/,
                              "",
                            )
                        : resourceRouteSegment(item, group.kind),
                    ),
                )}>{item.title || item.ref}</a
              >
            </li>
          {/each}
        </ul>
      </section>
    {/each}
  {/if}
</WorkspacePageShell>
