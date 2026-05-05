<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import { setActiveOrg } from "$lib/hosted/session.js";

  const orgId = $derived(String($page.params.orgId ?? ""));

  $effect(() => {
    if (!browser || !orgId) return;
    setActiveOrg(orgId);
    void goto("/hosted/dashboard", { replaceState: true });
  });
</script>

<svelte:head>
  <title>Dashboard — Agent Nexus (ANX)</title>
</svelte:head>
