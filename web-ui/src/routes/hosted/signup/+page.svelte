<script>
  import { onMount } from "svelte";

  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

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
  let busy = $state(false);
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
    busy = true;
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
        await goto("/hosted/dashboard");
      }
    } catch (e) {
      message = e instanceof Error ? e.message : "Passkey registration failed.";
    } finally {
      busy = false;
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
    <h1 class="text-lg font-semibold text-fg">Create your account</h1>
    <p class="mt-1.5 text-[12px] leading-relaxed text-fg-subtle">
      Sign up with a passkey — Face ID, Touch ID, Windows Hello, or a security
      key. No password to remember, no email round-trip.
    </p>

    <form
      class="mt-5 space-y-3"
      onsubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <label class="block text-[12px] font-medium text-fg-muted">
        Work email
        <input
          type="email"
          autocomplete="email"
          bind:value={email}
          disabled={busy}
          required
          placeholder="you@company.com"
          class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 text-[13px] text-fg placeholder:text-[var(--fg-subtle)]"
        />
      </label>

      <label class="block text-[12px] font-medium text-fg-muted">
        Your name
        <input
          type="text"
          autocomplete="name"
          bind:value={displayName}
          disabled={busy}
          required
          placeholder="Jane Doe"
          class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 text-[13px] text-fg placeholder:text-[var(--fg-subtle)]"
        />
      </label>

      {#if showInviteField}
        <label class="block text-[12px] font-medium text-fg-muted">
          Invite token
          <input
            type="text"
            bind:value={inviteToken}
            disabled={busy}
            placeholder="From your invite email"
            class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 text-[13px] text-fg placeholder:text-[var(--fg-subtle)]"
          />
        </label>
      {:else}
        <button
          type="button"
          class="text-[11px] font-medium text-accent-text hover:text-accent-text"
          onclick={() => (showInviteField = true)}
        >
          + I have an invite token
        </button>
      {/if}

      {#if message}
        <p
          role="alert"
          class="rounded-md bg-danger-soft px-3 py-2 text-[12px] text-danger-text"
        >
          {message}
        </p>
      {/if}

      <button
        type="submit"
        disabled={busy}
        class="w-full rounded-md bg-accent px-3 py-2 text-[13px] font-medium text-white transition-colors hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-60"
      >
        {busy ? "Setting up your passkey…" : "Continue with passkey"}
      </button>

      <p class="text-[11px] text-fg-subtle">
        By continuing you agree to the ANX terms of service.
      </p>
    </form>
  </div>

  <p class="mt-4 text-center text-[12px] text-fg-subtle">
    Already have an account?
    <a
      class="text-accent-text underline underline-offset-2 hover:text-accent-text"
      href={`/hosted/signin${continuationQuery}`}>Sign in</a
    >
  </p>
</div>
