<script>
  const tips = [
    "No workspace cores run until you create a workspace through the control plane (or attach a preconfigured core below).",
    "Prefer the Home → Create account / Dashboard flow for normal local testing.",
    "The local dev script can spawn oar-core for each ready workspace when SYNC_WORKSPACE_CORES=1.",
    "Optional — quick attach: set OAR_SAAS_DEV_PRECONFIGURED_WORKSPACES to a JSON array (same shape as OAR_WORKSPACES) so a core URL exists without the control plane lookup.",
    "Optional — resolve /{slug} from the API: set OAR_CONTROL_PLANE_DEV_ACCESS_TOKEN, or place the token in controlplane/.oar-dev-cp-token, or sign in via /hosted/signup so the UI sets oar_cp_dev_access_token.",
    "One-click local login is enabled by default when using controlplane `make serve`; use /hosted/signin → Local dev shortcut. Set OAR_CONTROL_PLANE_DEV_QUICK_AUTH_ENABLED=0 to force passkey/OAuth.",
    "To keep the control plane database between restarts: RESET_CONTROL_PLANE_WORKSPACE=0 when running make serve.",
  ];
</script>

<svelte:head>
  <title>Hosted SaaS — developer notes</title>
</svelte:head>

<main class="hosted-page hosted-page--wide">
  <h1 class="hosted-title">Developer notes</h1>
  <p class="hosted-lede">
    Advanced options and environment variables for local hosted SaaS. Most
    people can ignore this and use <a class="hosted-link" href="/hosted/start"
      >Home</a
    >.
  </p>
  <ul class="hosted-bullets">
    {#each tips as line}
      <li>{line}</li>
    {/each}
  </ul>
</main>
