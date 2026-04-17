<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";

  import { goto } from "$app/navigation";

  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  const orgId = $derived(String($page.params.orgId ?? ""));

  let phase = $state("loading");
  let message = $state("");
  /** @type {any} */
  let summary = $state(null);

  async function readError(res) {
    try {
      const j = await res.json();
      return j?.error?.message || j?.error?.code || res.statusText;
    } catch {
      return res.statusText;
    }
  }

  async function load() {
    phase = "loading";
    message = "";
    const res = await hostedCpFetch(
      `organizations/${encodeURIComponent(orgId)}/usage-summary`,
    );
    if (res.status === 401) {
      await goto("/hosted/start");
      return;
    }
    if (!res.ok) {
      message = await readError(res);
      summary = null;
      phase = "ready";
      return;
    }
    const body = await res.json();
    summary = body.summary ?? null;
    phase = "ready";
  }

  $effect(() => {
    if (!browser || !orgId) {
      return;
    }
    load();
  });
</script>

<div class="hosted-page hosted-page--wide">
  <p class="hosted-crumb">
    <a href="/hosted/organizations">Organizations</a>
    <span aria-hidden="true"> / </span>
    <span>Usage</span>
  </p>
  <h1 class="hosted-title">Usage</h1>
  <p class="hosted-sub">
    Organization <code class="hosted-code">{orgId}</code>
  </p>

  {#if message}
    <p class="hosted-error">{message}</p>
  {/if}
  {#if phase === "loading"}
    <p class="hosted-muted">Loading…</p>
  {:else if summary}
    {@const plan = summary.plan ?? {}}
    {@const usage = summary.usage ?? {}}
    {@const quota = summary.quota ?? {}}
    <section class="hosted-card">
      <h2>Plan</h2>
      <p class="hosted-hint">{plan.display_name ?? "—"} · {plan.id ?? "—"}</p>
      <ul class="hosted-kv">
        <li>
          <span>Workspaces</span><span
            >{usage.workspace_count ?? 0} / {plan.workspace_limit ?? "—"}</span
          >
        </li>
        <li>
          <span>Artifacts (org)</span><span
            >{usage.artifact_count ?? 0} / {plan.artifact_capacity ??
              "—"}</span
          >
        </li>
        <li>
          <span>Artifacts / workspace (cap)</span><span
            >{plan.max_artifacts_per_workspace ?? "—"}</span
          >
        </li>
        <li>
          <span>Storage</span><span
            >{usage.storage_gb ?? 0} GB / {plan.included_storage_gb ?? "—"} GB</span
          >
        </li>
        <li>
          <span>Launches (this month)</span><span
            >{usage.monthly_launch_count ?? 0}</span
          >
        </li>
      </ul>
    </section>

    <section class="hosted-card">
      <h2>Remaining</h2>
      <ul class="hosted-kv">
        <li>
          <span>Workspaces</span><span>{quota.workspaces_remaining ?? 0}</span>
        </li>
        <li>
          <span>Artifacts (headroom)</span><span
            >{quota.artifacts_remaining ?? 0}</span
          >
        </li>
        <li>
          <span>Storage (GB)</span><span>{quota.storage_gb_remaining ?? 0}</span
          >
        </li>
      </ul>
    </section>

    <section class="hosted-card">
      <h2>Workspaces</h2>
      {#if !summary.workspaces || summary.workspaces.length === 0}
        <p class="hosted-muted">No workspaces in this organization yet.</p>
      {:else}
        <div class="hosted-table-wrap">
          <table class="hosted-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Slug</th>
                <th>Artifacts</th>
                <th>Storage (GB)</th>
                <th>Launches (mo)</th>
                <th>Last active</th>
              </tr>
            </thead>
            <tbody>
              {#each summary.workspaces as w (w.id)}
                <tr>
                  <td>{w.display_name || "—"}</td>
                  <td><code class="hosted-code">{w.slug}</code></td>
                  <td>{w.artifact_count ?? 0}</td>
                  <td>{w.storage_gb ?? 0}</td>
                  <td>{w.monthly_launch_count ?? 0}</td>
                  <td class="hosted-muted">{w.last_active_at ?? "—"}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </section>
  {/if}
</div>

<style>
  .hosted-crumb {
    font-size: 0.88rem;
    color: var(--ui-text-muted);
    margin: 0 0 0.75rem;
  }
  .hosted-crumb a {
    color: var(--ui-accent);
  }
  .hosted-code {
    font-size: 0.85em;
    word-break: break-all;
  }
  .hosted-kv {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .hosted-kv li {
    display: flex;
    justify-content: space-between;
    gap: 1rem;
    flex-wrap: wrap;
    font-size: 0.95rem;
  }
  .hosted-kv li span:first-child {
    color: var(--ui-text-muted);
  }
  .hosted-error {
    color: var(--ui-danger, #c62828);
    margin: 0 0 1rem;
  }
  .hosted-table-wrap {
    overflow-x: auto;
  }
  .hosted-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.9rem;
  }
  .hosted-table th,
  .hosted-table td {
    text-align: left;
    padding: 0.5rem 0.65rem;
    border-bottom: 1px solid var(--ui-border);
  }
  .hosted-table th {
    color: var(--ui-text-muted);
    font-weight: 600;
  }
</style>
