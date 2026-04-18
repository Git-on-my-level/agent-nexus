<!-- State coverage: static marketing page — no data fetch, no state variants needed. -->
<script>
  import { onMount } from "svelte";

  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import Button from "$lib/components/Button.svelte";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  /** If the user already has a valid session cookie, skip the marketing page. */
  onMount(async () => {
    if (!browser) return;
    try {
      const res = await hostedCpFetch("account/me");
      if (res.ok) {
        const next = String($page.url.searchParams.get("next") ?? "").trim();
        await goto(next || "/hosted/dashboard");
      }
    } catch {
      // Ignore — let the marketing page render.
    }
  });

  const features = [
    {
      title: "Pair-program with your AI agent",
      body: "Spin up a workspace, hand it goals, and watch it ship. Your agent runs in the cloud, organized like a real teammate.",
    },
    {
      title: "Threads, topics, and artifacts",
      body: "Conversations don't disappear. Every plan, decision, and artifact is captured in a searchable workspace your team can audit.",
    },
    {
      title: "Built for teams",
      body: "Organizations, roles, and seat-based plans. Invite teammates, share workspaces, and bring your own billing.",
    },
  ];

  const continuationQuery = $derived($page.url.search ?? "");
</script>

<svelte:head>
  <title>Agent Nexus — autonomous workspaces for AI teams</title>
</svelte:head>

<section class="mx-auto max-w-3xl pt-10 pb-12 text-center sm:pt-16">
  <h1
    class="text-balance text-display text-fg sm:text-[36px]"
  >
    Give your AI agent a workspace,
    <span class="text-accent-text">not just a chat window.</span>
  </h1>
  <p class="mx-auto mt-4 max-w-xl text-body text-fg-subtle">
    Agent Nexus turns long-running AI work into something you can audit, share,
    and trust. Threads, topics, artifacts, and access — all in one place.
  </p>

  <div class="mt-7 flex flex-wrap justify-center gap-2">
    <Button variant="primary" size="large" href={`/hosted/signup${continuationQuery}`}>Create your workspace</Button>
    <Button variant="secondary" size="large" href={`/hosted/signin${continuationQuery}`}>I already have an account</Button>
  </div>

  <p class="mt-3 text-micro text-fg-subtle">
    Free Starter plan · No credit card required · Passkey sign-in
  </p>
</section>

<section class="mx-auto grid max-w-5xl gap-3 pb-12 sm:grid-cols-3">
  {#each features as feature}
    <div
      class="rounded-md border border-line bg-bg-soft px-4 py-4 text-left"
    >
      <h3 class="text-subtitle text-fg">
        {feature.title}
      </h3>
      <p class="mt-1.5 text-meta text-fg-subtle">
        {feature.body}
      </p>
    </div>
  {/each}
</section>

<section
  class="mx-auto mb-6 grid max-w-5xl gap-3 rounded-md border border-line bg-bg-soft px-5 py-5 sm:grid-cols-[2fr_1fr] sm:items-center"
>
  <div>
    <h2 class="text-subtitle text-fg">
      Need to bring a team along?
    </h2>
    <p class="mt-1 text-meta text-fg-subtle">
      Start free, then upgrade to Team or Scale when you need more workspaces,
      seats, and storage. Switch plans any time from the billing page.
    </p>
  </div>
  <div class="sm:text-right">
    <Button variant="ghost" href="/hosted/signup">Get started free</Button>
  </div>
</section>
