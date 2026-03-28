<script>
  import { goto, invalidateAll } from "$app/navigation";

  import { logoutControlSession } from "$lib/controlSession.js";
  import { controlClient } from "$lib/controlClient.js";

  let { data } = $props();

  let organizations = $derived(data.organizations ?? []);
  let billingByOrganization = $derived(data.billingByOrganization ?? {});
  let workspaces = $derived(data.workspaces ?? []);
  let error = $state("");
  let creatingOrg = $state(false);
  let launchingWorkspaceId = $state("");
  let billingActionOrganizationId = $state("");
  let billingActionMode = $state("");
  let newOrgSlug = $state("");
  let newOrgDisplayName = $state("");

  async function handleCreateOrganization() {
    if (!newOrgSlug.trim() || !newOrgDisplayName.trim()) {
      return;
    }

    creatingOrg = true;
    error = "";

    try {
      await controlClient.createOrganization({
        slug: newOrgSlug.trim(),
        display_name: newOrgDisplayName.trim(),
        plan_tier: "starter",
      });

      newOrgSlug = "";
      newOrgDisplayName = "";
      await invalidateAll();
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to create organization";
    } finally {
      creatingOrg = false;
    }
  }

  async function handleLogout() {
    await logoutControlSession({});
    goto("/auth");
  }

  function getWorkspaceStatus(workspace) {
    switch (workspace.status) {
      case "provisioning":
        return "Provisioning";
      case "ready":
        return "Ready";
      case "suspended":
        return "Suspended";
      case "degraded":
        return "Degraded";
      case "archived":
        return "Archived";
      default:
        return workspace.status;
    }
  }

  function getWorkspaceStatusColor(workspace) {
    switch (workspace.status) {
      case "provisioning":
        return "text-amber-400";
      case "ready":
        return "text-green-400";
      case "suspended":
        return "text-amber-400";
      case "degraded":
        return "text-red-400";
      case "archived":
        return "text-[var(--ui-text-muted)]";
      default:
        return "text-[var(--ui-text-muted)]";
    }
  }

  async function launchWorkspace(workspace) {
    if (workspace.status === "provisioning") {
      return;
    }

    launchingWorkspaceId = workspace.id;
    error = "";

    try {
      const response = await controlClient.launchWorkspace(
        workspace.id,
        workspace.slug,
      );
      await goto(
        response.redirect_to ||
          (workspace.workspace_path ?? `/${workspace.slug}`),
      );
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to launch workspace";
    } finally {
      launchingWorkspaceId = "";
    }
  }

  function getBillingSummary(org) {
    return billingByOrganization?.[org.id] ?? null;
  }

  function humanizeBillingValue(value) {
    return String(value ?? "")
      .replaceAll("_", " ")
      .replace(/\b\w/g, (match) => match.toUpperCase());
  }

  function recommendedBillingPlan(org) {
    if (org.plan_tier === "starter") {
      return "team";
    }
    if (org.plan_tier === "team") {
      return "team";
    }
    if (org.plan_tier === "scale") {
      return "scale";
    }
    return "";
  }

  async function handleStartBilling(org, planTier) {
    if (!planTier) {
      return;
    }

    billingActionOrganizationId = org.id;
    billingActionMode = `checkout:${planTier}`;
    error = "";

    try {
      const response =
        await controlClient.createOrganizationBillingCheckoutSession(org.id, {
          plan_tier: planTier,
        });
      const session = response?.session ?? {};
      if (!session.url) {
        throw new Error(
          session.note || "Billing checkout URL was not returned",
        );
      }
      window.location.assign(session.url);
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to start billing";
      billingActionOrganizationId = "";
      billingActionMode = "";
    }
  }

  async function handleManageBilling(org) {
    billingActionOrganizationId = org.id;
    billingActionMode = "portal";
    error = "";

    try {
      const response =
        await controlClient.createOrganizationBillingCustomerPortalSession(
          org.id,
          {},
        );
      const session = response?.session ?? {};
      if (!session.url) {
        throw new Error(session.note || "Billing portal URL was not returned");
      }
      window.location.assign(session.url);
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to open billing";
      billingActionOrganizationId = "";
      billingActionMode = "";
    }
  }
</script>

<svelte:head>
  <title>Dashboard - OAR Control</title>
</svelte:head>

<main class="min-h-screen bg-[var(--ui-bg)] px-4 py-10 text-[var(--ui-text)]">
  <div class="mx-auto max-w-6xl">
    <header class="mb-8 flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-bold text-[var(--ui-text)]">
          Organizations &amp; Workspaces
        </h1>
        <p class="text-[var(--ui-text-muted)]">
          Manage your organizations and workspaces
        </p>
      </div>

      <div class="flex gap-2">
        {#if organizations.length > 0}
          <a
            class="rounded-md bg-indigo-600 px-3 py-2 text-[12px] font-medium text-white hover:bg-indigo-500"
            href="/dashboard/workspaces"
          >
            Create Workspace
          </a>
        {/if}
        <button
          class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[12px] font-medium text-[var(--ui-text)] hover:bg-[var(--ui-border-subtle)]"
          onclick={handleLogout}
          type="button"
        >
          Sign out
        </button>
      </div>
    </header>

    {#if error}
      <div class="rounded-md bg-red-500/10 px-4 py-3 text-[13px] text-red-400">
        {error}
      </div>
    {:else if organizations.length === 0}
      <div
        class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-4 py-8"
      >
        <div class="text-center">
          <svg
            class="mx-auto h-12 w-12 text-[var(--ui-text-muted)]"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="1.5"
              d="M19 21V5a2 2 0 00-2-2H7a2 2 0 00-2 2v16m14 0h2m-2 0h-5m-9 0H3m2 0h5M9 7h1m-1 4h1m4-4h1m-1 4h1m-5 10v-5a1 1 0 011-1h2a1 1 0 011 1v5m-4 0h4"
            />
          </svg>
          <h2 class="mt-4 text-lg font-semibold text-[var(--ui-text)]">
            No organizations yet
          </h2>
          <p class="mt-2 text-[var(--ui-text-muted)]">
            Create an organization to get started.
          </p>
          <form
            class="mt-4 space-y-3 text-left"
            onsubmit={(e) => {
              e.preventDefault();
              handleCreateOrganization();
            }}
          >
            <div>
              <label
                class="block text-[12px] font-medium text-[var(--ui-text-muted)]"
                for="org-slug"
              >
                Slug
              </label>
              <input
                bind:value={newOrgSlug}
                class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
                id="org-slug"
                placeholder="my-org"
                type="text"
              />
            </div>

            <div>
              <label
                class="block text-[12px] font-medium text-[var(--ui-text-muted)]"
                for="org-display-name"
              >
                Display name
              </label>
              <input
                bind:value={newOrgDisplayName}
                class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
                id="org-display-name"
                placeholder="My Organization"
                type="text"
              />
            </div>

            <button
              class="w-full rounded-md bg-indigo-600 px-4 py-2 text-[13px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
              disabled={creatingOrg}
              type="submit"
            >
              {creatingOrg ? "Creating..." : "Create Organization"}
            </button>
          </form>
        </div>
      </div>
    {:else}
      <div class="space-y-8">
        {#each organizations as org}
          <section
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)]"
          >
            <div class="border-b border-[var(--ui-border)] px-4 py-3">
              <div class="flex items-center justify-between">
                <div>
                  <h2 class="text-lg font-semibold text-[var(--ui-text)]">
                    {org.display_name}
                  </h2>
                  <p class="text-[12px] text-[var(--ui-text-muted)]">
                    Slug: {org.slug} | Plan: {org.plan_tier}
                  </p>
                </div>
                <a
                  class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-1 text-[11px] font-medium text-[var(--ui-text)] hover:bg-[var(--ui-border-subtle)]"
                  href="/dashboard/workspaces"
                >
                  Add Workspace
                </a>
              </div>
            </div>

            <div class="px-4 py-3">
              {#if getBillingSummary(org)}
                {@const billing = getBillingSummary(org)}
                <div
                  class="mb-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-3 py-3"
                >
                  <div class="flex flex-wrap items-center gap-2">
                    <span
                      class="rounded-md bg-indigo-600/15 px-2 py-1 text-[11px] font-medium text-indigo-300"
                    >
                      Billing: {humanizeBillingValue(
                        billing.billing_account?.billing_status ?? "free",
                      )}
                    </span>
                    <span
                      class="rounded-md bg-[var(--ui-bg-soft)] px-2 py-1 text-[11px] font-medium text-[var(--ui-text-muted)]"
                    >
                      Stripe customer: {billing.billing_account
                        ?.stripe_customer_id || "not linked"}
                    </span>
                    <span
                      class="rounded-md bg-[var(--ui-bg-soft)] px-2 py-1 text-[11px] font-medium text-[var(--ui-text-muted)]"
                    >
                      Webhook: {billing.configuration?.webhook_secret_configured
                        ? "configured"
                        : "missing"}
                    </span>
                  </div>

                  <div class="mt-3 grid gap-3 md:grid-cols-3">
                    <div
                      class="rounded-md border border-[var(--ui-border)] px-3 py-2"
                    >
                      <p
                        class="text-[11px] uppercase tracking-wide text-[var(--ui-text-muted)]"
                      >
                        Usage
                      </p>
                      <p class="mt-1 text-[13px] text-[var(--ui-text)]">
                        {billing.usage_summary?.usage?.workspace_count ?? 0} workspaces
                      </p>
                      <p class="text-[12px] text-[var(--ui-text-muted)]">
                        {billing.usage_summary?.usage?.storage_gb ?? 0} GB tracked
                      </p>
                      <p class="text-[12px] text-[var(--ui-text-muted)]">
                        {billing.usage_summary?.usage?.monthly_launch_count ??
                          0} launches this month
                      </p>
                    </div>

                    <div
                      class="rounded-md border border-[var(--ui-border)] px-3 py-2"
                    >
                      <p
                        class="text-[11px] uppercase tracking-wide text-[var(--ui-text-muted)]"
                      >
                        Entitlement
                      </p>
                      <p class="mt-1 text-[13px] text-[var(--ui-text)]">
                        {billing.plan_tier} plan
                      </p>
                      <p class="text-[12px] text-[var(--ui-text-muted)]">
                        {billing.usage_summary?.quota?.workspaces_remaining ??
                          0}
                        workspaces remaining
                      </p>
                      <p class="text-[12px] text-[var(--ui-text-muted)]">
                        {billing.usage_summary?.quota?.storage_gb_remaining ??
                          0}
                        GB remaining
                      </p>
                    </div>

                    <div
                      class="rounded-md border border-[var(--ui-border)] px-3 py-2"
                    >
                      <p
                        class="text-[11px] uppercase tracking-wide text-[var(--ui-text-muted)]"
                      >
                        Stripe Billing
                      </p>
                      <p class="mt-1 text-[13px] text-[var(--ui-text)]">
                        {billing.configuration?.configured
                          ? "Config looks complete"
                          : "Review and fill values"}
                      </p>
                      <p class="text-[12px] text-[var(--ui-text-muted)]">
                        Publishable key: {billing.configuration
                          ?.publishable_key_configured
                          ? "set"
                          : "missing"}
                      </p>
                      <p class="text-[12px] text-[var(--ui-text-muted)]">
                        Checkout: {billing.configuration?.checkout_configured
                          ? "ready"
                          : "not ready"}
                      </p>
                      <div class="mt-3 flex flex-wrap gap-2">
                        {#if billing.billing_account?.stripe_customer_id}
                          <button
                            class="rounded-md bg-indigo-600 px-3 py-1 text-[11px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
                            disabled={billingActionOrganizationId === org.id}
                            onclick={() => handleManageBilling(org)}
                            type="button"
                          >
                            {billingActionOrganizationId === org.id &&
                            billingActionMode === "portal"
                              ? "Opening..."
                              : "Manage Billing"}
                          </button>
                        {:else if recommendedBillingPlan(org)}
                          <button
                            class="rounded-md bg-indigo-600 px-3 py-1 text-[11px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
                            disabled={billingActionOrganizationId === org.id}
                            onclick={() =>
                              handleStartBilling(
                                org,
                                recommendedBillingPlan(org),
                              )}
                            type="button"
                          >
                            {billingActionOrganizationId === org.id &&
                            billingActionMode ===
                              `checkout:${recommendedBillingPlan(org)}`
                              ? "Opening..."
                              : `Start ${humanizeBillingValue(
                                  recommendedBillingPlan(org),
                                )} Billing`}
                          </button>
                        {/if}
                      </div>
                    </div>
                  </div>

                  {#if (billing.configuration?.missing_configuration ?? []).length > 0}
                    <div
                      class="mt-3 rounded-md bg-amber-500/10 px-3 py-2 text-[12px] text-amber-300"
                    >
                      Missing Stripe values:
                      {#each billing.configuration.missing_configuration as missing, index}
                        <span
                          >{index === 0 ? " " : ", "}{humanizeBillingValue(
                            missing,
                          )}</span
                        >
                      {/each}
                    </div>
                  {/if}
                </div>
              {/if}

              {#if workspaces.filter((ws) => ws.organization_id === org.id).length === 0}
                <div class="py-4 text-center">
                  <svg
                    class="mx-auto h-8 w-8 text-[var(--ui-text-muted)]"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                  >
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      stroke-width="1.5"
                      d="M4 5a1 1 0 011-1h14a1 1 0 011 1v2a1 1 0 01-1 1H5a1 1 0 01-1-1V5zM4 13a1 1 0 011-1h6a1 1 0 011 1v6a1 1 0 01-1 1H5a1 1 0 01-1-1v-6zM16 13a1 1 0 011-1h2a1 1 0 011 1v6a1 1 0 01-1 1h-2a1 1 0 01-1-1v-6z"
                    />
                  </svg>
                  <p class="mt-2 text-[13px] text-[var(--ui-text-muted)]">
                    No workspaces yet.
                  </p>
                  <a
                    class="mt-3 inline-block rounded-md bg-indigo-600 px-3 py-1 text-[11px] font-medium text-white hover:bg-indigo-500"
                    href="/dashboard/workspaces"
                  >
                    Create your first workspace
                  </a>
                </div>
              {:else}
                <div class="space-y-2">
                  {#each workspaces.filter((ws) => ws.organization_id === org.id) as ws}
                    <div
                      class="flex items-center justify-between rounded-md border border-[var(--ui-border)] px-3 py-2"
                    >
                      <div>
                        <p class="font-medium text-[var(--ui-text)]">
                          {ws.display_name}
                        </p>
                        <p class="text-[12px]">
                          <span class={getWorkspaceStatusColor(ws)}>
                            {getWorkspaceStatus(ws)}
                          </span>
                          <span class="text-[var(--ui-text-muted)]">
                            {" "}| Region: {ws.region}
                          </span>
                        </p>
                      </div>
                      {#if ws.status === "provisioning"}
                        <span
                          class="rounded-md bg-amber-500/10 px-3 py-1 text-[11px] font-medium text-amber-400"
                        >
                          Provisioning...
                        </span>
                      {:else if ws.status === "suspended"}
                        <span
                          class="rounded-md bg-amber-500/10 px-3 py-1 text-[11px] font-medium text-amber-400"
                        >
                          Suspended
                        </span>
                      {:else}
                        <button
                          class="rounded-md bg-indigo-600 px-3 py-1 text-[12px] font-medium text-white hover:bg-indigo-500"
                          disabled={launchingWorkspaceId === ws.id}
                          onclick={() => launchWorkspace(ws)}
                          type="button"
                        >
                          {launchingWorkspaceId === ws.id
                            ? "Launching..."
                            : "Launch"}
                        </button>
                      {/if}
                    </div>
                  {/each}
                </div>
              {/if}
            </div>
          </section>
        {/each}

        {#if workspaces.filter((ws) => !organizations.find((org) => org.id === ws.organization_id)).length > 0}
          <section
            class="rounded-md border border-amber-500/50 bg-amber-500/10 px-4 py-3"
          >
            <p class="text-[var(--ui-text-muted)]">
              Workspaces without an organization
            </p>
          </section>
        {/if}
      </div>
    {/if}
  </div>
</main>
