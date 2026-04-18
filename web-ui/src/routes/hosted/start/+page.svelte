<script>
  import { onMount } from "svelte";

  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  onMount(async () => {
    if (!browser) return;
    const continuation = String($page.url.searchParams.get("next") ?? $page.url.search ?? "").trim();
    try {
      const res = await hostedCpFetch("account/me");
      if (res.ok) {
        await goto(continuation || "/hosted/dashboard");
        return;
      }
    } catch {
      // fall through to signup redirect
    }
    await goto(`/hosted/signup${$page.url.search}`);
  });
</script>

<svelte:head>
  <title>Agent Nexus</title>
</svelte:head>

<div class="flex min-h-screen items-center justify-center">
  <p class="text-meta text-fg-subtle">Redirecting…</p>
</div>
