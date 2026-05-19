<script>
  import { onMount } from "svelte";

  import Button from "$lib/components/Button.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import StatusPill from "$lib/hosted/StatusPill.svelte";
  import {
    countRows,
    detailHref,
    eventLabel,
    formatDateTime,
    formatListValue,
    formatNumber,
    usageMetricCards,
  } from "$lib/hosted/adminOverview.js";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  const TOKEN_STORAGE_KEY = "anx_admin_token";
  const ACTOR_STORAGE_KEY = "anx_admin_actor";

  let token = $state("");
  let actor = $state("");
  let organization = $state(null);
  let workspaces = $state([]);
  let loading = $state(false);
  let error = $state("");

  const organizationId = $derived(
    decodeURIComponent(globalThis.location?.pathname.split("/").pop() ?? ""),
  );
  const usageCards = $derived(usageMetricCards(organization?.usage ?? {}));

  onMount(() => {
    token = localStorage.getItem(TOKEN_STORAGE_KEY) ?? "";
    actor = localStorage.getItem(ACTOR_STORAGE_KEY) ?? "";
    void loadDetail();
  });

  function headers() {
    const out = { "x-anx-admin-token": token.trim() };
    if (actor.trim()) out["x-anx-admin-actor"] = actor.trim();
    return out;
  }

  async function loadDetail() {
    if (!token.trim()) {
      error = "Open /hosted/admin and enter an operator admin token first.";
      return;
    }
    loading = true;
    error = "";
    try {
      const [orgRes, workspaceRes] = await Promise.all([
        hostedCpFetch(
          `admin/analytics/organizations/${encodeURIComponent(organizationId)}`,
          {
            headers: headers(),
          },
        ),
        hostedCpFetch("admin/analytics/workspaces?limit=100", {
          headers: headers(),
        }),
      ]);
      if (!orgRes.ok) throw new Error(await responseError(orgRes));
      if (!workspaceRes.ok) throw new Error(await responseError(workspaceRes));
      organization = (await orgRes.json()).organization ?? null;
      const workspaceBody = await workspaceRes.json();
      workspaces = (workspaceBody.workspaces ?? []).filter(
        (ws) => ws.organization_id === organizationId,
      );
    } catch (e) {
      error = e instanceof Error ? e.message : "Organization did not load.";
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
  <title>Admin Organization - Agent Nexus (ANX)</title>
</svelte:head>

<div class="mx-auto max-w-7xl space-y-4 px-4 py-5">
  <header class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <a class="text-micro text-accent-text" href="/hosted/admin/organizations"
        >Organizations</a
      >
      <h1 class="mt-1 text-display text-fg">
        {organization?.display_name || organization?.slug || "Organization"}
      </h1>
      {#if organization}
        <p class="mt-1 font-mono text-micro text-fg-subtle">
          {organization.slug} · {organization.id}
        </p>
      {/if}
    </div>
    <Button variant="secondary" onclick={loadDetail} disabled={loading}>
      {loading ? "Refreshing..." : "Refresh"}
    </Button>
  </header>

  {#if error}
    <StateError
      title="Organization did not load"
      message={error}
      onretry={loadDetail}
      retrying={loading}
    />
  {:else if organization}
    <section class="grid gap-3 md:grid-cols-4">
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <div class="text-micro uppercase tracking-wide text-fg-subtle">
          Plan
        </div>
        <div class="mt-2 text-heading text-fg">
          {formatListValue(
            organization.plan_resolution?.effective_plan_tier ||
              organization.plan_tier,
          )}
        </div>
        <div class="mt-1 text-micro text-fg-muted">
          {formatListValue(organization.plan_resolution?.source)} source
        </div>
      </div>
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <div class="text-micro uppercase tracking-wide text-fg-subtle">
          Billing
        </div>
        <div class="mt-2 text-heading text-fg">
          {formatListValue(organization.billing?.billing_status)}
        </div>
        <div class="mt-1 text-micro text-fg-muted">
          {formatListValue(organization.billing?.stripe_subscription_status)}
        </div>
      </div>
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <div class="text-micro uppercase tracking-wide text-fg-subtle">
          Usage aggregation
        </div>
        <div class="mt-2 text-heading text-fg">
          {formatDateTime(organization.last_usage_aggregation_at)}
        </div>
        <div class="mt-1 text-micro text-fg-muted">
          {formatDateTime(organization.updated_at)} updated
        </div>
      </div>
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <div class="text-micro uppercase tracking-wide text-fg-subtle">
          State
        </div>
        <div class="mt-2 flex flex-wrap gap-1">
          <StatusPill status={organization.status} />
          <StatusPill
            status={organization.access_mode}
            label={formatListValue(organization.access_mode)}
          />
        </div>
      </div>
    </section>

    <section class="grid gap-3 md:grid-cols-4">
      {#each usageCards as card (card.key)}
        <div class="rounded-md border border-line bg-bg-soft p-4">
          <div class="text-micro uppercase tracking-wide text-fg-subtle">
            {card.label}
          </div>
          <div class="mt-2 text-heading text-fg">{card.value}</div>
          <div class="mt-1 text-micro text-fg-muted">{card.subvalue}</div>
        </div>
      {/each}
    </section>

    <section class="grid gap-3 xl:grid-cols-3">
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <h2 class="text-heading text-fg">Quota envelope</h2>
        <dl class="mt-3 grid gap-2 text-micro">
          {#each Object.entries(organization.plan_resolution?.quota ?? {}) as [key, value]}
            <div class="flex justify-between gap-3">
              <dt class="text-fg-subtle">{formatListValue(key)}</dt>
              <dd class="font-mono text-fg">{formatNumber(value)}</dd>
            </div>
          {:else}
            <p class="text-fg-subtle">No quota envelope reported.</p>
          {/each}
        </dl>
      </div>
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <h2 class="text-heading text-fg">Members</h2>
        <div class="mt-3 space-y-1">
          {#each countRows(organization.member_counts) as row (row.key)}
            <div class="flex justify-between gap-3 text-micro">
              <span class="text-fg-subtle">{row.label}</span>
              <span class="font-mono text-fg">{formatNumber(row.value)}</span>
            </div>
          {/each}
        </div>
      </div>
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <h2 class="text-heading text-fg">Workspace states</h2>
        <div class="mt-3 space-y-1">
          {#each countRows(organization.workspace_counts?.by_status) as row (row.key)}
            <div class="flex justify-between gap-3 text-micro">
              <span class="text-fg-subtle">{row.label}</span>
              <span class="font-mono text-fg">{formatNumber(row.value)}</span>
            </div>
          {/each}
        </div>
      </div>
    </section>

    <section class="rounded-md border border-line bg-bg-soft">
      <div class="border-b border-line px-4 py-3">
        <h2 class="text-heading text-fg">Workspaces</h2>
      </div>
      {#if workspaces.length}
        <div class="overflow-x-auto">
          <table class="min-w-full text-left text-micro">
            <thead class="border-b border-line text-fg-subtle">
              <tr
                ><th class="px-4 py-2">Workspace</th><th class="px-3 py-2"
                  >State</th
                ><th class="px-3 py-2">Host</th><th class="px-3 py-2 text-right"
                  >Storage</th
                ><th class="px-4 py-2">Activity</th></tr
              >
            </thead>
            <tbody>
              {#each workspaces as ws (ws.id)}
                <tr class="border-b border-line/60 last:border-b-0">
                  <td class="px-4 py-2"
                    ><a
                      class="text-fg hover:text-accent-text"
                      href={detailHref("workspace", ws.id)}
                      >{ws.display_name || ws.slug}</a
                    ><span class="block font-mono text-fg-subtle"
                      >{ws.slug}</span
                    ></td
                  >
                  <td class="px-3 py-2"><StatusPill status={ws.status} /></td>
                  <td class="px-3 py-2 text-fg-subtle"
                    >{ws.host_label || ws.host_id || "Unknown"}</td
                  >
                  <td class="px-3 py-2 text-right"
                    >{usageMetricCards(ws.usage)[0].value}</td
                  >
                  <td class="px-4 py-2 text-fg-subtle"
                    >{formatDateTime(ws.last_activity_at)}</td
                  >
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else}
        <StateEmpty
          title="No workspaces"
          helper="No workspace rows were returned for this organization."
        />
      {/if}
    </section>

    <section class="grid gap-3 xl:grid-cols-3">
      {@render EventList("Audit timeline", organization.recent_audit_events)}
      {@render JobList(
        "Provisioning jobs",
        organization.recent_provisioning_jobs,
      )}
      {@render JobList("Backup runs", organization.recent_backup_runs)}
    </section>
  {/if}
</div>

{#snippet EventList(title, events)}
  <div class="rounded-md border border-line bg-bg-soft p-4">
    <h2 class="text-heading text-fg">{title}</h2>
    {#if events?.length}
      <ul class="mt-3 divide-y divide-line">
        {#each events.slice(0, 8) as event (event.id)}
          <li class="py-2 text-micro">
            <div class="text-fg">{eventLabel(event.event_type)}</div>
            <div class="font-mono text-fg-subtle">
              {formatDateTime(event.occurred_at)}
            </div>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="mt-3 text-micro text-fg-subtle">No recent events.</p>
    {/if}
  </div>
{/snippet}

{#snippet JobList(title, jobs)}
  <div class="rounded-md border border-line bg-bg-soft p-4">
    <h2 class="text-heading text-fg">{title}</h2>
    {#if jobs?.length}
      <ul class="mt-3 divide-y divide-line">
        {#each jobs.slice(0, 8) as job (job.id)}
          <li class="py-2 text-micro">
            <div class="flex justify-between gap-3">
              <span class="text-fg"
                >{formatListValue(job.kind || job.schedule_name)}</span
              ><StatusPill status={job.status} />
            </div>
            <div class="font-mono text-fg-subtle">
              {formatDateTime(job.requested_at)}
            </div>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="mt-3 text-micro text-fg-subtle">No recent rows.</p>
    {/if}
  </div>
{/snippet}
