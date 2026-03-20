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
    replaceActorRegistry,
    selectedActorId,
    shouldShowActorGate,
  } from "$lib/actorSession";
  import {
    authenticatedAgent,
    authSessionReady,
    clearAuthSession,
    initializeAuthSession,
  } from "$lib/authSession";
  import { coreClient } from "$lib/coreClient";
  import { getShellContentConfig, navigationItems } from "$lib/navigation";
  import {
    setCurrentWorkspaceSlug,
    setDevActorMode,
    setDevActorModeReady,
    devActorMode,
    devActorModeReady,
  } from "$lib/workspaceContext";
  import {
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
    boards: "M3 6h4v12H3V6zm7 0h4v12h-4V6zm7 0h4v12h-4V6z",
    artifacts:
      "M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z",
    access:
      "M15.75 5.25a3 3 0 013 3m3 0a6 6 0 01-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1121.75 8.25z",
    docs: "M19.5 14.25v-2.625a3.375 3.375 0 00-3.375-3.375h-1.5A1.125 1.125 0 0113.5 7.125v-1.5a3.375 3.375 0 00-3.375-3.375H8.25m2.25 0H5.625c-.621 0-1.125.504-1.125 1.125v17.25c0 .621.504 1.125 1.125 1.125h12.75c.621 0 1.125-.504 1.125-1.125V11.25a9 9 0 00-9-9z",
  };

  let actorError = $state("");
  let loadingActors = $state(false);
  let creatingActor = $state(false);
  let newActorName = $state("");
  let mobileNavOpen = $state(false);
  let hydratedWorkspaceSlug = $state("");
  let workspacePickerOpen = $state(false);

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
  let selectedActorName = $derived(
    lookupActorDisplayName(activeActorId, $actorRegistry) ||
      $authenticatedAgent?.username ||
      "Unknown identity",
  );
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
  let pageTitle = $derived(() => {
    const navItem = navigationItems.find(
      (item) => isActive(item.href) && item.href !== "/",
    );
    const section = navItem?.label;
    const workspaceLabel = activeWorkspace?.label;
    const parts = [section, workspaceLabel, "OAR"].filter(Boolean);
    return parts.join(" · ");
  });

  $effect(() => {
    $page.url.pathname;
    mobileNavOpen = false;
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

  async function hydrateWorkspace(workspaceSlug) {
    setDevActorModeReady(false);
    initializeActorSession(localStorage, workspaceSlug);
    await initializeAuthSession({
      fetchFn: globalThis.fetch.bind(globalThis),
      workspaceSlug,
    });
    try {
      const handshake = await coreClient.getHandshake();
      const devActorModeEnabled = handshake.dev_actor_mode === true;
      setDevActorMode(devActorModeEnabled);
      if (devActorModeEnabled) {
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

  function selectActor(actorId) {
    if ($authenticatedAgent || !activeWorkspaceSlug) {
      return;
    }
    chooseActor(actorId, localStorage, activeWorkspaceSlug);
  }

  function switchIdentity() {
    if (!activeWorkspaceSlug) {
      return;
    }

    if ($authenticatedAgent) {
      clearAuthSession(undefined, activeWorkspaceSlug, { clearActor: true });
      window.location.assign(workspaceHref("/login"));
      return;
    }
    clearSelectedActor(localStorage, activeWorkspaceSlug);
    closeMobileNav();
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
    closeMobileNav();
    await goto(destination);
  }

  function iconPath(iconType) {
    return navIconPathByType[iconType] || navIconPathByType.inbox;
  }

  function closeMobileNav() {
    mobileNavOpen = false;
  }

  function toggleMobileNav() {
    mobileNavOpen = !mobileNavOpen;
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
    if (event.key === "Escape") {
      if (workspacePickerOpen) closeWorkspacePicker();
      if (mobileNavOpen) closeMobileNav();
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
                <span class="workspace-switcher-sub">OAR Control Surface</span>
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

        <div class="shell-actor-panel">
          <p class="shell-actor-label">
            {$authenticatedAgent ? "Authenticated principal" : "Signed in as"}
          </p>
          <div class="shell-actor-row">
            <span class="shell-actor-avatar" aria-hidden="true">{initials}</span
            >
            <div class="shell-actor-copy">
              <p>{selectedActorName}</p>
            </div>
          </div>
          <button onclick={switchIdentity} type="button">
            {$authenticatedAgent ? "Sign out" : "Switch identity"}
          </button>
        </div>
      </aside>

      <div class="shell-main">
        <header class="shell-mobile-header">
          <button
            aria-label="Open navigation menu"
            class="shell-mobile-menu"
            onclick={toggleMobileNav}
            type="button"
          >
            <svg
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M4 7h16M4 12h16M4 17h16"
              />
            </svg>
          </button>
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

        {#if mobileNavOpen}
          <div
            class="shell-mobile-drawer"
            aria-label="Navigation menu"
            aria-modal="true"
            role="dialog"
          >
            <button
              aria-label="Close navigation menu"
              class="shell-mobile-backdrop"
              onclick={closeMobileNav}
              type="button"
            ></button>
            <aside class="shell-mobile-panel">
              <div class="shell-mobile-panel-top">
                <p>Navigate</p>
                <button
                  aria-label="Close navigation menu"
                  onclick={closeMobileNav}
                  type="button"
                >
                  <svg
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
              <nav class="shell-mobile-nav" aria-label="Primary mobile">
                {#if hasMultipleWorkspaces}
                  <div class="mobile-workspace-list">
                    {#each data.workspaces ?? [] as workspace}
                      {@const isCurrent =
                        workspace.slug === activeWorkspaceSlug}
                      <button
                        class="workspace-switcher-option"
                        class:workspace-switcher-option--active={isCurrent}
                        onclick={() => {
                          pickWorkspace(workspace.slug);
                          closeMobileNav();
                        }}
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
                <div class="mobile-workspace-divider"></div>
                {#each navigationItems as item}
                  {@const active = isActive(item.href)}
                  <a
                    class={`shell-nav-link ${active ? "shell-nav-link--active" : ""}`}
                    href={workspaceHref(item.href)}
                    onclick={closeMobileNav}
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
                    <span>{item.label}</span>
                  </a>
                {/each}
              </nav>
              <button
                class="shell-mobile-switch"
                onclick={switchIdentity}
                type="button"
              >
                Switch identity
              </button>
            </aside>
          </div>
        {/if}

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
  {/if}
</div>
