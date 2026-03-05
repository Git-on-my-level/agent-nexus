import { get, writable } from "svelte/store";

export const ACTOR_STORAGE_KEY = "oar_ui_actor_id";

export const actorSessionReady = writable(false);
export const selectedActorId = writable("");
export const actorRegistry = writable([]);

export function loadStoredActorId(storage = localStorage) {
  return storage.getItem(ACTOR_STORAGE_KEY) ?? "";
}

export function saveSelectedActorId(actorId, storage = localStorage) {
  if (!actorId) {
    storage.removeItem(ACTOR_STORAGE_KEY);
    return "";
  }

  storage.setItem(ACTOR_STORAGE_KEY, actorId);
  return actorId;
}

export function initializeActorSession(storage = localStorage) {
  const actorId = loadStoredActorId(storage);
  selectedActorId.set(actorId);
  actorSessionReady.set(true);
  return actorId;
}

export function chooseActor(actorId, storage = localStorage) {
  const stored = saveSelectedActorId(actorId, storage);
  selectedActorId.set(stored);
  return stored;
}

export function shouldShowActorGate(isReady, actorId) {
  return Boolean(isReady) && !actorId;
}

export function buildActorCreatePayload({
  id,
  displayName,
  tags = [],
  createdAt = new Date().toISOString(),
}) {
  return {
    actor: {
      id,
      display_name: displayName,
      tags,
      created_at: createdAt,
    },
  };
}

export function buildActorNameMap(actors) {
  return new Map(
    (actors ?? []).map((actor) => [
      actor.id,
      actor.display_name || actor.id || "Unknown actor",
    ]),
  );
}

export function lookupActorDisplayName(actorId, actors) {
  if (!actorId) {
    return "Unknown actor";
  }

  const map = buildActorNameMap(actors);
  return map.get(actorId) ?? actorId;
}

export function getSelectedActorId() {
  return get(selectedActorId);
}
