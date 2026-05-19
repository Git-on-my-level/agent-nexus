<script>
  import { onMount } from "svelte";

  import Button from "$lib/components/Button.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import StatusPill from "$lib/hosted/StatusPill.svelte";
  import {
    detailHref,
    formatDateTime,
    formatListValue,
    formatNumber,
    sortRows,
    usageMetricCards,
    usagePressure,
  } from "$lib/hosted/adminOverview.js";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  const TOKEN_STORAGE_KEY = "anx_admin_token";
  const ACTOR_STORAGE_KEY = "anx_admin_actor";

  let token = $state("");
  let actor = $state("");
  let rows = $state([]);
  let q = $state("");
  let plan = $state("");
  let status = $state("");
  let accessMode = $state("");
  let restrictionReason = $state("");
  let pressure = $state("");
  let createdAfter = $state("");
  let sortKey = $state("usage.storage_bytes");
  let sortDirection = $state("desc");
  let loading = $state(false);
  let error = $state("");

  const filteredRows = $derived(
    sortRows(
      rows.filter((org) => {
        const text = [
          org.id,
          org.slug,
          org.display_name,
          org.plan_tier,
          org.plan_resolution?.effective_plan_tier,
        ]
          .join(" ")
          .toLowerCase();
        if (q.trim() && !text.includes(q.trim().toLowerCase())) return false;
        if (
          plan &&
          plan !== org.plan_tier &&
          plan !== org.plan_resolution?.effective_plan_tier
        )
          return false;
        if (status && status !== org.status) return false;
        if (accessMode && accessMode !== org.access_mode) return false;
        if (
          restrictionReason &&
          restrictionReason !== (org.restriction_reason || "none")
        )
          return false;
        if (pressure && pressure !== usagePressure(org)) return false;
        if (
          createdAfter &&
          String(org.created_at ?? "").slice(0, 10) < createdAfter
        )
          return false;
        return true;
      }),
      sortKey,
      sortDirection,
    ),
  );

  const planOptions = $derived(
    uniqueValues(
      rows.flatMap((org) => [
        org.plan_tier,
        org.plan_resolution?.effective_plan_tier,
      ]),
    ),
  );
  const statusOptions = $derived(uniqueValues(rows.map((org) => org.status)));
  const accessOptions = $derived(
    uniqueValues(rows.map((org) => org.access_mode)),
  );
  const restrictionOptions = $derived(
    uniqueValues(rows.map((org) => org.restriction_reason || "none")),
  );

  onMount(() => {
    token = localStorage.getItem(TOKEN_STORAGE_KEY) ?? "";
    actor = localStorage.getItem(ACTOR_STORAGE_KEY) ?? "";
    void loadOrganizations();
  });

  function uniqueValues(values) {
    return [...new Set(values.filter(Boolean))].sort();
  }

  function headers() {
    const out = { "x-anx-admin-token": token.trim() };
    if (actor.trim()) out["x-anx-admin-actor"] = actor.trim();
    return out;
  }

  async function loadOrganizations() {
    if (!token.trim()) {
      error = "Open /hosted/admin and enter an operator admin token first.";
      return;
    }
    loading = true;
    error = "";
    try {
      const res = await hostedCpFetch(
        "admin/analytics/organizations?limit=100",
        {
          headers: headers(),
        },
      );
      if (!res.ok) throw new Error(await responseError(res));
      const body = await res.json();
      rows = body.organizations ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : "Organizations did not load.";
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
  <title>Admin Organizations - Agent Nexus (ANX)</title>
</svelte:head>

<div class="mx-auto max-w-7xl space-y-4 px-4 py-5">
  <header class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <a class="text-micro text-accent-text" href="/hosted/admin"
        >Admin overview</a
      >
      <h1 class="mt-1 text-display text-fg">Organizations</h1>
    </div>
    <Button variant="secondary" onclick={loadOrganizations} disabled={loading}>
      {loading ? "Refreshing..." : "Refresh"}
    </Button>
  </header>

  <section
    class="grid gap-3 rounded-md border border-line bg-bg-soft p-4 md:grid-cols-4"
  >
    <input
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={q}
      placeholder="Slug, name, id, plan"
    />
    <select
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={plan}
    >
      <option value="">Any plan</option>
      {#each planOptions as value}
        <option {value}>{formatListValue(value)}</option>
      {/each}
    </select>
    <select
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={status}
    >
      <option value="">Any status</option>
      {#each statusOptions as value}
        <option {value}>{formatListValue(value)}</option>
      {/each}
    </select>
    <select
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={accessMode}
    >
      <option value="">Any access</option>
      {#each accessOptions as value}
        <option {value}>{formatListValue(value)}</option>
      {/each}
    </select>
    <select
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={restrictionReason}
    >
      <option value="">Any restriction</option>
      {#each restrictionOptions as value}
        <option {value}>{formatListValue(value)}</option>
      {/each}
    </select>
    <select
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={pressure}
    >
      <option value="">Any usage pressure</option>
      <option value="normal">Normal</option>
      <option value="medium">Medium</option>
      <option value="high">High</option>
    </select>
    <input
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={createdAfter}
      type="date"
      aria-label="Created after"
    />
    <div class="grid grid-cols-2 gap-2">
      <select
        class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
        bind:value={sortKey}
      >
        <option value="usage.storage_bytes">Storage</option>
        <option value="usage.artifact_count">Artifacts</option>
        <option value="usage.event_count">Events</option>
        <option value="workspace_counts.total">Workspaces</option>
        <option value="created_at">Created</option>
      </select>
      <select
        class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
        bind:value={sortDirection}
      >
        <option value="desc">Desc</option>
        <option value="asc">Asc</option>
      </select>
    </div>
  </section>

  {#if error}
    <StateError
      title="Organizations did not load"
      message={error}
      onretry={loadOrganizations}
      retrying={loading}
    />
  {:else if filteredRows.length}
    <section class="overflow-x-auto rounded-md border border-line bg-bg-soft">
      <table class="min-w-full text-left text-micro">
        <thead class="border-b border-line text-fg-subtle">
          <tr>
            <th class="px-4 py-2 font-medium">Organization</th>
            <th class="px-3 py-2 font-medium">Plan</th>
            <th class="px-3 py-2 font-medium">State</th>
            <th class="px-3 py-2 text-right font-medium">Storage</th>
            <th class="px-3 py-2 text-right font-medium">Artifacts</th>
            <th class="px-3 py-2 text-right font-medium">Workspaces</th>
            <th class="px-4 py-2 font-medium">Created</th>
          </tr>
        </thead>
        <tbody>
          {#each filteredRows as org (org.id)}
            <tr class="border-b border-line/60 last:border-b-0">
              <td class="max-w-[17rem] px-4 py-2">
                <a
                  class="block truncate text-fg hover:text-accent-text"
                  href={detailHref("org", org.id)}
                >
                  {org.display_name || org.slug}
                </a>
                <span class="block truncate font-mono text-fg-subtle"
                  >{org.slug} · {org.id}</span
                >
              </td>
              <td class="px-3 py-2">
                <StatusPill
                  status={org.plan_resolution?.effective_plan_tier ||
                    org.plan_tier}
                  label={org.plan_resolution?.effective_plan_tier ||
                    org.plan_tier}
                />
              </td>
              <td class="px-3 py-2">
                <div class="flex flex-wrap gap-1">
                  <StatusPill status={org.status} />
                  <StatusPill
                    status={org.access_mode}
                    label={formatListValue(org.access_mode)}
                  />
                  {#if org.restriction_reason}
                    <StatusPill
                      status={org.restriction_reason}
                      label={formatListValue(org.restriction_reason)}
                    />
                  {/if}
                </div>
              </td>
              <td class="px-3 py-2 text-right"
                >{usageMetricCards(org.usage)[0].value}</td
              >
              <td class="px-3 py-2 text-right"
                >{formatNumber(org.usage?.artifact_count)}</td
              >
              <td class="px-3 py-2 text-right"
                >{formatNumber(org.workspace_counts?.total)}</td
              >
              <td class="px-4 py-2 text-fg-subtle"
                >{formatDateTime(org.created_at)}</td
              >
            </tr>
          {/each}
        </tbody>
      </table>
    </section>
  {:else}
    <StateEmpty
      title="No organizations match"
      helper="Adjust filters or refresh the admin analytics data."
    />
  {/if}
</div>
