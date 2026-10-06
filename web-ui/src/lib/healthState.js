/** Read canonical health while preserving legacy v0.12.10 responses. */
export function healthState(item) {
  const state =
    item?.plan_health?.state ??
    item?.health?.state ??
    item?.plan_state?.health_state ??
    item?.health?.status ??
    (typeof item?.health === "string" ? item.health : undefined) ??
    item?.plan_state?.health;
  if (typeof state !== "string") return "";
  return state === "stalled" ? "stale" : state;
}
