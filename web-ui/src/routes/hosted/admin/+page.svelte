<script>
  import { onMount } from "svelte";

  import Button from "$lib/components/Button.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import StatusPill from "$lib/hosted/StatusPill.svelte";
  import {
    countRows,
    detailHref,
    eventLabel,
    formatDateTime,
    formatNumber,
    telemetryLabel,
    usageMetricCards,
  } from "$lib/hosted/adminOverview.js";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  const TOKEN_STORAGE_KEY = "anx_admin_token";
  const ACTOR_STORAGE_KEY = "anx_admin_actor";

  let token = $state("");
  let actor = $state("");
  let overview = $state(null);
  let loading = $state(false);
  let error = $state("");
  let unauthorized = $state(false);
  let checkedStoredToken = $state(false);

  const isAuthed = $derived(Boolean(overview));
  const usageCards = $derived(usageMetricCards(overview?.usage_totals ?? {}));
  const health = $derived(overview?.heartbeat_health ?? {});
  const recentOps = $derived(overview?.recent_operations ?? {});

  onMount(() => {
    token = localStorage.getItem(TOKEN_STORAGE_KEY) ?? "";
    actor = localStorage.getItem(ACTOR_STORAGE_KEY) ?? "";
    checkedStoredToken = true;
    if (token.trim()) {
      void loadOverview();
    }
  });

  async function loadOverview() {
    const cleanToken = token.trim();
    if (!cleanToken) {
      unauthorized = true;
      error = "Enter an operator admin token to view analytics.";
      overview = null;
      return;
    }

    loading = true;
    error = "";
    unauthorized = false;
    try {
      localStorage.setItem(TOKEN_STORAGE_KEY, cleanToken);
      localStorage.setItem(ACTOR_STORAGE_KEY, actor.trim());
      const headers = {
        "x-anx-admin-token": cleanToken,
      };
      if (actor.trim()) {
        headers["x-anx-admin-actor"] = actor.trim();
      }
      const res = await hostedCpFetch("admin/analytics/overview", { headers });
      if (!res.ok) {
        let detail = res.statusText;
        try {
          const body = await res.json();
          detail = body?.error?.message || body?.error?.code || detail;
        } catch {
          // Keep statusText.
        }
        if (res.status === 401 || res.status === 404) {
          unauthorized = true;
        }
        throw new Error(detail || "Admin analytics did not load.");
      }
      const body = await res.json();
      overview = body.overview ?? null;
      if (!overview) {
        throw new Error("Admin analytics response was empty.");
      }
    } catch (e) {
      overview = null;
      error = e instanceof Error ? e.message : "Admin analytics did not load.";
    } finally {
      loading = false;
    }
  }

  function clearToken() {
    localStorage.removeItem(TOKEN_STORAGE_KEY);
    localStorage.removeItem(ACTOR_STORAGE_KEY);
    token = "";
    actor = "";
    overview = null;
    error = "";
    unauthorized = false;
  }

  function targetHref(event) {
    if (event?.workspace_id) return detailHref("workspace", event.workspace_id);
    if (event?.organization_id) return detailHref("org", event.organization_id);
    if (event?.actor_account_id)
      return detailHref("account", event.actor_account_id);
    return "/hosted/admin";
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
        Read-only control-plane health, usage, quota pressure, and recent
        operational events.
      </p>
    </div>
    {#if isAuthed}
      <div class="flex items-center gap-2">
        <Button variant="secondary" onclick={loadOverview} disabled={loading}
          >Refresh</Button
        >
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
          void loadOverview();
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
            {loading ? "Checking..." : "Open"}
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
          This page stores the operator token only in this browser's local
          storage and sends it to the control plane for read-only admin
          analytics requests.
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
      onretry={loadOverview}
      retrying={loading}
      supportHint={true}
    />
  {:else if isAuthed}
    <section class="grid gap-3 md:grid-cols-4">
      {@render MetricCard({
        label: "Organizations",
        value: formatNumber(overview.organizations?.total),
        subvalue: `${formatNumber(overview.workspaces?.total)} workspaces`,
      })}
      {@render MetricCard({
        label: "Accounts",
        value: formatNumber(overview.accounts?.total),
        subvalue: "Global hosted accounts",
      })}
      {@render MetricCard({
        label: "Heartbeat issues",
        value: formatNumber((health.stale ?? 0) + (health.unknown ?? 0)),
        subvalue: `${formatNumber(health.stale)} stale / ${formatNumber(
          health.unknown,
        )} unknown`,
        tone: (health.stale ?? 0) + (health.unknown ?? 0) > 0 ? "warn" : "ok",
      })}
      {@render MetricCard({
        label: "Generated",
        value: formatDateTime(overview.generated_at),
        subvalue: `Freshness window ${formatNumber(
          overview.telemetry_max_age_seconds,
        )}s`,
      })}
    </section>

    <section class="grid gap-3 lg:grid-cols-4">
      {#each usageCards as card (card.key)}
        {@render MetricCard(card)}
      {/each}
    </section>

    <section class="grid gap-3 xl:grid-cols-3">
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <div class="mb-3 flex items-center justify-between">
          <h2 class="text-heading text-fg">Organizations</h2>
          <span class="text-micro text-fg-subtle">
            {formatNumber(overview.organizations?.total)} total
          </span>
        </div>
        <div class="grid gap-3 sm:grid-cols-2">
          {@render RollupGroup({
            title: "Status",
            rows: countRows(overview.organizations?.by_status),
          })}
          {@render RollupGroup({
            title: "Access",
            rows: countRows(overview.organizations?.by_access_mode),
          })}
          {@render RollupGroup({
            title: "Restrictions",
            rows: countRows(overview.organizations?.by_restriction_reason),
          })}
          {@render RollupGroup({
            title: "Plan",
            rows: countRows(overview.organizations?.by_plan),
          })}
        </div>
      </div>

      <div class="rounded-md border border-line bg-bg-soft p-4">
        <div class="mb-3 flex items-center justify-between">
          <h2 class="text-heading text-fg">Accounts</h2>
          <span class="text-micro text-fg-subtle">
            {formatNumber(overview.accounts?.total)} total
          </span>
        </div>
        <div class="grid gap-3 sm:grid-cols-2">
          {@render RollupGroup({
            title: "Status",
            rows: countRows(overview.accounts?.by_status),
          })}
          {@render RollupGroup({
            title: "Recent login",
            rows: countRows(overview.accounts?.by_recent_login_bucket),
          })}
        </div>
      </div>

      <div class="rounded-md border border-line bg-bg-soft p-4">
        <div class="mb-3 flex items-center justify-between">
          <h2 class="text-heading text-fg">Workspaces</h2>
          <span class="text-micro text-fg-subtle">
            {formatNumber(overview.workspaces?.total)} total
          </span>
        </div>
        <div class="grid gap-3 sm:grid-cols-2">
          {@render RollupGroup({
            title: "Status",
            rows: countRows(overview.workspaces?.by_status),
          })}
          {@render RollupGroup({
            title: "Power",
            rows: countRows(overview.workspaces?.by_runtime_power_state),
          })}
          {@render RollupGroup({
            title: "Heartbeat",
            rows: countRows(overview.workspaces?.by_freshness),
          })}
          {@render RollupGroup({
            title: "Host",
            rows: countRows(overview.workspaces?.by_host),
          })}
        </div>
      </div>
    </section>

    <section class="grid gap-3 xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
      <div class="rounded-md border border-line bg-bg-soft">
        <div
          class="flex items-center justify-between border-b border-line px-4 py-3"
        >
          <h2 class="text-heading text-fg">Top organizations</h2>
          <a
            class="text-micro text-accent-text"
            href="/hosted/admin/organizations"
          >
            Drilldown
          </a>
        </div>
        {#if overview.top_organizations?.length}
          <div class="overflow-x-auto">
            <table class="min-w-full text-left text-micro">
              <thead class="border-b border-line text-fg-subtle">
                <tr>
                  <th class="px-4 py-2 font-medium">Org</th>
                  <th class="px-3 py-2 font-medium">Plan</th>
                  <th class="px-3 py-2 text-right font-medium">Storage</th>
                  <th class="px-3 py-2 text-right font-medium">Artifacts</th>
                  <th class="px-3 py-2 text-right font-medium">Workspaces</th>
                  <th class="px-3 py-2 text-right font-medium">Stale</th>
                  <th class="px-4 py-2 font-medium">Activity</th>
                </tr>
              </thead>
              <tbody>
                {#each overview.top_organizations as org (org.id)}
                  <tr class="border-b border-line/60 last:border-b-0">
                    <td class="max-w-[15rem] px-4 py-2">
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
                    <td class="px-3 py-2">
                      <StatusPill
                        status={org.effective_plan_tier}
                        label={org.effective_plan_tier || org.plan_tier}
                      />
                    </td>
                    <td class="px-3 py-2 text-right">
                      {usageMetricCards(org)[0].value}
                    </td>
                    <td class="px-3 py-2 text-right">
                      {formatNumber(org.artifact_count)}
                    </td>
                    <td class="px-3 py-2 text-right">
                      {formatNumber(org.workspace_count)}
                    </td>
                    <td class="px-3 py-2 text-right">
                      {formatNumber(org.stale_workspace_count)}
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

      <div class="space-y-3">
        {@render OperationsPanel({
          title: "Provisioning",
          rollup: recentOps.provisioning,
        })}
        {@render OperationsPanel({
          title: "Backups",
          rollup: recentOps.backups,
        })}
        {@render OperationsPanel({
          title: "Billing",
          rollup: recentOps.billing,
        })}
        {@render OperationsPanel({
          title: "Entitlements",
          rollup: recentOps.entitlements,
        })}
      </div>
    </section>

    <section class="rounded-md border border-line bg-bg-soft">
      <div
        class="flex items-center justify-between border-b border-line px-4 py-3"
      >
        <h2 class="text-heading text-fg">Recent high-signal events</h2>
        <a
          class="text-micro text-accent-text"
          href="/hosted/admin/audit-events"
        >
          Drilldown
        </a>
      </div>
      {#if overview.recent_high_signal_events?.length}
        <ul class="divide-y divide-line">
          {#each overview.recent_high_signal_events as event (event.id)}
            <li
              class="grid gap-2 px-4 py-3 text-meta md:grid-cols-[12rem_1fr_auto]"
            >
              <span class="text-fg-subtle"
                >{formatDateTime(event.occurred_at)}</span
              >
              <a
                class="min-w-0 text-fg hover:text-accent-text"
                href={targetHref(event)}
              >
                <span class="block truncate"
                  >{eventLabel(event.event_type)}</span
                >
                <span
                  class="block truncate font-mono text-micro text-fg-subtle"
                >
                  {event.organization_id ||
                    event.workspace_id ||
                    event.actor_account_id ||
                    event.id}
                </span>
              </a>
              <StatusPill
                status={event.event_type?.includes("failed")
                  ? "failed"
                  : "active"}
                label={event.event_type?.includes("failed")
                  ? "Failure"
                  : "Event"}
              />
            </li>
          {/each}
        </ul>
      {:else}
        <StateEmpty
          title="No recent high-signal events"
          helper="Quota, restriction, billing, provisioning, backup, restore, and launch events will appear here."
        />
      {/if}
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

{#snippet RollupGroup({ title, rows })}
  <div>
    <h3 class="mb-1 text-micro uppercase tracking-wide text-fg-subtle">
      {title}
    </h3>
    {#if rows?.length}
      <ul class="space-y-1">
        {#each rows.slice(0, 6) as row (row.key)}
          <li class="flex items-center justify-between gap-3 text-micro">
            <span class="min-w-0 truncate text-fg-muted">
              {row.key === "stale" || row.key === "unknown"
                ? telemetryLabel(row.key)
                : row.label}
            </span>
            <span class="font-mono text-fg">{formatNumber(row.value)}</span>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="text-micro text-fg-subtle">No data</p>
    {/if}
  </div>
{/snippet}

{#snippet OperationsPanel({ title, rollup })}
  <div class="rounded-md border border-line bg-bg-soft px-4 py-3">
    <div class="flex items-center justify-between gap-3">
      <h3 class="text-meta font-medium text-fg">{title}</h3>
      <StatusPill
        status={rollup?.recent_failure_count > 0 ? "failed" : "active"}
        label={`${formatNumber(rollup?.recent_failure_count)} failures`}
      />
    </div>
    <p class="mt-1 text-micro text-fg-subtle">
      {formatNumber(rollup?.recent_change_count)} recent changes
    </p>
  </div>
{/snippet}
