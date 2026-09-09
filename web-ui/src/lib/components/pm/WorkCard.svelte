<script>
  import SignalBadge from "./SignalBadge.svelte";
  import { sourceLabel } from "$lib/pm/presentation.js";
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
  let { work, href, boardTitle = "" } = $props();

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
  let critical = $derived(
    ["critical", "urgent", "p0"].includes(
      String(work?.priority ?? "")
        .trim()
        .toLowerCase(),
    ),
  );
</script>

<a
  {href}
  class="block rounded-md border border-line bg-panel px-3 py-2.5 transition-colors hover:border-line-strong hover:bg-panel-hover"
>
  <h3
    class="line-clamp-2 break-words text-meta font-medium leading-snug text-fg"
  >
    {work.title || "Untitled task"}
  </h3>
  {#if meta}
    <p class="mt-1 truncate text-micro text-fg-muted">{meta}</p>
  {/if}
  {#if blocked || critical}
    <div class="mt-2 flex flex-wrap items-center gap-1.5">
      {#if blocked}
        <SignalBadge tone="warn">Blocked</SignalBadge>
      {/if}
      {#if critical}
        <SignalBadge tone="danger">Critical</SignalBadge>
      {/if}
    </div>
  {/if}
</a>
