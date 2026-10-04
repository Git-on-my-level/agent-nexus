export function humanActorIdSet(actors = [], principals = []) {
  const ids = new Set();
  for (const actor of Array.isArray(actors) ? actors : []) {
    const tags = Array.isArray(actor?.tags)
      ? actor.tags.map((tag) => String(tag).toLowerCase())
      : [];
    const id = String(actor?.id ?? actor?.actor_id ?? "").trim();
    if (id && tags.includes("human")) ids.add(id);
  }
  for (const principal of Array.isArray(principals) ? principals : []) {
    if (String(principal?.principal_kind ?? "").toLowerCase() !== "human")
      continue;
    const id = String(principal?.actor_id ?? "").trim();
    if (id) ids.add(id);
  }
  return ids;
}

export function isHumanNextActor(work, humanIds) {
  const raw = String(work?.next_actor ?? "").trim();
  if (!raw || !humanIds) return false;
  if (raw.toLowerCase() === "human" || /^human:/i.test(raw)) return true;
  const bare = raw.replace(/^actor:/i, "");
  return humanIds.has(raw) || humanIds.has(bare);
}
