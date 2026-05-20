<script>
  import { onMount } from "svelte";

  import Button from "$lib/components/Button.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import StatusPill from "$lib/hosted/StatusPill.svelte";
  import {
    adminHeaders,
    clearAdminCredentials,
    readAdminActor,
    readAdminToken,
    writeAdminCredentials,
  } from "$lib/hosted/adminAuth.js";
  import {
    filesystemUsage,
    saturationDriverLabel,
    saturationTone,
    telemetryResourceCards,
  } from "$lib/hosted/adminInfra.js";
  import {
    countRows,
    detailHref,
    eventLabel,
    formatAgeSeconds,
    formatDateTime,
    formatDuration,
    formatNumber,
    formatRate,
    isWithinOpsWindow,
    quotaPressureRatio,
    telemetryLabel,
    usageMetricCards,
  } from "$lib/hosted/adminOverview.js";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";
  import { formatStorageBytes } from "$lib/hosted/usageStats.js";

  const WINDOW_STORAGE_KEY = "anx_admin_ops_window";
  const WINDOW_OPTIONS = ["1h", "6h", "24h", "7d"];

  let token = $state("");
  let actor = $state("");
  let overview = $state(null);
  let capacity = $state(null);
  let opsHealth = $state(null);
  let opsWindow = $state("24h");
  let loading = $state(false);
  let opsLoading = $state(false);
  let error = $state("");
  let unauthorized = $state(false);
  let checkedStoredToken = $state(false);
  let expandedHostId = $state(null);
  let expandedHostDetail = $state(null);
  let expandedHostLoading = $state(false);
  let expandedOpsPanel = $state(null);

  const isAuthed = $derived(Boolean(overview));
  const fleet = $derived(capacity?.fleet ?? null);
  const hostRows = $derived(capacity?.hosts ?? []);
  const usageCards = $derived(usageMetricCards(overview?.usage_totals ?? {}));
  const heartbeatHealth = $derived(overview?.heartbeat_health ?? {});
  const attentionItems = $derived.by(() =>
    computeAttention(capacity, opsHealth, heartbeatHealth),
  );
  const filteredHighSignalEvents = $derived(
    (overview?.recent_high_signal_events ?? []).filter((event) =>
      isWithinOpsWindow(event.occurred_at, opsWindow),
    ),
  );

  onMount(() => {
    token = readAdminToken();
    actor = readAdminActor();
    opsWindow = localStorage.getItem(WINDOW_STORAGE_KEY) ?? "24h";
    if (!WINDOW_OPTIONS.includes(opsWindow)) opsWindow = "24h";
    checkedStoredToken = true;
    if (token.trim()) {
      void loadAll();
    }
  });

  async function loadAll() {
    const clean = token.trim();
    if (!clean) {
      unauthorized = true;
      error = "Enter an operator admin token to view analytics.";
      overview = null;
      capacity = null;
      opsHealth = null;
      return;
    }
    loading = true;
    error = "";
    unauthorized = false;
    try {
      writeAdminCredentials(clean, actor);
      const headers = adminHeaders(clean, actor);
      const [overviewRes, capacityRes, opsRes] = await Promise.all([
        hostedCpFetch("admin/analytics/overview", { headers }),
        hostedCpFetch("admin/analytics/capacity", { headers }),
        hostedCpFetch(
          `admin/analytics/operations-health?window=${encodeURIComponent(opsWindow)}`,
          { headers },
        ),
      ]);
      if (!overviewRes.ok) {
        await throwApiError(overviewRes, "Admin overview did not load.");
      }
      overview = (await overviewRes.json()).overview ?? null;
      capacity = capacityRes.ok ? (await capacityRes.json()).capacity : null;
      opsHealth = opsRes.ok ? (await opsRes.json()).operations_health : null;
    } catch (e) {
      overview = null;
      capacity = null;
      opsHealth = null;
      error = e instanceof Error ? e.message : "Admin overview did not load.";
    } finally {
      loading = false;
    }
  }

  async function throwApiError(res, fallback) {
    let detail = res.statusText;
    try {
      const body = await res.json();
      detail = body?.error?.message || body?.error?.code || detail;
    } catch {
      // keep statusText
    }
    if (res.status === 401 || res.status === 404) unauthorized = true;
    throw new Error(detail || fallback);
  }

  async function reloadOps() {
    const clean = token.trim();
    if (!clean) return;
    opsLoading = true;
    try {
      const headers = adminHeaders(clean, actor);
      const res = await hostedCpFetch(
        `admin/analytics/operations-health?window=${encodeURIComponent(opsWindow)}`,
        { headers },
      );
      if (res.ok) {
        opsHealth = (await res.json()).operations_health;
      }
    } finally {
      opsLoading = false;
    }
  }

  function setWindow(value) {
    if (!WINDOW_OPTIONS.includes(value)) return;
    opsWindow = value;
    localStorage.setItem(WINDOW_STORAGE_KEY, value);
    void reloadOps();
  }

  function clearToken() {
    clearAdminCredentials();
    token = "";
    actor = "";
    overview = null;
    capacity = null;
    opsHealth = null;
    error = "";
    unauthorized = false;
  }

  async function toggleHost(id) {
    if (expandedHostId === id) {
      expandedHostId = null;
      expandedHostDetail = null;
      return;
    }
    expandedHostId = id;
    expandedHostDetail = null;
    const clean = token.trim();
    if (!clean) return;
    expandedHostLoading = true;
    try {
      const res = await hostedCpFetch(
        `admin/analytics/hosts/${encodeURIComponent(id)}`,
        { headers: adminHeaders(clean, actor) },
      );
      if (res.ok) {
        expandedHostDetail = (await res.json()).host ?? null;
      }
    } finally {
      expandedHostLoading = false;
    }
  }

  function auditEventsHref(eventType = "") {
    const params = new URLSearchParams();
    if (eventType) params.set("event_types", eventType);
    const qs = params.toString();
    return qs
      ? `/hosted/admin/audit-events?${qs}`
      : "/hosted/admin/audit-events";
  }

  function toggleOps(key) {
    expandedOpsPanel = expandedOpsPanel === key ? null : key;
  }

  function targetHref(event) {
    if (event?.workspace_id) return detailHref("workspace", event.workspace_id);
    if (event?.organization_id) return detailHref("org", event.organization_id);
    if (event?.actor_account_id)
      return detailHref("account", event.actor_account_id);
    return "/hosted/admin";
  }

  function computeAttention(cap, ops, heartbeat) {
    const out = [];
    if (cap?.hosts?.length) {
      for (const host of cap.hosts) {
        if (
          host.freshness === "fresh" &&
          host.placement_available &&
          host.saturation_score >= 85
        ) {
          out.push({
            tone: "danger",
            title: `Host ${host.label || host.id} at ${Math.round(host.saturation_score)}%`,
            detail: `Driven by ${saturationDriverLabel(host.saturation_driver)}.`,
            href: "/hosted/admin/infra",
          });
        }
        if (host.freshness !== "fresh") {
          out.push({
            tone: "warn",
            title: `${host.label || host.id} telemetry ${host.freshness}`,
            detail:
              host.telemetry_age_seconds != null
                ? `Last snapshot ${formatAgeSeconds(host.telemetry_age_seconds)} ago.`
                : "No telemetry on file.",
            href: "/hosted/admin/infra",
          });
        }
      }
    }
    if (cap?.fleet) {
      if (cap.fleet.headroom_workspaces <= 5) {
        out.push({
          tone: "warn",
          title: `Fleet headroom ${cap.fleet.headroom_workspaces} workspaces`,
          detail: cap.fleet.headroom_bottleneck
            ? `Bottleneck: ${saturationDriverLabel(cap.fleet.headroom_bottleneck)}.`
            : "Add hosts before next launch wave.",
          href: "/hosted/admin/infra",
        });
      }
      if (cap.fleet.fleet_inode_pct_max >= 90) {
        out.push({
          tone: "danger",
          title: `Inode pressure ${Math.round(cap.fleet.fleet_inode_pct_max)}%`,
          detail: "A host is close to running out of inodes.",
          href: "/hosted/admin/infra",
        });
      }
    }
    if (ops?.provisioning && ops.provisioning.attempts >= 5) {
      const rate = ops.provisioning.success_rate ?? 1;
      if (rate < 0.9) {
        out.push({
          tone: "danger",
          title: `Provisioning ${formatRate(rate)} success`,
          detail: `${ops.provisioning.failures} failures of ${ops.provisioning.attempts} attempts in last ${ops.window}.`,
        });
      }
    }
    if (ops?.backups) {
      if (
        ops.backups.workspaces_eligible >= 5 &&
        ops.backups.backup_coverage != null &&
        ops.backups.backup_coverage < 0.7
      ) {
        out.push({
          tone: "warn",
          title: `Backup coverage ${formatRate(ops.backups.backup_coverage)}`,
          detail: `${ops.backups.stale_backup_workspace_count} workspaces without a fresh backup.`,
        });
      }
    }
    if (ops?.billing) {
      if (ops.billing.webhook_failure_count > 0) {
        out.push({
          tone: "warn",
          title: `${ops.billing.webhook_failure_count} billing webhook failures`,
          detail: `Within last ${ops?.window ?? "24h"}.`,
          href: "/hosted/admin/audit-events",
        });
      }
      if (ops.billing.subscriptions_past_due > 0) {
        out.push({
          tone: "warn",
          title: `${ops.billing.subscriptions_past_due} subscriptions past due`,
          detail: "Stripe billing status flagged.",
        });
      }
    }
    if (
      heartbeat &&
      Number(heartbeat.stale ?? 0) + Number(heartbeat.unknown ?? 0) >= 5
    ) {
      out.push({
        tone: "warn",
        title: `${heartbeat.stale ?? 0} stale + ${heartbeat.unknown ?? 0} unknown heartbeats`,
        detail: "Workspaces have not reported recently.",
        href: "/hosted/admin/workspaces",
      });
    }
    return out;
  }

  function planLimitLabel(org) {
    if (!org?.storage_bytes_limit) return "";
    return `of ${formatStorageBytes(org.storage_bytes_limit)}`;
  }

  function pressureRatio(org) {
    if (org?.storage_pressure_ratio != null) return org.storage_pressure_ratio;
    return quotaPressureRatio({
      usage: { storage_bytes: org?.storage_bytes },
      quota: { storage_bytes: org?.storage_bytes_limit },
    });
  }

  function pressureTone(ratio) {
    if (ratio == null) return "neutral";
    if (ratio >= 0.9) return "danger";
    if (ratio >= 0.75) return "warn";
    return "ok";
  }

  function fleetGaugeRows(f) {
    if (!f) return [];
    return [
      { key: "cpu", label: "CPU 5m", value: f.fleet_cpu_load_5m_pct },
      { key: "memory", label: "Memory", value: f.fleet_mem_pct },
      {
        key: "ws_disk",
        label: "Workspace disk",
        value: f.fleet_workspace_disk_pct,
      },
      {
        key: "docker_disk",
        label: "Docker disk",
        value: f.fleet_docker_disk_pct,
      },
    ];
  }

  function topOrgsWithPressure() {
    const rows = overview?.top_organizations ?? [];
    return rows.map((org) => ({
      ...org,
      _pressureRatio: pressureRatio(org),
    }));
  }
</script>

<svelte:head>
  <title>Admin Overview — Agent Nexus (ANX)</title>
</svelte:head>

<div class="mx-auto max-w-7xl space-y-5 px-4 py-5">
  <header class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <p class="text-micro uppercase tracking-wide text-fg-subtle">
        Hosted operations
      </p>
      <h1 class="text-display text-fg">Admin overview</h1>
      <p class="mt-1 max-w-3xl text-meta text-fg-muted">
        Fleet saturation, operational health, and tenant pressure — read-only.
      </p>
    </div>
    {#if isAuthed}
      <div class="flex items-center gap-2">
        <Button variant="secondary" onclick={loadAll} disabled={loading}>
          Refresh
        </Button>
        <Button variant="secondary" onclick={clearToken}>Lock</Button>
      </div>
    {/if}
  </header>

  {#if !isAuthed}
    <section class="rounded-md border border-line bg-bg-soft p-4">
      <form
        class="grid gap-3 md:grid-cols-[1fr_18rem_auto]"
        onsubmit={(e) => {
          e.preventDefault();
          void loadAll();
        }}
      >
        <label class="grid gap-1 text-meta text-fg">
          <span class="text-micro uppercase tracking-wide text-fg-subtle">
            Admin token
          </span>
          <input
            bind:value={token}
            type="password"
            autocomplete="off"
            class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg outline-none focus:border-accent"
            placeholder="ANX_CONTROL_PLANE_ADMIN_TOKEN"
          />
        </label>
        <label class="grid gap-1 text-meta text-fg">
          <span class="text-micro uppercase tracking-wide text-fg-subtle">
            Actor label
          </span>
          <input
            bind:value={actor}
            autocomplete="off"
            class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg outline-none focus:border-accent"
            placeholder="operator@example.com"
          />
        </label>
        <div class="flex items-end">
          <Button variant="primary" type="submit" disabled={loading}>
            {loading ? "Checking…" : "Open"}
          </Button>
        </div>
      </form>
      {#if error}
        <p
          class="mt-3 rounded-md px-3 py-2 text-micro {unauthorized
            ? 'bg-danger-soft text-danger-text'
            : 'bg-warn-soft text-warn-text'}"
          role="alert"
        >
          {error}
        </p>
      {:else if checkedStoredToken && !token}
        <p class="mt-3 text-micro text-fg-subtle">
          Press <kbd class="rounded border border-line bg-bg px-1">⌘K</kbd> after
          opening to jump to any org, workspace, or account.
        </p>
      {/if}
    </section>
  {/if}

  {#if loading && !overview}
    <div class="grid gap-3 md:grid-cols-4">
      {#each [0, 1, 2, 3] as i (i)}
        <div class="rounded-md border border-line bg-bg-soft p-4">
          <Skeleton rows={3} />
        </div>
      {/each}
    </div>
  {:else if error && isAuthed}
    <StateError
      title="Admin overview didn't load"
      message={error}
      onretry={loadAll}
      retrying={loading}
      supportHint={true}
    />
  {:else if isAuthed}
    {#if attentionItems.length}
      <section
        class="rounded-md border border-warn/60 bg-warn-soft/40 px-4 py-3"
      >
        <p class="text-micro uppercase tracking-wide text-warn-text">
          Needs attention
        </p>
        <ul class="mt-2 grid gap-2 md:grid-cols-2">
          {#each attentionItems as item, i (i)}
            <li class="flex items-start gap-2 text-meta">
              <span
                class="mt-0.5 inline-block h-2 w-2 shrink-0 rounded-full {item.tone ===
                'danger'
                  ? 'bg-danger'
                  : 'bg-warn'}"
              ></span>
              <div class="min-w-0">
                <p class="truncate text-fg">{item.title}</p>
                <p class="truncate text-micro text-fg-subtle">
                  {item.detail}
                  {#if item.href}
                    · <a class="text-accent-text" href={item.href}>open</a>
                  {/if}
                </p>
              </div>
            </li>
          {/each}
        </ul>
      </section>
    {:else if capacity}
      <section
        class="rounded-md border border-ok/40 bg-ok-soft/30 px-4 py-2 text-micro text-ok-text"
      >
        All hosts within thresholds · no operational alerts.
      </section>
    {/if}

    {#if fleet}
      {@render CapacityBand(fleet)}
    {/if}

    {#if hostRows.length}
      {@render HostHeatmap(hostRows)}
    {/if}

    <section class="grid gap-3 md:grid-cols-4">
      {@render MetricCard({
        label: "Organizations",
        value: formatNumber(overview.organizations?.total),
        subvalue: `${formatNumber(overview.workspaces?.total)} workspaces · ${formatNumber(overview.accounts?.total)} accounts`,
      })}
      {@render MetricCard({
        label: "Heartbeats",
        value: `${formatNumber(heartbeatHealth.fresh ?? 0)} fresh`,
        subvalue: `${formatNumber(heartbeatHealth.stale ?? 0)} stale · ${formatNumber(heartbeatHealth.unknown ?? 0)} unknown`,
        tone:
          (heartbeatHealth.stale ?? 0) + (heartbeatHealth.unknown ?? 0) > 0
            ? "warn"
            : "ok",
      })}
      {@render MetricCard({
        label: "Storage",
        value: usageCards[0].value,
        subvalue: usageCards[0].subvalue,
      })}
      {@render MetricCard({
        label: "Events",
        value: usageCards[2].value,
        subvalue: usageCards[2].subvalue,
      })}
    </section>

    {@render OperationsSection()}

    <section class="grid gap-3 xl:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]">
      {@render TopOrgsPanel()}
      {@render RecentEventsPanel()}
    </section>

    <section class="grid gap-3 xl:grid-cols-3">
      {@render RollupCard("Organizations", overview.organizations, [
        ["Status", overview.organizations?.by_status],
        ["Access", overview.organizations?.by_access_mode],
        ["Plan", overview.organizations?.by_plan],
        ["Restrictions", overview.organizations?.by_restriction_reason],
      ])}
      {@render RollupCard("Accounts", overview.accounts, [
        ["Status", overview.accounts?.by_status],
        ["Recent login", overview.accounts?.by_recent_login_bucket],
      ])}
      {@render RollupCard("Workspaces", overview.workspaces, [
        ["Status", overview.workspaces?.by_status],
        ["Power", overview.workspaces?.by_runtime_power_state],
        ["Heartbeat", overview.workspaces?.by_freshness],
        ["Host", overview.workspaces?.by_host],
      ])}
    </section>
  {/if}
</div>

{#snippet MetricCard({ label, value, subvalue, tone = "neutral" })}
  <div
    class="rounded-md border border-line bg-bg-soft p-4 {tone === 'warn'
      ? 'border-warn/40'
      : tone === 'ok'
        ? 'border-ok/40'
        : ''}"
  >
    <div class="text-micro uppercase tracking-wide text-fg-subtle">{label}</div>
    <div class="mt-2 truncate text-heading text-fg">{value}</div>
    <div class="mt-1 truncate text-micro text-fg-muted">{subvalue}</div>
  </div>
{/snippet}

{#snippet CapacityBand(f)}
  <section class="rounded-md border border-line bg-bg-soft p-4">
    <div class="flex flex-wrap items-end justify-between gap-3">
      <div>
        <p class="text-micro uppercase tracking-wide text-fg-subtle">
          Fleet headroom
        </p>
        <p class="mt-1 text-display text-fg">
          ≈ {formatNumber(f.headroom_workspaces)}
          <span class="text-meta text-fg-muted">workspaces placeable</span>
        </p>
        <p class="mt-1 text-micro text-fg-subtle">
          {f.hosts_fresh}/{f.hosts_total} hosts fresh ·
          {formatNumber(f.workspace_slots_allocated)}/{formatNumber(
            f.workspace_slots_total,
          )} slots used
          {#if f.headroom_bottleneck}
            · bottleneck {saturationDriverLabel(f.headroom_bottleneck)}
          {/if}
        </p>
      </div>
      <a class="text-micro text-accent-text" href="/hosted/admin/infra"
        >Infra detail →</a
      >
    </div>
    <div class="mt-4 grid gap-3 sm:grid-cols-4">
      {#each fleetGaugeRows(f) as row (row.key)}
        <div class="rounded border border-line bg-bg p-3">
          <p class="text-micro uppercase tracking-wide text-fg-subtle">
            {row.label}
          </p>
          <p class="mt-1 text-heading text-fg">
            {row.value != null ? Math.round(row.value) : "—"}%
          </p>
          <div class="mt-2 h-1.5 w-full overflow-hidden rounded bg-bg-soft">
            <div
              class="h-full {row.value >= 80
                ? 'bg-danger'
                : row.value >= 60
                  ? 'bg-warn'
                  : 'bg-ok'}"
              style="width: {Math.min(100, Math.max(0, row.value ?? 0))}%"
            ></div>
          </div>
        </div>
      {/each}
    </div>
  </section>
{/snippet}

{#snippet HostHeatmap(rows)}
  <section class="rounded-md border border-line bg-bg-soft">
    <div
      class="flex flex-wrap items-center justify-between gap-2 border-b border-line px-4 py-3"
    >
      <div>
        <h2 class="text-heading text-fg">Host saturation</h2>
        <p class="text-micro text-fg-subtle">
          Sorted by worst resource. Click a row for detail.
        </p>
      </div>
      <a class="text-micro text-accent-text" href="/hosted/admin/infra"
        >All hosts</a
      >
    </div>
    <div class="overflow-x-auto">
      <table class="min-w-full text-left text-micro">
        <thead class="border-b border-line text-fg-subtle">
          <tr>
            <th class="px-3 py-2 font-medium">Host</th>
            <th class="px-2 py-2 text-right font-medium">Slots</th>
            <th class="px-2 py-2 text-center font-medium">CPU 5m</th>
            <th class="px-2 py-2 text-center font-medium">Mem</th>
            <th class="px-2 py-2 text-center font-medium">WS disk</th>
            <th class="px-2 py-2 text-center font-medium">Docker</th>
            <th class="px-2 py-2 text-center font-medium">Inodes</th>
            <th class="px-3 py-2 font-medium">Bottleneck</th>
          </tr>
        </thead>
        <tbody>
          {#each rows as row (row.id)}
            <tr
              class="border-b border-line/60 last:border-b-0 hover:bg-bg/50"
              class:opacity-60={row.drain_mode || !row.placement_available}
            >
              <td class="px-3 py-2">
                <button
                  class="block max-w-[14rem] truncate text-left text-fg hover:text-accent-text"
                  onclick={() => toggleHost(row.id)}
                  type="button"
                >
                  {row.label || row.id}
                </button>
                <span class="block text-fg-subtle">
                  {#if row.freshness !== "fresh"}
                    <StatusPill
                      status="failed"
                      label={telemetryLabel(
                        row.freshness,
                        row.telemetry_age_seconds,
                      )}
                    />
                  {:else if row.drain_mode}
                    <StatusPill status="suspended" label="Draining" />
                  {:else if !row.placement_available}
                    <StatusPill status="suspended" label="Unavailable" />
                  {:else}
                    <span class="text-fg-subtle">
                      {telemetryLabel(row.freshness, row.telemetry_age_seconds)}
                    </span>
                  {/if}
                </span>
              </td>
              <td class="px-2 py-2 text-right font-mono text-fg">
                {formatNumber(row.slots_used)}/{formatNumber(row.slots_total)}
              </td>
              {@render HeatCell(row.cpu_load_5m_pct)}
              {@render HeatCell(row.mem_pct)}
              {@render HeatCell(row.workspace_disk_pct)}
              {@render HeatCell(row.docker_disk_pct)}
              {@render HeatCell(row.inode_pct)}
              <td class="px-3 py-2 text-fg-muted">
                {saturationDriverLabel(row.saturation_driver)}
                {#if row.headroom_workspaces != null && row.placement_available}
                  <span class="block text-fg-subtle">
                    +{formatNumber(row.headroom_workspaces)} room
                  </span>
                {/if}
              </td>
            </tr>
            {#if expandedHostId === row.id}
              <tr class="bg-bg/40">
                <td colspan="8" class="px-4 py-3 text-meta">
                  {@render HostDetail(row)}
                </td>
              </tr>
            {/if}
          {/each}
        </tbody>
      </table>
    </div>
  </section>
{/snippet}

{#snippet HeatCell(pct)}
  <td class="px-1 py-1 text-center">
    <span
      class="inline-block min-w-[3rem] rounded px-1 py-0.5 font-mono {saturationTone(
        pct,
      )}"
    >
      {pct != null ? Math.round(pct) : "—"}%
    </span>
  </td>
{/snippet}

{#snippet HostDetail(host)}
  <div class="space-y-4">
    <div class="grid gap-3 sm:grid-cols-4">
      <div>
        <p class="text-micro uppercase tracking-wide text-fg-subtle">
          Saturation
        </p>
        <p class="mt-0.5 text-fg">
          {Math.round(host.saturation_score)}% on
          {saturationDriverLabel(host.saturation_driver)}
        </p>
      </div>
      <div>
        <p class="text-micro uppercase tracking-wide text-fg-subtle">Headroom</p>
        <p class="mt-0.5 text-fg">
          ≈ {formatNumber(host.headroom_workspaces)} workspaces
          {#if host.headroom_driver && host.placement_available}
            <span class="block text-micro text-fg-subtle">
              limited by {saturationDriverLabel(host.headroom_driver)}
            </span>
          {/if}
        </p>
      </div>
      <div>
        <p class="text-micro uppercase tracking-wide text-fg-subtle">Docker</p>
        <p class="mt-0.5 text-fg">
          {host.docker_daemon_available ? "Daemon up" : "Daemon down"}
          {#if host.orphan_container_count > 0}
            <span class="block text-micro text-warn-text">
              {host.orphan_container_count} orphan containers
            </span>
          {/if}
        </p>
      </div>
      <div>
        <p class="text-micro uppercase tracking-wide text-fg-subtle">
          Telemetry
        </p>
        <p class="mt-0.5 text-fg">
          {telemetryLabel(host.freshness, host.telemetry_age_seconds)}
        </p>
        <a class="text-micro text-accent-text" href="/hosted/admin/infra">
          Open infra →
        </a>
      </div>
    </div>
    {#if expandedHostLoading}
      <Skeleton rows={2} />
    {:else if expandedHostDetail?.latest_snapshot?.payload}
      {@const payload = expandedHostDetail.latest_snapshot.payload}
      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {#each telemetryResourceCards(expandedHostDetail) as card (card.key)}
          <div class="rounded border border-line bg-bg p-2">
            <p class="text-micro uppercase tracking-wide text-fg-subtle">
              {card.label}
            </p>
            <p class="mt-0.5 font-mono text-fg">{card.value}</p>
            <p class="text-micro text-fg-subtle">{card.subvalue}</p>
          </div>
        {/each}
      </div>
      <div class="grid gap-3 md:grid-cols-2">
        {#each [{ label: "Workspace root", fs: payload.workspace_root_disk }, { label: "Docker root", fs: payload.docker_root_disk }] as mount (mount.label)}
          {@const fs = filesystemUsage(mount.fs)}
          <div class="rounded border border-line bg-bg p-2 text-micro">
            <p class="font-medium text-fg">{mount.label}</p>
            <p class="text-fg-subtle">{fs.path}</p>
            <p class="mt-1 text-fg">{fs.byteLabel}</p>
            <p class="text-fg-subtle">
              Inodes: {fs.inodeLabel}
              {#if fs.inodePercent != null}
                ({fs.inodePercent}%)
              {/if}
            </p>
          </div>
        {/each}
      </div>
    {/if}
  </div>
{/snippet}

{#snippet OperationsSection()}
  <section class="rounded-md border border-line bg-bg-soft">
    <div
      class="flex flex-wrap items-center justify-between gap-2 border-b border-line px-4 py-3"
    >
      <div>
        <h2 class="text-heading text-fg">Operational health</h2>
        <p class="text-micro text-fg-subtle">
          Click a card for recent failures.
        </p>
      </div>
      <div
        class="flex items-center gap-1 rounded-md border border-line bg-bg p-0.5"
      >
        {#each WINDOW_OPTIONS as opt (opt)}
          <button
            type="button"
            class="rounded px-2 py-0.5 text-micro {opt === opsWindow
              ? 'bg-bg-soft text-fg'
              : 'text-fg-muted hover:text-fg'}"
            onclick={() => setWindow(opt)}
            disabled={opsLoading}
          >
            {opt}
          </button>
        {/each}
      </div>
    </div>
    {#if opsHealth}
      <div class="grid gap-3 p-4 md:grid-cols-2 lg:grid-cols-4">
        {@render OpsCard({
          key: "provisioning",
          title: "Provisioning",
          headline: formatRate(opsHealth.provisioning?.success_rate ?? 0),
          headlineTone:
            opsHealth.provisioning?.attempts >= 5 &&
            opsHealth.provisioning?.success_rate < 0.9
              ? "danger"
              : "ok",
          subline: `${formatNumber(opsHealth.provisioning?.failures ?? 0)} failed of ${formatNumber(opsHealth.provisioning?.attempts ?? 0)}`,
          detail:
            opsHealth.provisioning?.median_time_to_ready_seconds != null
              ? `p50 ${formatDuration(opsHealth.provisioning.median_time_to_ready_seconds)} · p95 ${formatDuration(opsHealth.provisioning.p95_time_to_ready_seconds)}`
              : "No completed jobs",
          chips: (opsHealth.provisioning?.top_failure_reasons ?? []).map(
            (r) => ({ label: r.reason, count: r.count }),
          ),
        })}
        {@render OpsCard({
          key: "backups",
          title: "Backups",
          headline: formatRate(opsHealth.backups?.backup_coverage ?? 0),
          headlineTone:
            opsHealth.backups?.workspaces_eligible >= 5 &&
            opsHealth.backups?.backup_coverage != null &&
            opsHealth.backups.backup_coverage < 0.7
              ? "warn"
              : "ok",
          subline: `${formatNumber(opsHealth.backups?.stale_backup_workspace_count ?? 0)} stale of ${formatNumber(opsHealth.backups?.workspaces_eligible ?? 0)}`,
          detail:
            opsHealth.backups?.oldest_successful_backup_age_seconds != null
              ? `Oldest backup ${formatAgeSeconds(opsHealth.backups.oldest_successful_backup_age_seconds)} old`
              : "No backups on file",
        })}
        {@render OpsCard({
          key: "billing",
          title: "Billing",
          headline: formatNumber(opsHealth.billing?.webhook_failure_count ?? 0),
          headlineTone:
            (opsHealth.billing?.webhook_failure_count ?? 0) > 0 ? "warn" : "ok",
          subline: "webhook failures",
          detail: `${formatNumber(opsHealth.billing?.subscriptions_past_due ?? 0)} past due · ${formatNumber(opsHealth.billing?.subscriptions_unpaid ?? 0)} unpaid`,
        })}
        {@render OpsCard({
          key: "entitlements",
          title: "Entitlements",
          headline: formatNumber(
            (opsHealth.entitlements?.grants_in_window ?? 0) +
              (opsHealth.entitlements?.revokes_in_window ?? 0),
          ),
          headlineTone: "neutral",
          subline: "changes",
          detail: `${formatNumber(opsHealth.entitlements?.grants_in_window ?? 0)} granted · ${formatNumber(opsHealth.entitlements?.revokes_in_window ?? 0)} revoked`,
        })}
      </div>
      {#if expandedOpsPanel}
        <div class="border-t border-line bg-bg/50 px-4 py-3">
          {@render OpsExpansion(expandedOpsPanel)}
        </div>
      {/if}
    {:else}
      <div class="px-4 py-6">
        <Skeleton rows={2} />
      </div>
    {/if}
  </section>
{/snippet}

{#snippet OpsCard({
  key,
  title,
  headline,
  headlineTone,
  subline,
  detail,
  chips = [],
})}
  <button
    type="button"
    class="rounded-md border border-line bg-bg p-3 text-left transition hover:border-accent/60 {expandedOpsPanel ===
    key
      ? 'border-accent'
      : ''}"
    onclick={() => toggleOps(key)}
  >
    <div class="flex items-center justify-between gap-2">
      <span class="text-micro uppercase tracking-wide text-fg-subtle"
        >{title}</span
      >
      {#if headlineTone === "danger"}
        <span class="h-2 w-2 rounded-full bg-danger"></span>
      {:else if headlineTone === "warn"}
        <span class="h-2 w-2 rounded-full bg-warn"></span>
      {:else if headlineTone === "ok"}
        <span class="h-2 w-2 rounded-full bg-ok"></span>
      {/if}
    </div>
    <p class="mt-1 text-heading text-fg">{headline}</p>
    <p class="text-micro text-fg-muted">{subline}</p>
    <p class="mt-2 text-micro text-fg-subtle">{detail}</p>
    {#if chips.length}
      <div class="mt-2 flex flex-wrap gap-1">
        {#each chips as chip (chip.label)}
          <a
            class="rounded bg-bg-soft px-1.5 py-0.5 text-micro text-fg-muted hover:text-accent-text"
            href={auditEventsHref(
              key === "provisioning"
                ? "provisioning_failed"
                : key === "backups"
                  ? "workspace_backup_failed"
                  : "",
            )}
          >
            {chip.label} · {chip.count}
          </a>
        {/each}
      </div>
    {/if}
  </button>
{/snippet}

{#snippet OpsExpansion(key)}
  {#if key === "provisioning"}
    {#if opsHealth.provisioning?.recent_failures?.length}
      <ul class="space-y-1 text-micro">
        {#each opsHealth.provisioning.recent_failures as job (job.id)}
          <li class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <a
                class="block truncate font-mono text-fg hover:text-accent-text"
                href={detailHref("workspace", job.workspace_id)}
              >
                {job.workspace_id}
              </a>
              <p class="truncate text-fg-subtle">
                {job.kind} · {job.failure_reason || "no reason"}
              </p>
            </div>
            <span class="shrink-0 text-fg-subtle">
              {formatDateTime(job.requested_at)}
            </span>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="text-micro text-fg-subtle">
        No provisioning failures in window.
      </p>
    {/if}
  {:else if key === "backups"}
    {#if opsHealth.backups?.recent_failures?.length}
      <ul class="space-y-1 text-micro">
        {#each opsHealth.backups.recent_failures as run (run.id)}
          <li class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <a
                class="block truncate font-mono text-fg hover:text-accent-text"
                href={detailHref("workspace", run.workspace_id)}
              >
                {run.workspace_id}
              </a>
              <p class="truncate text-fg-subtle">
                {run.schedule_name || "manual"} ·
                {run.failure_reason || "no reason"}
              </p>
            </div>
            <span class="shrink-0 text-fg-subtle">
              {formatDateTime(run.requested_at)}
            </span>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="text-micro text-fg-subtle">No backup failures in window.</p>
    {/if}
  {:else if key === "billing"}
    {#if opsHealth.billing?.recent_webhook_failures?.length}
      <ul class="space-y-1 text-micro">
        {#each opsHealth.billing.recent_webhook_failures as event (event.id)}
          <li class="flex items-start justify-between gap-3">
            <a
              class="min-w-0 truncate font-mono text-fg hover:text-accent-text"
              href={targetHref(event)}
            >
              {event.organization_id || event.id}
            </a>
            <span class="shrink-0 text-fg-subtle">
              {formatDateTime(event.occurred_at)}
            </span>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="text-micro text-fg-subtle">No webhook failures in window.</p>
    {/if}
  {:else if key === "entitlements"}
    {#if opsHealth.entitlements?.recent_events?.length}
      <ul class="space-y-1 text-micro">
        {#each opsHealth.entitlements.recent_events as event (event.id)}
          <li class="flex items-start justify-between gap-3">
            <a
              class="min-w-0 truncate font-mono text-fg hover:text-accent-text"
              href={targetHref(event)}
            >
              {eventLabel(event.event_type)} · {event.organization_id ||
                event.id}
            </a>
            <span class="shrink-0 text-fg-subtle">
              {formatDateTime(event.occurred_at)}
            </span>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="text-micro text-fg-subtle">No entitlement changes in window.</p>
    {/if}
  {/if}
{/snippet}

{#snippet TopOrgsPanel()}
  <div class="rounded-md border border-line bg-bg-soft">
    <div
      class="flex items-center justify-between border-b border-line px-4 py-3"
    >
      <h2 class="text-heading text-fg">Top organizations</h2>
      <a class="text-micro text-accent-text" href="/hosted/admin/organizations">
        All orgs
      </a>
    </div>
    {#if overview.top_organizations?.length}
      <div class="overflow-x-auto">
        <table class="min-w-full text-left text-micro">
          <thead class="border-b border-line text-fg-subtle">
            <tr>
              <th class="px-4 py-2 font-medium">Org</th>
              <th class="px-2 py-2 font-medium">Plan</th>
              <th class="px-2 py-2 text-right font-medium">Storage</th>
              <th class="px-2 py-2 font-medium">Quota</th>
              <th class="px-2 py-2 text-right font-medium">Workspaces</th>
              <th class="px-2 py-2 text-right font-medium">Stale</th>
              <th class="px-4 py-2 font-medium">Activity</th>
            </tr>
          </thead>
          <tbody>
            {#each topOrgsWithPressure() as org (org.id)}
              <tr class="border-b border-line/60 last:border-b-0">
                <td class="max-w-[14rem] px-4 py-2">
                  <a
                    class="block truncate text-fg hover:text-accent-text"
                    href={detailHref("org", org.id)}
                  >
                    {org.display_name || org.slug}
                  </a>
                  <span class="block truncate font-mono text-fg-subtle">
                    {org.slug}
                  </span>
                </td>
                <td class="px-2 py-2">
                  <StatusPill
                    status={org.effective_plan_tier}
                    label={org.effective_plan_tier || org.plan_tier}
                  />
                </td>
                <td class="px-2 py-2 text-right">
                  <span class="block font-mono text-fg">
                    {formatStorageBytes(org.storage_bytes)}
                  </span>
                  <span class="block text-fg-subtle">
                    {planLimitLabel(org)}
                  </span>
                </td>
                <td class="px-2 py-2">
                  {#if org._pressureRatio != null}
                    {@render PressureBar(org._pressureRatio)}
                  {:else}
                    <span class="text-fg-subtle">—</span>
                  {/if}
                </td>
                <td class="px-2 py-2 text-right font-mono text-fg">
                  {formatNumber(org.workspace_count)}
                </td>
                <td class="px-2 py-2 text-right font-mono">
                  <span
                    class={org.stale_workspace_count > 0
                      ? "text-warn-text"
                      : "text-fg-subtle"}
                  >
                    {formatNumber(org.stale_workspace_count)}
                  </span>
                </td>
                <td class="px-4 py-2 text-fg-subtle">
                  {formatDateTime(org.last_activity_at)}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else}
      <StateEmpty
        title="No organizations yet"
        helper="The overview will populate after hosted organizations exist."
      />
    {/if}
  </div>
{/snippet}

{#snippet PressureBar(ratio)}
  {@const pct = Math.round(Math.min(1.5, ratio) * 100)}
  {@const tone = pressureTone(ratio)}
  <div class="flex items-center gap-2">
    <div class="h-1.5 w-16 overflow-hidden rounded bg-bg">
      <div
        class="h-full {tone === 'danger'
          ? 'bg-danger'
          : tone === 'warn'
            ? 'bg-warn'
            : 'bg-ok'}"
        style="width: {Math.min(100, pct)}%"
      ></div>
    </div>
    <span
      class="font-mono text-micro {tone === 'danger'
        ? 'text-danger-text'
        : tone === 'warn'
          ? 'text-warn-text'
          : 'text-fg-subtle'}"
    >
      {pct}%
    </span>
  </div>
{/snippet}

{#snippet RecentEventsPanel()}
  <div class="rounded-md border border-line bg-bg-soft">
    <div
      class="flex items-center justify-between border-b border-line px-4 py-3"
    >
      <h2 class="text-heading text-fg">Recent high-signal events</h2>
      <span class="text-micro text-fg-subtle">{opsWindow} window</span>
      <a class="text-micro text-accent-text" href="/hosted/admin/audit-events">
        Drilldown
      </a>
    </div>
    {#if filteredHighSignalEvents.length}
      <ul class="divide-y divide-line">
        {#each filteredHighSignalEvents.slice(0, 10) as event (event.id)}
          <li class="grid gap-1 px-4 py-2 text-micro">
            <div class="flex items-center justify-between gap-2">
              <span class="truncate text-fg"
                >{eventLabel(event.event_type)}</span
              >
              <span class="shrink-0 text-fg-subtle">
                {formatDateTime(event.occurred_at)}
              </span>
            </div>
            <a
              class="block truncate font-mono text-fg-subtle hover:text-accent-text"
              href={targetHref(event)}
            >
              {event.organization_id ||
                event.workspace_id ||
                event.actor_account_id ||
                event.id}
            </a>
          </li>
        {/each}
      </ul>
    {:else}
      <StateEmpty title="No recent high-signal events" helper="" />
    {/if}
  </div>
{/snippet}

{#snippet RollupCard(title, root, groups)}
  <div class="rounded-md border border-line bg-bg-soft p-4">
    <div class="mb-3 flex items-center justify-between">
      <h2 class="text-heading text-fg">{title}</h2>
      <span class="text-micro text-fg-subtle">
        {formatNumber(root?.total)} total
      </span>
    </div>
    <div class="grid gap-3 sm:grid-cols-2">
      {#each groups as [label, counts] (label)}
        <div>
          <h3 class="mb-1 text-micro uppercase tracking-wide text-fg-subtle">
            {label}
          </h3>
          {#if counts && Object.keys(counts).length}
            <ul class="space-y-1">
              {#each countRows(counts).slice(0, 6) as row (row.key)}
                <li class="flex items-center justify-between gap-3 text-micro">
                  <span class="min-w-0 truncate text-fg-muted">
                    {row.key === "stale" || row.key === "unknown"
                      ? telemetryLabel(row.key)
                      : row.label}
                  </span>
                  <span class="font-mono text-fg">
                    {formatNumber(row.value)}
                  </span>
                </li>
              {/each}
            </ul>
          {:else}
            <p class="text-micro text-fg-subtle">No data</p>
          {/if}
        </div>
      {/each}
    </div>
  </div>
{/snippet}
