<script>
  import { tooltip } from "$lib/actions/tooltip.js";
  import { agentStateLabel } from "$lib/agentPresence.js";

  /**
   * One status dot per agent, the way a session list shows it: amber waits on
   * a human, cyan works (with a soft halo), grey idles.
   *
   * Offline and never-checked-in are hollow and uncoloured, because they are
   * not conditions — an agent that is not running is the normal state of most
   * agents most of the time. Only `stale` is amber, and only a silence with
   * work riding on it reaches `stale` at all (see `agentPresence.js`).
   */
  let { state = "offline", size = "sm", class: extraClass = "" } = $props();

  const TONES = {
    waiting_on_human: "agent-dot--waiting",
    working: "agent-dot--working",
    idle: "agent-dot--idle",
    stale: "agent-dot--stale",
  };
  let tone = $derived(TONES[state] ?? "agent-dot--offline");
</script>

<span
  class="agent-dot {tone} {size === 'md' ? 'agent-dot--md' : ''} {extraClass}"
  role="img"
  aria-label={agentStateLabel(state)}
  data-agent-dot={state}
  use:tooltip={agentStateLabel(state)}
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
  /* The one alarming quiet state: silent while holding work. */
  .agent-dot--stale {
    background: var(--warn);
    box-shadow: 0 0 0 3px var(--warn-soft, transparent);
  }
  /* Not running, nothing waiting: present, uncoloured, unremarkable. */
  .agent-dot--offline {
    background: transparent;
    box-shadow: inset 0 0 0 1.5px var(--line-strong);
  }
</style>
