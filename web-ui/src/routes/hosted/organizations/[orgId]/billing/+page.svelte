<script>
  import { untrack } from "svelte";

  import { browser } from "$app/environment";
  import { page } from "$app/stores";

  import { goto } from "$app/navigation";

  import Button from "$lib/components/Button.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateError from "$lib/components/state/StateError.svelte";

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
  import { setActiveOrg } from "$lib/hosted/session.js";

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
  let upgradeBusy = $state("");
  let roleRetryBusy = $state(false);

  const PLAN_CARDS = [
    {
      id: "starter",
      name: "Starter",
      price: "$0",
      priceSuffix: "/mo",
      tagline: "Free forever — perfect for evaluating.",
      features: [
        "1 workspace",
        "1,000 artifacts total (org-wide pool)",
        "1 GB storage (org)",
        "Community support",
      ],
      ctaLabel: "Free plan",
      ctaUpgrade: false,
    },
    {
      id: "team",
      name: "Pro",
      price: "$29",
      priceSuffix: "/seat / mo",
      tagline: "For professionals and teams shipping with AI agents.",
      features: [
        "5 workspaces",
        "125,000 artifacts total (org-wide pool)",
        "25 GB storage (org)",
        "Email support",
      ],
      highlight: true,
      ctaLabel: "Upgrade to Pro",
      ctaUpgrade: true,
    },
    {
      id: "scale",
      name: "Scale",
      price: "$99",
      priceSuffix: "/seat / mo",
      tagline: "Higher limits and SSO-ready.",
      features: [
        "25 workspaces",
        "2.5M artifacts total (org-wide pool)",
        "250 GB storage (org)",
        "Priority support",
      ],
      ctaLabel: "Upgrade to Scale",
      ctaUpgrade: true,
    },
    {
      id: "enterprise",
      name: "Enterprise",
      price: "Custom",
      priceSuffix: "",
      tagline: "Dedicated infra, SSO, and contracts.",
      features: [
        "100 workspaces",
        "100M artifacts total (org-wide pool)",
        "1,000 GB storage (org)",
        "SSO + audit logs · dedicated support",
      ],
      ctaLabel: "Talk to sales",
      ctaUpgrade: false,
    },
  ];

  async function readError(res) {
    try {
      const j = await res.json();
      return j?.error?.message || j?.error?.code || res.statusText;
    } catch {
      return res.statusText;
    }
  }

  /** Stripe rows default to these; no subscription lifecycle to show next to plan name. */
  const IDLE_STRIPE_SUBSCRIPTION_STATUSES = new Set([
    "",
    "free",
    "not_started",
  ]);

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

  /** @param {any} billingAccount */
  function showSubscriptionStatusBadge(billingAccount) {
    const st = String(billingAccount?.stripe_subscription_status ?? "")
      .trim()
      .toLowerCase();
    return !IDLE_STRIPE_SUBSCRIPTION_STATUSES.has(st);
  }

  function stripActivatingQuery() {
    if (!browser) return;
    const u = new URL(window.location.href);
    if (!u.searchParams.has("activating")) return;
    u.searchParams.delete("activating");
    window.history.replaceState({}, "", `${u.pathname}${u.search}${u.hash}`);
  }

  function isActivatingCheckoutFromUrl() {
    if (!browser) return false;
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
    if (!res.ok) return;
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
    if (!got) return;
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

  async function retryLoad() {
    roleRetryBusy = true;
    await load();
    roleRetryBusy = false;
  }

  /** @param {any} initialSummary */
  async function maybeRunActivationPoll(initialSummary) {
    if (!browser) return;
    if (!isActivatingCheckoutFromUrl()) return;
    const snap = readBillingSnapshot(orgId);
    if (!snap || billingSnapshotExpired(snap)) {
      stripActivatingQuery();
      return;
    }
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
    if (!browser || !orgId) return;
    setActiveOrg(orgId);
    untrack(() => {
      void load();
    });
  });

  async function checkoutPlan(planTier) {
    message = "";
    if (!summary) return;
    upgradeBusy = planTier;
    try {
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
          "Billing is not fully configured yet. Contact support if this persists.";
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
    } finally {
      upgradeBusy = "";
    }
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
        "Customer portal is not configured yet. Contact support if this persists.";
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
    return dn || em || "Member";
  }
</script>

<svelte:head>
  <title>Billing — ANX</title>
</svelte:head>

<div class="space-y-5">
  <div>
    <p class="text-micro text-fg-subtle">
      <a
        class="text-fg-subtle underline-offset-2 transition-colors hover:text-fg hover:underline"
        href={`/hosted/organizations/${encodeURIComponent(orgId)}`}
        >← Overview</a
      >
    </p>
    <h1 class="mt-1 text-display text-fg">Billing</h1>
  </div>

  {#if message}
    <p
      role="alert"
      class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
    >
      {message}
    </p>
  {/if}

  {#if role === "loading"}
    <div class="space-y-3">
      <div class="rounded-md border border-line bg-bg-soft px-4 py-3">
        <Skeleton rows={2} />
      </div>
      <div class="rounded-md border border-line bg-bg-soft px-4 py-4">
        <Skeleton rows={5} />
      </div>
    </div>
  {:else if role === "member"}
    <section class="rounded-md border border-line bg-bg-soft px-5 py-5">
      <h2 class="text-subtitle text-fg">Who manages billing</h2>
      <p class="mt-1 text-meta text-fg-subtle">
        Billing is managed by your organization's owner or admin. Reach out to
        someone below to change plans, payment methods, or invoices.
      </p>
      {#if managers.length === 0}
        <p class="mt-3 text-meta text-fg-subtle">No active managers listed.</p>
      {:else}
        <ul
          class="mt-3 divide-y divide-line rounded-md border border-line bg-bg"
        >
          {#each managers as m (m.id)}
            <li class="flex items-center justify-between gap-3 px-3 py-2">
              <span class="text-body font-medium text-fg"
                >{managerDisplayName(m)}</span
              >
              {#if String(m.account_email ?? "").trim()}
                <span class="text-micro text-fg-subtle">{m.account_email}</span>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  {:else if role === "manager" && summary}
    {@const cfg = summary.configuration ?? {}}
    {@const publicBeta = Boolean(cfg.public_beta_no_paid_upgrades)}
    {#if activatingBanner && !publicBeta}
      <div
        class="flex items-start gap-3 rounded-md bg-accent-soft px-3 py-2 text-micro text-accent-text"
        role="status"
      >
        <span
          class="mt-0.5 inline-block h-2 w-2 animate-pulse rounded-full bg-accent-text"
          aria-hidden="true"
        ></span>
        <div>
          <strong class="font-medium">Activating your subscription…</strong>
          <span class="text-fg-subtle">
            We're confirming your plan with Stripe. This usually takes a few
            seconds.
          </span>
        </div>
      </div>
    {/if}
    {#if activationTimedOut}
      <div
        class="flex items-center justify-between gap-3 rounded-md bg-warn-soft px-3 py-2 text-micro text-warn-text"
        role="status"
      >
        <div>
          <strong class="font-medium">Still processing.</strong>
          <span class="text-fg-subtle">
            Refresh in a minute or contact support if billing doesn't update.
          </span>
        </div>
        <Button variant="secondary" onclick={() => refreshAfterTimeout()}
          >Refresh</Button
        >
      </div>
    {/if}

    {@const ba = summary.billing_account ?? {}}
    {@const us = summary.usage_summary ?? {}}
    {@const plan = us.plan ?? {}}
    {@const currentTier = String(summary.plan_tier ?? "starter").toLowerCase()}
    {@const managed = stripeSubscriptionManagedClient(ba)}

    {#if publicBeta}
      <section
        class="rounded-md border border-line bg-bg-soft px-4 py-3 text-meta text-fg"
      >
        <strong class="text-body text-fg">Public beta — no live payments</strong>
        <p class="mt-1.5 text-fg-subtle">
          Self-serve plan upgrades and in-app checkout are not available. Your
          organization uses the free tier. Contact us if you need a change for
          your team.
        </p>
      </section>
    {/if}

    {#if !publicBeta && (cfg.configured === false || (cfg.missing_configuration?.length ?? 0) > 0)}
      <section
        class="rounded-md bg-warn-soft px-3 py-2 text-micro text-warn-text"
      >
        <strong class="font-medium">Billing not yet configured.</strong>
        Stripe is incomplete in this environment. Upgrades will not work until an
        operator finishes setup.
      </section>
    {/if}

    <section class="rounded-md border border-line bg-bg-soft px-4 py-3">
      <div class="flex flex-wrap items-baseline justify-between gap-2">
        <div>
          <div class="text-micro uppercase tracking-wide text-fg-subtle">
            Current plan
          </div>
          <div class="mt-0.5 flex items-center gap-2">
            <span class="text-subtitle tabular-nums text-fg"
              >{plan.display_name ?? "Starter"}</span
            >
            {#if showSubscriptionStatusBadge(ba)}
              <span
                class="rounded bg-accent-soft px-1.5 py-0.5 text-micro text-accent-text"
                >{subscriptionStatusLabel(ba)}</span
              >
            {/if}
          </div>
          {#if ba.current_period_end}
            <p class="mt-1 text-micro text-fg-subtle">
              Renews
              {ba.cancel_at_period_end ? "(canceling)" : ""}
              <span class="text-fg">{ba.current_period_end}</span>
            </p>
          {/if}
        </div>
        {#if managed && !publicBeta}
          <Button variant="secondary" onclick={() => openPortal()}
            >Manage in Stripe</Button
          >
        {/if}
      </div>
    </section>

    <section>
      <h2 class="text-subtitle text-fg">Plans</h2>
      <p class="mt-1 text-meta text-fg-subtle">
        {#if publicBeta}
          The public beta includes the free tier. Paid self-serve upgrades are not
          available in this environment.
        {:else}
          Switch any time — you'll only pay the prorated difference.
        {/if}
      </p>
      <div class="mt-3 grid gap-3 lg:grid-cols-4 lg:items-stretch">
        {#each PLAN_CARDS as planCard (planCard.id)}
          {@const isCurrent = planCard.id === currentTier}
          <article
            class="flex flex-col rounded-md border bg-bg-soft px-4 py-4 {isCurrent
              ? 'border-accent/55 ring-1 ring-accent/25'
              : planCard.highlight
                ? 'border-line ring-1 ring-accent/15'
                : 'border-line'}"
          >
            <div class="flex items-center justify-between">
              <h3 class="text-subtitle text-fg">
                {planCard.name}
              </h3>
              {#if isCurrent}
                <span
                  class="rounded bg-accent-soft px-1.5 py-0.5 text-micro text-accent-text"
                  >Current</span
                >
              {:else if planCard.highlight}
                <span
                  class="rounded bg-accent-soft px-1.5 py-0.5 text-micro text-accent-text"
                  >Popular</span
                >
              {/if}
            </div>
            <p class="mt-1 text-meta text-fg-subtle">{planCard.tagline}</p>
            <div class="mt-3 flex items-baseline gap-1">
              <span class="text-title tabular-nums text-fg"
                >{planCard.price}</span
              >
              {#if planCard.priceSuffix}
                <span class="text-micro text-fg-subtle"
                  >{planCard.priceSuffix}</span
                >
              {/if}
            </div>
            <ul class="mt-3 space-y-1.5 text-meta text-fg-muted">
              {#each planCard.features as feat}
                <li class="flex items-start gap-1.5">
                  <span
                    class="mt-1.5 inline-block h-1 w-1 shrink-0 rounded-full bg-fg-muted"
                    aria-hidden="true"
                  ></span>
                  <span>{feat}</span>
                </li>
              {/each}
            </ul>
            <div class="mt-auto pt-4">
              {#if isCurrent}
                <Button variant="secondary" class="w-full" disabled
                  >Current plan</Button
                >
              {:else if planCard.id === "enterprise"}
                <Button
                  variant="secondary"
                  class="w-full"
                  href="mailto:sales@oar.app?subject=Enterprise%20plan%20inquiry"
                  >Talk to sales</Button
                >
              {:else if publicBeta && planCard.ctaUpgrade}
                <Button variant="secondary" class="w-full" disabled
                  >Not available in public beta</Button
                >
              {:else if planCard.ctaUpgrade && managed}
                <Button
                  variant="secondary"
                  class="w-full"
                  onclick={() => openPortal()}>Switch plan</Button
                >
              {:else if planCard.ctaUpgrade}
                <Button
                  variant="primary"
                  class="w-full"
                  onclick={() => checkoutPlan(planCard.id)}
                  disabled={!!upgradeBusy}
                >
                  {upgradeBusy === planCard.id
                    ? "Opening Stripe…"
                    : planCard.ctaLabel}
                </Button>
              {:else}
                <Button variant="secondary" class="w-full" disabled
                  >{planCard.ctaLabel}</Button
                >
              {/if}
            </div>
          </article>
        {/each}
      </div>
    </section>
  {:else if role === "error"}
    <StateError
      message={message || "Could not load billing."}
      onretry={retryLoad}
      retrying={roleRetryBusy}
    />
  {/if}
</div>
