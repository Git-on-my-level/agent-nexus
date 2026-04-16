<script>
  import { goto } from "$app/navigation";

  import { getPasskeyAssertion } from "$lib/passkeyBrowser";
  import {
    hostedCpFetch,
    persistHostedCpAccessToken,
  } from "$lib/hosted/cpFetch.js";

  let email = $state("");
  let busy = $state(false);
  let message = $state("");

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
      await goto("/hosted/onboarding");
    } catch (e) {
      message = e instanceof Error ? e.message : "Sign-in failed.";
    } finally {
      busy = false;
    }
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

  <p class="hosted-foot">
    New here?
    <a class="hosted-link" href="/hosted/signup">Create an account</a>
  </p>
</div>
