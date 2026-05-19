<script>
  import { onMount } from "svelte";

  import Button from "$lib/components/Button.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import StatusPill from "$lib/hosted/StatusPill.svelte";
  import {
    detailHref,
    eventLabel,
    formatDateTime,
    formatListValue,
    formatNumber,
    providerLabels,
  } from "$lib/hosted/adminOverview.js";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  const TOKEN_STORAGE_KEY = "anx_admin_token";
  const ACTOR_STORAGE_KEY = "anx_admin_actor";

  let token = $state("");
  let actor = $state("");
  let account = $state(null);
  let loading = $state(false);
  let error = $state("");

  const accountId = $derived(
    decodeURIComponent(globalThis.location?.pathname.split("/").pop() ?? ""),
  );

  onMount(() => {
    token = localStorage.getItem(TOKEN_STORAGE_KEY) ?? "";
    actor = localStorage.getItem(ACTOR_STORAGE_KEY) ?? "";
    void loadDetail();
  });

  function headers() {
    const out = { "x-anx-admin-token": token.trim() };
    if (actor.trim()) out["x-anx-admin-actor"] = actor.trim();
    return out;
  }

  async function loadDetail() {
    if (!token.trim()) {
      error = "Open /hosted/admin and enter an operator admin token first.";
      return;
    }
    loading = true;
    error = "";
    try {
      const res = await hostedCpFetch(
        `admin/analytics/accounts/${encodeURIComponent(accountId)}`,
        {
          headers: headers(),
        },
      );
      if (!res.ok) throw new Error(await responseError(res));
      account = (await res.json()).account ?? null;
    } catch (e) {
      error = e instanceof Error ? e.message : "Account did not load.";
    } finally {
      loading = false;
    }
  }

  async function responseError(res) {
    try {
      const body = await res.json();
      return body?.error?.message || body?.error?.code || res.statusText;
    } catch {
      return res.statusText;
    }
  }
</script>

<svelte:head>
  <title>Admin Account - Agent Nexus (ANX)</title>
</svelte:head>

<div class="mx-auto max-w-7xl space-y-4 px-4 py-5">
  <header class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <a class="text-micro text-accent-text" href="/hosted/admin/accounts"
        >Accounts</a
      >
      <h1 class="mt-1 text-display text-fg">
        {account?.display_name || account?.email || "Account"}
      </h1>
      {#if account}
        <p class="mt-1 font-mono text-micro text-fg-subtle">
          {account.email} · {account.id}
        </p>
      {/if}
    </div>
    <Button variant="secondary" onclick={loadDetail} disabled={loading}
      >{loading ? "Refreshing..." : "Refresh"}</Button
    >
  </header>

  {#if error}
    <StateError
      title="Account did not load"
      message={error}
      onretry={loadDetail}
      retrying={loading}
    />
  {:else if account}
    <section class="grid gap-3 md:grid-cols-4">
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <div class="text-micro uppercase tracking-wide text-fg-subtle">
          Status
        </div>
        <div class="mt-2"><StatusPill status={account.status} /></div>
      </div>
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <div class="text-micro uppercase tracking-wide text-fg-subtle">
          Created
        </div>
        <div class="mt-2 text-heading text-fg">
          {formatDateTime(account.created_at)}
        </div>
      </div>
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <div class="text-micro uppercase tracking-wide text-fg-subtle">
          Last login
        </div>
        <div class="mt-2 text-heading text-fg">
          {formatDateTime(account.last_login_at)}
        </div>
      </div>
      <div class="rounded-md border border-line bg-bg-soft p-4">
        <div class="text-micro uppercase tracking-wide text-fg-subtle">
          Sessions
        </div>
        <div class="mt-2 text-heading text-fg">
          {formatNumber(account.active_session_count)}
        </div>
      </div>
    </section>

    <section class="grid gap-3 xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
      <div class="rounded-md border border-line bg-bg-soft">
        <div class="border-b border-line px-4 py-3">
          <h2 class="text-heading text-fg">Org memberships</h2>
        </div>
        {#if account.organization_memberships?.length}
          <div class="overflow-x-auto">
            <table class="min-w-full text-left text-micro">
              <thead class="border-b border-line text-fg-subtle"
                ><tr
                  ><th class="px-4 py-2">Organization</th><th class="px-3 py-2"
                    >Role</th
                  ><th class="px-3 py-2">Status</th><th class="px-4 py-2"
                    >Joined</th
                  ></tr
                ></thead
              >
              <tbody>
                {#each account.organization_memberships as membership (`${membership.organization_id}-${membership.role}`)}
                  <tr class="border-b border-line/60 last:border-b-0">
                    <td class="px-4 py-2"
                      ><a
                        class="text-accent-text"
                        href={detailHref("org", membership.organization_id)}
                        >{membership.organization_slug ||
                          membership.organization_id}</a
                      ></td
                    >
                    <td class="px-3 py-2">{formatListValue(membership.role)}</td
                    >
                    <td class="px-3 py-2"
                      ><StatusPill status={membership.status} /></td
                    >
                    <td class="px-4 py-2 text-fg-subtle"
                      >{formatDateTime(membership.created_at)}</td
                    >
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {:else}
          <p class="p-4 text-micro text-fg-subtle">
            No organization memberships.
          </p>
        {/if}
      </div>

      <div class="rounded-md border border-line bg-bg-soft p-4">
        <h2 class="text-heading text-fg">Linked providers</h2>
        <p class="mt-3 text-meta text-fg">
          {providerLabels(account) || "None"}
        </p>
        <p class="mt-1 text-micro text-fg-subtle">
          Provider subject identifiers are intentionally not shown.
        </p>
      </div>
    </section>

    <section class="rounded-md border border-line bg-bg-soft p-4">
      <h2 class="text-heading text-fg">Recent audit events</h2>
      {#if account.recent_audit_events?.length}
        <ul class="mt-3 divide-y divide-line">
          {#each account.recent_audit_events.slice(0, 16) as event (event.id)}
            <li
              class="grid gap-2 py-2 text-micro md:grid-cols-[12rem_1fr_auto]"
            >
              <span class="text-fg-subtle"
                >{formatDateTime(event.occurred_at)}</span
              >
              <span class="min-w-0 truncate text-fg"
                >{eventLabel(event.event_type)}</span
              >
              <span class="font-mono text-fg-subtle"
                >{event.organization_id || event.workspace_id || event.id}</span
              >
            </li>
          {/each}
        </ul>
      {:else}
        <p class="mt-3 text-micro text-fg-subtle">No recent audit events.</p>
      {/if}
    </section>
  {/if}
</div>
