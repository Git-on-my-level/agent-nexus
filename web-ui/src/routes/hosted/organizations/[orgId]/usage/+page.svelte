<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";

  import { goto } from "$app/navigation";

  import Button from "$lib/components/Button.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import {
    classifiedCpFetch,
    errorUserMessage,
    isAuthError,
  } from "$lib/hosted/fetchState.js";
  import { setActiveOrg } from "$lib/hosted/session.js";

  const orgId = $derived(String($page.params.orgId ?? ""));

  let phase = $state("loading");
  let loadError = $state("");
  let retrying = $state(false);
  /** @type {any} */
  let summary = $state(null);

  async function load() {
    phase = "loading";
    loadError = "";
    retrying = false;
    try {
      const res = await classifiedCpFetch(
        `organizations/${encodeURIComponent(orgId)}/usage-summary`,
      );
      const body = await res.json();
      summary = body.summary ?? null;
      phase = "ready";
    } catch (e) {
      if (isAuthError(e)) {
        await goto("/hosted/start");
        return;
      }
      loadError = errorUserMessage(e);
      summary = null;
      phase = "ready";
    }
  }

  async function retry() {
    retrying = true;
    await load();
  }

  $effect(() => {
    if (!browser || !orgId) return;
    setActiveOrg(orgId);
    load();
  });

  function pct(used, total) {
    const u = Number(used ?? 0);
    const t = Number(total ?? 0);
    if (!t || t <= 0) return 0;
    return Math.min(100, Math.round((u / t) * 100));
  }

  function barColor(p) {
    if (p >= 90) return "bg-danger";
    if (p >= 75) return "bg-warn";
    return "bg-accent-solid";
  }

  function headroomNote(p) {
    if (p >= 90) return "Almost out — consider upgrading.";
    if (p >= 75) return "Getting close to the limit.";
    return "";
  }
</script>

<svelte:head>
  <title>Usage — ANX</title>
</svelte:head>

<div class="space-y-5">
  <div>
    <p class="text-micro text-fg-subtle">
      <a
        class="text-fg-subtle underline-offset-2 transition-colors hover:text-fg hover:underline"
        href={`/hosted/organizations/${encodeURIComponent(orgId)}`}
        >← Overview</a
      >
    </p>
    <h1 class="mt-1 text-display text-fg">Usage</h1>
  </div>

  {#if loadError}
    <StateError message={loadError} onretry={retry} {retrying} />
  {/if}

  {#if phase === "loading"}
    <div class="space-y-3">
      <div class="rounded-md border border-line bg-bg-soft px-4 py-3">
        <Skeleton rows={2} />
      </div>
      <div class="grid gap-3 sm:grid-cols-3">
        {#each [0, 1, 2] as i (i)}
          <div class="rounded-md border border-line bg-bg-soft px-4 py-3">
            <Skeleton rows={4} />
          </div>
        {/each}
      </div>
      <div class="rounded-md border border-line bg-bg-soft px-4 py-4">
        <Skeleton rows={3} />
      </div>
    </div>
  {:else if summary}
    {@const plan = summary.plan ?? {}}
    {@const usage = summary.usage ?? {}}
    {@const quota = summary.quota ?? {}}

    <section class="rounded-md border border-line bg-bg-soft px-4 py-3">
      <div class="flex flex-wrap items-baseline justify-between gap-2">
        <div>
          <div class="text-micro uppercase tracking-wide text-fg-subtle">
            Plan
          </div>
          <div class="mt-0.5 text-subtitle tabular-nums text-fg">
            {plan.display_name ?? "—"}
          </div>
        </div>
        <Button
          variant="secondary"
          href={`/hosted/organizations/${encodeURIComponent(orgId)}/billing`}
          >Change plan</Button
        >
      </div>
    </section>

    <section class="grid gap-3 sm:grid-cols-3">
      {#each [{ label: "Workspaces", used: usage.workspace_count, total: plan.workspace_limit, remaining: quota.workspaces_remaining }, { label: "Artifacts (org total)", used: usage.artifact_count, total: plan.artifact_capacity, remaining: quota.artifacts_remaining }, { label: "Storage (org)", used: usage.storage_gb, total: plan.included_storage_gb, remaining: quota.storage_gb_remaining, suffix: " GB" }] as metric}
        {@const p = pct(metric.used, metric.total)}
        <div class="rounded-md border border-line bg-bg-soft px-4 py-3">
          <div
            class="flex items-center justify-between text-micro uppercase tracking-wide text-fg-subtle"
          >
            <span>{metric.label}</span>
            <span class="tabular-nums">{p}%</span>
          </div>
          <div class="mt-2 text-subtitle tabular-nums text-fg">
            {Number(metric.used ?? 0)}<span class="text-meta text-fg-subtle"
              >{metric.suffix ?? ""} / {metric.total ?? "—"}{metric.suffix ??
                ""}</span
            >
          </div>
          <div class="mt-2 h-1 overflow-hidden rounded-full bg-panel-hover">
            <div
              class="h-full {barColor(p)} transition-all"
              style="width: {p}%"
            ></div>
          </div>
          <p class="mt-2 text-micro text-fg-subtle">
            {Number(metric.remaining ?? 0)}{metric.suffix ?? ""} remaining
            {#if headroomNote(p)}
              · <span class="text-warn-text">{headroomNote(p)}</span>
            {/if}
          </p>
        </div>
      {/each}
    </section>

    <section class="rounded-md border border-line bg-bg-soft px-4 py-3">
      <div class="flex items-center justify-between">
        <h2 class="text-subtitle text-fg">This month</h2>
      </div>
      <div class="mt-3 grid gap-3 sm:grid-cols-3">
        <div>
          <div class="text-micro uppercase tracking-wide text-fg-subtle">
            Launches
          </div>
          <div class="mt-1 text-subtitle tabular-nums text-fg">
            {usage.monthly_launch_count ?? 0}
          </div>
        </div>
        <div>
          <div class="text-micro uppercase tracking-wide text-fg-subtle">
            Org pool cap (per workspace)
          </div>
          <div class="mt-1 text-subtitle tabular-nums text-fg">
            {plan.max_artifacts_per_workspace ?? "—"}
          </div>
        </div>
      </div>
    </section>

    <section class="overflow-hidden rounded-md border border-line bg-bg-soft">
      <div
        class="flex items-center justify-between border-b border-line px-4 py-2.5"
      >
        <h2 class="text-subtitle text-fg">Workspace breakdown</h2>
      </div>
      {#if !summary.workspaces || summary.workspaces.length === 0}
        <p class="px-4 py-4 text-meta text-fg-subtle">
          No workspaces in this organization yet.
        </p>
      {:else}
        <div class="overflow-x-auto">
          <table class="min-w-full text-meta">
            <thead>
              <tr
                class="border-b border-line text-left text-micro uppercase tracking-wide text-fg-subtle"
              >
                <th class="px-4 py-2">Workspace</th>
                <th class="px-4 py-2">Artifacts</th>
                <th class="px-4 py-2">Storage</th>
                <th class="px-4 py-2">Launches (mo)</th>
                <th class="px-4 py-2">Last active</th>
              </tr>
            </thead>
            <tbody>
              {#each summary.workspaces as w (w.id)}
                <tr class="border-b border-line last:border-b-0">
                  <td class="px-4 py-2">
                    <div class="text-fg">
                      {w.display_name || w.slug}
                    </div>
                    <div class="font-mono text-micro text-fg-subtle">
                      {w.slug}
                    </div>
                  </td>
                  <td class="px-4 py-2 tabular-nums text-fg"
                    >{w.artifact_count ?? 0}</td
                  >
                  <td class="px-4 py-2 tabular-nums text-fg"
                    >{w.storage_gb ?? 0} GB</td
                  >
                  <td class="px-4 py-2 tabular-nums text-fg"
                    >{w.monthly_launch_count ?? 0}</td
                  >
                  <td class="px-4 py-2 text-fg-subtle"
                    >{w.last_active_at ?? "—"}</td
                  >
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </section>
  {/if}
</div>
