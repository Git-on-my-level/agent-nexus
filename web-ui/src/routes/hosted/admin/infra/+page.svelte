<script>
  import { onMount } from "svelte";

  import Button from "$lib/components/Button.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import StatusPill from "$lib/hosted/StatusPill.svelte";
  import {
    filesystemUsage,
    formatPercent,
    hostFreshnessLabel,
    hostWarning,
    runtimeCountsForHost,
    telemetryResourceCards,
  } from "$lib/hosted/adminInfra.js";
  import {
    detailHref,
    formatDateTime,
    formatListValue,
    formatNumber,
    telemetryLabel,
  } from "$lib/hosted/adminOverview.js";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  const TOKEN_STORAGE_KEY = "anx_admin_token";
  const ACTOR_STORAGE_KEY = "anx_admin_actor";

  let token = $state("");
  let actor = $state("");
  let hosts = $state([]);
  let workspaces = $state([]);
  let selectedHostId = $state("");
  let loading = $state(false);
  let error = $state("");

  const displayHosts = $derived.by(() => {
    if (hosts.length) return hosts;
    const grouped = new Map();
    for (const ws of workspaces) {
      const id = ws.host_id || ws.host_label || "unknown";
      if (!grouped.has(id)) {
        grouped.set(id, {
          id,
          label: ws.host_label || ws.host_id || "Unknown host",
          telemetry_freshness: "unknown",
          telemetry_age_seconds: null,
          placement_available: false,
          drain_mode: false,
          capacity_workspace_slots: 0,
          allocated_workspace_slots: 0,
          capacity_port_slots: 0,
          allocated_port_slots: 0,
          latest_snapshot: null,
        });
      }
    }
    return [...grouped.values()].sort((a, b) => a.label.localeCompare(b.label));
  });

  const selectedHost = $derived(
    displayHosts.find((host) => host.id === selectedHostId) ??
      displayHosts[0] ??
      null,
  );
  const selectedCounts = $derived(
    selectedHost ? runtimeCountsForHost(workspaces, selectedHost) : null,
  );
  const telemetryMissing = $derived(
    displayHosts.length > 0 &&
      displayHosts.every((host) => !host.latest_snapshot),
  );

  onMount(() => {
    token = localStorage.getItem(TOKEN_STORAGE_KEY) ?? "";
    actor = localStorage.getItem(ACTOR_STORAGE_KEY) ?? "";
    void loadInfra();
  });

  function headers() {
    const out = { "x-anx-admin-token": token.trim() };
    if (actor.trim()) out["x-anx-admin-actor"] = actor.trim();
    return out;
  }

  function slotLabel(used, total) {
    return `${formatNumber(used)} / ${formatNumber(total)}`;
  }

  async function loadInfra() {
    if (!token.trim()) {
      error = "Open /hosted/admin and enter an operator admin token first.";
      return;
    }
    loading = true;
    error = "";
    try {
      const [hostRes, workspaceRes] = await Promise.all([
        hostedCpFetch("admin/analytics/hosts", { headers: headers() }),
        hostedCpFetch("admin/analytics/workspaces?limit=100", {
          headers: headers(),
        }),
      ]);
      if (!hostRes.ok) throw new Error(await responseError(hostRes));
      if (!workspaceRes.ok) throw new Error(await responseError(workspaceRes));
      hosts = (await hostRes.json()).hosts ?? [];
      workspaces = (await workspaceRes.json()).workspaces ?? [];
      const firstID = (hosts[0] ?? displayHosts[0])?.id ?? "";
      if (
        !selectedHostId ||
        !displayHosts.some((h) => h.id === selectedHostId)
      ) {
        selectedHostId = firstID;
      }
    } catch (e) {
      error = e instanceof Error ? e.message : "Infra data did not load.";
    } finally {
      loading = false;
    }
  }

  async function responseError(res) {
    try {
      const body = await res.json();
      return body?.error?.message || body?.error?.code || res.statusText;
    } catch {
      return res.statusText;
    }
  }
</script>

<svelte:head>
  <title>Admin Infra - Agent Nexus (ANX)</title>
</svelte:head>

<div class="mx-auto max-w-7xl space-y-4 px-4 py-5">
  <header class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <a class="text-micro text-accent-text" href="/hosted/admin"
        >Admin overview</a
      >
      <h1 class="mt-1 text-display text-fg">Infra live view</h1>
      <p class="mt-1 max-w-3xl text-meta text-fg-muted">
        Host placement, workspace runtime state, and signed packed-host resource
        telemetry.
      </p>
    </div>
    <Button variant="secondary" onclick={loadInfra} disabled={loading}>
      {loading ? "Refreshing..." : "Refresh"}
    </Button>
  </header>

  {#if error}
    <StateError
      title="Infra data did not load"
      message={error}
      onretry={loadInfra}
      retrying={loading}
    />
  {:else if displayHosts.length}
    {#if telemetryMissing}
      <section class="rounded-md border border-warn bg-warn-soft p-3">
        <p class="text-meta font-semibold text-warn-text">
          Live resource telemetry is not wired.
        </p>
        <p class="mt-1 text-micro text-warn-text">
          Showing placement-derived host inventory from workspaces. CPU, memory,
          disk, inode, Docker daemon, and orphan resource metrics will appear
          after signed host telemetry is configured.
        </p>
      </section>
    {/if}

    <section class="grid gap-3 lg:grid-cols-3">
      {#each displayHosts as host (host.id)}
        {@const counts = runtimeCountsForHost(workspaces, host)}
        {@const warning = hostWarning(host, counts)}
        <button
          class="rounded-md border bg-bg-soft p-4 text-left transition hover:border-accent {selectedHost?.id ===
          host.id
            ? 'border-accent'
            : 'border-line'}"
          type="button"
          onclick={() => (selectedHostId = host.id)}
        >
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <h2 class="truncate text-title text-fg">
                {host.label || host.id}
              </h2>
              <p class="mt-1 truncate font-mono text-micro text-fg-subtle">
                {host.id}
              </p>
            </div>
            <StatusPill
              status={host.telemetry_freshness}
              label={hostFreshnessLabel(host)}
            />
          </div>
          <div class="mt-3 grid grid-cols-2 gap-2 text-micro">
            {@render Metric({
              label: "Workspaces",
              value: formatNumber(counts.total),
            })}
            {@render Metric({
              label: "Capacity",
              value: slotLabel(
                host.allocated_workspace_slots || counts.total,
                host.capacity_workspace_slots,
              ),
            })}
            {@render Metric({
              label: "Ports",
              value: slotLabel(
                host.allocated_port_slots,
                host.capacity_port_slots,
              ),
            })}
            {@render Metric({
              label: "Runtime issues",
              value: formatNumber(
                counts.staleHeartbeat + counts.staleRuntimeMetadata,
              ),
            })}
          </div>
          {#if warning}
            <p
              class="mt-3 rounded bg-warn-soft px-2 py-1 text-micro text-warn-text"
            >
              {warning}
            </p>
          {/if}
        </button>
      {/each}
    </section>

    {#if selectedHost && selectedCounts}
      {@const payload = selectedHost.latest_snapshot?.payload}
      {@const workspaceFs = filesystemUsage(payload?.workspace_root_disk)}
      {@const dockerFs = filesystemUsage(payload?.docker_root_disk)}
      <section class="rounded-md border border-line bg-bg-soft p-4">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 class="text-title text-fg">
              {selectedHost.label || selectedHost.id}
            </h2>
            <p class="mt-1 font-mono text-micro text-fg-subtle">
              {selectedHost.id}
            </p>
          </div>
          <div class="flex flex-wrap gap-2">
            <StatusPill
              status={selectedHost.drain_mode ? "pending" : "ready"}
              label={selectedHost.drain_mode ? "Draining" : "Not draining"}
            />
            <StatusPill
              status={selectedHost.placement_available ? "ready" : "failed"}
              label={selectedHost.placement_available
                ? "Placement available"
                : "Placement unavailable"}
            />
          </div>
        </div>

        <div class="mt-4 grid gap-3 md:grid-cols-4">
          {#each telemetryResourceCards(selectedHost) as card (card.key)}
            <div class="rounded-md border border-line bg-bg p-3">
              <p class="text-micro uppercase tracking-wide text-fg-subtle">
                {card.label}
              </p>
              <p class="mt-1 text-title text-fg">{card.value}</p>
              {#if card.subvalue}
                <p class="mt-1 text-micro text-fg-subtle">{card.subvalue}</p>
              {/if}
            </div>
          {/each}
        </div>

        <div class="mt-4 grid gap-3 lg:grid-cols-3">
          <section class="rounded-md border border-line bg-bg p-3">
            <h3 class="text-meta font-semibold text-fg">Runtime counts</h3>
            <div class="mt-3 grid grid-cols-2 gap-2 text-micro">
              {@render Metric({
                label: "Running",
                value: formatNumber(selectedCounts.running),
              })}
              {@render Metric({
                label: "Stopped",
                value: formatNumber(selectedCounts.stopped),
              })}
              {@render Metric({
                label: "Draining",
                value: formatNumber(selectedCounts.draining),
              })}
              {@render Metric({
                label: "Unknown",
                value: formatNumber(selectedCounts.unknown),
              })}
              {@render Metric({
                label: "Stale heartbeat",
                value: formatNumber(selectedCounts.staleHeartbeat),
              })}
              {@render Metric({
                label: "Stale runtime",
                value: formatNumber(selectedCounts.staleRuntimeMetadata),
              })}
            </div>
          </section>

          <section class="rounded-md border border-line bg-bg p-3">
            <h3 class="text-meta font-semibold text-fg">Docker health</h3>
            <div class="mt-3 space-y-2 text-micro text-fg">
              <div class="flex justify-between gap-3">
                <span class="text-fg-subtle">Daemon</span>
                <span
                  >{payload?.docker?.available ? "Available" : "Unknown"}</span
                >
              </div>
              <div class="flex justify-between gap-3">
                <span class="text-fg-subtle">Version</span>
                <span>{payload?.docker?.version || "Unknown"}</span>
              </div>
              <div class="flex justify-between gap-3">
                <span class="text-fg-subtle">Orphan containers</span>
                <span>{formatNumber(payload?.docker?.orphan_containers)}</span>
              </div>
              <div class="flex justify-between gap-3">
                <span class="text-fg-subtle">Orphan networks</span>
                <span>{formatNumber(payload?.docker?.orphan_networks)}</span>
              </div>
            </div>
            {#if payload?.docker?.container_counts}
              <div class="mt-3 flex flex-wrap gap-1">
                {#each Object.entries(payload.docker.container_counts) as [state, count]}
                  <StatusPill
                    status={state}
                    label={`${formatListValue(state)} ${formatNumber(count)}`}
                  />
                {/each}
              </div>
            {/if}
          </section>

          <section class="rounded-md border border-line bg-bg p-3">
            <h3 class="text-meta font-semibold text-fg">Filesystems</h3>
            <div class="mt-3 space-y-3 text-micro">
              {@render FilesystemRow({
                label: "Workspace root",
                fs: workspaceFs,
              })}
              {@render FilesystemRow({ label: "Docker root", fs: dockerFs })}
            </div>
          </section>
        </div>

        <div class="mt-4 grid gap-3 lg:grid-cols-2">
          <section class="rounded-md border border-line bg-bg p-3">
            <h3 class="text-meta font-semibold text-fg">Image tags</h3>
            {#if selectedCounts.imageTags.length}
              <div class="mt-3 flex flex-wrap gap-1">
                {#each selectedCounts.imageTags as image (image.reference)}
                  <StatusPill
                    status="neutral"
                    label={`${image.reference} (${formatNumber(image.count)})`}
                  />
                {/each}
              </div>
            {:else}
              <p class="mt-3 text-micro text-fg-subtle">
                No image tags reported.
              </p>
            {/if}
          </section>

          <section class="rounded-md border border-line bg-bg p-3">
            <h3 class="text-meta font-semibold text-fg">
              Recent failures by host
            </h3>
            <div class="mt-3 grid grid-cols-3 gap-2 text-micro">
              {@render Metric({
                label: "Failed runtimes",
                value: formatNumber(
                  selectedCounts.rows.filter((ws) => ws.status === "failed")
                    .length,
                ),
              })}
              {@render Metric({
                label: "Stale backups",
                value: formatNumber(
                  selectedCounts.rows.filter(
                    (ws) => !ws.last_successful_backup_at,
                  ).length,
                ),
              })}
              {@render Metric({
                label: "Runtime metadata",
                value: formatNumber(selectedCounts.staleRuntimeMetadata),
              })}
            </div>
          </section>
        </div>

        <section
          class="mt-4 overflow-x-auto rounded-md border border-line bg-bg"
        >
          <table class="min-w-full text-left text-micro">
            <thead class="border-b border-line text-fg-subtle">
              <tr>
                <th class="px-4 py-2">Workspace</th>
                <th class="px-3 py-2">Runtime</th>
                <th class="px-3 py-2">Heartbeat</th>
                <th class="px-3 py-2 text-right">Port</th>
                <th class="px-4 py-2">Activity</th>
              </tr>
            </thead>
            <tbody>
              {#each selectedCounts.rows as ws (ws.id)}
                <tr class="border-b border-line/60 last:border-b-0">
                  <td class="max-w-[18rem] px-4 py-2">
                    <a
                      class="block truncate text-fg hover:text-accent-text"
                      href={detailHref("workspace", ws.id)}
                      >{ws.display_name || ws.slug}</a
                    >
                    <span class="block truncate font-mono text-fg-subtle">
                      {ws.organization_slug} / {ws.slug}
                    </span>
                  </td>
                  <td class="px-3 py-2">
                    <div class="flex flex-wrap gap-1">
                      <StatusPill status={ws.status} />
                      <StatusPill
                        status={ws.runtime_power_state || "unknown"}
                        label={formatListValue(
                          ws.runtime_power_state || "unknown",
                        )}
                      />
                    </div>
                    <p class="mt-1 truncate text-fg-subtle">
                      {ws.runtime_image_tag || "Unknown image"}
                    </p>
                  </td>
                  <td class="px-3 py-2">
                    <StatusPill
                      status={ws.heartbeat_freshness}
                      label={telemetryLabel(
                        ws.heartbeat_freshness,
                        ws.heartbeat_age_seconds,
                      )}
                    />
                  </td>
                  <td class="px-3 py-2 text-right">
                    {formatNumber(ws.listen_port)}
                  </td>
                  <td class="px-4 py-2 text-fg-subtle">
                    {formatDateTime(ws.last_activity_at)}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </section>
      </section>
    {/if}
  {:else}
    <StateEmpty
      title="No host inventory yet"
      helper="No workspace placement rows or signed host telemetry snapshots are available."
    />
  {/if}
</div>

{#snippet Metric({ label, value })}
  <div class="rounded border border-line bg-bg px-2 py-1">
    <p class="text-fg-subtle">{label}</p>
    <p class="mt-0.5 font-semibold text-fg">{value}</p>
  </div>
{/snippet}

{#snippet FilesystemRow({ label, fs })}
  <div>
    <div class="flex justify-between gap-3">
      <span class="text-fg-subtle">{label}</span>
      <span>{formatPercent(fs.bytePercent)}</span>
    </div>
    <p class="mt-0.5 truncate text-fg">{fs.byteLabel}</p>
    <p class="mt-0.5 truncate text-fg-subtle">
      {fs.path} · inodes {formatPercent(fs.inodePercent)}
    </p>
  </div>
{/snippet}
