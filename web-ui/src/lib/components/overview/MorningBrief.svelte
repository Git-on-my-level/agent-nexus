<script>
  /**
   * The morning brief: five answers at the top of Overview.
   *
   *   What needs my decision?       ranked, with the reason for the rank
   *   What changed since I looked?  grouped counts that open into items
   *   What is at risk?              computed reasons only
   *   Is the machine running?       working / finished / stuck
   *   Where is each initiative?     progress, and an honest health chip
   *
   * Everything here is computed by core. The band's whole job is to be
   * readable in about thirty seconds, which means: one line per row, the
   * reason on the same line as the thing it explains, and a positive sentence
   * where a section is empty rather than a blank panel. An empty section is an
   * answer — it is just a short one.
   *
   * The five sections are a grid that collapses to one column on a phone, and
   * every row is a link to the surface that owns it. The band never answers
   * anything itself.
   */
  import HealthBadge from "$lib/components/HealthBadge.svelte";

  let {
    /** `morningBriefModel` output, or null on a core with no brief. */
    brief = null,
  } = $props();

  let decisions = $derived(brief?.sections.decisions ?? null);
  let changes = $derived(brief?.sections.changes ?? null);
  let risk = $derived(brief?.sections.risk ?? null);
  let machine = $derived(brief?.sections.machine ?? null);
  let initiatives = $derived(brief?.sections.initiatives ?? null);

  /** Which digest groups are open. Counts first; items on request. */
  let opened = $state(new Set());
  function toggle(key) {
    const next = new Set(opened);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    opened = next;
  }
</script>

{#if brief}
  <section
    class="brief"
    aria-labelledby="overview-brief"
    data-overview-section="brief"
    data-brief-quiet={brief.quiet ? "true" : "false"}
  >
    <header class="brief__head">
      <h2 id="overview-brief" class="brief__title">This morning</h2>
      {#if brief.generatedAt}
        <span class="brief__stamp"
          >As of {new Date(brief.generatedAt).toLocaleTimeString(undefined, {
            hour: "numeric",
            minute: "2-digit",
          })}</span
        >
      {/if}
    </header>

    <div class="brief__grid">
      <!-- 1. Decisions, ranked. The reason is what makes a ranking usable:
           without it the top row is just an assertion. -->
      <section
        class="brief__panel brief__panel--wide"
        data-brief-panel="decisions"
      >
        <header class="brief__panel-head">
          <h3 class="brief__panel-title">{decisions.title}</h3>
          {#if decisions.status === "ok" && decisions.total}
            <a
              class="brief__link"
              href={decisions.moreHref}
              data-brief-decision-count
              >{decisions.total}{decisions.truncated ? "+" : ""} waiting</a
            >
          {/if}
        </header>
        {#if decisions.status === "unavailable"}
          <p
            class="brief__note brief__note--warn"
            role="status"
            data-brief-empty="decisions"
          >
            {decisions.message}
          </p>
        {:else if decisions.empty}
          <p class="brief__note" data-brief-empty="decisions">
            {decisions.emptyLine}
          </p>
        {:else}
          <ol class="brief__rows">
            {#each decisions.rows as row, index (row.id)}
              <li class="brief__row" data-brief-decision={row.id}>
                <span class="brief__rank" aria-hidden="true">{index + 1}</span>
                <a class="brief__row-link" href={row.href}>{row.title}</a>
                {#if row.reason}
                  <span class="brief__reason" data-brief-reason
                    >{row.reason}</span
                  >
                {/if}
              </li>
            {/each}
          </ol>
          {#if decisions.more}
            <a
              class="brief__more"
              href={decisions.moreHref}
              data-brief-more="decisions">+{decisions.more} more</a
            >
          {/if}
        {/if}
      </section>

      <!-- 2. Since you last looked. Counts read first; the items are one
           click behind them, because "4 finished" is the answer and the four
           titles are the follow-up question. -->
      <section class="brief__panel" data-brief-panel="changes">
        <header class="brief__panel-head">
          <h3 class="brief__panel-title">{changes.title}</h3>
          {#if changes.total}
            <span class="brief__stamp" data-brief-change-total
              >{changes.total}{changes.truncated ? "+" : ""} changes</span
            >
          {/if}
        </header>
        {#if changes.empty}
          <p class="brief__note" data-brief-empty="changes">
            {changes.emptyLine}
          </p>
        {:else}
          <ul class="brief__groups">
            {#each changes.groups as group (group.key)}
              <li data-brief-group={group.key}>
                <button
                  class="brief__group"
                  type="button"
                  aria-expanded={opened.has(group.key)}
                  onclick={() => toggle(group.key)}
                >
                  <span class="brief__group-count">{group.count}</span>
                  <span class="brief__group-label">{group.label}</span>
                </button>
                {#if opened.has(group.key)}
                  <ul class="brief__sub">
                    {#each group.rows as row (row.ref)}
                      <li>
                        <a class="brief__row-link" href={row.href}
                          >{row.title}</a
                        >
                      </li>
                    {/each}
                    {#if group.more}
                      <li class="brief__note">+{group.more} more</li>
                    {/if}
                  </ul>
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <!-- 3. At risk, with the computed reason. A row with no reason is not
           shown at all: core omits it rather than guessing, and a bare
           "at risk" chip is the kind of thing nobody can act on. -->
      <section class="brief__panel" data-brief-panel="risk">
        <header class="brief__panel-head">
          <h3 class="brief__panel-title">{risk.title}</h3>
          {#if risk.total}
            <span class="brief__stamp brief__stamp--warn" data-brief-risk-count
              >{risk.total}{risk.truncated ? "+" : ""}</span
            >
          {/if}
        </header>
        {#if risk.empty}
          <p class="brief__note" data-brief-empty="risk">{risk.emptyLine}</p>
        {:else}
          <ul class="brief__rows">
            {#each risk.rows as row (row.ref)}
              <li class="brief__row" data-brief-risk={row.ref}>
                <HealthBadge health={row.health} />
                <a class="brief__row-link" href={row.href}>{row.title}</a>
                <span class="brief__reason">{row.reason}</span>
              </li>
            {/each}
          </ul>
          {#if risk.more}
            <p class="brief__note" data-brief-more="risk">+{risk.more} more</p>
          {/if}
        {/if}
      </section>

      <!-- 4. Is the machine running. Three or four numbers, and only "stuck"
           is allowed to be loud: an offline agent is the normal state of most
           agents most of the time. The roster is one click away. -->
      <section
        class="brief__panel brief__panel--wide brief__panel--machine"
        data-brief-panel="machine"
      >
        <header class="brief__panel-head">
          <h3 class="brief__panel-title">{machine.title}</h3>
          <a class="brief__link" href={machine.moreHref}>Agents</a>
        </header>
        <ul class="brief__stats">
          {#each machine.stats as stat (stat.key)}
            <li data-brief-stat={stat.key}>
              <span
                class="brief__stat-value"
                class:brief__stat-value--warn={stat.tone === "warn"}
                >{stat.value}{machine.truncated ? "+" : ""}</span
              >
              <span class="brief__stat-label">{stat.label}</span>
            </li>
          {/each}
        </ul>
        {#if machine.rosterDown}
          <p
            class="brief__note brief__note--warn"
            role="status"
            data-brief-roster-down
          >
            {machine.message}
          </p>
        {/if}
        {#if machine.rows.length}
          <ul class="brief__rows">
            {#each machine.rows as row (row.ref)}
              <li class="brief__row" data-brief-stuck={row.ref}>
                <a class="brief__row-link" href={row.href}>{row.title}</a>
                <span class="brief__reason">{row.reason}</span>
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <!-- 5. Where each initiative is. The state counts are the honest part:
           seven initiatives reporting "no plan" is a sentence a reader can
           act on, where seven green chips were a lie. -->
      <section
        class="brief__panel brief__panel--wide"
        data-brief-panel="initiatives"
      >
        <header class="brief__panel-head">
          <h3 class="brief__panel-title">{initiatives.title}</h3>
          <a class="brief__link" href={initiatives.moreHref}
            >{initiatives.total}{initiatives.truncated ? "+" : ""} total</a
          >
        </header>
        {#if initiatives.empty}
          <p class="brief__note" data-brief-empty="initiatives">
            {initiatives.emptyLine}
          </p>
        {:else}
          {#if initiatives.chips.length}
            <ul class="brief__chips">
              {#each initiatives.chips as chip (chip.state)}
                <li
                  class="brief__chip brief__chip--{chip.tone}"
                  data-brief-state={chip.state}
                >
                  {chip.count}
                  {chip.label}
                </li>
              {/each}
            </ul>
          {/if}
          <ul class="brief__rows">
            {#each initiatives.rows as row (row.ref)}
              <li
                class="brief__row brief__row--initiative"
                data-brief-initiative={row.ref}
              >
                <HealthBadge health={row.health} />
                <a class="brief__row-link" href={row.href}>{row.title}</a>
                {#if row.progress}
                  <span
                    class="brief__progress"
                    data-brief-progress={row.progress.percent}
                  >
                    <span class="brief__track" aria-hidden="true">
                      <span
                        class="brief__fill"
                        style="width: {row.progress.percent}%"
                      ></span>
                    </span>
                    <span class="brief__progress-text"
                      >{row.progress.done}/{row.progress.total}</span
                    >
                  </span>
                {:else}
                  <span class="brief__reason">{row.reason}</span>
                {/if}
              </li>
            {/each}
          </ul>
          {#if initiatives.more}
            <a
              class="brief__more"
              href={initiatives.moreHref}
              data-brief-more="initiatives">+{initiatives.more} more</a
            >
          {/if}
        {/if}
      </section>
    </div>
  </section>
{/if}

<style>
  .brief {
    border: 1px solid var(--line);
    border-radius: var(--radius-md);
    background: var(--panel);
  }
  .brief__head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
    padding: 8px 12px;
    border-bottom: 1px solid var(--line);
  }
  .brief__title {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    color: var(--fg);
  }
  .brief__stamp {
    color: var(--fg-muted);
    font-size: 11px;
  }
  .brief__stamp--warn {
    color: var(--warn-text);
  }
  /*
   * Two columns on a desktop, one on a phone. Decisions, machine and
   * initiatives span both: a ranked row needs the width for its reason, a
   * progress bar needs it to be a bar rather than a dash, and an odd number
   * of half-width panels would leave a hole in the grid.
   */
  .brief__grid {
    display: grid;
    /*
     * `minmax(0, 1fr)`, not `1fr`: a grid item's automatic minimum is its
     * content, so one row whose reason does not wrap used to widen the whole
     * track and push the band 55px off the side of a phone.
     */
    grid-template-columns: minmax(0, 1fr);
    gap: 1px;
    background: var(--line-subtle);
  }
  @media (min-width: 720px) {
    .brief__grid {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }
    .brief__panel--wide {
      grid-column: 1 / -1;
    }
  }
  .brief__panel {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
    padding: 8px 12px 10px;
    background: var(--panel);
  }
  .brief__panel-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
  }
  .brief__panel-title {
    margin: 0;
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--fg-muted);
  }
  .brief__link,
  .brief__more {
    color: var(--accent-text);
    font-size: 11px;
    text-decoration: none;
  }
  .brief__link:hover,
  .brief__more:hover {
    text-decoration: underline;
  }
  .brief__note {
    margin: 0;
    color: var(--fg-muted);
    font-size: 12px;
  }
  .brief__note--warn {
    color: var(--warn-text);
  }
  .brief__rows,
  .brief__groups,
  .brief__sub,
  .brief__stats,
  .brief__chips {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  /*
   * One row, one line: marker, title, then the reason. The reason is allowed
   * to be clipped where the title is not — a title a reader cannot read is
   * useless, where a reason that ends in an ellipsis still carries its first
   * and most important clause.
   */
  .brief__row {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 2px 6px;
    padding: 3px 0;
    font-size: 12px;
    min-width: 0;
  }
  .brief__row + .brief__row {
    border-top: 1px solid var(--line-subtle);
  }
  .brief__rank {
    flex: none;
    width: 12px;
    color: var(--fg-subtle, var(--fg-muted));
    font-size: 11px;
    font-variant-numeric: tabular-nums;
  }
  .brief__row-link {
    min-width: 0;
    color: var(--fg);
    text-decoration: none;
    overflow-wrap: anywhere;
  }
  .brief__row-link:hover {
    color: var(--accent-text);
    text-decoration: underline;
  }
  .brief__reason {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    color: var(--fg-muted);
    font-size: 11px;
    text-align: right;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .brief__group {
    display: flex;
    align-items: baseline;
    gap: 6px;
    width: 100%;
    padding: 3px 0;
    border: 0;
    background: none;
    color: var(--fg);
    font: inherit;
    font-size: 12px;
    text-align: left;
    cursor: pointer;
  }
  .brief__group:hover .brief__group-label {
    text-decoration: underline;
  }
  .brief__group-count {
    min-width: 18px;
    color: var(--fg);
    font-variant-numeric: tabular-nums;
  }
  .brief__group-label {
    color: var(--fg-muted);
  }
  .brief__sub {
    padding: 0 0 4px 24px;
    font-size: 11px;
  }
  .brief__sub li {
    padding: 1px 0;
  }
  /* The numbers. Big enough to read without stopping, small enough that four
     of them are a line rather than a dashboard. */
  .brief__stats {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 16px;
  }
  /* The ticker row: numbers on the left, the silences that matter beside
     them rather than stranded under a half-empty panel. */
  @media (min-width: 720px) {
    .brief__panel--machine {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr);
      align-items: baseline;
      column-gap: 24px;
    }
    .brief__panel--machine .brief__panel-head {
      grid-column: 1 / -1;
    }
  }
  .brief__stats li {
    display: flex;
    align-items: baseline;
    gap: 4px;
  }
  .brief__stat-value {
    color: var(--fg);
    font-size: 16px;
    font-variant-numeric: tabular-nums;
  }
  .brief__stat-value--warn {
    color: var(--warn-text);
  }
  .brief__stat-label {
    color: var(--fg-muted);
    font-size: 11px;
  }
  .brief__chips {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 6px;
  }
  .brief__chip {
    padding: 0 6px;
    border: 1px solid var(--line);
    border-radius: var(--radius-full, 999px);
    background: var(--bg-soft);
    color: var(--fg-muted);
    font-size: 11px;
    white-space: nowrap;
  }
  .brief__chip--warn {
    border-color: var(--warn-text);
    color: var(--warn-text);
  }
  .brief__chip--danger {
    border-color: var(--danger-text);
    color: var(--danger-text);
  }
  /* Progress as a bar with its own numbers beside it, so an initiative reads
     "1/4" and looks like 25% in the same glance. */
  .brief__progress {
    flex: none;
    display: flex;
    align-items: center;
    gap: 6px;
    margin-left: auto;
  }
  .brief__track {
    width: 60px;
    height: 3px;
    overflow: hidden;
    border-radius: 2px;
    background: var(--line);
  }
  .brief__fill {
    display: block;
    height: 100%;
    background: var(--accent-text);
  }
  .brief__progress-text {
    color: var(--fg-muted);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
  }
  .brief__row--initiative .brief__row-link {
    flex: 1 1 auto;
    min-width: 0;
  }
  /*
   * On a phone the reason takes its own line under the title and reads
   * left-to-right like a sentence. Right-aligning it against a title that
   * already fills the row left them fighting over the same 200px.
   */
  @media (max-width: 719px) {
    .brief__reason {
      flex-basis: 100%;
      text-align: left;
    }
  }
</style>
