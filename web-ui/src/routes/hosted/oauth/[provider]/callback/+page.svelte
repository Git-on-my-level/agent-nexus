<script>
  import { onMount } from "svelte";

  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import {
    hostedCpFetch,
    persistHostedCpAccessToken,
  } from "$lib/hosted/cpFetch.js";
  import {
    clearHostedOAuthContinuation,
    deriveHostedOAuthRedirectURI,
    normalizeHostedOAuthProvider,
    readHostedOAuthContinuation,
    resolveHostedPostAuthPath,
  } from "$lib/hosted/oauthFlow.js";
  import { normalizeHostedLaunchFinishURL } from "$lib/hosted/launchFlow.js";
  import { loadHostedSession } from "$lib/hosted/session.js";

  let busy = $state(true);
  let message = $state("");
  let mode = $state("signin");
  let sessionEstablished = $state(false);
  let continuation = $state(null);

  const titleText = $derived(
    mode === "signup" ? "Finishing signup — ANX" : "Finishing sign-in — ANX",
  );
  const headingText = $derived(
    mode === "signup" ? "Finishing signup" : "Finishing sign-in",
  );
  const recoveryHref = $derived.by(() => {
    if (sessionEstablished) {
      return resolveHostedPostAuthPath(continuation);
    }
    return mode === "signup" ? "/hosted/signup" : "/hosted/signin";
  });
  const recoveryLabel = $derived.by(() => {
    if (sessionEstablished) {
      return mode === "signup"
        ? "Continue to onboarding"
        : "Continue to dashboard";
    }
    return mode === "signup" ? "Back to signup" : "Back to sign in";
  });

  onMount(async () => {
    if (!browser) return;
    await finishOAuthFlow();
  });

  async function readError(res) {
    try {
      const j = await res.json();
      return j?.error?.message || j?.error?.code || res.statusText;
    } catch {
      return res.statusText;
    }
  }

  function friendlyProviderError(rawError) {
    const normalized = String(rawError ?? "")
      .trim()
      .toLowerCase();
    if (
      normalized === "access_denied" ||
      normalized === "user_denied" ||
      normalized === "cancelled_on_user_request"
    ) {
      return mode === "signup"
        ? "Signup was canceled at the identity provider."
        : "Sign-in was canceled at the identity provider.";
    }
    return mode === "signup"
      ? "Signup could not be completed at the identity provider."
      : "Sign-in could not be completed at the identity provider.";
  }

  async function continueWorkspaceLaunch() {
    if (!continuation?.workspaceId) {
      return false;
    }
    const launchResponse = await hostedCpFetch(
      `workspaces/${encodeURIComponent(continuation.workspaceId)}/launch-sessions`,
      {
        method: "POST",
        body: JSON.stringify({
          return_path: continuation.returnPath || "/",
        }),
      },
    );
    if (!launchResponse.ok) {
      message = await readError(launchResponse);
      return false;
    }

    const launchPayload = await launchResponse.json();
    const finishURL = normalizeHostedLaunchFinishURL(
      launchPayload?.launch_session?.finish_url,
    );
    if (!finishURL) {
      message = "Launch session response did not include a valid finish URL.";
      return false;
    }
    clearHostedOAuthContinuation(
      String($page.url.searchParams.get("state") ?? "").trim(),
    );
    window.location.assign(finishURL);
    return true;
  }

  async function finishOAuthFlow() {
    busy = true;
    message = "";

    const provider = normalizeHostedOAuthProvider($page.params.provider);
    if (!provider) {
      message = "Unsupported OAuth provider.";
      busy = false;
      return;
    }

    const state = String($page.url.searchParams.get("state") ?? "").trim();
    continuation = readHostedOAuthContinuation(state);
    mode = continuation?.mode === "signup" ? "signup" : "signin";

    const providerError = String(
      $page.url.searchParams.get("error") ?? "",
    ).trim();
    if (providerError) {
      message = friendlyProviderError(providerError);
      busy = false;
      return;
    }

    const code = String($page.url.searchParams.get("code") ?? "").trim();
    if (!code || !state) {
      message =
        mode === "signup"
          ? "OAuth signup callback is missing code or state."
          : "OAuth sign-in callback is missing code or state.";
      busy = false;
      return;
    }

    const redirectURI = deriveHostedOAuthRedirectURI(provider, window.location);
    if (!redirectURI) {
      message = "Could not determine the OAuth callback URL.";
      busy = false;
      return;
    }

    try {
      const finish = await hostedCpFetch(`account/oauth/${provider}/finish`, {
        method: "POST",
        body: JSON.stringify({
          code,
          state,
          redirect_uri: redirectURI,
          invite_token: continuation?.inviteToken || undefined,
        }),
      });
      if (!finish.ok) {
        message = await readError(finish);
        return;
      }
      const finishBody = await finish.json();
      const token = String(finishBody.session?.access_token ?? "").trim();
      if (!token) {
        message =
          mode === "signup"
            ? "Signed up but no session token was returned."
            : "Signed in but no session token was returned.";
        return;
      }
      persistHostedCpAccessToken(token);
      await loadHostedSession();
      sessionEstablished = true;

      if (continuation?.workspaceId) {
        if (!(await continueWorkspaceLaunch())) {
          return;
        }
        return;
      }

      clearHostedOAuthContinuation(state);
      await goto(resolveHostedPostAuthPath(continuation), {
        replaceState: true,
      });
    } catch (error) {
      message =
        error instanceof Error
          ? error.message
          : mode === "signup"
            ? "OAuth signup failed."
            : "OAuth sign-in failed.";
    } finally {
      busy = false;
    }
  }
</script>

<svelte:head>
  <title>{titleText}</title>
</svelte:head>

<div class="mx-auto max-w-md py-8">
  <div class="rounded-md border border-line bg-bg-soft px-6 py-6">
    <h1 class="text-display text-fg">{headingText}</h1>
    {#if message}
      <p
        role="alert"
        class="mt-4 rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
      >
        {message}
      </p>
      {#if sessionEstablished && continuation?.workspaceId}
        <button
          type="button"
          class="mt-4 w-full rounded-md border border-line bg-bg px-3 py-2 text-micro text-fg hover:bg-panel-hover"
          onclick={() => continueWorkspaceLaunch()}
        >
          Continue to workspace
        </button>
      {/if}
      <p class="mt-4 text-micro text-fg-subtle">
        <a
          class="text-accent-text underline underline-offset-2 hover:text-accent-text"
          href={recoveryHref}>{recoveryLabel}</a
        >
      </p>
    {:else}
      <p class="mt-2 text-meta text-fg-subtle">
        {busy
          ? mode === "signup"
            ? "Verifying the provider response and preparing your hosted account…"
            : "Verifying the provider response and loading your hosted session…"
          : "Redirecting…"}
      </p>
    {/if}
  </div>
</div>
