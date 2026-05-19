<script>
  import { onMount } from "svelte";

  import Button from "$lib/components/Button.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import StatusPill from "$lib/hosted/StatusPill.svelte";
  import {
    detailHref,
    eventLabel,
    formatDateTime,
    formatListValue,
    formatNumber,
    telemetryLabel,
    usageMetricCards,
  } from "$lib/hosted/adminOverview.js";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  const TOKEN_STORAGE_KEY = "anx_admin_token";
  const ACTOR_STORAGE_KEY = "anx_admin_actor";

  let token = $state("");
  let actor = $state("");
  let workspace = $state(null);
  let loading = $state(false);
  let error = $state("");

  const workspaceId = $derived(
    decodeURIComponent(globalThis.location?.pathname.split("/").pop() ?? ""),
  );
  const usageCards = $derived(usageMetricCards(workspace?.usage ?? {}));

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
      const res = await hostedCpFetch(
        `admin/analytics/workspaces/${encodeURIComponent(workspaceId)}`,
        {
          headers: headers(),
        },
      );
      if (!res.ok) throw new Error(await responseError(res));
      workspace = (await res.json()).workspace ?? null;
    } catch (e) {
      error = e instanceof Error ? e.message : "Workspace did not load.";
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
  <title>Admin Workspace - Agent Nexus (ANX)</title>
</svelte:head>

<div class="mx-auto max-w-7xl space-y-4 px-4 py-5">
  <header class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <a class="text-micro text-accent-text" href="/hosted/admin/workspaces"
        >Workspaces</a
      >
      <h1 class="mt-1 text-display text-fg">
        {workspace?.display_name || workspace?.slug || "Workspace"}
      </h1>
      {#if workspace}
        <p class="mt-1 font-mono text-micro text-fg-subtle">
          {workspace.organization_slug}/{workspace.slug} · {workspace.id}
        </p>
      {/if}
    </div>
    <Button variant="secondary" onclick={loadDetail} disabled={loading}
      >{loading ? "Refreshing..." : "Refresh"}</Button
    >
  </header>

  {#if error}
    <StateError
      title="Workspace did not load"
      message={error}
      onretry={loadDetail}
      retrying={loading}
    />
  {:else if workspace}
    <section class="grid gap-3 md:grid-cols-4">
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <div class="text-micro uppercase tracking-wide text-fg-subtle">
          Placement
        </div>
        <div class="mt-2 text-heading text-fg">
          {workspace.host_label || workspace.host_id || "Unknown"}
        </div>
        <div class="mt-1 font-mono text-micro text-fg-muted">
          :{workspace.listen_port || "unknown"}
        </div>
      </div>
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <div class="text-micro uppercase tracking-wide text-fg-subtle">
          Runtime
        </div>
        <div class="mt-2 flex flex-wrap gap-1">
          <StatusPill status={workspace.status} /><StatusPill
            status={workspace.runtime_power_state || "unknown"}
            label={formatListValue(workspace.runtime_power_state || "unknown")}
          />
        </div>
        <div class="mt-2 truncate text-micro text-fg-muted">
          {workspace.runtime_image_tag || "Unknown image"}
        </div>
      </div>
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <div class="text-micro uppercase tracking-wide text-fg-subtle">
          Heartbeat
        </div>
        <div class="mt-2">
          <StatusPill
            status={workspace.heartbeat_freshness}
            label={telemetryLabel(
              workspace.heartbeat_freshness,
              workspace.heartbeat_age_seconds,
            )}
          />
        </div>
        <div class="mt-2 text-micro text-fg-muted">
          {workspace.heartbeat_version || "Unknown"}
          {workspace.heartbeat_build || ""}
        </div>
      </div>
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <div class="text-micro uppercase tracking-wide text-fg-subtle">
          Activity
        </div>
        <div class="mt-2 text-heading text-fg">
          {formatDateTime(workspace.last_activity_at)}
        </div>
        <div class="mt-1 text-micro text-fg-muted">
          {formatNumber(workspace.active_stream_count)} streams
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
        <h2 class="text-heading text-fg">Runtime identifiers</h2>
        <dl class="mt-3 grid gap-2 text-micro">
          <div class="flex justify-between gap-3">
            <dt class="text-fg-subtle">Container</dt>
            <dd class="font-mono text-fg">
              {workspace.container_id_short || "Unknown"}
            </dd>
          </div>
          <div class="flex justify-between gap-3">
            <dt class="text-fg-subtle">Access</dt>
            <dd>
              <StatusPill
                status={workspace.access_mode}
                label={formatListValue(workspace.access_mode)}
              />
            </dd>
          </div>
          <div class="flex justify-between gap-3">
            <dt class="text-fg-subtle">Restriction</dt>
            <dd class="text-fg">
              {formatListValue(workspace.restriction_reason || "none")}
            </dd>
          </div>
          <div class="flex justify-between gap-3">
            <dt class="text-fg-subtle">Stopped</dt>
            <dd class="text-fg">
              {formatDateTime(workspace.runtime_stopped_at)}
            </dd>
          </div>
        </dl>
      </div>
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <h2 class="text-heading text-fg">Health summary</h2>
        <dl class="mt-3 grid gap-2 text-micro">
          {#each Object.entries(workspace.health_summary ?? {}) as [key, value]}
            <div class="flex justify-between gap-3">
              <dt class="text-fg-subtle">{formatListValue(key)}</dt>
              <dd class="max-w-[12rem] truncate text-fg">{String(value)}</dd>
            </div>
          {:else}
            <p class="text-fg-subtle">No health details reported.</p>
          {/each}
        </dl>
      </div>
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <h2 class="text-heading text-fg">Backup summary</h2>
        <div class="mt-3 text-meta text-fg">
          {formatDateTime(workspace.last_successful_backup_at)}
        </div>
        <p class="mt-1 text-micro text-fg-subtle">Last successful backup</p>
      </div>
    </section>

    <section class="grid gap-3 xl:grid-cols-3">
      {@render Rows("Provisioning jobs", workspace.recent_jobs, "kind")}
      {@render Rows(
        "Backup runs",
        workspace.recent_backup_runs,
        "schedule_name",
      )}
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <h2 class="text-heading text-fg">Audit timeline</h2>
        {#if workspace.recent_audit_events?.length}
          <ul class="mt-3 divide-y divide-line">
            {#each workspace.recent_audit_events.slice(0, 10) as event (event.id)}
              <li class="py-2 text-micro">
                <div class="text-fg">{eventLabel(event.event_type)}</div>
                <div class="font-mono text-fg-subtle">
                  {formatDateTime(event.occurred_at)}
                </div>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="mt-3 text-micro text-fg-subtle">No recent audit events.</p>
        {/if}
      </div>
    </section>

    <p class="text-micro text-fg-subtle">
      Organization: <a
        class="text-accent-text"
        href={detailHref("org", workspace.organization_id)}
        >{workspace.organization_slug}</a
      >
    </p>
  {/if}
</div>

{#snippet Rows(title, rows, labelKey)}
  <div class="rounded-md border border-line bg-bg-soft p-4">
    <h2 class="text-heading text-fg">{title}</h2>
    {#if rows?.length}
      <ul class="mt-3 divide-y divide-line">
        {#each rows.slice(0, 10) as row (row.id)}
          <li class="py-2 text-micro">
            <div class="flex justify-between gap-3">
              <span class="text-fg">{formatListValue(row[labelKey])}</span
              ><StatusPill status={row.status} />
            </div>
            <div class="font-mono text-fg-subtle">
              {formatDateTime(row.requested_at)}
            </div>
            {#if row.failure_reason}<div class="mt-1 text-danger-text">
                {row.failure_reason}
              </div>{/if}
          </li>
        {/each}
      </ul>
    {:else}
      <p class="mt-3 text-micro text-fg-subtle">No recent rows.</p>
    {/if}
  </div>
{/snippet}
