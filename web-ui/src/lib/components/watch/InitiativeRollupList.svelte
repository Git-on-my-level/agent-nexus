<script>
  /**
   * Watch's Initiatives list: one row per top-level initiative, in sections
   * by what it needs (`watchList.js`): Needs you, Lagging, Blocked, Stale,
   * Moving, Proposed. Done folds away under the list.
   *
   * A row is a title, one status line (what is late or stuck, then the next
   * step), a bar from core's progress, and on the right its owner and why
   * it is in its section. On a narrow container a row is a card with the
   * same parts stacked.
   *
   * A row opens inline on a click or tap anywhere on it outside its links,
   * or Enter/Space on its title button. Open, it shows the outcome, its
   * workstreams (or its children) and the initiative's details. Every row
   * starts closed; several can be open.
   *
   * Beside the list (under it on a phone): an over-target notice, only when
   * the open initiatives exceed the board's target, and the last week of
   * changes to the set. Watch offers no way to change the set.
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
  import { kidRows, outcomeLine } from "$lib/watchRollup.js";
  import {
    childrenCount,
    groupSections,
    overTargetNotice,
    rowProgress,
    rowStatus,
    statusParts,
    lineText,
    sectionTone,
  } from "$lib/watchList.js";
  import InitiativeChanges from "./InitiativeChanges.svelte";
  import InitiativeDetails from "./InitiativeDetails.svelte";

  let {
    /** `watchRows(brief.initiatives, now)`. */
    rows = [],
    now = Date.now(),
    /** `(ref) => href` for an initiative or child card. */
    hrefFor,
    /** `(row) => href` into Decide for that initiative. */
    decideHref,
    /** `brief.initiativeBudget`; null from a core that predates it. */
    budget = null,
    /** `brief.overTarget`; null within target. */
    overTarget = null,
    /** Href of the reader's open ask about the count, or "". */
    overTargetHref = "",
    /** `brief.initiativeChanges`. */
    changes = { items: [], truncated: false },
    /** Snippet for what this list leaves out (cards with no initiative). */
    hidden = null,
  } = $props();

  const opened = new SvelteSet();
  const isOpen = (row) => opened.has(row.ref);
  function toggle(row) {
    if (opened.has(row.ref)) opened.delete(row.ref);
    else opened.add(row.ref);
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

  let showDone = $state(false);
  let sections = $derived(groupSections(rows));
  let open = $derived(sections.filter((s) => s.key !== "done"));
  let done = $derived(sections.find((s) => s.key === "done")?.rows ?? []);
  let notice = $derived(overTargetNotice(rows, budget, overTarget));

  const TONE = {
    warn: "text-warn-text",
    danger: "text-danger-text",
    ok: "text-ok-text",
    accent: "text-accent-text",
    muted: "text-fg-muted",
    subtle: "text-fg-subtle",
  };
  /** Dot and bar colour by the section's tone. */
  const FILL = {
    warn: "bg-warn",
    danger: "bg-danger",
    muted: "bg-fg-subtle",
    subtle: "bg-fg-subtle",
    accent: "bg-accent",
    ok: "bg-ok",
  };
  const ownerLabel = (ref) =>
    actorDisplayLabel(ref, $actorRegistry, $principalRegistry);
</script>

{#snippet rowView(row)}
  {@const isRowOpen = isOpen(row)}
  {@const parts = statusParts(row, now)}
  {@const status = rowStatus(row, now)}
  {@const bar = rowProgress(row)}
  {@const kids = isRowOpen ? kidRows(row.rollup, now) : null}
  {@const outcome = isRowOpen ? outcomeLine(row) : ""}
  <div
    class="item"
    data-collection-row={row.ref}
    data-watch-state={row.state}
    data-bucket={row.section}
    data-proposed={row.proposed ? "true" : undefined}
    data-expanded={String(isRowOpen)}
  >
    <!-- The title button is the keyboard and screen-reader control; a click
         anywhere else on the row is the same toggle for a pointer. -->
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="row cursor-pointer" onclick={(event) => rowClick(event, row)}>
      <span class="dot {FILL[sectionTone(row.section)]}" aria-hidden="true"
      ></span>
      <span class="main min-w-0">
        <button
          type="button"
          class="title-toggle text-left font-semibold text-fg [overflow-wrap:anywhere]"
          aria-expanded={isRowOpen}
          aria-controls={isRowOpen ? detailsId(row.ref) : undefined}
          onclick={() => toggle(row)}
          data-watch-title
          data-collection-toggle>{row.title}</button
        >
        <span
          class="mt-0.5 block truncate text-meta text-fg-muted"
          title={lineText(parts)}
          data-rollup-next
        >
          {#each parts as part, i (i)}{#if i > 0}<span
                aria-hidden="true"
                class="text-fg-subtle">{" · "}</span
              >{/if}{#if part.prefix}<span
                class={part.tone ? TONE[part.tone] : ""}
                >{part.prefix}{" "}</span
              >{/if}<span
              class={part.tone ? TONE[part.tone] : ""}
              data-source="computed:{part.field}"
              >{#if part.label}<b class="font-semibold text-fg">{part.label}</b
                >{" "}{/if}{part.text}</span
            >{/each}
        </span>
        {#if bar}
          <span
            class="bar"
            role="img"
            aria-label="{bar.done} of {bar.total} {bar.unit} done"
            title="{bar.done} of {bar.total} {bar.unit} done"
            data-source="computed:{bar.field}"
            data-rollup-progress
            ><span
              class="fill {FILL[sectionTone(row.section)]}"
              style:width="{Math.round((100 * bar.done) / bar.total)}%"
            ></span></span
          >
        {/if}
      </span>
      <span class="who min-w-0 text-meta" data-rollup-owner>
        <span class="block truncate text-fg"
          >{row.owner ? ownerLabel(row.owner) : "Unassigned"}</span
        >
        <span class="block truncate text-fg-muted" data-rollup-children
          >{#if row.proposed && row.section !== "proposed"}<span
              data-source="computed:initiatives.proposed"
              data-rollup-proposed>Proposed</span
            ><span aria-hidden="true" class="mx-1 text-fg-subtle">·</span
            >{/if}<span data-source="computed:initiatives.rollup.total"
            >{childrenCount(row)}</span
          ></span
        >
      </span>
      <span class="status min-w-0 text-meta" data-rollup-promises>
        {#if status.decide}
          <a
            class="block truncate font-medium hover:underline {TONE[
              status.tone
            ]}"
            href={decideHref(row)}
            data-source="computed:{status.field}"
            data-rollup-status>{status.text}</a
          >
        {:else}
          <span
            class="block truncate font-medium {TONE[status.tone]}"
            data-source="computed:{status.field}"
            data-rollup-status>{status.text}</span
          >
        {/if}
        {#if status.sub}
          <span
            class="sub block truncate text-fg-muted"
            data-source="computed:{status.sub.field}">{status.sub.text}</span
          >
        {/if}
      </span>
    </div>
    {#if isRowOpen}
      <div
        id={detailsId(row.ref)}
        class="open-area"
        role="region"
        aria-label="Details for {row.title}"
        data-collection-details
      >
        {#if outcome}
          <p
            class="text-body text-fg [overflow-wrap:anywhere]"
            data-source="computed:initiatives.outcome"
            data-rollup-outcome
          >
            {outcome}
          </p>
        {/if}
        {#if kids.rows.length}
          <div
            class="kids"
            data-rollup-kids
            data-source="computed:initiatives.rollup.children"
          >
            {#each kids.rows as kid (kid.ref)}
              <div class="kid" data-rollup-kid={kid.ref}>
                <span class="min-w-0 [overflow-wrap:anywhere]">
                  <a
                    class="text-fg hover:text-accent-text"
                    href={hrefFor(kid.ref)}>{kid.title}</a
                  >{#if kid.codes}<span
                      class="text-fg-muted"
                      data-source="computed:initiatives.rollup.workstreams.rollup.children.short_code"
                      data-kid-codes>{` · ${kid.codes}`}</span
                    >{/if}{#if kid.cards}<span
                      class="text-fg-muted"
                      data-source="computed:initiatives.rollup.workstreams.rollup.total"
                      data-kid-cards
                      >{` · ${kid.cards} ${kid.cards === 1 ? "card" : "cards"}`}</span
                    >{/if}
                </span>
                <span
                  class={kid.stateTone === "needs"
                    ? "font-medium text-warn-text"
                    : "text-fg-muted"}
                  title={kid.stateTitle || undefined}
                  data-source="computed:{kid.stateField}"
                  data-kid-state
                  >{kid.stateTone === "needs" && !kid.cards
                    ? "needs you"
                    : kid.stateText}</span
                >
                <span
                  class="col-wide {kid.promise.tone === 'warn'
                    ? 'text-warn-text'
                    : 'text-fg-muted'}"
                  data-source={kid.promise.field
                    ? `computed:${kid.promise.field}`
                    : undefined}
                  data-kid-promise>{kid.promise.text}</span
                >
                <span class="col-wide min-w-0 truncate" data-kid-owner>
                  {#if kid.next.kind === "you"}
                    <span class="font-semibold text-fg">You</span>
                  {:else if kid.next.kind === "owner"}
                    <span
                      class="text-fg-muted"
                      data-source={kid.workstream
                        ? "computed:initiatives.rollup.workstreams.owner"
                        : "computed:initiatives.rollup.children.owner"}
                      >{ownerLabel(kid.next.ref)}</span
                    >
                  {:else}
                    <span class="text-fg-muted">—</span>
                  {/if}
                </span>
              </div>
            {/each}
            {#if kids.more || kids.otherCards}
              <div class="kid">
                <a
                  class="text-accent-text hover:underline"
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
        <InitiativeDetails
          initiative={row}
          href={hrefFor(row.ref)}
          childHref={hrefFor}
          rollup={false}
          compact
          autoload
          {now}
        />
      </div>
    {/if}
  </div>
{/snippet}

<div class="rollup" data-rollup-list>
  <div class="layout">
    {#if notice}
      <p
        class="notice text-meta"
        data-source="computed:over_target"
        data-over-target
      >
        <span class="font-medium text-warn-text"
          >Over target: {notice.open} open against a target of {notice.target}.</span
        >
        {#if overTargetHref}
          <a
            class="text-accent-text hover:underline"
            href={overTargetHref}
            data-over-target-ask>Fold or keep? Decide →</a
          >
        {/if}
      </p>
    {/if}

    <div
      class="list"
      aria-label="Initiatives"
      role="region"
      data-collection-list
    >
      {#each open as section (section.key)}
        <section
          class="group"
          aria-labelledby="watch-section-{section.key}"
          data-watch-section={section.key}
        >
          <h2 id="watch-section-{section.key}" class="group-head" data-ui-label>
            {section.label} · {section.rows.length}
          </h2>
          {#each section.rows as row (row.ref)}
            {@render rowView(row)}
          {/each}
        </section>
      {/each}
      {#if done.length}
        <section class="group" data-watch-section="done">
          <h2 class="m-0">
            <button
              type="button"
              class="fold"
              aria-expanded={showDone}
              onclick={() => (showDone = !showDone)}
              data-collection-folded
            >
              <span class="font-medium text-fg">Done · {done.length}</span>
              <span class="text-fg-muted">{showDone ? "Hide" : "Show"}</span>
            </button>
          </h2>
          {#if showDone}
            {#each done as row (row.ref)}
              {@render rowView(row)}
            {/each}
          {/if}
        </section>
      {/if}
      {@render hidden?.()}
    </div>

    <div class="side">
      <InitiativeChanges {changes} {now} {hrefFor} />
    </div>
  </div>
</div>

<style>
  .rollup {
    container-type: inline-size;
    min-width: 0;
  }
  .layout {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    grid-template-areas: "notice" "list" "side";
    row-gap: 16px;
  }
  .notice {
    grid-area: notice;
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    column-gap: 8px;
    row-gap: 4px;
    margin: 0;
    border-radius: var(--radius-md);
    background: var(--warn-soft);
    padding: 8px 12px;
  }
  .list {
    grid-area: list;
    min-width: 0;
  }
  .side {
    grid-area: side;
    min-width: 0;
  }
  .group + .group {
    margin-top: 20px;
  }
  .group-head {
    margin: 0;
    padding: 6px 0;
    border-bottom: 1px solid var(--line);
    color: var(--fg-muted);
    font-size: 13px;
    font-weight: 600;
    letter-spacing: 0.04em;
    line-height: 18px;
    text-transform: uppercase;
  }
  .item {
    border-bottom: 1px solid var(--line-subtle);
  }
  .row {
    display: grid;
    grid-template-columns: 10px minmax(0, 1fr) 170px 130px;
    grid-template-areas: "dot main who status";
    column-gap: 14px;
    align-items: start;
    padding: 14px 4px;
  }
  .row:hover {
    background: var(--panel-hover);
  }
  .dot {
    grid-area: dot;
    width: 9px;
    height: 9px;
    margin-top: 8px;
    border-radius: 999px;
  }
  .main {
    grid-area: main;
  }
  .who {
    grid-area: who;
  }
  .status {
    grid-area: status;
    text-align: right;
  }
  .title-toggle {
    border-radius: 4px;
    font-size: 16px;
    line-height: 22px;
  }
  .title-toggle:hover {
    color: var(--accent-text);
  }
  .title-toggle:focus-visible,
  .fold:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: 2px;
  }
  .bar {
    display: block;
    height: 6px;
    margin-top: 8px;
    border-radius: 999px;
    background: var(--line);
    overflow: hidden;
  }
  .fill {
    display: block;
    height: 100%;
    border-radius: 999px;
  }
  .open-area {
    display: grid;
    gap: 10px;
    padding: 4px 4px 14px 28px;
  }
  .kids {
    border-top: 1px solid var(--line-subtle);
  }
  .kid {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 170px 120px 120px;
    column-gap: 14px;
    padding: 8px 0;
    border-bottom: 1px solid var(--line-subtle);
    font-size: 13px;
    line-height: 18px;
  }
  .fold {
    display: flex;
    width: 100%;
    align-items: center;
    justify-content: space-between;
    min-height: 2.75rem;
    padding: 0 4px;
    border-bottom: 1px solid var(--line-subtle);
    border-radius: 0;
    font-size: 14px;
    text-align: left;
  }
  .fold:hover {
    background: var(--panel-hover);
  }

  /* Wide: changes and the notice sit in a column beside the list. */
  @container (min-width: 60rem) {
    .layout {
      grid-template-columns: minmax(0, 1fr) 300px;
      grid-template-rows: auto 1fr;
      grid-template-areas: "list notice" "list side";
      column-gap: 28px;
      row-gap: 14px;
      align-items: start;
    }
  }

  /* Narrow (a phone, a split pane): each initiative is a card. */
  @container (max-width: 60rem) {
    .group + .group {
      margin-top: 16px;
    }
    .group-head {
      border-bottom: 0;
      padding-bottom: 8px;
    }
    .item {
      border: 1px solid var(--line);
      border-radius: var(--radius-lg);
      background: var(--panel);
    }
    .item + .item {
      margin-top: 10px;
    }
    .row {
      grid-template-columns: minmax(0, 1fr) auto;
      grid-template-areas: "main main" "who status";
      row-gap: 8px;
      padding: 12px 14px;
      border-radius: var(--radius-lg);
    }
    .dot {
      display: none;
    }
    .title-toggle {
      font-size: 15px;
    }
    .who {
      display: flex;
      gap: 6px;
      min-width: 0;
    }
    .who > span + span::before {
      content: "·";
      margin-right: 6px;
      color: var(--fg-subtle);
    }
    .status .sub {
      display: none;
    }
    .open-area {
      padding: 0 14px 12px;
    }
    .kid {
      grid-template-columns: minmax(0, 1fr) auto;
    }
    .col-wide {
      display: none;
    }
    .fold {
      margin-top: 16px;
      border-top: 1px solid var(--line-subtle);
    }
  }
</style>
