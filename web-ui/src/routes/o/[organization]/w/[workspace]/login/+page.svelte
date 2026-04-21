<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { onMount } from "svelte";
  import { get } from "svelte/store";

  import { authenticatedAgent, completeAuthSession } from "$lib/authSession";
  import { normalizeAppPath } from "$lib/pathUtils.js";
  import { coreClient } from "$lib/coreClient";
  import {
    createPasskeyCredential,
    getPasskeyAssertion,
  } from "$lib/passkeyBrowser";
  import Button from "$lib/components/Button.svelte";
  import { workspacePath } from "$lib/workspacePaths";
  import { devActorMode } from "$lib/workspaceContext";

  function isAlreadyAtWorkspaceHome(pathname, org, ws) {
    const dest = normalizeAppPath(workspacePath(org, ws));
    const cur = normalizeAppPath(pathname);
    return cur === dest;
  }

  /** Single client redirect when session exists; no-ops if already on workspace home. */
  function redirectToWorkspaceIfNeeded() {
    if (!browser) {
      return;
    }
    const org = organizationSlug;
    const ws = workspaceSlug;
    if (!org || !ws) {
      return;
    }
    if (!get(authenticatedAgent)?.agent_id) {
      return;
    }
    const pathname = get(page).url.pathname;
    if (isAlreadyAtWorkspaceHome(pathname, org, ws)) {
      return;
    }
    void goto(workspacePath(org, ws));
  }

  let registrationName = $state("");
  let registrationToken = $state("");
  let registrationError = $state("");
  let loginError = $state("");
  let loadingRegistration = $state(false);
  let loadingLogin = $state(false);
  let loadingWorkspaceStatus = $state(true);
  let devPasskeyBypassAvailable = $state(false);
  let devLoginUsername = $state("");
  let devLoginDisplayName = $state("");
  let devLoginError = $state("");
  let loadingDevLogin = $state(false);
  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);

  onMount(async () => {
    redirectToWorkspaceIfNeeded();

    const tokenParam = $page.url.searchParams.get("token");
    if (tokenParam) {
      registrationToken = tokenParam;
    }

    try {
      const status = await coreClient.bootstrapStatus();
      devPasskeyBypassAvailable = status.dev_passkey_bypass_available ?? false;
    } catch {
      devPasskeyBypassAvailable = false;
    } finally {
      loadingWorkspaceStatus = false;
    }
  });

  $effect(() => {
    if (!$authenticatedAgent?.agent_id) {
      return;
    }
    redirectToWorkspaceIfNeeded();
  });

  async function handleRegistration() {
    if (!registrationName.trim()) {
      registrationError = "Display name is required.";
      return;
    }

    if (!registrationToken.trim()) {
      registrationError = "An invite token is required for registration.";
      return;
    }

    loadingRegistration = true;
    registrationError = "";
    loginError = "";

    try {
      const registrationTokenValue = registrationToken.trim();
      const optionsPayload = {
        display_name: registrationName.trim(),
        invite_token: registrationTokenValue,
      };
      const options = await coreClient.passkeyRegisterOptions(optionsPayload);
      const credential = await createPasskeyCredential(options.options);
      const verifyPayload = {
        session_id: options.session_id,
        credential,
        invite_token: registrationTokenValue,
      };
      const result = await coreClient.passkeyRegisterVerify(verifyPayload);
      completeAuthSession(result.agent, workspaceSlug);
      await goto(workspacePath(organizationSlug, workspaceSlug));
    } catch (error) {
      registrationError =
        error instanceof Error ? error.message : "Passkey registration failed.";
    } finally {
      loadingRegistration = false;
    }
  }

  async function handleDevRegistration() {
    if (!registrationName.trim()) {
      registrationError = "Display name is required.";
      return;
    }

    if (!registrationToken.trim()) {
      registrationError = "An invite token is required for registration.";
      return;
    }

    loadingRegistration = true;
    registrationError = "";
    loginError = "";
    devLoginError = "";

    try {
      const registrationTokenValue = registrationToken.trim();
      const body = {
        display_name: registrationName.trim(),
        invite_token: registrationTokenValue,
      };
      const result = await coreClient.passkeyDevRegister(body);
      completeAuthSession(result.agent, workspaceSlug);
      await goto(workspacePath(organizationSlug, workspaceSlug));
    } catch (error) {
      registrationError =
        error instanceof Error ? error.message : "Dev registration failed.";
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
      await goto(workspacePath(organizationSlug, workspaceSlug));
    } catch (error) {
      loginError =
        error instanceof Error ? error.message : "Passkey sign-in failed.";
    } finally {
      loadingLogin = false;
    }
  }

  async function handleDevLogin() {
    const u = devLoginUsername.trim();
    const d = devLoginDisplayName.trim();
    if (u && d) {
      devLoginError = "Enter a principal username or a display name, not both.";
      return;
    }

    loadingDevLogin = true;
    devLoginError = "";
    loginError = "";
    registrationError = "";

    try {
      const result = await coreClient.passkeyDevLogin({
        ...(u ? { username: u } : {}),
        ...(d ? { display_name: d } : {}),
      });
      completeAuthSession(result.agent, workspaceSlug);
      await goto(workspacePath(organizationSlug, workspaceSlug));
    } catch (error) {
      devLoginError =
        error instanceof Error ? error.message : "Dev sign-in failed.";
    } finally {
      loadingDevLogin = false;
    }
  }
</script>

{#if $authenticatedAgent}
  <main class="min-h-screen bg-[var(--bg)] px-4 py-6 text-[var(--fg)]">
    <div
      class="mx-auto flex max-w-xl items-center justify-center rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-4 py-6 text-meta"
    >
      Redirecting to the workspace...
    </div>
  </main>
{:else if loadingWorkspaceStatus}
  <main class="min-h-screen bg-[var(--bg)] px-4 py-6 text-[var(--fg)]">
    <div
      class="mx-auto flex max-w-xl items-center justify-center rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-4 py-6 text-meta"
    >
      Checking workspace status...
    </div>
  </main>
{:else}
  <main class="min-h-screen bg-[var(--bg)] px-4 py-6 text-[var(--fg)]">
    <div class="mx-auto flex max-w-5xl flex-col gap-4 lg:flex-row">
      <section
        class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] lg:w-[22rem]"
      >
        <div class="border-b border-[var(--line)] px-4 py-3">
          <p
            class="text-micro font-medium uppercase tracking-wide text-[var(--fg-muted)]"
          >
            Sign in
          </p>
          <h1 class="mt-1 text-subtitle font-semibold text-[var(--fg)]">
            {#if devPasskeyBypassAvailable}
              Sign in
            {:else}
              Sign in with a passkey
            {/if}
          </h1>
          <p class="mt-2 text-meta text-[var(--fg-muted)]">
            {#if devPasskeyBypassAvailable}
              Dev sign-in is the default in local mode. Expand the section below
              if you need WebAuthn passkey authentication.
            {:else}
              Use your existing passkey to authenticate. All writes are locked
              to your principal actor.
            {/if}
          </p>
        </div>

        <div class="space-y-3 px-4 py-3">
          {#if devPasskeyBypassAvailable}
            <div class="space-y-3">
              <p
                class="text-micro font-medium uppercase tracking-wide text-[var(--fg-muted)]"
              >
                Local development
              </p>
              <p class="text-micro text-[var(--fg-muted)]">
                Sign in as a human principal without creating a browser passkey.
                After a fresh <span class="font-mono">make serve</span> seed, leave
                the fields empty (one passkey principal). With several, set principal
                username (Access) or exact display name.
              </p>
              <div>
                <label
                  class="block text-micro font-medium text-[var(--fg-muted)]"
                  for="dev-login-username"
                >
                  Principal username (optional)
                </label>
                <input
                  bind:value={devLoginUsername}
                  class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 font-mono text-meta text-[var(--fg)]"
                  id="dev-login-username"
                  placeholder="e.g. passkey.ops.lead.a1b2"
                  type="text"
                />
              </div>
              <div>
                <label
                  class="block text-micro font-medium text-[var(--fg-muted)]"
                  for="dev-login-display"
                >
                  Display name (optional)
                </label>
                <input
                  bind:value={devLoginDisplayName}
                  class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-meta text-[var(--fg)]"
                  id="dev-login-display"
                  placeholder="Exact match when username is unknown"
                  type="text"
                />
              </div>
              <Button
                variant="primary"
                class="w-full"
                disabled={loadingDevLogin}
                onclick={handleDevLogin}
              >
                {loadingDevLogin ? "Signing in..." : "Sign in (dev)"}
              </Button>
              {#if devLoginError}
                <div
                  class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
                >
                  {devLoginError}
                </div>
              {/if}
            </div>

            <details class="mt-4 border-t border-[var(--line)] pt-3">
              <summary
                class="cursor-pointer text-micro font-medium text-[var(--fg-muted)] hover:text-[var(--fg)]"
              >
                Sign in with passkey
              </summary>
              <div class="mt-3 space-y-3">
                <Button
                  variant="secondary"
                  class="w-full"
                  disabled={loadingLogin}
                  onclick={handleLogin}
                >
                  {loadingLogin
                    ? "Waiting for passkey..."
                    : "Sign in with existing passkey"}
                </Button>

                {#if loginError}
                  <div
                    class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
                  >
                    {loginError}
                  </div>
                {/if}

                <p class="text-micro text-[var(--fg-muted)]">
                  This uses discoverable WebAuthn login. No username step is
                  required.
                </p>
              </div>
            </details>
          {:else}
            <Button
              variant="primary"
              class="w-full"
              disabled={loadingLogin}
              onclick={handleLogin}
            >
              {loadingLogin
                ? "Waiting for passkey..."
                : "Sign in with existing passkey"}
            </Button>

            {#if loginError}
              <div
                class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
              >
                {loginError}
              </div>
            {/if}

            <p class="text-micro text-[var(--fg-muted)]">
              This uses discoverable WebAuthn login. No username step is
              required.
            </p>
          {/if}
        </div>
      </section>

      <section
        class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] lg:flex-1"
      >
        <div class="border-b border-[var(--line)] px-4 py-3">
          <p
            class="text-micro font-medium uppercase tracking-wide text-[var(--fg-muted)]"
          >
            New to this workspace?
          </p>
          <h2 class="mt-1 text-meta font-semibold text-[var(--fg)]">
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
              class="block text-micro font-medium text-[var(--fg-muted)]"
              for="display-name"
            >
              Display name
            </label>
            <input
              bind:value={registrationName}
              class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-meta text-[var(--fg)]"
              id="display-name"
              maxlength="120"
              placeholder="Alex Chen"
              type="text"
            />
          </div>

          <div>
            <label
              class="block text-micro font-medium text-[var(--fg-muted)]"
              for="invite-token"
            >
              Invite token
            </label>
            <input
              bind:value={registrationToken}
              class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 font-mono text-meta text-[var(--fg)]"
              id="invite-token"
              placeholder="Paste your invite token"
              type="text"
            />
          </div>

          {#if registrationError}
            <div
              class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
            >
              {registrationError}
            </div>
          {/if}

          <div
            class="rounded-md bg-accent-soft px-3 py-2 text-micro text-accent-text"
          >
            This workspace requires an invite token to join. Contact your
            workspace administrator for an invitation.
          </div>

          <div class="flex flex-wrap gap-2">
            <Button
              type="submit"
              variant="primary"
              disabled={loadingRegistration || !registrationToken.trim()}
            >
              {loadingRegistration
                ? "Waiting for passkey..."
                : "Create passkey and join"}
            </Button>
            {#if devPasskeyBypassAvailable}
              <Button
                variant="secondary"
                disabled={loadingRegistration || !registrationToken.trim()}
                onclick={(e) => {
                  e.preventDefault();
                  handleDevRegistration();
                }}
              >
                {loadingRegistration
                  ? "Working..."
                  : "Join without passkey (dev)"}
              </Button>
            {/if}
            {#if $devActorMode}
              <Button
                variant="secondary"
                href={workspacePath(organizationSlug, workspaceSlug)}
              >
                Back to actor mode
              </Button>
            {/if}
          </div>
        </form>
      </section>
    </div>
  </main>
{/if}
