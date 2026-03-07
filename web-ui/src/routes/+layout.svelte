<script>
  import { onMount } from "svelte";
  import { page } from "$app/stores";

  import "../app.css";
  import {
    actorRegistry,
    actorSessionReady,
    buildActorCreatePayload,
    chooseActor,
    initializeActorSession,
    lookupActorDisplayName,
    selectedActorId,
    shouldShowActorGate,
  } from "$lib/actorSession";
  import { coreClient } from "$lib/coreClient";
  import { getShellContentConfig, navigationItems } from "$lib/navigation";

  let { children } = $props();

  const navIconPathByType = {
    home: "M3 11.5L12 4l9 7.5M5.5 10.5V20h13v-9.5M9.25 20v-5.5h5.5V20",
    inbox:
      "M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4",
    threads:
      "M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z",
    artifacts:
      "M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z",
  };

  let actorError = $state("");
  let loadingActors = $state(false);
  let creatingActor = $state(false);
  let newActorName = $state("");
  let mobileNavOpen = $state(false);

  let gateVisible = $derived(
    shouldShowActorGate($actorSessionReady, $selectedActorId),
  );
  let selectedActorName = $derived(
    lookupActorDisplayName($selectedActorId, $actorRegistry) || "Unknown actor",
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
  let shellContentConfig = $derived(getShellContentConfig($page.url.pathname));

  $effect(() => {
    $page.url.pathname;
    mobileNavOpen = false;
  });

  onMount(async () => {
    initializeActorSession();
    await refreshActors();
  });

  async function refreshActors() {
    loadingActors = true;
    actorError = "";

    try {
      const response = await coreClient.listActors();
      actorRegistry.set(response.actors ?? []);
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      actorError = `Failed to load actors: ${reason}`;
      actorRegistry.set([]);
    } finally {
      loadingActors = false;
    }
  }

  function selectActor(actorId) {
    chooseActor(actorId);
  }

  function switchIdentity() {
    chooseActor("");
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
      actorRegistry.set([...$actorRegistry, createdActor]);
      chooseActor(createdActor.id);
      newActorName = "";
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      actorError = `Failed to create actor: ${reason}`;
    } finally {
      creatingActor = false;
    }
  }

  function isActive(href) {
    return (
      $page.url.pathname === href || $page.url.pathname.startsWith(`${href}/`)
    );
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

  function handleWindowKeydown(event) {
    if (event.key === "Escape" && mobileNavOpen) {
      closeMobileNav();
    }
  }
</script>

<svelte:window onkeydown={handleWindowKeydown} />

<div class="shell-root">
  {#if !$actorSessionReady}
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
  {:else if gateVisible}
    <main class="actor-gate-wrap">
      <section class="actor-gate-card">
        <div class="actor-gate-header">
          <p class="actor-gate-eyebrow">Identity Required</p>
          <h1>Select Actor Identity</h1>
          <p>
            Choose an existing actor or register a new one to begin making
            changes.
          </p>
        </div>

        {#if actorError}
          <div class="actor-gate-error" role="alert">{actorError}</div>
        {/if}

        <div class="actor-gate-list" aria-live="polite">
          {#if loadingActors}
            <p class="actor-gate-empty">Loading actors...</p>
          {:else if $actorRegistry.length === 0}
            <p class="actor-gate-empty">No actors found. Create one below.</p>
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
                  <span class="actor-gate-id">{actor.id}</span>
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
      </section>
    </main>
  {:else}
    <div class="shell-frame">
      <aside class="shell-sidebar" aria-label="Primary">
        <div class="shell-brand">
          <div class="shell-brand-mark" aria-hidden="true">
            <svg
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2.5"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M13 10V3L4 14h7v7l9-11h-7z"
              />
            </svg>
          </div>
          <div class="shell-brand-copy">
            <p class="shell-brand-kicker">Control Surface</p>
            <h1>Organization Autorunner UI</h1>
          </div>
        </div>

        <nav class="shell-nav" aria-label="Primary">
          {#each navigationItems as item}
            {@const active = isActive(item.href)}
            <a
              class={`shell-nav-link ${active ? "shell-nav-link--active" : ""}`}
              href={item.href}
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
          <p class="shell-actor-label">Signed in as</p>
          <div class="shell-actor-row">
            <span class="shell-actor-avatar" aria-hidden="true">{initials}</span
            >
            <div class="shell-actor-copy">
              <p>{selectedActorName}</p>
              <span>{($selectedActorId || "").slice(0, 24)}</span>
            </div>
          </div>
          <button onclick={switchIdentity} type="button">Switch identity</button
          >
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
          <p>Organization Autorunner UI</p>
          <button
            class="shell-mobile-identity"
            onclick={switchIdentity}
            type="button"
          >
            <span aria-hidden="true">{initials}</span>
            Switch
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
                {#each navigationItems as item}
                  {@const active = isActive(item.href)}
                  <a
                    class={`shell-nav-link ${active ? "shell-nav-link--active" : ""}`}
                    href={item.href}
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
