<script>
  import UnsentIndicator from "$lib/components/inbox/UnsentIndicator.svelte";
  import LocalSaveWarning from "$lib/components/LocalSaveWarning.svelte";
  import {
    recoverStaleChunk,
    staleChunkRecoveryNeeded,
    isStaleChunkError,
    warnOverviewRecovery,
  } from "$lib/chunkRecovery.js";
  import {
    activateWorkspaceViewScope,
    clearWorkspaceViews,
    workspaceViewUpdatedAt,
  } from "$lib/workspaceViewCache.js";
  import { readerScope } from "$lib/readerScope.js";
  import { browser } from "$app/environment";
  import { goto, preloadData } from "$app/navigation";
  import { page } from "$app/stores";
  import { tick, untrack } from "svelte";
  import { get } from "svelte/store";

  import "../app.css";
  import {
    actorRegistry,
    actorSessionReady,
    buildActorCreatePayload,
    chooseActor,
    clearSelectedActor,
    lookupActorDisplayName,
    principalRegistry,
    replaceActorRegistry,
    setActorDirectoryIncomplete,
    selectedActorId,
    shouldShowActorGate,
  } from "$lib/actorSession";
  import { filterActorsForUserSelection } from "$lib/systemActor.js";
  import {
    authenticatedAgent,
    authSessionReady,
    isHumanWorkspacePrincipal,
    logoutAuthSession,
    sessionEndedByAccountStatus,
    startWorkspaceSessionMaintenance,
  } from "$lib/authSession";
  import SessionEndedOverlay from "$lib/components/SessionEndedOverlay.svelte";
  import CommandPalette from "$lib/components/CommandPalette.svelte";
  import TooltipHost from "$lib/components/TooltipHost.svelte";
  import { navIconPath } from "$lib/icons.js";
  import { commandPaletteRequests } from "$lib/stores/commandPalette.js";
  import {
    hostedSession,
    loadHostedSession,
  } from "$lib/extensions/accountSession.js";
  import {
    coreClient,
    createWorkspaceActivationTransport,
  } from "$lib/coreClient";
  import { computeWorkspaceShellIdentity } from "$lib/workspaceShellIdentity.js";
  import WorkspaceRouteContent from "$lib/components/layout/WorkspaceRouteContent.svelte";
  import WorkspacePageSkeleton from "$lib/components/layout/WorkspacePageSkeleton.svelte";
  import {
    getDestinationSkeleton,
    getShellContentConfig,
    isMoreHubActivePath,
    navigationItems,
    settingsNavGroups,
    settingsNavItems,
  } from "$lib/navigation";
  import {
    setCurrentCoreBaseUrl,
    setCurrentOrganizationSlug,
    setCurrentWorkspaceSlug,
    devActorMode,
    devActorModeReady,
  } from "$lib/workspaceContext";
  import WorkspaceTour from "$lib/components/onboarding/WorkspaceTour.svelte";
  import DecideNavCount from "$lib/components/decide/DecideNavCount.svelte";
  import PanelToggle from "$lib/components/layout/PanelToggle.svelte";
  import AccessNavCount from "$lib/components/access/AccessNavCount.svelte";
  import {
    pendingAccessCount,
    pendingAccessLabel,
  } from "$lib/pendingAccessCount.js";
  import {
    handleEscapeTextBlurCommit,
    handleModEnterBlurCommit,
    handleModEnterFormSubmit,
    handleModSave,
  } from "$lib/formSubmitShortcut.js";
  import {
    bindWorkspaceHref,
    stripBasePath,
    stripWorkspacePath,
    WORKSPACE_HEADER,
    workspacePath,
  } from "$lib/workspacePaths";
  import {
    adoptPanelPreference,
    panelAutoCollapsed,
    panelCollapsed,
    SHELL_PANELS,
    writePanelPreference,
  } from "$lib/shellPanels.js";
  import {
    activateDevPersonaSession as activateWorkspaceDevPersonaSession,
    buildLoginRedirectDestination,
    classifyWorkspaceBootstrap,
    createLoginRedirectController,
    hydrateWorkspaceBootstrap,
    installWorkspaceBootstrapLoopGuards,
    loadDevFixturePersonas as loadWorkspaceDevFixturePersonas,
    refreshWorkspacePrincipals,
    shouldRedirectToLoginForBootstrapState,
    WORKSPACE_BOOTSTRAP_STATES,
  } from "$lib/workspaceBootstrap.js";
  import { pmFeaturesVisible } from "$lib/pm/onboardingState.js";
  import { isWorkspaceUnavailableError } from "$lib/workspaceUnavailable.js";
  import {
    SHELL_REFRESH_MIN_MS,
    clearPmPresence,
    pmPresence,
    refreshPmPresence,
  } from "$lib/pm/presence.js";
  import { providePageTitle } from "$lib/pageTitle.svelte.js";

  let { children, data } = $props();
  const pageTitleOverride = providePageTitle();

  function onWorkspaceRenderError(error) {
    if (currentAppPath === "/watch" || currentAppPath === "/") {
      if (!isStaleChunkError(error))
        warnOverviewRecovery(
          "render-boundary",
          workspaceViewUpdatedAt(`${$readerScope}:overview`),
          error,
        );
    } else console.error("Workspace page failed to render", error);
    recoverStaleChunk(error);
  }

  $effect(() => {
    if (
      browser &&
      identityReady &&
      hydratedWorkspaceSlug ===
        `${activeOrganizationSlug}/${activeWorkspaceSlug}`
    )
      activateWorkspaceViewScope($readerScope);
  });

  let actorError = $state("");
  let loadingActors = $state(false);
  let creatingActor = $state(false);
  let newActorName = $state("");
  let hydratedWorkspaceSlug = $state("");
  let activationError = $state("");
  let activationPending = $state(false);
  const ACTIVATION_DEADLINE_MS = 15_000;
  const hydration = {
    attemptPath: "",
    sequence: 0,
    key: "",
    timer: null,
    controller: null,
    resumeKey: "",
    resumedAt: -Infinity,
    startedAt: 0,
    expire: null,
    release: null,
  };
  /** Which workspace the dev fixture personas have been read for. */
  const devPersonaLoad = { attempted: "" };
  let workspacePickerOpen = $state(false);
  let commandPaletteOpen = $state(false);
  let accountMenuOpen = $state(false);
  let personaSubmenuOpen = $state(false);
  let devFixturePersonas = $state([]);
  let devPersonaBusy = $state(false);
  let devPersonaError = $state("");

  let activeWorkspace = $derived($page.data.workspace ?? null);
  let shellCapabilities = $derived(
    data.shellCapabilities ?? {
      mode: "local",
      accountPath: null,
      publicOrigin: null,
    },
  );
  let hostedMode = $derived(shellCapabilities.mode === "hosted");
  let hostedAccountPath = $derived(
    String(shellCapabilities.accountPath ?? "").trim() || "/",
  );
  let activeWorkspaceSlug = $derived(activeWorkspace?.slug ?? "");
  let activeOrganizationSlug = $derived(
    activeWorkspace?.organizationSlug ?? $page.params?.organization ?? "",
  );
  let hasMultipleWorkspaces = $derived((data.workspaces ?? []).length > 1);
  let workspaceSwitcherSub = $derived.by(() => {
    const d = String(activeWorkspace?.description ?? "").trim();
    if (d) return d;
    if (hostedMode) return "Workspace";
    return "Agent Nexus";
  });
  let currentAppPath = $derived(
    activeWorkspaceSlug && activeOrganizationSlug
      ? stripWorkspacePath(
          $page.url.pathname,
          activeOrganizationSlug,
          activeWorkspaceSlug,
        )
      : stripBasePath($page.url.pathname),
  );
  let qaMode = $derived($page.url.searchParams.get("qa") === "1");
  let identityReady = $derived($actorSessionReady && $authSessionReady);
  let principalActorId = $derived($authenticatedAgent?.actor_id ?? "");
  // Prefer actor_id; fall back to agent_id for display when core row is inconsistent
  // or /agents/me omitted actor_id — buildActorNameMap still maps agent_id -> username.
  let activeActorId = $derived(
    principalActorId ||
      $selectedActorId ||
      ($authenticatedAgent?.agent_id ?? ""),
  );
  let onLoginRoute = $derived(currentAppPath === "/login");
  // anx-core human-only routes (e.g. /secrets) need a passkey or dev-bypass session
  // even when dev actor mode allows unauthenticated browsing elsewhere.
  let requiresHumanSession = $derived(currentAppPath === "/secrets");
  let hasHumanAuthSession = $derived(
    isHumanWorkspacePrincipal($authenticatedAgent),
  );
  let gateVisible = $derived(
    activeWorkspaceSlug &&
      identityReady &&
      !$authenticatedAgent &&
      !onLoginRoute &&
      $devActorMode &&
      !requiresHumanSession &&
      shouldShowActorGate($actorSessionReady, $selectedActorId),
  );
  let renderLoginOnly = $derived(
    activeWorkspaceSlug &&
      identityReady &&
      !$authenticatedAgent &&
      onLoginRoute,
  );
  let workspaceBootstrapState = $derived(
    classifyWorkspaceBootstrap({
      activeWorkspaceSlug,
      identityReady,
      devActorModeReady: $devActorModeReady,
      authenticatedAgent: $authenticatedAgent,
      hostedMode,
      devActorMode: $devActorMode,
      onLoginRoute,
      requiresHumanSession,
      hasHumanAuthSession,
    }),
  );
  // Hosted shells must never allow anonymous workspace browsing: dev_actor_mode
  // on core would otherwise skip login redirect while writes still require an agent.
  let shouldRedirectToLogin = $derived(
    activeWorkspaceSlug &&
      !onLoginRoute &&
      shouldRedirectToLoginForBootstrapState(workspaceBootstrapState),
  );
  /**
   * Block the main workspace shell until hydrateWorkspace finishes (handshake +
   * dev actor mode probe). Without this, an authenticated session skips
   * awaitingIdentityMode (agent present) and the dashboard mounts while
   * devActorModeReady is still false, spamming parallel core requests and
   * exhausting browser connection limits.
   */
  let workspaceBootstrapPending = $derived(
    workspaceBootstrapState === WORKSPACE_BOOTSTRAP_STATES.HYDRATING,
  );
  let selectedActorName = $derived.by(() => {
    const resolvedName = lookupActorDisplayName(
      activeActorId || $authenticatedAgent?.actor_id,
      $actorRegistry,
      $principalRegistry,
    );
    // Prefer the human-readable actor name; fall back to the principal
    // username only when the registry cannot name this actor.
    const unresolved =
      !resolvedName ||
      resolvedName === activeActorId ||
      resolvedName === "Unknown actor";
    if ($authenticatedAgent?.username && unresolved) {
      return $authenticatedAgent.username;
    }
    return resolvedName || "Unknown identity";
  });

  /** Mirrors {@link hostedSession} for runes-friendly reactivity. */
  let hostedSessionSnap = $state(get(hostedSession));
  $effect(() => {
    const unsub = hostedSession.subscribe((value) => {
      hostedSessionSnap = value;
    });
    return () => unsub();
  });

  $effect(() => {
    if (!browser || !hostedMode) {
      return;
    }
    void loadHostedSession();
  });

  /**
   * The left nav collapses to an icon rail.
   *
   * The viewer's choice is remembered per viewer; a window too narrow for a
   * 232px nav plus a readable content column collapses it anyway, and the
   * remembered choice comes back when the window widens. See
   * `$lib/shellPanels.js` for why the override is not written to storage.
   */
  let navPreference = $state(null);
  let viewportWidth = $state(0);
  let navViewerId = $derived(
    $authenticatedAgent?.agent_id || $selectedActorId || "",
  );
  /*
   * Read once per viewer, not on every identity tick. The shell paints while
   * the viewer is still unknown, so a collapse clicked then is stored against
   * the anonymous key; re-reading the newly identified viewer's empty slot
   * would spring the nav back open a second after they closed it.
   */
  let navPreferenceViewerId = $state(/** @type {string | null} */ (null));
  $effect(() => {
    if (!browser) return;
    const viewerId = navViewerId;
    const previousViewerId = untrack(() => navPreferenceViewerId);
    if (previousViewerId === viewerId) return;
    navPreference = adoptPanelPreference(SHELL_PANELS.NAV, {
      fromViewerId: previousViewerId,
      toViewerId: viewerId,
    });
    navPreferenceViewerId = viewerId;
  });
  let navCollapsed = $derived(
    panelCollapsed({
      panel: SHELL_PANELS.NAV,
      preference: navPreference,
      viewportWidth,
    }),
  );
  let navAutoCollapsed = $derived(
    panelAutoCollapsed({ panel: SHELL_PANELS.NAV, viewportWidth }),
  );
  function toggleNav(collapsed) {
    navPreference = collapsed;
    writePanelPreference(SHELL_PANELS.NAV, collapsed, navViewerId);
  }

  let shellIdentity = $derived(
    computeWorkspaceShellIdentity({
      hostedMode,
      hostedAccount: hostedSessionSnap.account,
      selectedActorName,
      authenticatedAgent: $authenticatedAgent,
    }),
  );
  let initials = $derived(shellIdentity.initials);
  // Access lives inside this menu, so the trigger carries its badge. An
  // explicit aria-label on a button suppresses any name its children would
  // contribute, so the number has to be part of the label itself or a screen
  // reader never hears it.
  // Deciding access is human-only in core, so an agent principal would only
  // ever get 403 here. Do not ask.
  let accessBadgeEnabled = $derived(
    identityReady &&
      !workspaceBootstrapPending &&
      !shouldRedirectToLogin &&
      hasHumanAuthSession,
  );
  let pendingAccess = $derived(
    accessBadgeEnabled &&
      $pendingAccessCount.workspace === activeWorkspaceSlug &&
      !$pendingAccessCount.forbidden
      ? ($pendingAccessCount.count ?? 0)
      : 0,
  );
  let accountMenuLabel = $derived(
    pendingAccess > 0
      ? `Account menu, ${pendingAccessLabel(pendingAccess)}`
      : "Account menu",
  );
  let shellContentConfig = $derived(getShellContentConfig(currentAppPath));
  /*
   * What to paint while the workspace boots. Where the destination is a list
   * we know its name and its shape, so the first paint shows where the reader
   * landed instead of a spinner over the whole screen.
   */
  let destinationSkeleton = $derived(getDestinationSkeleton(currentAppPath));
  let moreBottomNavActive = $derived(isMoreHubActivePath(currentAppPath));
  const shellNavForTitle = [
    // PM setup must be tried before Ask: `/pm` prefix-matches it.
    ...settingsNavItems.filter((item) => item.href === "/pm/setup"),
    ...navigationItems,
    ...settingsNavItems,
  ];

  /*
   * A PM agent runs on the reader's own computer, so a workspace can have
   * none. Until one is connected Ask is not shown at all (setup is one hop
   * down under Settings), and every other PM surface is absent (see
   * `pm/onboardingState.js`). State the shell has not read yet shows
   * nothing: naming Ask on a guess is worse than showing it a moment late.
   */
  let pmState = $derived(
    $pmPresence.workspace === activeWorkspaceSlug ? $pmPresence : null,
  );
  let pmVisible = $derived(pmFeaturesVisible(pmState));
  /** Primary items the shell renders: Ask only while a PM is connected. */
  let shellPrimaryItems = $derived(
    navigationItems.filter((item) => !item.requiresPm || pmVisible),
  );

  let pageTitle = $derived(() => {
    const navItem = shellNavForTitle.find(
      (item) => isActive(item.href) && item.href !== "/",
    );
    const section = navItem?.label;
    const workspaceLabel = activeWorkspace?.label;
    const parts = [section, workspaceLabel, "Agent Nexus"].filter(Boolean);
    return parts.join(" · ");
  });

  let hostedCpOrigin = $derived(
    String(shellCapabilities.publicOrigin ?? "").trim(),
  );
  let sessionEndedWorkspaceUrl = $derived.by(() => {
    if (!browser || !activeWorkspaceSlug || !activeOrganizationSlug) {
      return "";
    }
    try {
      return `${window.location.origin}${workspacePath(activeOrganizationSlug, activeWorkspaceSlug, "/")}`;
    } catch {
      return "";
    }
  });

  const { loginRedirectGuard } = installWorkspaceBootstrapLoopGuards({
    browser,
  });
  const loginRedirectController = createLoginRedirectController();

  let handledPaletteRequests = 0;
  $effect(() => {
    const requested = $commandPaletteRequests;
    if (requested > handledPaletteRequests) {
      handledPaletteRequests = requested;
      if (activeWorkspaceSlug) {
        commandPaletteOpen = true;
      }
    }
  });

  $effect(() => {
    if (!browser) {
      return;
    }
    if (qaMode) {
      document.documentElement.dataset.qa = "1";
      return;
    }
    delete document.documentElement.dataset.qa;
  });

  $effect(() => {
    if (!browser || !shouldRedirectToLogin) {
      return;
    }
    if (currentAppPath === "/login") {
      return;
    }
    const org = activeOrganizationSlug;
    const ws = activeWorkspaceSlug;
    if (!org || !ws) {
      return;
    }

    const destination = buildLoginRedirectDestination({
      hostedMode,
      organizationSlug: org,
      workspaceSlug: ws,
      workspaceId: $page.data?.workspace?.workspaceId,
      currentAppPath,
      search: $page.url.search,
      workspacePath,
    });

    void loginRedirectController.redirectIfNeeded({
      destination,
      workspaceSlug: ws,
      loginRedirectGuard,
    });
  });

  $effect(() => {
    if (!browser) {
      return;
    }

    const workspaceSlug = activeWorkspaceSlug;
    if (!workspaceSlug) {
      return;
    }

    setCurrentOrganizationSlug(
      activeOrganizationSlug || $page.params?.organization || "",
    );
    setCurrentWorkspaceSlug(workspaceSlug);
    setCurrentCoreBaseUrl(activeWorkspace?.coreBaseUrl ?? "");
    const workspaceKey = `${activeOrganizationSlug}/${workspaceSlug}`;
    if (hydratedWorkspaceSlug === workspaceKey) {
      if (hydration.key !== workspaceKey) {
        releaseActivation();
        hydration.sequence += 1;
        hydration.controller?.abort();
        clearTimeout(hydration.timer);
      }
      activationError = "";
      activationPending = false;
      return;
    }

    const attemptPath = `${workspaceKey}:${$page.url.pathname}`;
    if (hydration.attemptPath === attemptPath) return;
    // Activation records its path; do not track its single-flight state here.
    untrack(() => void activateCurrentWorkspace());
  });

  $effect(() => {
    if (!browser) return;
    const resumeActivation = () => {
      const key = `${activeOrganizationSlug}/${activeWorkspaceSlug}`;
      if (
        document.hidden ||
        !navigator.onLine ||
        !activeWorkspaceSlug ||
        hydratedWorkspaceSlug === key
      )
        return;
      const now = Date.now();
      if (
        activationPending &&
        hydration.key === key &&
        now - hydration.startedAt >= ACTIVATION_DEADLINE_MS
      )
        hydration.expire?.();
      if (activationPending) return;
      // Event bursts share one attempt, with at most one automatic retry per
      // workspace every 15 seconds. There is no timer-driven retry loop.
      if (
        hydration.resumeKey === key &&
        now - hydration.resumedAt < ACTIVATION_DEADLINE_MS
      )
        return;
      hydration.resumeKey = key;
      hydration.resumedAt = now;
      hydration.attemptPath = "";
      void activateCurrentWorkspace();
    };
    document.addEventListener("visibilitychange", resumeActivation);
    window.addEventListener("online", resumeActivation);
    return () => {
      document.removeEventListener("visibilitychange", resumeActivation);
      window.removeEventListener("online", resumeActivation);
      releaseActivation();
      hydration.sequence += 1;
      hydration.controller?.abort();
      clearTimeout(hydration.timer);
    };
  });

  /*
   * A PM can be installed or stopped while this tab sits in the background,
   * so re-read the state when the reader comes back — at most once a minute,
   * and never while the tab is hidden. The PM setup page runs its own faster
   * poll while it waits for a first heartbeat.
   */
  $effect(() => {
    if (!browser) return;
    const workspaceSlug = activeWorkspaceSlug;
    if (!workspaceSlug) {
      clearPmPresence();
      return;
    }
    const onVisibility = () => {
      if (document.hidden) return;
      void refreshPmPresence(workspaceSlug, {
        minIntervalMs: SHELL_REFRESH_MIN_MS,
      });
    };
    document.addEventListener("visibilitychange", onVisibility);
    return () => document.removeEventListener("visibilitychange", onVisibility);
  });

  $effect(() => {
    if (!browser) {
      return;
    }

    const workspaceSlug = activeWorkspaceSlug;
    if (!workspaceSlug || !$authSessionReady) {
      return;
    }

    const seedPrincipal = $authenticatedAgent ? [$authenticatedAgent] : [];
    void refreshPrincipals(workspaceSlug, seedPrincipal);
  });

  /*
   * Whether this workspace has a PM decides which surfaces exist at all, so
   * read it as soon as there is a session to read with — and again whenever
   * the identity changes.
   *
   * Keyed on the identity rather than read once after hydration: core needs a
   * session for `pm.presence`, and in the actor gate the reader creates their
   * identity *after* first paint. A single read then answered 401 and the
   * state stayed unknown for the rest of the session, which now means no PM
   * surface at all. This re-reads when the identity arrives, so a read that
   * raced the session repairs itself instead of stranding the PM.
   */
  $effect(() => {
    if (!browser) return;
    const workspaceSlug = activeWorkspaceSlug;
    if (!workspaceSlug || !$authSessionReady) return;
    // Tracked so the read runs again for a different principal.
    void $authenticatedAgent?.agent_id;
    void $selectedActorId;
    void refreshPmPresence(workspaceSlug, { force: true });
  });

  $effect(() => {
    if (
      !browser ||
      !$devActorMode ||
      !$devActorModeReady ||
      !activeWorkspaceSlug
    ) {
      return;
    }
    // Bootstrap already loaded them for this workspace; a second read on
    // every ready flip only adds to the auth chatter on first paint.
    if (devFixturePersonas.length > 0) return;
    /*
     * ...and when the read comes back empty — no `.dev/local-identities.json`,
     * or a 404 under a mount prefix — `devFixturePersonas` is assigned a fresh
     * `[]` whose identity differs from the last one, so this effect's own
     * dependency changes and it runs again. That is a request loop: the dev
     * loop guard counts 26 reads of `/auth/dev/identities` in two seconds on
     * every first paint, and the auth chatter delays the page behind it.
     *
     * One attempt per workspace, tracked outside the reactive graph the way
     * `hydration.attemptPath` is, so recording the attempt cannot itself
     * schedule another one.
     */
    if (devPersonaLoad.attempted === activeWorkspaceSlug) return;
    devPersonaLoad.attempted = activeWorkspaceSlug;
    void loadDevFixturePersonas();
  });

  async function loadDevFixturePersonas(workspaceSlug = activeWorkspaceSlug) {
    devFixturePersonas = await loadWorkspaceDevFixturePersonas({
      workspaceSlug,
      workspaceHeader: WORKSPACE_HEADER,
      organizationSlug: activeOrganizationSlug,
    });
    return devFixturePersonas;
  }

  /**
   * Writes the workspace refresh cookie from `.dev/local-identities.json` and
   * hydrates UI state. Used by the fixture dropdown and by the actor gate when
   * picking a seeded human (Secrets and other authenticated routes need this).
   */
  async function activateDevPersonaSession(personaId) {
    return activateWorkspaceDevPersonaSession({
      personaId,
      workspaceSlug: activeWorkspaceSlug,
      workspaceHeader: WORKSPACE_HEADER,
      organizationSlug: activeOrganizationSlug,
      setBusy: (busy) => {
        devPersonaBusy = busy;
      },
      onHydrate: hydrateWorkspace,
    });
  }

  async function switchDevFixturePersona(personaId) {
    const trimmed = String(personaId ?? "").trim();
    if (!activeWorkspaceSlug || devPersonaBusy || !trimmed) {
      return;
    }
    devPersonaError = "";
    const result = await activateDevPersonaSession(trimmed);
    if (result?.ok) return;
    const label =
      devFixturePersonas.find(
        (persona) => String(persona?.persona_id ?? "") === trimmed,
      )?.display_label || trimmed;
    devPersonaError =
      result?.status === 502
        ? `Could not switch to ${label}: its seeded sign-in is used up. Re-run make serve to re-seed; you are still signed in as before.`
        : `Could not switch to ${label} (HTTP ${result?.status || "error"}); you are still signed in as before.`;
  }

  async function establishDevHumanSessionForActor(actorId) {
    if (!browser || !$devActorMode || !activeWorkspaceSlug) {
      return;
    }
    const personas =
      devFixturePersonas.length > 0
        ? devFixturePersonas
        : await loadDevFixturePersonas(activeWorkspaceSlug);
    const personaId = personas.find(
      (p) => p.actor_id === actorId && p.principal_kind === "human",
    )?.persona_id;
    if (!personaId) {
      return;
    }
    const result = await activateDevPersonaSession(personaId);
    if (!result.ok) {
      actorError =
        result.status === 404
          ? "No dev auth token for this human identity. Use `make serve` with default identity seeding, or sign in without passkey on /login."
          : "Could not open a dev auth session for this identity.";
    } else {
      actorError = "";
    }
  }

  /**
   * A retired activation's own cleanup is ignored by its `isCurrent` guard,
   * so release the busy states it set, or they stay on until a reload.
   */
  function releaseActivation() {
    hydration.release?.();
    hydration.release = null;
  }

  async function activateCurrentWorkspace() {
    const key = `${activeOrganizationSlug}/${activeWorkspaceSlug}`;
    if (!activeWorkspaceSlug || (activationPending && hydration.key === key))
      return;
    releaseActivation();
    const sequence = ++hydration.sequence;
    hydration.controller?.abort();
    clearTimeout(hydration.timer);
    const controller = new AbortController();
    hydration.controller = controller;
    // Bootstrap flags are shared by the active workspace. If navigation
    // returns to an earlier workspace mid-activation, it must hydrate again.
    hydratedWorkspaceSlug = "";
    hydration.key = key;
    hydration.attemptPath = `${key}:${$page.url.pathname}`;
    const isCurrent = () =>
      sequence === hydration.sequence &&
      key === `${activeOrganizationSlug}/${activeWorkspaceSlug}`;
    activationError = "";
    activationPending = true;
    hydration.startedAt = Date.now();
    const owned = { personaBusy: false, loadingActors: false };
    hydration.release = () => {
      if (owned.personaBusy) devPersonaBusy = false;
      if (owned.loadingActors) loadingActors = false;
    };
    const expire = () => {
      if (!isCurrent()) return;
      // Retire the ticket before aborting: late failures and results cannot
      // overwrite the timeout or a subsequent successful activation.
      releaseActivation();
      hydration.sequence += 1;
      controller.abort();
      activationPending = false;
      activationError =
        "Opening this workspace took too long. Please try again.";
    };
    hydration.expire = expire;
    const timer = setTimeout(expire, ACTIVATION_DEADLINE_MS);
    hydration.timer = timer;
    try {
      const result = await hydrateWorkspace(
        activeWorkspaceSlug,
        (message) => {
          if (isCurrent()) activationError = message;
        },
        { isCurrent, signal: controller.signal, owned },
      );
      if (!isCurrent()) return;
      if (result === false) {
        activationError = "Could not open this workspace. Please try again.";
      } else {
        hydratedWorkspaceSlug = key;
        activationError = "";
      }
    } catch (error) {
      if (isCurrent())
        activationError = isWorkspaceUnavailableError(error)
          ? error.message
          : "Could not open this workspace. Please try again.";
    } finally {
      clearTimeout(timer);
      if (isCurrent()) activationPending = false;
    }
  }

  async function hydrateWorkspace(
    workspaceSlug,
    onActivationRetry = () => {},
    { isCurrent = () => true, signal, owned = {} } = {},
  ) {
    const { client, fetchFn } = createWorkspaceActivationTransport(signal);
    return hydrateWorkspaceBootstrap({
      isCurrent,
      signal,
      fetchFn,
      onActivationRetry,
      workspaceSlug,
      hostedSession: hostedMode ? $page.data.workspaceSession : undefined,
      workspaceHeader: WORKSPACE_HEADER,
      organizationSlug: activeOrganizationSlug,
      coreClient: client,
      storage: localStorage,
      onActorError: (message) => {
        if (isCurrent()) actorError = message;
      },
      onLoadingActors: (loading) => {
        if (isCurrent()) loadingActors = owned.loadingActors = loading;
      },
      onDevPersonaBusy: (busy) => {
        if (isCurrent()) devPersonaBusy = owned.personaBusy = busy;
      },
      onDevFixturePersonas: (personas) => {
        if (isCurrent()) devFixturePersonas = personas;
      },
      refreshActors: (slug) => refreshActors(slug, client, isCurrent, owned),
    });
  }

  async function refreshActors(
    workspaceSlug = activeWorkspaceSlug,
    client = coreClient,
    isCurrent = () => true,
    owned = {},
  ) {
    loadingActors = owned.loadingActors = true;
    actorError = "";

    try {
      const response = await client.listActors();
      if (!isCurrent()) return [];
      const actors = response.actors ?? [];
      replaceActorRegistry(actors, workspaceSlug);
      setActorDirectoryIncomplete(
        Boolean(
          String(response?.next_cursor ?? "").trim() ||
          response?.has_more === true,
        ),
        workspaceSlug,
      );
      return actors;
    } catch (error) {
      if (!isCurrent()) return [];
      const reason = error instanceof Error ? error.message : String(error);
      actorError = `Failed to load actors: ${reason}`;
      replaceActorRegistry([], workspaceSlug);
      setActorDirectoryIncomplete(true, workspaceSlug);
      return [];
    } finally {
      if (isCurrent()) loadingActors = owned.loadingActors = false;
    }
  }

  async function refreshPrincipals(
    workspaceSlug = activeWorkspaceSlug,
    seedPrincipals = [],
  ) {
    await refreshWorkspacePrincipals({
      coreClient,
      workspaceSlug,
      seedPrincipals,
    });
  }

  async function selectActor(actorId) {
    if ($authenticatedAgent || !activeWorkspaceSlug || devPersonaBusy) {
      return;
    }
    chooseActor(actorId, localStorage, activeWorkspaceSlug);
    await establishDevHumanSessionForActor(actorId);
  }

  async function switchIdentity() {
    if (!activeWorkspaceSlug) {
      return;
    }

    if ($authenticatedAgent) {
      await logoutAuthSession({
        workspaceSlug: activeWorkspaceSlug,
        clearActor: true,
      });
      window.location.assign(workspaceHref("/login"));
      return;
    }
    clearSelectedActor(localStorage, activeWorkspaceSlug);
  }

  function buildActorId(displayName) {
    const base = displayName
      .toLowerCase()
      .trim()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 24);
    const suffix = Math.random().toString(36).slice(2, 8);
    return `actor-${base || "user"}-${suffix}`;
  }

  async function createActor() {
    if (!newActorName.trim()) {
      actorError = "Display name is required.";
      return;
    }

    creatingActor = true;
    actorError = "";

    try {
      const payload = buildActorCreatePayload({
        id: buildActorId(newActorName),
        displayName: newActorName.trim(),
        tags: ["human"],
      });

      const response = await coreClient.createActor(payload);
      const createdActor = response.actor;
      replaceActorRegistry(
        [...$actorRegistry, createdActor],
        activeWorkspaceSlug,
      );
      chooseActor(createdActor.id, localStorage, activeWorkspaceSlug);
      newActorName = "";
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      actorError = `Failed to create identity: ${reason}`;
    } finally {
      creatingActor = false;
    }
  }

  function isActive(href) {
    if (href === "/decide" && currentAppPath === "/") return true;
    // `/pm` prefix-matches PM setup, which is not the conversation.
    if (href === "/pm" && currentAppPath.startsWith("/pm/setup")) return false;
    return currentAppPath === href || currentAppPath.startsWith(`${href}/`);
  }

  let workspaceHref = $derived(
    bindWorkspaceHref(activeOrganizationSlug, activeWorkspaceSlug),
  );

  function switchDestination(workspace) {
    // Resource IDs belong to one workspace. Switch to the corresponding list.
    const rawSection = currentAppPath.split("/")[1] || "decide";
    const section =
      { inbox: "decide", overview: "watch", tasks: "initiatives" }[
        rawSection
      ] ?? rawSection;
    return workspacePath(
      workspace.organizationSlug,
      workspace.slug,
      `/${section}`,
    );
  }

  async function switchWorkspace(workspace) {
    if (
      !workspace ||
      (workspace.slug === activeWorkspaceSlug &&
        workspace.organizationSlug === activeOrganizationSlug)
    )
      return;
    await goto(switchDestination(workspace));
  }

  function preloadWorkspace(workspace) {
    if (!workspace._loadFailed)
      void preloadData(switchDestination(workspace)).catch(() => {});
  }

  $effect(() => {
    if (!browser || !hostedMode) return;
    return startWorkspaceSessionMaintenance();
  });

  /** @param {string} href */
  function dataTourForNav(href) {
    switch (String(href)) {
      case "/decide":
        return "decide";
      case "/watch":
        return "watch";
      case "/pm":
        return "pm";
      case "/agents":
        return "agents";
      case "/initiatives":
        return "initiatives";
      case "/docs":
        return "docs";
      case "/access":
        return "access";
      default:
        return undefined;
    }
  }

  function workspaceInitials(label) {
    return (label || "?")
      .split(/[\s-]+/)
      .map((w) => w[0])
      .join("")
      .slice(0, 2)
      .toUpperCase();
  }

  function toggleWorkspacePicker() {
    workspacePickerOpen = !workspacePickerOpen;
  }

  function closeWorkspacePicker() {
    workspacePickerOpen = false;
  }

  function pickWorkspace(slug) {
    closeWorkspacePicker();
    switchWorkspace(slug);
  }

  function isTextEntryElement(target) {
    return (
      target instanceof HTMLElement &&
      (target.isContentEditable ||
        ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName))
    );
  }
  function handleWindowKeydown(event) {
    if (event.key === "k" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      if (activeWorkspaceSlug) {
        commandPaletteOpen = !commandPaletteOpen;
      }
      return;
    }
    // Ask PM is the product's primary verb; it gets a global shortcut, but
    // never while the reader is typing or the palette is open. With no PM
    // onboarded there is nothing to ask, so the key does nothing rather than
    // opening a surface that cannot answer.
    if (
      event.key === "j" &&
      (event.metaKey || event.ctrlKey) &&
      !event.shiftKey &&
      !event.altKey &&
      !commandPaletteOpen &&
      !isTextEntryElement(event.target)
    ) {
      // Claim the key either way: letting the browser's own ⌘J through
      // (downloads, in some browsers) is not "nothing happens".
      event.preventDefault();
      if (pmVisible && activeWorkspaceSlug) {
        void goto(workspaceHref("/pm"));
      }
      return;
    }
    if (!event.defaultPrevented) {
      handleModSave(event, { commandPaletteOpen });
    }
    if (!event.defaultPrevented) {
      handleModEnterFormSubmit(event, { commandPaletteOpen });
    }
    if (!event.defaultPrevented) {
      handleModEnterBlurCommit(event, { commandPaletteOpen });
    }
    if (event.key === "Escape" && accountMenuOpen) {
      closeAccountMenu({ restoreFocus: true });
      return;
    }
    if (event.key === "Escape" && workspacePickerOpen) {
      closeWorkspacePicker();
      return;
    }
    if (!event.defaultPrevented) {
      handleEscapeTextBlurCommit(event, { commandPaletteOpen });
    }
  }

  function closeAccountMenu({ restoreFocus = false } = {}) {
    accountMenuOpen = false;
    personaSubmenuOpen = false;
    if (restoreFocus) document.querySelector(".shell-account-row")?.focus?.();
  }

  function toggleAccountMenu() {
    accountMenuOpen = !accountMenuOpen;
    if (!accountMenuOpen) {
      personaSubmenuOpen = false;
      return;
    }
    // A menu that opens without taking the keyboard is invisible to a
    // screen reader; the first item gets focus, Escape hands it back.
    void tick().then(() =>
      document
        .querySelector('#shell-account-container [role="menuitem"]')
        ?.focus?.(),
    );
  }

  function handleWindowClick(event) {
    if (accountMenuOpen) {
      const account = document.getElementById("shell-account-container");
      if (account && !account.contains(event.target)) {
        closeAccountMenu();
      }
    }
    if (workspacePickerOpen) {
      const picker = document.getElementById("workspace-picker-container");
      if (picker && !picker.contains(event.target)) {
        closeWorkspacePicker();
      }
    }
  }
</script>

<svelte:head>
  <title>{$pageTitleOverride || pageTitle()}</title>
</svelte:head>

<svelte:window
  onkeydown={handleWindowKeydown}
  onclick={handleWindowClick}
  bind:innerWidth={viewportWidth}
/>

<div class="shell-root">
  {#if !activeWorkspaceSlug}
    {@render children()}
  {:else if renderLoginOnly}
    {@render children()}
  {:else if gateVisible}
    <main class="actor-gate-wrap">
      <section class="actor-gate-card">
        <div class="actor-gate-header">
          <p class="actor-gate-eyebrow">Who are you?</p>
          <h1>Select Actor Identity</h1>
          <p>Pick an existing identity or create a new one.</p>
        </div>

        {#if actorError}
          <div class="actor-gate-error" role="alert">{actorError}</div>
        {/if}

        <div class="actor-gate-list" aria-live="polite">
          {#if loadingActors}
            <p class="actor-gate-empty">Loading sign-in options…</p>
          {:else if $actorRegistry.length === 0}
            <p class="actor-gate-empty">
              No identities yet. Create one to get started.
            </p>
          {:else}
            {#each filterActorsForUserSelection($actorRegistry) as actor}
              <button
                class="actor-gate-item"
                disabled={devPersonaBusy}
                onclick={() => void selectActor(actor.id)}
                type="button"
              >
                <span class="actor-gate-avatar" aria-hidden="true"
                  >{(actor.display_name || "?").slice(0, 1).toUpperCase()}</span
                >
                <span class="actor-gate-meta">
                  <span class="actor-gate-name">{actor.display_name}</span>
                </span>
              </button>
            {/each}
          {/if}
        </div>

        <form
          class="actor-gate-create"
          onsubmit={(event) => {
            event.preventDefault();
            createActor();
          }}
        >
          <label for="actor-display-name">Display name</label>
          <div class="actor-gate-input-row">
            <input
              bind:value={newActorName}
              id="actor-display-name"
              name="actor-display-name"
              placeholder="Type a name"
              type="text"
            />
            <button disabled={creatingActor} type="submit">
              {creatingActor ? "Creating…" : "Create and continue"}
            </button>
          </div>
        </form>

        <p class="actor-gate-empty">
          Prefer authenticated access? <a href={workspaceHref("/login")}
            >Sign in with a passkey.</a
          >
        </p>
      </section>
    </main>
  {:else}
    <div class="shell-frame">
      <aside
        class="shell-sidebar"
        class:shell-sidebar--collapsed={navCollapsed}
        aria-label="Primary"
        data-shell-nav={navCollapsed ? "collapsed" : "expanded"}
      >
        <div class="shell-sidebar-top">
          <!--
            The toggle sits above Search in both states, so the control that
            collapses the nav does not move when you use it.
          -->
          <div class="shell-sidebar-toggle-row">
            <PanelToggle
              label="menu"
              side="left"
              collapsed={navCollapsed}
              auto={navAutoCollapsed}
              onToggle={toggleNav}
            />
          </div>
          <button
            class="shell-search-trigger"
            onclick={() => (commandPaletteOpen = true)}
            type="button"
            aria-label="Search the workspace"
            title={navCollapsed ? "Search the workspace" : undefined}
          >
            <svg
              class="shell-search-trigger-icon"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
              stroke-linejoin="round"
              aria-hidden="true"
            >
              <path d={navIconPath("search")} />
            </svg>
            <span class="shell-nav-copy">Search</span>
            <kbd class="shell-search-kbd">⌘K</kbd>
          </button>
        </div>

        <div class="shell-sidebar-main">
          {#if hasMultipleWorkspaces}
            <div class="workspace-switcher" id="workspace-picker-container">
              <button
                class="workspace-switcher-trigger"
                onclick={toggleWorkspacePicker}
                aria-expanded={workspacePickerOpen}
                aria-haspopup="listbox"
                type="button"
              >
                <span class="workspace-switcher-icon" aria-hidden="true">
                  {workspaceInitials(activeWorkspace?.label)}
                </span>
                <span class="workspace-switcher-label">
                  <span class="workspace-switcher-name"
                    >{activeWorkspace?.label ||
                      activeWorkspaceSlug ||
                      "Workspace"}</span
                  >
                  <span class="workspace-switcher-sub"
                    >{workspaceSwitcherSub}</span
                  >
                </span>
                <svg
                  class="workspace-switcher-chevron"
                  class:workspace-switcher-chevron--open={workspacePickerOpen}
                  viewBox="0 0 20 20"
                  fill="currentColor"
                  aria-hidden="true"
                >
                  <path
                    fill-rule="evenodd"
                    d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z"
                    clip-rule="evenodd"
                  />
                </svg>
              </button>

              {#if workspacePickerOpen}
                <div
                  class="workspace-switcher-dropdown"
                  role="listbox"
                  aria-label="Switch workspace"
                >
                  {#each data.workspaces ?? [] as workspace}
                    {@const isCurrent =
                      workspace.slug === activeWorkspaceSlug &&
                      workspace.organizationSlug === activeOrganizationSlug}
                    {@const loadFailed = workspace._loadFailed}
                    <button
                      class="workspace-switcher-option"
                      class:workspace-switcher-option--active={isCurrent}
                      role="option"
                      aria-selected={isCurrent}
                      onclick={() => !loadFailed && pickWorkspace(workspace)}
                      onpointerenter={() => preloadWorkspace(workspace)}
                      onfocus={() => preloadWorkspace(workspace)}
                      disabled={loadFailed}
                      type="button"
                    >
                      <span
                        class="workspace-switcher-option-icon"
                        aria-hidden="true"
                      >
                        {workspaceInitials(workspace.label)}
                      </span>
                      <span class="workspace-switcher-option-label">
                        <span
                          >{workspace.label ||
                            workspace.slug ||
                            "(unknown)"}</span
                        >
                        {#if loadFailed}
                          <span
                            class="workspace-switcher-option-desc text-fg-subtle"
                            >(failed to load)</span
                          >
                        {:else if workspace.description}
                          <span class="workspace-switcher-option-desc"
                            >{workspace.description}</span
                          >
                        {/if}
                      </span>
                      {#if isCurrent}
                        <svg
                          class="workspace-switcher-check"
                          viewBox="0 0 20 20"
                          fill="currentColor"
                          aria-hidden="true"
                        >
                          <path
                            fill-rule="evenodd"
                            d="M16.704 4.153a.75.75 0 01.143 1.052l-8 10.5a.75.75 0 01-1.127.075l-4.5-4.5a.75.75 0 011.06-1.06l3.894 3.893 7.48-9.817a.75.75 0 011.05-.143z"
                            clip-rule="evenodd"
                          />
                        </svg>
                      {/if}
                    </button>
                  {/each}
                </div>
              {/if}
            </div>
          {/if}

          <nav class="shell-nav" aria-label="Primary">
            {#each shellPrimaryItems as item (item.href)}
              {@const active = isActive(item.href)}
              {@const tour = dataTourForNav(item.href)}
              <a
                class={`shell-nav-link ${active ? "shell-nav-link--active" : ""}`}
                href={workspaceHref(item.href)}
                aria-label={item.label}
                title={navCollapsed ? item.label : undefined}
                data-tour={tour}
              >
                <svg
                  class="shell-nav-icon"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                  stroke-width="1.5"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  aria-hidden="true"
                >
                  <path d={navIconPath(item.icon)} />
                </svg>
                <span class="shell-nav-copy">
                  <span>{item.label}</span>
                  {#if item.hint}
                    <span class="shell-nav-hint">{item.hint}</span>
                  {/if}
                </span>
                {#if item.count === "decide-waiting"}
                  <DecideNavCount
                    workspace={activeWorkspaceSlug}
                    enabled={identityReady &&
                      !workspaceBootstrapPending &&
                      !shouldRedirectToLogin}
                  />
                {/if}
              </a>
            {/each}
          </nav>
        </div>

        <div class="shell-sidebar-footer">
          {#if devPersonaError}
            <p class="shell-persona-error" role="alert">
              {devPersonaError}
              <button
                class="ui-prose-link"
                type="button"
                onclick={() => (devPersonaError = "")}>Dismiss</button
              >
            </p>
          {/if}
          <div class="shell-account" id="shell-account-container">
            {#if accountMenuOpen}
              <div class="shell-account-menu" role="menu">
                <div class="shell-account-menu-header">
                  <p class="shell-account-menu-identity">
                    {shellIdentity.primaryLabel}
                  </p>
                  {#if shellIdentity.secondaryLabel}
                    <p
                      class="shell-account-menu-handle"
                      title={shellIdentity.secondaryLabel}
                    >
                      {shellIdentity.secondaryLabel}
                    </p>
                  {/if}
                </div>

                <!-- Settings, then Diagnostics, each under its own label;
                     Sign out stays last. -->
                {#each settingsNavGroups as group, groupIndex (group.label)}
                  {#if groupIndex > 0}
                    <div class="shell-account-menu-divider"></div>
                  {/if}
                  <div
                    class="shell-account-menu-section"
                    role="group"
                    aria-labelledby={`shell-account-group-${groupIndex}`}
                  >
                    <p
                      class="shell-account-menu-group-label"
                      id={`shell-account-group-${groupIndex}`}
                    >
                      {group.label}
                    </p>
                    {#each group.items as item}
                      {@const active = isActive(item.href)}
                      {@const tour = dataTourForNav(item.href)}
                      <a
                        class={`shell-settings-link ${active ? "shell-settings-link--active" : ""}`}
                        href={workspaceHref(item.href)}
                        role="menuitem"
                        data-tour={tour}
                        onclick={closeAccountMenu}
                      >
                        <svg
                          class="shell-settings-icon"
                          fill="none"
                          viewBox="0 0 24 24"
                          stroke="currentColor"
                          stroke-width="1.5"
                          stroke-linecap="round"
                          stroke-linejoin="round"
                          aria-hidden="true"
                        >
                          <path d={navIconPath(item.icon)} />
                        </svg>
                        <span class="shell-settings-link-text"
                          >{item.label}</span
                        >
                        {#if item.count === "access-pending"}
                          <AccessNavCount
                            workspace={activeWorkspaceSlug}
                            enabled={accessBadgeEnabled}
                          />
                        {/if}
                      </a>
                    {/each}
                  </div>
                {/each}

                {#if $devActorMode && devFixturePersonas.length > 0}
                  <div class="shell-account-menu-divider"></div>
                  <button
                    class="shell-account-menu-action"
                    type="button"
                    aria-expanded={personaSubmenuOpen}
                    disabled={devPersonaBusy}
                    onclick={() => (personaSubmenuOpen = !personaSubmenuOpen)}
                  >
                    <svg
                      class="shell-account-icon"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      stroke-width="1.5"
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      aria-hidden="true"
                    >
                      <path d={navIconPath("persona")} />
                    </svg>
                    <span class="shell-nav-copy">Switch fixture persona</span>
                    <svg
                      class={`shell-account-menu-chevron ${personaSubmenuOpen ? "shell-account-menu-chevron--open" : ""}`}
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      stroke-width="1.5"
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      aria-hidden="true"
                    >
                      <path d="M19 9l-7 7-7-7" />
                    </svg>
                  </button>
                  {#if personaSubmenuOpen}
                    <div class="shell-account-persona-list">
                      {#each devFixturePersonas as persona}
                        <button
                          class="shell-account-menu-action"
                          type="button"
                          disabled={devPersonaBusy}
                          onclick={() => {
                            closeAccountMenu();
                            void switchDevFixturePersona(persona.persona_id);
                          }}
                        >
                          <span class="shell-nav-copy"
                            >{persona.display_label}</span
                          >
                        </button>
                      {/each}
                    </div>
                  {/if}
                {/if}

                <div class="shell-account-menu-divider"></div>
                <div class="shell-account-menu-section">
                  {#if hostedMode}
                    <a
                      class="shell-account-link"
                      href={hostedAccountPath}
                      role="menuitem"
                      onclick={closeAccountMenu}
                    >
                      <svg
                        class="shell-account-icon"
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                        stroke-width="1.5"
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        aria-hidden="true"
                      >
                        <path d={navIconPath("account")} />
                      </svg>
                      <span class="shell-account-link-text">Account</span>
                    </a>
                  {/if}
                  <button
                    class="shell-account-menu-action shell-account-menu-action--danger"
                    type="button"
                    onclick={() => {
                      closeAccountMenu();
                      void switchIdentity();
                    }}
                  >
                    <svg
                      class="shell-account-icon"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      stroke-width="1.5"
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      aria-hidden="true"
                    >
                      <path d={navIconPath("signOut")} />
                    </svg>
                    <span class="shell-nav-copy"
                      >{$authenticatedAgent
                        ? "Sign out"
                        : "Switch identity"}</span
                    >
                  </button>
                </div>
              </div>
            {/if}

            <button
              class={`shell-account-row ${accountMenuOpen ? "shell-account-row--open" : ""}`}
              type="button"
              aria-expanded={accountMenuOpen}
              aria-haspopup="menu"
              aria-label={accountMenuLabel}
              onclick={toggleAccountMenu}
            >
              <span class="shell-account-avatar" aria-hidden="true"
                >{initials}</span
              >
              <span
                class="shell-account-name"
                title={shellIdentity.primaryLabel}
                >{shellIdentity.primaryLabel}</span
              >
              <!-- Access lives inside this menu, so what waits there has to
                   be visible while the menu is shut. -->
              <AccessNavCount
                workspace={activeWorkspaceSlug}
                enabled={accessBadgeEnabled}
                variant="trigger"
              />
              <svg
                class="shell-account-chevron"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
                stroke-linejoin="round"
                aria-hidden="true"
              >
                <path d="M19 9l-7 7-7-7" />
              </svg>
            </button>
          </div>
        </div>
      </aside>

      <div class="shell-main">
        {#if identityReady && !renderLoginOnly && !gateVisible}
          <UnsentIndicator />
        {/if}
        <main class="shell-main-scroll">
          <div
            class={`shell-content shell-content--${shellContentConfig.mode}`}
            style={`--shell-content-max: ${shellContentConfig.maxWidth}`}
          >
            {#if $staleChunkRecoveryNeeded}
              <div class="p-6" role="alert" data-workspace-update-error>
                <p>This page could not be loaded. Reload to try again.</p>
                <button
                  class="btn-secondary mt-3"
                  type="button"
                  onclick={() => window.location.reload()}>Retry</button
                >
              </div>
            {/if}
            {#if activationError}
              <div class="p-6" role="alert" data-workspace-activation-error>
                <p>{activationError}</p>
                <button
                  class="ui-btn-secondary mt-3"
                  type="button"
                  disabled={activationPending}
                  onclick={() => void activateCurrentWorkspace()}
                >
                  {activationPending ? "Retrying…" : "Retry"}
                </button>
              </div>
            {:else if !identityReady || workspaceBootstrapPending || shouldRedirectToLogin || hydratedWorkspaceSlug !== `${activeOrganizationSlug}/${activeWorkspaceSlug}`}
              <div aria-label="Loading workspace content" aria-busy="true">
                <p class="sr-only" role="status">Loading workspace…</p>
                {#if destinationSkeleton}
                  <WorkspacePageSkeleton
                    title={destinationSkeleton.title}
                    rows={destinationSkeleton.rows}
                  />
                {:else}
                  <div class="space-y-5 p-6">
                    <p class="text-meta text-fg-muted">Loading workspace…</p>
                    <div
                      class="h-6 w-48 animate-pulse rounded bg-panel-hover"
                      aria-hidden="true"
                    ></div>
                    <div
                      class="h-40 animate-pulse rounded-md border border-line bg-panel"
                      aria-hidden="true"
                    ></div>
                  </div>
                {/if}
              </div>
            {:else}
              {#key $readerScope}
                <WorkspaceRouteContent>
                  <svelte:boundary onerror={onWorkspaceRenderError}>
                    {@render children?.()}
                    {#snippet failed(error, reset)}
                      <div class="p-6" role="alert" data-workspace-render-error>
                        <p>This page could not be displayed.</p>
                        <button
                          class="btn-secondary mt-3"
                          type="button"
                          onclick={() => {
                            recoverStaleChunk(error);
                            clearWorkspaceViews();
                            reset();
                          }}>Retry</button
                        >
                      </div>
                    {/snippet}
                  </svelte:boundary>
                </WorkspaceRouteContent>
              {/key}
            {/if}
          </div>
        </main>
      </div>
    </div>

    <nav class="shell-bottom-nav" aria-label="Primary navigation">
      {#each shellPrimaryItems as item (item.href)}
        {@const active = isActive(item.href)}
        {@const tour = dataTourForNav(item.href)}
        <a
          class="shell-bottom-nav-item relative {active
            ? 'shell-bottom-nav-item--active'
            : ''}"
          href={workspaceHref(item.href)}
          aria-current={active ? "page" : undefined}
          data-tour={tour}
        >
          <svg
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d={navIconPath(item.icon)} />
          </svg>
          <span class="shell-bottom-nav-label">{item.label}</span>
          {#if item.count === "decide-waiting"}
            <DecideNavCount
              workspace={activeWorkspaceSlug}
              enabled={identityReady &&
                !workspaceBootstrapPending &&
                !shouldRedirectToLogin}
              variant="bottom"
            />
          {/if}
        </a>
      {/each}
      <a
        class="shell-bottom-nav-item {moreBottomNavActive
          ? 'shell-bottom-nav-item--active'
          : ''}"
        href={workspaceHref("/more")}
        aria-current={moreBottomNavActive ? "page" : undefined}
      >
        <svg
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="2.25"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d={navIconPath("more")} />
        </svg>
        <span class="shell-bottom-nav-label">More</span>
      </a>
    </nav>
  {/if}

  {#if activeWorkspaceSlug}
    <CommandPalette
      bind:open={commandPaletteOpen}
      organizationSlug={activeOrganizationSlug}
      workspaceSlug={activeWorkspaceSlug}
    />
  {/if}

  {#if activeWorkspaceSlug && identityReady && !workspaceBootstrapPending && !renderLoginOnly && !gateVisible && $devActorModeReady}
    <WorkspaceTour
      organizationSlug={activeOrganizationSlug}
      workspaceSlug={activeWorkspaceSlug}
      devActorModeReady={$devActorModeReady}
      userLabel={shellIdentity.primaryLabel}
      cliBaseUrl={$page.data?.workspace?.cliBaseUrl ?? ""}
      cliInstallCommand={$page.data?.workspace?.cliInstallCommand ?? ""}
      workspaceLabel={$page.data?.workspace?.label ?? ""}
    />
  {/if}

  <LocalSaveWarning />

  <!-- The one tooltip layer every `use:tooltip` reports into. -->
  <TooltipHost />

  {#if $sessionEndedByAccountStatus}
    <SessionEndedOverlay
      open={true}
      cpOrigin={hostedCpOrigin}
      workspaceUrl={sessionEndedWorkspaceUrl}
    />
  {/if}
</div>
