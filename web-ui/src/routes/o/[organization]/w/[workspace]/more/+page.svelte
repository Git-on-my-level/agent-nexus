<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { get } from "svelte/store";

  import {
    actorRegistry,
    clearSelectedActor,
    lookupActorDisplayName,
    principalRegistry,
    selectedActorId,
  } from "$lib/actorSession";
  import { authenticatedAgent, logoutAuthSession } from "$lib/authSession";
  import { hostedSession, loadHostedSession } from "$lib/hosted/session.js";
  import { navIconPath } from "$lib/icons.js";
  import { settingsNavItems } from "$lib/navigation";
  import { bindWorkspaceHref, workspacePath } from "$lib/workspacePaths";
  import { computeWorkspaceShellIdentity } from "$lib/workspaceShellIdentity.js";

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let workspaces = $derived($page.data?.workspaces ?? []);
  let hasMultipleWorkspaces = $derived(workspaces.length > 1);
  let hostedMode = $derived($page.data?.shellCapabilities?.mode === "hosted");
  let hostedAccountPath = $derived(
    String($page.data?.shellCapabilities?.accountPath ?? "").trim() ||
      "/hosted/onboarding",
  );

  let workspaceHref = $derived(
    bindWorkspaceHref(organizationSlug, workspaceSlug),
  );

  let selectedActorName = $derived.by(() => {
    const resolvedName = lookupActorDisplayName(
      $authenticatedAgent?.actor_id || $selectedActorId,
      $actorRegistry,
      $principalRegistry,
    );
    if ($authenticatedAgent?.username) return $authenticatedAgent.username;
    return resolvedName || "Unknown identity";
  });

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

  let shellIdentity = $derived(
    computeWorkspaceShellIdentity({
      hostedMode,
      hostedAccount: hostedSessionSnap.account,
      selectedActorName,
      authenticatedAgent: $authenticatedAgent,
    }),
  );
  let initials = $derived(shellIdentity.initials);

  function workspaceInitials(label) {
    return (label || "?")
      .split(/[\s-]+/)
      .map((w) => w[0])
      .join("")
      .slice(0, 2)
      .toUpperCase();
  }

  async function switchIdentity() {
    if (!workspaceSlug) return;
    if ($authenticatedAgent) {
      await logoutAuthSession({ workspaceSlug, clearActor: true });
      window.location.assign(workspaceHref("/login"));
      return;
    }
    if (browser) clearSelectedActor(localStorage, workspaceSlug);
  }

  async function switchWorkspace(slug) {
    if (!slug || slug === workspaceSlug) return;
    const entry = workspaces.find((w) => w.slug === slug);
    const org = entry?.organizationSlug ?? organizationSlug;
    await goto(workspacePath(org, slug, "/"));
  }
</script>

<div class="space-y-3 sm:space-y-4">
  <!-- Settings navigation -->
  <section>
    <p class="ui-label mb-1.5 sm:mb-2">Settings</p>
    <div class="overflow-hidden rounded-md border border-line bg-panel">
      {#each settingsNavItems as item, i}
        <a
          class="flex items-center gap-2.5 px-3 py-2.5 text-meta font-medium text-fg transition-colors hover:bg-line-subtle sm:gap-3 sm:px-4 sm:py-3 {i >
          0
            ? 'border-t border-line'
            : ''}"
          href={workspaceHref(item.href)}
          data-tour={item.href === "/access" ? "access" : undefined}
        >
          <svg
            class="h-4 w-4 shrink-0 text-fg-muted"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="1.75"
            aria-hidden="true"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d={navIconPath(item.icon)}
            />
          </svg>
          <span class="flex-1">{item.label}</span>
          {#if item.hint}
            <span class="hidden text-micro text-fg-muted sm:inline"
              >{item.hint}</span
            >
          {/if}
          <svg
            class="h-4 w-4 shrink-0 text-fg-muted"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="1.5"
            aria-hidden="true"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M8.25 4.5l7.5 7.5-7.5 7.5"
            />
          </svg>
        </a>
      {/each}
    </div>
  </section>

  {#if hostedMode}
    <section>
      <p class="ui-label mb-1.5 sm:mb-2">Account</p>
      <div class="overflow-hidden rounded-md border border-line bg-panel">
        <a
          class="flex items-center gap-2.5 px-3 py-2.5 text-meta font-medium text-fg transition-colors hover:bg-line-subtle sm:gap-3 sm:px-4 sm:py-3"
          href={hostedAccountPath}
        >
          <svg
            class="h-4 w-4 shrink-0 text-fg-muted"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="1.75"
            aria-hidden="true"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d={navIconPath("account")}
            />
          </svg>
          <span class="flex-1">Account</span>
          <span class="hidden text-micro text-fg-muted sm:inline"
            >Organizations, billing, all workspaces</span
          >
          <svg
            class="h-4 w-4 shrink-0 text-fg-muted"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="1.5"
            aria-hidden="true"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M8.25 4.5l7.5 7.5-7.5 7.5"
            />
          </svg>
        </a>
      </div>
    </section>
  {/if}

  <!-- Workspace switcher (multi-workspace only) -->
  {#if hasMultipleWorkspaces}
    <section>
      <p class="ui-label mb-1.5 sm:mb-2">Workspace</p>
      <div class="overflow-hidden rounded-md border border-line bg-panel">
        {#each workspaces as ws, i}
          {@const isCurrent = ws.slug === workspaceSlug}
          <button
            class="flex w-full items-center gap-2.5 px-3 py-2.5 text-left text-meta transition-colors hover:bg-line-subtle sm:gap-3 sm:px-4 sm:py-3 {i >
            0
              ? 'border-t border-line'
              : ''} {isCurrent ? 'font-medium text-fg' : 'text-fg-muted'}"
            onclick={() => switchWorkspace(ws.slug)}
            type="button"
          >
            <span
              class="inline-grid h-6 w-6 shrink-0 place-items-center rounded bg-accent-solid text-micro font-bold text-white"
              aria-hidden="true"
            >
              {workspaceInitials(ws.label)}
            </span>
            <span class="flex-1 truncate">{ws.label}</span>
            {#if isCurrent}
              <svg
                class="h-3.5 w-3.5 shrink-0 text-accent"
                fill="currentColor"
                viewBox="0 0 20 20"
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
    </section>
  {/if}

  <!-- Identity -->
  <section>
    <p class="ui-label mb-1.5 sm:mb-2">Identity</p>
    <div class="overflow-hidden rounded-md border border-line bg-panel">
      <div
        class="flex items-center gap-2.5 border-b border-line px-3 py-2.5 sm:gap-3 sm:px-4 sm:py-3"
      >
        <span
          class="inline-grid h-7 w-7 shrink-0 place-items-center rounded-full bg-line-strong text-micro font-bold text-fg sm:h-8 sm:w-8"
          aria-hidden="true"
        >
          {initials}
        </span>
        <div class="min-w-0 flex-1">
          <p class="truncate text-meta font-medium text-fg">
            {shellIdentity.primaryLabel}
          </p>
          {#if shellIdentity.secondaryLabel}
            <p
              class="truncate font-mono text-micro text-fg-subtle"
              title={shellIdentity.secondaryLabel}
            >
              {shellIdentity.secondaryLabel}
            </p>
          {/if}
          <p class="hidden text-micro text-fg-muted sm:block">
            {$authenticatedAgent ? "Authenticated principal" : "Dev actor mode"}
          </p>
        </div>
      </div>
      <button
        class="flex w-full items-center gap-2 px-3 py-2.5 text-left text-meta font-medium text-fg-muted transition-colors hover:bg-line-subtle hover:text-fg sm:px-4 sm:py-3"
        onclick={switchIdentity}
        type="button"
      >
        <svg
          class="h-4 w-4 shrink-0"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="1.75"
          aria-hidden="true"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d={navIconPath("signOut")}
          />
        </svg>
        {$authenticatedAgent ? "Sign out" : "Switch identity"}
      </button>
    </div>
  </section>
</div>
