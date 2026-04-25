<script>
  import LegalPageShell from "$lib/hosted/LegalPageShell.svelte";
</script>

<svelte:head>
  <title>Privacy — Agent Nexus (beta)</title>
  <meta
    name="description"
    content="Public beta privacy notice for the hosted Agent Nexus service."
  />
</svelte:head>

<LegalPageShell title="Agent Nexus — public beta privacy">
  <p>
    This notice describes how we handle information in the
    <strong>public beta</strong> of the hosted Agent Nexus (ANX) service. It is plain
    language, not a full privacy policy or DPA. Add formal documents before a paid
    or enterprise launch and have them reviewed by counsel.
  </p>

  <h2>What we collect</h2>
  <ul>
    <li>
      <strong>Account and identity:</strong> When you sign in with Google or GitHub
      OAuth, we receive identifiers and email (and name when the provider shares it)
      to create and secure your account.
    </li>
    <li>
      <strong>Product data:</strong> Your organizations, workspace metadata, and content
      you store in the product are processed to run the service.
    </li>
    <li>
      <strong>Operations:</strong> We may log technical data (e.g. IP address, request
      metadata, error logs) to operate, secure, and improve the service.
    </li>
    <li>
      <strong>Support:</strong> If you contact us, we keep that communication to help
      you.
    </li>
  </ul>

  <h2>Why we use it</h2>
  <p>
    To provide, secure, and improve the service; to communicate about the beta;
    to enforce abuse limits; and to meet legal obligations where applicable.
  </p>

  <h2>Deletion and export (beta)</h2>
  <p>
    Retention and automated export are <strong>not</strong> fully built out in
    this beta. To <strong>request account or data deletion</strong>, or to
    discuss export, email
    <a href="mailto:support@runoar.com?subject=Data%20deletion%20request"
      >support@runoar.com</a
    >
    with subject
    <span class="font-mono text-[11px]">Data deletion request</span>
    and the email you use to sign in. We may need to verify ownership. Response times
    are best-effort during beta.
  </p>
  <p class="text-micro text-fg-subtle">
    Intended long-term data lifecycle and gaps are tracked in
    <span class="font-mono">TODOS.md</span> and
    <span class="font-mono">LAUNCH_CHECKLIST.md</span> in the monorepo.
  </p>

  <h2>Where data is processed</h2>
  <p>
    Data may be processed in the region(s) where the beta is deployed, including
    subprocessors used to host, authenticate, or operate the product. A full
    subprocessor list belongs in a later, formal notice.
  </p>

  <h2>Your choices</h2>
  <ul>
    <li>
      <strong>OAuth:</strong> You can use provider settings to view or revoke app
      access; that may limit sign-in here.
    </li>
    <li>
      <strong>Cookies and local storage:</strong> See
      <a href="/hosted/legal/cookies">Cookies notice</a>.
    </li>
    <li>
      <strong>Rights:</strong> If applicable law gives you access, correction,
      or deletion rights, contact
      <a href="mailto:support@runoar.com">support@runoar.com</a>.
    </li>
  </ul>

  <h2>Children</h2>
  <p>
    The service is not directed at children under the age required in your
    country to consent. Do not use the beta if you cannot meet that requirement.
  </p>

  <h2>Changes</h2>
  <p>
    We may update this notice. The “last updated” date and the in-app link show
    the current version.
  </p>

  <h2>Contact</h2>
  <p>
    <a href="mailto:support@runoar.com">support@runoar.com</a> (or the support address
    shown in the product).
  </p>
  <p class="text-micro text-fg-subtle">
    Canonical copy:
    <code class="font-mono text-[11px]">docs/legal/beta-privacy.md</code>
  </p>
</LegalPageShell>
