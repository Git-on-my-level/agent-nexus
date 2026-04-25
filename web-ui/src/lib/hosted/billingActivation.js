/** sessionStorage snapshot before Checkout redirect; per-tab; see hosted billing UX plan. */
export const BILLING_SNAPSHOT_PREFIX = "anx_billing_snapshot_";

/** @param {string} orgId */
export function billingSnapshotKey(orgId) {
  return `${BILLING_SNAPSHOT_PREFIX}${String(orgId ?? "").trim()}`;
}

/** @param {{ ts: number, plan_tier: string, stripe_subscription_status: string }} snap */
export function billingSnapshotExpired(snap, nowMs = Date.now()) {
  if (!snap || typeof snap.ts !== "number") {
    return true;
  }
  const maxAgeMs = 10 * 60 * 1000;
  return nowMs - snap.ts > maxAgeMs;
}

/**
 * Poll intervals: 2s → 3s → 4.5s → 6.75s → 10s → 10s (factor 1.5, cap 10s), ~36s total budget.
 * @param {number} index zero-based attempt index after the first fetch
 */
export function billingPollDelayMs(index) {
  const initial = 2000;
  const factor = 1.5;
  const cap = 10000;
  let ms = initial;
  for (let i = 0; i < index; i++) {
    ms = Math.min(cap, Math.round(ms * factor));
  }
  return ms;
}

/** Full sequence of delays between GET /billing polls (six gaps after t=0 read). */
export function billingPollScheduleDelays() {
  const out = [];
  for (let i = 0; i < 6; i++) {
    out.push(billingPollDelayMs(i));
  }
  return out;
}

/**
 * @param {string} orgId
 * @param {{ plan_tier: string, stripe_subscription_status: string }} summaryLike from BillingSummary
 */
export function writeBillingSnapshot(orgId, summaryLike) {
  if (typeof sessionStorage === "undefined") {
    return;
  }
  const key = billingSnapshotKey(orgId);
  const payload = {
    ts: Date.now(),
    plan_tier: String(summaryLike?.plan_tier ?? ""),
    stripe_subscription_status: String(
      summaryLike?.billing_account?.stripe_subscription_status ??
        summaryLike?.stripe_subscription_status ??
        "",
    ),
  };
  sessionStorage.setItem(key, JSON.stringify(payload));
}

/**
 * @param {string} orgId
 * @returns {{ ts: number, plan_tier: string, stripe_subscription_status: string } | null}
 */
export function readBillingSnapshot(orgId) {
  if (typeof sessionStorage === "undefined") {
    return null;
  }
  const raw = sessionStorage.getItem(billingSnapshotKey(orgId));
  if (!raw) {
    return null;
  }
  try {
    const j = JSON.parse(raw);
    if (
      typeof j?.ts !== "number" ||
      typeof j?.plan_tier !== "string" ||
      typeof j?.stripe_subscription_status !== "string"
    ) {
      return null;
    }
    return j;
  } catch {
    return null;
  }
}

/** @param {string} orgId */
export function clearBillingSnapshot(orgId) {
  if (typeof sessionStorage === "undefined") {
    return;
  }
  sessionStorage.removeItem(billingSnapshotKey(orgId));
}

/**
 * @param {{ plan_tier: string, stripe_subscription_status: string }} snap
 * @param {{ plan_tier: string, billing_account?: { stripe_subscription_status?: string }}} summary
 */
export function billingSnapshotMatchesSummary(snap, summary) {
  const pt = String(summary?.plan_tier ?? "");
  const st = String(summary?.billing_account?.stripe_subscription_status ?? "");
  return snap.plan_tier !== pt || snap.stripe_subscription_status !== st;
}

/** Mirrors control plane `stripeSubscriptionManaged` for billing UI branching. */
export function stripeSubscriptionManagedClient(billingAccount) {
  const sid = String(billingAccount?.stripe_subscription_id ?? "").trim();
  if (!sid) {
    return false;
  }
  const st = String(billingAccount?.stripe_subscription_status ?? "")
    .trim()
    .toLowerCase();
  return ![
    "",
    "free",
    "not_started",
    "canceled",
    "incomplete_expired",
    "unpaid",
  ].includes(st);
}
