<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";
  import { onDestroy, onMount } from "svelte";

  import EventRow from "$lib/components/EventRow.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { initializeAuthSession } from "$lib/authSession";
  import { normalizeEventRow } from "$lib/events/eventRows";
  import { replayWorkspaceTour } from "$lib/tourState";
  import { bindWorkspaceHref } from "$lib/workspacePaths";

  const POLL_INTERVAL_MS = 30_000;

  let loading = $state(true);
  let error = $state("");
  let feed = $state({
    groups: [],
    unread_count: 0,
    group_count: 0,
    generated_at: "",
  });
  let markingGroupRef = $state("");
  let pollTimer;

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let groups = $derived(feed.groups ?? []);

  let workspaceHref = $derived(
    bindWorkspaceHref(organizationSlug, workspaceSlug),
  );

  function priorityLabel(group) {
    return String(group?.priority ?? "").trim() || "P2";
  }

  function groupLabel(group) {
    const name = String(group?.display_name ?? "").trim();
    if (!name) return group?.group_ref ?? "Untitled";
    const type = group?.group_type ?? "";
    if (type === "topic") return name;
    if (type === "board") return name + " (board)";
    return name;
  }

  function groupHref(group) {
    const type = group?.group_type ?? "";
    const ref = group?.group_ref ?? "";
    if (type === "topic") {
      const id = ref.replace(/^topic:/, "");
      return workspaceHref(`/topics/${encodeURIComponent(id)}`);
    }
    if (type === "board") {
      const id = ref.replace(/^board:/, "");
      return workspaceHref(`/boards/${encodeURIComponent(id)}`);
    }
    return workspaceHref("/events");
  }

  function rowFor(event) {
    return normalizeEventRow(event, { workspaceHref });
  }

  async function loadHome() {
    const initial = !feed.generated_at;
    if (initial) loading = true;
    error = "";
    try {
      if (browser && workspaceSlug) {
        await initializeAuthSession({
          fetchFn: globalThis.fetch.bind(globalThis),
          workspaceSlug,
          authDriver: "home-unread",
        });
      }
      feed = await coreClient.getHomeUnread();
    } catch (err) {
      error =
        err instanceof Error
          ? err.message
          : String(err ?? "Failed to load Home.");
    } finally {
      loading = false;
    }
  }

  async function markGroupRead(groupRef) {
    const ref = String(groupRef ?? "").trim();
    if (!ref) return;
    const group = groups.find(
      (entry) => String(entry?.group_ref ?? "") === ref,
    );
    const newest = group?.newest_event;
    markingGroupRef = ref;
    error = "";
    try {
      await coreClient.markHomeRead({
        group_ref: ref,
        expected_newest_event_cursor: {
          ts: newest?.ts,
          id: newest?.id,
        },
      });
      await loadHome();
    } catch (err) {
      error =
        err instanceof Error
          ? err.message
          : String(err ?? "Failed to mark read.");
    } finally {
      markingGroupRef = "";
    }
  }

  async function markAllRead() {
    const group_refs = groups
      .map((group) => String(group?.group_ref ?? "").trim())
      .filter(Boolean);
    if (group_refs.length === 0) return;
    markingGroupRef = "*";
    error = "";
    try {
      await coreClient.markHomeRead({
        group_refs,
        group_cursors: Object.fromEntries(
          groups
            .map((group) => [
              String(group?.group_ref ?? "").trim(),
              {
                ts: group?.newest_event?.ts,
                id: group?.newest_event?.id,
              },
            ])
            .filter(([groupRef, cursor]) => groupRef && cursor.ts && cursor.id),
        ),
      });
      await loadHome();
    } catch (err) {
      error =
        err instanceof Error
          ? err.message
          : String(err ?? "Failed to mark all read.");
    } finally {
      markingGroupRef = "";
    }
  }

  onMount(async () => {
    await loadHome();
    pollTimer = setInterval(() => loadHome(), POLL_INTERVAL_MS);
  });

  onDestroy(() => {
    clearInterval(pollTimer);
  });
</script>

<WorkspacePageShell data-tour="home">
  <WorkspacePageHeader title="Home">
    {#snippet subtitle()}
      {#if loading && !feed.generated_at}
        Loading unread activity…
      {:else}
        Updated {formatTimestamp(feed.generated_at) || "just now"} · {feed.unread_count ??
          0}
        unread across {feed.group_count ?? 0} groups
      {/if}
    {/snippet}
    {#snippet actions()}
      {#if groups.length === 0}
        <button
          class="rounded-md border border-line px-2.5 py-1.5 text-meta font-medium text-fg-muted transition-colors hover:bg-bg-soft"
          onclick={() => replayWorkspaceTour()}
          type="button"
        >
          Take the tour
        </button>
      {/if}
      <button
        class="rounded-md border border-line px-2.5 py-1.5 text-meta font-medium text-fg-muted transition-colors hover:bg-bg-soft disabled:opacity-60"
        onclick={markAllRead}
        disabled={loading || groups.length === 0 || markingGroupRef === "*"}
        type="button"
      >
        Mark all read
      </button>
      <button
        class="rounded-md border border-line px-2.5 py-1.5 text-meta font-medium text-fg-muted transition-colors hover:bg-bg-soft disabled:opacity-60"
        onclick={loadHome}
        disabled={loading}
        type="button"
      >
        Refresh
      </button>
    {/snippet}
  </WorkspacePageHeader>

  {#if error}
    <StateError message={error} />
  {/if}

  {#if loading && !feed.generated_at}
    <section
      data-testid="home-unread-loading"
      class="rounded-md border border-line bg-panel px-4 py-5"
    >
      <p class="text-meta text-fg-muted">Loading unread activity…</p>
    </section>
  {:else if groups.length === 0}
    <div data-testid="home-unread-empty">
      <StateEmpty
        title="You're caught up."
        helper="Use the sidebar to open Events or Topics, or take the tour when your inbox is empty."
        actionLabel="Open Events"
        actionHref={workspaceHref("/events")}
      />
    </div>
  {:else}
    <div class="space-y-3" data-testid="home-unread-feed">
      {#each groups as group (group.group_ref)}
        <section class="overflow-hidden rounded-md border border-line bg-panel">
          <div
            class="flex flex-wrap items-center justify-between gap-3 border-b border-line px-3 py-2.5 sm:px-4"
          >
            <a
              class="min-w-0 flex-1 font-semibold text-fg hover:underline"
              href={groupHref(group)}
            >
              {groupLabel(group)}
            </a>
            <div class="flex shrink-0 items-center gap-2">
              <span
                class="rounded bg-line px-1.5 py-0.5 text-micro font-medium text-fg-muted"
              >
                {priorityLabel(group)} · {group.unread_count} unread
              </span>
              <button
                class="rounded-md border border-line px-2 py-1 text-micro font-medium text-fg-muted transition-colors hover:bg-line-subtle disabled:opacity-60"
                onclick={() => markGroupRead(group.group_ref)}
                disabled={markingGroupRef === group.group_ref ||
                  markingGroupRef === "*"}
                type="button"
              >
                Mark read
              </button>
            </div>
          </div>
          <div class="divide-y divide-line">
            {#each (group.events ?? []).slice(0, 5) as event (event.id)}
              <EventRow row={rowFor(event)} />
            {/each}
            {#if (group.events ?? []).length > 5}
              <details class="group">
                <summary
                  class="cursor-pointer px-3 py-2 text-meta font-medium text-fg-muted hover:bg-line-subtle sm:px-4"
                >
                  Show all {group.events.length}
                </summary>
                <div class="divide-y divide-line border-t border-line">
                  {#each group.events.slice(5) as event (event.id)}
                    <EventRow row={rowFor(event)} />
                  {/each}
                </div>
              </details>
            {/if}
          </div>
        </section>
      {/each}
    </div>
  {/if}

  <p class="text-meta text-fg-muted">
    <a
      class="font-medium text-fg hover:underline"
      href={workspaceHref("/events")}
    >
      View full Events history
    </a>
  </p>
</WorkspacePageShell>
