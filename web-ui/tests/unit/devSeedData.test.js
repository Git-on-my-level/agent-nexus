import { describe, expect, it } from "vitest";

function assertTypedRef(refValue) {
  const value = String(refValue ?? "").trim();
  const separator = value.indexOf(":");
  expect(separator).toBeGreaterThan(0);
  expect(separator).toBeLessThan(value.length - 1);
}

describe("dev seed fixtures", () => {
  it("exposes topic, board, card, and clean artifact seed views", async () => {
    const mod = await import("../../src/lib/devSeedData.js");
    const seed = mod.getDevSeedData();

    expect(seed.topics[0]).toMatchObject({
      id: "thread-lemon-shortage",
      thread_id: "thread-lemon-shortage",
      state: "active",
    });
    expect(
      seed.topics.find((t) => t.id === "thread-lemon-shortage")?.owner_refs,
    ).toEqual(["actor:actor-supply-rover"]);
    const maintenance = seed.topics.find(
      (t) => t.id === "thread-squeezebot-maintenance",
    );
    const pricing = seed.topics.find((t) => t.id === "thread-pricing-glitch");
    expect(maintenance?.state).toBe("archived");
    expect(pricing?.state).toBe("archived");
    const pricingAudit = seed.cards.find((c) => c.id === "card-pricing-audit");
    expect(pricingAudit?.resolution).toBeNull();
    expect(pricingAudit?.trashed_at).toBeTruthy();
    expect(seed.boards[0]).toMatchObject({
      id: "board-product-launch",
      thread_id: "thread-q2-initiative",
    });
    expect(seed.cards[0]).toMatchObject({
      board_id: "board-product-launch",
      thread_id: "thread-summer-menu",
      topic_ref: "topic:summer-menu",
      resolution: null,
    });
    expect(seed.cards[0].thread_ref).toBeUndefined();
    expect(seed.packets).toEqual([]);
    expect(
      seed.artifacts.every((artifact) =>
        ["doc", "card", "agent_wake"].includes(String(artifact.kind ?? "")),
      ),
    ).toBe(false);
    expect(
      seed.artifacts.some((artifact) =>
        ["receipt", "review"].includes(String(artifact.kind ?? "")),
      ),
    ).toBe(false);
    expect(
      seed.events.some((event) =>
        ["receipt_added", "review_completed"].includes(
          String(event.type ?? ""),
        ),
      ),
    ).toBe(false);
    const exportedRefs = [
      ...seed.topics.flatMap((topic) => topic.related_refs ?? []),
      ...seed.cards.flatMap((card) => [
        ...(card.related_refs ?? []),
        ...(card.resolution_refs ?? []),
      ]),
      ...seed.events.flatMap((event) => event.refs ?? []),
    ];
    expect(
      exportedRefs.some(
        (ref) =>
          String(ref).startsWith("artifact:artifact-receipt-") ||
          String(ref).startsWith("artifact:artifact-review-"),
      ),
    ).toBe(false);
  });

  it("normalizes topic related_refs into typed refs", async () => {
    const mod = await import("../../src/lib/devSeedData.js");
    const seed = mod.getDevSeedData();

    seed.topics.forEach((topic) => {
      (topic.related_refs ?? []).forEach(assertTypedRef);
    });
    const lemon = seed.topics.find((t) => t.id === "thread-lemon-shortage");
    expect(lemon?.related_refs ?? []).toContain(
      "artifact:artifact-supplier-sla",
    );
  });

  it("maps mock-style thread ids to topic refs", async () => {
    const mod = await import("../../src/lib/devSeedData.js");
    expect(mod.mockTopicRefFromThreadId("thread-summer-menu")).toBe(
      "topic:summer-menu",
    );
    expect(mod.mockTopicRefSuffixFromThreadId("thread-summer-menu")).toBe(
      "summer-menu",
    );
  });

  it("keeps decision lifecycle seed events thread-anchored with optional topic refs", async () => {
    const mod = await import("../../src/lib/devSeedData.js");
    const seed = mod.getDevSeedData();
    const eventIds = new Set(["evt-price-003", "evt-price-008"]);

    const migratedEvents = seed.events.filter((event) =>
      eventIds.has(event.id),
    );
    expect(migratedEvents).toHaveLength(2);
    migratedEvents.forEach((event) => {
      expect(event.refs).toContain("thread:thread-pricing-glitch");
      expect(event.refs).toContain("topic:pricing-glitch");
    });
  });

  it("rejects unknown thread: refs and orphan thread_id fields in dev seeds (static scan)", async () => {
    const { listDevSeedThreadRefViolations } =
      await import("../../src/lib/devSeedData.js");
    const { getDevSeedScenarioConfig, listDevSeedScenarioNames } =
      await import("../../scripts/dev-seed-scenarios.mjs");

    for (const name of listDevSeedScenarioNames()) {
      const cfg = getDevSeedScenarioConfig(name);
      expect(cfg?.getSeedData, `scenario ${name}`).toBeTruthy();
      const scenarioSeed = cfg.getSeedData();
      const violations = listDevSeedThreadRefViolations(scenarioSeed);
      expect(violations, `scenario ${name}: ${violations.join(" | ")}`).toEqual(
        [],
      );
    }
  });
});
