import { describe, expect, it } from "vitest";

function assertTypedRef(refValue) {
  const value = String(refValue ?? "").trim();
  const separator = value.indexOf(":");
  expect(separator).toBeGreaterThan(0);
  expect(separator).toBeLessThan(value.length - 1);
}

describe("dev seed fixtures", () => {
  it("exposes topic, board, card, and packet seed views", async () => {
    const mod = await import("../../src/lib/devSeedData.js");
    const seed = mod.getDevSeedData();

    expect(seed.topics[0]).toMatchObject({
      id: "thread-lemon-shortage",
      thread_id: "thread-lemon-shortage",
      type: "incident",
      status: "active",
    });
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
    expect(
      seed.packets.every((packet) => packet?.artifact && packet?.packet),
    ).toBe(true);
    expect(seed.packets.map((packet) => packet.kind)).toEqual([
      "receipt",
      "review",
      "receipt",
      "review",
      "receipt",
      "review",
    ]);
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
});
