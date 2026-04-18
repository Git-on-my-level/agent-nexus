<script>
  import { onMount } from "svelte";

  import { goto } from "$app/navigation";

  import Avatar from "$lib/hosted/Avatar.svelte";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";
  import { setActiveOrg } from "$lib/hosted/session.js";

  let phase = $state("loading");
  let message = $state("");
  /** @type {any[]} */
  let organizations = $state([]);

  async function readError(res) {
    try {
      const j = await res.json();
      return j?.error?.message || j?.error?.code || res.statusText;
    } catch {
      return res.statusText;
    }
  }

  function planBadgeClasses(planTier) {
    const t = String(planTier ?? "starter").toLowerCase();
    if (t === "enterprise") return "text-fuchsia-400 bg-fuchsia-500/10";
    if (t === "scale") return "text-accent-text bg-accent-soft";
    if (t === "team") return "text-ok-text bg-ok-soft";
    return "text-fg-subtle bg-panel-hover";
  }

  function planLabel(planTier) {
    const t = String(planTier ?? "starter").toLowerCase();
    return t.charAt(0).toUpperCase() + t.slice(1);
  }

  function openOrg(org) {
    setActiveOrg(String(org.id));
    void goto(`/hosted/organizations/${encodeURIComponent(org.id)}`);
  }

  onMount(async () => {
    const res = await hostedCpFetch("organizations?limit=200");
    if (res.status === 401) {
      await goto("/hosted/start");
      return;
    }
    if (!res.ok) {
      message = await readError(res);
      phase = "ready";
      return;
    }
    const body = await res.json();
    organizations = body.organizations ?? [];
    const params = new URLSearchParams(window.location.search);
    if (params.get("billing_error") === "1") {
      message =
        "We could not confirm your checkout session. Open Billing from your organization when ready.";
    }
    phase = "ready";
  });
</script>

<svelte:head>
  <title>Organizations — ANX</title>
</svelte:head>

<div class="space-y-5">
  <div class="flex flex-wrap items-end justify-between gap-3">
    <div>
      <h1 class="text-display text-fg">Organizations</h1>
      <p class="mt-1 hidden text-meta text-fg-subtle sm:block">
        Pick an organization to manage its workspaces, members, and billing.
      </p>
    </div>
    <a
      href="/hosted/organizations/new"
      class="rounded-md bg-accent px-3 py-1.5 text-body font-semibold text-white transition-colors hover:bg-accent-hover"
      >+ New organization</a
    >
  </div>

  {#if message}
    <div
      role="status"
      class="rounded-md bg-warn-soft px-3 py-2 text-micro text-warn-text"
    >
      {message}
    </div>
  {/if}

  {#if phase === "loading"}
    <div
      class="rounded-md border border-line bg-bg-soft px-4 py-6 text-meta text-fg-subtle"
    >
      Loading…
    </div>
  {:else if organizations.length === 0}
    <div
      class="rounded-md border border-line bg-bg-soft px-6 py-8 text-center"
    >
      <h2 class="text-subtitle text-fg">
        No organizations yet
      </h2>
      <p class="mx-auto mt-1.5 max-w-md text-meta text-fg-subtle">
        Create one to start adding workspaces and inviting teammates.
      </p>
      <a
        href="/hosted/organizations/new"
        class="mt-4 inline-flex rounded-md bg-accent px-3 py-1.5 text-body font-semibold text-white transition-colors hover:bg-accent-hover"
      >
        Create organization
      </a>
    </div>
  {:else}
    <div
      class="space-y-px overflow-hidden rounded-md border border-line bg-bg-soft"
    >
      {#each organizations as org, i (org.id)}
        <button
          type="button"
          onclick={() => openOrg(org)}
          class="flex w-full items-center justify-between gap-3 px-4 py-3 text-left transition-colors hover:bg-panel-hover {i >
          0
            ? 'border-t border-line'
            : ''}"
        >
          <div class="flex min-w-0 items-center gap-3">
            <Avatar
              label={org.display_name || org.slug}
              seed={org.id || org.slug}
              size="lg"
            />
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <span class="truncate text-subtitle text-fg"
                  >{org.display_name || org.slug}</span
                >
                <span
                  class="rounded px-1.5 py-0.5 text-micro {planBadgeClasses(
                    org.plan_tier,
                  )}"
                >
                  {planLabel(org.plan_tier)}
                </span>
              </div>
              <div class="mt-0.5 truncate font-mono text-mono text-fg-subtle">
                {org.slug}
              </div>
            </div>
          </div>
          <span
            class="shrink-0 text-micro text-fg-subtle"
            aria-hidden="true">Open →</span
          >
        </button>
      {/each}
    </div>
  {/if}
</div>
