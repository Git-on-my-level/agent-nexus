<svelte:head>
  <title>Welcome — OAR hosted</title>
</svelte:head>

<main class="hosted-page hosted-page--hero">
  <h1 class="hosted-title hosted-title--hero">Welcome</h1>
  <p class="hosted-lede">
    Create an account with a passkey, then set up your organization and first
    workspace. Your workspace core starts after provisioning completes.
  </p>

  <div class="hosted-actions">
    <a class="hosted-btn hosted-btn--primary" href="/hosted/signup"
      >Create account</a
    >
    <a class="hosted-btn hosted-btn--secondary" href="/hosted/signin">Sign in</a
    >
  </div>

  <p class="hosted-dash">
    Already set up?
    <a class="hosted-link" href="/hosted/onboarding">Open dashboard</a>
  </p>

  <p class="hosted-dev-hint">
    <a class="hosted-link" href="/hosted/dev">Developer notes</a>
    — environment variables and optional shortcuts.
  </p>
</main>
