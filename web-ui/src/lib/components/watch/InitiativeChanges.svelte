<script>
  /**
   * Recent changes to the initiative set: who added, retired or moved an
   * initiative in the last week, and why. Core's `initiative_changes` (its
   * newest 50), kept to the last `CHANGES_DAYS` days here; read only, since
   * agents manage the set and the reader decides in Decide.
   *
   * Always starts closed: a heading with a count pill ("5 in 7 days").
   * Open, it lists the latest few with the rest one click away. A card
   * beside the list on a wide screen; a fold under it on a phone.
   */
  import {
    actorDisplayLabel,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import { changeLine } from "$lib/watchRollup.js";
  import {
    CHANGES_DAYS,
    CHANGES_PREVIEW,
    recentChanges,
  } from "$lib/watchList.js";

  let {
    /** `brief.initiativeChanges`. */
    changes = { items: [], truncated: false },
    now = Date.now(),
    /** `(ref) => href` for the changed card. */
    hrefFor,
  } = $props();

  let items = $derived(recentChanges(changes, now));
  let open = $state(false);
  let showAll = $state(false);
  let shown = $derived(showAll ? items : items.slice(0, CHANGES_PREVIEW));
  // Every change core sent is from this week, and it sent its limit.
  let clipped = $derived(
    Boolean(changes?.truncated) &&
      items.length > 0 &&
      items.length === (changes?.items?.length ?? 0),
  );
  let pill = $derived(
    items.length
      ? `${items.length} in ${CHANGES_DAYS} days`
      : `None in ${CHANGES_DAYS} days`,
  );
  const actorLabel = (id) =>
    actorDisplayLabel(id, $actorRegistry, $principalRegistry);
  const listId = "initiative-changes-list";
</script>

<section
  class="changes"
  aria-labelledby="initiative-changes-heading"
  data-source="computed:initiative_changes"
  data-initiative-changes
  data-open={String(open)}
>
  <h2 id="initiative-changes-heading" class="m-0 text-meta">
    {#if items.length}
      <button
        type="button"
        class="toggle"
        aria-expanded={open}
        aria-controls={open ? listId : undefined}
        onclick={() => (open = !open)}
        data-initiative-changes-toggle
      >
        <span class="font-semibold text-fg">Recent changes</span>
        <span class="pill" data-initiative-changes-count>{pill}</span>
        <span class="ml-auto text-fg-muted">{open ? "Hide" : "Show"}</span>
      </button>
    {:else}
      <span class="toggle">
        <span class="font-semibold text-fg">Recent changes</span>
        <span class="pill" data-initiative-changes-count>{pill}</span>
      </span>
    {/if}
  </h2>
  {#if open && items.length}
    <ul id={listId} class="mt-1">
      {#each shown as change (change.ref)}
        {@const line = changeLine(change, actorLabel, now)}
        <li
          class="change text-meta text-fg-muted [overflow-wrap:anywhere]"
          data-initiative-change={change.type}
        >
          <span class="font-medium text-fg">{line.verb}</span>
          <a
            class="text-fg underline decoration-line underline-offset-2 hover:text-accent-text"
            href={hrefFor(change.subjectRef)}>{line.title}</a
          >
          — by {line.actor}{#if line.reason}: {line.reason}{/if}{#if line.time}<span
              class="sr-only">,</span
            ><span aria-hidden="true" class="mx-1 text-fg-subtle">·</span><time
              datetime={change.ts}
              title={change.ts}>{line.time}</time
            >{/if}
        </li>
      {/each}
    </ul>
    {#if items.length > CHANGES_PREVIEW || clipped}
      <p class="mt-1 text-meta text-fg-muted">
        {#if items.length > CHANGES_PREVIEW}
          <button
            type="button"
            class="more text-accent-text hover:underline"
            aria-expanded={showAll}
            aria-controls={listId}
            onclick={() => (showAll = !showAll)}
            data-initiative-changes-more
            >{showAll
              ? "Show fewer"
              : `Show ${items.length - CHANGES_PREVIEW} more`}</button
          >
        {/if}
        {#if clipped}
          <span data-initiative-changes-truncated
            >Only the newest {items.length} are shown.</span
          >
        {/if}
      </p>
    {/if}
  {/if}
</section>

<style>
  .changes {
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    background: var(--panel);
    padding: 4px 14px 8px;
  }
  .toggle {
    display: flex;
    width: 100%;
    align-items: center;
    gap: 8px;
    min-height: 2.5rem;
    border-radius: 4px;
    text-align: left;
  }
  .pill {
    border: 1px solid var(--line);
    border-radius: 999px;
    padding: 0 8px;
    color: var(--fg-muted);
    font-size: 12px;
    line-height: 18px;
    white-space: nowrap;
  }
  .change {
    padding: 6px 0;
    border-top: 1px solid var(--line-subtle);
  }
  .toggle:focus-visible,
  .more:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: 2px;
  }
  .more {
    border-radius: 4px;
  }
  /* On a phone the card reads as a fold line under the list. */
  @container (max-width: 60rem) {
    .changes {
      border-width: 1px 0 0;
      border-radius: 0;
      background: transparent;
      padding: 0 0 8px;
    }
  }
</style>
