<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import { coreClient } from "$lib/coreClient";
  import Time from "$lib/time/Time.svelte";
  import {
    buildThreadFilterQueryParamsFromThreadListState,
    buildTopicListSearchString,
    parseTopicListSearchParams,
    TOPIC_STATUSES,
  } from "$lib/topicFilters";
  import {
    actorDirectoryLoaded,
    actorRegistry,
    agentDirectoryLoaded,
    agentRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import { buildOwnerDisplay } from "$lib/ownerDisplay.js";
  import {
    threadRowContextParts,
    threadSubjectRef,
  } from "$lib/threadListContext.js";
  import { createInflightSlot } from "$lib/urlState";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import LifecycleStateFilter from "$lib/components/LifecycleStateFilter.svelte";
  import WorkspaceListFilters from "$lib/components/WorkspaceListFilters.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import SkeletonList from "$lib/components/state/SkeletonList.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import CopyButton from "$lib/components/CopyButton.svelte";
  import {
    resourceDisplayLabel,
    resourceRouteSegment,
  } from "$lib/resourceIdentity.js";
  import { usePageTitle } from "$lib/pageTitle.svelte.js";

  let loading = $state(false);
  let error = $state("");
  let retrying = $state(false);

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);

  let backingThreads = $state([]);
  let activeListLoadToken = 0;

  let workspaceHref = $derived(
    bindWorkspaceHref(organizationSlug, workspaceSlug),
  );

  /*
   * The URL is the filter state: a narrowed list is shareable and the Back
   * button restores it. Only the search box holds a draft, until it is
   * submitted.
   */
  let urlFilters = $derived(parseTopicListSearchParams($page.url.searchParams));
  let states = $derived(urlFilters.states);
  let query = $derived(urlFilters.q);
  let searchDraft = $state("");
  $effect(() => {
    searchDraft = query;
  });
  /*
   * Each live control navigates on its own. Blurring search to click a
   * lifecycle box fires two `goto`s before `$page` updates, so the second
   * must not rebuild from the still-stale URL.
   */
  const inflightFilters = createInflightSlot();
  $effect(() => {
    void $page.url.search;
    inflightFilters.clear();
  });

  let filterCount = $derived(
    (states.length === 1 && states[0] === "active" ? 0 : 1) + (query ? 1 : 0),
  );
  let hasActiveFilters = $derived(filterCount > 0);

  /**
   * Who a thread belongs to, from `updated_by` on the row and the directory
   * the shell already loaded. Two threads can share a title ("Asks", one per
   * actor); they do not share an owner.
   */
  let ownerCellFor = $derived(
    buildOwnerDisplay({
      actors: $actorRegistry,
      principals: $principalRegistry,
      agents: $agentRegistry,
      actorsLoaded: $actorDirectoryLoaded,
      agentsLoaded: $agentDirectoryLoaded,
    }),
  );
  function rowContext(thread) {
    const owner = ownerCellFor(thread?.updated_by || thread?.created_by);
    return threadRowContextParts(thread, {
      ownerName: owner && !owner.pending ? owner.name : "",
    });
  }

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
      const listQuery = buildThreadFilterQueryParamsFromThreadListState(state);
      const response = await coreClient.listThreads(listQuery);
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
    void loadBackingThreadsFromState({ states, q: query });
  });

  function currentFilters() {
    return inflightFilters.get({ states, q: searchDraft });
  }

  /** Apply filters by navigating: the list re-reads from the new URL. */
  function applyFilters(next) {
    inflightFilters.set(next);
    const qs = buildTopicListSearchString(next);
    const base = workspaceHref("/threads");
    void goto(`${base}${qs ? `?${qs}` : ""}`, {
      replaceState: true,
      noScroll: true,
      keepFocus: true,
    });
  }

  function resetFilters() {
    applyFilters({});
  }

  /** @param {string} value */
  function toggleLifecycleState(value) {
    const current = currentFilters();
    const set = new Set(current.states);
    if (set.has(value)) {
      if (set.size <= 1) return;
      set.delete(value);
    } else {
      set.add(value);
    }
    applyFilters({
      states: TOPIC_STATUSES.filter((s) => set.has(s)),
      q: current.q,
    });
  }

  usePageTitle(() => "Threads · Agent Nexus");
</script>

<WorkspacePageShell>
  <WorkspacePageHeader title="Threads">
    {#snippet subtitle()}
      <span class="hidden sm:inline"
        >The conversation behind each initiative and doc. Open one to read what
        was said and when.</span
      >
    {/snippet}
  </WorkspacePageHeader>

  {#if error}
    <StateError
      message={error}
      onretry={() =>
        void loadBackingThreadsFromState({ states, q: query }, true)}
      {retrying}
    />
  {/if}

  <WorkspaceListFilters
    activeCount={filterCount}
    toggleTestId="topics-filters-toggle"
    panelTestId="topics-filter-panel"
    openWhenFiltered={hasActiveFilters}
    onclear={resetFilters}
  >
    {#snippet children()}
      <LifecycleStateFilter {states} ontoggle={toggleLifecycleState} />
      <form
        class="flex items-end gap-2"
        onsubmit={(event) => {
          event.preventDefault();
          applyFilters({ states: currentFilters().states, q: searchDraft });
        }}
      >
        <label class="ui-label mb-0" for="thread-filter-search">Search</label>
        <input
          id="thread-filter-search"
          class="ui-input w-auto"
          bind:value={searchDraft}
          placeholder="Title or id…"
          type="search"
          autocomplete="off"
          onchange={() =>
            applyFilters({ states: currentFilters().states, q: searchDraft })}
        />
      </form>
    {/snippet}
  </WorkspaceListFilters>

  {#if loading && backingThreads.length === 0}
    <SkeletonList rows={6} rowClass="h-14" trailing={false} />
  {:else if backingThreads.length === 0 && !error}
    <StateEmpty
      title={hasActiveFilters
        ? "No threads match the current filters"
        : "No threads yet"}
      helper={hasActiveFilters
        ? "Try adjusting or clearing the current filters."
        : "Threads appear as work gets discussed. Nothing has been said here yet."}
      actionLabel={hasActiveFilters ? "Clear filters" : ""}
      onclick={hasActiveFilters ? resetFilters : undefined}
    />
  {:else if backingThreads.length > 0}
    <!-- With an error and no rows the bordered container would render as a
         stray 2px rule under the alert. -->
    <div
      class="space-y-px overflow-hidden rounded-md border border-line bg-bg-soft"
    >
      {#each backingThreads as thread, i}
        {@const topicSeg = topicSegmentFromTypedRef(thread.topic_ref)}
        <div class="flex items-stretch {i > 0 ? 'border-t border-line' : ''}">
          <a
            class="flex min-w-0 flex-1 flex-col gap-0.5 px-3 py-2.5 transition-colors hover:bg-panel-hover"
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
            <!-- Names, not identifiers: the ref is one click away behind
                 Copy ref for whoever needs it on the CLI. The context parts
                 come first, because that is what tells two rows of the same
                 name apart. -->
            <p
              class="text-micro text-fg-muted"
              title={threadSubjectRef(thread)}
            >
              {#each rowContext(thread) as part, partIndex (partIndex)}<span
                  >{part}</span
                ><span aria-hidden="true" class="text-fg-subtle">{" · "}</span
                >{/each}Updated <Time
                value={thread.updated_at}
                fallback="—"
              />{#if topicSeg}{" · "}linked to a project{/if}
            </p>
          </a>
          {#if thread.ref || thread.handle}
            <div class="flex shrink-0 items-center pr-2">
              <CopyButton
                value={thread.ref || `thread:${thread.handle}`}
                label="Copy ref"
                title={thread.ref || `thread:${thread.handle}`}
                iconOnly
              />
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</WorkspacePageShell>
