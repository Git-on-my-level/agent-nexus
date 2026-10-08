<script>
  import SignalBadge from "./SignalBadge.svelte";
  import FreshnessBadge from "$lib/components/FreshnessBadge.svelte";
  import WorkSummary from "$lib/components/WorkSummary.svelte";
  import {
    isNexusOwned,
    sourceLabel,
    workFreshness,
  } from "$lib/pm/presentation.js";
  import { workSummaryModel } from "$lib/workSummary.js";
  import {
    actorDisplayLabel,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";

  /**
   * One board card: a title, the shared summary, and the two signals that are
   * about the card rather than its state — a failing source read and a request
   * waiting in the Inbox.
   *
   * Status, the stored phase when it disagrees with it, progress and the age
   * all come from `WorkSummary`, which is also what the Tasks table and the
   * Overview render. The card used to decide "Blocked" for itself from
   * `work.phase`, so the same task could read Blocked here, In progress in the
   * table and At risk on the dashboard.
   *
   * The title owns its full width. The badges share the line under it, so a
   * long task name is never squeezed by a pill it is not competing with for
   * attention.
   */
  let {
    work,
    /**
     * A `workSummaryModel` result. The parent builds one per row so a board
     * of fifty cards models each once; a card given none models its own.
     */
    summary = null,
    href,
    boardTitle = "",
    requested = false,
    requestedHref = "",
    /** Reference time, injectable so the age badge is testable. */
    now = Date.now(),
    /** Primes the card cache and prefetches on hover; see `workCache.js`. */
    onprefetch = null,
  } = $props();

  let model = $derived(summary ?? workSummaryModel(work, { now }));
  let ownerLabel = $derived(
    actorDisplayLabel(work?.owner, $actorRegistry, $principalRegistry),
  );
  // The board name only when the parent says it tells boards apart; a
  // source-owned card says where it lives instead.
  let placeLabel = $derived(
    boardTitle || (isNexusOwned(work) ? "" : sourceLabel(work?.source)),
  );
  // A read that is failing is worth a badge: the reader cannot tell from the
  // column that this card's evidence is going stale.
  let readError = $derived(workFreshness(work, now));
  let critical = $derived(
    ["critical", "urgent", "p0"].includes(
      String(work?.priority ?? "")
        .trim()
        .toLowerCase(),
    ),
  );
  let showSignals = $derived(
    critical || readError.key === "error" || requested,
  );
</script>

<div
  class="rounded-md border border-line bg-panel transition-colors hover:border-line-strong hover:bg-panel-hover"
>
  <a
    {href}
    draggable="false"
    class="work-card-link block px-3 pb-1.5 pt-2.5"
    ondragstart={(event) => event.preventDefault()}
    onpointerenter={() => onprefetch?.(work)}
    onfocus={() => onprefetch?.(work)}
  >
    <h3
      class="line-clamp-2 break-words text-meta font-medium leading-snug text-fg"
    >
      {work.title || "Untitled task"}
    </h3>
  </a>
  <div class="flex flex-wrap items-center gap-1.5 px-3 pb-2.5">
    <WorkSummary
      summary={model}
      density="row"
      title={work.title}
      {ownerLabel}
      {placeLabel}
      {now}
    />
    {#if showSignals}
      {#if readError.key === "error"}
        <SignalBadge tone="warn">{readError.label}</SignalBadge>
      {/if}
      {#if critical}
        <SignalBadge tone="danger">Critical</SignalBadge>
      {/if}
      {#if requested}
        {#if requestedHref}
          <a
            class="inline-flex rounded-sm outline-none focus-visible:ring-1 focus-visible:ring-accent"
            href={requestedHref}
            draggable="false"
            aria-label="Answer this request in Inbox"
            ondragstart={(event) => event.preventDefault()}
          >
            <SignalBadge tone="warn">Requested</SignalBadge>
          </a>
        {:else}
          <SignalBadge tone="warn">Requested</SignalBadge>
        {/if}
      {/if}
    {/if}
    {#if model?.freshness}
      <!--
        When it last moved, judged against the cadence the summary carries.
        Row density leaves the age to the host because a table has a Last
        checked column of its own; a board column has nowhere else to put it,
        so the card places it here, at the end of the badge line.
      -->
      <FreshnessBadge
        class="ml-auto"
        at={model.lastMovementAt}
        kind={model.freshnessKind}
        expectationHours={model.expectationHours}
        verb="moved"
        {now}
      />
    {/if}
  </div>
</div>

<style>
  /* Native link dragging cancels our pointer session. Keep the title a
     link for click-to-open, but never let the browser pick up the URL. */
  .work-card-link {
    -webkit-user-drag: none;
  }
</style>
