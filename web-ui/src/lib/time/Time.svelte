<script>
  /**
   * One timestamp, everywhere.
   *
   * The words come from `formatTime`. The machine-readable instant is
   * `datetime`. Hover uses the shared tooltip. A tap or a long-press on
   * touch pins that same sentence, because a native `title` does not show
   * on mobile. The accessible name is the full local date and time with
   * the timezone abbreviation.
   *
   * Omit `now` and the label follows the shared clock. Pass `now` from a
   * test or a fixture that has already frozen the clock.
   */
  import { tooltip } from "$lib/actions/tooltip.js";
  import { clockNow, retainClock } from "$lib/time/clock.svelte.js";
  import { formatTime, instantIso } from "$lib/time/format.js";

  let {
    /** ISO instant, epoch millis, or a Date. */
    value = "",
    /**
     * Frozen reference time. Leave it unset to follow the shared clock.
     * @type {number|undefined}
     */
    now = undefined,
    /** "relative" | "exact" | "clock" | "date" */
    style = "relative",
    locale = undefined,
    /** Verb in the accessible name: "moved", "updated", "read". */
    verb = "",
    /** Shown when there is no instant, for example "—". */
    fallback = "",
    class: className = "",
  } = $props();

  let live = $derived(now === undefined || now === null);
  let current = $derived(live ? clockNow() : Number(now));
  /**
   * The server has no reader timezone. The first paint matches an empty
   * stamp; the phrase appears once this component is on the client, so
   * hydration does not swap "3 h ago" for "yesterday".
   */
  let client = $state(false);

  $effect(() => {
    client = true;
    if (!live) return;
    return retainClock();
  });

  let text = $derived(formatTime(value, { now: current, locale, style }));
  let exact = $derived(
    formatTime(value, { now: current, locale, style: "exact" }),
  );
  let label = $derived.by(() => {
    if (!exact || exact === String(value ?? "")) return "";
    const action = String(verb ?? "").trim();
    if (!action) return exact;
    return `${action[0].toUpperCase()}${action.slice(1)} ${exact}`;
  });
  let datetime = $derived(instantIso(value));
</script>

{#if client && text && datetime}
  <time
    class="anx-time {className}"
    {datetime}
    title={label}
    aria-label={label}
    use:tooltip={label}>{text}</time
  >
{:else if client && text}
  <span class="anx-time {className}">{text}</span>
{:else}
  <span class="anx-time {className}">{fallback}</span>
{/if}

<style>
  .anx-time {
    font-variant-numeric: tabular-nums;
  }
</style>
