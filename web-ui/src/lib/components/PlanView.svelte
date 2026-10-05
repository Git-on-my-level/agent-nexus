<script>
  /**
   * An initiative plan, rendered in the shape the plan itself has: a chain is a
   * timeline, a DAG is a tech tree, separate runs are lanes. Agents never pick
   * the view — adding one step with `after:` is what turns a timeline into a
   * tree — so `planShape.js` decides and this only draws.
   *
   * Accessibility is not a separate mode. Nodes are the same ref chips
   * everywhere, in declaration order in the DOM, so they are focusable and
   * readable in every shape; only the SVG edge layer is hidden from assistive
   * tech, and the dependencies it draws are also written out in the step list
   * below. That list is the fallback the tech tree needs, and it is the same
   * markup `dependency-diagram` panels already render.
   */
  import AnxRefChip from "$lib/components/AnxRefChip.svelte";
  import {
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
    onpreview = null,
    onpreviewclose = null,
  } = $props();

  let layout = $derived(planLayout(plan, { planState, resolved, now }));
  let nodeById = $derived(new Map(layout.nodes.map((node) => [node.id, node])));
  let geometry = $derived(planTreeGeometry(layout));
  let geometryById = $derived(
    new Map(geometry.nodes.map((node) => [node.id, node])),
  );

  /** Timeline and lane order follow the layers, which is dependency order. */
  let chainOrder = $derived(
    layout.layers
      .flat()
      .concat(layout.cyclic)
      .map((id) => nodeById.get(id)),
  );
  let markerIndex = $derived(todayMarkerIndex(chainOrder, now));

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
</script>

{#if !layout.nodes.length}
  <p class="plan-empty">No plan yet.</p>
{:else}
  <div class="plan" data-plan-shape={shape}>
    {#if shape === "dag"}
      <!-- Horizontal scroll on a narrow screen; the region takes focus so it
           can be scrolled from the keyboard. -->
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div
        class="plan-tree-scroll"
        tabindex="0"
        role="region"
        aria-label="Plan diagram"
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
              />
            {/each}
          </svg>
          {#each layout.nodes as node (node.id)}
            {@const box = geometryById.get(node.id)}
            <div
              class="plan-node"
              class:plan-node--critical={node.onCriticalPath}
              data-status={node.status}
              style:left="{box.x}px"
              style:top="{box.y}px"
              style:width="{box.width}px"
              style:height="{box.height}px"
            >
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
                <span class="plan-node__title">{node.title}</span>
              {/if}
              <span class="plan-node__status">
                {statusLabel(node.status)}
              </span>
            </div>
          {/each}
        </div>
      </div>
    {:else if shape === "lanes"}
      <div class="plan-lanes">
        {#each layout.lanes as lane, index (lane.join("+"))}
          <section class="plan-lane" aria-label={`Lane ${index + 1}`}>
            <ol class="plan-track">
              {#each laneSteps(lane) as node (node.id)}
                <li data-status={node.status}>
                  <span class="plan-track__dot" aria-hidden="true"></span>
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
                    <span class="plan-node__title">{node.title}</span>
                  {/if}
                  <span class="plan-node__status"
                    >{statusLabel(node.status)}</span
                  >
                </li>
              {/each}
            </ol>
          </section>
        {/each}
      </div>
    {:else}
      <ol class="plan-track plan-track--timeline">
        {#each chainOrder as node, index (node.id)}
          {#if index === markerIndex}
            <li class="plan-today" aria-label="Today">
              <span class="plan-today__line" aria-hidden="true"></span>
              <span class="plan-today__label">Today</span>
            </li>
          {/if}
          <li
            data-status={node.status}
            class:plan-critical={node.onCriticalPath}
          >
            <span class="plan-track__dot" aria-hidden="true"></span>
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
              <span class="plan-node__title">{node.title}</span>
            {/if}
            <span class="plan-node__status">{statusLabel(node.status)}</span>
            {#if node.due}<span class="plan-node__due">{node.due}</span>{/if}
          </li>
        {/each}
        {#if markerIndex === chainOrder.length}
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
      The step list: the tech tree's screen-reader fallback, and the only place
      the dependencies drawn as edges are written out as words.
    -->
    <details class="plan-steps">
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
  .plan-node {
    position: absolute;
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: 2px;
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
    color: var(--fg);
  }
  .plan-node__status,
  .plan-node__due,
  .plan-node__after {
    color: var(--fg-muted);
    font-size: 10px;
  }

  .plan-lanes {
    display: grid;
    gap: 10px;
  }
  .plan-track {
    display: grid;
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
  .plan-track__dot {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--line-strong);
  }
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

  @media (max-width: 640px) {
    .plan-node__status {
      display: none;
    }
  }
</style>
