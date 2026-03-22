<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
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
  import { workspacePath } from "$lib/workspacePaths";
  import { devActorMode } from "$lib/workspaceContext";

  let registrationName = $state("");
  let registrationToken = $state("");
  let registrationError = $state("");
  let loginError = $state("");
  let loadingRegistration = $state(false);
  let loadingLogin = $state(false);
  let loadingBootstrapStatus = $state(true);
  let bootstrapAvailable = $state(false);
  let workspaceSlug = $derived($page.params.workspace);

  onMount(async () => {
    if (isAuthenticated(workspaceSlug)) {
      goto(workspacePath(workspaceSlug));
      return;
    }

    const tokenParam = $page.url.searchParams.get("token");
    if (tokenParam) {
      registrationToken = tokenParam;
    }

    try {
      const status = await coreClient.bootstrapStatus();
      bootstrapAvailable = status.bootstrap_registration_available ?? false;
    } catch {
      bootstrapAvailable = false;
    } finally {
      loadingBootstrapStatus = false;
    }
  });

  $effect(() => {
    if ($authenticatedAgent) {
      goto(workspacePath(workspaceSlug));
    }
  });

  async function handleRegistration() {
    if (!registrationName.trim()) {
      registrationError = "Display name is required.";
      return;
    }

    if (!registrationToken.trim()) {
      registrationError = bootstrapAvailable
        ? "A bootstrap token is required for registration."
        : "An invite token is required for registration.";
      return;
    }

    loadingRegistration = true;
    registrationError = "";
    loginError = "";

    try {
      const registrationTokenValue = registrationToken.trim();
      const tokenKey = bootstrapAvailable ? "bootstrap_token" : "invite_token";
      const optionsPayload = {
        display_name: registrationName.trim(),
      };
      if (registrationTokenValue) {
        optionsPayload[tokenKey] = registrationTokenValue;
      }
      const options = await coreClient.passkeyRegisterOptions(optionsPayload);
      const credential = await createPasskeyCredential(options.options);
      const verifyPayload = {
        session_id: options.session_id,
        credential,
      };
      if (registrationTokenValue) {
        verifyPayload[tokenKey] = registrationTokenValue;
      }
      const result = await coreClient.passkeyRegisterVerify(verifyPayload);
      completeAuthSession(result.agent, workspaceSlug);
      await goto(workspacePath(workspaceSlug));
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
      completeAuthSession(result.agent, workspaceSlug);
      await goto(workspacePath(workspaceSlug));
    } catch (error) {
      loginError =
        error instanceof Error ? error.message : "Passkey sign-in failed.";
    } finally {
      loadingLogin = false;
    }
  }
</script>

{#if $authenticatedAgent}
  <main class="min-h-screen bg-[var(--ui-bg)] px-4 py-10 text-[var(--ui-text)]">
    <div
      class="mx-auto flex max-w-xl items-center justify-center rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-4 py-10 text-[13px]"
    >
      Redirecting to the workspace...
    </div>
  </main>
{:else if loadingBootstrapStatus}
  <main class="min-h-screen bg-[var(--ui-bg)] px-4 py-10 text-[var(--ui-text)]">
    <div
      class="mx-auto flex max-w-xl items-center justify-center rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-4 py-10 text-[13px]"
    >
      Checking workspace status...
    </div>
  </main>
{:else}
  <main class="min-h-screen bg-[var(--ui-bg)] px-4 py-10 text-[var(--ui-text)]">
    <div class="mx-auto flex max-w-5xl flex-col gap-4 lg:flex-row">
      <section
        class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] lg:w-[22rem]"
      >
        <div class="border-b border-[var(--ui-border)] px-4 py-3">
          <p
            class="text-[11px] font-medium uppercase tracking-wide text-[var(--ui-text-muted)]"
          >
            Sign in
          </p>
          <h1 class="mt-1 text-lg font-semibold text-[var(--ui-text)]">
            Sign in with a passkey
          </h1>
          <p class="mt-2 text-[13px] text-[var(--ui-text-muted)]">
            Use your existing passkey to authenticate. All writes are locked to
            your principal actor.
          </p>
        </div>

        <div class="space-y-3 px-4 py-3">
          <button
            class="cursor-pointer w-full rounded-md bg-indigo-600 px-3 py-2 text-[12px] font-medium text-white hover:bg-indigo-500"
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

          <p class="text-[12px] text-[var(--ui-text-muted)]">
            This uses discoverable WebAuthn login. No username step is required.
          </p>
        </div>
      </section>

      <section
        class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] lg:flex-1"
      >
        <div class="border-b border-[var(--ui-border)] px-4 py-3">
          <p
            class="text-[11px] font-medium uppercase tracking-wide text-[var(--ui-text-muted)]"
          >
            New to this workspace?
          </p>
          <h2 class="mt-1 text-[13px] font-semibold text-[var(--ui-text)]">
            Join with an invite token
          </h2>
        </div>

        <form
          class="space-y-4 px-4 py-4"
          onsubmit={(event) => {
            event.preventDefault();
            handleRegistration();
          }}
        >
          <div>
            <label
              class="block text-[12px] font-medium text-[var(--ui-text-muted)]"
              for="display-name"
            >
              Display name
            </label>
            <input
              bind:value={registrationName}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
              id="display-name"
              maxlength="120"
              placeholder="Alex Chen"
              type="text"
            />
          </div>

          <div>
            <label
              class="block text-[12px] font-medium text-[var(--ui-text-muted)]"
              for="invite-token"
            >
              {#if bootstrapAvailable}
                Bootstrap token
              {:else}
                Invite token
              {/if}
            </label>
            <input
              bind:value={registrationToken}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 font-mono text-[13px] text-[var(--ui-text)]"
              id="invite-token"
              placeholder={bootstrapAvailable
                ? "Paste the bootstrap token"
                : "Paste your invite token"}
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

          {#if bootstrapAvailable}
            <div
              class="rounded-md bg-amber-500/10 px-3 py-2 text-[12px] text-amber-400"
            >
              Bootstrap registration is available for the first principal and
              requires the workspace bootstrap token. After the first
              registration, new members require an invite token.
            </div>
          {:else}
            <div
              class="rounded-md bg-indigo-500/10 px-3 py-2 text-[12px] text-indigo-400"
            >
              This workspace requires an invite token to join. Contact your
              workspace administrator for an invitation.
            </div>
          {/if}

          <div class="flex flex-wrap gap-2">
            <button
              class="cursor-pointer rounded-md bg-indigo-600 px-3 py-2 text-[12px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
              disabled={loadingRegistration || !registrationToken.trim()}
              type="submit"
            >
              {loadingRegistration
                ? "Waiting for passkey..."
                : "Create passkey and join"}
            </button>
            {#if $devActorMode}
              <a
                class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[12px] font-medium text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)]"
                href={workspacePath(workspaceSlug)}
              >
                Back to actor mode
              </a>
            {/if}
          </div>
        </form>
      </section>
    </div>
  </main>
{/if}
