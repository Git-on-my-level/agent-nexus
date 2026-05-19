<script>
  import { onMount } from "svelte";

  import Button from "$lib/components/Button.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import StatusPill from "$lib/hosted/StatusPill.svelte";
  import {
    detailHref,
    formatDateTime,
    formatListValue,
    formatNumber,
    providerLabels,
    sortRows,
  } from "$lib/hosted/adminOverview.js";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  const TOKEN_STORAGE_KEY = "anx_admin_token";
  const ACTOR_STORAGE_KEY = "anx_admin_actor";

  let token = $state("");
  let actor = $state("");
  let rows = $state([]);
  let q = $state("");
  let status = $state("");
  let createdAfter = $state("");
  let loginAfter = $state("");
  let membership = $state("");
  let provider = $state("");
  let sortKey = $state("last_login_at");
  let sortDirection = $state("desc");
  let loading = $state(false);
  let error = $state("");

  const filteredRows = $derived(
    sortRows(
      rows.filter((account) => {
        const memberships = account.organization_memberships ?? [];
        const text = [
          account.id,
          account.email,
          account.display_name,
          providerLabels(account),
          memberships
            .map(
              (m) =>
                `${m.organization_id} ${m.organization_slug} ${m.role} ${m.status}`,
            )
            .join(" "),
        ]
          .join(" ")
          .toLowerCase();
        if (q.trim() && !text.includes(q.trim().toLowerCase())) return false;
        if (status && status !== account.status) return false;
        if (
          createdAfter &&
          String(account.created_at ?? "").slice(0, 10) < createdAfter
        )
          return false;
        if (
          loginAfter &&
          String(account.last_login_at ?? "").slice(0, 10) < loginAfter
        )
          return false;
        if (membership && !text.includes(membership.toLowerCase()))
          return false;
        if (provider && !(account.oauth_providers ?? []).includes(provider))
          return false;
        return true;
      }),
      sortKey,
      sortDirection,
    ),
  );

  const statusOptions = $derived(
    uniqueValues(rows.map((account) => account.status)),
  );
  const providerOptions = $derived(
    uniqueValues(rows.flatMap((account) => account.oauth_providers ?? [])),
  );

  onMount(() => {
    token = localStorage.getItem(TOKEN_STORAGE_KEY) ?? "";
    actor = localStorage.getItem(ACTOR_STORAGE_KEY) ?? "";
    void loadAccounts();
  });

  function uniqueValues(values) {
    return [...new Set(values.filter(Boolean))].sort();
  }

  function headers() {
    const out = { "x-anx-admin-token": token.trim() };
    if (actor.trim()) out["x-anx-admin-actor"] = actor.trim();
    return out;
  }

  async function loadAccounts() {
    if (!token.trim()) {
      error = "Open /hosted/admin and enter an operator admin token first.";
      return;
    }
    loading = true;
    error = "";
    try {
      const res = await hostedCpFetch("admin/analytics/accounts?limit=100", {
        headers: headers(),
      });
      if (!res.ok) throw new Error(await responseError(res));
      rows = (await res.json()).accounts ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : "Accounts did not load.";
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
  <title>Admin Accounts - Agent Nexus (ANX)</title>
</svelte:head>

<div class="mx-auto max-w-7xl space-y-4 px-4 py-5">
  <header class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <a class="text-micro text-accent-text" href="/hosted/admin"
        >Admin overview</a
      >
      <h1 class="mt-1 text-display text-fg">Accounts</h1>
    </div>
    <Button variant="secondary" onclick={loadAccounts} disabled={loading}
      >{loading ? "Refreshing..." : "Refresh"}</Button
    >
  </header>

  <section
    class="grid gap-3 rounded-md border border-line bg-bg-soft p-4 md:grid-cols-4"
  >
    <input
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={q}
      placeholder="Email, name, id"
    />
    <select
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={status}
      ><option value="">Any status</option>{#each statusOptions as value}<option
          {value}>{formatListValue(value)}</option
        >{/each}</select
    >
    <select
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={provider}
      ><option value="">Any provider</option
      >{#each providerOptions as value}<option {value}
          >{formatListValue(value)}</option
        >{/each}</select
    >
    <input
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={membership}
      placeholder="Org membership"
    />
    <input
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={createdAfter}
      type="date"
      aria-label="Created after"
    />
    <input
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={loginAfter}
      type="date"
      aria-label="Last login after"
    />
    <select
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={sortKey}
      ><option value="last_login_at">Last login</option><option
        value="created_at">Created</option
      ><option value="email">Email</option><option value="active_session_count"
        >Sessions</option
      ></select
    >
    <select
      class="h-10 rounded-md border border-line bg-bg px-3 text-meta text-fg"
      bind:value={sortDirection}
      ><option value="desc">Desc</option><option value="asc">Asc</option
      ></select
    >
  </section>

  {#if error}
    <StateError
      title="Accounts did not load"
      message={error}
      onretry={loadAccounts}
      retrying={loading}
    />
  {:else if filteredRows.length}
    <section class="overflow-x-auto rounded-md border border-line bg-bg-soft">
      <table class="min-w-full text-left text-micro">
        <thead class="border-b border-line text-fg-subtle">
          <tr
            ><th class="px-4 py-2">Account</th><th class="px-3 py-2">Status</th
            ><th class="px-3 py-2">Providers</th><th
              class="px-3 py-2 text-right">Memberships</th
            ><th class="px-3 py-2 text-right">Sessions</th><th class="px-4 py-2"
              >Last login</th
            ></tr
          >
        </thead>
        <tbody>
          {#each filteredRows as account (account.id)}
            <tr class="border-b border-line/60 last:border-b-0">
              <td class="max-w-[19rem] px-4 py-2"
                ><a
                  class="block truncate text-fg hover:text-accent-text"
                  href={detailHref("account", account.id)}
                  >{account.display_name || account.email}</a
                ><span class="block truncate font-mono text-fg-subtle"
                  >{account.email} · {account.id}</span
                ></td
              >
              <td class="px-3 py-2"><StatusPill status={account.status} /></td>
              <td class="px-3 py-2 text-fg-subtle"
                >{providerLabels(account) || "None"}</td
              >
              <td class="px-3 py-2 text-right"
                >{formatNumber(account.organization_memberships?.length)}</td
              >
              <td class="px-3 py-2 text-right"
                >{formatNumber(account.active_session_count)}</td
              >
              <td class="px-4 py-2 text-fg-subtle"
                >{formatDateTime(account.last_login_at)}</td
              >
            </tr>
          {/each}
        </tbody>
      </table>
    </section>
  {:else}
    <StateEmpty
      title="No accounts match"
      helper="Adjust filters or refresh the admin analytics data."
    />
  {/if}
</div>
