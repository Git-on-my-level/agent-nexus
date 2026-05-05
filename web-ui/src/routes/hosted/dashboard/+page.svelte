<script>
  import { onMount } from "svelte";

  import { browser } from "$app/environment";

  import Button from "$lib/components/Button.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import SkeletonCard from "$lib/components/state/SkeletonCard.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import Avatar from "$lib/hosted/Avatar.svelte";
  import StatusPill from "$lib/hosted/StatusPill.svelte";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";
  import {
    classifiedCpFetch,
    errorUserMessage,
    isAuthError,
  } from "$lib/hosted/fetchState.js";
  import { normalizeHostedLaunchFinishURL } from "$lib/hosted/launchFlow.js";
  import { planBadgeClasses, planLabel } from "$lib/hosted/planCatalog.js";
  import { hostedSession, loadHostedSession } from "$lib/hosted/session.js";
  import { pct, storageMetric } from "$lib/hosted/usageStats.js";

  /** @type {any[]} */
  let workspaces = $state([]);
  /** @type {any} */
  let usage = $state(null);
  let loadingWorkspaces = $state(true);
  let loadingUsage = $state(true);
  let wsError = $state("");
  let usageError = $state("");
  let retrying = $state(false);
  let message = $state("");
  let launchingWorkspaceId = $state("");

  const session = $derived($hostedSession);
  const orgs = $derived(session.organizations);
  const activeOrg = $derived(
    orgs.find((o) => String(o.id) === session.activeOrgId) ?? null,
  );
  const billingHref = $derived(
    activeOrg
      ? `/hosted/organizations/${encodeURIComponent(activeOrg.id)}/billing`
      : null,
  );

  async function loadWorkspaces(orgId) {
    if (!orgId) {
      workspaces = [];
      loadingWorkspaces = false;
      wsError = "";
      return;
    }
    loadingWorkspaces = true;
    wsError = "";
    try {
      const res = await classifiedCpFetch(
        `workspaces?organization_id=${encodeURIComponent(orgId)}&limit=100`,
      );
      const body = await res.json();
      workspaces = body.workspaces ?? [];
    } catch (e) {
      if (isAuthError(e)) throw e;
      wsError = errorUserMessage(e);
      workspaces = [];
    } finally {
      loadingWorkspaces = false;
    }
  }

  async function loadUsage(orgId) {
    if (!orgId) {
      usage = null;
      loadingUsage = false;
      usageError = "";
      return;
    }
    loadingUsage = true;
    usageError = "";
    try {
      const res = await classifiedCpFetch(
        `organizations/${encodeURIComponent(orgId)}/usage-summary`,
      );
      const body = await res.json();
      usage = body.summary ?? null;
    } catch (e) {
      if (isAuthError(e)) throw e;
      usageError = errorUserMessage(e);
      usage = null;
    } finally {
      loadingUsage = false;
    }
  }

  async function retry() {
    if (!activeOrg?.id) return;
    retrying = true;
    await Promise.all([loadWorkspaces(activeOrg.id), loadUsage(activeOrg.id)]);
    retrying = false;
  }

  onMount(async () => {
    if (!browser) return;
    if (session.phase !== "authed") {
      await loadHostedSession();
    }
  });

  $effect(() => {
    if (!browser) return;
    if (session.phase !== "authed") return;
    void loadWorkspaces(session.activeOrgId);
    void loadUsage(session.activeOrgId);
  });

  async function openWorkspaceLaunch(workspace) {
    message = "";
    const workspaceID = String(workspace?.id ?? "").trim();
    if (!workspaceID) {
      message = "This workspace cannot be launched.";
      return;
    }
    launchingWorkspaceId = workspaceID;
    try {
      const res = await hostedCpFetch(
        `workspaces/${encodeURIComponent(workspaceID)}/launch-sessions`,
        {
          method: "POST",
          body: JSON.stringify({ return_path: "/" }),
        },
      );
      if (!res.ok) {
        try {
          const j = await res.json();
          message = j?.error?.message || j?.error?.code || res.statusText;
        } catch {
          message = res.statusText;
        }
        return;
      }
      const body = await res.json();
      const finishURL = normalizeHostedLaunchFinishURL(
        body?.launch_session?.finish_url,
      );
      if (!finishURL) {
        message = "Launch response did not include a valid finish URL.";
        return;
      }
      window.location.assign(finishURL);
    } finally {
      launchingWorkspaceId = "";
    }
  }

  function statusHint(status) {
    const s = String(status ?? "").toLowerCase();
    if (s === "provisioning" || s === "pending") return "Setting up…";
    if (s === "failed" || s === "error") return "Setup failed";
    if (s === "degraded") return "Recovering";
    if (s === "suspended") return "Suspended";
    return "";
  }

  function barColor(p) {
    if (p >= 90) return "bg-danger";
    if (p >= 75) return "bg-warn";
    return "bg-accent-solid";
  }
</script>

<svelte:head>
  <title>Dashboard — Agent Nexus (ANX)</title>
</svelte:head>

<div class="space-y-6">
  <!-- Org header -->
  <div class="flex flex-wrap items-end justify-between gap-3">
    <div class="flex min-w-0 items-center gap-3">
      {#if activeOrg}
        <Avatar
          label={activeOrg.display_name || activeOrg.slug}
          seed={activeOrg.id || activeOrg.slug}
          size="lg"
        />
      {/if}
      <div class="min-w-0">
        <h1 class="flex items-center gap-2 text-display text-fg">
          <span class="truncate">
            {activeOrg
              ? activeOrg.display_name || activeOrg.slug
              : "Welcome to Agent Nexus"}
          </span>
          {#if activeOrg}
            <span
              class="rounded px-1.5 py-0.5 text-micro {planBadgeClasses(
                activeOrg.plan_tier,
              )}"
            >
              {planLabel(activeOrg.plan_tier)}
            </span>
          {/if}
        </h1>
        {#if activeOrg}
          <p class="mt-0.5 truncate font-mono text-micro text-fg-subtle">
            {activeOrg.slug}
          </p>
        {/if}
      </div>
    </div>
    {#if activeOrg}
      <div class="flex flex-wrap items-center gap-2">
        {#if billingHref}
          <Button variant="secondary" href={billingHref}>Manage billing</Button>
        {/if}
        {#if !loadingWorkspaces && !wsError && workspaces.length > 0}
          <Button variant="primary" href="/hosted/workspaces/new"
            >+ New workspace</Button
          >
        {/if}
      </div>
    {/if}
  </div>

  {#if message}
    <p
      role="alert"
      class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
    >
      {message} If this persists, use the Support link in the footer or retry after
      a few minutes.
    </p>
  {/if}

  {#if session.phase === "loading" || session.phase === "idle"}
    <SkeletonCard />
  {:else if orgs.length === 0}
    <StateEmpty
      title="Create your first organization"
      helper="Organizations group workspaces, members, and billing. Most teams need just one."
      actionLabel="Create organization"
      actionHref="/hosted/organizations/new"
    />
  {:else}
    <!-- Usage strip -->
    {#if loadingUsage && !usageError}
      <div class="grid gap-3 sm:grid-cols-3">
        {#each [0, 1, 2] as i (i)}
          <div class="rounded-md border border-line bg-bg-soft px-4 py-3">
            <Skeleton rows={3} />
          </div>
        {/each}
      </div>
    {:else if usageError}
      <StateError
        title="Usage didn't load"
        message={usageError}
        onretry={retry}
        {retrying}
        supportHint={true}
      />
    {:else if usage}
      {@const plan = usage.plan ?? {}}
      {@const u = usage.usage ?? {}}
      <section class="grid gap-3 sm:grid-cols-3">
        {#each [{ label: "Workspaces", used: u.workspace_count, total: plan.workspace_limit }, { label: "Artifacts", used: u.artifact_count, total: plan.artifact_capacity }, storageMetric(u, plan)] as metric}
          {@const p = pct(metric.used, metric.total)}
          {@const usedText = metric.displayUsed ?? Number(metric.used ?? 0)}
          {@const totalText = metric.displayTotal ?? metric.total ?? "—"}
          <div class="rounded-md border border-line bg-bg-soft px-4 py-3">
            <div
              class="flex items-center justify-between text-micro uppercase tracking-wide text-fg-subtle"
            >
              <span>{metric.label}</span>
              <span class="tabular-nums">{p}%</span>
            </div>
            <div class="mt-2 text-subtitle tabular-nums text-fg">
              {usedText}<span class="text-meta text-fg-subtle"
                >{" / "}{totalText}</span
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
    {/if}

    <!-- Workspaces -->
    <section class="space-y-3">
      <div class="flex items-baseline justify-between">
        <h2 class="text-subtitle text-fg">Workspaces</h2>
        {#if !loadingWorkspaces && workspaces.length > 0}
          <span class="text-micro text-fg-subtle tabular-nums"
            >{workspaces.length} total</span
          >
        {/if}
      </div>

      {#if loadingWorkspaces && !wsError}
        <SkeletonCard />
      {:else if wsError}
        <StateError
          message={wsError}
          onretry={retry}
          {retrying}
          supportHint={true}
        />
      {:else if workspaces.length === 0}
        <StateEmpty
          title="Spin up your first workspace"
          helper="Workspaces hold the threads, topics, and artifacts your AI agent produces. Create one to get started."
          actionLabel="Create workspace"
          actionHref="/hosted/workspaces/new"
        />
      {:else}
        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {#each workspaces as ws (ws.id)}
            {@const isReady =
              String(ws.status ?? "").toLowerCase() === "ready" && ws.slug}
            {@const hint = statusHint(ws.status)}
            <article
              class="flex flex-col rounded-md border border-line bg-bg-soft px-4 py-3"
            >
              <div class="flex items-start justify-between gap-2">
                <div class="flex min-w-0 items-start gap-2.5">
                  <Avatar
                    label={ws.display_name || ws.slug}
                    seed={ws.id || ws.slug}
                    size="sm"
                  />
                  <div class="min-w-0">
                    <h3 class="truncate text-subtitle text-fg">
                      {ws.display_name || ws.slug}
                    </h3>
                    <p
                      class="mt-0.5 truncate font-mono text-mono text-fg-subtle"
                    >
                      {ws.slug}
                    </p>
                  </div>
                </div>
                <StatusPill status={ws.status} />
              </div>

              <div class="mt-4 flex items-center justify-between gap-2">
                {#if isReady}
                  <Button
                    type="button"
                    variant="ghost"
                    onclick={() => openWorkspaceLaunch(ws)}
                    disabled={launchingWorkspaceId === ws.id}
                  >
                    {launchingWorkspaceId === ws.id ? "Opening…" : "Open"}
                  </Button>
                {:else if hint}
                  <span class="text-micro text-fg-subtle">{hint}</span>
                {:else}
                  <span></span>
                {/if}
              </div>
            </article>
          {/each}
        </div>
      {/if}
    </section>
  {/if}
</div>
