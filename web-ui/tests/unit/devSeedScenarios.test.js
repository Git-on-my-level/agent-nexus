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
      requireBoards: false,
    });
    expect(scenario.personas.map((persona) => persona.actor_id)).toEqual([
      "actor-boss-kid",
      "actor-sales-kid",
      "actor-backoffice-kid",
    ]);
    expect(seed.topics.map((topic) => topic.title)).toContain(
      "Neighborhood Lemonade Stand Master Plan",
    );
    expect(seed.artifacts.length).toBeGreaterThan(0);
    expect(seed.documents[0]).toMatchObject({
      document: {
        id: "kid-boss-lemonade-plan",
        title: "Kid Boss Lemonade Plan",
      },
      content_type: "text",
    });
    expect(seed.boards).toEqual([]);
    expect(seed.cards).toEqual([]);
  });

  it("returns null for an unknown scenario", async () => {
    const mod = await import("../../scripts/dev-seed-scenarios.mjs");
    expect(mod.getDevSeedScenarioConfig("nope")).toBeNull();
  });
});
