<script>
  import { page } from "$app/stores";

  import { authenticatedAgent } from "$lib/authSession";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import CopyButton from "$lib/components/CopyButton.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatAbsoluteDateTime, formatTimestamp } from "$lib/formatDate";
  import { buildRegistrationMessage } from "$lib/inviteRegistrationMessage";
  import { buildWakeRegistrationMessage } from "$lib/wakeRegistrationMessage.js";
  import { enrichPrincipalsWithWakeRouting as loadPrincipalsWithWakeRouting } from "$lib/principalWakeRouting.js";
  import { workspacePath } from "$lib/workspacePaths";

  let { data } = $props();

  let loading = $state(true);
  let pageError = $state("");
  let workspaceSlug = $derived($page.params.workspace);

  let principals = $state([]);
  let activeHumanPrincipalCount = $state(0);
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
  let newInviteKind = $state("agent");
  let newInviteAgentName = $state("");
  let newInviteUsername = $state("");

  let createdToken = $state("");
  let createdInviteKind = $state("");
  let createdInviteAgentName = $state("");
  let createdInviteUsername = $state("");
  let tokenCopied = $state(false);
  let messageCopied = $state(false);
  let tokenDismissed = $state(false);

  let revokingInviteId = $state("");
  let revokeInviteConfirm = $state({ open: false, id: "" });
  let revokeError = $state("");
  let showResolvedInvites = $state(false);

  let principalRevokeTarget = $state(null);
  let principalRevokeConfirming = $state(false);
  let principalRevokeForcing = $state(false);
  let principalRevokeError = $state("");
  let principalRevokeTypedConfirmation = $state("");
  let principalRevokeHumanLockoutReason = $state("");
  let principalRevokeRequiresHumanLockout = $state(false);

  const SECTION_IDLE = "idle";
  const SECTION_READY = "ready";
  const SECTION_ERROR = "error";

  let principalsState = $state({ status: SECTION_IDLE, error: "" });
  let invitesState = $state({ status: SECTION_IDLE, error: "" });
  let auditState = $state({ status: SECTION_IDLE, error: "" });

  let canManageAccess = $derived(Boolean($authenticatedAgent));
  let authenticatedAgentId = $derived($authenticatedAgent?.agent_id ?? "");

  let pendingInvites = $derived(
    invites.filter((i) => !i.revoked_at && !i.consumed_at),
  );
  let resolvedInvites = $derived(
    invites.filter((i) => i.revoked_at || i.consumed_at),
  );
  let visibleInvites = $derived(showResolvedInvites ? invites : pendingInvites);

  let wakePopoverTarget = $state(null);
  let wakeRegistrationMessageCopiedFor = $state("");

  $effect(() => {
    if (!canManageAccess) return;
    loadAccessData();
  });

  async function loadAccessData() {
    loading = true;
    pageError = "";

    const [principalsResult, invitesResult, auditResult] =
      await Promise.allSettled([
        coreClient.listPrincipals({ limit: 50 }),
        coreClient.listInvites(),
        coreClient.listAuthAudit({ limit: 50 }),
      ]);

    if (principalsResult.status === "fulfilled") {
      const data = principalsResult.value;
      principals = await enrichPrincipalsWithWakeRouting(
        data?.principals ?? [],
      );
      activeHumanPrincipalCount = data?.active_human_principal_count ?? 0;
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
      const newPrincipals = await enrichPrincipalsWithWakeRouting(
        result?.principals ?? [],
      );
      principals = [...principals, ...newPrincipals];
      activeHumanPrincipalCount =
        result?.active_human_principal_count ?? activeHumanPrincipalCount;
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
    createdInviteKind = "";
    tokenCopied = false;
    messageCopied = false;
    tokenDismissed = false;

    try {
      const payload = {
        kind: newInviteKind,
      };
      const result = await coreClient.createInvite(payload);
      createdToken = result.token ?? "";
      createdInviteKind = newInviteKind;
      createdInviteAgentName = newInviteAgentName.trim();
      createdInviteUsername = newInviteUsername.trim();
      newInviteAgentName = "";
      newInviteUsername = "";
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
    if (!principal?.agent_id || principal.agent_id === authenticatedAgentId) {
      return;
    }
    principalRevokeTarget = principal;
    principalRevokeConfirming = false;
    principalRevokeForcing = false;
    principalRevokeError = "";
    principalRevokeTypedConfirmation = "";
    principalRevokeHumanLockoutReason = "";
    principalRevokeRequiresHumanLockout = isLastActiveHumanPrincipal(principal);
  }

  function cancelPrincipalRevoke() {
    principalRevokeTarget = null;
    principalRevokeConfirming = false;
    principalRevokeForcing = false;
    principalRevokeError = "";
    principalRevokeTypedConfirmation = "";
    principalRevokeHumanLockoutReason = "";
    principalRevokeRequiresHumanLockout = false;
  }

  async function confirmPrincipalRevoke() {
    if (!principalRevokeTarget || principalRevokeRequiresHumanLockout) return;

    const agentId = principalRevokeTarget.agent_id;
    principalRevokeConfirming = true;
    principalRevokeError = "";

    try {
      await coreClient.revokePrincipal(agentId, {});
      cancelPrincipalRevoke();
      await loadAccessData();
    } catch (error) {
      const details = error?.details ?? "";
      if (details.includes("last_active_principal") || error?.status === 409) {
        principalRevokeRequiresHumanLockout = true;
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
    if (!principalRevokeTarget || !principalRevokeRequiresHumanLockout) return;
    if (
      principalRevokeTypedConfirmation.trim() !==
        principalRevokeTarget.agent_id ||
      principalRevokeHumanLockoutReason.trim() === ""
    ) {
      principalRevokeError =
        "Type the agent ID and provide a human-lockout reason before using break-glass revoke.";
      return;
    }

    principalRevokeForcing = true;
    principalRevokeError = "";

    try {
      await coreClient.revokePrincipal(principalRevokeTarget.agent_id, {
        allow_human_lockout: true,
        human_lockout_reason: principalRevokeHumanLockoutReason.trim(),
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

  async function copyRegistrationMessage() {
    if (!createdToken) return;
    try {
      await navigator.clipboard.writeText(
        buildRegistrationMessage(
          createdToken,
          data.registrationBaseUrl,
          createdInviteAgentName,
          createdInviteUsername,
        ),
      );
      messageCopied = true;
    } catch {
      messageCopied = false;
    }
  }

  function dismissToken() {
    tokenDismissed = true;
    createdToken = "";
    createdInviteKind = "";
    createdInviteAgentName = "";
    createdInviteUsername = "";
  }

  function extractErrorMessage(error, fallback) {
    if (!error) return fallback;
    if (typeof error === "string") return error || fallback;
    if (error instanceof Error) return error.message || fallback;
    if (error.details) return error.details;
    return fallback;
  }

  async function enrichPrincipalsWithWakeRouting(principalList) {
    return loadPrincipalsWithWakeRouting(principalList, {
      workspaceBindingTarget: data?.workspaceId ?? "",
      client: coreClient,
    });
  }

  async function copyWakeRegistrationMessage(principal) {
    const handle = String(
      principal?.wakeRouting?.handle ?? principal?.username ?? "",
    ).trim();
    if (!handle) return;
    try {
      await navigator.clipboard.writeText(
        buildWakeRegistrationMessage(
          data?.registrationBaseUrl,
          data?.workspaceId,
          handle,
        ),
      );
      wakeRegistrationMessageCopiedFor = principal?.agent_id ?? "";
    } catch {
      wakeRegistrationMessageCopiedFor = "";
    }
  }

  function workspaceHref(pathname = "/") {
    return workspacePath(workspaceSlug, pathname);
  }

  function principalBadge(principal) {
    if (principal?.revoked) {
      return { label: "Revoked", class: "bg-danger-soft text-danger-text" };
    }
    return null;
  }

  function inviteBadge(invite) {
    if (invite?.revoked_at) {
      return { label: "Revoked", class: "bg-danger-soft text-danger-text" };
    }
    if (invite?.consumed_at) {
      return { label: "Consumed", class: "bg-blue-500/10 text-blue-400" };
    }
    return null;
  }

  function principalLabel(principal) {
    const kind = principal?.principal_kind ?? "principal";
    const method = principal?.auth_method ?? "auth";
    return `${kind} via ${method}`;
  }

  function isLastActiveHumanPrincipal(principal) {
    return Boolean(
      principal?.principal_kind === "human" &&
      !principal?.revoked &&
      activeHumanPrincipalCount === 1,
    );
  }

  function principalRevokeBreakGlassReady() {
    return Boolean(
      principalRevokeRequiresHumanLockout &&
      principalRevokeTarget &&
      principalRevokeTypedConfirmation.trim() ===
        principalRevokeTarget.agent_id &&
      principalRevokeHumanLockoutReason.trim() !== "",
    );
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
      case "principal_human_lockout_revoked":
        return `Principal ${subjectDisplay} revoked under human lockout by ${actorDisplay}`;
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

  function isCurrentPrincipal(principal) {
    return (
      Boolean(principal?.agent_id) &&
      principal.agent_id === authenticatedAgentId
    );
  }

  function toggleWakePopover(agentId) {
    wakePopoverTarget = wakePopoverTarget === agentId ? null : agentId;
    wakeRegistrationMessageCopiedFor = "";
  }

  function truncateId(id, maxLen = 20) {
    if (!id || id.length <= maxLen) return id ?? "";
    return id.slice(0, maxLen) + "\u2026";
  }
</script>

<svelte:head>
  <title>Access - {workspaceSlug} - ANX</title>
</svelte:head>

{#if !canManageAccess}
  <div class="space-y-4">
    <div class="flex items-baseline justify-between gap-4">
      <div>
        <h1 class="text-lg font-semibold text-[var(--fg)]">Access</h1>
        <p class="mt-0.5 text-[13px] text-[var(--fg-muted)]">
          Manage workspace access and invitations
        </p>
      </div>
    </div>

    <div
      class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-4 py-10 text-center text-[13px] text-[var(--fg-muted)]"
    >
      <p>Sign in with a passkey to manage workspace access.</p>
      <p class="mt-2">
        <a
          class="text-accent-text hover:text-accent-text"
          href={workspaceHref("/login")}
        >
          Go to sign in
        </a>
      </p>
    </div>
  </div>
{:else}
  <div class="space-y-6">
    <div class="flex items-baseline justify-between gap-4">
      <div>
        <h1 class="text-lg font-semibold text-[var(--fg)]">Access</h1>
        <p class="mt-0.5 text-[13px] text-[var(--fg-muted)]">
          Manage workspace access, principals, and invitations
        </p>
      </div>
      <button
        class="cursor-pointer rounded-md border border-[var(--line)] px-2.5 py-1.5 text-[13px] font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)]"
        onclick={loadAccessData}
        type="button"
      >
        Refresh
      </button>
    </div>

    {#if loading}
      <div
        class="flex items-center gap-2 py-6 text-[13px] text-[var(--fg-muted)]"
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
        class="rounded-md border border-ok/30 bg-ok-soft px-4 py-3"
      >
        <div class="flex items-start gap-3">
          <div class="flex-1">
            <p class="text-[13px] font-medium text-ok-text">
              Invite created successfully
            </p>
            <p class="mt-1 text-[11px] text-[var(--fg-muted)]">
              This one-time token will not be shown again. Copy it now.
            </p>
            <div
              class="mt-2 flex items-center gap-2 rounded bg-bg px-2 py-1.5 font-mono text-[11px] text-[var(--fg)]"
            >
              <span class="flex-1 break-all">{createdToken}</span>
              {#if createdInviteKind === "agent" || createdInviteKind === "any"}
                <button
                  class="shrink-0 cursor-pointer rounded px-2 py-1.5 text-[10px] font-medium text-ok-text hover:bg-ok-soft"
                  onclick={copyTokenToClipboard}
                  type="button"
                >
                  {tokenCopied ? "Copied" : "Copy token"}
                </button>
              {:else}
                <button
                  class="shrink-0 cursor-pointer rounded px-2 py-1.5 text-[10px] font-medium text-ok-text hover:bg-ok-soft"
                  onclick={copyTokenToClipboard}
                  type="button"
                >
                  {tokenCopied ? "Copied" : "Copy"}
                </button>
              {/if}
            </div>
            {#if createdInviteKind === "agent" || createdInviteKind === "any"}
              <button
                class="mt-2 cursor-pointer rounded border border-ok/30 px-3 py-1.5 text-[11px] font-medium text-ok-text hover:bg-ok-soft"
                onclick={copyRegistrationMessage}
                type="button"
              >
                {messageCopied ? "Copied" : "Copy registration message"}
              </button>
              <p class="mt-1.5 text-[11px] text-[var(--fg-muted)]">
                Copies a ready-to-paste command with instructions for your agent
                to register.
              </p>
            {/if}
          </div>
          <button
            aria-label="Dismiss token banner"
            class="shrink-0 cursor-pointer text-[var(--fg-muted)] hover:text-[var(--fg)]"
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
        class="rounded-md bg-danger-soft px-3 py-2 text-[13px] text-danger-text"
        role="alert"
      >
        {pageError}
      </div>
    {/if}

    <section>
      <h2 class="mb-2 text-[13px] font-semibold text-[var(--fg)]">
        Create invite
      </h2>
      <div
        class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-4 py-3"
      >
        {#if inviteError}
          <p
            class="mb-3 rounded-md bg-danger-soft px-3 py-2 text-[12px] text-danger-text"
          >
            {inviteError}
          </p>
        {/if}
        {#if revokeError}
          <p
            class="mb-3 rounded-md bg-danger-soft px-3 py-2 text-[12px] text-danger-text"
          >
            {revokeError}
          </p>
        {/if}
        <form
          onsubmit={(event) => {
            event.preventDefault();
            handleCreateInvite();
          }}
        >
          <div class="flex flex-wrap items-end gap-3">
            <div class="flex-1 min-w-[200px]">
              <label
                class="mb-1 block text-[11px] font-medium text-[var(--fg-muted)]"
                for="invite-kind"
              >
                Kind
              </label>
              <select
                bind:value={newInviteKind}
                class="w-full rounded-md border border-[var(--line)] bg-[var(--bg)] px-2 py-1.5 text-[13px] text-[var(--fg)]"
                id="invite-kind"
              >
                <option value="agent">Agent</option>
                <option value="human">Human</option>
                <option value="any">Any</option>
              </select>
            </div>
            {#if newInviteKind === "agent" || newInviteKind === "any"}
              <div class="flex-[2] min-w-[240px]">
                <label
                  class="mb-1 block text-[11px] font-medium text-[var(--fg-muted)]"
                  for="invite-agent-name"
                >
                  Agent name (optional)
                </label>
                <input
                  bind:value={newInviteAgentName}
                  class="w-full rounded-md border border-[var(--line)] bg-[var(--bg)] px-2 py-1.5 text-[13px] text-[var(--fg)]"
                  id="invite-agent-name"
                  placeholder="e.g. hermes-prod"
                  type="text"
                />
              </div>
              <div class="flex-[2] min-w-[240px]">
                <label
                  class="mb-1 block text-[11px] font-medium text-[var(--fg-muted)]"
                  for="invite-username"
                >
                  Username (optional)
                </label>
                <input
                  bind:value={newInviteUsername}
                  class="w-full rounded-md border border-[var(--line)] bg-[var(--bg)] px-2 py-1.5 text-[13px] text-[var(--fg)]"
                  id="invite-username"
                  placeholder="e.g. hermes.prod"
                  type="text"
                />
              </div>
            {/if}
            <button
              class="cursor-pointer rounded-md bg-accent px-3 py-1.5 text-[13px] font-medium text-white hover:bg-accent-hover disabled:opacity-50"
              disabled={creatingInvite}
              type="submit"
            >
              {creatingInvite ? "Creating..." : "Create invite"}
            </button>
          </div>
        </form>
      </div>
    </section>

    <section>
      <div class="mb-2 flex items-baseline justify-between gap-2">
        <h2 class="text-[13px] font-semibold text-[var(--fg)]">
          Invites
          {#if invitesState.status === SECTION_READY && pendingInvites.length > 0}
            <span class="ml-1 font-normal text-[var(--fg-muted)]"
              >{pendingInvites.length} pending</span
            >
          {/if}
        </h2>
        {#if resolvedInvites.length > 0}
          <button
            class="cursor-pointer text-[11px] font-medium text-[var(--accent)] hover:text-accent-text"
            onclick={() => (showResolvedInvites = !showResolvedInvites)}
            type="button"
          >
            {showResolvedInvites
              ? "Hide resolved"
              : `Show ${resolvedInvites.length} resolved`}
          </button>
        {/if}
      </div>
      {#if invitesState.status === SECTION_ERROR}
        <p class="rounded-md bg-danger-soft px-3 py-2 text-[13px] text-danger-text">
          {invitesState.error}
        </p>
      {:else if invitesState.status === SECTION_READY}
        {#if visibleInvites.length === 0}
          <p
            class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-4 text-[13px] text-[var(--fg-muted)]"
          >
            {invites.length === 0
              ? "No invites yet. Create one above to onboard new principals."
              : "No pending invites."}
          </p>
        {:else}
          <div
            class="space-y-px rounded-md border border-[var(--line)] bg-[var(--bg-soft)] overflow-hidden"
          >
            {#each visibleInvites as invite, i}
              {@const badge = inviteBadge(invite)}
              <div
                class="flex items-center gap-3 px-3 py-2 {i > 0
                  ? 'border-t border-[var(--line)]'
                  : ''} {invite.revoked_at || invite.consumed_at
                  ? 'opacity-60'
                  : ''}"
              >
                {#if badge}
                  <span
                    class="shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium {badge.class}"
                  >
                    {badge.label}
                  </span>
                {/if}
                <div class="min-w-0 flex-1">
                  <div class="flex items-center gap-1.5">
                    <p
                      class="truncate font-mono text-[12px] text-[var(--fg)]"
                      title={invite.id}
                    >
                      {invite.id.length > 14
                        ? `${invite.id.slice(0, 14)}…`
                        : invite.id}
                    </p>
                    <CopyButton value={invite.id} label="Copy invite ID" />
                  </div>
                  <p class="text-[11px] text-[var(--fg-muted)]">
                    {invite.kind}
                  </p>
                </div>
                <span
                  class="hidden text-[11px] text-[var(--fg-muted)] sm:inline"
                >
                  {formatTimestamp(invite.created_at)}
                </span>
                {#if !invite.revoked_at && !invite.consumed_at}
                  <button
                    class="shrink-0 cursor-pointer rounded px-2 py-1 text-[11px] font-medium text-danger-text hover:bg-danger-soft disabled:opacity-50"
                    disabled={revokingInviteId === invite.id}
                    onclick={() => {
                      revokeInviteConfirm = { open: true, id: invite.id };
                    }}
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
      <h2 class="mb-1 text-[13px] font-semibold text-[var(--fg)]">
        Principals
        {#if principalsState.status === SECTION_READY && principals.length > 0}
          <span class="ml-1 font-normal text-[var(--fg-muted)]"
            >{principals.length}</span
          >
        {/if}
      </h2>
      <p class="mb-2 text-[11px] text-[var(--fg-muted)]">
        Registered agents can be tagged from thread messages with
        <code class="rounded bg-[var(--line)] px-1 py-px text-[10px]"
          >@handle</code
        >.
        <span
          class="rounded bg-ok-soft px-1 py-px text-[10px] font-medium text-ok-text"
          >Online</span
        >
        agents have a fresh bridge check-in, while
        <span
          class="rounded bg-warn-soft px-1 py-px text-[10px] font-medium text-warn-text"
          >Offline</span
        >
        agents stay taggable and will receive wakes when they come back.
      </p>
      {#if principalsState.status === SECTION_ERROR}
        <p class="rounded-md bg-danger-soft px-3 py-2 text-[13px] text-danger-text">
          {principalsState.error}
        </p>
      {:else if principalsState.status === SECTION_READY}
        {#if principals.length === 0}
          <p
            class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-4 text-[13px] text-[var(--fg-muted)]"
          >
            No principals found.
          </p>
        {:else}
          <div
            class="space-y-px rounded-md border border-[var(--line)] bg-[var(--bg-soft)] overflow-hidden"
          >
            {#each principals as principal, i}
              {@const badge = principalBadge(principal)}
              {@const isCurrent = isCurrentPrincipal(principal)}
              <div
                class="group relative px-3 py-2 transition-colors hover:bg-[var(--line-subtle)] {i >
                0
                  ? 'border-t border-[var(--line)]'
                  : ''} {principal.revoked ? 'opacity-50' : ''}"
              >
                <div class="flex items-center gap-2.5 sm:gap-3">
                  {#if badge}
                    <span
                      class="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium {badge.class}"
                    >
                      {badge.label}
                    </span>
                  {/if}
                  <div class="min-w-0 flex-1">
                    <div class="flex items-center gap-1.5">
                      <span
                        class="truncate text-[13px] font-medium text-[var(--fg)]"
                      >
                        {principal.username ||
                          truncateId(principal.agent_id, 24)}
                      </span>
                      <span
                        class="hidden shrink-0 text-[11px] text-[var(--fg-muted)] sm:inline"
                      >
                        {principalLabel(principal)}
                      </span>
                      {#if principal.wakeRouting?.applicable && principal.wakeRouting.state !== "revoked"}
                        <button
                          class="relative shrink-0 cursor-pointer rounded px-1.5 py-0.5 text-[10px] font-medium {principal
                            .wakeRouting
                            .badgeClass} hover:brightness-125 focus:outline-none"
                          onclick={() => toggleWakePopover(principal.agent_id)}
                          type="button"
                          title={principal.wakeRouting.summary}
                        >
                          {principal.wakeRouting.badgeLabel}
                        </button>
                        {#if wakePopoverTarget === principal.agent_id}
                          <div
                            class="absolute left-12 top-full z-30 mt-1 w-72 rounded-md border border-[var(--line)] bg-[var(--panel)] px-3 py-2 sm:left-24"
                            style="box-shadow: var(--shadow-modal)"
                            role="tooltip"
                          >
                            <div class="flex items-start justify-between gap-2">
                              <div>
                                <p
                                  class="text-[11px] font-medium {principal
                                    .wakeRouting.badgeClass}"
                                >
                                  {principal.wakeRouting.badgeLabel}
                                </p>
                                <p
                                  class="mt-0.5 text-[11px] text-[var(--fg-muted)]"
                                >
                                  {principal.wakeRouting.summary}
                                </p>
                                {#if principal.wakeRouting.state === "unregistered"}
                                  <p
                                    class="mt-2 text-[11px] text-[var(--fg-muted)]"
                                  >
                                    Copy a sendable CLI setup snippet for this
                                    agent's existing ANX profile.
                                  </p>
                                  <button
                                    class="mt-2 cursor-pointer rounded border border-[var(--line)] px-2 py-1 text-[11px] font-medium text-[var(--fg)] hover:bg-[var(--line-subtle)]"
                                    onclick={() =>
                                      copyWakeRegistrationMessage(principal)}
                                    type="button"
                                  >
                                    {wakeRegistrationMessageCopiedFor ===
                                    principal.agent_id
                                      ? "Copied"
                                      : "Copy registration steps"}
                                  </button>
                                {/if}
                              </div>
                              <button
                                class="shrink-0 cursor-pointer text-[var(--fg-muted)] hover:text-[var(--fg)]"
                                onclick={() => (wakePopoverTarget = null)}
                                type="button"
                                aria-label="Close"
                              >
                                <svg
                                  class="h-3 w-3"
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
                      {/if}
                    </div>
                    <div
                      class="mt-0.5 flex min-w-0 items-center gap-1.5 text-[10px] text-[var(--fg-muted)]"
                    >
                      <span
                        class="min-w-0 flex-1 truncate font-mono"
                        title={principal.agent_id}
                      >
                        {principal.agent_id}
                      </span>
                      <span class="shrink-0">
                        <CopyButton
                          value={principal.agent_id}
                          label="Copy agent ID"
                        />
                      </span>
                    </div>
                  </div>
                  <div
                    class="hidden shrink-0 text-right text-[11px] leading-4 text-[var(--fg-muted)] sm:block"
                  >
                    <p title={formatAbsoluteDateTime(principal.created_at)}>
                      Joined {formatTimestamp(principal.created_at) || "\u2014"}
                    </p>
                    <p title={formatAbsoluteDateTime(principal.last_seen_at)}>
                      Last seen {formatTimestamp(principal.last_seen_at) ||
                        "\u2014"}
                    </p>
                  </div>
                  {#if !principal.revoked && !isCurrent}
                    {@const lastHuman = isLastActiveHumanPrincipal(principal)}
                    <button
                      class="shrink-0 cursor-pointer rounded px-2 py-1 text-[11px] font-medium text-danger-text hover:bg-danger-soft disabled:opacity-50"
                      disabled={principalRevokeConfirming ||
                        principalRevokeForcing}
                      onclick={() => startPrincipalRevoke(principal)}
                      type="button"
                    >
                      {lastHuman ? "Break glass" : "Revoke"}
                    </button>
                  {:else if !principal.revoked}
                    <span
                      class="shrink-0 rounded bg-[var(--line-subtle)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--fg-muted)]"
                    >
                      You
                    </span>
                  {/if}
                </div>
              </div>
            {/each}
          </div>
          {#if principalsHasMore}
            <div class="mt-2 flex justify-center">
              <button
                class="cursor-pointer rounded-md border border-[var(--line)] px-3 py-1.5 text-[12px] font-medium text-[var(--fg-muted)] hover:bg-[var(--line-subtle)] disabled:opacity-50"
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
        class="rounded-md border border-danger/30 bg-danger-soft px-4 py-3"
        role="alert"
      >
        <div class="flex items-start gap-3">
          <div class="flex-1">
            {#if principalRevokeRequiresHumanLockout}
              <p class="text-[13px] font-medium text-danger-text">
                Warning: this is the last active human principal
              </p>
              <p class="mt-1 text-[11px] text-[var(--fg-muted)]">
                Revoking it will lock every human principal out of this
                workspace. Type the agent ID and provide a reason before the
                break-glass path becomes available.
              </p>
              <p class="mt-1 text-[11px] text-[var(--fg-muted)]">
                Principal: <strong
                  >{principalRevokeTarget.username ||
                    principalRevokeTarget.agent_id}</strong
                >
              </p>
              <div class="mt-3 grid gap-3 sm:grid-cols-2">
                <div>
                  <label
                    class="mb-1 block text-[11px] font-medium text-[var(--fg-muted)]"
                    for="principal-lockout-confirmation"
                  >
                    Type agent ID to confirm
                  </label>
                  <input
                    bind:value={principalRevokeTypedConfirmation}
                    class="w-full rounded-md border border-[var(--line)] bg-[var(--bg)] px-2 py-1.5 font-mono text-[12px] text-[var(--fg)]"
                    id="principal-lockout-confirmation"
                    placeholder={principalRevokeTarget.agent_id}
                    type="text"
                  />
                </div>
                <div>
                  <label
                    class="mb-1 block text-[11px] font-medium text-[var(--fg-muted)]"
                    for="principal-lockout-reason"
                  >
                    Human lockout reason
                  </label>
                  <input
                    bind:value={principalRevokeHumanLockoutReason}
                    class="w-full rounded-md border border-[var(--line)] bg-[var(--bg)] px-2 py-1.5 text-[12px] text-[var(--fg)]"
                    id="principal-lockout-reason"
                    placeholder="Explain the recovery path"
                    type="text"
                  />
                </div>
              </div>
            {:else}
              <p class="text-[13px] font-medium text-danger-text">
                Confirm revoke principal?
              </p>
              <p class="mt-1 text-[11px] text-[var(--fg-muted)]">
                This will revoke access for <strong
                  >{principalRevokeTarget.username ||
                    principalRevokeTarget.agent_id}</strong
                >. This action is audit-logged.
              </p>
            {/if}
            {#if principalRevokeError}
              <p
                class="mt-2 rounded bg-danger-soft px-2 py-1 text-[11px] text-danger-text"
              >
                {principalRevokeError}
              </p>
            {/if}
            <div class="mt-3 flex items-center gap-2">
              {#if principalRevokeRequiresHumanLockout}
                <button
                  class="cursor-pointer rounded bg-danger px-3 py-1.5 text-[12px] font-medium text-white hover:bg-danger disabled:opacity-50"
                  disabled={principalRevokeForcing ||
                    !principalRevokeBreakGlassReady()}
                  onclick={forcePrincipalRevoke}
                  type="button"
                >
                  {principalRevokeForcing
                    ? "Revoking..."
                    : "Allow human lockout and revoke"}
                </button>
              {:else}
                <button
                  class="cursor-pointer rounded bg-danger px-3 py-1.5 text-[12px] font-medium text-white hover:bg-danger disabled:opacity-50"
                  disabled={principalRevokeConfirming}
                  onclick={confirmPrincipalRevoke}
                  type="button"
                >
                  {principalRevokeConfirming ? "Revoking..." : "Confirm revoke"}
                </button>
              {/if}
              <button
                class="cursor-pointer rounded border border-[var(--line)] px-3 py-1.5 text-[12px] font-medium text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]"
                onclick={cancelPrincipalRevoke}
                type="button"
              >
                Cancel
              </button>
            </div>
          </div>
          <button
            aria-label="Dismiss confirmation"
            class="shrink-0 cursor-pointer text-[var(--fg-muted)] hover:text-[var(--fg)]"
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
      <h2 class="mb-2 text-[13px] font-semibold text-[var(--fg)]">
        Recent auth events
      </h2>
      {#if auditState.status === SECTION_ERROR}
        <p class="rounded-md bg-danger-soft px-3 py-2 text-[13px] text-danger-text">
          {auditState.error}
        </p>
      {:else if auditState.status === SECTION_READY}
        {#if auditEvents.length === 0}
          <p
            class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-4 text-[13px] text-[var(--fg-muted)]"
          >
            No audit events yet.
          </p>
        {:else}
          <div
            class="space-y-px rounded-md border border-[var(--line)] bg-[var(--bg-soft)] overflow-hidden"
          >
            {#each auditEvents as event, i}
              <div
                class="flex items-center gap-3 px-3 py-2.5 {i > 0
                  ? 'border-t border-[var(--line)]'
                  : ''}"
              >
                <div class="min-w-0 flex-1">
                  <p
                    class="truncate text-[13px] font-medium text-[var(--fg)]"
                  >
                    {auditEventDescription(event)}
                  </p>
                  <p class="text-[11px] text-[var(--fg-muted)]">
                    {auditEventSecondary(event)}
                  </p>
                </div>
                <span class="text-[11px] text-[var(--fg-muted)]">
                  {formatTimestamp(event.occurred_at)}
                </span>
              </div>
            {/each}
          </div>
          {#if auditHasMore}
            <div class="mt-2 flex justify-center">
              <button
                class="cursor-pointer rounded-md border border-[var(--line)] px-3 py-1.5 text-[12px] font-medium text-[var(--fg-muted)] hover:bg-[var(--line-subtle)] disabled:opacity-50"
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
  </div>
{/if}

<ConfirmModal
  open={revokeInviteConfirm.open}
  title="Revoke invite"
  message="This invite will be revoked and can no longer be used to join the workspace."
  confirmLabel="Revoke"
  variant="danger"
  busy={revokingInviteId === revokeInviteConfirm.id}
  onconfirm={() => {
    void handleRevokeInvite(revokeInviteConfirm.id);
    revokeInviteConfirm = { open: false, id: "" };
  }}
  oncancel={() => {
    revokeInviteConfirm = { open: false, id: "" };
  }}
/>
