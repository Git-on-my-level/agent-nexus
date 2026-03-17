import { get, writable } from "svelte/store";

import {
  getCurrentWorkspaceSlug,
  currentWorkspaceSlug,
} from "./workspaceContext.js";
import {
  DEFAULT_WORKSPACE_SLUG,
  buildWorkspaceStorageKey,
  buildProjectStorageKey,
} from "./workspacePaths.js";

export const ACTOR_STORAGE_KEY = "oar_ui_actor_id";

export const actorSessionReady = writable(false);
export const selectedActorId = writable("");
export const actorRegistry = writable([]);

const actorStateByWorkspace = new Map();

function createEmptyActorState() {
  return {
    ready: false,
    selectedActorId: "",
    actorRegistry: [],
  };
}

function ensureActorState(workspaceSlug = getCurrentWorkspaceSlug()) {
  const slug = String(workspaceSlug ?? "").trim();
  if (!actorStateByWorkspace.has(slug)) {
    actorStateByWorkspace.set(slug, createEmptyActorState());
  }

  return actorStateByWorkspace.get(slug);
}

function syncCurrentWorkspaceStores(workspaceSlug = getCurrentWorkspaceSlug()) {
  const state = ensureActorState(workspaceSlug);
  actorSessionReady.set(state.ready);
  selectedActorId.set(state.selectedActorId);
  actorRegistry.set([...state.actorRegistry]);
  return state;
}

currentWorkspaceSlug.subscribe((workspaceSlug) => {
  syncCurrentWorkspaceStores(workspaceSlug);
});

function migrateProjectActorStorageKey(storage, workspaceSlug) {
  const oldKey = buildProjectStorageKey(ACTOR_STORAGE_KEY, workspaceSlug);
  const newKey = buildWorkspaceStorageKey(ACTOR_STORAGE_KEY, workspaceSlug);

  if (oldKey === newKey) return;

  const oldValue = storage.getItem(oldKey);
  if (oldValue && !storage.getItem(newKey)) {
    storage.setItem(newKey, oldValue);
  }
}

export function actorStorageKey(workspaceSlug = getCurrentWorkspaceSlug()) {
  return buildWorkspaceStorageKey(ACTOR_STORAGE_KEY, workspaceSlug);
}

export function loadStoredActorId(
  storage = localStorage,
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  migrateProjectActorStorageKey(storage, workspaceSlug);

  const scopedActorId = storage.getItem(actorStorageKey(workspaceSlug));
  if (scopedActorId) {
    return scopedActorId;
  }

  const normalizedWorkspaceSlug = String(workspaceSlug ?? "").trim();
  if (
    !normalizedWorkspaceSlug ||
    normalizedWorkspaceSlug === DEFAULT_WORKSPACE_SLUG
  ) {
    return storage.getItem(ACTOR_STORAGE_KEY) ?? "";
  }

  return "";
}

export function saveSelectedActorId(
  actorId,
  storage = localStorage,
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  const storageKey = actorStorageKey(workspaceSlug);
  if (!actorId) {
    storage.removeItem(storageKey);
    storage.removeItem(ACTOR_STORAGE_KEY);
    return "";
  }

  storage.setItem(storageKey, actorId);
  return actorId;
}

export function initializeActorSession(
  storage = localStorage,
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  const state = ensureActorState(workspaceSlug);
  state.selectedActorId = loadStoredActorId(storage, workspaceSlug);
  state.ready = true;
  syncCurrentWorkspaceStores(workspaceSlug);
  return state.selectedActorId;
}

export function chooseActor(
  actorId,
  storage = localStorage,
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  const state = ensureActorState(workspaceSlug);
  state.selectedActorId = saveSelectedActorId(actorId, storage, workspaceSlug);
  syncCurrentWorkspaceStores(workspaceSlug);
  return state.selectedActorId;
}

export function clearSelectedActor(
  storage = localStorage,
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  return chooseActor("", storage, workspaceSlug);
}

export function replaceActorRegistry(
  actors,
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  const state = ensureActorState(workspaceSlug);
  state.actorRegistry = [...(actors ?? [])];
  syncCurrentWorkspaceStores(workspaceSlug);
  return state.actorRegistry;
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

export function getSelectedActorId(workspaceSlug = getCurrentWorkspaceSlug()) {
  if (workspaceSlug && workspaceSlug !== getCurrentWorkspaceSlug()) {
    return ensureActorState(workspaceSlug).selectedActorId;
  }

  return get(selectedActorId);
}
