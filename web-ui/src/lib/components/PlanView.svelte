<script>
  /**
   * An initiative plan, rendered in the shape the plan itself has: a chain is a
   * timeline, a DAG is a tech tree, separate runs are lanes. Agents never pick
   * the view — adding one step with `after:` is what turns a timeline into a
   * tree — so `planShape.js` decides and this only draws.
   *
   * What a node says: the **step's title**, with its ref as a chip beneath it.
   * A chip alone used to be the whole node, which meant a reader looking for
   * "pick the launch date" saw `card:launch-date` — or, for an external step,
   * a raw GitHub URL. The title is what the author wrote; the chip is where it
   * points, and whether that thing is open or merged.
   *
   * What it hides: a plan's finished prefix. `planTreeGeometry` folds leading
   * all-done layers into one summary column and shrinks the rest to the width
   * the page actually has, so the remaining steps and the critical path are on
   * screen rather than past the right edge.
   *
   * Accessibility is not a separate mode. Nodes are in declaration order in
   * the DOM and the titles are real text, so they are focusable and readable in
   * every shape; only the SVG edge layer is hidden from assistive tech, and the
   * dependencies it draws are also written out in the step list below. That
   * list is the fallback the tech tree needs, and it is the same markup
   * `dependency-diagram` panels already render — it also still lists the
   * collapsed steps, so nothing is hidden from it.
   */
  import AnxRefChip from "$lib/components/AnxRefChip.svelte";
  import {
    PLAN_COLLAPSE_MIN,
    planLayout,
    planTreeGeometry,
    todayMarkerIndex,
  } from "$lib/planShape.js";

  let {
    /** The authored plan from the card: `{ steps: [...] }`. */
    plan = null,
    /**
     * Core's computed `plan_state`. Authoritative when present — it sees source
     * activity and access scoping the browser cannot — and the renderer falls
     * back to deriving the same values when it is absent.
     */
    planState = null,
    /** Page-level batch resolve result, from `indexResolvedRefs`. */
    resolved = new Map(),
    organizationSlug = "",
    workspaceSlug = "",
    /** Reference time, injectable so the today marker is testable. */
    now = Date.now(),
    /** Expand the full checklist in report and document previews. */
    stepsExpanded = false,
    onpreview = null,
    onpreviewclose = null,
  } = $props();

  let layout = $derived(planLayout(plan, { planState, resolved, now }));
  let nodeById = $derived(new Map(layout.nodes.map((node) => [node.id, node])));

  /**
   * How much room the diagram has. Measured rather than assumed: the fit is
   * the difference between "the critical path is on screen" and "it is three
   * columns past the right edge", and the same plan has to fit a phone and a
   * wide monitor.
   */
  let frameWidth = $state(0);
  let geometry = $derived(
    planTreeGeometry(layout, { availableWidth: frameWidth }),
  );
  /**
   * Wider than the room it has. `planTreeGeometry` shrinks to a legible floor
   * first, so this is only true once shrinking has run out — the one case the
   * diagram is allowed to scroll sideways.
   */
  let scrollable = $derived(frameWidth > 0 && geometry.width > frameWidth + 1);
  /** Timeline and lane order follow the layers, which is dependency order. */
  let chainOrder = $derived(
    layout.layers
      .flat()
      .concat(layout.cyclic)
      .map((id) => nodeById.get(id)),
  );

  /**
   * A timeline's finished prefix folds the same way a tree's does, and for the
   * same reason: a 12-step plan with 9 done is three steps of news.
   */
  let chainView = $derived.by(() => {
    let prefix = 0;
    while (
      prefix < chainOrder.length &&
      chainOrder[prefix]?.status === "done"
    ) {
      prefix += 1;
    }
    // Nothing left to show means the plan is finished; draw it in full. And
    // folding one step saves nothing, so it takes two.
    if (prefix < PLAN_COLLAPSE_MIN || prefix === chainOrder.length) {
      return { doneCount: 0, steps: chainOrder };
    }
    return { doneCount: prefix, steps: chainOrder.slice(prefix) };
  });
  let markerIndex = $derived(todayMarkerIndex(chainView.steps, now));

  const STATUS_LABELS = {
    done: "Done",
    active: "In progress",
    blocked: "Blocked",
    not_started: "Not started",
  };
  /** Core computes status; an uncomputed step says so rather than guessing. */
  const statusLabel = (status) => STATUS_LABELS[status] ?? "Status unknown";

  /**
   * The shape core computed picks the view. With no computed shape — an unsaved
   * plan, or a `dependency-diagram` panel — the timeline is the fallback,
   * because it is the view that assumes least about an unclassified graph.
   */
  let shape = $derived(layout.shape || "chain");

  function laneSteps(lane) {
    // Dependency order, then anything the layering could not place. A step must
    // never vanish because it could not be ordered.
    const inLane = new Set(lane);
    const ordered = layout.layers.flat().filter((id) => inLane.has(id));
    const placed = new Set(ordered);
    return ordered
      .concat(lane.filter((id) => !placed.has(id)))
      .map((id) => nodeById.get(id));
  }

  function dependencyTitles(node) {
    return node.after.map((id) => nodeById.get(id)?.title ?? id);
  }

  const doneSummary = (count) =>
    `${count} ${count === 1 ? "step" : "steps"} done`;
</script>

{#snippet stepBody(node)}
  <!-- Title first, ref beneath: the words the author wrote, then where they
       point. A step with no ref is just its title. -->
  <span class="plan-node__title" title={node.title}>{node.title}</span>
  <span class="plan-node__meta">
    {#if node.ref}
      <AnxRefChip
        refValue={node.ref}
        {resolved}
        {organizationSlug}
        {workspaceSlug}
        showKind={false}
        {onpreview}
        {onpreviewclose}
      />
    {:else}
      <span class="plan-node__status">{statusLabel(node.status)}</span>
    {/if}
  </span>
{/snippet}

{#if !layout.nodes.length}
  <p class="plan-empty">No plan yet.</p>
{:else}
  <div class="plan" data-plan-shape={shape}>
    {#if shape === "dag"}
      <!--
        Horizontal scroll only when the fit ran out of room, and when it does,
        the box says so. A diagram that runs past the right edge with no
        scrollbar in sight reads as broken rather than as scrollable — which is
        exactly how it read embedded in a dashboard column. The hint names the
        gesture, and the region takes focus so the keyboard can scroll it too.
        Said in words rather than with an edge gradient: the gradient would
        have to overlay the scroller, and the last node once you reached it.
      -->
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div
        class="plan-tree-scroll"
        class:plan-tree-scroll--scrollable={scrollable}
        data-plan-scrollable={scrollable ? "true" : "false"}
        bind:clientWidth={frameWidth}
        tabindex="0"
        role="region"
        aria-label={scrollable
          ? "Plan diagram — scrolls sideways"
          : "Plan diagram"}
      >
        <div
          class="plan-tree"
          style:width="{geometry.width}px"
          style:height="{geometry.height}px"
        >
          <svg
            class="plan-tree__edges"
            width={geometry.width}
            height={geometry.height}
            aria-hidden="true"
            focusable="false"
          >
            {#each geometry.edges as edge (`${edge.from}->${edge.to}`)}
              <path
                d={edge.path}
                class="plan-edge"
                class:plan-edge--critical={edge.onCriticalPath}
                class:plan-edge--from-collapsed={edge.fromCollapsed}
              />
            {/each}
          </svg>
          {#if geometry.collapsed}
            <!-- The finished prefix, as one column. The steps are still in the
                 list below, so nothing is lost by folding them. -->
            <div
              class="plan-done-column"
              data-plan-done-count={geometry.collapsed.count}
              style:left="{geometry.collapsed.x}px"
              style:top="{geometry.collapsed.y}px"
              style:width="{geometry.collapsed.width}px"
              style:height="{geometry.collapsed.height}px"
              title={`${doneSummary(geometry.collapsed.count)} — listed under View plan steps`}
            >
              <span class="plan-done-column__check" aria-hidden="true">✓</span>
              <span class="plan-done-column__count"
                >{geometry.collapsed.count}</span
              >
              <span class="plan-done-column__label">done</span>
            </div>
          {/if}
          {#each geometry.nodes as box (box.id)}
            {@const node = nodeById.get(box.id)}
            <div
              class="plan-node"
              class:plan-node--critical={node.onCriticalPath}
              data-status={node.status}
              data-plan-node={node.id}
              style:left="{box.x}px"
              style:top="{box.y}px"
              style:width="{box.width}px"
              style:height="{box.height}px"
            >
              {@render stepBody(node)}
            </div>
          {/each}
        </div>
      </div>
      {#if scrollable}
        <p class="plan-scroll-hint" data-plan-scroll-hint>
          <span aria-hidden="true">↔</span>
          Scroll to see the rest of the plan
        </p>
      {/if}
    {:else if shape === "lanes"}
      <div class="plan-lanes">
        {#each layout.lanes as lane, index (lane.join("+"))}
          <section class="plan-lane" aria-label={`Lane ${index + 1}`}>
            <ol class="plan-track">
              {#each laneSteps(lane) as node (node.id)}
                <li data-status={node.status} data-plan-node={node.id}>
                  <span class="plan-track__dot" aria-hidden="true"></span>
                  <span class="plan-track__body">{@render stepBody(node)}</span>
                </li>
              {/each}
            </ol>
          </section>
        {/each}
      </div>
    {:else}
      <ol class="plan-track plan-track--timeline">
        {#if chainView.doneCount}
          <li class="plan-done-row" data-plan-done-count={chainView.doneCount}>
            <span
              class="plan-track__dot plan-track__dot--done"
              aria-hidden="true"
            ></span>
            <span class="plan-done-row__label"
              >{doneSummary(chainView.doneCount)}</span
            >
          </li>
        {/if}
        {#each chainView.steps as node, index (node.id)}
          {#if index === markerIndex}
            <li class="plan-today" aria-label="Today">
              <span class="plan-today__line" aria-hidden="true"></span>
              <span class="plan-today__label">Today</span>
            </li>
          {/if}
          <li
            data-status={node.status}
            data-plan-node={node.id}
            class:plan-critical={node.onCriticalPath}
          >
            <span class="plan-track__dot" aria-hidden="true"></span>
            <span class="plan-track__body">
              {@render stepBody(node)}
              {#if node.due}<span class="plan-node__due">{node.due}</span>{/if}
            </span>
          </li>
        {/each}
        {#if markerIndex === chainView.steps.length}
          <li class="plan-today" aria-label="Today">
            <span class="plan-today__line" aria-hidden="true"></span>
            <span class="plan-today__label">Today</span>
          </li>
        {/if}
      </ol>
    {/if}

    {#if layout.issues.length}
      <ul class="plan-issues">
        {#each layout.issues as issue (issue)}
          <li>{issue}</li>
        {/each}
      </ul>
    {/if}

    <!--
      The step list: the tech tree's screen-reader fallback, the only place the
      dependencies drawn as edges are written out as words, and the place the
      collapsed done steps are still listed in full.
    -->
    <details class="plan-steps" open={stepsExpanded}>
      <summary>View plan steps <span>({layout.nodes.length})</span></summary>
      <ol>
        {#each layout.nodes as node (node.id)}
          <li>
            <span class="plan-node__title">{node.title}</span>
            <span class="plan-node__status">{statusLabel(node.status)}</span>
            {#if node.due}<span class="plan-node__due">due {node.due}</span
              >{/if}
            {#if node.after.length}
              <span class="plan-node__after"
                >after {dependencyTitles(node).join(", ")}</span
              >
            {/if}
            {#if node.onCriticalPath}
              <span class="plan-node__after">on the critical path</span>
            {/if}
          </li>
        {/each}
      </ol>
    </details>
  </div>
{/if}

<style>
  .plan-empty {
    color: var(--fg-muted);
    font-size: 12px;
  }
  .plan-tree-scroll {
    overflow-x: auto;
    padding-bottom: 4px;
  }
  .plan-scroll-hint {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-top: 4px;
    color: var(--fg-subtle, var(--fg-muted));
    font-size: 10px;
  }
  .plan-tree-scroll:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: 2px;
  }
  .plan-tree {
    position: relative;
    min-width: 100%;
  }
  .plan-tree__edges {
    position: absolute;
    inset: 0;
    overflow: visible;
  }
  .plan-edge {
    fill: none;
    stroke: var(--line-strong);
    stroke-width: 1.5;
  }
  .plan-edge--critical {
    stroke: var(--accent-solid);
    stroke-width: 2.5;
  }
  /* An edge out of the folded prefix: real, but not the reader's problem. */
  .plan-edge--from-collapsed {
    stroke-dasharray: 3 3;
    opacity: 0.6;
  }
  .plan-node {
    position: absolute;
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: 3px;
    box-sizing: border-box;
    padding: 6px 8px;
    border: 1px solid var(--line);
    border-radius: 5px;
    background: var(--panel);
    overflow: hidden;
  }
  .plan-node--critical {
    border-color: var(--accent-solid);
  }
  .plan-node[data-status="done"] {
    border-left: 3px solid var(--ok-text, var(--accent-solid));
  }
  .plan-node[data-status="active"] {
    border-left: 3px solid var(--accent-solid);
  }
  .plan-node[data-status="blocked"] {
    border-left: 3px solid var(--warn-text);
  }
  .plan-node[data-status="not_started"] {
    border-left: 3px solid var(--line-strong);
  }
  .plan-node__title {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 12px;
    font-weight: 500;
    color: var(--fg);
  }
  /* The chip sits under the title and may not fit: clip it rather than let it
     widen the node and break the column grid. */
  .plan-node__meta {
    display: flex;
    min-width: 0;
    align-items: center;
    gap: 4px;
    overflow: hidden;
  }
  .plan-node__status,
  .plan-node__due,
  .plan-node__after {
    color: var(--fg-muted);
    font-size: 10px;
  }

  /* The folded finished prefix. */
  .plan-done-column {
    position: absolute;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 1px;
    box-sizing: border-box;
    padding: 6px 4px;
    border: 1px dashed var(--line-strong);
    border-radius: 5px;
    background: var(--bg-soft);
    color: var(--fg-muted);
    cursor: help;
  }
  .plan-done-column__check {
    color: var(--ok-text, var(--accent-solid));
    font-size: 12px;
    line-height: 1;
  }
  .plan-done-column__count {
    font-size: 13px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    color: var(--fg);
  }
  .plan-done-column__label {
    font-size: 10px;
  }
  .plan-done-row {
    color: var(--fg-muted);
    font-size: 11px;
  }
  .plan-done-row__label {
    color: var(--fg-muted);
  }

  .plan-lanes {
    display: grid;

    /* An implicit column sizes to its widest child's max-content. */

    grid-template-columns: minmax(0, 1fr);
    gap: 10px;
  }
  .plan-track {
    display: grid;
    /* An implicit column sizes to its widest child's max-content. */
    grid-template-columns: minmax(0, 1fr);
    gap: 8px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .plan-track li {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .plan-track__body {
    display: flex;
    min-width: 0;
    flex-wrap: wrap;
    align-items: center;
    gap: 2px 8px;
  }
  .plan-track__dot {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--line-strong);
  }
  .plan-track__dot--done,
  .plan-track li[data-status="done"] .plan-track__dot {
    background: var(--ok-text, var(--accent-solid));
  }
  .plan-track li[data-status="active"] .plan-track__dot {
    background: var(--accent-solid);
  }
  .plan-track li[data-status="blocked"] .plan-track__dot {
    background: var(--warn-text);
  }
  .plan-today {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .plan-today__line {
    flex: 1;
    height: 1px;
    background: var(--accent-solid);
  }
  .plan-today__label {
    flex: none;
    color: var(--accent-text);
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .plan-issues {
    margin: 10px 0 0;
    padding-left: 18px;
    color: var(--warn-text);
    font-size: 11px;
  }
  .plan-steps {
    margin-top: 12px;
    border-top: 1px solid var(--line-subtle);
  }
  .plan-steps summary {
    cursor: pointer;
    padding: 8px 0;
    color: var(--fg-muted);
    font-size: 11px;
  }
  .plan-steps summary span {
    color: var(--fg-subtle);
  }
  .plan-steps ol {
    display: grid;
    /* An implicit column sizes to its widest child's max-content. */
    grid-template-columns: minmax(0, 1fr);
    gap: 6px;
    margin: 0 0 8px;
    padding-left: 18px;
    font-size: 11px;
  }
  .plan-steps li {
    display: flex;
    flex-wrap: wrap;
    gap: 2px 8px;
    min-width: 0;
  }

  /* On a phone the node's own status word is the first thing to go: the dot
     and the column already carry it. */
  @media (max-width: 640px) {
    .plan-track__body .plan-node__status {
      display: none;
    }
  }
</style>
