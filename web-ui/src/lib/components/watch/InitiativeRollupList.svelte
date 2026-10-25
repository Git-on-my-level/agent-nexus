<script>
  /**
   * Watch's Initiatives list: one row per top-level initiative with the
   * children core rolled into it, sorted by what needs the reader.
   *
   * Columns: initiative and its next step, children by state, promises, and
   * who is next. Below a 56rem container the last two drop, as on a phone.
   *
   * A row opens inline on a click or tap anywhere on it outside its links,
   * or Enter/Space on its title button. Open, it lists its workstreams (or
   * its children) as kid rows, then the initiative's details. Rows that need
   * the reader start open, so the rows that matter read without a click;
   * every other row starts closed. Several rows can be open.
   *
   * Everything shown is in the brief except a row's plan steps, which
   * `InitiativeDetails` reads only when the reader opens that row.
   */
  import { SvelteSet } from "svelte/reactivity";
  import {
    actorDisplayLabel,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import {
    BUCKETS,
    bucketCounts,
    childSegments,
    childrenLine,
    kidRows,
    nextCell,
    nextLine,
    promisesCell,
  } from "$lib/watchRollup.js";
  import InitiativeDetails from "./InitiativeDetails.svelte";

  let {
    /** `rollupRows(brief.initiatives)`. */
    rows = [],
    now = Date.now(),
    /** `(ref) => href` for an initiative or child card. */
    hrefFor,
    /** `(row) => href` into Decide for that initiative. */
    decideHref,
  } = $props();

  // Open by the reader, or closed by the reader from a default-open row.
  const opened = new SvelteSet();
  const closed = new SvelteSet();
  const startsOpen = (row) => row.bucket === "needs_you";
  const isOpen = (row) =>
    opened.has(row.ref) || (startsOpen(row) && !closed.has(row.ref));
  function toggle(row) {
    if (isOpen(row)) {
      opened.delete(row.ref);
      if (startsOpen(row)) closed.add(row.ref);
    } else {
      closed.delete(row.ref);
      opened.add(row.ref);
    }
  }
  /** Links in the row keep their action; a text selection is not a click. */
  function rowClick(event, row) {
    const target = event.target;
    if (
      target instanceof Element &&
      target.closest("a, button, input, select, textarea, label")
    )
      return;
    if (String(globalThis.getSelection?.() ?? "").trim()) return;
    toggle(row);
  }
  const detailsId = (ref) => `rollup-details-${ref.replace(/\W/g, "-")}`;

  let picked = $state("");
  let counts = $derived(bucketCounts(rows));
  let chips = $derived(
    BUCKETS.filter(
      (b) =>
        ["needs_you", "slipping", "moving"].includes(b.key) && counts[b.key],
    ),
  );
  // A refresh can empty the picked bucket; never hide every row for it.
  let filter = $derived(counts[picked] ? picked : "");
  let showDone = $state(false);
  let open = $derived(
    rows.filter((row) =>
      filter ? row.bucket === filter : row.bucket !== "done",
    ),
  );
  let done = $derived(filter ? [] : rows.filter((r) => r.bucket === "done"));

  const DOT = {
    needs_you: "bg-danger",
    slipping: "bg-warn",
    moving: "bg-ok",
    quiet: "bg-fg-subtle",
    done: "bg-fg-subtle",
  };
  const SEG = {
    needs: "bg-danger",
    slipping: "bg-warn",
    moving: "bg-accent",
    done: "bg-ok",
    not_started: "bg-line",
  };
  const CHIP = {
    needs_you: "bg-danger-soft text-danger-text",
    slipping: "bg-warn-soft text-warn-text",
    moving: "bg-ok-soft text-ok-text",
  };
  const ownerLabel = (ref) =>
    actorDisplayLabel(ref, $actorRegistry, $principalRegistry);
</script>

{#snippet segments(rollup)}
  {@const segs = childSegments(rollup)}
  {#if segs.length}
    <span
      class="flex h-1.5 gap-0.5"
      role="img"
      aria-label={childrenLine(rollup)}
      data-source="computed:initiatives.rollup.by_state"
    >
      {#each segs as seg, i (i)}
        <span class="rounded-sm {SEG[seg.tone]}" style="flex: {seg.flex} 1 0%"
        ></span>
      {/each}
    </span>
  {/if}
{/snippet}

{#snippet rowView(row)}
  {@const isRowOpen = isOpen(row)}
  {@const line = nextLine(row, now)}
  {@const promises = promisesCell(row)}
  {@const next = nextCell(row)}
  {@const kids = isRowOpen ? kidRows(row.rollup) : null}
  <div
    class="border-b border-line-subtle last:border-b-0"
    data-collection-row={row.ref}
    data-watch-state={row.state}
    data-bucket={row.bucket}
    data-expanded={String(isRowOpen)}
  >
    <!-- The title button is the keyboard and screen-reader control; a click
         anywhere else on the row is the same toggle for a pointer. -->
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div
      class="grid-row cursor-pointer hover:bg-panel-hover"
      onclick={(event) => rowClick(event, row)}
    >
      <span
        class="mt-1.5 h-2.5 w-2.5 rounded-full {DOT[row.bucket]}"
        role="img"
        aria-label={BUCKETS.find((b) => b.key === row.bucket)?.label}
        data-source={row.bucket === "needs_you"
          ? "computed:initiatives.waiting_on_you_count"
          : "computed:initiatives.state"}
      ></span>
      <span class="min-w-0">
        <span class="flex flex-wrap items-center gap-x-2 gap-y-1">
          <button
            type="button"
            class="title-toggle text-left text-meta font-semibold text-fg [overflow-wrap:anywhere]"
            aria-expanded={isRowOpen}
            aria-controls={isRowOpen ? detailsId(row.ref) : undefined}
            onclick={() => toggle(row)}
            data-watch-title
            data-collection-toggle>{row.title}</button
          >
          {#if row.waitingOnYouCount > 0}
            <a
              class="tag bg-danger-soft text-danger-text hover:underline"
              href={decideHref(row)}
              data-source="computed:initiatives.waiting_on_you_count"
              >{row.waitingOnYouCount} waiting on you</a
            >
          {/if}
        </span>
        {#if line.length}
          <span
            class="mt-0.5 block text-micro text-fg-muted [overflow-wrap:anywhere]"
            data-rollup-next
          >
            {#each line as part, i (i)}{#if i > 0}<span
                  aria-hidden="true"
                  class="mx-1 text-fg-subtle">·</span
                >{/if}<span
                class={part.tone === "warn" ? "text-warn-text" : ""}
                data-source="computed:{part.field}">{part.text}</span
              >{/each}
          </span>
        {/if}
      </span>
      <span class="min-w-0" data-rollup-children>
        {@render segments(row.rollup)}
        <span
          class="mt-1 block text-micro text-fg-muted"
          data-source="computed:initiatives.rollup.total"
          >{childrenLine(row.rollup) || "no children yet"}</span
        >
      </span>
      <span
        class="col-wide text-meta {promises.tone === 'warn'
          ? 'text-warn-text'
          : 'text-fg-muted'}"
        data-source={promises.field ? `computed:${promises.field}` : undefined}
        data-rollup-promises>{promises.text}</span
      >
      <span class="col-wide min-w-0 truncate text-micro" data-rollup-owner>
        {#if next.kind === "you"}
          <a
            class="font-semibold text-fg hover:underline"
            href={decideHref(row)}
            data-source="computed:{next.field}">You</a
          >
        {:else if next.kind === "owner"}
          <span class="text-fg">{ownerLabel(next.ref)}</span>
        {:else}
          <span class="text-fg-muted">—</span>
        {/if}
      </span>
    </div>
    {#if isRowOpen}
      <div
        id={detailsId(row.ref)}
        role="region"
        aria-label="Details for {row.title}"
        data-collection-details
      >
        {#if kids.rows.length}
          <div
            data-rollup-kids
            data-source="computed:initiatives.rollup.children"
          >
            {#each kids.rows as kid (kid.ref)}
              <div class="grid-row kid" data-rollup-kid={kid.ref}>
                <span></span>
                <a
                  class="min-w-0 text-fg hover:text-accent-text [overflow-wrap:anywhere]"
                  href={hrefFor(kid.ref)}>{kid.title}</a
                >
                <span
                  class={kid.tone === "needs"
                    ? "font-medium text-danger-text"
                    : "text-fg-muted"}
                  >{kid.tone === "needs" ? "needs you" : kid.stateText}</span
                >
                <span
                  class="col-wide {kid.promise.tone === 'warn'
                    ? 'text-warn-text'
                    : 'text-fg-muted'}">{kid.promise.text}</span
                >
                <span
                  class="col-wide {kid.next === 'You'
                    ? 'font-semibold text-fg'
                    : 'text-fg-muted'}">{kid.next}</span
                >
              </div>
            {/each}
            {#if kids.more || kids.otherCards}
              <div class="grid-row kid">
                <span></span>
                <a
                  class="text-micro text-accent-text hover:underline"
                  href={hrefFor(row.ref)}
                  data-source="computed:initiatives.rollup.total"
                  >{[
                    kids.more
                      ? `${kids.more} more ${kids.workstreams ? "workstreams" : "children"}`
                      : "",
                    kids.otherCards
                      ? `${kids.otherCards} ${kids.otherCards === 1 ? "card" : "cards"} outside workstreams`
                      : "",
                  ]
                    .filter(Boolean)
                    .join(" · ")} →</a
                >
              </div>
            {/if}
          </div>
        {/if}
        <div class="details">
          <InitiativeDetails
            initiative={row}
            href={hrefFor(row.ref)}
            childHref={hrefFor}
            rollup={false}
            compact
            autoload={opened.has(row.ref)}
            {now}
          />
        </div>
      </div>
    {/if}
  </div>
{/snippet}

<div class="rollup space-y-3" data-rollup-list>
  <div class="flex flex-wrap items-center gap-2 text-meta">
    {#if filter}
      <button
        type="button"
        class="tag tag--pill border border-line text-fg-muted"
        onclick={() => (picked = "")}
        data-collection-filter-all>All</button
      >
    {/if}
    {#each chips as chip (chip.key)}
      <button
        type="button"
        class="tag tag--pill {CHIP[chip.key]}"
        class:tag--on={filter === chip.key}
        aria-pressed={filter === chip.key}
        onclick={() => (picked = filter === chip.key ? "" : chip.key)}
        data-collection-filter={chip.key}
        data-source={chip.key === "needs_you"
          ? "computed:initiatives.waiting_on_you_count"
          : "computed:initiatives.state"}
        >{counts[chip.key]} {chip.label}</button
      >
    {/each}
    <span class="flex-1"></span>
    <span class="text-micro text-fg-muted"
      >Sorted by what needs you · children fold into their initiative</span
    >
  </div>

  <section
    class="overflow-hidden rounded-md border border-line bg-panel"
    aria-label="Initiatives"
    data-collection-list
  >
    <div
      class="grid-row border-b border-line text-micro text-fg-muted"
      aria-hidden="true"
    >
      <span></span><span>Initiative · next step</span><span
        >Children by state</span
      ><span class="col-wide">Promises</span><span class="col-wide">Next</span>
    </div>
    {#each open as row (row.ref)}
      {@render rowView(row)}
    {/each}
    {#if done.length}
      <button
        type="button"
        class="flex w-full items-center gap-1.5 px-4 py-2 text-left text-micro text-fg-muted hover:bg-panel-hover hover:text-fg"
        aria-expanded={showDone}
        onclick={() => (showDone = !showDone)}
        data-collection-folded
        ><svg
          class="h-2.5 w-2.5 transition-transform {showDone ? 'rotate-90' : ''}"
          viewBox="0 0 20 20"
          fill="currentColor"
          aria-hidden="true"><path d="M7 4l7 6-7 6V4z" /></svg
        >{done.length} done</button
      >
      {#if showDone}
        {#each done as row (row.ref)}
          {@render rowView(row)}
        {/each}
      {/if}
    {/if}
  </section>
</div>

<style>
  .rollup {
    container-type: inline-size;
    min-width: 0;
  }
  .grid-row {
    display: grid;
    grid-template-columns: 14px minmax(0, 2.4fr) 170px 150px 120px;
    gap: 14px;
    align-items: center;
    padding: 12px 16px;
  }
  .grid-row > :first-child {
    align-self: start;
  }
  .kid {
    padding: 8px 16px 8px 44px;
    font-size: 13px;
    border-top: 1px solid var(--line-subtle);
    background: var(--bg-soft);
  }
  .details {
    padding: 8px 16px 10px 44px;
    border-top: 1px solid var(--line-subtle);
    background: var(--bg-soft);
  }
  .tag {
    display: inline-flex;
    align-items: center;
    border-radius: 6px;
    padding: 2px 7px;
    font-size: 11px;
    font-weight: 600;
    white-space: nowrap;
  }
  .tag--pill {
    min-height: 2rem;
    padding: 6px 10px;
  }
  .tag--on {
    outline: 2px solid currentColor;
    outline-offset: 1px;
  }
  .title-toggle {
    border-radius: 4px;
  }
  .title-toggle:hover {
    color: var(--accent-text);
  }
  .title-toggle:focus-visible,
  .tag:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: 2px;
  }
  /* Narrow containers (a phone, a split pane) drop Promises and Next. */
  @container (max-width: 56rem) {
    .grid-row {
      grid-template-columns: 14px minmax(0, 1fr) 110px;
    }
    .col-wide {
      display: none;
    }
    .kid {
      padding-left: 30px;
    }
    .details {
      padding-left: 30px;
    }
  }
</style>
