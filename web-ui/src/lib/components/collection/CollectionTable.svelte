<script>
  /**
   * A collection — initiatives, loops, or a `live-collection` panel — as
   * dense, glanceable rows or as a board of state columns. The row shape is
   * `$lib/collection.js`; this component only lays it out.
   *
   * Columns that no row fills are not drawn, and the layout follows the width
   * it is given (container queries), so the same table works full-page, in a
   * report panel, and on a phone. Pieces that came from a brief field carry
   * `data-source="computed:<field>"` (I1).
   */
  import {
    boardColumns,
    columnsFor,
    filterRows,
    sparkPoints,
  } from "$lib/collection.js";
  import InitiativeProgress from "$lib/components/watch/InitiativeProgress.svelte";

  let {
    collection,
    view = "list",
    /** State keys that start folded under a disclosure (e.g. done). */
    folded = [],
    label = "Items",
    /** Optional snippet rendered under an expanded row: details(row). */
    details = null,
    /** Show the state filter pills. */
    filters = true,
    /** Optional snippet at the right of the filter row (layout toggle). */
    actions = null,
  } = $props();

  /** The state pill the reader picked; see `active` for the one applied. */
  let picked = $state("");
  let expanded = $state("");
  let showFolded = $state(false);

  let foldedSet = $derived(new Set(folded));
  let cols = $derived(columnsFor(collection));
  let stateByKey = $derived(
    Object.fromEntries(collection.states.map((s) => [s.key, s])),
  );
  let pills = $derived(
    collection.states.filter((s) => (collection.counts[s.key] ?? 0) > 0),
  );
  // A refresh can empty the picked state (every failing loop recovers). Its
  // pill is gone then, so applying it would hide every row: never apply it,
  // and clear it, so it does not come back when that state does.
  let filter = $derived(pills.some((s) => s.key === picked) ? picked : "");
  $effect(() => {
    if (picked && !filter) picked = "";
  });
  let groups = $derived(filterRows(collection, filter));
  let visibleGroups = $derived(
    groups.map((g) => ({
      ...g,
      open: g.rows.filter((r) => filter || !foldedSet.has(r.state)),
      hidden: filter ? [] : g.rows.filter((r) => foldedSet.has(r.state)),
    })),
  );
  let hiddenCount = $derived(
    visibleGroups.reduce((n, g) => n + g.hidden.length, 0),
  );
  let board = $derived(boardColumns(collection));
  /* One grid template per width: wide, medium (no owner or activity) and
     narrow (title and progress only). The trailing 2rem is the expander. */
  function templateFor(keep) {
    return [
      "1.25rem",
      "minmax(0, 1fr)",
      cols.owner && keep.owner ? "7.5rem" : "",
      cols.progress ? (keep.narrow ? "5.5rem" : "9.5rem") : "",
      cols.time && keep.time ? "8.5rem" : "",
      cols.activity && keep.activity ? "6.5rem" : "",
      cols.next && keep.next ? "4.5rem" : "",
      "2rem",
    ]
      .filter(Boolean)
      .join(" ");
  }
  let template = $derived(
    templateFor({ owner: true, time: true, activity: true, next: true }),
  );
  let templateMd = $derived(templateFor({ time: true, next: true }));
  let templateSm = $derived(templateFor({ narrow: true }));

  function toggle(ref) {
    expanded = expanded === ref ? "" : ref;
  }
  function activityStyle(n) {
    return `height:${3 + Math.min(n, 4) * 4}px`;
  }
</script>

{#snippet dot(row)}
  {@const s = stateByKey[row.state]}
  <span
    class="dot dot--{s?.tone ?? 'neutral'}"
    role="img"
    aria-label={s?.label ?? row.state}
    title={s?.label ?? row.state}
    data-source={row.stateField ? `computed:${row.stateField}` : undefined}
  ></span>
{/snippet}

{#snippet progressCell(p)}
  {#if !p}
    <span></span>
  {:else if p.kind === "plan"}
    <InitiativeProgress
      progress={p.progress}
      state={p.initiativeState}
      title=""
    />
  {:else if p.kind === "segments"}
    <span class="flex min-w-0 flex-col gap-1">
      <span class="segs" style:--n={p.segments.length} aria-hidden="true">
        {#each p.segments as seg, i (i)}<span class="seg seg--{seg.status}"
          ></span>{/each}
      </span>
      {#if p.label}<span class="text-micro text-fg-muted">{p.label}</span>{/if}
    </span>
  {:else if p.kind === "spark"}
    <span
      class="flex min-w-0 items-center gap-2"
      data-source={p.untagged ? undefined : "computed:initiatives.loop"}
    >
      {#if p.values.length > 1}
        <svg
          width="56"
          height="22"
          viewBox="0 0 88 22"
          preserveAspectRatio="none"
          aria-hidden="true"
          class="shrink-0"
          ><polyline
            points={sparkPoints(p.values)}
            fill="none"
            stroke="var(--accent-solid)"
            stroke-width="2"
            stroke-linejoin="round"
            vector-effect="non-scaling-stroke"
          /></svg
        >
      {/if}
      <span
        class="truncate text-micro font-medium tabular-nums text-fg"
        title={p.metric ? `${p.metric}: ${p.label}` : undefined}
        aria-label={p.metric ? `${p.metric}: ${p.label}` : undefined}
        >{p.label}</span
      >
    </span>
  {:else}
    <span class="truncate text-micro text-fg-muted">{p.label}</span>
  {/if}
{/snippet}

<div class="collection" data-collection>
  <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
    <!-- While a filter is on, its controls stay, even with one state left. -->
    {#if filters && (pills.length > 1 || filter)}
      <div
        class="flex flex-wrap items-center gap-1.5"
        role="group"
        aria-label="Filter {label} by state"
      >
        {#if filter}
          <button
            type="button"
            class="pill"
            onclick={() => (picked = "")}
            data-collection-filter-all>All</button
          >
        {/if}
        {#each pills as s (s.key)}
          <button
            type="button"
            class="pill"
            class:pill--on={filter === s.key}
            aria-pressed={filter === s.key}
            onclick={() => (picked = filter === s.key ? "" : s.key)}
            data-collection-filter={s.key}
          >
            <span class="dot dot--{s.tone}" aria-hidden="true"></span>
            <span class="tabular-nums font-medium text-fg"
              >{collection.counts[s.key]}</span
            >
            {s.label}
          </button>
        {/each}
      </div>
    {:else}
      <span></span>
    {/if}
    {@render actions?.()}
  </div>

  {#if view === "board"}
    <div class="board" data-collection-board>
      {#each board as column (column.key)}
        <section class="min-w-0 space-y-2" aria-label={column.label}>
          <h3 class="ui-label mb-0 flex items-center gap-1.5">
            <span class="dot dot--{column.tone}" aria-hidden="true"></span>
            {column.label}
            <span class="font-normal tabular-nums text-fg-subtle"
              >{column.rows.length}</span
            >
          </h3>
          {#each column.rows as row (row.ref)}
            <a
              class="block rounded-md border border-line bg-panel px-3 py-2.5 transition-colors hover:border-line-strong hover:bg-panel-hover"
              href={row.href || undefined}
              data-collection-card={row.ref}
            >
              <span
                class="flex items-center gap-1.5 text-meta font-medium text-fg"
                data-watch-title
                ><span class="line-clamp-2">{row.title}</span
                >{#if row.changed}<span
                    class="changed"
                    aria-label="changed since you last looked"
                  ></span>{/if}</span
              >
              {#if row.progress}<span class="mt-2 block"
                  >{@render progressCell(row.progress)}</span
                >{/if}
              {#if row.time}<span
                  class="mt-1.5 block text-micro {row.time.tone === 'warning'
                    ? 'text-warn-text'
                    : 'text-fg-muted'}"
                  data-source={row.time.field
                    ? `computed:${row.time.field}`
                    : undefined}>{row.time.text}</span
                >{/if}
            </a>
          {/each}
        </section>
      {/each}
    </div>
  {:else}
    <div
      class="rows rounded-md border border-line bg-panel"
      style:--cols={template}
      style:--cols-md={templateMd}
      style:--cols-sm={templateSm}
      data-collection-list
    >
      {#each visibleGroups as group (group.key)}
        {#if group.label && group.open.length}
          <div
            class="border-b border-line-subtle bg-bg-soft px-3 py-1.5 text-micro font-semibold uppercase tracking-wide text-fg-muted"
          >
            {group.label}
            <span class="font-normal tabular-nums text-fg-subtle"
              >{group.open.length}</span
            >
          </div>
        {/if}
        {#each group.open as row (row.ref)}
          {@render rowView(row)}
        {/each}
      {/each}
      {#if hiddenCount}
        <button
          type="button"
          class="flex w-full items-center gap-1.5 px-3 py-2 text-left text-micro text-fg-muted hover:bg-panel-hover hover:text-fg"
          aria-expanded={showFolded}
          onclick={() => (showFolded = !showFolded)}
          data-collection-folded
        >
          <svg
            class="h-2.5 w-2.5 transition-transform {showFolded
              ? 'rotate-90'
              : ''}"
            viewBox="0 0 20 20"
            fill="currentColor"
            aria-hidden="true"><path d="M7 4l7 6-7 6V4z" /></svg
          >
          {hiddenCount}
          {folded
            .map((k) => stateByKey[k]?.label ?? k)
            .join(", ")
            .toLowerCase()}
        </button>
        {#if showFolded}
          {#each visibleGroups as group (group.key)}
            {#each group.hidden as row (row.ref)}
              {@render rowView(row)}
            {/each}
          {/each}
        {/if}
      {/if}
    </div>
  {/if}
</div>

{#snippet rowView(row)}
  <div
    class="row-wrap"
    data-collection-row={row.ref}
    data-state={row.state}
    data-watch-state={row.computedState || undefined}
  >
    <div class="row">
      {@render dot(row)}
      <div class="min-w-0">
        <div class="flex min-w-0 items-center gap-1.5">
          {#if row.href}
            <a
              class="truncate text-meta font-medium text-fg hover:text-accent-text"
              href={row.href}
              data-watch-title>{row.title}</a
            >
          {:else}
            <span
              class="truncate text-meta font-medium text-fg"
              data-watch-title>{row.title}</span
            >
          {/if}
          {#if row.changed}<span
              class="changed"
              role="img"
              aria-label="changed since you last looked"
            ></span>{/if}
        </div>
        {#if row.subtitle?.parts}
          <div
            class="truncate text-micro text-fg-muted"
            title={row.subtitle.parts.map((part) => part.text).join(" · ")}
          >
            {#each row.subtitle.parts as part, i (i)}{#if i > 0}<span
                  aria-hidden="true"
                  class="mx-1 text-fg-subtle">·</span
                >{/if}<span
                data-source={part.field ? `computed:${part.field}` : undefined}
                >{part.text}</span
              >{/each}
          </div>
        {:else if row.subtitle}
          <div
            class="truncate text-micro text-fg-muted"
            title={row.subtitle.text}
            data-source={row.subtitle.field
              ? `computed:${row.subtitle.field}`
              : undefined}
          >
            {row.subtitle.text}
          </div>
        {/if}
      </div>
      {#if cols.owner}
        <span class="col-owner truncate text-micro text-fg-muted"
          >{row.owner?.label ?? ""}</span
        >
      {/if}
      {#if cols.progress}
        <span class="col-progress min-w-0"
          >{@render progressCell(row.progress)}</span
        >
      {/if}
      {#if cols.time}
        <span
          class="col-time truncate text-micro {row.time?.tone === 'warning'
            ? 'text-warn-text'
            : 'text-fg-muted'}"
          title={row.time?.title || undefined}
          data-source={row.time?.field
            ? `computed:${row.time.field}`
            : undefined}>{row.time?.text ?? ""}</span
        >
      {/if}
      {#if cols.activity}
        <span class="col-activity activity" aria-hidden="true">
          {#each row.activity ?? [] as n, i (i)}<span
              class="bar"
              class:bar--on={n > 0}
              style={activityStyle(n)}
            ></span>{/each}
        </span>
      {/if}
      {#if cols.next}
        <span class="col-next">
          {#if row.next}
            {#if row.next.href}
              <a
                class="next"
                class:next--you={row.next.isViewer}
                href={row.next.href}
                data-source={row.next.field
                  ? `computed:${row.next.field}`
                  : undefined}>{row.next.label}</a
              >
            {:else}
              <span class="next" class:next--you={row.next.isViewer}
                >{row.next.label}</span
              >
            {/if}
          {/if}
        </span>
      {/if}
      {#if details}
        <button
          type="button"
          class="expand"
          aria-expanded={expanded === row.ref}
          aria-label="{expanded === row.ref
            ? 'Hide'
            : 'Show'} details for {row.title}"
          onclick={() => toggle(row.ref)}
          ><svg
            class="h-3 w-3 transition-transform {expanded === row.ref
              ? 'rotate-90'
              : ''}"
            viewBox="0 0 20 20"
            fill="currentColor"
            aria-hidden="true"><path d="M7 4l7 6-7 6V4z" /></svg
          ></button
        >
      {/if}
    </div>
    {#if details && expanded === row.ref}
      <div class="details" data-collection-details>
        {@render details(row)}
      </div>
    {/if}
  </div>
{/snippet}

<style>
  .collection {
    container-type: inline-size;
    min-width: 0;
  }
  .row-wrap + .row-wrap {
    border-top: 1px solid var(--line-subtle);
  }
  .row {
    display: grid;
    grid-template-columns: var(--cols);
    align-items: center;
    gap: 0.75rem;
    padding: 0.55rem 0.5rem 0.55rem 0.75rem;
    min-width: 0;
  }
  .row:hover {
    background: var(--panel-hover);
  }
  .details {
    padding: 0.25rem 0.75rem 0.85rem 2.75rem;
    background: var(--bg-soft);
  }
  .expand {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2rem;
    height: 2rem;
    border-radius: 6px;
    color: var(--fg-muted);
  }
  .expand:hover {
    color: var(--fg);
    background: var(--panel-hover);
  }
  .expand:focus-visible,
  .pill:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: 1px;
  }

  /* Five shared tones; shape differs as well as colour. */
  .dot {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 999px;
    flex: none;
    box-sizing: border-box;
    background: var(--fg-subtle);
  }
  .dot--attention {
    background: var(--warn-text);
  }
  .dot--warning {
    background: transparent;
    border: 2px solid var(--warn-text);
  }
  .dot--quiet {
    background: transparent;
    border: 2px dashed var(--fg-subtle);
  }
  .dot--good {
    background: var(--accent-solid);
  }

  .changed {
    width: 7px;
    height: 7px;
    border-radius: 999px;
    background: var(--accent-solid);
    flex: none;
  }

  .pill {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    min-height: 2rem;
    padding: 0.25rem 0.7rem;
    border: 1px solid var(--line);
    border-radius: 999px;
    background: var(--panel);
    color: var(--fg-muted);
    font-size: 12px;
  }
  .pill--on {
    border-color: var(--accent-solid);
    background: var(--accent-soft);
  }

  .segs {
    display: grid;
    grid-template-columns: repeat(var(--n), minmax(0, 1fr));
    gap: 2px;
  }
  .seg {
    height: 6px;
    border-radius: 2px;
    background: var(--line);
  }
  .seg--done {
    background: var(--accent-solid);
  }
  .seg--current {
    background: color-mix(in srgb, var(--accent-solid) 45%, var(--line));
  }
  .seg--blocked {
    background: var(--warn-text);
  }

  .activity {
    display: flex;
    align-items: flex-end;
    gap: 2px;
    height: 20px;
  }
  .bar {
    width: 4px;
    border-radius: 1px;
    background: var(--line);
  }
  .bar--on {
    background: color-mix(in srgb, var(--accent-solid) 75%, transparent);
  }

  .next {
    display: inline-block;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    padding: 1px 7px;
    border-radius: 6px;
    background: var(--bg-soft);
    color: var(--fg-muted);
    font-size: 11px;
    font-weight: 600;
  }
  .next--you {
    background: rgba(245, 158, 11, 0.12);
    color: var(--warn-text);
  }

  .board {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
    gap: 0.9rem;
    align-items: start;
  }

  /* Narrower containers drop the least important columns first. */
  @container (max-width: 56rem) {
    .col-owner,
    .col-activity {
      display: none;
    }
    .row {
      grid-template-columns: var(--cols-md);
    }
  }
  @container (max-width: 34rem) {
    .col-time,
    .col-next {
      display: none;
    }
    /* Phone: the plan bar shrinks to fit, the title gets the room. */
    .col-progress :global(.progress--row .track) {
      width: 2.75rem;
    }
    .row {
      grid-template-columns: var(--cols-sm);
    }
  }
</style>
