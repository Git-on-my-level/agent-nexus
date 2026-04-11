import { getKidsLemonadeStandSeedData } from "./kids-lemonade-stand-data.mjs";
import {
  KIDS_LEMONADE_STAND_CHAPTER_IDS,
  getKidsLemonadeStandChapteredSeedData,
} from "./kids-lemonade-stand-chapters.mjs";
import { getPilotRescueSeedData } from "./pilot-rescue-data.mjs";

function getKidsLemonadeStandScenarioSeedData(options = {}) {
  const target = String(options?.target ?? "").trim();
  const explicitChapters = options?.chapters;

  // Pi seeds stay on the base scenario by default; richer chapter stacks are
  // for deterministic dev fixtures and explicit continuation-style callers.
  if (explicitChapters != null || target === "web-ui") {
    return getKidsLemonadeStandChapteredSeedData({
      chapters: explicitChapters ?? "all",
    });
  }

  return getKidsLemonadeStandSeedData();
}

const scenarioSeedConfigs = {
  "pilot-rescue": {
    getSeedData: getPilotRescueSeedData,
    defaultActorId: "actor-product-lead",
    detectActorId: "actor-product-lead",
    detectTopicTitle: "Pilot Rescue Sprint: NorthWave Launch Readiness",
  },
  "kids-lemonade-stand": {
    getSeedData: getKidsLemonadeStandScenarioSeedData,
    defaultActorId: "actor-boss-kid",
    detectActorId: "actor-boss-kid",
    detectTopicTitle: "Neighborhood Lemonade Stand Master Plan",
    detectBoardTitle: "Saturday Lemonade Stand Mission Board",
    chapterIds: KIDS_LEMONADE_STAND_CHAPTER_IDS,
  },
};

export function getScenarioSeedConfig(name) {
  return scenarioSeedConfigs[name] ?? null;
}
