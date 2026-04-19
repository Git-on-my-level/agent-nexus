<script>
  import { onMount } from "svelte";

  import { browser } from "$app/environment";

  import Button from "$lib/components/Button.svelte";
  import SkeletonCard from "$lib/components/state/SkeletonCard.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import Avatar from "$lib/hosted/Avatar.svelte";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";
  import {
    classifiedCpFetch,
    errorUserMessage,
    isAuthError,
  } from "$lib/hosted/fetchState.js";
  import { normalizeHostedLaunchFinishURL } from "$lib/hosted/launchFlow.js";
  import { hostedSession, loadHostedSession } from "$lib/hosted/session.js";

  /** @type {any[]} */
  let workspaces = $state([]);
  let loadingWorkspaces = $state(true);
  let wsError = $state("");
  let wsRetrying = $state(false);
  let message = $state("");
  let launchingWorkspaceId = $state("");

  const session = $derived($hostedSession);
  const orgs = $derived(session.organizations);
  const activeOrg = $derived(
    orgs.find((o) => String(o.id) === session.activeOrgId) ?? null,
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
    wsRetrying = false;
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

  async function retryWorkspaces() {
    if (!activeOrg?.id) return;
    wsRetrying = true;
    await loadWorkspaces(activeOrg.id);
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

  function statusClasses(status) {
    const s = String(status ?? "").toLowerCase();
    if (s === "ready" || s === "active") {
      return "text-ok-text bg-ok-soft";
    }
    if (s === "provisioning" || s === "pending") {
      return "text-warn-text bg-warn-soft";
    }
    if (s === "failed" || s === "error") {
      return "text-danger-text bg-danger-soft";
    }
    return "text-fg-subtle bg-panel-hover";
  }
</script>

<svelte:head>
  <title>Dashboard — ANX</title>
</svelte:head>

<div class="space-y-6">
  <div class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <h1 class="text-display text-fg">
        {activeOrg
          ? `${activeOrg.display_name || activeOrg.slug} workspaces`
          : "Welcome to ANX"}
      </h1>
      <p class="mt-1 hidden text-meta text-fg-subtle sm:block">
        Workspaces are isolated environments where your AI agents do their work.
        Each one has its own threads, topics, and artifacts.
      </p>
    </div>
    {#if activeOrg}
      <Button variant="primary" href="/hosted/workspaces/new">
        + New workspace
      </Button>
    {/if}
  </div>

  {#if message}
    <p
      role="alert"
      class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
    >
      {message}
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
  {:else if loadingWorkspaces && !wsError}
    <SkeletonCard />
  {:else if wsError}
    <StateError
      message={wsError}
      onretry={retryWorkspaces}
      retrying={wsRetrying}
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
        <article
          class="flex flex-col rounded-md border border-line bg-bg-soft px-4 py-3"
        >
          <div class="flex items-start justify-between gap-2">
            <div class="flex min-w-0 items-start gap-2.5">
              <Avatar
                label={ws.display_name || ws.slug}
                seed={ws.id || ws.slug}
                size="md"
              />
              <div class="min-w-0">
                <h2 class="truncate text-subtitle text-fg">
                  {ws.display_name || ws.slug}
                </h2>
                <p class="mt-0.5 truncate font-mono text-mono text-fg-subtle">
                  {ws.slug}
                </p>
              </div>
            </div>
            <span
              class="shrink-0 rounded px-1.5 py-0.5 text-micro {statusClasses(
                ws.status,
              )}"
            >
              {ws.status || "unknown"}
            </span>
          </div>

          <div class="mt-4 flex items-center gap-2">
            {#if String(ws.status ?? "").toLowerCase() === "ready" && ws.slug}
              <Button
                type="button"
                variant="ghost"
                onclick={() => openWorkspaceLaunch(ws)}
                disabled={launchingWorkspaceId === ws.id}
              >
                {launchingWorkspaceId === ws.id ? "Opening…" : "Open"}
              </Button>
            {:else}
              <span
                class="rounded-md border border-line px-2.5 py-1.5 text-micro text-fg-subtle"
              >
                {String(ws.status ?? "").toLowerCase() === "provisioning"
                  ? "Setting up…"
                  : "Not ready"}
              </span>
            {/if}
          </div>
        </article>
      {/each}
    </div>
  {/if}
</div>
