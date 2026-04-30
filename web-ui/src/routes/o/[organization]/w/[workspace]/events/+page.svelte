<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { onMount } from "svelte";

  import EventRow from "$lib/components/EventRow.svelte";
  import { coreClient } from "$lib/coreClient";
  import { initializeAuthSession } from "$lib/authSession";
  import { HOME_FEED_PRESET, normalizeEventRow } from "$lib/events/eventRows";
  import { workspacePath } from "$lib/workspacePaths";

  let events = $state([]);
  let pageInfo = $state({ has_more: false, next_cursor: "" });
  let loading = $state(true);
  let loadingMore = $state(false);
  let error = $state("");
  let filters = $state({
    preset: "",
    type: "",
    topic_id: "",
    actor_id: "",
    q: "",
    since: "",
    until: "",
  });
  let urlCursor = $state("");

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);

  function workspaceHref(pathname = "/") {
    return workspacePath(organizationSlug, workspaceSlug, pathname);
  }

  function rowFor(event) {
    return normalizeEventRow(event, { workspaceHref });
  }

  function queryFromFilters(cursor = "") {
    const query = {};
    for (const [key, value] of Object.entries(filters)) {
      const text = String(value ?? "").trim();
      if (text) query[key] = text;
    }
    if (cursor) query.cursor = cursor;
    query.limit = 50;
    return query;
  }

  function filtersFromUrl() {
    const params = $page.url.searchParams;
    urlCursor = params.get("cursor") ?? "";
    filters = {
      preset: params.get("preset") ?? "",
      type: params.get("type") ?? "",
      topic_id: params.get("topic_id") ?? "",
      actor_id: params.get("actor_id") ?? "",
      q: params.get("q") ?? "",
      since: params.get("since") ?? "",
      until: params.get("until") ?? "",
    };
  }

  async function replaceUrlFilters(nextCursor = "") {
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(filters)) {
      const text = String(value ?? "").trim();
      if (text) params.set(key, text);
    }
    if (nextCursor) params.set("cursor", nextCursor);
    const query = params.toString();
    await goto(`${$page.url.pathname}${query ? `?${query}` : ""}`, {
      replaceState: false,
      noScroll: true,
      keepFocus: true,
    });
  }

  async function loadEvents({ append = false } = {}) {
    if (append) {
      loadingMore = true;
    } else {
      loading = true;
      events = [];
    }
    error = "";
    try {
      if (browser && workspaceSlug) {
        await initializeAuthSession({
          fetchFn: globalThis.fetch.bind(globalThis),
          workspaceSlug,
          authDriver: "events-page",
        });
      }
      const result = await coreClient.listEvents(
        queryFromFilters(append ? pageInfo.next_cursor : urlCursor),
      );
      events = append
        ? [...events, ...(result.events ?? [])]
        : (result.events ?? []);
      pageInfo = result.page_info ?? { has_more: false, next_cursor: "" };
    } catch (err) {
      error =
        err instanceof Error
          ? err.message
          : String(err ?? "Failed to load Events.");
    } finally {
      loading = false;
      loadingMore = false;
    }
  }

  async function applyFilters() {
    urlCursor = "";
    await replaceUrlFilters();
    await loadEvents();
  }

  async function showOlder() {
    const nextCursor = pageInfo?.next_cursor ?? "";
    if (!nextCursor) return;
    urlCursor = nextCursor;
    await replaceUrlFilters(nextCursor);
    await loadEvents({ append: true });
  }

  async function useHomePreset() {
    filters.preset =
      filters.preset === HOME_FEED_PRESET ? "" : HOME_FEED_PRESET;
    await applyFilters();
  }

  onMount(() => {
    filtersFromUrl();
    void loadEvents();
  });
</script>

<div class="min-w-0 max-w-full space-y-5">
  <div class="flex flex-wrap items-start justify-between gap-3">
    <div class="min-w-0">
      <h1 class="text-subtitle font-semibold text-[var(--fg)]">Events</h1>
      <p class="mt-0.5 text-meta text-[var(--fg-muted)]">
        Full workspace event history.
      </p>
    </div>
    <button
      class="rounded-md border border-[var(--line)] px-2.5 py-1.5 text-meta font-medium transition-colors hover:bg-[var(--line-subtle)] {filters.preset ===
      HOME_FEED_PRESET
        ? 'text-[var(--fg)]'
        : 'text-[var(--fg-muted)]'}"
      onclick={useHomePreset}
      type="button"
    >
      Home feed
    </button>
  </div>

  <section class="rounded-md border border-[var(--line)] bg-[var(--panel)] p-3">
    <div class="grid gap-2 sm:grid-cols-2 lg:grid-cols-6">
      <input
        class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta text-[var(--fg)]"
        bind:value={filters.q}
        placeholder="Search"
      />
      <input
        class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta text-[var(--fg)]"
        bind:value={filters.type}
        placeholder="Type"
      />
      <input
        class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta text-[var(--fg)]"
        bind:value={filters.topic_id}
        placeholder="Topic"
      />
      <input
        class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta text-[var(--fg)]"
        bind:value={filters.actor_id}
        placeholder="Actor"
      />
      <input
        class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta text-[var(--fg)]"
        bind:value={filters.since}
        placeholder="Since"
      />
      <input
        class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta text-[var(--fg)]"
        bind:value={filters.until}
        placeholder="Until"
      />
    </div>
    <div class="mt-2 flex justify-end">
      <button
        class="rounded-md border border-[var(--line)] px-2.5 py-1.5 text-meta font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)]"
        onclick={applyFilters}
        type="button"
      >
        Apply filters
      </button>
    </div>
  </section>

  {#if error}
    <p class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text">
      {error}
    </p>
  {/if}

  <section
    class="overflow-hidden rounded-md border border-[var(--line)] bg-[var(--panel)]"
  >
    {#if loading}
      <p class="px-4 py-5 text-meta text-[var(--fg-muted)]">Loading events…</p>
    {:else if events.length === 0}
      <p class="px-4 py-5 text-meta text-[var(--fg-muted)]">
        No events match these filters.
      </p>
    {:else}
      <div class="divide-y divide-[var(--line)]">
        {#each events as event (event.id)}
          <EventRow row={rowFor(event)} inspectable />
        {/each}
      </div>
    {/if}
  </section>

  {#if pageInfo?.has_more}
    <button
      class="rounded-md border border-[var(--line)] px-3 py-1.5 text-meta font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] disabled:opacity-60"
      onclick={showOlder}
      disabled={loadingMore}
      type="button"
    >
      Show older
    </button>
  {/if}
</div>
