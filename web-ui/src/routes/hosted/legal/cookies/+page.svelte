<script>
  import LegalPageShell from "$lib/hosted/LegalPageShell.svelte";
</script>

<svelte:head>
  <title>Cookies — Agent Nexus (beta)</title>
  <meta
    name="description"
    content="Cookie and local storage notice for the hosted Agent Nexus beta."
  />
</svelte:head>

<LegalPageShell title="Cookies and local storage (public beta)">
  <h2>Summary</h2>
  <p>
    In the default public beta configuration, we use
    <strong>strictly necessary</strong> cookies and similar storage to keep you
    signed in, protect sessions, and complete login flows. We do
    <strong>not</strong> rely on third-party advertising or analytics cookies
    in the default product configuration described in this repository.
  </p>

  <h2>What the app may set</h2>
  <ul>
    <li>
      <strong>Hosted session</strong> — Server-set cookies (including HttpOnly
      where applicable) for OAuth completion and control-plane API access.
      Required for the hosted product.
    </li>
    <li>
      <strong>Client storage</strong> — Short-lived data in
      <span class="font-mono text-[11px]">sessionStorage</span> (or similar) for
      login continuation. Not used for off-site ad tracking.
    </li>
    <li>
      <strong>Self-hosted OSS builds</strong> — Different names and behavior
      apply when you run the open-source stack yourself; this notice targets
      the <strong>hosted</strong> beta.
    </li>
  </ul>

  <h2>Third-party sign-in</h2>
  <p>
    When you use <strong>Google</strong> or <strong>GitHub</strong>, those
    services may set their own cookies on their domains. We do not control
    those.
  </p>

  <h2>Optional analytics</h2>
  <p>
    If we add product analytics later, we will update this notice and, where
    required, the in-app experience before enabling non-essential tracking.
  </p>

  <h2>Contact</h2>
  <p>
    <a href="mailto:support@runoar.com">support@runoar.com</a>
  </p>
  <p class="text-micro text-fg-subtle">
    Canonical copy:
    <code class="font-mono text-[11px]">docs/legal/cookies-notice.md</code>
  </p>
</LegalPageShell>
