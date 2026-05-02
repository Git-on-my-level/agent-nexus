<!--
  Compact mobile-friendly resource header shell.

  Layout contract (matches topic / board / doc detail pages):
  - Outer row: `justify-between` with a single flex-1 `<nav>` on the left and an
    optional action cluster on the right.
  - Inside `<nav>`: lead segments `shrink-0`, then a `flex-1 min-w-0` row that
    wraps **current title** (`min-w-0 shrink truncate`, no `flex-1` on the
    title) with **badges** (`shrink-0`) so pills sit tight to the right of the
    text instead of at the end of a stretched title cell.
  - Optional `desktop` snippet: wrapped in `hidden md:flex` for full title,
    summary, and long metadata lines that are redundant with the crumb row on
    narrow viewports.

  Pass optional named snippets: `breadcrumb` (required for the nav body),
  `actions`, `desktop`.
-->
<script>
  let {
    breadcrumbAriaLabel = "Breadcrumb",
    desktopAriaLabel = "Resource details",
    breadcrumb,
    actions,
    desktop,
  } = $props();
</script>

<div
  class="mb-1 flex min-w-0 items-center justify-between gap-1.5 sm:gap-2 md:mb-2"
>
  <nav
    class="flex min-w-0 flex-1 items-center gap-1.5 text-meta text-fg-muted"
    aria-label={breadcrumbAriaLabel}
  >
    {@render breadcrumb()}
  </nav>
  {#if actions}
    <div class="flex shrink-0 items-center gap-0.5 sm:gap-1.5">
      {@render actions()}
    </div>
  {/if}
</div>

{#if desktop}
  <div
    class="mt-0 hidden max-w-full flex-col gap-1.5 md:flex"
    aria-label={desktopAriaLabel}
  >
    {@render desktop()}
  </div>
{/if}
