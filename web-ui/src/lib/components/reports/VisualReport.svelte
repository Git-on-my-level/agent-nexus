<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { onMount, untrack } from "svelte";
  import { coreClient } from "$lib/coreClient";
  import { isLivePanel, withLiveObservation } from "$lib/liveReports.js";
  import {
    nextReviewDeadline,
    reviewReadPending,
    withRenderedProvenance,
    withReportDefaults,
  } from "$lib/reportProvenance.js";
  import { getPanelFreshness } from "$lib/visualReports.js";
  import VisualReportPanel from "./VisualReportPanel.svelte";
  import AnxRefPreview from "$lib/components/AnxRefPreview.svelte";
  import {
    collectPageRefs,
    hasUnreadableRefs,
    indexResolvedRefs,
    keepReadableRefs,
    resolveRefsInBatches,
  } from "$lib/refResolve.js";
  import { reportRefStrings } from "./reportRefs.js";
  import ReportLayout from "./ReportLayout.svelte";
  import { layoutPanelIds } from "./reportLayout.js";

  let {
    report,
    documentId = "",
    revisionRef = "",
    compact = false,
    previewObservations = null,
  } = $props();
  let liveObservations = $state(new Map());
  let hasLive = $derived(report.panels.some(isLivePanel));
  let observedPanels = $derived(
    report.panels.map((panel) =>
      withLiveObservation(
        withRenderedProvenance(
          withReportDefaults(panel, report),
          liveObservations.get(panel.id),
        ),
        liveObservations.get(panel.id),
      ),
    ),
  );
  let now = $state(Date.now());

  /**
   * Every ref written anywhere in the report, resolved in one request so chips
   * in table cells, callouts, timelines and diagram nodes render without a
   * fetch each.
   *
   * Observed panels, not authored ones: a live panel's prose arrives with its
   * observation, so a ref written only in a live initiative's summary is not
   * in the report at load time. Reading the authored panels left those chips
   * dashed and "not found" for a ref that resolves perfectly well. The effect
   * re-runs when an observation lands, and the key guard keeps a refresh that
   * names the same refs from re-asking.
   */
  let refPreview = $state();
  let resolvedRefs = $state(new Map());
  /**
   * What the latest request asked for: the ref set, and which request it was.
   * Deliberately outside the reactive graph — the effect both reads and writes
   * it, and as state that would be a loop.
   *
   * Staleness is decided when a response lands rather than by cancelling on
   * re-run: an observation that refreshes without changing any ref re-runs
   * this effect, and cancelling there would throw away the in-flight answer
   * and leave every chip blank.
   *
   * It takes the counter as well as the key to decide that, because the key
   * only says *what* was asked and two requests can ask the same thing. Switch
   * dashboards A → B → A with the first A request still in flight and its
   * answer — resolved before anything moved — arrives last and matches the key
   * exactly, overwriting the titles and statuses the second A just read. Only
   * the newest request may write.
   */
  const resolving = { key: "", generation: 0 };
  $effect(() => {
    const refs = collectPageRefs(reportRefStrings(observedPanels));
    if (!refs.length) {
      resolving.key = "";
      // A report with no refs supersedes an in-flight answer like any other.
      resolving.generation += 1;
      resolvedRefs = new Map();
      return;
    }
    // Sorted: the same refs in a different order are the same question. A live
    // initiatives panel lists by attention, so an unchanged set arrives
    // reordered the moment anything moves, and treating that as a new question
    // both re-asks and supersedes the answer already on its way — with a slow
    // resolver, every answer in turn.
    const key = [...refs].sort().join("\u0000");
    if (key === resolving.key) return;
    resolving.key = key;
    const generation = ++resolving.generation;
    // Batched: a report may name more refs than one request accepts, and an
    // oversized request is rejected whole.
    void resolveRefsInBatches(refs, (batch) => coreClient.resolveRefs(batch))
      .then((result) => {
        if (resolving.generation !== generation) return;
        resolvedRefs = keepReadableRefs(resolvedRefs, result);
        // A batch that could not be read is not an answer about those refs.
        // Its chips read "not found" because that beats blank, but remembering
        // the key would keep a single 503 on screen for as long as the reader
        // stays on these refs: clearing it means the next report or
        // observation asks again. A ref the resolver answered "no such thing"
        // is not retried — that answer will not change by asking twice.
        if (hasUnreadableRefs(result)) resolving.key = "";
      })
      .catch(() => {
        // A batch that rejects is already handled above, as unreadable refs;
        // this is the resolver itself failing to run. Chips fall back to
        // "not found", and the key is cleared so the next report or
        // observation retries rather than inheriting the failure.
        if (resolving.generation !== generation) return;
        resolving.key = "";
        resolvedRefs = indexResolvedRefs({}, refs);
      });
  });
  const refProps = () => ({
    resolved: resolvedRefs,
    // A report can render outside a workspace route; without a workspace a
    // chip simply does not link rather than throwing.
    organizationSlug: $page?.params?.organization ?? "",
    workspaceSlug: $page?.params?.workspace ?? "",
    onpreview: (model, anchor) => refPreview?.open(model, anchor),
    onpreviewclose: () => refPreview?.requestClose(),
  });
  const freshnessOptions = [
    "all",
    "current",
    "stale",
    "unknown",
    "unavailable",
  ];
  let project = $derived(
    report.projects.some(
      (item) => item.id === $page.url.searchParams.get("reportProject"),
    )
      ? $page.url.searchParams.get("reportProject")
      : "all",
  );
  let freshness = $derived(
    freshnessOptions.includes($page.url.searchParams.get("reportFreshness"))
      ? $page.url.searchParams.get("reportFreshness")
      : "all",
  );
  let evidence = $derived($page.url.searchParams.get("reportEvidence") ?? "");
  let panels = $derived(
    observedPanels.filter(
      (panel) =>
        (compact || project === "all" || panel.project_id === project) &&
        (compact ||
          freshness === "all" ||
          getPanelFreshness(panel, now) === freshness),
    ),
  );
  let panelsById = $derived(new Map(panels.map((panel) => [panel.id, panel])));
  let referencedPanels = $derived(layoutPanelIds(report.layout));
  let remainingPanels = $derived(
    report.layout
      ? panels.filter((panel) => !referencedPanels.has(panel.id))
      : panels,
  );
  let tabSelections = $derived(
    new Map(
      [...$page.url.searchParams.entries()]
        .filter(([key]) => key.startsWith("reportTab."))
        .map(([key, value]) => [key.slice("reportTab.".length), value]),
    ),
  );
  let staleCount = $derived(
    observedPanels.filter((panel) => getPanelFreshness(panel, now) === "stale")
      .length,
  );

  function setFilter(key, value) {
    const url = new URL($page.url);
    if (!value || (value === "all" && !key.startsWith("reportTab.")))
      url.searchParams.delete(key);
    else url.searchParams.set(key, value);
    if (key === "reportProject" || key === "reportFreshness")
      url.searchParams.delete("reportEvidence");
    void goto(url, { noScroll: true, keepFocus: true });
  }
  function inspectPanel(id) {
    setFilter("reportEvidence", evidence === id ? "" : id);
  }
  // Each definition/reader change starts a fresh read. Ignore late responses
  // after navigation and drop old successful data immediately on a failed refresh.
  $effect(() => {
    const id = documentId;
    const expectedRevision = revisionRef;
    const livePanels = report.panels.filter(isLivePanel);
    liveObservations = new Map();
    // Every report is read once: core resolves each authored panel's class and
    // review deadline against its own clock and returns them here, so a
    // dashboard of hand-written notes still gets a true "written 9d ago,
    // overdue" rather than this reader's arithmetic on their own clock.
    // Only live panels are worth polling for, though — an authored panel's
    // provenance does not change while it is on screen.
    if (!report.panels.length) return;
    if (Array.isArray(previewObservations)) {
      liveObservations = new Map(
        previewObservations.map((panel) => [panel.id, panel]),
      );
      return;
    }
    let disposed = false;
    let inFlight = false;
    let reviewTimer = 0;
    // Reads that should have told core about a deadline and did not. Backed
    // off rather than repeated at a fixed minute: five minutes only covers
    // five minutes of clock disagreement, and a machine without NTP can be out
    // by much more. Eight tries reach about three hours and then stop, so a
    // document that has gone for good is not asked about for ever.
    let reviewAttempts = 0;
    const REVIEW_ATTEMPTS = 8;
    const reviewRetryWait = (attempt) =>
      Math.min(60_000 * 2 ** attempt, 60 * 60_000);
    async function refresh() {
      if (inFlight || disposed) return;
      inFlight = true;
      let results;
      try {
        if (!id) throw new Error("A saved document is required.");
        // `summary=1` opts the native card panels into the computed summary
        // the shared renderer draws.
        const response = await coreClient.renderReport(id, { summary: 1 });
        if (
          !Array.isArray(response?.panels) ||
          (expectedRevision && response.revision_ref !== expectedRevision)
        )
          throw new Error("The report changed. Reload this document.");
        results = new Map(response.panels.map((panel) => [panel.id, panel]));
        for (const panel of livePanels) {
          if (results.get(panel.id)?.type !== panel.type)
            results.set(panel.id, {
              status: "unavailable",
              message: "The report changed. Reload this document.",
              data: {},
            });
        }
      } catch {
        // Only live panels lose anything. An authored panel keeps whatever the
        // last good read resolved — the absolute deadline, whether it was
        // defaulted, the principal core corrected the author to — because a
        // failed request is not news about a hand-written panel, and starting
        // over from the document would quietly contradict what core said.
        // Untracked: a document id that is missing throws before the first
        // await, so this runs inside the effect that writes the same state.
        results = new Map(untrack(() => liveObservations));
        for (const panel of livePanels)
          results.set(panel.id, {
            status: "unavailable",
            message:
              "Live data unavailable. Reload the document or check your access.",
            data: {},
          });
      }
      if (!disposed) {
        liveObservations = results;
        armReviewDeadline();
      }
      inFlight = false;
    }
    /**
     * Read once more when the soonest authored panel falls due.
     *
     * The provenance line already turns amber on its own as the clock ticks,
     * but the read is also what tells core to remind the author — so a
     * dashboard left open on a wall display should take itself past the
     * deadline rather than wait for someone to reload it. One timer, re-armed
     * on each read, and clamped because `setTimeout` silently fires at once
     * past about 24 days.
     */
    function armReviewDeadline() {
      window.clearTimeout(reviewTimer);
      // Untracked: this runs inside the effect that writes `liveObservations`,
      // and a synchronous failure path would otherwise make the derived a
      // dependency of the effect that feeds it.
      const panels = untrack(() => observedPanels);
      const at = Date.now();
      const pending = reviewReadPending(panels, at);
      if (!pending) reviewAttempts = 0;
      // The two are independent. A panel core will never confirm must not
      // silence the next panel's deadline, and running out of tries for one
      // must not stop the report reading for another.
      const retryAt =
        pending && reviewAttempts < REVIEW_ATTEMPTS
          ? at + reviewRetryWait(reviewAttempts)
          : null;
      const deadlineAt = nextReviewDeadline(panels, at);
      // Which of the two this wake is for. The callback cannot work it out
      // afterwards — a deadline that has just arrived is "pending" by
      // definition — and the budget must bound re-asking about one deadline,
      // never asking about the next.
      const forDeadline =
        retryAt === null || (deadlineAt !== null && deadlineAt < retryAt);
      const target = forDeadline ? deadlineAt : retryAt;
      if (target === null) return;
      // `setTimeout` fires immediately past about 24 days, so a deadline
      // further out than the cap waits in hops. Only the hop that reaches the
      // deadline reads: a dashboard open for a month should not re-read the
      // report every six hours on the way there.
      const wait = Math.max(Math.min(target - at + 1000, 21_600_000), 1000);
      reviewTimer = window.setTimeout(() => {
        if (disposed) return;
        now = Date.now();
        if (forDeadline) {
          // A hop on the way to a distant deadline, not the deadline itself.
          if (Date.now() < target) {
            armReviewDeadline();
            return;
          }
          // A deadline of its own gets a fresh budget: three hours spent on a
          // panel core will never confirm must not cost the next panel its
          // one read.
          reviewAttempts = 0;
          void refresh();
          return;
        }
        if (
          reviewReadPending(
            untrack(() => observedPanels),
            Date.now(),
          ) &&
          reviewAttempts < REVIEW_ATTEMPTS
        ) {
          // A read already running will re-arm when it lands; do not spend a
          // try on a call that returns without asking core anything.
          if (!inFlight) reviewAttempts += 1;
          void refresh();
        } else armReviewDeadline();
      }, wait);
    }
    void refresh();
    // A report with nothing live is read once. The teardown is still returned:
    // without it a response that lands after navigation would write into the
    // next document's observations.
    if (!livePanels.length)
      return () => {
        disposed = true;
        window.clearTimeout(reviewTimer);
      };
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible") void refresh();
    }, 30_000);
    const resume = () => {
      if (document.visibilityState === "visible") void refresh();
    };
    document.addEventListener("visibilitychange", resume);
    return () => {
      disposed = true;
      window.clearInterval(timer);
      window.clearTimeout(reviewTimer);
      document.removeEventListener("visibilitychange", resume);
    };
  });
  onMount(() => {
    const timer = window.setInterval(() => {
      now = Date.now();
    }, 60_000);
    return () => window.clearInterval(timer);
  });
</script>

<section class="visual-report" aria-label="Visual report">
  <header class="report-heading">
    <div>
      {#if !compact}<p class="report-kicker">
          Visual report <span>· v{report.schema_version}</span>
        </p>{/if}
      <h2>{report.title}</h2>
      {#if !compact}<p class="report-summary">{report.summary}</p>{/if}
    </div>
    {#if !compact}<div class="report-snapshot">
        <span class="report-snapshot-dot" aria-hidden="true"></span><span
          >{hasLive ? "Live workspace + snapshots" : "Snapshot, not live"}<br
          /><time datetime={report.generated_at}
            >{new Date(report.generated_at)
              .toISOString()
              .slice(0, 16)
              .replace("T", " ")} UTC</time
          ></span
        >
      </div>{/if}
  </header>

  {#if !compact && (!report.layout || report.projects.length > 1)}
    <div class="report-projects" aria-label="Project overview">
      {#each report.projects as item}
        <button
          type="button"
          class="report-project"
          class:report-project-selected={project === item.id}
          aria-pressed={project === item.id}
          onclick={() =>
            setFilter("reportProject", project === item.id ? "all" : item.id)}
        >
          <span class="report-project-name"
            >{item.title}<span aria-hidden="true">↗</span></span
          ><strong>{item.outcome}</strong><span class="report-project-summary"
            >{item.summary}</span
          >
        </button>
      {/each}
    </div>
  {/if}

  {#if !compact}<div class="report-toolbar">
      <div class="flex flex-wrap items-center gap-3">
        <button
          type="button"
          class="report-all"
          aria-pressed={project === "all"}
          onclick={() => setFilter("reportProject", "all")}>All projects</button
        >
        <p class="text-micro text-fg-muted" aria-live="polite">
          {panels.length} of {report.panels.length} panels{staleCount
            ? ` · ${staleCount} stale`
            : ""}
        </p>
      </div>
      <label class="report-filter-label"
        >Freshness <select
          aria-label="Filter by freshness"
          value={freshness}
          onchange={(event) =>
            setFilter("reportFreshness", event.currentTarget.value)}
          >{#each freshnessOptions as option}<option value={option}
              >{option === "all"
                ? "All evidence"
                : option.charAt(0).toUpperCase() + option.slice(1)}</option
            >{/each}</select
        ></label
      >
    </div>{/if}

  {#if panels.length}
    {#if report.layout}
      <ReportLayout
        {compact}
        node={report.layout}
        {panelsById}
        sources={report.sources}
        {now}
        {evidence}
        {tabSelections}
        oninspect={inspectPanel}
        {...refProps()}
        ontab={(id, value) => setFilter(`reportTab.${id}`, value)}
      />
    {/if}
    {#if remainingPanels.length}
      <div class="report-grid" class:report-layout-remainder={report.layout}>
        {#each remainingPanels as panel (panel.id)}
          <VisualReportPanel
            {compact}
            {panel}
            {now}
            sources={report.sources}
            freshness={getPanelFreshness(panel, now)}
            evidenceOpen={evidence === panel.id}
            oninspect={inspectPanel}
            {...refProps()}
          />
        {/each}
      </div>
    {/if}
  {:else}
    <div class="report-empty">
      <h3>No panels match these filters</h3>
      <p>Choose another project or freshness state to inspect the evidence.</p>
      <button type="button" onclick={() => setFilter("reportFreshness", "all")}
        >Show all freshness states</button
      >
    </div>
  {/if}
  {#if !compact}<p class="report-footnote">
      {hasLive
        ? "Live panels refresh from workspace data · Authored snapshots retain their observation time"
        : "Agent-assembled report · Source-linked claims · No automatic source refresh"}
    </p>{/if}
</section>

<!-- One preview layer for every chip in this report. -->
<AnxRefPreview bind:this={refPreview} />

<style>
  .visual-report {
    container: visual-report / inline-size;
    min-width: 0;
    overflow-wrap: anywhere;
    color: var(--fg);
  }
  .report-heading {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    align-items: flex-start;
    gap: 20px;
    padding-bottom: 24px;
  }
  .report-heading > div:first-child {
    flex: 1;
    min-width: min(100%, 240px);
  }
  .report-kicker {
    color: var(--accent-text);
    text-transform: uppercase;
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.14em;
  }
  .report-kicker span {
    color: var(--fg-muted);
  }
  .report-heading h2 {
    font-size: 25px;
    font-weight: 600;
    line-height: 1.25;
    letter-spacing: -0.04em;
    margin-top: 10px;
    overflow-wrap: anywhere;
  }
  .report-summary {
    color: var(--fg-muted);
    font-size: 12px;
    line-height: 1.7;
    max-width: 650px;
    margin-top: 10px;
  }
  .report-snapshot {
    display: flex;
    gap: 8px;
    color: var(--fg-muted);
    font-size: 10px;
    line-height: 1.8;
    padding-top: 2px;
  }
  .report-snapshot-dot {
    width: 6px;
    height: 6px;
    border: 1px solid var(--fg-muted);
    border-radius: 50%;
    margin-top: 6px;
  }
  .report-projects {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px;
  }
  .report-project {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    text-align: left;
    gap: 10px;
    padding: 16px;
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--bg-soft);
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .report-project:hover {
    background: var(--panel-hover);
  }
  .report-project-selected {
    border-color: var(--accent);
  }
  .report-project-name {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    width: 100%;
    color: var(--fg-muted);
    font-size: 11px;
  }
  .report-project strong {
    font-size: 17px;
    letter-spacing: -0.025em;
    font-weight: 500;
  }
  .report-project-summary {
    font-size: 11px;
    line-height: 1.6;
    color: var(--fg-muted);
  }
  .report-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 12px;
    padding: 20px 0 12px;
  }
  .report-all {
    border: 1px solid var(--line);
    border-radius: 4px;
    padding: 6px 10px;
    font-size: 11px;
  }
  .report-all[aria-pressed="true"] {
    background: var(--panel);
    border-color: var(--line-strong);
  }
  .report-filter-label {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--fg-muted);
    font-size: 11px;
  }
  .report-filter-label select {
    background: var(--bg-soft);
    border: 1px solid var(--line);
    color: var(--fg);
    border-radius: 4px;
    padding: 6px 24px 6px 8px;
  }
  .report-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 16px;
    align-items: start;
  }
  .report-layout-remainder {
    margin-top: 24px;
  }
  .report-footnote {
    text-align: center;
    color: var(--fg-muted);
    font-size: 10px;
    padding: 20px 0 4px;
  }
  .report-empty {
    padding: 40px 16px;
    border: 1px dashed var(--line-strong);
    text-align: center;
    color: var(--fg-muted);
  }
  .report-empty h3 {
    color: var(--fg);
    font-weight: 500;
  }
  .report-empty p {
    margin: 8px 0 16px;
    font-size: 12px;
  }
  .report-empty button {
    color: var(--accent-text);
    font-size: 12px;
  }
  @media (max-width: 800px) {
    .report-grid {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  @media (max-width: 520px) {
    .report-projects {
      grid-template-columns: minmax(0, 1fr);
    }
    .report-heading h2 {
      font-size: 22px;
    }
    .report-heading {
      gap: 12px;
    }
  }
</style>
