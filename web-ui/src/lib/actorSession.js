import { get, writable } from "svelte/store";

import {
  getCurrentWorkspaceSlug,
  currentWorkspaceSlug,
} from "./workspaceContext.js";
import { buildWorkspaceStorageKey } from "./workspacePaths.js";
import { buildLegacyProjectStorageKey } from "./compat/workspaceCompat.js";
import {
  isReservedSystemActorId,
  SYSTEM_ACTOR_DISPLAY_LABEL,
} from "./systemActor.js";

export const ACTOR_STORAGE_KEY = "anx_ui_actor_id";

export const actorSessionReady = writable(false);
export const selectedActorId = writable("");
export const actorRegistry = writable([]);
export const principalRegistry = writable([]);
/** True when the actor or principal list stopped early or failed. */
export const actorDirectoryIncomplete = writable(false);
export const principalDirectoryIncomplete = writable(false);
/**
 * Has a directory read landed for this workspace yet?
 *
 * A list that prints owner names needs to tell "nobody has told me this
 * actor's name" from "I have not asked yet": without the distinction every
 * row paints the raw id on first frame and swaps to the name a moment later,
 * and the page reads as if it changed its mind. A row shows a placeholder
 * while these are false. Both flip on the first read, successful or not —
 * a failed read is an answer, and the fallback label is then the truth.
 */
export const actorDirectoryLoaded = writable(false);
export const agentDirectoryLoaded = writable(false);
/**
 * Agent roster summaries (`GET /agents`) for the current workspace. A derived
 * agent is named by its host relation ("codex on workstation-a") wherever the UI
 * says who did something; see `buildActorNameMap`.
 */
export const agentRegistry = writable([]);

const actorStateByWorkspace = new Map();

function createEmptyActorState() {
  return {
    ready: false,
    selectedActorId: "",
    actorRegistry: [],
    principalRegistry: [],
    agentRegistry: [],
    actorDirectoryIncomplete: false,
    principalDirectoryIncomplete: false,
    actorDirectoryLoaded: false,
    agentDirectoryLoaded: false,
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
  agentRegistry.set([...state.agentRegistry]);
  actorDirectoryIncomplete.set(Boolean(state.actorDirectoryIncomplete));
  principalDirectoryIncomplete.set(Boolean(state.principalDirectoryIncomplete));
  actorDirectoryLoaded.set(Boolean(state.actorDirectoryLoaded));
  agentDirectoryLoaded.set(Boolean(state.agentDirectoryLoaded));
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
    if (isReservedSystemActorId(scopedActorId)) {
      storage.removeItem(actorStorageKey(workspaceSlug));
      storage.removeItem(ACTOR_STORAGE_KEY);
      return "";
    }
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
  if (actorId && isReservedSystemActorId(actorId)) {
    actorId = "";
  }
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
  state.actorDirectoryLoaded = true;
  syncCurrentWorkspaceStores(workspaceSlug);
  return state.actorRegistry;
}

export function setActorDirectoryIncomplete(
  incomplete,
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  const state = ensureActorState(workspaceSlug);
  state.actorDirectoryIncomplete = Boolean(incomplete);
  syncCurrentWorkspaceStores(workspaceSlug);
}

export function setPrincipalDirectoryIncomplete(
  incomplete,
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  const state = ensureActorState(workspaceSlug);
  state.principalDirectoryIncomplete = Boolean(incomplete);
  syncCurrentWorkspaceStores(workspaceSlug);
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

/**
 * Replace the agent roster used for names. Re-publishes the actor registry so
 * every label derived from `$actorRegistry` picks up the new names.
 */
export function replaceAgentRegistry(
  agents,
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  const state = ensureActorState(workspaceSlug);
  state.agentRegistry = [...(agents ?? [])];
  state.agentDirectoryLoaded = true;
  if (String(workspaceSlug ?? "").trim() === getCurrentWorkspaceSlug()) {
    syncCurrentWorkspaceStores(workspaceSlug);
  }
  return state.agentRegistry;
}

/**
 * Record that the agent roster read finished without a usable roster (a
 * failed read, an empty workspace). Lists waiting on agent names stop
 * waiting: the fallback label is the best answer there is.
 */
export function markAgentDirectoryRead(
  workspaceSlug = getCurrentWorkspaceSlug(),
) {
  const state = ensureActorState(workspaceSlug);
  state.agentDirectoryLoaded = true;
  if (String(workspaceSlug ?? "").trim() === getCurrentWorkspaceSlug()) {
    syncCurrentWorkspaceStores(workspaceSlug);
  }
}

/** The roster entry for an actor id, agent id or handle, or null. */
export function findAgentSummary(value, agents = get(agentRegistry)) {
  const key = String(value ?? "")
    .trim()
    .replace(/^actor:/, "");
  if (!key) return null;
  return (
    (agents ?? []).find(
      (agent) =>
        agent?.actor_id === key || agent?.id === key || agent?.handle === key,
    ) ?? null
  );
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

/**
 * True when the value is a stable, machine-generated ANX handle (not a person-chosen
 * username). These should not be shown in UI as the "name" of an actor.
 */
export function isSyntheticAnxHandle(raw) {
  const s = String(raw ?? "").trim();
  if (!s) {
    return false;
  }
  if (/^external\.[0-9a-f]{8,128}$/i.test(s)) {
    return true;
  }
  if (/^actor_ext_[0-9a-f]{8,128}$/i.test(s)) {
    return true;
  }
  if (/^agent_ext_[0-9a-f]{8,128}$/i.test(s)) {
    return true;
  }
  // Dev passkey registrations mint `passkey.<slug>.<hex>` usernames; they are
  // machine identifiers, never a person's name.
  if (/^passkey\.[a-z0-9.]+\.[0-9a-f]{6,}$/i.test(s)) {
    return true;
  }
  return false;
}

/**
 * Human label for an owner / actor field that may arrive as `actor:<id>`,
 * a bare actor id, or an already-readable name.
 */
export function actorDisplayLabel(
  value,
  actors,
  principals = get(principalRegistry),
) {
  const raw = String(value ?? "").trim();
  if (!raw) {
    return "";
  }
  const id = raw.replace(/^actor:/, "");
  const resolved = lookupActorDisplayName(id, actors, principals);
  if (resolved && resolved !== "Unknown actor" && resolved !== id) {
    return resolved;
  }
  return humanizeUnmappedActorIdForUi(id) || id;
}

/**
 * Does a registry actually name this actor, or is the label just the field's
 * own text read back?
 *
 * `next_actor` is a free-text annotation: it holds an actor id on a card an
 * agent filed properly, and the word "agent" on one where somebody wrote down
 * a role. Both come back from `actorDisplayLabel` as something renderable, and
 * a caller that draws an avatar beside the second gets "AG agent" — initials
 * for a word that names nobody. So a caller that needs a real principal asks
 * here first and falls back to an owner it can name.
 */
export function isKnownActor(
  value,
  actors,
  principals = get(principalRegistry),
  agents = get(agentRegistry),
) {
  const id = String(value ?? "")
    .trim()
    .replace(/^actor:/, "");
  if (!id) {
    return false;
  }
  if (isReservedSystemActorId(id)) {
    return true;
  }
  return buildActorNameMap(actors, principals, agents).has(id);
}

function pickBetterActorDisplayLabel(existing, candidate) {
  const e = String(existing ?? "").trim();
  const c = String(candidate ?? "").trim();
  if (!c) {
    return e;
  }
  if (!e) {
    return c;
  }
  if (!isSyntheticAnxHandle(e) && isSyntheticAnxHandle(c)) {
    return e;
  }
  if (isSyntheticAnxHandle(e) && !isSyntheticAnxHandle(c)) {
    return c;
  }
  return e;
}

/**
 * Per-principal display label: never prefer synthetic usernames over actor registry names.
 * External / OIDC users often have usernames like `external.<hash>`; map those to a
 * human string instead of the handle.
 */
function principalDisplayLabelFromAuth(principal) {
  const u = String(principal?.username ?? "").trim();
  const authMethod = String(principal?.auth_method ?? "")
    .trim()
    .toLowerCase();
  if (u && !isSyntheticAnxHandle(u)) {
    return u;
  }
  if (
    authMethod === "external_grant" ||
    (u && /^external\./i.test(u)) ||
    (u && (u.startsWith("actor_ext_") || u.startsWith("agent_ext_")))
  ) {
    if (u && /^external\./i.test(u)) {
      return "External user";
    }
    if (u && (u.startsWith("actor_ext_") || u.startsWith("agent_ext_"))) {
      return "Workspace member";
    }
  }
  return u;
}

export function humanizeUnmappedActorIdForUi(id) {
  const s = String(id ?? "").trim();
  if (!s) {
    return "";
  }
  if (isReservedSystemActorId(s)) {
    return SYSTEM_ACTOR_DISPLAY_LABEL;
  }
  if (/^external\./i.test(s) || isSyntheticAnxHandle(s)) {
    if (/^external\./i.test(s)) {
      return "External user";
    }
    if (s.startsWith("actor_ext_") || s.startsWith("agent_ext_")) {
      return "Workspace member";
    }
  }
  return s;
}

export function buildActorNameMap(
  actors,
  principals = get(principalRegistry),
  agents = get(agentRegistry),
) {
  const map = new Map();

  for (const actor of actors ?? []) {
    const id = String(actor?.id ?? actor?.actor_id ?? "").trim();
    if (!id) {
      continue;
    }
    const rawLabel = String(
      actor?.display_name ??
        actor?.displayName ??
        actor?.username ??
        actor?.label ??
        "",
    ).trim();
    let label = rawLabel;
    if (!label || isSyntheticAnxHandle(label)) {
      label = !isSyntheticAnxHandle(id) ? id : humanizeUnmappedActorIdForUi(id);
    }
    if (!label) {
      label = "Unknown actor";
    }
    map.set(id, label);
  }

  for (const principal of principals ?? []) {
    const username = String(principal?.username ?? "").trim();
    const displayFromPrincipal = principalDisplayLabelFromAuth(principal);
    const actorId = String(principal?.actor_id ?? "").trim();
    const agentId = String(principal?.agent_id ?? "").trim();

    function mergePrincipalLabel(existingId) {
      const existing = map.get(existingId);
      // A real display name from the actors API ("Leo Park") outranks a
      // login handle from the principal ("dev.leo").
      const existingIsName =
        existing &&
        existing !== existingId &&
        !isSyntheticAnxHandle(existing) &&
        existing !== username;
      if (existingIsName) return;
      if (displayFromPrincipal && username && !isSyntheticAnxHandle(username)) {
        map.set(existingId, displayFromPrincipal);
        return;
      }
      map.set(
        existingId,
        pickBetterActorDisplayLabel(existing, displayFromPrincipal),
      );
    }

    if (actorId) {
      mergePrincipalLabel(actorId);
    }
    if (agentId) {
      mergePrincipalLabel(agentId);
    }
    if (username) {
      const forUsername =
        isSyntheticAnxHandle(username) &&
        displayFromPrincipal &&
        !isSyntheticAnxHandle(displayFromPrincipal)
          ? displayFromPrincipal
          : isSyntheticAnxHandle(username)
            ? displayFromPrincipal || humanizeUnmappedActorIdForUi(username)
            : displayFromPrincipal || username;
      map.set(
        username,
        pickBetterActorDisplayLabel(map.get(username), forUsername),
      );
    }
  }

  // A host-derived agent is named by its host relation ("codex on workstation-a"),
  // which outranks the actor's own display name: the host is what tells the
  // operator where the work ran.
  for (const agent of agents ?? []) {
    const label = String(agent?.display_name ?? "").trim();
    if (!label || !agent?.host_id) continue;
    for (const key of [agent.actor_id, agent.id, agent.handle]) {
      const id = String(key ?? "").trim();
      if (id) map.set(id, label);
    }
  }

  return map;
}

export function lookupActorDisplayName(
  actorId,
  actors,
  principals = get(principalRegistry),
  agents = get(agentRegistry),
) {
  if (!actorId) {
    return "Unknown actor";
  }
  if (isReservedSystemActorId(actorId)) {
    return SYSTEM_ACTOR_DISPLAY_LABEL;
  }

  const map = buildActorNameMap(actors, principals, agents);
  const key = String(actorId).trim();
  const resolved = map.get(key) ?? map.get(actorId);
  const humanized = humanizeUnmappedActorIdForUi(key);
  if (resolved && !isSyntheticAnxHandle(resolved)) {
    return resolved;
  }
  if (resolved && isSyntheticAnxHandle(resolved)) {
    if (humanized && !isSyntheticAnxHandle(humanized)) {
      return humanized;
    }
  }
  if (humanized && !isSyntheticAnxHandle(humanized)) {
    return humanized;
  }
  return String(resolved || key);
}

export function getSelectedActorId(workspaceSlug = getCurrentWorkspaceSlug()) {
  if (workspaceSlug && workspaceSlug !== getCurrentWorkspaceSlug()) {
    return ensureActorState(workspaceSlug).selectedActorId;
  }

  return get(selectedActorId);
}
