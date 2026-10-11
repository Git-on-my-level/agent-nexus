<!-- worksummary-guard: not-a-card-state: Reports whether a PM agent is running on the reader's own computer, which is a connection state and has nothing to do with any card. -->
<script>
  import { tooltip } from "$lib/actions/tooltip.js";
  import {
    pmConnected,
    pmStatusLabel,
    pmStatusSummary,
  } from "$lib/pm/onboardingState.js";

  /**
   * Unobtrusive PM status: a dot and one word, beside the heading.
   *
   * It used to print the whole sentence — "PM connected, Hermes on studio" —
   * on its own line under the title, which is a header line spent on a
   * process nobody is thinking about. Which runner, on which machine, and
   * how long it has been quiet are the tooltip's; the badge keeps the state.
   *
   * It says nothing at all when core has not reported a state, so an older
   * core does not grow an empty badge.
   */
  let {
    presence = null,
    now = Date.now(),
    /** Where the status and uninstall commands live. */
    manageHref = "",
  } = $props();

  let connected = $derived(pmConnected(presence));
  let summary = $derived(pmStatusSummary(presence, now));
  // `pmStatusSummary` is empty for every state `pmStatusLabel` cannot name,
  // so the badge never renders with one and not the other.
  let label = $derived(pmStatusLabel(presence));
</script>

{#if summary}
  <span
    class="inline-flex min-w-0 items-center gap-1.5 text-micro text-fg-subtle"
    data-pm-status={connected ? "connected" : "offline"}
  >
    <!--
      `role="img"`, as `AgentStateDot` does it: ARIA drops `aria-label` on a
      generic span, so without the role the runner, the host and the
      last-seen age reach nobody using a screen reader — and they used to be
      the visible text.
    -->
    <span
      class="inline-flex min-w-0 items-center gap-1.5"
      role="img"
      aria-label={summary}
      use:tooltip={summary}
    >
      <span
        class="h-1.5 w-1.5 shrink-0 rounded-full {connected
          ? 'bg-ok'
          : 'bg-fg-subtle'}"
        aria-hidden="true"
      ></span>
      <span class="truncate" aria-hidden="true">{label}</span>
    </span>
    {#if manageHref}
      <a class="ui-prose-link shrink-0" href={manageHref}>Manage</a>
    {/if}
  </span>
{/if}
