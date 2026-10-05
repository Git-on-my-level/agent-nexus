<script>
  /**
   * A value that is not there: an em dash, with the reason on hover.
   *
   * A metric tile sized for `94%` cannot hold the word "unknown", so a report
   * whose reading failed used to wrap a paragraph of it down the tile and push
   * everything below it off the grid. The fact a reader needs is "no number
   * here"; *why* is a tooltip.
   *
   * It is not an error state — a metric nobody has reported yet is normal — so
   * it is muted rather than coloured, and it carries the reason as its
   * accessible name so the dash is never all a screen reader gets.
   */
  let {
    /** Why the value is missing. Shown on hover and read out. */
    reason = "",
    class: extraClass = "",
  } = $props();

  const FALLBACK = "No value reported";
  let title = $derived(String(reason ?? "").trim() || FALLBACK);
</script>

<span
  class="unavailable {extraClass}"
  data-unavailable
  {title}
  aria-label={title}
  role="img">—</span
>

<style>
  .unavailable {
    color: var(--fg-subtle, var(--fg-muted));
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    cursor: help;
  }
</style>
