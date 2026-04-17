<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";

  import { goto } from "$app/navigation";
  import { untrack } from "svelte";

  import {
    billingPollScheduleDelays,
    billingSnapshotExpired,
    billingSnapshotMatchesSummary,
    clearBillingSnapshot,
    readBillingSnapshot,
    stripeSubscriptionManagedClient,
    writeBillingSnapshot,
  } from "$lib/hosted/billingActivation.js";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";

  const orgId = $derived(String($page.params.orgId ?? ""));

  /** @type {'loading'|'manager'|'member'|'error'} */
  let role = $state("loading");
  let message = $state("");
  /** @type {any} */
  let summary = $state(null);
  /** @type {any[]} */
  let managers = $state([]);
  let activatingBanner = $state(false);
  let activationTimedOut = $state(false);
  let pollStop = $state(() => {});

  async function readError(res) {
    try {
      const j = await res.json();
      return j?.error?.message || j?.error?.code || res.statusText;
    } catch {
      return res.statusText;
    }
  }

  /** @param {any} billingAccount */
  function subscriptionStatusLabel(billingAccount) {
    const st = String(billingAccount?.stripe_subscription_status ?? "")
      .trim()
      .toLowerCase();
    const map = {
      active: "Active",
      trialing: "Trialing",
      past_due: "Past due",
      canceled: "Canceled",
      unpaid: "Unpaid",
      paused: "Paused",
      incomplete: "Incomplete",
      incomplete_expired: "Incomplete (expired)",
      not_started: "Not started",
      free: "Free",
      "": "—",
    };
    return (map[st] ?? st) || "—";
  }

  function stripActivatingQuery() {
    if (!browser) {
      return;
    }
    const u = new URL(window.location.href);
    if (!u.searchParams.has("activating")) {
      return;
    }
    u.searchParams.delete("activating");
    window.history.replaceState({}, "", `${u.pathname}${u.search}${u.hash}`);
  }

  /** Use the live URL, not $page, so async activation polling does not subscribe $effect to the page store. */
  function isActivatingCheckoutFromUrl() {
    if (!browser) {
      return false;
    }
    try {
      return (
        new URL(window.location.href).searchParams.get("activating") === "1"
      );
    } catch {
      return false;
    }
  }

  async function fetchBillingSummary() {
    const res = await hostedCpFetch(
      `organizations/${encodeURIComponent(orgId)}/billing`,
    );
    if (res.status === 401) {
      await goto("/hosted/start");
      return null;
    }
    if (res.status === 403) {
      return { forbidden: true };
    }
    if (!res.ok) {
      return { error: await readError(res) };
    }
    const body = await res.json();
    return { summary: body.summary ?? null };
  }

  async function loadManagers() {
    const res = await hostedCpFetch(
      `organizations/${encodeURIComponent(orgId)}/memberships?limit=200`,
    );
    if (!res.ok) {
      return;
    }
    const body = await res.json();
    const items = body.memberships ?? [];
    managers = items.filter(
      (m) =>
        String(m.status ?? "") === "active" &&
        (m.role === "owner" || m.role === "admin"),
    );
  }

  async function load() {
    role = "loading";
    message = "";
    summary = null;
    activationTimedOut = false;
    activatingBanner = false;
    pollStop();

    const got = await fetchBillingSummary();
    if (!got) {
      return;
    }
    if (got.forbidden) {
      role = "member";
      await loadManagers();
      return;
    }
    if (got.error) {
      role = "error";
      message = got.error;
      return;
    }
    summary = got.summary;
    role = "manager";
    await maybeRunActivationPoll(got.summary);
  }

  /**
   * Post-checkout: poll until plan/subscription changes vs sessionStorage snapshot.
   * @param {any} initialSummary
   */
  async function maybeRunActivationPoll(initialSummary) {
    if (!browser) {
      return;
    }
    if (!isActivatingCheckoutFromUrl()) {
      return;
    }
    const snap = readBillingSnapshot(orgId);
    if (!snap || billingSnapshotExpired(snap)) {
      stripActivatingQuery();
      return;
    }
    // Snapshot captured pre-redirect; if API already reflects the new state, stop immediately.
    if (billingSnapshotMatchesSummary(snap, initialSummary)) {
      clearBillingSnapshot(orgId);
      activatingBanner = false;
      stripActivatingQuery();
      return;
    }

    activatingBanner = true;
    let cancelled = false;
    pollStop = () => {
      cancelled = true;
    };

    const delays = billingPollScheduleDelays();
    for (let i = 0; i < delays.length && !cancelled; i++) {
      await new Promise((r) => setTimeout(r, delays[i]));
      if (
        typeof document !== "undefined" &&
        document.visibilityState === "hidden"
      ) {
        cancelled = true;
        activatingBanner = false;
        break;
      }
      const next = await fetchBillingSummary();
      if (!next || next.forbidden || next.error || !next.summary) {
        break;
      }
      summary = next.summary;
      if (billingSnapshotMatchesSummary(snap, next.summary)) {
        clearBillingSnapshot(orgId);
        activatingBanner = false;
        stripActivatingQuery();
        return;
      }
    }

    if (!cancelled) {
      activationTimedOut = true;
    }
  }

  async function refreshAfterTimeout() {
    activationTimedOut = false;
    const got = await fetchBillingSummary();
    if (got?.summary) {
      summary = got.summary;
    }
  }

  $effect(() => {
    if (!browser || !orgId) {
      return;
    }
    // Avoid subscribing this effect to reactive state read inside async load() (e.g. $page), which caused a fetch loop.
    untrack(() => {
      void load();
    });
  });

  async function checkoutPlan(planTier) {
    message = "";
    if (!summary) {
      return;
    }
    writeBillingSnapshot(orgId, summary);
    const res = await hostedCpFetch(
      `organizations/${encodeURIComponent(orgId)}/billing/checkout-session`,
      {
        method: "POST",
        body: JSON.stringify({ plan_tier: planTier }),
      },
    );
    if (res.status === 401) {
      await goto("/hosted/start");
      return;
    }
    const body = await res.json().catch(() => ({}));
    const session = body.session ?? {};
    if (session.status === "configuration_required") {
      message =
        session.note ||
        "Billing is not fully configured (Stripe env). Check operator docs.";
      summary = {
        ...summary,
        configuration: {
          ...(summary.configuration ?? {}),
          missing_configuration: session.missing_configuration ?? [],
        },
      };
      return;
    }
    if (session.status === "created" && session.url) {
      window.location.assign(session.url);
      return;
    }
    message = await readError(res);
  }

  async function openPortal() {
    message = "";
    const res = await hostedCpFetch(
      `organizations/${encodeURIComponent(orgId)}/billing/customer-portal-session`,
      { method: "POST", body: "{}" },
    );
    if (res.status === 401) {
      await goto("/hosted/start");
      return;
    }
    const body = await res.json().catch(() => ({}));
    const session = body.session ?? {};
    if (session.status === "configuration_required") {
      message =
        session.note ||
        "Customer portal is not fully configured. Check Stripe env.";
      return;
    }
    if (session.status === "created" && session.url) {
      window.location.assign(session.url);
      return;
    }
    message = await readError(res);
  }

  function managerDisplayName(m) {
    const dn = String(m.account_display_name ?? "").trim();
    const em = String(m.account_email ?? "").trim();
    if (dn) {
      return dn;
    }
    if (em) {
      return em;
    }
    return "Member";
  }
</script>

<div class="hosted-page hosted-page--wide">
  <p class="hosted-crumb">
    <a href="/hosted/organizations">Organizations</a>
    <span aria-hidden="true"> / </span>
    <span>Billing</span>
  </p>
  <h1 class="hosted-title">Billing</h1>
  <p class="hosted-sub">
    Organization <code class="hosted-code">{orgId}</code>
  </p>

  {#if message}
    <p class="hosted-error">{message}</p>
  {/if}

  {#if role === "loading"}
    <p class="hosted-muted">Loading…</p>
  {:else if role === "member"}
    <section class="hosted-card">
      <h2>Who manages billing</h2>
      <p class="hosted-hint">
        Billing is managed by your organization owner or admin. Contact one of
        the people below to change plans, payment method, or invoices.
      </p>
      {#if managers.length === 0}
        <p class="hosted-muted">No active managers listed.</p>
      {:else}
        <ul class="hosted-manager-list">
          {#each managers as m (m.id)}
            <li>
              <strong>{managerDisplayName(m)}</strong>
              {#if String(m.account_email ?? "").trim()}
                <span class="hosted-muted"> · {m.account_email}</span>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  {:else if role === "manager" && summary}
    {#if activatingBanner}
      <div class="hosted-banner" role="status">
        <strong>Activating your subscription…</strong>
        <span class="hosted-muted"
          >We are confirming your plan with Stripe. This usually takes a few
          seconds.</span
        >
      </div>
    {/if}
    {#if activationTimedOut}
      <div class="hosted-banner hosted-banner--warn" role="status">
        <strong>Still processing</strong>
        <span class="hosted-muted"
          >Refresh in a minute or contact support if billing does not update.</span
        >
        <button
          type="button"
          class="hosted-btn hosted-btn--secondary"
          onclick={() => refreshAfterTimeout()}>Refresh</button
        >
      </div>
    {/if}

    {@const cfg = summary.configuration ?? {}}
    {@const ba = summary.billing_account ?? {}}
    {@const us = summary.usage_summary ?? {}}
    {@const plan = us.plan ?? {}}
    {#if cfg.configured === false || (cfg.missing_configuration?.length ?? 0) > 0}
      <section class="hosted-card">
        <h2>Billing not yet configured</h2>
        <p class="hosted-hint">
          Stripe environment variables are incomplete. In local dev this is
          expected until you follow
          <code class="hosted-code">make billing-local-smoke</code>
          in the control plane.
        </p>
        {#if cfg.missing_configuration?.length}
          <ul class="hosted-missing">
            {#each cfg.missing_configuration as item (item)}
              <li>{item}</li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}

    <section class="hosted-card">
      <h2>Plan</h2>
      <p class="hosted-hint">
        {plan.display_name ?? "—"} ·
        <code class="hosted-code">{summary.plan_tier}</code>
      </p>
      <ul class="hosted-kv">
        <li>
          <span>Workspaces</span><span>{plan.workspace_limit ?? "—"}</span>
        </li>
        <li><span>Seats</span><span>{plan.human_seat_limit ?? "—"}</span></li>
        <li>
          <span>Storage (GB)</span><span>{plan.included_storage_gb ?? "—"}</span
          >
        </li>
      </ul>
    </section>

    <section class="hosted-card">
      <h2>Subscription</h2>
      <ul class="hosted-kv">
        <li>
          <span>Billing status</span><span>{ba.billing_status ?? "—"}</span>
        </li>
        <li>
          <span>Stripe subscription</span><span
            >{subscriptionStatusLabel(ba)}</span
          >
        </li>
        <li>
          <span>Current period end</span><span
            >{ba.current_period_end ?? "—"}</span
          >
        </li>
        <li>
          <span>Cancel at period end</span><span
            >{ba.cancel_at_period_end ? "Yes" : "No"}</span
          >
        </li>
      </ul>
    </section>

    <section class="hosted-actions">
      {#if stripeSubscriptionManagedClient(ba)}
        <button
          type="button"
          class="hosted-btn hosted-btn--primary"
          onclick={() => openPortal()}>Manage billing</button
        >
        <p class="hosted-hint">
          Opens Stripe Customer Portal for invoices, payment method, upgrades,
          and cancellation.
        </p>
      {:else}
        <button
          type="button"
          class="hosted-btn hosted-btn--primary"
          onclick={() => checkoutPlan("team")}>Upgrade to Team</button
        >
        <button
          type="button"
          class="hosted-btn hosted-btn--secondary"
          onclick={() => checkoutPlan("scale")}>Upgrade to Scale</button
        >
        <p class="hosted-hint">
          Checkout opens in Stripe. After paying, you will return here while we
          activate your plan.
        </p>
      {/if}
    </section>
  {:else if role === "error"}
    <p class="hosted-muted">Could not load billing.</p>
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
  .hosted-error {
    color: var(--ui-danger, #c62828);
    margin: 0 0 1rem;
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
  .hosted-manager-list {
    margin: 0;
    padding-left: 1.2rem;
  }
  .hosted-missing {
    margin: 0.5rem 0 0;
    padding-left: 1.2rem;
    color: var(--ui-text-muted);
    font-size: 0.9rem;
  }
  .hosted-banner {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
    padding: 0.85rem 1rem;
    border-radius: var(--ui-radius-md);
    border: 1px solid var(--ui-border);
    background: color-mix(in srgb, var(--ui-accent) 10%, transparent);
    margin-bottom: 1rem;
  }
  .hosted-banner--warn {
    background: color-mix(in srgb, var(--ui-warning, #f9a825) 12%, transparent);
  }
</style>
