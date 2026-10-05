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

  import AgeBadge from "$lib/components/AgeBadge.svelte";
  import HealthBadge from "$lib/components/HealthBadge.svelte";
  import { groupedInitiativeTiles } from "$lib/initiativeTiles.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";

  let { items = [], now = Date.now() } = $props();

  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
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
    >
      <span class="tile-head">
        <span class="tile-title">{tile.title}</span>
        <!-- The mockup's status pill: a short word, not a glyph. It reads at a
             glance without a hover, and the vocabulary is short enough
             ("Blocked", "At risk", "Stale", "On track") that it never needs
             truncating — which was the reason the glyph existed. -->
        <HealthBadge health={tile.health} variant="pill" />
      </span>

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
            data-viz-kind={tile.viz.kind}
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

      <!-- One meta line, the way the mockup writes it: "Next: … · moved 2h
           ago". Two separate blocks made the tile's last two rows compete. -->
      <span class="tile-foot">
        {#if tile.next}
          <span class="tile-next" data-tile-next
            >Next: {tile.next.title}{tile.next.extra
              ? ` +${tile.next.extra}`
              : ""}</span
          >
        {/if}
        {#if tile.next && tile.movedAt}
          <span class="tile-sep" aria-hidden="true">·</span>
        {/if}
        {#if tile.movedAt}
          <AgeBadge at={tile.movedAt} verb="moved" {now} />
        {/if}
      </span>
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
    justify-content: space-between;
    gap: 8px;
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
  .tile-sep {
    color: var(--fg-subtle, var(--fg-muted));
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
</style>
