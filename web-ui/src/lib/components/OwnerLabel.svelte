<script>
  import ActorAvatar from "$lib/components/ActorAvatar.svelte";
  import { navIconPath } from "$lib/icons.js";

  /**
   * One owner in a dense list: a name, an agent marked as an agent, and the
   * role / host / handle in the title.
   *
   * The cell comes from `buildOwnerDisplay` so the page resolves ids once for
   * the whole list. A pending cell is a placeholder of the same height and
   * position as the name that replaces it — the row never moves.
   */

  /** @type {{
   *   cell?: null | { pending: boolean, name: string, isAgent: boolean, title: string, seed: string },
   *   size?: 'xs'|'sm'|'md',
   *   class?: string,
   * }} */
  let { cell = null, size = "xs", class: extraClass = "" } = $props();
</script>

{#if !cell}
  <span class="text-fg-subtle">—</span>
{:else if cell.pending}
  <span
    class="inline-flex min-w-0 items-center gap-2 {extraClass}"
    title={cell.title}
    data-owner-pending
  >
    <span class="owner-skeleton owner-skeleton--avatar" aria-hidden="true"
    ></span>
    <span class="owner-skeleton owner-skeleton--name" aria-hidden="true"></span>
    <span class="sr-only">Loading owner</span>
  </span>
{:else}
  <span
    class="inline-flex min-w-0 items-center gap-1.5 {extraClass}"
    title={cell.title}
    data-owner-name={cell.name}
  >
    <ActorAvatar label={cell.name} seed={cell.seed} {size} />
    <span class="min-w-0 truncate text-meta text-fg">{cell.name}</span>
    {#if cell.isAgent}
      <!-- The pulse line the Agents nav uses: agents are presence. One glyph
           rather than the word, because the column is 10rem wide. -->
      <svg
        class="h-3 w-3 shrink-0 text-fg-subtle"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
        stroke-linecap="round"
        stroke-linejoin="round"
        role="img"
        aria-label="agent"
      >
        <path d={navIconPath("agents")} />
      </svg>
    {/if}
  </span>
{/if}

<style>
  .owner-skeleton {
    display: inline-block;
    border-radius: var(--radius-sm);
    background: var(--line-subtle);
  }
  .owner-skeleton--avatar {
    height: 1.25rem;
    width: 1.25rem;
    flex-shrink: 0;
  }
  .owner-skeleton--name {
    height: 0.5rem;
    width: 3.5rem;
  }
  @media (prefers-reduced-motion: no-preference) {
    .owner-skeleton {
      animation: owner-skeleton-pulse 1.4s ease-in-out infinite;
    }
  }
  @keyframes owner-skeleton-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.45;
    }
  }
</style>
