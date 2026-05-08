import { describe, expect, it } from "vitest";

describe("dev seed scenarios", () => {
  it("uses the game dev studio scenario as the default", async () => {
    const mod = await import("../../scripts/dev-seed-scenarios.mjs");
    const scenario = mod.getDevSeedScenarioConfig("default");

    expect(scenario).toMatchObject({
      detectActorId: "actor-gds-producer",
      detectTopicTitle: "Vertical Slice: Combat + Hub Demo",
      detectBoardTitle: "Studio Production Board",
      requireBoards: true,
    });
    expect(scenario.personas[0]).toMatchObject({
      principal_kind: "human",
      default: true,
    });

    const seed = scenario.getSeedData();
    expect(seed.topics).toHaveLength(4);
    expect(seed.boards).toHaveLength(3);
    expect(seed.documents).toHaveLength(5);
    expect(seed.cards.length).toBeGreaterThanOrEqual(10);
  });

  it("keeps the old ops lemonade scenario available by explicit name", async () => {
    const mod = await import("../../scripts/dev-seed-scenarios.mjs");
    const scenario = mod.getDevSeedScenarioConfig("ops-lemonade");

    expect(scenario).toMatchObject({
      detectActorId: "actor-ops-ai",
      detectTopicTitle: "Emergency: Lemon Supply Disruption",
      requireBoards: true,
    });

    const seed = scenario.getSeedData();
    expect(seed.topics.map((topic) => topic.title)).toContain(
      "Emergency: Lemon Supply Disruption",
    );
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
      "actor-parent-operator",
      "actor-boss-kid",
      "actor-sales-kid",
      "actor-backoffice-kid",
    ]);
    expect(scenario.personas.map((persona) => persona.auth_username)).toEqual([
      "dev.pat",
      "milo",
      "ruby",
      "theo",
    ]);
    expect(scenario.personas[0]).toMatchObject({
      principal_kind: "human",
      default: true,
    });
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

  it("exposes the game dev studio scenario with required seeded-test coverage", async () => {
    const mod = await import("../../scripts/dev-seed-scenarios.mjs");
    const scenario = mod.getDevSeedScenarioConfig("game-dev-studio");
    const seed = scenario.getSeedData();

    expect(scenario).toMatchObject({
      detectActorId: "actor-gds-producer",
      detectTopicTitle: "Vertical Slice: Combat + Hub Demo",
      detectBoardTitle: "Studio Production Board",
      requireBoards: true,
    });
    expect(scenario.personas.map((persona) => persona.actor_id)).toEqual([
      "actor-gds-producer",
      "actor-gds-gameplay",
      "actor-gds-art",
      "actor-gds-narrative",
      "actor-gds-qa",
    ]);
    expect(scenario.personas[0]).toMatchObject({
      principal_kind: "human",
      default: true,
    });
    expect(seed.topics).toHaveLength(4);
    expect(seed.boards).toHaveLength(3);
    expect(seed.documents).toHaveLength(5);
    expect(
      seed.documents.every((document) =>
        String(document.backing_thread_id ?? "").startsWith("thread-gds-doc-"),
      ),
    ).toBe(true);
    expect(seed.cards.length).toBeGreaterThanOrEqual(10);
    expect(seed.cards.some((card) => card.column_key === "backlog")).toBe(true);
    expect(seed.cards.some((card) => card.column_key === "done")).toBe(true);
    expect(
      Object.values(seed.documentRevisions).every(
        (revisions) => revisions.length >= 2,
      ),
    ).toBe(true);

    const messageEvents = seed.events.filter(
      (event) => event.type === "message_posted",
    );
    const documentMessages = messageEvents.filter(
      (event) => event.payload?.subject_kind === "document",
    );
    const documentBackingThreads = new Set(
      seed.documents.map((document) => document.backing_thread_id),
    );
    const surfaceCounts = countMessageSurfaces(messageEvents);
    expect(messageEvents.length).toBeGreaterThanOrEqual(100);
    expect(surfaceCounts.topic).toBeGreaterThan(0);
    expect(surfaceCounts.document).toBeGreaterThanOrEqual(50);
    expect(
      documentMessages.every(
        (event) => event.payload?.kind === "document_message",
      ),
    ).toBe(true);
    expect(
      documentMessages.every((event) =>
        documentBackingThreads.has(event.thread_id),
      ),
    ).toBe(true);
    expect(surfaceCounts.card).toBeGreaterThan(0);
    expect(surfaceCounts.topicReplies).toBeGreaterThan(0);
    expect(surfaceCounts.documentReplies).toBeGreaterThan(0);
    expect(surfaceCounts.cardReplies).toBeGreaterThan(0);
    expect(seed.events.some((e) => e.type === "document_revised")).toBe(true);
    expect(seed.events.some((e) => e.type === "card_moved")).toBe(true);
    expect(seed.events.some((e) => e.type === "topic_updated")).toBe(true);
    expect(
      seed.events.some((e) => e.type === "human_attention_requested"),
    ).toBe(true);
    const attn = seed.events.filter(
      (e) => e.type === "human_attention_requested",
    );
    expect(attn.length).toBeGreaterThanOrEqual(3);
    expect(
      attn.every((e) => Array.isArray(e.payload?.response_proposals)),
    ).toBe(true);
  });

  it("returns null for an unknown scenario", async () => {
    const mod = await import("../../scripts/dev-seed-scenarios.mjs");
    expect(mod.getDevSeedScenarioConfig("nope")).toBeNull();
  });
});

function countMessageSurfaces(events) {
  return events.reduce(
    (counts, event) => {
      const payload = event.payload ?? {};
      const kind = String(payload.subject_kind ?? payload.kind ?? "");
      const surface = kind.includes("document")
        ? "document"
        : kind.includes("card")
          ? "card"
          : kind.includes("topic")
            ? "topic"
            : "";
      if (!surface) {
        return counts;
      }
      counts[surface] += 1;
      if (payload.reply_to_event_id) {
        counts[`${surface}Replies`] += 1;
      }
      return counts;
    },
    {
      topic: 0,
      document: 0,
      card: 0,
      topicReplies: 0,
      documentReplies: 0,
      cardReplies: 0,
    },
  );
}
