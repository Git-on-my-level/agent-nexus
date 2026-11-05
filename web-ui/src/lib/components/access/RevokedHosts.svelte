<script>
  import HostCard from "$lib/components/access/HostCard.svelte";

  /** Revoked hosts stay inspectable for audit, folded behind a toggle. */
  let {
    hosts = [],
    workspaceHref = (path) => path,
    now = Date.now(),
  } = $props();

  let open = $state(false);
</script>

{#if hosts.length}
  <button
    class="mt-2 text-micro text-fg-muted hover:text-fg"
    type="button"
    onclick={() => (open = !open)}
    >{open
      ? "Hide revoked hosts"
      : `Show ${hosts.length} revoked ${hosts.length === 1 ? "host" : "hosts"}`}</button
  >
  {#if open}
    <div class="mt-2 space-y-3">
      {#each hosts as host (host.id)}
        <HostCard {host} agents={host.agents ?? []} {workspaceHref} {now} />
      {/each}
    </div>
  {/if}
{/if}
