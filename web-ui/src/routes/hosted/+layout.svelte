<script>
  import { page } from "$app/stores";

  import "$lib/styles/hosted.css";

  let { children } = $props();

  const nav = [
    { href: "/hosted/start", label: "Home" },
    { href: "/hosted/signup", label: "Create account" },
    { href: "/hosted/signin", label: "Sign in" },
    { href: "/hosted/onboarding", label: "Dashboard" },
    { href: "/hosted/dev", label: "Developer notes" },
  ];
</script>

<div class="hosted-shell">
  <header class="hosted-header">
    <span class="hosted-brand">OAR — hosted</span>
    <nav class="hosted-nav" aria-label="Hosted account">
      {#each nav as item}
        <a
          href={item.href}
          class="hosted-nav-link"
          class:hosted-nav-link--active={$page.url.pathname === item.href}
          data-sveltekit-preload-data="tap">{item.label}</a
        >
      {/each}
    </nav>
  </header>
  {@render children()}
</div>
