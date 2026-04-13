import { getScenarioSeedConfig } from "../../cli/dogfood/pi/seed/scenario-seeds.mjs";
import { DEV_FIXTURE_PERSONAS, getDevSeedData } from "../src/lib/devSeedData.js";

const KIDS_LEMONADE_STAND_PERSONAS = [
  {
    persona_id: "pat",
    actor_id: "actor-parent-operator",
    auth_username: "dev.pat",
    display_label: "Pat (Parent operator)",
    principal_kind: "human",
    default: true,
    dev_bridge: false,
  },
  {
    persona_id: "milo",
    actor_id: "actor-boss-kid",
    auth_username: "milo",
    display_label: "Milo Bosserson",
    principal_kind: "agent",
    default: false,
    dev_bridge: false,
  },
  {
    persona_id: "ruby",
    actor_id: "actor-sales-kid",
    auth_username: "ruby",
    display_label: "Ruby Pitch",
    principal_kind: "agent",
    default: false,
    dev_bridge: false,
  },
  {
    persona_id: "theo",
    actor_id: "actor-backoffice-kid",
    auth_username: "theo",
    display_label: "Theo Squeeze",
    principal_kind: "agent",
    default: false,
    dev_bridge: false,
  },
];

const kidsLemonadeStandScenarioConfig = getScenarioSeedConfig(
  "kids-lemonade-stand",
);

/**
 * Dev seed scenario registry.
 *
 * Convention: every scenario should include a persona with
 * `principal_kind: "human"` and `default: true` so that `make serve`
 * auto-authenticates the human operator on page load. If a scenario
 * intentionally has no human operator, set `noDefaultHuman: true` on
 * its config entry to suppress the seed-time warning.
 */
const scenarioConfigs = {
  default: {
    defaultActorId: "actor-ops-ai",
    detectActorId: "actor-ops-ai",
    detectTopicTitle: "Emergency: Lemon Supply Disruption",
    requireBoards: true,
    personas: DEV_FIXTURE_PERSONAS,
    getSeedData: getDevSeedData,
  },
  "kids-lemonade-stand": {
    defaultActorId: kidsLemonadeStandScenarioConfig.defaultActorId,
    detectActorId: kidsLemonadeStandScenarioConfig.detectActorId,
    detectTopicTitle: kidsLemonadeStandScenarioConfig.detectTopicTitle,
    detectBoardTitle: kidsLemonadeStandScenarioConfig.detectBoardTitle,
    requireBoards: true,
    personas: KIDS_LEMONADE_STAND_PERSONAS,
    getSeedData: getKidsLemonadeStandSeedForWebUi,
  },
};

export function getDevSeedScenarioConfig(name) {
  const key = normalizeScenarioName(name);
  return scenarioConfigs[key] ?? null;
}

export function listDevSeedScenarioNames() {
  return Object.keys(scenarioConfigs);
}

function normalizeScenarioName(name) {
  const value = String(name ?? "default").trim();
  return value || "default";
}

function getKidsLemonadeStandSeedForWebUi() {
  const seed = kidsLemonadeStandScenarioConfig.getSeedData({
    target: "web-ui",
    chapters: "all",
  });
  return {
    actors: seed.actors,
    topics: (seed.threads ?? []).map(mapThreadToTopic),
    artifacts: seed.artifacts ?? [],
    documents: seed.documents ?? [],
    documentRevisions: seed.documentRevisions ?? {},
    boards: seed.boards ?? [],
    cards: seed.cards ?? [],
    packets: seed.packets ?? [],
    events: seed.events ?? [],
  };
}

function mapThreadToTopic(thread) {
  return {
    id: thread.id,
    thread_id: thread.id,
    type: normalizeTopicType(thread.type),
    title: thread.title,
    status: thread.status,
    summary: String(thread.current_summary ?? thread.title ?? "").trim(),
    owner_refs: thread.updated_by ? [`actor:${thread.updated_by}`] : [],
    board_refs: [],
    document_refs: [],
    related_refs: (thread.key_artifacts ?? []).map(
      (artifactId) => `artifact:${String(artifactId ?? "").trim()}`,
    ),
    provenance: thread.provenance,
    updated_by: thread.updated_by,
    created_by: thread.updated_by,
  };
}

function normalizeTopicType(type) {
  switch (String(type ?? "").trim()) {
    case "initiative":
      return "initiative";
    case "case":
      return "incident";
    case "process":
      return "other";
    default:
      return "other";
  }
}
