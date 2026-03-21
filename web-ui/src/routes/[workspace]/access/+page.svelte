<script>
  import { page } from "$app/stores";

  import { authenticatedAgent } from "$lib/authSession";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { workspacePath } from "$lib/workspacePaths";

  let loading = $state(true);
  let pageError = $state("");
  let workspaceSlug = $derived($page.params.workspace);

  let bootstrapStatus = $state(null);
  let principals = $state([]);
  let invites = $state([]);
  let auditEvents = $state([]);

  let principalsCursor = $state("");
  let principalsHasMore = $state(false);
  let loadingMorePrincipals = $state(false);

  let auditCursor = $state("");
  let auditHasMore = $state(false);
  let loadingMoreAudit = $state(false);

  let creatingInvite = $state(false);
  let inviteError = $state("");
  let newInviteNote = $state("");
  let newInviteKind = $state("agent");

  let createdToken = $state("");
  let tokenCopied = $state(false);
  let tokenDismissed = $state(false);

  let revokingInviteId = $state("");
  let revokeError = $state("");

  let principalRevokeTarget = $state(null);
  let principalRevokeConfirming = $state(false);
  let principalRevokeForcing = $state(false);
  let principalRevokeError = $state("");
  let principalRevokeRequiresForce = $state(false);

  const SECTION_IDLE = "idle";
  const SECTION_READY = "ready";
  const SECTION_ERROR = "error";

  let bootstrapState = $state({ status: SECTION_IDLE, error: "" });
  let principalsState = $state({ status: SECTION_IDLE, error: "" });
  let invitesState = $state({ status: SECTION_IDLE, error: "" });
  let auditState = $state({ status: SECTION_IDLE, error: "" });

  let canManageAccess = $derived(Boolean($authenticatedAgent));

  $effect(() => {
    if (!canManageAccess) return;
    loadAccessData();
  });

  async function loadAccessData() {
    loading = true;
    pageError = "";

    const [bootstrapResult, principalsResult, invitesResult, auditResult] =
      await Promise.allSettled([
        coreClient.bootstrapStatus(),
        coreClient.listPrincipals({ limit: 50 }),
        coreClient.listInvites(),
        coreClient.listAuthAudit({ limit: 50 }),
      ]);

    if (bootstrapResult.status === "fulfilled") {
      bootstrapStatus = bootstrapResult.value;
      bootstrapState = { status: SECTION_READY, error: "" };
    } else {
      bootstrapState = {
        status: SECTION_ERROR,
        error: extractErrorMessage(
          bootstrapResult.reason,
          "Failed to load bootstrap status",
        ),
      };
    }

    if (principalsResult.status === "fulfilled") {
      const data = principalsResult.value;
      principals = data?.principals ?? [];
      principalsCursor = data?.next_cursor ?? "";
      principalsHasMore = Boolean(data?.next_cursor);
      principalsState = { status: SECTION_READY, error: "" };
    } else {
      principalsState = {
        status: SECTION_ERROR,
        error: extractErrorMessage(
          principalsResult.reason,
          "Failed to load principals",
        ),
      };
    }

    if (invitesResult.status === "fulfilled") {
      invites = invitesResult.value?.invites ?? [];
      invitesState = { status: SECTION_READY, error: "" };
    } else {
      invitesState = {
        status: SECTION_ERROR,
        error: extractErrorMessage(
          invitesResult.reason,
          "Failed to load invites",
        ),
      };
    }

    if (auditResult.status === "fulfilled") {
      const data = auditResult.value;
      auditEvents = data?.events ?? [];
      auditCursor = data?.next_cursor ?? "";
      auditHasMore = Boolean(data?.next_cursor);
      auditState = { status: SECTION_READY, error: "" };
    } else {
      auditState = {
        status: SECTION_ERROR,
        error: extractErrorMessage(
          auditResult.reason,
          "Failed to load audit events",
        ),
      };
    }

    loading = false;
  }

  async function loadMorePrincipals() {
    if (loadingMorePrincipals || !principalsCursor) return;
    loadingMorePrincipals = true;

    try {
      const result = await coreClient.listPrincipals({
        limit: 50,
        cursor: principalsCursor,
      });
      const newPrincipals = result?.principals ?? [];
      principals = [...principals, ...newPrincipals];
      principalsCursor = result?.next_cursor ?? "";
      principalsHasMore = Boolean(result?.next_cursor);
    } catch (error) {
      pageError = extractErrorMessage(error, "Failed to load more principals");
    } finally {
      loadingMorePrincipals = false;
    }
  }

  async function loadMoreAudit() {
    if (loadingMoreAudit || !auditCursor) return;
    loadingMoreAudit = true;

    try {
      const result = await coreClient.listAuthAudit({
        limit: 50,
        cursor: auditCursor,
      });
      const newEvents = result?.events ?? [];
      auditEvents = [...auditEvents, ...newEvents];
      auditCursor = result?.next_cursor ?? "";
      auditHasMore = Boolean(result?.next_cursor);
    } catch (error) {
      pageError = extractErrorMessage(
        error,
        "Failed to load more audit events",
      );
    } finally {
      loadingMoreAudit = false;
    }
  }

  async function handleCreateInvite() {
    creatingInvite = true;
    inviteError = "";
    createdToken = "";
    tokenCopied = false;
    tokenDismissed = false;

    try {
      const payload = {
        kind: newInviteKind,
      };
      if (newInviteNote.trim()) {
        payload.note = newInviteNote.trim();
      }
      const result = await coreClient.createInvite(payload);
      createdToken = result.token ?? "";
      newInviteNote = "";
      await loadAccessData();
    } catch (error) {
      inviteError = extractErrorMessage(error, "Failed to create invite");
    } finally {
      creatingInvite = false;
    }
  }

  async function handleRevokeInvite(inviteId) {
    if (!inviteId) return;
    revokingInviteId = inviteId;
    revokeError = "";

    try {
      await coreClient.revokeInvite(inviteId);
      await loadAccessData();
    } catch (error) {
      revokeError = extractErrorMessage(error, "Failed to revoke invite");
    } finally {
      revokingInviteId = "";
    }
  }

  function startPrincipalRevoke(principal) {
    principalRevokeTarget = principal;
    principalRevokeConfirming = false;
    principalRevokeForcing = false;
    principalRevokeError = "";
    principalRevokeRequiresForce = false;
  }

  function cancelPrincipalRevoke() {
    principalRevokeTarget = null;
    principalRevokeConfirming = false;
    principalRevokeForcing = false;
    principalRevokeError = "";
    principalRevokeRequiresForce = false;
  }

  async function confirmPrincipalRevoke() {
    if (!principalRevokeTarget) return;

    const agentId = principalRevokeTarget.agent_id;
    principalRevokeConfirming = true;
    principalRevokeError = "";
    principalRevokeRequiresForce = false;

    try {
      await coreClient.revokePrincipal(agentId, {});
      cancelPrincipalRevoke();
      await loadAccessData();
    } catch (error) {
      const details = error?.details ?? "";
      if (details.includes("last_active_principal") || error?.status === 409) {
        principalRevokeRequiresForce = true;
        principalRevokeConfirming = false;
      } else {
        principalRevokeError = extractErrorMessage(
          error,
          "Failed to revoke principal",
        );
        principalRevokeConfirming = false;
      }
    }
  }

  async function forcePrincipalRevoke() {
    if (!principalRevokeTarget) return;

    principalRevokeForcing = true;
    principalRevokeError = "";

    try {
      await coreClient.revokePrincipal(principalRevokeTarget.agent_id, {
        force_last_active: true,
      });
      cancelPrincipalRevoke();
      await loadAccessData();
    } catch (error) {
      principalRevokeError = extractErrorMessage(
        error,
        "Failed to revoke principal",
      );
      principalRevokeForcing = false;
    }
  }

  async function copyTokenToClipboard() {
    if (!createdToken) return;
    try {
      await navigator.clipboard.writeText(createdToken);
      tokenCopied = true;
    } catch {
      tokenCopied = false;
    }
  }

  function dismissToken() {
    tokenDismissed = true;
    createdToken = "";
  }

  function extractErrorMessage(error, fallback) {
    if (!error) return fallback;
    if (typeof error === "string") return error || fallback;
    if (error instanceof Error) return error.message || fallback;
    if (error.details) return error.details;
    return fallback;
  }

  function workspaceHref(pathname = "/") {
    return workspacePath(workspaceSlug, pathname);
  }

  function principalBadge(principal) {
    if (principal?.revoked) {
      return { label: "Revoked", class: "bg-red-500/10 text-red-400" };
    }
    return { label: "Active", class: "bg-emerald-500/10 text-emerald-400" };
  }

  function inviteBadge(invite) {
    if (invite?.revoked_at) {
      return { label: "Revoked", class: "bg-red-500/10 text-red-400" };
    }
    if (invite?.consumed_at) {
      return { label: "Consumed", class: "bg-blue-500/10 text-blue-400" };
    }
    return { label: "Pending", class: "bg-amber-500/10 text-amber-400" };
  }

  function principalLabel(principal) {
    const parts = [];
    if (principal?.username) {
      parts.push(principal.username);
    }
    const kind = principal?.principal_kind ?? "principal";
    const method = principal?.auth_method ?? "auth";
    parts.push(`${kind} via ${method}`);
    return parts.join(" \u2022 ");
  }

  function auditActorLabel(event) {
    const username = event?.actor_username;
    const agentId = event?.actor_agent_id;
    const actorId = event?.actor_actor_id;
    if (username) {
      return { primary: username, secondary: agentId ?? actorId };
    }
    const id = agentId ?? actorId ?? "unknown";
    return { primary: id, secondary: null };
  }

  function auditSubjectLabel(event) {
    const username = event?.subject_username;
    const agentId = event?.subject_agent_id;
    const actorId = event?.subject_actor_id;
    if (username) {
      return { primary: username, secondary: agentId ?? actorId };
    }
    const id = agentId ?? actorId;
    return { primary: id ?? null, secondary: null };
  }

  function auditEventDescription(event) {
    const kind = event?.event_type ?? "";
    const actor = auditActorLabel(event);
    const subject = auditSubjectLabel(event);
    const inviteId = event?.invite_id;
    const inviteLabel = inviteId ? inviteId.slice(0, 12) + "\u2026" : "invite";

    const actorDisplay = actor.primary;
    const subjectDisplay = subject.primary ?? actor.primary;

    switch (kind) {
      case "bootstrap_consumed":
        return `Bootstrap consumed by ${subjectDisplay}`;
      case "principal_registered":
        return `Principal ${subjectDisplay} registered`;
      case "invite_created":
        return `${inviteLabel} created by ${actorDisplay}`;
      case "invite_consumed":
        return `${inviteLabel} consumed by ${subjectDisplay}`;
      case "invite_revoked":
        return `${inviteLabel} revoked by ${actorDisplay}`;
      case "principal_revoked":
        return `Principal ${subjectDisplay} revoked by ${actorDisplay}`;
      case "principal_self_revoked":
        return `Principal ${subjectDisplay} self-revoked`;
      default:
        return `${kind || "unknown"} (${actorDisplay})`;
    }
  }

  function auditEventSecondary(event) {
    const actor = auditActorLabel(event);
    const subject = auditSubjectLabel(event);
    const parts = [];

    if (actor.secondary) {
      parts.push(`actor: ${actor.secondary}`);
    }
    if (subject.secondary && subject.secondary !== actor.secondary) {
      parts.push(`subject: ${subject.secondary}`);
    }
    if (event?.event_id) {
      parts.push(`id: ${event.event_id}`);
    }

    return parts.join(" \u2022 ");
  }
</script>

<svelte:head>
  <title>Access - {workspaceSlug} - OAR</title>
</svelte:head>

{#if !canManageAccess}
  <main class="space-y-4">
    <div class="flex items-baseline justify-between gap-4">
      <div>
        <h1 class="text-lg font-semibold text-[var(--ui-text)]">Access</h1>
        <p class="mt-0.5 text-[13px] text-[var(--ui-text-muted)]">
          Manage workspace access and invitations
        </p>
      </div>
    </div>

    <div
      class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-4 py-10 text-center text-[13px] text-[var(--ui-text-muted)]"
    >
      <p>Sign in with a passkey to manage workspace access.</p>
      <p class="mt-2">
        <a
          class="text-indigo-400 hover:text-indigo-300"
          href={workspaceHref("/login")}
        >
          Go to sign in
        </a>
      </p>
    </div>
  </main>
{:else}
  <main class="space-y-6">
    <div class="flex items-baseline justify-between gap-4">
      <div>
        <h1 class="text-lg font-semibold text-[var(--ui-text)]">Access</h1>
        <p class="mt-0.5 text-[13px] text-[var(--ui-text-muted)]">
          Manage workspace access, principals, and invitations
        </p>
      </div>
      <button
        class="cursor-pointer rounded-md border border-[var(--ui-border)] px-2.5 py-1.5 text-[13px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)]"
        onclick={loadAccessData}
        type="button"
      >
        Refresh
      </button>
    </div>

    {#if loading}
      <div
        class="flex items-center gap-2 py-6 text-[13px] text-[var(--ui-text-muted)]"
      >
        <svg class="h-3.5 w-3.5 animate-spin" fill="none" viewBox="0 0 24 24">
          <circle
            class="opacity-25"
            cx="12"
            cy="12"
            r="10"
            stroke="currentColor"
            stroke-width="4"
          ></circle>
          <path
            class="opacity-75"
            fill="currentColor"
            d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
          ></path>
        </svg>
        Loading...
      </div>
    {/if}

    {#if createdToken && !tokenDismissed}
      <div
        class="rounded-md border border-emerald-500/30 bg-emerald-500/10 px-4 py-3"
      >
        <div class="flex items-start gap-3">
          <div class="flex-1">
            <p class="text-[13px] font-medium text-emerald-400">
              Invite created successfully
            </p>
            <p class="mt-1 text-[11px] text-[var(--ui-text-muted)]">
              This one-time token will not be shown again. Copy it now.
            </p>
            <div
              class="mt-2 flex items-center gap-2 rounded bg-black/20 px-2 py-1.5 font-mono text-[11px] text-[var(--ui-text)]"
            >
              <span class="flex-1 break-all">{createdToken}</span>
              <button
                class="shrink-0 cursor-pointer rounded px-2 py-0.5 text-[10px] font-medium text-emerald-400 hover:bg-emerald-400/10"
                onclick={copyTokenToClipboard}
                type="button"
              >
                {tokenCopied ? "Copied" : "Copy"}
              </button>
            </div>
          </div>
          <button
            aria-label="Dismiss token banner"
            class="shrink-0 cursor-pointer text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]"
            onclick={dismissToken}
            type="button"
          >
            <svg
              class="h-4 w-4"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M6 18L18 6M6 6l12 12"
              />
            </svg>
          </button>
        </div>
      </div>
    {/if}

    {#if pageError}
      <div
        class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400"
        role="alert"
      >
        {pageError}
      </div>
    {/if}

    <section>
      <h2 class="mb-2 text-[13px] font-semibold text-[var(--ui-text)]">
        Bootstrap status
      </h2>
      {#if bootstrapState.status === SECTION_ERROR}
        <p class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
          {bootstrapState.error}
        </p>
      {:else if bootstrapState.status === SECTION_READY}
        <div
          class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2"
        >
          <p class="text-[13px] text-[var(--ui-text)]">
            {#if bootstrapStatus?.bootstrap_registration_available}
              <span class="font-medium text-emerald-400"
                >Bootstrap registration is available</span
              >
              - the first principal can register with a bootstrap token.
            {:else}
              <span class="font-medium text-amber-400"
                >Bootstrap registration is closed</span
              >
              - new principals must be invited by an existing operator.
            {/if}
          </p>
        </div>
      {/if}
    </section>

    <section>
      <h2 class="mb-2 text-[13px] font-semibold text-[var(--ui-text)]">
        Create invite
      </h2>
      <div
        class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-4 py-3"
      >
        {#if inviteError}
          <p
            class="mb-3 rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
          >
            {inviteError}
          </p>
        {/if}
        {#if revokeError}
          <p
            class="mb-3 rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
          >
            {revokeError}
          </p>
        {/if}
        <form
          class="flex flex-wrap items-end gap-3"
          onsubmit={(event) => {
            event.preventDefault();
            handleCreateInvite();
          }}
        >
          <div class="flex-1 min-w-[200px]">
            <label
              class="mb-1 block text-[11px] font-medium text-[var(--ui-text-muted)]"
              for="invite-kind"
            >
              Kind
            </label>
            <select
              bind:value={newInviteKind}
              class="w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-2 py-1.5 text-[13px] text-[var(--ui-text)]"
              id="invite-kind"
            >
              <option value="agent">Agent</option>
              <option value="human">Human</option>
              <option value="any">Any</option>
            </select>
          </div>
          <div class="flex-[2] min-w-[240px]">
            <label
              class="mb-1 block text-[11px] font-medium text-[var(--ui-text-muted)]"
              for="invite-note"
            >
              Note (optional)
            </label>
            <input
              bind:value={newInviteNote}
              class="w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-2 py-1.5 text-[13px] text-[var(--ui-text)]"
              id="invite-note"
              placeholder="e.g. 'ops bot for CI'"
              type="text"
            />
          </div>
          <button
            class="cursor-pointer rounded-md bg-indigo-600 px-3 py-1.5 text-[13px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
            disabled={creatingInvite}
            type="submit"
          >
            {creatingInvite ? "Creating..." : "Create invite"}
          </button>
        </form>
      </div>
    </section>

    <section>
      <h2 class="mb-2 text-[13px] font-semibold text-[var(--ui-text)]">
        Invites
      </h2>
      {#if invitesState.status === SECTION_ERROR}
        <p class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
          {invitesState.error}
        </p>
      {:else if invitesState.status === SECTION_READY}
        {#if invites.length === 0}
          <p
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-4 text-[13px] text-[var(--ui-text-muted)]"
          >
            No invites yet. Create one above to onboard new principals.
          </p>
        {:else}
          <div
            class="space-y-px rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
          >
            {#each invites as invite, i}
              {@const badge = inviteBadge(invite)}
              <div
                class="flex items-center gap-3 px-3 py-2.5 {i > 0
                  ? 'border-t border-[var(--ui-border)]'
                  : ''}"
              >
                <span
                  class="shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium {badge.class}"
                >
                  {badge.label}
                </span>
                <div class="min-w-0 flex-1">
                  <p
                    class="truncate text-[13px] font-medium text-[var(--ui-text)]"
                  >
                    {invite.id}
                  </p>
                  <p class="text-[11px] text-[var(--ui-text-muted)]">
                    {invite.kind} - {invite.note || "No note"}
                  </p>
                </div>
                <span class="text-[11px] text-[var(--ui-text-muted)]">
                  {formatTimestamp(invite.created_at)}
                </span>
                {#if !invite.revoked_at && !invite.consumed_at}
                  <button
                    class="shrink-0 cursor-pointer rounded px-2 py-1 text-[11px] font-medium text-red-400 hover:bg-red-400/10 disabled:opacity-50"
                    disabled={revokingInviteId === invite.id}
                    onclick={() => handleRevokeInvite(invite.id)}
                    type="button"
                  >
                    {revokingInviteId === invite.id ? "Revoking..." : "Revoke"}
                  </button>
                {/if}
              </div>
            {/each}
          </div>
        {/if}
      {/if}
    </section>

    <section>
      <h2 class="mb-2 text-[13px] font-semibold text-[var(--ui-text)]">
        Principals
      </h2>
      {#if principalsState.status === SECTION_ERROR}
        <p class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
          {principalsState.error}
        </p>
      {:else if principalsState.status === SECTION_READY}
        {#if principals.length === 0}
          <p
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-4 text-[13px] text-[var(--ui-text-muted)]"
          >
            No principals found.
          </p>
        {:else}
          <div
            class="space-y-px rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
          >
            {#each principals as principal, i}
              {@const badge = principalBadge(principal)}
              <div
                class="flex items-center gap-3 px-3 py-2.5 {i > 0
                  ? 'border-t border-[var(--ui-border)]'
                  : ''}"
              >
                <span
                  class="shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium {badge.class}"
                >
                  {badge.label}
                </span>
                <div class="min-w-0 flex-1">
                  <p
                    class="truncate text-[13px] font-medium text-[var(--ui-text)]"
                  >
                    {principal.username || principal.agent_id}
                  </p>
                  <p class="text-[11px] text-[var(--ui-text-muted)]">
                    {principalLabel(principal)}
                  </p>
                  <p
                    class="mt-0.5 font-mono text-[10px] text-[var(--ui-text-muted)]"
                  >
                    {principal.agent_id}
                  </p>
                </div>
                <span class="text-[11px] text-[var(--ui-text-muted)]">
                  {formatTimestamp(principal.created_at)}
                </span>
                {#if !principal.revoked}
                  <button
                    class="shrink-0 cursor-pointer rounded px-2 py-1 text-[11px] font-medium text-red-400 hover:bg-red-400/10 disabled:opacity-50"
                    disabled={principalRevokeConfirming ||
                      principalRevokeForcing}
                    onclick={() => startPrincipalRevoke(principal)}
                    type="button"
                  >
                    Revoke
                  </button>
                {/if}
              </div>
            {/each}
          </div>
          {#if principalsHasMore}
            <div class="mt-2 flex justify-center">
              <button
                class="cursor-pointer rounded-md border border-[var(--ui-border)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)] disabled:opacity-50"
                disabled={loadingMorePrincipals}
                onclick={loadMorePrincipals}
                type="button"
              >
                {loadingMorePrincipals ? "Loading..." : "Load more"}
              </button>
            </div>
          {/if}
        {/if}
      {/if}
    </section>

    {#if principalRevokeTarget}
      <div
        class="rounded-md border border-red-500/30 bg-red-500/10 px-4 py-3"
        role="alert"
      >
        <div class="flex items-start gap-3">
          <div class="flex-1">
            {#if principalRevokeRequiresForce}
              <p class="text-[13px] font-medium text-red-400">
                Warning: This is the last active principal
              </p>
              <p class="mt-1 text-[11px] text-[var(--ui-text-muted)]">
                Revoking the last active principal will lock everyone out of
                this workspace. Only proceed if you have a recovery path (e.g.
                bootstrap token or direct database access).
              </p>
              <p class="mt-1 text-[11px] text-[var(--ui-text-muted)]">
                Principal: <strong
                  >{principalRevokeTarget.username ||
                    principalRevokeTarget.agent_id}</strong
                >
              </p>
            {:else}
              <p class="text-[13px] font-medium text-red-400">
                Confirm revoke principal?
              </p>
              <p class="mt-1 text-[11px] text-[var(--ui-text-muted)]">
                This will revoke access for <strong
                  >{principalRevokeTarget.username ||
                    principalRevokeTarget.agent_id}</strong
                >. This action is audit-logged.
              </p>
            {/if}
            {#if principalRevokeError}
              <p
                class="mt-2 rounded bg-red-500/20 px-2 py-1 text-[11px] text-red-300"
              >
                {principalRevokeError}
              </p>
            {/if}
            <div class="mt-3 flex items-center gap-2">
              {#if principalRevokeRequiresForce}
                <button
                  class="cursor-pointer rounded bg-red-600 px-3 py-1.5 text-[12px] font-medium text-white hover:bg-red-500 disabled:opacity-50"
                  disabled={principalRevokeForcing}
                  onclick={forcePrincipalRevoke}
                  type="button"
                >
                  {principalRevokeForcing
                    ? "Revoking..."
                    : "Force revoke (break-glass)"}
                </button>
              {:else}
                <button
                  class="cursor-pointer rounded bg-red-600 px-3 py-1.5 text-[12px] font-medium text-white hover:bg-red-500 disabled:opacity-50"
                  disabled={principalRevokeConfirming}
                  onclick={confirmPrincipalRevoke}
                  type="button"
                >
                  {principalRevokeConfirming ? "Revoking..." : "Confirm revoke"}
                </button>
              {/if}
              <button
                class="cursor-pointer rounded border border-[var(--ui-border)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)]"
                onclick={cancelPrincipalRevoke}
                type="button"
              >
                Cancel
              </button>
            </div>
          </div>
          <button
            aria-label="Dismiss confirmation"
            class="shrink-0 cursor-pointer text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]"
            onclick={cancelPrincipalRevoke}
            type="button"
          >
            <svg
              class="h-4 w-4"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M6 18L18 6M6 6l12 12"
              />
            </svg>
          </button>
        </div>
      </div>
    {/if}

    <section>
      <h2 class="mb-2 text-[13px] font-semibold text-[var(--ui-text)]">
        Recent auth events
      </h2>
      {#if auditState.status === SECTION_ERROR}
        <p class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
          {auditState.error}
        </p>
      {:else if auditState.status === SECTION_READY}
        {#if auditEvents.length === 0}
          <p
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-4 text-[13px] text-[var(--ui-text-muted)]"
          >
            No audit events yet.
          </p>
        {:else}
          <div
            class="space-y-px rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
          >
            {#each auditEvents as event, i}
              <div
                class="flex items-center gap-3 px-3 py-2.5 {i > 0
                  ? 'border-t border-[var(--ui-border)]'
                  : ''}"
              >
                <div class="min-w-0 flex-1">
                  <p
                    class="truncate text-[13px] font-medium text-[var(--ui-text)]"
                  >
                    {auditEventDescription(event)}
                  </p>
                  <p class="text-[11px] text-[var(--ui-text-muted)]">
                    {auditEventSecondary(event)}
                  </p>
                </div>
                <span class="text-[11px] text-[var(--ui-text-muted)]">
                  {formatTimestamp(event.occurred_at)}
                </span>
              </div>
            {/each}
          </div>
          {#if auditHasMore}
            <div class="mt-2 flex justify-center">
              <button
                class="cursor-pointer rounded-md border border-[var(--ui-border)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)] disabled:opacity-50"
                disabled={loadingMoreAudit}
                onclick={loadMoreAudit}
                type="button"
              >
                {loadingMoreAudit ? "Loading..." : "Load more"}
              </button>
            </div>
          {/if}
        {/if}
      {/if}
    </section>
  </main>
{/if}
