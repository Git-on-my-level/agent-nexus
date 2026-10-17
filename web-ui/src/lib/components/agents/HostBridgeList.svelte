<script>
  import { page } from "$app/stores";
  import { authenticatedAgent } from "$lib/authSession";
  import { coreClient } from "$lib/coreClient";
  import { liveAgentChanges } from "$lib/liveWorkspaceEvents.js";
  import {
    hostBridgeStatus,
    summarizeBridgeFleet,
  } from "$lib/hostBridgeStatus.js";
  import HostBridgeBadge from "./HostBridgeBadge.svelte";
  let hosts = $state([]);
  let now = $state(Date.now());
  $effect(() => {
    const timer = setInterval(() => (now = Date.now()), 15_000);
    return () => clearInterval(timer);
  });
  let failed = $state(false);
  let scope = $derived(
    `${$page.params.organization}/${$page.params.workspace}/${$authenticatedAgent?.agent_id ?? ""}`,
  );
  let fleet = $derived(
    $hostBridgeStatus.workspace === scope
      ? summarizeBridgeFleet(
          hosts,
          $hostBridgeStatus.hosts,
          $hostBridgeStatus.checked,
          now,
        )
      : { total: 0, live: 0 },
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
    <div class="flex flex-wrap items-baseline justify-between gap-2">
      <h2 class="text-meta font-medium">Machine bridges</h2>
      {#if fleet.total}<p
          class="text-micro {fleet.live === fleet.total
            ? 'text-ok-text'
            : 'text-fg-muted'}"
          data-bridge-fleet-summary
        >
          {fleet.live} of {fleet.total}
          {fleet.total === 1 ? "machine" : "machines"} live on the bridge
        </p>{/if}
    </div>
    {#if failed}<p class="text-micro text-fg-muted">
        Machine status could not be refreshed.
      </p>{/if}
    <ul class="mt-2 divide-y divide-line">
      {#each hosts as host (host.id)}<li
          class="flex flex-wrap items-start justify-between gap-x-4 gap-y-1 py-2"
        >
          <span class="text-meta font-medium text-fg"
            >{host.display_name || host.slug}</span
          ><HostBridgeBadge {host} detail />
        </li>{/each}
    </ul>
    {#if hosts.length === 100}<p class="text-micro text-fg-muted">
        Showing the 100 most recent visible machines.
      </p>{/if}
  </section>
{/if}
