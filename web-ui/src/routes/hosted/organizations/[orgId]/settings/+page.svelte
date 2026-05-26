<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import Button from "$lib/components/Button.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";
  import {
    classifiedCpFetch,
    errorUserMessage,
    isAuthError,
  } from "$lib/hosted/fetchState.js";
  import { hostedSession, loadHostedSession } from "$lib/hosted/session.js";

  const orgId = $derived(String($page.params.orgId ?? ""));
  const session = $derived($hostedSession);

  let phase = $state("loading");
  let loadError = $state("");
  let retrying = $state(false);
  let actionBusy = $state(false);
  let actionMessage = $state("");
  let confirmation = $state("");
  let org = $state(null);
  /** @type {any[]} */
  let memberships = $state([]);
  let lastLoadedKey = $state("");

  const myAccountId = $derived(String(session.account?.id ?? "").trim());
  const myMembership = $derived(
    memberships.find((m) => String(m.account_id) === myAccountId) ?? null,
  );
  const canManage = $derived(
    myMembership &&
      (myMembership.role === "owner" || myMembership.role === "admin") &&
      myMembership.status === "active",
  );
  const deactivated = $derived(
    org?.status !== "active" || org?.restriction_reason === "decommission",
  );
  const expectedConfirmation = $derived(
    String(org?.display_name || org?.slug || "").trim(),
  );
  const confirmationMatches = $derived(
    confirmation.trim() === expectedConfirmation ||
      confirmation.trim() === String(org?.slug ?? "").trim(),
  );

  async function loadData() {
    if (!orgId) return;
    phase = "loading";
    loadError = "";
    actionMessage = "";
    try {
      const orgRes = await classifiedCpFetch(
        `organizations/${encodeURIComponent(orgId)}`,
      );
      const orgBody = await orgRes.json();
      const nextOrg = orgBody.organization ?? null;
      org = nextOrg;
      if (nextOrg?.status === "active") {
        const memRes = await classifiedCpFetch(
          `organizations/${encodeURIComponent(orgId)}/memberships?limit=100`,
        );
        const memBody = await memRes.json();
        memberships = Array.isArray(memBody.memberships)
          ? memBody.memberships
          : [];
      } else {
        memberships = [];
      }
      phase = "ready";
    } catch (e) {
      if (isAuthError(e)) {
        await goto("/hosted/start");
        return;
      }
      loadError = errorUserMessage(e);
      phase = "ready";
    }
  }

  async function retry() {
    retrying = true;
    await loadData();
    retrying = false;
  }

  async function readJsonError(res) {
    try {
      const j = await res.json();
      return j?.error?.message || j?.error?.code || res.statusText;
    } catch {
      return res.statusText;
    }
  }

  async function deactivateOrganization() {
    actionMessage = "";
    if (!confirmationMatches) {
      actionMessage = "Type the organization name or slug to continue.";
      return;
    }
    actionBusy = true;
    try {
      const res = await hostedCpFetch(
        `organizations/${encodeURIComponent(orgId)}/deactivate`,
        {
          method: "POST",
          body: JSON.stringify({ confirmation: confirmation.trim() }),
        },
      );
      if (!res.ok) {
        actionMessage = await readJsonError(res);
        return;
      }
      const body = await res.json();
      org = body.organization ?? org;
      confirmation = "";
      actionMessage = "Organization deactivated.";
      await loadHostedSession();
    } catch (e) {
      actionMessage =
        e instanceof Error ? e.message : "Could not deactivate organization.";
    } finally {
      actionBusy = false;
    }
  }

  $effect(() => {
    if (!browser || !orgId) return;
    if (session.phase === "idle" || session.phase === "loading") {
      lastLoadedKey = "";
      void loadHostedSession();
      return;
    }
    if (session.phase !== "authed") {
      return;
    }
    const key = `${session.account?.id ?? ""}::${orgId}`;
    if (lastLoadedKey === key) {
      return;
    }
    lastLoadedKey = key;
    void loadData();
  });
</script>

<svelte:head>
  <title>Organization settings — ANX</title>
</svelte:head>

<div class="space-y-5">
  <div class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <a
        class="text-micro text-fg-subtle hover:text-fg"
        href="/hosted/dashboard">Dashboard</a
      >
      <h1 class="mt-1 text-display text-fg">Organization settings</h1>
    </div>
    {#if !deactivated}
      <Button
        variant="secondary"
        href={`/hosted/organizations/${encodeURIComponent(orgId)}/team`}
        >Team</Button
      >
    {/if}
  </div>

  {#if phase === "loading"}
    <div class="rounded-md border border-line bg-bg-soft px-4 py-4">
      <Skeleton rows={5} />
    </div>
  {:else if loadError}
    <StateError
      message={loadError}
      onretry={retry}
      {retrying}
      supportHint={true}
    />
  {:else if org}
    <section class="rounded-md border border-line bg-bg-soft px-4 py-4">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 class="text-subtitle text-fg">{org.display_name || org.slug}</h2>
          <p class="mt-1 font-mono text-mono text-fg-subtle">{org.slug}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <span class="rounded bg-panel px-2 py-1 text-micro text-fg-muted"
            >{org.status}</span
          >
          <span class="rounded bg-panel px-2 py-1 text-micro text-fg-muted"
            >{org.access_mode || "read_write"}</span
          >
          {#if org.restriction_reason}
            <span
              class="rounded bg-danger-soft px-2 py-1 text-micro text-danger-text"
              >{org.restriction_reason}</span
            >
          {/if}
        </div>
      </div>
    </section>

    <section class="rounded-md border border-danger bg-danger-soft px-4 py-4">
      <div class="max-w-2xl">
        <h2 class="text-subtitle text-danger-text">Deactivate organization</h2>
        <p class="mt-2 text-body text-danger-text">
          Deactivation is a soft lock. Workspaces and billing records are kept.
          Hosted access is blocked, and running workspace runtimes stop through
          normal idle handling.
        </p>
      </div>

      {#if deactivated}
        {#if actionMessage}
          <p class="mt-4 text-body text-danger-text">{actionMessage}</p>
        {/if}
        <p class="mt-4 text-body text-danger-text">
          This organization is already deactivated.
        </p>
      {:else if !canManage}
        <p class="mt-4 text-body text-danger-text">
          Owner or admin access is required.
        </p>
      {:else}
        <div class="mt-4 max-w-xl space-y-3">
          <label
            class="block text-micro font-medium text-danger-text"
            for="confirm-org">Type {expectedConfirmation}</label
          >
          <input
            id="confirm-org"
            class="h-9 w-full rounded border border-danger bg-bg px-3 text-body text-fg outline-none focus:border-danger-text"
            autocomplete="off"
            bind:value={confirmation}
          />
          <div class="flex flex-wrap items-center gap-3">
            <Button
              variant="danger"
              busy={actionBusy}
              disabled={!confirmationMatches}
              onclick={deactivateOrganization}>Deactivate organization</Button
            >
            {#if actionMessage}
              <span class="text-micro text-danger-text">{actionMessage}</span>
            {/if}
          </div>
        </div>
      {/if}
    </section>
  {/if}
</div>
