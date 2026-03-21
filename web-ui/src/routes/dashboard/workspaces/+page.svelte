<script>
  import { goto } from "$app/navigation";
  import { onMount } from "svelte";

  import {
    controlAuthenticated,
    logoutControlSession,
  } from "$lib/controlSession.js";
  import { controlClient } from "$lib/controlClient.js";

  let organizations = $state([]);
  let loading = $state(true);
  let creating = $state(false);
  let error = $state("");
  let newSlug = $state("");
  let newDisplayName = $state("");
  let newRegion = $state("us-east-1");
  let selectedOrgId = $state("");

  onMount(async () => {
    if (!$controlAuthenticated) {
      goto("/auth");
      return;
    }

    await loadOrganizations();
  });

  async function loadOrganizations() {
    loading = true;
    error = "";

    try {
      const result = await controlClient.listOrganizations();
      organizations = result.organizations ?? [];

      if (organizations.length > 0 && !selectedOrgId) {
        selectedOrgId = organizations[0].id;
      }
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load organizations";
    } finally {
      loading = false;
    }
  }

  async function handleCreateWorkspace() {
    if (!newSlug.trim() || !newDisplayName.trim() || !selectedOrgId) {
      error = "All fields are required.";
      return;
    }

    creating = true;
    error = "";

    try {
      await controlClient.createWorkspace({
        slug: newSlug.trim(),
        display_name: newDisplayName.trim(),
        organization_id: selectedOrgId,
        region: newRegion,
        workspace_tier: "standard",
      });

      goto("/dashboard");
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to create workspace";
    } finally {
      creating = false;
    }
  }

  async function handleLogout() {
    await logoutControlSession({});
    goto("/auth");
  }
</script>

<svelte:head>
  <title>Create Workspace - OAR Control</title>
</svelte:head>

<main class="min-h-screen bg-[var(--ui-bg)] px-4 py-10 text-[var(--ui-text)]">
  <div class="mx-auto max-w-2xl">
    <header class="mb-6">
      <div class="flex items-center justify-between">
        <div>
          <h1 class="text-2xl font-bold text-[var(--ui-text)]">
            Create Workspace
          </h1>
          <p class="text-[var(--ui-text-muted)]">
            Create a new workspace for your organization
          </p>
        </div>
        <button
          class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[12px] font-medium text-[var(--ui-text)] hover:bg-[var(--ui-border-subtle)]"
          onclick={handleLogout}
          type="button"
        >
          Sign out
        </button>
      </div>
    </header>

    {#if loading}
      <div class="py-10 text-center text-[var(--ui-text-muted)]">
        Loading...
      </div>
    {:else if error}
      <div class="rounded-md bg-red-500/10 px-4 py-3 text-sm text-red-400">
        {error}
      </div>
    {:else if organizations.length === 0}
      <div
        class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-4 py-8"
      >
        <p class="text-center text-[var(--ui-text-muted)]">
          No organizations available. Create an organization first.
        </p>
        <div class="mt-4 text-center">
          <a
            class="rounded-md bg-indigo-600 px-4 py-2 text-[13px] font-medium text-white hover:bg-indigo-500"
            href="/dashboard"
          >
            Go to Dashboard
          </a>
        </div>
      </div>
    {:else}
      <form
        class="space-y-4"
        onsubmit={(e) => {
          e.preventDefault();
          handleCreateWorkspace();
        }}
      >
        <div>
          <label
            class="block text-[12px] font-medium text-[var(--ui-text-muted)]"
            for="org-select"
          >
            Organization
          </label>
          <select
            bind:value={selectedOrgId}
            class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
            id="org-select"
          >
            {#each organizations as org}
              <option value={org.id}>
                {org.display_name} ({org.slug})
              </option>
            {/each}
          </select>
        </div>

        <div>
          <label
            class="block text-[12px] font-medium text-[var(--ui-text-muted)]"
            for="workspace-slug"
          >
            Slug
          </label>
          <input
            bind:value={newSlug}
            class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
            id="workspace-slug"
            placeholder="my-workspace"
            type="text"
          />
        </div>

        <div>
          <label
            class="block text-[12px] font-medium text-[var(--ui-text-muted)]"
            for="workspace-display-name"
          >
            Display name
          </label>
          <input
            bind:value={newDisplayName}
            class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
            id="workspace-display-name"
            placeholder="My Workspace"
            type="text"
          />
        </div>

        <div>
          <label
            class="block text-[12px] font-medium text-[var(--ui-text-muted)]"
            for="workspace-region"
          >
            Region
          </label>
          <select
            bind:value={newRegion}
            class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
            id="workspace-region"
          >
            <option value="us-east-1">US East (N. Virginia)</option>
            <option value="us-west-2">US West (Oregon)</option>
            <option value="eu-west-1">EU (Ireland)</option>
            <option value="ap-southeast-1">Asia Pacific (Singapore)</option>
          </select>
        </div>

        <div class="flex gap-3">
          <button
            class="flex-1 rounded-md bg-indigo-600 px-4 py-2 text-[13px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
            disabled={creating}
            type="submit"
          >
            {creating ? "Creating..." : "Create Workspace"}
          </button>
          <a
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-4 py-2 text-[13px] font-medium text-[var(--ui-text)] hover:bg-[var(--ui-border-subtle)]"
            href="/dashboard"
          >
            Cancel
          </a>
        </div>
      </form>
    {/if}
  </div>
</main>
