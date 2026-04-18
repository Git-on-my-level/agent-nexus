<script>
  import { onMount } from "svelte";

  import { browser } from "$app/environment";

  import Avatar from "$lib/hosted/Avatar.svelte";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";
  import { normalizeHostedLaunchFinishURL } from "$lib/hosted/launchFlow.js";
  import { hostedSession, loadHostedSession } from "$lib/hosted/session.js";

  /** @type {any[]} */
  let workspaces = $state([]);
  let loadingWorkspaces = $state(true);
  let message = $state("");
  let launchingWorkspaceId = $state("");

  const session = $derived($hostedSession);
  const orgs = $derived(session.organizations);
  const activeOrg = $derived(
    orgs.find((o) => String(o.id) === session.activeOrgId) ?? null,
  );

  async function readError(res) {
    try {
      const j = await res.json();
      return j?.error?.message || j?.error?.code || res.statusText;
    } catch {
      return res.statusText;
    }
  }

  async function loadWorkspaces(orgId) {
    if (!orgId) {
      workspaces = [];
      loadingWorkspaces = false;
      return;
    }
    loadingWorkspaces = true;
    try {
      const res = await hostedCpFetch(
        `workspaces?organization_id=${encodeURIComponent(orgId)}&limit=100`,
      );
      if (!res.ok) {
        message = await readError(res);
        workspaces = [];
        return;
      }
      const body = await res.json();
      workspaces = body.workspaces ?? [];
    } finally {
      loadingWorkspaces = false;
    }
  }

  onMount(async () => {
    if (!browser) return;
    if (session.phase !== "authed") {
      await loadHostedSession();
    }
  });

  // React to active-org switches.
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
        message = await readError(res);
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
      <a
        href="/hosted/workspaces/new"
        class="rounded-md bg-accent px-3 py-1.5 text-body font-semibold text-white transition-colors hover:bg-accent-hover"
      >
        + New workspace
      </a>
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
    <div
      class="rounded-md border border-line bg-bg-soft px-4 py-6 text-meta text-fg-subtle"
    >
      Loading…
    </div>
  {:else if orgs.length === 0}
    <div
      class="rounded-md border border-line bg-bg-soft px-6 py-8 text-center"
    >
      <h2 class="text-subtitle text-fg">
        Create your first organization
      </h2>
      <p class="mx-auto mt-1.5 max-w-md text-meta text-fg-subtle">
        Organizations group workspaces, members, and billing. Most teams need
        just one.
      </p>
      <a
        href="/hosted/organizations/new"
        class="mt-4 inline-flex rounded-md bg-accent px-3 py-1.5 text-body font-semibold text-white transition-colors hover:bg-accent-hover"
      >
        Create organization
      </a>
    </div>
  {:else if loadingWorkspaces}
    <div
      class="rounded-md border border-line bg-bg-soft px-4 py-6 text-meta text-fg-subtle"
    >
      Loading workspaces…
    </div>
  {:else if workspaces.length === 0}
    <div
      class="rounded-md border border-line bg-bg-soft px-6 py-8 text-center"
    >
      <h2 class="text-subtitle text-fg">
        Spin up your first workspace
      </h2>
      <p class="mx-auto mt-1.5 max-w-md text-meta text-fg-subtle">
        Workspaces hold the threads, topics, and artifacts your AI agent
        produces. Create one to get started.
      </p>
      <a
        href="/hosted/workspaces/new"
        class="mt-4 inline-flex rounded-md bg-accent px-3 py-1.5 text-body font-semibold text-white transition-colors hover:bg-accent-hover"
      >
        Create workspace
      </a>
    </div>
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
              <button
                type="button"
                class="rounded-md bg-panel-hover px-2.5 py-1.5 text-micro text-fg transition-colors hover:bg-line-strong disabled:opacity-60"
                onclick={() => openWorkspaceLaunch(ws)}
                disabled={launchingWorkspaceId === ws.id}
              >
                {launchingWorkspaceId === ws.id ? "Opening…" : "Open"}
              </button>
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
