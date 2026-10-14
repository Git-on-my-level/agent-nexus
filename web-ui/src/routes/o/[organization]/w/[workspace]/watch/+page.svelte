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
  import { ageSeconds, formatSpan, freshnessLabel } from "$lib/brief.js";
  import { mergeDashboardReports } from "$lib/overview.js";
  import { isNexusOwned, workKey } from "$lib/pm/presentation.js";
  import {
    mutationOutbox,
    mutationConfirmations,
  } from "$lib/mutationOutbox.js";
  import {
    enqueueTaskMove,
    taskMoveDraft,
    taskMoveTarget,
    isTaskMove,
  } from "$lib/taskMoveWork.js";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import SkeletonList from "$lib/components/state/SkeletonList.svelte";
  import Banner from "$lib/components/Banner.svelte";
  import Button from "$lib/components/Button.svelte";
  import DashboardPicker from "$lib/components/overview/DashboardPicker.svelte";
  import VisualReport from "$lib/components/reports/VisualReport.svelte";
  import WatchList from "$lib/components/watch/WatchList.svelte";
  import WatchBoard from "$lib/components/watch/WatchBoard.svelte";

  const store = briefStore();
  let brief = $derived($store.data);
  let failed = $derived(Boolean($store.error));
  let refreshing = $derived($store.refreshing);
  let now = $state(Date.now());

  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let view = $derived(
    $page.url.searchParams.get("view") === "board" ? "board" : "list",
  );
  function setView(next) {
    const params = patchSearchParams($page.url.searchParams, {
      view: next === "board" ? "board" : "",
    });
    const query = params.toString();
    void goto(`${$page.url.pathname}${query ? `?${query}` : ""}`, {
      keepFocus: true,
      noScroll: true,
      replaceState: true,
    });
  }

  let staleAge = $derived.by(() => {
    const age = ageSeconds(
      $store.updatedAt ? new Date($store.updatedAt).toISOString() : "",
      now,
    );
    if (age === null) return "a while ago";
    return formatSpan(age) === "now" ? "just now" : `${formatSpan(age)} ago`;
  });

  // Optimistic placements, dropped as soon as a fresh brief lands.
  let placed = $state({});
  $effect(() => {
    void brief;
    placed = {};
  });
  let initiatives = $derived(
    (brief?.initiatives ?? []).map((row) =>
      placed[row.ref] ? { ...row, placement: placed[row.ref] } : row,
    ),
  );

  let notice = $state(null);
  let moveError = $state("");

  /** A confirmed move changes what core computes; read it again. */
  let confirmed = $derived(
    Object.values($mutationConfirmations).filter(isTaskMove).length,
  );
  let seenConfirmed = -1;
  $effect(() => {
    const count = confirmed;
    if (seenConfirmed >= 0 && count !== seenConfirmed) void refreshBrief();
    seenConfirmed = count;
  });

  async function moveInitiative(row, placement, beforeRef = "") {
    moveError = "";
    notice = null;
    if (placement === row.placement) return;
    if (placement === "done") {
      notice = {
        text: `Marking “${row.title}” done needs evidence.`,
        href: workspaceHref(`/initiatives/${encodeURIComponent(row.id)}`),
        link: "Open the initiative to add it",
      };
      return;
    }
    let work;
    let boardToken = "";
    try {
      work = (await coreClient.getWork(row.ref, { summary: 1 })).work;
      if (isNexusOwned(work)) {
        const board = await coreClient.getBoard(
          work.board_ref || work.board_id,
        );
        boardToken = (board?.board || board)?.updated_at || "";
      }
    } catch {
      moveError = "Couldn't read that initiative. Nothing was moved.";
      return;
    }
    if (
      get(mutationOutbox).some((entry) => entry.target === taskMoveTarget(work))
    )
      return;
    if (
      !isNexusOwned(work) &&
      !window.confirm(
        `Ask the PM to request moving “${row.title}”? You approve the request in Decide.`,
      )
    )
      return;
    const draft = taskMoveDraft(work, placement, {
      key: workKey(work),
      ref: "",
      boardToken,
      beforeCardId: beforeRef,
      approved: true,
    });
    placed = { ...placed, [row.ref]: placement };
    try {
      await enqueueTaskMove(draft, {
        draftTarget: "tasks:move",
        href: workspaceHref("/watch?view=board"),
      });
      // The confirmation effect above re-reads the brief once core accepts it.
      notice = { text: "Saved. Waiting for confirmation." };
    } catch {
      const rest = { ...placed };
      delete rest[row.ref];
      placed = rest;
      moveError = "Couldn't save that move. Try again.";
    }
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
          class="ml-1 inline-block h-3 w-3 animate-spin rounded-full border-2 border-border border-t-fg-muted align-middle"
          role="status"
          aria-label="Updating"
        ></span>
      {/if}
    {/snippet}
    {#snippet actions()}
      <div
        class="inline-flex rounded-md border border-border"
        role="group"
        aria-label="View"
      >
        {#each [["list", "List"], ["board", "By stage"]] as [key, text] (key)}
          <button
            type="button"
            class="px-3 py-1 text-meta {view === key
              ? 'bg-panel-hover text-fg'
              : 'text-fg-muted'}"
            aria-pressed={view === key}
            onclick={() => setView(key)}>{text}</button
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
    <p class="text-meta text-danger-text" role="alert">{moveError}</p>
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
    <p class="text-meta text-fg-muted" data-watch-empty>No initiatives yet.</p>
  {:else if view === "board"}
    <WatchBoard {initiatives} {now} onMove={moveInitiative} />
  {:else}
    <WatchList {initiatives} {now} />
  {/if}

  {#if selectedReport}
    <section
      class="rounded-md border border-border bg-panel"
      data-watch-dashboard
    >
      <header
        class="flex flex-wrap items-center justify-between gap-2 border-b border-border px-3 py-2"
      >
        <h2 class="text-subtitle text-fg">Dashboard</h2>
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
      {#if pinError}<p
          class="px-3 py-2 text-meta text-danger-text"
          role="alert"
        >
          {pinError}
        </p>{/if}
      <div class="px-1 py-2">
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
