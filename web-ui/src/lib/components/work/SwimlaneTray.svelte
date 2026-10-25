<script>
  /**
   * Ungrouped: recent top-level cards and the homes core suggests for them.
   * Each home is one tap; "Other…" groups under any lane. A mouse can also
   * drag a row onto a lane (the board owns the drop); touch never drags.
   */
  import { formatAgo } from "$lib/time/format.js";
  import { homeReason } from "$lib/swimlanes.js";

  let {
    rows = [],
    hasMore = false,
    lanes = [],
    /** Refs with a grouping change waiting in the outbox. */
    pending = new Set(),
    now = Date.now(),
    onGroup,
    /** `(event, row)`: a pointer went down on a row. */
    onGrab = null,
    /** Keeps ids unique when the tray is drawn twice (inline and sheet). */
    idPrefix = "tray",
  } = $props();

  function other(row, event) {
    const select = event.currentTarget;
    const lane = lanes.find((entry) => entry.ref === select.value);
    select.value = "";
    if (lane) onGroup(row, lane);
  }
</script>

<ul class="divide-y divide-line-subtle" data-swimlane-tray-rows>
  {#each rows as row (row.ref)}
    {@const waiting = pending.has(row.ref)}
    {@const homes = row.homes}
    <li
      class="flex flex-wrap items-center gap-x-2.5 gap-y-2 py-2 text-meta {onGrab &&
      !waiting
        ? 'md:cursor-grab'
        : ''}"
      data-tray-ref={row.ref}
      onpointerdown={onGrab && !waiting
        ? (event) => onGrab(event, row)
        : undefined}
    >
      <span class="min-w-0 flex-[1_1_15rem]">
        <span class="text-fg [overflow-wrap:anywhere]">“{row.title}”</span>
        {#if row.createdAt}
          <span class="text-fg-muted">
            · created {formatAgo(row.createdAt, now) || "recently"}</span
          >
        {/if}
      </span>
      <span
        class="min-w-0 text-fg-muted [overflow-wrap:anywhere]"
        data-source="computed:next_actions"
      >
        {#if waiting}
          Grouping saved. Sending…
        {:else if homes.length}
          likely: {homes[0].title}{homeReason(homes[0].reasons)
            ? ` · ${homeReason(homes[0].reasons)}`
            : ""}
        {:else}
          no likely home
        {/if}
      </span>
      <span class="flex flex-wrap items-center gap-2">
        {#each homes as home (home.ref)}
          <button
            type="button"
            class="tray-take min-h-9"
            disabled={waiting}
            data-group-under={home.ref}
            onclick={() => onGroup(row, home)}>Group under {home.title}</button
          >
        {/each}
        {#if lanes.length}
          <label class="sr-only" for="{idPrefix}-other-{row.ref}"
            >Group “{row.title}” under another initiative</label
          >
          <select
            id="{idPrefix}-other-{row.ref}"
            class="tray-other min-h-9 w-auto max-w-[12rem]"
            disabled={waiting}
            onchange={(event) => other(row, event)}
          >
            <option value="">{homes.length ? "Other…" : "Group under…"}</option>
            {#each lanes as lane (lane.ref)}
              <option value={lane.ref}>{lane.title}</option>
            {/each}
          </select>
        {/if}
      </span>
    </li>
  {/each}
</ul>
{#if hasMore}
  <p class="pt-2 text-micro text-fg-muted">
    More recent cards are waiting. Group these to see the rest.
  </p>
{/if}

<style>
  /* The suggested home is the one-tap answer; "Other…" is the quiet one. */
  .tray-take {
    border: 1px solid color-mix(in srgb, var(--accent-solid) 45%, transparent);
    border-radius: 7px;
    background: var(--accent-soft);
    color: var(--accent-text);
    padding: 5px 10px;
    font-size: 12px;
  }
  .tray-take:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent-soft) 70%, var(--accent-solid));
  }
  .tray-other {
    border: 1px solid var(--line-strong);
    border-radius: 7px;
    background: transparent;
    color: var(--fg-muted);
    padding: 5px 8px;
    font-size: 12px;
  }
  .tray-take:disabled,
  .tray-other:disabled {
    opacity: 0.55;
  }
  .tray-take:focus-visible,
  .tray-other:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: 1px;
  }
</style>
