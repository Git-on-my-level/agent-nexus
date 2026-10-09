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
  import {
    authenticatedAgent,
    isHumanWorkspacePrincipal,
  } from "$lib/authSession";
  import { coreClient } from "$lib/coreClient";
  import { createDecisionEpoch } from "$lib/decisionEpoch.js";
  import { isAdministrationRefusal } from "$lib/coreAuthErrors.js";
  import { formatAbsoluteDateTime } from "$lib/formatDate";
  import {
    claimPendingAccessCount,
    countPendingAccessItems,
    clearPendingAccessCount,
    publishPendingAccessForbidden,
    publishPendingAccessSources,
  } from "$lib/pendingAccessCount.js";
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
  import AuthAdminGrants from "$lib/components/access/AuthAdminGrants.svelte";
  import HostCard from "$lib/components/access/HostCard.svelte";
  import AccessRequestRow from "$lib/components/access/AccessRequestRow.svelte";
  import HostEnrollmentRequest from "$lib/components/access/HostEnrollmentRequest.svelte";
  import HostEnrollmentTokens from "$lib/components/access/HostEnrollmentTokens.svelte";
  import SetupPrompt from "$lib/components/setup/SetupPrompt.svelte";

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
  let cliInstallCommand = $derived(
    $page.data?.workspace?.cliInstallCommand ?? "",
  );
  let workspaceLabel = $derived(
    $page.data?.workspace?.label || $page.params.workspace || "",
  );

  let now = $state(Date.now());
  let loaded = $state(false);
  let pageError = $state("");

  /** Section state: each section loads and fails on its own. */
  let sections = $state({
    hosts: { status: "idle", error: "", forbidden: false },
    pending: { status: "idle", error: "", forbidden: false },
    requests: { status: "idle", error: "", forbidden: false },
    tokens: { status: "idle", error: "", forbidden: false },
    admins: { status: "idle", error: "", forbidden: false },
    principals: { status: "idle", error: "", forbidden: false },
    invites: { status: "idle", error: "", forbidden: false },
    audit: { status: "idle", error: "", forbidden: false },
  });

  let hosts = $state([]);
  let pending = $state([]);
  let accessRequests = $state([]);
  let tokens = $state([]);
  let admins = $state([]);
  let principals = $state([]);
  let activeHumanPrincipalCount = $state(0);
  let invites = $state([]);
  let auditEvents = $state([]);
  let auditCursor = $state("");
  let loadingMoreAudit = $state(false);
  let showAllAudit = $state(false);

  let enrollOpen = $state(false);
  let showRevokedHosts = $state(false);
  /**
   * The first host to arrive while this page was watching an empty workspace.
   *
   * Held separately from `activeHosts` so the confirmation says which machine
   * answered, and so it keeps saying it after a later read returns several.
   */
  let arrivedHost = $state(null);
  /** Hosts were read at least once and there were none. */
  let watchingForFirstHost = $state(false);
  /** Busy action per enrollment id, so one decision cannot re-enable another. */
  let enrollmentBusy = $state({});
  let enrollmentErrors = $state({});
  let enrollmentNotice = $state("");

  /** Busy action per request id: two decisions can be in flight at once. */
  let requestBusy = $state({});
  let requestErrors = $state({});

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
  // Which host's shared key can request a given agent. A grant confirmation
  // may only name a host when the roster actually says there is one.
  let hostSlugByAgentId = $derived(
    new Map(
      hosts.flatMap((host) =>
        (host.agents ?? []).map((agent) => [agent.id, host.slug]),
      ),
    ),
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
  // Administration reaches people implicitly and agents by explicit grant;
  // the heading counts both, the same way the list shows both.
  // `listPrincipals` returns one page, newest first, and host-derived agents
  // can fill it; core's active-human count is the number that is true.
  let adminCount = $derived(activeHumanPrincipalCount + admins.length);
  // Deciding a grant is human-only, so an agent principal never sees the
  // requests — including the ones a person was reading a moment ago in this
  // same mounted page.
  let isHumanPrincipal = $derived(
    isHumanWorkspacePrincipal($authenticatedAgent),
  );

  // Rows are shown only while a current, unrefused read vouches for them.
  // Without this gate a refusal leaves the previous reader's rows, and their
  // live controls, beside the line explaining that they cannot be shown.
  let visibleAccessRequests = $derived(
    isHumanPrincipal && !sections.requests.forbidden ? accessRequests : [],
  );
  let visiblePending = $derived(sections.pending.forbidden ? [] : pending);

  // What the shell badge counts, by core's own rule (`AccessSummary`), over
  // what this reader may actually see.
  let pendingDecisionCount = $derived(
    countPendingAccessItems(
      { enrollments: visiblePending, accessRequests: visibleAccessRequests },
      now,
    ),
  );
  let pendingInvites = $derived(
    invites.filter((invite) => !invite.revoked_at && !invite.consumed_at),
  );
  let visibleAudit = $derived(
    showAllAudit ? auditEvents : auditEvents.slice(0, AUDIT_PREVIEW),
  );
  // Every read on this page needs administration authority, so they are
  // refused together. Saying that once beats seven sections each describing
  // an empty workspace the reader was not allowed to see.
  let accessRefused = $derived(
    Object.values(sections).every((section) => section.forbidden),
  );
  let showTourBanner = $derived(
    tourArrived &&
      canManageAccess &&
      sections.hosts.status === "ready" &&
      !sections.hosts.forbidden &&
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
      sections[key] = { status: "ready", error: "", forbidden: false };
    } else if (isAdministrationRefusal(result.reason)) {
      // Refused means this reader may not see it. Every `apply` reads its
      // list out of the response, so handing it nothing empties the section
      // rather than leaving the previous reader's rows on screen.
      apply(undefined);
      sections[key] = { status: "ready", error: "", forbidden: true };
    } else {
      sections[key] = {
        status: "error",
        error: message(result.reason, "This did not load."),
        forbidden: false,
      };
    }
  }

  // The shell badge and this page show the same number, from the same read.
  function publishPending(forbidden = false) {
    if (!workspaceSlug) return;
    if (forbidden) publishPendingAccessForbidden(workspaceSlug);
    else
      publishPendingAccessSources(workspaceSlug, {
        enrollments: visiblePending,
        accessRequests: visibleAccessRequests,
      });
  }

  // Hosts and tokens only. The two lists above them have their own reader,
  // which polls; loading them here as well fetched each of them twice on
  // every roster event.
  /**
   * Await reads for the reader who started them.
   *
   * Returns null once the reader has changed, so the caller applies nothing.
   * Every read on this page is inventory one principal was allowed to see;
   * a response that arrives after the session became someone else would
   * repaint their rows and, worse, clear the refusal that hid them. Going
   * through here is what keeps that from depending on each caller
   * remembering.
   */
  async function readForThisReader(reads) {
    const epoch = decisions.current();
    const results = await Promise.allSettled(reads);
    return decisions.isStale(epoch) ? null : results;
  }

  async function loadHosts() {
    const results = await readForThisReader([
      coreClient.listHosts(),
      coreClient.listHostEnrollmentTokens(),
    ]);
    if (!results) return;
    const [hostsResult, tokensResult] = results;
    settle("hosts", hostsResult, (value) => {
      const next = value?.hosts ?? [];
      /*
       * A machine that enrolls with a token never files a request, so nothing
       * else on this page would notice it. Watch the roster while there are
       * none and name the first one that answers, so the reader sees the setup
       * they just started finish without reloading.
       */
      const live = next.filter((host) => !host.revoked_at);
      if (watchingForFirstHost && !arrivedHost && live.length) {
        arrivedHost = live[0];
      }
      // A confirmation outlives its machine otherwise: revoke the host it
      // names and the green card keeps saying the workspace has it.
      if (arrivedHost && !live.some((host) => host.id === arrivedHost.id)) {
        arrivedHost = null;
      }
      watchingForFirstHost = live.length === 0;
      hosts = next;
    });
    settle("tokens", tokensResult, (value) => {
      tokens = value?.enrollment_tokens ?? [];
    });
  }

  async function loadPeople() {
    const results = await readForThisReader([
      coreClient.listPrincipals({ limit: 200 }),
      coreClient.listInvites(),
      coreClient.listAuthAudit({ limit: 50 }),
      coreClient.listAuthAdmins(),
    ]);
    if (!results) return;
    const [principalsResult, invitesResult, auditResult, adminsResult] =
      results;
    settle("admins", adminsResult, (value) => {
      admins = value?.admins ?? [];
    });
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

  /**
   * Drop everything this page read for whoever was signed in a moment ago.
   *
   * Every list here is inventory one principal was allowed to see. When the
   * session becomes someone else without the page unmounting, none of it is
   * vouched for any more: in-flight reads are discarded, the rows and the
   * one-time invite token go, each section returns to idle, and the shell
   * count stops reporting a number nothing will refresh.
   */
  function forgetReaderState() {
    decisions.invalidate();
    hosts = [];
    arrivedHost = null;
    watchingForFirstHost = false;
    pending = [];
    accessRequests = [];
    tokens = [];
    admins = [];
    principals = [];
    activeHumanPrincipalCount = 0;
    invites = [];
    auditEvents = [];
    auditCursor = "";
    createdInviteToken = "";
    enrollmentNotice = "";
    enrollmentErrors = {};
    requestErrors = {};
    for (const key of Object.keys(sections)) {
      sections[key] = { status: "idle", error: "", forbidden: false };
    }
    loaded = false;
    clearPendingAccessCount();
  }

  async function loadAll() {
    await Promise.all([loadHosts(), loadPending(), loadPeople()]);
    loaded = true;
  }

  function sameIds(next, current) {
    return (
      next.length === current.length &&
      next.every((entry, index) => entry.id === current[index]?.id)
    );
  }

  // Reads that overlap a decision carry pre-decision rows; see decisionEpoch.
  const decisions = createDecisionEpoch();

  /**
   * Both lists in the "Waiting for you" section, from one reader.
   *
   * An enrolling machine polls every few seconds, so this does too; an agent
   * can ask for a grant at any moment, so that list refreshes on the same
   * tick. A read that fails keeps the rows it already has and says the read
   * failed, rather than reporting an empty section it cannot vouch for.
   */
  async function loadPending() {
    const results = await readForThisReader([
      coreClient.listPendingHostEnrollments(),
      coreClient.listAccessRequests(),
    ]);
    if (!results) return;
    const [pendingResult, requestsResult] = results;
    let changed = false;
    settle("pending", pendingResult, (value) => {
      const next = value?.enrollments ?? [];
      changed = !sameIds(next, pending);
      pending = next;
    });
    settle("requests", requestsResult, (value) => {
      accessRequests = value?.requests ?? [];
    });
    publishPending(
      [sections.pending, sections.requests].every(
        (section) => section.forbidden,
      ),
    );
    if (changed) {
      void loadHosts();
      void refreshAgentRoster();
    }
  }

  async function decideAccessRequest(request, action) {
    // The rows are gated already; this is the second lock on the authority.
    if (!isHumanPrincipal) return;
    requestBusy = { ...requestBusy, [request.id]: action };
    requestErrors = { ...requestErrors, [request.id]: "" };
    await decisions.during(async () => {
      try {
        if (action === "approve") {
          await coreClient.approveAccessRequest(request.id);
        } else {
          await coreClient.denyAccessRequest(request.id);
        }
        accessRequests = accessRequests.filter(
          (entry) => entry.id !== request.id,
        );
        publishPending();
        enrollmentNotice =
          action === "approve"
            ? `${requestName(request)} can now administer access, and is listed under Administrators.`
            : `Denied ${requestName(request)}. Its access is unchanged.`;
      } catch (error) {
        requestErrors = {
          ...requestErrors,
          [request.id]: message(
            error,
            action === "approve"
              ? "The request was not approved."
              : "The request was not denied.",
          ),
        };
      } finally {
        requestBusy = Object.fromEntries(
          Object.entries(requestBusy).filter(([id]) => id !== request.id),
        );
      }
    });
    // Re-read either way, after the epoch has settled so this read is kept.
    // A decision that reported failure may still have landed (core grants and
    // projects in separate steps), and a reader must not be told the grant
    // failed while the agent holds it.
    await Promise.all([loadPeople(), loadPending()]);
  }

  function requestName(request) {
    return (
      nameFor(request?.actor_id) ||
      nameFor(request?.principal_id) ||
      request?.username ||
      "An agent"
    );
  }

  async function decideEnrollment(enrollment, action) {
    enrollmentBusy = { ...enrollmentBusy, [enrollment.id]: action };
    enrollmentErrors = { ...enrollmentErrors, [enrollment.id]: "" };
    await decisions.during(async () => {
      try {
        if (action === "approve") {
          await coreClient.approveHostEnrollment(enrollment.id);
        } else {
          await coreClient.denyHostEnrollment(enrollment.id);
        }
        pending = pending.filter((entry) => entry.id !== enrollment.id);
        publishPending();
        enrollmentNotice =
          action === "approve"
            ? `Approved ${enrollment.requested_slug}. It appears under Hosts once the machine finishes enrolling.`
            : `Denied ${enrollment.requested_slug}.`;
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
        enrollmentBusy = Object.fromEntries(
          Object.entries(enrollmentBusy).filter(([id]) => id !== enrollment.id),
        );
      }
    });
    // Re-read either way, after the epoch has settled so this read is kept: a
    // decision that reported failure may still have landed.
    await Promise.all([loadHosts(), loadPeople(), loadPending()]);
  }

  async function saveExclusions(host, names) {
    const epoch = decisions.current();
    const result = await coreClient.patchHost(host.id, {
      excluded_names: names,
    });
    if (decisions.isStale(epoch)) return;
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
      const epoch = decisions.current();
      const result = await coreClient.createInvite({ kind: "human" });
      // A one-time token belongs to the person who asked for it; showing it
      // to whoever is signed in by the time it arrives would hand it over.
      if (decisions.isStale(epoch)) return;
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
      const epoch = decisions.current();
      const result = await coreClient.listAuthAudit({
        limit: 50,
        cursor: auditCursor,
      });
      if (decisions.isStale(epoch)) return;
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

  // Identity can change under a mounted page: sign out and in, or a dev
  // persona switch. Held in a plain object rather than `$state` because it
  // records what the effect has already handled; making it reactive would
  // re-run the effect that writes it.
  const reader = { identity: "" };
  $effect(() => {
    const identity = [
      $authenticatedAgent?.agent_id ?? "",
      $authenticatedAgent?.actor_id ?? "",
      $authenticatedAgent?.principal_kind ?? "",
    ].join("|");
    if (identity === reader.identity) return;
    const firstReader = reader.identity === "";
    reader.identity = identity;
    // The first pass is the mount, which loads on its own.
    if (firstReader) return;
    forgetReaderState();
    if (canManageAccess) void loadAll();
  });

  onMount(() => {
    if (!canManageAccess) return;
    // This page polls pending access every few seconds; while it is open the
    // shell badge reads that instead of polling a second time.
    const releaseCount = claimPendingAccessCount();
    void loadAll();
    // Host cards show agent states; core's roster stream says when they move.
    const stopAgentChanges = liveAgentChanges({
      client: coreClient,
      debounceMs: 600,
      onChange: () => loadHosts(),
    });
    const poll = setInterval(() => {
      now = Date.now();
      if (document.hidden) return;
      void loadPending();
      /*
       * Two extra bounded reads per tick (hosts and enrollment tokens), and
       * only while the workspace has no machine at all: a token enrollment
       * files no request, so `loadPending` would never see it. Stops on the
       * tick after the first host arrives.
       */
      if (watchingForFirstHost) void loadHosts();
    }, PENDING_POLL_MS);
    return () => {
      // First: a throw in either teardown below must not strand the claim and
      // leave the shell badge frozen for the rest of the session.
      releaseCount();
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

    {#if accessRefused}
      <div
        class="rounded-md border border-line bg-bg-soft px-4 py-10 text-center text-meta text-fg-muted"
        data-access-refused
      >
        <p>Only workspace administrators can manage access.</p>
        <p class="mt-2 text-micro">
          Ask a person who administers this workspace to make the change, or to
          grant you administration.
        </p>
      </div>
    {/if}

    {#if !accessRefused}
      {#if showTourBanner}
        <aside class="tour-arrival-banner" role="status" aria-live="polite">
          <div class="tour-arrival-banner__body">
            <p class="tour-arrival-banner__title">
              Last step: connect the machine your agents run on
            </p>
            <p class="tour-arrival-banner__text">
              Copy the setup prompt below into the agent you already use on that
              machine. Every agent on it can use the workspace from then on,
              with no per-agent setup.
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

      <!-- Everything waiting on a decision, above everything that is already
         settled. `#host-requests` is the anchor the CLI's verification URL
         and the agent pages link to, so it stays on this section. -->
      {#if visiblePending.length || visibleAccessRequests.length}
        <section
          id="host-requests"
          class="scroll-mt-20"
          aria-labelledby="pending-access-title"
          data-pending-access
        >
          <h2
            id="pending-access-title"
            class="mb-2 flex items-center gap-2 text-meta font-semibold text-fg"
          >
            <span class="h-2 w-2 rounded-full bg-warn" aria-hidden="true"
            ></span>
            <!-- An approved ceremony is waiting on its machine, not on the
                 reader, so a section holding only those does not claim to
                 need a decision and the badge stays dark. -->
            {pendingDecisionCount
              ? "Waiting for you"
              : "Enrollment in progress"}
            {#if pendingDecisionCount}
              <span class="font-normal text-fg-muted" data-pending-access-count
                >{pendingDecisionCount}</span
              >
            {/if}
          </h2>
          <ul
            class="divide-y divide-line-subtle overflow-hidden rounded-md border bg-bg-soft"
            style="border-color: color-mix(in srgb, var(--warn) 40%, var(--line))"
          >
            <!-- Agents asking for authority come first: a person decides
                 those, and an enrolling machine is still polling. -->
            {#each visibleAccessRequests as request (request.id)}
              <AccessRequestRow
                {request}
                {now}
                name={requestName(request)}
                hostSlug={hostSlugByAgentId.get(request.principal_id) ?? ""}
                busy={requestBusy[request.id] ?? ""}
                error={requestErrors[request.id] ?? ""}
                onapprove={(entry) => decideAccessRequest(entry, "approve")}
                ondeny={(entry) => decideAccessRequest(entry, "deny")}
              />
            {/each}
            {#each visiblePending as enrollment (enrollment.id)}
              <HostEnrollmentRequest
                {enrollment}
                {now}
                busy={enrollmentBusy[enrollment.id] ?? ""}
                error={enrollmentErrors[enrollment.id] ?? ""}
                onapprove={(entry) => decideEnrollment(entry, "approve")}
                ondeny={(entry) => decideEnrollment(entry, "deny")}
              />
            {/each}
          </ul>
        </section>
      {:else if sections.pending.forbidden}
        <p class="text-meta text-fg-muted" data-pending-access-forbidden>
          Only workspace administrators can see access requests.
        </p>
      {/if}
      <!-- Deciding a grant is human-only, so an agent administrator can read
           enrollments and not requests. Saying "ask for administration" there
           would be advice that cannot work. -->
      {#if (sections.requests.forbidden || !isHumanPrincipal) && !sections.pending.forbidden && sections.requests.status !== "idle"}
        <p class="mt-2 text-micro text-fg-muted" data-access-requests-forbidden>
          Agents asking for a grant are shown to people only.
        </p>
      {/if}
      <!-- A read that failed says so. An empty section that quietly dropped
           one of its two sources would read as "nothing is waiting". -->
      {#if sections.pending.status === "error"}
        <p
          class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
        >
          Pending host requests did not load: {sections.pending.error}
        </p>
      {/if}
      {#if sections.requests.status === "error"}
        <p
          class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
          data-access-requests-error
        >
          Access requests did not load: {sections.requests.error}
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

        {#if arrivedHost}
          <!--
            The machine the reader just set up answered. Say which one, and
            hand over the single next step rather than leaving them on a page
            whose work is done.
          -->
          <div
            class="mb-3 space-y-2 rounded-md border border-ok bg-ok-soft px-4 py-3"
            role="status"
            aria-live="polite"
            data-host-enrolled
          >
            <p
              class="flex items-center gap-2 text-meta font-medium text-ok-text"
            >
              <span class="h-1.5 w-1.5 rounded-full bg-ok" aria-hidden="true"
              ></span>
              Enrolled — {arrivedHost.slug}
            </p>
            <p class="text-micro text-fg-muted">
              Every agent on {arrivedHost.slug} can work in this workspace now. They
              appear under Agents as soon as each one uses <code>anx</code>.
            </p>
            <div class="flex flex-wrap items-center gap-2 pt-1">
              <Button
                variant="primary"
                size="compact"
                href={workspaceHref("/pm/setup")}>Set up your PM</Button
              >
              <button
                class="text-micro text-fg-muted hover:text-fg"
                type="button"
                onclick={() => {
                  arrivedHost = null;
                  enrollOpen = true;
                }}>Connect another machine</button
              >
            </div>
          </div>
        {/if}

        {#if enrollOpen || (sections.hosts.status === "ready" && !sections.hosts.forbidden && !activeHosts.length && !arrivedHost)}
          <div
            class="mb-3 space-y-4 rounded-md border border-line bg-bg-soft px-4 py-3"
            data-host-enroll-help
          >
            <SetupPrompt
              kind="machine"
              {cliBaseUrl}
              {cliInstallCommand}
              {workspaceLabel}
              heading={activeHosts.length
                ? "Connect another machine"
                : "Connect your first machine"}
              lede="Agents reach this workspace through the computer they run on. Set one up once — every agent on that computer is in from then on, with no per-agent setup. Agents already set up there keep their history."
            >
              {#snippet status()}
                {#if !activeHosts.length}
                  <p
                    class="flex items-center gap-2 border-t border-line-subtle pt-3 text-micro text-fg-muted"
                    data-host-waiting
                  >
                    <span
                      class="h-1.5 w-1.5 shrink-0 rounded-full bg-fg-subtle"
                      aria-hidden="true"
                    ></span>
                    No machine has checked in yet. This page notices the first one
                    on its own.
                  </p>
                {/if}
              {/snippet}
            </SetupPrompt>
            <div class="space-y-2 border-t border-line-subtle pt-3">
              <p class="text-micro text-fg-muted">
                For CI or cloud machines that cannot wait for approval, create a
                one-time token instead.
              </p>
              {#if sections.tokens.status === "error"}
                <p class="text-micro text-danger-text">
                  {sections.tokens.error}
                </p>
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

      <section
        id="admins"
        class="scroll-mt-20 space-y-3"
        aria-labelledby="admins-title"
      >
        <h2 id="admins-title" class="text-meta font-semibold text-fg">
          Administrators
          {#if sections.admins.status === "ready" && !sections.admins.forbidden && adminCount}
            <span class="ml-1 font-normal text-fg-muted">{adminCount}</span>
          {/if}
        </h2>
        {#if sections.admins.status === "error"}
          <p class="text-meta text-danger-text" role="alert">
            {sections.admins.error}
          </p>
        {:else if sections.admins.status === "ready"}
          <AuthAdminGrants
            {admins}
            {principals}
            {hosts}
            {auditEvents}
            activeHumanCount={activeHumanPrincipalCount}
            currentPrincipalId={authenticatedAgentId}
            displayName={principalName}
            canEdit={$authenticatedAgent?.principal_kind === "human"}
            forbidden={sections.admins.forbidden}
            onchanged={loadPeople}
          />
        {/if}
      </section>

      <section id="people" class="scroll-mt-20" aria-labelledby="people-title">
        <div class="mb-2 flex items-baseline justify-between gap-3">
          <h2 id="people-title" class="text-meta font-semibold text-fg">
            People
            {#if humans.length}
              <span class="ml-1 font-normal text-fg-muted">{humans.length}</span
              >
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
              href={$page.data?.shellCapabilities?.peoplePath || "/"}
              >{$page.data?.shellCapabilities?.peopleLabel || "your account"}</a
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
                Invite created. Send this one-time token to the person; they
                paste it under “Join with an invite token” when they sign in. It
                is not shown again.
              </p>
              <button
                class="shrink-0 text-micro text-fg-muted hover:text-fg"
                type="button"
                onclick={() => (createdInviteToken = "")}>Dismiss</button
              >
            </div>
            <div class="flex items-center gap-1 rounded bg-bg px-2 py-1.5">
              <code
                class="min-w-0 flex-1 break-all font-mono text-micro text-fg"
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
        {:else if sections.principals.forbidden}
          <p class="text-meta text-fg-muted" data-principals-forbidden>
            Only workspace administrators can see the people in this workspace.
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
                  <p
                    class="flex items-center gap-1.5 text-micro text-fg-subtle"
                  >
                    <span title={formatAbsoluteDateTime(principal.created_at)}
                      >joined {formatAge(principal.created_at, now) === "<1m"
                        ? "just now"
                        : `${formatAge(principal.created_at, now)} ago`}</span
                    >
                    {#if principal.last_seen_at}
                      <span aria-hidden="true">·</span>
                      <span
                        title={formatAbsoluteDateTime(principal.last_seen_at)}
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
          <h2
            id="standalone-title"
            class="mb-1 text-meta font-semibold text-fg"
          >
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
                  <p
                    class="flex items-center gap-1.5 text-micro text-fg-subtle"
                  >
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
                  onclick={() => startPrincipalRevoke(principal)}
                  >Revoke…</button
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
        {:else if sections.audit.forbidden}
          <p class="text-meta text-fg-muted" data-audit-forbidden>
            Only workspace administrators can see access events.
          </p>
        {:else if sections.audit.status === "ready"}
          {#if auditEvents.length}
            <ol
              class="overflow-hidden rounded-md border border-line bg-bg-soft"
            >
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
                    >{loadingMoreAudit
                      ? "Loading…"
                      : "Load older events"}</button
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
    {/if}
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
</style>
