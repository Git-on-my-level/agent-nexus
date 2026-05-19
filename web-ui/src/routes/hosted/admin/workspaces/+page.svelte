<script>
  import { onMount } from "svelte";

  import Button from "$lib/components/Button.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import StatusPill from "$lib/hosted/StatusPill.svelte";
  import {
    backupFreshness,
    detailHref,
    formatDateTime,
    formatListValue,
    formatNumber,
    sortRows,
    telemetryLabel,
    usageMetricCards,
  } from "$lib/hosted/adminOverview.js";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  const TOKEN_STORAGE_KEY = "anx_admin_token";
  const ACTOR_STORAGE_KEY = "anx_admin_actor";

  let token = $state("");
  let actor = $state("");
  let rows = $state([]);
  let q = $state("");
  let status = $state("");
  let accessMode = $state("");
  let restrictionReason = $state("");
  let host = $state("");
  let runtimeState = $state("");
  let heartbeat = $state("");
  let backup = $state("");
  let version = $state("");
  let sortKey = $state("last_activity_at");
  let sortDirection = $state("desc");
  let loading = $state(false);
  let error = $state("");

  const filteredRows = $derived(
    sortRows(
      rows.filter((ws) => {
        const text = [
          ws.id,
          ws.slug,
          ws.display_name,
          ws.organization_slug,
          ws.organization_id,
          ws.heartbeat_version,
          ws.heartbeat_build,
        ]
          .join(" ")
          .toLowerCase();
        if (q.trim() && !text.includes(q.trim().toLowerCase())) return false;
        if (status && status !== ws.status) return false;
        if (accessMode && accessMode !== ws.access_mode) return false;
        if (
          restrictionReason &&
          restrictionReason !== (ws.restriction_reason || "none")
        )
          return false;
        if (host && host !== (ws.host_label || ws.host_id || "unknown"))
          return false;
        if (
          runtimeState &&
          runtimeState !== (ws.runtime_power_state || "unknown")
        )
          return false;
        if (heartbeat && heartbeat !== ws.heartbeat_freshness) return false;
        if (backup && backup !== backupFreshness(ws)) return false;
        if (
          version &&
          !`${ws.heartbeat_version ?? ""} ${ws.heartbeat_build ?? ""}`
            .toLowerCase()
            .includes(version.toLowerCase())
        )
          return false;
        return true;
      }),
      sortKey,
      sortDirection,
    ),
  );

  const statusOptions = $derived(uniqueValues(rows.map((ws) => ws.status)));
  const accessOptions = $derived(
    uniqueValues(rows.map((ws) => ws.access_mode)),
  );
  const restrictionOptions = $derived(
    uniqueValues(rows.map((ws) => ws.restriction_reason || "none")),
  );
  const hostOptions = $derived(
    uniqueValues(rows.map((ws) => ws.host_label || ws.host_id || "unknown")),
  );
  const runtimeOptions = $derived(
    uniqueValues(rows.map((ws) => ws.runtime_power_state || "unknown")),
  );
  const heartbeatOptions = $derived(
    uniqueValues(rows.map((ws) => ws.heartbeat_freshness)),
  );

  onMount(() => {
    token = localStorage.getItem(TOKEN_STORAGE_KEY) ?? "";
    actor = localStorage.getItem(ACTOR_STORAGE_KEY) ?? "";
    void loadWorkspaces();
  });

  function uniqueValues(values) {
    return [...new Set(values.filter(Boolean))].sort();
  }

  function headers() {
    const out = { "x-anx-admin-token": token.trim() };
    if (actor.trim()) out["x-anx-admin-actor"] = actor.trim();
    return out;
  }

  async function loadWorkspaces() {
    if (!token.trim()) {
      error = "Open /hosted/admin and enter an operator admin token first.";
      return;
    }
    loading = true;
    error = "";
    try {
      const res = await hostedCpFetch("admin/analytics/workspaces?limit=100", {
        headers: headers(),
      });
      if (!res.ok) throw new Error(await responseError(res));
      rows = (await res.json()).workspaces ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : "Workspaces did not load.";
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
  <title>Admin Workspaces - Agent Nexus (ANX)</title>
</svelte:head>

<div class="mx-auto max-w-7xl space-y-4 px-4 py-5">
  <header class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <a class="text-micro text-accent-text" href="/hosted/admin"
        >Admin overview</a
      >
      <h1 class="mt-1 text-display text-fg">Workspaces</h1>
    </div>
    <Button variant="secondary" onclick={loadWorkspaces} disabled={loading}>
      {loading ? "Refreshing..." : "Refresh"}
    </Button>
  </header>

  <section
    class="grid gap-3 rounded-md border border-line bg-bg-soft p-4 md:grid-cols-4"
  >
    <input
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={q}
      placeholder="Org, slug, id"
    />
    <select
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={status}
      ><option value="">Any status</option>{#each statusOptions as value}<option
          {value}>{formatListValue(value)}</option
        >{/each}</select
    >
    <select
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={accessMode}
      ><option value="">Any access</option>{#each accessOptions as value}<option
          {value}>{formatListValue(value)}</option
        >{/each}</select
    >
    <select
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={restrictionReason}
      ><option value="">Any restriction</option
      >{#each restrictionOptions as value}<option {value}
          >{formatListValue(value)}</option
        >{/each}</select
    >
    <select
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={host}
      ><option value="">Any host</option>{#each hostOptions as value}<option
          {value}>{value}</option
        >{/each}</select
    >
    <select
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={runtimeState}
      ><option value="">Any runtime</option
      >{#each runtimeOptions as value}<option {value}
          >{formatListValue(value)}</option
        >{/each}</select
    >
    <select
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={heartbeat}
      ><option value="">Any heartbeat</option
      >{#each heartbeatOptions as value}<option {value}
          >{telemetryLabel(value)}</option
        >{/each}</select
    >
    <select
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={backup}
      ><option value="">Any backup</option><option value="fresh"
        >Fresh backup</option
      ><option value="stale">Stale backup</option><option value="unknown"
        >Unknown backup</option
      ></select
    >
    <input
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={version}
      placeholder="Version/build"
    />
    <div class="grid grid-cols-2 gap-2 md:col-span-2">
      <select
        class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
        bind:value={sortKey}
      >
        <option value="last_activity_at">Activity</option>
        <option value="usage.storage_bytes">Storage</option>
        <option value="active_stream_count">Streams</option>
        <option value="heartbeat_age_seconds">Heartbeat age</option>
        <option value="listen_port">Listen port</option>
      </select>
      <select
        class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
        bind:value={sortDirection}
        ><option value="desc">Desc</option><option value="asc">Asc</option
        ></select
      >
    </div>
  </section>

  {#if error}
    <StateError
      title="Workspaces did not load"
      message={error}
      onretry={loadWorkspaces}
      retrying={loading}
    />
  {:else if filteredRows.length}
    <section class="overflow-x-auto rounded-md border border-line bg-bg-soft">
      <table class="min-w-full text-left text-micro">
        <thead class="border-b border-line text-fg-subtle">
          <tr
            ><th class="px-4 py-2">Workspace</th><th class="px-3 py-2">Org</th
            ><th class="px-3 py-2">Runtime</th><th class="px-3 py-2"
              >Heartbeat</th
            ><th class="px-3 py-2 text-right">Storage</th><th
              class="px-3 py-2 text-right">Streams</th
            ><th class="px-4 py-2">Activity</th></tr
          >
        </thead>
        <tbody>
          {#each filteredRows as ws (ws.id)}
            <tr class="border-b border-line/60 last:border-b-0">
              <td class="max-w-[17rem] px-4 py-2"
                ><a
                  class="block truncate text-fg hover:text-accent-text"
                  href={detailHref("workspace", ws.id)}
                  >{ws.display_name || ws.slug}</a
                ><span class="block truncate font-mono text-fg-subtle"
                  >{ws.slug} · {ws.id}</span
                ></td
              >
              <td class="px-3 py-2"
                ><a
                  class="text-accent-text"
                  href={detailHref("org", ws.organization_id)}
                  >{ws.organization_slug}</a
                ></td
              >
              <td class="px-3 py-2"
                ><div class="flex flex-wrap gap-1">
                  <StatusPill status={ws.status} /><StatusPill
                    status={ws.runtime_power_state || "unknown"}
                    label={formatListValue(ws.runtime_power_state || "unknown")}
                  />
                </div>
                <div class="mt-1 text-fg-subtle">
                  {ws.host_label || ws.host_id || "Unknown"}
                </div></td
              >
              <td class="px-3 py-2"
                ><StatusPill
                  status={ws.heartbeat_freshness}
                  label={telemetryLabel(
                    ws.heartbeat_freshness,
                    ws.heartbeat_age_seconds,
                  )}
                />
                <div class="mt-1 text-fg-subtle">
                  {ws.heartbeat_version || "Unknown"}
                  {ws.heartbeat_build || ""}
                </div></td
              >
              <td class="px-3 py-2 text-right"
                >{usageMetricCards(ws.usage)[0].value}</td
              >
              <td class="px-3 py-2 text-right"
                >{formatNumber(ws.active_stream_count)}</td
              >
              <td class="px-4 py-2 text-fg-subtle"
                >{formatDateTime(ws.last_activity_at)}</td
              >
            </tr>
          {/each}
        </tbody>
      </table>
    </section>
  {:else}
    <StateEmpty
      title="No workspaces match"
      helper="Adjust filters or refresh the admin analytics data."
    />
  {/if}
</div>
