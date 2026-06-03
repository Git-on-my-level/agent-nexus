<script>
  import { onMount } from "svelte";

  import { page } from "$app/stores";

  import Button from "$lib/components/Button.svelte";
  import HostedNarrowLayout from "$lib/components/layout/HostedNarrowLayout.svelte";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";
  import { readHostedOAuthError } from "$lib/hosted/oauthFlow.js";
  import { hostedSession, loadHostedSession } from "$lib/hosted/session.js";

  const oauthFields = [
    "client_id",
    "redirect_uri",
    "response_type",
    "state",
    "code_challenge",
    "code_challenge_method",
    "scope",
    "resource",
  ];

  let loading = $state(true);
  let submitting = $state(false);
  let message = $state("");
  let organizations = $state([]);
  let workspaces = $state([]);
  let workspaceID = $state("");
  let visibleName = $state("");

  const session = $derived($hostedSession);
  const authorizeQuery = $derived($page.url.search);

  onMount(async () => {
    await hydrate();
  });

  async function hydrate() {
    loading = true;
    message = "";
    try {
      const loaded = await loadHostedSession();
      if (loaded.phase !== "authed") {
        const next = `/hosted/mcp/authorize${authorizeQuery}`;
        window.location.assign(
          `/hosted/signin?next=${encodeURIComponent(next)}`,
        );
        return;
      }
      organizations = loaded.organizations ?? [];
      await loadWorkspaces();
    } catch (error) {
      message =
        error instanceof Error ? error.message : "Could not load workspaces.";
    } finally {
      loading = false;
    }
  }

  async function loadWorkspaces() {
    const rows = [];
    for (const org of organizations) {
      const organizationId = String(org?.id ?? "").trim();
      if (!organizationId) continue;
      const res = await hostedCpFetch(
        `workspaces?organization_id=${encodeURIComponent(organizationId)}&limit=200`,
      );
      if (!res.ok) {
        throw new Error(await readHostedOAuthError(res));
      }
      const body = await res.json();
      for (const workspace of body?.workspaces ?? []) {
        rows.push({
          ...workspace,
          organization_display_name: org.display_name,
          organization_slug: org.slug,
        });
      }
    }
    workspaces = rows.filter((workspace) => workspace.status === "ready");
    if (!workspaceID && workspaces.length > 0) {
      workspaceID = String(workspaces[0].id ?? "");
    }
  }

  function oauthPayload() {
    const payload = {};
    for (const field of oauthFields) {
      payload[field] = String($page.url.searchParams.get(field) ?? "").trim();
    }
    payload.workspace_id = workspaceID;
    payload.visible_name = visibleName;
    return payload;
  }

  async function authorize() {
    submitting = true;
    message = "";
    try {
      const res = await hostedCpFetch("mcp/oauth/browser/authorize", {
        method: "POST",
        body: JSON.stringify(oauthPayload()),
      });
      if (!res.ok) {
        throw new Error(await readHostedOAuthError(res));
      }
      const body = await res.json();
      const redirectURL = String(body?.redirect_url ?? "").trim();
      if (!redirectURL) {
        throw new Error("We couldn't finish connecting ChatGPT. Try again.");
      }
      window.location.assign(redirectURL);
    } catch (error) {
      message =
        error instanceof Error ? error.message : "Could not authorize ChatGPT.";
    } finally {
      submitting = false;
    }
  }
</script>

<svelte:head>
  <title>Connect ChatGPT — ANX</title>
</svelte:head>

<HostedNarrowLayout>
  <div class="rounded-md border border-line bg-bg-soft px-6 py-6">
    <h1 class="text-display text-fg">Connect ChatGPT</h1>

    {#if loading || session.phase === "loading" || session.phase === "idle"}
      <p class="mt-4 text-body text-fg-muted">Loading your workspaces…</p>
    {:else if workspaces.length === 0}
      <p class="mt-4 text-body text-fg-muted">
        Create a hosted workspace before connecting ChatGPT.
      </p>
      <Button
        href="/hosted/workspaces/new"
        variant="primary"
        class="mt-5 w-full"
      >
        New workspace
      </Button>
    {:else}
      <form
        class="mt-5 space-y-4"
        onsubmit={(event) => {
          event.preventDefault();
          authorize();
        }}
      >
        <label class="block">
          <span class="text-micro font-medium text-fg">Workspace</span>
          <select
            bind:value={workspaceID}
            class="mt-1 h-9 w-full rounded border border-line bg-panel px-3 text-body text-fg"
          >
            {#each workspaces as workspace}
              <option value={workspace.id}>
                {workspace.organization_display_name} / {workspace.display_name}
              </option>
            {/each}
          </select>
        </label>

        <label class="block">
          <span class="text-micro font-medium text-fg">ChatGPT agent name</span>
          <input
            bind:value={visibleName}
            class="mt-1 h-9 w-full rounded border border-line bg-panel px-3 text-body text-fg"
            autocomplete="off"
            maxlength="120"
            required
            placeholder="chatgpt-researcher"
          />
        </label>

        <Button
          type="submit"
          variant="primary"
          busy={submitting}
          disabled={!workspaceID || !visibleName.trim()}
          class="w-full"
        >
          Connect ChatGPT
        </Button>
      </form>
    {/if}

    {#if message}
      <p
        role="alert"
        class="mt-4 rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
      >
        {message}
      </p>
    {/if}
  </div>
</HostedNarrowLayout>
