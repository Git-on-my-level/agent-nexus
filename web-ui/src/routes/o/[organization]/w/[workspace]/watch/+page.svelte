<script>
  /**
   * Watch: where things stand, at a glance. Read-only — every action lives in
   * Decide, and the header links there when something is waiting.
   *
   * Tabs, left to right: Initiatives (where Watch lands), Loops, then the
   * pinned dashboard and any dashboard opened from "More dashboards". The
   * first two are built from the brief; dashboards are report documents.
   * Initiatives and Loops share one row layout (`CollectionTable`), so every
   * tab reads the same way.
   */
  import { onMount } from "svelte";
  import { get } from "svelte/store";
  import { page } from "$app/stores";
  import { goto } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { usePageTitle } from "$lib/pageTitle.svelte.js";
  import { patchSearchParams } from "$lib/urlState.js";
  import { briefStore, mountBrief, refreshBrief } from "$lib/briefStore.js";
  import {
    countWaitingDecisions,
    freshnessLabel,
    initiativeLine,
  } from "$lib/brief.js";
  import {
    attentionCount,
    collectionFromInitiatives,
    collectionFromLoops,
  } from "$lib/collection.js";
  import { formatAgo } from "$lib/time/format.js";
  import { mergeDashboardReports } from "$lib/overview.js";
  import { isNexusOwned } from "$lib/pm/presentation.js";
  import {
    mutationOutbox,
    mutationConfirmations,
    supersedeLocalMutation,
  } from "$lib/mutationOutbox.js";
  import {
    enqueueTaskMove,
    taskMoveError,
    isTaskMove,
  } from "$lib/taskMoveWork.js";
  import {
    createWatchMover,
    displayedPlacement,
    watchMoveAttention,
  } from "$lib/watchMove.js";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import SkeletonList from "$lib/components/state/SkeletonList.svelte";
  import Banner from "$lib/components/Banner.svelte";
  import Button from "$lib/components/Button.svelte";
  import InlineAlert from "$lib/components/InlineAlert.svelte";
  import DashboardPicker from "$lib/components/overview/DashboardPicker.svelte";
  import VisualReport from "$lib/components/reports/VisualReport.svelte";
  import WatchBoard from "$lib/components/watch/WatchBoard.svelte";
  import LoopCard from "$lib/components/loops/LoopCard.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import CollectionTable from "$lib/components/collection/CollectionTable.svelte";

  const store = briefStore();
  let brief = $derived($store.data);
  let failed = $derived(Boolean($store.error));
  let refreshing = $derived($store.refreshing);
  let now = $state(Date.now());

  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  const initiativeHref = (row) =>
    workspaceHref(`/initiatives/${encodeURIComponent(row.id)}`);
  const decideHref = (row) =>
    workspaceHref(`/decide?initiative=${encodeURIComponent(row.ref)}`);

  // ?tab=initiatives|loops|dashboard; the old ?view= links still land.
  let params = $derived($page.url.searchParams);
  let tab = $derived.by(() => {
    const wanted = params.get("tab") || "";
    if (["initiatives", "loops", "dashboard"].includes(wanted)) return wanted;
    if (params.get("view") === "loops") return "loops";
    if (params.get("dashboard")) return "dashboard";
    return "initiatives";
  });
  let layout = $derived(
    params.get("layout") === "board" || params.get("view") === "board"
      ? "board"
      : "list",
  );
  function navigate(patch) {
    const next = patchSearchParams(params, { view: "", ...patch });
    const query = next.toString();
    void goto(`${$page.url.pathname}${query ? `?${query}` : ""}`, {
      keepFocus: true,
      noScroll: true,
      replaceState: true,
    });
  }
  function selectTab(next, dashboard = "") {
    navigate({
      tab: next === "initiatives" ? "" : next,
      dashboard: next === "dashboard" ? dashboard : "",
      layout: "",
    });
  }

  let staleAge = $derived(formatAgo($store.updatedAt, now) || "earlier");

  // Local column until the outbox entry is visible. A brief refresh does not
  // clear it; confirmation and a rejected move do, via displayedPlacement.
  let localPlacement = $state({});
  let initiatives = $derived(
    (brief?.initiatives ?? []).map((row) => {
      const placement = displayedPlacement(
        row,
        $mutationOutbox,
        $mutationConfirmations,
        localPlacement[row.ref] || "",
      );
      return placement ? { ...row, placement } : row;
    }),
  );
  let initiativeCollection = $derived(
    collectionFromInitiatives(initiatives, {
      now,
      href: initiativeHref,
      decideHref,
    }),
  );
  let loopCollection = $derived(
    collectionFromLoops(initiatives, { now, href: initiativeHref, decideHref }),
  );
  let loopRows = $derived(
    loopCollection.groups.reduce((n, g) => n + g.rows.length, 0),
  );
  let initiativeAttention = $derived(attentionCount(initiativeCollection));
  let loopAttention = $derived(attentionCount(loopCollection));
  let waitingDecisions = $derived(countWaitingDecisions(brief?.decisions));

  $effect(() => {
    const rows = brief?.initiatives ?? [];
    const outbox = $mutationOutbox;
    const confirmations = $mutationConfirmations;
    let changed = false;
    const next = { ...localPlacement };
    for (const ref of Object.keys(next)) {
      const row = rows.find((item) => item.ref === ref);
      if (brief && !row) {
        delete next[ref];
        changed = true;
        continue;
      }
      if (!row) continue;
      if (
        displayedPlacement(row, outbox, confirmations, "") ||
        watchMoveAttention(outbox, [ref]) ||
        row.placement === next[ref]
      ) {
        delete next[ref];
        changed = true;
      }
    }
    if (changed) localPlacement = next;
  });

  let notice = $state(null);
  let localMoveError = $state("");
  let attentionError = $derived(
    watchMoveAttention(
      $mutationOutbox,
      (brief?.initiatives ?? []).map((row) => row.ref),
    ),
  );
  let moveError = $derived(localMoveError || attentionError);
  let held = $derived(
    (brief?.initiatives ?? [])
      .filter((row) =>
        displayedPlacement(
          row,
          $mutationOutbox,
          $mutationConfirmations,
          localPlacement[row.ref] || "",
        ),
      )
      .map((row) => row.ref),
  );

  const mover = createWatchMover({
    async readWork(row) {
      const work = (await coreClient.getWork(row.ref, { summary: 1 })).work;
      let boardToken = "";
      if (isNexusOwned(work)) {
        const board = await coreClient.getBoard(
          work.board_ref || work.board_id,
        );
        boardToken = (board?.board || board)?.updated_at || "";
      }
      return { work, boardToken };
    },
    enqueue(draft) {
      return enqueueTaskMove(draft, {
        draftTarget: "tasks:move",
        href: workspaceHref("/watch?layout=board"),
      });
    },
    entries: () => get(mutationOutbox),
    supersede: (key, replacementKey) =>
      supersedeLocalMutation(key, { replacementKey }),
    confirmExternal(work) {
      return window.confirm(
        `Ask the PM to request moving “${work.title}”? You approve the request in Decide.`,
      );
    },
  });

  /** A confirmed move changes what core computes; read it again. */
  let confirmed = $derived(
    Object.values($mutationConfirmations).filter(isTaskMove).length,
  );
  // Effect bookkeeping, deliberately not reactive.
  const seen = { confirmed: -1 };
  $effect(() => {
    const count = confirmed;
    if (seen.confirmed >= 0 && count !== seen.confirmed) void refreshBrief();
    seen.confirmed = count;
  });

  async function moveInitiative(row, placement) {
    localMoveError = "";
    notice = null;
    const result = await mover.move(row, placement);
    if (result.status === "needs-evidence") {
      notice = {
        text: `Marking “${row.title}” done needs evidence.`,
        href: workspaceHref(`/initiatives/${encodeURIComponent(row.id)}`),
        link: "Open the initiative to add it",
      };
      return false;
    }
    if (result.status === "unreadable") {
      localMoveError = "Couldn't read that initiative. Nothing was moved.";
      return false;
    }
    if (result.status === "busy") {
      localMoveError = result.message;
      return false;
    }
    if (result.status === "failed") {
      localMoveError = taskMoveError(result.error);
      return false;
    }
    if (result.status !== "queued") return false;
    localPlacement = { ...localPlacement, [row.ref]: result.placement };
    // The confirmation effect above re-reads the brief once core accepts it.
    notice = { text: "Saved. Waiting for confirmation." };
    return true;
  }

  // Dashboards: the pinned one is a tab; any other opens as a tab from the
  // "More dashboards" menu.
  let dashboards = $state(null);
  let loadingMore = $state(false);
  let pinning = $state(false);
  let pinError = $state("");
  let reports = $derived(dashboards?.reports ?? []);
  let pinnedRef = $derived(dashboards?.pinned_ref ?? "");
  let pinnedReport = $derived(
    reports.find((entry) => pinnedRef && entry.ref === pinnedRef) ?? null,
  );
  let wantedDashboard = $derived(params.get("dashboard") || "");
  let selectedReport = $derived(
    reports.find(
      (entry) =>
        entry.id === wantedDashboard ||
        entry.ref === wantedDashboard ||
        entry.requested_as === wantedDashboard,
    ) ||
      pinnedReport ||
      reports[0] ||
      null,
  );
  /** A dashboard opened from the menu that is not the pinned one. */
  let extraReport = $derived(
    tab === "dashboard" && selectedReport && selectedReport !== pinnedReport
      ? selectedReport
      : null,
  );
  async function loadDashboards(cursor) {
    try {
      const more = await coreClient.getDashboardReports(
        cursor ? { cursor } : {},
      );
      dashboards = mergeDashboardReports(cursor ? dashboards : {}, more);
    } catch {
      // Dashboards are optional; initiatives and loops stay usable.
    }
  }
  async function loadMoreReports() {
    if (!dashboards?.has_more || loadingMore) return;
    loadingMore = true;
    await loadDashboards(dashboards.next_cursor);
    loadingMore = false;
  }
  async function pinDashboard(ref) {
    pinning = true;
    pinError = "";
    try {
      await coreClient.setWorkspaceDashboard({ document_ref: ref });
      await loadDashboards();
    } catch {
      pinError = "Couldn't pin that dashboard. Try again.";
    } finally {
      pinning = false;
    }
  }

  let tabs = $derived(
    [
      {
        key: "initiatives",
        label: "Initiatives",
        badge: initiativeAttention,
        active: tab === "initiatives",
        select: () => selectTab("initiatives"),
      },
      loopRows || tab === "loops"
        ? {
            key: "loops",
            label: "Loops",
            badge: loopAttention,
            active: tab === "loops",
            select: () => selectTab("loops"),
          }
        : null,
      pinnedReport
        ? {
            key: `dashboard:${pinnedReport.id}`,
            label: pinnedReport.title || "Dashboard",
            badge: 0,
            pinned: true,
            active: tab === "dashboard" && selectedReport === pinnedReport,
            select: () => selectTab("dashboard", pinnedReport.id),
          }
        : null,
      extraReport
        ? {
            key: `dashboard:${extraReport.id}`,
            label: extraReport.title || "Dashboard",
            badge: 0,
            active: true,
            select: () => selectTab("dashboard", extraReport.id),
          }
        : null,
    ].filter(Boolean),
  );

  onMount(() => {
    const stop = mountBrief();
    void loadDashboards();
    const clock = setInterval(() => (now = Date.now()), 30_000);
    return () => {
      clearInterval(clock);
      stop();
    };
  });

  usePageTitle(() => "Watch · Agent Nexus");
</script>

{#snippet initiativeDetails(row)}
  {@const initiative = row.source.initiative}
  {@const line = initiativeLine(initiative, now)}
  <div class="space-y-2 pt-1 text-meta">
    {#if line.length}
      <p class="flex flex-wrap gap-x-1.5 text-micro text-fg-muted">
        {#each line as part, i (part.field)}
          {#if i > 0}<span aria-hidden="true" class="text-fg-subtle">·</span
            >{/if}<span data-source="computed:{part.field}">{part.text}</span>
        {/each}
      </p>
    {/if}
    {#if initiative.note}
      <blockquote
        class="border-l-2 border-line-strong pl-2 text-micro text-fg [overflow-wrap:anywhere]"
      >
        <MarkdownRenderer
          inline
          plain
          source={initiative.note.text}
          organizationSlug={$page.params.organization}
          workspaceSlug={$page.params.workspace}
        />
        <span class="text-fg-muted" data-author-chip
          >— {initiative.note.author?.display || "Unknown"}</span
        >
      </blockquote>
    {/if}
    <a class="text-micro text-accent-text hover:underline" href={row.href}
      >Open initiative</a
    >
  </div>
{/snippet}

{#snippet loopDetails(row)}
  {@const initiative = row.source.initiative}
  {#if initiative.loop}
    <LoopCard
      ref={initiative.ref}
      title={initiative.title}
      href={row.href}
      loop={initiative.loop}
      {now}
    />
  {:else}
    {@render initiativeDetails(row)}
  {/if}
{/snippet}

<WorkspacePageShell>
  <header class="space-y-3" data-watch-header>
    <div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
      <div class="flex min-w-0 items-baseline gap-2">
        <h1 class="text-title font-semibold text-fg">Watch</h1>
        {#if brief}
          <span
            class="text-micro text-fg-muted"
            data-source="computed:generated_at"
            >{freshnessLabel(brief.asOf, now)}</span
          >
        {/if}
        {#if refreshing}
          <span
            class="inline-block h-3 w-3 animate-spin rounded-full border-2 border-line border-t-fg-muted align-middle"
            role="status"
            aria-label="Updating"
          ></span>
        {/if}
      </div>
      {#if waitingDecisions > 0}
        <a
          class="decide-link"
          href={workspaceHref("/decide")}
          data-watch-decide-link
          >{waitingDecisions} waiting for you in Decide
          <span aria-hidden="true">→</span></a
        >
      {/if}
    </div>
    <div class="flex items-end gap-2 border-b border-line">
      <div
        class="tabs flex min-w-0 flex-1 gap-1 overflow-x-auto"
        role="tablist"
        aria-label="Watch views"
      >
        {#each tabs as t (t.key)}
          <button
            type="button"
            role="tab"
            class="tab"
            class:tab--on={t.active}
            aria-selected={t.active}
            onclick={t.select}
            data-watch-tab={t.key}
          >
            {#if t.pinned}<svg
                class="h-3 w-3 text-fg-subtle"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                aria-label="Pinned"
                ><path d="M12 17v5" /><path d="M9 3h6l-1 6 4 4H6l4-4z" /></svg
              >{/if}
            <span class="truncate">{t.label}</span>
            {#if t.badge > 0}<span
                class="tab-badge"
                aria-label="{t.badge} need you">{t.badge}</span
              >{/if}
          </button>
        {/each}
      </div>
      {#if reports.length}
        <div class="shrink-0 pb-1.5" data-watch-more-dashboards>
          <DashboardPicker
            {reports}
            selected={tab === "dashboard" ? selectedReport : null}
            {pinnedRef}
            triggerLabel="More dashboards"
            hasMore={Boolean(dashboards?.has_more)}
            loading={loadingMore}
            onload={() => void loadMoreReports()}
            onselect={(id) => selectTab("dashboard", id || reports[0]?.id)}
            onpin={(ref) => void pinDashboard(ref)}
          />
        </div>
      {/if}
    </div>
  </header>

  {#if failed && brief}
    <Banner>
      Couldn't refresh. Showing data from {staleAge}.
      {#snippet actions()}
        <Button
          variant="secondary"
          size="compact"
          onclick={() => refreshBrief()}>Retry</Button
        >
      {/snippet}
    </Banner>
  {/if}
  {#if notice}
    <p class="text-meta text-fg-muted" role="status" data-watch-notice>
      {notice.text}
      {#if notice.href}<a
          class="text-accent-text hover:underline"
          href={notice.href}>{notice.link}</a
        >{/if}
    </p>
  {/if}
  {#if moveError}
    <InlineAlert>{moveError}</InlineAlert>
  {/if}
  {#if pinning}<p class="text-micro text-fg-muted" role="status">
      Pinning dashboard…
    </p>{/if}
  {#if pinError}<p class="text-meta text-danger-text" role="alert">
      {pinError}
    </p>{/if}

  {#if tab === "dashboard"}
    {#if selectedReport}
      <section class="min-w-0 space-y-2" data-watch-dashboard>
        <div class="flex items-center justify-end gap-3">
          {#if selectedReport.ref && selectedReport.ref !== pinnedRef}
            <Button
              variant="secondary"
              size="compact"
              disabled={pinning}
              onclick={() => void pinDashboard(selectedReport.ref)}
              data-watch-pin>Pin as tab</Button
            >
          {/if}
          <a
            class="text-meta text-accent-text hover:underline"
            href={workspaceHref(
              `/docs/${encodeURIComponent(selectedReport.segment)}`,
            )}>Open document</a
          >
        </div>
        <VisualReport
          compact
          report={selectedReport.report}
          documentTitle={selectedReport.title || ""}
          documentId={selectedReport.id}
          revisionRef={selectedReport.revision_ref ?? ""}
        />
      </section>
    {:else if dashboards}
      <p class="text-meta text-fg-muted">That dashboard isn't available.</p>
    {:else}
      <SkeletonList rows={4} />
    {/if}
  {:else if !brief && failed}
    <Banner>
      Couldn't load Watch. Your work is safe; try again in a moment.
      {#snippet actions()}
        <Button
          variant="secondary"
          size="compact"
          onclick={() => refreshBrief()}>Retry</Button
        >
      {/snippet}
    </Banner>
  {:else if !brief}
    <SkeletonList rows={6} />
  {:else if tab === "loops"}
    {#if !loopRows}
      <div
        class="rounded-md border border-dashed border-line px-4 py-8 text-center"
        data-watch-loops-empty
      >
        <p class="text-meta text-fg">No loops yet.</p>
        <p class="mt-1 text-micro text-fg-muted">
          A loop is an initiative that runs on a cadence. Record each run with
          <code>anx work receipt</code> and it shows up here.
        </p>
      </div>
    {:else}
      <CollectionTable
        collection={loopCollection}
        view={layout}
        folded={["done"]}
        label="loops"
        details={loopDetails}
        actions={loopActions}
      />
    {/if}
  {:else if !initiatives.length}
    <div
      class="rounded-md border border-dashed border-line px-4 py-8 text-center"
      data-watch-empty
    >
      <p class="text-meta text-fg">No initiatives yet.</p>
      <p class="mt-1 text-micro text-fg-muted">
        Initiatives appear here once an agent or a person files one.
        <a
          class="text-accent-text hover:underline"
          href={workspaceHref("/initiatives/new")}>Create one</a
        >
      </p>
    </div>
  {:else if layout === "board"}
    <div class="flex justify-end">{@render initiativeActions()}</div>
    <WatchBoard {initiatives} {now} {held} onMove={moveInitiative} />
  {:else}
    <CollectionTable
      collection={initiativeCollection}
      folded={["done", "no_promise"]}
      label="initiatives"
      details={initiativeDetails}
      actions={initiativeActions}
    />
  {/if}
</WorkspacePageShell>

{#snippet initiativeActions()}
  {@render layoutToggle([
    ["list", "List"],
    ["board", "By stage"],
  ])}
{/snippet}

{#snippet loopActions()}
  {@render layoutToggle([
    ["list", "List"],
    ["board", "Board"],
  ])}
{/snippet}

{#snippet layoutToggle(options)}
  <div
    class="inline-flex rounded-md border border-line bg-bg-soft p-0.5"
    role="group"
    aria-label="Layout"
    data-watch-view-toggle
  >
    {#each options as [key, text] (key)}
      <button
        type="button"
        class="rounded px-3 py-1 text-meta font-medium transition-colors max-md:min-h-10 focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent {layout ===
        key
          ? 'bg-panel-hover text-fg shadow-sm'
          : 'text-fg-muted hover:text-fg'}"
        aria-pressed={layout === key}
        data-watch-view={key}
        onclick={() => navigate({ layout: key === "list" ? "" : key })}
        >{text}</button
      >
    {/each}
  </div>
{/snippet}

<style>
  .tab {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    max-width: 16rem;
    min-height: 2.75rem;
    padding: 0.5rem 0.75rem;
    margin-bottom: -1px;
    border-bottom: 2px solid transparent;
    color: var(--fg-muted);
    font-size: 14px;
    font-weight: 500;
    white-space: nowrap;
  }
  .tab:hover {
    color: var(--fg);
  }
  .tab--on {
    border-bottom-color: var(--accent-solid);
    color: var(--fg);
    font-weight: 600;
  }
  .tab:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: -2px;
  }
  .tab-badge {
    border-radius: 999px;
    padding: 0 6px;
    background: var(--warn-text);
    color: var(--bg);
    font-size: 11px;
    font-weight: 700;
    line-height: 16px;
  }
  .decide-link {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    min-height: 2.25rem;
    padding: 0.35rem 0.75rem;
    border: 1px solid rgba(245, 158, 11, 0.35);
    border-radius: 8px;
    background: rgba(245, 158, 11, 0.08);
    color: var(--warn-text);
    font-size: 13px;
  }
  .decide-link:hover {
    background: rgba(245, 158, 11, 0.14);
  }
</style>
