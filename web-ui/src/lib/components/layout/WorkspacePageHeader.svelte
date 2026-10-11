<script>
  /**
   * `eyebrow` is the line above the title — where the title is the thing
   * being read rather than the name of the page, the eyebrow says which page
   * it is. `meta` sits on the title's own baseline: a state badge belongs
   * beside a heading, not on a line of its own under it.
   *
   * `clamp` is for a title that is *content* rather than the page's name. A
   * page name is two words and wraps to nothing; a title taken from what
   * somebody typed can be a hundred characters of unbroken token, and left
   * to wrap it takes three lines, pushes the actions onto a row of their
   * own, and drags any popover anchored to them off the side of the screen.
   * Clamped, the full text stays in the tooltip and the accessible name.
   */
  import { tooltip } from "$lib/actions/tooltip.js";

  let {
    title = "",
    eyebrow = null,
    meta = null,
    subtitle = null,
    actions = null,
    clamp = false,
  } = $props();
</script>

<div class="flex flex-wrap items-start justify-between gap-3">
  <!--
    `sm:flex-1` only for a clamped title, and for a reason: flex decides
    where to wrap from each item's *max-content* size, before any shrinking,
    so an unclamped hundred-character title always pushes the actions onto a
    row of their own. A zero basis takes that decision away. A page whose
    title is allowed to wrap wants the opposite — the whole width for the
    title, and the actions below — so it keeps the auto basis.

    Never on a phone, where there is no width to share: the actions wrap
    below, the title keeps the row, and a popover anchored to the actions
    opens under the whole header instead of over it.
  -->
  <div class="min-w-0 {clamp ? 'sm:flex-1' : ''}">
    {#if eyebrow}
      <div class="text-micro text-fg-subtle">{@render eyebrow()}</div>
    {/if}
    {#if title}
      <div class="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <!-- Titles can come from a source and be one long unbroken token. -->
        {#if clamp}
          <!-- `aria-label`, not `title`: the browser's own tooltip waits a
               second and paints a question-mark cursor, which is why this
               app has a tooltip layer at all. -->
          <h1
            class="line-clamp-2 min-w-0 text-title text-fg [overflow-wrap:anywhere]"
            aria-label={title}
            use:tooltip={title}
          >
            {title}
          </h1>
        {:else}
          <h1 class="min-w-0 text-title text-fg [overflow-wrap:anywhere]">
            {title}
          </h1>
        {/if}
        {#if meta}<div class="min-w-0">{@render meta()}</div>{/if}
      </div>
    {:else if meta}
      <div class="min-w-0">{@render meta()}</div>
    {/if}
    {#if subtitle}
      <div class="mt-0.5 text-meta text-fg-muted">{@render subtitle()}</div>
    {/if}
  </div>
  {#if actions}
    <div class="flex shrink-0 flex-wrap items-center gap-2">
      {@render actions()}
    </div>
  {/if}
</div>
