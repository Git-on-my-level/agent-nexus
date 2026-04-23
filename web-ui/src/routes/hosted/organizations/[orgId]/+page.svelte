<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import Button from "$lib/components/Button.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import Avatar from "$lib/hosted/Avatar.svelte";
  import {
    classifiedCpFetch,
    errorUserMessage,
    isAuthError,
  } from "$lib/hosted/fetchState.js";
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
  let orgError = $state("");
  let usageError = $state("");
  let wsError = $state("");
  let retrying = $state(false);

  async function loadAll() {
    phase = "loading";
    orgError = "";
    usageError = "";
    wsError = "";
    retrying = false;
    organization = null;
    usage = null;
    workspaces = [];

    try {
      const orgRes = await classifiedCpFetch(
        `organizations/${encodeURIComponent(orgId)}`,
      );
      const orgBody = await orgRes.json();
      organization = orgBody?.organization ?? null;
    } catch (e) {
      if (isAuthError(e)) {
        await goto("/hosted/start");
        return;
      }
      orgError = errorUserMessage(e);
      phase = "ready";
      return;
    }

    const [usageRes, wsRes] = await Promise.all([
      classifiedCpFetch(
        `organizations/${encodeURIComponent(orgId)}/usage-summary`,
      ).catch((e) => e),
      classifiedCpFetch(
        `workspaces?organization_id=${encodeURIComponent(orgId)}&limit=20`,
      ).catch((e) => e),
    ]);

    if (usageRes instanceof Response) {
      try {
        const u = await usageRes.json();
        usage = u.summary ?? null;
      } catch {
        /* ignore parse errors */
      }
    } else {
      usageError = errorUserMessage(usageRes);
    }

    if (wsRes instanceof Response) {
      try {
        const w = await wsRes.json();
        workspaces = w.workspaces ?? [];
      } catch {
        /* ignore parse errors */
      }
    } else {
      wsError = errorUserMessage(wsRes);
    }

    phase = "ready";
  }

  async function retry() {
    retrying = true;
    await loadAll();
  }

  let lastLoadedKey = $state("");

  $effect(() => {
    if (!browser || !orgId) return;
    setActiveOrg(orgId);

    if (session.phase === "idle" || session.phase === "unauthed") {
      lastLoadedKey = "";
      void loadHostedSession();
      return;
    }

    // Hosted session failed (e.g. organizations list error). Do not leave local
    // `phase === "loading"` forever — surface CP error and stop the skeleton.
    if (session.phase === "error") {
      orgError = session.error || "Could not load your session.";
      usageError = "";
      wsError = "";
      phase = "ready";
      organization = null;
      usage = null;
      workspaces = [];
      return;
    }

    // Still hydrating hosted session — keep skeleton until authed or terminal state above.
    if (session.phase === "loading") {
      return;
    }

    if (session.phase !== "authed") {
      return;
    }

    // Authed and we have an orgId. Load workspace data once per (orgId, account)
    // pair; we don't want this effect to re-fire `loadAll` every time
    // `setActiveOrg` mutates the store.
    const key = `${session.account?.id ?? ""}::${orgId}`;
    if (lastLoadedKey === key) return;
    lastLoadedKey = key;
    void loadAll();
  });

  function planBadgeClasses(planTier) {
    const t = String(planTier ?? "starter").toLowerCase();
    if (t === "enterprise") return "text-fuchsia-400 bg-fuchsia-500/10";
    if (t === "scale") return "text-accent-text bg-accent-soft";
    if (t === "team") return "text-ok-text bg-ok-soft";
    return "text-fg-subtle bg-panel-hover";
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
    if (p >= 90) return "bg-danger";
    if (p >= 75) return "bg-warn";
    return "bg-accent-solid";
  }
</script>

<svelte:head>
  <title>{organization?.display_name ?? "Organization"} · Agent Nexus</title>
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
        <p class="text-micro text-fg-subtle">
          <a
            class="text-fg-subtle underline-offset-2 transition-colors hover:text-fg hover:underline"
            href="/hosted/organizations">Organizations</a
          >
        </p>
        <h1 class="mt-1 flex items-center gap-2 text-display text-fg">
          <span class="truncate"
            >{organization?.display_name ||
              organization?.slug ||
              "Organization"}</span
          >
          {#if organization}
            <span
              class="rounded px-1.5 py-0.5 text-micro {planBadgeClasses(
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
      <Button
        variant="secondary"
        href={`/hosted/organizations/${encodeURIComponent(orgId)}/billing`}
        >Manage billing</Button
      >
      <Button variant="primary" href="/hosted/workspaces/new"
        >+ New workspace</Button
      >
    </div>
  </div>

  {#if orgError}
    <StateError message={orgError} onretry={retry} {retrying} />
  {/if}

  {#if phase === "loading"}
    <div class="space-y-3">
      <div class="grid gap-3 sm:grid-cols-3">
        {#each [0, 1, 2] as i (i)}
          <div class="rounded-md border border-line bg-bg-soft px-4 py-3">
            <Skeleton rows={3} />
          </div>
        {/each}
      </div>
      <div class="rounded-md border border-line bg-bg-soft px-4 py-4">
        <Skeleton rows={4} />
      </div>
    </div>
  {:else if usage || wsError || usageError}
    {#if usageError}
      <StateError
        title="Usage didn't load"
        message={usageError}
        onretry={retry}
        {retrying}
      />
    {/if}
    {#if usage}
      {@const plan = usage.plan ?? {}}
      {@const u = usage.usage ?? {}}
      <section class="grid gap-3 sm:grid-cols-3">
        {#each [{ label: "Workspaces", used: u.workspace_count, total: plan.workspace_limit }, { label: "Artifacts (org total)", used: u.artifact_count, total: plan.artifact_capacity }, { label: "Storage (org)", used: u.storage_gb, total: plan.included_storage_gb, suffix: " GB" }] as metric}
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
          </div>
        {/each}
      </section>

      <section class="rounded-md border border-line bg-bg-soft">
        <div
          class="flex items-center justify-between border-b border-line px-4 py-2.5"
        >
          <h2 class="text-subtitle text-fg">Workspaces</h2>
          <a
            href="/hosted/dashboard"
            class="text-micro text-fg-subtle transition-colors hover:text-fg"
            >View all →</a
          >
        </div>
        {#if wsError}
          <div class="px-4 py-3">
            <StateError
              title="Workspaces didn't load"
              message={wsError}
              onretry={retry}
              {retrying}
            />
          </div>
        {:else if workspaces.length === 0}
          <StateEmpty
            title="No workspaces yet"
            helper="Create one to start working."
            actionLabel="Create workspace"
            actionHref="/hosted/workspaces/new"
            class="border-0 bg-transparent"
          />
        {:else}
          <ul class="divide-y divide-line">
            {#each workspaces as ws (ws.id)}
              <li class="flex items-center justify-between gap-3 px-4 py-2.5">
                <div class="flex min-w-0 items-center gap-2.5">
                  <Avatar
                    label={ws.display_name || ws.slug}
                    seed={ws.id || ws.slug}
                    size="sm"
                  />
                  <div class="min-w-0">
                    <div class="truncate text-subtitle text-fg">
                      {ws.display_name || ws.slug}
                    </div>
                    <div class="truncate font-mono text-mono text-fg-subtle">
                      {ws.slug}
                    </div>
                  </div>
                </div>
                <span
                  class="shrink-0 rounded px-1.5 py-0.5 text-micro {ws.status ===
                  'ready'
                    ? 'text-ok-text bg-ok-soft'
                    : ws.status === 'provisioning'
                      ? 'text-warn-text bg-warn-soft'
                      : 'text-fg-subtle bg-panel-hover'}"
                >
                  {ws.status ? ws.status : "—"}
                </span>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}
  {/if}
</div>
