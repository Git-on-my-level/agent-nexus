<script>
  import { onMount, onDestroy } from "svelte";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import { getAuthenticatedAgent } from "$lib/authSession";
  import {
    readWorkspaceView,
    writeWorkspaceView,
  } from "$lib/workspaceViewCache";

  import { coreClient } from "$lib/coreClient";
  import {
    WORK_ROW_CAP,
    formatPartialCount,
    loadOverview,
  } from "$lib/overview.js";
  import { inboxWaitingLine } from "$lib/initiativeTiles.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import LiveInitiatives from "$lib/components/reports/LiveInitiatives.svelte";
  import VisualReport from "$lib/components/reports/VisualReport.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateError from "$lib/components/state/StateError.svelte";

  // The shell keys this component by organization/workspace. Snapshots are
  // memory-only, principal-scoped, at most 30 seconds old, and revalidated.
  const cacheKey = JSON.stringify([
    $page.params.organization,
    $page.params.workspace,
    getAuthenticatedAgent()?.agent_id,
    "overview",
  ]);
  let fetched = $state(readWorkspaceView(cacheKey));
  let model = $derived(fetched);
  let inboxWaiting = $derived(inboxWaitingLine(model?.needsYou));
  let refreshing = $state(false);
  let loadingMoreReports = $state(false);
  let pinning = $state(false);
  let pinError = $state("");

  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let reports = $derived(
    model?.reports?.status === "ok" ? model.reports.reports : [],
  );
  let agentCounts = $derived(
    model?.agents?.status === "ok"
      ? [
          { key: "working", label: "Working", count: model.agents.working },
          { key: "waiting", label: "Waiting", count: model.agents.waiting },
          { key: "stale", label: "Stale", count: model.agents.stale },
        ]
      : [],
  );

  function countClass(key, count) {
    if (count && (key === "waiting" || key === "stale"))
      return "text-warn-text";
    return "text-fg";
  }

  let selectedReport = $derived.by(() => {
    const wanted = $page.url.searchParams.get("dashboard") || "";
    return (
      reports.find(
        (entry) => entry.id === wanted || entry.segment === wanted,
      ) ||
      reports[0] ||
      null
    );
  });

  let request = 0;
  let requestedReportLoad = $state("");
  $effect(() => {
    const wanted = $page.url.searchParams.get("dashboard") || "";
    const key = `${request}:${wanted}`;
    if (
      wanted &&
      model?.reports?.status === "ok" &&
      model.reports.has_more &&
      !reports.some(
        (entry) => entry.id === wanted || entry.segment === wanted,
      ) &&
      requestedReportLoad !== key &&
      !loadingMoreReports
    ) {
      requestedReportLoad = key;
      void loadMoreReports();
    }
  });

  async function refresh() {
    const id = ++request;
    refreshing = true;
    try {
      const next = await loadOverview(coreClient);
      if (id === request) {
        fetched = next;
        writeWorkspaceView(cacheKey, next);
      }
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : "Overview could not be loaded.";
      const section = { status: "unavailable", message };
      if (id !== request) return;
      fetched = {
        needsYou: section,
        work: section,
        agents: section,
        reports: section,
        initiatives: section,
      };
    } finally {
      if (id === request) refreshing = false;
    }
  }

  async function pinDashboard(ref) {
    pinning = true;
    pinError = "";
    try {
      await coreClient.setWorkspaceDashboard({ document_ref: ref });
      await refresh();
    } catch (error) {
      pinError =
        error instanceof Error
          ? error.message
          : "Dashboard could not be pinned.";
    } finally {
      pinning = false;
    }
  }

  function selectReport(id) {
    const url = new URL($page.url);
    const first = reports[0];
    if (!id || id === first?.id) url.searchParams.delete("dashboard");
    else url.searchParams.set("dashboard", id);
    void goto(url, { keepFocus: true, noScroll: true, replaceState: true });
  }

  async function loadMoreReports() {
    const section = fetched?.reports;
    if (section?.status !== "ok" || !section.has_more || loadingMoreReports)
      return;
    const id = request;
    loadingMoreReports = true;
    try {
      const more = await coreClient.getDashboardReports();
      if (id !== request || !fetched?.reports) return;
      fetched = { ...fetched, reports: more };
    } catch (error) {
      if (id === request)
        pinError =
          error instanceof Error
            ? error.message
            : "Report choices could not be loaded.";
    } finally {
      if (id === request) loadingMoreReports = false;
    }
  }

  onDestroy(() => {
    request += 1;
  });

  onMount(() => {
    void refresh();
  });
</script>

<svelte:head><title>Overview · Agent Nexus</title></svelte:head>

<WorkspacePageShell>
  <WorkspacePageHeader title="Overview">
    {#snippet subtitle()}
      What needs you, what is in flight, and which reports are current.
    {/snippet}
  </WorkspacePageHeader>

  {#if !model}
    <div role="status" aria-label="Loading overview">
      <Skeleton rows={6} />
    </div>
  {:else}
    <!--
      One line, not a second Inbox. The dashboard says how many decisions are
      waiting and links across; which initiative each belongs to is a pill on
      that initiative's tile. Restating the Inbox here was the duplication the
      brief rules out.
    -->
    <section
      class="rounded-md border border-line bg-panel"
      aria-labelledby="overview-needs-you"
      data-overview-section="needs-you"
      data-overview-status={model.needsYou.status}
    >
      <header
        class="flex flex-wrap items-baseline justify-between gap-2 px-3 py-2"
      >
        <h2 id="overview-needs-you" class="text-subtitle text-fg">Needs you</h2>
        {#if inboxWaiting}
          <a
            class="text-meta text-accent-text hover:underline"
            href={workspaceHref(inboxWaiting.href)}
            data-overview-needs-you-count
          >
            {inboxWaiting.label} →
          </a>
        {/if}
      </header>
      {#if model.needsYou.status !== "ok"}
        <div class="px-3 pb-3">
          <StateError
            title="Needs you is unavailable"
            message={model.needsYou.message}
            onretry={refresh}
            retrying={refreshing}
          />
        </div>
      {:else if !inboxWaiting}
        <p class="px-3 pb-3 text-meta text-fg-muted" data-overview-empty>
          Nothing is waiting on you.
        </p>
      {/if}
    </section>
    <section
      class="rounded-md border border-line bg-panel"
      aria-labelledby="overview-reports"
      data-overview-section="reports"
      data-overview-status={model.reports.status}
    >
      <header
        class="flex flex-wrap items-center justify-between gap-2 border-b border-line px-3 py-2"
      >
        <h2 id="overview-reports" class="text-subtitle text-fg">Dashboard</h2>
        {#if model.reports.status === "ok" && selectedReport}
          <div class="flex flex-wrap items-center gap-2">
            {#if reports.length > 1 || model.reports.has_more}
              <label class="flex items-center gap-2 text-micro text-fg-muted">
                Report
                <select
                  class="ui-input w-auto"
                  aria-label="Report"
                  value={selectedReport.id}
                  onfocus={() => void loadMoreReports()}
                  onpointerdown={() => void loadMoreReports()}
                  onchange={(event) => selectReport(event.currentTarget.value)}
                >
                  {#each reports as entry (entry.id)}
                    <option value={entry.id}>{entry.title || entry.id}</option>
                  {/each}
                  {#if model.reports.has_more}
                    <option disabled value="">
                      {loadingMoreReports
                        ? "Loading reports…"
                        : "Other reports"}
                    </option>
                  {/if}
                </select>
              </label>
            {/if}
            <button
              class="ui-button"
              disabled={pinning ||
                model.reports.pinned_ref === selectedReport.ref}
              onclick={() => pinDashboard(selectedReport.ref)}
            >
              {pinning
                ? "Pinning dashboard…"
                : model.reports.pinned_ref === selectedReport.ref
                  ? "Pinned dashboard"
                  : "Pin as dashboard"}
            </button>
            {#if model.reports.pinned_ref}
              <button
                class="ui-button"
                disabled={pinning}
                onclick={() => pinDashboard(null)}>Use newest report</button
              >
            {/if}
            <a
              class="text-meta text-accent-text hover:underline"
              href={workspaceHref(
                `/docs/${encodeURIComponent(selectedReport.segment)}`,
              )}
              data-overview-report-link>Open document</a
            >
          </div>
        {/if}
      </header>
      {#if pinError}<p
          class="px-3 py-2 text-meta text-danger-text"
          role="alert"
        >
          {pinError}
        </p>{/if}
      {#if model.reports.status !== "ok"}
        <div class="p-3">
          <StateError
            title="Dashboard is unavailable"
            message={model.reports.message}
            onretry={refresh}
            retrying={refreshing}
          />
        </div>
      {:else if !selectedReport}
        <p class="px-3 py-4 text-meta text-fg-muted" data-overview-empty>
          Pin a visual report to show your workspace dashboard here.
        </p>
      {:else}
        {#if model.reports.warning}
          <p class="px-3 pt-2 text-micro text-warn-text" role="status">
            {model.reports.warning}
          </p>
        {/if}
        <div class="px-1 py-2" data-overview-report={selectedReport.id}>
          <VisualReport
            compact
            report={selectedReport.report}
            documentId={selectedReport.id}
            revisionRef={selectedReport.revision_ref ?? ""}
          />
        </div>
      {/if}
    </section>

    <section
      class="rounded-md border border-line bg-panel"
      aria-labelledby="overview-initiatives"
      data-overview-section="initiatives"
    >
      <header
        class="flex items-baseline justify-between border-b border-line px-3 py-2"
      >
        <h2 id="overview-initiatives" class="text-subtitle text-fg">
          Initiatives
        </h2>
        <a
          class="text-meta text-accent-text hover:underline"
          href={workspaceHref("/tasks")}>All tasks</a
        >
      </header>
      {#if model.initiatives.status !== "ok"}
        <div class="p-3">
          <StateError
            title="Initiatives are unavailable"
            message={model.initiatives.message}
            onretry={refresh}
            retrying={refreshing}
          />
        </div>
      {:else if !model.initiatives.items.length}
        <p class="px-3 py-4 text-meta text-fg-muted">No open initiatives.</p>
      {:else}
        <div class="p-3">
          <LiveInitiatives items={model.initiatives.items} />
        </div>
      {/if}
    </section>
    <details
      class="rounded-md border border-line bg-panel"
      data-overview-detail
    >
      <summary class="cursor-pointer px-3 py-2 text-meta text-fg-muted"
        >Work detail</summary
      >
      <div class="space-y-4 p-3">
        <section
          class="rounded-md border border-line bg-panel"
          aria-labelledby="overview-work"
          data-overview-section="work"
          data-overview-status={model.work.status}
        >
          <header
            class="flex flex-wrap items-baseline justify-between gap-2 border-b border-line px-3 py-2"
          >
            <h2 id="overview-work" class="text-subtitle text-fg">
              Work at a glance
            </h2>
            {#if model.work.status === "ok"}
              <a
                class="text-meta text-accent-text hover:underline"
                href={workspaceHref("/tasks")}
                data-overview-work-total
              >
                {formatPartialCount(model.work.total, model.work.truncated)}
                {model.work.truncated || model.work.total !== 1
                  ? "tasks"
                  : "task"}
              </a>
            {/if}
          </header>
          {#if model.work.status !== "ok"}
            <div class="p-3">
              <StateError
                title="Tasks are unavailable"
                message={model.work.message}
                onretry={refresh}
                retrying={refreshing}
              />
            </div>
          {:else}
            {#if model.work.truncated}
              <p class="px-3 pt-2 text-micro text-fg-muted">
                Counts cover the {WORK_ROW_CAP.toLocaleString("en-US")} most recently
                updated tasks.
              </p>
            {/if}
            {#if model.work.matrix.rows.length === 0}
              <p class="px-3 py-4 text-meta text-fg-muted" data-overview-empty>
                No tasks yet.
              </p>
            {:else}
              <div class="overflow-x-auto">
                <table class="w-full border-collapse text-micro">
                  <caption class="sr-only">Tasks by source and status</caption>
                  <thead>
                    <tr class="text-fg-muted">
                      <th class="px-3 py-1.5 text-left font-medium" scope="col"
                        >Source</th
                      >
                      {#each model.work.matrix.phases as phase (phase.key)}
                        <th
                          class="px-2 py-1.5 text-right font-medium"
                          scope="col">{phase.label}</th
                        >
                      {/each}
                    </tr>
                  </thead>
                  <tbody>
                    {#each model.work.matrix.rows as row (row.key || "none")}
                      <tr
                        class="border-t border-line-subtle"
                        data-overview-source={row.key || "none"}
                      >
                        <th
                          class="whitespace-nowrap px-3 py-1.5 text-left font-medium text-fg"
                          scope="row">{row.label}</th
                        >
                        {#each row.cells as cell (cell.phase)}
                          {@const cellClass = cell.count
                            ? cell.phase === "blocked"
                              ? "text-warn-text"
                              : "text-fg"
                            : "text-fg-subtle"}
                          {@const cellText = formatPartialCount(
                            cell.count,
                            model.work.truncated,
                          )}
                          <td class="px-2 py-1.5 text-right tabular-nums">
                            {#if cell.href}
                              <a
                                class="hover:underline {cellClass}"
                                href={workspaceHref(cell.href)}
                                data-overview-cell="{row.key ||
                                  'none'}:{cell.phase}">{cellText}</a
                              >
                            {:else}
                              <span
                                class={cellClass}
                                data-overview-cell="{row.key ||
                                  'none'}:{cell.phase}">{cellText}</span
                              >
                            {/if}
                          </td>
                        {/each}
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>
            {/if}

            <div
              class="grid gap-px border-t border-line bg-line sm:grid-cols-2"
            >
              <div class="bg-panel px-3 py-2" data-overview-blocked>
                <a
                  class="text-meta text-accent-text hover:underline"
                  href={workspaceHref(model.work.blocked.href)}
                >
                  <span class="tabular-nums text-fg"
                    >{formatPartialCount(
                      model.work.blocked.count,
                      model.work.truncated,
                    )}</span
                  >
                  blocked
                </a>
                {#if model.work.blocked.items.length}
                  <ul class="mt-1 space-y-0.5">
                    {#each model.work.blocked.items as item (item.key)}
                      <li class="truncate text-micro">
                        <a
                          class="text-fg-muted hover:text-fg hover:underline"
                          href={workspaceHref(item.href)}>{item.title}</a
                        >
                      </li>
                    {/each}
                  </ul>
                {/if}
              </div>
              <div class="bg-panel px-3 py-2" data-overview-human>
                {#if model.work.human.status !== "ok"}
                  <p class="text-meta text-warn-text">
                    Next actor is unavailable. {model.work.human.message}
                  </p>
                {:else}
                  <a
                    class="text-meta text-accent-text hover:underline"
                    href={workspaceHref(model.work.human.href)}
                    data-overview-human-count
                  >
                    <span class="tabular-nums text-fg"
                      >{formatPartialCount(
                        model.work.human.count,
                        model.work.truncated,
                      )}</span
                    >
                    {model.work.truncated || model.work.human.count !== 1
                      ? "tasks have a person as next actor"
                      : "task has a person as next actor"}
                  </a>
                  {#if model.work.human.incomplete}
                    <p
                      class="mt-1 text-micro text-warn-text"
                      data-overview-human-partial
                    >
                      Next-actor count may be incomplete.
                    </p>
                  {/if}
                  {#if model.work.human.items.length}
                    <ul class="mt-1 space-y-0.5">
                      {#each model.work.human.items as item (item.key)}
                        <li class="truncate text-micro">
                          <a
                            class="text-fg-muted hover:text-fg hover:underline"
                            href={workspaceHref(item.href)}>{item.title}</a
                          >
                        </li>
                      {/each}
                    </ul>
                  {/if}
                {/if}
              </div>
            </div>
            <ul
              class="flex flex-wrap gap-x-4 gap-y-1 border-t border-line px-3 py-2"
            >
              {#each model.work.freshness as bucket (bucket.key)}
                <li>
                  <a
                    class="text-micro text-accent-text hover:underline"
                    href={workspaceHref(bucket.href)}
                    data-overview-freshness={bucket.key}
                  >
                    <span
                      class="tabular-nums {bucket.count &&
                      bucket.key !== 'unknown'
                        ? 'text-warn-text'
                        : 'text-fg'}"
                      >{formatPartialCount(
                        bucket.count,
                        model.work.truncated,
                      )}</span
                    >
                    {bucket.label.toLowerCase()}
                  </a>
                </li>
              {/each}
            </ul>
          {/if}
        </section>
        <section
          class="rounded-md border border-line bg-panel"
          aria-labelledby="overview-agents"
          data-overview-section="agents"
          data-overview-status={model.agents.status}
        >
          <header
            class="flex items-baseline justify-between gap-2 border-b border-line px-3 py-2"
          >
            <h2 id="overview-agents" class="text-subtitle text-fg">Agents</h2>
            {#if model.agents.status === "ok"}
              <a
                class="text-meta text-accent-text hover:underline"
                href={workspaceHref(model.agents.href)}>Roster</a
              >
            {/if}
          </header>
          {#if model.agents.status !== "ok"}
            <div class="p-3">
              <StateError
                title="Agents are unavailable"
                message={model.agents.message}
                onretry={refresh}
                retrying={refreshing}
              />
            </div>
          {:else}
            <ul class="grid grid-cols-3 divide-x divide-line-subtle">
              {#each agentCounts as item (item.key)}
                <li>
                  <a
                    class="flex flex-col gap-0.5 px-3 py-3 hover:bg-panel-hover"
                    href={workspaceHref(model.agents.href)}
                    data-overview-agents={item.key}
                  >
                    <span
                      class="text-title tabular-nums {countClass(
                        item.key,
                        item.count,
                      )}">{item.count}</span
                    >
                    <span class="text-micro text-fg-muted">{item.label}</span>
                  </a>
                </li>
              {/each}
            </ul>
          {/if}
        </section>
      </div>
    </details>
  {/if}
</WorkspacePageShell>
