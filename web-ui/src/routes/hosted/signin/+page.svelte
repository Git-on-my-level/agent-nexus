<script>
  import { onMount } from "svelte";

  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import Button from "$lib/components/Button.svelte";
  import {
    createHostedLaunchSession,
    startHostedOAuthFlow,
  } from "$lib/hosted/oauthFlow.js";
  import {
    normalizeHostedLaunchFinishURL,
    readHostedLaunchParams,
  } from "$lib/hosted/launchFlow.js";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  let oauthBusyProvider = $state("");
  let message = $state("");
  const continuationQuery = $derived($page.url.search ?? "");

  onMount(async () => {
    await continueLaunchFlowIfPresent();
  });

  async function startOAuth(provider) {
    message = "";
    oauthBusyProvider = String(provider ?? "")
      .trim()
      .toLowerCase();
    try {
      const { authorizationURL, provider: normalizedProvider } =
        await startHostedOAuthFlow({
          cpFetch: hostedCpFetch,
          provider,
          pageUrl: $page.url,
          mode: "signin",
        });
      oauthBusyProvider = normalizedProvider;
      window.location.assign(authorizationURL);
    } catch (e) {
      message = e instanceof Error ? e.message : "OAuth sign-in failed.";
    } finally {
      oauthBusyProvider = "";
    }
  }

  async function continueLaunchFlowIfPresent() {
    const launchParams = readHostedLaunchParams($page.url.searchParams);
    if (!launchParams.hasContinuation) return { kind: "noop" };

    let launchPayload;
    try {
      launchPayload = await createHostedLaunchSession({
        cpFetch: hostedCpFetch,
        workspaceId: launchParams.workspaceId,
        returnPath: launchParams.returnPath,
      });
    } catch (error) {
      message =
        error instanceof Error ? error.message : "Could not continue launch.";
      return { kind: "error" };
    }

    const finishURL = normalizeHostedLaunchFinishURL(
      launchPayload?.launch_session?.finish_url,
    );
    if (!finishURL) {
      message = "Launch session response did not include a valid finish URL.";
      return { kind: "error" };
    }

    if (typeof window !== "undefined") {
      window.location.assign(finishURL);
      return { kind: "redirect" };
    }
    await goto(finishURL);
    return { kind: "redirect" };
  }
</script>

<svelte:head>
  <title>Sign in — ANX</title>
</svelte:head>

<div class="mx-auto max-w-md py-8">
  <div class="rounded-md border border-line bg-bg-soft px-6 py-6">
    <h1 class="text-display text-fg">Welcome back</h1>
    <p class="mt-1.5 text-meta text-fg-subtle">
      Hosted sign-in uses Google or GitHub only.
    </p>

    <div class="mt-5 space-y-3">
      <Button
        type="button"
        variant="primary"
        onclick={() => startOAuth("google")}
        disabled={Boolean(oauthBusyProvider)}
        class="w-full"
      >
        {oauthBusyProvider === "google"
          ? "Redirecting to Google…"
          : "Continue with Google"}
      </Button>
      <Button
        type="button"
        variant="secondary"
        onclick={() => startOAuth("github")}
        disabled={Boolean(oauthBusyProvider)}
        class="w-full"
      >
        {oauthBusyProvider === "github"
          ? "Redirecting to GitHub…"
          : "Continue with GitHub"}
      </Button>
    </div>

    {#if message}
      <p
        role="alert"
        class="mt-4 rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
      >
        {message}
      </p>
    {/if}
  </div>

  <p class="mt-4 text-center text-meta text-fg-subtle">
    New to ANX?
    <a
      class="text-accent-text underline underline-offset-2 hover:text-accent-text"
      href={`/hosted/signup${continuationQuery}`}>Create an account</a
    >
  </p>
</div>
