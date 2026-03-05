import { describe, expect, it } from "vitest";

import {
  ACTOR_STORAGE_KEY,
  buildActorCreatePayload,
  chooseActor,
  initializeActorSession,
  loadStoredActorId,
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

describe("actor session / gate logic", () => {
  it("decides gate visibility from readiness + actor selection", () => {
    expect(shouldShowActorGate(false, "")).toBe(false);
    expect(shouldShowActorGate(true, "")).toBe(true);
    expect(shouldShowActorGate(true, "actor-123")).toBe(false);
  });

  it("persists and restores selected actor id", () => {
    const storage = createMemoryStorage();

    expect(loadStoredActorId(storage)).toBe("");
    expect(saveSelectedActorId("actor-123", storage)).toBe("actor-123");
    expect(loadStoredActorId(storage)).toBe("actor-123");

    expect(chooseActor("actor-789", storage)).toBe("actor-789");
    expect(storage.getItem(ACTOR_STORAGE_KEY)).toBe("actor-789");

    expect(initializeActorSession(storage)).toBe("actor-789");
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
});
