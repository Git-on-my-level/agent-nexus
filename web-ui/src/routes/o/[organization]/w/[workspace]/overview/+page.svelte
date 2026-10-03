<script>
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import { coreClient } from "$lib/coreClient";
  import {
    DOC_SCAN_CAP,
    WORK_ROW_CAP,
    formatPartialCount,
    loadOverview,
    loadPendingReports,
  } from "$lib/overview.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import VisualReport from "$lib/components/reports/VisualReport.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateError from "$lib/components/state/StateError.svelte";

  // The server load stays empty so the skeleton is the first paint.
  // The browser is the only business loader, matching Tasks and Inbox.
  let fetched = $state(null);
  let model = $derived(fetched);
  let refreshing = $state(false);
  let loadingMoreReports = $state(false);

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
  async function refresh() {
    const id = ++request;
    refreshing = true;
    try {
      const next = await loadOverview(coreClient);
      if (id === request) fetched = next;
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
      };
    } finally {
      if (id === request) refreshing = false;
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
    const pending = section?.status === "ok" ? section.pending : null;
    if (!pending?.length || loadingMoreReports) return;
    const id = request;
    loadingMoreReports = true;
    try {
      const more = await loadPendingReports(
        coreClient,
        pending,
        section.reports,
      );
      if (id !== request || !fetched?.reports) return;
      fetched = {
        ...fetched,
        reports: {
          ...fetched.reports,
          reports: more.reports,
          pending: [],
          warning: more.failures
            ? "Some documents could not be read, so this list may be incomplete."
            : fetched.reports.warning,
        },
      };
    } finally {
      if (id === request) loadingMoreReports = false;
    }
  }

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
    <div class="grid grid-cols-1 gap-4 lg:grid-cols-2 [&>*]:min-w-0">
      <section
        class="rounded-md border border-line bg-panel"
        aria-labelledby="overview-needs-you"
        data-overview-section="needs-you"
        data-overview-status={model.needsYou.status}
      >
        <header
          class="flex flex-wrap items-baseline justify-between gap-2 border-b border-line px-3 py-2"
        >
          <h2 id="overview-needs-you" class="text-subtitle text-fg">
            Needs you
          </h2>
          {#if model.needsYou.status === "ok"}
            <a
              class="text-meta text-accent-text hover:underline"
              href={workspaceHref(model.needsYou.href)}
              data-overview-needs-you-count
            >
              {model.needsYou.count}{model.needsYou.truncated ? "+" : ""}
              {model.needsYou.count === 1 ? "item" : "items"}
            </a>
          {/if}
        </header>
        {#if model.needsYou.status !== "ok"}
          <div class="p-3">
            <StateError
              title="Needs you is unavailable"
              message={model.needsYou.message}
              onretry={refresh}
              retrying={refreshing}
            />
          </div>
        {:else if model.needsYou.count === 0}
          <p class="px-3 py-4 text-meta text-fg-muted" data-overview-empty>
            Nothing is waiting on you.
          </p>
        {:else}
          <ul class="divide-y divide-line-subtle">
            {#each model.needsYou.rows as row (row.id)}
              <li>
                <a
                  class="flex min-w-0 flex-col gap-0.5 px-3 py-2 hover:bg-panel-hover"
                  href={workspaceHref(row.href)}
                  data-overview-needs-you-row
                >
                  <span class="flex min-w-0 items-center gap-2">
                    <span
                      class="min-w-0 flex-1 truncate text-meta font-medium text-fg"
                      >{row.title}</span
                    >
                    {#if row.badge}
                      <SignalBadge tone={row.badge.tone} class="shrink-0"
                        >{row.badge.label}</SignalBadge
                      >
                    {/if}
                    {#if row.wait}
                      <span
                        class="shrink-0 text-micro tabular-nums text-warn-text"
                        >{row.wait}</span
                      >
                    {/if}
                  </span>
                  {#if row.requester || row.source}
                    <span class="truncate text-micro text-fg-muted">
                      {#if row.requester}<span class="text-fg"
                          >{row.requester}</span
                        >{/if}{#if row.requester && row.source}
                        ·
                      {/if}{row.source}
                    </span>
                  {/if}
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
            {model.work.truncated || model.work.total !== 1 ? "tasks" : "task"}
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
                    <th class="px-2 py-1.5 text-right font-medium" scope="col"
                      >{phase.label}</th
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

        <div class="grid gap-px border-t border-line bg-line sm:grid-cols-2">
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
                  class="tabular-nums {bucket.count && bucket.key !== 'unknown'
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
      aria-labelledby="overview-reports"
      data-overview-section="reports"
      data-overview-status={model.reports.status}
    >
      <header
        class="flex flex-wrap items-center justify-between gap-2 border-b border-line px-3 py-2"
      >
        <h2 id="overview-reports" class="text-subtitle text-fg">Reports</h2>
        {#if model.reports.status === "ok" && selectedReport}
          <div class="flex flex-wrap items-center gap-2">
            {#if reports.length > 1 || model.reports.pending?.length}
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
                  {#if model.reports.pending?.length}
                    <option disabled value="">
                      {loadingMoreReports
                        ? "Loading reports…"
                        : "Other reports"}
                    </option>
                  {/if}
                </select>
              </label>
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
      {#if model.reports.status !== "ok"}
        <div class="p-3">
          <StateError
            title="Reports are unavailable"
            message={model.reports.message}
            onretry={refresh}
            retrying={refreshing}
          />
        </div>
      {:else if !selectedReport}
        <p class="px-3 py-4 text-meta text-fg-muted" data-overview-empty>
          No visual report in the latest documents.
        </p>
      {:else}
        {#if model.reports.warning}
          <p class="px-3 pt-2 text-micro text-warn-text" role="status">
            {model.reports.warning}
          </p>
        {/if}
        {#if model.reports.truncated}
          <p class="px-3 pt-2 text-micro text-fg-muted">
            Scanned the {DOC_SCAN_CAP.toLocaleString("en-US")} most recently updated
            documents.
          </p>
        {/if}
        <div class="px-1 py-2" data-overview-report={selectedReport.id}>
          <VisualReport report={selectedReport.report} />
        </div>
      {/if}
    </section>
  {/if}
</WorkspacePageShell>
