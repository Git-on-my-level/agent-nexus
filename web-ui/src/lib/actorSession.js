import { get, writable } from "svelte/store";

import {
  getCurrentWorkspaceSlug,
  currentWorkspaceSlug,
} from "./workspaceContext.js";
import { buildWorkspaceStorageKey } from "./workspacePaths.js";
import { buildLegacyProjectStorageKey } from "./compat/workspaceCompat.js";

export const ACTOR_STORAGE_KEY = "oar_ui_actor_id";

export const actorSessionReady = writable(false);
export const selectedActorId = writable("");
export const actorRegistry = writable([]);
export const principalRegistry = writable([]);

const actorStateByWorkspace = new Map();

function createEmptyActorState() {
  return {
    ready: false,
    selectedActorId: "",
    actorRegistry: [],
    principalRegistry: [],
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
  principalRegistry.set([...state.principalRegistry]);
  return state;
}

currentWorkspaceSlug.subscribe((workspaceSlug) => {
  syncCurrentWorkspaceStores(workspaceSlug);
});

function migrateProjectActorStorageKey(storage, workspaceSlug) {
  const slug = String(workspaceSlug ?? "").trim();
  if (!slug) {
    return;
  }
  const oldKey = buildLegacyProjectStorageKey(ACTOR_STORAGE_KEY, slug);
  const newKey = buildWorkspaceStorageKey(ACTOR_STORAGE_KEY, slug);

  if (oldKey === newKey) return;

  const oldValue = storage.getItem(oldKey);
  if (oldValue && !storage.getItem(newKey)) {
    storage.setItem(newKey, oldValue);
  }
}

export function actorStorageKey(workspaceSlug = getCurrentWorkspaceSlug()) {
  const slug = String(workspaceSlug ?? "").trim();
  if (!slug) {
    return ACTOR_STORAGE_KEY;
  }
  return buildWorkspaceStorageKey(ACTOR_STORAGE_KEY, slug);
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
  if (!normalizedWorkspaceSlug) {
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

export function replacePrincipalRegistry(
  principals,
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  const state = ensureActorState(workspaceSlug);
  state.principalRegistry = [...(principals ?? [])];
  syncCurrentWorkspaceStores(workspaceSlug);
  return state.principalRegistry;
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

export function buildActorNameMap(actors, principals = get(principalRegistry)) {
  const map = new Map();

  for (const actor of actors ?? []) {
    const id = String(actor?.id ?? actor?.actor_id ?? "").trim();
    if (!id) {
      continue;
    }
    const label =
      String(
        actor?.display_name ??
          actor?.displayName ??
          actor?.username ??
          actor?.label ??
          id,
      ).trim() ||
      id ||
      "Unknown actor";
    map.set(id, label);
  }

  for (const principal of principals ?? []) {
    const username = String(principal?.username ?? "").trim();
    if (!username) {
      continue;
    }

    const actorId = String(principal?.actor_id ?? "").trim();
    if (actorId) {
      map.set(actorId, username);
    }

    const agentId = String(principal?.agent_id ?? "").trim();
    if (agentId) {
      map.set(agentId, username);
    }
  }

  return map;
}

export function lookupActorDisplayName(
  actorId,
  actors,
  principals = get(principalRegistry),
) {
  if (!actorId) {
    return "Unknown actor";
  }

  const map = buildActorNameMap(actors, principals);
  return map.get(actorId) ?? actorId;
}

export function getSelectedActorId(workspaceSlug = getCurrentWorkspaceSlug()) {
  if (workspaceSlug && workspaceSlug !== getCurrentWorkspaceSlug()) {
    return ensureActorState(workspaceSlug).selectedActorId;
  }

  return get(selectedActorId);
}
