<script>
  /**
   * Recent changes to the initiative set: who added, retired or moved an
   * initiative, and why. Core's `initiative_changes`, newest first; read
   * only, since agents manage the set and the reader decides in Decide.
   *
   * Nothing renders without a change. The list starts open while the newest
   * change is under a week old, and otherwise starts as one closed line.
   * Open, it shows the latest few with the rest one click away.
   */
  import {
    actorDisplayLabel,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import {
    CHANGES_PREVIEW,
    changeLine,
    changesStartOpen,
  } from "$lib/watchRollup.js";

  let {
    /** `brief.initiativeChanges`. */
    changes = { items: [], truncated: false },
    now = Date.now(),
    /** `(ref) => href` for the changed card. */
    hrefFor,
  } = $props();

  let items = $derived(changes?.items ?? []);
  // The reader's own choice wins over the default once they make one.
  let choice = $state(null);
  let open = $derived(choice ?? changesStartOpen(changes, now));
  let showAll = $state(false);
  let shown = $derived(showAll ? items : items.slice(0, CHANGES_PREVIEW));
  let countText = $derived(`${items.length}${changes?.truncated ? "+" : ""}`);
  const actorLabel = (id) =>
    actorDisplayLabel(id, $actorRegistry, $principalRegistry);
  const listId = "initiative-changes-list";
</script>

{#if items.length}
  <section
    class="text-micro"
    aria-labelledby="initiative-changes-heading"
    data-source="computed:initiative_changes"
    data-initiative-changes
    data-open={String(open)}
  >
    <h2 id="initiative-changes-heading" class="m-0">
      <button
        type="button"
        class="toggle flex items-center gap-1.5 font-medium text-fg-muted hover:text-fg"
        aria-expanded={open}
        aria-controls={open ? listId : undefined}
        onclick={() => (choice = !open)}
        data-initiative-changes-toggle
        ><svg
          class="h-2.5 w-2.5 transition-transform {open ? 'rotate-90' : ''}"
          viewBox="0 0 20 20"
          fill="currentColor"
          aria-hidden="true"><path d="M7 4l7 6-7 6V4z" /></svg
        >Recent changes to initiatives
        <span class="text-fg-subtle">({countText})</span></button
      >
    </h2>
    {#if open}
      <ul id={listId} class="mt-1.5 space-y-1 pl-4">
        {#each shown as change (change.ref)}
          {@const line = changeLine(change, actorLabel, now)}
          <li
            class="text-fg-muted [overflow-wrap:anywhere]"
            data-initiative-change={change.type}
          >
            <span class="font-medium text-fg">{line.verb}</span>
            <a
              class="text-fg underline decoration-line underline-offset-2 hover:text-accent-text"
              href={hrefFor(change.subjectRef)}>{line.title}</a
            >
            — by {line.actor}{#if line.reason}: {line.reason}{/if}{#if line.time}<span
                class="sr-only">,</span
              ><span aria-hidden="true" class="mx-1 text-fg-subtle">·</span
              ><time datetime={change.ts} title={change.ts}>{line.time}</time
              >{/if}
          </li>
        {/each}
      </ul>
      {#if items.length > CHANGES_PREVIEW || changes?.truncated}
        <p class="mt-1 pl-4 text-fg-subtle">
          {#if items.length > CHANGES_PREVIEW}
            <button
              type="button"
              class="toggle text-accent-text hover:underline"
              aria-expanded={showAll}
              aria-controls={listId}
              onclick={() => (showAll = !showAll)}
              data-initiative-changes-more
              >{showAll
                ? "Show fewer"
                : `Show ${items.length - CHANGES_PREVIEW} more`}</button
            >
          {/if}
          {#if changes?.truncated}
            <span data-initiative-changes-truncated
              >Older changes aren't shown.</span
            >
          {/if}
        </p>
      {/if}
    {/if}
  </section>
{/if}

<style>
  .toggle {
    border-radius: 4px;
  }
  .toggle:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: 2px;
  }
</style>
