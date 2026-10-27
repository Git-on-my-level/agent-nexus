<script>
  /**
   * Watch: where things stand, at a glance. Read-only — every action lives in
   * Decide, and the header links there when something is waiting.
   *
   * Tabs, left to right: Initiatives (where Watch lands), Loops, Trends,
   * then the reader's pinned dashboards and any dashboard opened from "More
   * dashboards". The first two are built from the brief; Trends reads
   * `watch_trends.v1` (change over time, which no other view shows);
   * dashboards are report documents.
   * Initiatives and Loops share one row layout (`CollectionTable`), so every
   * tab reads the same way. Initiatives also lay out as the swimlane board
   * (`?view=lanes`), with the Ungrouped tray and the budget chip.
   */
  import { onMount, tick, untrack } from "svelte";
  import { page } from "$app/stores";
  import { goto } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { usePageTitle } from "$lib/pageTitle.svelte.js";
  import { patchSearchParams } from "$lib/urlState.js";
  import { TREND_RANGES, trendRange } from "$lib/watchTrends.js";
  import { briefStore, mountBrief, refreshBrief } from "$lib/briefStore.js";
  import {
    countWaitingDecisions,
    freshnessLabel,
    briefLoops,
  } from "$lib/brief.js";
  import {
    attentionCount,
    collectionFromLoops,
    collectionFromDailyDigest,
  } from "$lib/collection.js";
  import {
    bucketCounts,
    overTargetAsk,
    rollupRows,
    rollupSummary,
  } from "$lib/watchRollup.js";
  import { formatAgo } from "$lib/time/format.js";
  import { mergeDashboardReports } from "$lib/overview.js";
  import {
    mutationOutbox,
    mutationConfirmations,
    enqueueDashboardPin,
    DASHBOARD_PINS_TARGET,
  } from "$lib/mutationOutbox.js";
  import {
    MAX_DASHBOARD_PINS,
    PINS_OPERATION,
    displayedPins,
    personalPins,
  } from "$lib/dashboardPins.js";
  import { layoutPatch, watchView } from "$lib/watchView.js";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import SkeletonList from "$lib/components/state/SkeletonList.svelte";
  import Banner from "$lib/components/Banner.svelte";
  import Button from "$lib/components/Button.svelte";
  import InlineAlert from "$lib/components/InlineAlert.svelte";
  import DashboardPicker from "$lib/components/overview/DashboardPicker.svelte";
  import VisualReport from "$lib/components/reports/VisualReport.svelte";
  import SwimlaneBoard from "$lib/components/work/SwimlaneBoard.svelte";
  import InitiativeDetails from "$lib/components/watch/InitiativeDetails.svelte";
  import InitiativeRollupList from "$lib/components/watch/InitiativeRollupList.svelte";
  import LoopCard from "$lib/components/loops/LoopCard.svelte";
  import CollectionTable from "$lib/components/collection/CollectionTable.svelte";
  import TrendsView from "$lib/components/watch/TrendsView.svelte";

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
  const childInitiativeHref = (ref) =>
    workspaceHref(
      `/initiatives/${encodeURIComponent(ref.replace(/^card:/, ""))}`,
    );
  const decideHref = (row) =>
    workspaceHref(`/decide?initiative=${encodeURIComponent(row.ref)}`);

  // ?tab= and ?view=; see watchView for the links that still land.
  let params = $derived($page.url.searchParams);
  let tab = $derived(watchView(params).tab);
  let layout = $derived(watchView(params).layout);
  let trendDays = $derived(trendRange(params));
  function navigate(patch) {
    const next = patchSearchParams(params, { layout: "", ...patch });
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
      view: "",
      range: next === "trends" ? params.get("range") || "" : "",
    });
  }

  let staleAge = $derived(formatAgo($store.updatedAt, now) || "earlier");

  let initiatives = $derived(brief?.initiatives ?? []);
  // Top-level initiatives with their children rolled in; loops and
  // children are not rows of their own.
  let rollup = $derived(rollupRows(initiatives));
  let rollupLine = $derived(
    rollupSummary(rollup, {
      truncated: Boolean(brief?.truncated?.initiatives),
    }),
  );
  let digestCollection = $derived(
    collectionFromDailyDigest(brief?.dailyDigest, { href: initiativeHref }),
  );
  let loopCollection = $derived(
    collectionFromLoops(briefLoops(brief, initiatives), {
      now,
      href: initiativeHref,
      decideHref,
    }),
  );
  let loopRows = $derived(
    loopCollection.groups.reduce((n, g) => n + g.rows.length, 0),
  );
  let initiativeAttention = $derived(bucketCounts(rollup).needs_you);
  let countAsk = $derived(overTargetAsk(brief?.overTarget, brief?.decisions));
  let loopAttention = $derived(attentionCount(loopCollection));
  let waitingDecisions = $derived(countWaitingDecisions(brief?.decisions));

  // Dashboards: each of the reader's pins is a tab; any other opens as a tab
  // from the "More dashboards" menu. Pins are per person; with none, core
  // answers with the workspace default (`source: "workspace_default"`). That
  // default is no longer a standing tab — it repeated Initiatives, Loops and
  // Decide, and Trends now holds the place — but `?tab=dashboard` still opens
  // it, as a tab that is not the reader's pin to unpin or count.
  let dashboards = $state(null);
  let dashboardsFailed = $state(false);
  let loadingMore = $state(false);
  let pinError = $state("");
  // Core's set as of `at`: the start of the read that returned it, or the
  // moment an edit's receipt confirmed. The newer one wins, so a read that
  // was in flight when an edit landed cannot roll the edit back.
  let serverPins = $state(null);
  let pinsReadFailed = $state(false);
  function offerPins(value, at) {
    if (!Array.isArray(value?.document_refs)) return;
    if (serverPins && serverPins.at > at) return;
    serverPins = {
      document_refs: value.document_refs,
      source: value.source,
      at,
    };
    pinsReadFailed = false;
  }
  // A receipt from this tab, or recovered from another tab's confirmation.
  let pinReceipt = $derived($mutationConfirmations[DASHBOARD_PINS_TARGET]);
  $effect(() => {
    const receipt = pinReceipt;
    if (receipt?.result)
      untrack(() => offerPins(receipt.result, receipt.confirmedAt ?? 0));
  });
  let pinAttention = $derived(
    $mutationOutbox.find(
      (row) => row.operation === PINS_OPERATION && row.state === "attention",
    ),
  );
  let reports = $derived(dashboards?.reports ?? []);
  /**
   * The reader's own pins: core's set with every saved edit still on its way
   * applied in order, so a reload or another tab's edit shows at once and no
   * read can hide a pending one.
   */
  let pinnedRefs = $derived(
    displayedPins(personalPins(serverPins), $mutationOutbox),
  );
  let defaultRefs = $derived(
    serverPins
      ? serverPins.source === "workspace_default"
        ? serverPins.document_refs
        : []
      : dashboards?.pinned_ref
        ? [dashboards.pinned_ref]
        : [],
  );
  let pinnedReports = $derived(
    pinnedRefs
      .map((ref) => reports.find((entry) => entry.ref === ref))
      .filter(Boolean),
  );
  let pinnedReport = $derived(pinnedReports[0] ?? null);
  let defaultReport = $derived(
    reports.find((entry) => defaultRefs.includes(entry.ref)) ?? null,
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
      defaultReport ||
      reports[0] ||
      null,
  );
  /** A dashboard opened from the menu that is not pinned. */
  let extraReport = $derived(
    tab === "dashboard" &&
      selectedReport &&
      !pinnedReports.includes(selectedReport)
      ? selectedReport
      : null,
  );
  async function loadDashboards(cursor) {
    if (!cursor) dashboardsFailed = false;
    try {
      const more = await coreClient.getDashboardReports(
        cursor ? { cursor } : {},
      );
      dashboards = mergeDashboardReports(cursor ? dashboards : {}, more);
    } catch {
      // Dashboards are optional; initiatives and loops stay usable. A failed
      // first read says so on the dashboard tab instead of loading forever.
      if (!cursor) dashboardsFailed = true;
    }
  }
  async function loadMoreReports() {
    if (!dashboards?.has_more || loadingMore) return;
    loadingMore = true;
    await loadDashboards(dashboards.next_cursor);
    loadingMore = false;
  }
  async function loadPins() {
    const at = Date.now();
    try {
      offerPins(await coreClient.getDashboardPins(), at);
    } catch {
      // The workspace default still shows as a tab. Editing stays safe: the
      // outbox applies each edit to a fresh read when it sends.
      if (!serverPins) pinsReadFailed = true;
    }
  }
  async function editPin(op, ref) {
    pinError = "";
    try {
      await enqueueDashboardPin({
        op,
        ref,
        href: workspaceHref("/watch?tab=dashboard"),
      });
    } catch {
      pinError = "Couldn't save your pin change. Try again.";
    }
  }
  function pinDashboard(ref) {
    if (!ref || pinnedRefs.includes(ref)) return;
    // Known to be full. Unread pins are checked when the edit is sent.
    if (serverPins && pinnedRefs.length >= MAX_DASHBOARD_PINS) {
      pinError = `You can pin up to ${MAX_DASHBOARD_PINS} dashboards. Unpin one first.`;
      return;
    }
    void editPin("add", ref);
  }
  function unpinDashboard(ref) {
    if (!ref || !pinnedRefs.includes(ref)) return;
    void editPin("remove", ref);
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
      {
        key: "trends",
        label: "Trends",
        badge: 0,
        active: tab === "trends",
        select: () => selectTab("trends"),
      },
      ...pinnedReports.map((report) => ({
        key: `dashboard:${report.id}`,
        label: report.title || "Dashboard",
        badge: 0,
        pinned: pinnedRefs.includes(report.ref),
        active: tab === "dashboard" && selectedReport === report,
        select: () => selectTab("dashboard", report.id),
      })),
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
    void loadPins();
    const clock = setInterval(() => (now = Date.now()), 30_000);
    return () => {
      clearInterval(clock);
      stop();
    };
  });

  // A tab picked off-screen (a dashboard from the menu, a link to Loops on a
  // phone) scrolls into the hidden-scrollbar row.
  let tabList = $state(null);
  let activeTabKey = $derived(tabs.find((t) => t.active)?.key ?? "");
  $effect(() => {
    if (!activeTabKey || !tabList) return;
    const list = tabList;
    // Sideways only: scrollIntoView would also move the page.
    void tick().then(() => {
      const el = list.querySelector('[aria-selected="true"]');
      if (!(el instanceof HTMLElement)) return;
      const right = el.offsetLeft + el.offsetWidth;
      if (el.offsetLeft < list.scrollLeft) list.scrollLeft = el.offsetLeft;
      else if (right > list.scrollLeft + list.clientWidth)
        list.scrollLeft = right - list.clientWidth;
    });
  });

  usePageTitle(() => "Watch · Agent Nexus");
</script>

{#snippet initiativeDetails(row)}
  <InitiativeDetails
    initiative={row.source.initiative}
    href={row.href}
    childHref={childInitiativeHref}
    {now}
  />
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
      <div class="min-w-0">
        <h1 class="text-title font-semibold text-fg">Watch</h1>
        <p
          class="mt-1 flex flex-wrap items-center gap-x-1.5 text-meta text-fg-muted"
          data-watch-summary
        >
          <!-- The board prints its own inventory line beside the budget. -->
          {#if brief && tab === "initiatives" && layout === "list"}
            <span data-source="computed:initiatives.rollup.total"
              >{rollupLine}</span
            ><span aria-hidden="true">·</span>
          {/if}
          {#if brief}
            <span data-source="computed:generated_at"
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
        </p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        {#if tab === "initiatives"}{@render initiativeActions()}{/if}
        {#if tab === "trends"}{@render rangeToggle()}{/if}
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
    </div>
    <div class="tab-row flex items-end gap-2">
      <div
        class="tabs flex min-w-0 flex-1 gap-1"
        role="tablist"
        aria-label="Watch views"
        bind:this={tabList}
        data-watch-tabs
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
            {pinnedRefs}
            triggerLabel="More dashboards"
            pinLabel="Pin as tab"
            unpinLabel="Unpin tab"
            hasMore={Boolean(dashboards?.has_more)}
            loading={loadingMore}
            onload={() => void loadMoreReports()}
            onselect={(id) => selectTab("dashboard", id || reports[0]?.id)}
            onpin={(ref) =>
              ref ? pinDashboard(ref) : unpinDashboard(selectedReport?.ref)}
          />
        </div>
      {/if}
    </div>
  </header>

  <!-- The lanes say this themselves, beside the board. -->
  {#if failed && brief && layout !== "lanes"}
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
  {#if pinError || pinAttention}<p
      class="text-meta text-danger-text"
      role="alert"
      data-watch-pin-error
    >
      {pinError ||
        pinAttention.failure?.message ||
        "Couldn't save your pin change. It's kept in your unsent work."}
    </p>{/if}
  {#if tab === "dashboard" && pinsReadFailed}
    <InlineAlert variant="warn">
      Couldn't read your pinned tabs, so some may be missing here. Pinning still
      works.
      <button
        type="button"
        class="text-accent-text underline"
        onclick={() => void loadPins()}
        data-watch-pins-retry>Retry reading pins</button
      >
    </InlineAlert>
  {/if}

  {#if tab === "dashboard"}
    {#if selectedReport}
      <section class="min-w-0 space-y-2" data-watch-dashboard>
        <div class="flex items-center justify-end gap-3">
          {#if selectedReport.ref && pinnedRefs.includes(selectedReport.ref)}
            <Button
              variant="secondary"
              size="compact"
              onclick={() => unpinDashboard(selectedReport.ref)}
              >Unpin tab</Button
            >
          {:else if selectedReport.ref}
            <Button
              variant="secondary"
              size="compact"
              onclick={() => pinDashboard(selectedReport.ref)}
              >Pin as tab</Button
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
    {:else if dashboardsFailed}
      <Banner>
        Couldn't load dashboards. Initiatives and loops are unaffected.
        {#snippet actions()}
          <Button
            variant="secondary"
            size="compact"
            onclick={() => loadDashboards()}>Retry</Button
          >
        {/snippet}
      </Banner>
    {:else}
      <SkeletonList rows={4} />
    {/if}
  {:else if tab === "trends"}
    <TrendsView days={trendDays} />
  {:else if layout === "lanes"}
    <!-- The board reads the same brief and draws its own loading, error
         and empty states: Ungrouped cards can wait with no initiative yet. -->
    <SwimlaneBoard
      {workspaceHref}
      {now}
      boardHref={workspaceHref("/watch?view=lanes")}
      decideLink={false}
    />
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
  {:else if !rollup.length && !digestCollection.counts.digest}
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
  {:else if rollup.length}
    <InitiativeRollupList
      rows={rollup}
      {now}
      hrefFor={childInitiativeHref}
      {decideHref}
      budget={brief.initiativeBudget}
      overTarget={brief.overTarget}
      overTargetHref={countAsk
        ? workspaceHref(`/decide/${encodeURIComponent(countAsk.inboxItemId)}`)
        : ""}
      changes={brief.initiativeChanges}
    />
  {/if}
  {#if tab === "initiatives" && brief && digestCollection.counts.digest}
    <section data-source="computed:daily_digest" aria-label="Daily digest">
      <h2 class="mb-2 text-meta font-semibold text-fg-muted">
        Needs an owner's look ({digestCollection.counts.digest})
      </h2>
      <CollectionTable
        collection={digestCollection}
        filters={false}
        label="daily digest"
      />
    </section>
  {/if}
</WorkspacePageShell>

{#snippet initiativeActions()}
  {@render layoutToggle([
    ["list", "List"],
    ["lanes", "Lanes"],
  ])}
{/snippet}

{#snippet loopActions()}
  {@render layoutToggle([
    ["list", "List"],
    ["board", "Board"],
  ])}
{/snippet}

{#snippet rangeToggle()}
  <div
    class="inline-flex rounded-md border border-line bg-bg-soft p-0.5"
    role="group"
    aria-label="Period"
    data-watch-trend-range
  >
    {#each TREND_RANGES as days (days)}
      <button
        type="button"
        class="rounded px-3 py-1 text-meta font-medium transition-colors max-md:min-h-10 focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent {trendDays ===
        days
          ? 'bg-panel-hover text-fg shadow-sm'
          : 'text-fg-muted hover:text-fg'}"
        aria-pressed={trendDays === days}
        aria-label="{days}d — last {days} days"
        data-watch-trend-days={days}
        onclick={() => navigate({ range: days === 30 ? "" : `${days}d` })}
        >{days}d</button
      >
    {/each}
  </div>
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
        onclick={() => navigate(layoutPatch(key))}>{text}</button
      >
    {/each}
  </div>
{/snippet}

<style>
  /*
   * The row's rule is an inset shadow, not a border, so the active tab's
   * underline covers it without a negative margin. That margin overflowed
   * the scrolling tab list by a pixel, which drew a stray vertical
   * scrollbar beside "More dashboards".
   */
  .tab-row {
    box-shadow: inset 0 -1px 0 var(--line);
  }
  /* Tabs that do not fit scroll sideways, with no visible scrollbar. */
  .tabs {
    position: relative;
    overflow-x: auto;
    overflow-y: hidden;
    overscroll-behavior-x: contain;
    scrollbar-width: none;
  }
  .tabs::-webkit-scrollbar {
    display: none;
  }
  .tab {
    display: inline-flex;
    flex: none;
    align-items: center;
    gap: 0.4rem;
    max-width: 16rem;
    min-height: 2.75rem;
    padding: 0.5rem 0.75rem;
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
