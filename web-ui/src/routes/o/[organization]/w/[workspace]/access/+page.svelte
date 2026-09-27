<script>
  import { onMount, tick } from "svelte";
  import { page } from "$app/stores";

  import {
    actorDisplayLabel,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import { refreshAgentRoster } from "$lib/agentRoster.js";
  import { liveAgentChanges } from "$lib/liveWorkspaceEvents.js";
  import { formatAge } from "$lib/agentPresence.js";
  import { describeAuthAuditEvent } from "$lib/authAuditModel.js";
  import { authenticatedAgent } from "$lib/authSession";
  import { coreClient } from "$lib/coreClient";
  import { formatAbsoluteDateTime } from "$lib/formatDate";
  import {
    isWorkspaceTourArrived,
    markWorkspaceTourArrived,
  } from "$lib/tourState";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import ActorAvatar from "$lib/components/ActorAvatar.svelte";
  import Button from "$lib/components/Button.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import CopyableId from "$lib/components/CopyableId.svelte";
  import CopyButton from "$lib/components/CopyButton.svelte";
  import HostCard from "$lib/components/access/HostCard.svelte";
  import HostEnrollmentRequest from "$lib/components/access/HostEnrollmentRequest.svelte";
  import HostEnrollmentTokens from "$lib/components/access/HostEnrollmentTokens.svelte";

  let { data } = $props();

  const PENDING_POLL_MS = 5_000;
  const AUDIT_PREVIEW = 8;

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let workspaceHref = $derived(
    bindWorkspaceHref(organizationSlug, workspaceSlug),
  );
  let canManageAccess = $derived(Boolean($authenticatedAgent));
  let authenticatedAgentId = $derived($authenticatedAgent?.agent_id ?? "");
  let hostedMode = $derived(
    data?.outOfWorkspaceMode === "hosted" ||
      $page.data?.shellCapabilities?.mode === "hosted",
  );
  let cliBaseUrl = $derived(data?.cliBaseUrl ?? "");
  let enrollCommand = $derived(
    `anx ${cliBaseUrl ? `--base-url ${cliBaseUrl} ` : ""}host enroll`,
  );

  let now = $state(Date.now());
  let loaded = $state(false);
  let pageError = $state("");

  /** Section state: each section loads and fails on its own. */
  let sections = $state({
    hosts: { status: "idle", error: "" },
    pending: { status: "idle", error: "" },
    tokens: { status: "idle", error: "" },
    principals: { status: "idle", error: "" },
    invites: { status: "idle", error: "" },
    audit: { status: "idle", error: "" },
  });

  let hosts = $state([]);
  let pending = $state([]);
  let tokens = $state([]);
  let principals = $state([]);
  let activeHumanPrincipalCount = $state(0);
  let invites = $state([]);
  let auditEvents = $state([]);
  let auditCursor = $state("");
  let loadingMoreAudit = $state(false);
  let showAllAudit = $state(false);

  let enrollOpen = $state(false);
  let showRevokedHosts = $state(false);
  let enrollmentBusy = $state({ id: "", action: "" });
  let enrollmentErrors = $state({});
  let enrollmentNotice = $state("");

  let creatingInvite = $state(false);
  let inviteError = $state("");
  let createdInviteToken = $state("");
  let revokingInviteId = $state("");
  let revokeInviteConfirm = $state({ open: false, id: "" });

  let principalRevokeTarget = $state(null);
  let principalRevokeBusy = $state(false);
  let principalRevokeError = $state("");
  let principalRevokeLockout = $state(false);
  let principalRevokeReason = $state("");

  let tourArrived = $state(false);
  let hostsSectionEl = $state(null);

  let activeHosts = $derived(hosts.filter((host) => !host.revoked_at));
  let revokedHosts = $derived(hosts.filter((host) => host.revoked_at));
  let hostById = $derived(new Map(hosts.map((host) => [host.id, host])));
  let hostAgentIds = $derived(
    new Set(hosts.flatMap((host) => (host.agents ?? []).map((a) => a.id))),
  );

  let humans = $derived(
    principals.filter((principal) => principal.principal_kind === "human"),
  );
  // Agents enrolled before hosts that were not adopted keep working until
  // revoked; they are the only agents listed outside a host.
  let standaloneAgents = $derived(
    principals.filter(
      (principal) =>
        principal.principal_kind === "agent" &&
        !principal.revoked &&
        !hostAgentIds.has(principal.agent_id),
    ),
  );
  let humanInvites = $derived(
    invites.filter((invite) => String(invite.kind) !== "agent"),
  );
  let pendingInvites = $derived(
    humanInvites.filter((invite) => !invite.revoked_at && !invite.consumed_at),
  );
  let visibleAudit = $derived(
    showAllAudit ? auditEvents : auditEvents.slice(0, AUDIT_PREVIEW),
  );
  let showTourBanner = $derived(
    tourArrived &&
      canManageAccess &&
      sections.hosts.status === "ready" &&
      activeHosts.length === 0,
  );

  function message(error, fallback) {
    if (!error) return fallback;
    if (typeof error === "string") return error || fallback;
    // Core's reason reads better than the transport-level message around it.
    if (typeof error.details === "string" && error.details.trim()) {
      return error.details;
    }
    if (error instanceof Error) return error.message || fallback;
    return fallback;
  }

  function nameFor(id) {
    const raw = String(id ?? "").trim();
    if (!raw) return "";
    const label = actorDisplayLabel(raw, $actorRegistry, $principalRegistry);
    return label && label !== raw ? label : "";
  }

  function principalName(principal) {
    return (
      nameFor(principal?.actor_id) ||
      nameFor(principal?.agent_id) ||
      principal?.username ||
      "Unnamed"
    );
  }

  function settle(key, result, apply) {
    if (result.status === "fulfilled") {
      apply(result.value);
      sections[key] = { status: "ready", error: "" };
    } else {
      sections[key] = {
        status: "error",
        error: message(result.reason, "This did not load."),
      };
    }
  }

  async function loadHosts() {
    const [hostsResult, pendingResult, tokensResult] = await Promise.allSettled(
      [
        coreClient.listHosts(),
        coreClient.listPendingHostEnrollments(),
        coreClient.listHostEnrollmentTokens(),
      ],
    );
    settle("hosts", hostsResult, (value) => {
      hosts = value?.hosts ?? [];
    });
    settle("pending", pendingResult, (value) => {
      pending = value?.enrollments ?? [];
    });
    settle("tokens", tokensResult, (value) => {
      tokens = value?.enrollment_tokens ?? [];
    });
  }

  async function loadPeople() {
    const [principalsResult, invitesResult, auditResult] =
      await Promise.allSettled([
        coreClient.listPrincipals({ limit: 200 }),
        coreClient.listInvites(),
        coreClient.listAuthAudit({ limit: 50 }),
      ]);
    settle("principals", principalsResult, (value) => {
      principals = value?.principals ?? [];
      activeHumanPrincipalCount = value?.active_human_principal_count ?? 0;
    });
    settle("invites", invitesResult, (value) => {
      invites = value?.invites ?? [];
    });
    settle("audit", auditResult, (value) => {
      auditEvents = value?.events ?? [];
      auditCursor = value?.next_cursor ?? "";
    });
  }

  async function loadAll() {
    await Promise.all([loadHosts(), loadPeople()]);
    loaded = true;
  }

  // An enrolling machine polls every few seconds; so does this list, so the
  // request shows up while the operator is looking at the page.
  async function pollPending() {
    try {
      const result = await coreClient.listPendingHostEnrollments();
      const next = result?.enrollments ?? [];
      const changed =
        next.length !== pending.length ||
        next.some((entry, index) => entry.id !== pending[index]?.id);
      pending = next;
      sections.pending = { status: "ready", error: "" };
      if (changed) {
        void loadHosts();
        void refreshAgentRoster();
      }
    } catch {
      // The section keeps its last state; the next poll tries again.
    }
  }

  async function decideEnrollment(enrollment, action) {
    enrollmentBusy = { id: enrollment.id, action };
    enrollmentErrors = { ...enrollmentErrors, [enrollment.id]: "" };
    try {
      if (action === "approve") {
        await coreClient.approveHostEnrollment(enrollment.id);
      } else {
        await coreClient.denyHostEnrollment(enrollment.id);
      }
      pending = pending.filter((entry) => entry.id !== enrollment.id);
      enrollmentNotice =
        action === "approve"
          ? `Approved ${enrollment.requested_slug}. It appears under Hosts once the machine finishes enrolling.`
          : `Denied ${enrollment.requested_slug}.`;
      await Promise.all([loadHosts(), loadPeople()]);
    } catch (error) {
      enrollmentErrors = {
        ...enrollmentErrors,
        [enrollment.id]: message(
          error,
          action === "approve"
            ? "The request was not approved."
            : "The request was not denied.",
        ),
      };
    } finally {
      enrollmentBusy = { id: "", action: "" };
    }
  }

  async function saveExclusions(host, names) {
    const result = await coreClient.patchHost(host.id, {
      excluded_names: names,
    });
    const updated = result?.host;
    if (updated) {
      hosts = hosts.map((entry) => (entry.id === updated.id ? updated : entry));
    }
    void refreshAgentRoster();
  }

  async function revokeHost(host) {
    await coreClient.revokeHost(host.id);
    await Promise.all([loadHosts(), loadPeople(), refreshAgentRoster()]);
  }

  async function createToken(payload) {
    const result = await coreClient.createHostEnrollmentToken(payload);
    await loadHosts();
    return result;
  }

  async function revokeToken(token) {
    await coreClient.revokeHostEnrollmentToken(token.id);
    await loadHosts();
  }

  async function inviteHuman() {
    creatingInvite = true;
    inviteError = "";
    createdInviteToken = "";
    try {
      const result = await coreClient.createInvite({ kind: "human" });
      createdInviteToken = result?.token ?? "";
      await loadPeople();
    } catch (error) {
      inviteError = message(error, "The invite was not created.");
    } finally {
      creatingInvite = false;
    }
  }

  async function revokeInvite(inviteId) {
    revokingInviteId = inviteId;
    try {
      await coreClient.revokeInvite(inviteId);
      await loadPeople();
    } catch (error) {
      pageError = message(error, "The invite was not revoked.");
    } finally {
      revokingInviteId = "";
    }
  }

  function startPrincipalRevoke(principal) {
    principalRevokeTarget = principal;
    principalRevokeError = "";
    principalRevokeReason = "";
    principalRevokeLockout = Boolean(
      principal?.principal_kind === "human" && activeHumanPrincipalCount === 1,
    );
  }

  function cancelPrincipalRevoke() {
    principalRevokeTarget = null;
    principalRevokeBusy = false;
    principalRevokeError = "";
    principalRevokeReason = "";
    principalRevokeLockout = false;
  }

  async function confirmPrincipalRevoke() {
    if (!principalRevokeTarget) return;
    if (principalRevokeLockout && !principalRevokeReason.trim()) return;
    principalRevokeBusy = true;
    principalRevokeError = "";
    try {
      await coreClient.revokePrincipal(
        principalRevokeTarget.agent_id,
        principalRevokeLockout
          ? {
              allow_human_lockout: true,
              human_lockout_reason: principalRevokeReason.trim(),
            }
          : {},
      );
      cancelPrincipalRevoke();
      await loadPeople();
    } catch (error) {
      const details = String(error?.details ?? "");
      if (
        !principalRevokeLockout &&
        (details.includes("last_active_principal") || error?.status === 409)
      ) {
        principalRevokeLockout = true;
        principalRevokeBusy = false;
        return;
      }
      principalRevokeError = message(error, "Access was not revoked.");
      principalRevokeBusy = false;
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
      auditEvents = [...auditEvents, ...(result?.events ?? [])];
      auditCursor = result?.next_cursor ?? "";
    } catch (error) {
      pageError = message(error, "More events did not load.");
    } finally {
      loadingMoreAudit = false;
    }
  }

  function auditSentence(event) {
    return describeAuthAuditEvent(event, {
      nameFor,
      hostName: (hostId) => hostById.get(hostId)?.slug ?? "",
    });
  }

  // `#host-<slug>` and `#host-requests` links (agent pages, the CLI's
  // verification URL) land on the right card once hosts have loaded.
  $effect(() => {
    if (!loaded) return;
    const hash = $page.url.hash;
    if (!hash) return;
    void tick().then(() => {
      document
        .getElementById(decodeURIComponent(hash.slice(1)))
        ?.scrollIntoView({ block: "start" });
    });
  });

  $effect(() => {
    if (!workspaceSlug) return;
    if ($page.url.searchParams.get("from") === "tour") {
      markWorkspaceTourArrived(workspaceSlug);
    }
    tourArrived = isWorkspaceTourArrived(workspaceSlug);
  });

  onMount(() => {
    if (!canManageAccess) return;
    void loadAll();
    // Host cards show agent states; core's roster stream says when they move.
    const stopAgentChanges = liveAgentChanges({
      client: coreClient,
      debounceMs: 600,
      onChange: () => loadHosts(),
    });
    const poll = setInterval(() => {
      now = Date.now();
      if (!document.hidden) void pollPending();
    }, PENDING_POLL_MS);
    return () => {
      clearInterval(poll);
      stopAgentChanges();
    };
  });
</script>

<svelte:head>
  <title>Access - {workspaceSlug} - ANX</title>
</svelte:head>

{#if !canManageAccess}
  <div class="space-y-4">
    <div>
      <h1 class="text-title text-fg">Access</h1>
      <p class="mt-0.5 hidden text-meta text-fg-muted sm:block">
        Machines, people and invitations for this workspace
      </p>
    </div>
    <div
      class="rounded-md border border-line bg-bg-soft px-4 py-10 text-center text-meta text-fg-muted"
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
  <div class="space-y-6 sm:space-y-8">
    <div>
      <h1 class="text-title text-fg">Access</h1>
      <p class="mt-0.5 hidden text-meta text-fg-muted sm:block">
        Machines, people and invitations for this workspace
      </p>
    </div>

    {#if pageError}
      <div
        class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
        role="alert"
      >
        {pageError}
      </div>
    {/if}

    {#if !loaded}
      <p class="text-meta text-fg-muted">Loading access…</p>
    {/if}

    {#if showTourBanner}
      <aside class="tour-arrival-banner" role="status" aria-live="polite">
        <div class="tour-arrival-banner__body">
          <p class="tour-arrival-banner__title">
            Last step: enroll the machine your agents run on
          </p>
          <p class="tour-arrival-banner__text">
            Run <code>{enrollCommand}</code> there, then approve the request that
            appears below. Every agent on that machine can use the workspace from
            then on, with no per-agent setup.
          </p>
        </div>
      </aside>
    {/if}

    {#if enrollmentNotice}
      <p
        class="flex items-center gap-3 rounded-md border border-line bg-bg-soft px-3 py-2 text-micro text-fg-muted"
        role="status"
        data-enrollment-notice
      >
        <span class="min-w-0 flex-1">{enrollmentNotice}</span>
        <button
          class="shrink-0 hover:text-fg"
          type="button"
          onclick={() => (enrollmentNotice = "")}>Dismiss</button
        >
      </p>
    {/if}

    {#if pending.length}
      <section
        id="host-requests"
        class="scroll-mt-20"
        aria-labelledby="host-requests-title"
      >
        <h2
          id="host-requests-title"
          class="mb-2 flex items-center gap-2 text-meta font-semibold text-fg"
        >
          <span class="h-2 w-2 rounded-full bg-warn" aria-hidden="true"></span>
          Waiting for approval
          <span class="font-normal text-fg-muted">{pending.length}</span>
        </h2>
        <ul
          class="divide-y divide-line-subtle overflow-hidden rounded-md border bg-bg-soft"
          style="border-color: color-mix(in srgb, var(--warn) 40%, var(--line))"
        >
          {#each pending as enrollment (enrollment.id)}
            <HostEnrollmentRequest
              {enrollment}
              {now}
              busy={enrollmentBusy.id === enrollment.id
                ? enrollmentBusy.action
                : ""}
              error={enrollmentErrors[enrollment.id] ?? ""}
              onapprove={(entry) => decideEnrollment(entry, "approve")}
              ondeny={(entry) => decideEnrollment(entry, "deny")}
            />
          {/each}
        </ul>
      </section>
    {:else if sections.pending.status === "error"}
      <p
        class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
      >
        Pending host requests did not load: {sections.pending.error}
      </p>
    {/if}

    <section
      id="hosts"
      bind:this={hostsSectionEl}
      class="scroll-mt-20"
      aria-labelledby="hosts-title"
    >
      <div class="mb-2 flex items-baseline justify-between gap-3">
        <h2 id="hosts-title" class="text-meta font-semibold text-fg">
          Hosts
          {#if activeHosts.length}
            <span class="ml-1 font-normal text-fg-muted"
              >{activeHosts.length}</span
            >
          {/if}
        </h2>
        {#if activeHosts.length}
          <button
            class="text-micro font-medium text-accent-text hover:underline"
            type="button"
            aria-expanded={enrollOpen}
            onclick={() => (enrollOpen = !enrollOpen)}
            >{enrollOpen ? "Close" : "Enroll a machine"}</button
          >
        {/if}
      </div>

      {#if enrollOpen || (sections.hosts.status === "ready" && !activeHosts.length)}
        <div
          class="mb-3 space-y-4 rounded-md border border-line bg-bg-soft px-4 py-3"
          data-host-enroll-help
        >
          <div class="space-y-1.5">
            <p class="text-meta text-fg">
              {activeHosts.length
                ? "Enroll another machine"
                : "No machines enrolled yet"}
            </p>
            <p class="text-micro text-fg-muted">
              Run this on the machine your agents use. It prints a code; the
              request appears above, and you approve it when the codes match.
              Agents already set up on that machine keep their history.
            </p>
            <div class="flex items-center gap-1 rounded bg-bg px-2 py-1.5">
              <code
                class="min-w-0 flex-1 break-all font-mono text-micro text-fg"
                data-host-enroll-command>{enrollCommand}</code
              >
              <CopyButton value={enrollCommand} label="Copy command" />
            </div>
          </div>
          <div class="space-y-2 border-t border-line-subtle pt-3">
            <p class="text-micro text-fg-muted">
              For CI or cloud machines that cannot wait for approval, create a
              one-time token instead.
            </p>
            {#if sections.tokens.status === "error"}
              <p class="text-micro text-danger-text">{sections.tokens.error}</p>
            {/if}
            <HostEnrollmentTokens
              {tokens}
              {cliBaseUrl}
              {now}
              oncreate={createToken}
              onrevoke={revokeToken}
            />
          </div>
        </div>
      {/if}

      {#if sections.hosts.status === "error"}
        <p
          class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
        >
          {sections.hosts.error}
        </p>
      {:else if activeHosts.length}
        <div class="space-y-3">
          {#each activeHosts as host (host.id)}
            <HostCard
              {host}
              agents={host.agents ?? []}
              canManage={canManageAccess}
              {workspaceHref}
              {now}
              onexclusions={saveExclusions}
              onrevoke={revokeHost}
            />
          {/each}
        </div>
      {/if}
      {#if revokedHosts.length}
        <button
          class="mt-2 text-micro text-fg-muted hover:text-fg"
          type="button"
          onclick={() => (showRevokedHosts = !showRevokedHosts)}
          >{showRevokedHosts
            ? "Hide revoked hosts"
            : `Show ${revokedHosts.length} revoked ${revokedHosts.length === 1 ? "host" : "hosts"}`}</button
        >
        {#if showRevokedHosts}
          <div class="mt-2 space-y-3">
            {#each revokedHosts as host (host.id)}
              <HostCard
                {host}
                agents={host.agents ?? []}
                {workspaceHref}
                {now}
              />
            {/each}
          </div>
        {/if}
      {/if}
    </section>

    <section id="people" class="scroll-mt-20" aria-labelledby="people-title">
      <div class="mb-2 flex items-baseline justify-between gap-3">
        <h2 id="people-title" class="text-meta font-semibold text-fg">
          People
          {#if humans.length}
            <span class="ml-1 font-normal text-fg-muted">{humans.length}</span>
          {/if}
        </h2>
        {#if !hostedMode}
          <Button
            variant="secondary"
            size="compact"
            busy={creatingInvite}
            onclick={inviteHuman}
            >{creatingInvite ? "Creating invite…" : "Invite a person"}</Button
          >
        {/if}
      </div>

      {#if hostedMode}
        <p class="mb-2 text-micro text-fg-muted">
          To invite a person, go to
          <a
            class="font-medium text-accent-text hover:text-accent-text"
            href="/hosted/organizations">your Organizations</a
          >.
        </p>
      {/if}
      {#if inviteError}
        <p
          class="mb-2 rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
          role="alert"
        >
          {inviteError}
        </p>
      {/if}
      {#if createdInviteToken}
        <div
          class="mb-2 space-y-2 rounded-md border border-ok bg-ok-soft px-3 py-2.5"
          role="status"
          data-invite-token-banner
        >
          <div class="flex items-start justify-between gap-3">
            <p class="text-micro text-ok-text">
              Invite created. Send this one-time token to the person; they paste
              it under “Join with an invite token” when they sign in. It is not
              shown again.
            </p>
            <button
              class="shrink-0 text-micro text-fg-muted hover:text-fg"
              type="button"
              onclick={() => (createdInviteToken = "")}>Dismiss</button
            >
          </div>
          <div class="flex items-center gap-1 rounded bg-bg px-2 py-1.5">
            <code class="min-w-0 flex-1 break-all font-mono text-micro text-fg"
              >{createdInviteToken}</code
            >
            <CopyButton value={createdInviteToken} label="Copy token" />
          </div>
        </div>
      {/if}

      {#if sections.principals.status === "error"}
        <p
          class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
        >
          {sections.principals.error}
        </p>
      {:else if sections.principals.status === "ready"}
        <ul class="overflow-hidden rounded-md border border-line bg-bg-soft">
          {#each humans as principal (principal.agent_id)}
            {@const name = principalName(principal)}
            {@const isYou = principal.agent_id === authenticatedAgentId}
            <li
              class="flex items-center gap-3 border-t border-line-subtle px-4 py-2 first:border-t-0 {principal.revoked
                ? 'opacity-50'
                : ''}"
              data-principal={principal.agent_id}
            >
              <ActorAvatar label={name} seed={principal.agent_id} size="xs" />
              <div class="min-w-0 flex-1">
                <p class="truncate text-meta text-fg">
                  {name}
                  {#if isYou}<span class="ml-1 text-micro text-fg-muted"
                      >(you)</span
                    >{/if}
                  {#if principal.revoked}<span
                      class="ml-1 rounded bg-danger-soft px-1.5 py-0.5 text-micro text-danger-text"
                      >Revoked</span
                    >{/if}
                </p>
                <p class="flex items-center gap-1.5 text-micro text-fg-subtle">
                  <span title={formatAbsoluteDateTime(principal.created_at)}
                    >joined {formatAge(principal.created_at, now) === "<1m"
                      ? "just now"
                      : `${formatAge(principal.created_at, now)} ago`}</span
                  >
                  {#if principal.last_seen_at}
                    <span aria-hidden="true">·</span>
                    <span title={formatAbsoluteDateTime(principal.last_seen_at)}
                      >seen {formatAge(principal.last_seen_at, now) === "<1m"
                        ? "just now"
                        : `${formatAge(principal.last_seen_at, now)} ago`}</span
                    >
                  {/if}
                  <span aria-hidden="true">·</span>
                  <CopyableId
                    value={principal.agent_id}
                    label="Copy principal id"
                  />
                </p>
              </div>
              {#if !principal.revoked && !isYou}
                <button
                  class="shrink-0 text-micro text-danger-text hover:underline"
                  type="button"
                  onclick={() => startPrincipalRevoke(principal)}
                  >{activeHumanPrincipalCount === 1
                    ? "Break glass…"
                    : "Revoke…"}</button
                >
              {/if}
            </li>
          {:else}
            <li class="px-4 py-3 text-meta text-fg-muted">No people yet.</li>
          {/each}
        </ul>

        {#if pendingInvites.length}
          <p class="mb-1 mt-3 text-micro font-medium text-fg-muted">
            Open invites
          </p>
          <ul
            class="overflow-hidden rounded-md border border-line-subtle text-micro"
          >
            {#each pendingInvites as invite (invite.id)}
              <li
                class="flex items-center gap-3 border-t border-line-subtle px-3 py-1.5 first:border-t-0"
                data-invite={invite.id}
              >
                <span class="text-fg-muted"
                  >Invite created {formatAge(invite.created_at, now) === "<1m"
                    ? "just now"
                    : `${formatAge(invite.created_at, now)} ago`}</span
                >
                <CopyableId value={invite.id} label="Copy invite id" />
                <button
                  class="ml-auto shrink-0 text-danger-text hover:underline disabled:opacity-50"
                  type="button"
                  disabled={revokingInviteId === invite.id}
                  onclick={() =>
                    (revokeInviteConfirm = { open: true, id: invite.id })}
                  >{revokingInviteId === invite.id
                    ? "Revoking…"
                    : "Revoke"}</button
                >
              </li>
            {/each}
          </ul>
        {/if}
      {/if}
    </section>

    {#if standaloneAgents.length}
      <section aria-labelledby="standalone-title">
        <h2 id="standalone-title" class="mb-1 text-meta font-semibold text-fg">
          Standalone agents
          <span class="ml-1 font-normal text-fg-muted"
            >{standaloneAgents.length}</span
          >
        </h2>
        <p class="mb-2 text-micro text-fg-muted">
          Registered before hosts and not adopted by one. They keep working
          until revoked; enroll their machine to bring them under a host.
        </p>
        <ul class="overflow-hidden rounded-md border border-line bg-bg-soft">
          {#each standaloneAgents as principal (principal.agent_id)}
            <li
              class="flex items-center gap-3 border-t border-line-subtle px-4 py-2 first:border-t-0"
              data-standalone-agent={principal.agent_id}
            >
              <div class="min-w-0 flex-1">
                <a
                  class="truncate text-meta text-fg hover:underline"
                  href={workspaceHref(
                    `/agents/${encodeURIComponent(principal.username || principal.agent_id)}`,
                  )}>{principalName(principal)}</a
                >
                <p class="flex items-center gap-1.5 text-micro text-fg-subtle">
                  <span>@{principal.username}</span>
                  <span aria-hidden="true">·</span>
                  <CopyableId
                    value={principal.agent_id}
                    label="Copy agent id"
                  />
                </p>
              </div>
              <button
                class="shrink-0 text-micro text-danger-text hover:underline"
                type="button"
                onclick={() => startPrincipalRevoke(principal)}>Revoke…</button
              >
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    <section aria-labelledby="audit-title">
      <h2 id="audit-title" class="mb-2 text-meta font-semibold text-fg">
        Recent access events
      </h2>
      {#if sections.audit.status === "error"}
        <p
          class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
        >
          {sections.audit.error}
        </p>
      {:else if sections.audit.status === "ready"}
        {#if auditEvents.length}
          <ol class="overflow-hidden rounded-md border border-line bg-bg-soft">
            {#each visibleAudit as event (event.event_id)}
              <li
                class="group/audit flex items-center gap-3 border-t border-line-subtle px-4 py-1.5 first:border-t-0"
                data-audit-event={event.event_type}
              >
                <p
                  class="min-w-0 flex-1 text-meta text-fg [overflow-wrap:anywhere]"
                >
                  {auditSentence(event)}
                </p>
                <span
                  class="shrink-0 text-micro text-fg-subtle"
                  title={formatAbsoluteDateTime(event.occurred_at)}
                  >{formatAge(event.occurred_at, now) === "<1m"
                    ? "just now"
                    : `${formatAge(event.occurred_at, now)} ago`}</span
                >
                <span
                  class="shrink-0 opacity-0 transition-opacity group-hover/audit:opacity-100 focus-within:opacity-100"
                  ><CopyButton
                    value={event.event_id}
                    label="Copy event id"
                    iconOnly
                  /></span
                >
              </li>
            {/each}
          </ol>
          {#if auditEvents.length > AUDIT_PREVIEW || auditCursor}
            <div class="mt-2 flex gap-3">
              {#if !showAllAudit && auditEvents.length > AUDIT_PREVIEW}
                <button
                  class="text-micro text-fg-muted hover:text-fg"
                  type="button"
                  onclick={() => (showAllAudit = true)}
                  >Show {auditEvents.length - AUDIT_PREVIEW} more</button
                >
              {:else if auditCursor}
                <button
                  class="text-micro text-fg-muted hover:text-fg disabled:opacity-50"
                  type="button"
                  disabled={loadingMoreAudit}
                  onclick={loadMoreAudit}
                  >{loadingMoreAudit ? "Loading…" : "Load older events"}</button
                >
              {/if}
            </div>
          {/if}
        {:else}
          <p
            class="rounded-md border border-line bg-bg-soft px-4 py-3 text-meta text-fg-muted"
          >
            No access events yet.
          </p>
        {/if}
      {/if}
    </section>
  </div>
{/if}

<ConfirmModal
  open={Boolean(principalRevokeTarget)}
  title={principalRevokeLockout
    ? "Last person with access"
    : `Revoke ${principalRevokeTarget ? principalName(principalRevokeTarget) : ""}`}
  message={principalRevokeLockout
    ? `Revoking ${principalRevokeTarget ? principalName(principalRevokeTarget) : ""} locks every person out of this workspace. Type the principal id and give a reason to continue. This is audit-logged.`
    : "They lose access to this workspace now. Their history stays. This is audit-logged."}
  confirmLabel={principalRevokeLockout
    ? "Allow lockout and revoke"
    : "Revoke access"}
  busyLabel="Revoking…"
  variant="danger"
  busy={principalRevokeBusy}
  typedConfirmation={principalRevokeLockout
    ? (principalRevokeTarget?.agent_id ?? "")
    : ""}
  confirmBlocked={principalRevokeLockout && principalRevokeReason.trim() === ""}
  error={principalRevokeError}
  onconfirm={confirmPrincipalRevoke}
  oncancel={cancelPrincipalRevoke}
>
  {#if principalRevokeLockout}
    <label class="mt-3 block">
      <span class="mb-1.5 block text-micro text-fg-muted">Lockout reason</span>
      <input
        bind:value={principalRevokeReason}
        class="w-full rounded-md border border-line bg-bg px-2.5 py-1.5 text-meta text-fg"
        id="principal-lockout-reason"
        placeholder="Explain the recovery path"
        type="text"
        autocomplete="off"
      />
    </label>
  {/if}
</ConfirmModal>

<ConfirmModal
  open={revokeInviteConfirm.open}
  title="Revoke invite"
  message="This invite stops working and can no longer be used to join the workspace."
  confirmLabel="Revoke"
  variant="danger"
  busy={revokingInviteId === revokeInviteConfirm.id}
  onconfirm={() => {
    void revokeInvite(revokeInviteConfirm.id);
    revokeInviteConfirm = { open: false, id: "" };
  }}
  oncancel={() => {
    revokeInviteConfirm = { open: false, id: "" };
  }}
/>

<style>
  .tour-arrival-banner {
    padding: 0.95rem 1rem;
    border-radius: 0.75rem;
    border: 1px solid color-mix(in srgb, var(--accent) 35%, var(--line));
    background: color-mix(in srgb, var(--accent) 8%, var(--bg-soft));
  }
  .tour-arrival-banner__title {
    margin: 0 0 0.2rem 0;
    font-size: 0.95rem;
    font-weight: 600;
    color: var(--fg);
  }
  .tour-arrival-banner__text {
    margin: 0;
    font-size: 0.85rem;
    line-height: 1.5;
    color: var(--fg-muted);
  }
  .tour-arrival-banner__text code {
    font-family: var(--font-mono);
    font-size: 0.8rem;
    color: var(--fg);
  }
</style>
