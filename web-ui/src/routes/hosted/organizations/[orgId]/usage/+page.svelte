<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";

  import { goto } from "$app/navigation";

  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";
  import { setActiveOrg } from "$lib/hosted/session.js";

  const orgId = $derived(String($page.params.orgId ?? ""));

  let phase = $state("loading");
  let message = $state("");
  /** @type {any} */
  let summary = $state(null);

  async function readError(res) {
    try {
      const j = await res.json();
      return j?.error?.message || j?.error?.code || res.statusText;
    } catch {
      return res.statusText;
    }
  }

  async function load() {
    phase = "loading";
    message = "";
    const res = await hostedCpFetch(
      `organizations/${encodeURIComponent(orgId)}/usage-summary`,
    );
    if (res.status === 401) {
      await goto("/hosted/start");
      return;
    }
    if (!res.ok) {
      message = await readError(res);
      summary = null;
      phase = "ready";
      return;
    }
    const body = await res.json();
    summary = body.summary ?? null;
    phase = "ready";
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
    if (p >= 90) return "bg-red-500";
    if (p >= 75) return "bg-amber-500";
    return "bg-indigo-500";
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
    <p class="text-[11px] text-gray-500">
      <a
        class="text-gray-500 underline-offset-2 transition-colors hover:text-gray-800 hover:underline"
        href={`/hosted/organizations/${encodeURIComponent(orgId)}`}
        >← Overview</a
      >
    </p>
    <h1 class="mt-1 text-lg font-semibold text-gray-900">Usage</h1>
  </div>

  {#if message}
    <p
      role="alert"
      class="rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
    >
      {message}
    </p>
  {/if}

  {#if phase === "loading"}
    <div
      class="rounded-md border border-gray-200 bg-gray-100 px-4 py-6 text-[13px] text-gray-500"
    >
      Loading…
    </div>
  {:else if summary}
    {@const plan = summary.plan ?? {}}
    {@const usage = summary.usage ?? {}}
    {@const quota = summary.quota ?? {}}

    <section class="rounded-md border border-gray-200 bg-gray-100 px-4 py-3">
      <div class="flex flex-wrap items-baseline justify-between gap-2">
        <div>
          <div
            class="text-[11px] font-medium uppercase tracking-wide text-gray-500"
          >
            Plan
          </div>
          <div class="mt-0.5 text-[16px] font-semibold text-gray-900">
            {plan.display_name ?? "—"}
          </div>
        </div>
        <a
          href={`/hosted/organizations/${encodeURIComponent(orgId)}/billing`}
          class="rounded-md border border-gray-200 bg-gray-50 px-3 py-1.5 text-[12px] font-medium text-gray-800 hover:bg-gray-200"
          >Change plan</a
        >
      </div>
    </section>

    <section class="grid gap-3 sm:grid-cols-3">
      {#each [{ label: "Workspaces", used: usage.workspace_count, total: plan.workspace_limit, remaining: quota.workspaces_remaining }, { label: "Artifacts", used: usage.artifact_count, total: plan.artifact_capacity, remaining: quota.artifacts_remaining }, { label: "Storage", used: usage.storage_gb, total: plan.included_storage_gb, remaining: quota.storage_gb_remaining, suffix: " GB" }] as metric}
        {@const p = pct(metric.used, metric.total)}
        <div class="rounded-md border border-gray-200 bg-gray-100 px-4 py-3">
          <div
            class="flex items-center justify-between text-[11px] font-medium uppercase tracking-wide text-gray-500"
          >
            <span>{metric.label}</span>
            <span>{p}%</span>
          </div>
          <div class="mt-2 text-[18px] font-semibold text-gray-900">
            {Number(metric.used ?? 0)}<span
              class="text-[12px] font-normal text-gray-500"
              >{metric.suffix ?? ""} / {metric.total ?? "—"}{metric.suffix ??
                ""}</span
            >
          </div>
          <div class="mt-2 h-1 overflow-hidden rounded-full bg-gray-200">
            <div
              class="h-full {barColor(p)} transition-all"
              style="width: {p}%"
            ></div>
          </div>
          <p class="mt-2 text-[11px] text-gray-500">
            {Number(metric.remaining ?? 0)}{metric.suffix ?? ""} remaining
            {#if headroomNote(p)}
              · <span class="text-amber-400">{headroomNote(p)}</span>
            {/if}
          </p>
        </div>
      {/each}
    </section>

    <section class="rounded-md border border-gray-200 bg-gray-100 px-4 py-3">
      <div class="flex items-center justify-between">
        <h2 class="text-[13px] font-medium text-gray-900">This month</h2>
      </div>
      <div class="mt-3 grid gap-3 sm:grid-cols-3">
        <div>
          <div class="text-[11px] uppercase tracking-wide text-gray-500">
            Launches
          </div>
          <div class="mt-1 text-[16px] font-semibold text-gray-900">
            {usage.monthly_launch_count ?? 0}
          </div>
        </div>
        <div>
          <div class="text-[11px] uppercase tracking-wide text-gray-500">
            Artifacts / workspace cap
          </div>
          <div class="mt-1 text-[16px] font-semibold text-gray-900">
            {plan.max_artifacts_per_workspace ?? "—"}
          </div>
        </div>
      </div>
    </section>

    <section
      class="overflow-hidden rounded-md border border-gray-200 bg-gray-100"
    >
      <div
        class="flex items-center justify-between border-b border-gray-200 px-4 py-2.5"
      >
        <h2 class="text-[13px] font-medium text-gray-900">
          Workspace breakdown
        </h2>
      </div>
      {#if !summary.workspaces || summary.workspaces.length === 0}
        <p class="px-4 py-4 text-[12px] text-gray-500">
          No workspaces in this organization yet.
        </p>
      {:else}
        <div class="overflow-x-auto">
          <table class="min-w-full text-[12px]">
            <thead>
              <tr
                class="border-b border-gray-200 text-left text-[11px] uppercase tracking-wide text-gray-500"
              >
                <th class="px-4 py-2 font-medium">Workspace</th>
                <th class="px-4 py-2 font-medium">Artifacts</th>
                <th class="px-4 py-2 font-medium">Storage</th>
                <th class="px-4 py-2 font-medium">Launches (mo)</th>
                <th class="px-4 py-2 font-medium">Last active</th>
              </tr>
            </thead>
            <tbody>
              {#each summary.workspaces as w (w.id)}
                <tr class="border-b border-gray-200 last:border-b-0">
                  <td class="px-4 py-2">
                    <div class="font-medium text-gray-900">
                      {w.display_name || w.slug}
                    </div>
                    <div class="text-[11px] text-gray-500">{w.slug}</div>
                  </td>
                  <td class="px-4 py-2 text-gray-800"
                    >{w.artifact_count ?? 0}</td
                  >
                  <td class="px-4 py-2 text-gray-800">{w.storage_gb ?? 0} GB</td
                  >
                  <td class="px-4 py-2 text-gray-800"
                    >{w.monthly_launch_count ?? 0}</td
                  >
                  <td class="px-4 py-2 text-gray-500"
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
