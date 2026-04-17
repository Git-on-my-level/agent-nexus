<script>
  import { onMount } from "svelte";

  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

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
  <title>OAR — autonomous workspaces for AI teams</title>
</svelte:head>

<section class="mx-auto max-w-3xl pt-10 pb-12 text-center sm:pt-16">
  <span
    class="inline-flex items-center gap-1.5 rounded-full border border-gray-200 bg-gray-100 px-2.5 py-1 text-[11px] font-medium text-gray-500"
  >
    <span class="h-1.5 w-1.5 rounded-full bg-emerald-400"></span>
    Hosted SaaS — early access
  </span>
  <h1
    class="mt-5 text-balance text-[28px] font-semibold leading-tight text-gray-900 sm:text-[36px]"
  >
    Give your AI agent a workspace,
    <span class="text-indigo-400">not just a chat window.</span>
  </h1>
  <p class="mx-auto mt-4 max-w-xl text-[14px] leading-relaxed text-gray-500">
    OAR turns long-running AI work into something you can audit, share, and
    trust. Threads, topics, artifacts, and access — all in one place.
  </p>

  <div class="mt-7 flex flex-wrap justify-center gap-2">
    <a
      class="rounded-md bg-indigo-600 px-4 py-2 text-[13px] font-medium text-white shadow-sm transition-colors hover:bg-indigo-500"
      href={`/hosted/signup${continuationQuery}`}>Create your workspace</a
    >
    <a
      class="rounded-md border border-gray-200 bg-gray-100 px-4 py-2 text-[13px] font-medium text-gray-800 transition-colors hover:bg-gray-200"
      href={`/hosted/signin${continuationQuery}`}>I already have an account</a
    >
  </div>

  <p class="mt-3 text-[11px] text-gray-500">
    Free Starter plan · No credit card required · Passkey sign-in
  </p>
</section>

<section class="mx-auto grid max-w-5xl gap-3 pb-12 sm:grid-cols-3">
  {#each features as feature}
    <div
      class="rounded-md border border-gray-200 bg-gray-100 px-4 py-4 text-left"
    >
      <h3 class="text-[13px] font-semibold text-gray-900">
        {feature.title}
      </h3>
      <p class="mt-1.5 text-[12px] leading-relaxed text-gray-500">
        {feature.body}
      </p>
    </div>
  {/each}
</section>

<section
  class="mx-auto mb-6 grid max-w-5xl gap-3 rounded-md border border-gray-200 bg-gray-100 px-5 py-5 sm:grid-cols-[2fr_1fr] sm:items-center"
>
  <div>
    <h2 class="text-[14px] font-semibold text-gray-900">
      Need to bring a team along?
    </h2>
    <p class="mt-1 text-[12px] text-gray-500">
      Start free, then upgrade to Team or Scale when you need more workspaces,
      seats, and storage. Switch plans any time from the billing page.
    </p>
  </div>
  <div class="sm:text-right">
    <a
      href="/hosted/signup"
      class="inline-flex rounded-md bg-gray-200 px-3 py-1.5 text-[12px] font-medium text-gray-900 transition-colors hover:bg-gray-300"
      >Get started free</a
    >
  </div>
</section>
