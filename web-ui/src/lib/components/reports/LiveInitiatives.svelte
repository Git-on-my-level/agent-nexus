<script>
  /**
   * The initiatives section of the Overview, as a grid of tiles.
   *
   * One tile answers "what is the state and progress of this initiative" in a
   * glance: name, health, a one-line plain-text excerpt of the body, a
   * mini-viz sized to the plan's shape, and the next step.
   *
   * The order is the point. Tiles are sorted by attention — blocked, at risk,
   * stale, on track, done — so the top-left tile is always the one that most
   * needs a decision. Finished initiatives and ones with no plan are real but
   * are not what a dashboard is for, so they collapse at the bottom instead of
   * pushing live work off the first screen.
   *
   * Everything comes from the live initiatives projection the panel already
   * reads — see `initiativeTiles.js` for what that projection does and does not
   * carry, and why the real tree lives on the initiative page rather than here.
   */
  import { page } from "$app/stores";

  import { coreClient } from "$lib/coreClient";
  import { prefetchWork } from "$lib/workCache.js";
  import FreshnessBadge from "$lib/components/FreshnessBadge.svelte";
  import HealthBadge from "$lib/components/HealthBadge.svelte";
  import { groupedInitiativeTiles } from "$lib/initiativeTiles.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";

  let { items = [], now = Date.now() } = $props();

  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  /*
   * Pointing at a tile reads the card, so the click has nothing left to wait
   * for. Costs nothing when the card is already cached or already in flight.
   */
  function prefetch(ref) {
    if (ref) void prefetchWork(ref, coreClient);
  }

  let grouped = $derived(
    groupedInitiativeTiles(items, {
      now,
      href: (ref) => workspaceHref(`/tasks/${encodeURIComponent(ref)}`),
    }),
  );
</script>

{#snippet tileCard(tile)}
  <li>
    <a
      class="tile"
      href={tile.href}
      data-initiative-tile={tile.ref}
      data-tile-health={tile.health.state || "unknown"}
      onpointerenter={() => prefetch(tile.ref)}
      onfocus={() => prefetch(tile.ref)}
    >
      <!--
        Title first, on a line of its own. The status pill and the freshness
        badge used to sit beside it, which left a long initiative name
        competing for width with two things that are each two words wide. They
        get their own line underneath now, where neither squeezes the other.
      -->
      <span class="tile-head">
        <span class="tile-title">{tile.title}</span>
      </span>

      {#if tile.showHealth || tile.freshness}
        <span class="tile-status" data-tile-status>
          {#if tile.showHealth}
            <HealthBadge health={tile.health} variant="pill" />
          {/if}
          {#if tile.freshness}
            <FreshnessBadge
              at={tile.movedAt}
              kind={tile.freshnessKind}
              row={tile}
              verb="moved"
              {now}
            />
          {/if}
        </span>
      {/if}

      {#if tile.excerpt}
        <!-- A plain line. The body is markdown; a tile that rendered it
             verbatim read `**Goal:** …`. -->
        <span class="tile-excerpt" data-tile-excerpt>{tile.excerpt}</span>
      {/if}

      {#if tile.needs.length}
        <!-- A pill on the initiative the decision belongs to, rather than a
             second copy of the Inbox on the dashboard. -->
        <span class="tile-needs" data-tile-needs
          >Needs you: {tile.needs[0]}{tile.needs.length > 1
            ? ` +${tile.needs.length - 1}`
            : ""}</span
        >
      {/if}

      {#if tile.segments.length}
        <!--
          One row of segments, in plan order: the mockup's bar. A tile used to
          draw the plan's real shape in miniature — a column per dependency
          layer for a tree — which at tile size became a block of colour tall
          enough to crowd everything under it, and said less than the word
          "tech tree" already says in the line below. The shape stays named;
          the graph itself is one click away on the initiative page.
        -->
        <span class="tile-viz" data-tile-shape={tile.shape}>
          <span
            class="tile-bar"
            style:--tile-segments={tile.segments.length}
            role="img"
            aria-label={`${tile.title} checklist: ${tile.progress?.done ?? 0} of ${tile.progress?.total ?? 0} steps done`}
          >
            {#each tile.segments as segment (segment.id)}
              <span
                class="seg"
                data-status={segment.status}
                class:seg--critical={segment.onCriticalPath}
              ></span>
            {/each}
          </span>
          <span class="tile-meta">
            {#if tile.progress}<span class="tile-count"
                >{tile.progress.done}/{tile.progress.total}</span
              >{/if}
            {#if tile.shapeLabel}
              <span class="tile-shape">{tile.shapeLabel}</span>
            {/if}
            {#if tile.overflow}<span>+{tile.overflow} more</span>{/if}
          </span>
        </span>
      {:else if tile.progress}
        <span class="tile-viz">
          <progress
            value={tile.progress.done}
            max={tile.progress.total}
            aria-label={`${tile.title} checklist`}
          ></progress>
          <span class="tile-meta"
            ><span class="tile-count"
              >{tile.progress.done}/{tile.progress.total}</span
            ></span
          >
        </span>
      {/if}

      <!--
        What landed, what is moving, what is next. Three short lists instead of
        one "Next:" line, because the question a reader opens the Overview with
        is "where is this" and a single next step answers a third of it. Core
        bounds each list to three rows and counts the rest.
      -->
      {#if tile.steps?.groups.length}
        <span class="tile-steps" data-tile-steps>
          {#each tile.steps.groups as list (list.key)}
            <span class="step-label" data-tile-step-group={list.key}
              >{list.label}</span
            >
            <span class="step-rows">
              {#each list.items as step (step.key)}
                <span class="step-row" data-tile-step={step.id}>
                  <span class="step-title">{step.title}</span>
                  {#if step.status === "blocked"}
                    <span class="step-flag" data-tile-step-blocked>blocked</span
                    >
                  {/if}
                  {#if step.age}
                    <time
                      class="step-age"
                      datetime={step.at}
                      title={step.ageTitle}>{step.age}</time
                    >
                  {/if}
                </span>
              {/each}
              {#if list.more}
                <span class="step-row step-more" data-tile-step-more={list.key}
                  >+{list.more} more</span
                >
              {/if}
            </span>
          {/each}
        </span>
      {:else if tile.next}
        <!-- An older core computes no step lists; the one next step it does
             compute is still worth a line. -->
        <span class="tile-foot">
          <span class="tile-next" data-tile-next
            >Next: {tile.next.title}{tile.next.extra
              ? ` +${tile.next.extra}`
              : ""}</span
          >
        </span>
      {/if}
    </a>
  </li>
{/snippet}

{#if grouped.attention.length}
  <ul
    class="tiles"
    aria-label="Open initiatives"
    data-initiative-group="attention"
  >
    {#each grouped.attention as tile (tile.ref)}
      {@render tileCard(tile)}
    {/each}
  </ul>
{:else if !grouped.done.length && !grouped.noPlan.length}
  <p class="tiles-empty">No open initiatives.</p>
{/if}

{#if grouped.done.length}
  <details class="tiles-fold" data-initiative-group="done">
    <summary>Done <span>({grouped.done.length})</span> </summary>
    <ul class="tiles" aria-label="Finished initiatives">
      {#each grouped.done as tile (tile.ref)}
        {@render tileCard(tile)}
      {/each}
    </ul>
  </details>
{/if}

{#if grouped.noPlan.length}
  <details class="tiles-fold" data-initiative-group="no-plan">
    <summary>No plan <span>({grouped.noPlan.length})</span> </summary>
    <ul class="tiles" aria-label="Initiatives with no plan">
      {#each grouped.noPlan as tile (tile.ref)}
        {@render tileCard(tile)}
      {/each}
    </ul>
  </details>
{/if}

<style>
  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(17rem, 1fr));
    gap: 10px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .tiles-empty {
    margin: 0;
    color: var(--fg-muted);
    font-size: 12px;
  }
  /* Done and planless initiatives: present, counted, out of the way. */
  .tiles-fold {
    margin-top: 10px;
    border-top: 1px solid var(--line-subtle);
  }
  .tiles-fold summary {
    cursor: pointer;
    padding: 8px 0;
    color: var(--fg-muted);
    font-size: 11px;
  }
  .tiles-fold summary span {
    color: var(--fg-subtle);
  }
  .tiles-fold[open] summary {
    margin-bottom: 8px;
  }
  .tile {
    display: grid;
    /* An implicit column sizes to its widest child's max-content. */
    grid-template-columns: minmax(0, 1fr);
    /* Mockup density: 18px padding, 12px internal gap, 12px radius. */
    gap: 12px;
    align-content: start;
    height: 100%;
    padding: 18px;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--panel);
    color: var(--fg);
  }
  .tile:hover {
    border-color: var(--line-strong);
    background: var(--panel-hover, var(--bg-soft));
  }
  .tile:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: 2px;
  }
  /* The worst state gets an edge, so the sort is visible and not just true. */
  .tile[data-tile-health="blocked"] {
    border-left: 3px solid var(--danger-text, var(--warn-text));
  }
  .tile[data-tile-health="at_risk"],
  .tile[data-tile-health="stale"] {
    border-left: 3px solid var(--warn-text);
  }
  .tile-head {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }
  /* The status line: everything that used to crowd the title. */
  .tile-status {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    margin-top: -6px;
  }
  .tile-title {
    min-width: 0;
    font-size: 15px;
    font-weight: 600;
    line-height: 1.3;
    overflow-wrap: anywhere;
  }
  .tile:hover .tile-title {
    color: var(--accent-text);
  }
  .tile-excerpt {
    color: var(--fg-muted);
    font-size: 13px;
    line-height: 1.5;
    /* Two lines: a tile is a glance, not the card body. */
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .tile-needs {
    justify-self: start;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--bg-soft);
    color: var(--warn-text);
    font-size: 11px;
    overflow-wrap: anywhere;
  }
  .tile-viz {
    display: grid;
    /* An implicit column sizes to its widest child's max-content. */
    grid-template-columns: minmax(0, 1fr);
    gap: 6px;
  }
  /*
   * One row, one column per step, each the same width — the mockup's bar. The
   * count of steps comes in as a custom property so the track widths are the
   * grid's job rather than flex rounding's.
   */
  .tile-bar {
    display: grid;
    grid-template-columns: repeat(var(--tile-segments, 1), minmax(0, 1fr));
    gap: 3px;
  }
  .seg {
    height: 8px;
    min-width: 2px;
    border-radius: 2px;
    background: var(--line-strong);
  }
  .seg[data-status="done"] {
    background: var(--ok-text, var(--accent-solid));
  }
  .seg[data-status="active"] {
    background: var(--accent-solid);
  }
  .seg[data-status="blocked"] {
    background: var(--warn-text);
  }
  /* The spine of the plan, shown as weight rather than another colour. */
  .seg--critical {
    box-shadow: inset 0 0 0 1px var(--fg-muted);
  }
  .tile-viz progress {
    width: 100%;
    height: 6px;
    border: 0;
    border-radius: 3px;
    background: var(--bg-soft);
    accent-color: var(--accent-solid);
  }
  .tile-meta,
  .tile-foot {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 3px 8px;
    color: var(--fg-muted);
    font-size: 12.5px;
  }
  .tile-count {
    font-variant-numeric: tabular-nums;
  }
  .tile-foot {
    gap: 3px 6px;
  }
  .tile-shape {
    text-transform: lowercase;
  }
  .tile-next {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  /*
   * The three lists: a short label, then its rows beside it. One grid, so the
   * labels line up down the tile and the rows share one left edge — on a phone
   * as well, where a stacked label per group would cost three lines to say
   * three words.
   */
  .tile-steps {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: 4px 10px;
    align-items: baseline;
    font-size: 12.5px;
  }
  .step-label {
    color: var(--fg-subtle, var(--fg-muted));
    font-size: 11px;
    line-height: 1.5;
    white-space: nowrap;
  }
  .step-rows {
    display: grid;
    gap: 1px;
    min-width: 0;
  }
  .step-row {
    display: flex;
    align-items: baseline;
    gap: 6px;
    min-width: 0;
    color: var(--fg-muted);
    line-height: 1.4;
  }
  .step-title {
    min-width: 0;
    /* One line per step. A tile is a glance; the plan is one click away. */
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .step-flag {
    flex: none;
    color: var(--warn-text);
    font-size: 11px;
  }
  .step-age {
    flex: none;
    margin-left: auto;
    color: var(--fg-subtle, var(--fg-muted));
    font-size: 11px;
    font-variant-numeric: tabular-nums;
  }
  .step-more {
    color: var(--fg-subtle, var(--fg-muted));
    font-size: 11px;
  }
</style>
