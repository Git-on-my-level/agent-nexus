<script>
  import { onMount } from "svelte";
  import { page } from "$app/state";

  import Button from "$lib/components/Button.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import StatusPill from "$lib/hosted/StatusPill.svelte";
  import {
    adminHeaders,
    readAdminActor,
    readAdminToken,
  } from "$lib/hosted/adminAuth.js";
  import {
    detailHref,
    eventLabel,
    formatDateTime,
    formatListValue,
  } from "$lib/hosted/adminOverview.js";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  let token = $state("");
  let actor = $state("");
  let organizationID = $state("");
  let workspaceID = $state("");
  let accountID = $state("");
  let eventTypes = $state("");
  let events = $state([]);
  let nextCursor = $state("");
  let loading = $state(false);
  let error = $state("");

  onMount(() => {
    token = readAdminToken();
    actor = readAdminActor();
    const params = page.url.searchParams;
    organizationID = params.get("organization_id") ?? "";
    workspaceID = params.get("workspace_id") ?? "";
    accountID = params.get("account_id") ?? "";
    eventTypes = params.get("event_types") ?? "";
    void loadEvents("");
  });

  function query(cursor = "") {
    const q = new URLSearchParams();
    q.set("limit", "50");
    if (cursor) q.set("cursor", cursor);
    if (organizationID.trim()) q.set("organization_id", organizationID.trim());
    if (workspaceID.trim()) q.set("workspace_id", workspaceID.trim());
    if (accountID.trim()) q.set("account_id", accountID.trim());
    if (eventTypes.trim()) q.set("event_types", eventTypes.trim());
    return q.toString();
  }

  async function loadEvents(cursor = "") {
    if (!token.trim()) {
      error = "Open /hosted/admin and enter an operator admin token first.";
      return;
    }
    loading = true;
    error = "";
    try {
      const res = await hostedCpFetch(
        `admin/analytics/audit-events?${query(cursor)}`,
        {
          headers: adminHeaders(token, actor),
        },
      );
      if (!res.ok) throw new Error(await responseError(res));
      const body = await res.json();
      const rows = body.events ?? [];
      events = cursor ? [...events, ...rows] : rows;
      nextCursor = body.next_cursor ?? "";
    } catch (e) {
      if (!cursor) events = [];
      error = e instanceof Error ? e.message : "Audit events did not load.";
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

  function targetHref(event) {
    if (event?.workspace_id) return detailHref("workspace", event.workspace_id);
    if (event?.organization_id) return detailHref("org", event.organization_id);
    if (event?.actor_account_id)
      return detailHref("account", event.actor_account_id);
    return "/hosted/admin/audit-events";
  }
</script>

<svelte:head>
  <title>Admin Audit Events - Agent Nexus (ANX)</title>
</svelte:head>

<div class="mx-auto max-w-7xl space-y-4 px-4 py-5">
  <header class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <a class="text-micro text-accent-text" href="/hosted/admin"
        >Admin overview</a
      >
      <h1 class="mt-1 text-display text-fg">Audit events</h1>
      <p class="mt-1 max-w-3xl text-meta text-fg-muted">
        Read-only control-plane audit trail with admin redaction applied.
      </p>
    </div>
    <Button
      variant="secondary"
      onclick={() => loadEvents("")}
      disabled={loading}
    >
      {loading ? "Refreshing..." : "Refresh"}
    </Button>
  </header>

  <section class="rounded-md border border-line bg-bg-soft p-4">
    <form
      class="grid gap-3 md:grid-cols-4"
      onsubmit={(e) => {
        e.preventDefault();
        void loadEvents("");
      }}
    >
      <label class="grid gap-1 text-micro text-fg-muted">
        Organization id
        <input
          bind:value={organizationID}
          class="rounded-md border border-line bg-bg px-3 py-2 font-mono text-micro text-fg"
          placeholder="org_..."
        />
      </label>
      <label class="grid gap-1 text-micro text-fg-muted">
        Workspace id
        <input
          bind:value={workspaceID}
          class="rounded-md border border-line bg-bg px-3 py-2 font-mono text-micro text-fg"
          placeholder="ws_..."
        />
      </label>
      <label class="grid gap-1 text-micro text-fg-muted">
        Account id
        <input
          bind:value={accountID}
          class="rounded-md border border-line bg-bg px-3 py-2 font-mono text-micro text-fg"
          placeholder="acct_..."
        />
      </label>
      <label class="grid gap-1 text-micro text-fg-muted">
        Event types
        <input
          bind:value={eventTypes}
          class="rounded-md border border-line bg-bg px-3 py-2 font-mono text-micro text-fg"
          placeholder="type_a,type_b"
        />
      </label>
      <div class="md:col-span-4">
        <Button variant="secondary" type="submit" disabled={loading}>
          Apply filters
        </Button>
      </div>
    </form>
  </section>

  {#if error}
    <StateError
      title="Audit events did not load"
      message={error}
      onretry={() => loadEvents("")}
      retrying={loading}
    />
  {:else if events.length}
    <section class="overflow-x-auto rounded-md border border-line bg-bg-soft">
      <table class="min-w-full text-left text-micro">
        <thead class="border-b border-line text-fg-subtle">
          <tr>
            <th class="px-4 py-2">Occurred</th>
            <th class="px-3 py-2">Event</th>
            <th class="px-3 py-2">Target</th>
            <th class="px-3 py-2">Actor</th>
            <th class="px-4 py-2">Metadata</th>
          </tr>
        </thead>
        <tbody>
          {#each events as event (event.id)}
            <tr class="border-b border-line/60 last:border-b-0">
              <td class="whitespace-nowrap px-4 py-2 text-fg-subtle">
                {formatDateTime(event.occurred_at)}
              </td>
              <td class="px-3 py-2">
                <a
                  class="text-fg hover:text-accent-text"
                  href={targetHref(event)}
                >
                  {eventLabel(event.event_type)}
                </a>
                <div class="mt-1">
                  <StatusPill
                    status={event.event_type?.includes("failed")
                      ? "failed"
                      : "active"}
                    label={formatListValue(event.event_type)}
                  />
                </div>
              </td>
              <td class="max-w-[18rem] px-3 py-2 font-mono text-fg-subtle">
                <div class="truncate">{event.target_type || "unknown"}</div>
                <div class="truncate">
                  {event.target_id ||
                    event.workspace_id ||
                    event.organization_id ||
                    "unknown"}
                </div>
              </td>
              <td class="max-w-[14rem] px-3 py-2 font-mono text-fg-subtle">
                <span class="truncate"
                  >{event.actor_account_id || "system"}</span
                >
              </td>
              <td class="max-w-[24rem] px-4 py-2">
                {#if event.metadata && Object.keys(event.metadata).length}
                  <code
                    class="block max-h-24 overflow-auto rounded bg-bg px-2 py-1 font-mono text-[11px] text-fg-muted"
                    >{JSON.stringify(event.metadata)}</code
                  >
                {:else}
                  <span class="text-fg-subtle">None</span>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </section>

    {#if nextCursor}
      <div class="flex justify-center">
        <Button
          variant="secondary"
          onclick={() => loadEvents(nextCursor)}
          disabled={loading}
        >
          {loading ? "Loading..." : "Load more"}
        </Button>
      </div>
    {/if}
  {:else if !loading}
    <StateEmpty
      title="No audit events"
      helper="Adjust filters or refresh the admin analytics data."
    />
  {/if}
</div>
