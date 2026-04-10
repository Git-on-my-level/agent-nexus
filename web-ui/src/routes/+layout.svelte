<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import "../app.css";
  import {
    actorRegistry,
    actorSessionReady,
    buildActorCreatePayload,
    chooseActor,
    clearSelectedActor,
    initializeActorSession,
    lookupActorDisplayName,
    principalRegistry,
    replaceActorRegistry,
    replacePrincipalRegistry,
    selectedActorId,
    shouldShowActorGate,
  } from "$lib/actorSession";
  import {
    authenticatedAgent,
    authSessionReady,
    initializeAuthSession,
    logoutAuthSession,
  } from "$lib/authSession";
  import { listAllPrincipals } from "$lib/authPrincipals";
  import CommandPalette from "$lib/components/CommandPalette.svelte";
  import { coreClient } from "$lib/coreClient";
  import {
    getShellContentConfig,
    isMoreHubActivePath,
    navigationItems,
    settingsNavItems,
  } from "$lib/navigation";
  import {
    setCurrentWorkspaceSlug,
    setDevActorMode,
    setDevActorModeReady,
    devActorMode,
    devActorModeReady,
  } from "$lib/workspaceContext";
  import { handleModEnterFormSubmit } from "$lib/formSubmitShortcut.js";
  import {
    appPath,
    workspacePath,
    stripBasePath,
    stripWorkspacePath,
  } from "$lib/workspacePaths";

  let { children, data } = $props();

  const navIconPathByType = {
    home: "M3 11.5L12 4l9 7.5M5.5 10.5V20h13v-9.5M9.25 20v-5.5h5.5V20",
    inbox:
      "M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4",
    threads:
      "M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z",
    topics: "M4.5 6.75h15M4.5 12h15M4.5 17.25h9.5",
    boards: "M3 6h4v12H3V6zm7 0h4v12h-4V6zm7 0h4v12h-4V6z",
    artifacts:
      "M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z",
    trash:
      "M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0",
    access:
      "M15.75 5.25a3 3 0 013 3m3 0a6 6 0 01-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1121.75 8.25z",
    docs: "M19.5 14.25v-2.625a3.375 3.375 0 00-3.375-3.375h-1.5A1.125 1.125 0 0113.5 7.125v-1.5a3.375 3.375 0 00-3.375-3.375H8.25m2.25 0H5.625c-.621 0-1.125.504-1.125 1.125v17.25c0 .621.504 1.125 1.125 1.125h12.75c.621 0 1.125-.504 1.125-1.125V11.25a9 9 0 00-9-9z",
  };

  let actorError = $state("");
  let loadingActors = $state(false);
  let creatingActor = $state(false);
  let newActorName = $state("");
  let hydratedWorkspaceSlug = $state("");
  let workspacePickerOpen = $state(false);
  let commandPaletteOpen = $state(false);
  let devFixturePersonas = $state([]);
  let devPersonaBusy = $state(false);

  let activeWorkspace = $derived($page.data.workspace ?? null);
  let activeWorkspaceSlug = $derived(activeWorkspace?.slug ?? "");
  let hasMultipleWorkspaces = $derived((data.workspaces ?? []).length > 1);
  let currentAppPath = $derived(
    activeWorkspaceSlug
      ? stripWorkspacePath($page.url.pathname, activeWorkspaceSlug)
      : stripBasePath($page.url.pathname),
  );
  let identityReady = $derived($actorSessionReady && $authSessionReady);
  let principalActorId = $derived($authenticatedAgent?.actor_id ?? "");
  let activeActorId = $derived(principalActorId || $selectedActorId);
  let onLoginRoute = $derived(currentAppPath === "/login");
  let gateVisible = $derived(
    activeWorkspaceSlug &&
      identityReady &&
      !$authenticatedAgent &&
      !onLoginRoute &&
      $devActorMode &&
      shouldShowActorGate($actorSessionReady, $selectedActorId),
  );
  let renderLoginOnly = $derived(
    activeWorkspaceSlug &&
      identityReady &&
      !$authenticatedAgent &&
      onLoginRoute,
  );
  let shouldRedirectToLogin = $derived(
    activeWorkspaceSlug &&
      identityReady &&
      $devActorModeReady &&
      !$authenticatedAgent &&
      !onLoginRoute &&
      !$devActorMode,
  );
  let awaitingIdentityMode = $derived(
    activeWorkspaceSlug &&
      identityReady &&
      !$authenticatedAgent &&
      !$devActorModeReady,
  );
  let selectedActorName = $derived.by(() => {
    const resolvedName = lookupActorDisplayName(
      activeActorId,
      $actorRegistry,
      $principalRegistry,
    );
    if (
      $authenticatedAgent?.username &&
      ($authenticatedAgent?.actor_id === activeActorId ||
        resolvedName === activeActorId ||
        resolvedName === "Unknown actor")
    ) {
      return $authenticatedAgent.username;
    }
    return resolvedName || "Unknown identity";
  });
  let initials = $derived(
    selectedActorName
      ? selectedActorName
          .split(/\s+/)
          .map((word) => word[0])
          .join("")
          .slice(0, 2)
          .toUpperCase()
      : "?",
  );
  let shellContentConfig = $derived(getShellContentConfig(currentAppPath));
  let moreBottomNavActive = $derived(isMoreHubActivePath(currentAppPath));
  const shellNavForTitle = [...navigationItems, ...settingsNavItems];

  let pageTitle = $derived(() => {
    const navItem = shellNavForTitle.find(
      (item) => isActive(item.href) && item.href !== "/",
    );
    const section = navItem?.label;
    const workspaceLabel = activeWorkspace?.label;
    const parts = [section, workspaceLabel, "OAR"].filter(Boolean);
    return parts.join(" · ");
  });

  $effect(() => {
    if (!browser || !shouldRedirectToLogin) {
      return;
    }
    goto(workspacePath(activeWorkspaceSlug, "/login"));
  });

  $effect(() => {
    if (!browser) {
      return;
    }

    const workspaceSlug = activeWorkspaceSlug;
    if (!workspaceSlug) {
      return;
    }

    setCurrentWorkspaceSlug(workspaceSlug);
    if (hydratedWorkspaceSlug === workspaceSlug) {
      return;
    }

    hydratedWorkspaceSlug = workspaceSlug;
    void hydrateWorkspace(workspaceSlug);
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

  $effect(() => {
    if (
      !browser ||
      !$devActorMode ||
      !$devActorModeReady ||
      !activeWorkspaceSlug
    ) {
      return;
    }
    void loadDevFixturePersonas();
  });

  async function loadDevFixturePersonas() {
    try {
      const response = await fetch(appPath("/auth/dev/identities"));
      if (!response.ok) {
        devFixturePersonas = [];
        return;
      }
      const payload = await response.json();
      devFixturePersonas = Array.isArray(payload.personas)
        ? payload.personas
        : [];
    } catch {
      devFixturePersonas = [];
    }
  }

  async function switchDevFixturePersona(personaId) {
    const trimmed = String(personaId ?? "").trim();
    if (!activeWorkspaceSlug || devPersonaBusy || !trimmed) {
      return;
    }
    devPersonaBusy = true;
    try {
      const response = await fetch(appPath("/auth/dev/session"), {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ persona_id: trimmed }),
      });
      if (!response.ok) {
        return;
      }
      await hydrateWorkspace(activeWorkspaceSlug);
    } finally {
      devPersonaBusy = false;
    }
  }

  async function hydrateWorkspace(workspaceSlug) {
    setDevActorModeReady(false);
    initializeActorSession(localStorage, workspaceSlug);
    const agent = await initializeAuthSession({
      fetchFn: globalThis.fetch.bind(globalThis),
      workspaceSlug,
    });
    replacePrincipalRegistry(agent ? [agent] : [], workspaceSlug);
    try {
      const handshake = await coreClient.getHandshake();
      const devActorModeEnabled = handshake.dev_actor_mode === true;
      setDevActorMode(devActorModeEnabled);
      if (devActorModeEnabled || agent?.actor_id) {
        await refreshActors(workspaceSlug);
      } else {
        actorError = "";
        loadingActors = false;
        replaceActorRegistry([], workspaceSlug);
      }
    } catch {
      setDevActorMode(false);
      actorError = "";
      loadingActors = false;
      replaceActorRegistry([], workspaceSlug);
    } finally {
      setDevActorModeReady(true);
    }
  }

  async function refreshActors(workspaceSlug = activeWorkspaceSlug) {
    loadingActors = true;
    actorError = "";

    try {
      const response = await coreClient.listActors();
      replaceActorRegistry(response.actors ?? [], workspaceSlug);
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      actorError = `Failed to load actors: ${reason}`;
      replaceActorRegistry([], workspaceSlug);
    } finally {
      loadingActors = false;
    }
  }

  function mergePrincipals(...principalLists) {
    const seen = new Set();
    const merged = [];

    for (const principals of principalLists) {
      for (const principal of principals ?? []) {
        const agentId = String(principal?.agent_id ?? "").trim();
        const actorId = String(principal?.actor_id ?? "").trim();
        const username = String(principal?.username ?? "").trim();
        const key = `${agentId}\n${actorId}\n${username}`;
        if (!key.trim() || seen.has(key)) {
          continue;
        }
        seen.add(key);
        merged.push(principal);
      }
    }

    return merged;
  }

  async function refreshPrincipals(
    workspaceSlug = activeWorkspaceSlug,
    seedPrincipals = [],
  ) {
    const seeded = mergePrincipals(seedPrincipals);
    replacePrincipalRegistry(seeded, workspaceSlug);

    if (seeded.length === 0) {
      return;
    }

    try {
      const principals = await listAllPrincipals(coreClient, { limit: 200 });

      replacePrincipalRegistry(
        mergePrincipals(principals, seeded),
        workspaceSlug,
      );
    } catch {
      replacePrincipalRegistry(seeded, workspaceSlug);
    }
  }

  function selectActor(actorId) {
    if ($authenticatedAgent || !activeWorkspaceSlug) {
      return;
    }
    chooseActor(actorId, localStorage, activeWorkspaceSlug);
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
    return currentAppPath === href || currentAppPath.startsWith(`${href}/`);
  }

  function workspaceHref(pathname = "/") {
    return workspacePath(activeWorkspaceSlug, pathname);
  }

  async function switchWorkspace(nextWorkspaceSlug) {
    if (!nextWorkspaceSlug || nextWorkspaceSlug === activeWorkspaceSlug) {
      return;
    }

    const destination = `${workspacePath(nextWorkspaceSlug, currentAppPath)}${$page.url.search}${$page.url.hash}`;
    await goto(destination);
  }

  function iconPath(iconType) {
    return navIconPathByType[iconType] || navIconPathByType.inbox;
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

  function handleWindowKeydown(event) {
    if (event.key === "k" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      if (activeWorkspaceSlug) {
        commandPaletteOpen = !commandPaletteOpen;
      }
      return;
    }
    handleModEnterFormSubmit(event, { commandPaletteOpen });
    if (event.key === "Escape") {
      if (workspacePickerOpen) closeWorkspacePicker();
    }
  }

  function handleWindowClick(event) {
    if (workspacePickerOpen) {
      const picker = document.getElementById("workspace-picker-container");
      if (picker && !picker.contains(event.target)) {
        closeWorkspacePicker();
      }
    }
  }
</script>

<svelte:head>
  <title>{pageTitle()}</title>
</svelte:head>

<svelte:window onkeydown={handleWindowKeydown} onclick={handleWindowClick} />

<div class="shell-root">
  {#if !activeWorkspaceSlug}
    {@render children()}
  {:else if !identityReady || awaitingIdentityMode}
    <main class="shell-loading" aria-live="polite">
      <div class="shell-loading-card">
        <svg
          class="shell-spinner"
          fill="none"
          viewBox="0 0 24 24"
          aria-hidden="true"
        >
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
        <p>Loading Organization Autorunner UI...</p>
      </div>
    </main>
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
            <p class="actor-gate-empty">Loading identities...</p>
          {:else if $actorRegistry.length === 0}
            <p class="actor-gate-empty">
              No identities yet. Create one to get started.
            </p>
          {:else}
            {#each $actorRegistry as actor}
              <button
                class="actor-gate-item"
                onclick={() => selectActor(actor.id)}
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
              {creatingActor ? "Creating..." : "Create and continue"}
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
      <aside class="shell-sidebar" aria-label="Primary">
        <div class="shell-sidebar-top">
          <button
            class="shell-search-trigger"
            onclick={() => (commandPaletteOpen = true)}
            type="button"
          >
            <svg
              class="shell-search-trigger-icon"
              viewBox="0 0 20 20"
              fill="currentColor"
              aria-hidden="true"
            >
              <path
                fill-rule="evenodd"
                d="M9 3.5a5.5 5.5 0 100 11 5.5 5.5 0 000-11zM2 9a7 7 0 1112.452 4.391l3.328 3.329a.75.75 0 11-1.06 1.06l-3.329-3.328A7 7 0 012 9z"
                clip-rule="evenodd"
              />
            </svg>
            <span>Search</span>
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
                    >{activeWorkspace?.label || activeWorkspaceSlug}</span
                  >
                  <span class="workspace-switcher-sub">OAR Control Surface</span
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
                    {@const isCurrent = workspace.slug === activeWorkspaceSlug}
                    <button
                      class="workspace-switcher-option"
                      class:workspace-switcher-option--active={isCurrent}
                      role="option"
                      aria-selected={isCurrent}
                      onclick={() => pickWorkspace(workspace.slug)}
                      type="button"
                    >
                      <span
                        class="workspace-switcher-option-icon"
                        aria-hidden="true"
                      >
                        {workspaceInitials(workspace.label)}
                      </span>
                      <span class="workspace-switcher-option-label">
                        <span>{workspace.label}</span>
                        {#if workspace.description}
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
            {#each navigationItems as item}
              {@const active = isActive(item.href)}
              <a
                class={`shell-nav-link ${active ? "shell-nav-link--active" : ""}`}
                href={workspaceHref(item.href)}
                aria-label={item.label}
              >
                <svg
                  class="shell-nav-icon"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                  stroke-width="1.75"
                  aria-hidden="true"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d={iconPath(item.icon)}
                  />
                </svg>
                <span class="shell-nav-copy">
                  <span>{item.label}</span>
                  {#if item.hint}
                    <span class="shell-nav-hint">{item.hint}</span>
                  {/if}
                </span>
              </a>
            {/each}
          </nav>
        </div>

        <div class="shell-sidebar-footer">
          <div
            class="shell-actor-panel"
            aria-label="Identity and workspace links"
          >
            {#if $devActorMode && devFixturePersonas.length > 0}
              <div class="shell-dev-personas">
                <p class="shell-actor-label">Fixture persona</p>
                <select
                  class="shell-dev-persona-select"
                  aria-label="Switch fixture persona"
                  disabled={devPersonaBusy}
                  onchange={(event) => {
                    const value = String(
                      event.currentTarget.value ?? "",
                    ).trim();
                    if (value) {
                      void switchDevFixturePersona(value);
                    }
                    event.currentTarget.value = "";
                  }}
                >
                  <option value="">Switch session…</option>
                  {#each devFixturePersonas as persona}
                    <option value={persona.persona_id}
                      >{persona.display_label}</option
                    >
                  {/each}
                </select>
              </div>
            {/if}
            <nav class="shell-secondary-nav" aria-label="Workspace">
              <div class="shell-settings-links">
                {#each settingsNavItems as item}
                  {@const active = isActive(item.href)}
                  <a
                    class={`shell-settings-link ${active ? "shell-settings-link--active" : ""}`}
                    href={workspaceHref(item.href)}
                    aria-label={item.label}
                  >
                    <svg
                      class="shell-settings-icon"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                      stroke-width="1.75"
                      aria-hidden="true"
                    >
                      <path
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        d={iconPath(item.icon)}
                      />
                    </svg>
                    <span class="shell-settings-link-text">
                      <span>{item.label}</span>
                      {#if item.hint}
                        <span class="shell-settings-link-hint">{item.hint}</span
                        >
                      {/if}
                    </span>
                  </a>
                {/each}
              </div>
            </nav>
            <div class="shell-actor-identity">
              <p class="shell-actor-label">
                {$authenticatedAgent
                  ? "Authenticated principal"
                  : "Signed in as"}
              </p>
              <div class="shell-actor-row">
                <span class="shell-actor-avatar" aria-hidden="true"
                  >{initials}</span
                >
                <div class="shell-actor-copy">
                  <p>{selectedActorName}</p>
                </div>
              </div>
            </div>
            <button onclick={switchIdentity} type="button">
              {$authenticatedAgent ? "Sign out" : "Switch identity"}
            </button>
          </div>
        </div>
      </aside>

      <div class="shell-main">
        <header class="shell-mobile-header">
          <p>OAR</p>
          <button
            class="shell-mobile-identity"
            onclick={switchIdentity}
            type="button"
          >
            <span aria-hidden="true">{initials}</span>
            {$authenticatedAgent ? "Sign out" : "Switch"}
          </button>
        </header>

        <main class="shell-main-scroll">
          <div
            class={`shell-content shell-content--${shellContentConfig.mode}`}
            style={`--shell-content-max: ${shellContentConfig.maxWidth}`}
          >
            {@render children?.()}
          </div>
        </main>
      </div>
    </div>

    <nav class="shell-bottom-nav" aria-label="Primary navigation">
      {#each navigationItems as item}
        {@const active = isActive(item.href)}
        <a
          class="shell-bottom-nav-item {active
            ? 'shell-bottom-nav-item--active'
            : ''}"
          href={workspaceHref(item.href)}
          aria-current={active ? "page" : undefined}
        >
          <svg
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="1.75"
            aria-hidden="true"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d={iconPath(item.icon)}
            />
          </svg>
          <span>{item.label}</span>
        </a>
      {/each}
      <a
        class="shell-bottom-nav-item {moreBottomNavActive
          ? 'shell-bottom-nav-item--active'
          : ''}"
        href={workspaceHref("/more")}
        aria-current={moreBottomNavActive ? "page" : undefined}
      >
        <svg fill="currentColor" viewBox="0 0 24 24" aria-hidden="true">
          <circle cx="5" cy="12" r="1.5" />
          <circle cx="12" cy="12" r="1.5" />
          <circle cx="19" cy="12" r="1.5" />
        </svg>
        <span>More</span>
      </a>
    </nav>
  {/if}

  {#if activeWorkspaceSlug}
    <CommandPalette
      bind:open={commandPaletteOpen}
      workspaceSlug={activeWorkspaceSlug}
    />
  {/if}
</div>
