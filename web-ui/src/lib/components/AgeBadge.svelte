<script>
  /**
   * How long ago something happened: `8h`, `3d`.
   *
   * It replaces "moved 2h ago" everywhere. The verb and the exact instant are
   * in the tooltip and the accessible name, so a row spends two characters on
   * the fact instead of four words — and a reader who wants the timestamp can
   * still get it.
   *
   * Renders a `<time>` so the machine-readable instant travels with it.
   */
  import { ageTitle, formatAge } from "$lib/ageBadge.js";

  let {
    /** ISO instant. */
    at = "",
    /** What happened then: "moved", "updated", "checked". */
    verb = "",
    /** Reference time, injectable so the badge is testable. */
    now = Date.now(),
    class: extraClass = "",
  } = $props();

  let age = $derived(formatAge(at, now));
  let title = $derived(ageTitle(at, verb, now));
</script>

{#if age}
  <time class="age-badge {extraClass}" datetime={at} {title} aria-label={title}
    >{age}</time
  >
{/if}

<style>
  .age-badge {
    flex: none;
    color: var(--fg-muted);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    cursor: help;
  }
</style>
