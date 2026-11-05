<script>
  import { agentPath, agentStateShortLabel } from "$lib/agentPresence.js";
  import {
    groupHostPersonas,
    idlePersonaRangeLabel,
    personaKind,
    personaMatches,
    personaUseLabel,
  } from "$lib/accessPersonas.js";
  import AgentStateDot from "$lib/components/agents/AgentStateDot.svelte";
  import Button from "$lib/components/Button.svelte";

  /**
   * One host's personas: adapters and active personas in full, idle ones
   * folded behind a count. Archive is offered on persona rows to people only;
   * it cuts the persona's access now and keeps its history and name.
   */
  let {
    host,
    agents = [],
    excluded = [],
    canArchive = false,
    showIdle = false,
    query = "",
    workspaceHref = (path) => path,
    now = Date.now(),
    onarchive = async () => {},
  } = $props();

  let expanded = $state(false);
  let confirming = $state("");
  let busy = $state(false);
  let error = $state("");

  let groups = $derived(
    groupHostPersonas(
      host,
      agents.filter((agent) => personaMatches(agent, host, query)),
      now,
    ),
  );
  let idleOpen = $derived(showIdle || expanded);
  let rows = $derived(
    idleOpen ? [...groups.shown, ...groups.idle] : groups.shown,
  );

  function stateLabel(agent) {
    if (agent.revoked_at) return "revoked";
    if (excluded.includes(agent.name)) return "excluded";
    return agentStateShortLabel(agent.state);
  }

  async function archive(agent) {
    busy = true;
    error = "";
    try {
      await onarchive(host, agent);
      confirming = "";
    } catch (cause) {
      error =
        cause?.details ||
        (cause instanceof Error ? cause.message : "") ||
        `${agent.name} was not archived.`;
    } finally {
      busy = false;
    }
  }
</script>

{#if rows.length}
  <ul>
    {#each rows as agent (agent.id)}
      {@const kind = personaKind(agent, host)}
      <li
        class="border-t border-line-subtle px-4 py-1.5 first:border-t-0 {agent.revoked_at
          ? 'opacity-60'
          : ''}"
        data-host-agent={agent.handle}
        data-persona-kind={kind}
      >
        <div class="flex items-center gap-3 max-sm:min-h-11">
          <AgentStateDot state={agent.state} />
          <div class="min-w-0">
            <a
              class="block truncate text-meta text-fg hover:underline"
              href={workspaceHref(agentPath(agent))}>{agent.name}</a
            >
            <span class="block truncate text-micro text-fg-subtle"
              >@{agent.handle} · {kind === "adapter"
                ? "adapter, always shown"
                : personaUseLabel(agent, now)}</span
            >
          </div>
          <span class="ml-auto shrink-0 text-micro text-fg-subtle">{kind}</span>
          <span class="w-16 shrink-0 text-right text-micro text-fg-muted"
            >{stateLabel(agent)}</span
          >
          {#if canArchive && kind !== "adapter" && !agent.revoked_at}
            <button
              class="shrink-0 text-micro text-fg-muted hover:text-fg hover:underline"
              type="button"
              aria-label={`Archive ${agent.name}`}
              data-persona-archive={agent.name}
              onclick={() => {
                confirming = agent.id;
                error = "";
              }}>Archive</button
            >
          {/if}
        </div>
        {#if confirming === agent.id}
          <div
            class="mt-1.5 flex flex-wrap items-center gap-2 rounded-md bg-bg px-2 py-1.5 text-micro"
            data-persona-archive-confirm={agent.name}
          >
            <span class="text-fg"
              >Archive {agent.name}? It loses access now. Its history stays and
              the name stays reserved until you restore it.</span
            >
            <Button
              variant="secondary"
              size="compact"
              {busy}
              onclick={() => archive(agent)}
              >{busy ? "Archiving…" : "Archive"}</Button
            >
            <Button
              variant="ghost"
              size="compact"
              disabled={busy}
              onclick={() => (confirming = "")}>Cancel</Button
            >
            {#if error}
              <p class="basis-full text-danger-text" role="alert">{error}</p>
            {/if}
          </div>
        {/if}
      </li>
    {/each}
  </ul>
{/if}
{#if groups.idle.length && !showIdle}
  <div
    class="flex flex-wrap items-center gap-2 border-t border-line-subtle px-4 py-1.5 text-micro text-fg-muted"
    data-host-idle-personas={groups.idle.length}
  >
    <span
      >{groups.idle.length === 1
        ? "1 idle persona"
        : `${groups.idle.length} idle personas`}
      {expanded ? "shown" : "hidden"}. {idlePersonaRangeLabel(
        groups.idle,
        now,
      )}</span
    >
    <button
      class="ml-auto font-medium text-accent-text hover:underline"
      type="button"
      aria-expanded={expanded}
      onclick={() => (expanded = !expanded)}
      >{expanded ? "Hide" : `Show ${groups.idle.length}`}</button
    >
  </div>
{/if}
{#if !rows.length && !groups.idle.length && !agents.length}
  <p class="px-4 py-2 text-micro text-fg-muted">
    No agents yet. An agent appears here the first time it runs <code
      class="rounded bg-line px-1 py-px text-fg">anx</code
    > on this machine.
  </p>
{/if}
