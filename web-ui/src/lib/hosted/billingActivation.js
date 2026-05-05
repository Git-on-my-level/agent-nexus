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

function abortError() {
  return new DOMException("Billing activation poll aborted", "AbortError");
}

/**
 * @param {number} ms
 * @param {AbortSignal | undefined} signal
 */
function waitForBillingPollDelay(ms, signal) {
  if (signal?.aborted) {
    return Promise.reject(abortError());
  }
  return new Promise((resolve, reject) => {
    const cleanup = () => signal?.removeEventListener("abort", abort);
    const timeout = setTimeout(() => {
      cleanup();
      resolve();
    }, ms);
    const abort = () => {
      cleanup();
      clearTimeout(timeout);
      reject(abortError());
    };
    signal?.addEventListener("abort", abort, { once: true });
  });
}

/**
 * Runs the delayed hosted billing activation polls and skips every callback once aborted.
 *
 * @param {{
 *   snapshot: { plan_tier: string, stripe_subscription_status: string },
 *   initialSummary: { plan_tier: string, billing_account?: { stripe_subscription_status?: string }},
 *   fetchBillingSummary: () => Promise<any>,
 *   signal?: AbortSignal,
 *   delays?: number[],
 *   documentHidden?: () => boolean,
 *   onSummary?: (summary: any) => void,
 *   onMatched?: (summary: any) => void,
 *   onHidden?: () => void,
 *   onTimeout?: () => void,
 * }} options
 * @returns {Promise<'matched'|'hidden'|'timeout'|'stopped'|'unauthorized'|'aborted'>}
 */
export async function pollBillingActivation(options) {
  const {
    snapshot,
    initialSummary,
    fetchBillingSummary,
    signal,
    delays = billingPollScheduleDelays(),
    documentHidden = () =>
      typeof document !== "undefined" && document.visibilityState === "hidden",
    onSummary = () => {},
    onMatched = () => {},
    onHidden = () => {},
    onTimeout = () => {},
  } = options;

  const aborted = () => signal?.aborted === true;
  if (aborted()) return "aborted";

  if (billingSnapshotMatchesSummary(snapshot, initialSummary)) {
    onMatched(initialSummary);
    return "matched";
  }

  for (const delay of delays) {
    try {
      await waitForBillingPollDelay(delay, signal);
    } catch (err) {
      if (err?.name === "AbortError") {
        return "aborted";
      }
      throw err;
    }
    if (aborted()) return "aborted";

    if (documentHidden()) {
      onHidden();
      return "hidden";
    }

    const next = await fetchBillingSummary();
    if (aborted()) return "aborted";
    if (next?.unauthorized) {
      return "unauthorized";
    }
    if (!next || next.forbidden || next.error || !next.summary) {
      return "stopped";
    }

    onSummary(next.summary);
    if (billingSnapshotMatchesSummary(snapshot, next.summary)) {
      onMatched(next.summary);
      return "matched";
    }
  }

  if (aborted()) return "aborted";
  onTimeout();
  return "timeout";
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
