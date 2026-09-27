<script>
  import { agentStateLabel } from "$lib/agentPresence.js";

  /**
   * One status dot per agent, the way a session list shows it: amber waits
   * on a human, cyan works (with a soft halo), grey idles, dim grey is stale.
   */
  let { state = "stale", size = "sm", class: extraClass = "" } = $props();

  let tone = $derived(
    state === "waiting_on_human"
      ? "agent-dot--waiting"
      : state === "working"
        ? "agent-dot--working"
        : state === "idle"
          ? "agent-dot--idle"
          : "agent-dot--stale",
  );
</script>

<span
  class="agent-dot {tone} {size === 'md' ? 'agent-dot--md' : ''} {extraClass}"
  role="img"
  aria-label={agentStateLabel(state)}
  title={agentStateLabel(state)}
></span>

<style>
  .agent-dot {
    display: inline-block;
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 9999px;
  }
  .agent-dot--md {
    width: 10px;
    height: 10px;
  }
  .agent-dot--waiting {
    background: var(--warn);
  }
  .agent-dot--working {
    background: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .agent-dot--idle {
    background: var(--fg-subtle);
  }
  .agent-dot--stale {
    background: transparent;
    box-shadow: inset 0 0 0 1.5px var(--line-strong);
  }
</style>
