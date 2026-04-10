import { getKidsLemonadeStandSeedData } from "./kids-lemonade-stand-data.mjs";
import { getPilotRescueSeedData } from "./pilot-rescue-data.mjs";

const scenarioSeedConfigs = {
  "pilot-rescue": {
    getSeedData: getPilotRescueSeedData,
    defaultActorId: "actor-product-lead",
    detectActorId: "actor-product-lead",
    detectTopicTitle: "Pilot Rescue Sprint: NorthWave Launch Readiness",
  },
  "kids-lemonade-stand": {
    getSeedData: getKidsLemonadeStandSeedData,
    defaultActorId: "actor-boss-kid",
    detectActorId: "actor-boss-kid",
    detectTopicTitle: "Neighborhood Lemonade Stand Master Plan",
  },
};

export function getScenarioSeedConfig(name) {
  return scenarioSeedConfigs[name] ?? null;
}
