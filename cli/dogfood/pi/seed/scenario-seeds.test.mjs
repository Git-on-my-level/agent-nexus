import test from "node:test";
import assert from "node:assert/strict";

import { getScenarioSeedConfig } from "./scenario-seeds.mjs";

test("kids lemonade stand defaults to the base Pi seed", () => {
  const scenario = getScenarioSeedConfig("kids-lemonade-stand");
  const seed = scenario.getSeedData();

  assert.equal(seed.boards, undefined);
  assert.equal(seed.cards, undefined);
  assert.equal(seed.documentRevisions, undefined);
  assert.ok(Array.isArray(seed.documents));
  assert.equal(seed.documents[0]?.document?.id, "kid-boss-lemonade-plan");
});

test("kids lemonade stand returns all chapters for web-ui seeds", () => {
  const scenario = getScenarioSeedConfig("kids-lemonade-stand");
  const seed = scenario.getSeedData({ target: "web-ui" });

  assert.equal(seed.boards.length, 1);
  assert.equal(seed.cards.length, 3);
  assert.equal(seed.documentRevisions["kid-boss-lemonade-plan"].length, 5);
  assert.equal(seed.documentRevisions["kid-sales-pitch-notebook"].length, 5);
  assert.equal(seed.documentRevisions["kid-prep-notebook"].length, 5);
  assert.ok(
    seed.events.some(
      (event) =>
        event.thread_id === "thread-kids-lemonade-sales" &&
        event.type === "message_posted",
    ),
  );
  assert.ok(
    seed.events.some(
      (event) =>
        event.thread_id === "thread-kids-lemonade-backoffice" &&
        event.type === "message_posted",
    ),
  );
  assert.ok(
    seed.events.some(
      (event) =>
        event.type === "message_posted" &&
        String(event.payload?.text ?? "").includes("@ruby") &&
        String(event.payload?.text ?? "").includes("@theo"),
    ),
  );
});
