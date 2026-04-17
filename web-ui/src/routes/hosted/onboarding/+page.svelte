<script>
  import { onMount } from "svelte";

  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";

  import { PUBLIC_OAR_SAAS_DEV_SERVICE_PUBLIC_KEY } from "$env/static/public";
  import {
    clearHostedCpAccessToken,
    hostedCpFetch,
  } from "$lib/hosted/cpFetch.js";
  import { normalizeHostedLaunchFinishURL } from "$lib/hosted/launchFlow.js";

  let organizations = $state(/** @type {any[]} */ ([]));
  let workspaces = $state(/** @type {any[]} */ ([]));
  let selectedOrgId = $state("");
  let orgSlug = $state("");
  let orgName = $state("");
  let wsSlug = $state("");
  let wsName = $state("");
  let serviceId = $state("dev-local-1");
  let servicePublicKey = $state(
    String(PUBLIC_OAR_SAAS_DEV_SERVICE_PUBLIC_KEY ?? "").trim(),
  );
  let busy = $state(false);
  let message = $state("");
  let phase = $state("loading");
  let launchingWorkspaceId = $state("");

  async function readError(res) {
    try {
      const j = await res.json();
      return j?.error?.message || j?.error?.code || res.statusText;
    } catch {
      return res.statusText;
    }
  }

  async function loadOrganizations() {
    const res = await hostedCpFetch("organizations?limit=50");
    if (res.status === 401) {
      phase = "auth";
      return;
    }
    if (!res.ok) {
      message = await readError(res);
      phase = "ready";
      return;
    }
    const body = await res.json();
    organizations = body.organizations ?? [];
    if (organizations.length > 0 && !selectedOrgId) {
      selectedOrgId = String(organizations[0].id ?? "");
    }
    phase = "ready";
  }

  async function loadWorkspaces() {
    if (!selectedOrgId) {
      workspaces = [];
      return;
    }
    const res = await hostedCpFetch(
      `workspaces?organization_id=${encodeURIComponent(selectedOrgId)}&limit=50`,
    );
    if (!res.ok) {
      return;
    }
    const body = await res.json();
    workspaces = body.workspaces ?? [];
  }

  onMount(async () => {
    await loadOrganizations();
    await loadWorkspaces();
  });

  $effect(() => {
    if (!browser || !selectedOrgId) {
      return;
    }
    loadWorkspaces();
  });

  async function createOrg() {
    message = "";
    if (!orgSlug.trim() || !orgName.trim()) {
      message = "Organization slug and name are required.";
      return;
    }
    busy = true;
    try {
      const res = await hostedCpFetch("organizations", {
        method: "POST",
        body: JSON.stringify({
          slug: orgSlug.trim(),
          display_name: orgName.trim(),
          plan_tier: "starter",
        }),
      });
      if (!res.ok) {
        message = await readError(res);
        return;
      }
      const body = await res.json();
      const org = body.organization;
      if (org?.id) {
        organizations = [...organizations, org];
        selectedOrgId = String(org.id);
      }
      orgSlug = "";
      orgName = "";
      await loadWorkspaces();
    } finally {
      busy = false;
    }
  }

  async function createWorkspace() {
    message = "";
    if (!selectedOrgId) {
      message = "Select or create an organization first.";
      return;
    }
    if (!wsSlug.trim() || !wsName.trim()) {
      message = "Workspace slug and name are required.";
      return;
    }
    const pub = servicePublicKey.trim();
    if (!pub) {
      message =
        "Service identity public key is missing. Set PUBLIC_OAR_SAAS_DEV_SERVICE_PUBLIC_KEY or paste the key from your serve-control-plane output.";
      return;
    }
    const sid = serviceId.trim() || `dev-${wsSlug.trim()}`;
    busy = true;
    try {
      const res = await hostedCpFetch("workspaces", {
        method: "POST",
        body: JSON.stringify({
          organization_id: selectedOrgId,
          slug: wsSlug.trim(),
          display_name: wsName.trim(),
          service_identity_id: sid,
          service_identity_public_key: pub,
        }),
      });
      if (!res.ok) {
        message = await readError(res);
        return;
      }
      wsSlug = "";
      wsName = "";
      await loadWorkspaces();
    } finally {
      busy = false;
    }
  }

  async function signOut() {
    await hostedCpFetch("account/sessions/current", { method: "DELETE" });
    clearHostedCpAccessToken();
    await goto("/hosted/start");
  }

  async function openWorkspaceLaunch(workspace) {
    message = "";
    const workspaceID = String(workspace?.id ?? "").trim();
    if (!workspaceID) {
      message = "This workspace is missing an id and cannot be launched.";
      return;
    }

    launchingWorkspaceId = workspaceID;
    try {
      const launchResponse = await hostedCpFetch(
        `workspaces/${encodeURIComponent(workspaceID)}/launch-sessions`,
        {
          method: "POST",
          body: JSON.stringify({
            return_path: "/",
          }),
        },
      );
      if (!launchResponse.ok) {
        message = await readError(launchResponse);
        return;
      }
      const launchPayload = await launchResponse.json();
      const finishURL = normalizeHostedLaunchFinishURL(
        launchPayload?.launch_session?.finish_url,
      );
      if (!finishURL) {
        message = "Launch session response did not include a valid finish URL.";
        return;
      }
      if (typeof window !== "undefined") {
        window.location.assign(finishURL);
        return;
      }
      await goto(finishURL);
    } finally {
      launchingWorkspaceId = "";
    }
  }
</script>

<div class="hosted-page">
  <h1 class="hosted-title">Your workspace</h1>

  {#if phase === "loading"}
    <p class="hosted-sub">Loading…</p>
  {:else if phase === "auth"}
    <p class="hosted-sub">You need an account to continue.</p>
    <p>
      <a class="hosted-link" href="/hosted/signup">Create account</a>
      or
      <a class="hosted-link" href="/hosted/signin">Sign in</a>
    </p>
  {:else}
    {#if message}
      <p class="hosted-error" role="alert">{message}</p>
    {/if}

    <section class="hosted-card">
      <h2>Organization</h2>
      {#if organizations.length > 0}
        <label class="hosted-field">
          Active organization
          <select
            class="hosted-input"
            bind:value={selectedOrgId}
            disabled={busy}
          >
            {#each organizations as org}
              <option value={org.id}>{org.display_name} ({org.slug})</option>
            {/each}
          </select>
        </label>
      {/if}
      <p class="hosted-hint">
        Create an organization if you don’t have one yet.
      </p>
      <div class="hosted-row">
        <label class="hosted-field">
          New org slug
          <input
            class="hosted-input"
            bind:value={orgSlug}
            disabled={busy}
            placeholder="acme-corp"
          />
        </label>
        <label class="hosted-field">
          Display name
          <input
            class="hosted-input"
            bind:value={orgName}
            disabled={busy}
            placeholder="Acme Corp"
          />
        </label>
      </div>
      <button
        class="hosted-btn-submit"
        type="button"
        onclick={() => createOrg()}
        disabled={busy}
      >
        Create organization
      </button>
    </section>

    <section class="hosted-card">
      <h2>Workspace</h2>
      <p class="hosted-hint">
        Uses the dev service identity public key from your local stack (same as
        the
        <code>make serve</code> banner). Each workspace needs a unique service identity
        id.
      </p>
      <div class="hosted-row">
        <label class="hosted-field">
          Slug
          <input
            class="hosted-input"
            bind:value={wsSlug}
            disabled={busy}
            placeholder="demo"
          />
        </label>
        <label class="hosted-field">
          Display name
          <input
            class="hosted-input"
            bind:value={wsName}
            disabled={busy}
            placeholder="Demo"
          />
        </label>
      </div>
      <label class="hosted-field">
        Service identity id
        <input class="hosted-input" bind:value={serviceId} disabled={busy} />
      </label>
      <label class="hosted-field">
        Service identity public key (base64)
        <textarea
          class="hosted-input"
          bind:value={servicePublicKey}
          disabled={busy}
          rows="3"
        ></textarea>
      </label>
      <button
        class="hosted-btn-submit"
        type="button"
        onclick={() => createWorkspace()}
        disabled={busy}
      >
        Create workspace
      </button>
    </section>

    {#if workspaces.length > 0}
      <section class="hosted-card">
        <h2>Your workspaces</h2>
        <ul class="hosted-ws-list">
          {#each workspaces as ws}
            <li>
              <strong>{ws.display_name}</strong>
              <span class="hosted-muted">({ws.slug})</span>
              —
              <span class="hosted-status">{ws.status}</span>
              {#if ws.status === "ready" && ws.slug}
                <button
                  type="button"
                  class="hosted-linkish-btn"
                  onclick={() => openWorkspaceLaunch(ws)}
                  disabled={launchingWorkspaceId === ws.id}
                >
                  {launchingWorkspaceId === ws.id ? "Opening…" : "Open in OAR"}
                </button>
              {/if}
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    <p class="hosted-foot">
      <button
        type="button"
        class="hosted-linkish-btn"
        onclick={() => signOut()}
      >
        Sign out
      </button>
    </p>
  {/if}
</div>
