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
    isNexusOwned,
    workFreshness,
    workKey,
    errorMessage,
  } from "$lib/pm/presentation.js";
  import { navIconPath } from "$lib/icons.js";
  import { openCommandPalette } from "$lib/stores/commandPalette.js";
  import {
    applyTaskPhaseMove,
    requestedDecisionMap,
  } from "$lib/taskBoardMove.js";

  let records = $state([]),
    loading = $state(true),
    error = $state(""),
    nextCursor = $state(""),
    loaded = $state(false),
    now = $state(Date.now());
  let requested = $state({});
  let boards = $state([]);
  let boardsLoaded = false;
  let decisions = $state([]);
  let decisionsLoaded = $state(false);
  let shortcutsOpen = $state(false);
  let moveError = $state("");
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
  // Three separate facts, three separate filters. They used to be blended into
  // one "without fresh evidence" number, which merged "we have never looked"
  // with "the source is down" — two problems with different fixes.
  let neverCheckedCount = $derived(
    records.filter(
      (work) =>
        !isNexusOwned(work) && workFreshness(work, now).key === "unknown",
    ).length,
  );
  let unreachableCount = $derived(
    records.filter((work) => workFreshness(work, now).key === "error").length,
  );
  let boardTitles = $derived(
    Object.fromEntries(
      boards
        .map((entry) => entry?.board ?? entry)
        .filter((board) => board && (board.ref || board.handle || board.id))
        .map((board) => [
          board.ref || board.handle || board.id,
          board.title || board.name || board.handle || board.id,
        ]),
    ),
  );
  let filterCount = $derived(
    ["source", "phase", "freshness", "project_ref", "owner"].filter(
      (key) => filters[key],
    ).length,
  );
  let requestedDecisions = $derived(requestedDecisionMap(decisions, records));
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
    return `${workspaceHref("/tasks")}?${params}`;
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
      if (!decisionsLoaded) void loadDecisions();
      if (!boardsLoaded) void loadBoards();
    } catch (err) {
      if (id === requestId) error = errorMessage(err);
    } finally {
      if (id === requestId) loading = false;
    }
  }
  async function loadBoards() {
    boardsLoaded = true;
    try {
      const result = await coreClient.listBoards({ limit: 200 });
      boards = Array.isArray(result?.boards) ? result.boards : [];
    } catch {
      // Fail soft: the Board column falls back to the board_ref slug.
    }
  }
  async function loadDecisions() {
    decisionsLoaded = true;
    try {
      const result = await coreClient.listPmDecisions({ limit: 200 });
      decisions = Array.isArray(result?.items) ? result.items : [];
    } catch {
      // Fail soft: the Requested badge link degrades, the page stays usable.
    }
  }
  async function moveTask(work, phase) {
    moveError = "";
    const key = workKey(work);
    try {
      const result = await applyTaskPhaseMove(coreClient, work, phase);
      if (result.kind === "moved") {
        records = records.map((item) =>
          workKey(item) === key ? { ...item, phase } : item,
        );
        const next = { ...requested };
        delete next[key];
        requested = next;
        decisions = decisions.filter(
          (decision) => decision.work_ref !== work.ref,
        );
      } else if (result.kind === "requested") {
        requested = { ...requested, [key]: phase };
        if (result.decision) decisions = [...decisions, result.decision];
      }
    } catch (err) {
      moveError = errorMessage(err);
    }
  }
  function isTextEntryTarget(target) {
    return (
      target instanceof HTMLElement &&
      (target.isContentEditable ||
        ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName))
    );
  }
  function taskFocusTargets() {
    return view === "board"
      ? [...document.querySelectorAll('[data-work-ref][tabindex="0"]')]
      : [...document.querySelectorAll("tr[data-work-ref] a[href]")];
  }
  function moveTaskFocus(delta) {
    const targets = taskFocusTargets();
    if (!targets.length) return;
    const active = document.activeElement;
    const currentIndex = targets.findIndex(
      (target) => target === active || target.contains(active),
    );
    const nextIndex =
      currentIndex < 0
        ? 0
        : Math.min(targets.length - 1, Math.max(0, currentIndex + delta));
    targets[nextIndex].focus();
  }
  function setViewFromKeyboard(next) {
    if (view === next) return;
    void goto(queryHref({ view: next }), { keepFocus: true, noScroll: true });
  }
  function handleShortcutKeydown(event) {
    if (event.key === "Escape") {
      if (shortcutsOpen) {
        event.preventDefault();
        shortcutsOpen = false;
      }
      return;
    }
    if (
      event.defaultPrevented ||
      event.metaKey ||
      event.ctrlKey ||
      event.altKey ||
      isTextEntryTarget(event.target) ||
      (!shortcutsOpen && document.querySelector('[aria-modal="true"]'))
    )
      return;
    if (shortcutsOpen) {
      if (event.key === "?") {
        event.preventDefault();
        shortcutsOpen = false;
      }
      return;
    }
    if (event.key === "?") {
      event.preventDefault();
      shortcutsOpener = document.activeElement;
      shortcutsOpen = true;
    } else if (event.key === "j") {
      event.preventDefault();
      moveTaskFocus(1);
    } else if (event.key === "k") {
      event.preventDefault();
      moveTaskFocus(-1);
    } else if (event.key === "b") {
      setViewFromKeyboard("board");
    } else if (event.key === "t") {
      setViewFromKeyboard("table");
    }
  }
  onMount(() => {
    let disposed = false;
    initializeAuthSession({
      fetchFn: globalThis.fetch.bind(globalThis),
      workspaceSlug: $page.params.workspace,
      authDriver: "task-portfolio",
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
  const SHORTCUTS = [
    ["Next task", ["J"]],
    ["Previous task", ["K"]],
    ["Open focused task", ["Enter"]],
    ["Board view", ["B"]],
    ["Table view", ["T"]],
    ["Shortcut help", ["?"]],
    ["Close this help", ["Esc"]],
  ];
  let shortcutsDialog = $state(null);
  let shortcutsWereOpen = false;
  // The "?" button is gone — the overlay is a keyboard surface, opened and
  // closed with "?" — so focus returns to whatever the reader was on.
  let shortcutsOpener = null;
  $effect(() => {
    if (shortcutsOpen) {
      shortcutsDialog?.focus();
      shortcutsWereOpen = true;
    } else if (shortcutsWereOpen) {
      shortcutsWereOpen = false;
      shortcutsOpener?.focus?.();
      shortcutsOpener = null;
    }
  });
</script>

<svelte:head><title>Tasks · Agent Nexus</title></svelte:head>
<svelte:window onkeydown={handleShortcutKeydown} />
<WorkspacePageShell>
  <WorkspacePageHeader title="Tasks">
    {#snippet subtitle()}
      {#if records.length}
        <span class="text-fg-muted"
          >{records.length}{nextCursor ? "+" : ""} tracked</span
        >{#if blockedCount}
          · <a
            class="ui-prose-link text-warn-text"
            href={queryHref({ phase: "blocked" })}>{blockedCount} blocked</a
          >{/if}{#if neverCheckedCount}
          · <a class="ui-prose-link" href={queryHref({ freshness: "unknown" })}
            >{neverCheckedCount} never checked</a
          >{/if}{#if unreachableCount}
          · <a
            class="ui-prose-link text-warn-text"
            href={workspaceHref("/integrations")}
            >{unreachableCount} can't reach source</a
          >{/if}
      {/if}
    {/snippet}
    {#snippet actions()}
      <button
        class="ui-icon-btn"
        onclick={openCommandPalette}
        aria-label="Search workspace"
        aria-keyshortcuts="Meta+K"
        title="Search workspace (⌘K)"
        type="button"
      >
        <svg
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="1.5"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d={navIconPath("search")} />
        </svg>
      </button>
      <a class="ui-btn-secondary" href={workspaceHref("/pm")}>Ask PM</a>
      <a class="ui-btn-primary" href={workspaceHref("/tasks/new")}>New task</a>
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
      <label class="sr-only" for="task-search">Search tasks</label>
      <input
        id="task-search"
        class="ui-input"
        type="search"
        bind:value={search}
        placeholder="Search title, source ID or next action…"
      />
    </form>
    <!-- The board is a desktop surface: 18rem columns cannot be dragged on a
         390px screen, so the toggle that leads there is hidden below 640px. -->
    <nav
      class="hidden rounded-md border border-line bg-bg-soft p-0.5 sm:flex"
      aria-label="Task view"
    >
      <a
        class="rounded px-2.5 py-1 text-micro font-medium {view === 'table'
          ? 'bg-panel text-fg'
          : 'text-fg-muted'}"
        href={queryHref({ view: "table" })}
        aria-current={view === "table" ? "page" : undefined}>Table</a
      >
      <a
        class="rounded px-2.5 py-1 text-micro font-medium {view === 'board'
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

  <!--
    One disclosure, not four control rows. Source, Status, Freshness and the
    project/owner form were four separate always-open surfaces above a list
    that most readers never filtered.
  -->
  <details class="text-micro text-fg-muted">
    <summary class="w-fit cursor-pointer"
      >Filters{#if filterCount}
        · {filterCount} active{/if}</summary
    >
    <div class="mt-2 flex flex-wrap items-end gap-2">
      <label class="ui-label mb-0" for="task-filter-source">Source</label>
      <select
        id="task-filter-source"
        class="ui-input w-auto"
        value={filters.source}
        onchange={(event) => setFilter("source", event.currentTarget.value)}
      >
        <option value="">All sources</option>
        {#each SOURCES as [value, title]}<option {value}>{title}</option>{/each}
      </select>
      <label class="ui-label mb-0" for="task-filter-phase">Status</label>
      <select
        id="task-filter-phase"
        class="ui-input w-auto"
        value={filters.phase}
        onchange={(event) => setFilter("phase", event.currentTarget.value)}
      >
        <option value="">All statuses</option>
        {#each PHASES as phase}<option value={phase}>{label(phase)}</option
          >{/each}
      </select>
      <label class="ui-label mb-0" for="task-filter-freshness">Freshness</label>
      <select
        id="task-filter-freshness"
        class="ui-input w-auto"
        value={filters.freshness}
        onchange={(event) => setFilter("freshness", event.currentTarget.value)}
      >
        <option value="">Any freshness</option>
        <option value="fresh">Checked recently</option>
        <option value="stale">Not checked lately</option>
        <option value="error">Can't reach source</option>
        <option value="unknown">Never checked</option>
      </select>
      <form
        class="flex flex-wrap items-end gap-2"
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
      {#if activeFilters}
        <a class="ui-prose-link" href={workspaceHref(`/tasks?view=${view}`)}
          >Clear filters</a
        >
      {/if}
    </div>
  </details>

  {#if moveError}
    <StateError message={moveError} />
  {/if}

  {#if error}
    <StateError
      title="Tasks could not be refreshed"
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
      Loading tasks…
    </p>
  {:else if !error && !records.length}
    <section class="py-14 text-center">
      <h2 class="text-subtitle font-semibold text-fg">
        {activeFilters ? "No matching tasks" : "Nothing tracked yet"}
      </h2>
      <p class="mx-auto mt-2 max-w-md text-meta text-fg-muted">
        {activeFilters
          ? "Change or clear the filters."
          : "Create task here, or connect a source so its issues show up."}
      </p>
      <a
        class="ui-prose-link mt-4 inline-block text-meta"
        href={activeFilters
          ? workspaceHref("/tasks")
          : workspaceHref("/tasks/new")}
        >{activeFilters ? "Clear filters" : "Create task"}</a
      >
    </section>
  {:else if records.length}
    <WorkViews
      {records}
      {view}
      {workspaceHref}
      {now}
      {requested}
      {requestedDecisions}
      {boardTitles}
      onMove={moveTask}
    />
  {/if}

  {#if nextCursor}
    <button
      class="ui-btn-secondary mx-auto"
      onclick={() => load(true)}
      disabled={loading}>{loading ? "Loading…" : "Load more"}</button
    >
  {/if}
  {#if shortcutsOpen}
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <div
      bind:this={shortcutsDialog}
      class="fixed inset-0 z-50 flex items-start justify-center bg-black/60 px-4 pt-[12vh] outline-none"
      role="dialog"
      aria-modal="true"
      aria-label="Keyboard shortcuts"
      tabindex="-1"
      onclick={(event) => {
        if (event.target === event.currentTarget) shortcutsOpen = false;
      }}
    >
      <div class="w-full max-w-xs rounded-md border border-line bg-panel p-4">
        <h2
          class="text-micro font-semibold uppercase tracking-wide text-fg-muted"
        >
          Keyboard shortcuts
        </h2>
        <dl class="mt-3 space-y-1.5 text-meta">
          {#each SHORTCUTS as [action, keys] (action)}
            <div class="flex items-baseline justify-between gap-6">
              <dt class="text-fg-muted">{action}</dt>
              <dd class="flex gap-1">
                {#each keys as key (key)}
                  <kbd
                    class="rounded border border-line bg-accent-soft px-1.5 py-0.5 font-mono text-micro text-accent-text"
                    >{key}</kbd
                  >
                {/each}
              </dd>
            </div>
          {/each}
        </dl>
      </div>
    </div>
  {/if}
</WorkspacePageShell>
