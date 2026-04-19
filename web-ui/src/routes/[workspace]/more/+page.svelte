<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import {
    actorRegistry,
    clearSelectedActor,
    lookupActorDisplayName,
    principalRegistry,
    selectedActorId,
  } from "$lib/actorSession";
  import { authenticatedAgent, logoutAuthSession } from "$lib/authSession";
  import { settingsNavItems } from "$lib/navigation";
  import { workspacePath } from "$lib/workspacePaths";

  const navIconPathByType = {
    artifacts:
      "M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z",
    trash:
      "M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0",
    access:
      "M15.75 5.25a3 3 0 013 3m3 0a6 6 0 01-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1121.75 8.25z",
    secrets:
      "M15 7a2 2 0 0 1 2 2m4 0a6 6 0 0 1-7.743 5.743L11 17H9v2H7v2H4a1 1 0 0 1-1-1v-2.586a1 1 0 0 1 .293-.707l5.964-5.964A6 6 0 1 1 21 9z",
  };

  let workspaceSlug = $derived($page.params.workspace);
  let workspaces = $derived($page.data?.workspaces ?? []);
  let hasMultipleWorkspaces = $derived(workspaces.length > 1);
  let hostedMode = $derived(Boolean($page.data?.hostedMode));
  let hostedAccountPath = $derived(
    String($page.data?.hostedAccountPath ?? "").trim() || "/hosted/onboarding",
  );

  function workspaceHref(path = "/") {
    return workspacePath(workspaceSlug, path);
  }

  let selectedActorName = $derived.by(() => {
    const resolvedName = lookupActorDisplayName(
      $authenticatedAgent?.actor_id || $selectedActorId,
      $actorRegistry,
      $principalRegistry,
    );
    if ($authenticatedAgent?.username) return $authenticatedAgent.username;
    return resolvedName || "Unknown identity";
  });

  let initials = $derived(
    selectedActorName
      ? selectedActorName
          .split(/\s+/)
          .map((w) => w[0])
          .join("")
          .slice(0, 2)
          .toUpperCase()
      : "?",
  );

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
    await goto(workspacePath(slug, "/"));
  }
</script>

<div class="space-y-4">
  <!-- Settings navigation -->
  <section>
    <p
      class="mb-2 text-micro font-medium uppercase tracking-wide text-[var(--fg-muted)]"
    >
      Settings
    </p>
    <div
      class="overflow-hidden rounded-md border border-[var(--line)] bg-[var(--panel)]"
    >
      {#each settingsNavItems as item, i}
        <a
          class="flex items-center gap-3 px-4 py-3 text-meta font-medium text-[var(--fg)] transition-colors hover:bg-[var(--line-subtle)] {i >
          0
            ? 'border-t border-[var(--line)]'
            : ''}"
          href={workspaceHref(item.href)}
        >
          <svg
            class="h-4 w-4 shrink-0 text-[var(--fg-muted)]"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="1.75"
            aria-hidden="true"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d={navIconPathByType[item.icon] ?? ""}
            />
          </svg>
          <span class="flex-1">{item.label}</span>
          {#if item.hint}
            <span class="text-micro text-[var(--fg-muted)]">{item.hint}</span>
          {/if}
          <svg
            class="h-4 w-4 shrink-0 text-[var(--fg-muted)]"
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
      <p
        class="mb-2 text-micro font-medium uppercase tracking-wide text-[var(--fg-muted)]"
      >
        Account
      </p>
      <div
        class="overflow-hidden rounded-md border border-[var(--line)] bg-[var(--panel)]"
      >
        <a
          class="flex items-center gap-3 px-4 py-3 text-meta font-medium text-[var(--fg)] transition-colors hover:bg-[var(--line-subtle)]"
          href={hostedAccountPath}
        >
          <svg
            class="h-4 w-4 shrink-0 text-[var(--fg-muted)]"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="1.75"
            aria-hidden="true"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M13.5 6H5.25A2.25 2.25 0 003 8.25v10.5A2.25 2.25 0 005.25 21h10.5A2.25 2.25 0 0018 18.75V10.5M19.5 3v6m0 0h-6m6 0l-9 9"
            />
          </svg>
          <span class="flex-1">Account</span>
          <span class="text-micro text-[var(--fg-muted)]"
            >Organizations, billing, all workspaces</span
          >
          <svg
            class="h-4 w-4 shrink-0 text-[var(--fg-muted)]"
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
      <p
        class="mb-2 text-micro font-medium uppercase tracking-wide text-[var(--fg-muted)]"
      >
        Workspace
      </p>
      <div
        class="overflow-hidden rounded-md border border-[var(--line)] bg-[var(--panel)]"
      >
        {#each workspaces as ws, i}
          {@const isCurrent = ws.slug === workspaceSlug}
          <button
            class="flex w-full items-center gap-3 px-4 py-3 text-left text-meta transition-colors hover:bg-[var(--line-subtle)] {i >
            0
              ? 'border-t border-[var(--line)]'
              : ''} {isCurrent
              ? 'font-medium text-[var(--fg)]'
              : 'text-[var(--fg-muted)]'}"
            onclick={() => switchWorkspace(ws.slug)}
            type="button"
          >
            <span
              class="inline-grid h-6 w-6 shrink-0 place-items-center rounded bg-[var(--accent-hover)] text-[0.5625rem] font-bold text-white"
              aria-hidden="true"
            >
              {workspaceInitials(ws.label)}
            </span>
            <span class="flex-1 truncate">{ws.label}</span>
            {#if isCurrent}
              <svg
                class="h-3.5 w-3.5 shrink-0 text-[var(--accent)]"
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
    <p
      class="mb-2 text-micro font-medium uppercase tracking-wide text-[var(--fg-muted)]"
    >
      Identity
    </p>
    <div
      class="overflow-hidden rounded-md border border-[var(--line)] bg-[var(--panel)]"
    >
      <div
        class="flex items-center gap-3 border-b border-[var(--line)] px-4 py-3"
      >
        <span
          class="inline-grid h-8 w-8 shrink-0 place-items-center rounded-full bg-[#4a5060] text-[0.625rem] font-bold text-white"
          aria-hidden="true"
        >
          {initials}
        </span>
        <div class="min-w-0 flex-1">
          <p class="truncate text-meta font-medium text-[var(--fg)]">
            {selectedActorName}
          </p>
          <p class="text-micro text-[var(--fg-muted)]">
            {$authenticatedAgent ? "Authenticated principal" : "Dev actor mode"}
          </p>
        </div>
      </div>
      <button
        class="flex w-full items-center gap-2 px-4 py-3 text-left text-meta font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] hover:text-[var(--fg)]"
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
            d="M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15m3 0l3-3m0 0l-3-3m3 3H9"
          />
        </svg>
        {$authenticatedAgent ? "Sign out" : "Switch identity"}
      </button>
    </div>
  </section>
</div>
