export const SYSTEM_ACTOR_ID = "system";
export const LEGACY_SYSTEM_ACTOR_ID = "anx-core";
export const SYSTEM_ACTOR_DISPLAY_LABEL = "System";

const RESERVED_IDS = new Set([SYSTEM_ACTOR_ID, LEGACY_SYSTEM_ACTOR_ID]);

/**
 * True for the canonical system actor id, legacy id, or actor:/human:/agent: typed refs to them.
 */
export function isReservedSystemActorId(raw) {
  let id = String(raw ?? "").trim();
  if (!id) {
    return false;
  }
  if (id.includes(":")) {
    const idx = id.indexOf(":");
    const prefix = id.slice(0, idx).toLowerCase();
    const rest = id.slice(idx + 1).trim();
    if (prefix === "actor" || prefix === "human" || prefix === "agent") {
      id = rest;
    } else {
      return false;
    }
  }
  return RESERVED_IDS.has(id);
}

/** Actors a human may assume or tag (owners, assignees, dev gate); excludes system. */
export function filterActorsForUserSelection(actorList) {
  return (actorList ?? []).filter((a) => !isReservedSystemActorId(a?.id));
}

export function toActorPickerOptions(actorList) {
  return filterActorsForUserSelection(actorList).map((actor) => ({
    id: actor.id,
    title: actor.display_name || actor.id,
    subtitle: actor.id,
    keywords: actor.tags ?? [],
  }));
}
