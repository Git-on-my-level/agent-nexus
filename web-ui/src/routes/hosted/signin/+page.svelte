<script>
  import { onMount } from "svelte";

  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import {
    normalizeHostedLaunchFinishURL,
    readHostedLaunchParams,
  } from "$lib/hosted/launchFlow.js";
  import { getPasskeyAssertion } from "$lib/passkeyBrowser";
  import {
    hostedCpFetch,
    persistHostedCpAccessToken,
  } from "$lib/hosted/cpFetch.js";

  let email = $state("");
  let busy = $state(false);
  let quickAuthBusy = $state(false);
  let quickAuthOptions = $state(null);
  let message = $state("");
  let continuationQuery = $derived($page.url.search ?? "");
  let quickAuthLabel = $derived.by(() => {
    if (!quickAuthOptions?.enabled) {
      return "";
    }
    const emailHint = String(quickAuthOptions.default_email ?? "").trim();
    if (emailHint) {
      return `Use ${emailHint}`;
    }
    return "Use local dev account";
  });

  onMount(async () => {
    const launchFlow = await continueLaunchFlowIfPresent();
    if (launchFlow.kind === "redirect") {
      return;
    }
    try {
      const res = await hostedCpFetch("account/dev/session/options");
      if (!res.ok) {
        return;
      }
      const body = await res.json();
      const options = body?.dev_quick_auth;
      if (options?.enabled) {
        quickAuthOptions = options;
      }
    } catch {
      // Ignore: this is an optional local-dev helper.
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
    if (!email.trim()) {
      message = "Email is required.";
      return;
    }
    busy = true;
    try {
      const start = await hostedCpFetch("account/sessions/start", {
        method: "POST",
        body: JSON.stringify({ email: email.trim() }),
      });
      if (!start.ok) {
        message = await readError(start);
        return;
      }
      const startBody = await start.json();
      const sessionId = String(startBody.session_id ?? "").trim();
      const options = startBody.public_key_options;
      if (!sessionId || !options) {
        message = "Unexpected response from control plane.";
        return;
      }
      const credential = await getPasskeyAssertion(options);
      const finish = await hostedCpFetch("account/sessions/finish", {
        method: "POST",
        body: JSON.stringify({
          session_id: sessionId,
          credential,
        }),
      });
      if (!finish.ok) {
        message = await readError(finish);
        return;
      }
      const finishBody = await finish.json();
      const token = String(finishBody.session?.access_token ?? "").trim();
      if (!token) {
        message = "Signed in but no session token was returned.";
        return;
      }
      persistHostedCpAccessToken(token);
      const continuationHandled = await continueLaunchFlowIfPresent();
      if (continuationHandled.kind === "noop") {
        await goto("/hosted/onboarding");
      }
    } catch (e) {
      message = e instanceof Error ? e.message : "Sign-in failed.";
    } finally {
      busy = false;
    }
  }

  async function quickSignIn() {
    message = "";
    quickAuthBusy = true;
    try {
      const res = await hostedCpFetch("account/dev/session", {
        method: "POST",
        body: "{}",
      });
      if (!res.ok) {
        message = await readError(res);
        return;
      }
      const body = await res.json();
      const token = String(body.session?.access_token ?? "").trim();
      if (!token) {
        message = "Quick sign-in succeeded but no session token was returned.";
        return;
      }
      persistHostedCpAccessToken(token);
      const continuationHandled = await continueLaunchFlowIfPresent();
      if (continuationHandled.kind === "noop") {
        await goto("/hosted/onboarding");
      }
    } catch (e) {
      message = e instanceof Error ? e.message : "Quick sign-in failed.";
    } finally {
      quickAuthBusy = false;
    }
  }

  async function continueLaunchFlowIfPresent() {
    const launchParams = readHostedLaunchParams($page.url.searchParams);
    if (!launchParams.hasContinuation) {
      return { kind: "noop" };
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
      return { kind: "error" };
    }

    const launchPayload = await launchResponse.json();
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

<div class="hosted-page hosted-page--narrow">
  <h1 class="hosted-title">Sign in</h1>
  <p class="hosted-sub">Use the passkey you registered with for this email.</p>

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
        autocomplete="username webauthn"
        bind:value={email}
        disabled={busy}
        required
      />
    </label>
    {#if message}
      <p class="hosted-error" role="alert">{message}</p>
    {/if}
    <button class="hosted-btn-submit" type="submit" disabled={busy}>
      {busy ? "Signing in…" : "Sign in with passkey"}
    </button>
  </form>

  {#if quickAuthOptions?.enabled}
    <section class="hosted-card">
      <h2>Local dev shortcut</h2>
      <p class="hosted-hint">
        Skip passkey for local resets. This endpoint is dev-only and disabled
        unless explicitly configured on the control plane.
      </p>
      <button
        class="hosted-btn hosted-btn--secondary"
        type="button"
        onclick={quickSignIn}
        disabled={busy || quickAuthBusy}
      >
        {quickAuthBusy ? "Signing in…" : quickAuthLabel}
      </button>
    </section>
  {/if}

  <p class="hosted-foot">
    New here?
    <a class="hosted-link" href={`/hosted/signup${continuationQuery}`}
      >Create an account</a
    >
  </p>
</div>
