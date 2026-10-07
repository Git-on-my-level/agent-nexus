<script>
  import { agentRoster, startAgentRoster } from "$lib/agentRoster.js";
  import { workingAgentCount } from "$lib/agentPresence.js";

  /**
   * How many agents are working, beside the Agents nav item. Mounting it
   * keeps the workspace roster loaded for the shell (names and the badge).
   * Zero renders nothing.
   */
  let { workspace = "", enabled = false, variant = "sidebar" } = $props();

  $effect(() => {
    if (!enabled || !workspace) return;
    const key = workspace;
    let stop = () => {};
    // The Agents page starts its own roster immediately. A navigation badge
    // should follow the primary view's reads rather than compete with them.
    const timer = setTimeout(() => {
      stop = startAgentRoster(key);
    }, 1500);
    return () => {
      clearTimeout(timer);
      stop();
    };
  });

  let count = $derived(
    $agentRoster.workspace === workspace
      ? workingAgentCount($agentRoster.agents)
      : 0,
  );
  let text = $derived(count > 0 ? (count > 99 ? "99+" : String(count)) : "");
</script>

{#if text}
  {#if variant === "bottom"}
    <span
      class="absolute left-1/2 top-1 ml-1.5 min-w-4 rounded-full bg-accent-solid px-1 text-center text-[10px] font-semibold leading-4 text-white tabular-nums"
      aria-label={`${count} working`}
      data-agents-nav-count>{text}</span
    >
  {:else}
    <span
      class="ml-auto shrink-0 rounded-full bg-accent-soft px-1.5 text-micro font-medium tabular-nums text-accent-text"
      aria-label={`${count} working`}
      title={`${count} ${count === 1 ? "agent" : "agents"} working`}
      data-agents-nav-count>{text}</span
    >
  {/if}
{/if}
