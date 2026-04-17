<script>
  import { onMount } from "svelte";

  import { goto } from "$app/navigation";

  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  let phase = $state("loading");
  let message = $state("");
  /** @type {any[]} */
  let organizations = $state([]);

  async function readError(res) {
    try {
      const j = await res.json();
      return j?.error?.message || j?.error?.code || res.statusText;
    } catch {
      return res.statusText;
    }
  }

  onMount(async () => {
    const res = await hostedCpFetch("organizations?limit=200");
    if (res.status === 401) {
      await goto("/hosted/start");
      return;
    }
    if (!res.ok) {
      message = await readError(res);
      phase = "ready";
      return;
    }
    const body = await res.json();
    organizations = body.organizations ?? [];
    const params = new URLSearchParams(window.location.search);
    if (params.get("billing_error") === "1") {
      message =
        "We could not confirm your checkout session. Open Billing from your organization when ready.";
    }
    phase = "ready";
  });
</script>

<div class="hosted-page hosted-page--wide">
  <h1 class="hosted-title">Organizations</h1>
  <p class="hosted-sub">
    Choose an organization to view billing and usage. The URL is the source of
    truth for which org you are managing.
  </p>
  {#if message}
    <p class="hosted-callout" role="status">{message}</p>
  {/if}
  {#if phase === "loading"}
    <p class="hosted-muted">Loading…</p>
  {:else if organizations.length === 0}
    <p class="hosted-muted">
      No organizations yet. Continue from
      <a href="/hosted/onboarding">Dashboard</a>
      to create one.
    </p>
  {:else}
    <ul class="hosted-org-list">
      {#each organizations as org (org.id)}
        <li class="hosted-org-row">
          <div class="hosted-org-row-main">
            <span class="hosted-org-name">{org.display_name || org.slug}</span>
            <span class="hosted-muted hosted-org-id">{org.id}</span>
          </div>
          <div class="hosted-org-row-actions">
            <a
              class="hosted-btn hosted-btn--secondary"
              href="/hosted/organizations/{encodeURIComponent(org.id)}/billing"
              >Billing</a
            >
            <a
              class="hosted-btn hosted-btn--secondary"
              href="/hosted/organizations/{encodeURIComponent(org.id)}/usage"
              >Usage</a
            >
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .hosted-org-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }
  .hosted-org-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    padding: 0.85rem 1rem;
    border: 1px solid var(--ui-border);
    border-radius: var(--ui-radius-md);
    background: var(--ui-panel);
  }
  .hosted-org-row-main {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    min-width: 12rem;
  }
  .hosted-org-name {
    font-weight: 600;
  }
  .hosted-org-id {
    font-size: 0.8rem;
  }
  .hosted-org-row-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }
  .hosted-callout {
    padding: 0.75rem 1rem;
    border-radius: var(--ui-radius-md);
    background: color-mix(in srgb, var(--ui-accent) 12%, transparent);
    border: 1px solid var(--ui-border);
    margin: 0 0 1rem;
  }
  .hosted-muted {
    color: var(--ui-text-muted);
    font-size: 0.95rem;
  }
</style>
