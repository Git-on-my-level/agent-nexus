<script>
  import { page } from "$app/stores";
  import { authenticatedAgent } from "$lib/authSession";
  import { coreClient } from "$lib/coreClient";
  import { liveAgentChanges } from "$lib/liveWorkspaceEvents.js";
  import HostBridgeBadge from "./HostBridgeBadge.svelte";
  let hosts = $state([]);
  let failed = $state(false);
  let scope = $derived(
    `${$page.params.organization}/${$page.params.workspace}/${$authenticatedAgent?.agent_id ?? ""}`,
  );
  $effect(() => {
    const key = scope;
    void key;
    hosts = [];
    failed = false;
    let active = true,
      busy = false,
      again = false;
    async function load() {
      if (!active || document.hidden) return;
      if (busy) {
        again = true;
        return;
      }
      busy = true;
      try {
        const result = await coreClient.listHostSummaries();
        if (active) {
          hosts = result?.hosts ?? [];
          failed = false;
        }
      } catch {
        if (active) failed = true;
      } finally {
        busy = false;
        if (again && active) {
          again = false;
          void load();
        }
      }
    }
    void load();
    const release = liveAgentChanges({
      client: coreClient,
      onChange: () => void load(),
    });
    const visible = () => {
      if (!document.hidden) void load();
    };
    document.addEventListener("visibilitychange", visible);
    return () => {
      document.removeEventListener("visibilitychange", visible);
      active = false;
      release();
    };
  });
</script>

{#if hosts.length || failed}
  <section
    class="my-4 rounded border border-line p-3"
    aria-label="Machine bridges"
  >
    <h2 class="text-meta font-medium">Machine bridges</h2>
    {#if failed}<p class="text-micro text-fg-muted">
        Machine status could not be refreshed.
      </p>{/if}
    <ul class="mt-2 space-y-2">
      {#each hosts as host (host.id)}<li>
          <span class="text-meta">{host.display_name || host.slug}</span
          ><HostBridgeBadge {host} detail />
        </li>{/each}
    </ul>
    {#if hosts.length === 100}<p class="text-micro text-fg-muted">
        Showing the 100 most recent visible machines.
      </p>{/if}
  </section>
{/if}
