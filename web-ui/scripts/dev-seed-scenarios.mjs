import { getKidsLemonadeStandSeedData } from "../../cli/dogfood/pi/seed/kids-lemonade-stand-data.mjs";
import { DEV_FIXTURE_PERSONAS, getDevSeedData } from "../src/lib/devSeedData.js";

const KIDS_LEMONADE_STAND_PERSONAS = [
  {
    persona_id: "milo",
    actor_id: "actor-boss-kid",
    auth_username: "dev.milo",
    display_label: "Milo Bosserson",
    principal_kind: "agent",
    default: true,
    dev_bridge: false,
  },
  {
    persona_id: "ruby",
    actor_id: "actor-sales-kid",
    auth_username: "dev.ruby",
    display_label: "Ruby Pitch",
    principal_kind: "agent",
    default: false,
    dev_bridge: false,
  },
  {
    persona_id: "theo",
    actor_id: "actor-backoffice-kid",
    auth_username: "dev.theo",
    display_label: "Theo Squeeze",
    principal_kind: "agent",
    default: false,
    dev_bridge: false,
  },
];

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
    defaultActorId: "actor-boss-kid",
    detectActorId: "actor-boss-kid",
    detectTopicTitle: "Neighborhood Lemonade Stand Master Plan",
    requireBoards: false,
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
  const seed = getKidsLemonadeStandSeedData();
  return {
    actors: seed.actors,
    topics: (seed.threads ?? []).map((thread) => ({
      id: thread.id,
      thread_id: thread.id,
      type: normalizeTopicType(thread.type),
      title: thread.title,
      status: thread.status,
      summary: String(thread.current_summary ?? thread.title ?? "").trim(),
      owner_refs: thread.updated_by
        ? [`actor:${thread.updated_by}`]
        : [],
      board_refs: [],
      document_refs: [],
      related_refs: (thread.key_artifacts ?? []).map(
        (artifactId) => `artifact:${String(artifactId ?? "").trim()}`,
      ),
      provenance: thread.provenance,
      updated_by: thread.updated_by,
      created_by: thread.updated_by,
    })),
    artifacts: seed.artifacts ?? [],
    documents: seed.documents ?? [],
    documentRevisions: {},
    boards: [],
    cards: [],
    packets: [],
    events: seed.events ?? [],
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
