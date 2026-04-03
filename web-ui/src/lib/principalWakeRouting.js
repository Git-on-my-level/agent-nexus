function badgeForWakeRoutingState(state) {
  switch (String(state ?? "").trim()) {
    case "online":
      return {
        badgeLabel: "Online",
        badgeClass: "bg-emerald-500/10 text-emerald-400",
      };
    case "revoked":
      return {
        badgeLabel: "Revoked",
        badgeClass: "bg-red-500/10 text-red-400",
      };
    case "disabled":
      return {
        badgeLabel: "Disabled",
        badgeClass: "bg-amber-500/10 text-amber-400",
      };
    case "unregistered":
      return {
        badgeLabel: "Unregistered",
        badgeClass: "bg-amber-500/10 text-amber-400",
      };
    case "unknown":
      return {
        badgeLabel: "Unknown",
        badgeClass: "bg-slate-500/10 text-slate-300",
      };
    default:
      return {
        badgeLabel: "Offline",
        badgeClass: "bg-amber-500/10 text-amber-400",
      };
  }
}

function normalizeWakeRouting(value, principal) {
  const wakeRouting = value && typeof value === "object" ? value : null;
  const applicable =
    wakeRouting?.applicable ?? principal?.principal_kind === "agent";
  const state = String(wakeRouting?.state ?? "unknown").trim() || "unknown";
  const summary =
    String(wakeRouting?.summary ?? "").trim() ||
    "Wake routing status is unavailable right now.";
  return {
    applicable,
    handle: String(wakeRouting?.handle ?? principal?.username ?? "").trim(),
    taggable: Boolean(wakeRouting?.taggable),
    online: Boolean(wakeRouting?.online),
    offline: state === "offline",
    state,
    ...badgeForWakeRoutingState(state),
    summary,
  };
}

export async function enrichPrincipalsWithWakeRouting(principalList) {
  return (principalList ?? []).map((principal) => ({
    ...principal,
    wakeRouting: normalizeWakeRouting(principal?.wake_routing, principal),
  }));
}
