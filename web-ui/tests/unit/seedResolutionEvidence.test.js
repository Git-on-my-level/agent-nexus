import { describe, expect, it } from "vitest";

import {
  getDevSeedScenarioConfig,
  listDevSeedScenarioNames,
} from "../../scripts/dev-seed-scenarios.mjs";
import {
  backingThreadsAvailableBeforeCards,
  listResolutionEvidenceViolations,
  resolutionEvidenceToCreateBeforeCard,
} from "../../scripts/seed-resolution-evidence.mjs";

describe("seed resolution evidence order", () => {
  it("requires default GDS done-card events before those cards", async () => {
    const scenario = getDevSeedScenarioConfig("default");
    const seed = scenario.getSeedData();
    const checkpoint = seed.cards.find(
      (card) => card.id === "card-gds-checkpoint-done",
    );
    const capsule = seed.cards.find(
      (card) => card.id === "card-gds-capsule-done",
    );
    expect(checkpoint?.resolution_refs).toEqual([
      "event:evt-gds-vertical-slice-001",
    ]);
    expect(capsule?.resolution_refs).toEqual(["event:evt-gds-launch-001"]);

    const beforeThreads = backingThreadsAvailableBeforeCards(seed);
    for (const card of [checkpoint, capsule]) {
      const evidence = resolutionEvidenceToCreateBeforeCard(seed, card);
      expect(evidence).toHaveLength(1);
      expect(evidence[0].kind).toBe("event");
      expect(evidence[0].record).toBeTruthy();
      expect(beforeThreads.has(String(evidence[0].record.thread_id))).toBe(
        true,
      );
    }
  });

  it("keeps every scenario's resolution evidence seedable before cards", async () => {
    for (const name of listDevSeedScenarioNames()) {
      const cfg = getDevSeedScenarioConfig(name);
      const seed = cfg.getSeedData();
      const violations = listResolutionEvidenceViolations(seed);
      expect(violations, `${name}: ${violations.join(" | ")}`).toEqual([]);

      const createPlan = [];
      for (const card of seed.cards ?? []) {
        const cardId =
          String(card?.id ?? "").trim() ||
          String(card?.thread_id ?? "").trim();
        for (const item of resolutionEvidenceToCreateBeforeCard(seed, card)) {
          createPlan.push(`${item.kind}:${item.id}`);
          createPlan.push(`card:${cardId}`);
        }
      }
      for (let i = 0; i + 1 < createPlan.length; i += 2) {
        const evidenceRef = createPlan[i];
        expect(
          evidenceRef.startsWith("event:") ||
            evidenceRef.startsWith("artifact:"),
        ).toBe(true);
        expect(createPlan[i + 1].startsWith("card:")).toBe(true);
      }
    }
  });

  it("flags a done card whose evidence event lives on a card-owned thread", () => {
    const seed = {
      topics: [{ id: "topic-a", thread_id: "thread-topic-a" }],
      documents: [],
      artifacts: [],
      events: [
        {
          id: "evt-on-card",
          thread_id: "card-done",
        },
      ],
      cards: [
        {
          id: "card-done",
          column_key: "done",
          resolution_refs: ["event:evt-on-card"],
        },
      ],
    };
    expect(listResolutionEvidenceViolations(seed)).toEqual([
      "card:card-done: resolution event evt-on-card is on thread card-done, which is not a topic/document thread available before cards",
    ]);
  });

  it("flags a missing resolution event", () => {
    const seed = {
      topics: [{ id: "topic-a", thread_id: "thread-topic-a" }],
      documents: [],
      artifacts: [],
      events: [],
      cards: [
        {
          id: "card-done",
          column_key: "done",
          resolution_refs: ["event:evt-missing"],
        },
      ],
    };
    expect(listResolutionEvidenceViolations(seed)).toEqual([
      "card:card-done: missing event evt-missing for resolution_refs",
    ]);
  });
});
