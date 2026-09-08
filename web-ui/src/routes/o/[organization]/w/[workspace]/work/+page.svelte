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
  let attentionCount = $derived(
    records.filter((work) => work.phase === "blocked").length,
  );
  let evidenceCount = $derived(
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
</script>

<svelte:head><title>Work · Agent Nexus</title></svelte:head>
<WorkspacePageShell>
  <WorkspacePageHeader title="Work">
    {#snippet subtitle()}<span class="hidden sm:inline"
        >Commitments across your sources. Evidence beside every next step.</span
      >{/snippet}
    {#snippet actions()}<a class="ui-btn-secondary" href={workspaceHref("/pm")}
        >Ask PM</a
      ><a class="ui-btn-primary" href={workspaceHref("/work/new")}
        >New commitment</a
      >{/snippet}
  </WorkspacePageHeader>
  <div
    class="flex flex-wrap items-center gap-x-5 gap-y-2 border-y border-line py-3 text-meta"
    aria-live="polite"
  >
    <span
      ><strong class="text-fg">{records.length}</strong>
      <span class="text-fg-muted"
        >loaded{nextCursor ? "; more available" : ""}</span
      ></span
    >
    <span class={attentionCount ? "text-warn-text" : "text-fg-muted"}
      >{attentionCount} blocked</span
    >
    <a
      class={evidenceCount
        ? "text-warn-text hover:underline"
        : "text-fg-muted hover:underline"}
      href={workspaceHref("/integrations")}
      >{evidenceCount} with stale, missing or failed evidence</a
    >
    <nav
      class="ml-auto flex rounded-md border border-line bg-bg-soft p-0.5"
      aria-label="Work view"
    >
      <a
        class="rounded px-3 py-1.5 text-micro {view === 'table'
          ? 'bg-panel text-fg'
          : 'text-fg-muted'}"
        href={queryHref({ view: "table" })}
        aria-current={view === "table" ? "page" : undefined}>Table</a
      >
      <a
        class="rounded px-3 py-1.5 text-micro {view === 'board'
          ? 'bg-panel text-fg'
          : 'text-fg-muted'}"
        href={queryHref({ view: "board" })}
        aria-current={view === "board" ? "page" : undefined}>Board</a
      >
    </nav>
  </div>
  <div class="flex flex-wrap items-end gap-2">
    <form
      class="min-w-48 flex-1"
      onsubmit={(event) => {
        event.preventDefault();
        setFilter("q", search);
      }}
    >
      <label class="text-micro text-fg-muted"
        >Search work
        <div class="mt-1 flex gap-1">
          <input
            class="ui-input w-full"
            type="search"
            bind:value={search}
            placeholder="Title, source ID, or next action"
          /><button class="ui-btn-secondary" type="submit">Search</button>
        </div></label
      >
    </form>
    <label class="text-micro text-fg-muted"
      >Source<select
        class="ui-input mt-1 block"
        value={filters.source}
        onchange={(event) => setFilter("source", event.currentTarget.value)}
        ><option value="">All sources</option
        >{#each ["nexus", "github", "multica", "git", "other"] as source}<option
            value={source}
            >{source === "nexus"
              ? "Nexus"
              : source === "github"
                ? "GitHub"
                : source === "multica"
                  ? "Multica"
                  : source}</option
          >{/each}</select
      ></label
    >
    <label class="text-micro text-fg-muted"
      >Phase<select
        class="ui-input mt-1 block"
        value={filters.phase}
        onchange={(event) => setFilter("phase", event.currentTarget.value)}
        ><option value="">All phases</option>{#each PHASES as phase}<option
            value={phase}>{label(phase)}</option
          >{/each}</select
      ></label
    >
    <label class="text-micro text-fg-muted"
      >Freshness<select
        class="ui-input mt-1 block"
        value={filters.freshness}
        onchange={(event) => setFilter("freshness", event.currentTarget.value)}
        ><option value="">Any freshness</option><option value="fresh"
          >Fresh</option
        ><option value="stale">Stale</option><option value="error"
          >Refresh failed</option
        ><option value="unknown">Unknown</option></select
      ></label
    >
    <button class="ui-btn-secondary" onclick={() => load()} disabled={loading}
      >{loading ? "Loading work…" : "Reload"}</button
    >
  </div>
  <details class="text-micro text-fg-muted">
    <summary class="w-fit cursor-pointer"
      >Project and owner filters{filters.project_ref || filters.owner
        ? " · active"
        : ""}</summary
    >
    <div class="mt-2 flex flex-wrap gap-3">
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
        class="flex flex-wrap items-end gap-2"
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
    </div>
  </details>
  {#if activeFilters}<a
      class="w-fit text-micro text-accent-text hover:underline"
      href={workspaceHref(`/work?view=${view}`)}>Clear filters</a
    >{/if}
  {#if error}<StateError
      title="Work could not be refreshed"
      message={error}
      onretry={() => load()}
      retrying={loading}
    />{#if records.length}<p class="text-micro text-warn-text">
        Showing the previously loaded records. They may no longer match current
        filters or source state.
      </p>{/if}{/if}
  {#if loading && !records.length}<div
      class="rounded-md border border-line bg-panel px-4 py-12 text-center text-fg-muted"
      role="status"
    >
      Loading commitments and evidence…
    </div>
  {:else if !error && !records.length}<section
      class="rounded-md border border-line bg-panel px-5 py-12 text-center"
    >
      <h2 class="text-subtitle font-semibold text-fg">
        {activeFilters
          ? "No matching commitments"
          : "Your commitments, in one place"}
      </h2>
      <p class="mx-auto mt-2 max-w-lg text-meta text-fg-muted">
        {activeFilters
          ? "Change or clear your filters to see more work."
          : "Create a Nexus commitment or register work from an authoritative source. Empty views do not establish integration health."}
      </p>
      <a
        class="mt-4 inline-block text-accent-text hover:underline"
        href={activeFilters
          ? workspaceHref("/work")
          : workspaceHref("/work/new")}
        >{activeFilters ? "Clear filters" : "Create a commitment"}</a
      >
    </section>
  {:else if records.length}<WorkViews
      {records}
      {view}
      {workspaceHref}
      {now}
    />{/if}
  {#if nextCursor}<button
      class="ui-btn-secondary mx-auto"
      onclick={() => load(true)}
      disabled={loading}
      >{loading ? "Loading more work…" : "Load more work"}</button
    >{/if}
  <p class="text-micro text-fg-muted">
    Board and table show the same work records. External workflows stay with
    their source; request changes through <a
      class="text-accent-text hover:underline"
      href={workspaceHref("/pm")}>PM</a
    >.
  </p>
</WorkspacePageShell>
