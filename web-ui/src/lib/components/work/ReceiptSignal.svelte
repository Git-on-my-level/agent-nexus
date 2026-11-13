<script>
  import SignalBadge from "./SignalBadge.svelte";

  /**
   * Receipt state with the ruling's fold applied: the four primary states
   * (needs you, delivered, done, failed) render as badges; every other
   * machine state sits behind a disclosure. `quiet` renders the folded label
   * as plain text for use inside links, where a nested disclosure would
   * intercept the click.
   */
  let { signal = null, quiet = false } = $props();
</script>

{#if signal?.primary !== false}
  <SignalBadge tone={signal?.tone || "neutral"}
    >{signal?.label ?? ""}</SignalBadge
  >
{:else if quiet}
  <span class="text-micro text-fg-subtle">{signal.label}</span>
{:else}
  <details class="text-micro text-fg-muted">
    <summary class="cursor-pointer">{signal.label}</summary>
  </details>
{/if}
