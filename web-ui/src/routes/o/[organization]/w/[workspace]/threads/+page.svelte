<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import {
    buildThreadFilterQueryParamsFromThreadListState,
    buildTopicListSearchString,
    parseTopicListSearchParams,
  } from "$lib/topicFilters";
  import { BOARD_LIFECYCLE_STATE_LABELS } from "$lib/boardUtils";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import CompactFilterBar from "$lib/components/CompactFilterBar.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import {
    resourceDisplayLabel,
    resourceRouteSegment,
  } from "$lib/resourceIdentity.js";

  const defaultFilters = {
    states: ["active"],
    q: "",
  };

  let filters = $state({ ...defaultFilters });
  let loading = $state(false);
  let error = $state("");
  let retrying = $state(false);
  let filtersOpen = $state(false);

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);

  let backingThreads = $state([]);
  let activeListLoadToken = 0;

  let workspaceHref = $derived(
    bindWorkspaceHref(organizationSlug, workspaceSlug),
  );

  /** @param {string} ref */
  function topicSegmentFromTypedRef(ref) {
    const s = String(ref ?? "").trim();
    if (!s.startsWith("topic:")) return "";
    return s.slice("topic:".length).trim();
  }

  async function loadBackingThreadsFromState(state, isRetry = false) {
    const loadToken = ++activeListLoadToken;
    loading = true;
    error = "";
    retrying = isRetry;
    try {
      const query = buildThreadFilterQueryParamsFromThreadListState(state);
      const response = await coreClient.listThreads(query);
      if (loadToken !== activeListLoadToken) return;
      backingThreads = response.threads ?? [];
    } catch (loadError) {
      if (loadToken !== activeListLoadToken) return;
      const reason =
        loadError instanceof Error ? loadError.message : String(loadError);
      error = `Failed to load threads: ${reason}`;
    } finally {
      if (loadToken === activeListLoadToken) {
        loading = false;
        retrying = false;
      }
    }
  }

  $effect(() => {
    workspaceSlug;
    const parsed = parseTopicListSearchParams($page.url.searchParams);
    filters = { ...defaultFilters, ...parsed };
    if ([...$page.url.searchParams.keys()].length > 0) {
      filtersOpen = true;
    }
    void loadBackingThreadsFromState(parsed);
  });

  async function applyFilters() {
    const qs = buildTopicListSearchString(filters);
    const base = workspaceHref("/threads");
    await goto(`${base}${qs ? `?${qs}` : ""}`, {
      replaceState: true,
      noScroll: true,
      keepFocus: true,
    });
  }

  async function resetFilters() {
    await goto(workspaceHref("/threads"), {
      replaceState: true,
      noScroll: true,
      keepFocus: true,
    });
  }

  let hasActiveFilters = $derived.by(() => {
    const st = filters.states ?? ["active"];
    const isDefaultFilters =
      st.length === 1 && String(st[0]) === "active" && filters.q.trim() === "";
    return !isDefaultFilters;
  });

  /** @param {string} value */
  function toggleLifecycleState(value) {
    const cur = [...(filters.states ?? ["active"])];
    const set = new Set(cur);
    if (set.has(value)) {
      if (set.size <= 1) return;
      set.delete(value);
    } else {
      set.add(value);
    }
    const order = /** @type {const} */ (["active", "archived", "trashed"]);
    filters = {
      ...filters,
      states: order.filter((s) => set.has(s)),
    };
  }
</script>

<div
  class="mb-3 flex max-md:mb-2 flex-wrap items-center justify-between gap-2 sm:items-start sm:gap-4"
>
  <div class="min-w-0 flex-1">
    <h1 class="text-title text-fg">Threads</h1>
    <p class="mt-1 hidden text-micro text-fg-muted sm:block">
      Diagnostic list of append-only backing threads (timelines). Not every
      thread is a topic.
    </p>
  </div>
  <div class="flex flex-wrap items-center justify-end gap-1.5 sm:gap-1.5">
    <button
      class="cursor-pointer inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-micro font-medium transition-colors {hasActiveFilters
        ? 'border-accent bg-accent-soft text-accent hover:bg-accent-soft'
        : 'border-line bg-bg-soft text-fg-muted hover:bg-line-subtle'}"
      onclick={() => (filtersOpen = !filtersOpen)}
      type="button"
      data-testid="topics-filters-toggle"
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
      {hasActiveFilters ? "Filtered" : "Filters"}
    </button>
  </div>
</div>

{#if error}
  <StateError
    message={error}
    onretry={() => void loadBackingThreadsFromState(filters, true)}
    {retrying}
    class="mb-4"
  />
{/if}

{#if filtersOpen}
  <CompactFilterBar testId="topics-filter-panel">
    {#snippet children()}
      <div class="grid gap-3 sm:grid-cols-2">
        <div class="text-micro">
          <span class="font-medium text-fg-muted">Lifecycle</span>
          <fieldset
            class="mt-1 space-y-1 rounded-md border border-line bg-bg-soft px-2.5 py-2"
          >
            {#each Object.entries(BOARD_LIFECYCLE_STATE_LABELS) as [value, label] (value)}
              <label
                class="flex cursor-pointer items-center gap-2 text-meta text-fg"
              >
                <input
                  checked={(filters.states ?? ["active"]).includes(value)}
                  class="h-3.5 w-3.5 cursor-pointer rounded border-line bg-bg text-accent-hover focus:ring-2 focus:ring-accent focus:ring-offset-0"
                  type="checkbox"
                  onchange={() => toggleLifecycleState(value)}
                />
                {label}
              </label>
            {/each}
          </fieldset>
        </div>
        <label class="text-micro sm:col-span-1">
          <span class="font-medium text-fg-muted">Search</span>
          <input
            bind:value={filters.q}
            class="mt-1 w-full rounded-md border border-line bg-bg-soft px-2.5 py-1.5 text-meta transition-colors focus:bg-panel"
            placeholder="Title or id…"
            type="search"
            autocomplete="off"
          />
        </label>
      </div>
      <div class="mt-3 flex gap-1.5">
        <button
          class="cursor-pointer rounded-md bg-panel px-3 py-1.5 text-micro font-medium text-fg hover:bg-line"
          onclick={applyFilters}
          type="button">Apply</button
        >
        <button
          class="cursor-pointer rounded-md border border-line bg-bg-soft px-3 py-1.5 text-micro font-medium text-fg-muted hover:bg-line-subtle"
          onclick={resetFilters}
          type="button">Clear filters</button
        >
      </div>
    {/snippet}
  </CompactFilterBar>
{/if}

{#if loading && backingThreads.length === 0}
  <Skeleton rows={6} />
{:else if backingThreads.length === 0 && !error}
  <StateEmpty
    title={hasActiveFilters
      ? "No threads match the current filters"
      : "No threads returned"}
    helper={hasActiveFilters
      ? "Try adjusting or clearing the current filters."
      : "Backing threads are append-only timelines. Not every thread is a topic."}
    actionLabel={hasActiveFilters ? "Clear filters" : ""}
    onclick={hasActiveFilters ? resetFilters : undefined}
  />
{:else}
  <div
    class="space-y-px overflow-hidden rounded-md border border-line bg-bg-soft"
  >
    {#each backingThreads as thread, i}
      {@const topicSeg = topicSegmentFromTypedRef(thread.topic_ref)}
      <div class="flex items-stretch {i > 0 ? 'border-t border-line' : ''}">
        <a
          class="flex min-w-0 flex-1 flex-col gap-0.5 px-3 py-2.5 transition-colors hover:bg-line-subtle"
          href={workspaceHref(
            `/threads/${encodeURIComponent(resourceRouteSegment(thread, "thread"))}`,
          )}
        >
          <div class="flex flex-wrap items-center gap-2">
            <p class="truncate text-meta font-medium text-fg">
              {resourceDisplayLabel(thread)}
            </p>
            {#if thread.state === "archived"}
              <span
                class="shrink-0 rounded bg-warn-soft px-1.5 py-0.5 text-micro font-medium text-warn-text"
                >Archived</span
              >
            {/if}
          </div>
          {#if thread.ref || thread.handle}
            <p class="truncate font-mono text-micro text-fg-muted">
              {thread.ref || thread.handle}
            </p>
          {/if}
          {#if topicSeg}
            <p class="truncate text-micro text-fg-muted">
              Linked topic:
              <span class="text-fg">{topicSeg}</span>
            </p>
          {:else}
            <p class="truncate text-micro text-fg-muted">
              No topic ref (non-topic or internal timeline)
            </p>
          {/if}
          <p class="text-micro text-fg-muted">
            Updated {formatTimestamp(thread.updated_at) || "—"}
          </p>
        </a>
      </div>
    {/each}
  </div>
{/if}
