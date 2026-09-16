<script>
  import SignalBadge from "./SignalBadge.svelte";
  import { sourceLabel, workFreshness } from "$lib/pm/presentation.js";
  import { formatTimestamp } from "$lib/formatDate";
  import {
    actorDisplayLabel,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";

  /**
   * One board card: a title and one meta line. Everything else a card used to
   * carry — a freshness badge on every card, a blocker count, a separate
   * progress line — repeated what the column and the row already said.
   */
  let {
    work,
    href,
    boardTitle = "",
    requested = false,
    requestedHref = "",
  } = $props();

  let ownerLabel = $derived(
    actorDisplayLabel(work?.owner, $actorRegistry, $principalRegistry),
  );
  let boardLabel = $derived(
    boardTitle || String(work?.board_ref ?? "").replace(/^board:/, ""),
  );
  let age = $derived(
    formatTimestamp(work?.freshness?.last_observed_at || work?.updated_at) ||
      "",
  );
  let meta = $derived(
    [ownerLabel, boardLabel || sourceLabel(work?.source), age]
      .filter(Boolean)
      .join(" · "),
  );
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
    class="work-card-link block px-3 py-2.5"
    ondragstart={(event) => event.preventDefault()}
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
  {#if showSignals}
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
            title="Answer this request in Inbox"
            ondragstart={(event) => event.preventDefault()}
          >
            <SignalBadge tone="warn">Requested</SignalBadge>
          </a>
        {:else}
          <SignalBadge tone="warn">Requested</SignalBadge>
        {/if}
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
