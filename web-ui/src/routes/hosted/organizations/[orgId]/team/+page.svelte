<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";

  import Button from "$lib/components/Button.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
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
  /** @type {any[]} */
  let memberships = $state([]);
  /** @type {any[]} */
  let invites = $state([]);
  let loadError = $state("");
  let inviteError = $state("");
  let actionMessage = $state("");
  let phase = $state("loading");
  let retrying = $state(false);
  let actionBusy = $state(false);
  let inviteEmail = $state("");
  let inviteRole = $state("member");
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

  async function loadData() {
    if (!orgId) return;
    loadError = "";
    inviteError = "";
    actionMessage = "";
    phase = "loading";
    try {
      const [memRes, invRes] = await Promise.all([
        classifiedCpFetch(
          `organizations/${encodeURIComponent(orgId)}/memberships?limit=100`,
        ),
        classifiedCpFetch(
          `organizations/${encodeURIComponent(orgId)}/invites?limit=100`,
        ),
      ]);
      const mb = await memRes.json();
      const ib = await invRes.json();
      memberships = Array.isArray(mb?.memberships) ? mb.memberships : [];
      invites = Array.isArray(ib?.invites) ? ib.invites : [];
      phase = "ready";
    } catch (e) {
      if (isAuthError(e)) {
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

  $effect(() => {
    if (!browser || !orgId) return;
    if (session.phase === "idle" || session.phase === "loading") {
      lastLoadedKey = "";
      void loadHostedSession();
      return;
    }
    if (session.phase === "unauthed" || session.phase === "error") {
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

  const roleChoices = [
    { value: "owner", label: "Owner" },
    { value: "admin", label: "Admin" },
    { value: "member", label: "Member" },
    { value: "viewer", label: "Viewer" },
  ];

  const inviteRoleChoices = [
    { value: "admin", label: "Admin" },
    { value: "member", label: "Member" },
    { value: "viewer", label: "Viewer" },
  ];

  async function readJsonError(res) {
    try {
      const j = await res.json();
      return j?.error?.message || j?.error?.code || res.statusText;
    } catch {
      return res.statusText;
    }
  }

  async function updateMemberRole(membership, nextRole) {
    actionMessage = "";
    actionBusy = true;
    try {
      const res = await hostedCpFetch(
        `organizations/${encodeURIComponent(orgId)}/memberships/${encodeURIComponent(membership.id)}`,
        {
          method: "PATCH",
          body: JSON.stringify({ role: nextRole }),
        },
      );
      if (!res.ok) {
        actionMessage = await readJsonError(res);
        return;
      }
      await loadData();
    } catch (e) {
      actionMessage = e instanceof Error ? e.message : "Update failed.";
    } finally {
      actionBusy = false;
    }
  }

  async function removeMember(m) {
    if (String(m.account_id) === myAccountId) {
      actionMessage = "You cannot remove yourself here. Ask another admin.";
      return;
    }
    if (!confirm("Remove this member from the organization?")) {
      return;
    }
    actionMessage = "";
    actionBusy = true;
    try {
      const res = await hostedCpFetch(
        `organizations/${encodeURIComponent(orgId)}/memberships/${encodeURIComponent(m.id)}`,
        {
          method: "PATCH",
          body: JSON.stringify({ status: "disabled" }),
        },
      );
      if (!res.ok) {
        actionMessage = await readJsonError(res);
        return;
      }
      await loadData();
    } catch (e) {
      actionMessage = e instanceof Error ? e.message : "Remove failed.";
    } finally {
      actionBusy = false;
    }
  }

  async function createInvite() {
    inviteError = "";
    const email = inviteEmail.trim();
    if (!email) {
      inviteError = "Email is required.";
      return;
    }
    actionBusy = true;
    try {
      const res = await hostedCpFetch(
        `organizations/${encodeURIComponent(orgId)}/invites`,
        {
          method: "POST",
          body: JSON.stringify({ email, role: inviteRole }),
        },
      );
      const j = await res.json().catch(() => ({}));
      if (!res.ok) {
        inviteError = j?.error?.message || j?.error?.code || res.statusText;
        return;
      }
      inviteEmail = "";
      if (j.invite_url) {
        actionMessage = `Invite link created and copied. Send it to ${email} (it is only shown once).`;
        try {
          await navigator.clipboard.writeText(String(j.invite_url));
        } catch {
          /* clipboard may be unavailable */
        }
      } else {
        actionMessage = "Invitation created.";
      }
      await loadData();
    } catch (e) {
      inviteError = e instanceof Error ? e.message : "Invite failed.";
    } finally {
      actionBusy = false;
    }
  }

  async function revokeInvite(inv) {
    if (!confirm("Revoke this invitation?")) {
      return;
    }
    actionBusy = true;
    try {
      const res = await hostedCpFetch(
        `organizations/${encodeURIComponent(orgId)}/invites/${encodeURIComponent(inv.id)}/revoke`,
        { method: "POST" },
      );
      if (!res.ok) {
        actionMessage = await readJsonError(res);
        return;
      }
      await loadData();
    } catch (e) {
      actionMessage = e instanceof Error ? e.message : "Revoke failed.";
    } finally {
      actionBusy = false;
    }
  }
</script>

<svelte:head>
  <title>Team — Agent Nexus</title>
</svelte:head>

<div class="space-y-6">
  <div>
    <p class="text-micro text-fg-subtle">
      <a
        class="text-fg-subtle underline-offset-2 transition-colors hover:text-fg hover:underline"
        href="/hosted/organizations/{encodeURIComponent(orgId)}"
        >← Organization</a
      >
    </p>
    <h1 class="mt-1 text-display text-fg">Team &amp; invites</h1>
    <p class="mt-1 text-meta text-fg-subtle">
      Agent Nexus (ANX) uses organization roles for the hosted control plane.
      Workspaces and agents in the app stay workspace-local; invite people here
      to join your org on ANX.
    </p>
  </div>

  {#if actionMessage}
    <p
      role="status"
      class="rounded-md bg-ok-soft px-3 py-2 text-micro text-ok-text"
    >
      {actionMessage}
    </p>
  {/if}

  {#if loadError}
    <StateError
      message={loadError}
      onretry={retry}
      {retrying}
      supportHint={true}
    />
  {/if}

  {#if phase === "loading" && !loadError}
    <div class="rounded-md border border-line bg-bg-soft px-4 py-4">
      <Skeleton rows={5} />
    </div>
  {:else if !loadError && session.phase === "authed" && canManage}
    <section
      class="space-y-3 rounded-md border border-line bg-bg-soft px-4 py-4"
    >
      <h2 class="text-subtitle text-fg">Invite by email</h2>
      <p class="text-meta text-fg-subtle">
        Admins and owners can send an invitation link. The recipient uses Google
        or GitHub to finish signing in.
      </p>
      <div
        class="flex max-w-2xl flex-col gap-2 sm:flex-row sm:items-end sm:gap-3"
      >
        <label class="block min-w-0 flex-1 text-micro text-fg-muted">
          Email
          <input
            type="email"
            bind:value={inviteEmail}
            disabled={actionBusy}
            placeholder="colleague@company.com"
            class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 text-body text-fg"
          />
        </label>
        <label class="block text-micro text-fg-muted sm:w-40">
          Role
          <select
            bind:value={inviteRole}
            disabled={actionBusy}
            class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 text-body text-fg"
          >
            {#each inviteRoleChoices as opt (opt.value)}
              <option value={opt.value}>{opt.label}</option>
            {/each}
          </select>
        </label>
        <Button
          variant="primary"
          class="shrink-0"
          busy={actionBusy}
          disabled={actionBusy}
          onclick={createInvite}
        >
          Send invite
        </Button>
      </div>
      {#if inviteError}
        <p role="alert" class="text-micro text-danger-text">
          {inviteError}
        </p>
      {/if}
    </section>
  {:else if !loadError && session.phase === "authed" && !canManage}
    <p
      class="rounded-md border border-line bg-bg-soft px-4 py-3 text-body text-fg-subtle"
    >
      Only organization <strong class="text-fg">owners</strong> and
      <strong class="text-fg">admins</strong> can send invites and change roles.
    </p>
  {/if}

  {#if !loadError && phase === "ready" && session.phase === "authed"}
    <section class="rounded-md border border-line bg-bg-soft">
      <div class="border-b border-line px-4 py-2.5">
        <h2 class="text-subtitle text-fg">Members</h2>
      </div>
      {#if memberships.length === 0}
        <StateEmpty
          class="border-0 bg-transparent"
          title="No members listed"
          helper="Try again in a moment or contact support if this is unexpected."
        />
      {:else}
        <ul class="divide-y divide-line">
          {#each memberships as m (m.id)}
            <li
              class="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center sm:justify-between"
            >
              <div>
                <div class="text-subtitle text-fg">
                  {m.account_display_name || m.account_email || m.account_id}
                </div>
                {#if m.account_email}
                  <div class="text-micro text-fg-subtle">{m.account_email}</div>
                {/if}
                <div class="mt-0.5 text-micro text-fg-subtle">
                  Status: {m.status} ·
                  {String(m.account_id) === myAccountId ? "You" : "Member"}
                </div>
              </div>
              <div
                class="flex flex-wrap items-center gap-2 sm:shrink-0 sm:justify-end"
              >
                {#if canManage && m.status === "active"}
                  <label class="text-micro text-fg-muted">
                    <span class="sr-only">Role for member</span>
                    <select
                      class="rounded-md border border-line bg-bg px-2 py-1 text-body text-fg"
                      disabled={actionBusy ||
                        String(m.account_id) === myAccountId}
                      value={m.role}
                      onchange={(e) => {
                        const v = e.currentTarget.value;
                        if (v && v !== m.role) {
                          void updateMemberRole(m, v);
                        }
                      }}
                    >
                      {#each roleChoices as r (r.value)}
                        <option value={r.value}>{r.label}</option>
                      {/each}
                    </select>
                  </label>
                  <Button
                    variant="ghost"
                    size="compact"
                    class="text-danger-text"
                    disabled={actionBusy ||
                      String(m.account_id) === myAccountId}
                    onclick={() => removeMember(m)}
                  >
                    Remove
                  </Button>
                {:else}
                  <span
                    class="rounded-md border border-line px-2.5 py-1 text-micro text-fg"
                    >{m.role}</span
                  >
                {/if}
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </section>

    {#if invites.filter((i) => i.status === "pending").length > 0}
      <section class="rounded-md border border-line bg-bg-soft">
        <div class="border-b border-line px-4 py-2.5">
          <h2 class="text-subtitle text-fg">Pending invites</h2>
        </div>
        <ul class="divide-y divide-line">
          {#each invites.filter((i) => i.status === "pending") as inv (inv.id)}
            <li
              class="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center sm:justify-between"
            >
              <div>
                <div class="text-subtitle text-fg">{inv.email}</div>
                <div class="text-micro text-fg-subtle">Role: {inv.role}</div>
              </div>
              {#if canManage}
                <Button
                  variant="ghost"
                  size="compact"
                  disabled={actionBusy}
                  onclick={() => revokeInvite(inv)}
                >
                  Revoke
                </Button>
              {/if}
            </li>
          {/each}
        </ul>
      </section>
    {/if}
  {/if}
</div>
