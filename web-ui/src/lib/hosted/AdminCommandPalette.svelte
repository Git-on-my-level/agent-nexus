<script>
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";

  import {
    adminHeaders,
    readAdminActor,
    readAdminToken,
  } from "$lib/hosted/adminAuth.js";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  let open = $state(false);
  let query = $state("");
  let results = $state([]);
  let loading = $state(false);
  let activeIndex = $state(0);
  let inputEl = $state(null);
  let debounceHandle = $state(null);

  const STATIC_LINKS = [
    {
      kind: "page",
      label: "Overview",
      href: "/hosted/admin",
      hint: "Dashboard",
    },
    {
      kind: "page",
      label: "Organizations",
      href: "/hosted/admin/organizations",
      hint: "List",
    },
    {
      kind: "page",
      label: "Workspaces",
      href: "/hosted/admin/workspaces",
      hint: "List",
    },
    {
      kind: "page",
      label: "Accounts",
      href: "/hosted/admin/accounts",
      hint: "List",
    },
    {
      kind: "page",
      label: "Infra",
      href: "/hosted/admin/infra",
      hint: "Hosts",
    },
    {
      kind: "page",
      label: "Audit events",
      href: "/hosted/admin/audit-events",
      hint: "Drilldown",
    },
  ];

  function handleKey(event) {
    const mod = event.metaKey || event.ctrlKey;
    if (mod && (event.key === "k" || event.key === "K")) {
      event.preventDefault();
      open = true;
      requestAnimationFrame(() => inputEl?.focus());
      return;
    }
    if (!open) return;
    if (event.key === "Escape") {
      close();
      return;
    }
    if (event.key === "ArrowDown") {
      event.preventDefault();
      activeIndex = Math.min(activeIndex + 1, currentResults().length - 1);
      return;
    }
    if (event.key === "ArrowUp") {
      event.preventDefault();
      activeIndex = Math.max(activeIndex - 1, 0);
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      const list = currentResults();
      const target = list[activeIndex];
      if (target) navigate(target);
    }
  }

  function close() {
    open = false;
    query = "";
    results = [];
    activeIndex = 0;
  }

  function currentResults() {
    return query.trim() ? results : STATIC_LINKS;
  }

  function navigate(target) {
    const href = target?.href;
    if (!href) return;
    close();
    goto(href);
  }

  function onInput(value) {
    query = value;
    activeIndex = 0;
    if (debounceHandle) clearTimeout(debounceHandle);
    const trimmed = value.trim();
    if (!trimmed) {
      results = [];
      loading = false;
      return;
    }
    debounceHandle = setTimeout(() => search(trimmed), 180);
  }

  async function search(q) {
    const token = readAdminToken();
    if (!token) {
      results = [
        {
          kind: "hint",
          label: "Sign in on any admin page first",
          hint: "Token not set",
        },
      ];
      return;
    }
    loading = true;
    const headers = adminHeaders(token, readAdminActor());
    try {
      const params = `q=${encodeURIComponent(q)}&limit=8`;
      const [orgRes, wsRes, accRes] = await Promise.all([
        hostedCpFetch(`admin/analytics/organizations?${params}`, { headers }),
        hostedCpFetch(`admin/analytics/workspaces?${params}`, { headers }),
        hostedCpFetch(`admin/analytics/accounts?${params}`, { headers }),
      ]);
      const items = [];
      if (orgRes.ok) {
        const body = await orgRes.json();
        for (const org of body.organizations ?? []) {
          items.push({
            kind: "org",
            label: org.display_name || org.slug || org.id,
            hint: `Org · ${org.slug}`,
            href: `/hosted/admin/organizations/${encodeURIComponent(org.id)}`,
          });
        }
      }
      if (wsRes.ok) {
        const body = await wsRes.json();
        for (const ws of body.workspaces ?? []) {
          items.push({
            kind: "workspace",
            label: `${ws.organization_slug}/${ws.slug}`,
            hint: `Workspace · ${ws.host_label || ws.host_id || "no host"}`,
            href: `/hosted/admin/workspaces/${encodeURIComponent(ws.id)}`,
          });
        }
      }
      if (accRes.ok) {
        const body = await accRes.json();
        for (const account of body.accounts ?? []) {
          items.push({
            kind: "account",
            label: account.email || account.display_name || account.id,
            hint: `Account · ${account.display_name || account.id}`,
            href: `/hosted/admin/accounts/${encodeURIComponent(account.id)}`,
          });
        }
      }
      results = items.slice(0, 20);
    } catch {
      results = [
        {
          kind: "hint",
          label: "Search failed",
          hint: "Check token or try again",
        },
      ];
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    window.addEventListener("keydown", handleKey);
    return () => window.removeEventListener("keydown", handleKey);
  });
</script>

{#if open}
  <div
    class="fixed inset-0 z-50 flex items-start justify-center bg-bg/70 px-4 pt-24 backdrop-blur-sm"
    role="presentation"
    onclick={(event) => {
      if (event.target === event.currentTarget) close();
    }}
    onkeydown={(event) => {
      if (event.key === "Escape" && event.target === event.currentTarget)
        close();
    }}
  >
    <div
      class="w-full max-w-xl overflow-hidden rounded-lg border border-line bg-bg shadow-2xl"
    >
      <div class="border-b border-line px-3 py-2">
        <input
          bind:this={inputEl}
          value={query}
          oninput={(event) => onInput(event.currentTarget.value)}
          placeholder="Jump to org, workspace, account…"
          class="w-full bg-transparent text-meta text-fg outline-none placeholder:text-fg-subtle"
        />
      </div>
      <ul class="max-h-[60vh] overflow-y-auto py-1">
        {#if loading}
          <li class="px-3 py-2 text-micro text-fg-subtle">Searching…</li>
        {/if}
        {#each currentResults() as item, i (item.href ?? item.label + i)}
          <li>
            <button
              type="button"
              class="flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-meta {i ===
              activeIndex
                ? 'bg-bg-soft'
                : ''} hover:bg-bg-soft"
              onmouseenter={() => (activeIndex = i)}
              onclick={() => navigate(item)}
              disabled={item.kind === "hint"}
            >
              <span class="min-w-0 truncate text-fg">{item.label}</span>
              <span class="shrink-0 text-micro text-fg-subtle">{item.hint}</span
              >
            </button>
          </li>
        {/each}
        {#if !loading && query.trim() && currentResults().length === 0}
          <li class="px-3 py-2 text-micro text-fg-subtle">No matches.</li>
        {/if}
      </ul>
      <div
        class="border-t border-line bg-bg-soft px-3 py-1.5 text-micro text-fg-subtle"
      >
        <kbd class="rounded border border-line bg-bg px-1">↑↓</kbd> navigate ·
        <kbd class="rounded border border-line bg-bg px-1">↵</kbd> open ·
        <kbd class="rounded border border-line bg-bg px-1">esc</kbd> close
      </div>
    </div>
  </div>
{/if}
