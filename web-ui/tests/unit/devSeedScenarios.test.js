import { describe, expect, it } from "vitest";

describe("dev seed scenarios", () => {
  it("keeps the existing default scenario intact", async () => {
    const mod = await import("../../scripts/dev-seed-scenarios.mjs");
    const scenario = mod.getDevSeedScenarioConfig("default");

    expect(scenario).toMatchObject({
      detectActorId: "actor-ops-ai",
      detectTopicTitle: "Emergency: Lemon Supply Disruption",
      requireBoards: true,
    });
    expect(scenario.personas.length).toBeGreaterThan(0);

    const seed = scenario.getSeedData();
    expect(seed.boards.length).toBeGreaterThan(0);
    expect(seed.cards.length).toBeGreaterThan(0);
  });

  it("exposes the kids lemonade stand scenario in web-ui seed shape", async () => {
    const mod = await import("../../scripts/dev-seed-scenarios.mjs");
    const scenario = mod.getDevSeedScenarioConfig("kids-lemonade-stand");
    const seed = scenario.getSeedData();

    expect(scenario).toMatchObject({
      detectActorId: "actor-boss-kid",
      detectTopicTitle: "Neighborhood Lemonade Stand Master Plan",
      detectBoardTitle: "Saturday Lemonade Stand Mission Board",
      requireBoards: true,
    });
    expect(scenario.personas.map((persona) => persona.actor_id)).toEqual([
      "actor-boss-kid",
      "actor-sales-kid",
      "actor-backoffice-kid",
    ]);
    expect(scenario.personas.map((persona) => persona.auth_username)).toEqual([
      "milo",
      "ruby",
      "theo",
    ]);
    expect(seed.topics.map((topic) => topic.title)).toContain(
      "Neighborhood Lemonade Stand Master Plan",
    );
    expect(seed.artifacts.length).toBeGreaterThan(0);
    const bossDoc = seed.documents.find(
      (document) => document.id === "kid-boss-lemonade-plan",
    );
    expect(bossDoc).toMatchObject({
      id: "kid-boss-lemonade-plan",
      title: "Kid Boss Lemonade Plan",
    });
    expect(seed.documentRevisions["kid-boss-lemonade-plan"]).toHaveLength(5);
    expect(seed.documentRevisions["kid-sales-pitch-notebook"]).toHaveLength(5);
    expect(seed.documentRevisions["kid-prep-notebook"]).toHaveLength(5);
    expect(seed.boards[0]).toMatchObject({
      title: "Saturday Lemonade Stand Mission Board",
    });
    expect(seed.cards).toHaveLength(3);
    expect(
      seed.cards.some(
        (card) =>
          card.summary ===
          "Sales combo pitch: launch Halftime Happy Combo without overselling mint",
      ),
    ).toBe(true);
    expect(
      seed.events.some(
        (event) =>
          event.thread_id === "thread-kids-lemonade-sales" &&
          event.type === "message_posted",
      ),
    ).toBe(true);
    expect(
      seed.events.some(
        (event) =>
          event.thread_id === "thread-kids-lemonade-backoffice" &&
          event.type === "message_posted",
      ),
    ).toBe(true);
    expect(
      seed.events.some(
        (event) =>
          event.type === "message_posted" &&
          String(event.payload?.text ?? "").includes("@ruby") &&
          String(event.payload?.text ?? "").includes("@theo"),
      ),
    ).toBe(true);
  });

  it("returns null for an unknown scenario", async () => {
    const mod = await import("../../scripts/dev-seed-scenarios.mjs");
    expect(mod.getDevSeedScenarioConfig("nope")).toBeNull();
  });
});
