<script>
  /**
   * How long ago something happened: "8 min ago", "yesterday", "Oct 5".
   *
   * The verb and the exact local instant are in the tooltip and the
   * accessible name. Where a cadence colours the age, use `FreshnessBadge`.
   */
  import Time from "$lib/time/Time.svelte";

  let {
    /** ISO instant. */
    at = "",
    /** What happened then: "moved", "updated", "checked". */
    verb = "",
    /** Reference time. Omit it to follow the shared clock. */
    now = undefined,
    class: extraClass = "",
  } = $props();
</script>

<Time value={at} {verb} {now} class="age-badge {extraClass}" />

<style>
  :global(time.age-badge) {
    flex: none;
    color: var(--fg-muted);
    font-size: 11px;
    white-space: nowrap;
  }
</style>
