<script>
  import FreshnessBadge from "$lib/components/FreshnessBadge.svelte";
  import SignalBadge from "./SignalBadge.svelte";
  import {
    isNexusOwned,
    sourceLabel,
    workFreshness,
  } from "$lib/pm/presentation.js";
  import { freshnessKindForPhase } from "$lib/freshness.js";
  import {
    actorDisplayLabel,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";

  /**
   * One board card: a title, one status line, one meta line. Everything else a
   * card used to carry — a blocker count, a separate progress line — repeated
   * what the column and the row already said.
   *
   * The title owns its full width. The status badges and the freshness badge
   * share the line under it, so a long task name is never squeezed by a pill
   * it is not competing with for attention.
   */
  let {
    work,
    href,
    boardTitle = "",
    requested = false,
    requestedHref = "",
    /** Reference time, injectable so the freshness badge is testable. */
    now = Date.now(),
    /** Primes the card cache and prefetches on hover; see `workPrefetch`. */
    onprefetch = null,
  } = $props();

  let ownerLabel = $derived(
    actorDisplayLabel(work?.owner, $actorRegistry, $principalRegistry),
  );
  // The board name only when the parent says it tells boards apart; a
  // source-owned card says where it lives instead.
  let placeLabel = $derived(
    boardTitle || (isNexusOwned(work) ? "" : sourceLabel(work?.source)),
  );
  let meta = $derived([ownerLabel, placeLabel].filter(Boolean).join(" · "));
  // When it last moved, judged against the cadence its phase implies: a card
  // in progress is expected daily, one in a backlog every fortnight.
  let movedAt = $derived(
    work?.freshness?.last_observed_at || work?.updated_at || "",
  );
  // Phase and lifecycle both end a card: done, cancelled, archived, trashed.
  let freshnessKind = $derived(freshnessKindForPhase(work?.phase, work?.state));
  let blocked = $derived(work?.phase === "blocked");
  // A read that is failing is worth a badge: the reader cannot tell from
  // the column that this card's evidence is going stale.
  let readError = $derived(workFreshness(work));
  let critical = $derived(
    ["critical", "urgent", "p0"].includes(
      String(work?.priority ?? "")
        .trim()
        .toLowerCase(),
    ),
  );
  let showSignals = $derived(
    blocked || critical || readError.key === "error" || requested,
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
    {#if meta}
      <p class="mt-1 truncate text-micro text-fg-muted">{meta}</p>
    {/if}
  </a>
  {#if showSignals || movedAt}
    <div class="flex flex-wrap items-center gap-1.5 px-3 pb-2.5">
      {#if blocked}
        <SignalBadge tone="warn">Blocked</SignalBadge>
      {/if}
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
      {#if movedAt}
        <FreshnessBadge
          class="ml-auto"
          at={movedAt}
          kind={freshnessKind}
          row={work}
          verb="updated"
          {now}
        />
      {/if}
    </div>
  {/if}
</div>

<style>
  /* Native link dragging cancels our pointer session. Keep the title a
     link for click-to-open, but never let the browser pick up the URL. */
  .work-card-link {
    -webkit-user-drag: none;
  }
</style>
