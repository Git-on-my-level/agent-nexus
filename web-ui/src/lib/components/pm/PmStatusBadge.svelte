<!-- worksummary-guard: not-a-card-state: Reports whether a PM agent is running on the reader's own computer, which is a connection state and has nothing to do with any card. -->
<script>
  import { formatWait } from "$lib/inboxMailbox.js";
  import { pmConnected, pmStatusSummary } from "$lib/pm/onboardingState.js";

  /**
   * Unobtrusive PM status: a dot, and one sentence from the shared status
   * copy. It says nothing at all when core has not reported a state, so an
   * older core does not grow an empty badge.
   */
  let {
    presence = null,
    now = Date.now(),
    /** Where the status and uninstall commands live. */
    manageHref = "",
  } = $props();

  let connected = $derived(pmConnected(presence));
  let summary = $derived(pmStatusSummary(presence, now, formatWait));
</script>

{#if summary}
  <span
    class="inline-flex min-w-0 items-center gap-1.5 text-micro text-fg-subtle"
    data-pm-status={connected ? "connected" : "offline"}
  >
    <span
      class="h-1.5 w-1.5 shrink-0 rounded-full {connected
        ? 'bg-ok'
        : 'bg-fg-subtle'}"
      aria-hidden="true"
    ></span>
    <span class="truncate">{summary}</span>
    {#if manageHref}
      <a class="ui-prose-link shrink-0" href={manageHref}>Manage</a>
    {/if}
  </span>
{/if}
