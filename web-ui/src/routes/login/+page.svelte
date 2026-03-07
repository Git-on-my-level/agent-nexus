<script>
  import { goto } from "$app/navigation";
  import { onMount } from "svelte";

  import {
    authenticatedAgent,
    completeAuthSession,
    isAuthenticated,
  } from "$lib/authSession";
  import { coreClient } from "$lib/coreClient";
  import {
    createPasskeyCredential,
    getPasskeyAssertion,
  } from "$lib/passkeyBrowser";

  let registrationName = $state("");
  let registrationError = $state("");
  let loginError = $state("");
  let loadingRegistration = $state(false);
  let loadingLogin = $state(false);

  onMount(() => {
    if (isAuthenticated()) {
      goto("/");
    }
  });

  async function handleRegistration() {
    if (!registrationName.trim()) {
      registrationError = "Display name is required.";
      return;
    }

    loadingRegistration = true;
    registrationError = "";
    loginError = "";

    try {
      const options = await coreClient.passkeyRegisterOptions({
        display_name: registrationName.trim(),
      });
      const credential = await createPasskeyCredential(options.options);
      const result = await coreClient.passkeyRegisterVerify({
        session_id: options.session_id,
        credential,
      });
      completeAuthSession(result.agent, result.tokens);
      await goto("/");
    } catch (error) {
      registrationError =
        error instanceof Error ? error.message : "Passkey registration failed.";
    } finally {
      loadingRegistration = false;
    }
  }

  async function handleLogin() {
    loadingLogin = true;
    loginError = "";
    registrationError = "";

    try {
      const options = await coreClient.passkeyLoginOptions({});
      const credential = await getPasskeyAssertion(options.options);
      const result = await coreClient.passkeyLoginVerify({
        session_id: options.session_id,
        credential,
      });
      completeAuthSession(result.agent, result.tokens);
      await goto("/");
    } catch (error) {
      loginError =
        error instanceof Error ? error.message : "Passkey sign-in failed.";
    } finally {
      loadingLogin = false;
    }
  }
</script>

{#if $authenticatedAgent}
  <main class="min-h-screen bg-gray-50 px-4 py-10 text-gray-700">
    <div
      class="mx-auto flex max-w-xl items-center justify-center rounded-md border border-gray-200 bg-gray-100 px-4 py-10 text-[13px]"
    >
      Redirecting to the workspace...
    </div>
  </main>
{:else}
  <main class="min-h-screen bg-gray-50 px-4 py-10 text-gray-700">
    <div class="mx-auto flex max-w-5xl flex-col gap-4 lg:flex-row">
      <section
        class="rounded-md border border-gray-200 bg-gray-100 lg:w-[22rem]"
      >
        <div class="border-b border-gray-200 px-4 py-3">
          <p
            class="text-[11px] font-medium uppercase tracking-wide text-gray-400"
          >
            Auth-first
          </p>
          <h1 class="mt-1 text-lg font-semibold text-gray-900">
            Sign in with a passkey
          </h1>
          <p class="mt-2 text-[13px] text-gray-500">
            Browser passkeys are now the primary web identity path. Once
            authenticated, all writes are locked to your principal actor.
          </p>
        </div>

        <div class="space-y-3 px-4 py-3">
          <button
            class="w-full rounded-md bg-indigo-600 px-3 py-2 text-[12px] font-medium text-white hover:bg-indigo-500"
            disabled={loadingLogin}
            onclick={handleLogin}
            type="button"
          >
            {loadingLogin
              ? "Waiting for passkey..."
              : "Sign in with existing passkey"}
          </button>

          {#if loginError}
            <div
              class="rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
            >
              {loginError}
            </div>
          {/if}

          <p class="text-[12px] text-gray-500">
            This uses discoverable WebAuthn login. No username step is required.
          </p>
        </div>
      </section>

      <section class="rounded-md border border-gray-200 bg-gray-100 lg:flex-1">
        <div class="border-b border-gray-200 px-4 py-3">
          <p
            class="text-[11px] font-medium uppercase tracking-wide text-gray-400"
          >
            First-time setup
          </p>
          <h2 class="mt-1 text-[13px] font-semibold text-gray-900">
            Create a new passkey-backed principal
          </h2>
        </div>

        <form
          class="space-y-4 px-4 py-4"
          onsubmit={(event) => {
            event.preventDefault();
            handleRegistration();
          }}
        >
          <label
            class="block text-[12px] font-medium text-gray-600"
            for="display-name"
          >
            Display name
          </label>
          <input
            bind:value={registrationName}
            class="w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-[13px] text-gray-800"
            id="display-name"
            maxlength="120"
            placeholder="Alex Chen"
            type="text"
          />

          {#if registrationError}
            <div
              class="rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
            >
              {registrationError}
            </div>
          {/if}

          <div
            class="rounded-md bg-indigo-500/10 px-3 py-2 text-[12px] text-indigo-400"
          >
            Registration creates a new agent, a linked actor, and an
            authenticated browser session in one step.
          </div>

          <div class="flex flex-wrap gap-2">
            <button
              class="rounded-md bg-indigo-600 px-3 py-2 text-[12px] font-medium text-white hover:bg-indigo-500"
              disabled={loadingRegistration}
              type="submit"
            >
              {loadingRegistration
                ? "Waiting for passkey..."
                : "Create passkey and continue"}
            </button>
            <a
              class="rounded-md border border-gray-200 bg-gray-100 px-3 py-2 text-[12px] font-medium text-gray-600 hover:bg-gray-200"
              href="/"
            >
              Back to actor mode
            </a>
          </div>
        </form>
      </section>
    </div>
  </main>
{/if}
