<script>
  /**
   * Ungrouped: recent top-level cards, who made them, and the homes core
   * suggests for them. Each home is one tap; "Other…" groups under any lane.
   * A mouse can also drag a row onto a lane (the board owns the drop); touch
   * never drags.
   *
   * Core's other two tray actions sit beside them. "Move to top level"
   * is a write the board sends (`onPromote`), the one-tap answer when no
   * home fits. "Move to another workspace" only shows the command core
   * suggests: a move needs a person with access to both workspaces, so
   * nothing here performs it.
   */
  import { formatAgo } from "$lib/time/format.js";
  import { homeReason } from "$lib/swimlanes.js";
  import CopyButton from "$lib/components/CopyButton.svelte";

  let {
    rows = [],
    hasMore = false,
    lanes = [],
    /** Refs with a grouping change waiting in the outbox. */
    pending = new Set(),
    now = Date.now(),
    onGroup,
    /** `(row)`: make the card an initiative of its own. */
    onPromote = null,
    /** `(event, row)`: a pointer went down on a row. */
    onGrab = null,
    /** Keeps ids unique when the tray is drawn twice (inline and sheet). */
    idPrefix = "tray",
  } = $props();

  // Option values for the actions in "Other…"; a lane's value is its ref.
  const PROMOTE = "+promote";
  const MOVE = "+move";
  /** The row whose move command is showing. */
  let moving = $state("");

  function other(row, event) {
    const select = event.currentTarget;
    const value = select.value;
    select.value = "";
    if (value === PROMOTE) return onPromote?.(row);
    if (value === MOVE) {
      moving = row.ref;
      return;
    }
    const lane = lanes.find((entry) => entry.ref === value);
    if (lane) onGroup(row, lane);
  }
  const canPromote = (row) => Boolean(row.promote && onPromote);
</script>

<ul class="divide-y divide-line-subtle" data-swimlane-tray-rows>
  {#each rows as row (row.ref)}
    {@const waiting = pending.has(row.ref)}
    {@const homes = row.homes}
    {@const promoteFirst = !homes.length && canPromote(row)}
    {@const extra = (!promoteFirst && canPromote(row)) || Boolean(row.move)}
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
        {#if row.createdBy}
          <span
            class="text-fg-muted"
            data-source="computed:ungrouped.created_by"
            >{row.createdAt ? " by " : " · by "}{row.createdBy}</span
          >
        {/if}
      </span>
      <span
        class="min-w-0 text-fg-muted [overflow-wrap:anywhere]"
        data-source="computed:next_actions"
      >
        {#if waiting}
          Saved. Sending…
        {:else if homes.length}
          likely: {homes[0].title}{homeReason(homes[0].reasons)
            ? ` · ${homeReason(homes[0].reasons)}`
            : ""}
        {:else}
          no likely home
        {/if}
      </span>
      <span class="flex flex-wrap items-center gap-2">
        {#if promoteFirst}
          <button
            type="button"
            class="tray-take min-h-9"
            disabled={waiting}
            data-source="computed:next_actions"
            data-promote={row.ref}
            onclick={() => onPromote(row)}>Move to top level</button
          >
        {/if}
        {#each homes as home (home.ref)}
          <button
            type="button"
            class="tray-take min-h-9"
            disabled={waiting}
            data-group-under={home.ref}
            onclick={() => onGroup(row, home)}>Move under {home.title}</button
          >
        {/each}
        {#if lanes.length || extra}
          <label class="sr-only" for="{idPrefix}-other-{row.ref}"
            >Move “{row.title}” under another initiative, or another action</label
          >
          <select
            id="{idPrefix}-other-{row.ref}"
            class="tray-other min-h-9 w-auto max-w-[12rem]"
            disabled={waiting}
            onchange={(event) => other(row, event)}
          >
            <option value="">{homes.length ? "Other…" : "Move under…"}</option>
            {#each lanes as lane (lane.ref)}
              <option value={lane.ref}>{lane.title}</option>
            {/each}
            {#if extra}
              <optgroup label="Or">
                {#if !promoteFirst && canPromote(row)}
                  <option value={PROMOTE}>Move to top level</option>
                {/if}
                {#if row.move}
                  <option value={MOVE}>Move to another workspace…</option>
                {/if}
              </optgroup>
            {/if}
          </select>
        {/if}
      </span>
      {#if moving === row.ref && row.move}
        <div
          class="flex w-full flex-wrap items-center gap-2 rounded-md bg-panel px-3 py-2 text-micro text-fg-muted"
          role="group"
          aria-label="Move “{row.title}” to another workspace"
          data-source="computed:next_actions"
          data-tray-move={row.ref}
        >
          <span class="w-full"
            >A person with access to both workspaces moves a card. Run this,
            with the workspace it should go to:</span
          >
          <code
            class="min-w-0 flex-1 rounded bg-bg-soft px-2 py-1 font-mono text-fg [overflow-wrap:anywhere]"
            >{row.move}</code
          >
          <CopyButton value={row.move} label="Copy the move command" />
          <button
            type="button"
            class="ui-prose-link"
            onclick={() => (moving = "")}>Close</button
          >
        </div>
      {/if}
    </li>
  {/each}
</ul>
{#if hasMore}
  <p class="pt-2 text-micro text-fg-muted">
    More recent cards are waiting. Move these to see the rest.
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
