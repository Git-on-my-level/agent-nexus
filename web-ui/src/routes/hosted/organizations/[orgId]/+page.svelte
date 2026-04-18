<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import Avatar from "$lib/hosted/Avatar.svelte";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";
  import {
    hostedSession,
    loadHostedSession,
    setActiveOrg,
  } from "$lib/hosted/session.js";

  const orgId = $derived(String($page.params.orgId ?? ""));
  const session = $derived($hostedSession);

  /** @type {any} */
  let organization = $state(null);
  /** @type {any} */
  let usage = $state(null);
  let workspaces = $state(/** @type {any[]} */ ([]));
  let phase = $state("loading");
  let message = $state("");

  async function readError(res) {
    try {
      const j = await res.json();
      return j?.error?.message || j?.error?.code || res.statusText;
    } catch {
      return res.statusText;
    }
  }

  async function loadAll() {
    phase = "loading";
    message = "";
    organization = null;
    usage = null;
    workspaces = [];

    const orgRes = await hostedCpFetch(
      `organizations/${encodeURIComponent(orgId)}`,
    );
    if (orgRes.status === 401) {
      await goto("/hosted/start");
      return;
    }
    if (!orgRes.ok) {
      message = await readError(orgRes);
      phase = "ready";
      return;
    }
    const orgBody = await orgRes.json();
    organization = orgBody?.organization ?? null;

    const [usageRes, wsRes] = await Promise.all([
      hostedCpFetch(`organizations/${encodeURIComponent(orgId)}/usage-summary`),
      hostedCpFetch(
        `workspaces?organization_id=${encodeURIComponent(orgId)}&limit=20`,
      ),
    ]);
    if (usageRes.ok) {
      const u = await usageRes.json();
      usage = u.summary ?? null;
    }
    if (wsRes.ok) {
      const w = await wsRes.json();
      workspaces = w.workspaces ?? [];
    }
    phase = "ready";
  }

  $effect(() => {
    if (!browser || !orgId) return;
    setActiveOrg(orgId);
    if (session.phase !== "authed") {
      void loadHostedSession();
    }
    void loadAll();
  });

  function planBadgeClasses(planTier) {
    const t = String(planTier ?? "starter").toLowerCase();
    if (t === "enterprise") return "text-fuchsia-400 bg-fuchsia-500/10";
    if (t === "scale") return "text-indigo-400 bg-indigo-500/10";
    if (t === "team") return "text-emerald-400 bg-emerald-500/10";
    return "text-gray-500 bg-gray-200";
  }

  function planLabel(planTier) {
    const t = String(planTier ?? "starter").toLowerCase();
    return t.charAt(0).toUpperCase() + t.slice(1);
  }

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
</script>

<svelte:head>
  <title>{organization?.display_name ?? "Organization"} — ANX</title>
</svelte:head>

<div class="space-y-5">
  <div class="flex flex-wrap items-end justify-between gap-3">
    <div class="flex min-w-0 items-center gap-3">
      {#if organization}
        <Avatar
          label={organization.display_name || organization.slug}
          seed={organization.id || organization.slug}
          size="lg"
        />
      {/if}
      <div class="min-w-0">
        <p class="text-[11px] text-gray-500">
          <a
            class="text-gray-500 underline-offset-2 transition-colors hover:text-gray-800 hover:underline"
            href="/hosted/organizations">Organizations</a
          >
        </p>
        <h1
          class="mt-1 flex items-center gap-2 text-lg font-semibold text-gray-900"
        >
          <span class="truncate"
            >{organization?.display_name ||
              organization?.slug ||
              "Organization"}</span
          >
          {#if organization}
            <span
              class="rounded px-1.5 py-0.5 text-[11px] font-medium {planBadgeClasses(
                organization.plan_tier,
              )}"
            >
              {planLabel(organization.plan_tier)}
            </span>
          {/if}
        </h1>
      </div>
    </div>
    <div class="flex flex-wrap items-center gap-2">
      <a
        href={`/hosted/organizations/${encodeURIComponent(orgId)}/billing`}
        class="rounded-md border border-gray-200 bg-gray-100 px-3 py-1.5 text-[12px] font-medium text-gray-600 transition-colors hover:bg-gray-200"
        >Manage billing</a
      >
      <a
        href="/hosted/workspaces/new"
        class="rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white transition-colors hover:bg-indigo-500"
        >+ New workspace</a
      >
    </div>
  </div>

  {#if message}
    <div
      role="alert"
      class="rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
    >
      {message}
    </div>
  {/if}

  {#if phase === "loading"}
    <div
      class="rounded-md border border-gray-200 bg-gray-100 px-4 py-6 text-[13px] text-gray-500"
    >
      Loading…
    </div>
  {:else if usage}
    {@const plan = usage.plan ?? {}}
    {@const u = usage.usage ?? {}}
    <section class="grid gap-3 sm:grid-cols-3">
      {#each [{ label: "Workspaces", used: u.workspace_count, total: plan.workspace_limit }, { label: "Artifacts", used: u.artifact_count, total: plan.artifact_capacity }, { label: "Storage", used: u.storage_gb, total: plan.included_storage_gb, suffix: " GB" }] as metric}
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
        </div>
      {/each}
    </section>

    <section class="rounded-md border border-gray-200 bg-gray-100">
      <div
        class="flex items-center justify-between border-b border-gray-200 px-4 py-2.5"
      >
        <h2 class="text-[13px] font-medium text-gray-900">Workspaces</h2>
        <a
          href="/hosted/dashboard"
          class="text-[11px] font-medium text-gray-500 transition-colors hover:text-gray-800"
          >View all →</a
        >
      </div>
      {#if workspaces.length === 0}
        <div class="px-4 py-6 text-center">
          <p class="text-[12px] text-gray-500">
            No workspaces in this organization yet.
          </p>
          <a
            href="/hosted/workspaces/new"
            class="mt-3 inline-flex rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white transition-colors hover:bg-indigo-500"
            >Create your first workspace</a
          >
        </div>
      {:else}
        <ul class="divide-y divide-gray-200">
          {#each workspaces as ws (ws.id)}
            <li class="flex items-center justify-between gap-3 px-4 py-2.5">
              <div class="flex min-w-0 items-center gap-2.5">
                <Avatar
                  label={ws.display_name || ws.slug}
                  seed={ws.id || ws.slug}
                  size="sm"
                />
                <div class="min-w-0">
                  <div class="truncate text-[13px] font-medium text-gray-900">
                    {ws.display_name || ws.slug}
                  </div>
                  <div class="truncate text-[11px] text-gray-500">
                    {ws.slug}
                  </div>
                </div>
              </div>
              <span
                class="shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium {ws.status ===
                'ready'
                  ? 'text-emerald-400 bg-emerald-500/10'
                  : ws.status === 'provisioning'
                    ? 'text-amber-400 bg-amber-500/10'
                    : 'text-gray-500 bg-gray-200'}"
              >
                {ws.status || "unknown"}
              </span>
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  {/if}
</div>
