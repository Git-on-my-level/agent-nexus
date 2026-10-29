import { afterEach, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import { hydrateWorkspaceBootstrap } from "../../src/lib/workspaceBootstrap.js";
import {
  authenticatedAgent,
  completeAuthSession,
  clearAuthSession,
} from "../../src/lib/authSession.js";
import {
  setCurrentOrganizationSlug,
  setCurrentWorkspaceSlug,
  setDevActorMode,
  setDevActorModeReady,
  devActorMode,
  devActorModeReady,
} from "../../src/lib/workspaceContext.js";

const workspaceSlug = "activation-cancellation";
const storage = { getItem: () => null, setItem: () => {} };
function select() {
  setCurrentOrganizationSlug("local");
  setCurrentWorkspaceSlug(workspaceSlug);
}
afterEach(() => clearAuthSession(workspaceSlug));

it("a retired adapter activation cannot replace the newer session", async () => {
  select();
  let finish;
  let current = true;
  const fetchFn = vi.fn(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const refreshActors = vi.fn();
  const pending = hydrateWorkspaceBootstrap({
    workspaceSlug,
    organizationSlug: "local",
    hostedSession: {},
    storage,
    fetchFn,
    coreClient: {},
    refreshActors,
    isCurrent: () => current,
  });
  current = false;
  completeAuthSession({ agent_id: "newer-session" }, workspaceSlug);
  finish(Response.json({ agent: { agent_id: "retired-session" } }));
  expect(await pending).toBe(false);
  expect(get(authenticatedAgent)?.agent_id).toBe("newer-session");
  expect(refreshActors).not.toHaveBeenCalled();
});

it.each(["success", "failure"])(
  "a retired handshake %s cannot reset the newer bootstrap state",
  async (outcome) => {
    select();
    let finish, fail;
    let current = true;
    const handshake = new Promise((resolve, reject) => {
      finish = resolve;
      fail = reject;
    });
    const started = Promise.withResolvers();
    const onDevFixturePersonas = vi.fn();
    const onActorError = vi.fn();
    const refreshActors = vi.fn(async () => []);
    const pending = hydrateWorkspaceBootstrap({
      workspaceSlug,
      organizationSlug: "local",
      storage,
      fetchFn: async () => Response.json({ agent: { agent_id: "human" } }),
      coreClient: {
        getHandshake: () => {
          started.resolve();
          return handshake;
        },
      },
      refreshActors,
      onDevFixturePersonas,
      onActorError,
      isCurrent: () => current,
    });
    await started.promise;
    current = false;
    setDevActorMode(false);
    setDevActorModeReady(true);
    if (outcome === "success") finish({ dev_actor_mode: true });
    else fail(new Error("late failure"));
    expect(await pending).toBe(false);
    expect(get(devActorMode)).toBe(false);
    expect(get(devActorModeReady)).toBe(true);
    expect(onDevFixturePersonas).not.toHaveBeenCalled();
    expect(onActorError).not.toHaveBeenCalled();
    expect(refreshActors).not.toHaveBeenCalled();
  },
);

it("keeps the previous actor state and reports a failed handshake", async () => {
  select();
  setDevActorMode(true);
  const onActorError = vi.fn();
  const onDevFixturePersonas = vi.fn();
  const refreshActors = vi.fn(async () => []);
  await hydrateWorkspaceBootstrap({
    workspaceSlug,
    organizationSlug: "local",
    storage,
    fetchFn: async () => Response.json({ agent: { agent_id: "human" } }),
    coreClient: {
      getHandshake: async () => {
        throw new Error("workspace server did not answer");
      },
    },
    refreshActors,
    onDevFixturePersonas,
    onActorError,
  });
  expect(onActorError).toHaveBeenCalledWith(
    "Failed to load actors: workspace server did not answer",
  );
  // Not silently switched off or emptied.
  expect(get(devActorMode)).toBe(true);
  expect(get(devActorModeReady)).toBe(true);
  expect(onDevFixturePersonas).not.toHaveBeenCalled();
  setDevActorMode(false);
});

it("stays quiet when the handshake is aborted", async () => {
  select();
  const onActorError = vi.fn();
  await hydrateWorkspaceBootstrap({
    workspaceSlug,
    organizationSlug: "local",
    storage,
    fetchFn: async () => Response.json({ agent: { agent_id: "human" } }),
    coreClient: {
      getHandshake: async () => {
        throw new DOMException("aborted", "AbortError");
      },
    },
    refreshActors: vi.fn(async () => []),
    onActorError,
  });
  expect(onActorError).not.toHaveBeenCalled();
});

it("does not sign the viewer out when the first session read hits an outage", async () => {
  select();
  const getHandshake = vi.fn();
  await expect(
    hydrateWorkspaceBootstrap({
      workspaceSlug,
      organizationSlug: "local",
      storage,
      fetchFn: async () => {
        throw new TypeError("Failed to fetch");
      },
      coreClient: { getHandshake },
      refreshActors: vi.fn(async () => []),
    }),
  ).rejects.toMatchObject({ code: "workspace_unavailable" });
  expect(getHandshake).not.toHaveBeenCalled();
  expect(get(authenticatedAgent)).toBeNull();
});
