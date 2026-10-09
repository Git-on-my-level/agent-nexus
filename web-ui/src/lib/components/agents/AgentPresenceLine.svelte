<script>
  import { page } from "$app/stores";

  import { agentRegistry, findAgentSummary } from "$lib/actorSession";
  import { agentPath, agentPresentationState } from "$lib/agentPresence.js";
  import AgentStateDot from "$lib/components/agents/AgentStateDot.svelte";
  import Time from "$lib/time/Time.svelte";
  import { instantIso } from "$lib/time/format.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";

  /**
   * The requesting agent's own presence note (`anx work note`), for the
   * Inbox context strip. Renders nothing for humans or agents with no note.
   */
  let { actorId = "" } = $props();

  let agent = $derived(findAgentSummary(actorId, $agentRegistry));
  let note = $derived(String(agent?.last_progress_note ?? "").trim());
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
</script>

{#if agent && note}
  <p
    class="flex min-w-0 items-baseline gap-1.5 [overflow-wrap:anywhere]"
    data-inbox-presence
  >
    <span class="translate-y-[-1px] self-center"
      ><AgentStateDot state={agentPresentationState(agent)} /></span
    >
    <span class="line-clamp-2 min-w-0">
      <a class="text-fg hover:underline" href={workspaceHref(agentPath(agent))}
        >{agent.display_name}</a
      ><span class="text-fg-subtle">{" · "}</span
      >{#if instantIso(agent.last_progress_at)}<Time
          value={agent.last_progress_at}
        />{": "}{/if}“{note}”
    </span>
  </p>
{/if}
