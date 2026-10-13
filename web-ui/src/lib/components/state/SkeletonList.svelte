<script>
  /**
   * Row placeholders in the same bordered list the data will fill, so the
   * page does not move when it lands. Match the real row: `rowClass` sets its
   * height, `lines` its title/meta stack, `leading` a status dot and
   * `trailing` the right-hand age or count.
   */
  let {
    rows = 5,
    lines = 2,
    leading = false,
    trailing = true,
    bare = false,
    rowClass = "h-[52px]",
    class: className = "",
  } = $props();

  const WIDTHS = ["62%", "48%", "55%", "40%", "58%", "44%"];
</script>

<div
  class="ui-skeleton-reveal {bare
    ? ''
    : 'overflow-hidden rounded-md border border-line bg-panel'} {className}"
  aria-hidden="true"
  data-skeleton-list
>
  {#each Array.from({ length: rows }, (_, index) => index) as index (index)}
    <div
      class="flex items-center gap-3 px-4 {rowClass} {index > 0
        ? 'border-t border-line'
        : ''}"
    >
      {#if leading}
        <div class="h-2 w-2 shrink-0 rounded-full bg-panel-hover"></div>
      {/if}
      <div class="flex min-w-0 flex-1 flex-col gap-1.5">
        <div
          class="h-3 animate-pulse rounded bg-panel-hover"
          style="width: {WIDTHS[index % WIDTHS.length]}"
        ></div>
        {#if lines > 1}
          <div class="h-2.5 w-28 animate-pulse rounded bg-panel-hover"></div>
        {/if}
      </div>
      {#if trailing}
        <div
          class="h-2.5 w-10 shrink-0 animate-pulse rounded bg-panel-hover"
        ></div>
      {/if}
    </div>
  {/each}
</div>
