import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";

import {
  billingPollScheduleDelays,
  billingSnapshotExpired,
  billingSnapshotMatchesSummary,
  clearBillingSnapshot,
  pollBillingActivation,
  readBillingSnapshot,
  writeBillingSnapshot,
} from "../../src/lib/hosted/billingActivation.js";

function deferred() {
  let resolve;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

describe("billing activation snapshot", () => {
  beforeEach(() => {
    global.sessionStorage = {
      _m: new Map(),
      getItem(k) {
        return this._m.get(k) ?? null;
      },
      setItem(k, v) {
        this._m.set(k, v);
      },
      removeItem(k) {
        this._m.delete(k);
      },
    };
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("writes anx_billing_snapshot_{orgId} with plan fields before checkout redirect", () => {
    writeBillingSnapshot("org_x", {
      plan_tier: "starter",
      billing_account: { stripe_subscription_status: "not_started" },
    });
    const raw = sessionStorage.getItem("anx_billing_snapshot_org_x");
    const j = JSON.parse(raw);
    expect(j.plan_tier).toBe("starter");
    expect(j.stripe_subscription_status).toBe("not_started");
    expect(typeof j.ts).toBe("number");
  });

  it("treats snapshot older than 10 minutes as missing for polling", () => {
    const snap = {
      ts: Date.now() - 11 * 60 * 1000,
      plan_tier: "team",
      stripe_subscription_status: "active",
    };
    expect(billingSnapshotExpired(snap)).toBe(true);
  });

  it("detects plan or subscription change vs snapshot", () => {
    const snap = {
      ts: Date.now(),
      plan_tier: "starter",
      stripe_subscription_status: "not_started",
    };
    const summary = {
      plan_tier: "team",
      billing_account: { stripe_subscription_status: "active" },
    };
    expect(billingSnapshotMatchesSummary(snap, summary)).toBe(true);
  });

  it("clearBillingSnapshot removes key", () => {
    writeBillingSnapshot("org_z", {
      plan_tier: "starter",
      billing_account: { stripe_subscription_status: "x" },
    });
    clearBillingSnapshot("org_z");
    expect(readBillingSnapshot("org_z")).toBe(null);
  });
});

describe("billing poll schedule", () => {
  it("matches 2s → 3s → 4.5s → 6.75s → 10s → 10s", () => {
    expect(billingPollScheduleDelays()).toEqual([
      2000, 3000, 4500, 6750, 10000, 10000,
    ]);
  });
});

describe("billing activation poll", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("stops delayed polling when cleanup aborts the run", async () => {
    vi.useFakeTimers();
    const controller = new AbortController();
    const fetchBillingSummary = vi.fn();
    const onSummary = vi.fn();
    const onTimeout = vi.fn();

    const poll = pollBillingActivation({
      snapshot: {
        plan_tier: "starter",
        stripe_subscription_status: "not_started",
      },
      initialSummary: {
        plan_tier: "starter",
        billing_account: { stripe_subscription_status: "not_started" },
      },
      fetchBillingSummary,
      signal: controller.signal,
      delays: [1000],
      documentHidden: () => false,
      onSummary,
      onTimeout,
    });

    await vi.advanceTimersByTimeAsync(500);
    controller.abort();
    await vi.advanceTimersByTimeAsync(1000);

    await expect(poll).resolves.toBe("aborted");
    expect(fetchBillingSummary).not.toHaveBeenCalled();
    expect(onSummary).not.toHaveBeenCalled();
    expect(onTimeout).not.toHaveBeenCalled();
  });

  it("does not mutate summary when cleanup aborts an in-flight fetch", async () => {
    vi.useFakeTimers();
    const controller = new AbortController();
    const slowFetch = deferred();
    const onSummary = vi.fn();
    const onTimeout = vi.fn();
    const fetchBillingSummary = vi.fn(() => slowFetch.promise);

    const poll = pollBillingActivation({
      snapshot: {
        plan_tier: "starter",
        stripe_subscription_status: "not_started",
      },
      initialSummary: {
        plan_tier: "starter",
        billing_account: { stripe_subscription_status: "not_started" },
      },
      fetchBillingSummary,
      signal: controller.signal,
      delays: [1],
      documentHidden: () => false,
      onSummary,
      onTimeout,
    });

    await vi.advanceTimersByTimeAsync(1);
    expect(fetchBillingSummary).toHaveBeenCalledTimes(1);

    controller.abort();
    slowFetch.resolve({
      summary: {
        plan_tier: "team",
        billing_account: { stripe_subscription_status: "active" },
      },
    });

    await expect(poll).resolves.toBe("aborted");
    expect(onSummary).not.toHaveBeenCalled();
    expect(onTimeout).not.toHaveBeenCalled();
  });
});
