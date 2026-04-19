import { afterEach, describe, expect, it } from "vitest";

import {
  ACTOR_STORAGE_KEY,
  actorStorageKey,
  buildActorCreatePayload,
  chooseActor,
  initializeActorSession,
  lookupActorDisplayName,
  loadStoredActorId,
  replacePrincipalRegistry,
  saveSelectedActorId,
  shouldShowActorGate,
} from "../../src/lib/actorSession.js";

function createMemoryStorage() {
  const data = new Map();

  return {
    getItem(key) {
      return data.has(key) ? data.get(key) : null;
    },
    setItem(key, value) {
      data.set(key, String(value));
    },
    removeItem(key) {
      data.delete(key);
    },
  };
}

afterEach(() => {
  replacePrincipalRegistry([]);
});

describe("actor session / gate logic", () => {
  it("decides gate visibility from readiness + actor selection", () => {
    expect(shouldShowActorGate(false, "")).toBe(false);
    expect(shouldShowActorGate(true, "")).toBe(true);
    expect(shouldShowActorGate(true, "actor-123")).toBe(false);
  });

  it("persists and restores selected actor id", () => {
    const storage = createMemoryStorage();
    const workspaceSlug = "local";
    const storageKey = actorStorageKey(workspaceSlug);

    expect(loadStoredActorId(storage, workspaceSlug)).toBe("");
    expect(saveSelectedActorId("actor-123", storage, workspaceSlug)).toBe(
      "actor-123",
    );
    expect(loadStoredActorId(storage, workspaceSlug)).toBe("actor-123");

    expect(chooseActor("actor-789", storage, workspaceSlug)).toBe("actor-789");
    expect(storage.getItem(storageKey)).toBe("actor-789");
    expect(storage.getItem(ACTOR_STORAGE_KEY)).toBe(null);

    expect(initializeActorSession(storage, workspaceSlug)).toBe("actor-789");
  });

  it("builds actor create payload for POST /actors", () => {
    expect(
      buildActorCreatePayload({
        id: "actor-alex",
        displayName: "Alex",
        tags: ["human"],
        createdAt: "2026-03-04T00:00:00.000Z",
      }),
    ).toEqual({
      actor: {
        id: "actor-alex",
        display_name: "Alex",
        tags: ["human"],
        created_at: "2026-03-04T00:00:00.000Z",
      },
    });
  });

  it("prefers principal usernames for actor and agent identifiers", () => {
    replacePrincipalRegistry([
      {
        agent_id: "agent-26",
        actor_id: "actor-hermes",
        username: "m4-hermes",
      },
    ]);

    expect(
      lookupActorDisplayName("actor-hermes", [
        { id: "actor-hermes", display_name: "Hermes Operator" },
      ]),
    ).toBe("m4-hermes");
    expect(lookupActorDisplayName("agent-26", [])).toBe("m4-hermes");
    expect(
      lookupActorDisplayName("actor-human", [
        { id: "actor-human", display_name: "Ops Lead" },
      ]),
    ).toBe("Ops Lead");
  });
});
