<script>
  import { onMount } from "svelte";

  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import SkeletonCard from "$lib/components/state/SkeletonCard.svelte";
  import { setActiveOrg } from "$lib/hosted/session.js";

  const orgId = $derived(String($page.params.orgId ?? ""));

  // Organization overview was folded into Workspaces to keep one landing page.
  onMount(() => {
    if (orgId) {
      setActiveOrg(orgId);
    }
    void goto("/hosted/dashboard", { replaceState: true });
  });
</script>

<svelte:head>
  <title>Workspaces — ANX</title>
</svelte:head>

<SkeletonCard />
