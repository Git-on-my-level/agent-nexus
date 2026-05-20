<script>
  import { page } from "$app/state";

  import AdminCommandPalette from "$lib/hosted/AdminCommandPalette.svelte";

  /** @type {{ children?: any }} */
  let { children } = $props();

  const NAV = [
    { label: "Overview", href: "/hosted/admin" },
    { label: "Organizations", href: "/hosted/admin/organizations" },
    { label: "Workspaces", href: "/hosted/admin/workspaces" },
    { label: "Accounts", href: "/hosted/admin/accounts" },
    { label: "Infra", href: "/hosted/admin/infra" },
    { label: "Audit events", href: "/hosted/admin/audit-events" },
  ];

  const activePath = $derived(page.url?.pathname ?? "");

  function isActive(href) {
    if (href === "/hosted/admin") return activePath === "/hosted/admin";
    return activePath === href || activePath.startsWith(href + "/");
  }
</script>

<div class="grid min-h-screen grid-cols-1 md:grid-cols-[12rem_1fr]">
  <aside
    class="hidden border-r border-line bg-bg-soft md:flex md:flex-col md:gap-1 md:px-3 md:py-5"
  >
    <p class="px-2 pb-2 text-micro uppercase tracking-wide text-fg-subtle">
      Hosted ops
    </p>
    <nav class="flex flex-col gap-0.5">
      {#each NAV as item (item.href)}
        <a
          href={item.href}
          class="rounded-md px-2 py-1.5 text-meta {isActive(item.href)
            ? 'bg-bg text-fg shadow-inner'
            : 'text-fg-muted hover:bg-bg hover:text-fg'}"
        >
          {item.label}
        </a>
      {/each}
    </nav>
    <div class="mt-auto px-2 pt-4 text-micro text-fg-subtle">
      <p>
        <kbd class="rounded border border-line bg-bg px-1">⌘</kbd>
        <kbd class="rounded border border-line bg-bg px-1">K</kbd> jump to anything
      </p>
    </div>
  </aside>

  <div class="min-w-0">
    <div class="border-b border-line bg-bg-soft md:hidden">
      <nav class="flex gap-1 overflow-x-auto px-3 py-2 text-meta">
        {#each NAV as item (item.href)}
          <a
            href={item.href}
            class="shrink-0 rounded-md px-2 py-1 {isActive(item.href)
              ? 'bg-bg text-fg'
              : 'text-fg-muted hover:bg-bg hover:text-fg'}"
          >
            {item.label}
          </a>
        {/each}
      </nav>
    </div>
    {@render children?.()}
  </div>
</div>

<AdminCommandPalette />
