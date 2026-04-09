<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { onMount } from "svelte";

  import {
    completeControlSession,
    controlAuthenticated,
    controlSessionReady,
    initializeControlSession,
  } from "$lib/controlSession.js";
  import { controlClient } from "$lib/controlClient.js";
  import {
    createPasskeyCredential,
    getPasskeyAssertion,
  } from "$lib/passkeyBrowser.js";

  let loginEmail = $state("");
  let registrationEmail = $state("");
  let registrationDisplayName = $state("");
  let registrationError = $state("");
  let loginError = $state("");
  let loadingRegistration = $state(false);
  let loadingLogin = $state(false);

  function hasInviteFlow() {
    return $page.url.searchParams.get("invite") === "1";
  }

  function resolveRedirectPath() {
    const redirectPath = $page.url.searchParams.get("redirect") || "/dashboard";
    return redirectPath.startsWith("/") ? redirectPath : "/dashboard";
  }

  function requireAuthResponse(result, label) {
    if (!result?.account || !result?.session) {
      throw new Error(`${label} returned an unexpected response.`);
    }

    return result;
  }

  onMount(async () => {
    await initializeControlSession();
    if ($controlAuthenticated && !hasInviteFlow()) {
      goto(resolveRedirectPath());
    }
  });

  $effect(() => {
    if ($controlAuthenticated && !hasInviteFlow()) {
      goto(resolveRedirectPath());
    }
  });

  async function handleRegistration() {
    if (!registrationEmail.trim()) {
      registrationError = "Email is required.";
      return;
    }

    if (!registrationDisplayName.trim()) {
      registrationError = "Display name is required.";
      return;
    }

    loadingRegistration = true;
    registrationError = "";
    loginError = "";

    try {
      const options = await controlClient.startPasskeyRegistration({
        email: registrationEmail.trim(),
        display_name: registrationDisplayName.trim(),
      });
      const credential = await createPasskeyCredential(
        options.public_key_options,
      );
      const result = await controlClient.finishPasskeyRegistration({
        registration_session_id: options.registration_session_id,
        credential,
      });
      requireAuthResponse(result, "Registration");

      completeControlSession(result.account);
      goto(resolveRedirectPath());
    } catch (error) {
      registrationError =
        error instanceof Error ? error.message : "Registration failed.";
    } finally {
      loadingRegistration = false;
    }
  }

  async function handleLogin() {
    if (!loginEmail.trim()) {
      loginError = "Email is required.";
      return;
    }

    loadingLogin = true;
    loginError = "";
    registrationError = "";

    try {
      const options = await controlClient.startSession({
        email: loginEmail.trim(),
      });
      const credential = await getPasskeyAssertion(options.public_key_options);
      const result = await controlClient.finishSession({
        session_id: options.session_id,
        credential,
      });
      requireAuthResponse(result, "Login");

      completeControlSession(result.account);
      goto(resolveRedirectPath());
    } catch (error) {
      loginError = error instanceof Error ? error.message : "Sign in failed.";
    } finally {
      loadingLogin = false;
    }
  }
</script>

<svelte:head>
  <title>Sign in - OAR Control</title>
</svelte:head>

{#if !$controlSessionReady}
  <main class="min-h-screen bg-[var(--ui-bg)] px-4 py-6 text-[var(--ui-text)]">
    <div
      class="mx-auto flex max-w-xl items-center justify-center rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-4 py-6 text-[13px]"
    >
      Loading...
    </div>
  </main>
{:else if $controlAuthenticated && !$page.url.searchParams.get("invite")}
  <main class="min-h-screen bg-[var(--ui-bg)] px-4 py-6 text-[var(--ui-text)]">
    <div
      class="mx-auto flex max-w-xl items-center justify-center rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-4 py-6 text-[13px]"
    >
      Redirecting to dashboard...
    </div>
  </main>
{:else}
  <main class="min-h-screen bg-[var(--ui-bg)] px-4 py-6 text-[var(--ui-text)]">
    <div class="mx-auto max-w-2xl">
      <div class="mb-5 text-center">
        <h1 class="text-lg font-semibold text-[var(--ui-text)]">
          Organization Autorunner
        </h1>
        <p class="text-[var(--ui-text-muted)]">
          Sign in to access your organizations and workspaces
        </p>
      </div>

      {#if $page.url.searchParams.get("invite") === "1"}
        <div
          class="mb-4 rounded-md border border-indigo-500/30 bg-indigo-500/10 px-4 py-3 text-[13px] text-indigo-300"
        >
          Continue with your passkey to accept this organization invite.
        </div>
      {/if}

      <div class="flex gap-4 lg:flex-row">
        <section
          class="flex-1 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)]"
        >
          <div class="border-b border-[var(--ui-border)] px-4 py-3">
            <p
              class="text-[11px] font-medium uppercase tracking-wide text-[var(--ui-text-muted)]"
            >
              Sign in
            </p>
            <h2 class="mt-1 text-lg font-semibold text-[var(--ui-text)]">
              Use your passkey
            </h2>
            <p class="mt-2 text-[13px] text-[var(--ui-text-muted)]">
              Sign in with your existing passkey to access your account.
            </p>
          </div>

          <div class="space-y-3 px-4 py-4">
            <div>
              <label
                class="block text-[12px] font-medium text-[var(--ui-text-muted)]"
                for="login-email"
              >
                Email
              </label>
              <input
                bind:value={loginEmail}
                class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
                id="login-email"
                placeholder="you@example.com"
                type="email"
              />
            </div>

            <button
              class="w-full rounded-md bg-indigo-600 px-3 py-2 text-[12px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
              disabled={loadingLogin}
              onclick={handleLogin}
              type="button"
            >
              {loadingLogin ? "Signing in..." : "Sign in with passkey"}
            </button>

            {#if loginError}
              <div
                class="rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
              >
                {loginError}
              </div>
            {/if}
          </div>
        </section>

        <section
          class="flex-1 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)]"
        >
          <div class="border-b border-[var(--ui-border)] px-4 py-3">
            <p
              class="text-[11px] font-medium uppercase tracking-wide text-[var(--ui-text-muted)]"
            >
              New account?
            </p>
            <h2 class="mt-1 text-lg font-semibold text-[var(--ui-text)]">
              Create an account
            </h2>
            <p class="mt-2 text-[13px] text-[var(--ui-text-muted)]">
              Register a new account with a new passkey.
            </p>
          </div>

          <div class="space-y-3 px-4 py-4">
            <div>
              <label
                class="block text-[12px] font-medium text-[var(--ui-text-muted)]"
                for="register-email"
              >
                Email
              </label>
              <input
                bind:value={registrationEmail}
                class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
                id="register-email"
                placeholder="you@example.com"
                type="email"
              />
            </div>

            <div>
              <label
                class="block text-[12px] font-medium text-[var(--ui-text-muted)]"
                for="register-display-name"
              >
                Display name
              </label>
              <input
                bind:value={registrationDisplayName}
                class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
                id="register-display-name"
                placeholder="Your name"
                type="text"
              />
            </div>

            {#if registrationError}
              <div
                class="rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
              >
                {registrationError}
              </div>
            {/if}

            <button
              class="w-full rounded-md bg-indigo-600 px-3 py-2 text-[12px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
              disabled={loadingRegistration}
              onclick={handleRegistration}
              type="button"
            >
              {loadingRegistration
                ? "Creating account..."
                : "Create account with passkey"}
            </button>
          </div>
        </section>
      </div>
    </div>
  </main>
{/if}
