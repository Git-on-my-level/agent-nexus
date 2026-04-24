<script>
  import { onMount } from "svelte";

  import { page } from "$app/stores";

  import Button from "$lib/components/Button.svelte";
  import { startHostedOAuthFlow } from "$lib/hosted/oauthFlow.js";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  let inviteToken = $state("");
  let oauthBusyProvider = $state("");
  let message = $state("");
  let showInviteField = $state(false);
  const continuationQuery = $derived($page.url.search ?? "");

  onMount(() => {
    const inv = $page.url.searchParams.get("invite");
    if (inv) {
      inviteToken = inv;
      showInviteField = true;
    }
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
          mode: "signup",
          inviteToken,
        });
      oauthBusyProvider = normalizedProvider;
      window.location.assign(authorizationURL);
    } catch (e) {
      message = e instanceof Error ? e.message : "OAuth sign-up failed.";
    } finally {
      oauthBusyProvider = "";
    }
  }
</script>

<svelte:head>
  <title>Create your account — ANX</title>
</svelte:head>

<div class="mx-auto max-w-md py-8">
  <div class="rounded-md border border-line bg-bg-soft px-6 py-6">
    <h1 class="text-display text-fg">Create your account</h1>
    <p class="mt-1.5 text-meta text-fg-subtle">
      Hosted signup is Google or GitHub first. Invite tokens are still accepted
      here when you have one.
    </p>

    <div class="mt-5 space-y-3">
      {#if showInviteField}
        <label class="block text-micro text-fg-muted">
          Invite token
          <input
            type="text"
            bind:value={inviteToken}
            disabled={Boolean(oauthBusyProvider)}
            placeholder="From your invite email"
            class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 text-body text-fg placeholder:text-[var(--fg-subtle)]"
          />
        </label>
      {:else}
        <button
          type="button"
          class="text-micro text-accent-text hover:text-accent-text"
          onclick={() => (showInviteField = true)}
        >
          + I have an invite token
        </button>
      {/if}

      {#if inviteToken.trim()}
        <p class="rounded-md bg-ok-soft px-3 py-2 text-micro text-ok-text">
          Invite token attached. It will continue through Google or GitHub
          signup.
        </p>
      {/if}

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

    <div class="mt-4">
      <p class="text-micro text-fg-subtle">
        By continuing you agree to the ANX terms of service.
      </p>
    </div>
  </div>

  <p class="mt-4 text-center text-meta text-fg-subtle">
    Already have an account?
    <a
      class="text-accent-text underline underline-offset-2 hover:text-accent-text"
      href={`/hosted/signin${continuationQuery}`}>Sign in</a
    >
  </p>
</div>
