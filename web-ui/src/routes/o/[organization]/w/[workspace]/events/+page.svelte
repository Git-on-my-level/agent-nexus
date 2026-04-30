<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import CompactFilterBar from "$lib/components/CompactFilterBar.svelte";
  import EventRow from "$lib/components/EventRow.svelte";
  import { coreClient } from "$lib/coreClient";
  import { initializeAuthSession } from "$lib/authSession";
  import {
    DEFAULT_EVENT_LIST_FILTERS,
    buildEventListApiQuery,
    buildEventListSearchString,
    EVENT_BACKING_SCOPE_VALUES,
    EVENT_GROUP_ORDER,
    hasEventListFilters,
    parseEventListSearchParams,
  } from "$lib/eventFilters";
  import { HOME_FEED_PRESET, normalizeEventRow } from "$lib/events/eventRows";
  import { workspacePath } from "$lib/workspacePaths";

  const EVENT_GROUP_LABELS = {
    messages: "Messages",
    topics: "Topics",
    documents: "Documents",
    boards: "Boards",
    cards: "Cards",
    attention: "Attention",
    notifications: "Notifications",
    reviews: "Reviews",
    exceptions: "Exceptions",
  };
  const BACKING_SCOPE_LABELS = {
    all: "All",
    standalone: "Standalone",
    backing_only: "Backing only",
  };

  let events = $state([]);
  let pageInfo = $state({ has_more: false, next_cursor: "" });
  let loading = $state(true);
  let loadingMore = $state(false);
  let error = $state("");
  let filters = $state({ ...DEFAULT_EVENT_LIST_FILTERS });
  let urlCursor = $state("");
  let filtersOpen = $state(false);

  /** @type {string | null} */
  let prevFilterSig = null;
  /** @type {string | null} */
  let prevUrlCursor = null;

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let hasActiveFilters = $derived(hasEventListFilters(filters));

  function workspaceHref(pathname = "/") {
    return workspacePath(organizationSlug, workspaceSlug, pathname);
  }

  function rowFor(event) {
    return normalizeEventRow(event, { workspaceHref });
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
      const cursorForQuery = append ? pageInfo.next_cursor : urlCursor;
      const result = await coreClient.listEvents(
        buildEventListApiQuery(filters, {
          cursor: cursorForQuery,
          limit: 50,
        }),
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

  $effect(() => {
    const sp = $page.url.searchParams;
    $page.url.search;

    const parsed = parseEventListSearchParams(sp);
    const cur = sp.get("cursor") ?? "";
    const sig = buildEventListSearchString(parsed);

    filters = { ...DEFAULT_EVENT_LIST_FILTERS, ...parsed };
    urlCursor = cur;
    filtersOpen =
      hasEventListFilters(parsed) ||
      String(parsed.preset ?? "").trim() === HOME_FEED_PRESET;

    const isFirst = prevFilterSig === null;
    const filtersChanged = isFirst || sig !== prevFilterSig;
    const cursorOnly = !filtersChanged && cur !== prevUrlCursor;

    if (filtersChanged) {
      prevFilterSig = sig;
      prevUrlCursor = cur;
      void loadEvents();
      return;
    }
    if (cursorOnly && cur) {
      prevUrlCursor = cur;
      void loadEvents({ append: true });
      return;
    }
    if (cursorOnly && !cur) {
      prevUrlCursor = cur;
      void loadEvents();
    }
  });

  async function applyFilters() {
    const base = workspaceHref("/events");
    const qs = buildEventListSearchString(filters);
    await goto(`${base}${qs ? `?${qs}` : ""}`, {
      noScroll: true,
      keepFocus: true,
    });
  }

  async function clearFilters() {
    filters = { ...DEFAULT_EVENT_LIST_FILTERS };
    filtersOpen = false;
    const base = workspaceHref("/events");
    await goto(base, { noScroll: true, keepFocus: true });
  }

  async function showOlder() {
    const nextCursor = pageInfo?.next_cursor ?? "";
    if (!nextCursor) return;
    const base = workspaceHref("/events");
    const qs = buildEventListSearchString(filters);
    const params = new URLSearchParams(qs);
    params.set("cursor", nextCursor);
    const query = params.toString();
    await goto(`${base}${query ? `?${query}` : ""}`, {
      noScroll: true,
      keepFocus: true,
    });
  }

  async function useHomePreset() {
    filters.preset =
      filters.preset === HOME_FEED_PRESET ? "" : HOME_FEED_PRESET;
    await applyFilters();
  }

  function toggleEventGroup(group) {
    const current = new Set(filters.event_group ?? []);
    if (current.has(group)) {
      current.delete(group);
    } else {
      current.add(group);
    }
    filters.event_group = EVENT_GROUP_ORDER.filter((value) =>
      current.has(value),
    );
  }
</script>

<div class="min-w-0 max-w-full space-y-5">
  <div
    class="mb-3 flex max-md:mb-2 flex-wrap items-center justify-between gap-2"
  >
    <div class="min-w-0">
      <h1 class="text-subtitle font-semibold text-[var(--fg)]">Events</h1>
      <p class="mt-0.5 text-meta text-[var(--fg-muted)]">
        Full workspace event history.
      </p>
    </div>
    <div class="flex flex-wrap items-center justify-end gap-1.5">
      <button
        class="cursor-pointer rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-micro font-medium transition-colors hover:bg-[var(--line-subtle)] {filters.preset ===
        HOME_FEED_PRESET
          ? 'text-[var(--fg)]'
          : 'text-[var(--fg-muted)]'}"
        onclick={useHomePreset}
        type="button"
      >
        Home feed
      </button>
      <button
        class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-micro font-medium transition-colors {hasActiveFilters
          ? 'border-[var(--accent)]/40 bg-[var(--accent)]/10 text-[var(--accent)] hover:bg-[var(--accent)]/15'
          : 'border-[var(--line)] bg-[var(--bg-soft)] text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]'}"
        onclick={() => (filtersOpen = !filtersOpen)}
        type="button"
      >
        <svg
          class="h-3.5 w-3.5"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="2"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M3 4a1 1 0 011-1h16a1 1 0 011 1v2.586a1 1 0 01-.293.707l-6.414 6.414a1 1 0 00-.293.707V17l-4 4v-6.586a1 1 0 00-.293-.707L3.293 7.293A1 1 0 013 6.586V4z"
          />
        </svg>
        {hasActiveFilters ? "Filtered" : "Filter"}
      </button>
    </div>
  </div>

  {#if filtersOpen}
    <CompactFilterBar testId="events-filter-panel">
      {#snippet children()}
        <form
          class="contents"
          onsubmit={(event) => {
            event.preventDefault();
            void applyFilters();
          }}
        >
          <div class="grid gap-2 sm:grid-cols-2 lg:grid-cols-6">
            <label
              class="text-micro font-medium text-[var(--fg-muted)] sm:col-span-2 lg:col-span-2"
            >
              Search
              <input
                bind:value={filters.q}
                class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
                placeholder="Search"
              />
            </label>
            <label class="text-micro font-medium text-[var(--fg-muted)]">
              Type
              <input
                bind:value={filters.type}
                class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
                placeholder="Type"
              />
            </label>
            <label class="text-micro font-medium text-[var(--fg-muted)]">
              Backing
              <select
                bind:value={filters.backing_scope}
                class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
              >
                {#each EVENT_BACKING_SCOPE_VALUES as scope}
                  <option value={scope}
                    >{BACKING_SCOPE_LABELS[scope] ?? scope}</option
                  >
                {/each}
              </select>
            </label>
            <label class="text-micro font-medium text-[var(--fg-muted)]">
              Topic
              <input
                bind:value={filters.topic_id}
                class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
                placeholder="Topic ID"
              />
            </label>
            <label class="text-micro font-medium text-[var(--fg-muted)]">
              Actor
              <input
                bind:value={filters.actor_id}
                class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
                placeholder="Actor ID"
              />
            </label>
            <label class="text-micro font-medium text-[var(--fg-muted)]">
              Since
              <input
                bind:value={filters.since}
                class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
                placeholder="Since"
              />
            </label>
            <label class="text-micro font-medium text-[var(--fg-muted)]">
              Until
              <input
                bind:value={filters.until}
                class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
                placeholder="Until"
              />
            </label>
          </div>
          <div class="mt-3 text-micro">
            <span class="font-medium text-[var(--fg-muted)]">Event groups</span>
            <fieldset
              class="mt-1 flex flex-wrap gap-2 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-2"
            >
              {#each EVENT_GROUP_ORDER as group}
                <label
                  class="flex cursor-pointer items-center gap-1.5 text-meta text-[var(--fg)]"
                >
                  <input
                    checked={(filters.event_group ?? []).includes(group)}
                    class="h-3.5 w-3.5 cursor-pointer rounded border-[var(--line)] bg-[var(--bg)] text-[var(--accent-hover)] focus:ring-2 focus:ring-[var(--accent)] focus:ring-offset-0"
                    type="checkbox"
                    onchange={() => toggleEventGroup(group)}
                  />
                  {EVENT_GROUP_LABELS[group] ?? group}
                </label>
              {/each}
            </fieldset>
          </div>
          <div class="mt-3 flex gap-1.5">
            <button
              class="cursor-pointer rounded-md bg-[var(--panel)] px-3 py-1.5 text-micro font-medium text-[var(--fg)] hover:bg-[var(--line)]"
              type="submit">Apply</button
            >
            <button
              class="cursor-pointer rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-1.5 text-micro font-medium text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]"
              onclick={() => void clearFilters()}
              type="button">Clear filters</button
            >
          </div>
        </form>
      {/snippet}
    </CompactFilterBar>
  {/if}

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
      onclick={() => void showOlder()}
      disabled={loadingMore}
      type="button"
    >
      Show older
    </button>
  {/if}
</div>
