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
  import {
    BOARD_HOME_EVENT_TYPES,
    DOCUMENT_HOME_EVENT_TYPES,
    TOPIC_HOME_EVENT_TYPES,
    filterEventsForHomeSection,
  } from "$lib/homeEventSections";
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

  /** Compact pill (matches unread / priority styling). */
  const inlineBadgeClass =
    "shrink-0 rounded bg-line px-1.5 py-0.5 text-micro font-medium text-fg-muted";

  function documentIdFromEvent(event) {
    const refs = Array.isArray(event?.refs) ? event.refs : [];
    for (const r of refs) {
      const s = String(r ?? "").trim();
      if (s.startsWith("document:")) return s.slice("document:".length);
    }
    const payload =
      event?.payload && typeof event.payload === "object" ? event.payload : {};
    return String(payload.document_id ?? payload.documentId ?? "").trim();
  }

  let topicFeedGroups = $derived.by(() => {
    const raw = groups.filter((g) => String(g?.group_type ?? "") === "topic");
    return raw
      .map((g) => {
        const displayEvents = filterEventsForHomeSection(
          g.events,
          TOPIC_HOME_EVENT_TYPES,
        );
        return { ...g, displayEvents };
      })
      .filter((g) => (g.displayEvents ?? []).length > 0);
  });

  let boardFeedGroups = $derived.by(() => {
    const raw = groups.filter((g) => String(g?.group_type ?? "") === "board");
    return raw
      .map((g) => {
        const displayEvents = filterEventsForHomeSection(
          g.events,
          BOARD_HOME_EVENT_TYPES,
        );
        return { ...g, displayEvents };
      })
      .filter((g) => (g.displayEvents ?? []).length > 0);
  });

  let docFeedSections = $derived.by(() => {
    const map = new Map();
    for (const group of groups) {
      const groupRef = String(group?.group_ref ?? "").trim();
      for (const event of group.events ?? []) {
        const t = String(event?.type ?? "").trim();
        if (!DOCUMENT_HOME_EVENT_TYPES.has(t)) continue;
        const docId = documentIdFromEvent(event);
        if (!docId) continue;
        let row = map.get(docId);
        if (!row) {
          row = {
            docId,
            events: [],
            title: "",
            sourceGroupRefs: new Set(),
          };
          map.set(docId, row);
        }
        row.events.push(event);
        if (groupRef) row.sourceGroupRefs.add(groupRef);
        const payload =
          event?.payload && typeof event.payload === "object"
            ? event.payload
            : {};
        const title = String(
          payload.title ?? payload.document_title ?? "",
        ).trim();
        if (title && !row.title) row.title = title;
      }
    }
    const list = [...map.values()].map((row) => {
      const sorted = [...row.events].sort((a, b) =>
        String(b.ts ?? "").localeCompare(String(a.ts ?? "")),
      );
      return {
        docId: row.docId,
        title: row.title || row.docId,
        events: sorted,
        newest_event: sorted[0] ?? null,
        sourceGroupRefs: [...row.sourceGroupRefs],
      };
    });
    list.sort((a, b) =>
      String(b.newest_event?.ts ?? "").localeCompare(
        String(a.newest_event?.ts ?? ""),
      ),
    );
    return list;
  });

  /** Topic/board rows plus any workspace group that only appears via document sections. */
  let markAllReadTargets = $derived.by(() => {
    const refSet = new Set();
    for (const g of topicFeedGroups) {
      refSet.add(String(g.group_ref ?? "").trim());
    }
    for (const g of boardFeedGroups) {
      refSet.add(String(g.group_ref ?? "").trim());
    }
    for (const s of docFeedSections) {
      for (const r of s.sourceGroupRefs ?? []) {
        if (String(r ?? "").trim()) refSet.add(String(r).trim());
      }
    }
    return [...refSet]
      .filter(Boolean)
      .map((ref) => groups.find((g) => String(g?.group_ref ?? "") === ref))
      .filter(Boolean);
  });

  function priorityLabel(group) {
    return String(group?.priority ?? "").trim() || "P2";
  }

  function groupLabel(group) {
    const name = String(group?.display_name ?? "").trim();
    if (!name) return group?.group_ref ?? "Untitled";
    return name;
  }

  function docSectionHref(section) {
    return workspaceHref(`/docs/${encodeURIComponent(section.docId)}`);
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
    const targets = markAllReadTargets;
    const group_refs = targets
      .map((group) => String(group?.group_ref ?? "").trim())
      .filter(Boolean);
    if (group_refs.length === 0) return;
    markingGroupRef = "*";
    error = "";
    try {
      await coreClient.markHomeRead({
        group_refs,
        group_cursors: Object.fromEntries(
          targets
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

  /** Document sections are views on topic/board groups; mark those groups read at once. */
  async function markDocSectionRead(section) {
    const refs = (section.sourceGroupRefs ?? [])
      .map((r) => String(r ?? "").trim())
      .filter(Boolean);
    if (refs.length === 0) return;
    const key = `doc:${section.docId}`;
    markingGroupRef = key;
    error = "";
    try {
      await coreClient.markHomeRead({
        group_refs: refs,
        group_cursors: Object.fromEntries(
          refs
            .map((ref) => {
              const g = groups.find(
                (entry) => String(entry?.group_ref ?? "") === ref,
              );
              return [
                ref,
                { ts: g?.newest_event?.ts, id: g?.newest_event?.id },
              ];
            })
            .filter(([ref, cursor]) => ref && cursor.ts && cursor.id),
        ),
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
          0} unread across {feed.group_count ?? 0} groups
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
        disabled={loading ||
          markAllReadTargets.length === 0 ||
          Boolean(markingGroupRef)}
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
      {#each topicFeedGroups as group (group.group_ref)}
        <section class="overflow-hidden rounded-md border border-line bg-panel">
          <div class="space-y-2 border-b border-line px-3 py-2.5 sm:px-4">
            <div class="min-w-0">
              <a
                class="block break-words font-semibold text-fg hover:underline"
                href={groupHref(group)}
              >
                {groupLabel(group)}
              </a>
            </div>
            <div class="flex flex-wrap items-center gap-y-2 gap-x-2">
              <div class="flex flex-wrap items-center gap-2">
                <span class={inlineBadgeClass}>Topic</span>
                <span
                  class={inlineBadgeClass}
                  title="Unread events tied to this topic (may include document activity shown in Doc sections below until marked read)."
                >
                  {priorityLabel(group)} · {group.unread_count} unread
                </span>
              </div>
              <button
                class="ml-auto shrink-0 rounded-md border border-line px-2 py-1 text-micro font-medium text-fg-muted transition-colors hover:bg-line-subtle disabled:opacity-60"
                onclick={() => markGroupRead(group.group_ref)}
                disabled={Boolean(markingGroupRef)}
                type="button"
              >
                Mark read
              </button>
            </div>
          </div>
          <div class="divide-y divide-line">
            {#each group.displayEvents.slice(0, 5) as event (event.id)}
              <EventRow row={rowFor(event)} />
            {/each}
            {#if group.displayEvents.length > 5}
              <details class="group">
                <summary
                  class="cursor-pointer px-3 py-2 text-meta font-medium text-fg-muted hover:bg-line-subtle sm:px-4"
                >
                  Show all {group.displayEvents.length}
                </summary>
                <div class="divide-y divide-line border-t border-line">
                  {#each group.displayEvents.slice(5) as event (event.id)}
                    <EventRow row={rowFor(event)} />
                  {/each}
                </div>
              </details>
            {/if}
          </div>
          {#if group.unread_count > group.displayEvents.length}
            <p
              class="border-t border-line bg-bg-soft px-3 py-2 text-micro text-fg-muted sm:px-4"
            >
              {group.unread_count - group.displayEvents.length} more unread for this
              topic appears in the Doc sections below.
            </p>
          {/if}
        </section>
      {/each}

      {#each boardFeedGroups as group (group.group_ref)}
        <section class="overflow-hidden rounded-md border border-line bg-panel">
          <div class="space-y-2 border-b border-line px-3 py-2.5 sm:px-4">
            <div class="min-w-0">
              <a
                class="block break-words font-semibold text-fg hover:underline"
                href={groupHref(group)}
              >
                {groupLabel(group)}
              </a>
            </div>
            <div class="flex flex-wrap items-center gap-y-2 gap-x-2">
              <div class="flex flex-wrap items-center gap-2">
                <span class={inlineBadgeClass}>Board</span>
                <span class={inlineBadgeClass}>{group.unread_count} unread</span
                >
              </div>
              <button
                class="ml-auto shrink-0 rounded-md border border-line px-2 py-1 text-micro font-medium text-fg-muted transition-colors hover:bg-line-subtle disabled:opacity-60"
                onclick={() => markGroupRead(group.group_ref)}
                disabled={Boolean(markingGroupRef)}
                type="button"
              >
                Mark read
              </button>
            </div>
          </div>
          <div class="divide-y divide-line">
            {#each group.displayEvents.slice(0, 5) as event (event.id)}
              <EventRow row={rowFor(event)} />
            {/each}
            {#if group.displayEvents.length > 5}
              <details class="group">
                <summary
                  class="cursor-pointer px-3 py-2 text-meta font-medium text-fg-muted hover:bg-line-subtle sm:px-4"
                >
                  Show all {group.displayEvents.length}
                </summary>
                <div class="divide-y divide-line border-t border-line">
                  {#each group.displayEvents.slice(5) as event (event.id)}
                    <EventRow row={rowFor(event)} />
                  {/each}
                </div>
              </details>
            {/if}
          </div>
        </section>
      {/each}

      {#each docFeedSections as section (section.docId)}
        <section class="overflow-hidden rounded-md border border-line bg-panel">
          <div class="space-y-2 border-b border-line px-3 py-2.5 sm:px-4">
            <div class="min-w-0">
              <a
                class="block break-words font-semibold text-fg hover:underline"
                href={docSectionHref(section)}
              >
                {section.title}
              </a>
            </div>
            <div class="flex flex-wrap items-center gap-y-2 gap-x-2">
              <div class="flex flex-wrap items-center gap-2">
                <span class={inlineBadgeClass}>Doc</span>
                <span class={inlineBadgeClass}
                  >{section.events.length} unread</span
                >
              </div>
              <button
                class="ml-auto shrink-0 rounded-md border border-line px-2 py-1 text-micro font-medium text-fg-muted transition-colors hover:bg-line-subtle disabled:opacity-60"
                onclick={() => markDocSectionRead(section)}
                disabled={Boolean(markingGroupRef) ||
                  (section.sourceGroupRefs?.length ?? 0) === 0}
                title="Marks the owning topic or board group read for Home (includes other unread in that group)."
                type="button"
              >
                Mark read
              </button>
            </div>
          </div>
          <div class="divide-y divide-line">
            {#each section.events.slice(0, 5) as event (event.id)}
              <EventRow row={rowFor(event)} />
            {/each}
            {#if section.events.length > 5}
              <details class="group">
                <summary
                  class="cursor-pointer px-3 py-2 text-meta font-medium text-fg-muted hover:bg-line-subtle sm:px-4"
                >
                  Show all {section.events.length}
                </summary>
                <div class="divide-y divide-line border-t border-line">
                  {#each section.events.slice(5) as event (event.id)}
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
