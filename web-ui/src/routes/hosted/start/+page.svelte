<script>
  import { onMount } from "svelte";

  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";
  import { sanitizeHostedReturnPath } from "$lib/hosted/launchFlow.js";

  function sanitizeHostedStartNext(value) {
    const path = sanitizeHostedReturnPath(value, "/hosted/dashboard");
    if (path === "/hosted" || path.startsWith("/hosted/")) {
      return path;
    }
    return "/hosted/dashboard";
  }

  onMount(async () => {
    if (!browser) return;
    const continuation = sanitizeHostedStartNext(
      $page.url.searchParams.get("next") ?? "",
    );
    try {
      const res = await hostedCpFetch("account/me");
      if (res.ok) {
        await goto(continuation);
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
