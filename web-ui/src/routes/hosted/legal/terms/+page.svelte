<script>
  import LegalPageShell from "$lib/hosted/LegalPageShell.svelte";
</script>

<svelte:head>
  <title>Terms — Agent Nexus (beta)</title>
  <meta
    name="description"
    content="Public beta terms of service for the hosted Agent Nexus service."
  />
</svelte:head>

<LegalPageShell title="Agent Nexus — public beta terms">
  <p>
    This public beta is offered by the Agent Nexus / ANX project team
    <strong> (“we,” “us”) </strong>. By using the hosted beta, you agree to
    these terms. If you do not agree, do not use the service.
  </p>

  <h2>1. Beta status</h2>
  <p>
    The service is experimental. It may change, break, or be unavailable. We do <strong
      >not</strong
    >
    guarantee uptime, data durability, or fitness for any purpose.
    <strong>Use at your own risk</strong> for work that is not business-critical or
    legally sensitive.
  </p>

  <h2>2. Acceptable use</h2>
  <p>
    You agree not to: abuse, attack, or overload the service; attempt
    unauthorized access; use the service to violate law or others’ rights; or
    send spam or malware. We may suspend or end access to protect the service or
    other users.
  </p>

  <h2>3. Accounts and orgs</h2>
  <p>
    You are responsible for activity under your account. Keep OAuth credentials
    and sessions secure. We may change signup limits, org limits, and quotas to
    protect the platform.
  </p>

  <h2>4. Third parties</h2>
  <p>
    The service may integrate sign-in and infrastructure from third parties
    (e.g. Google, GitHub, cloud hosts). Their terms apply to their parts of the
    experience.
  </p>

  <h2>5. Liability</h2>
  <p>
    <strong>To the maximum extent allowed by law, we are not liable</strong>
    for lost data, lost profits, indirect or consequential damages, or any damages
    arising from your use of the beta. Some jurisdictions do not allow certain limitations;
    in those cases our liability is limited to the fullest extent permitted.
  </p>

  <h2>6. Changes</h2>
  <p>
    We may update these terms for the beta. We will use reasonable means to show
    the current version (e.g. linked from the app). Continued use after changes
    means you accept the updated terms.
  </p>

  <h2>7. Contact</h2>
  <p>
    Questions: <a href="mailto:support@agentnexus.com">support@agentnexus.com</a
    > (or the support link shown in the app).
  </p>
  <p class="text-micro text-fg-subtle">
    Canonical copy for operators:
    <code class="font-mono text-[11px]">docs/legal/beta-terms.md</code> in the agent-nexus-saas
    monorepo.
  </p>
</LegalPageShell>
