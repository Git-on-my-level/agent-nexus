<script>
  import { accessErrorMessage } from "$lib/accessErrors.js";
  import {
    accessPersonaSummary,
    accessSummaryLine,
    groupHostPersonas,
    personaMatches,
    personaUseLabel,
  } from "$lib/accessPersonas.js";
  import HostCard from "$lib/components/access/HostCard.svelte";

  /**
   * Enrolled hosts with their personas, plus the page-level persona filter:
   * Active only (idle personas folded per host), Show idle, or Archived.
   */
  let {
    hosts = [],
    canManage = false,
    canArchive = false,
    workspaceHref = (path) => path,
    now = Date.now(),
    onexclusions = async () => {},
    onrevoke = async () => {},
    onarchive = async () => {},
    onrestore = async () => {},
  } = $props();

  let filter = $state("active");
  let query = $state("");
  let restoring = $state("");
  let restoreError = $state("");

  let summary = $derived(accessPersonaSummary(hosts, now));
  let archived = $derived(
    hosts.flatMap((host) =>
      groupHostPersonas(host, host.agents ?? [], now)
        .archived.filter((agent) => personaMatches(agent, host, query))
        .map((agent) => ({ host, agent })),
    ),
  );

  async function restore(host, agent) {
    restoring = agent.id;
    restoreError = "";
    try {
      await onrestore(host, agent);
    } catch (cause) {
      restoreError = accessErrorMessage(
        cause,
        `${agent.name} was not restored.`,
      );
    } finally {
      restoring = "";
    }
  }

  const chip =
    "rounded-full border px-2.5 py-0.5 text-micro font-medium transition-colors";
</script>

<p class="mb-2 text-micro text-fg-muted" data-access-persona-summary>
  {accessSummaryLine(summary)}
</p>
<div
  class="mb-3 flex flex-wrap items-center gap-2"
  role="toolbar"
  aria-label="Filter personas"
>
  <input
    bind:value={query}
    class="w-52 rounded-md border border-line bg-bg px-2 py-1 text-micro text-fg max-sm:w-full"
    placeholder="Filter by name or host"
    aria-label="Filter personas by name or host"
    autocomplete="off"
    data-anx-no-submit-shortcut
  />
  {#each [["active", "Active only"], ["idle", `Show idle (${summary.idle})`], ["archived", `Archived (${summary.archived})`]] as [key, label] (key)}
    <button
      type="button"
      class="{chip} {filter === key
        ? 'border-accent bg-accent-soft text-accent-text'
        : 'border-line text-fg-muted hover:text-fg'}"
      aria-pressed={filter === key}
      data-persona-filter={key}
      onclick={() => (filter = key)}>{label}</button
    >
  {/each}
</div>

{#if filter === "archived"}
  <section
    class="overflow-hidden rounded-md border border-line bg-bg-soft"
    aria-label="Archived personas"
    data-archived-personas
  >
    {#if archived.length}
      <ul>
        {#each archived as { host, agent } (agent.id)}
          <li
            class="flex items-center gap-3 border-t border-line-subtle px-4 py-1.5 first:border-t-0 max-sm:min-h-11"
            data-archived-persona={agent.handle}
          >
            <div class="min-w-0">
              <span class="block truncate text-meta text-fg">{agent.name}</span>
              <span class="block truncate text-micro text-fg-subtle"
                >{host.slug} · {personaUseLabel(agent, now)}</span
              >
            </div>
            {#if canArchive}
              <button
                class="ml-auto shrink-0 text-micro font-medium text-accent-text hover:underline disabled:opacity-60"
                type="button"
                disabled={restoring === agent.id}
                aria-label={`Restore ${agent.name} on ${host.slug}`}
                data-persona-restore={agent.name}
                onclick={() => restore(host, agent)}
                >{restoring === agent.id ? "Restoring…" : "Restore"}</button
              >
            {/if}
          </li>
        {/each}
      </ul>
    {:else}
      <p class="px-4 py-2 text-micro text-fg-muted">No archived personas.</p>
    {/if}
    <p
      class="border-t border-line-subtle px-4 py-1.5 text-micro text-fg-subtle"
    >
      Auto-archived personas are hidden until they are used again. Names a human
      archived stay reserved and cannot act. Revoke is separate and permanent.
    </p>
    {#if restoreError}
      <p class="px-4 pb-2 text-micro text-danger-text" role="alert">
        {restoreError}
      </p>
    {/if}
  </section>
{:else}
  <div class="space-y-3">
    {#each hosts as host (host.id)}
      <HostCard
        {host}
        agents={host.agents ?? []}
        {canManage}
        {canArchive}
        showIdle={filter === "idle"}
        {query}
        {workspaceHref}
        {now}
        {onexclusions}
        {onrevoke}
        {onarchive}
      />
    {/each}
  </div>
{/if}
