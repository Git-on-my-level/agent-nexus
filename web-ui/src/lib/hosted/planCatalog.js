/**
 * Hosted plan marketing + tier display helpers.
 * Tier ids must stay aligned with control plane validatePlanTier (starter | team | scale | enterprise).
 * Canonical limits live in control plane `planForTier` as `BillingSummary.plan_usage_envelopes`.
 */

/** Enterprise "Talk to sales" CTA href. */
export const ENTERPRISE_SALES_HREF =
  "mailto:david@scalingforever.com?subject=Enterprise%20plan%20inquiry";

/**
 * Short label for org badges — matches control plane displayNameForPlanTier.
 * @param {string} [planTier]
 */
export function planLabel(planTier) {
  const t = String(planTier ?? "starter").toLowerCase();
  switch (t) {
    case "team":
      return "Pro";
    case "scale":
      return "Scale";
    case "enterprise":
      return "Enterprise";
    case "starter":
    default:
      return "Free";
  }
}

/**
 * @param {string} [planTier]
 */
export function planBadgeClasses(planTier) {
  const t = String(planTier ?? "starter").toLowerCase();
  if (t === "enterprise") return "text-fuchsia-400 bg-fuchsia-500/10";
  if (t === "scale") return "text-accent-text bg-accent-soft";
  if (t === "team") return "text-ok-text bg-ok-soft";
  return "text-fg-subtle bg-panel-hover";
}

/**
 * @param {number} n
 */
export function formatCountForPlanCard(n) {
  const v = Number(n);
  if (!Number.isFinite(v)) return "0";
  return new Intl.NumberFormat("en-US", { maximumFractionDigits: 0 }).format(
    Math.trunc(v),
  );
}

/**
 * @param {unknown} v
 */
function coerceNonNegativeInt(v) {
  if (typeof v === "number" && Number.isFinite(v)) {
    return Math.max(0, Math.trunc(v));
  }
  if (typeof v === "string" && v.trim() !== "") {
    const n = Number(v);
    if (Number.isFinite(n)) return Math.max(0, Math.trunc(n));
  }
  return null;
}

/**
 * Last resort when billing JSON predates `plan_usage_envelopes` or payloads stringify ints.
 * Must match `catalogPlanUsageEnvelopes` / `planForTier` in controlplane/internal/controlplane/organizations.go.
 */
export const PLAN_USAGE_LIMIT_FALLBACK = /** @type {const} */ ({
  starter: { workspace_limit: 1, artifact_capacity: 1000 },
  team: { workspace_limit: 5, artifact_capacity: 125_000 },
  scale: { workspace_limit: 25, artifact_capacity: 2_500_000 },
  enterprise: { workspace_limit: 100, artifact_capacity: 100_000_000 },
});

/**
 * Prefer `BillingSummary.plan_usage_envelopes[tier]`; fall back only when counts are absent.
 * @param {Record<string, any> | null | undefined} billingSummary
 * @param {string} tierId
 */
export function tierEnvelopeForBillingSummary(billingSummary, tierId) {
  const id = String(tierId ?? "")
    .trim()
    .toLowerCase();
  const api = billingSummary?.plan_usage_envelopes?.[id];
  const wl = coerceNonNegativeInt(api?.workspace_limit);
  const cap = coerceNonNegativeInt(api?.artifact_capacity);
  if (wl !== null && cap !== null) {
    return { workspace_limit: wl, artifact_capacity: cap };
  }
  const fb = PLAN_USAGE_LIMIT_FALLBACK[id];
  if (!fb) return {};
  return {
    workspace_limit: fb.workspace_limit,
    artifact_capacity: fb.artifact_capacity,
  };
}

/**
 * User-facing bullets from a control-plane `UsagePlan` envelope (or `{ workspace_limit, artifact_capacity }`).
 * @param {{ workspace_limit?: unknown; artifact_capacity?: unknown }} envelope
 * @returns {string[]}
 */
export function usagePlanLimitFeatureLines(envelope) {
  const wl = coerceNonNegativeInt(envelope?.workspace_limit);
  const cap = coerceNonNegativeInt(envelope?.artifact_capacity);
  if (wl === null || cap === null) return [];
  return [
    `Up to ${formatCountForPlanCard(wl)} workspaces`,
    `${formatCountForPlanCard(cap)} artifacts included`,
  ];
}

export const PLAN_CARDS = [
  {
    id: "starter",
    name: "Starter",
    price: "$0",
    priceSuffix: "/mo",
    tagline: "Good for weekend projects or a small business.",
    features: ["Community support", "Core hosted features"],
    ctaLabel: "Free plan",
    ctaUpgrade: false,
  },
  {
    id: "team",
    name: "Pro",
    price: "$10",
    priceSuffix: "/mo",
    tagline: "Run real AI-first organizations.",
    features: ["Email support", "Built for growing teams"],
    highlight: true,
    ctaLabel: "Upgrade to Pro",
    ctaUpgrade: true,
  },
  {
    id: "scale",
    name: "Scale",
    price: "$50",
    priceSuffix: "/mo",
    tagline: "Run multiple AI-first organizations at the same time.",
    features: ["1-1 support from founder", "Prioritized feature requests"],
    ctaLabel: "Upgrade to Scale",
    ctaUpgrade: true,
  },
  {
    id: "enterprise",
    name: "Enterprise",
    price: "Custom",
    priceSuffix: "",
    tagline: "Custom deployments, on-premise available.",
    features: [
      "Tailored to your environment",
      "Work with us on security and compliance",
      "Dedicated support",
    ],
    ctaLabel: "Talk to sales",
    ctaUpgrade: false,
  },
];
