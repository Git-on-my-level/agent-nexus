<script>
  import { onMount } from "svelte";

  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import "$lib/styles/hosted.css";
  import Avatar from "$lib/hosted/Avatar.svelte";
  import {
    hostedSession,
    initialsFor,
    loadHostedSession,
    setActiveOrg,
    signOutHostedSession,
  } from "$lib/hosted/session.js";

  let { children } = $props();

  /** Routes that don't require auth (and shouldn't load the session). */
  const PUBLIC_PREFIXES = [
    "/hosted/start",
    "/hosted/signup",
    "/hosted/signin",
    "/hosted/dev",
    "/hosted/billing/return",
    "/hosted/billing/mock-portal",
  ];

  let menuOpen = $state(false);
  let orgPickerOpen = $state(false);

  const path = $derived($page.url.pathname);
  /** Full product name on the public landing page only; app chrome stays “ANX”. */
  const isLandingMarketing = $derived(path === "/hosted/start");
  const isPublic = $derived(
    PUBLIC_PREFIXES.some((p) => path === p || path.startsWith(p + "/")),
  );
  const session = $derived($hostedSession);
  const account = $derived(session.account);
  const orgs = $derived(session.organizations);
  const activeOrg = $derived(
    orgs.find((o) => String(o.id) === session.activeOrgId) ?? null,
  );

  /** Primary nav for signed-in users — anchored to the active org. */
  const primaryNav = $derived.by(() => {
    const items = [{ href: "/hosted/dashboard", label: "Dashboard" }];
    if (activeOrg) {
      const base = `/hosted/organizations/${encodeURIComponent(activeOrg.id)}`;
      items.push(
        { href: base, label: "Overview" },
        { href: `${base}/usage`, label: "Usage" },
        { href: `${base}/billing`, label: "Billing" },
      );
    } else {
      items.push({ href: "/hosted/organizations", label: "Organizations" });
    }
    return items;
  });

  function isActive(href) {
    if (!href) return false;
    if (href === path) return true;
    if (href === "/hosted/dashboard" && path === "/hosted/dashboard") {
      return true;
    }
    return path.startsWith(href + "/");
  }

  onMount(() => {
    if (!browser) return;
    if (!isPublic) {
      void loadHostedSession();
    }
  });

  // Close popovers when route changes.
  $effect(() => {
    void path;
    menuOpen = false;
    orgPickerOpen = false;
  });

  // If we're on a private route and resolved as unauthed, send the user to /start.
  $effect(() => {
    if (!browser) return;
    if (isPublic) return;
    if (session.phase === "unauthed") {
      void goto(
        `/hosted/start?next=${encodeURIComponent(path + ($page.url.search ?? ""))}`,
        { replaceState: true },
      );
    }
  });

  async function handleSignOut() {
    await signOutHostedSession();
    await goto("/hosted/start");
  }

  function pickOrg(orgId) {
    setActiveOrg(orgId);
    orgPickerOpen = false;
    void goto(`/hosted/organizations/${encodeURIComponent(orgId)}`);
  }
</script>

<div class="min-h-screen bg-[var(--bg)] text-[var(--fg)]">
  <header
    class="sticky top-0 z-30 border-b border-line bg-[var(--bg)]/95 backdrop-blur supports-[backdrop-filter]:bg-[var(--bg)]/80"
  >
    <div
      class="mx-auto flex h-12 max-w-6xl items-center justify-between gap-4 px-4"
    >
      <div class="flex items-center gap-6">
        <a
          href={isPublic && session.phase !== "authed"
            ? "/hosted/start"
            : "/hosted/dashboard"}
          class="flex items-center gap-2 text-[13px] font-semibold text-fg whitespace-nowrap"
        >
          <span
            class="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded bg-accent-soft text-[10px] font-bold uppercase text-accent-text"
          >
            O
          </span>
          {isLandingMarketing ? "Agent Nexus" : "ANX"}
        </a>

        {#if session.phase === "authed"}
          <nav class="hidden items-center gap-1 md:flex" aria-label="Primary">
            {#each primaryNav as item (item.href)}
              <a
                href={item.href}
                data-sveltekit-preload-data="tap"
                class="rounded-md px-2.5 py-1.5 text-[12px] font-medium transition-colors {isActive(
                  item.href,
                )
                  ? 'bg-panel-hover text-fg'
                  : 'text-fg-subtle hover:bg-panel-hover hover:text-fg'}"
              >
                {item.label}
              </a>
            {/each}
          </nav>
        {/if}
      </div>

      <div class="flex items-center gap-2">
        {#if session.phase === "authed"}
          {#if orgs.length > 0}
            <div class="relative">
              <button
                type="button"
                aria-haspopup="listbox"
                aria-expanded={orgPickerOpen}
                onclick={() => (orgPickerOpen = !orgPickerOpen)}
                class="flex max-w-[16rem] items-center gap-2 rounded-md border border-line bg-bg-soft px-2 py-1 text-[12px] font-medium text-fg transition-colors hover:bg-panel-hover"
              >
                {#if activeOrg}
                  <Avatar
                    label={activeOrg.display_name || activeOrg.slug}
                    seed={activeOrg.id || activeOrg.slug}
                    size="sm"
                  />
                {/if}
                <span class="min-w-0 truncate text-left">
                  {activeOrg?.display_name ??
                    activeOrg?.slug ??
                    "Choose organization"}
                </span>
                <svg
                  class="h-3 w-3 shrink-0 text-fg-subtle"
                  viewBox="0 0 12 12"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.6"
                  aria-hidden="true"
                >
                  <path d="M3 4.5l3 3 3-3" />
                </svg>
              </button>
              {#if orgPickerOpen}
                <div
                  role="listbox"
                  class="absolute right-0 top-full z-40 mt-1 w-64 overflow-hidden rounded-md border border-line bg-bg-soft shadow-lg"
                >
                  <div
                    class="border-b border-line px-3 py-2 text-[11px] font-medium uppercase tracking-wide text-fg-subtle"
                  >
                    Switch organization
                  </div>
                  <ul class="max-h-72 overflow-auto py-1">
                    {#each orgs as org (org.id)}
                      <li>
                        <button
                          type="button"
                          role="option"
                          aria-selected={org.id === activeOrg?.id}
                          onclick={() => pickOrg(org.id)}
                          class="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-[12px] text-fg transition-colors hover:bg-panel-hover"
                        >
                          <Avatar
                            label={org.display_name || org.slug}
                            seed={org.id || org.slug}
                            size="sm"
                          />
                          <span class="min-w-0 flex-1">
                            <span
                              class="block truncate text-[12px] text-fg"
                            >
                              {org.display_name || org.slug}
                            </span>
                            {#if org.display_name && org.slug && org.display_name !== org.slug}
                              <span
                                class="block truncate text-[11px] text-fg-subtle"
                              >
                                {org.slug}
                              </span>
                            {/if}
                          </span>
                          {#if org.id === activeOrg?.id}
                            <span
                              class="shrink-0 text-[11px] font-medium text-accent-text"
                              >Active</span
                            >
                          {/if}
                        </button>
                      </li>
                    {/each}
                  </ul>
                  <div class="border-t border-line px-1 py-1">
                    <a
                      href="/hosted/organizations/new"
                      class="block rounded px-2 py-1.5 text-[12px] font-medium text-accent-text transition-colors hover:bg-panel-hover"
                      >+ New organization</a
                    >
                    <a
                      href="/hosted/organizations"
                      class="block rounded px-2 py-1.5 text-[12px] text-fg-subtle transition-colors hover:bg-panel-hover"
                      >Manage organizations</a
                    >
                  </div>
                </div>
              {/if}
            </div>
          {/if}

          <div class="relative">
            <button
              type="button"
              aria-haspopup="menu"
              aria-expanded={menuOpen}
              onclick={() => (menuOpen = !menuOpen)}
              class="inline-flex h-7 w-7 items-center justify-center rounded-full bg-panel-hover text-[11px] font-semibold text-fg transition-colors hover:bg-line-strong"
              title={account?.email ?? account?.display_name ?? "Account"}
            >
              {initialsFor(account)}
            </button>
            {#if menuOpen}
              <div
                role="menu"
                class="absolute right-0 top-full z-40 mt-1 w-60 overflow-hidden rounded-md border border-line bg-bg-soft shadow-lg"
              >
                <div
                  class="flex items-center gap-2 border-b border-line px-3 py-2"
                >
                  <Avatar
                    label={account?.display_name || account?.email || ""}
                    seed={account?.email || account?.display_name || ""}
                    size="md"
                  />
                  <div class="min-w-0">
                    <div class="truncate text-[12px] font-medium text-fg">
                      {account?.display_name || account?.email || "Signed in"}
                    </div>
                    {#if account?.email && account?.display_name}
                      <div class="truncate text-[11px] text-fg-subtle">
                        {account.email}
                      </div>
                    {:else if !account?.email && !account?.display_name}
                      <div class="truncate text-[11px] text-fg-subtle">
                        Account details unavailable
                      </div>
                    {/if}
                  </div>
                </div>
                <ul class="py-1">
                  <li>
                    <a
                      role="menuitem"
                      href="/hosted/dashboard"
                      class="block px-3 py-1.5 text-[12px] text-fg transition-colors hover:bg-panel-hover"
                      >Dashboard</a
                    >
                  </li>
                  <li>
                    <a
                      role="menuitem"
                      href="/hosted/organizations"
                      class="block px-3 py-1.5 text-[12px] text-fg transition-colors hover:bg-panel-hover"
                      >Organizations</a
                    >
                  </li>
                </ul>
                <div class="border-t border-line py-1">
                  <button
                    role="menuitem"
                    type="button"
                    onclick={handleSignOut}
                    class="block w-full px-3 py-1.5 text-left text-[12px] text-fg transition-colors hover:bg-panel-hover"
                  >
                    Sign out
                  </button>
                </div>
              </div>
            {/if}
          </div>
        {:else if isPublic && path !== "/hosted/signin"}
          <a
            href="/hosted/signin"
            class="rounded-md px-2.5 py-1.5 text-[12px] font-medium text-fg-subtle transition-colors hover:bg-panel-hover hover:text-fg"
            >Sign in</a
          >
          {#if path !== "/hosted/signup"}
            <a
              href="/hosted/signup"
              class="rounded-md bg-accent px-2.5 py-1.5 text-[12px] font-medium text-white transition-colors hover:bg-accent-hover"
              >Get started</a
            >
          {/if}
        {/if}
      </div>
    </div>

    {#if session.phase === "authed"}
      <nav
        class="flex items-center gap-1 overflow-x-auto border-t border-line px-4 py-1 md:hidden"
        aria-label="Primary mobile"
      >
        {#each primaryNav as item (item.href)}
          <a
            href={item.href}
            class="shrink-0 rounded-md px-2.5 py-1.5 text-[12px] font-medium transition-colors {isActive(
              item.href,
            )
              ? 'bg-panel-hover text-fg'
              : 'text-fg-subtle hover:bg-panel-hover hover:text-fg'}"
            >{item.label}</a
          >
        {/each}
      </nav>
    {/if}
  </header>

  <main class="mx-auto w-full max-w-6xl px-4 py-6">
    {@render children()}
  </main>

  <footer
    class="mx-auto mt-8 w-full max-w-6xl border-t border-line px-4 pb-6 pt-4 text-[11px] text-fg-subtle"
  >
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-2">
        <span
          class="inline-flex h-4 w-4 items-center justify-center rounded bg-accent-soft text-[9px] font-bold uppercase text-accent-text"
          aria-hidden="true">O</span
        >
        <span>{isLandingMarketing ? "Agent Nexus" : "ANX"} · &copy; {new Date().getFullYear()}</span>
      </div>
      <nav
        class="flex flex-wrap items-center gap-x-4 gap-y-1"
        aria-label="Footer"
      >
        <a
          class="transition-colors hover:text-fg"
          href="https://github.com/run-llama/oar"
          rel="noreferrer"
          target="_blank">Docs</a
        >
        <a
          class="transition-colors hover:text-fg"
          href="https://github.com/run-llama/oar"
          rel="noreferrer"
          target="_blank">GitHub</a
        >
        <a
          class="transition-colors hover:text-fg"
          href="https://status.runoar.com"
          rel="noreferrer"
          target="_blank"
        >
          <span
            class="mr-1 inline-block h-1.5 w-1.5 rounded-full bg-ok-text align-middle"
            aria-hidden="true"
          ></span>
          Status
        </a>
        <a
          class="transition-colors hover:text-fg"
          href="mailto:support@runoar.com">Support</a
        >
        <a class="transition-colors hover:text-fg" href="/hosted/dev"
          >Developer notes</a
        >
      </nav>
    </div>
  </footer>
</div>
