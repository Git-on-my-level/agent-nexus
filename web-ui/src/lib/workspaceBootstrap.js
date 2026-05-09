import { get } from "svelte/store";

import { goto } from "$app/navigation";
import { page } from "$app/stores";
import { dev } from "$app/environment";

import {
  authenticatedAgent,
  authSessionReady,
  initializeAuthSession,
  isHumanWorkspacePrincipal,
} from "$lib/authSession";
import {
  actorSessionReady,
  chooseActor,
  clearSelectedActor,
  initializeActorSession,
  replaceActorRegistry,
  replacePrincipalRegistry,
  selectedActorId,
} from "$lib/actorSession";
import { listAllPrincipals } from "$lib/authPrincipals";
import {
  appPath,
  stripBasePath,
  stripWorkspacePath,
} from "$lib/workspacePaths";
import {
  devActorMode,
  devActorModeReady,
  setDevActorMode,
  setDevActorModeReady,
} from "$lib/workspaceContext";
import {
  buildHostedSignInPath,
  sanitizeHostedReturnPath,
} from "$lib/hosted/launchFlow.js";
import { installFetchLoopGuard } from "$lib/dev/fetchLoopGuard.js";
import { createRedirectLoopGuard } from "$lib/dev/redirectLoopGuard.js";

export const WORKSPACE_BOOTSTRAP_STATES = Object.freeze({
  UNRESOLVED: "unresolved",
  HYDRATING: "hydrating",
  ANONYMOUS_DEV: "anonymous-dev",
  AUTHENTICATED_HUMAN: "authenticated-human",
  AUTHENTICATED_AGENT: "authenticated-agent",
  LOGIN_REQUIRED: "login-required",
  FAILED: "failed",
});

export function classifyWorkspaceBootstrap({
  activeWorkspaceSlug,
  identityReady,
  devActorModeReady: modeReady,
  authenticatedAgent: agent,
  hostedMode,
  devActorMode: devMode,
  onLoginRoute,
  requiresHumanSession,
  hasHumanAuthSession,
  failed = false,
}) {
  if (!activeWorkspaceSlug) {
    return WORKSPACE_BOOTSTRAP_STATES.UNRESOLVED;
  }
  if (failed) {
    return WORKSPACE_BOOTSTRAP_STATES.FAILED;
  }
  if (!identityReady || !modeReady) {
    return WORKSPACE_BOOTSTRAP_STATES.HYDRATING;
  }
  if (isHumanWorkspacePrincipal(agent)) {
    return WORKSPACE_BOOTSTRAP_STATES.AUTHENTICATED_HUMAN;
  }
  if (agent) {
    return WORKSPACE_BOOTSTRAP_STATES.AUTHENTICATED_AGENT;
  }
  if (
    onLoginRoute ||
    hostedMode ||
    !devMode ||
    (requiresHumanSession && !hasHumanAuthSession)
  ) {
    return WORKSPACE_BOOTSTRAP_STATES.LOGIN_REQUIRED;
  }
  return WORKSPACE_BOOTSTRAP_STATES.ANONYMOUS_DEV;
}

export function shouldRedirectToLoginForBootstrapState(state) {
  return state === WORKSPACE_BOOTSTRAP_STATES.LOGIN_REQUIRED;
}

function currentAppPathFromPageSnapshot(snapshot) {
  const workspace = snapshot.data?.workspace ?? null;
  const workspaceSlug = workspace?.slug ?? "";
  const organizationSlug =
    workspace?.organizationSlug ?? snapshot.params?.organization ?? "";
  return workspaceSlug && organizationSlug
    ? stripWorkspacePath(snapshot.url.pathname, organizationSlug, workspaceSlug)
    : stripBasePath(snapshot.url.pathname);
}

export function needsLoginRedirectNow() {
  const snapshot = get(page);
  const workspace = snapshot.data?.workspace ?? null;
  const workspaceSlug = workspace?.slug ?? "";
  if (!workspaceSlug) {
    return false;
  }
  if (!(get(actorSessionReady) && get(authSessionReady))) {
    return false;
  }
  if (!get(devActorModeReady)) {
    return false;
  }

  const appPathNow = currentAppPathFromPageSnapshot(snapshot);
  if (appPathNow === "/login") {
    return false;
  }

  const agent = get(authenticatedAgent);
  const requiresHumanSession = appPathNow === "/secrets";
  const hasHumanAuthSession = isHumanWorkspacePrincipal(agent);
  const hostedMode = snapshot.data?.shellCapabilities?.mode === "hosted";

  return (
    ((hostedMode || !get(devActorMode)) && !agent) ||
    (get(devActorMode) && requiresHumanSession && !hasHumanAuthSession)
  );
}

export function buildLoginRedirectDestination({
  hostedMode,
  organizationSlug,
  workspaceSlug,
  workspaceId,
  currentAppPath,
  search = "",
  workspacePath,
}) {
  const returnPath = sanitizeHostedReturnPath(
    `${currentAppPath || "/"}${search || ""}`,
  );
  if (hostedMode) {
    return buildHostedSignInPath({
      organizationSlug,
      workspaceSlug,
      workspaceId,
      returnPath,
    });
  }

  const loginPath = workspacePath(organizationSlug, workspaceSlug, "/login");
  const params = new URLSearchParams();
  if (returnPath !== "/") {
    params.set("return_to", returnPath);
  }
  return params.size > 0 ? `${loginPath}?${params.toString()}` : loginPath;
}

export function installWorkspaceBootstrapLoopGuards({ browser }) {
  if (!browser) {
    return { loginRedirectGuard: null };
  }

  installFetchLoopGuard(
    dev
      ? {}
      : {
          onTrip: (message, info) => {
            console.warn(message, info);
          },
        },
  );

  return {
    loginRedirectGuard: createRedirectLoopGuard(
      dev
        ? {}
        : {
            onTrip: (message, info) => {
              console.warn(message, info);
            },
          },
    ),
  };
}

export function createLoginRedirectController() {
  let generation = 0;

  return {
    async redirectIfNeeded({
      destination,
      workspaceSlug,
      loginRedirectGuard,
      fetchFn = globalThis.fetch.bind(globalThis),
    }) {
      const currentGeneration = ++generation;
      await initializeAuthSession({
        fetchFn,
        workspaceSlug,
        authDriver: "layout",
      });
      if (currentGeneration !== generation) {
        return;
      }
      if (!needsLoginRedirectNow()) {
        return;
      }
      if (
        loginRedirectGuard &&
        !loginRedirectGuard.shouldNavigate(destination)
      ) {
        return;
      }
      await goto(destination);
    },
  };
}

export async function loadDevFixturePersonas({
  fetchFn = globalThis.fetch.bind(globalThis),
  workspaceSlug,
  workspaceHeader,
}) {
  try {
    const response = await fetchFn(appPath("/auth/dev/identities"), {
      headers: { [workspaceHeader]: workspaceSlug },
    });
    if (!response.ok) {
      return [];
    }
    const payload = await response.json();
    return Array.isArray(payload.personas) ? payload.personas : [];
  } catch {
    return [];
  }
}

export async function activateDevPersonaSession({
  personaId,
  workspaceSlug,
  workspaceHeader,
  fetchFn = globalThis.fetch.bind(globalThis),
  setBusy = () => {},
  onHydrate,
}) {
  const trimmed = String(personaId ?? "").trim();
  if (!workspaceSlug || !trimmed) {
    return { ok: false, status: 0 };
  }

  setBusy(true);
  try {
    const response = await fetchFn(appPath("/auth/dev/session"), {
      method: "POST",
      headers: {
        "content-type": "application/json",
        [workspaceHeader]: workspaceSlug,
      },
      body: JSON.stringify({ persona_id: trimmed }),
    });
    if (!response.ok) {
      return { ok: false, status: response.status };
    }
    if (onHydrate) {
      await onHydrate(workspaceSlug);
    }
    return { ok: true };
  } finally {
    setBusy(false);
  }
}

export function actorRegistryHasActor(actorId, actors = []) {
  const wanted = String(actorId ?? "").trim();
  if (!wanted) {
    return false;
  }
  return actors.some(
    (actor) => String(actor?.id ?? actor?.actor_id ?? "").trim() === wanted,
  );
}

export function defaultHumanActorIdFromDevFixtures(devFixturePersonas) {
  return (
    devFixturePersonas.find(
      (persona) =>
        String(persona?.principal_kind ?? "").toLowerCase() === "human" &&
        persona?.default === true,
    )?.actor_id ?? ""
  );
}

export function reconcileDevActorSelection({
  workspaceSlug,
  agent,
  actors,
  devFixturePersonas,
  storage,
}) {
  const authenticatedActorId = String(agent?.actor_id ?? "").trim();
  if (actorRegistryHasActor(authenticatedActorId, actors)) {
    chooseActor(authenticatedActorId, storage, workspaceSlug);
    return;
  }

  const storedActorId = String(get(selectedActorId) ?? "").trim();
  if (actorRegistryHasActor(storedActorId, actors)) {
    return;
  }

  const defaultActorId = defaultHumanActorIdFromDevFixtures(devFixturePersonas);
  if (actorRegistryHasActor(defaultActorId, actors)) {
    chooseActor(defaultActorId, storage, workspaceSlug);
    return;
  }

  if (storedActorId) {
    clearSelectedActor(storage, workspaceSlug);
  }
}

export function mergePrincipals(...principalLists) {
  const seen = new Set();
  const merged = [];

  for (const principals of principalLists) {
    for (const principal of principals ?? []) {
      const agentId = String(principal?.agent_id ?? "").trim();
      const actorId = String(principal?.actor_id ?? "").trim();
      const username = String(principal?.username ?? "").trim();
      const key = `${agentId}\n${actorId}\n${username}`;
      if (!key.trim() || seen.has(key)) {
        continue;
      }
      seen.add(key);
      merged.push(principal);
    }
  }

  return merged;
}

export async function refreshWorkspacePrincipals({
  coreClient,
  workspaceSlug,
  seedPrincipals = [],
}) {
  const seeded = mergePrincipals(seedPrincipals);
  replacePrincipalRegistry(seeded, workspaceSlug);

  if (seeded.length === 0) {
    return;
  }

  try {
    const principals = await listAllPrincipals(coreClient, { limit: 200 });
    replacePrincipalRegistry(
      mergePrincipals(principals, seeded),
      workspaceSlug,
    );
  } catch {
    replacePrincipalRegistry(seeded, workspaceSlug);
  }
}

export async function hydrateWorkspaceBootstrap({
  workspaceSlug,
  workspaceHeader,
  coreClient,
  storage,
  fetchFn = globalThis.fetch.bind(globalThis),
  onActorError = () => {},
  onLoadingActors = () => {},
  onDevPersonaBusy = () => {},
  onDevFixturePersonas = () => {},
  refreshActors,
}) {
  setDevActorModeReady(false);
  initializeActorSession(storage, workspaceSlug);
  let agent = await initializeAuthSession({
    fetchFn,
    workspaceSlug,
    authDriver: "layout",
  });
  replacePrincipalRegistry(agent ? [agent] : [], workspaceSlug);

  try {
    const handshake = await coreClient.getHandshake();
    const devActorModeEnabled = handshake.dev_actor_mode === true;
    setDevActorMode(devActorModeEnabled);
    let devFixturePersonas = [];

    if (devActorModeEnabled) {
      devFixturePersonas = await loadDevFixturePersonas({
        fetchFn,
        workspaceSlug,
        workspaceHeader,
      });
      onDevFixturePersonas(devFixturePersonas);
    } else {
      onDevFixturePersonas([]);
    }

    if (devActorModeEnabled && !agent) {
      agent = await activateDefaultDevPersonaSession({
        workspaceSlug,
        workspaceHeader,
        fetchFn,
        onDevPersonaBusy,
      });
      if (agent) {
        replacePrincipalRegistry([agent], workspaceSlug);
        chooseActor(agent.actor_id, storage, workspaceSlug);
      }
    }

    if (devActorModeEnabled || agent) {
      const actors = await refreshActors(workspaceSlug);
      reconcileDevActorSelection({
        workspaceSlug,
        agent,
        actors,
        devFixturePersonas,
        storage,
      });
    } else {
      onActorError("");
      onLoadingActors(false);
      replaceActorRegistry([], workspaceSlug);
    }
  } catch {
    setDevActorMode(false);
    onActorError("");
    onLoadingActors(false);
    onDevFixturePersonas([]);
    replaceActorRegistry([], workspaceSlug);
  } finally {
    setDevActorModeReady(true);
  }
}

async function activateDefaultDevPersonaSession({
  workspaceSlug,
  workspaceHeader,
  fetchFn,
  onDevPersonaBusy,
}) {
  try {
    const response = await fetchFn(appPath("/auth/dev/default-persona"), {
      headers: { [workspaceHeader]: workspaceSlug },
    });
    if (!response.ok) {
      return null;
    }
    const data = await response.json();
    const personaId = data?.persona?.persona_id;
    if (!personaId) {
      return null;
    }

    onDevPersonaBusy(true);
    try {
      const sessionResponse = await fetchFn(appPath("/auth/dev/session"), {
        method: "POST",
        headers: {
          "content-type": "application/json",
          [workspaceHeader]: workspaceSlug,
        },
        body: JSON.stringify({ persona_id: personaId }),
      });
      if (!sessionResponse.ok) {
        return null;
      }
      return await initializeAuthSession({
        fetchFn,
        workspaceSlug,
        authDriver: "layout",
      });
    } finally {
      onDevPersonaBusy(false);
    }
  } catch {
    return null;
  }
}
