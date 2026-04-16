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

  let email = $state("");
  let displayName = $state("");
  let inviteToken = $state("");
  let busy = $state(false);
  let message = $state("");
  let continuationQuery = $derived($page.url.search ?? "");

  onMount(() => {
    const inv = $page.url.searchParams.get("invite");
    if (inv) {
      inviteToken = inv;
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
      message = "Email and display name are required.";
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
      const continuationHandled = await continueLaunchFlowIfPresent();
      if (!continuationHandled) {
        await goto("/hosted/onboarding");
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

<div class="hosted-page hosted-page--narrow">
  <h1 class="hosted-title">Create your account</h1>
  <p class="hosted-sub">
    Use your email and a passkey (Face ID, Touch ID, Windows Hello, or a
    security key). You’ll create an organization next.
  </p>

  <form
    class="hosted-form"
    onsubmit={(e) => {
      e.preventDefault();
      submit();
    }}
  >
    <label class="hosted-field">
      Email
      <input
        class="hosted-input"
        type="email"
        autocomplete="email"
        bind:value={email}
        disabled={busy}
        required
      />
    </label>
    <label class="hosted-field">
      Display name
      <input
        class="hosted-input"
        type="text"
        autocomplete="name"
        bind:value={displayName}
        disabled={busy}
        required
      />
    </label>
    <label class="hosted-field">
      Invite token (optional)
      <input
        class="hosted-input"
        type="text"
        bind:value={inviteToken}
        disabled={busy}
        placeholder="If you were invited to an organization"
      />
    </label>
    {#if message}
      <p class="hosted-error" role="alert">{message}</p>
    {/if}
    <button class="hosted-btn-submit" type="submit" disabled={busy}>
      {busy ? "Continuing…" : "Continue with passkey"}
    </button>
  </form>

  <p class="hosted-foot">
    Already have an account?
    <a class="hosted-link" href={`/hosted/signin${continuationQuery}`}
      >Sign in</a
    >
  </p>
</div>
