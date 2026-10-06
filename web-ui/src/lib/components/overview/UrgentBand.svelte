<script>
  /**
   * The one urgent band at the top of the Overview.
   *
   * It is the first thing on the page because it is the only thing on the page
   * that might need doing in the next minute: open asks for the reader across
   * every workspace they can reach, then initiatives that have stopped moving.
   *
   * It is not a second Inbox. Each row links to the surface that owns it — the
   * right workspace's Inbox for an ask, the initiative page for an initiative
   * — and the band never offers to answer anything itself.
   *
   * Empty is the common case and it stays one line, because a dashboard whose
   * top third says "nothing is waiting" in a big empty box has spent a third
   * of the screen on good news.
   */
  import AgeBadge from "$lib/components/AgeBadge.svelte";
  import HealthBadge from "$lib/components/HealthBadge.svelte";

  let {
    /** `urgentBandModel` output. */
    band = null,
    /** `(path, workspace) => href` — the ask's own workspace, not this one. */
    hrefFor = (path) => path,
    /** True while the cross-workspace fan-out is still in flight. */
    loading = false,
  } = $props();

  let asks = $derived(band?.asks ?? { rows: [], count: 0 });
  /**
   * Which workspace a row came from, but only when that is news. On a
   * self-hosted single-workspace deployment every row says "Local", which is
   * a column of noise rather than a fact.
   */
  let showWorkspace = $derived((asks.workspaces ?? 0) > 1);
  let initiatives = $derived(band?.initiatives ?? { rows: [], count: 0 });
  let unavailable = $derived(band?.unavailable ?? []);
  /**
   * Workspaces this browser has no session for. Hosted writes a session per
   * workspace, so a reader who has not opened one simply is not covered — a
   * fact about scope, not a failure, and not a reason for the band to look
   * broken when nothing is waiting.
   */
  let notCovered = $derived(band?.notCovered ?? []);
  /*
   * Two lists, two sentences. "Not included" is a workspace nobody asked —
   * normal, and quiet. "Could not be read" is a workspace that answered with a
   * failure — rarer, and worth saying out loud. Merging them told a reader
   * that a workspace they had simply never opened had failed.
   */
  let notCoveredNames = $derived(
    notCovered.map((entry) => entry.label || entry.slug).filter(Boolean),
  );
  let failedNames = $derived(
    unavailable.map(
      (entry) =>
        entry.workspace?.label || entry.workspace?.slug || "one workspace",
    ),
  );
  /*
   * A read that failed is not "nothing is waiting on you" — we do not know
   * what is waiting. Only a band that read everything it meant to can say the
   * page is clear. A workspace nobody asked does not count: that one is a
   * footnote about scope, and the band is still honestly empty.
   */
  let empty = $derived(
    asks.count === 0 && initiatives.count === 0 && failedNames.length === 0,
  );
</script>

<section
  class="urgent"
  class:urgent--empty={empty}
  aria-labelledby="overview-urgent"
  data-overview-section="urgent"
  data-urgent-state={empty ? "empty" : "active"}
>
  <header class="urgent__head">
    <h2 id="overview-urgent" class="urgent__title">Needs you</h2>
    {#if loading}
      <span class="urgent__note" role="status">Checking workspaces…</span>
    {:else if empty}
      <span class="urgent__note" data-urgent-empty
        >Nothing is waiting on you.</span
      >
    {:else}
      <span class="urgent__counts">
        {#if asks.count}
          <span data-urgent-ask-count
            >{asks.count}{asks.truncated ? "+" : ""}
            {asks.count === 1 ? "ask" : "asks"}</span
          >
        {/if}
        {#if initiatives.count}
          <span data-urgent-initiative-count
            >{initiatives.count}
            {initiatives.count === 1 ? "initiative needs" : "initiatives need"} attention</span
          >
        {/if}
      </span>
    {/if}
  </header>

  {#if !empty}
    <ul class="urgent__rows">
      {#each asks.rows as row (`${row.workspace?.slug ?? ""}:${row.id}`)}
        <li class="urgent__row" data-urgent-ask={row.id}>
          <a class="urgent__link" href={hrefFor(row.href, row.workspace)}>
            <span class="urgent__row-title">{row.title}</span>
          </a>
          <span class="urgent__row-meta">
            {#if showWorkspace && row.workspace?.label}
              <span class="urgent__chip" data-urgent-workspace
                >{row.workspace.label}</span
              >
            {/if}
            {#if row.source}<span>{row.source}</span>{/if}
          </span>
        </li>
      {/each}

      {#each initiatives.rows as tile (tile.ref)}
        <li class="urgent__row" data-urgent-initiative={tile.ref}>
          <a class="urgent__link" href={tile.href}>
            <span class="urgent__row-title">{tile.title}</span>
          </a>
          <span class="urgent__row-meta">
            <HealthBadge health={tile.health} variant="pill" />
            {#if tile.movedAt}
              <AgeBadge at={tile.movedAt} verb="moved" />
            {/if}
          </span>
        </li>
      {/each}
    </ul>

    {#if asks.truncated || initiatives.truncated}
      <p class="urgent__note" data-urgent-truncated>
        More is waiting than fits here. Open the Inbox for the rest.
      </p>
    {/if}
  {/if}

  {#if failedNames.length}
    <!-- A read that answered with a failure. Louder than the coverage line
         below it, because the band genuinely does not know what it missed. -->
    <p class="urgent__note urgent__note--warn" role="status" data-urgent-failed>
      {failedNames.length === 1
        ? "One workspace"
        : `${failedNames.length} workspaces`}
      could not be read, so this may be short: {failedNames.join(", ")}.
    </p>
  {/if}

  {#if notCoveredNames.length}
    <!--
      Workspaces this browser has no session for. Hosted writes a session per
      workspace, so a reader who has not opened one simply is not covered —
      a fact about scope, not a failure. It used to shout "could not be read"
      in warning amber, which read as an outage.
    -->
    <p
      class="urgent__note urgent__coverage"
      data-urgent-coverage={notCoveredNames.length}
    >
      Covers the workspaces you have open. Not included: {notCoveredNames.join(
        ", ",
      )}.
    </p>
  {/if}
</section>

<style>
  .urgent {
    border: 1px solid var(--line);
    border-radius: var(--radius-md);
    background: var(--panel);
  }
  /* Something is wrong: the band says so at the edge, not with a fill that
     would make the page's first third shout on every visit. */
  .urgent:not(.urgent--empty) {
    border-left: 3px solid var(--warn-text);
  }
  .urgent__head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
    padding: 8px 12px;
  }
  .urgent:not(.urgent--empty) .urgent__head {
    border-bottom: 1px solid var(--line);
  }
  .urgent__title {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    color: var(--fg);
  }
  .urgent__counts {
    display: flex;
    flex-wrap: wrap;
    gap: 2px 10px;
    color: var(--warn-text);
    font-size: 12px;
  }
  .urgent__note {
    color: var(--fg-muted);
    font-size: 12px;
  }
  .urgent__head .urgent__note {
    margin: 0;
  }
  .urgent__rows,
  p.urgent__note {
    margin: 0;
  }
  .urgent__rows {
    padding: 0;
    list-style: none;
  }
  .urgent__row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 4px 10px;
    padding: 6px 12px;
    border-top: 1px solid var(--line-subtle);
  }
  .urgent__row:first-child {
    border-top: 0;
  }
  .urgent__link {
    min-width: 0;
    flex: 1 1 14rem;
    color: var(--fg);
    font-size: 12px;
    text-decoration: none;
  }
  .urgent__link:hover .urgent__row-title {
    color: var(--accent-text);
    text-decoration: underline;
  }
  .urgent__row-title {
    display: block;
    overflow-wrap: anywhere;
  }
  .urgent__row-meta {
    display: flex;
    flex: none;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    color: var(--fg-muted);
    font-size: 11px;
  }
  /* Which workspace a row came from. The fan-out is the point of the band, so
     a row from elsewhere has to say where it is from. */
  .urgent__chip {
    padding: 0 6px;
    border: 1px solid var(--line);
    border-radius: var(--radius-full, 999px);
    background: var(--bg-soft);
    white-space: nowrap;
  }
  p.urgent__note {
    padding: 6px 12px;
  }
  .urgent__note--warn {
    color: var(--warn-text);
  }
  .urgent__coverage {
    color: var(--fg-subtle, var(--fg-muted));
    font-size: 11px;
  }
  /* An empty band with a coverage footnote is still one quiet line of chrome. */
  .urgent--empty .urgent__coverage {
    padding-top: 0;
  }
</style>
