<script>
  /**
   * One card's summary, everywhere a card appears.
   *
   * This is the only renderer for a card's status in the app. A table row, a
   * board card, an Overview card, a task header, an Inbox context line and a
   * ⌘K result all render this component, which is what makes them agree: the
   * Tasks table and the Overview used to read the same task's state from
   * different fields and say different things about it.
   *
   * It renders whichever parts core sent — status, the stored phase when it
   * disagrees, progress, the next step, the step digest, owner, due, age,
   * source and the viewer's open asks — and `density` decides how many fit:
   *
   * - `row` — one line: status, the stored phase when it disagrees, progress
   *   and the viewer's asks. For a table cell, a board card and a search
   *   result. No age: a row-density host is a list, and it either has a time
   *   column of its own or no room for one, so where the age goes is its
   *   call — and no source status word, for the same reason: a fourth badge
   *   here turned every Tasks row two lines tall.
   * - `card` — the Overview card: a status line with its age, any hints, a
   *   progress bar, the step lists or the next step, and a meta line.
   * - `header` — a page header, which has room for words: the status with its
   *   computed reason, any hints, progress with its unit, due, age and asks.
   *
   * There is no branching on whether the card is an initiative, which board
   * it is on, or who is looking. A part is shown when core sent it and the
   * density has room; that is the whole rule.
   *
   * `summary` is a `workSummaryModel` result. `ownerLabel` is the owner's
   * display name, resolved by the caller — the model carries core's actor
   * ref, and the actor directory is the page's to read, not a badge's.
   */
  import { tooltip } from "$lib/actions/tooltip.js";
  import FreshnessBadge from "$lib/components/FreshnessBadge.svelte";
  import { formatAge } from "$lib/ageBadge.js";
  import { formatAbsoluteDateTime, formatTimestamp } from "$lib/formatDate";

  let {
    summary = null,
    /** `row` | `card` | `header` */
    density = "row",
    /** The card's title, for the accessible name of the progress bar only. */
    title = "",
    /** Owner display name; omitted where the surface has its own column. */
    ownerLabel = "",
    /** Where the card lives, when the surface tells several apart. */
    placeLabel = "",
    now = Date.now(),
    class: extraClass = "",
  } = $props();

  let status = $derived(summary?.status ?? null);
  let setStatus = $derived(summary?.setStatus ?? null);
  /*
   * Notes that are not the card's state: "No plan", and whatever core adds
   * next. They are deliberately absent from `row` density — a list of fifty
   * tasks, most of which have no plan, would say so fifty times, which is
   * what moving `no_plan` out of the status was for.
   */
  let hints = $derived(density === "row" ? [] : (summary?.hints ?? []));
  let progress = $derived(summary?.progress ?? null);
  let attention = $derived(summary?.attention ?? null);
  /*
   * The computed reason, as the badge's accessible name and tooltip. At header
   * density it is also on screen: a page has room for the sentence, and
   * "Blocked" without "waiting on review" sends the reader looking for it.
   */
  let statusTitle = $derived(
    [status?.label, status?.reason].filter(Boolean).join(" — "),
  );
  let dueTitle = $derived(
    summary?.due ? formatAbsoluteDateTime(summary.due) : "",
  );
  let dueText = $derived(summary?.due ? formatTimestamp(summary.due) : "");
  /*
   * How old the card is — a different question from when it last moved.
   *
   * Core sends both a creation instant and the seconds it measured at read
   * time, and marks the seconds as excluded from change detection for this
   * reason: an age computed from them freezes on a page left open. The
   * instant wins; the seconds are the fallback for a core that sends only
   * those, anchored once so the age still counts up.
   */
  let openedAt = $derived.by(() => {
    if (summary?.createdAt) return summary.createdAt;
    if (summary?.age == null) return "";
    return new Date(now - summary.age * 1000).toISOString();
  });
  let ageText = $derived(openedAt ? formatAge(openedAt, now) : "");
  let ageTitle = $derived(
    openedAt ? `Opened ${formatAbsoluteDateTime(openedAt)}` : "",
  );
  /** The sentence behind the count. */
  let progressLabel = $derived(
    progress ? `${title || "Checklist"}: ${progress.sentence}` : "",
  );
  // Core samples the viewer's open asks; a capped window is a lower bound and
  // has to read as one rather than as an exact count.
  let attentionTitle = $derived.by(() => {
    if (!attention) return "";
    /*
     * The oldest ask's age in the same words every other age on the page
     * uses. Core sends it as seconds at read time, so it becomes an instant
     * to format: rounding straight to hours called an ask raised a minute
     * ago "1h" and one from a month ago "720h".
     */
    const firedAt =
      attention.oldestAt ||
      (attention.oldestAge == null
        ? ""
        : new Date(now - attention.oldestAge * 1000).toISOString());
    const oldest = firedAt ? `oldest ${formatAge(firedAt, now)}` : "";
    const bound = attention.truncated || summary?.attentionTruncated;
    return [
      `${attention.count}${bound ? " or more" : ""} open ${attention.count === 1 ? "ask" : "asks"} for you`,
      oldest,
    ]
      .filter(Boolean)
      .join(", ");
  });
</script>

{#if status}
  <span
    class="summary summary--{density} {extraClass}"
    data-work-summary={density}
  >
    <span class="summary__status-line">
      <span
        class="ui-badge ui-badge--{status.tone} summary__badge"
        data-health={status.state}
        aria-label={statusTitle}
        use:tooltip={statusTitle}
      >
        <span class="summary__glyph" aria-hidden="true">{status.glyph}</span>
        <span class="summary__label">{status.label}</span>
      </span>
      {#if setStatus}
        <!-- Core sends the stored phase only when it disagrees with what it
             computed. Saying both is the point: "Blocked · marked in
             progress" tells the reader the board is wrong, not the card. -->
        <span class="summary__set" data-summary-set-status={setStatus.state}
          >· marked {setStatus.label.toLocaleLowerCase()}</span
        >
      {/if}
      {#if density !== "row" && summary.sourceStatusShown}
        <!--
          The source's own word for where this stands — "In UAT", "awaiting
          triage". It sits beside the computed status rather than replacing
          it: the two answer different questions, and a tracker's vocabulary
          is the one its users speak. Core sends it only for a non-Nexus
          authority, so a card created here never grows this badge.

          Not at row density: the badge cannot wrap by design, and a fourth
          one in a 12rem Status cell is what made every Tasks row two lines
          tall. A row still shows the word where it is the only readable
          answer — for a state Nexus has no name for the status label *is*
          the source's word, which is also why this badge is skipped when it
          would repeat it.
        -->
        <span
          class="ui-badge ui-badge--neutral summary__badge"
          data-summary-source-status>{summary.sourceStatus}</span
        >
      {/if}
      {#if density !== "row" && summary.freshness}
        <FreshnessBadge
          at={summary.lastMovementAt}
          kind={summary.freshnessKind}
          expectationHours={summary.expectationHours}
          verb="moved"
          {now}
        />
      {/if}
    </span>

    {#if density === "header" && status.reason}
      <span class="summary__reason" data-summary-reason>{status.reason}</span>
    {/if}

    {#if hints.length}
      <!-- Quiet, and under the status rather than beside it: a note about
           the card, not a second opinion on what state it is in. -->
      <span class="summary__hints" data-summary-hints>
        {#each hints as hint (hint.key)}
          <span data-summary-hint={hint.key}>{hint.label}</span>
        {/each}
      </span>
    {/if}

    {#if progress}
      {#if density === "card"}
        <span class="summary__viz" data-summary-shape={summary.shape || null}>
          {#if summary.segments.length}
            <!--
              One row of segments, in plan order. The plan's real shape lives
              on the task page; at card size it became a block of colour that
              said less than naming the shape does.
            -->
            <span
              class="summary__bar"
              style:--summary-segments={summary.segments.length}
              role="img"
              aria-label={progressLabel}
            >
              {#each summary.segments as segment (segment.key)}
                <span
                  class="seg"
                  data-status={segment.status}
                  class:seg--critical={segment.onCriticalPath}
                ></span>
              {/each}
            </span>
          {:else}
            <progress
              value={progress.done}
              max={progress.total}
              aria-label={progressLabel}
            ></progress>
          {/if}
          <span class="summary__meta">
            <span class="summary__count" data-summary-progress
              >{progress.count}</span
            >
            {#if progress.truncated}
              <span
                class="summary__partial"
                use:tooltip={"Some linked work could not be read, so this is a lower bound."}
                >at least</span
              >
            {/if}
            {#if summary.shapeLabel}
              <span class="summary__shape">{summary.shapeLabel}</span>
            {/if}
            {#if summary.overflow}<span>+{summary.overflow} more</span>{/if}
          </span>
        </span>
      {:else}
        <!--
          `3/7` reads as a date to a screen reader, so the compact densities
          carry the sentence as the accessible name: this is the only progress
          element they render, where card density has the labelled bar.
        -->
        <span
          class="summary__count"
          data-summary-progress
          role="img"
          aria-label={progressLabel}
          use:tooltip={progressLabel}
          >{density === "header" ? progress.label : progress.count}</span
        >
      {/if}
    {/if}

    {#if density === "card"}
      <!--
        What landed, what is moving, what is next. Three short lists instead
        of one "Next" line, because the question a reader opens a dashboard
        with is "where is this" and a single next step answers a third of it.
        Core bounds each list to three rows and counts the rest; a core that
        computes no digest falls back to the one next step it does send.
      -->
      {#if summary.steps?.groups.length}
        <span class="summary__steps" data-tile-steps>
          {#each summary.steps.groups as list (list.key)}
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
      {:else if summary.next}
        <span class="summary__foot">
          <span class="summary__next" data-tile-next
            >Next: {summary.next.title}{summary.next.more
              ? ` +${summary.next.more}`
              : ""}</span
          >
        </span>
      {/if}
    {/if}

    {#if ownerLabel || placeLabel || dueText || attention}
      <span class="summary__foot" data-summary-foot>
        {#if ownerLabel}<span class="summary__owner" data-summary-owner
            >{ownerLabel}</span
          >{/if}
        {#if placeLabel}<span data-summary-place>{placeLabel}</span>{/if}
        {#if dueText}
          <time
            class="summary__due"
            data-summary-due
            datetime={summary.due}
            use:tooltip={dueTitle}>Due {dueText}</time
          >
        {/if}
        {#if density === "header" && ageText}
          <!-- A page header has room to say how long this has been open,
               which the age badge beside the status does not answer. -->
          <time
            class="summary__due"
            data-summary-age
            datetime={summary.createdAt || undefined}
            use:tooltip={ageTitle}>Opened {ageText}</time
          >
        {/if}
        {#if attention}
          <span
            class="ui-badge ui-badge--warn summary__badge"
            data-summary-attention={attention.count}
            aria-label={attentionTitle}
            use:tooltip={attentionTitle}>{attention.label}</span
          >
        {/if}
      </span>
    {/if}
  </span>
{/if}

<style>
  .summary {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 10px;
    min-width: 0;
    align-content: start;
  }
  /* One line that wraps, for a table cell or a board card. */
  .summary--row,
  .summary--header {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 3px 8px;
  }
  .summary__status-line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .summary__badge {
    flex: none;
    gap: 4px;
    /* The badge is never the thing that gets clipped, so it never needs an
       ellipsis: a badge that cannot show its own text is worse than none. */
    white-space: nowrap;
  }
  .summary__glyph {
    font-size: 10px;
    line-height: 1;
  }
  .summary__set,
  .summary__reason,
  .summary__meta,
  .summary__foot {
    color: var(--fg-muted);
    font-size: 12px;
    min-width: 0;
    max-width: 100%;
  }
  .summary__set {
    overflow-wrap: anywhere;
  }
  .summary__reason {
    flex-basis: 100%;
    font-size: 11px;
  }
  /*
   * A secondary note: the same muted scale as the reason, one step quieter
   * than anything that states the card's status.
   */
  .summary__hints {
    display: flex;
    flex-wrap: wrap;
    gap: 2px 8px;
    flex-basis: 100%;
    min-width: 0;
    color: var(--fg-subtle, var(--fg-muted));
    font-size: 11px;
  }
  .summary__viz {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 6px;
  }
  /*
   * One row, one column per step, each the same width. The count of steps
   * comes in as a custom property so the track widths are the grid's job
   * rather than flex rounding's.
   */
  .summary__bar {
    display: grid;
    grid-template-columns: repeat(var(--summary-segments, 1), minmax(0, 1fr));
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
  .summary__viz progress {
    width: 100%;
    height: 6px;
    border: 0;
    border-radius: 3px;
    background: var(--bg-soft);
    accent-color: var(--accent-solid);
  }
  .summary__meta,
  .summary__foot {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 3px 8px;
  }
  .summary__count {
    font-variant-numeric: tabular-nums;
  }
  .summary--row .summary__count,
  .summary--header .summary__count {
    color: var(--fg-muted);
    font-size: 12px;
  }
  .summary__partial,
  .summary__shape {
    text-transform: lowercase;
  }
  .summary__next {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  /*
   * An owner and a board name are identities, not sentences: one line each,
   * ellipsised. A board called "Archive of everything nobody owns anymore
   * (0123456789abcdef)" is a real row, and it must not push a card wider than
   * its column — which it does without `min-width: 0` on every flex ancestor
   * as well as on the span itself.
   */
  .summary__owner,
  .summary__foot [data-summary-place] {
    min-width: 0;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /*
   * The three lists: a short label, then its rows beside it. One grid, so the
   * labels line up down the card and the rows share one left edge — on a
   * phone as well, where a stacked label per group would cost three lines to
   * say three words.
   */
  .summary__steps {
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
    /* One line per step. A card is a glance; the plan is one click away. */
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
