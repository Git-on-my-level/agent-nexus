<script>
  import { onMount } from "svelte";
  import { page } from "$app/stores";
  import { goto } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
  import { initializeAuthSession } from "$lib/authSession";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import WorkViews from "$lib/components/pm/WorkViews.svelte";
  import {
    PHASES,
    label,
    workFreshness,
    workKey,
    errorMessage,
  } from "$lib/pm/presentation.js";

  let records = $state([]),
    loading = $state(true),
    error = $state(""),
    nextCursor = $state(""),
    loaded = $state(false),
    now = $state(Date.now());
  let requestId = 0;
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let view = $derived(
    $page.url.searchParams.get("view") === "board" ? "board" : "table",
  );
  let filters = $derived(
    Object.fromEntries(
      ["q", "source", "project_ref", "owner", "phase", "freshness"].map(
        (key) => [key, $page.url.searchParams.get(key) || ""],
      ),
    ),
  );
  let filterKey = $derived(JSON.stringify(filters));
  let activeFilters = $derived(Object.values(filters).some(Boolean));
  let blockedCount = $derived(
    records.filter((work) => work.phase === "blocked").length,
  );
  let staleCount = $derived(
    records.filter((work) => workFreshness(work, now).key !== "fresh").length,
  );
  let search = $state("");
  $effect(() => {
    search = filters.q;
  });
  $effect(() => {
    const key = filterKey;
    if (loaded) void load(false, JSON.parse(key));
  });

  function queryHref(changes) {
    const params = new URLSearchParams($page.url.searchParams);
    for (const [key, value] of Object.entries(changes)) {
      if (value) params.set(key, value);
      else params.delete(key);
    }
    return `${workspaceHref("/work")}?${params}`;
  }
  function setFilter(key, value) {
    void goto(queryHref({ [key]: value }), { keepFocus: true, noScroll: true });
  }
  async function load(append = false, query = filters) {
    const id = ++requestId;
    loading = true;
    error = "";
    try {
      const result = await coreClient.listWork({
        ...query,
        limit: 50,
        cursor: append ? nextCursor : undefined,
      });
      if (id !== requestId) return;
      if (!Array.isArray(result.work))
        throw new Error(
          "The workspace returned an invalid work list. Reload to try again.",
        );
      const rows = append ? [...records, ...result.work] : result.work;
      records = [
        ...new Map(rows.map((work) => [workKey(work), work])).values(),
      ];
      nextCursor = result.next_cursor || "";
    } catch (err) {
      if (id === requestId) error = errorMessage(err);
    } finally {
      if (id === requestId) loading = false;
    }
  }
  onMount(() => {
    let disposed = false;
    initializeAuthSession({
      fetchFn: globalThis.fetch.bind(globalThis),
      workspaceSlug: $page.params.workspace,
      authDriver: "work-portfolio",
    })
      .then(() => {
        if (!disposed) loaded = true;
      })
      .catch((err) => {
        error = errorMessage(err);
        loading = false;
      });
    const timer = setInterval(() => {
      now = Date.now();
    }, 30_000);
    return () => {
      disposed = true;
      requestId++;
      clearInterval(timer);
    };
  });
  const SOURCES = [
    ["nexus", "Nexus"],
    ["github", "GitHub"],
    ["multica", "Multica"],
    ["git", "Git"],
    ["other", "Other"],
  ];
</script>

<svelte:head><title>Work · Agent Nexus</title></svelte:head>
<WorkspacePageShell>
  <WorkspacePageHeader title="Work">
    {#snippet subtitle()}
      {#if records.length}
        <span class="text-fg-muted"
          >{records.length}{nextCursor ? "+" : ""} tracked</span
        >{#if blockedCount}
          · <span class="text-warn-text">{blockedCount} blocked</span
          >{/if}{#if staleCount}
          · <a
            class="text-fg-muted underline decoration-line-strong underline-offset-2 hover:text-fg"
            href={workspaceHref("/integrations")}
            >{staleCount} without fresh evidence</a
          >{/if}
      {/if}
    {/snippet}
    {#snippet actions()}
      <a class="ui-btn-secondary" href={workspaceHref("/pm")}>Ask PM</a>
      <a class="ui-btn-primary" href={workspaceHref("/work/new")}>New work</a>
    {/snippet}
  </WorkspacePageHeader>

  <div class="flex flex-wrap items-center gap-2">
    <form
      class="min-w-48 flex-1"
      onsubmit={(event) => {
        event.preventDefault();
        setFilter("q", search);
      }}
    >
      <label class="sr-only" for="work-search">Search work</label>
      <input
        id="work-search"
        class="ui-input"
        type="search"
        bind:value={search}
        placeholder="Search title, source ID or next action…"
      />
    </form>
    <label class="sr-only" for="work-filter-source">Source</label>
    <select
      id="work-filter-source"
      class="ui-input w-auto"
      value={filters.source}
      onchange={(event) => setFilter("source", event.currentTarget.value)}
    >
      <option value="">All sources</option>
      {#each SOURCES as [value, title]}<option {value}>{title}</option>{/each}
    </select>
    <label class="sr-only" for="work-filter-phase">Phase</label>
    <select
      id="work-filter-phase"
      class="ui-input w-auto"
      value={filters.phase}
      onchange={(event) => setFilter("phase", event.currentTarget.value)}
    >
      <option value="">All phases</option>
      {#each PHASES as phase}<option value={phase}>{label(phase)}</option
        >{/each}
    </select>
    <label class="sr-only" for="work-filter-freshness">Freshness</label>
    <select
      id="work-filter-freshness"
      class="ui-input w-auto"
      value={filters.freshness}
      onchange={(event) => setFilter("freshness", event.currentTarget.value)}
    >
      <option value="">Any freshness</option>
      <option value="fresh">Fresh</option>
      <option value="stale">Stale</option>
      <option value="error">Refresh failed</option>
      <option value="unknown">Unknown</option>
    </select>
    <nav
      class="flex rounded-md border border-line bg-bg-soft p-0.5"
      aria-label="Work view"
    >
      <a
        class="rounded px-2.5 py-1 text-micro {view === 'table'
          ? 'bg-panel text-fg'
          : 'text-fg-muted'}"
        href={queryHref({ view: "table" })}
        aria-current={view === "table" ? "page" : undefined}>Table</a
      >
      <a
        class="rounded px-2.5 py-1 text-micro {view === 'board'
          ? 'bg-panel text-fg'
          : 'text-fg-muted'}"
        href={queryHref({ view: "board" })}
        aria-current={view === "board" ? "page" : undefined}>Board</a
      >
    </nav>
    <button
      class="ui-btn-secondary"
      onclick={() => load()}
      disabled={loading}
      aria-label="Reload">{loading ? "Loading…" : "Reload"}</button
    >
  </div>

  <div
    class="flex flex-wrap items-center gap-x-4 gap-y-1 text-micro text-fg-muted"
  >
    <details class="text-micro text-fg-muted">
      <summary class="w-fit cursor-pointer"
        >Project and owner{filters.project_ref || filters.owner
          ? " · active"
          : ""}</summary
      >
      <form
        onsubmit={(event) => {
          event.preventDefault();
          const data = new FormData(event.currentTarget);
          void goto(
            queryHref({
              project_ref: String(data.get("project_ref") || "").trim(),
              owner: String(data.get("owner") || "").trim(),
            }),
          );
        }}
        class="mt-2 flex flex-wrap items-end gap-2"
      >
        <label
          >Project reference<input
            name="project_ref"
            class="ui-input mt-1 block"
            value={filters.project_ref}
            placeholder="topic:project"
          /></label
        ><label
          >Owner<input
            name="owner"
            class="ui-input mt-1 block"
            value={filters.owner}
            placeholder="Owner identifier"
          /></label
        ><button class="ui-btn-secondary" type="submit">Apply</button>
      </form>
    </details>
    {#if activeFilters}
      <a class="ui-prose-link" href={workspaceHref(`/work?view=${view}`)}
        >Clear filters</a
      >
    {/if}
  </div>

  {#if error}
    <StateError
      title="Work could not be refreshed"
      message={error}
      onretry={() => load()}
      retrying={loading}
    />
    {#if records.length}
      <p class="text-micro text-warn-text">
        Showing the previously loaded records. They may no longer match current
        filters or source state.
      </p>
    {/if}
  {/if}

  {#if loading && !records.length}
    <p class="py-10 text-center text-meta text-fg-muted" role="status">
      Loading commitments and evidence…
    </p>
  {:else if !error && !records.length}
    <section class="py-14 text-center">
      <h2 class="text-subtitle font-semibold text-fg">
        {activeFilters ? "No matching work" : "Nothing tracked yet"}
      </h2>
      <p class="mx-auto mt-2 max-w-md text-meta text-fg-muted">
        {activeFilters
          ? "Change or clear the filters."
          : "Create work here, or connect a source so its issues show up."}
      </p>
      <a
        class="ui-prose-link mt-4 inline-block text-meta"
        href={activeFilters
          ? workspaceHref("/work")
          : workspaceHref("/work/new")}
        >{activeFilters ? "Clear filters" : "Create work"}</a
      >
    </section>
  {:else if records.length}
    <WorkViews {records} {view} {workspaceHref} {now} />
  {/if}

  {#if nextCursor}
    <button
      class="ui-btn-secondary mx-auto"
      onclick={() => load(true)}
      disabled={loading}>{loading ? "Loading…" : "Load more"}</button
    >
  {/if}
</WorkspacePageShell>
