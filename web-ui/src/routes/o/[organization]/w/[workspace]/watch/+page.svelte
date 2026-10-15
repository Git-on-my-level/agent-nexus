<script>
  import { onMount } from "svelte";
  import { get } from "svelte/store";
  import { page } from "$app/stores";
  import { goto } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { usePageTitle } from "$lib/pageTitle.svelte.js";
  import { patchSearchParams } from "$lib/urlState.js";
  import { briefStore, mountBrief, refreshBrief } from "$lib/briefStore.js";
  import { freshnessLabel, isLoop } from "$lib/brief.js";
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
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import SkeletonList from "$lib/components/state/SkeletonList.svelte";
  import Banner from "$lib/components/Banner.svelte";
  import Button from "$lib/components/Button.svelte";
  import InlineAlert from "$lib/components/InlineAlert.svelte";
  import DashboardPicker from "$lib/components/overview/DashboardPicker.svelte";
  import VisualReport from "$lib/components/reports/VisualReport.svelte";
  import WatchList from "$lib/components/watch/WatchList.svelte";
  import WatchBoard from "$lib/components/watch/WatchBoard.svelte";
  import WatchLoops from "$lib/components/watch/WatchLoops.svelte";

  const store = briefStore();
  let brief = $derived($store.data);
  let failed = $derived(Boolean($store.error));
  let refreshing = $derived($store.refreshing);
  let now = $state(Date.now());

  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  const VIEWS = ["list", "board", "loops"];
  let view = $derived(
    VIEWS.includes($page.url.searchParams.get("view") ?? "")
      ? $page.url.searchParams.get("view")
      : "list",
  );
  function setView(next) {
    const params = patchSearchParams($page.url.searchParams, {
      view: next === "list" ? "" : next,
    });
    const query = params.toString();
    void goto(`${$page.url.pathname}${query ? `?${query}` : ""}`, {
      keepFocus: true,
      noScroll: true,
      replaceState: true,
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
  let loopCount = $derived(initiatives.filter(isLoop).length);
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
        href: workspaceHref("/watch?view=board"),
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

  // Dashboards, kept from the old Overview below the initiatives.
  let dashboards = $state(null);
  let loadingMore = $state(false);
  let pinning = $state(false);
  let pinError = $state("");
  let reports = $derived(dashboards?.reports ?? []);
  let wantedDashboard = $derived($page.url.searchParams.get("dashboard") || "");
  let selectedReport = $derived(
    reports.find(
      (entry) =>
        entry.id === wantedDashboard ||
        entry.ref === wantedDashboard ||
        entry.requested_as === wantedDashboard,
    ) ||
      reports[0] ||
      null,
  );
  async function loadDashboards(cursor) {
    try {
      const more = await coreClient.getDashboardReports(
        cursor ? { cursor } : {},
      );
      dashboards = mergeDashboardReports(cursor ? dashboards : {}, more);
    } catch {
      // The dashboard is optional furniture; the initiatives stay usable.
    }
  }
  async function loadMoreReports() {
    if (!dashboards?.has_more || loadingMore) return;
    loadingMore = true;
    await loadDashboards(dashboards.next_cursor);
    loadingMore = false;
  }
  function selectReport(id) {
    const params = patchSearchParams($page.url.searchParams, {
      dashboard: !id || id === reports[0]?.id ? "" : id,
    });
    const query = params.toString();
    void goto(`${$page.url.pathname}${query ? `?${query}` : ""}`, {
      keepFocus: true,
      noScroll: true,
      replaceState: true,
    });
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

<WorkspacePageShell>
  <WorkspacePageHeader title="Watch">
    {#snippet subtitle()}
      {#if brief}
        <span data-source="computed:generated_at"
          >{freshnessLabel(brief.asOf, now)}</span
        >
      {/if}
      {#if refreshing}
        <span
          class="ml-1 inline-block h-3 w-3 animate-spin rounded-full border-2 border-line border-t-fg-muted align-middle"
          role="status"
          aria-label="Updating"
        ></span>
      {/if}
    {/snippet}
    {#snippet actions()}
      <div
        class="inline-flex rounded-md border border-line bg-bg-soft p-0.5"
        role="group"
        aria-label="View"
        data-watch-view-toggle
      >
        {#each [["list", "List"], ["board", "By stage"], ["loops", "Loops"]] as [key, text] (key)}
          <button
            type="button"
            class="rounded px-3 py-1 text-meta font-medium transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent {view ===
            key
              ? 'bg-panel-hover text-fg shadow-sm'
              : 'text-fg-muted hover:text-fg'}"
            aria-pressed={view === key}
            data-watch-view={key}
            onclick={() => setView(key)}
            >{text}{#if key === "loops" && loopCount}<span
                class="ml-1 tabular-nums text-fg-subtle">{loopCount}</span
              >{/if}</button
          >
        {/each}
      </div>
    {/snippet}
  </WorkspacePageHeader>

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

  {#if !brief && failed}
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
    <SkeletonList rows={4} />
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
  {:else if view === "loops"}
    <WatchLoops {initiatives} {now} />
  {:else if view === "board"}
    <WatchBoard {initiatives} {now} {held} onMove={moveInitiative} />
  {:else}
    <WatchList {initiatives} {now} />
  {/if}

  {#if selectedReport}
    <section class="min-w-0 space-y-2 pt-2" data-watch-dashboard>
      <header
        class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1"
      >
        <h2 class="ui-label mb-0">Dashboard</h2>
        <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
          <DashboardPicker
            {reports}
            selected={selectedReport}
            pinnedRef={dashboards?.pinned_ref ?? ""}
            hasMore={Boolean(dashboards?.has_more)}
            loading={loadingMore}
            onload={() => void loadMoreReports()}
            onselect={selectReport}
            onpin={(ref) => void pinDashboard(ref)}
          />
          <a
            class="text-meta text-accent-text hover:underline"
            href={workspaceHref(
              `/docs/${encodeURIComponent(selectedReport.segment)}`,
            )}>Open document</a
          >
          {#if pinning}<span class="text-micro text-fg-muted" role="status"
              >Pinning dashboard…</span
            >{/if}
        </div>
      </header>
      {#if pinError}<p class="text-meta text-danger-text" role="alert">
          {pinError}
        </p>{/if}
      <div class="min-w-0">
        <VisualReport
          compact
          report={selectedReport.report}
          documentTitle={selectedReport.title || ""}
          documentId={selectedReport.id}
          revisionRef={selectedReport.revision_ref ?? ""}
        />
      </div>
    </section>
  {/if}
</WorkspacePageShell>
