<script>
  /**
   * One card, at card density.
   *
   * The shell around `WorkSummary`: a title, a plain-text excerpt of the body
   * and the one pill the Overview is allowed for a decision waiting on this
   * card. Everything that is state — status, the stored phase, progress, the
   * step lists, the next step, owner, due, age, asks — is the summary's, so a
   * card here and a row in the Tasks table cannot say different things.
   *
   * `card` is a `workSummaryCards` entry.
   */
  import WorkSummary from "$lib/components/WorkSummary.svelte";

  let {
    card,
    now = Date.now(),
    /** Reads the card ahead of the click; see `workCache.js`. */
    onprefetch = null,
  } = $props();
</script>

<a
  class="card"
  href={card.href}
  data-initiative-tile={card.ref}
  data-tile-health={card.summary?.status?.state || "unknown"}
  onpointerenter={() => onprefetch?.(card.ref)}
  onfocus={() => onprefetch?.(card.ref)}
>
  <!--
    Title first, on a line of its own. The status badge and the age used to
    sit beside it, which left a long name competing for width with two things
    that are each two words wide.
  -->
  <span class="card-head">
    <span class="card-title">{card.title}</span>
  </span>

  <span class="card-status" data-tile-status>
    <WorkSummary
      summary={card.summary}
      density="card"
      title={card.title}
      {now}
    />
  </span>

  {#if card.excerpt}
    <!-- A plain line. The body is markdown; a card rendering it verbatim
         read `**Goal:** ship the…`. -->
    <span class="card-excerpt" data-tile-excerpt>{card.excerpt}</span>
  {/if}

  {#if card.needs.length}
    <!-- A pill on the card the decision belongs to, rather than a second
         copy of the Inbox on the dashboard. -->
    <span class="card-needs" data-tile-needs
      >Needs you: {card.needs[0]}{card.needs.length > 1
        ? ` +${card.needs.length - 1}`
        : ""}</span
    >
  {/if}
</a>

<style>
  .card {
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
  .card:hover {
    border-color: var(--line-strong);
    background: var(--panel-hover, var(--bg-soft));
  }
  .card:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: 2px;
  }
  /* The worst state gets an edge, so the sort is visible and not just true. */
  .card[data-tile-health="blocked"] {
    border-left: 3px solid var(--danger-text, var(--warn-text));
  }
  .card[data-tile-health="at_risk"],
  .card[data-tile-health="stale"] {
    border-left: 3px solid var(--warn-text);
  }
  .card-head {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }
  .card-title {
    min-width: 0;
    font-size: 15px;
    font-weight: 600;
    line-height: 1.3;
    overflow-wrap: anywhere;
  }
  .card:hover .card-title {
    color: var(--accent-text);
  }
  .card-status {
    display: block;
    min-width: 0;
    margin-top: -6px;
  }
  .card-excerpt {
    color: var(--fg-muted);
    font-size: 13px;
    line-height: 1.5;
    /* Two lines: a card is a glance, not the body. */
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  /*
   * Phone density: 18px of padding and 12px of gap on a 17rem card spends
   * about a fifth of its height on air, and a column of them is most of a
   * scroll. The card keeps its shape and loses the margin.
   */
  @media (max-width: 640px) {
    .card {
      gap: 9px;
      padding: 13px 14px;
      border-radius: 10px;
    }
    .card-title {
      font-size: 14.5px;
    }
  }
  .card-needs {
    justify-self: start;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--bg-soft);
    color: var(--warn-text);
    font-size: 11px;
    overflow-wrap: anywhere;
  }
</style>
