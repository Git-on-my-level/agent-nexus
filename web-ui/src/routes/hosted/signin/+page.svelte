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
  import { loadHostedSession } from "$lib/hosted/session.js";

  let email = $state("");
  let busy = $state(false);
  let quickAuthBusy = $state(false);
  let quickAuthOptions = $state(null);
  let message = $state("");
  let showDevShortcut = $state(false);
  const continuationQuery = $derived($page.url.search ?? "");
  const quickAuthLabel = $derived.by(() => {
    if (!quickAuthOptions?.enabled) return "";
    const emailHint = String(quickAuthOptions.default_email ?? "").trim();
    if (emailHint) return `Sign in as ${emailHint}`;
    return "Use local dev account";
  });

  onMount(async () => {
    const launchFlow = await continueLaunchFlowIfPresent();
    if (launchFlow.kind === "redirect") return;
    try {
      const res = await hostedCpFetch("account/dev/session/options");
      if (!res.ok) return;
      const body = await res.json();
      const options = body?.dev_quick_auth;
      if (options?.enabled) quickAuthOptions = options;
    } catch {
      // Optional helper.
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
      message = "Enter the email you signed up with.";
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
      await loadHostedSession();
      const continuationHandled = await continueLaunchFlowIfPresent();
      if (continuationHandled.kind === "noop") {
        await navigateNext();
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
      await loadHostedSession();
      const continuationHandled = await continueLaunchFlowIfPresent();
      if (continuationHandled.kind === "noop") {
        await navigateNext();
      }
    } catch (e) {
      message = e instanceof Error ? e.message : "Quick sign-in failed.";
    } finally {
      quickAuthBusy = false;
    }
  }

  async function navigateNext() {
    const next = String($page.url.searchParams.get("next") ?? "").trim();
    await goto(next || "/hosted/dashboard");
  }

  async function continueLaunchFlowIfPresent() {
    const launchParams = readHostedLaunchParams($page.url.searchParams);
    if (!launchParams.hasContinuation) return { kind: "noop" };

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

<svelte:head>
  <title>Sign in — ANX</title>
</svelte:head>

<div class="mx-auto max-w-md py-8">
  <div class="rounded-md border border-gray-200 bg-gray-100 px-6 py-6">
    <h1 class="text-lg font-semibold text-gray-900">Welcome back</h1>
    <p class="mt-1.5 text-[12px] leading-relaxed text-gray-500">
      Enter the email you signed up with. Your browser will prompt you for the
      passkey on this device.
    </p>

    <form
      class="mt-5 space-y-3"
      onsubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <label class="block text-[12px] font-medium text-gray-600">
        Email
        <input
          type="email"
          autocomplete="username webauthn"
          bind:value={email}
          disabled={busy}
          required
          placeholder="you@company.com"
          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-1.5 text-[13px] text-gray-900 placeholder:text-[var(--ui-text-subtle)]"
        />
      </label>

      {#if message}
        <p
          role="alert"
          class="rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
        >
          {message}
        </p>
      {/if}

      <button
        type="submit"
        disabled={busy}
        class="w-full rounded-md bg-indigo-600 px-3 py-2 text-[13px] font-medium text-white transition-colors hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-60"
      >
        {busy ? "Signing in…" : "Continue with passkey"}
      </button>
    </form>

    {#if quickAuthOptions?.enabled}
      <div class="mt-5 border-t border-gray-200 pt-3">
        <button
          type="button"
          class="text-[11px] font-medium text-gray-500 hover:text-gray-800"
          onclick={() => (showDevShortcut = !showDevShortcut)}
        >
          {showDevShortcut ? "Hide" : "Show"} local dev shortcut
        </button>
        {#if showDevShortcut}
          <p class="mt-2 text-[11px] text-gray-500">
            Skips the passkey for local resets. Disabled in production.
          </p>
          <button
            type="button"
            onclick={quickSignIn}
            disabled={busy || quickAuthBusy}
            class="mt-2 w-full rounded-md border border-gray-200 bg-gray-100 px-3 py-1.5 text-[12px] font-medium text-gray-600 hover:bg-gray-200 disabled:opacity-60"
          >
            {quickAuthBusy ? "Signing in…" : quickAuthLabel}
          </button>
        {/if}
      </div>
    {/if}
  </div>

  <p class="mt-4 text-center text-[12px] text-gray-500">
    New to ANX?
    <a
      class="text-indigo-400 underline underline-offset-2 hover:text-indigo-300"
      href={`/hosted/signup${continuationQuery}`}>Create an account</a
    >
  </p>
</div>
