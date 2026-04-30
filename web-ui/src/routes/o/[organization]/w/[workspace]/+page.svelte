<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";
  import { onDestroy, onMount } from "svelte";

  import EventRow from "$lib/components/EventRow.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { initializeAuthSession } from "$lib/authSession";
  import { normalizeEventRow } from "$lib/events/eventRows";
  import { replayWorkspaceTour } from "$lib/tourState";
  import { workspacePath } from "$lib/workspacePaths";

  const POLL_INTERVAL_MS = 30_000;

  let loading = $state(true);
  let error = $state("");
  let feed = $state({
    groups: [],
    unread_count: 0,
    topic_count: 0,
    generated_at: "",
  });
  let markingTopicId = $state("");
  let pollTimer;

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let groups = $derived(feed.groups ?? []);

  function workspaceHref(pathname = "/") {
    return workspacePath(organizationSlug, workspaceSlug, pathname);
  }

  function priorityLabel(topic) {
    return String(topic?.priority ?? "").trim() || "P2";
  }

  function topicTitle(topic) {
    return String(topic?.title ?? topic?.id ?? "Untitled topic").trim();
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

  async function markTopicRead(topicId) {
    const id = String(topicId ?? "").trim();
    if (!id) return;
    const group = groups.find((entry) => String(entry?.topic?.id ?? "") === id);
    const newest = group?.newest_event;
    markingTopicId = id;
    error = "";
    try {
      await coreClient.markHomeRead({
        topic_id: id,
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
          : String(err ?? "Failed to mark topic read.");
    } finally {
      markingTopicId = "";
    }
  }

  async function markAllRead() {
    const topic_ids = groups
      .map((group) => String(group?.topic?.id ?? "").trim())
      .filter(Boolean);
    if (topic_ids.length === 0) return;
    markingTopicId = "*";
    error = "";
    try {
      await coreClient.markHomeRead({
        topic_ids,
        topic_cursors: Object.fromEntries(
          groups
            .map((group) => [
              String(group?.topic?.id ?? "").trim(),
              {
                ts: group?.newest_event?.ts,
                id: group?.newest_event?.id,
              },
            ])
            .filter(([topicId, cursor]) => topicId && cursor.ts && cursor.id),
        ),
      });
      await loadHome();
    } catch (err) {
      error =
        err instanceof Error
          ? err.message
          : String(err ?? "Failed to mark all read.");
    } finally {
      markingTopicId = "";
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

<div class="min-w-0 max-w-full space-y-5" data-tour="home">
  <div class="flex flex-wrap items-start justify-between gap-3">
    <div class="min-w-0">
      <h1 class="text-subtitle font-semibold text-[var(--fg)]">Home</h1>
      <p class="mt-0.5 text-meta text-[var(--fg-muted)]">
        {#if loading && !feed.generated_at}
          Loading unread activity…
        {:else}
          Updated {formatTimestamp(feed.generated_at) || "just now"} · {feed.unread_count ??
            0}
          unread across {feed.topic_count ?? 0} topics
        {/if}
      </p>
    </div>
    <div class="flex shrink-0 flex-wrap items-center gap-2">
      {#if groups.length === 0}
        <button
          class="rounded-md border border-[var(--line)] px-2.5 py-1.5 text-meta font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)]"
          onclick={() => replayWorkspaceTour()}
          type="button"
        >
          Take the tour
        </button>
      {/if}
      <button
        class="rounded-md border border-[var(--line)] px-2.5 py-1.5 text-meta font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] disabled:opacity-60"
        onclick={markAllRead}
        disabled={loading || groups.length === 0 || markingTopicId === "*"}
        type="button"
      >
        Mark all read
      </button>
      <button
        class="rounded-md border border-[var(--line)] px-2.5 py-1.5 text-meta font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] disabled:opacity-60"
        onclick={loadHome}
        disabled={loading}
        type="button"
      >
        Refresh
      </button>
    </div>
  </div>

  {#if error}
    <p class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text">
      {error}
    </p>
  {/if}

  {#if loading && !feed.generated_at}
    <section
      class="rounded-md border border-[var(--line)] bg-[var(--panel)] px-4 py-5"
    >
      <p class="text-meta text-[var(--fg-muted)]">Loading unread activity…</p>
    </section>
  {:else if groups.length === 0}
    <section
      class="rounded-md border border-[var(--line)] bg-[var(--panel)] px-4 py-5"
    >
      <p class="text-meta font-medium text-[var(--fg)]">You're caught up.</p>
      <p class="mt-1 text-meta text-[var(--fg-muted)]">
        Browse <a
          class="font-medium text-[var(--fg)] hover:underline"
          href={workspaceHref("/events")}>Events</a
        >
        or
        <a
          class="font-medium text-[var(--fg)] hover:underline"
          href={workspaceHref("/topics")}>Topics</a
        >.
      </p>
    </section>
  {:else}
    <div class="space-y-3">
      {#each groups as group (group.topic.id)}
        <section
          class="overflow-hidden rounded-md border border-[var(--line)] bg-[var(--panel)]"
        >
          <div
            class="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--line)] px-3 py-2.5 sm:px-4"
          >
            <a
              class="min-w-0 flex-1 font-semibold text-[var(--fg)] hover:underline"
              href={workspaceHref(
                `/topics/${encodeURIComponent(group.topic.id)}`,
              )}
            >
              {topicTitle(group.topic)}
            </a>
            <div class="flex shrink-0 items-center gap-2">
              <span
                class="rounded bg-[var(--line)] px-1.5 py-0.5 text-micro font-medium text-[var(--fg-muted)]"
              >
                {priorityLabel(group.topic)} · {group.unread_count} unread
              </span>
              <button
                class="rounded-md border border-[var(--line)] px-2 py-1 text-micro font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] disabled:opacity-60"
                onclick={() => markTopicRead(group.topic.id)}
                disabled={markingTopicId === group.topic.id ||
                  markingTopicId === "*"}
                type="button"
              >
                Mark read
              </button>
            </div>
          </div>
          <div class="divide-y divide-[var(--line)]">
            {#each (group.events ?? []).slice(0, 5) as event (event.id)}
              <EventRow row={rowFor(event)} />
            {/each}
            {#if (group.events ?? []).length > 5}
              <details class="group">
                <summary
                  class="cursor-pointer px-3 py-2 text-meta font-medium text-[var(--fg-muted)] hover:bg-[var(--line-subtle)] sm:px-4"
                >
                  Show all {group.events.length}
                </summary>
                <div
                  class="divide-y divide-[var(--line)] border-t border-[var(--line)]"
                >
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

  <p class="text-meta text-[var(--fg-muted)]">
    <a
      class="font-medium text-[var(--fg)] hover:underline"
      href={workspaceHref("/events")}
    >
      View full Events history
    </a>
  </p>
</div>
