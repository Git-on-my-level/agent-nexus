<!-- State coverage: static developer notes page — no data fetch, no state variants needed. -->
<script>
  const tips = [
    "No workspace cores run until you create a workspace through the control plane (or attach a preconfigured core below).",
    "Prefer the Home → Create account / Dashboard flow for normal local testing.",
    "The local dev script can spawn anx-core for each ready workspace when SYNC_WORKSPACE_CORES=1.",
    "Optional — quick attach: set ANX_SAAS_DEV_PRECONFIGURED_WORKSPACES to a JSON array (same shape as ANX_WORKSPACES) so a core URL exists without the control plane lookup.",
    "Optional — resolve /{slug} from the API: set ANX_CONTROL_PLANE_DEV_ACCESS_TOKEN, or place the token in controlplane/.anx-dev-cp-token, or sign in via /hosted/signup so the UI sets oar_cp_dev_access_token.",
    "One-click local login is enabled by default when using controlplane `make serve`; use /hosted/signin → Local dev shortcut. Set ANX_CONTROL_PLANE_DEV_QUICK_AUTH_ENABLED=0 to force passkey/OAuth.",
    "Control plane data persists across restarts by default. Wipe both stacks: `make reset` in controlplane/. One-run wipe: RESET_CONTROL_PLANE_WORKSPACE=1 with make serve / make serve-docker.",
  ];
</script>

<svelte:head>
  <title>Developer notes — ANX</title>
</svelte:head>

<div class="mx-auto max-w-2xl py-6">
  <p class="text-micro text-fg-subtle">
    <a class="text-accent-text hover:text-accent-text" href="/hosted/start"
      >← Home</a
    >
  </p>
  <h1 class="mt-1 text-display text-fg">Developer notes</h1>
  <p class="mt-1 text-meta text-fg-subtle">
    Advanced options and environment variables for local development. Most
    people don't need any of this — pick the right plan from
    <a
      class="text-accent-text underline underline-offset-2 hover:text-accent-text"
      href="/hosted/dashboard">your dashboard</a
    >
    instead.
  </p>

  <ul
    class="mt-4 divide-y divide-line overflow-hidden rounded-md border border-line bg-bg-soft text-meta text-fg-muted"
  >
    {#each tips as line}
      <li class="px-4 py-2.5">{line}</li>
    {/each}
  </ul>
</div>
