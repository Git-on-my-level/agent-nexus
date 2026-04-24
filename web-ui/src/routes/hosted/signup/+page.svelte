<script>
  import { onMount } from "svelte";

  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import Button from "$lib/components/Button.svelte";
  import {
    buildHostedOAuthContinuation,
    storeHostedOAuthContinuation,
  } from "$lib/hosted/oauthFlow.js";
  import { createPasskeyCredential } from "$lib/passkeyBrowser";
  import {
    hostedCpFetch,
    persistHostedCpAccessToken,
  } from "$lib/hosted/cpFetch.js";
  import {
    normalizeHostedLaunchFinishURL,
    readHostedLaunchParams,
  } from "$lib/hosted/launchFlow.js";
  import { loadHostedSession } from "$lib/hosted/session.js";

  let email = $state("");
  let displayName = $state("");
  let inviteToken = $state("");
  let passkeyBusy = $state(false);
  let oauthBusyProvider = $state("");
  let message = $state("");
  let showInviteField = $state(false);
  let showPasskeyFallback = $state(false);
  const continuationQuery = $derived($page.url.search ?? "");

  onMount(() => {
    const inv = $page.url.searchParams.get("invite");
    if (inv) {
      inviteToken = inv;
      showInviteField = true;
    }
  });

  async function readError(res) {
    try {
      const j = await res.json();
      return j?.error?.message || j?.error?.code || res.statusText;
    } catch {
      return res.statusText;
    }
  }

  async function submit() {
    message = "";
    if (!email.trim() || !displayName.trim()) {
      message = "Email and your name are required.";
      return;
    }
    passkeyBusy = true;
    try {
      const start = await hostedCpFetch(
        "account/passkeys/registrations/start",
        {
          method: "POST",
          body: JSON.stringify({
            email: email.trim(),
            display_name: displayName.trim(),
          }),
        },
      );
      if (!start.ok) {
        message = await readError(start);
        return;
      }
      const startBody = await start.json();
      const registrationSessionId = String(
        startBody.registration_session_id ?? "",
      ).trim();
      const options = startBody.public_key_options;
      if (!registrationSessionId || !options) {
        message = "Unexpected response from control plane.";
        return;
      }
      const credential = await createPasskeyCredential(options);
      const finishPayload = {
        registration_session_id: registrationSessionId,
        credential,
      };
      const inv = inviteToken.trim();
      if (inv) {
        finishPayload.invite_token = inv;
      }
      const finish = await hostedCpFetch(
        "account/passkeys/registrations/finish",
        {
          method: "POST",
          body: JSON.stringify(finishPayload),
        },
      );
      if (!finish.ok) {
        message = await readError(finish);
        return;
      }
      const finishBody = await finish.json();
      const token = String(finishBody.session?.access_token ?? "").trim();
      if (!token) {
        message = "Signed up but no session token was returned.";
        return;
      }
      persistHostedCpAccessToken(token);
      await loadHostedSession();
      const continuationHandled = await continueLaunchFlowIfPresent();
      if (!continuationHandled) {
        await goto("/hosted/onboarding/organization");
      }
    } catch (e) {
      message = e instanceof Error ? e.message : "Passkey registration failed.";
    } finally {
      passkeyBusy = false;
    }
  }

  async function startOAuth(provider) {
    const normalizedProvider = String(provider ?? "")
      .trim()
      .toLowerCase();
    message = "";
    oauthBusyProvider = normalizedProvider;
    try {
      const start = await hostedCpFetch(
        `account/oauth/${normalizedProvider}/start`,
        {
          method: "POST",
          body: "{}",
        },
      );
      if (!start.ok) {
        message = await readError(start);
        return;
      }
      const startBody = await start.json();
      const oauthSession = startBody?.oauth_session;
      const authorizationURL = String(
        oauthSession?.authorization_url ?? "",
      ).trim();
      const state = String(oauthSession?.state ?? "").trim();
      if (!authorizationURL || !state) {
        message = "Unexpected response from control plane.";
        return;
      }
      storeHostedOAuthContinuation(
        state,
        buildHostedOAuthContinuation($page.url, {
          mode: "signup",
          inviteToken,
        }),
      );
      window.location.assign(authorizationURL);
    } catch (e) {
      message = e instanceof Error ? e.message : "OAuth sign-up failed.";
    } finally {
      oauthBusyProvider = "";
    }
  }

  async function continueLaunchFlowIfPresent() {
    const launchParams = readHostedLaunchParams($page.url.searchParams);
    if (!launchParams.hasContinuation) {
      return false;
    }

    const launchResponse = await hostedCpFetch(
      `workspaces/${encodeURIComponent(launchParams.workspaceId)}/launch-sessions`,
      {
        method: "POST",
        body: JSON.stringify({
          return_path: launchParams.returnPath,
        }),
      },
    );
    if (!launchResponse.ok) {
      message = await readError(launchResponse);
      return true;
    }

    const launchPayload = await launchResponse.json();
    const finishURL = normalizeHostedLaunchFinishURL(
      launchPayload?.launch_session?.finish_url,
    );
    if (!finishURL) {
      message = "Launch session response did not include a valid finish URL.";
      return true;
    }

    if (typeof window !== "undefined") {
      window.location.assign(finishURL);
      return true;
    }
    await goto(finishURL);
    return true;
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
            disabled={passkeyBusy || Boolean(oauthBusyProvider)}
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
        disabled={Boolean(oauthBusyProvider || passkeyBusy)}
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
        disabled={Boolean(oauthBusyProvider || passkeyBusy)}
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

    <div class="mt-5 border-t border-line pt-3">
      <button
        type="button"
        class="text-micro text-fg-subtle hover:text-fg"
        onclick={() => (showPasskeyFallback = !showPasskeyFallback)}
      >
        {showPasskeyFallback ? "Hide" : "Use a passkey instead"}
      </button>
      {#if showPasskeyFallback}
        <p class="mt-2 text-micro text-fg-subtle">
          Only use this if your account still depends on a passkey during
          migration or local testing.
        </p>
        <form
          class="mt-3 space-y-3"
          onsubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <label class="block text-micro text-fg-muted">
            Work email
            <input
              type="email"
              autocomplete="email"
              bind:value={email}
              disabled={passkeyBusy || Boolean(oauthBusyProvider)}
              required
              placeholder="you@company.com"
              class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 text-body text-fg placeholder:text-[var(--fg-subtle)]"
            />
          </label>

          <label class="block text-micro text-fg-muted">
            Your name
            <input
              type="text"
              autocomplete="name"
              bind:value={displayName}
              disabled={passkeyBusy || Boolean(oauthBusyProvider)}
              required
              placeholder="Jane Doe"
              class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 text-body text-fg placeholder:text-[var(--fg-subtle)]"
            />
          </label>

          <Button
            type="submit"
            variant="secondary"
            busy={passkeyBusy}
            disabled={passkeyBusy || Boolean(oauthBusyProvider)}
            class="w-full"
          >
            {passkeyBusy ? "Setting up your passkey…" : "Continue with passkey"}
          </Button>
        </form>
      {/if}
    </div>

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
