/**
 * Hosted plan marketing + tier display helpers.
 * Tier ids must stay aligned with control plane validatePlanTier (starter | team | scale | enterprise).
 */

/** Enterprise "Talk to sales" CTA href. */
export const ENTERPRISE_SALES_HREF =
  "mailto:sales@scalingforever.com?subject=Enterprise%20plan%20inquiry";

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
    features: ["Priority support", "SSO-ready options"],
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
