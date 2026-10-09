<script>
  import { getPanelFreshness } from "$lib/visualReports.js";
  import VisualReportPanel from "./VisualReportPanel.svelte";
  import ReportLayout from "./ReportLayout.svelte";
  import {
    gridPlacement,
    layoutContainsPanel,
    layoutHasVisiblePanels,
    selectedLayoutTab,
    visibleLayoutTabs,
  } from "./reportLayout.js";

  let {
    compact = false,
    node,
    panelsById,
    sources,
    now,
    evidence = "",
    tabSelections,
    /** Page-level ref resolution and preview handlers, forwarded to panels. */
    resolved = new Map(),
    organizationSlug = "",
    workspaceSlug = "",
    onpreview = null,
    onpreviewclose = null,
    oninspect,
    ontab,
    path = "root",
  } = $props();

  let visible = $derived(layoutHasVisiblePanels(node, panelsById));
  let children = $derived(
    (node.children ?? []).filter((child) =>
      layoutHasVisiblePanels(child, panelsById),
    ),
  );
  let tabs = $derived(visibleLayoutTabs(node, panelsById));
  let selectedTab = $derived(
    selectedLayoutTab(tabs, tabSelections.get(node.id), evidence),
  );
  let panel = $derived(panelsById.get(node.panel_id));
  // Placement for a grid, from content rather than from an authored column
  // count. Derived alongside `children` so a filter that hides the wide panel
  // in a row gives the rest of the row its columns back.
  let placement = $derived(
    node.type === "grid" ? gridPlacement(node, children, panelsById) : null,
  );
  let disclosureChoice = $state(null);
  let disclosureOpen = $derived(
    disclosureChoice ??
      (node.open === true || layoutContainsPanel(node, evidence)),
  );
  // Evidence links also reveal a disclosure on history navigation.
  $effect(() => {
    if (evidence && layoutContainsPanel(node, evidence))
      disclosureChoice = true;
  });
  const domId = (suffix) =>
    `report-layout-${encodeURIComponent(path)}-${encodeURIComponent(suffix)}`;

  function handleTabKey(event, item) {
    const index = tabs.indexOf(item);
    let target;
    if (event.key === "ArrowRight") target = (index + 1) % tabs.length;
    else if (event.key === "ArrowLeft")
      target = (index - 1 + tabs.length) % tabs.length;
    else if (event.key === "Home") target = 0;
    else if (event.key === "End") target = tabs.length - 1;
    else return;
    event.preventDefault();
    const buttons =
      event.currentTarget.parentElement.querySelectorAll('[role="tab"]');
    buttons[target]?.focus();
    ontab(node.id, tabs[target].id);
  }
</script>

{#snippet renderChild(child, childPath)}
  <ReportLayout
    {resolved}
    {organizationSlug}
    {workspaceSlug}
    {onpreview}
    {onpreviewclose}
    {compact}
    node={child}
    {panelsById}
    {sources}
    {now}
    {evidence}
    {tabSelections}
    {oninspect}
    {ontab}
    path={childPath}
  />
{/snippet}

{#if visible}
  {#if node.type === "panel" && panel}
    <div class="report-layout-panel" data-report-layout="panel">
      <VisualReportPanel
        {compact}
        {now}
        {panel}
        {sources}
        freshness={getPanelFreshness(panel, now)}
        evidenceOpen={evidence === panel.id}
        {oninspect}
        {resolved}
        {organizationSlug}
        {workspaceSlug}
        {onpreview}
        {onpreviewclose}
      />
    </div>
  {:else if node.type === "stack"}
    <div class="report-layout-stack" data-report-layout="stack">
      {#each children as child (child)}
        {@render renderChild(
          child,
          `${path}/child/${node.children.indexOf(child)}`,
        )}
      {/each}
    </div>
  {:else if node.type === "section"}
    <section
      class="report-layout-section"
      aria-labelledby={domId("heading")}
      data-report-layout="section"
    >
      <header class="report-layout-section-heading">
        <h3 id={domId("heading")}>{node.title}</h3>
        {#if node.description}<p>{node.description}</p>{/if}
      </header>
      <div class="report-layout-stack">
        {#each children as child (child)}
          {@render renderChild(
            child,
            `${path}/child/${node.children.indexOf(child)}`,
          )}
        {/each}
      </div>
    </section>
  {:else if node.type === "grid"}
    <div
      class="report-layout-grid"
      class:layout-fit={!placement.authored}
      class:layout-columns-2={placement.authored && placement.columns === 2}
      class:layout-columns-3={placement.authored && placement.columns === 3}
      class:layout-columns-4={placement.authored && placement.columns === 4}
      class:layout-cap-2={!placement.authored && placement.columns === 2}
      class:layout-cap-3={!placement.authored && placement.columns === 3}
      class:layout-cap-4={!placement.authored && placement.columns === 4}
      class:layout-fit-tight={!placement.authored && placement.tier === "tight"}
      class:layout-fit-text={!placement.authored && placement.tier === "text"}
      data-report-layout="grid"
      data-report-columns={placement.columns}
      data-report-placement={placement.authored ? "authored" : "fit"}
      data-report-fit={placement.authored ? undefined : placement.tier}
    >
      {#each children as child, index (child)}
        <div
          class="report-layout-cell {placement.cells[index].spanClass}"
          class:layout-full={placement.cells[index].full}
          data-report-cell={placement.cells[index].full ? "full" : "column"}
        >
          {@render renderChild(
            child,
            `${path}/child/${node.children.indexOf(child)}`,
          )}
        </div>
      {/each}
    </div>
  {:else if node.type === "tabs" && selectedTab}
    <div class="report-layout-tabs" data-report-layout="tabs">
      <div class="report-tab-list" role="tablist" aria-label="Report views">
        {#each tabs as item (item.id)}
          <button
            type="button"
            role="tab"
            id={domId(`tab-${item.id}`)}
            aria-controls={domId(`view-${item.id}`)}
            aria-selected={selectedTab.id === item.id}
            tabindex={selectedTab.id === item.id ? 0 : -1}
            onclick={() => ontab(node.id, item.id)}
            onkeydown={(event) => handleTabKey(event, item)}
            >{item.label}</button
          >
        {/each}
      </div>
      {#each tabs as item (item.id)}
        <div
          id={domId(`view-${item.id}`)}
          role="tabpanel"
          aria-labelledby={domId(`tab-${item.id}`)}
          tabindex="0"
          hidden={selectedTab.id !== item.id}
          class="report-tab-content"
        >
          {#if selectedTab.id === item.id}
            {#each item.children as child, index (child)}
              {@render renderChild(
                child,
                `${path}/tab/${encodeURIComponent(item.id)}/child/${index}`,
              )}
            {/each}
          {/if}
        </div>
      {/each}
    </div>
  {:else if node.type === "disclosure"}
    <details
      class="report-layout-disclosure"
      open={disclosureOpen}
      ontoggle={(event) => (disclosureChoice = event.currentTarget.open)}
      data-report-layout="disclosure"
    >
      <summary>{node.title}</summary>
      {#if disclosureOpen}
        <div class="report-layout-stack report-disclosure-content">
          {#each children as child (child)}
            {@render renderChild(
              child,
              `${path}/child/${node.children.indexOf(child)}`,
            )}
          {/each}
        </div>
      {/if}
    </details>
  {/if}
{/if}

<style>
  .report-layout-panel,
  .report-layout-stack,
  .report-layout-section,
  .report-layout-grid,
  .report-layout-cell,
  .report-layout-tabs,
  .report-layout-disclosure {
    min-width: 0;
    width: 100%;
  }
  .report-layout-stack {
    display: grid;
    /* An implicit column sizes to its widest child's max-content. */
    grid-template-columns: minmax(0, 1fr);
    gap: 24px;
  }
  .report-layout-section-heading {
    margin-bottom: 16px;
    padding: 2px 0 12px;
    border-bottom: 1px solid var(--line);
  }
  .report-layout-section-heading h3 {
    color: var(--fg);
    font-size: 18px;
    font-weight: 600;
    line-height: 1.35;
    letter-spacing: -0.025em;
  }
  .report-layout-section-heading p {
    margin-top: 6px;
    max-width: 80ch;
    color: var(--fg-muted);
    font-size: 12px;
    line-height: 1.7;
  }
  .report-layout-grid {
    display: grid;
    gap: 16px;
    align-items: start;
  }
  /*
   * Content-driven columns, which is what a grid gets unless a child asked for
   * an exact span.
   *
   * The track minimum is the widest minimum among the panels sharing the row
   * (`--report-fit-min`, set by the tier class). The floor is the width one
   * column would have at the grid's cap, so the grid never exceeds it.
   * `auto-fit` then reads the column count off the width actually available:
   * two columns when both panels clear their minimum, one when they do not.
   * No breakpoint names a device, and a grid nested in a section or a tab is
   * measured in the width it really has rather than the window's.
   */
  .report-layout-grid.layout-fit {
    /*
     * The `min(100%, ...)` is load-bearing. A minimum useful width is what a
     * panel reads at, not a promise the container can keep: in a column
     * narrower than the tier — the embedded dashboard on a phone — an
     * unclamped minimum makes the single track wider than the grid, and the
     * shell's own `overflow: hidden` then cuts the right edge off every panel
     * with no scrollbar to say so. Clamped, the panel simply gets the width
     * there is.
     *
     * The tier and cap defaults are `var()` fallbacks rather than
     * declarations here. As declarations they sit on
     * `.report-layout-grid.layout-fit` — two classes — and silently beat the
     * single-class `.layout-fit-tight` they are supposed to fall back to,
     * which made the 260px tier dead and stacked two small panels that had
     * room to sit side by side.
     */
    grid-template-columns: repeat(
      auto-fit,
      minmax(
        min(
          100%,
          max(var(--report-fit-min, 380px), var(--report-fit-floor, 50%))
        ),
        1fr
      )
    );
  }
  .layout-cap-2 {
    --report-fit-floor: calc((100% - 16px) / 2);
  }
  .layout-cap-3 {
    --report-fit-floor: calc((100% - 32px) / 3);
  }
  .layout-cap-4 {
    --report-fit-floor: calc((100% - 48px) / 4);
  }
  .layout-fit-tight {
    --report-fit-min: 260px;
  }
  .layout-fit-text {
    --report-fit-min: 380px;
  }
  /* Wide content — a plan graph, a chart, a real table — takes the row. */
  .report-layout-grid > .report-layout-cell.layout-full {
    grid-column: 1 / -1;
  }
  .layout-columns-2 {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .layout-columns-3 {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
  .layout-columns-4 {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
  .layout-span-1 {
    grid-column: span 1;
  }
  .layout-span-2 {
    grid-column: span 2;
  }
  .layout-span-3 {
    grid-column: span 3;
  }
  .layout-span-4 {
    grid-column: span 4;
  }
  .report-tab-list {
    display: flex;
    align-items: stretch;
    gap: 4px;
    overflow-x: auto;
    border-bottom: 1px solid var(--line);
    padding: 2px 2px 0;
  }
  .report-tab-list button {
    flex: 0 0 auto;
    max-width: 100%;
    padding: 10px 12px;
    border-bottom: 2px solid transparent;
    color: var(--fg-muted);
    font-size: 12px;
    font-weight: 500;
    overflow-wrap: anywhere;
  }
  .report-tab-list button:hover {
    background: var(--panel-hover);
    color: var(--fg);
  }
  .report-tab-list button[aria-selected="true"] {
    border-color: var(--accent);
    color: var(--fg);
  }
  .report-tab-content:not([hidden]) {
    display: grid;
    /* An implicit column sizes to its widest child's max-content. */
    grid-template-columns: minmax(0, 1fr);
    gap: 24px;
    padding-top: 20px;
  }
  .report-tab-content[hidden] {
    display: none;
  }
  .report-tab-content:focus-visible {
    outline: 1px solid var(--accent);
    outline-offset: 4px;
  }
  .report-layout-disclosure {
    border-top: 1px solid var(--line);
    border-bottom: 1px solid var(--line);
  }
  .report-layout-disclosure summary {
    padding: 14px 2px;
    color: var(--fg);
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
  }
  .report-layout-disclosure summary::marker {
    color: var(--fg-muted);
  }
  .report-disclosure-content {
    padding: 4px 0 20px;
  }
  /*
   * Authored grids only: an exact span needs exact tracks, so these keep the
   * collapse behaviour spans have always had. A content-driven grid resolves
   * its own column count above and must not be overridden here.
   */
  @container visual-report (max-width: 1000px) {
    .layout-columns-4 {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .layout-columns-4 > .layout-span-3,
    .layout-columns-4 > .layout-span-4 {
      grid-column: span 2;
    }
  }
  @container visual-report (max-width: 760px) {
    .report-layout-grid:not(.layout-fit) {
      grid-template-columns: minmax(0, 1fr);
    }
    .report-layout-grid:not(.layout-fit) > .report-layout-cell {
      grid-column: span 1;
    }
  }
  @media (max-width: 800px) {
    .report-layout-grid:not(.layout-fit) {
      grid-template-columns: minmax(0, 1fr);
    }
    .report-layout-grid:not(.layout-fit) > .report-layout-cell {
      grid-column: span 1;
    }
    .report-layout-stack,
    .report-tab-content:not([hidden]) {
      gap: 16px;
    }
  }
</style>
