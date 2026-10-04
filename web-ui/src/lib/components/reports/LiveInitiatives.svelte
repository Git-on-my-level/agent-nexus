<script>
  /**
   * The initiatives section of the Overview, as a grid of tiles.
   *
   * One tile answers "what is the state and progress of this initiative" in a
   * glance: name, health, a one-line status, a mini-viz sized to the plan's
   * shape, and what is next. The whole tile is the link to the initiative page.
   *
   * Everything comes from the live initiatives projection the panel already
   * reads — see `initiativeTiles.js` for what that projection does and does not
   * carry, and why the real tree lives on the initiative page rather than here.
   */
  import { page } from "$app/stores";

  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
  import { initiativeTiles } from "$lib/initiativeTiles.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";

  let { items = [], now = Date.now() } = $props();

  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let tiles = $derived(
    initiativeTiles(items, {
      now,
      href: (ref) => workspaceHref(`/tasks/${encodeURIComponent(ref)}`),
    }),
  );
</script>

<ul class="tiles" aria-label="Open initiatives">
  {#each tiles as tile (tile.ref)}
    <li>
      <a class="tile" href={tile.href} data-initiative-tile={tile.ref}>
        <span class="tile-head">
          <span class="tile-title">{tile.title}</span>
          {#if tile.healthLabel}
            <SignalBadge tone={tile.healthTone}>{tile.healthLabel}</SignalBadge>
          {/if}
        </span>

        {#if tile.status}
          <span class="tile-status">{tile.status}</span>
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
          <span class="tile-viz" data-tile-shape={tile.shape}>
            <span
              class="tile-bar"
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
              {#if tile.progress}{tile.progress.done}/{tile.progress.total}{/if}
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
              >{tile.progress.done}/{tile.progress.total}</span
            >
          </span>
        {:else}
          <span class="tile-meta">No plan yet</span>
        {/if}

        <span class="tile-foot">
          {#if tile.next}
            <span class="tile-next"
              >Next: {tile.next}{tile.extraNext
                ? ` +${tile.extraNext}`
                : ""}</span
            >
          {/if}
          {#if tile.movedLabel}
            <span class="tile-moved">moved {tile.movedLabel}</span>
          {/if}
        </span>
      </a>
    </li>
  {/each}
</ul>

<style>
  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(17rem, 1fr));
    gap: 10px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .tile {
    display: grid;
    gap: 7px;
    align-content: start;
    height: 100%;
    padding: 11px 12px;
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--bg);
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
  .tile-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
  }
  .tile-title {
    min-width: 0;
    font-size: 13px;
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .tile:hover .tile-title {
    color: var(--accent-text);
  }
  .tile-status {
    color: var(--fg-muted);
    font-size: 12px;
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
    gap: 5px;
  }
  .tile-bar {
    display: flex;
    gap: 2px;
    height: 6px;
  }
  .seg {
    flex: 1;
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
  /* The spine of a tree, shown as weight rather than another colour. */
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
    gap: 3px 8px;
    color: var(--fg-muted);
    font-size: 11px;
  }
  .tile-shape {
    text-transform: lowercase;
  }
  .tile-next {
    min-width: 0;
    overflow-wrap: anywhere;
  }
</style>
