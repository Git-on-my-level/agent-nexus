function badgeForWakeRoutingState(state) {
  switch (String(state ?? "").trim()) {
    case "online":
      return {
        badgeLabel: "Online",
        badgeClass: "bg-ok-soft text-ok-text",
      };
    case "revoked":
      return {
        badgeLabel: "Revoked",
        badgeClass: "bg-danger-soft text-danger-text",
      };
    case "disabled":
      return {
        badgeLabel: "Disabled",
        badgeClass: "bg-warn-soft text-warn-text",
      };
    case "unregistered":
      return {
        badgeLabel: "Unregistered",
        badgeClass: "bg-warn-soft text-warn-text",
      };
    case "unknown":
      return {
        badgeLabel: "Unknown",
        badgeClass: "bg-bg-soft text-fg-muted",
      };
    default:
      return {
        badgeLabel: "Offline",
        badgeClass: "bg-warn-soft text-warn-text",
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
